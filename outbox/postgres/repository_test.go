package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/whagency/core-common/outbox"
	outboxpostgres "github.com/whagency/core-common/outbox/postgres"
	outboxmigrations "github.com/whagency/core-common/outbox/postgres/migrations"
	"github.com/whagency/core-common/testkit/postgrestest"
)

var suite *postgrestest.Suite

func TestMain(m *testing.M) {
	var err error
	suite, err = postgrestest.Start(context.Background(), postgrestest.ContainerConfig{})
	if err != nil {
		panic("postgrestest: start: " + err.Error())
	}
	defer suite.Terminate(context.Background()) //nolint:errcheck
	m.Run()
}

func openRepo(t *testing.T) *outboxpostgres.Repository {
	t.Helper()
	db := suite.OpenGormDB(t, postgrestest.OpenConfig{
		MigrationsFS:      outboxmigrations.FS,
		SkipMigrationLine: postgrestest.SkipDatabaseLevelMigrationLine,
	})
	return outboxpostgres.New(db, "outbox_messages")
}

func TestRepository_Create_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	p := outbox.CreateParams{
		AggregateType: "deposit_request",
		AggregateID:   "42",
		EventType:     "restriction.counter.update",
		DedupeKey:     "counter:account-deposit-42",
		Payload:       map[string]string{"key": "value"},
	}

	// First insert succeeds.
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Second insert with same dedupe_key must be silently ignored.
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("duplicate create: %v", err)
	}

	msgs, err := repo.ClaimDue(ctx, "worker-test", 10*time.Second, 100)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("messages = %d, want 1 (duplicate must be ignored)", len(msgs))
	}
}

func TestRepository_ClaimDue_ClaimsReadyMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	for i := range 3 {
		if err := repo.Create(ctx, outbox.CreateParams{
			AggregateType: "test",
			AggregateID:   "1",
			EventType:     "ev",
			DedupeKey:     "claim-test-" + string(rune('a'+i)),
			Payload:       nil,
		}); err != nil {
			t.Fatal(err)
		}
	}

	msgs, err := repo.ClaimDue(ctx, "worker-1", 30*time.Second, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("claimed = %d, want 3", len(msgs))
	}
	for _, m := range msgs {
		if m.Status != outbox.StatusProcessing {
			t.Errorf("msg %d: status = %q, want processing", m.ID, m.Status)
		}
		if m.LockedBy == nil || *m.LockedBy != "worker-1" {
			t.Errorf("msg %d: locked_by unexpected", m.ID)
		}
	}
}

func TestRepository_ClaimDue_SkipsNotReady(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	// Insert one message with next_attempt_at far in the future.
	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test",
		AggregateID:   "1",
		EventType:     "ev",
		DedupeKey:     "future-attempt",
		Payload:       nil,
	}); err != nil {
		t.Fatal(err)
	}

	// Claim to move to processing, then mark failed with future retry.
	msgs, err := repo.ClaimDue(ctx, "w", 30*time.Second, 1)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("initial claim: msgs=%d err=%v", len(msgs), err)
	}
	if err := repo.MarkFailed(ctx, msgs[0].ID, "w", "err", time.Now().Add(1*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Now claim again — should return nothing (next_attempt_at is in the future).
	msgs2, err := repo.ClaimDue(ctx, "w", 30*time.Second, 10)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(msgs2) != 0 {
		t.Errorf("claimed = %d, want 0 (message is not yet due)", len(msgs2))
	}
}

func TestRepository_MarkDelivered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test", AggregateID: "1",
		EventType: "ev", DedupeKey: "delivered-1", Payload: nil,
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := repo.ClaimDue(ctx, "w", 30*time.Second, 1)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("claim: %v", err)
	}

	if err := repo.MarkDelivered(ctx, msgs[0].ID, "w"); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}

	// Should not be claimable anymore.
	msgs2, _ := repo.ClaimDue(ctx, "w", 30*time.Second, 10)
	if len(msgs2) != 0 {
		t.Errorf("delivered message must not be re-claimed")
	}
}

