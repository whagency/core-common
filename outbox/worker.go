package outbox

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"time"

	"github.com/whagency/core-common/logger"
	"go.uber.org/zap"
)

// WorkerHooks are optional callbacks fired by the Worker after each message
// outcome. Wire Prometheus counters or structured log aggregators here.
// Nil fields are silently skipped — no-op by default.
type WorkerHooks struct {
	// OnDelivered is called when a message handler returns nil.
	OnDelivered func(eventType string)
	// OnFailed is called when a retryable handler error is recorded.
	OnFailed func(eventType string)
	// OnDead is called when a message is moved to dead status
	// (non-retryable error or retry budget exhausted).
	OnDead func(eventType string)
	// OnHandlerDuration is called with the wall-clock handler execution time
	// regardless of outcome. Use for latency histograms.
	OnHandlerDuration func(eventType string, d time.Duration)

	// OnClaimError is called when the relay cannot claim work — the database is
	// unreachable, or the claim path panicked.
	//
	// This is the only immediate signal that delivery has stopped entirely. The
	// lag gauge cannot cover it: sampling the lag reads the same database, so a
	// database outage leaves the gauge frozen on its last value rather than
	// climbing. Alert on this counter, not only on lag.
	OnClaimError func(err error)

	// OnLeaseLost is called when a status update is discarded because another
	// worker had already reclaimed the message.
	//
	// Not an error — the new owner is responsible for the outcome — but a
	// sustained rate means work is being done twice and thrown away, which is
	// what an over-subscribed pool or a too-short lease looks like.
	OnLeaseLost func(eventType string)
}

// WorkerConfig controls the runtime behaviour of a Worker.
// Zero values are replaced with safe defaults by NewWorker.
type WorkerConfig struct {
	// WorkerID is stamped on claimed messages as the lease owner.
	// Defaults to "<hostname>:<pid>" when empty.
	WorkerID string

	// Lease is how long a claimed message remains locked to this worker.
	// Default 15s. Enforced floor: HandlerTimeout + statusUpdateGrace —
	// applyDefaults raises Lease when the supplied value is smaller, because a
	// handler that can outlive its own lease guarantees double processing.
	//
	// Note this covers one in-flight message only. Pool additionally requires
	// the lease to cover the queueing delay before dispatch; see PoolConfig.
	Lease time.Duration

	// HandlerTimeout is the per-message deadline applied to each handler call.
	// A handler that exceeds this deadline receives a cancelled context and its
	// error is treated as a retryable failure. Default 5s.
	//
	// Size it from the transport's tail latency, not from caution. It is not a
	// free safety margin: it sets the floor under Lease (and therefore under
	// crash-recovery time), it bounds how long one stuck call occupies a worker,
	// and a larger value buys fewer queue slots for the same lease.
	HandlerTimeout time.Duration

	// BaseBackoff is the base delay for the exponential retry schedule. Default 30s.
	BaseBackoff time.Duration

	// MaxBackoff caps the exponential growth. Default 6h.
	MaxBackoff time.Duration

	// Hooks are optional outcome callbacks for observability (metrics, tracing).
	// Nil hooks are skipped. Safe to leave zero-valued.
	Hooks WorkerHooks
}

// statusUpdateGrace is the head-room the lease must keep beyond the handler
// deadline so the terminal status update still lands inside the lease window,
// and the deadline applied to that update.
//
// The update is a single-row UPDATE, measured at well under a millisecond, so
// this is generous by three orders of magnitude on purpose: it also absorbs a
// connection-pool wait or a brief database stall. Keeping it small matters
// because it is a fixed term in the lease floor — an oversized grace would
// dominate the floor once HandlerTimeout is tuned down to the real transport
// latency.
const statusUpdateGrace = time.Second

func (c *WorkerConfig) applyDefaults() {
	if c.WorkerID == "" {
		c.WorkerID = defaultWorkerID()
	}
	if c.Lease <= 0 {
		c.Lease = 15 * time.Second
	}
	if c.HandlerTimeout <= 0 {
		c.HandlerTimeout = 5 * time.Second
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 30 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 6 * time.Hour
	}

	// A handler that can outlive its own lease guarantees double processing:
	// the row becomes reclaimable while the first worker is still running it.
	// Clamp rather than fail — a running service with a mis-tuned env var is
	// better off degraded than refusing to boot.
	if c.HandlerTimeout+statusUpdateGrace > c.Lease {
		c.Lease = c.HandlerTimeout + statusUpdateGrace
	}
}

