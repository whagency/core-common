package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	commondb "github.com/whagency/core-common/db"
	"github.com/whagency/core-common/ledgercompensation"
	compmigrations "github.com/whagency/core-common/ledgercompensation/postgres/migrations"
	"github.com/whagency/core-common/services/account"
	"github.com/whagency/core-common/testkit/loggertest"
	"github.com/whagency/core-common/testkit/postgrestest"
	postgresGorm "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var pgSuite *postgrestest.Suite

// TestMain owns one disposable PostgreSQL container for this package.
//
// These tests run against a real engine rather than mocks because the property
// under test is a database property: whether a write survives the rollback of a
// transaction it did not join. No mock transaction manager can express that —
// the passthrough mock used in controller tests cannot roll anything back — so
// the guarantee the whole compensation design rests on would otherwise be
// asserted nowhere.
func TestMain(m *testing.M) {
	ctx := context.Background()

	suite, err := postgrestest.Start(ctx, postgrestest.ContainerConfig{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start postgres container: %v\n", err)
		os.Exit(1)
	}
	pgSuite = suite

	// Extensions are database-scoped, so a per-test schema can never own one.
	// Create them once here and strip them from the migrations below: left in
	// place, parallel schemas race on CREATE EXTENSION IF NOT EXISTS and
	// Postgres reports a duplicate-key violation on pg_extension rather than
	// tolerating the concurrent attempt.
	if err := createExtensions(ctx, suite.DSN()); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create extensions: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if errTerminate := pgSuite.Terminate(ctx); errTerminate != nil {
		fmt.Fprintf(os.Stderr, "failed to terminate postgres container: %v\n", errTerminate)
	}

	os.Exit(code)
}

// createExtensions installs the database-level extensions the migrations expect.
func createExtensions(ctx context.Context, dsn string) error {
	db, err := gorm.Open(postgresGorm.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	for _, extension := range []string{"pgcrypto", "pg_stat_statements"} {
		if _, errExec := sqlDB.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS "+extension); errExec != nil {
			return fmt.Errorf("%s: %w", extension, errExec)
		}
	}

	return nil
}

// skipExtensionLine strips CREATE EXTENSION statements from migrations; TestMain
// has already installed them database-wide.
func skipExtensionLine(line string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "CREATE EXTENSION")
}

// openDB creates an isolated schema with the service's real migrations applied.
func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	loggertest.Init(t, "ledgercompensation-test")

	// Four connections, not the default two. These tests deliberately hold a
	// business transaction open on one connection while writing the compensation
	// row on another — the same shape production uses — so a pool of two would
	// deadlock the test against itself rather than exercise the behaviour.
	return pgSuite.OpenGormDB(t, postgrestest.OpenConfig{
		MigrationsFS:      compmigrations.FS,
		SchemaPrefix:      "ledgercomp",
		SkipMigrationLine: skipExtensionLine,
		MaxOpenConns:      4,
	})
}

// aggregateType is what a service would pass; the shared table treats it as an
// opaque label and never joins on it.
const aggregateType = "pay_order"

// newAggregateID returns a fresh aggregate id.
//
// Nothing is seeded: the shared table carries no foreign key, because it cannot
// know which table — or which key type — a given service's aggregate lives in.
// That is the same trade already made for outbox_messages.
func newAggregateID() string {
	return uuid.NewString()
}

// armSwitch runs the two writes a controller makes before and after the ledger call.
//
// It deliberately goes through WithTxFromContext, which is the shape of the
// mistake worth defending against: a caller who reaches for the transaction-bound
// repository out of habit must still get a row that outlives the transaction.
// Calling the un-rebound repository would prove nothing, since its handle is the
// pool either way.
func armSwitch(t *testing.T, repo ledgercompensation.Repository, ctx context.Context, aggregateID string, journalID uint64) string {
	t.Helper()

	repo = repo.WithTxFromContext(ctx)

	key := "checkout_payment:" + aggregateID
	if err := repo.RecordIntent(ctx, ledgercompensation.CreateParams{
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
		BizType:        account.JournalBizTypeCheckoutPayment,
		IdempotencyKey: key,
	}); err != nil {
		t.Fatalf("RecordIntent failed: %v", err)
	}
	if err := repo.RecordPosted(ctx, key, journalID); err != nil {
		t.Fatalf("RecordPosted failed: %v", err)
	}

	return key
}

