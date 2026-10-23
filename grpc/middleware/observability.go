package middleware

import (
	"context"
	"fmt"
	"strings"
	"time"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tracer"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelCodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	grpcStatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var jsonMarshaler = protojson.MarshalOptions{
	UseProtoNames:   true,
	EmitUnpopulated: false,
}

// ObservabilityInterceptor combines logging + tracing into a single gRPC interceptor middleware.
// It ensures RequestID/InternalTraceID are generated once and used consistently in both logs and traces.
func ObservabilityInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		md, _ := metadata.FromIncomingContext(ctx)

		// Extract or generate Request ID.
		var reqID string
		if vals := md.Get(logger.DefaultRequestIDHeader); len(vals) > 0 && vals[0] != "" {
			reqID = vals[0]
		} else {
			reqID = uuid.New().String()
		}

		// Generate Internal Trace ID.
		internalID := uuid.New().String()

		// Build request-scoped logger.
		reqLogger := logger.Log().With(
			zap.String(logger.ContextKeyRequestID.String(), reqID),
			zap.String(logger.ContextKeyInternalTraceID.String(), internalID),
			zap.String("grpc_method", info.FullMethod),
		)

		ctx = context.WithValue(ctx, logger.ContextKeyRequestID, reqID)
		ctx = context.WithValue(ctx, logger.ContextKeyInternalTraceID, internalID)
		ctx = context.WithValue(ctx, logger.ContextKeyLogger, reqLogger)

		var endTrace func(error)
		if tracer.Enabled() {
			ctx, endTrace = startGRPCSpan(
				ctx,
				md,
				info.FullMethod,
				"gRPC "+info.FullMethod,
				reqID,
				internalID,
			)
		}

		reqLogger.Infof("[gRPC][middleware:ObservabilityInterceptor][incoming]")

		if logger.IsLoggableBody() {
			reqLogger.Infow("[gRPC][middleware:ObservabilityInterceptor][request]",
				"grpc_request_body", logger.BodyFieldValueForLog([]byte(marshalMessage(req))),
			)
		}

		resp, err := handler(ctx, req)

		latency := time.Since(start)
		if endTrace != nil {
			endTrace(err)
		}

		reqLogger.With(zap.Duration("latency", latency)).
			Infof("[gRPC][middleware:ObservabilityInterceptor][completed]")

		if logger.IsLoggableBody() {
			reqLogger.Infow("[gRPC][middleware:ObservabilityInterceptor][response]",
				"grpc_response_body", logger.BodyFieldValueForLog([]byte(marshalMessage(resp))),
			)
		}

		return resp, err
	}
}

// marshalMessage marshals a gRPC message into a JSON string.
func marshalMessage(m interface{}) string {
	if m == nil {
		return "<nil>"
	}
	pm, ok := m.(proto.Message)
	if !ok {
		return fmt.Sprintf("%v", m)
	}
	b, err := jsonMarshaler.Marshal(pm)
	if err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return string(b)
}

// startGRPCSpan starts a new tracing span for a gRPC request.
func startGRPCSpan(
	ctx context.Context,
	md metadata.MD,
	fullMethod string,
	spanName string,
	reqID string,
	internalID string,
) (context.Context, func(error)) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.TextMapCarrier(grpcCommon.NewMetadataCarrier(md)))

	service, method := splitGRPCFullMethod(fullMethod)
	attrs := []attribute.KeyValue{
		attribute.String("rpc.system", "grpc"),
		attribute.String("rpc.service", service),
		attribute.String("rpc.method", method),
		attribute.String("grpc.method", fullMethod),
		attribute.String("request.id", reqID),
		attribute.String("internal.trace_id", internalID),
	}

	var endSpan func(tracer.SpanEndOptions)
	var spanErr func(error)
	var span trace.Span
	ctx, span, endSpan = tracer.NewSpan(ctx, tracer.SpanOptions{
		Name:       spanName,
		Kind:       trace.SpanKindServer,
		Attributes: attrs,
	})

	spanErr = func(err error) {
		if endSpan == nil {
			return
		}

		code := grpcStatus.Code(err)
		statusMsg := code.String()
		span.SetAttributes(attribute.Int("rpc.grpc.status_code", int(code)))
		if err != nil {
			span.RecordError(err)
			endSpan(tracer.SpanEndOptions{Code: otelCodes.Error, Message: statusMsg})
			return
		}
		endSpan(tracer.SpanEndOptions{Code: otelCodes.Ok, Message: "OK"})
	}

	return ctx, spanErr
}

// splitGRPCFullMethod splits a gRPC full method string into service and method components.
func splitGRPCFullMethod(fullMethod string) (string, string) {
	if fullMethod == "" {
		return "", ""
	}
	trimmed := strings.TrimPrefix(fullMethod, "gRPC ")
	trimmed = strings.TrimPrefix(trimmed, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		return trimmed, ""
	}
	return parts[0], parts[1]
}
