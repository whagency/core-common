// Package testkit provides test helpers for the outbox package.
package testkit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/whagency/core-common/outbox"
)

// FakeRepository is a thread-safe in-memory implementation of outbox.Repository.
// Use in unit tests to avoid database dependencies.
//
// Example:
//
//	repo := testkit.NewFakeRepository()
//	worker := outbox.NewWorker(repo, registry, outbox.WorkerConfig{})
type FakeRepository struct {
	mu       sync.Mutex
	messages map[int64]*outbox.Message
	nextID   int64

	// FailMarkDelivered, when set, is consulted before MarkDelivered applies its
	// update; a non-nil result is returned and the row is left untouched. Use it
	// to exercise the redelivery path taken when the status write fails after a
	// handler has already succeeded. Set before the worker starts.
	FailMarkDelivered func(id int64) error
}

// NewFakeRepository creates an empty FakeRepository.
func NewFakeRepository() *FakeRepository {
	return &FakeRepository{
		messages: make(map[int64]*outbox.Message),
	}
}

// ExpireLease forces the lease on id to have already expired, simulating a
// worker that died mid-processing. The message becomes reclaimable on the next
// ClaimDue. No-op when id is unknown.
func (r *FakeRepository) ExpireLease(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.messages[id]
	if !ok {
		return
	}
	past := time.Now().UTC().Add(-time.Second)
	m.LockedUntil = &past
}

// MakeDue brings a scheduled retry forward so the message is claimable now,
// letting tests exercise multi-attempt behaviour without sleeping through the
// backoff. No-op when id is unknown.
func (r *FakeRepository) MakeDue(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.messages[id]
	if !ok {
		return
	}
	m.NextAttemptAt = time.Now().UTC().Add(-time.Second)
}

// holdsLease reports whether workerID still owns an unexpired lease on m.
func holdsLease(m *outbox.Message, workerID string) bool {
	return m.LockedBy != nil && *m.LockedBy == workerID &&
		m.LockedUntil != nil && m.LockedUntil.After(time.Now().UTC())
}

// WithTxFromContext returns the same repository (transactions are no-ops in tests).
func (r *FakeRepository) WithTxFromContext(_ context.Context) outbox.Repository {
	return r
}