func TestRepository_MarkDead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test", AggregateID: "1",
		EventType: "ev", DedupeKey: "dead-1", Payload: nil,
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := repo.ClaimDue(ctx, "w", 30*time.Second, 1)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("claim: %v", err)
	}

	if err := repo.MarkDead(ctx, msgs[0].ID, "w", "non-retryable"); err != nil {
		t.Fatalf("mark dead: %v", err)
	}

	// Must not be claimed again.
	msgs2, _ := repo.ClaimDue(ctx, "w", 30*time.Second, 10)
	if len(msgs2) != 0 {
		t.Errorf("dead message must not be re-claimed")
	}
}

func TestRepository_ClaimDue_ReclainsExpiredLease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test", AggregateID: "1",
		EventType: "ev", DedupeKey: "stale-lease", Payload: nil,
	}); err != nil {
		t.Fatal(err)
	}

	// Claim with a very short lease (already expired by the time we claim again).
	// We use a negative lease duration to simulate an already-expired lock.
	msgs, err := repo.ClaimDue(ctx, "worker-A", -1*time.Second, 1)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("initial claim: %v", err)
	}

	// A second worker must be able to reclaim the stale message.
	msgs2, err := repo.ClaimDue(ctx, "worker-B", 30*time.Second, 10)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if len(msgs2) != 1 {
		t.Errorf("reclaimed = %d, want 1 (expired lease must be reclaimable)", len(msgs2))
	}
	if msgs2[0].LockedBy == nil || *msgs2[0].LockedBy != "worker-B" {
		t.Error("reclaimed message must be owned by worker-B")
	}
}

func TestRepository_OldestPendingAge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test", AggregateID: "1",
		EventType: "ev.a", DedupeKey: "lag-a", Payload: nil,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test", AggregateID: "1",
		EventType: "ev.b", DedupeKey: "lag-b", Payload: nil,
	}); err != nil {
		t.Fatal(err)
	}

	ages, err := repo.OldestPendingAge(ctx)
	if err != nil {
		t.Fatalf("oldest pending: %v", err)
	}
	if _, ok := ages["ev.a"]; !ok {
		t.Error("ev.a must appear in lag results")
	}
	if _, ok := ages["ev.b"]; !ok {
		t.Error("ev.b must appear in lag results")
	}
}

func TestRepository_MarkFailed_IncrementsAttempts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test", AggregateID: "1",
		EventType: "ev", DedupeKey: "failed-attempts", Payload: nil,
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := repo.ClaimDue(ctx, "w", 30*time.Second, 1)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("claim: %v", err)
	}
	if msgs[0].Attempts != 0 {
		t.Fatalf("initial attempts = %d, want 0", msgs[0].Attempts)
	}

	// Schedule the retry in the past so the message is immediately re-claimable.
	if err := repo.MarkFailed(ctx, msgs[0].ID, "w", "transient error", time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	// A status update requires a live lease, so re-claim before failing again.
	reclaimed, err := repo.ClaimDue(ctx, "w", 30*time.Second, 1)
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("reclaim after first failure: msgs=%d err=%v", len(reclaimed), err)
	}
	if reclaimed[0].Attempts != 1 {
		t.Fatalf("attempts after 1x MarkFailed = %d, want 1", reclaimed[0].Attempts)
	}
	if err := repo.MarkFailed(ctx, reclaimed[0].ID, "w", "transient error", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}

	msgs2, err := repo.ClaimDue(ctx, "w", 30*time.Second, 1)
	if err != nil || len(msgs2) != 1 {
		t.Fatalf("reclaim after failed: %v", err)
	}
	if msgs2[0].Attempts != 2 {
		t.Errorf("attempts after 2x MarkFailed = %d, want 2", msgs2[0].Attempts)
	}
	if msgs2[0].LastError == nil || *msgs2[0].LastError != "transient error" {
		t.Errorf("last_error = %v, want 'transient error'", msgs2[0].LastError)
	}
}

