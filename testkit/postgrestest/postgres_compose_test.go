package postgrestest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	outboxmigrations "github.com/whagency/core-common/outbox/postgres/migrations"
	"github.com/whagency/core-common/testkit/postgrestest"
)

var suite *postgrestest.Suite

func TestMain(m *testing.M) {
	var err error
	suite, err = postgrestest.Start(context.Background(), postgrestest.ContainerConfig{})
	if err != nil {
		panic("postgrestest: start: " + err.Error())
	}
	code := m.Run()
	_ = suite.Terminate(context.Background())
	os.Exit(code)
}

// A service applies its own migrations alongside the shared ones. Service
// migrations start at 1, so the shared set has to sit in a reserved high band —
// golang-migrate keys migrations by their integer prefix and refuses duplicates,
// which would otherwise make this combination impossible to use at all.
func TestOpenGormDB_CombinesMigrationsFSAndDir(t *testing.T) {
	t.Parallel()

	serviceDir := t.TempDir()
	writeMigration(t, serviceDir, "000001_accounts.up.sql", `
		CREATE TABLE IF NOT EXISTS accounts (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			name TEXT NOT NULL
		);
	`)
	writeMigration(t, serviceDir, "000001_accounts.down.sql", `DROP TABLE IF EXISTS accounts;`)

	db := suite.OpenGormDB(t, postgrestest.OpenConfig{
		MigrationsFS:      outboxmigrations.FS,
		MigrationsDir:     serviceDir,
		SkipMigrationLine: postgrestest.SkipDatabaseLevelMigrationLine,
	})

	for _, table := range []string{"outbox_messages", "accounts"} {
		var exists bool
		if err := db.Raw(
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = ?)", table,
		).Scan(&exists).Error; err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %q was not created — combined migration sources did not both apply", table)
		}
	}
}

func writeMigration(t testing.TB, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
