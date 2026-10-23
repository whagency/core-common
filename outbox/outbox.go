// Package outbox provides the transactional outbox pattern for reliable
// post-commit side-effect delivery across app Go services.
//
// # Usage contract
//
//  1. Inside a business transaction, call Repository.Create to persist the
//     side-effect alongside the business state change. Create joins the ambient
//     transaction from ctx itself. If the transaction rolls back the outbox row
//     does not exist — no side-effect, no orphan message.
//
//  2. Register a Handler per event_type in a Registry, then create a Worker
//     backed by the Repository. Wrap the Worker in a Pool and register its
//     Run method in the application errgroup for graceful shutdown.
//
//  3. The Pool runs an internal relay goroutine that claims due messages via
//     ClaimDue (FOR UPDATE SKIP LOCKED) and fans them out to Workers goroutines.
//     Each handler runs under a per-message deadline (WorkerConfig.HandlerTimeout,
//     default 5 s) so one stuck call cannot occupy a worker indefinitely.
//
//  4. Handlers must be idempotent — the outbox guarantees at-least-once
//     delivery. A handler is called again whenever the service crashes mid-
//     processing and the lease expires, or when MarkDelivered fails transiently.
//
//  5. Retryable failures are scheduled with exponential backoff. Non-retryable
//     failures (use MarkNonRetryable), budget-exhausted messages and handler
//     panics go to status="dead" and require operator attention.
//
//  6. Register Repository.Prune on a cron to bound table growth, and
//     Repository.OldestPendingAge as a lag gauge — counters alone cannot
//     distinguish a stalled relay from an idle service.
package outbox

import (
	"context"
	"time"
)

// Status represents the delivery state of an outbox message.
type Status string

const (
	// StatusPending means the message is waiting to be processed.
	StatusPending Status = "pending"

	// StatusProcessing means a worker has claimed the message and holds the lease.
	StatusProcessing Status = "processing"

	// StatusDelivered means the handler returned nil; the side-effect was applied.
	StatusDelivered Status = "delivered"

	// StatusFailed means the last delivery attempt failed; retry is scheduled.
	StatusFailed Status = "failed"

	// StatusDead means the message exhausted its retry budget or was non-retryable.
	StatusDead Status = "dead"
)

// Message is a single outbox record returned by the repository after a claim.
type Message struct {
	ID            int64
	AggregateType string
	AggregateID   string
	EventType     string
	DedupeKey     string
	Payload       []byte // raw JSON
	Status        Status
	Attempts      int
	MaxAttempts   int
	NextAttemptAt time.Time
	LockedUntil   *time.Time
	LockedBy      *string
	LastError     *string
	CreatedAt     time.Time
	UpdatedAt     *time.Time
	DeliveredAt   *time.Time
}

// CreateParams carries the fields needed to insert a new outbox message.
// MaxAttempts of 0 means use the repository default (10).
type CreateParams struct {
	AggregateType string
	AggregateID   string
	EventType     string
	// DedupeKey is the stable idempotency key for this side-effect.
	// A duplicate insert with the same DedupeKey is silently ignored.
	// Recommended format: "<event_namespace>:<business_key>[:<discriminator>]"
	// Examples:
	//   "counter:account-deposit-42"
	//   "push:deposit_request:42:success"
	//   "withdrawal.event:42:created"
	DedupeKey   string
	Payload     any // JSON-marshalled before storage
	MaxAttempts int // 0 → default (10)
}

// Repository is the persistence contract for outbox messages.
// All implementations must be safe for concurrent use.
type Repository interface {
	// Create inserts a new outbox message.
	// A duplicate dedupe_key is silently ignored (returns nil).
	// Must be called inside a business transaction via WithTxFromContext.
	Create(ctx context.Context, p CreateParams) error

	// ClaimDue atomically claims up to batch messages that are ready for delivery:
	//   - status IN ('pending','failed') AND next_attempt_at <= NOW(), or
	//   - status = 'processing' AND locked_until < NOW()  (stale lease recovery)
	// Claimed rows are set to 'processing' with locked_until = NOW()+lease.
	//
	// Stale-lease recovery increments attempts, so a message that repeatedly kills
	// its worker (panic, OOM, pod eviction) consumes its retry budget instead of
	// being reclaimed forever. Message.Attempts therefore reflects the value after
	// the claim, and the Worker dead-letters a message whose budget is already
	// exhausted before dispatching it.
	ClaimDue(ctx context.Context, workerID string, lease time.Duration, batch int) ([]Message, error)

	// MarkDelivered transitions the message to 'delivered' and stamps delivered_at.
	// The update only applies while workerID still holds an unexpired lease;
	// otherwise it returns ErrLeaseLost and leaves the row untouched.
	MarkDelivered(ctx context.Context, id int64, workerID string) error

	// MarkFailed sets status = 'failed', increments attempts, records last_error,
	// and sets next_attempt_at. Dead-lettering is the caller's responsibility:
	// the Worker calls MarkDead directly before this method is reached.
	// Returns ErrLeaseLost when workerID no longer holds the lease.
	MarkFailed(ctx context.Context, id int64, workerID, lastErr string, nextAttemptAt time.Time) error

	// MarkDead moves the message to 'dead' status immediately.
	// Used for non-retryable errors or budget exhaustion.
	// Returns ErrLeaseLost when workerID no longer holds the lease.
	MarkDead(ctx context.Context, id int64, workerID, lastErr string) error

	// OldestPendingAge returns the maximum age of pending/failed messages grouped
	// by event_type. Used for lag metrics and alerting.
	OldestPendingAge(ctx context.Context) (map[string]time.Duration, error)

	// Prune deletes delivered and dead messages and returns the number of rows
	// removed. Safe to call concurrently with active workers.
	//
	// The two retentions are separate because the rows serve different purposes.
	// Delivered rows are history plus the dedupe fence, and at volume they are
	// what fills the disk, so they are collected aggressively. Dead rows are the
	// incident record and the only replay source, so they must outlive the time
	// it takes an operator to notice an alert and act on it — a shared retention
	// would force a choice between disk and triage window.
	Prune(ctx context.Context, deliveredOlderThan, deadOlderThan time.Duration) (int64, error)

	// WithTxFromContext returns a Repository that joins the GORM transaction stored
	// in ctx by common/db.GetTxFromContext. If no transaction is found, returns
	// the receiver unchanged (uses the base connection).
	WithTxFromContext(ctx context.Context) Repository
}