// readStatus reads the committed status of a compensation row.
func readStatus(t *testing.T, db *gorm.DB, key string) (ledgercompensation.Status, *uint64) {
	t.Helper()

	var row compensationModel
	if err := db.Where("idempotency_key = ?", key).First(&row).Error; err != nil {
		t.Fatalf("failed to read compensation row: %v", err)
	}

	return ledgercompensation.Status(row.Status), row.JournalID
}

// TestRepository_SwitchSurvivesRollback is the invariant the whole
// design rests on.
//
// The compensation row must be written outside the business transaction, so a
// rollback leaves it standing; the settle must be written inside it, so the same
// rollback erases it. If either half joined the wrong transaction the mechanism
// silently stops working, with no local symptom — the checkout still returns an
// error and the money is still gone.
func TestRepository_SwitchSurvivesRollback(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	repo := New(db)
	txManager := commondb.NewTransactionManager(db)
	aggregateID := newAggregateID()

	// Open the business transaction, arm the switch inside that request, then
	// abandon the transaction — the pre-commit failure window.
	txCtx := txManager.Begin(context.Background())
	key := armSwitch(t, repo, txCtx, aggregateID, 777)

	if err := repo.WithTxFromContext(txCtx).MarkSettled(txCtx, key); err != nil {
		t.Fatalf("MarkSettled failed: %v", err)
	}
	if err := txManager.Rollback(txCtx); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	status, journalID := readStatus(t, db, key)
	if status != ledgercompensation.StatusPosted {
		t.Fatalf("expected the switch to survive the rollback as posted, got %s", status.String())
	}
	if journalID == nil || *journalID != 777 {
		t.Fatalf("expected journal id 777 to survive, got %#v", journalID)
	}
}

// TestRepository_SettleCommitsWithTheTransaction is the other half:
// on the happy path the switch must actually clear, or every successful checkout
// would leave an orphan for the cron to reverse.
func TestRepository_SettleCommitsWithTheTransaction(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	repo := New(db)
	txManager := commondb.NewTransactionManager(db)
	aggregateID := newAggregateID()

	txCtx := txManager.Begin(context.Background())
	key := armSwitch(t, repo, txCtx, aggregateID, 888)

	if err := repo.WithTxFromContext(txCtx).MarkSettled(txCtx, key); err != nil {
		t.Fatalf("MarkSettled failed: %v", err)
	}
	if err := txManager.Commit(txCtx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if status, _ := readStatus(t, db, key); status != ledgercompensation.StatusSettled {
		t.Fatalf("expected settled, got %s", status.String())
	}
}

// TestRepository_RetriedFlowReusesTheSwitch covers a customer
// retrying a failed checkout. The idempotency key is derived from the order, so
// the second attempt hits the same row; a unique-violation error there would
// break the retry outright.
func TestRepository_RetriedFlowReusesTheSwitch(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	repo := New(db)
	aggregateID := newAggregateID()
	ctx := context.Background()

	key := armSwitch(t, repo, ctx, aggregateID, 777)

	// Second attempt: the intent insert must be a no-op rather than a conflict.
	if err := repo.RecordIntent(ctx, ledgercompensation.CreateParams{
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
		BizType:        account.JournalBizTypeCheckoutPayment,
		IdempotencyKey: key,
	}); err != nil {
		t.Fatalf("second RecordIntent failed: %v", err)
	}

	var count int64
	if err := db.Model(&compensationModel{}).Where("aggregate_id = ?", aggregateID).Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one switch per posting, got %d", count)
	}
}

