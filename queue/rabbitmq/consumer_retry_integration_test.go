//go:build rabbitmq_integration
// +build rabbitmq_integration

package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	gorabbitmq "github.com/wagslane/go-rabbitmq"

	"github.com/whagency/core-common/queue"
	"github.com/whagency/core-common/testkit/loggertest"
)

const integrationRetryDelay = 100 * time.Millisecond

type integrationHandlerMode uint8

const (
	integrationHandlerTransient integrationHandlerMode = iota + 1
	integrationHandlerPermanent
	integrationHandlerPanic
)

type integrationHandler struct {
	mode  integrationHandlerMode
	calls atomic.Int32
}

func (h *integrationHandler) Handle(context.Context, queue.MessageType, []byte) error {
	h.calls.Add(1)

	switch h.mode {
	case integrationHandlerTransient:
		return errors.New("transient integration failure")
	case integrationHandlerPermanent:
		return queue.MarkNonRetryable(errors.New("permanent integration failure"))
	case integrationHandlerPanic:
		panic("panic integration failure")
	default:
		return nil
	}
}

func TestConsumerRetryPipelineIntegration(t *testing.T) {
	amqpURL := os.Getenv("TEST_RABBITMQ_URL")
	if amqpURL == "" {
		t.Skip("set TEST_RABBITMQ_URL to run RabbitMQ retry pipeline integration tests")
	}

	loggertest.Init(t, "common")

	cases := []struct {
		name        string
		mode        integrationHandlerMode
		wantReason  string
		wantAttempt int
		minCalls    int32
		maxWait     time.Duration
	}{
		{
			name:        "transient error retries once then parks",
			mode:        integrationHandlerTransient,
			wantReason:  ReasonMaxRetriesExceeded,
			wantAttempt: 1,
			minCalls:    2,
			maxWait:     5 * time.Second,
		},
		{
			name:        "non-retryable error parks immediately",
			mode:        integrationHandlerPermanent,
			wantReason:  ReasonPermanent,
			wantAttempt: 0,
			minCalls:    1,
			maxWait:     3 * time.Second,
		},
		{
			name:        "panic parks immediately",
			mode:        integrationHandlerPanic,
			wantReason:  ReasonPanic,
			wantAttempt: 0,
			minCalls:    1,
			maxWait:     3 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runConsumerRetryPipelineCase(t, amqpURL, tc.mode, tc.wantReason, tc.wantAttempt, tc.minCalls, tc.maxWait)
		})
	}
}

func runConsumerRetryPipelineCase(
	t *testing.T,
	amqpURL string,
	mode integrationHandlerMode,
	wantReason string,
	wantAttempt int,
	minCalls int32,
	maxWait time.Duration,
) {
	t.Helper()

	name := integrationName(t.Name())
	queueName := name + ".queue"
	exchangeName := name + ".exchange"
	routingKey := name + ".routing"
	retryExchange := DefaultRetryExchangeName(queueName)
	dlqExchange := DefaultDLQExchangeName(queueName)
	dlqQueue := DefaultDLQQueueName(queueName)

	topology := RetryTopology{
		QueueName:     queueName,
		RoutingKey:    routingKey,
		RetryExchange: retryExchange,
		DLQExchange:   dlqExchange,
		DLQQueueName:  dlqQueue,
	}
	if err := DeclareRetryTopology(amqpURL, topology); err != nil {
		cleanupRetryIntegrationTopologyAfterDeclareError(t, amqpURL, queueName, exchangeName, retryExchange, dlqExchange, dlqQueue)
		t.Fatalf("declare retry topology: %v", err)
	}

	amqpConn, err := amqp.Dial(amqpURL)
	if err != nil {
		t.Fatalf("dial amqp: %v", err)
	}
	defer func() { _ = amqpConn.Close() }()

	ch, err := amqpConn.Channel()
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer func() { _ = ch.Close() }()
	defer cleanupRetryIntegrationTopology(t, ch, queueName, exchangeName, retryExchange, dlqExchange, dlqQueue)

	declareOriginalRoute(t, ch, exchangeName, queueName, routingKey)
	dlqDeliveries := consumeDLQ(t, ch, dlqQueue)

	conn, err := gorabbitmq.NewConn(
		amqpURL,
		gorabbitmq.WithConnectionOptionsLogger(QueueLogger{}),
	)
	if err != nil {
		t.Fatalf("create go-rabbitmq connection: %v", err)
	}
	defer func() { _ = conn.Close() }()

	handler := &integrationHandler{mode: mode}
	consumer, err := NewConsumer(conn, handler, ConsumerConfig{
		RoutingKey:     routingKey,
		QueueName:      queueName,
		ExchangeName:   exchangeName,
		ExchangeType:   "direct",
		ConsumerName:   name + ".consumer",
		ConsumerNumber: 1,
		PrefetchCount:  1,
		Retry: &RetryPolicy{
			MaxRetries: 1,
			Delays:     []time.Duration{integrationRetryDelay},
		},
		RetryExchange: retryExchange,
		DLQExchange:   dlqExchange,
	})
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	var closeConsumer sync.Once
	closeConsumerFn := func() {
		closeConsumer.Do(func() {
			_ = consumer.Close()
		})
	}
	defer closeConsumerFn()

	runErr := make(chan error, 1)
	go func() {
		runErr <- consumer.Run()
	}()

	publishIntegrationMessage(t, ch, exchangeName, routingKey)
	delivery := waitForDLQDelivery(t, dlqDeliveries, maxWait)

	if got := string(delivery.Body); got != "integration-message" {
		t.Fatalf("DLQ body = %q; want integration-message", got)
	}
	if got := readAttempt(delivery.Headers); got != wantAttempt {
		t.Fatalf("x-attempt = %d; want %d", got, wantAttempt)
	}
	if got := headerString(delivery.Headers, HeaderXReason); got != wantReason {
		t.Fatalf("x-reason = %q; want %q", got, wantReason)
	}
	if got := headerString(delivery.Headers, HeaderXOriginalRoutingKey); got != routingKey {
		t.Fatalf("x-original-routing-key = %q; want %q", got, routingKey)
	}
	if got := headerString(delivery.Headers, HeaderXLastError); got == "" {
		t.Fatalf("x-last-error is empty")
	}
	if calls := handler.calls.Load(); calls < minCalls {
		t.Fatalf("handler calls = %d; want at least %d", calls, minCalls)
	}

	closeConsumerFn()
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatalf("consumer did not stop")
	}
}

