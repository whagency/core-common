package outbox_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/outbox"
	"github.com/whagency/core-common/outbox/testkit"
)

func TestMain(m *testing.M) {
	if err := logger.Init(logger.Config{
		AppName:      "outbox-test",
		Environment:  "test",
		Version:      "0.0.0",
		Level:        "error",
		StdoutFormat: logger.StdoutFormatConsole,
	}); err != nil {
		panic("outbox test: logger init: " + err.Error())
	}
	os.Exit(m.Run())
}

// --- test helpers ---

type recordingHandler struct {
	eventType string
	err       error
	calls     int
}

func (h *recordingHandler) EventType() string { return h.eventType }
func (h *recordingHandler) Handle(_ context.Context, _ outbox.Message) error {
	h.calls++
	return h.err
}

// funcHandler calls a user-supplied function so tests can inject custom logic.
type funcHandler struct {
	eventType string
	handle    func(ctx context.Context, msg outbox.Message) error
}

func (h *funcHandler) EventType() string { return h.eventType }
func (h *funcHandler) Handle(ctx context.Context, msg outbox.Message) error {
	return h.handle(ctx, msg)
}

func newWorker(repo outbox.Repository, registry *outbox.Registry) *outbox.Worker {
	return outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		Lease:       5 * time.Second,
		BaseBackoff: 100 * time.Millisecond,
		MaxBackoff:  1 * time.Second,
	})
}

// tickAll claims a batch and processes all messages — mirrors the cron relay pattern.
func tickAll(t *testing.T, worker *outbox.Worker) {
	t.Helper()
	msgs, err := worker.Claim(context.Background(), 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	for _, msg := range msgs {
		worker.ProcessOne(context.Background(), msg)
	}
}

func seedMessage(t *testing.T, repo outbox.Repository, eventType, dedupeKey string) {
	t.Helper()
	err := repo.Create(context.Background(), outbox.CreateParams{
		AggregateType: "test_aggregate",
		AggregateID:   "1",
		EventType:     eventType,
		DedupeKey:     dedupeKey,
		Payload:       map[string]string{"k": "v"},
		MaxAttempts:   3,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// --- tests ---

func TestWorker_Deliver_Delivers(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	h := &recordingHandler{eventType: "test.event"}
	registry := outbox.NewRegistry()
	registry.Register(h)
	worker := newWorker(repo, registry)

	seedMessage(t, repo, "test.event", "k1")

	tickAll(t, worker)

	if h.calls != 1 {
		t.Errorf("handler calls = %d, want 1", h.calls)
	}
	counts := repo.CountByStatus()
	if counts[outbox.StatusDelivered] != 1 {
		t.Errorf("delivered = %d, want 1", counts[outbox.StatusDelivered])
	}
}

func TestWorker_Deliver_RetryableError_SchedulesRetry(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	h := &recordingHandler{eventType: "test.event", err: errors.New("transient")}
	registry := outbox.NewRegistry()
	registry.Register(h)
	worker := newWorker(repo, registry)

	seedMessage(t, repo, "test.event", "k2")

	tickAll(t, worker)

	counts := repo.CountByStatus()
	if counts[outbox.StatusFailed] != 1 {
		t.Errorf("failed = %d, want 1", counts[outbox.StatusFailed])
	}
	if counts[outbox.StatusDelivered] != 0 {
		t.Errorf("delivered = %d, want 0", counts[outbox.StatusDelivered])
	}
}

func TestWorker_Deliver_BudgetExhausted_DeadLetters(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	h := &recordingHandler{eventType: "test.event", err: errors.New("persistent")}
	registry := outbox.NewRegistry()
	registry.Register(h)
	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		Lease:       time.Second,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  time.Millisecond,
	})

	// MaxAttempts=2: first attempt fails → retry, second attempt → dead
	if err := repo.Create(context.Background(), outbox.CreateParams{
		AggregateType: "agg",
		AggregateID:   "1",
		EventType:     "test.event",
		DedupeKey:     "k-budget",
		Payload:       nil,
		MaxAttempts:   2,
	}); err != nil {
		t.Fatal(err)
	}

	// First run → failed (attempts=1, budget=2 → 1 < 2)
	tickAll(t, worker)
	counts := repo.CountByStatus()
	if counts[outbox.StatusFailed] != 1 {
		t.Errorf("after tick1: failed=%d, want 1", counts[outbox.StatusFailed])
	}

	// BaseBackoff/MaxBackoff are 1ms, so next_attempt_at is already in the past
	// and the second relay tick re-claims the message without any fast-forward.
	time.Sleep(5 * time.Millisecond)

	// Second run → dead (nextAttemptNum=2 >= maxAttempts=2)
	tickAll(t, worker)
	counts = repo.CountByStatus()
	if counts[outbox.StatusDead] != 1 {
		t.Errorf("after tick2: dead=%d, want 1", counts[outbox.StatusDead])
	}
}

func TestWorker_Deliver_NonRetryableError_DeadLettersImmediately(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	nonRetryableErr := outbox.MarkNonRetryable(errors.New("bad payload"))
	h := &recordingHandler{eventType: "test.event", err: nonRetryableErr}
	registry := outbox.NewRegistry()
	registry.Register(h)
	worker := newWorker(repo, registry)

	seedMessage(t, repo, "test.event", "k3")

	tickAll(t, worker)

	counts := repo.CountByStatus()
	if counts[outbox.StatusDead] != 1 {
		t.Errorf("dead = %d, want 1", counts[outbox.StatusDead])
	}
	if counts[outbox.StatusFailed] != 0 {
		t.Errorf("failed = %d, want 0 (non-retryable must not be retried)", counts[outbox.StatusFailed])
	}
	if h.calls != 1 {
		t.Errorf("handler calls = %d, want 1 (must not retry)", h.calls)
	}
}

func TestWorker_Deliver_UnregisteredEventType_DeadLetters(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	registry := outbox.NewRegistry() // no handlers
	worker := newWorker(repo, registry)

	seedMessage(t, repo, "unknown.event", "k4")

	tickAll(t, worker)

	counts := repo.CountByStatus()
	if counts[outbox.StatusDead] != 1 {
		t.Errorf("dead = %d, want 1 (unknown event_type must be dead-lettered)", counts[outbox.StatusDead])
	}
}

func TestWorker_Deliver_EmptyQueue_NoOp(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	registry := outbox.NewRegistry()
	worker := newWorker(repo, registry)

	tickAll(t, worker) // must not panic or error on empty queue
}

func TestWorker_Deliver_DedupeKey_SilentIgnore(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	h := &recordingHandler{eventType: "test.event"}
	registry := outbox.NewRegistry()
	registry.Register(h)
	worker := newWorker(repo, registry)

	// Insert same dedupe_key twice.
	for range 2 {
		if err := repo.Create(context.Background(), outbox.CreateParams{
			AggregateType: "agg",
			AggregateID:   "1",
			EventType:     "test.event",
			DedupeKey:     "same-key",
			Payload:       nil,
		}); err != nil {
			t.Fatal(err)
		}
	}

	all := repo.All()
	if len(all) != 1 {
		t.Errorf("messages in repo = %d, want 1 (duplicate must be ignored)", len(all))
	}

	tickAll(t, worker)

	if h.calls != 1 {
		t.Errorf("handler calls = %d, want 1", h.calls)
	}
}

func TestRegistry_DuplicateHandler_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate handler registration")
		}
	}()

	registry := outbox.NewRegistry()
	registry.Register(&recordingHandler{eventType: "ev"})
	registry.Register(&recordingHandler{eventType: "ev"}) // must panic
}

