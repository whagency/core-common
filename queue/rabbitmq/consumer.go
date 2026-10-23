package rabbitmq

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/queue"
	"github.com/whagency/core-common/tracer"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/wagslane/go-rabbitmq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelCodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type ConsumerConfig struct {
	RoutingKey     string
	QueueName      string
	ExchangeName   string
	ExchangeType   string
	ConsumerName   string
	ConsumerNumber int
	PrefetchCount  int

	// Exclusive and AutoDelete opt a consumer into the per-instance fanout
	// pattern: bound to a fanout/topic exchange, every running instance
	// receives its own copy of each message, instead of the default
	// competing-consumers behaviour where one shared queue balances messages
	// across instances. Use this only when every instance holds local state
	// (e.g. in-memory websocket connections) that must observe every event.
	//
	// QueueName stays the logical base name — NewConsumer derives the actual
	// per-instance queue name itself, since a shared name here would silently
	// turn the fanout back into a work queue. Consumer.QueueName reports the
	// name that was declared.
	//
	// Either flag makes the queue auto-delete and non-durable, so the queue
	// disappears with its instance. Exclusive additionally rejects any other
	// connection, which surfaces an accidental name collision as an error
	// instead of silent message stealing.
	//
	// Exclusive and AutoDelete are mutually exclusive with Retry: the
	// retry/DLQ pipeline assumes a stable, durable, operator-inspectable
	// queue, which an ephemeral per-instance queue is not.
	Exclusive  bool
	AutoDelete bool

	// Retry — optional bounded-retry policy. When nil, no DLQ/retry topology is
	// used and a failed delivery is discarded (NackDiscard): without a bounded
	// budget a requeue is an unbounded loop, so it is never chosen. Services opt
	// in to bounded retries by setting this and calling DeclareRetryTopology at
	// startup.
	Retry *RetryPolicy

	// RetryExchange and DLQExchange must match the topology declared by
	// DeclareRetryTopology when Retry is set.
	RetryExchange string
	DLQExchange   string
}

// RetryPolicy is a bounded retry schedule. Delays must contain exactly
// MaxRetries entries — the i-th delay is applied before retry i+1.
type RetryPolicy struct {
	MaxRetries int
	Delays     []time.Duration
}

// Validate enforces the invariants required for the retry pipeline.
func (p *RetryPolicy) Validate() error {
	if p == nil {
		return nil
	}
	if p.MaxRetries <= 0 {
		return fmt.Errorf("[rabbitmq][RetryPolicy][err: MaxRetries must be > 0]")
	}
	if len(p.Delays) != p.MaxRetries {
		return fmt.Errorf("[rabbitmq][RetryPolicy][err: Delays length (%d) must match MaxRetries (%d)]", len(p.Delays), p.MaxRetries)
	}
	for i, d := range p.Delays {
		if d < 0 {
			return fmt.Errorf("[rabbitmq][RetryPolicy][err: Delays[%d] must be >= 0]", i)
		}
	}
	return nil
}

// Header keys used by the retry pipeline. Exposed so operational tooling
// (DLQ inspectors) can read them.
const (
	HeaderXAttempt            = "x-attempt"
	HeaderXFirstFailedAt      = "x-first-failed-at"
	HeaderXLastError          = "x-last-error"
	HeaderXOriginalRoutingKey = "x-original-routing-key"
	HeaderXReason             = "x-reason"
	headerXDelay              = "x-delay" // owned by the delayed-message plugin

	// Reason values written into HeaderXReason when a message is parked.
	ReasonPermanent          = "permanent"
	ReasonMaxRetriesExceeded = "max_retries_exceeded"
	// ReasonPanic is set when the handler panicked. Panics are deterministic
	// programming errors, so the message is parked instead of retried.
	ReasonPanic = "panic"

	// maxPanicStackBytes caps the captured stack trace written into
	// HeaderXLastError so the AMQP header stays small.
	maxPanicStackBytes = 4096

	// maxLastErrorBytes caps HeaderXLastError so headers stay small.
	maxLastErrorBytes = 512
)

