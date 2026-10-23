package rabbitmq

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/whagency/core-common/queue"
)

// TestClassifyOutcome covers the new retry-pipeline decision matrix.
//
// We test only the pure decision function (classifyOutcome), not the AMQP
// publish side effects — those are exercised by integration tests against a
// real broker.
func TestClassifyOutcome(t *testing.T) {
	policy := &RetryPolicy{
		MaxRetries: 3,
		Delays:     []time.Duration{time.Second, 2 * time.Second, 4 * time.Second},
	}

	cases := []struct {
		name    string
		retry   *RetryPolicy
		err     error
		attempt int
		want    outcome
	}{
		// Legacy mode (Retry == nil) preserves pre-retry-pipeline behaviour.
		{
			name:    "legacy: nil error -> ack",
			retry:   nil,
			err:     nil,
			attempt: 0,
			want:    outcomeAck,
		},
		{
			name:    "legacy: non-retryable -> legacy discard",
			retry:   nil,
			err:     queue.MarkNonRetryable(errors.New("bad payload")),
			attempt: 0,
			want:    outcomeLegacyDiscard,
		},
		{
			name:    "legacy: regular error -> legacy requeue",
			retry:   nil,
			err:     errors.New("temporary failure"),
			attempt: 0,
			want:    outcomeLegacyRequeue,
		},

		// Retry-pipeline mode.
		{
			name:    "retry: nil error -> ack",
			retry:   policy,
			err:     nil,
			attempt: 0,
			want:    outcomeAck,
		},
		{
			name:    "retry: non-retryable -> park",
			retry:   policy,
			err:     queue.MarkNonRetryable(errors.New("permanent")),
			attempt: 0,
			want:    outcomePark,
		},
		{
			name:    "retry: first failure within budget -> retry",
			retry:   policy,
			err:     errors.New("transient"),
			attempt: 0,
			want:    outcomeRetry,
		},
		{
			name:    "retry: last retry within budget -> retry",
			retry:   policy,
			err:     errors.New("transient"),
			attempt: 2, // attempt+1 == MaxRetries, still allowed
			want:    outcomeRetry,
		},
		{
			name:    "retry: budget exhausted -> park",
			retry:   policy,
			err:     errors.New("transient"),
			attempt: 3, // attempt+1 > MaxRetries
			want:    outcomePark,
		},
		{
			name:    "retry: non-retryable wins over exhausted budget",
			retry:   policy,
			err:     queue.MarkNonRetryable(errors.New("permanent")),
			attempt: 99,
			want:    outcomePark,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Consumer{cfg: ConsumerConfig{Retry: tc.retry}}
			got := c.classifyOutcome(tc.err, tc.attempt)
			if got != tc.want {
				t.Fatalf("classifyOutcome(err=%v, attempt=%d) = %d; want %d", tc.err, tc.attempt, got, tc.want)
			}
		})
	}
}

func TestRetryPolicyValidate(t *testing.T) {
	cases := []struct {
		name    string
		policy  *RetryPolicy
		wantErr bool
	}{
		{name: "nil is valid (legacy mode)", policy: nil, wantErr: false},
		{
			name:    "valid policy",
			policy:  &RetryPolicy{MaxRetries: 2, Delays: []time.Duration{time.Second, 2 * time.Second}},
			wantErr: false,
		},
		{
			name:    "MaxRetries must be > 0",
			policy:  &RetryPolicy{MaxRetries: 0, Delays: nil},
			wantErr: true,
		},
		{
			name:    "Delays length must match MaxRetries",
			policy:  &RetryPolicy{MaxRetries: 3, Delays: []time.Duration{time.Second}},
			wantErr: true,
		},
		{
			name:    "negative delay rejected",
			policy:  &RetryPolicy{MaxRetries: 1, Delays: []time.Duration{-time.Second}},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestInjectTraceContext(t *testing.T) {
	// When tracing is disabled (the default in unit tests, no Init call),
	// injectTraceContext must be a complete no-op: no panics, no header
	// mutation. This guards against a regression where the helper would
	// touch the headers even without an active tracer.
	t.Run("noop when tracer disabled", func(t *testing.T) {
		headers := amqp091.Table{"existing": "value"}
		injectTraceContext(context.Background(), headers)
		if got := headers["existing"]; got != "value" {
			t.Fatalf("existing header mutated unexpectedly; got %v", got)
		}
		if _, has := headers["traceparent"]; has {
			t.Fatalf("traceparent should not be injected when tracer is disabled")
		}
	})
}

func TestTruncatePanicError(t *testing.T) {
	t.Run("includes panic value and is non-retryable", func(t *testing.T) {
		stack := []byte("goroutine 1 [running]:\nmain.boom()\n\t/x.go:1 +0x0\n")
		err := truncatePanicError("boom", stack)
		if err == nil {
			t.Fatalf("expected non-nil error")
		}
		if !errors.Is(err, queue.NonRetryableErr) {
			t.Fatalf("panic error must be marked non-retryable; got %v", err)
		}
		if got := err.Error(); !contains(got, "panic: boom") {
			t.Fatalf("expected message to contain panic value; got %q", got)
		}
	})

	t.Run("oversized stack is truncated", func(t *testing.T) {
		stack := make([]byte, maxPanicStackBytes*2)
		for i := range stack {
			stack[i] = 'x'
		}
		err := truncatePanicError("boom", stack)
		// Header writer also truncates via truncateError, but the helper
		// itself bounds the captured stack to keep the in-memory string
		// reasonable. Allow some slack for the "panic: boom\n" prefix.
		if got := len(err.Error()); got > maxPanicStackBytes+64 {
			t.Fatalf("expected truncated error length <= %d; got %d", maxPanicStackBytes+64, got)
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestReadAttempt(t *testing.T) {
	cases := []struct {
		name string
		v    interface{}
		want int
	}{
		{name: "missing header -> 0", v: nil, want: 0},
		{name: "int", v: int(5), want: 5},
		{name: "int32", v: int32(7), want: 7},
		{name: "int64", v: int64(9), want: 9},
		{name: "unsupported type -> 0", v: "10", want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := amqp091.Table{}
			if tc.v != nil {
				h[HeaderXAttempt] = tc.v
			}
			got := readAttempt(h)
			if got != tc.want {
				t.Fatalf("readAttempt(%v) = %d; want %d", tc.v, got, tc.want)
			}
		})
	}
}
