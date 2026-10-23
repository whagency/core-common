# Code walkthrough

Call-by-call tour of the outbox runtime: what runs, in what order, and which
failure each piece exists to prevent. For usage, configuration and operations
see [README.md](README.md).

*Русская версия: [ARCHITECTURE.ru.md](ARCHITECTURE.ru.md)*

Read it top to bottom once; after that the call graph below is enough.

```
WRITE PATH  (inside the business transaction, one per side effect)
  controller.enqueueXxx
    └─ postgres.Repository.Create ──────────────► INSERT … ON CONFLICT DO NOTHING

STARTUP  (composition root)
  NewRegistry → Register(handler)…
  NewWorker(repo, registry, cfg) → WorkerConfig.applyDefaults
  NewPool(worker, cfg)           → PoolConfig.applyDefaults → queueSizeFor
  errgroup.Go(pool.Run)

RUNTIME  (Workers + 1 goroutines)
  Pool.Run
    ├─ relay goroutine
    │    Pool.safeClaimUntilDrained   (recover → OnClaimError)
    │    └─ Pool.claimUntilDrained
    │      ├─ Pool.claimBudget
    │      ├─ sleepCtx                      (saturated: wait for a slot)
    │      └─ Pool.relayOnce
    │           ├─ Worker.Claim ───────────► UPDATE … FOR UPDATE SKIP LOCKED … RETURNING
    │           ├─ Pool.tryAcquireInFlight
    │           └─ Pool.submit ─────────────► queue channel
    │
    └─ worker goroutines ×Workers
         ◄── queue channel
         Pool.safeProcessOne
           ├─ defer Pool.releaseInFlight
           ├─ defer recover → Worker.DeadLetter
           └─ Worker.ProcessOne → Worker.process
                ├─ Registry.Dispatch → Handler.Handle
                └─ Worker.withStatusCtx → repo.Mark{Delivered,Failed,Dead}
                                            └─ ownedByWorker → applyStatusUpdate

SHUTDOWN
  ctx cancelled → workers Pool.drain → relay returns → wg.Wait → Run returns

CRON  (registered separately by the service)
  Repository.Prune             — bounded GC loop
  Repository.OldestPendingAge  — lag gauge
```

---

## 1. Write path

### `controller.enqueueXxx` → `Repository.Create`

Called from inside a business transaction, between `Begin` and `Commit`:

```go
ctx = c.dbTransactionManager.Begin(ctx)
c.depositRequests.WithTxFromContext(ctx).Create(ctx, &depositRequest)  // business state
c.enqueueDepositCounter(ctx, depositRequest)                          // intent
return c.dbTransactionManager.Commit(ctx)
```

The row builders (`buildCounterOutboxParams`, `buildNotificationPushOutboxParams`
in account-service) only assemble `CreateParams`. They live in the controller
package because choosing *what* to emit is a business decision; the handlers that
later deliver it are transport and live under `internal/transport/outbox`.

### `postgres.Repository.Create`

Three deliberate details:

**It resolves the transaction itself** via `commondb.GetTxFromContext(ctx)`
rather than requiring the caller to chain `WithTxFromContext`. Forgetting that
chain writes the row on the base connection, so the side effect survives a
rolled-back business transaction — a silent failure with no local symptom. The
guarantee is too important to rest on a convention.

**It omits `next_attempt_at` and `created_at`** so their `DEFAULT NOW()` applies.
`ClaimDue` compares `next_attempt_at` against the *database* clock; stamping it
from the application clock would make claim eligibility depend on skew between
the two, and a pod running milliseconds ahead would silently delay its own fresh
messages.

**The conflict target is pinned to `dedupe_key`.** A bare `DO NOTHING` would also
swallow violations of any constraint added later, dropping rows silently.

`withoutImplicitTx` disables GORM's default write transaction. Every write here
is a single statement, which PostgreSQL already executes atomically, so the
wrapper only adds `BEGIN`/`COMMIT` round trips — measured at ~2.7× the cost of
the statement itself.

---

## 2. Startup

### `NewWorker` → `WorkerConfig.applyDefaults`

Fills zero values, then enforces one invariant:

```go
if c.HandlerTimeout+statusUpdateGrace > c.Lease {
    c.Lease = c.HandlerTimeout + statusUpdateGrace
}
```

A handler able to outlive its own lease guarantees double processing: the row
becomes reclaimable while the first worker is still inside it. `NewWorker` logs a
warning when it has to raise the lease — that warning means the configuration
asked for something unsafe.