// outcome is the consumer's decision after a handler invocation.
type outcome int

const (
	outcomeAck outcome = iota
	outcomeLegacyRequeue
	outcomeLegacyDiscard
	outcomeRetry
	outcomePark
)

type Consumer struct {
	name string
	// queueName is the name actually declared, which differs from
	// cfg.QueueName for fanout consumers (see uniqueQueueName).
	queueName string
	cfg       ConsumerConfig
	conn      *rabbitmq.Conn
	consumer  *rabbitmq.Consumer
	handler   queue.ConsumerHandler

	retryPub *rabbitmq.Publisher // lazily-created publisher to RetryExchange
	dlqPub   *rabbitmq.Publisher // lazily-created publisher to DLQExchange
}

// NewConsumer creates a new consumer
func NewConsumer(conn *rabbitmq.Conn, handler queue.ConsumerHandler, cfg ConsumerConfig) (*Consumer, error) {
	if err := cfg.Retry.Validate(); err != nil {
		return nil, err
	}
	if cfg.Retry != nil {
		if cfg.RetryExchange == "" {
			return nil, fmt.Errorf("[rabbitmq][NewConsumer][err: RetryExchange is required when Retry is set]")
		}
		if cfg.DLQExchange == "" {
			return nil, fmt.Errorf("[rabbitmq][NewConsumer][err: DLQExchange is required when Retry is set]")
		}
		if cfg.Exclusive || cfg.AutoDelete {
			return nil, fmt.Errorf("[rabbitmq][NewConsumer][err: Exclusive/AutoDelete cannot be combined with Retry]")
		}
	}

	opts := []func(*rabbitmq.ConsumerOptions){
		rabbitmq.WithConsumerOptionsRoutingKey(cfg.RoutingKey),
		rabbitmq.WithConsumerOptionsConcurrency(cfg.ConsumerNumber),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsExchangeName(cfg.ExchangeName),
		rabbitmq.WithConsumerOptionsExchangeKind(cfg.ExchangeType),
		rabbitmq.WithConsumerOptionsExchangeDurable,
		rabbitmq.WithConsumerOptionsConsumerName(cfg.ConsumerName),
		rabbitmq.WithConsumerOptionsQOSPrefetch(cfg.PrefetchCount),
	}
	// Fanout consumers must not share a queue, so the declared name is derived
	// here rather than trusting every caller to remember. Getting this wrong on
	// an AutoDelete-only queue is silent: RabbitMQ simply balances messages
	// across instances instead of fanning them out, with no error anywhere.
	queueName := cfg.QueueName
	if cfg.Exclusive || cfg.AutoDelete {
		unique, errName := uniqueQueueName(cfg.QueueName)
		if errName != nil {
			return nil, fmt.Errorf("[rabbitmq][NewConsumer]%w", errName)
		}
		queueName = unique

		opts = append(opts, rabbitmq.WithConsumerOptionsQueueAutoDelete)
		if cfg.Exclusive {
			opts = append(opts, rabbitmq.WithConsumerOptionsQueueExclusive)
		}
	} else {
		opts = append(opts, rabbitmq.WithConsumerOptionsQueueDurable)
	}

	consumer, err := rabbitmq.NewConsumer(conn, queueName, opts...)
	if err != nil {
		return nil, fmt.Errorf("[rabbitmq][NewConsumer][err: %w]", err)
	}

	c := &Consumer{
		name:      cfg.ConsumerName,
		queueName: queueName,
		cfg:       cfg,
		conn:      conn,
		consumer:  consumer,
		handler:   handler,
	}

	if cfg.Retry != nil {
		retryPub, errPub := rabbitmq.NewPublisher(
			conn,
			rabbitmq.WithPublisherOptionsExchangeName(cfg.RetryExchange),
		)
		if errPub != nil {
			return nil, fmt.Errorf("[rabbitmq][NewConsumer][retryPublisher][err: %w]", errPub)
		}
		dlqPub, errPub := rabbitmq.NewPublisher(
			conn,
			rabbitmq.WithPublisherOptionsExchangeName(cfg.DLQExchange),
		)
		if errPub != nil {
			retryPub.Close()
			return nil, fmt.Errorf("[rabbitmq][NewConsumer][dlqPublisher][err: %w]", errPub)
		}
		c.retryPub = retryPub
		c.dlqPub = dlqPub
	}

	return c, nil
}

