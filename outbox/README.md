# outbox

Transactional outbox for reliable post-commit side effects — queue publishes,
gRPC calls to other services, notifications — across app Go services.

The problem it solves: a business transaction commits, then a follow-up call
fails. Without an outbox that side effect is simply lost, and nothing in the
system records that it should have happened. With one, the intent is written in
the same transaction as the business state, so it either both happens or neither
does.

Delivery is **at-least-once**. Handlers must be idempotent.

*Русская версия: [README.ru.md](README.ru.md)*

For a function-by-function tour of the runtime — what calls what, in what order,
and which failure each step prevents — see [ARCHITECTURE.md](ARCHITECTURE.md).

## Layout

| Package | Contents |
|---|---|
| `outbox` | `Message`, `CreateParams`, `Repository`, `Handler`, `Registry`, `Worker`, `Pool` |
| `outbox/postgres` | GORM/PostgreSQL `Repository` implementation |
| `outbox/postgres/migrations` | Canonical DDL as an `embed.FS` |
| `outbox/metrics` | Standard Prometheus collectors and wired `WorkerHooks` |
| `outbox/cronjobs` | `Register` for the prune and lag maintenance jobs |
| `outbox/testkit` | `FakeRepository` for unit tests |

Configuration lives in `outbox.Settings` (`settings.go`): the env-driven values
every adopting service needs, with the defaults and the reasoning behind them.

## Usage

### 1. Write the row inside the business transaction

```go
ctx = c.dbTransactionManager.Begin(ctx)
// ... business writes ...

if err := c.outboxRepo.Create(ctx, outbox.CreateParams{
    AggregateType: "deposit_request",
    AggregateID:   depositRequest.ID.String(),
    EventType:     "restriction.counter.update",
    DedupeKey:     "counter:account-deposit-42",
    Payload:       payload,
}); err != nil {
    return err // rolls back; no orphan message
}

return c.dbTransactionManager.Commit(ctx)
```

`Create` picks up the ambient transaction from `ctx` itself. Calling
`WithTxFromContext(ctx)` first is still supported but no longer required — that
was too easy to forget, and forgetting it published side effects for
transactions that rolled back.

### 2. Implement a handler per event type

```go
func (h *CounterUpdateHandler) EventType() string { return "restriction.counter.update" }

func (h *CounterUpdateHandler) Handle(ctx context.Context, msg outbox.Message) error {
    var p CounterUpdatePayload
    if err := json.Unmarshal(msg.Payload, &p); err != nil {
        // A malformed payload can never succeed; do not burn ten retries on it.
        return outbox.MarkNonRetryable(err)
    }
    return h.restriction.CountersUpdate(ctx, ...) // transport errors are retryable
}
```

### 3. Wire the pool into the composition root

Nest `outbox.Settings` in the service's cron settings — envconfig recurses into
it and reads the `CRON_OUTBOX_*` tags without adding a prefix:

```go
type CronSettings struct {
    // ... service-owned jobs ...
    Outbox outbox.Settings
}
```

Then build the runtime from it:

```go
registry := outbox.NewRegistry()
registry.Register(controllers.NewCounterUpdateHandler(restrictionClient))

settings := config.Cron().Outbox
worker := outbox.NewWorker(outboxRepo, registry, settings.WorkerConfig(tools.OutboxHooks()))
pool := outbox.NewPool(worker, settings.PoolConfig())
// Queue capacity is derived from the lease; see "Configuration invariant".

g.Go(func() error { return pool.Run(gctx) })
```

### 4. Register the maintenance cron jobs

```go
outboxcron.Register(cronManager, outboxRepo, config.Cron().Outbox, tools.SetOutboxLag)
```

Both jobs are infrastructure, not service behaviour, so they come from
`outbox/cronjobs` rather than being re-implemented. Skipping `outbox.prune` lets
the table grow without bound; skipping `outbox.lag` means delivery can stop
silently, because a stalled relay looks exactly like an idle service to every
counter.

### 5. Apply the schema

Do not copy the DDL into the service's migration directory. Run
`outbox/postgres/migrations.FS` as a second migrator source against its own
history table:

