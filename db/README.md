# DB

Database integration package providing transaction management, advanced GORM logging with tracing, and schema migrations.

## Features

- 💼 **Transaction Management**: Easy-to-use `TransactionManager` that propagates transactions via `context.Context`.
- 📊 **Advanced GORM Logger**: Custom logger for GORM with:
    - OpenTelemetry (tracing) integration.
    - Configurable slow query thresholds.
    - SQL statement logging policies.
    - Automatic span creation for DB operations.
- 🚀 **Schema Migrations**: Wrapper around `golang-migrate` for PostgreSQL and MySQL.
- 🛠️ **Driver Utilities**: Support for `pgx` and `mysql` drivers with unified naming.

## Installation

```bash
go get github.com/whagency/core-common/db
```

## Quick Start

### 1. GORM with Advanced Logger

The `gormlog` package provides a logger that integrates with both `logger` and `tracer` packages.

```go
import (
    "github.com/whagency/core-common/db/gormlog"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

cfg := gormlog.Config{
    LogLevel:                  "info",
    SlowThreshold:             200 * time.Millisecond,
    LogSQL:                    true,
    TraceStatementAlways:      true,
    DBSystem:                  "postgresql",
    DBName:                    "user_service",
}

dbLogger := gormlog.NewDBLogger(cfg)

db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
    Logger: dbLogger,
})
```

### 2. Transaction Management

Use `TransactionManager` to handle transactions across different service layers without passing the DB object directly.

```go
import "github.com/whagency/core-common/db"

txManager := db.NewTransactionManager(dbConn)

// In your service/usecase:
ctx := txManager.Begin(ctx)
defer func() {
    if r := recover(); r != nil {
        _ = txManager.Rollback(ctx)
        panic(r)
    }
}()

err := repository.CreateUser(ctx, user)
if err != nil {
    _ = txManager.Rollback(ctx)
    return err
}

return txManager.Commit(ctx)

// Inside repository:
func (r *UserRepository) CreateUser(ctx context.Context, user *User) error {
    tx := db.GetTxFromContext(ctx)
    if tx == nil {
        tx = r.defaultDB // fallback to non-transactional if needed
    }
    return tx.Create(user).Error
}
```

*Note: TransactionManager methods `Begin`, `Commit`, `Rollback` are used to manage transactions via context.*

### 3. Running Migrations

```go
import "github.com/whagency/core-common/db/migrator"

m, err := migrator.New(migrator.Config{
    Driver:        "postgres",
    DSN:           "postgres://user:pass@localhost:5432/db",
    MigrationsDir: "data/db/migrations/",
})

// Execute "up" migrations
status, err := m.Execute(ctx, migrator.ExecuteConfig{
    Command: migrator.CommandUp,
})
```

## Configuration

### GORM Logger Config (`gormlog.Config`)

| Field | Env Var | Default | Description |
|-------|---------|---------|-------------|
| `LogLevel` | `DB_LOG_LEVEL` | `warn` | silent, error, warn, info |
| `SlowThreshold` | `DB_LOG_SLOW_THRESHOLD` | `100ms` | Threshold for slow query logging |
| `LogSQL` | `DB_LOG_SQL` | `false` | Include SQL statements in logs |
| `DisableLogsWhenTracing` | `DB_LOG_DISABLE_WHEN_TRACING` | `false` | Skip normal logs if tracing is active |
| `TraceStatementAlways` | `DB_LOG_TRACE_STATEMENT_ALWAYS` | `false` | Always attach SQL to spans |

### Migrator Config (`migrator.Config`)

| Field | Description |
|-------|-------------|
| `Driver` | `postgres` or `mysql` |
| `DSN` | Connection string |
| `MigrationsDir` | Path to migration files (default: `data/db/migrations/`) |
| `MigrationsTable` | Table name for tracking (default: `schema_migrations`) |

## API Reference

### Transaction Manager
- `NewTransactionManager(db *gorm.DB) *TransactionManager`
- `(tm *TransactionManager) Begin(ctx context.Context) context.Context`
- `(tm *TransactionManager) Commit(ctx context.Context) error`
- `(tm *TransactionManager) Rollback(ctx context.Context) error`
- `GetTxFromContext(ctx context.Context) *gorm.DB`

### Migrator
- `ParseCommand(command string) (Command, error)`
- `(m *Manager) Execute(ctx context.Context, cfg ExecuteConfig) (Status, error)`

### Driver and connection settings
- `SqlDriverName(driver string) (string, error)`
- `NormalizePostgresSSLMode(sslMode string) (string, error)`
- `PostgresSSLModes []string`

`NormalizePostgresSSLMode` validates a configured `DB_SSL_MODE` and returns the
lower-case value libpq expects. Services call it from `loadDBConfig` so an
unrecognised value stops startup: the DSN is assembled by string formatting, so
a typo would otherwise reach the driver unchecked. It does not pick a default —
services declare `default:"disable"` on the config field.

## License

MIT