// Name returns the name of the consumer
func (c *Consumer) Name() string {
	return c.name
}

// QueueName returns the queue name that was actually declared. For fanout
// consumers this is the per-instance name, not the configured base.
func (c *Consumer) QueueName() string {
	return c.queueName
}

// Run runs the consumer
func (c *Consumer) Run() error {
	if c == nil || c.consumer == nil {
		return fmt.Errorf("[rabbitmq][Consumer:Run][err: consumer is nil]")
	}
	if c.handler == nil {
		return fmt.Errorf("[rabbitmq][Consumer:Run][name:%s][err: handler is nil]", c.Name())
	}

	return c.consumer.Run(func(d rabbitmq.Delivery) (action rabbitmq.Action) {
		// Default: be conservative on unexpected exits
		action = rabbitmq.NackDiscard

		ctx := context.Background()

		internalID := uuid.New().String()
		reqID := getRequestIDFromHeadersOrCreate(d.Headers)

		// Create a logger with standard fields
		newLogger := logger.Log().With(
			zap.String(logger.ContextKeyInternalTraceID.String(), internalID),
			zap.String(logger.ContextKeyRequestID.String(), reqID),
			zap.String("consumer_name", c.Name()),
		)

		// --- Tracing state (declared early so recover can mark span as errored) ---
		var (
			endSpan tracer.SpanEnd
			spErr   error
			span    trace.Span
		)

		defer func() {
			r := recover()
			if r != nil {
				stack := debug.Stack()
				spErr = fmt.Errorf("panic: %v", r)
				newLogger.Errorf("[rabbitmq][Consumer:Run][name:%s][panic:%v]\n%s", c.Name(), r, stack)

				// Without the retry pipeline, NackDiscard is the only sane choice:
				// requeue would just panic again. With the pipeline, park the
				// message in DLQ so the poison message is preserved for triage
				// instead of being silently dropped.
				if c.cfg.Retry == nil || c.dlqPub == nil {
					action = rabbitmq.NackDiscard
				} else {
					panicErr := truncatePanicError(r, stack)
					attempt := readAttempt(d.Headers)
					if pubErr := c.publishDLQ(ctx, d, attempt, ReasonPanic, panicErr); pubErr != nil {
						newLogger.Errorf("[rabbitmq][Consumer:Run][name:%s][publishDLQ on panic failed][err:%s]", c.Name(), pubErr)
						// Falling back to NackDiscard rather than NackRequeue: a
						// requeue of a panicking message is a guaranteed tight loop.
						action = rabbitmq.NackDiscard
					} else {
						newLogger.Warnf("[rabbitmq][Consumer:Run][name:%s][message parked to DLQ on panic]", c.Name())
						action = rabbitmq.Ack
					}
				}
			}

			if endSpan == nil {
				return
			}
			if spErr != nil {
				if span != nil {
					span.RecordError(spErr)
				}
				endSpan(tracer.SpanEndOptions{Code: otelCodes.Error, Message: "handler error"})
				return
			}
			endSpan(tracer.SpanEndOptions{Code: otelCodes.Ok, Message: "OK"})
		}()

		messageType, err := getMessageTypeFromHeaders(d.Headers)
		if err != nil {
			newLogger.Errorf("[rabbitmq][Consumer:Run][name:%s]%s", c.Name(), err)
			return rabbitmq.NackDiscard
		}
		if messageType != "" {
			newLogger = newLogger.With(zap.String("message_type", messageType.String()))
		}

		ctx = context.WithValue(ctx, logger.ContextKeyRequestID, reqID)
		ctx = context.WithValue(ctx, logger.ContextKeyInternalTraceID, internalID)
		ctx = context.WithValue(ctx, logger.ContextKeyLogger, newLogger)

		// --- Tracing: extract remote parent span context from AMQP headers and start consumer span ---
		if tracer.Enabled() {
			ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.TextMapCarrier(rabbitHeadersCarrier{H: d.Headers}))

			attrs := []attribute.KeyValue{
				attribute.String("messaging.system", "rabbitmq"),
				attribute.String("messaging.operation", "process"),
				attribute.String("messaging.destination.kind", "queue"),
				attribute.String("consumer.name", c.Name()),
				attribute.String("request.id", reqID),
				attribute.Int("messaging.message.payload_size_bytes", len(d.Body)),
			}
			if messageType != "" {
				attrs = append(attrs, attribute.String("message.type", messageType.String()))
			}

			ctx, span, endSpan = tracer.NewSpan(ctx, tracer.SpanOptions{
				Name:       "rabbitmq consume " + c.Name(),
				Kind:       trace.SpanKindConsumer,
				Attributes: attrs,
			})

		}

		newLogger.Infof("[rabbitmq][Consumer:Run][name:%s][messageType:%s][msg received]", c.Name(), messageType)
		if logger.IsLoggableBody() {
			newLogger.Infow("[rabbitmq][Consumer:Run][consumeBody]",
				"queue_consume_body", logger.BodyFieldValueForLog(d.Body),
			)
		}

		start := time.Now()

		err = c.handler.Handle(ctx, messageType, d.Body)
		spErr = err
		if err != nil {
			newLogger.Errorf("[rabbitmq][Consumer:Run][name:%s]%s", c.Name(), err)
			return c.decideOnError(ctx, d, err, newLogger)
		}

		newLogger.With(zap.Duration("latency", time.Since(start))).Infof("[rabbitmq][Consumer:Run][name:%s][messageType:%s][msg processed]", c.Name(), messageType)

		action = rabbitmq.Ack
		return action
	})
}

