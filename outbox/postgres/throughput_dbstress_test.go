//go:build dbstress

// Throughput measurement for the outbox pool against a real PostgreSQL.
//
// Excluded from normal runs by the dbstress tag; see the repository README.
//
//	go test -tags=dbstress ./outbox/postgres -run Throughput -v -timeout=15m
//
// The numbers are a capacity signal, not a pass/fail threshold: they depend on
// the host, so treat them as "what does this configuration afford" rather than
// a regression gate.
package postgres_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whagency/core-common/outbox"
	outboxpostgres "github.com/whagency/core-common/outbox/postgres"
	outboxmigrations "github.com/whagency/core-common/outbox/postgres/migrations"
	"github.com/whagency/core-common/testkit/loggertest"
	"github.com/whagency/core-common/testkit/postgrestest"
	"gorm.io/gorm"
)

// accountServiceWorker mirrors account-service's shipped defaults so the
// measurement reflects what actually runs in production.
func accountServiceWorker(repo outbox.Repository, registry *outbox.Registry) *outbox.Worker {
	return outbox.NewWorker(repo, registry, outbox.WorkerConfig{
		WorkerID:       "throughput",
		Lease:          8 * time.Second,
		HandlerTimeout: 2 * time.Second,
		BaseBackoff:    30 * time.Second,
		MaxBackoff:     6 * time.Hour,
	})
}

func TestThroughput_DefaultConfig(t *testing.T) {
	tests := []struct {
		name            string
		handlerDuration time.Duration
		workers         int
		messages        int
	}{
		{name: "free handler, 4 workers", handlerDuration: 0, workers: 4, messages: 3000},
		{name: "5ms handler, 4 workers", handlerDuration: 5 * time.Millisecond, workers: 4, messages: 1500},
		{name: "20ms handler, 4 workers", handlerDuration: 20 * time.Millisecond, workers: 4, messages: 800},
		{name: "20ms handler, 16 workers", handlerDuration: 20 * time.Millisecond, workers: 16, messages: 1500},
		{name: "20ms handler, 24 workers", handlerDuration: 20 * time.Millisecond, workers: 24, messages: 2000},
		{name: "20ms handler, 32 workers", handlerDuration: 20 * time.Millisecond, workers: 32, messages: 2000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := openThroughputDB(t)
			repo := outboxpostgres.New(db, "outbox_messages")

			counting := &countingRepo{Repository: repo}
			handler := &sleepHandler{d: tt.handlerDuration}
			registry := outbox.NewRegistry()
			registry.Register(handler)

			for i := range tt.messages {
				if err := repo.Create(ctx, outbox.CreateParams{
					AggregateType: "deposit_request",
					AggregateID:   fmt.Sprint(i),
					EventType:     "bench.event",
					DedupeKey:     fmt.Sprintf("bench-%s-%06d", tt.name, i),
					Payload:       map[string]any{"amount": "10.5", "asset": "USDT"},
				}); err != nil {
					t.Fatal(err)
				}
			}

			worker := accountServiceWorker(counting, registry)
			pool := outbox.NewPool(worker, outbox.PoolConfig{
				Workers:       tt.workers,
				RelayInterval: 500 * time.Millisecond,
			})

			runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			done := make(chan error, 1)
			start := time.Now()
			go func() { done <- pool.Run(runCtx) }()

			var elapsed time.Duration
			for {
				var delivered int64
				if err := db.Raw("SELECT count(*) FROM outbox_messages WHERE status = 'delivered'").
					Scan(&delivered).Error; err != nil {
					t.Fatalf("count: %v", err)
				}
				if delivered >= int64(tt.messages) {
					elapsed = time.Since(start)
					break
				}
				if runCtx.Err() != nil {
					t.Fatalf("timed out with %d/%d delivered", delivered, tt.messages)
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
			<-done

			rate := float64(tt.messages) / elapsed.Seconds()
			calls := handler.count()
			// Duplicate handler calls mean leases lapsed while messages were
			// queued and the relay re-dispatched them. At-least-once permits it,
			// but it is wasted work and a signal the pool is over-subscribed.
			claims, claimed, statusOps := counting.stats()
			avgBatch := float64(claimed) / float64(max(claims, 1))
			queriesPerMsg := float64(claims+statusOps) / float64(tt.messages)

			t.Logf("%s: %.0f msg/s | dup %.1f%% | claims %d (avg batch %.1f) + status %d = %.2f queries/msg",
				tt.name, rate,
				100*float64(calls-int64(tt.messages))/float64(tt.messages),
				claims, avgBatch, statusOps, queriesPerMsg)

			var dead int64
			if err := db.Raw("SELECT count(*) FROM outbox_messages WHERE status = 'dead'").
				Scan(&dead).Error; err != nil {
				t.Fatalf("dead count: %v", err)
			}
			if dead != 0 {
				t.Errorf("dead = %d, want 0: throughput pressure must never dead-letter a message", dead)
			}
		})
	}
}

// openThroughputDB opens an isolated schema with the canonical outbox schema and
// a pool wide enough that the connection limit is not what is being measured.
func openThroughputDB(t *testing.T) *gorm.DB {
	t.Helper()
	// The pool logs; the repository does not, which is why the plain repository
	// tests never needed this.
	loggertest.Init(t, "outbox-throughput")

	return suite.OpenGormDB(t, postgrestest.OpenConfig{
		MigrationsFS:      outboxmigrations.FS,
		SkipMigrationLine: postgrestest.SkipDatabaseLevelMigrationLine,
		MaxOpenConns:      64,
		MaxIdleConns:      64,
	})
}

// countingRepo records how many database round trips the pool actually makes,
// separating the amortised claim from the per-message status write.
type countingRepo struct {
	outbox.Repository

	claims    atomic.Int64
	claimed   atomic.Int64
	statusOps atomic.Int64
}

func (r *countingRepo) ClaimDue(ctx context.Context, workerID string, lease time.Duration, batch int) ([]outbox.Message, error) {
	r.claims.Add(1)
	msgs, err := r.Repository.ClaimDue(ctx, workerID, lease, batch)
	r.claimed.Add(int64(len(msgs)))
	return msgs, err
}

func (r *countingRepo) MarkDelivered(ctx context.Context, id int64, workerID string) error {
	r.statusOps.Add(1)
	return r.Repository.MarkDelivered(ctx, id, workerID)
}

func (r *countingRepo) stats() (claims, claimed, statusOps int64) {
	return r.claims.Load(), r.claimed.Load(), r.statusOps.Load()
}

type sleepHandler struct {
	d     time.Duration
	calls atomic.Int64
}

func (h *sleepHandler) EventType() string { return "bench.event" }

func (h *sleepHandler) Handle(_ context.Context, _ outbox.Message) error {
	h.calls.Add(1)
	if h.d > 0 {
		time.Sleep(h.d)
	}
	return nil
}

func (h *sleepHandler) count() int64 { return h.calls.Load() }
