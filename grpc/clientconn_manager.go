package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ClientConnManager manages a single gRPC client connection.
// It dials lazily on first GetConnection() and then reuses the same connection.
// Safe for concurrent use.
type ClientConnManager struct {
	url string
	opt []grpc.DialOption

	mu   sync.Mutex
	conn *grpc.ClientConn
	err  error
}

// NewClientConnManager creates a new connection manager for the given url.
// It does not connect immediately; connection is established on first GetConnection().
func NewClientConnManager(url string, opts ...grpc.DialOption) *ClientConnManager {
	return &ClientConnManager{
		url: url,
		opt: opts,
	}
}

// URL returns the target URL this manager dials (useful for debugging/logging).
func (m *ClientConnManager) URL() string {
	if m == nil {
		return ""
	}
	return m.url
}

// GetConnection returns an existing connection or dials a new one on first use.
//
// Important:
// gRPC-Go ClientConn internally handles reconnect/backoff. In most cases you should NOT
// close and recreate connections on transient states; doing so can add churn and hurt latency.
func (m *ClientConnManager) GetConnection() (*grpc.ClientConn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn != nil {
		return m.conn, nil
	}

	m.conn, m.err = grpc.NewClient(m.url, m.opt...)
	if m.err != nil {
		return nil, m.err
	}

	return m.conn, nil
}

// Close closes the underlying connection (if any). Safe to call multiple times.
func (m *ClientConnManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn == nil {
		return nil
	}

	err := m.conn.Close()
	m.conn = nil
	m.err = nil
	return err
}

// Reset closes the existing connection (if any) and forces a new dial on next GetConnection().
// This should be rarely needed; prefer relying on gRPC's built-in reconnect.
func (m *ClientConnManager) Reset() error {
	return m.Close()
}

// DecodeResponseError normalizes gRPC errors to a consistent wrapped error with a stable prefix.
func DecodeResponseError(err error, method string) error {
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("[grpc][method:%s][non-grpc-error]%w", method, err)
	}

	switch st.Code() {
	case codes.Unavailable:
		return fmt.Errorf("[grpc][method:%s][unavailable][err: %w]", method, err)
	case codes.DeadlineExceeded:
		return fmt.Errorf("[grpc][method:%s][timeout][err: %w]", method, err)
	default:
		return fmt.Errorf("[grpc][method:%s][code:%s][msg:%s]%w", method, st.Code().String(), st.Message(), err)
	}
}

// WithShorterTimeout applies timeout only if it shortens the existing context deadline.
// cancel is nil if no new context was created.
func WithShorterTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= timeout {
			return ctx, nil
		}
	}

	return context.WithTimeout(ctx, timeout)
}

// UnaryClientTimeoutInterceptor applies a per-call timeout without extending an existing deadline.
// Handy if you want to avoid repeating WithShorterTimeout in every service method.
func UnaryClientTimeoutInterceptor(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx2, cancel := WithShorterTimeout(ctx, timeout)
		if cancel != nil {
			defer cancel()
		}
		return invoker(ctx2, method, req, reply, cc, opts...)
	}
}