### `NewPool` → `queueSizeFor`

`PoolConfig.applyDefaults` only fills `Workers` and `RelayInterval`. The channel
capacity is *derived*, not configured:

```go
queueSize = Workers × (Lease − HandlerTimeout − statusUpdateGrace) / HandlerTimeout
```

A claimed message holds its lease while it waits in the channel, so the lease has
to cover the queueing delay. Anything larger lets leases expire on merely-queued
messages; anything smaller only costs throughput. A result of zero means the
lease affords no queueing at all, and the channel becomes unbuffered — a hand-off
then completes only when a worker is actually ready.

---

## 3. The relay

One goroutine, the only one that reads the database. That is what guarantees
there is never a second concurrent claim — no lock, no leader election.

`Run` calls it through `safeClaimUntilDrained`, which recovers panics. Handlers
already get that containment from `safeProcessOne`; without the same for the
relay, a panic in the claim path — a driver bug, an unexpected column type —
would take down a process that also answers HTTP and gRPC. A stalled relay with
a live service, reported through `OnClaimError`, is the better failure mode.

### `Pool.claimUntilDrained`

```go
for ctx.Err() == nil {
    budget := p.claimBudget()
    if budget <= 0 {
        if !sleepCtx(ctx, saturatedPollInterval) { return }
        continue
    }
    if !p.relayOnce(ctx, budget) { return }
}
```

Two exits, and the difference matters:

- **Budget is zero** — the pool is full. Saturation is transient, so it waits
  25 ms for a slot rather than returning. Returning here would hand a draining
  backlog back to the ticker and stall it for a whole `RelayInterval`.
- **`relayOnce` returns false** — the claim came back short, meaning the database
  has nothing more due. *That* is the reason to sleep until the next tick.

`Run` calls this once before entering the ticker loop, so a pod that starts up to
an existing backlog begins immediately instead of idling for an interval.

### `Pool.claimBudget`

```go
queueSize + Workers − len(inFlight)
```

How many more messages the pool can take on right now: every queue slot plus
every worker slot, minus what it is already holding. Claiming beyond this starts
leases the pool cannot serve before they expire.

### `Pool.relayOnce`

Claims up to `want`, then for each message:

1. `tryAcquireInFlight(msg.ID)` — rejects a message this instance is already
   processing. This is not paranoia: `locked_by` is identical for every worker in
   one process, so the database cannot distinguish this instance's own
   stale-lease reclaim from a legitimate one. Without the set, a message still
   sitting in the channel could be reclaimed and dispatched a second time.
2. `submit` — blocks when the channel is full. That block *is* the back-pressure:
   the relay cannot outrun the workers, and goroutine count stays fixed.

Returns `len(msgs) == want`, i.e. "the claim was full, there is probably more".
Returning anything else would end the drain early — that exact mistake once
capped throughput at one batch per tick.

---

## 4. The workers

### `Pool.safeProcessOne`

```go
defer p.releaseInFlight(msg.ID)
defer func() {
    if r := recover(); r != nil {
        log…
        p.worker.DeadLetter(ctx, msg, fmt.Sprintf("handler panic: %v", r))
    }
}()
p.worker.ProcessOne(ctx, msg)
```

The recover keeps a panicking handler from killing the worker goroutine, and
dead-letters the message. Leaving it in `processing` instead would have it
reclaimed on every lease expiry forever, panicking each time, without ever firing
`OnDead` — an invisible hot loop.

### `Worker.process` — the decision ladder

The order is the design; each step exists because the one below it would get the
wrong answer.

**1. Budget already exhausted → dead, without dispatching.**

```go
if msg.Attempts >= msg.MaxAttempts { deadLetter(…); return }
```

A worker that dies mid-handler never returns an error, so it never reaches the
post-handler budget check. Stale-lease reclaims charge the budget instead
(see `ClaimDue`), and this is where a message that keeps killing its worker
finally terminates.

**2. Dispatch under `HandlerTimeout`.** The deadline is applied to
`handlerCtx` only. Status updates use a different context (step 4), so a timed-out
handler can still record its outcome.

**3. `OnHandlerDuration` fires regardless of result** — a latency histogram that
only saw successes would hide exactly the degradation you want to catch.

**4. Success → `MarkDelivered`.** Note the ordering across the whole system: the
side effect happens first, the row is marked second. Reversing it would give
at-most-once — silent loss. This ordering is what makes the guarantee
at-least-once, and why handlers must be idempotent.