func TestRepository_Prune_DeletesOldDeliveredAndDead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	keys := []string{"prune-delivered", "prune-dead", "prune-pending"}
	for _, k := range keys {
		if err := repo.Create(ctx, outbox.CreateParams{
			AggregateType: "test", AggregateID: "1",
			EventType: "ev", DedupeKey: k, Payload: nil,
		}); err != nil {
			t.Fatal(err)
		}
	}

	msgs, err := repo.ClaimDue(ctx, "w", 30*time.Second, 10)
	if err != nil || len(msgs) != 2 { // prune-pending is not claimed (it's just pending)
		// Actually all 3 are pending and ready — claim all
	}
	msgs, err = repo.ClaimDue(ctx, "w", 30*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}

	// Find and transition: delivered, dead, leave one as pending by not claiming it.
	// Re-seed so we have exactly the statuses we want.
	repo2 := openRepo(t) // fresh schema for this sub-scenario
	for _, k := range []string{"old-delivered", "old-dead", "keep-pending"} {
		if err := repo2.Create(ctx, outbox.CreateParams{
			AggregateType: "test", AggregateID: "1",
			EventType: "ev", DedupeKey: k, Payload: nil,
		}); err != nil {
			t.Fatal(err)
		}
	}

	claimed, err := repo2.ClaimDue(ctx, "w", 30*time.Second, 10)
	if err != nil || len(claimed) != 3 {
		t.Fatalf("claim all: got %d err=%v", len(claimed), err)
	}

	for _, m := range claimed {
		switch m.DedupeKey {
		case "old-delivered":
			if err := repo2.MarkDelivered(ctx, m.ID, "w"); err != nil {
				t.Fatal(err)
			}
		case "old-dead":
			if err := repo2.MarkDead(ctx, m.ID, "w", "fatal"); err != nil {
				t.Fatal(err)
			}
		case "keep-pending":
			// leave in processing — MarkFailed back to pending
			if err := repo2.MarkFailed(ctx, m.ID, "w", "retry", time.Now().Add(-time.Second)); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Prune with 0 retention — everything older than now qualifies.
	n, err := repo2.Prune(ctx, 0, 0)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 2 {
		t.Errorf("pruned = %d, want 2 (delivered + dead)", n)
	}

	// Pending/failed message must still exist.
	remaining, err := repo2.ClaimDue(ctx, "w2", 30*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].DedupeKey != "keep-pending" {
		t.Errorf("remaining after prune: got %d messages, want 1 keep-pending", len(remaining))
	}
}

func TestRepository_ClaimDue_RespectsRequestedLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	for i := range 5 {
		if err := repo.Create(ctx, outbox.CreateParams{
			AggregateType: "test", AggregateID: "1",
			EventType: "ev", DedupeKey: "batch-" + string(rune('a'+i)), Payload: nil,
		}); err != nil {
			t.Fatal(err)
		}
	}

	msgs, err := repo.ClaimDue(ctx, "w", 30*time.Second, 3)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("claimed = %d, want 3 (batch_size=3)", len(msgs))
	}
}

// FOR UPDATE SKIP LOCKED is the whole basis for running the relay on several
// pods at once: two simultaneous claims must partition the ready rows, never
// overlap on one.
func TestRepository_ClaimDue_ConcurrentClaimsDoNotOverlap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	const total = 60
	for i := range total {
		if err := repo.Create(ctx, outbox.CreateParams{
			AggregateType: "test", AggregateID: "1",
			EventType: "ev", DedupeKey: fmt.Sprintf("concurrent-%03d", i), Payload: nil,
		}); err != nil {
			t.Fatal(err)
		}
	}

	const workers = 6
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = make(map[int64]string)
		dupes   []int64
	)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workerID := fmt.Sprintf("worker-%d", w)
			msgs, err := repo.ClaimDue(ctx, workerID, 30*time.Second, total)
			if err != nil {
				t.Errorf("%s: claim: %v", workerID, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, m := range msgs {
				if prev, exists := claimed[m.ID]; exists {
					dupes = append(dupes, m.ID)
					t.Errorf("message %d claimed by both %s and %s", m.ID, prev, workerID)
					continue
				}
				claimed[m.ID] = workerID
			}
		}()
	}
	wg.Wait()

	if len(dupes) > 0 {
		t.Fatalf("%d messages were claimed more than once", len(dupes))
	}
	if len(claimed) != total {
		t.Errorf("distinct messages claimed = %d, want %d", len(claimed), total)
	}
}

