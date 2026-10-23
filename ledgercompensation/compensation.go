// Package ledgercompensation guarantees that money moved in account-service is
// never left without a committed local counterpart.
//
// # The problem
//
// A service that posts to the ledger does so from inside its own transaction:
//
//	tx := begin()
//	journalID := account.MakeTransaction(...)  // commits on the other side, now
//	tx.writeLocalState(journalID)
//	tx.Commit()                                // may fail
//
// The ledger posting commits on account-service the moment the call returns. If
// anything after that fails, money has moved and nothing locally accounts for it.
//
// The outbox does not solve this. The outbox guarantees that a message written
// inside a *committed* transaction is delivered; here the transaction is the
// thing that failed, so a compensating message written into it disappears with
// it.
//
// # The mechanism
//
// This package inverts the write. A row is committed OUTSIDE the business
// transaction and cleared INSIDE it:
//
//	before the ledger call   INSERT pending  (base connection, autocommit)
//	after the ledger call    UPDATE posted   (journal id now known)
//	inside the business tx   UPDATE settled  (shares the transaction's fate)
//
// If the transaction commits, the clearing commits with it and nothing remains.
// If it rolls back, the clearing rolls back too and the row survives. A row still
// open past its grace period is a posting with no committed local counterpart,
// and Resolver reverses it.
//
// # Why the commit-ambiguity window needs no handling
//
// A failed Commit leaves the caller unable to tell whether the transaction
// applied. It does not matter here. If it applied, the clearing applied with it
// and there is no row to find; if it did not, the row is there. Atomicity is the
// decision procedure, so nothing in this package reasons about "the commit might
// have succeeded".
//
// # Usage
//
//  1. Wrap the ledger call in Guard.Around, which arms and advances the switch
//     around it. Failing to advance it is not a recoverable state, so Around
//     makes the pairing impossible to get wrong.
//  2. Call Repository.MarkSettled inside the business transaction, alongside the
//     state it is guarding.
//  3. Register Resolver's jobs from the cronjobs subpackage, and its outbox
//     Handler in the service's outbox registry.
//
// See README.md for the adoption checklist.
package ledgercompensation

import (
	"context"
	"time"

	"github.com/whagency/core-common/services/account"
)

// Status is the state of a single compensation row.
//
// The happy path is Pending -> Posted -> Settled, all within one request. The
// other two terminal states are only ever set by Resolver.
type Status uint8

const (
	// StatusPending means the ledger call is in flight; the journal id is not
	// known yet.
	StatusPending Status = iota + 1

	// StatusPosted means the ledger call returned and the journal id is recorded.
	// This is the state that matters: a row left here past its grace period is a
	// posting whose business transaction never committed.
	StatusPosted

	// StatusSettled means the business transaction committed. Set inside that
	// transaction, so it shares its fate.
	StatusSettled

	// StatusVoid means there is nothing to compensate — the posting never
	// happened, or it was already reversed.
	StatusVoid

	// StatusReversalRequested means a reversal was enqueued. Terminal from this
	// side: account-service does not report back.
	StatusReversalRequested
)

// Uint8 converts the status to its DB-compatible smallint value.
func (s Status) Uint8() uint8 {
	return uint8(s)
}

// String returns a metric- and log-friendly label.
func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusPosted:
		return "posted"
	case StatusSettled:
		return "settled"
	case StatusVoid:
		return "void"
	case StatusReversalRequested:
		return "reversal_requested"
	default:
		return "unknown"
	}
}

// AllStatuses lists every status, in order.
//
// Callers publishing a gauge must sample all of them: a Prometheus GaugeVec keeps
// the last value for a label it stops hearing about, so omitting a status that
// dropped to zero leaves a stale non-zero reading alerting forever.
func AllStatuses() []Status {
	return []Status{
		StatusPending,
		StatusPosted,
		StatusSettled,
		StatusVoid,
		StatusReversalRequested,
	}
}

// Compensation is one guarded ledger posting.
type Compensation struct {
	ID uint64

	// AggregateType and AggregateID identify the local state the posting belongs
	// to — "pay_order" and an order UUID, for example. They are opaque to this
	// package and handed back to Verifier unchanged.
	AggregateType string
	AggregateID   string

	// BizType is the journal type the ledger call used. It is stored because
	// account-service derives the idempotency key it holds from the business type
	// and the caller's key together, so a lookup needs both.
	BizType account.JournalBizType

	// IdempotencyKey is the key passed to MakeTransaction. It resolves to the
	// journal when JournalID was never recorded, and it is the row's unique key.
	IdempotencyKey string

	// JournalID is nil while Status is Pending.
	JournalID *uint64

	Status         Status
	ResolvedReason *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateParams describes a ledger call that is about to be made.
type CreateParams struct {
	AggregateType  string
	AggregateID    string
	BizType        account.JournalBizType
	IdempotencyKey string
}

// Repository persists the switch.
//
// The transaction behaviour is fixed per method rather than left to the caller,
// because getting it backwards silently defeats the mechanism:
//
//   - RecordIntent and RecordPosted always write on the base connection, never on
//     the ambient transaction. A switch written inside the business transaction
//     would be rolled back by exactly the failure it exists to detect.
//   - MarkSettled always joins the ambient transaction. Clearing the switch
//     outside it would clear it even when the business transaction went on to
//     fail.
//
// An implementation that gets this wrong produces no local symptom — the request
// still fails and the money is still gone — so it is worth asserting against a
// real database rather than a mock. See the postgres subpackage tests.
type Repository interface {
	// RecordIntent inserts a pending row before the ledger call. Re-running the
	// same business path reuses the existing row rather than adding a second.
	RecordIntent(context.Context, CreateParams) error

	// RecordPosted attaches the journal id the ledger call returned.
	RecordPosted(ctx context.Context, idempotencyKey string, journalID uint64) error

	// MarkSettled clears the switch.
	MarkSettled(ctx context.Context, idempotencyKey string) error

	// ClaimOrphans locks unresolved rows older than olderThan for this run.
	ClaimOrphans(ctx context.Context, olderThan time.Time, limit int) ([]Compensation, error)

	// Resolve writes a terminal status plus the operator-facing reason.
	Resolve(ctx context.Context, id uint64, status Status, reason string) error

	// AttachJournalID records a journal id recovered from the ledger.
	AttachJournalID(ctx context.Context, id uint64, journalID uint64) error

	// CountByStatus powers the gauge. A rising count of open rows means
	// compensation has stopped converging.
	CountByStatus(context.Context) (map[Status]int64, error)

	// Prune deletes resolved rows older than the retention window.
	Prune(ctx context.Context, olderThan time.Time) (int64, error)

	WithTxFromContext(context.Context) Repository
}
