package outbox

import (
	"context"
	"errors"
	"fmt"
)

// ErrNonRetryable is the sentinel for non-retryable outbox handler errors.
// Use MarkNonRetryable to wrap errors rather than comparing directly.
var ErrNonRetryable = errors.New("outbox: non-retryable")

// ErrLeaseLost is returned by Repository status updates when the calling worker
// no longer holds the lease on the message — another worker reclaimed it after
// the lease expired. The status update is discarded rather than overwriting the
// new owner's state; the new owner is responsible for the outcome.
var ErrLeaseLost = errors.New("outbox: lease lost")

// NonRetryableError wraps an error with the non-retryable marker.
// The worker moves the message directly to 'dead' status when it sees this.
type NonRetryableError struct {
	Err error
}

// Error implements the error interface.
func (e NonRetryableError) Error() string {
	if e.Err == nil {
		return ErrNonRetryable.Error()
	}
	return e.Err.Error()
}

// Unwrap enables errors.Is and errors.As to unwrap through this type.
func (e NonRetryableError) Unwrap() error {
	return e.Err
}

// Is allows errors.Is(err, ErrNonRetryable) to match NonRetryableError values.
func (e NonRetryableError) Is(target error) bool {
	return target == ErrNonRetryable
}

// MarkNonRetryable wraps err with the non-retryable marker.
// The worker dead-letters the message immediately without consuming the retry budget.
//
// Usage in handlers:
//
//	if err := json.Unmarshal(msg.Payload, &p); err != nil {
//	    return outbox.MarkNonRetryable(fmt.Errorf("malformed payload: %w", err))
//	}
func MarkNonRetryable(err error) error {
	if err == nil {
		return nil
	}
	return NonRetryableError{Err: err}
}

// ErrNoHandler is returned by the Registry when no handler is registered for
// an event_type. It wraps ErrNonRetryable so unrecognised messages are
// dead-lettered immediately rather than retried indefinitely.
var ErrNoHandler = NonRetryableError{Err: errors.New("no handler registered")}

// Handler processes a single outbox message for one event_type.
type Handler interface {
	// EventType returns the event_type string this handler handles.
	EventType() string

	// Handle processes msg. Returning nil marks the message as delivered.
	// Returning a plain error schedules a retry.
	// Returning MarkNonRetryable(...) or any error wrapping ErrNonRetryable
	// moves the message directly to dead status.
	Handle(ctx context.Context, msg Message) error
}

// Registry maps event_type strings to Handlers.
//
// Register must be called only during startup, before any Worker or Pool is
// running. Dispatch reads the map from every pool worker concurrently, and the
// map is deliberately unsynchronised: registering a handler after the pool has
// started is a data race, not a supported way to add handlers at runtime.
type Registry struct {
	handlers map[string]Handler
}

// NewRegistry creates an empty handler registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register adds h to the registry.
// Panics if a handler for h.EventType() is already registered.
func (r *Registry) Register(h Handler) {
	if _, exists := r.handlers[h.EventType()]; exists {
		panic(fmt.Sprintf("[outbox][Registry:Register][err: duplicate handler for event_type=%q]", h.EventType()))
	}
	r.handlers[h.EventType()] = h
}

// Dispatch looks up and calls the handler for msg.EventType.
// Returns ErrNoHandler (which wraps ErrNonRetryable) when no handler is registered.
func (r *Registry) Dispatch(ctx context.Context, msg Message) error {
	h, ok := r.handlers[msg.EventType]
	if !ok {
		return fmt.Errorf("[outbox][Registry:Dispatch][err: %w][event_type: %s]", ErrNoHandler, msg.EventType)
	}
	return h.Handle(ctx, msg)
}