// Create inserts a new message. Duplicate dedupe_key is silently ignored.
func (r *FakeRepository) Create(_ context.Context, p outbox.CreateParams) error {
	payloadBytes, err := json.Marshal(p.Payload)
	if err != nil {
		return fmt.Errorf("[outbox][Repository:Create][err: %w]", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, m := range r.messages {
		if m.DedupeKey == p.DedupeKey {
			return nil // silently ignore duplicate
		}
	}

	maxAttempts := p.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 10
	}

	r.nextID++
	id := r.nextID
	now := time.Now().UTC()
	r.messages[id] = &outbox.Message{
		ID:            id,
		AggregateType: p.AggregateType,
		AggregateID:   p.AggregateID,
		EventType:     p.EventType,
		DedupeKey:     p.DedupeKey,
		Payload:       payloadBytes,
		Status:        outbox.StatusPending,
		Attempts:      0,
		MaxAttempts:   maxAttempts,
		NextAttemptAt: now,
		CreatedAt:     now,
	}
	return nil
}

// ClaimDue claims up to batch ready messages.
func (r *FakeRepository) ClaimDue(_ context.Context, workerID string, lease time.Duration, batch int) ([]outbox.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	lockedUntil := now.Add(lease)

	var claimed []outbox.Message
	for _, m := range r.messages {
		if len(claimed) >= batch {
			break
		}
		ready := (m.Status == outbox.StatusPending || m.Status == outbox.StatusFailed) &&
			!m.NextAttemptAt.After(now)
		stale := m.Status == outbox.StatusProcessing &&
			m.LockedUntil != nil && m.LockedUntil.Before(now)

		if !ready && !stale {
			continue
		}

		if stale {
			// Mirror the SQL claim: a stale reclaim consumes retry budget so a
			// message that keeps killing its worker cannot loop forever.
			m.Attempts++
		}
		m.Status = outbox.StatusProcessing
		m.LockedUntil = &lockedUntil
		m.LockedBy = &workerID
		updatedAt := now
		m.UpdatedAt = &updatedAt
		claimed = append(claimed, *m)
	}
	return claimed, nil
}

// MarkDelivered sets the message to delivered.
// Returns outbox.ErrLeaseLost when workerID no longer holds the lease.
func (r *FakeRepository) MarkDelivered(_ context.Context, id int64, workerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.FailMarkDelivered != nil {
		if err := r.FailMarkDelivered(id); err != nil {
			return err
		}
	}

	m, ok := r.messages[id]
	if !ok {
		return fmt.Errorf("[outbox][Repository:MarkDelivered][err: message %d not found]", id)
	}
	if !holdsLease(m, workerID) {
		return fmt.Errorf("[outbox][Repository:MarkDelivered][err: %w]", outbox.ErrLeaseLost)
	}
	now := time.Now().UTC()
	m.Status = outbox.StatusDelivered
	m.DeliveredAt = &now
	m.LockedUntil = nil
	m.LockedBy = nil
	m.UpdatedAt = &now
	return nil
}

// MarkFailed increments attempts, records last_error, schedules retry.
// Returns outbox.ErrLeaseLost when workerID no longer holds the lease.
func (r *FakeRepository) MarkFailed(_ context.Context, id int64, workerID, lastErr string, nextAttemptAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.messages[id]
	if !ok {
		return fmt.Errorf("[outbox][Repository:MarkFailed][err: message %d not found]", id)
	}
	if !holdsLease(m, workerID) {
		return fmt.Errorf("[outbox][Repository:MarkFailed][err: %w]", outbox.ErrLeaseLost)
	}
	now := time.Now().UTC()
	m.Status = outbox.StatusFailed
	m.Attempts++
	m.LastError = &lastErr
	m.NextAttemptAt = nextAttemptAt
	m.LockedUntil = nil
	m.LockedBy = nil
	m.UpdatedAt = &now
	return nil
}

// MarkDead moves the message to dead status.
// Returns outbox.ErrLeaseLost when workerID no longer holds the lease.
func (r *FakeRepository) MarkDead(_ context.Context, id int64, workerID, lastErr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.messages[id]
	if !ok {
		return fmt.Errorf("[outbox][Repository:MarkDead][err: message %d not found]", id)
	}
	if !holdsLease(m, workerID) {
		return fmt.Errorf("[outbox][Repository:MarkDead][err: %w]", outbox.ErrLeaseLost)
	}
	now := time.Now().UTC()
	m.Status = outbox.StatusDead
	m.Attempts++
	m.LastError = &lastErr
	m.LockedUntil = nil
	m.LockedBy = nil
	m.UpdatedAt = &now
	return nil
}

// OldestPendingAge returns age of the oldest pending/failed message per event_type.
func (r *FakeRepository) OldestPendingAge(_ context.Context) (map[string]time.Duration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	oldest := make(map[string]time.Time)
	now := time.Now().UTC()

	for _, m := range r.messages {
		if m.Status != outbox.StatusPending && m.Status != outbox.StatusFailed {
			continue
		}
		if t, exists := oldest[m.EventType]; !exists || m.CreatedAt.Before(t) {
			oldest[m.EventType] = m.CreatedAt
		}
	}

	ages := make(map[string]time.Duration, len(oldest))
	for eventType, t := range oldest {
		ages[eventType] = now.Sub(t)
	}
	return ages, nil
}

// Prune removes delivered and dead messages past their respective retentions.
func (r *FakeRepository) Prune(_ context.Context, deliveredOlderThan, deadOlderThan time.Duration) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	deliveredCutoff := now.Add(-deliveredOlderThan)
	deadCutoff := now.Add(-deadOlderThan)

	var deleted int64
	for id, m := range r.messages {
		// Mirror the SQL implementation: delivered rows age from delivered_at,
		// dead rows from created_at.
		var expired bool
		switch m.Status {
		case outbox.StatusDelivered:
			expired = m.DeliveredAt != nil && m.DeliveredAt.Before(deliveredCutoff)
		case outbox.StatusDead:
			expired = m.CreatedAt.Before(deadCutoff)
		}
		if expired {
			delete(r.messages, id)
			deleted++
		}
	}
	return deleted, nil
}

// All returns a snapshot of all messages (useful for assertions in tests).
func (r *FakeRepository) All() []outbox.Message {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]outbox.Message, 0, len(r.messages))
	for _, m := range r.messages {
		result = append(result, *m)
	}
	return result
}

// CountByStatus returns a count of messages grouped by status (useful for assertions).
func (r *FakeRepository) CountByStatus() map[outbox.Status]int {
	r.mu.Lock()
	defer r.mu.Unlock()

	counts := make(map[outbox.Status]int)
	for _, m := range r.messages {
		counts[m.Status]++
	}
	return counts
}
