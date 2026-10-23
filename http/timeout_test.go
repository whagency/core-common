package http

import (
	"context"
	"testing"
	"time"
)

func TestWithCallTimeout(t *testing.T) {
	t.Parallel()

	t.Run("positive timeout sets a deadline", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := WithCallTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("expected a deadline")
		}
		if time.Until(deadline) > 50*time.Millisecond {
			t.Fatalf("deadline is further out than the timeout: %s", time.Until(deadline))
		}
	})

	t.Run("zero timeout leaves the context unbounded", func(t *testing.T) {
		t.Parallel()

		// SetTimeout(0) meant "no timeout". Wrapping with a zero duration would
		// hand back an already-expired context and fail every call instantly.
		ctx, cancel := WithCallTimeout(context.Background(), 0)
		defer cancel()

		if _, ok := ctx.Deadline(); ok {
			t.Fatal("expected no deadline for a zero timeout")
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("context is already done: %v", err)
		}
	})

	t.Run("negative timeout leaves the context unbounded", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := WithCallTimeout(context.Background(), -time.Second)
		defer cancel()

		if _, ok := ctx.Deadline(); ok {
			t.Fatal("expected no deadline for a negative timeout")
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("context is already done: %v", err)
		}
	})

	t.Run("parent cancellation still propagates", func(t *testing.T) {
		t.Parallel()

		parent, cancelParent := context.WithCancel(context.Background())
		ctx, cancel := WithCallTimeout(parent, 0)
		defer cancel()

		cancelParent()
		<-ctx.Done()
	})
}
