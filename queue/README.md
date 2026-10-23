# Queue

Abstraction for working with Message Queues with support for RabbitMQ, lifecycle management via `Manager`, and integration with the logging system.

## Features

- 🏗️ **Abstract Interface**: Easy replacement of queue implementations (Publisher/Consumer).
- 🐇 **RabbitMQ**: Ready-to-use implementation based on `go-rabbitmq`.
- 🧩 **Queue Manager**: Centralized management of multiple publishers and consumers.
- 🆔 **Trace ID Propagation**: Automatic transmission of `X-Request-ID` via message headers.
- 🛠️ **Typing**: Support for `MessageType` for routing and processing different data types in a single queue.
- 🔁 **Retry/DLQ Pipeline**: Optional bounded RabbitMQ retries through delayed exchanges and parking DLQs.

## Installation

```bash
go get github.com/whagency/core-common/queue
```

## Quick Start

### 1. Connection (RabbitMQ)

A shared connection from `go-rabbitmq` is used.

```go
import (
    "github.com/wagslane/go-rabbitmq"
    "github.com/whagency/core-common/queue"
)

conn, err := rabbitmq.NewConn(
    "amqp://user:password@localhost:5672/",
    rabbitmq.WithConnectionOptionsLogging,
)
```

### 2. Creating a Publisher

```go
import "github.com/whagency/core-common/queue/rabbitmq"

publisher, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{
    PublisherType: 1, // Custom type ID
    ExchangeName:  "events",
    ExchangeType:  "direct",
    Persistent:    true,
})

// Sending a message
err = publisher.Publish(ctx, queue.Message{
    RoutingKeys: []string{"user.created"},
    MessageType: queue.NewMessageType("UserCreated"),
    Data:        []byte(`{"id": 123}`),
})
```

### 3. Creating a Consumer

```go
type MyHandler struct{}

func (h *MyHandler) Handle(ctx context.Context, msgType queue.MessageType, data []byte) error {
    fmt.Printf("Received message of type %s: %s\n", msgType, string(data))
    return nil
}

consumer, err := rabbitmq.NewConsumer(conn, &MyHandler{}, rabbitmq.ConsumerConfig{
    QueueName:      "user_events_queue",
    ExchangeName:   "events",
    RoutingKey:     "user.created",
    ConsumerName:   "user-service-consumer",
    ConsumerNumber: 3, // Number of workers
})

// Start
go consumer.Run()
```

### 4. Optional RabbitMQ Retry/DLQ Pipeline

Consumers can opt into bounded retries by setting `ConsumerConfig.Retry` and
declaring the matching topology during service startup.

```go
topology := rabbitmq.RetryTopology{
    QueueName:     "user.created",
    RoutingKey:    "user.created",
    RetryExchange: rabbitmq.DefaultRetryExchangeName("user.created"),
    DLQExchange:   rabbitmq.DefaultDLQExchangeName("user.created"),
    DLQQueueName:  rabbitmq.DefaultDLQQueueName("user.created"),
}
if err := rabbitmq.DeclareRetryTopology(amqpURL, topology); err != nil {
    return err
}

consumer, err := rabbitmq.NewConsumer(conn, handler, rabbitmq.ConsumerConfig{
    QueueName:      "user.created",
    ExchangeName:   "user.lifecycle",
    ExchangeType:   "direct",
    RoutingKey:     "user.created",
    ConsumerName:   "user-created-consumer",
    ConsumerNumber: 1,
    PrefetchCount:  10,
    Retry: &rabbitmq.RetryPolicy{
        MaxRetries: 3,
        Delays: []time.Duration{
            30 * time.Second,
            2 * time.Minute,
            10 * time.Minute,
        },
    },
    RetryExchange: rabbitmq.DefaultRetryExchangeName("user.created"),
    DLQExchange:   rabbitmq.DefaultDLQExchangeName("user.created"),
})
```

`MaxRetries` means retry count, not total processing attempts. For example,
`MaxRetries: 3` allows the original delivery plus three delayed retries before
the message is parked in DLQ.

When `Retry` is `nil`, the consumer keeps legacy behavior:

- regular errors return `NackRequeue`
- errors wrapped with `queue.MarkNonRetryable` return `NackDiscard`

When `Retry` is set:

- handler success ACKs the message
- retryable errors are republished to `<queue>.retry` with `x-delay`
- non-retryable errors are parked in `<queue>.dlq`
- exhausted retries are parked in `<queue>.dlq`
- panics are recovered and parked in `<queue>.dlq`

