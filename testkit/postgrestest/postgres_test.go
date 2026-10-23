package postgrestest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeContainerConfigDefaults(t *testing.T) {
	cfg := normalizeContainerConfig(ContainerConfig{})

	if cfg.Image != defaultImage {
		t.Fatalf("expected image %q, got %q", defaultImage, cfg.Image)
	}
	if len(cfg.ConnectionOptions) != 1 || cfg.ConnectionOptions[0] != "sslmode=disable" {
		t.Fatalf("unexpected connection options: %#v", cfg.ConnectionOptions)
	}
}

func TestSkipDatabaseLevelMigrationLine(t *testing.T) {
	if !SkipDatabaseLevelMigrationLine("CREATE EXTENSION IF NOT EXISTS pg_stat_statements;") {
		t.Fatal("expected pg_stat_statements extension line to be skipped")
	}
	if SkipDatabaseLevelMigrationLine("CREATE TABLE tier_policies (id bigint);") {
		t.Fatal("expected table creation line to be kept")
	}
}

func TestPrepareMigrationSourceCopiesAndSanitizesUpMigrations(t *testing.T) {
	sourceDir := t.TempDir()
	up := filepath.Join(sourceDir, "000001_init.up.sql")
	down := filepath.Join(sourceDir, "000001_init.down.sql")

	if err := os.WriteFile(up, []byte("CREATE EXTENSION IF NOT EXISTS pg_stat_statements;\nCREATE TABLE things (id bigint);\n"), 0o600); err != nil {
		t.Fatalf("write up migration failed: %v", err)
	}
	if err := os.WriteFile(down, []byte("DROP TABLE things;\n"), 0o600); err != nil {
		t.Fatalf("write down migration failed: %v", err)
	}

	sourceURL := prepareMigrationSource(t, OpenConfig{
		MigrationsDir:     sourceDir,
		SkipMigrationLine: SkipDatabaseLevelMigrationLine,
	})
	targetDir := strings.TrimPrefix(sourceURL, "file://")

	sanitized, err := os.ReadFile(filepath.Join(targetDir, "000001_init.up.sql"))
	if err != nil {
		t.Fatalf("read sanitized migration failed: %v", err)
	}
	if strings.Contains(string(sanitized), "pg_stat_statements") {
		t.Fatalf("expected extension line to be removed, got %s", string(sanitized))
	}
	if !strings.Contains(string(sanitized), "CREATE TABLE things") {
		t.Fatalf("expected table statement to stay, got %s", string(sanitized))
	}
}

func TestConfigureConnectionPoolDefaultsAndCapsIdleConns(t *testing.T) {
	pool := &fakeConnectionPool{}

	configureConnectionPool(pool, OpenConfig{
		MaxOpenConns: 3,
		MaxIdleConns: 10,
	})

	if pool.maxOpenConns != 3 {
		t.Fatalf("expected max open conns 3, got %d", pool.maxOpenConns)
	}
	if pool.maxIdleConns != 3 {
		t.Fatalf("expected max idle conns to be capped to 3, got %d", pool.maxIdleConns)
	}

	defaultPool := &fakeConnectionPool{}
	configureConnectionPool(defaultPool, OpenConfig{})
	if defaultPool.maxOpenConns != defaultMaxOpenConns {
		t.Fatalf("expected default max open conns %d, got %d", defaultMaxOpenConns, defaultPool.maxOpenConns)
	}
	if defaultPool.maxIdleConns != defaultMaxIdleConns {
		t.Fatalf("expected default max idle conns %d, got %d", defaultMaxIdleConns, defaultPool.maxIdleConns)
	}
}

type fakeConnectionPool struct {
	maxOpenConns int
	maxIdleConns int
}

func (p *fakeConnectionPool) SetMaxOpenConns(n int) {
	p.maxOpenConns = n
}

func (p *fakeConnectionPool) SetMaxIdleConns(n int) {
	p.maxIdleConns = n
}
