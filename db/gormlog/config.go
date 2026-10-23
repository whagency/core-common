package gormlog

import (
	"time"

	gormlogger "gorm.io/gorm/logger"
)

type Config struct {
	LogLevel                  string        `envconfig:"DB_LOG_LEVEL" default:"warn"` // silent, error, warn, info
	SlowThreshold             time.Duration `envconfig:"DB_LOG_SLOW_THRESHOLD" default:"100ms"`
	VerySlowThreshold         time.Duration `envconfig:"DB_LOG_VERY_SLOW_THRESHOLD" default:"1s"`
	IgnoreRecordNotFoundError bool          `envconfig:"DB_LOG_IGNORE_RECORD_NOT_FOUND" default:"true"`

	DisableLogsWhenTracing bool `envconfig:"DB_LOG_DISABLE_WHEN_TRACING" default:"false"`
	LogSlowQueries         bool `envconfig:"DB_LOG_SLOW" default:"false"`
	LogSQL                 bool `envconfig:"DB_LOG_SQL" default:"false"`

	TraceStatementAlways  bool `envconfig:"DB_LOG_TRACE_STATEMENT_ALWAYS" default:"false"`
	TraceStatementOnError bool `envconfig:"DB_LOG_TRACE_STATEMENT_ON_ERROR" default:"true"`
	TraceStatementOnSlow  bool `envconfig:"DB_LOG_TRACE_STATEMENT_ON_SLOW" default:"true"`

	LogVerySlowEvenWhenTracing bool `envconfig:"DB_LOG_VERY_SLOW_WHEN_TRACING" default:"true"`
	LogErrorEvenWhenTracing    bool `envconfig:"DB_LOG_ERROR_WHEN_TRACING" default:"false"`

	DBSystem string `envconfig:"DB_LOG_SYSTEM"`
	DBName   string `envconfig:"DB_LOG_NAME"`
}

// parseGormLogLevel parses a string into a gormlogger.LogLevel.
func parseGormLogLevel(levelStr string) gormlogger.LogLevel {
	switch levelStr {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "warn":
		return gormlogger.Warn
	case "info":
		return gormlogger.Info
	default:
		return gormlogger.Warn
	}
}
