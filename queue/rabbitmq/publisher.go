package rabbitmq

import (
	"context"
	"fmt"
	"time"

	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/queue"
	"github.com/whagency/core-common/tracer"
	"go.opentelemetry.io/otel/attribute"
	otelCodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/wagslane/go-rabbitmq"
	"go.opentelemetry.io/otel"
)

const (
	_defaultExchangeType   = "direct"
	_defaultContentType    = "application/json"
	_defaultPublishTimeout = 500 * time.Millisecond
)

type PublisherConfig struct {
	PublisherType      queue.PublisherType // type of the publisher
	ExchangeName       string              // exchange name to publish to
	ExchangeType       string              // exchange type (default: direct)
	ContentType        string              // default: application/json
	Mandatory          bool                // set 'mandatory' publish flag
	Persistent         bool                // persistent delivery mode
	DefaultRoutingKeys []string            // default routing keys if not set in the message
}

type Publisher struct {
	publisherType      queue.PublisherType // type of the publisher
	exchangeName       string              // exchange name to publish to
	publisher          *rabbitmq.Publisher
	baseOpts           []func(*rabbitmq.PublishOptions)
	defaultRoutingKeys []string // default routing keys if not set in the message
}

// NewPublisher creates a new instance of Publisher.
//
// The exchange named by cfg.ExchangeName must already exist before any
// Publish call is made. Publishers do not declare exchanges — it is the
// responsibility of the consumer (via WithConsumerOptionsExchangeDeclare) or
// an explicit DeclareRetryTopology call to ensure the exchange is present.
func NewPublisher(queueConn *rabbitmq.Conn, cfg PublisherConfig) (*Publisher, error) {
	if queueConn == nil {
		return nil, fmt.Errorf("[rabbitmq][NewPublisher][publisherType: %d][err: queue connection is nil]", cfg.PublisherType)
	}

	// Exchange name is required
	if cfg.ExchangeName == "" {
		return nil, fmt.Errorf("[rabbitmq][NewPublisher][publisherType: %d][err: exchange name is required]", cfg.PublisherType)
	}

	// Set default exchange type
	if cfg.ExchangeType == "" {
		cfg.ExchangeType = _defaultExchangeType
	}

	// Set default content type
	if cfg.ContentType == "" {
		cfg.ContentType = _defaultContentType
	}

	logger.Log().Infof("[rabbitmq][NewPublisher][config: %+v]", cfg)

	publisher, err := rabbitmq.NewPublisher(
		queueConn,
		rabbitmq.WithPublisherOptionsLogging,
		rabbitmq.WithPublisherOptionsExchangeName(cfg.ExchangeName),
		rabbitmq.WithPublisherOptionsExchangeKind(cfg.ExchangeType),
	)
	if err != nil {
		return nil, fmt.Errorf("[rabbitmq][NewPublisher][publisherType: %d][exchangeName: %s][err: %w]", cfg.PublisherType, cfg.ExchangeName, err)
	}

	if cfg.Mandatory {
		// Set return callback to log undeliverable messages
		publisher.NotifyReturn(func(r rabbitmq.Return) {
			logger.Log().Errorf(
				"[rabbitmq][NotifyReturn][publisherType: %d][exchangeName: %s][code=%d text=%s exchange=%s routingKey=%s contentType=%s bodySize=%d headers=%v]",
				cfg.PublisherType, cfg.ExchangeName, r.ReplyCode, r.ReplyText, r.Exchange, r.RoutingKey, r.ContentType, len(r.Body), r.Headers,
			)
		})
	}

	// Set base options
	baseOpts := []func(*rabbitmq.PublishOptions){
		rabbitmq.WithPublishOptionsExchange(cfg.ExchangeName),
		rabbitmq.WithPublishOptionsContentType(cfg.ContentType),
	}
	if cfg.Persistent {
		baseOpts = append(baseOpts, rabbitmq.WithPublishOptionsPersistentDelivery)
	}
	if cfg.Mandatory {
		baseOpts = append(baseOpts, rabbitmq.WithPublishOptionsMandatory)
	}

	return &Publisher{publisher: publisher, baseOpts: baseOpts, exchangeName: cfg.ExchangeName, publisherType: cfg.PublisherType, defaultRoutingKeys: append([]string(nil), cfg.DefaultRoutingKeys...)}, nil
}

