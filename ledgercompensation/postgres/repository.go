package postgres

import (
	"context"
	"fmt"
	"time"

	commondb "github.com/whagency/core-common/db"
	"github.com/whagency/core-common/ledgercompensation"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/account"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type compensationModel struct {
	ID             uint64    `gorm:"column:id;primaryKey"`
	AggregateType  string    `gorm:"column:aggregate_type"`
	AggregateID    string    `gorm:"column:aggregate_id"`
	BizType        uint8     `gorm:"column:biz_type"`
	IdempotencyKey string    `gorm:"column:idempotency_key"`
	JournalID      *uint64   `gorm:"column:journal_id"`
	Status         uint8     `gorm:"column:status"`
	ResolvedReason *string   `gorm:"column:resolved_reason"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

// TableName specifies the DB table for ledger compensation rows.
func (compensationModel) TableName() string {
	return "ledger_compensations"
}

// Repository persists the ledger dead-man's switch.
//
// It holds two handles rather than one. base is the pooled connection and never
// changes; db is what WithTxFromContext rebinds. The write methods that must
// outlive a rolled-back business transaction use base explicitly, so a caller
// cannot accidentally enlist them in the transaction they are guarding against.
type Repository struct {
	base *gorm.DB
	db   *gorm.DB
}

// New builds the repository backed by GORM.
func New(db *gorm.DB) *Repository {
	return &Repository{base: db, db: db}
}

// WithTxFromContext rebinds the repository DB handle to the transaction from the
// context when present. It deliberately leaves base alone.
func (r Repository) WithTxFromContext(ctx context.Context) ledgercompensation.Repository {
	if tx := commondb.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// RecordIntent inserts a pending switch before the ledger call is made.
//
// It writes on the base connection under a non-cancellable context. Both matter:
// a row written inside the business transaction would be erased by the very
// rollback it exists to detect, and a row abandoned because the caller's HTTP
// context was cancelled would be missing in exactly the case that cancellation
// causes — the client hanging up mid-payment.
//
// A conflict on the idempotency key is not an error. Services derive the key from
// the aggregate, so a retried business flow legitimately reaches the same switch;
// the existing row already guards the posting.
func (r Repository) RecordIntent(ctx context.Context, params ledgercompensation.CreateParams) error {
	logger.LogCtx(ctx).Debugf(
		"[ledgercompensation][Repository:RecordIntent][aggregate:%s/%s]",
		params.AggregateType, params.AggregateID,
	)

	writeCtx := context.WithoutCancel(ctx)
	now := time.Now().UTC()

	row := &compensationModel{
		AggregateType:  params.AggregateType,
		AggregateID:    params.AggregateID,
		BizType:        uint8(params.BizType),
		IdempotencyKey: params.IdempotencyKey,
		Status:         ledgercompensation.StatusPending.Uint8(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := r.base.WithContext(writeCtx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "idempotency_key"}}, DoNothing: true}).
		Create(row).Error; err != nil {
		return fmt.Errorf("[ledgercompensation][Repository:RecordIntent][err: %w]", err)
	}

	return nil
}

// RecordPosted attaches the journal id the ledger call returned.
//
// Only pending rows advance: once a row has been settled, voided or sent for
// reversal, the cron owns it and a late writer must not drag it back.
func (r Repository) RecordPosted(ctx context.Context, idempotencyKey string, journalID uint64) error {
	logger.LogCtx(ctx).Debugf(
		"[ledgercompensation][Repository:RecordPosted][key:%s][journalID:%d]",
		idempotencyKey, journalID,
	)

	writeCtx := context.WithoutCancel(ctx)

	res := r.base.WithContext(writeCtx).
		Model(&compensationModel{}).
		Where("idempotency_key = ? AND status = ?", idempotencyKey, ledgercompensation.StatusPending.Uint8()).
		Updates(map[string]any{
			"journal_id": journalID,
			"status":     ledgercompensation.StatusPosted.Uint8(),
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return fmt.Errorf("[ledgercompensation][Repository:RecordPosted][err: %w]", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf(
			"[ledgercompensation][Repository:RecordPosted][key:%s][err: no pending compensation row to advance]",
			idempotencyKey,
		)
	}

	return nil
}

// MarkSettled clears the switch. It joins the ambient transaction on purpose: if
// that transaction rolls back the row stays open and the cron reverses the
// posting, which is exactly the behaviour being bought.
func (r Repository) MarkSettled(ctx context.Context, idempotencyKey string) error {
	logger.LogCtx(ctx).Debugf("[ledgercompensation][Repository:MarkSettled][key:%s]", idempotencyKey)

	res := r.db.WithContext(ctx).
		Model(&compensationModel{}).
		Where("idempotency_key = ? AND status IN ?", idempotencyKey, openStatuses()).
		Updates(map[string]any{
			"status":     ledgercompensation.StatusSettled.Uint8(),
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return fmt.Errorf("[ledgercompensation][Repository:MarkSettled][err: %w]", res.Error)
	}

	return nil
}

// ClaimOrphans locks unresolved rows that have been open longer than the grace
// period. SKIP LOCKED keeps concurrent cron runs on different replicas from
// fighting over the same rows.
func (r Repository) ClaimOrphans(
	ctx context.Context,
	olderThan time.Time,
	limit int,
) ([]ledgercompensation.Compensation, error) {
	logger.LogCtx(ctx).Debugf("[ledgercompensation][Repository:ClaimOrphans][olderThan:%s][limit:%d]", olderThan, limit)

	var rows []compensationModel
	if err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("status IN ? AND updated_at <= ?", openStatuses(), olderThan.UTC()).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("[ledgercompensation][Repository:ClaimOrphans][err: %w]", err)
	}

	out := make([]ledgercompensation.Compensation, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toDomain())
	}

	return out, nil
}

// Resolve writes a terminal status and the operator-facing reason.
func (r Repository) Resolve(
	ctx context.Context,
	id uint64,
	status ledgercompensation.Status,
	reason string,
) error {
	logger.LogCtx(ctx).Debugf("[ledgercompensation][Repository:Resolve][id:%d][status:%s]", id, status.String())

	if err := r.db.WithContext(ctx).
		Model(&compensationModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":          status.Uint8(),
			"resolved_reason": reason,
			"updated_at":      time.Now().UTC(),
		}).Error; err != nil {
		return fmt.Errorf("[ledgercompensation][Repository:Resolve][err: %w]", err)
	}

	return nil
}

// AttachJournalID records a journal id the cron recovered from the ledger for a
// row whose own RecordPosted never landed.
func (r Repository) AttachJournalID(ctx context.Context, id uint64, journalID uint64) error {
	logger.LogCtx(ctx).Debugf("[ledgercompensation][Repository:AttachJournalID][id:%d][journalID:%d]", id, journalID)

	if err := r.db.WithContext(ctx).
		Model(&compensationModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"journal_id": journalID,
			"status":     ledgercompensation.StatusPosted.Uint8(),
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
		return fmt.Errorf("[ledgercompensation][Repository:AttachJournalID][err: %w]", err)
	}

	return nil
}

// CountByStatus returns the row count per status for the gauge.
func (r Repository) CountByStatus(ctx context.Context) (map[ledgercompensation.Status]int64, error) {

	var rows []struct {
		Status uint8 `gorm:"column:status"`
		Total  int64 `gorm:"column:total"`
	}
	if err := r.db.WithContext(ctx).
		Model(&compensationModel{}).
		Select("status, COUNT(*) AS total").
		Group("status").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("[ledgercompensation][Repository:CountByStatus][err: %w]", err)
	}

	out := make(map[ledgercompensation.Status]int64, len(rows))
	for i := range rows {
		out[ledgercompensation.Status(rows[i].Status)] = rows[i].Total
	}

	return out, nil
}

// Prune deletes resolved rows older than the retention window. Open rows are
// never pruned: an unresolved switch is unfinished business, however old.
func (r Repository) Prune(ctx context.Context, olderThan time.Time) (int64, error) {

	res := r.db.WithContext(ctx).
		Where("status IN ? AND updated_at < ?", resolvedStatuses(), olderThan.UTC()).
		Delete(&compensationModel{})
	if res.Error != nil {
		return 0, fmt.Errorf("[ledgercompensation][Repository:Prune][err: %w]", res.Error)
	}

	return res.RowsAffected, nil
}

// openStatuses lists the statuses that still need resolving.
//
// The element type is int rather than uint8 on purpose: []uint8 is []byte in Go,
// and GORM binds a byte slice as a single bytea parameter rather than expanding
// it into an IN list, producing a syntax error at query time.
func openStatuses() []int {
	return []int{
		int(ledgercompensation.StatusPending),
		int(ledgercompensation.StatusPosted),
	}
}

// resolvedStatuses lists the terminal statuses eligible for pruning. See
// openStatuses for why these are int.
func resolvedStatuses() []int {
	return []int{
		int(ledgercompensation.StatusSettled),
		int(ledgercompensation.StatusVoid),
		int(ledgercompensation.StatusReversalRequested),
	}
}

// toDomain maps the persistence row to its domain model.
func (row compensationModel) toDomain() ledgercompensation.Compensation {
	return ledgercompensation.Compensation{
		ID:             row.ID,
		AggregateType:  row.AggregateType,
		AggregateID:    row.AggregateID,
		BizType:        account.JournalBizType(row.BizType),
		IdempotencyKey: row.IdempotencyKey,
		JournalID:      row.JournalID,
		Status:         ledgercompensation.Status(row.Status),
		ResolvedReason: row.ResolvedReason,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
