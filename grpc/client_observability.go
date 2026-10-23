package grpc

import (
	"context"

	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tracer"

	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ObservabilityUnaryClientInterceptor injects outgoing metadata (RequestID + trace context)
// for every unary client call.
func ObservabilityUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = WithOutgoingObservability(ctx)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// WithOutgoingObservability injects RequestID and OpenTelemetry trace context into outgoing gRPC metadata.
// It merges with existing outgoing metadata (if any) instead of overwriting it.
func WithOutgoingObservability(ctx context.Context) context.Context {
	// Preserve existing outgoing metadata if present.
	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		md = md.Copy()
	} else {
		md = metadata.New(nil)
	}

	// Ensure Request ID is propagated.
	reqID := logger.GetRequestID(ctx)
	if reqID != "" {
		md.Set(logger.DefaultRequestIDHeader, reqID)
	}

	// Inject trace context into metadata only when tracer is enabled.
	if tracer.Enabled() {
		otel.GetTextMapPropagator().Inject(ctx, NewMetadataCarrier(md))
	}

	return metadata.NewOutgoingContext(ctx, md)
}
