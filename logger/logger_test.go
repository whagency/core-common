package logger

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestLogCtxNilContextDoesNotPanic(t *testing.T) {
	prev := globalLogger
	globalLogger = zap.NewNop()
	t.Cleanup(func() {
		globalLogger = prev
	})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LogCtx(nil) panicked: %v", r)
		}
	}()

	got := LogCtx(nil)
	if got == nil {
		t.Fatal("expected logger, got nil")
	}
}

func TestLogCtxReturnsContextLogger(t *testing.T) {
	prev := globalLogger
	globalLogger = zap.NewNop()
	t.Cleanup(func() {
		globalLogger = prev
	})

	ctxLogger := zap.NewNop().Sugar()
	ctx := context.WithValue(context.Background(), ContextKeyLogger, ctxLogger)

	got := LogCtx(ctx)
	if got != ctxLogger {
		t.Fatal("expected logger from context")
	}
}