// Worker claims and dispatches outbox messages.
// Safe for concurrent calls — each Claim is an independent batch.
type Worker struct {
	repo     Repository
	registry *Registry
	cfg      WorkerConfig
}

// NewWorker creates a Worker.
// Lease is raised to HandlerTimeout+statusUpdateGrace when the supplied config
// would let a handler outlive its own lease.
func NewWorker(repo Repository, registry *Registry, cfg WorkerConfig) *Worker {
	requested := cfg.Lease
	cfg.applyDefaults()
	if requested > 0 && cfg.Lease != requested {
		logger.Log().Warnf(
			"[outbox][NewWorker][msg: lease raised to cover handler timeout][requested: %s][effective: %s][handler_timeout: %s]",
			requested, cfg.Lease, cfg.HandlerTimeout,
		)
	}
	return &Worker{
		repo:     repo,
		registry: registry,
		cfg:      cfg,
	}
}

// Claim takes ownership of at most max due messages without processing them.
//
// The caller states how many it can handle, and that is the only bound: a
// claimed message starts burning its lease immediately, so claiming more than
// the caller can dispatch would let the tail of the batch expire while it is
// still waiting. A separate batch limit could only ever be smaller than that or
// irrelevant, so there isn't one.
//
// Returns (nil, nil) when max <= 0 or nothing is due.
func (w *Worker) Claim(ctx context.Context, max int) ([]Message, error) {
	if max <= 0 {
		return nil, nil
	}

	msgs, err := w.repo.ClaimDue(ctx, w.cfg.WorkerID, w.cfg.Lease, max)
	if err != nil {
		return nil, fmt.Errorf("[outbox][Worker:Claim]%w", err)
	}
	return msgs, nil
}

// ProcessOne dispatches an already-claimed Message to its registered handler.
// The message must already be in 'processing' status with an active lease
// (returned by Claim). Called by Pool workers.
func (w *Worker) ProcessOne(ctx context.Context, msg Message) {
	w.process(ctx, msg)
}

// DeadLetter moves an already-claimed message straight to 'dead' without
// dispatching it. Used by the Pool when a handler panics: a panic is a
// deterministic code defect, so retrying it only produces an endless reclaim
// loop that never surfaces through the OnDead hook.
func (w *Worker) DeadLetter(ctx context.Context, msg Message, reason string) {
	w.deadLetter(ctx, w.msgLogger(ctx, msg), msg, reason)
}

// msgLogger returns a logger pre-populated with the message's identifying fields.
func (w *Worker) msgLogger(ctx context.Context, msg Message) *zap.SugaredLogger {
	return logger.LogCtx(ctx).With(
		zap.Int64("outbox_id", msg.ID),
		zap.String("event_type", msg.EventType),
		zap.String("dedupe_key", msg.DedupeKey),
		zap.String("aggregate_type", msg.AggregateType),
		zap.String("aggregate_id", msg.AggregateID),
		zap.Int("attempt", msg.Attempts+1),
		zap.Int("max_attempts", msg.MaxAttempts),
		zap.String("worker_id", w.cfg.WorkerID),
	)
}

// withStatusCtx runs fn with the context used for terminal status updates. It
// drops cancellation so that work already performed by the handler is still
// recorded when the pool is shutting down — otherwise every in-flight message
// that succeeded during a SIGTERM would be redelivered on the next boot. The
// bound matches the lease head-room reserved by applyDefaults.
func (w *Worker) withStatusCtx(ctx context.Context, fn func(context.Context) error) error {
	statusCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusUpdateGrace)
	defer cancel()
	return fn(statusCtx)
}

// logStatusErr logs a failed status update, distinguishing lease loss (benign,
// the new owner is responsible) from a real persistence failure.
func (w *Worker) logStatusErr(log *zap.SugaredLogger, msg Message, err error) {
	if errors.Is(err, ErrLeaseLost) {
		if fn := w.cfg.Hooks.OnLeaseLost; fn != nil {
			fn(msg.EventType)
		}
		log.Warnf("[outbox][Worker:process][msg: lease lost, status update discarded]%s", err)
		return
	}
	log.Errorf("[outbox][Worker:process]%s", err)
}

// reportClaimError fires the OnClaimError hook. Lives on Worker because that is
// where Hooks are configured, but is called by the Pool's relay.
func (w *Worker) reportClaimError(err error) {
	if fn := w.cfg.Hooks.OnClaimError; fn != nil {
		fn(err)
	}
}

