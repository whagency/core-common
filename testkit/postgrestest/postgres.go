package postgrestest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gomigrate "github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/testcontainers/testcontainers-go"
	testPostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	postgresGorm "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	defaultImage          = "postgres:17"
	defaultDatabase       = "testdb"
	defaultUsername       = "test"
	defaultPassword       = "test"
	defaultDataDir        = "/var/lib/postgresql/data"
	defaultSchemaPrefix   = "itest"
	defaultCleanupTimeout = 30 * time.Second
	defaultMaxOpenConns   = 2
	defaultMaxIdleConns   = 1
)

// ContainerConfig describes the disposable PostgreSQL container used by repository tests.
type ContainerConfig struct {
	Image             string
	Database          string
	Username          string
	Password          string
	DataDir           string
	ConnectionOptions []string
}

// OpenConfig describes a per-test isolated PostgreSQL schema.
//
// At least one of MigrationsDir or MigrationsFS must be set. When both are
// set, files from MigrationsFS are written first, then files from MigrationsDir
// — service-level migrations therefore extend the shared FS migrations. The
// caller is responsible for ensuring filenames do not conflict.
type OpenConfig struct {
	// MigrationsDir is a path to a directory of golang-migrate SQL files.
	MigrationsDir string
	// MigrationsFS is an embedded or in-memory FS of golang-migrate SQL files.
	// Use this to reference a canonical migration embedded with //go:embed.
	MigrationsFS      fs.FS
	SchemaPrefix      string
	SkipMigrationLine func(line string) bool
	GormConfig        *gorm.Config
	MaxOpenConns      int
	MaxIdleConns      int
}

// Suite owns one disposable PostgreSQL container for a package-level test suite.
type Suite struct {
	container *testPostgres.PostgresContainer
	dsn       string
	counter   atomic.Uint64
}

// Start launches a disposable PostgreSQL container.
func Start(ctx context.Context, cfg ContainerConfig) (*Suite, error) {
	normalized := normalizeContainerConfig(cfg)
	container, err := testPostgres.Run(
		ctx,
		normalized.Image,
		testPostgres.WithDatabase(normalized.Database),
		testPostgres.WithUsername(normalized.Username),
		testPostgres.WithPassword(normalized.Password),
		testPostgres.BasicWaitStrategies(),
		testcontainers.WithTmpfs(map[string]string{
			normalized.DataDir: "rw",
		}),
	)
	if err != nil {
		return nil, err
	}

	dsn, err := container.ConnectionString(ctx, normalized.ConnectionOptions...)
	if err != nil {
		terminateCtx, cancel := context.WithTimeout(context.Background(), defaultCleanupTimeout)
		defer cancel()
		_ = testcontainers.TerminateContainer(container, testcontainers.StopContext(terminateCtx))
		return nil, err
	}

	return &Suite{container: container, dsn: dsn}, nil
}

// Terminate stops and removes the suite container.
func (s *Suite) Terminate(ctx context.Context) error {
	if s == nil || s.container == nil {
		return nil
	}
	return testcontainers.TerminateContainer(s.container, testcontainers.StopContext(ctx))
}

// DSN returns the root database connection string for the disposable container.
func (s *Suite) DSN() string {
	if s == nil {
		return ""
	}
	return s.dsn
}

// OpenGormDB creates a per-test schema, applies migrations, and opens a GORM connection.
func (s *Suite) OpenGormDB(t testing.TB, cfg OpenConfig) *gorm.DB {
	t.Helper()

	if s == nil || strings.TrimSpace(s.dsn) == "" {
		t.Fatal("postgres test suite is not initialized")
	}
	if strings.TrimSpace(cfg.MigrationsDir) == "" && cfg.MigrationsFS == nil {
		t.Fatal("postgres test: at least one of MigrationsDir or MigrationsFS is required")
	}

	rootDB, err := gorm.Open(postgresGorm.Open(s.dsn), gormConfig(cfg.GormConfig))
	if err != nil {
		t.Fatalf("gorm.Open root DB failed: %v", err)
	}
	rootSQLDB, err := rootDB.DB()
	if err != nil {
		t.Fatalf("rootDB.DB failed: %v", err)
	}
	configureConnectionPool(rootSQLDB, cfg)
	t.Cleanup(func() { _ = rootSQLDB.Close() })

	schemaName := s.nextSchemaName(cfg.SchemaPrefix)
	quotedSchema := quoteIdentifier(schemaName)
	if err := rootDB.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatalf("create schema %s failed: %v", schemaName, err)
	}
	t.Cleanup(func() {
		_ = rootDB.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE").Error
	})

	schemaDSN := withSearchPath(s.dsn, schemaName)
	applyMigrations(t, schemaDSN, cfg)

	schemaDB, err := gorm.Open(postgresGorm.Open(schemaDSN), gormConfig(cfg.GormConfig))
	if err != nil {
		t.Fatalf("gorm.Open schema DB failed: %v", err)
	}
	schemaSQLDB, err := schemaDB.DB()
	if err != nil {
		t.Fatalf("schemaDB.DB failed: %v", err)
	}
	configureConnectionPool(schemaSQLDB, cfg)
	t.Cleanup(func() { _ = schemaSQLDB.Close() })

	return schemaDB
}

