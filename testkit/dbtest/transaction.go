package dbtest

import "context"

// PassthroughTxManager implements the common transaction-manager shape for unit tests.
type PassthroughTxManager struct {
	CommitErr   error
	RollbackErr error
}

// NewPassthroughTxManager returns a transaction manager that preserves the incoming context.
func NewPassthroughTxManager(commitErr error) PassthroughTxManager {
	return PassthroughTxManager{CommitErr: commitErr}
}

// Begin returns the incoming context unchanged.
func (m PassthroughTxManager) Begin(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// Commit returns the configured commit error, if any.
func (m PassthroughTxManager) Commit(context.Context) error {
	return m.CommitErr
}

// Rollback returns the configured rollback error, if any.
func (m PassthroughTxManager) Rollback(context.Context) error {
	return m.RollbackErr
}

// ObservableTxManager is a pointer-based transaction manager stub that counts
// how many times Begin, Commit, and Rollback are called. Use it when a test
// needs to assert on the number of transaction lifecycle calls in addition to
// controlling commit/rollback errors.
type ObservableTxManager struct {
	BeginCalls    int
	CommitCalls   int
	RollbackCalls int
	CommitErr     error
	RollbackErr   error
}

// Begin increments BeginCalls and returns the incoming context unchanged.
func (m *ObservableTxManager) Begin(ctx context.Context) context.Context {
	m.BeginCalls++
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// Commit increments CommitCalls and returns the configured commit error, if any.
func (m *ObservableTxManager) Commit(context.Context) error {
	m.CommitCalls++
	return m.CommitErr
}

// Rollback increments RollbackCalls and returns the configured rollback error, if any.
func (m *ObservableTxManager) Rollback(context.Context) error {
	m.RollbackCalls++
	return m.RollbackErr
}