// process dispatches one message and updates its outbox row according to the result.
// Per-message errors are logged and never returned.
func (w *Worker) process(ctx context.Context, msg Message) {
	log := w.msgLogger(ctx, msg)

	// A message whose budget was consumed by stale-lease reclaims never reaches
	// the post-handler budget check below, because a worker that dies mid-handler
	// never returns an error. Catch it here so poison messages terminate.
	if msg.Attempts >= msg.MaxAttempts {
		w.deadLetter(ctx, log, msg, "retry budget exhausted before dispatch")
		return
	}

	// Apply per-message deadline to the handler only. Status updates use a
	// non-cancellable context so they still complete even if the handler timed
	// out or the pool is shutting down.
	handlerCtx, cancel := context.WithTimeout(ctx, w.cfg.HandlerTimeout)
	defer cancel()

	start := time.Now()
	handlerErr := w.registry.Dispatch(handlerCtx, msg)
	elapsed := time.Since(start)

	if fn := w.cfg.Hooks.OnHandlerDuration; fn != nil {
		fn(msg.EventType, elapsed)
	}

	if handlerErr == nil {
		err := w.withStatusCtx(ctx, func(c context.Context) error {
			return w.repo.MarkDelivered(c, msg.ID, w.cfg.WorkerID)
		})
		if err != nil {
			w.logStatusErr(log, msg, err)
			return
		}
		if fn := w.cfg.Hooks.OnDelivered; fn != nil {
			fn(msg.EventType)
		}
		log.Infof("[outbox][Worker:process][delivered][handler_duration: %s]", elapsed)
		return
	}

	// Our own shutdown is not a delivery failure. Recording it would burn a
	// retry attempt and push the message a full backoff away; leaving the row
	// in 'processing' costs only a lease expiry and keeps the budget intact —
	// the same path a hard crash already takes. A HandlerTimeout expiry is
	// unaffected: that cancels handlerCtx, not ctx.
	if ctx.Err() != nil {
		log.Warnf("[outbox][Worker:process][msg: shutdown interrupted delivery, leaving message for lease expiry]%s", handlerErr)
		return
	}

	// Non-retryable: go directly to dead regardless of remaining budget.
	if errors.Is(handlerErr, ErrNonRetryable) {
		w.deadLetter(ctx, log, msg, handlerErr.Error())
		return
	}

	// Retryable: check budget.
	nextAttemptNum := msg.Attempts + 1
	if nextAttemptNum >= msg.MaxAttempts {
		w.deadLetter(ctx, log, msg, handlerErr.Error())
		return
	}

	delay := w.backoff(nextAttemptNum)
	nextAttemptAt := time.Now().UTC().Add(delay)
	err := w.withStatusCtx(ctx, func(c context.Context) error {
		return w.repo.MarkFailed(c, msg.ID, w.cfg.WorkerID, handlerErr.Error(), nextAttemptAt)
	})
	if err != nil {
		w.logStatusErr(log, msg, err)
		return
	}
	if fn := w.cfg.Hooks.OnFailed; fn != nil {
		fn(msg.EventType)
	}
	log.Warnf("[outbox][Worker:process][failed][retry_in: %s][next_attempt_at: %s]%s",
		delay, nextAttemptAt.Format(time.RFC3339), handlerErr)
}

// deadLetter moves msg to 'dead' and fires the OnDead hook.
func (w *Worker) deadLetter(ctx context.Context, log *zap.SugaredLogger, msg Message, reason string) {
	err := w.withStatusCtx(ctx, func(c context.Context) error {
		return w.repo.MarkDead(c, msg.ID, w.cfg.WorkerID, reason)
	})
	if err != nil {
		w.logStatusErr(log, msg, err)
		return
	}
	if fn := w.cfg.Hooks.OnDead; fn != nil {
		fn(msg.EventType)
	}
	log.Errorf("[outbox][Worker:process][dead]%s", reason)
}

// backoff returns the exponential delay with ±20% jitter for the given
// one-based attempt number.
func (w *Worker) backoff(attempt int) time.Duration {
	exp := float64(w.cfg.BaseBackoff) * math.Pow(2, float64(attempt-1))
	if exp > float64(w.cfg.MaxBackoff) {
		exp = float64(w.cfg.MaxBackoff)
	}
	// Add ±20% jitter to spread worker load.
	jitter := exp * 0.2 * (rand.Float64() - 0.5)
	d := time.Duration(exp + jitter)
	if d < 0 {
		d = w.cfg.BaseBackoff
	}
	return d
}

// defaultWorkerID returns "<hostname>:<pid>" for use as a stable worker lease owner.
func defaultWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s:%d", hostname, os.Getpid())
}
