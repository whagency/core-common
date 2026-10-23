package middleware

import (
	"context"
	"errors"
	"time"

	"github.com/whagency/core-common/logger"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TimeoutInterceptor adds a timeout to each gRPC request(context timeout)
func TimeoutInterceptor(timeout time.Duration) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if timeout <= 0 {
			return handler(ctx, req)
		}

		ctx2, cancel := withShorterTimeout(ctx, timeout)
		if cancel != nil {
			defer cancel()
		}

		resp, err := handler(ctx2, req)

		if errors.Is(ctx2.Err(), context.DeadlineExceeded) {
			// Important: use request-scoped logger from context (if ObservabilityInterceptor ran before).
			logger.LogCtx(ctx2).Warnf(
				"[gRPC][middleware:TimeoutInterceptor][timeout reached][method:%s][timeout:%v]",
				info.FullMethod,
				timeout,
			)
			return nil, status.Error(codes.DeadlineExceeded, "request timeout")
		}

		return resp, err
	}
}

// withShorterTimeout applies timeout only if it shortens the existing context deadline.
// Returns (ctx, cancel). cancel is nil if no new context was created.
func withShorterTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}

	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		// Existing deadline is already sooner (or effectively immediate) — keep it.
		if remaining <= timeout {
			return ctx, nil
		}
	}

	return context.WithTimeout(ctx, timeout)
}
