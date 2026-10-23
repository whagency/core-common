package tracer

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// SpanOptions are the options for the NewSpan function.
type SpanOptions struct {
	Name      string
	StartTime time.Time

	// Kind sets span kind (server/client/consumer/producer/internal).
	// If zero (SpanKindUnspecified), OpenTelemetry default kind will be used.
	Kind trace.SpanKind

	// Typed OTel attributes
	Attributes []attribute.KeyValue
}

// SpanEndOptions are the options for the SpanEnd function.
type SpanEndOptions struct {
	Code    codes.Code
	Message string
	EndTime time.Time
}

// SpanEnd ends the span. You can still call span.RecordError(err) before End.
type SpanEnd func(SpanEndOptions)

// noopEnd is a no-operation SpanEnd function.
var noopEnd SpanEnd = func(SpanEndOptions) {}

// NewSpan creates a new span in the given context with the provided options.
func NewSpan(ctx context.Context, opts SpanOptions) (context.Context, trace.Span, SpanEnd) {
	if !Enabled() {
		return ctx, noop.Span{}, noopEnd
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "unnamed-operation"
	}

	start := opts.StartTime
	if start.IsZero() {
		start = time.Now()
	}

	startOpts := []trace.SpanStartOption{
		trace.WithTimestamp(start),
	}
	if opts.Kind != trace.SpanKindUnspecified {
		startOpts = append(startOpts, trace.WithSpanKind(opts.Kind))
	}

	ctx2, sp := Tracer().Start(ctx, name, startOpts...)

	if len(opts.Attributes) > 0 {
		sp.SetAttributes(opts.Attributes...)
	}

	return ctx2, sp, func(endOpts SpanEndOptions) {
		end := endOpts.EndTime
		if end.IsZero() {
			end = time.Now()
		}

		if endOpts.Code != codes.Unset {
			sp.SetStatus(endOpts.Code, endOpts.Message)
		}

		sp.End(trace.WithTimestamp(end))
	}
}

// CopySpanContext copies the span context from the parent context to the new context. Safe for goroutines: copy only SpanContext.
func CopySpanContext(parentCtx, newCtx context.Context) context.Context {
	sc := trace.SpanContextFromContext(parentCtx)
	if !sc.IsValid() {
		return newCtx
	}
	return trace.ContextWithSpanContext(newCtx, sc)
}