```go
migrator.New(migrator.Config{
    Driver: driver, DSN: dsn,
    MigrationsFS:    outboxmigrations.FS,
    MigrationsTable: "schema_migrations_common",
})
```

Two sources cannot share one history table — they would fight over the version
counter. Shared migrations are numbered from a reserved `9_000_000+` band so they
can never collide with a service's own numbers.

## Configuration invariant

`Lease` is a **correctness** setting, not a throughput one.

A message starts burning its lease the moment `ClaimDue` returns — including the
time it spends waiting to be dispatched. If the lease expires while the message
is still queued, the relay reclaims it and hands it to a second worker, and the
side effect happens twice.

The pool derives its own capacity from the lease:

```
rounds     = (Lease - HandlerTimeout - 1s) / HandlerTimeout
queueSize  = Workers × rounds          // derived, not configurable
claim size = queueSize + Workers - in-flight
```

Neither `queueSize` nor a batch limit is a knob. Any queue larger than the
derived one lets leases expire on merely-queued messages; any smaller only costs
throughput. A separate batch size could only ever be below the claim budget —
where it just throttles — or above it, where it does nothing. Settings that can
only do nothing or harm are not settings.

`HandlerTimeout` deserves the same scrutiny in the other direction. It looks like
a free safety margin and is not: it sets the floor under `Lease` and therefore
under crash-recovery time, it bounds how long one stuck call holds a worker, and
a larger value buys fewer queue slots for the same lease. Size it from the
transport's tail latency. account-service and pay-service both publish to
RabbitMQ and call restriction-service over gRPC, both inside 200 ms, so they run
2 s — an order of magnitude of head-room, not two.

The defaults live in `outbox.Settings` rather than in each service for a reason:
adopting services share one dashboard and one alert set, and those only describe
every service correctly while the numbers agree. Override through the
environment when a service genuinely needs something different, and record why.

Consequences worth knowing before tuning:

- With the defaults (`Lease=8s`, `HandlerTimeout=2s`, `Workers=4`) the pool holds
  at most 12 messages at once. That is a deliberate ceiling derived from the
  lease, not a bottleneck to remove.
- Tightening `HandlerTimeout` widens that ceiling: it is the unit the queue
  budget is divided by. Going from 5 s to 2 s took the ceiling from 8 to 12 and
  measured throughput from 289 to 432 msg/s on a free handler.
- `NewWorker` raises `Lease` to `HandlerTimeout + 1s` if the supplied value is
  smaller, and logs a warning. That warning means the configuration asked for
  something unsafe — a handler able to outlive its own lease.

## Throughput

Measured end to end against a real PostgreSQL with account-service's shipped
config (`Lease=8s`, `HandlerTimeout=2s`, `RelayInterval=500ms`).
Reproduce with:

```bash
go test -tags=dbstress ./outbox/postgres -run Throughput -v -timeout=15m
```

| Handler | Workers | Throughput | Duplicated |
|---|---|---|---|
| free | 4 | 432 msg/s | 0% |
| 5 ms | 4 | 436 msg/s | 0% |
| 20 ms | 4 | 182 msg/s | 0% |
| 20 ms | 16 | 715 msg/s | 0% |
| 20 ms | 24 | 1086 msg/s | 0% |
| 20 ms | 32 | 1463 msg/s | 0% |

Numbers are from a laptop Docker PostgreSQL, so read the *shape*, not the
absolute values — production hardware will differ. What transfers:

- **Throughput scales close to linearly with `Workers`** up to at least 32. That
  is the knob that matters.
- **At low worker counts the database, not the handler, is the bottleneck.**
  A free handler and a 5 ms handler both give ~435 msg/s at 4 workers: the floor
  is one status-update commit per message.
- **No duplicates and no dead-letters at any load tested.** Back-pressure absorbs
  the pressure instead of the correctness guarantees degrading.
- **Multiply by pod count.** Each instance runs its own pool and `SKIP LOCKED`
  partitions the rows between them.

Two structural bounds:

1. **In-flight ceiling** — `queueSize + Workers` messages at once. With the
   defaults that is 12. It is derived from the lease, not a tuning mistake.
2. **Worker capacity** — `Workers / handler duration`, floored by the per-message
   database commit.

