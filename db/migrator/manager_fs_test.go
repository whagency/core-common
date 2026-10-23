package migrator_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"testing/fstest"

	"github.com/whagency/core-common/db/migrator"
	outboxmigrations "github.com/whagency/core-common/outbox/postgres/migrations"
	"github.com/whagency/core-common/testkit/postgrestest"
	postgresGorm "gorm.io/driver/postgres"
	"gorm.io/gorm"
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

// serviceMigrations stands in for a service's own migrations. Version 1 is
// deliberately the same number the shared source starts at, because that
// collision is exactly what separate history tables make irrelevant.
var serviceMigrations = fstest.MapFS{
	"000001_accounts.up.sql":   {Data: []byte(`CREATE TABLE accounts (id BIGINT PRIMARY KEY);`)},
	"000001_accounts.down.sql": {Data: []byte(`DROP TABLE accounts;`)},
}

// isolatedDSN gives the test its own schema so migrations do not collide with
// other tests running against the same container.
func isolatedDSN(t *testing.T) (string, *gorm.DB) {
	t.Helper()

	schema := fmt.Sprintf("migtest_%d", os.Getpid()+len(t.Name()))
	root, err := gorm.Open(postgresGorm.Open(suite.DSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	if err := root.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error; err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if err := root.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _ = root.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error })

	// The suite hands out a URL-form DSN, so the option is a query parameter.
	dsn := suite.DSN() + "&search_path=" + schema
	db, err := gorm.Open(postgresGorm.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open schema db: %v", err)
	}
	return dsn, db
}

func applyUp(t *testing.T, cfg migrator.Config) {
	t.Helper()

	m, err := migrator.New(cfg)
	if err != nil {
		t.Fatalf("new migrator: %v", err)
	}
	if _, err := m.Execute(context.Background(), migrator.ExecuteConfig{Command: migrator.CommandUp}); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
}

func tableExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()

	var exists bool
	if err := db.Raw(
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = ? AND table_schema = current_schema())",
		name,
	).Scan(&exists).Error; err != nil {
		t.Fatalf("check %s: %v", name, err)
	}
	return exists
}

// A shared schema is applied straight from the package that owns it, instead of
// being copied into the service's migration directory where the copy drifts.
//
// Both sources start at version 1. Merged into one history they would collide;
// with a history table each they are simply unrelated, which is the point.
func TestManager_SharedAndServiceSourcesCoexist(t *testing.T) {
	dsn, db := isolatedDSN(t)

	applyUp(t, migrator.Config{
		Driver:          "postgres",
		DSN:             dsn,
		MigrationsFS:    serviceMigrations,
		MigrationsTable: "schema_migrations",
	})
	applyUp(t, migrator.Config{
		Driver:          "postgres",
		DSN:             dsn,
		MigrationsFS:    outboxmigrations.FS,
		MigrationsTable: "schema_migrations_common",
	})

	for _, table := range []string{"accounts", "outbox_messages"} {
		if !tableExists(t, db, table) {
			t.Errorf("table %q missing: the two migration sources did not both apply", table)
		}
	}

	// Each source tracks its own version, in its own table, with no relation
	// between the two numbers. The shared source sits in a reserved high band
	// only so it can also be merged into one directory by test helpers; through
	// the migrator the separate history tables already make collisions
	// impossible.
	for _, tc := range []struct {
		historyTable string
		wantVersion  int64
	}{
		{historyTable: "schema_migrations", wantVersion: 1},
		{historyTable: "schema_migrations_common", wantVersion: 9000001},
	} {
		var version int64
		if err := db.Raw("SELECT version FROM " + tc.historyTable).Scan(&version).Error; err != nil {
			t.Fatalf("read %s: %v", tc.historyTable, err)
		}
		if version != tc.wantVersion {
			t.Errorf("%s version = %d, want %d", tc.historyTable, version, tc.wantVersion)
		}
	}
}

// Applying the shared source twice must be a no-op, because every replica runs
// migrations on start.
func TestManager_SharedSourceIsIdempotent(t *testing.T) {
	dsn, db := isolatedDSN(t)

	cfg := migrator.Config{
		Driver:          "postgres",
		DSN:             dsn,
		MigrationsFS:    outboxmigrations.FS,
		MigrationsTable: "schema_migrations_common",
	}
	applyUp(t, cfg)
	applyUp(t, cfg)

	if !tableExists(t, db, "outbox_messages") {
		t.Error("outbox_messages missing after a second up")
	}
}
