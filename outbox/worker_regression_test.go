package outbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/whagency/core-common/outbox"
	"github.com/whagency/core-common/outbox/testkit"
)

// A worker whose lease expired must not overwrite the state of the worker that
// reclaimed the message. Without the ownership guard the late writer wins and
// can mark a message delivered while its new owner is still running the handler.
func TestWorker_LeaseLost_StatusUpdateDiscarded(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	h := &recordingHandler{eventType: "test.event"}
	registry := outbox.NewRegistry()
	registry.Register(h)

	seedMessage(t, repo, "test.event", "lease-lost")

	// Worker A claims the message, then stalls long enough for its lease to lapse.
	workerA := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		WorkerID: "worker-A", Lease: 30 * time.Second,
	})
	claimed, err := workerA.Claim(context.Background(), 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim by A: msgs=%d err=%v", len(claimed), err)
	}
	repo.ExpireLease(claimed[0].ID)

	// Worker B reclaims the stale message and now owns it.
	workerB := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		WorkerID: "worker-B", Lease: 30 * time.Second,
	})
	reclaimed, err := workerB.Claim(context.Background(), 10)
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("reclaim by B: msgs=%d err=%v", len(reclaimed), err)
	}

	// A finishes late. Its status update must be dropped, not applied.
	workerA.ProcessOne(context.Background(), claimed[0])

	counts := repo.CountByStatus()
	if counts[outbox.StatusDelivered] != 0 {
		t.Errorf("delivered = %d, want 0 (stale worker must not overwrite the new owner)", counts[outbox.StatusDelivered])
	}
	if counts[outbox.StatusProcessing] != 1 {
		t.Errorf("processing = %d, want 1 (message still belongs to worker-B)", counts[outbox.StatusProcessing])
	}
}

// A message that keeps killing its worker never returns an error, so it never
// reaches the post-handler budget check. Stale reclaims consume the budget and
// the worker must dead-letter it before dispatching it yet again.
func TestWorker_BudgetExhaustedByReclaims_DeadLettersBeforeDispatch(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	h := &recordingHandler{eventType: "test.event"}
	registry := outbox.NewRegistry()
	registry.Register(h)

	if err := repo.Create(context.Background(), outbox.CreateParams{
		AggregateType: "agg", AggregateID: "1",
		EventType: "test.event", DedupeKey: "poison", MaxAttempts: 2,
	}); err != nil {
		t.Fatal(err)
	}

	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		WorkerID: "w", Lease: 30 * time.Second,
	})

	// First claim is a fresh pending row: no budget consumed. Each subsequent
	// claim follows a lapsed lease and therefore costs one attempt.
	for range 3 {
		claimed, err := worker.Claim(context.Background(), 10)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claim: msgs=%d err=%v", len(claimed), err)
		}
		repo.ExpireLease(claimed[0].ID)
	}

	live := worker
	msgs, err := live.Claim(context.Background(), 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("final claim: msgs=%d err=%v", len(msgs), err)
	}
	if msgs[0].Attempts < msgs[0].MaxAttempts {
		t.Fatalf("attempts = %d, want >= max_attempts %d", msgs[0].Attempts, msgs[0].MaxAttempts)
	}

	live.ProcessOne(context.Background(), msgs[0])

	if h.calls != 0 {
		t.Errorf("handler calls = %d, want 0 (exhausted message must not be dispatched)", h.calls)
	}
	if counts := repo.CountByStatus(); counts[outbox.StatusDead] != 1 {
		t.Errorf("dead = %d, want 1 (poison message must terminate)", counts[outbox.StatusDead])
	}
}

