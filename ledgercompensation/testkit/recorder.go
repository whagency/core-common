// Package testkit provides test doubles for the ledger compensation runtime.
//
// It lives in common because the interesting assertion is the same in every
// adopting service: did the flow arm the switch before the ledger call and clear
// it after everything else? Order is the assertion, not just the call count.
package testkit

import (
	"context"
	"sync"
	"time"

	"github.com/whagency/core-common/ledgercompensation"
)

// Step names recorded by Recorder.
const (
	StepIntent  = "intent"
	StepPosted  = "posted"
	StepSettled = "settled"
)

// Recorder is an in-memory ledgercompensation.Repository that records the
// transitions a flow drove.
//
// It implements the full Repository interface, but only the three write methods a
// controller calls are meaningful; the resolver-facing methods return zero values
// so a controller test never has to stub them.
type Recorder struct {
	mu    sync.Mutex
	steps []string
	keys  []string

	// SettleErr, when set, fails MarkSettled — the step that shares the business
	// transaction's fate, and therefore the one whose failure must abort the flow
	// rather than be logged.
	SettleErr error

	// IntentErr and PostedErr fail the corresponding write.
	IntentErr error
	PostedErr error
}

// NewRecorder creates an empty Recorder.
func NewRecorder() *Recorder {
	return &Recorder{}
}

// Steps returns the recorded transitions in order.
func (r *Recorder) Steps() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.steps...)
}

// Keys returns the idempotency key each recorded step carried.
func (r *Recorder) Keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.keys...)
}

func (r *Recorder) record(step, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
	r.keys = append(r.keys, key)
}

// RecordIntent records the arming step.
func (r *Recorder) RecordIntent(_ context.Context, p ledgercompensation.CreateParams) error {
	if r.IntentErr != nil {
		return r.IntentErr
	}
	r.record(StepIntent, p.IdempotencyKey)
	return nil
}

// RecordPosted records the journal id step.
func (r *Recorder) RecordPosted(_ context.Context, idempotencyKey string, _ uint64) error {
	if r.PostedErr != nil {
		return r.PostedErr
	}
	r.record(StepPosted, idempotencyKey)
	return nil
}

// MarkSettled records the clearing step.
func (r *Recorder) MarkSettled(_ context.Context, idempotencyKey string) error {
	if r.SettleErr != nil {
		return r.SettleErr
	}
	r.record(StepSettled, idempotencyKey)
	return nil
}

// WithTxFromContext returns the recorder itself.
//
// A recorder cannot model a rollback, which is exactly why the survives-a-
// rollback invariant has to be asserted against a real database instead. See the
// postgres subpackage tests.
func (r *Recorder) WithTxFromContext(context.Context) ledgercompensation.Repository {
	return r
}

// ClaimOrphans is unused by controller tests.
func (r *Recorder) ClaimOrphans(context.Context, time.Time, int) ([]ledgercompensation.Compensation, error) {
	return nil, nil
}

// Resolve is unused by controller tests.
func (r *Recorder) Resolve(context.Context, uint64, ledgercompensation.Status, string) error {
	return nil
}

// AttachJournalID is unused by controller tests.
func (r *Recorder) AttachJournalID(context.Context, uint64, uint64) error { return nil }

// CountByStatus is unused by controller tests.
func (r *Recorder) CountByStatus(context.Context) (map[ledgercompensation.Status]int64, error) {
	return nil, nil
}

// Prune is unused by controller tests.
func (r *Recorder) Prune(context.Context, time.Time) (int64, error) { return 0, nil }
