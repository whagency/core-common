package ledgercompensation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whagency/core-common/outbox"
	"github.com/whagency/core-common/services/account"
	"github.com/whagency/core-common/testkit/dbtest"
	"github.com/whagency/core-common/testkit/loggertest"
)

const testAggregateType = "pay_order"

// --- doubles -----------------------------------------------------------------

// fakeRepo serves one claim batch and records the terminal transitions written.
type fakeRepo struct {
	claimed  []Compensation
	resolved []resolvedRow
	attached map[uint64]uint64
	counts   map[Status]int64
}

type resolvedRow struct {
	id     uint64
	status Status
	reason string
}

func newFakeRepo(claimed []Compensation) *fakeRepo {
	return &fakeRepo{claimed: claimed, attached: map[uint64]uint64{}}
}

func (r *fakeRepo) WithTxFromContext(context.Context) Repository { return r }

func (r *fakeRepo) ClaimOrphans(context.Context, time.Time, int) ([]Compensation, error) {
	return r.claimed, nil
}

func (r *fakeRepo) Resolve(_ context.Context, id uint64, status Status, reason string) error {
	r.resolved = append(r.resolved, resolvedRow{id: id, status: status, reason: reason})
	return nil
}

func (r *fakeRepo) AttachJournalID(_ context.Context, id, journalID uint64) error {
	r.attached[id] = journalID
	return nil
}

func (r *fakeRepo) CountByStatus(context.Context) (map[Status]int64, error) { return r.counts, nil }

func (r *fakeRepo) RecordIntent(context.Context, CreateParams) error   { return nil }
func (r *fakeRepo) RecordPosted(context.Context, string, uint64) error { return nil }
func (r *fakeRepo) MarkSettled(context.Context, string) error          { return nil }
func (r *fakeRepo) Prune(context.Context, time.Time) (int64, error)    { return 0, nil }

// fakeOutbox records the reversal rows the resolver wrote.
type fakeOutbox struct{ rows []outbox.CreateParams }

func (o *fakeOutbox) Create(_ context.Context, p outbox.CreateParams) error {
	o.rows = append(o.rows, p)
	return nil
}
func (o *fakeOutbox) WithTxFromContext(context.Context) outbox.Repository { return o }
func (o *fakeOutbox) ClaimDue(context.Context, string, time.Duration, int) ([]outbox.Message, error) {
	return nil, nil
}
func (o *fakeOutbox) MarkDelivered(context.Context, int64, string) error { return nil }
func (o *fakeOutbox) MarkFailed(context.Context, int64, string, string, time.Time) error {
	return nil
}
func (o *fakeOutbox) MarkDead(context.Context, int64, string, string) error { return nil }
func (o *fakeOutbox) OldestPendingAge(context.Context) (map[string]time.Duration, error) {
	return nil, nil
}
func (o *fakeOutbox) Prune(context.Context, time.Duration, time.Duration) (int64, error) {
	return 0, nil
}

func (o *fakeOutbox) dedupeKeys() []string {
	keys := make([]string, 0, len(o.rows))
	for i := range o.rows {
		keys = append(keys, o.rows[i].DedupeKey)
	}
	return keys
}

// fakeVerifier answers the "is the local side committed?" question.
type fakeVerifier struct {
	settled bool
	err     error
}

func (v fakeVerifier) IsSettled(context.Context, string, string, uint64) (bool, error) {
	return v.settled, v.err
}

// fakeJournals answers the read-only ledger lookup.
type fakeJournals struct {
	result account.GetJournalByIdempotencyKeyResult
	err    error
	calls  int
}

func (j *fakeJournals) GetJournalByIdempotencyKey(
	context.Context,
	account.GetJournalByIdempotencyKeyParams,
) (account.GetJournalByIdempotencyKeyResult, error) {
	j.calls++
	return j.result, j.err
}

// --- helpers -----------------------------------------------------------------