// SkipDatabaseLevelMigrationLine skips migration statements that cannot be owned by per-test schemas.
func SkipDatabaseLevelMigrationLine(line string) bool {
	normalized := strings.ToUpper(strings.TrimSuffix(strings.Join(strings.Fields(line), " "), ";"))
	return normalized == "CREATE EXTENSION IF NOT EXISTS PG_STAT_STATEMENTS"
}

func normalizeContainerConfig(cfg ContainerConfig) ContainerConfig {
	if cfg.Image == "" {
		cfg.Image = defaultImage
	}
	if cfg.Database == "" {
		cfg.Database = defaultDatabase
	}
	if cfg.Username == "" {
		cfg.Username = defaultUsername
	}
	if cfg.Password == "" {
		cfg.Password = defaultPassword
	}
	if cfg.DataDir == "" {
		cfg.DataDir = defaultDataDir
	}
	if len(cfg.ConnectionOptions) == 0 {
		cfg.ConnectionOptions = []string{"sslmode=disable"}
	}
	return cfg
}

func (s *Suite) nextSchemaName(prefix string) string {
	if strings.TrimSpace(prefix) == "" {
		prefix = defaultSchemaPrefix
	}
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UTC().UnixNano(), s.counter.Add(1))
}

func applyMigrations(t testing.TB, dsn string, cfg OpenConfig) {
	t.Helper()

	sourceURL := prepareMigrationSource(t, cfg)
	migrator, err := gomigrate.New(sourceURL, dsn)
	if err != nil {
		t.Fatalf("create postgres migrator failed: %v", err)
	}
	defer func() {
		sourceErr, databaseErr := migrator.Close()
		if sourceErr != nil {
			t.Fatalf("close postgres migration source failed: %v", sourceErr)
		}
		if databaseErr != nil {
			t.Fatalf("close postgres migration database failed: %v", databaseErr)
		}
	}()

	if err := migrator.Up(); err != nil && !errors.Is(err, gomigrate.ErrNoChange) {
		t.Fatalf("apply postgres migrations failed: %v", err)
	}
}

func prepareMigrationSource(t testing.TB, cfg OpenConfig) string {
	t.Helper()

	targetDir := t.TempDir()
	if cfg.MigrationsFS != nil {
		writeMigrationsFromFS(t, cfg.MigrationsFS, targetDir, cfg.SkipMigrationLine)
	}
	if strings.TrimSpace(cfg.MigrationsDir) != "" {
		writeMigrationsFromDir(t, cfg.MigrationsDir, targetDir, cfg.SkipMigrationLine)
	}
	return "file://" + targetDir
}

func writeMigrationsFromFS(t testing.TB, migrFS fs.FS, targetDir string, skipLine func(string) bool) {
	t.Helper()

	entries, err := fs.ReadDir(migrFS, ".")
	if err != nil {
		t.Fatalf("read postgres migrations from FS failed: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		contents, err := fs.ReadFile(migrFS, entry.Name())
		if err != nil {
			t.Fatalf("read postgres migration %s from FS failed: %v", entry.Name(), err)
		}
		if skipLine != nil && strings.HasSuffix(entry.Name(), ".up.sql") {
			contents = []byte(stripMigrationLines(string(contents), skipLine))
		}
		targetPath := filepath.Join(targetDir, entry.Name())
		if err := os.WriteFile(targetPath, contents, 0o600); err != nil {
			t.Fatalf("write postgres migration %s failed: %v", targetPath, err)
		}
	}
}

func writeMigrationsFromDir(t testing.TB, sourceDir, targetDir string, skipLine func(string) bool) {
	t.Helper()

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatalf("read postgres migrations from %s failed: %v", sourceDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		path := filepath.Join(sourceDir, entry.Name())
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read postgres migration %s failed: %v", path, err)
		}
		if skipLine != nil && strings.HasSuffix(entry.Name(), ".up.sql") {
			contents = []byte(stripMigrationLines(string(contents), skipLine))
		}
		targetPath := filepath.Join(targetDir, entry.Name())
		if err := os.WriteFile(targetPath, contents, 0o600); err != nil {
			t.Fatalf("write postgres migration %s failed: %v", targetPath, err)
		}
	}
}

func stripMigrationLines(migrationSQL string, skipLine func(line string) bool) string {
	lines := strings.Split(migrationSQL, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		if skipLine(line) {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func withSearchPath(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		return dsn + separator + "search_path=" + schema
	}
	return dsn + " search_path=" + schema
}

func gormConfig(cfg *gorm.Config) *gorm.Config {
	if cfg != nil {
		return cfg
	}
	return &gorm.Config{}
}

type connectionPool interface {
	SetMaxOpenConns(n int)
	SetMaxIdleConns(n int)
}

func configureConnectionPool(db connectionPool, cfg OpenConfig) {
	maxOpenConns := cfg.MaxOpenConns
	if maxOpenConns <= 0 {
		maxOpenConns = defaultMaxOpenConns
	}
	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = defaultMaxIdleConns
	}
	if maxIdleConns > maxOpenConns {
		maxIdleConns = maxOpenConns
	}

	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
}
