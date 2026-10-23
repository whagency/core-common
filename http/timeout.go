package http

import (
	"context"
	"time"
)

// WithCallTimeout bounds a single outbound call with a context deadline.
//
// It replaces req.Client.SetTimeout on the per-call path. SetTimeout writes
// httpClient.Timeout on the client, and services share one client, so calling
// it per request is an unsynchronised write to a field every in-flight request
// reads — a data race the detector reports as SetTimeout against
// net/http.(*Client).do. A deadline on the request's own context bounds the
// same call without touching shared state.
//
// A non-positive duration means "no per-call bound", matching what
// SetTimeout(0) did. Wrapping with a zero timeout instead would produce an
// already-expired context and fail every request immediately, so the caller
// still gets a cancel func to defer either way.
func WithCallTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, timeout)
}