// PublisherType returns the type of the publisher.
func (p *Publisher) PublisherType() queue.PublisherType {
	return p.publisherType
}

// ExchangeName returns the exchange name of the publisher.
func (p *Publisher) ExchangeName() string {
	return p.exchangeName
}

// Close closes the publisher.
func (p *Publisher) Close() error {
	if p.publisher != nil {
		p.publisher.Close()
	}
	return nil
}

// Publish publishes a message to the queue.
func (p *Publisher) Publish(ctx context.Context, message queue.Message) error {
	if p.publisher == nil {
		return fmt.Errorf("[rabbitmq][Publisher:Publish][publisherType: %d][exchangeName: %s][err: publisher is nil]", p.PublisherType(), p.exchangeName)
	}
	if len(message.Data) == 0 {
		return fmt.Errorf("[rabbitmq][Publisher:Publish][publisherType: %d][exchangeName: %s][err: message data is empty]", p.PublisherType(), p.exchangeName)
	}

	// Normalize routing keys based on exchange type
	routingKeys := message.RoutingKeys
	if len(routingKeys) == 0 && len(p.defaultRoutingKeys) > 0 {
		routingKeys = p.defaultRoutingKeys
	}

	logger.LogCtx(ctx).Debugf(
		"[rabbitmq][Publisher:Publish][publisherType:%d][exchangeName:%s][messageType:%s][routingKeys:%v][size:%dB]",
		p.PublisherType(), p.ExchangeName(), message.MessageType, routingKeys, len(message.Data),
	)
	if logger.IsLoggableBody() {
		logger.LogCtx(ctx).Debugw("[rabbitmq][Publisher:Publish][publishBody]",
			"queue_publish_body", logger.BodyFieldValueForLog(message.Data),
		)
	}

	// Optional producer span around publish
	var (
		endSpan tracer.SpanEnd
		span    trace.Span
		pubErr  error
	)
	if tracer.Enabled() {
		attrs := []attribute.KeyValue{
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.operation", "publish"),
			attribute.String("messaging.destination", p.exchangeName),
			attribute.String("messaging.destination.kind", "exchange"),
			attribute.Int("messaging.message.payload_size_bytes", len(message.Data)),
		}
		if message.MessageType != "" {
			attrs = append(attrs, attribute.String("message.type", message.MessageType.String()))
		}

		ctx, span, endSpan = tracer.NewSpan(ctx, tracer.SpanOptions{
			Name:       "rabbitmq publish " + p.exchangeName,
			Kind:       trace.SpanKindProducer,
			Attributes: attrs,
		})

		defer func() {
			if endSpan == nil {
				return
			}
			if pubErr != nil {
				if span != nil {
					span.RecordError(pubErr)
				}
				endSpan(tracer.SpanEndOptions{Code: otelCodes.Error, Message: "publish error"})
				return
			}
			endSpan(tracer.SpanEndOptions{Code: otelCodes.Ok, Message: "OK"})
		}()
	}

	headers := make(map[string]any, 8)
	if traceID := logger.GetRequestID(ctx); traceID != "" {
		headers[logger.DefaultRequestIDHeader] = traceID
	}
	if message.MessageType != "" {
		headers[queue.MessageTypeHeader] = message.MessageType.String()
	}

	// --- Tracing: inject W3C trace context into RabbitMQ headers ---
	if tracer.Enabled() {
		otel.GetTextMapPropagator().Inject(ctx, rabbitHeadersCarrier{H: headers})
	}

	opts := append([]func(*rabbitmq.PublishOptions){},
		p.baseOpts...,
	)
	opts = append(opts,
		rabbitmq.WithPublishOptionsHeaders(headers),
	)

	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ctx, cancel = context.WithTimeout(ctx, _defaultPublishTimeout)
		defer cancel()
	}

	pubErr = p.publisher.PublishWithContext(
		ctx,
		message.Data,
		routingKeys,
		opts...,
	)
	if pubErr != nil {
		return fmt.Errorf("[rabbitmq][Publisher:Publish][err: %w: %w]", queue.PublishFailedErr, pubErr)
	}

	return nil
}
