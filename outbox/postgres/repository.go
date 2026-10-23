// Package postgres provides a GORM/PostgreSQL implementation of outbox.Repository.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	commondb "github.com/whagency/core-common/db"
	"github.com/whagency/core-common/outbox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultMaxAttempts = 10

// outboxModel maps to the outbox_messages table.
type outboxModel struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement:false"`
	AggregateType string     `gorm:"column:aggregate_type"`
	AggregateID   string     `gorm:"column:aggregate_id"`
	EventType     string     `gorm:"column:event_type"`
	DedupeKey     string     `gorm:"column:dedupe_key"`
	Payload       []byte     `gorm:"column:payload;type:jsonb"`
	Status        string     `gorm:"column:status"`
	Attempts      int        `gorm:"column:attempts"`
	MaxAttempts   int        `gorm:"column:max_attempts"`
	NextAttemptAt time.Time  `gorm:"column:next_attempt_at"`
	LockedUntil   *time.Time `gorm:"column:locked_until"`
	LockedBy      *string    `gorm:"column:locked_by"`
	LastError     *string    `gorm:"column:last_error"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt     *time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
	DeliveredAt   *time.Time `gorm:"column:delivered_at"`
}

// oldestPendingRow is used to scan OldestPendingAge results.
type oldestPendingRow struct {
	EventType string  `gorm:"column:event_type"`
	AgeMs     float64 `gorm:"column:age_ms"`
}

// Repository is a GORM/PostgreSQL implementation of outbox.Repository.
// Construct via New; use a single instance per service.
type Repository struct {
	db        *gorm.DB
	tableName string
}

// New creates a Repository targeting tableName in the given *gorm.DB.
// tableName defaults to "outbox_messages" when empty.
func New(db *gorm.DB, tableName string) *Repository {
	if tableName == "" {
		tableName = "outbox_messages"
	}
	return &Repository{db: withoutImplicitTx(db), tableName: tableName}
}

// withoutImplicitTx disables GORM's implicit write transaction.
//
// Every write in this repository is a single statement, which PostgreSQL
// already executes atomically, so the wrapper only buys BEGIN/COMMIT round
// trips — measured at roughly 2.7x the cost of the statement itself.
//
// Inside a business transaction GORM wraps writes in a SAVEPOINT instead, which
// is worse than merely pointless here: a failed outbox insert must abort the
// business transaction, never be isolated from it. Isolating it would commit
// the business change while silently dropping the side effect it promised —
// exactly the failure the outbox exists to prevent.
func withoutImplicitTx(db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{SkipDefaultTransaction: true})
}

// WithTxFromContext returns a Repository joined to the GORM transaction stored
// in ctx by common/db transaction manager. Falls back to the base connection.
func (r *Repository) WithTxFromContext(ctx context.Context) outbox.Repository {
	if tx := commondb.GetTxFromContext(ctx); tx != nil {
		return &Repository{db: withoutImplicitTx(tx), tableName: r.tableName}
	}
	return r
}

// Create inserts a new outbox message.
// A duplicate dedupe_key is silently ignored (ON CONFLICT DO NOTHING).
func (r *Repository) Create(ctx context.Context, p outbox.CreateParams) error {
	payloadBytes, err := json.Marshal(p.Payload)
	if err != nil {
		return fmt.Errorf("[outbox][Repository:Create][err: %w]", err)
	}

	maxAttempts := p.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}

	model := outboxModel{
		AggregateType: p.AggregateType,
		AggregateID:   p.AggregateID,
		EventType:     p.EventType,
		DedupeKey:     p.DedupeKey,
		Payload:       payloadBytes,
		Status:        string(outbox.StatusPending),
		Attempts:      0,
		MaxAttempts:   maxAttempts,
	}

	// Resolve the ambient transaction here rather than relying on every caller to
	// remember WithTxFromContext. Getting that wrong writes the outbox row on the
	// base connection, so the side effect survives a rolled-back business
	// transaction — a silent, data-corrupting failure with no local symptom.
	db := r.db
	if tx := commondb.GetTxFromContext(ctx); tx != nil {
		db = withoutImplicitTx(tx)
	}

	// Omit("id") prevents GORM from sending id=0 in the INSERT — required for
	// BIGINT GENERATED ALWAYS AS IDENTITY columns which reject explicit values.
	//
	// next_attempt_at and created_at are omitted so their DEFAULT NOW() applies:
	// ClaimDue compares next_attempt_at against the database clock, so stamping
	// it from the application clock makes claim eligibility depend on skew
	// between the two. A pod running milliseconds ahead of the database would
	// silently delay its own freshly written messages by that skew.
	//
	// The conflict target is pinned to dedupe_key: a bare DO NOTHING would also
	// swallow violations of any constraint added later, silently dropping rows.
	result := db.WithContext(ctx).
		Table(r.tableName).
		Omit("id", "next_attempt_at", "created_at").
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "dedupe_key"}},
			DoNothing: true,
		}).
		Create(&model)

	if result.Error != nil {
		return fmt.Errorf("[outbox][Repository:Create][err: %w]", result.Error)
	}
	return nil
}

// ClaimDue atomically claims up to batch ready messages and returns them.
// Ready means:
//   - status IN ('pending','failed') AND next_attempt_at <= NOW(), or
//   - status = 'processing' AND locked_until < NOW() (stale lease recovery)
func (r *Repository) ClaimDue(ctx context.Context, workerID string, lease time.Duration, batch int) ([]outbox.Message, error) {
	// make_interval takes a float so sub-second leases survive; int64(seconds)
	// would truncate a 500ms lease to 0 and make every row instantly reclaimable.
	leaseSeconds := lease.Seconds()

	// The stale branch increments attempts so a message that repeatedly kills its
	// worker consumes its retry budget instead of looping forever. In an UPDATE,
	// SET expressions see the pre-update row, so `status` here is the old value.
	claimSQL := fmt.Sprintf(`
		UPDATE %s
		SET
			status       = 'processing',
			attempts     = CASE WHEN status = 'processing' THEN attempts + 1 ELSE attempts END,
			locked_until = NOW() + make_interval(secs => ?),
			locked_by    = ?,
			updated_at   = NOW()
		WHERE id IN (
			SELECT id
			FROM %s
			WHERE (status IN ('pending', 'failed') AND next_attempt_at <= NOW())
			   OR (status = 'processing' AND locked_until < NOW())
			ORDER BY next_attempt_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *
	`, r.tableName, r.tableName)

	var models []outboxModel
	result := r.db.WithContext(ctx).Raw(claimSQL, leaseSeconds, workerID, batch).Scan(&models)
	if result.Error != nil {
		return nil, fmt.Errorf("[outbox][Repository:ClaimDue][err: %w]", result.Error)
	}

	msgs := make([]outbox.Message, len(models))
	for i, m := range models {
		msgs[i] = toMessage(m)
	}
	return msgs, nil
}

// ownedByWorker scopes an update to a message this worker still holds a live
// lease on. Without it a worker whose lease expired — and whose row was already
// reclaimed by another instance — would overwrite the new owner's state.
func (r *Repository) ownedByWorker(ctx context.Context, id int64, workerID string) *gorm.DB {
	return r.db.WithContext(ctx).
		Table(r.tableName).
		Where("id = ? AND locked_by = ? AND locked_until > NOW()", id, workerID)
}

// applyStatusUpdate runs a lease-scoped update and reports lease loss when the
// row was not matched, so a zero-row update is never mistaken for success.
func applyStatusUpdate(q *gorm.DB, op string, values map[string]any) error {
	result := q.Updates(values)
	if result.Error != nil {
		return fmt.Errorf("[outbox][Repository:%s][err: %w]", op, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("[outbox][Repository:%s][err: %w]", op, outbox.ErrLeaseLost)
	}
	return nil
}

// MarkDelivered sets status='delivered' and stamps delivered_at.
// Returns outbox.ErrLeaseLost when workerID no longer owns the lease.
func (r *Repository) MarkDelivered(ctx context.Context, id int64, workerID string) error {
	now := time.Now().UTC()
	return applyStatusUpdate(r.ownedByWorker(ctx, id, workerID), "MarkDelivered", map[string]any{
		"status":       string(outbox.StatusDelivered),
		"delivered_at": now,
		"locked_until": nil,
		"locked_by":    nil,
		"updated_at":   now,
	})
}

// MarkFailed increments attempts, records last_error, and schedules nextAttemptAt.
// Returns outbox.ErrLeaseLost when workerID no longer owns the lease.
func (r *Repository) MarkFailed(ctx context.Context, id int64, workerID, lastErr string, nextAttemptAt time.Time) error {
	now := time.Now().UTC()
	return applyStatusUpdate(r.ownedByWorker(ctx, id, workerID), "MarkFailed", map[string]any{
		"status":          string(outbox.StatusFailed),
		"attempts":        gorm.Expr("attempts + 1"),
		"last_error":      lastErr,
		"next_attempt_at": nextAttemptAt,
		"locked_until":    nil,
		"locked_by":       nil,
		"updated_at":      now,
	})
}

// MarkDead moves the message to 'dead' status with no further retry.
// Returns outbox.ErrLeaseLost when workerID no longer owns the lease.
func (r *Repository) MarkDead(ctx context.Context, id int64, workerID, lastErr string) error {
	now := time.Now().UTC()
	return applyStatusUpdate(r.ownedByWorker(ctx, id, workerID), "MarkDead", map[string]any{
		"status":       string(outbox.StatusDead),
		"attempts":     gorm.Expr("attempts + 1"),
		"last_error":   lastErr,
		"locked_until": nil,
		"locked_by":    nil,
		"updated_at":   now,
	})
}

// OldestPendingAge returns the age of the oldest pending/failed message per event_type.
func (r *Repository) OldestPendingAge(ctx context.Context) (map[string]time.Duration, error) {
	lagSQL := fmt.Sprintf(`
		SELECT event_type,
		       EXTRACT(EPOCH FROM (NOW() - MIN(created_at))) * 1000 AS age_ms
		FROM %s
		WHERE status IN ('pending', 'failed')
		GROUP BY event_type
	`, r.tableName)

	var rows []oldestPendingRow
	result := r.db.WithContext(ctx).Raw(lagSQL).Scan(&rows)
	if result.Error != nil {
		return nil, fmt.Errorf("[outbox][Repository:OldestPendingAge][err: %w]", result.Error)
	}

	ages := make(map[string]time.Duration, len(rows))
	for _, row := range rows {
		ages[row.EventType] = time.Duration(row.AgeMs) * time.Millisecond
	}
	return ages, nil
}

// pruneBatchSize bounds a single DELETE so GC never holds locks on a large
// table for long. Prune loops until the table is clean or ctx is cancelled.
const pruneBatchSize = 5000

// Prune deletes delivered and dead messages past their respective retentions.
// Returns the number of rows deleted.
//
// Retention runs from delivered_at for delivered rows — that is the semantic
// operators expect, and it lets the partial delivered GC index serve the scan.
// Dead rows have no delivery timestamp, so they age from created_at against the
// partial dead index.
//
// Safe to run alongside active workers: neither 'delivered' nor 'dead' is a
// claimable status, so no in-flight message can be removed.
//
// SKIP LOCKED matters for a different reason: the platform cron has no leader
// election, so every pod runs this job. Without it, concurrent prunes block on
// each other's row locks and then find the rows already gone — N pods doing one
// pod's work, slowly. With it they partition the batches and pruning gets
// faster with pod count instead of slower.
func (r *Repository) Prune(ctx context.Context, deliveredOlderThan, deadOlderThan time.Duration) (int64, error) {
	now := time.Now().UTC()
	deliveredCutoff := now.Add(-deliveredOlderThan)
	deadCutoff := now.Add(-deadOlderThan)

	pruneSQL := fmt.Sprintf(`
		DELETE FROM %s
		WHERE id IN (
			SELECT id
			FROM %s
			WHERE (status = 'delivered' AND delivered_at < ?)
			   OR (status = 'dead' AND created_at < ?)
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
	`, r.tableName, r.tableName)

	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, fmt.Errorf("[outbox][Repository:Prune][err: %w]", err)
		}
		result := r.db.WithContext(ctx).Exec(pruneSQL, deliveredCutoff, deadCutoff, pruneBatchSize)
		if result.Error != nil {
			return total, fmt.Errorf("[outbox][Repository:Prune][err: %w]", result.Error)
		}
		total += result.RowsAffected
		if result.RowsAffected < pruneBatchSize {
			return total, nil
		}
	}
}

// toMessage converts the internal GORM model to the public outbox.Message type.
func toMessage(m outboxModel) outbox.Message {
	return outbox.Message{
		ID:            m.ID,
		AggregateType: m.AggregateType,
		AggregateID:   m.AggregateID,
		EventType:     m.EventType,
		DedupeKey:     m.DedupeKey,
		Payload:       m.Payload,
		Status:        outbox.Status(m.Status),
		Attempts:      m.Attempts,
		MaxAttempts:   m.MaxAttempts,
		NextAttemptAt: m.NextAttemptAt,
		LockedUntil:   m.LockedUntil,
		LockedBy:      m.LockedBy,
		LastError:     m.LastError,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
		DeliveredAt:   m.DeliveredAt,
	}
}
