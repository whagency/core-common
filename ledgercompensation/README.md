# ledgercompensation

Guarantees that money moved in account-service is never left without a committed
local counterpart.

## The problem

A service that posts to the ledger does so from inside its own transaction:

```go
tx := begin()
journalID := account.MakeTransaction(...)  // commits on the other side, now
tx.writeLocalState(journalID)
tx.Commit()                                // may fail
```

The posting commits on account-service the moment the call returns. If anything
after that fails, money has moved and nothing locally accounts for it.

**The outbox does not solve this.** The outbox guarantees that a message written
inside a *committed* transaction is delivered. Here the transaction is the thing
that failed, so a compensating message written into it disappears with it.

## The mechanism

This package inverts the write. A row is committed **outside** the business
transaction and cleared **inside** it:

```
before the ledger call   INSERT pending  (base connection, autocommit)
after the ledger call    UPDATE posted   (journal id now known)
inside the business tx   UPDATE settled  (shares the transaction's fate)
```

If the transaction commits, the clearing commits with it and nothing remains. If
it rolls back, the clearing rolls back too and the row survives. A row still open
past `Grace` is a posting with no committed local counterpart, and `Resolver`
reverses it through an `account.transaction.rollback` outbox row.

### Why the commit-ambiguity window needs no handling

A failed `Commit` leaves the caller unable to tell whether the transaction
applied. It does not matter. If it applied, the clearing applied with it and
there is no row to find; if it did not, the row is there. Atomicity is the
decision procedure, so nothing here reasons about "the commit might have
succeeded".

### Why it cannot be a column on the aggregate

The business transaction typically holds `SELECT ... FOR UPDATE` on the aggregate
row, so an autocommit write to that row from a second connection would block on a
lock the same request already holds. The switch has to live in its own row.

### Why it cannot live in `outbox_messages`

`outbox/postgres.Repository.Create` deliberately joins the ambient transaction;
writing past it is called out in that package as a data-corrupting mistake. The
switch needs exactly the opposite semantics.

## Adopting it

### 1. Apply the schema

The DDL is owned here. Add a second migrator source rather than copying the SQL:

```go
cfg.MigrationsFS = compmigrations.FS
cfg.MigrationsTable = "schema_migrations_ledgercomp"
```

Its own history table, because each shared source is versioned independently and
a service should be able to adopt the outbox without inheriting this table.

### 2. Implement `Verifier`

The one seam with business meaning: is the local state this posting belongs to
actually committed?

```go
func (v *PayOrderSettledVerifier) IsSettled(
    ctx context.Context, aggregateType, aggregateID string, journalID uint64,
) (bool, error) {
    order, err := v.payOrders.GetByID(ctx, uuid.MustParse(aggregateID))
    if err != nil {
        return false, err
    }
    return order.Status == succeeded && order.LedgerTxID != nil && *order.LedgerTxID == journalID, nil
}
```

Both halves matter. A succeeded aggregate carrying a *different* journal id means
the operation went through on a later attempt, and this posting is still an
orphan.

Returning `false` means "reverse the posting", so an implementation that cannot
tell must return an error instead.

### 3. Wrap the ledger call

```go
journalID, err := guard.Around(ctx, ledgercompensation.CreateParams{
    AggregateType:  "pay_order",
    AggregateID:    order.ID.String(),
    BizType:        account.JournalBizTypeCheckoutPayment,
    IdempotencyKey: idempotencyKey,
}, func(ctx context.Context) (uint64, error) {
    return c.makeCheckoutPaymentTransaction(ctx, order, ...)
})
```

`Around` returns the ledger's error untouched, so callers keep matching on
`account.ErrInsufficientFunds` and friends exactly as before.

### 4. Clear it inside the business transaction

```go
if err := guard.Settled(txCtx, idempotencyKey); err != nil {
    return err  // must abort the transaction, never be logged and swallowed
}
```

### 5. Wire the runtime

```go
resolver, err := ledgercompensation.NewResolver(ledgercompensation.ResolverConfig{
    Repository:         comppostgres.New(db),
    Outbox:             outboxRepo,
    Verifier:           NewPayOrderSettledVerifier(payOrders),
    Journals:           accountGRPCClient,   // satisfies JournalFinder structurally
    TransactionManager: txManager,
    Settings:           config.Cron().LedgerCompensation,
    OnReversal:         metrics.ObserveReversal,
})

compcron.Register(cronManager, resolver, settings, metrics.SetCurrent)
outboxRegistry.Register(ledgercompensation.NewHandler(eventCtrl))
```

## Invariants

- `RecordIntent` and `RecordPosted` always write on the base connection under
  `context.WithoutCancel`. Joining the transaction defeats the mechanism;
  honouring cancellation loses the row in precisely the case that causes it — a
  client hanging up mid-payment.
- `MarkSettled` always joins the ambient transaction, for the same reason in
  reverse.
- A `RecordPosted` failure aborts the flow. `Guard.Around` enforces this;
  continuing would leave a posting recorded nowhere.
- The resolver re-verifies before reversing. Clearing shares the transaction's
  fate so this should be redundant, but reversing a real payment is severe enough
  to be worth one read on a rare path.
- A row with no journal id is resolved through `JournalFinder`. Never use
  `MakeTransaction` to probe: it creates the posting when the key does not match.
- Not found from that lookup means "nothing to compensate" for both possible
  causes — never posted, or already reversed and the key renamed by the rollback.
  account-service renames the key deliberately so the operation can be
  re-attempted; that behaviour is load-bearing here.
- The table carries **no foreign key** to the aggregate. It cannot: the shared
  table does not know which table, or which key type, a given service uses. Same
  trade already made for `outbox_messages`.
- Flows that make no ledger call — demo modes, for instance — must not create
  rows.

## Tuning

`Grace` is the one number that needs thought. It must comfortably exceed the
guarded request: too short and the resolver races a transaction that is about to
commit, reversing a payment that was going to succeed. It is also the delay
before a genuine orphan is reversed, so it should not be padded beyond what that
safety margin needs. Two minutes suits a flow bounded by a handful of gRPC calls
and one transaction.

Everything else in `Settings` is shared on purpose: adopters share one dashboard
and one alert set, which only describe every service correctly while the numbers
agree.

## Alerting

`<service>_ledger_compensation_current{status="pending"|"posted"}` is the primary
alert. A rising count means postings are accumulating without a committed local
counterpart. Counters cannot show this: a stalled resolver simply stops
incrementing them, which is indistinguishable from having nothing to compensate.

`<service>_ledger_compensation_reversal_total` counts requested reversals by
journal business type. Any sustained increase means a flow is losing its local
transaction after moving money.

## Testing

`testkit.Recorder` records the transitions a flow drove, so a controller test can
assert the order — arming before the ledger call, clearing after everything else.

It cannot model a rollback, which is why the survives-a-rollback invariant is
asserted against a real PostgreSQL in `postgres/repository_test.go`. That test is
worth checking is non-vacuous: swap `r.base` for `r.db` in the repository and it
must fail.

## Current adopters

- `pay-service` — checkout confirmation and settlement release

One adopter, so the shape here is proven against exactly one flow. It was
extracted before that flow shipped, on the reasoning that renaming an unshipped
table is cheap while migrating a live one that guards money is not.
