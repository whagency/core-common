package ledgercompensation

import (
	"context"
	"fmt"
)

// Guard arms and advances the switch around a ledger call.
type Guard struct {
	repo Repository
}

// NewGuard creates a Guard backed by repo.
func NewGuard(repo Repository) *Guard {
	return &Guard{repo: repo}
}

// LedgerCall performs the posting and returns its journal id.
type LedgerCall func(ctx context.Context) (uint64, error)

// Around records the intent, runs call, and records the resulting journal id.
//
// The pairing is wrapped rather than left to the caller because the two writes
// are not independently useful: an intent with no journal id is a posting the
// resolver can only recover by asking the ledger, and a posting with no intent is
// invisible entirely. Forgetting the second call is silent, and the cost lands on
// a money path.
//
// The error from call is returned unwrapped, so callers keep matching on the
// ledger's typed errors — insufficient funds, restricted operation — exactly as
// they would without the guard.
//
// ctx may carry the business transaction; the repository pins these two writes to
// the base connection regardless, which is the whole point.
func (g *Guard) Around(ctx context.Context, params CreateParams, call LedgerCall) (uint64, error) {
	if err := g.repo.RecordIntent(ctx, params); err != nil {
		return 0, fmt.Errorf("[ledgercompensation][Guard:Around][arm]%w", err)
	}

	journalID, err := call(ctx)
	if err != nil {
		// Return the ledger error untouched: the caller's error mapping depends on
		// it. The row stays pending, and the resolver decides later whether a
		// posting happened at all — the call may have succeeded on the far side
		// and failed on the way back.
		return 0, err
	}

	// Failing here is fatal rather than best-effort. Continuing would leave a
	// posting whose journal id is recorded nowhere, which is the one state this
	// package exists to prevent.
	if errPosted := g.repo.RecordPosted(ctx, params.IdempotencyKey, journalID); errPosted != nil {
		return 0, fmt.Errorf("[ledgercompensation][Guard:Around][advance]%w", errPosted)
	}

	return journalID, nil
}

// Settled clears the switch. Call it inside the business transaction, alongside
// the state the posting is being recorded against.
func (g *Guard) Settled(ctx context.Context, idempotencyKey string) error {
	if err := g.repo.WithTxFromContext(ctx).MarkSettled(ctx, idempotencyKey); err != nil {
		return fmt.Errorf("[ledgercompensation][Guard:Settled]%w", err)
	}
	return nil
}
