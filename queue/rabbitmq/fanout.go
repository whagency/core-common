package rabbitmq

import (
	"fmt"
	"os"

	"github.com/google/uuid"
)

// uniqueQueueName returns base suffixed with an identifier unique among
// concurrently running processes.
//
// It is used for consumers using ConsumerConfig.Exclusive/AutoDelete (the
// per-instance fanout pattern), where every running instance must bind its own
// queue instead of sharing one: reusing a static name across instances would
// make RabbitMQ balance messages between them like a normal work queue instead
// of fanning each message out to every instance.
//
// NewConsumer applies this automatically, so callers configure the logical
// base name and never build the unique name themselves. Consumer.QueueName
// reports the name that was actually declared.
//
// The suffix prefers "<hostname>-<pid>", matching the worker identity format
// used elsewhere in the platform: readable enough for an operator to map a
// queue back to a pod, and unique even when several processes share a hostname
// (sidecar containers, or two processes in one container). When the hostname is
// unavailable — common outside a container — it falls back to a random UUID so
// local processes still get distinct queues.
func uniqueQueueName(base string) (string, error) {
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return fmt.Sprintf("%s-%s-%d", base, hostname, os.Getpid()), nil
	}

	// Deliberately an error rather than a fallback to a shared name: a
	// non-unique name silently turns a fanout queue back into a work queue,
	// which is the exact failure this helper exists to prevent.
	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("[rabbitmq][uniqueQueueName][err: no hostname and random id failed: %w]", err)
	}
	return fmt.Sprintf("%s-%s", base, id.String()), nil
}