func TestMarkNonRetryable_ErrorsIs(t *testing.T) {
	t.Parallel()

	err := outbox.MarkNonRetryable(errors.New("inner"))
	if !errors.Is(err, outbox.ErrNonRetryable) {
		t.Error("MarkNonRetryable result must satisfy errors.Is(err, ErrNonRetryable)")
	}
}

func TestWorker_Hooks_OnDelivered(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	h := &recordingHandler{eventType: "test.event"}
	registry := outbox.NewRegistry()
	registry.Register(h)

	var deliveredTypes []string
	var durationCalled bool
	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		Lease: time.Second,
		Hooks: outbox.WorkerHooks{
			OnDelivered:       func(et string) { deliveredTypes = append(deliveredTypes, et) },
			OnHandlerDuration: func(_ string, _ time.Duration) { durationCalled = true },
		},
	})

	seedMessage(t, repo, "test.event", "hook-delivered")
	tickAll(t, worker)

	if len(deliveredTypes) != 1 || deliveredTypes[0] != "test.event" {
		t.Errorf("OnDelivered: got %v, want [test.event]", deliveredTypes)
	}
	if !durationCalled {
		t.Error("OnHandlerDuration must be called on every outcome")
	}
}

func TestWorker_Hooks_OnFailed(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	h := &recordingHandler{eventType: "test.event", err: errors.New("transient")}
	registry := outbox.NewRegistry()
	registry.Register(h)

	var failedTypes []string
	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		Lease: time.Second,
		Hooks: outbox.WorkerHooks{
			OnFailed: func(et string) { failedTypes = append(failedTypes, et) },
		},
	})

	seedMessage(t, repo, "test.event", "hook-failed")
	tickAll(t, worker)

	if len(failedTypes) != 1 || failedTypes[0] != "test.event" {
		t.Errorf("OnFailed: got %v, want [test.event]", failedTypes)
	}
}

func TestWorker_Hooks_OnDead(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	h := &recordingHandler{eventType: "test.event", err: outbox.MarkNonRetryable(errors.New("bad"))}
	registry := outbox.NewRegistry()
	registry.Register(h)

	var deadTypes []string
	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		Lease: time.Second,
		Hooks: outbox.WorkerHooks{
			OnDead: func(et string) { deadTypes = append(deadTypes, et) },
		},
	})

	seedMessage(t, repo, "test.event", "hook-dead")
	tickAll(t, worker)

	if len(deadTypes) != 1 || deadTypes[0] != "test.event" {
		t.Errorf("OnDead: got %v, want [test.event]", deadTypes)
	}
}

func TestWorker_HandlerTimeout_TreatedAsRetryable(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()

	handlerDone := make(chan struct{})
	h := &funcHandler{
		eventType: "slow.event",
		handle: func(ctx context.Context, _ outbox.Message) error {
			defer close(handlerDone)
			<-ctx.Done() // blocks until HandlerTimeout fires
			return ctx.Err()
		},
	}

	registry := outbox.NewRegistry()
	registry.Register(h)

	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		Lease:          2 * time.Second,
		HandlerTimeout: 50 * time.Millisecond,
		BaseBackoff:    time.Millisecond,
	})

	seedMessage(t, repo, "slow.event", "timeout-1")
	tickAll(t, worker) // blocks for HandlerTimeout then returns

	<-handlerDone

	counts := repo.CountByStatus()
	if counts[outbox.StatusFailed] != 1 {
		t.Errorf("timed-out handler must result in failed status, got: %v", counts)
	}
}
