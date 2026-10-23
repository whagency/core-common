package postgres_test

import (
	"context"
	"testing"
	"time"

	commondb "github.com/whagency/core-common/db"
	"github.com/whagency/core-common/outbox"
	outboxpostgres "github.com/whagency/core-common/outbox/postgres"
	outboxmigrations "github.com/whagency/core-common/outbox/postgres/migrations"
	"github.com/whagency/core-common/testkit/postgrestest"
	"gorm.io/gorm"
)

func openAtomicityDB(t *testing.T) *gorm.DB {
	t.Helper()
	return suite.OpenGormDB(t, postgrestest.OpenConfig{
		MigrationsFS:      outboxmigrations.FS,
		SkipMigrationLine: postgrestest.SkipDatabaseLevelMigrationLine,
	})
}

// The whole point of the pattern: the side effect is recorded in the same
// transaction as the business change, so a rollback takes it with it. Nothing
// else in the suite exercises this against a real transaction.
func TestRepository_Create_RollbackTakesTheMessageWithIt(t *testing.T) {
	t.Parallel()
	db := openAtomicityDB(t)
	repo := outboxpostgres.New(db, "outbox_messages")

	tx := db.Begin()
	ctx := commondb.NewTxInContext(context.Background(), tx)

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "deposit_request", AggregateID: "1",
		EventType: "ev", DedupeKey: "rolled-back",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("rollback: %v", err)
	}

	// A row that survived the rollback would mean a side effect promised for a
	// business change that never happened.
	msgs, err := repo.ClaimDue(context.Background(), "w", 30*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("claimed = %d, want 0: the outbox row outlived its transaction", len(msgs))
	}
}

// The mirror image: a committed transaction must leave the message claimable.
func TestRepository_Create_CommitPublishesTheMessage(t *testing.T) {
	t.Parallel()
	db := openAtomicityDB(t)
	repo := outboxpostgres.New(db, "outbox_messages")

	tx := db.Begin()
	ctx := commondb.NewTxInContext(context.Background(), tx)

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "deposit_request", AggregateID: "1",
		EventType: "ev", DedupeKey: "committed",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit: %v", err)
	}

	msgs, err := repo.ClaimDue(context.Background(), "w", 30*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("claimed = %d, want 1", len(msgs))
	}
	if msgs[0].DedupeKey != "committed" {
		t.Errorf("dedupe_key = %q, want committed", msgs[0].DedupeKey)
	}
}

// A duplicate insert must not abort the caller's transaction. ON CONFLICT DO
// NOTHING makes it a no-op, but a SAVEPOINT-wrapped write could turn it into a
// silent partial rollback instead.
func TestRepository_Create_DuplicateLeavesTransactionUsable(t *testing.T) {
	t.Parallel()
	db := openAtomicityDB(t)
	repo := outboxpostgres.New(db, "outbox_messages")
	ctx := context.Background()

	if err := repo.Create(ctx, outbox.CreateParams{
		AggregateType: "t", AggregateID: "1", EventType: "ev", DedupeKey: "dup",
	}); err != nil {
		t.Fatal(err)
	}

	tx := db.Begin()
	txCtx := commondb.NewTxInContext(ctx, tx)

	if err := repo.Create(txCtx, outbox.CreateParams{
		AggregateType: "t", AggregateID: "1", EventType: "ev", DedupeKey: "dup",
	}); err != nil {
		t.Fatalf("duplicate create must be a no-op, got: %v", err)
	}
	// The transaction must still accept work after the ignored conflict.
	if err := repo.Create(txCtx, outbox.CreateParams{
		AggregateType: "t", AggregateID: "1", EventType: "ev", DedupeKey: "after-dup",
	}); err != nil {
		t.Fatalf("transaction unusable after ignored duplicate: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit: %v", err)
	}

	msgs, err := repo.ClaimDue(ctx, "w", 30*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Errorf("claimed = %d, want 2 (original + after-dup)", len(msgs))
	}
}