// Close closes the consumer and any retry/DLQ publishers it owns.
func (c *Consumer) Close() error {
	if c.retryPub != nil {
		c.retryPub.Close()
	}
	if c.dlqPub != nil {
		c.dlqPub.Close()
	}
	if c.consumer != nil {
		c.consumer.Close()
	}
	return nil
}

// decodeMessageType decodes the message type from the header
func decodeMessageType(header interface{}) (queue.MessageType, error) {
	messageType, ok := header.(string)
	if !ok {
		return "", fmt.Errorf("[decodeMessageType][type: %T][header: %v][err: invalid message type]", header, header)
	}

	return queue.NewMessageType(messageType), nil
}

// decodeRequestID decodes the request ID from the header
func decodeRequestID(header interface{}) string {
	reqID, ok := header.(string)
	if !ok {
		return ""
	}

	return reqID
}

// getRequestIDFromHeadersOrCreate returns the request ID from the headers or generates a new one
func getRequestIDFromHeadersOrCreate(headers amqp091.Table) string {
	if headers != nil {
		if v, ok := headers[logger.DefaultRequestIDHeader]; ok {
			if decoded := decodeRequestID(v); decoded != "" {
				return decoded
			}
		}
	}

	return uuid.New().String()
}

// getMessageTypeFromHeaders returns the message type from the headers or an empty string
func getMessageTypeFromHeaders(headers amqp091.Table) (queue.MessageType, error) {
	if headers != nil {
		if v, ok := headers[queue.MessageTypeHeader]; ok {
			return decodeMessageType(v)
		}
	}
	return "", nil
}
