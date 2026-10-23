package db

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrUnsupportedDriver is returned when an unknown driver identifier is provided.
var ErrUnsupportedDriver = errors.New("unsupported driver")

// ErrUnsupportedSSLMode is returned when an unknown sslmode value is provided.
var ErrUnsupportedSSLMode = errors.New("unsupported sslmode")

const (
	// DriverPostgres represents the postgres migration driver name.
	DriverPostgres     = "postgres"
	DriverNamePostgres = "pgx"

	// DriverMySQL represents the MySQL migration driver name.
	DriverMySQL     = "mysql"
	DriverNameMySQL = "mysql"
)

var DriverNames = map[string]string{
	DriverPostgres: DriverNamePostgres,
	DriverMySQL:    DriverNameMySQL,
}

// SqlDriverName maps the configured driver to the sql package driver name.
func SqlDriverName(driver string) (string, error) {
	if name, ok := DriverNames[driver]; ok {
		return name, nil
	}
	return "", fmt.Errorf("[sqlDriverName][driver:%s][err: %w]", driver, ErrUnsupportedDriver)
}

// PostgreSQL sslmode values, as accepted by libpq in the connection string.
const (
	SSLModeDisable    = "disable"
	SSLModeAllow      = "allow"
	SSLModePrefer     = "prefer"
	SSLModeRequire    = "require"
	SSLModeVerifyCA   = "verify-ca"
	SSLModeVerifyFull = "verify-full"
)

// PostgresSSLModes lists the sslmode values accepted by the PostgreSQL driver.
var PostgresSSLModes = []string{
	SSLModeDisable,
	SSLModeAllow,
	SSLModePrefer,
	SSLModeRequire,
	SSLModeVerifyCA,
	SSLModeVerifyFull,
}

// NormalizePostgresSSLMode validates the configured sslmode and returns it in
// the lower-case form libpq expects.
//
// An unrecognised value is rejected rather than passed through: the DSN is
// assembled by string formatting, so a typo would otherwise reach the driver
// and fail at connect time, or — worse for a value whose whole purpose is
// transport security — be read as something weaker than intended.
func NormalizePostgresSSLMode(sslMode string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(sslMode))
	if slices.Contains(PostgresSSLModes, normalized) {
		return normalized, nil
	}

	return "", fmt.Errorf("[normalizePostgresSSLMode][sslmode:%s][expected: %s][err: %w]",
		sslMode, strings.Join(PostgresSSLModes, "|"), ErrUnsupportedSSLMode)
}

// SQLSTATE codes for deadlocks
const (
	sqlstateLockNotAvailable = "55P03" // lock_not_available (NOWAIT)
	sqlstateDeadlockDetected = "40P01" // deadlock_detected
)

// asPgx returns the pgconn.PgError if err is a pgx error, or nil and false if not.
func asPgx(err error) (*pgconn.PgError, bool) {
	var pgxErr *pgconn.PgError
	ok := errors.As(err, &pgxErr)
	return pgxErr, ok
}

// IsRowLockConflict returns true if the error is a row lock conflict.
func IsRowLockConflict(err error) bool {
	if err == nil {
		return false
	}
	if pgxErr, ok := asPgx(err); ok {
		switch pgxErr.Code {
		case sqlstateLockNotAvailable, sqlstateDeadlockDetected:
			return true
		}
	}
	return false
}
