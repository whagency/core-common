package rabbitmq

import (
	"strings"
	"testing"
	"time"
)

// The whole point of the fanout pattern is that instances do not share a queue.
// A name that is merely "usually" unique silently degrades into a work queue.
func TestUniqueQueueName_IsUniquePerProcessAndKeepsBase(t *testing.T) {
	t.Parallel()

	name, err := uniqueQueueName("notification.push")
	if err != nil {
		t.Fatalf("uniqueQueueName: %v", err)
	}

	if !strings.HasPrefix(name, "notification.push-") {
		t.Errorf("name = %q, want it to start with the configured base", name)
	}
	if name == "notification.push" {
		t.Error("name must be suffixed, otherwise instances share one queue")
	}

	// Stable within a process, so a reconnect rebinds the same queue rather
	// than leaking a new one on every attempt.
	again, err := uniqueQueueName("notification.push")
	if err != nil {
		t.Fatalf("uniqueQueueName (second call): %v", err)
	}
	if again != name {
		t.Errorf("name changed between calls: %q then %q", name, again)
	}
}

// Two hostname-sharing processes — sidecar containers, or two processes in one
// container — must still get distinct queues, which is why the pid is included.
func TestUniqueQueueName_IncludesProcessIdentity(t *testing.T) {
	t.Parallel()

	name, err := uniqueQueueName("base")
	if err != nil {
		t.Fatalf("uniqueQueueName: %v", err)
	}

	suffix := strings.TrimPrefix(name, "base-")
	if suffix == "" {
		t.Fatalf("name = %q, want a non-empty suffix", name)
	}
	// Either "<hostname>-<pid>" or a UUID fallback; both must be more than a
	// bare hostname, which is shared by co-located processes.
	if !strings.Contains(suffix, "-") {
		t.Errorf("suffix = %q, want it to carry per-process identity, not just a hostname", suffix)
	}
}

// Retry needs a durable, operator-inspectable queue; an ephemeral per-instance
// queue is neither. Catching this at construction beats discovering it when a
// DLQ turns out to be empty.
func TestNewConsumer_RejectsRetryWithEphemeralQueue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  ConsumerConfig
	}{
		{
			name: "exclusive",
			cfg: ConsumerConfig{
				QueueName: "q", ExchangeName: "ex", ConsumerName: "c",
				Exclusive:     true,
				Retry:         &RetryPolicy{MaxRetries: 1, Delays: []time.Duration{time.Second}},
				RetryExchange: "retry-ex", DLQExchange: "dlq-ex",
			},
		},
		{
			name: "auto delete",
			cfg: ConsumerConfig{
				QueueName: "q", ExchangeName: "ex", ConsumerName: "c",
				AutoDelete:    true,
				Retry:         &RetryPolicy{MaxRetries: 1, Delays: []time.Duration{time.Second}},
				RetryExchange: "retry-ex", DLQExchange: "dlq-ex",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Validation runs before the connection is touched, so a nil conn
			// is enough to exercise it.
			_, err := NewConsumer(nil, nil, tt.cfg)
			if err == nil {
				t.Fatal("expected an error combining Retry with an ephemeral queue")
			}
			if !strings.Contains(err.Error(), "Exclusive/AutoDelete cannot be combined with Retry") {
				t.Errorf("err = %v, want the Exclusive/AutoDelete vs Retry rejection", err)
			}
		})
	}
}
