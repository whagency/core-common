package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/wagslane/go-rabbitmq"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"

	"github.com/whagency/core-common/queue"
	"github.com/whagency/core-common/tracer"
)

// injectTraceContext writes the current span context from ctx into the
// headers as W3C TraceContext (traceparent/tracestate). This makes the next
// consumer attempt link back to *this* attempt as remote parent instead of
// inheriting the original delivery's trace, which would produce N disconnected
// traces in Jaeger across retries.
//
// No-op when tracing is disabled. Existing trace headers in the map (copied
// from the original delivery) are overwritten — this is intentional: we want
// the chain to walk attempt -> attempt -> ... -> DLQ, not to sit as siblings
// of the first publish.
func injectTraceContext(ctx context.Context, headers amqp091.Table) {
	if !tracer.Enabled() {
		return
	}
	carrier := rabbitHeadersCarrier{H: map[string]any(headers)}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// classifyOutcome decides what to do with a failed message.
//
// Order of rules:
//  1. nil error          -> ack (handled by the caller; included for completeness).
//  2. legacy mode        -> NackDiscard for NonRetryableErr, NackRequeue otherwise.
//     This preserves the pre-retry-pipeline behaviour for any consumer that
//     does not opt in.
//  3. NonRetryableErr    -> park (publish to DLQ with reason=permanent).
//  4. retries exceeded   -> park (publish to DLQ with reason=max_retries_exceeded).
//  5. otherwise          -> retry (republish to RetryExchange with x-delay).
func (c *Consumer) classifyOutcome(err error, attempt int) outcome {
	if err == nil {
		return outcomeAck
	}
	if c.cfg.Retry == nil {
		if errors.Is(err, queue.NonRetryableErr) {
			return outcomeLegacyDiscard
		}
		return outcomeLegacyRequeue
	}
	if errors.Is(err, queue.NonRetryableErr) {
		return outcomePark
	}
	if attempt+1 > c.cfg.Retry.MaxRetries {
		return outcomePark
	}
	return outcomeRetry
}

// decideOnError applies the retry pipeline outcome and returns the AMQP
// action the underlying library should take.
//
// For "retry" and "park" outcomes, the message is republished elsewhere
// (retry/DLQ) and then ACK'd from the consumer queue.
func (c *Consumer) decideOnError(ctx context.Context, d rabbitmq.Delivery, handlerErr error, log *zap.SugaredLogger) rabbitmq.Action {
	attempt := readAttempt(d.Headers)

	switch c.classifyOutcome(handlerErr, attempt) {
	case outcomeLegacyDiscard:
		return rabbitmq.NackDiscard
	case outcomeLegacyRequeue:
		return rabbitmq.NackRequeue
	case outcomeRetry:
		nextAttempt := attempt + 1
		delay := c.cfg.Retry.Delays[attempt] // Delays[i] applied before attempt i+1
		if pubErr := c.publishRetry(ctx, d, nextAttempt, delay, handlerErr); pubErr != nil {
			// If we cannot publish the retry, do not lose the message —
			// fall back to the legacy NackRequeue so the broker keeps it.
			log.Errorw("[rabbitmq][Consumer][decideOnError][publishRetry failed; falling back to NackRequeue]",
				"error", pubErr,
				"retry_exchange", c.cfg.RetryExchange,
				"attempt", attempt,
			)
			return rabbitmq.NackRequeue
		}
		log.Infow("[rabbitmq][Consumer][decideOnError][retry scheduled]",
			"attempt", nextAttempt,
			"max_retries", c.cfg.Retry.MaxRetries,
			"delay", delay,
		)
		return rabbitmq.Ack

	case outcomePark:
		reason := ReasonMaxRetriesExceeded
		if errors.Is(handlerErr, queue.NonRetryableErr) {
			reason = ReasonPermanent
		}
		if pubErr := c.publishDLQ(ctx, d, attempt, reason, handlerErr); pubErr != nil {
			// Do not lose the message — fall back to NackRequeue so the
			// broker keeps it. This converts a "park" failure into a
			// retryable visibility issue rather than data loss.
			log.Errorw("[rabbitmq][Consumer][decideOnError][publishDLQ failed; falling back to NackRequeue]",
				"error", pubErr,
				"dlq_exchange", c.cfg.DLQExchange,
				"reason", reason,
			)
			return rabbitmq.NackRequeue
		}
		log.Warnw("[rabbitmq][Consumer][decideOnError][message parked to DLQ]",
			"reason", reason,
			"attempt", attempt,
		)
		return rabbitmq.Ack

	default:
		// Includes outcomeAck. Nil error is handled before this is called,
		// so reaching here means a programming error — fall back to safe
		// requeue rather than silently discarding.
		return rabbitmq.NackRequeue
	}
}

// publishRetry republishes a copy of the delivery to the retry exchange
// with x-delay set so the delayed-message plugin holds it for `delay`
// before routing it back to the original queue.
func (c *Consumer) publishRetry(ctx context.Context, d rabbitmq.Delivery, attempt int, delay time.Duration, handlerErr error) error {
	headers := copyHeaders(d.Headers)
	headers[HeaderXAttempt] = int32(attempt)
	headers[headerXDelay] = delay.Milliseconds()
	if _, ok := headers[HeaderXFirstFailedAt]; !ok {
		headers[HeaderXFirstFailedAt] = time.Now().UTC().Format(time.RFC3339)
	}
	headers[HeaderXLastError] = truncateError(handlerErr)
	headers[HeaderXOriginalRoutingKey] = c.cfg.RoutingKey
	injectTraceContext(ctx, headers)

	return c.retryPub.PublishWithContext(
		ctx,
		d.Body,
		[]string{c.cfg.RoutingKey},
		rabbitmq.WithPublishOptionsExchange(c.cfg.RetryExchange),
		rabbitmq.WithPublishOptionsContentType(_defaultContentType),
		rabbitmq.WithPublishOptionsPersistentDelivery,
		rabbitmq.WithPublishOptionsHeaders(rabbitmq.Table(headers)),
	)
}

// publishDLQ parks the delivery in the DLQ. The DLQ exchange is fanout, so
// routing key is irrelevant on publish but the original is preserved in
// headers for operator tooling.
func (c *Consumer) publishDLQ(ctx context.Context, d rabbitmq.Delivery, attempt int, reason string, handlerErr error) error {
	headers := copyHeaders(d.Headers)
	headers[HeaderXAttempt] = int32(attempt)
	headers[HeaderXReason] = reason
	if _, ok := headers[HeaderXFirstFailedAt]; !ok {
		headers[HeaderXFirstFailedAt] = time.Now().UTC().Format(time.RFC3339)
	}
	headers[HeaderXLastError] = truncateError(handlerErr)
	headers[HeaderXOriginalRoutingKey] = c.cfg.RoutingKey
	injectTraceContext(ctx, headers)

	return c.dlqPub.PublishWithContext(
		ctx,
		d.Body,
		[]string{c.cfg.RoutingKey},
		rabbitmq.WithPublishOptionsExchange(c.cfg.DLQExchange),
		rabbitmq.WithPublishOptionsContentType(_defaultContentType),
		rabbitmq.WithPublishOptionsPersistentDelivery,
		rabbitmq.WithPublishOptionsHeaders(rabbitmq.Table(headers)),
	)
}

// readAttempt extracts the current attempt counter from message headers.
// AMQP encodes integer headers as int32/int64 depending on the publisher;
// readAttempt handles both. Defaults to 0 when the header is missing —
// that means "this is the first delivery; the next failure is attempt 1".
func readAttempt(h amqp091.Table) int {
	if h == nil {
		return 0
	}
	v, ok := h[HeaderXAttempt]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

// copyHeaders returns a copy of the headers map.
func copyHeaders(h amqp091.Table) amqp091.Table {
	out := make(amqp091.Table, len(h)+5)
	for k, v := range h {
		out[k] = v
	}
	return out
}

// truncateError returns a truncated error string.
func truncateError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > maxLastErrorBytes {
		return s[:maxLastErrorBytes]
	}
	return s
}

// truncatePanicError builds a non-retryable error describing the panic value
// and a bounded stack trace. The result is suitable as the handlerErr argument
// to publishDLQ.
func truncatePanicError(r interface{}, stack []byte) error {
	if len(stack) > maxPanicStackBytes {
		stack = stack[:maxPanicStackBytes]
	}
	return queue.MarkNonRetryable(fmt.Errorf("panic: %v\n%s", r, stack))
}