func newResolver(t *testing.T, repo Repository, ob outbox.Repository, v Verifier, j JournalFinder) *Resolver {
	t.Helper()
	loggertest.Init(t, "ledgercompensation-test")

	r, err := NewResolver(ResolverConfig{
		Repository:         repo,
		Outbox:             ob,
		Verifier:           v,
		Journals:           j,
		TransactionManager: &dbtest.ObservableTxManager{},
		Settings:           Settings{Grace: 2 * time.Minute, BatchSize: 100, Retention: time.Hour},
	})
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	return r
}

// postedRow is a row whose ledger call landed and whose transaction did not.
func postedRow(id uint64, journalID uint64) Compensation {
	return Compensation{
		ID:             id,
		AggregateType:  testAggregateType,
		AggregateID:    "order-1",
		BizType:        account.JournalBizTypeCheckoutPayment,
		IdempotencyKey: "checkout_payment:order-1",
		JournalID:      &journalID,
		Status:         StatusPosted,
	}
}

// pendingRow is a row whose RecordPosted never landed.
func pendingRow(id uint64) Compensation {
	row := postedRow(id, 0)
	row.JournalID = nil
	row.Status = StatusPending
	return row
}

// --- tests -------------------------------------------------------------------

// TestResolver_PostedOrphanRequestsReversal covers the core case: money moved,
// nothing locally accounts for it, so the posting is reversed.
func TestResolver_PostedOrphanRequestsReversal(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo([]Compensation{postedRow(1, 777)})
	ob := &fakeOutbox{}
	r := newResolver(t, repo, ob, fakeVerifier{settled: false}, &fakeJournals{})

	if err := r.ResolveOrphans(context.Background()); err != nil {
		t.Fatalf("ResolveOrphans returned error: %v", err)
	}

	if len(repo.resolved) != 1 || repo.resolved[0].status != StatusReversalRequested {
		t.Fatalf("expected one reversal_requested row, got %#v", repo.resolved)
	}
	if got := ob.dedupeKeys(); len(got) != 1 || got[0] != "rollback:777" {
		t.Fatalf("expected one reversal keyed on the journal, got %#v", got)
	}
}

// TestResolver_SettledLocalStateIsNotReversed is the guard that keeps the
// resolver from undoing a payment that actually went through.
func TestResolver_SettledLocalStateIsNotReversed(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo([]Compensation{postedRow(1, 777)})
	ob := &fakeOutbox{}
	r := newResolver(t, repo, ob, fakeVerifier{settled: true}, &fakeJournals{})

	if err := r.ResolveOrphans(context.Background()); err != nil {
		t.Fatalf("ResolveOrphans returned error: %v", err)
	}

	if len(repo.resolved) != 1 || repo.resolved[0].status != StatusSettled {
		t.Fatalf("expected the row to be settled, got %#v", repo.resolved)
	}
	if len(ob.rows) != 0 {
		t.Fatalf("expected no reversal for committed local state, got %d rows", len(ob.rows))
	}
}