**5. `ctx.Err() != nil` → leave the message alone.** Our own shutdown is not a
delivery failure. Recording it would burn a retry attempt and push the message a
full backoff away; leaving the row in `processing` costs only a lease expiry and
keeps the budget intact — the same path a hard crash already takes. This check
sits *above* the error classification on purpose. A `HandlerTimeout` expiry is
unaffected: that cancels `handlerCtx`, not `ctx`.

**6. `ErrNonRetryable` → dead immediately,** budget untouched. A malformed
payload or an unregistered `event_type` cannot succeed on retry; spending ten
attempts on it only delays the alert.

**7. Budget exhausted → dead.** `msg.Attempts + 1 >= msg.MaxAttempts`, so
`MaxAttempts = N` yields exactly N handler executions.

**8. Otherwise → `MarkFailed`** with `backoff(attempt)`: exponential from
`BaseBackoff`, capped at `MaxBackoff`, ±10% jitter to keep instances from
retrying in lockstep. The cap is applied before the `float64 → Duration`
conversion, so no overflow is possible.

### Status writes

`withStatusCtx` runs the update under `context.WithoutCancel(ctx)` with a
`statusUpdateGrace` deadline. Without it, every in-flight message that *succeeded*
during a SIGTERM would fail its status write and be redelivered on the next boot.

`ownedByWorker` scopes the update:

```sql
WHERE id = ? AND locked_by = ? AND locked_until > NOW()
```

`applyStatusUpdate` turns a zero-row result into `ErrLeaseLost`. Together these
mean a worker whose lease was already reclaimed discards its own result instead
of overwriting the new owner's state — and a zero-row update is never mistaken
for success. `logStatusErr` separates lease loss (benign, someone else owns it)
from a real persistence failure.

---

## 5. Shutdown

`ctx` is cancelled by the errgroup on SIGTERM.

Workers hit `case <-ctx.Done()` and call `Pool.drain`, which finishes what is
already in the channel using a **fresh** context bounded by one handler timeout.
Running those handlers under the cancelled context would be worse than not
draining at all: each would fail instantly, consume an attempt and be rescheduled
a full backoff away, whereas an abandoned message just waits out its lease with
its budget intact. Whatever does not fit the budget is left in the channel and
falls back to lease expiry — the same safety net that covers a hard crash.

The relay returns on `ctx.Done()`, `wg.Wait()` releases, `Run` returns nil.

---

## 6. Cron jobs

Registered by the service, not by the library.

**`Repository.Prune(deliveredOlderThan, deadOlderThan)`** deletes in batches of
5000 so GC never holds locks on a large table for long. Two retentions because
the rows serve different purposes: delivered rows are what fill the disk, dead
rows are the incident record and must outlive the time it takes an operator to
act. Delivered rows age from `delivered_at` (which is also what lets the partial
GC index serve the scan), dead rows from `created_at`.

Safe alongside active workers: neither status is claimable.

`Prune` claims its batches with `FOR UPDATE SKIP LOCKED` because the platform
cron has no leader election: every pod runs the job. Without it concurrent
pruners serialise on each other's row locks and N pods do one pod's work.

**`Repository.OldestPendingAge`** feeds a gauge. Counters cannot detect a stalled
relay — it simply stops incrementing them, which is indistinguishable from an
idle service. Reset the gauge on each sample: event types with a drained backlog
are absent from the map, and a `GaugeVec` would otherwise report the last value
forever.

---

## 7. Where each guarantee lives

| Guarantee | Enforced by |
|---|---|
| Intent is atomic with business state | `Create` inside the caller's transaction |
| No duplicate rows for one logical event | `UNIQUE (dedupe_key)` + `ON CONFLICT DO NOTHING` |
| One claimer across instances | `FOR UPDATE SKIP LOCKED` |
| One dispatch within an instance | `Pool.inFlight` |
| No stale writer overwrites a new owner | `ownedByWorker` + `ErrLeaseLost` |
| Nothing lost when a pod dies | `locked_until` expiry + stale-lease branch of `ClaimDue` |
| Poison messages terminate | `attempts` charged on reclaim + budget check before dispatch |
| Handler panics terminate | `safeProcessOne` recover → `DeadLetter` |
| A relay panic does not kill the service | `safeClaimUntilDrained` recover → `OnClaimError` |
| Concurrent pruners do not block each other | `FOR UPDATE SKIP LOCKED` in `Prune` |
| Bounded memory and goroutines under load | blocking `submit`, fixed `Workers+1` |
| Shutdown costs nothing | `ctx.Err()` check + `drain` with a fresh context |
