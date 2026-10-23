package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whagency/core-common/outbox"
	"github.com/whagency/core-common/outbox/testkit"
)

func TestPool_Worker_PanicRecovery(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()

	var mu sync.Mutex
	var processed []string

	// First message panics; second must still be processed by the same worker goroutine.
	h := &funcHandler{
		eventType: "test.event",
		handle: func(_ context.Context, msg outbox.Message) error {
			if msg.DedupeKey == "panic-msg" {
				panic("intentional handler panic")
			}
			mu.Lock()
			processed = append(processed, msg.DedupeKey)
			mu.Unlock()
			return nil
		},
	}

	registry := outbox.NewRegistry()
	registry.Register(h)

	for _, key := range []string{"panic-msg", "ok-msg"} {
		if err := repo.Create(context.Background(), outbox.CreateParams{
			AggregateType: "test",
			AggregateID:   "1",
			EventType:     "test.event",
			DedupeKey:     key,
			Payload:       nil,
		}); err != nil {
			t.Fatal(err)
		}
	}

	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		Lease: 30 * time.Second,
	})
	pool := outbox.NewPool(worker, outbox.PoolConfig{
		Workers:       1, // single worker so panic path and ok path are sequential
		RelayInterval: 20 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	poolDone := make(chan error, 1)
	go func() { poolDone <- pool.Run(ctx) }()

	// Wait until ok-msg is delivered — proves the worker goroutine survived the panic.
	deadline := time.After(2 * time.Second)
	for {
		counts := repo.CountByStatus()
		if counts[outbox.StatusDelivered] >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("ok-msg was not delivered: worker goroutine likely died after panic")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	<-poolDone

	// The panicking message must reach a terminal state. Leaving it in
	// 'processing' would have it reclaimed on every lease expiry forever,
	// panicking each time, without ever firing OnDead — an invisible hot loop.
	counts := repo.CountByStatus()
	if counts[outbox.StatusDead] != 1 {
		t.Errorf("dead = %d, want 1 (a panicking handler must dead-letter its message)", counts[outbox.StatusDead])
	}
	if counts[outbox.StatusProcessing] != 0 {
		t.Errorf("processing = %d, want 0 (no message may be left mid-flight)", counts[outbox.StatusProcessing])
	}

	mu.Lock()
	defer mu.Unlock()
	if len(processed) != 1 || processed[0] != "ok-msg" {
		t.Errorf("processed = %v, want [ok-msg]", processed)
	}
}

func TestPool_Run_ContextCancel_ExitsCleanly(t *testing.T) {
	t.Parallel()

	repo := testkit.NewFakeRepository()
	registry := outbox.NewRegistry()
	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		Lease: time.Second,
	})
	pool := outbox.NewPool(worker, outbox.PoolConfig{
		Workers:       2,
		RelayInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- pool.Run(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Pool.Run returned %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Pool.Run did not exit after ctx cancel")
	}
}

// A backlog far larger than the pool's capacity must still be delivered exactly
// once per message. This is the end-to-end guard on the claim budget and the
// in-flight set working together: without them the relay claims ahead, leases
// lapse on queued messages, and the relay re-dispatches work already running.
func TestPool_LargeBacklog_DeliversEachMessageExactlyOnce(t *testing.T) {
	t.Parallel()

	const total = 60

	repo := testkit.NewFakeRepository()

	var mu sync.Mutex
	handled := make(map[string]int)

	h := &funcHandler{
		eventType: "test.event",
		handle: func(_ context.Context, msg outbox.Message) error {
			mu.Lock()
			handled[msg.DedupeKey]++
			mu.Unlock()
			time.Sleep(time.Millisecond)
			return nil
		},
	}
	registry := outbox.NewRegistry()
	registry.Register(h)

	for i := range total {
		if err := repo.Create(context.Background(), outbox.CreateParams{
			AggregateType: "test", AggregateID: "1",
			EventType: "test.event",
			DedupeKey: fmt.Sprintf("backlog-%03d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		WorkerID:       "w", // deliberately larger than the pool can absorb
		HandlerTimeout: 50 * time.Millisecond,
	})
	pool := outbox.NewPool(worker, outbox.PoolConfig{
		Workers:       4, // will be capped to what the lease affords
		RelayInterval: 2 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolDone := make(chan error, 1)
	go func() { poolDone <- pool.Run(ctx) }()

	deadline := time.After(8 * time.Second)
	for {
		if repo.CountByStatus()[outbox.StatusDelivered] == total {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only %d/%d messages delivered", repo.CountByStatus()[outbox.StatusDelivered], total)
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	<-poolDone

	mu.Lock()
	defer mu.Unlock()
	if len(handled) != total {
		t.Errorf("distinct messages handled = %d, want %d", len(handled), total)
	}
	for key, calls := range handled {
		if calls != 1 {
			t.Errorf("message %s handled %d times, want exactly 1", key, calls)
		}
	}
}

// RelayInterval is the idle poll interval, not a throughput ceiling. Claiming
// one budget per tick would cap the pool at (QueueSize+Workers)/RelayInterval
// messages per second regardless of how fast handlers actually are.
func TestPool_Backlog_DrainsFasterThanOneBatchPerTick(t *testing.T) {
	t.Parallel()

	// Worker counts below the backlog are the interesting case: one claim per
	// tick would cap throughput at (queue+workers) per RelayInterval regardless
	// of how fast the handlers are.
	for _, workers := range []int{1, 4, 8} {
		t.Run(fmt.Sprintf("%d workers", workers), func(t *testing.T) {
			t.Parallel()
			assertDrainsWithinOneTick(t, workers)
		})
	}
}

func assertDrainsWithinOneTick(t *testing.T, workers int) {
	t.Helper()

	const total = 120

	repo := testkit.NewFakeRepository()
	registry := outbox.NewRegistry()
	registry.Register(&funcHandler{
		eventType: "test.event",
		handle:    func(context.Context, outbox.Message) error { return nil },
	})

	for i := range total {
		if err := repo.Create(context.Background(), outbox.CreateParams{
			AggregateType: "test", AggregateID: "1",
			EventType: "test.event",
			DedupeKey: fmt.Sprintf("drain-%03d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		WorkerID: "w", HandlerTimeout: 50 * time.Millisecond,
	})
	// A single tick's budget is QueueSize+Workers, far below the backlog, so
	// draining within one interval proves the relay keeps going inside a tick.
	pool := outbox.NewPool(worker, outbox.PoolConfig{
		Workers:       workers,
		RelayInterval: 2 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolDone := make(chan error, 1)
	go func() { poolDone <- pool.Run(ctx) }()

	deadline := time.After(1500 * time.Millisecond)
	for {
		if repo.CountByStatus()[outbox.StatusDelivered] == total {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("%d workers: only %d/%d delivered within one relay interval: the tick is capping throughput",
				workers, repo.CountByStatus()[outbox.StatusDelivered], total)
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	<-poolDone
}

// Shutdown must not punish messages that were merely queued. Running their
// handlers under the pool's cancelled context would fail every one of them
// instantly, burn a retry attempt and reschedule them a full backoff away —
// strictly worse than abandoning them, which costs only a lease expiry.
func TestPool_Shutdown_DrainsQueuedMessagesWithoutBurningAttempts(t *testing.T) {
	t.Parallel()

	const total = 6

	repo := testkit.NewFakeRepository()

	release := make(chan struct{})
	var released sync.Once
	registry := outbox.NewRegistry()
	registry.Register(&funcHandler{
		eventType: "test.event",
		handle: func(ctx context.Context, _ outbox.Message) error {
			// Block the first handlers so messages pile up in the queue, then let
			// everything through once shutdown has started.
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})

	for i := range total {
		if err := repo.Create(context.Background(), outbox.CreateParams{
			AggregateType: "test", AggregateID: "1",
			EventType: "test.event",
			DedupeKey: fmt.Sprintf("shutdown-%02d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		WorkerID:       "w",
		HandlerTimeout: 2 * time.Second,
		BaseBackoff:    time.Hour, // a burnt attempt would be unmistakable
	})
	pool := outbox.NewPool(worker, outbox.PoolConfig{
		Workers:       2,
		RelayInterval: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pool.Run(ctx) }()

	// Wait until the pool is holding messages, then shut down and unblock.
	deadline := time.After(2 * time.Second)
	for {
		if len(repo.All()) == total {
			break
		}
		select {
		case <-deadline:
			t.Fatal("messages were never claimed")
		case <-time.After(2 * time.Millisecond):
		}
	}
	time.Sleep(20 * time.Millisecond)

	cancel()
	released.Do(func() { close(release) })

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Pool.Run did not exit")
	}

	counts := repo.CountByStatus()
	if counts[outbox.StatusFailed] != 0 {
		t.Errorf("failed = %d, want 0: shutdown must not fail queued messages", counts[outbox.StatusFailed])
	}
	if counts[outbox.StatusDead] != 0 {
		t.Errorf("dead = %d, want 0", counts[outbox.StatusDead])
	}
	// Anything not drained within the budget stays 'processing' and is reclaimed
	// by lease expiry with its retry budget intact — the correct fallback.
	for _, m := range repo.All() {
		if m.Attempts != 0 {
			t.Errorf("message %s consumed %d attempts during shutdown, want 0", m.DedupeKey, m.Attempts)
		}
	}
}

// A panic in the claim path must not take the process down with it. The relay
// runs inside a service that also answers HTTP and gRPC: an outbox defect that
// stops deposits being accepted is a wildly disproportionate blast radius.
func TestPool_RelayPanic_IsContainedAndReported(t *testing.T) {
	t.Parallel()

	repo := &panicOnClaimRepo{FakeRepository: testkit.NewFakeRepository()}

	var mu sync.Mutex
	var claimErrs []string

	registry := outbox.NewRegistry()
	registry.Register(&funcHandler{
		eventType: "test.event",
		handle:    func(context.Context, outbox.Message) error { return nil },
	})

	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		WorkerID: "w",
		Hooks: outbox.WorkerHooks{
			OnClaimError: func(err error) {
				mu.Lock()
				claimErrs = append(claimErrs, err.Error())
				mu.Unlock()
			},
		},
	})
	pool := outbox.NewPool(worker, outbox.PoolConfig{
		Workers:       2,
		RelayInterval: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- pool.Run(ctx) }()

	// Let the relay tick into the panic several times.
	deadline := time.After(time.Second)
	for {
		mu.Lock()
		n := len(claimErrs)
		mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("relay did not recover and keep ticking after a panic")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Pool.Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Pool.Run did not exit after a recovered relay panic")
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(claimErrs[0], "relay panic") {
		t.Errorf("claim error = %q, want it to name the panic", claimErrs[0])
	}
}

// A claim failure is the only immediate signal that delivery has stopped: the
// lag gauge reads the same database, so an outage freezes it instead of making
// it climb.
func TestPool_ClaimError_FiresHook(t *testing.T) {
	t.Parallel()

	repo := &failOnClaimRepo{FakeRepository: testkit.NewFakeRepository()}

	var mu sync.Mutex
	var seen int

	worker := outbox.NewWorker(repo, outbox.NewRegistry(), outbox.WorkerConfig{
		WorkerID: "w",
		Hooks: outbox.WorkerHooks{
			OnClaimError: func(error) {
				mu.Lock()
				seen++
				mu.Unlock()
			},
		},
	})
	pool := outbox.NewPool(worker, outbox.PoolConfig{Workers: 1, RelayInterval: 5 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- pool.Run(ctx) }()

	deadline := time.After(500 * time.Millisecond)
	for {
		mu.Lock()
		n := seen
		mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("OnClaimError never fired for an unreachable repository")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done
}

type panicOnClaimRepo struct{ *testkit.FakeRepository }

func (r *panicOnClaimRepo) ClaimDue(context.Context, string, time.Duration, int) ([]outbox.Message, error) {
	panic("driver blew up mid-scan")
}

type failOnClaimRepo struct{ *testkit.FakeRepository }

func (r *failOnClaimRepo) ClaimDue(context.Context, string, time.Duration, int) ([]outbox.Message, error) {
	return nil, errors.New("connection refused")
}