Crash recovery costs one lease: a killed pod's messages sit in `processing`
until `locked_until` passes. That is the other reason to keep the lease tight.

`RelayInterval` is *not* a ceiling. The relay keeps claiming while a backlog
drains and only sleeps until the next tick once a claim comes back short. It
drains once at startup rather than idling for an interval first, and waits 25 ms
for a worker slot when saturated rather than dropping back to the tick.

### Table growth is the real constraint at volume

Delivery keeps up long before storage does. At a sustained rate `R` msg/s the
48-hour delivered retention holds `R × 172800` rows:

| Sustained rate | Rows at 48h retention |
|---|---|
| 20 msg/s | ~3.5 M |
| 100 msg/s | ~17 M |
| 300 msg/s | ~52 M |

With a JSONB payload of a few hundred bytes that is tens of gigabytes sitting in
the business database — and every insert is on the critical path of the
transaction that writes it.

**Retention is the lever; partitioning is not.** Time-range partitioning is the
usual answer to a table this shape, and it does not work here: PostgreSQL
requires a unique constraint to include every partition key column, so
partitioning by `created_at` makes the global `UNIQUE (dedupe_key)` impossible.

```
ERROR:  unique constraint on partitioned table must include all partitioning columns
DETAIL:  UNIQUE constraint on table "ob" lacks column "created_at" ...
```

Partitioning by `hash(dedupe_key)` keeps the constraint but gives no time-based
`DROP PARTITION`, which is the only reason to partition in the first place. So
partitioning costs the dedupe guarantee and buys nothing — unless the guarantee
moves somewhere else first.

Retention is therefore split in two, because the two row types serve different
purposes and a shared window forces a choice between disk and triage:

| Rows | Default | Why |
|---|---|---|
| `delivered` | 48h | This is what fills the disk. Also the dedupe fence at insert time |
| `dead` | 7 days | Forensic window: long enough to notice, investigate and act |

Prune runs hourly, not daily: a daily sweep lets a day of rows pile up and then
deletes them in one burst, which is exactly the shape that causes vacuum
pressure.

Dead retention is a **forensic** window, not a replay window. Replay is only
mechanically safe while the consumer's own idempotency key is alive — 48h for
both current consumers — because a dead message does not prove the effect never
applied: a handler can succeed and its status write fail, repeatedly, until
stale-lease reclaims exhaust the budget. Past that window an operator must
verify state by hand anyway, so extra retention buys nothing.

It also actively costs something. A dead row's `dedupe_key` still blocks
re-insertion, so every extra day of dead retention is another day in which a
legitimate re-run of the business path cannot recreate that side effect. Long
dead retention obstructs recovery rather than aiding it.

Do not drop `delivered` retention below the downstream dedupe TTL for event
types whose consumer has no fence of its own. Protection is the longer of the
two windows: while the outbox row exists the duplicate is rejected at insert;
after that, only a downstream idempotency key can stop it. Both current
consumers keep theirs for 48h, which is where the delivered default comes from.

## Behaviour under failure

| Failure | What happens |
|---|---|
| Handler returns an error | `failed`, retried with exponential backoff; budget consumed |
| Downstream down for hours | Messages accumulate in `failed`; lag gauge climbs; dead-letter after ~4h with defaults |
| Pod killed mid-handler | Lease expires, another instance reclaims, **attempts increments** so a crash loop terminates |
| Database unreachable | Relay logs and retries next tick; nothing is lost, nothing is delivered |
| `MarkDelivered` fails after a successful handler | Message redelivered — the handler runs twice. This is why handlers must be idempotent |
| Handler panics | `dead` immediately, `OnDead` fires |
| Relay panics | Recovered and reported via `OnClaimError`; delivery stalls until the next tick, the rest of the service keeps serving |
| SIGTERM | Workers drain the queue, in-flight status writes still land (non-cancellable context) |

Under sustained load the pool exerts back-pressure rather than growing: goroutine
count is fixed at `Workers + 1`, and memory is bounded by `queueSize` messages.
The table grows instead, which is what `Prune` and the lag gauge are for.

## Tuning

Change one thing at a time and watch `outbox_oldest_pending_age_seconds`.

