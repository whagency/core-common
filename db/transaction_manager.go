package db

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

type txCtxType string

var txFieldName txCtxType = "tx"

// NewTxInContext Store the transaction object in the context
func NewTxInContext(ctx context.Context, db interface{}) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, txFieldName, db)
}

// GetTxFromContext Get transaction object from context
func GetTxFromContext(ctx context.Context) *gorm.DB {
	if ctx == nil {
		return nil
	}
	if tx, ok := ctx.Value(txFieldName).(*gorm.DB); ok {
		return tx
	}
	return nil
}

type TransactionManager struct {
	db *gorm.DB
}

// NewTransactionManager creates a new TransactionManager
func NewTransactionManager(db *gorm.DB) *TransactionManager {
	return &TransactionManager{
		db: db,
	}
}

// Begin creates a new transaction and returns a new context with the transaction
func (r TransactionManager) Begin(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if r.db == nil {
		return ctx
	}
	return NewTxInContext(ctx, r.db.WithContext(ctx).Begin())
}

// Commit commits the transaction
func (r TransactionManager) Commit(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tx := GetTxFromContext(ctx)
	if tx == nil {
		return errors.New("[TransactionManager:Commit][err: no transaction found in context]")
	}

	if err := tx.WithContext(ctx).Commit().Error; err != nil {
		return fmt.Errorf("[TransactionManager:Commit][err: %w]", err)
	}

	return nil
}

// Rollback rolls back the transaction
func (r TransactionManager) Rollback(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tx := GetTxFromContext(ctx)
	if tx == nil {
		return errors.New("[TransactionManager:Rollback][err: no transaction found in context]")
	}

	if err := tx.WithContext(ctx).Rollback().Error; err != nil {
		return fmt.Errorf("[TransactionManager:Rollback][err: %w]", err)
	}

	return nil
}