// A worker that dies mid-handler never reports an error, so the only way its
// message can ever stop being retried is for the reclaim itself to charge the
// retry budget.
func TestRepository_ClaimDue_StaleReclaimConsumesBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test", AggregateID: "1",
		EventType: "ev", DedupeKey: "stale-budget", Payload: nil,
	}); err != nil {
		t.Fatal(err)
	}

	// A fresh pending row costs nothing.
	first, err := repo.ClaimDue(ctx, "w", -time.Second, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: msgs=%d err=%v", len(first), err)
	}
	if first[0].Attempts != 0 {
		t.Errorf("attempts after first claim = %d, want 0", first[0].Attempts)
	}

	for want := 1; want <= 3; want++ {
		got, err := repo.ClaimDue(ctx, "w", -time.Second, 1)
		if err != nil || len(got) != 1 {
			t.Fatalf("reclaim %d: msgs=%d err=%v", want, len(got), err)
		}
		if got[0].Attempts != want {
			t.Errorf("attempts after %d reclaims = %d, want %d", want, got[0].Attempts, want)
		}
	}
}

// The status updates are scoped to the lease holder, so a worker that lost its
// lease cannot overwrite the state of whoever reclaimed the message.
func TestRepository_StatusUpdates_RejectedAfterLeaseLost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test", AggregateID: "1",
		EventType: "ev", DedupeKey: "lease-lost", Payload: nil,
	}); err != nil {
		t.Fatal(err)
	}

	// worker-A claims with an already-expired lease; worker-B takes it over.
	a, err := repo.ClaimDue(ctx, "worker-A", -time.Second, 1)
	if err != nil || len(a) != 1 {
		t.Fatalf("claim by A: %v", err)
	}
	b, err := repo.ClaimDue(ctx, "worker-B", 30*time.Second, 1)
	if err != nil || len(b) != 1 {
		t.Fatalf("reclaim by B: %v", err)
	}

	id := a[0].ID
	if err := repo.MarkDelivered(ctx, id, "worker-A"); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Errorf("MarkDelivered by stale worker: err = %v, want ErrLeaseLost", err)
	}
	if err := repo.MarkFailed(ctx, id, "worker-A", "boom", time.Now()); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Errorf("MarkFailed by stale worker: err = %v, want ErrLeaseLost", err)
	}
	if err := repo.MarkDead(ctx, id, "worker-A", "boom"); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Errorf("MarkDead by stale worker: err = %v, want ErrLeaseLost", err)
	}

	// The rightful owner still succeeds.
	if err := repo.MarkDelivered(ctx, id, "worker-B"); err != nil {
		t.Errorf("MarkDelivered by lease holder: %v", err)
	}
}

// GC must never remove a message a worker is still holding.
func TestRepository_Prune_LeavesInFlightAndPendingAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	for _, k := range []string{"in-flight", "still-pending"} {
		if err := repo.Create(ctx, outbox.CreateParams{
			AggregateType: "test", AggregateID: "1",
			EventType: "ev", DedupeKey: k, Payload: nil,
		}); err != nil {
			t.Fatal(err)
		}
	}

	claimed, err := repo.ClaimDue(ctx, "w", 30*time.Second, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: msgs=%d err=%v", len(claimed), err)
	}

	// Zero retention: everything that is eligible at all is eligible now.
	n, err := repo.Prune(ctx, 0, 0)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 0 {
		t.Errorf("pruned = %d, want 0 (processing and pending are not prunable)", n)
	}

	// The in-flight message must still be updatable by its owner.
	if err := repo.MarkDelivered(ctx, claimed[0].ID, "w"); err != nil {
		t.Errorf("in-flight message was disturbed by prune: %v", err)
	}
}