| Symptom | Change |
|---|---|
| Lag climbing, handler duration low | Raise `Workers` |
| Lag climbing, handler duration high | Fix the downstream; raising workers just parallelises the waiting |
| Want a bigger in-flight ceiling | Raise `Lease`, or lower `HandlerTimeout` — queue depth is `(Lease−HandlerTimeout−1s)/HandlerTimeout` rounds |
| Timeouts in `last_error` | Raise `HandlerTimeout` toward the real tail latency, and `Lease` with it |
| Slow crash recovery | Lower `Lease` — it is the delay before another pod reclaims |
| Table growing | Shorten `OutboxDeliveredRetention`, but keep it above the downstream dedupe TTL |
| Idle service polling too often | Raise `RelayInterval`; it only affects how fast an idle pool notices new work |

Startup logs a warning whenever a supplied value was overridden. A warning means
the configuration asked for something unsafe — read it, do not silence it.

## Production risks worth knowing

- **The table is on the business database.** A deposit cannot commit if the
  outbox insert fails. That is the point — but it does mean outbox bloat becomes
  a business-availability problem. Keep `Prune` running and alert on table size.
- **`Prune` deletes in 5000-row batches** and is safe to run alongside workers,
  but the first run against a long-unpruned table will be long. Run it manually
  in a maintenance window before enabling the cron on an existing table.
- **Dedupe protection expires with retention** (see above). Retention shorter
  than a downstream dedupe TTL narrows the real guarantee.
- **Ordering is not preserved.** Messages are claimed by `next_attempt_at` and
  processed concurrently. If two events for the same aggregate must apply in
  order, the outbox will not do that for you.
- **A dead message is invisible without the alert.** There is no automatic
  replay. `increase(outbox_dead_total[1h]) > 0` is the only thing standing
  between a dead-lettered payout notification and nobody noticing.
- **Every pod runs the cron jobs.** The platform cron has no leader election, so
  `Prune` and the lag sample run once per replica. `Prune` claims its batches
  with `FOR UPDATE SKIP LOCKED`, so concurrent pruners partition the work instead
  of blocking each other; the lag sample is a read and simply reports the same
  value from each pod.

## Delivery semantics

| Handler result | Outcome |
|---|---|
| `nil` | `delivered` |
| error | `failed`, retried with exponential backoff (±10% jitter) |
| `MarkNonRetryable(err)` | `dead` immediately, budget untouched |
| unknown `event_type` | `dead` (wraps `ErrNonRetryable`) |
| attempts reach `MaxAttempts` | `dead` |
| panic | `dead` — a panic is a code defect, not a transient fault |

A worker that loses its lease (its message was reclaimed after expiry) gets
`ErrLeaseLost` from the status update and discards it rather than overwriting
the new owner's state.

Stale-lease reclaims **consume retry budget**. A message that kills its worker
every time — OOM, panic, pod eviction — would otherwise be reclaimed forever;
instead it reaches `dead` after `MaxAttempts` reclaims.

## Idempotency and its limits

`dedupe_key` is `UNIQUE`, so a duplicate insert is silently ignored. Two things
bound that guarantee, and both matter when picking a retention window:

1. **Retention.** `Prune` deletes delivered rows after the retention window,
   taking the key with it. After that, re-running the same business path
   re-inserts the same logical event.
2. **Downstream TTLs.** Where the receiving service dedupes — restriction-service
   keeps `limit_counter_idempotency_keys`, notification-service keeps
   `notification_dedupe_keys`, both 48h by default — that TTL is the real
   protection window. Keeping outbox retention *longer* than the downstream TTL
   does not extend safety.

   Producers should pass the outbox row's `dedupe_key` to consumers that support
   fencing. account-service does this for `notification.push`, where a retry
   would otherwise send the user a second notification.

Handlers must be idempotent regardless. Retries happen normally, not only in
edge cases.

## Observability

Use `outbox/metrics` rather than hand-rolling collectors:

```go
m, err := outboxmetrics.New("account_service", prometheus.DefaultRegisterer)
worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{Hooks: m.Hooks(), ...})
// and in the lag cron: m.SetLag(ages)
```