// A handler that succeeded but whose status write failed must be redelivered
// rather than silently dropped — the at-least-once guarantee in its rawest form.
func TestWorker_MarkDeliveredFails_MessageIsRedelivered(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()

	var mu sync.Mutex
	failNext := true
	repo.FailMarkDelivered = func(int64) error {
		mu.Lock()
		defer mu.Unlock()
		if failNext {
			failNext = false
			return errors.New("connection reset")
		}
		return nil
	}

	h := &recordingHandler{eventType: "test.event"}
	registry := outbox.NewRegistry()
	registry.Register(h)

	seedMessage(t, repo, "test.event", "redeliver")

	// First pass: handler succeeds but the status write fails.
	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		WorkerID: "w", Lease: 30 * time.Second,
	})
	msgs, err := worker.Claim(context.Background(), 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("first claim: %v", err)
	}
	worker.ProcessOne(context.Background(), msgs[0])

	if h.calls != 1 {
		t.Fatalf("handler calls after first pass = %d, want 1", h.calls)
	}
	if counts := repo.CountByStatus(); counts[outbox.StatusDelivered] != 0 {
		t.Fatalf("delivered = %d, want 0 (status write failed)", counts[outbox.StatusDelivered])
	}

	// Second pass: once the lease lapses the message is reclaimable and lands.
	repo.ExpireLease(msgs[0].ID)
	live := worker
	msgs2, err := live.Claim(context.Background(), 10)
	if err != nil || len(msgs2) != 1 {
		t.Fatalf("reclaim: msgs=%d err=%v", len(msgs2), err)
	}
	live.ProcessOne(context.Background(), msgs2[0])

	if h.calls != 2 {
		t.Errorf("handler calls = %d, want 2 (message must be redelivered)", h.calls)
	}
	if counts := repo.CountByStatus(); counts[outbox.StatusDelivered] != 1 {
		t.Errorf("delivered = %d, want 1", counts[outbox.StatusDelivered])
	}
}

// The retry schedule must grow, stay inside MaxBackoff, and never produce a
// non-positive delay that would spin the relay.
func TestWorker_Backoff_GrowsAndStaysWithinBounds(t *testing.T) {
	t.Parallel()

	const (
		base = 100 * time.Millisecond
		max  = 2 * time.Second
	)

	repo := testkit.NewFakeRepository()
	h := &recordingHandler{eventType: "test.event", err: errors.New("transient")}
	registry := outbox.NewRegistry()
	registry.Register(h)

	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		WorkerID: "w", Lease: 30 * time.Second,
		BaseBackoff: base, MaxBackoff: max,
	})

	if err := repo.Create(context.Background(), outbox.CreateParams{
		AggregateType: "agg", AggregateID: "1",
		EventType: "test.event", DedupeKey: "backoff", MaxAttempts: 9,
	}); err != nil {
		t.Fatal(err)
	}

	var delays []time.Duration
	for attempt := 1; attempt <= 8; attempt++ {
		msgs, err := worker.Claim(context.Background(), 10)
		if err != nil || len(msgs) != 1 {
			t.Fatalf("attempt %d: claim returned %d msgs, err=%v", attempt, len(msgs), err)
		}
		before := time.Now().UTC()
		worker.ProcessOne(context.Background(), msgs[0])

		all := repo.All()
		if len(all) != 1 {
			t.Fatalf("attempt %d: expected 1 message", attempt)
		}
		delay := all[0].NextAttemptAt.Sub(before)
		if delay <= 0 {
			t.Fatalf("attempt %d: delay = %s, must be positive", attempt, delay)
		}
		// ±10% jitter around the capped exponential.
		if delay > max+max/5 {
			t.Errorf("attempt %d: delay %s exceeds MaxBackoff %s plus jitter", attempt, delay, max)
		}
		delays = append(delays, delay)

		// Bring the scheduled retry forward instead of sleeping through it.
		repo.MakeDue(all[0].ID)
	}

	// Growth holds until the cap absorbs it; compare across the doubling region.
	if delays[3] <= delays[0] {
		t.Errorf("backoff must grow: delays[0]=%s delays[3]=%s", delays[0], delays[3])
	}
	if delays[len(delays)-1] < max/2 {
		t.Errorf("late delays should sit near the cap, got %s (max %s)", delays[len(delays)-1], max)
	}
}