// TestRepository_ResolvedRowIsNotDraggedBack checks that a late
// RecordPosted cannot reopen a row the cron has already closed. Without the
// status guard, a slow request returning after its posting was reversed would
// mark the row posted again and the cron would reverse a second time.
func TestRepository_ResolvedRowIsNotDraggedBack(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	repo := New(db)
	aggregateID := newAggregateID()
	ctx := context.Background()

	key := armSwitch(t, repo, ctx, aggregateID, 777)

	var row compensationModel
	if err := db.Where("idempotency_key = ?", key).First(&row).Error; err != nil {
		t.Fatalf("failed to read row: %v", err)
	}
	if err := repo.Resolve(ctx, row.ID, ledgercompensation.StatusReversalRequested, "test"); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if err := repo.RecordPosted(ctx, key, 999); err == nil {
		t.Fatal("expected RecordPosted to refuse a row the cron already closed")
	}

	if status, _ := readStatus(t, db, key); status != ledgercompensation.StatusReversalRequested {
		t.Fatalf("expected the row to stay reversal_requested, got %s", status.String())
	}
}

// TestRepository_ClaimOrphansRespectsTheGracePeriod keeps the cron
// off rows belonging to requests that are still in flight. Claiming one early
// would reverse a payment that was about to succeed.
func TestRepository_ClaimOrphansRespectsTheGracePeriod(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	repo := New(db)
	ctx := context.Background()

	freshAggregate := newAggregateID()
	freshKey := armSwitch(t, repo, ctx, freshAggregate, 100)

	staleAggregate := newAggregateID()
	staleKey := armSwitch(t, repo, ctx, staleAggregate, 200)
	if err := db.Model(&compensationModel{}).
		Where("idempotency_key = ?", staleKey).
		Update("updated_at", time.Now().UTC().Add(-time.Hour)).Error; err != nil {
		t.Fatalf("failed to age the stale row: %v", err)
	}

	settledAggregate := newAggregateID()
	settledKey := armSwitch(t, repo, ctx, settledAggregate, 300)
	if err := repo.MarkSettled(ctx, settledKey); err != nil {
		t.Fatalf("MarkSettled failed: %v", err)
	}
	if err := db.Model(&compensationModel{}).
		Where("idempotency_key = ?", settledKey).
		Update("updated_at", time.Now().UTC().Add(-time.Hour)).Error; err != nil {
		t.Fatalf("failed to age the settled row: %v", err)
	}

	claimed, err := repo.ClaimOrphans(ctx, time.Now().UTC().Add(-time.Minute), 10)
	if err != nil {
		t.Fatalf("ClaimOrphans failed: %v", err)
	}

	if len(claimed) != 1 {
		t.Fatalf("expected only the aged open row, got %d: %#v", len(claimed), claimed)
	}
	if claimed[0].IdempotencyKey != staleKey {
		t.Fatalf("expected %q, got %q", staleKey, claimed[0].IdempotencyKey)
	}
	if claimed[0].BizType != account.JournalBizTypeCheckoutPayment {
		t.Fatalf("expected the biz type to round-trip, got %v", claimed[0].BizType)
	}
	_ = freshKey
}

// TestRepository_PruneKeepsOpenRows checks that retention never
// deletes unfinished business. An open switch is a posting nobody has accounted
// for, and age does not make it safe to forget.
func TestRepository_PruneKeepsOpenRows(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	repo := New(db)
	ctx := context.Background()

	openAggregate := newAggregateID()
	openKey := armSwitch(t, repo, ctx, openAggregate, 100)

	settledAggregate := newAggregateID()
	settledKey := armSwitch(t, repo, ctx, settledAggregate, 200)
	if err := repo.MarkSettled(ctx, settledKey); err != nil {
		t.Fatalf("MarkSettled failed: %v", err)
	}

	if err := db.Model(&compensationModel{}).
		Where("idempotency_key IN ?", []string{openKey, settledKey}).
		Update("updated_at", time.Now().UTC().Add(-30*24*time.Hour)).Error; err != nil {
		t.Fatalf("failed to age rows: %v", err)
	}

	n, err := repo.Prune(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly the resolved row to be pruned, got %d", n)
	}

	if status, _ := readStatus(t, db, openKey); status != ledgercompensation.StatusPosted {
		t.Fatalf("expected the open row to survive pruning, got %s", status.String())
	}
}