// TestResolver_PendingRowResolvesThroughLedgerLookup covers the row whose
// RecordPosted never landed: the journal id has to come from the ledger.
func TestResolver_PendingRowResolvesThroughLedgerLookup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		lookup        account.GetJournalByIdempotencyKeyResult
		wantStatus    Status
		wantOutboxKey string
		wantAttached  uint64
	}{
		{
			name: "live posting is reversed",
			lookup: account.GetJournalByIdempotencyKeyResult{
				Found: true, JournalID: 999, Status: account.JournalStatusCompleted,
			},
			wantStatus:    StatusReversalRequested,
			wantOutboxKey: "rollback:999",
			wantAttached:  999,
		},
		{
			// The ledger call never landed. Nothing moved, nothing to undo.
			name:       "missing posting is voided",
			lookup:     account.GetJournalByIdempotencyKeyResult{Found: false},
			wantStatus: StatusVoid,
		},
		{
			// Already compensated — most likely by a best-effort publish on the
			// failure path. A rollback frees the original key, so this is what an
			// already-reversed posting looks like from here.
			name: "already reversed posting is voided",
			lookup: account.GetJournalByIdempotencyKeyResult{
				Found: true, JournalID: 999, Status: account.JournalStatusReversed,
			},
			wantStatus: StatusVoid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newFakeRepo([]Compensation{pendingRow(1)})
			ob := &fakeOutbox{}
			journals := &fakeJournals{result: tt.lookup}
			r := newResolver(t, repo, ob, fakeVerifier{settled: false}, journals)

			if err := r.ResolveOrphans(context.Background()); err != nil {
				t.Fatalf("ResolveOrphans returned error: %v", err)
			}

			if len(repo.resolved) != 1 || repo.resolved[0].status != tt.wantStatus {
				t.Fatalf("expected status %s, got %#v", tt.wantStatus, repo.resolved)
			}
			if journals.calls != 1 {
				t.Fatalf("expected exactly one ledger lookup, got %d", journals.calls)
			}

			got := ob.dedupeKeys()
			if tt.wantOutboxKey == "" {
				if len(got) != 0 {
					t.Fatalf("expected no reversal request, got %#v", got)
				}
				return
			}
			if len(got) != 1 || got[0] != tt.wantOutboxKey {
				t.Fatalf("expected reversal keyed %q, got %#v", tt.wantOutboxKey, got)
			}
			// A recovered journal id is persisted so a later run need not ask again.
			if repo.attached[1] != tt.wantAttached {
				t.Fatalf("expected journal %d to be attached, got %d", tt.wantAttached, repo.attached[1])
			}
		})
	}
}

// TestResolver_PostedRowSkipsTheLookup keeps the resolver from calling
// account-service for a row that already knows its journal.
func TestResolver_PostedRowSkipsTheLookup(t *testing.T) {
	t.Parallel()

	journals := &fakeJournals{}
	r := newResolver(t, newFakeRepo([]Compensation{postedRow(1, 777)}), &fakeOutbox{},
		fakeVerifier{settled: false}, journals)

	if err := r.ResolveOrphans(context.Background()); err != nil {
		t.Fatalf("ResolveOrphans returned error: %v", err)
	}
	if journals.calls != 0 {
		t.Fatalf("expected no ledger lookup for a posted row, got %d", journals.calls)
	}
}

// TestResolver_UndecidableRowsStayOpen checks that a row the resolver cannot
// decide is left alone rather than guessed at. Reversing on a failed lookup or a
// failed verification could undo a real payment; leaving it open costs a tick.
func TestResolver_UndecidableRowsStayOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		row      Compensation
		verifier Verifier
		journals JournalFinder
	}{
		{
			name:     "ledger lookup fails",
			row:      pendingRow(1),
			verifier: fakeVerifier{settled: false},
			journals: &fakeJournals{err: errors.New("account-service unavailable")},
		},
		{
			name:     "verification fails",
			row:      postedRow(1, 777),
			verifier: fakeVerifier{err: errors.New("database unavailable")},
			journals: &fakeJournals{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newFakeRepo([]Compensation{tt.row})
			ob := &fakeOutbox{}
			r := newResolver(t, repo, ob, tt.verifier, tt.journals)

			if err := r.ResolveOrphans(context.Background()); err == nil {
				t.Fatal("expected ResolveOrphans to report the failure")
			}
			if len(repo.resolved) != 0 {
				t.Fatalf("expected the row to stay open, got %#v", repo.resolved)
			}
			if len(ob.rows) != 0 {
				t.Fatalf("expected no reversal on an undecidable row, got %d rows", len(ob.rows))
			}
		})
	}
}

