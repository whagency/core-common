package rabbitmq

import (
	"fmt"
	"net/url"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/whagency/core-common/queue"
)

const (
	// exchangeTypeDelayed is the type provided by the
	// rabbitmq_delayed_message_exchange community plugin. It holds messages
	// for the duration specified by the `x-delay` header before routing them
	// using the wrapped exchange type configured via `x-delayed-type`.
	exchangeTypeDelayed = "x-delayed-message"

	// retryDelayedKindArg names the inner exchange type wrapped by the
	// delayed-message exchange. We use direct routing because retry routes
	// every message back to a single dedicated queue.
	retryDelayedKindArg = "x-delayed-type"

	// exchangeTypeFanout is used for the DLQ exchange so any failed message
	// for the queue ends up in its single parking queue regardless of the
	// original routing key.
	exchangeTypeFanout = "fanout"
)

// RetryTopology describes the per-queue retry/DLQ topology that
// DeclareRetryTopology is responsible for creating.
//
// It is intentionally per-queue (not per-exchange): an exchange may have
// multiple consumers (different queues), and a retry on one consumer must
// not fan out back to the others.
type RetryTopology struct {
	// QueueName is the original consumer queue name.
	// It must already be (or will be) declared with the same durability
	// settings the consumer expects.
	QueueName string

	// RoutingKey is the original routing key the consumer binds with.
	// Retry republishing uses the same key so the retry exchange routes
	// back to the consumer queue.
	RoutingKey string

	// RetryExchange is the name of the per-queue delayed-message exchange.
	// Recommended convention: "<QueueName>.retry".
	RetryExchange string

	// DLQExchange is the name of the per-queue DLQ exchange (fanout).
	// Recommended convention: "<QueueName>.dlq".
	DLQExchange string

	// DLQQueueName is the parking queue. No consumer reads from it
	// automatically — it is inspected by operators.
	// Recommended convention: "<QueueName>.dlq".
	DLQQueueName string
}

// Validate checks that RetryTopology has all required fields.
func (t RetryTopology) Validate() error {
	if t.QueueName == "" {
		return fmt.Errorf("[rabbitmq][RetryTopology][err: QueueName is required]")
	}
	if t.RoutingKey == "" {
		return fmt.Errorf("[rabbitmq][RetryTopology][err: RoutingKey is required]")
	}
	if t.RetryExchange == "" {
		return fmt.Errorf("[rabbitmq][RetryTopology][err: RetryExchange is required]")
	}
	if t.DLQExchange == "" {
		return fmt.Errorf("[rabbitmq][RetryTopology][err: DLQExchange is required]")
	}
	if t.DLQQueueName == "" {
		return fmt.Errorf("[rabbitmq][RetryTopology][err: DLQQueueName is required]")
	}
	return nil
}

// DefaultRetryExchangeName returns the recommended retry exchange name
// for a given queue: "<queue>.retry".
func DefaultRetryExchangeName(queueName string) string {
	return queueName + ".retry"
}

// DefaultDLQExchangeName returns the recommended DLQ exchange name for a
// given queue: "<queue>.dlq".
func DefaultDLQExchangeName(queueName string) string {
	return queueName + ".dlq"
}

// DefaultDLQQueueName returns the recommended DLQ queue name for a given
// queue: "<queue>.dlq".
func DefaultDLQQueueName(queueName string) string {
	return queueName + ".dlq"
}

// BuildAMQPURL builds an AMQP DSN from a queue.ConnectionConfig.
// Centralised so consumers and the topology bootstrap use the same format.
func BuildAMQPURL(c queue.ConnectionConfig) string {
	u := &url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(c.User, c.Password),
		Host:   net(c.Host, c.Port),
		Path:   "/" + c.VHost,
	}
	return u.String()
}

// net returns a host:port string, omitting the port if it is empty.
func net(host, port string) string {
	if port == "" {
		return host
	}
	return host + ":" + port
}

// DeclareRetryTopology creates the retry+DLQ topology for the given queue
// using a short-lived dedicated AMQP connection. It is idempotent and safe
// to call on every service start.
//
// It declares (in this order):
//  1. the original queue (durable; harmless if already declared with the same
//     settings — RabbitMQ will accept the redundant declaration).
//  2. the retry exchange (x-delayed-message wrapping a direct exchange).
//  3. the DLQ exchange (fanout) and the DLQ queue.
//  4. bindings:
//     - retry -> original queue with the original routing key.
//     - DLQ queue -> DLQ exchange (no routing key; fanout).
//
// The function does not declare the original exchange (publishers/consumers
// for that already do it), and does not bind the original queue to the
// original exchange (the consumer does that itself via wagslane options).
func DeclareRetryTopology(amqpURL string, t RetryTopology) error {
	if err := t.Validate(); err != nil {
		return err
	}

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return fmt.Errorf("[rabbitmq][DeclareRetryTopology][dial][err: %w]", err)
	}
	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("[rabbitmq][DeclareRetryTopology][channel][err: %w]", err)
	}
	defer func() { _ = ch.Close() }()

	// 1. Original queue (idempotent declare with durable=true to match
	//    consumer's WithConsumerOptionsQueueDurable). If the consumer later
	//    declares the same queue with different settings, RabbitMQ will
	//    raise a precondition-failed error — that is intentional, it forces
	//    config drift to surface immediately.
	if _, errDeclare := ch.QueueDeclare(
		t.QueueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,
	); errDeclare != nil {
		return fmt.Errorf("[rabbitmq][DeclareRetryTopology][queue:%s][err: %w]", t.QueueName, errDeclare)
	}

	// 2. Retry exchange (delayed-message, wrapping direct).
	if errDeclare := ch.ExchangeDeclare(
		t.RetryExchange,
		exchangeTypeDelayed,
		true,  // durable
		false, // autoDelete
		false, // internal
		false, // noWait
		amqp.Table{retryDelayedKindArg: "direct"},
	); errDeclare != nil {
		return fmt.Errorf("[rabbitmq][DeclareRetryTopology][retryExchange:%s][err: %w]", t.RetryExchange, errDeclare)
	}

	// 3. DLQ exchange (fanout) + DLQ queue.
	if errDeclare := ch.ExchangeDeclare(
		t.DLQExchange,
		exchangeTypeFanout,
		true,  // durable
		false, // autoDelete
		false, // internal
		false, // noWait
		nil,
	); errDeclare != nil {
		return fmt.Errorf("[rabbitmq][DeclareRetryTopology][dlqExchange:%s][err: %w]", t.DLQExchange, errDeclare)
	}
	if _, errDeclare := ch.QueueDeclare(
		t.DLQQueueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,
	); errDeclare != nil {
		return fmt.Errorf("[rabbitmq][DeclareRetryTopology][dlqQueue:%s][err: %w]", t.DLQQueueName, errDeclare)
	}

	// 4. Bindings.
	if errBind := ch.QueueBind(
		t.QueueName,
		t.RoutingKey,
		t.RetryExchange,
		false,
		nil,
	); errBind != nil {
		return fmt.Errorf("[rabbitmq][DeclareRetryTopology][bindRetry][err: %w]", errBind)
	}
	if errBind := ch.QueueBind(
		t.DLQQueueName,
		"", // fanout ignores routing key
		t.DLQExchange,
		false,
		nil,
	); errBind != nil {
		return fmt.Errorf("[rabbitmq][DeclareRetryTopology][bindDLQ][err: %w]", errBind)
	}

	return nil
}
