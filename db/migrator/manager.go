package migrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	dbcommon "github.com/whagency/core-common/db"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	// _defaultMigrationsDir is the default root folder for migration files.
	_defaultMigrationsDir = "data/db/migrations/"
	// _defaultMigrationsTable is the default table used to track migration state.
	_defaultMigrationsTable = "schema_migrations"
)

// Command describes an action that can be executed by the migration manager.
type Command string

// String returns the string representation of the command.
func (c Command) String() string {
	return string(c)
}

const (
	// CommandUp applies all pending migrations.
	CommandUp Command = "up"
	// CommandDown rolls back the last applied migration.
	CommandDown Command = "down"
	// CommandSteps moves forward or backward a fixed number of migrations.
	CommandSteps Command = "steps"
	// CommandVersion prints the current migration version and dirty flag.
	CommandVersion Command = "version"
	// CommandForce sets the migration version without running migrations.
	CommandForce Command = "force"
	// CommandDrop drops everything in the database.
	CommandDrop Command = "drop"
)

var _validCommands = map[Command]struct{}{
	CommandUp:      {},
	CommandDown:    {},
	CommandSteps:   {},
	CommandVersion: {},
	CommandForce:   {},
	CommandDrop:    {},
}

// ErrUnknownCommand is returned when attempting to execute an unsupported command.
var ErrUnknownCommand = errors.New("unknown command")

// Config groups options for configuring a migration Manager.
type Config struct {
	// Driver is the database driver identifier (postgres or mysql).
	Driver string
	// DSN is the database connection string.
	DSN string
	// MigrationsDir is the root directory that contains driver specific
	// migrations, laid out as <MigrationsDir>/<Driver>/*.sql.
	//
	// Ignored when MigrationsFS is set.
	MigrationsDir string

	// MigrationsFS is an embedded migration source whose root contains the SQL
	// files directly. Use fs.Sub to point at a subdirectory.
	//
	// It exists so a shared schema — the outbox table, for example — can be
	// applied from the package that owns it instead of being copied into every
	// service's migration directory, where the copies inevitably drift.
	//
	// A shared source must be applied as its own Manager with its own
	// MigrationsTable, not merged into the service's migrations. Two sources
	// sharing one history table would fight over the version number; separate
	// tables let each evolve independently and remove version collisions
	// entirely.
	MigrationsFS fs.FS
	// MigrationsTable is the table used to store migration history.
	MigrationsTable string
}

// Manager coordinates migration execution for the configured driver and DSN.
type Manager struct {
	cfg Config
}

// New validates configuration and returns a Manager instance ready to run migrations.
func New(cfg Config) (*Manager, error) {
	if cfg.Driver == "" {
		return nil, fmt.Errorf("[migrator][New][err: driver is required]")
	}
	cfg.Driver = strings.ToLower(cfg.Driver)

	if cfg.DSN == "" {
		return nil, fmt.Errorf("[migrator][New][err: dsn is required]")
	}
	if cfg.MigrationsFS == nil && cfg.MigrationsDir == "" {
		cfg.MigrationsDir = _defaultMigrationsDir
	}
	if cfg.MigrationsTable == "" {
		cfg.MigrationsTable = _defaultMigrationsTable
	}

	return &Manager{cfg: cfg}, nil
}

// Status describes the current migration version and whether the schema is dirty.
type Status struct {
	Version uint
	Dirty   bool
}

// up runs all available up migrations until the schema is fully migrated.
func (m *Manager) up(ctx context.Context) error {
	return m.withMigrator(ctx, func(migrator *migrate.Migrate) error {
		if err := migrator.Up(); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				return nil
			}
			return fmt.Errorf("[up][err: %w]", err)
		}
		return nil
	})
}

// down rolls back the latest applied migration.
func (m *Manager) down(ctx context.Context) error {
	return m.steps(ctx, -1)
}

// steps executes a fixed number of migration steps. Negative values roll back.
func (m *Manager) steps(ctx context.Context, steps int) error {
	if steps == 0 {
		return nil
	}

	return m.withMigrator(ctx, func(migrator *migrate.Migrate) error {
		if err := migrator.Steps(steps); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				return nil
			}
			return fmt.Errorf("[steps][err: %w]", err)
		}
		return nil
	})
}

// status returns the current migration version along with its dirty flag.
func (m *Manager) status(ctx context.Context) (Status, error) {
	var state Status
	err := m.withMigrator(ctx, func(migrator *migrate.Migrate) error {
		version, dirty, err := migrator.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			state = Status{Version: 0, Dirty: false}
			return nil
		}
		if err != nil {
			return fmt.Errorf("[status][err: %w]", err)
		}
		state = Status{Version: version, Dirty: dirty}
		return nil
	})
	if err != nil {
		return Status{}, fmt.Errorf("[status][err: %w]", err)
	}
	return state, nil
}