// TestResolver_OneBadRowDoesNotBlockTheBatch keeps a single undecidable row from
// stalling every other orphan behind it.
func TestResolver_OneBadRowDoesNotBlockTheBatch(t *testing.T) {
	t.Parallel()

	bad := pendingRow(1)
	good := postedRow(2, 888)

	repo := newFakeRepo([]Compensation{bad, good})
	ob := &fakeOutbox{}
	r := newResolver(t, repo, ob, fakeVerifier{settled: false},
		&fakeJournals{err: errors.New("account-service unavailable")})

	if err := r.ResolveOrphans(context.Background()); err == nil {
		t.Fatal("expected ResolveOrphans to report the failed row")
	}

	if len(repo.resolved) != 1 || repo.resolved[0].id != good.ID {
		t.Fatalf("expected only the resolvable row to be closed, got %#v", repo.resolved)
	}
	if got := ob.dedupeKeys(); len(got) != 1 || got[0] != "rollback:888" {
		t.Fatalf("expected the resolvable row to be reversed, got %#v", got)
	}
}

// TestResolver_NoOrphansDoesNothing keeps the quiet path quiet: the job runs
// every minute and must not touch the ledger when there is no work.
func TestResolver_NoOrphansDoesNothing(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo(nil)
	ob := &fakeOutbox{}
	journals := &fakeJournals{}
	r := newResolver(t, repo, ob, fakeVerifier{}, journals)

	if err := r.ResolveOrphans(context.Background()); err != nil {
		t.Fatalf("ResolveOrphans returned error: %v", err)
	}
	if len(repo.resolved) != 0 || len(ob.rows) != 0 || journals.calls != 0 {
		t.Fatal("expected no work on an empty batch")
	}
}

// TestResolver_CountByStatusReportsEveryStatus pins the gauge contract: a status
// that dropped to zero must still be sampled, or its last non-zero value keeps
// alerting forever.
func TestResolver_CountByStatusReportsEveryStatus(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo(nil)
	repo.counts = map[Status]int64{StatusPosted: 3}
	r := newResolver(t, repo, &fakeOutbox{}, fakeVerifier{}, &fakeJournals{})

	got, err := r.CountByStatus(context.Background())
	if err != nil {
		t.Fatalf("CountByStatus returned error: %v", err)
	}

	if len(got) != len(AllStatuses()) {
		t.Fatalf("expected every status to be sampled, got %#v", got)
	}
	if got["posted"] != 3 {
		t.Fatalf("expected posted=3, got %v", got["posted"])
	}
	if got["pending"] != 0 {
		t.Fatalf("expected an explicit zero for pending, got %v", got["pending"])
	}
}

// TestNewResolver_RejectsUnsafeConfig checks the constructor fails fast. A zero
// grace in particular would let the resolver reverse a posting whose transaction
// is still in flight.
func TestNewResolver_RejectsUnsafeConfig(t *testing.T) {
	t.Parallel()

	valid := ResolverConfig{
		Repository:         newFakeRepo(nil),
		Outbox:             &fakeOutbox{},
		Verifier:           fakeVerifier{},
		Journals:           &fakeJournals{},
		TransactionManager: &dbtest.ObservableTxManager{},
		Settings:           Settings{Grace: time.Minute, BatchSize: 10},
	}

	tests := []struct {
		name   string
		mutate func(*ResolverConfig)
	}{
		{"missing repository", func(c *ResolverConfig) { c.Repository = nil }},
		{"missing outbox", func(c *ResolverConfig) { c.Outbox = nil }},
		{"missing verifier", func(c *ResolverConfig) { c.Verifier = nil }},
		{"missing journal finder", func(c *ResolverConfig) { c.Journals = nil }},
		{"missing transaction manager", func(c *ResolverConfig) { c.TransactionManager = nil }},
		{"zero grace", func(c *ResolverConfig) { c.Settings.Grace = 0 }},
		{"zero batch size", func(c *ResolverConfig) { c.Settings.BatchSize = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			tt.mutate(&cfg)
			if _, err := NewResolver(cfg); err == nil {
				t.Fatal("expected NewResolver to reject the config")
			}
		})
	}
}