```mermaid
flowchart LR
    Q["Main queue"] --> C["Consumer"]
    C -->|success| ACK["ACK"]
    C -->|retryable error| RX["<queue>.retry<br/>x-delayed-message"]
    RX -->|x-delay elapsed| Q
    C -->|non-retryable<br/>exhausted retries<br/>panic| DLX["<queue>.dlq<br/>fanout exchange"]
    DLX --> DLQ["<queue>.dlq<br/>parking queue"]
```

DLQ messages include operational headers:

| Header | Description |
| --- | --- |
| `x-attempt` | Current retry attempt counter. |
| `x-first-failed-at` | UTC timestamp of the first failure. |
| `x-last-error` | Truncated last error message. |
| `x-original-routing-key` | Routing key used by the original queue. |
| `x-reason` | Parking reason. |

Known `x-reason` values:

| Reason | Meaning |
| --- | --- |
| `permanent` | Handler returned an error marked with `queue.MarkNonRetryable`. |
| `max_retries_exceeded` | Retry budget was exhausted. |
| `panic` | Handler panicked; the panic was recovered and parked. |

The retry exchange uses RabbitMQ's `rabbitmq_delayed_message_exchange` plugin.
Local app infrastructure should enable it by default. Verify with:

```bash
docker exec app-infra-rabbitmq-1 rabbitmq-plugins list -e | grep delayed
```

## Queue Manager

`Manager` allows you to combine all publishers and consumers in one place for convenient application startup and shutdown.

```go
mgr := queue.NewManager()

// Add components
mgr.AddPublisher(myPublisher)
mgr.AddConsumer(myConsumer)

// Run all consumers in separate goroutines
go mgr.RunConsumers()

// Graceful shutdown
defer mgr.CloseConsumers()
defer mgr.ClosePublishers()
```

## Configuration

### ConnectionConfig (for DSN generation)
| Field | Description |
|------|----------|
| `Host` | RabbitMQ host |
| `Port` | Port (usually 5672) |
| `User` | Username |
| `Password` | Password |
| `VHost` | Virtual Host |

### PublisherConfig
| Field | Description |
|------|----------|
| `ExchangeName` | Exchange name |
| `ExchangeType` | Type (direct, topic, fanout) |
| `Persistent` | Whether to save messages on disk |
| `Mandatory` | Mandatory delivery |

### ConsumerConfig
| Field | Description |
|------|----------|
| `QueueName` | Queue name |
| `RoutingKey` | Routing key |
| `ConsumerNumber` | Number of parallel handlers (concurrency) |
| `PrefetchCount` | Message limit in processing (QoS) |
| `Retry` | Optional `RetryPolicy`; nil keeps legacy `NackRequeue` behavior |
| `RetryExchange` | Retry delayed exchange name, required when `Retry` is set |
| `DLQExchange` | DLQ exchange name, required when `Retry` is set |

### RetryPolicy
| Field | Description |
|------|----------|
| `MaxRetries` | Number of delayed retries before parking in DLQ |
| `Delays` | Per-retry delay schedule; length must match `MaxRetries` |

## API Reference

### `Publisher` Interface
```go
type Publisher interface {
    Publish(ctx context.Context, msg Message) error
    PublisherType() PublisherType
    ExchangeName() string
    Close() error
}
```

### `ConsumerHandler` Interface
```go
type ConsumerHandler interface {
    // Handle is called for each incoming message
    Handle(ctx context.Context, msgType MessageType, data []byte) error
}
```

## Logger Integration

The `queue` package automatically extracts `X-Request-ID` from the context when publishing and from message headers when consuming. This allows tracking the request path even through message queues in the logs:

```go
// Logger inside Handler will automatically get RequestID from the message
func (h *MyHandler) Handle(ctx context.Context, msgType queue.MessageType, data []byte) error {
    logger.LogCtx(ctx).Info("Processing message") 
    return nil
}
```

## Testing Retry/DLQ Behavior

Run always-on unit tests:

```bash
go test ./queue/rabbitmq
```

Run the opt-in RabbitMQ integration smoke against a local broker:

```bash
TEST_RABBITMQ_URL=amqp://user:password@127.0.0.1:5672/ \
go test -tags=rabbitmq_integration ./queue/rabbitmq -run TestConsumerRetryPipelineIntegration -v
```

The integration smoke validates retryable, non-retryable, and panic paths
against a real RabbitMQ broker.

## License

MIT