// Retention for delivered rows runs from delivered_at, so a long-queued message
// delivered a moment ago is not collected just because it was created earlier.
func TestRepository_Prune_DeliveredAgesFromDeliveredAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test", AggregateID: "1",
		EventType: "ev", DedupeKey: "just-delivered", Payload: nil,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimDue(ctx, "w", 30*time.Second, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v", err)
	}
	if err := repo.MarkDelivered(ctx, claimed[0].ID, "w"); err != nil {
		t.Fatal(err)
	}

	n, err := repo.Prune(ctx, time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 0 {
		t.Errorf("pruned = %d, want 0 (delivered under an hour ago must be kept)", n)
	}
}

// Delivered rows fill the disk; dead rows are the incident record and the only
// replay source. A shared retention would force a choice between the two.
func TestRepository_Prune_RetainsDeadLongerThanDelivered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	for _, k := range []string{"gone-delivered", "kept-dead"} {
		if err := repo.Create(ctx, outbox.CreateParams{
			AggregateType: "test", AggregateID: "1",
			EventType: "ev", DedupeKey: k, Payload: nil,
		}); err != nil {
			t.Fatal(err)
		}
	}

	claimed, err := repo.ClaimDue(ctx, "w", 30*time.Second, 10)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim: got %d err=%v", len(claimed), err)
	}
	for _, m := range claimed {
		switch m.DedupeKey {
		case "gone-delivered":
			if err := repo.MarkDelivered(ctx, m.ID, "w"); err != nil {
				t.Fatal(err)
			}
		case "kept-dead":
			if err := repo.MarkDead(ctx, m.ID, "w", "fatal"); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Delivered collected immediately, dead retained for an hour.
	n, err := repo.Prune(ctx, 0, time.Hour)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned = %d, want 1 (only the delivered row)", n)
	}

	remaining, err := repo.ClaimDue(ctx, "w2", 30*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("dead rows must not be claimable, got %d", len(remaining))
	}
	// A dead row that survived pruning still blocks its dedupe key.
	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "test", AggregateID: "1",
		EventType: "ev", DedupeKey: "kept-dead", Payload: nil,
	}); err != nil {
		t.Fatal(err)
	}
	again, err := repo.ClaimDue(ctx, "w3", 30*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	if dead := len(again); dead != 0 {
		t.Errorf("re-insert of a retained dead key created %d claimable rows, want 0", dead)
	}
}

// Every pod runs the prune cron — the platform cron has no leader election — so
// concurrent prunes are the normal case, not an edge case. SKIP LOCKED makes
// them partition the work; without it they serialise on each other's row locks
// and N pods do one pod's work.
func TestRepository_Prune_ConcurrentPrunersPartitionTheWork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := openRepo(t)

	const total = 400
	for i := range total {
		if err := repo.Create(ctx, outbox.CreateParams{
			AggregateType: "test", AggregateID: "1",
			EventType: "ev", DedupeKey: fmt.Sprintf("prune-race-%03d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := repo.ClaimDue(ctx, "w", 5*time.Minute, total)
	if err != nil || len(claimed) != total {
		t.Fatalf("claim: got %d err=%v", len(claimed), err)
	}
	for _, m := range claimed {
		if err := repo.MarkDelivered(ctx, m.ID, "w"); err != nil {
			t.Fatal(err)
		}
	}

	const pods = 4
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		deleted int64
	)
	for range pods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := repo.Prune(ctx, 0, time.Hour)
			if err != nil {
				t.Errorf("prune: %v", err)
				return
			}
			mu.Lock()
			deleted += n
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Every row deleted exactly once across all pruners: the counts must sum to
	// the total, never exceed it.
	if deleted != total {
		t.Errorf("rows deleted across %d pruners = %d, want %d", pods, deleted, total)
	}

	remaining, err := repo.ClaimDue(ctx, "w2", 30*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Errorf("claimable rows after prune = %d, want 0", len(remaining))
	}
}
