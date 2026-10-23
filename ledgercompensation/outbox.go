package ledgercompensation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/whagency/core-common/outbox"
)

// OutboxEventAccountRollback is the event_type this package writes and consumes.
//
// It is fixed here rather than chosen per service so every adopter's reversal
// rows are recognisable in one query across the platform.
const OutboxEventAccountRollback = "account.transaction.rollback"

// OutboxAggregateType is the aggregate_type on reversal rows.
const OutboxAggregateType = "ledger_compensation"

// ReversalPayload is the JSON payload of a reversal outbox row.
type ReversalPayload struct {
	JournalID uint64 `json:"journalId"`
}

// Reverser delivers a reversal request to account-service. Services pass their
// own queue publisher; the signature matches the existing
// PublishAccountTransactionRollback on both current adopters.
type Reverser interface {
	PublishAccountTransactionRollback(ctx context.Context, journalID uint64) error
}

// reversalOutboxParams builds the outbox row carrying a reversal request.
//
// The journal id is the dedupe key because a journal is reversed at most once.
// Keying on the aggregate would be wrong: a retried business flow can produce a
// second posting for the same aggregate, and that one needs its own reversal.
func reversalOutboxParams(aggregateID string, journalID uint64) outbox.CreateParams {
	return outbox.CreateParams{
		AggregateType: OutboxAggregateType,
		AggregateID:   aggregateID,
		EventType:     OutboxEventAccountRollback,
		DedupeKey:     fmt.Sprintf("rollback:%d", journalID),
		Payload:       ReversalPayload{JournalID: journalID},
	}
}

// Handler delivers reversal rows. Register it in the service's outbox registry.
type Handler struct {
	reverser Reverser
}

// NewHandler creates the outbox handler for reversal rows.
func NewHandler(reverser Reverser) *Handler {
	return &Handler{reverser: reverser}
}

// EventType returns the event_type this handler processes.
func (h *Handler) EventType() string {
	return OutboxEventAccountRollback
}

// Handle requests the reversal. Redelivery is safe: account-service's
// RollbackTransaction returns early when the journal is already reversed.
func (h *Handler) Handle(ctx context.Context, msg outbox.Message) error {
	var p ReversalPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		return outbox.MarkNonRetryable(fmt.Errorf("[ledgercompensation][Handler:Handle][err: %w]", err))
	}
	if p.JournalID == 0 {
		// Nothing to reverse, and forwarding it would have account-service look up
		// a journal that cannot exist. Retrying cannot change either fact.
		return outbox.MarkNonRetryable(fmt.Errorf("[ledgercompensation][Handler:Handle][err: journal id is required]"))
	}

	return h.reverser.PublishAccountTransactionRollback(ctx, p.JournalID)
}