// force sets the migration version without running migrations. This is useful for recovering from a failed migration.
func (m *Manager) force(ctx context.Context, version int) error {
	return m.withMigrator(ctx, func(mi *migrate.Migrate) error {
		return mi.Force(version)
	})
}

// drop drops everything in the database.
func (m *Manager) drop(ctx context.Context) error {
	return m.withMigrator(ctx, func(mi *migrate.Migrate) error {
		return mi.Drop()
	})
}

// ExecuteConfig encapsulates the desired migration command and optional arguments.
type ExecuteConfig struct {
	Command Command
	Steps   int
	Version int
}

// Execute runs the configured migration command and returns the resulting status (if any).
// For commands other than CommandVersion, the returned Status will be zero-valued.
func (m *Manager) Execute(ctx context.Context, cfg ExecuteConfig) (status Status, err error) {
	switch cfg.Command {
	case CommandUp:
		if err = m.up(ctx); err != nil {
			return status, fmt.Errorf("[migrator][Manager:Execute]%w", err)
		}
		return
	case CommandDown:
		if err = m.down(ctx); err != nil {
			return status, fmt.Errorf("[migrator][Manager:Execute]%w", err)
		}
		return
	case CommandSteps:
		if cfg.Steps == 0 {
			return
		}

		if err = m.steps(ctx, cfg.Steps); err != nil {
			return status, fmt.Errorf("[migrator][Manager:Execute]%w", err)
		}
		return
	case CommandVersion:
		status, err = m.status(ctx)
		if err != nil {
			return status, fmt.Errorf("[migrator][Manager:Execute]%w", err)
		}
		return
	case CommandForce:
		if cfg.Version < -1 {
			return status, fmt.Errorf("[migrator][Manager:Execute][err: version must be >= -1]")
		}

		if err = m.force(ctx, cfg.Version); err != nil {
			return status, fmt.Errorf("[migrator][Manager:Execute]%w", err)
		}
		return
	case CommandDrop:
		if err = m.drop(ctx); err != nil {
			return status, fmt.Errorf("[migrator][Manager:Execute]%w", err)
		}
		return
	default:
		return status, fmt.Errorf("[migrator][Manager:Execute][err: invalid command]")
	}
}

// ParseCommand normalizes a raw command string and validates that it is supported.
func ParseCommand(command string) (Command, error) {
	normalized := Command(strings.ToLower(strings.TrimSpace(command)))
	if normalized == "" {
		normalized = CommandUp
	}

	if _, ok := _validCommands[normalized]; !ok {
		return "", fmt.Errorf("[command: %s][err: %w]", command, ErrUnknownCommand)
	}

	return normalized, nil
}

// withMigrator creates a migrator, defers cleanup, and executes the provided function.
func (m *Manager) withMigrator(ctx context.Context, fn func(*migrate.Migrate) error) error {
	migrator, closer, err := m.newMigrator(ctx)
	if err != nil {
		return err
	}
	defer closer()
	return fn(migrator)
}

// newMigrator constructs a golang-migrate instance bound to the configured database.
// openMigrate builds the migrate instance from whichever source is configured.
func (m *Manager) openMigrate(driver database.Driver) (*migrate.Migrate, error) {
	if m.cfg.MigrationsFS != nil {
		src, err := iofs.New(m.cfg.MigrationsFS, ".")
		if err != nil {
			return nil, fmt.Errorf("[openMigrate][err: %w]", err)
		}
		return migrate.NewWithInstance("iofs", src, m.cfg.Driver, driver)
	}

	sourceURL := fmt.Sprintf("file://%s", filepath.Join(m.cfg.MigrationsDir, m.cfg.Driver))
	return migrate.NewWithDatabaseInstance(sourceURL, m.cfg.Driver, driver)
}

func (m *Manager) newMigrator(ctx context.Context) (*migrate.Migrate, func(), error) {
	sqlDriverName, err := dbcommon.SqlDriverName(m.cfg.Driver)
	if err != nil {
		return nil, nil, fmt.Errorf("[newMigrator]%w", err)
	}

	db, err := sql.Open(sqlDriverName, m.cfg.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("[newMigrator]%w", err)
	}

	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("[newMigrator]%w", err)
	}

	var driver database.Driver
	switch m.cfg.Driver {
	case dbcommon.DriverPostgres:
		driver, err = postgres.WithInstance(db, &postgres.Config{MigrationsTable: m.cfg.MigrationsTable})
	case dbcommon.DriverMySQL:
		driver, err = mysql.WithInstance(db, &mysql.Config{MigrationsTable: m.cfg.MigrationsTable})
	default:
		db.Close()
		return nil, nil, fmt.Errorf("[newMigrator]%w", dbcommon.ErrUnsupportedDriver)
	}
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("[newMigrator]%w", err)
	}

	migrator, err := m.openMigrate(driver)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("[newMigrator]%w", err)
	}

	closer := func() {
		_, _ = migrator.Close()
		_ = db.Close()
	}

	return migrator, closer, nil
}
