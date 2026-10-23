package ledgercompensation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/whagency/core-common/outbox"
)

type fakeReverser struct {
	journalID uint64
	err       error
	calls     int
}

func (r *fakeReverser) PublishAccountTransactionRollback(_ context.Context, journalID uint64) error {
	r.calls++
	r.journalID = journalID
	return r.err
}

// TestHandler_EventTypeMatchesTheRowsResolverWrites pins the registry key. A
// mismatch silently routes nothing, and the rows pile up until the lag alert
// fires.
func TestHandler_EventTypeMatchesTheRowsResolverWrites(t *testing.T) {
	t.Parallel()

	if got := NewHandler(nil).EventType(); got != OutboxEventAccountRollback {
		t.Fatalf("expected %q, got %q", OutboxEventAccountRollback, got)
	}
	if got := reversalOutboxParams("order-1", 7).EventType; got != OutboxEventAccountRollback {
		t.Fatalf("resolver writes %q but the handler consumes %q", got, OutboxEventAccountRollback)
	}
}

// TestReversalOutboxParams_KeyedOnTheJournal pins the dedupe key. Keying on the
// aggregate would be wrong: a retried flow can produce a second posting for the
// same aggregate, and that one needs its own reversal.
func TestReversalOutboxParams_KeyedOnTheJournal(t *testing.T) {
	t.Parallel()

	first := reversalOutboxParams("order-1", 777)
	second := reversalOutboxParams("order-1", 888)

	if first.DedupeKey != "rollback:777" {
		t.Fatalf("unexpected dedupe key %q", first.DedupeKey)
	}
	if first.DedupeKey == second.DedupeKey {
		t.Fatal("two postings for one aggregate must not share a dedupe key")
	}
}

func TestHandler_Handle(t *testing.T) {
	t.Parallel()

	t.Run("forwards the journal id", func(t *testing.T) {
		t.Parallel()

		reverser := &fakeReverser{}
		payload, err := json.Marshal(ReversalPayload{JournalID: 777})
		if err != nil {
			t.Fatalf("failed to marshal payload: %v", err)
		}

		if errHandle := NewHandler(reverser).
			Handle(context.Background(), outbox.Message{Payload: payload}); errHandle != nil {
			t.Fatalf("Handle returned error: %v", errHandle)
		}
		if reverser.journalID != 777 {
			t.Fatalf("expected journal id 777, got %d", reverser.journalID)
		}
	})

	// A payload that cannot be acted on will never become actionable, so retrying
	// only burns the budget before it dead-letters anyway.
	t.Run("undeliverable payloads are non-retryable", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			payload []byte
		}{
			{name: "malformed json", payload: []byte("{not json")},
			{name: "missing journal id", payload: []byte(`{"journalId":0}`)},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				reverser := &fakeReverser{}
				err := NewHandler(reverser).
					Handle(context.Background(), outbox.Message{Payload: tt.payload})
				if err == nil {
					t.Fatal("expected an error")
				}
				if !errors.Is(err, outbox.ErrNonRetryable) {
					t.Fatalf("expected ErrNonRetryable, got %v", err)
				}
				if reverser.calls != 0 {
					t.Fatal("expected no reversal to be attempted")
				}
			})
		}
	})

	// Transport failures stay retryable: account-service being briefly unavailable
	// is not a reason to abandon a reversal.
	t.Run("transport failures stay retryable", func(t *testing.T) {
		t.Parallel()

		reverser := &fakeReverser{err: errors.New("queue unavailable")}
		payload, _ := json.Marshal(ReversalPayload{JournalID: 777})

		err := NewHandler(reverser).Handle(context.Background(), outbox.Message{Payload: payload})
		if err == nil {
			t.Fatal("expected an error")
		}
		if errors.Is(err, outbox.ErrNonRetryable) {
			t.Fatalf("expected a retryable error, got %v", err)
		}
	})
}