func declareOriginalRoute(t *testing.T, ch *amqp.Channel, exchangeName, queueName, routingKey string) {
	t.Helper()

	if err := ch.ExchangeDeclare(exchangeName, "direct", true, false, false, false, nil); err != nil {
		t.Fatalf("declare original exchange: %v", err)
	}
	if err := ch.QueueBind(queueName, routingKey, exchangeName, false, nil); err != nil {
		t.Fatalf("bind original queue: %v", err)
	}
}

func consumeDLQ(t *testing.T, ch *amqp.Channel, dlqQueue string) <-chan amqp.Delivery {
	t.Helper()

	deliveries, err := ch.Consume(
		dlqQueue,
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("consume dlq: %v", err)
	}
	return deliveries
}

func publishIntegrationMessage(t *testing.T, ch *amqp.Channel, exchangeName, routingKey string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := ch.PublishWithContext(
		ctx,
		exchangeName,
		routingKey,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Headers: amqp.Table{
				queue.MessageTypeHeader: "integration.test",
			},
			Body: []byte("integration-message"),
		},
	)
	if err != nil {
		t.Fatalf("publish integration message: %v", err)
	}
}

func waitForDLQDelivery(t *testing.T, deliveries <-chan amqp.Delivery, maxWait time.Duration) amqp.Delivery {
	t.Helper()

	timer := time.NewTimer(maxWait)
	defer timer.Stop()

	select {
	case delivery := <-deliveries:
		return delivery
	case <-timer.C:
		t.Fatalf("timed out waiting for DLQ delivery after %s", maxWait)
		return amqp.Delivery{}
	}
}

func cleanupRetryIntegrationTopology(t *testing.T, ch *amqp.Channel, queueName, exchangeName, retryExchange, dlqExchange, dlqQueue string) {
	t.Helper()

	_, _ = ch.QueueDelete(queueName, false, false, false)
	_, _ = ch.QueueDelete(dlqQueue, false, false, false)
	_ = ch.ExchangeDelete(exchangeName, false, false)
	_ = ch.ExchangeDelete(retryExchange, false, false)
	_ = ch.ExchangeDelete(dlqExchange, false, false)
}

func cleanupRetryIntegrationTopologyAfterDeclareError(t *testing.T, amqpURL, queueName, exchangeName, retryExchange, dlqExchange, dlqQueue string) {
	t.Helper()

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		return
	}
	defer func() { _ = ch.Close() }()

	cleanupRetryIntegrationTopology(t, ch, queueName, exchangeName, retryExchange, dlqExchange, dlqQueue)
}

func headerString(headers amqp.Table, key string) string {
	if headers == nil {
		return ""
	}
	if v, ok := headers[key]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

func integrationName(name string) string {
	replacer := strings.NewReplacer(
		"/", ".",
		" ", "_",
		"(", "",
		")", "",
	)
	return strings.ToLower(fmt.Sprintf("retry.integration.%s.%d", replacer.Replace(name), time.Now().UnixNano()))
}
