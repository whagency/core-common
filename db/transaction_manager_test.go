package db

import "testing"

func TestNewTxInContext_NilContextDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewTxInContext(nil, nil) panicked: %v", r)
		}
	}()

	ctx := NewTxInContext(nil, nil)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
}

func TestGetTxFromContext_NilContextReturnsNil(t *testing.T) {
	if tx := GetTxFromContext(nil); tx != nil {
		t.Fatal("expected nil tx for nil context")
	}
}

func TestBegin_NilDBDoesNotPanic(t *testing.T) {
	tm := TransactionManager{}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Begin(nil) panicked with nil db: %v", r)
		}
	}()

	ctx := tm.Begin(nil)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if tx := GetTxFromContext(ctx); tx != nil {
		t.Fatal("expected nil tx when db is nil")
	}
}

func TestCommitRollback_NilContextReturnsError(t *testing.T) {
	tm := TransactionManager{}

	if err := tm.Commit(nil); err == nil {
		t.Fatal("expected commit error for nil context without tx")
	}
	if err := tm.Rollback(nil); err == nil {
		t.Fatal("expected rollback error for nil context without tx")
	}
}
