package dbtest

import (
	"context"
	"errors"
	"testing"
)

func TestPassthroughTxManager(t *testing.T) {
	commitErr := errors.New("commit failed")
	manager := NewPassthroughTxManager(commitErr)
	ctx := context.WithValue(context.Background(), struct{}{}, "value")

	if got := manager.Begin(ctx); got != ctx {
		t.Fatal("expected Begin to preserve context")
	}
	if err := manager.Commit(ctx); !errors.Is(err, commitErr) {
		t.Fatalf("expected commit error, got %v", err)
	}
	if err := manager.Rollback(ctx); err != nil {
		t.Fatalf("expected nil rollback error, got %v", err)
	}
}