The names are the contract with dashboards and alert rules. Fixing them in one
place is what makes a single dashboard and a single set of alerts work across
services — and a rename after the first deployment breaks the history behind
every panel.

`WorkerHooks` (`OnDelivered`, `OnFailed`, `OnDead`, `OnHandlerDuration`) map
directly onto Prometheus counters and a histogram.

Counters alone cannot detect a stalled relay: it simply stops incrementing them,
which looks exactly like an idle service. Sample `OldestPendingAge` on a cron
into a gauge and alert on that. Reset the gauge each sample — event types with a
drained backlog are absent from the map, and a `GaugeVec` otherwise keeps
reporting the last value forever.

`OnClaimError` and `OnLeaseLost` cover the two failure paths the outcome hooks
cannot see:

- **`OnClaimError`** — the relay could not claim work. This is the only immediate
  signal that delivery has stopped entirely, and it is not redundant with the lag
  gauge: sampling the lag reads the same database, so a database outage leaves
  the gauge frozen on its last value rather than climbing. Without this counter a
  database outage looks exactly like an idle service.
- **`OnLeaseLost`** — a status update was discarded because another worker had
  already reclaimed the message. Not an error, but a sustained rate means work is
  being done twice and thrown away: an over-subscribed pool or too short a lease.

Suggested alerts:

- `increase(outbox_claim_errors_total[5m]) > 0` — delivery has stopped
- `outbox_oldest_pending_age_seconds > 300` — delivery is falling behind
- `increase(outbox_dead_total[1h]) > 0` — something needs a human
- `rate(outbox_failed_total[5m])` sustained — a downstream dependency is unhealthy
- `rate(outbox_lease_lost_total[5m])` sustained — the pool is over-subscribed

## DLQ runbook

`status = 'dead'` means delivery was abandoned. Nothing retries it automatically.

**Triage:**

```sql
SELECT event_type, count(*), max(created_at) AS newest
FROM outbox_messages
WHERE status = 'dead'
GROUP BY event_type
ORDER BY count DESC;
```

**Inspect a specific failure:**

```sql
SELECT id, aggregate_type, aggregate_id, dedupe_key, attempts, last_error, payload
FROM outbox_messages
WHERE status = 'dead'
ORDER BY created_at DESC
LIMIT 20;
```

`last_error` distinguishes the two causes: a non-retryable error (bad payload,
unregistered `event_type`, business invariant) versus an exhausted retry budget
(a dependency that stayed down longer than the budget allowed).

**Replay after fixing the cause** — only once you have confirmed the handler now
succeeds, and that replaying is safe for that side effect:

```sql
UPDATE outbox_messages
SET status = 'pending', attempts = 0, next_attempt_at = NOW(),
    locked_by = NULL, locked_until = NULL, last_error = NULL
WHERE id = ANY($1::bigint[]);
```

Replay by explicit id list, never by a blanket `WHERE status = 'dead'`: the dead
set usually mixes messages that are safe to replay with ones that are dead for a
good reason.

**Do not replay** unregistered `event_type` messages until a handler exists —
they will simply die again. Check `Registry` registrations first.

## Schema

The canonical DDL lives in `outbox/postgres/migrations` as an `embed.FS`.
Apply it directly from that FS rather than copying the DDL into a service's
migration directory, where the copy drifts:

```go
migrator.New(migrator.Config{
    Driver: "postgres", DSN: dsn,
    MigrationsFS:    outboxmigrations.FS,
    MigrationsTable: "schema_migrations_common", // its own history
})
```

The shared source runs as its own migrator with its own history table, so its
versions are unrelated to the service's and cannot collide. Tests can also load
it through `postgrestest.OpenConfig.MigrationsFS`.

Those files use a reserved `9_000_000+` version band. golang-migrate keys
migrations by their integer prefix and rejects duplicates, so a shared migration
numbered `0001` would collide with every service's own first migration and make
the combined FS+Dir setup impossible to use.

## Testing

`outbox/testkit.FakeRepository` is an in-memory `Repository` for unit tests. It
mirrors the SQL semantics that matter — dedupe, lease ownership, budget
consumption on stale reclaim — and adds test-only helpers:

- `ExpireLease(id)` — simulate a worker that died mid-processing
- `MakeDue(id)` — bring a scheduled retry forward instead of sleeping
- `FailMarkDelivered` — exercise the redelivery path after a failed status write

Repository behaviour is covered against a real PostgreSQL container via
`testkit/postgrestest`; Docker is required for `go test ./outbox/...`.

## Where the row builders live

Put the `CreateParams` constructors in the service's `internal/domain/outbox.go`,
next to the event-type constants and payload structs they use — not in
`internal/controllers`.

The dedupe key is a business decision, and usually the most consequential one in
the whole adoption: get it wrong and events vanish with no error anywhere. All
four adopters learned this the same way —
`merchant-sync:{orderID}:{transition}`, `verify-level:{userID}:{level}`,
`account-withdrawal-{id}-reverse`. Each of those discriminators exists because
without it two genuinely different events collapse into one row and the second is
dropped.

Two rules follow from that:

1. **One file per service holds every key format.** Scattering the format strings
   across controllers makes them impossible to review as a set, which is the only
   way to notice that two events can collide.
2. **Derive the key inside the builder, never take it as a parameter** — most of
   all when the same value also goes into the payload. account-service's counter
   key appears both in `dedupe_key` and in the payload restriction-service fences
   on; letting callers pass it invited the two to drift, which fences nothing
   while looking correct.

Pin the formats with a table test. They are a cross-service contract:
restriction-service stores the counter idempotency key in its own table, so
changing the format silently un-fences every counter already recorded under the
old spelling, and the symptom is double-counted limits rather than a failure.

## Current adopters

| Service | Event types |
|---|---|
| `account-service` | `restriction.counter.update`, `notification.push` |
| `pay-service` | `merchant.operation.sync`, `restriction.counter.update`, `account.transaction.rollback` |
| `user-service` | `user.created`, `user.verify_level.updated` |
| `restriction-service` | `account.deposits.auto_approve` |

All four run the same `Settings` defaults. That is the point: they share one
Grafana dashboard and one set of alert rules, which only describe every service
correctly while the numbers agree. A service that genuinely needs a different
value should override it through the environment and say why.

### What is deliberately not on the outbox

Not every post-commit publish belongs here, and what was considered and rejected
is worth recording so the question is not reopened blindly:

- **OTP delivery** (`user-service`). The token lives five minutes. A retry would
  deliver an expired code, or a second live one after the user already pressed
  resend. The right fix there was to stop reporting success when the send failed,
  not to make the send durable.
- **`pay_webhook_outbox`** (`pay-service`). Already a correct outbox with typed
  domain columns and its own delivery history. Moving it here would trade that
  structure for an opaque JSON blob.

  It is the same *pattern* as this package — a row committed inside the business
  transaction and delivered at least once afterwards — so the invariants
  transfer, and it is worth keeping them in step. When this package gained the
  lease-ownership check on status writes (`ErrLeaseLost`), the webhook outbox
  needed the same guard for the same reason and now has it via
  `ErrWebhookLeaseLost`. A change to one is a prompt to look at the other.

The test is not "is this a post-commit publish" but "does a late delivery still
help". When the answer is no, durability is the wrong tool.

### Services with no producer side

`currency-service` and `merchant-service` were reviewed and deliberately left
out. Neither has anything to deliver:

- **`currency-service`** has no queue infrastructure at all — no publishers, no
  consumers, no cron. It fetches rates from external providers and serves them
  over HTTP.
- **`merchant-service`** is a pure consumer. Its only `AddPublisher`/
  `GetPublisher` mentions are interface declarations in `internal/domain/queue.go`;
  the composition root calls neither.

Adopting the outbox in a service that emits nothing buys a table, a worker pool,
two cron jobs, a metrics set and a second migration source, all for zero rows.
Revisit when one of them gains a real post-commit side effect — for example if
merchant-service starts publishing merchant profile changes rather than only
consuming them.

Note that merchant-service is on the *receiving* end of pay-service's
`merchant.operation.sync`, and that is what makes at-least-once delivery safe
there: its projection upserts under
`excluded.updated_at > merchant_operations.updated_at`, so a late or repeated
snapshot is discarded rather than overwriting a newer one.
