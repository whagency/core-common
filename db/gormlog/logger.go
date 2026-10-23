package gormlog

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tracer"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	_defaultSlowQueryThreshold     = 100 * time.Millisecond
	_defaultVerySlowQueryThreshold = 1 * time.Second
)

type DBLogger struct {
	LogLevel                  gormlogger.LogLevel
	SlowThreshold             time.Duration
	VerySlowThreshold         time.Duration
	IgnoreRecordNotFoundError bool

	DBSystem string // "postgresql", "mysql", "sqlite", ...
	DBName   string // optional

	// Logging policy
	DisableLogsWhenTracing bool // if tracer.Enabled() -> skip normal logs
	LogSlowQueries         bool // log slow queries (Warn)
	LogSQL                 bool // include SQL statement in logs

	// Tracing SQL policy
	TraceStatementAlways  bool // always attach db.statement + rows to span
	TraceStatementOnError bool // attach statement on error
	TraceStatementOnSlow  bool // attach statement on slow/very slow

	// Optional: still log some signals even when DisableLogsWhenTracing=true
	LogVerySlowEvenWhenTracing bool
	LogErrorEvenWhenTracing    bool
}

// NewDBLogger creates a new DBLogger with default settings
func NewDBLogger(cfg Config) gormlogger.Interface {
	l := &DBLogger{
		LogLevel:                  parseGormLogLevel(cfg.LogLevel),
		SlowThreshold:             cfg.SlowThreshold,
		VerySlowThreshold:         cfg.VerySlowThreshold,
		IgnoreRecordNotFoundError: cfg.IgnoreRecordNotFoundError,

		DBSystem: cfg.DBSystem,
		DBName:   cfg.DBName,

		DisableLogsWhenTracing: cfg.DisableLogsWhenTracing,
		LogSlowQueries:         cfg.LogSlowQueries,
		LogSQL:                 cfg.LogSQL,

		TraceStatementAlways:  cfg.TraceStatementAlways,
		TraceStatementOnError: cfg.TraceStatementOnError,
		TraceStatementOnSlow:  cfg.TraceStatementOnSlow,

		LogVerySlowEvenWhenTracing: cfg.LogVerySlowEvenWhenTracing,
		LogErrorEvenWhenTracing:    cfg.LogErrorEvenWhenTracing,
	}

	if l.SlowThreshold <= 0 {
		l.SlowThreshold = _defaultSlowQueryThreshold
	}
	if l.VerySlowThreshold <= 0 {
		l.VerySlowThreshold = _defaultVerySlowQueryThreshold
	}

	return l
}

// ---- gorm logger interface ----

// LogMode set log level
func (l *DBLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	newLogger := *l
	newLogger.LogLevel = level
	return &newLogger
}

// Info print information log.
func (l *DBLogger) Info(ctx context.Context, str string, args ...interface{}) {
	if l.LogLevel < gormlogger.Info {
		return
	}
	logger.LogCtx(ctx).Infof(str, args...)
}

// Warn print warning log.
func (l *DBLogger) Warn(ctx context.Context, str string, args ...interface{}) {
	if l.LogLevel < gormlogger.Warn {
		return
	}
	logger.LogCtx(ctx).Warnf(str, args...)
}

// Error print error log.
func (l *DBLogger) Error(ctx context.Context, str string, args ...interface{}) {
	if l.LogLevel < gormlogger.Error {
		return
	}
	logger.LogCtx(ctx).Errorf(str, args...)
}

// Trace print sql trace log.
func (l *DBLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	// If both logging and tracing are effectively off, do nothing.
	if l.LogLevel <= gormlogger.Silent && !tracer.Enabled() {
		return
	}

	elapsed := time.Since(begin)

	// NotFound policy: if ignored -> treat as non-error everywhere
	isNotFound := errors.Is(err, gorm.ErrRecordNotFound)
	isIgnoredNotFound := isNotFound && l.IgnoreRecordNotFoundError
	isErr := err != nil && !isIgnoredNotFound

	isSlow := l.SlowThreshold != 0 && elapsed > l.SlowThreshold
	isVerySlow := l.VerySlowThreshold != 0 && elapsed > l.VerySlowThreshold

	var (
		sql   string
		rows  int64
		built bool
	)
	getSQL := func() (string, int64) {
		if !built {
			sql, rows = fc()
			built = true
		}
		return sql, rows
	}

	// ---- Tracing ----
	if tracer.Enabled() {
		attrs := []attribute.KeyValue{
			attribute.Int64("db.duration_ms", elapsed.Milliseconds()),
		}
		if l.DBSystem != "" {
			attrs = append(attrs, attribute.String("db.system", l.DBSystem))
		}
		if l.DBName != "" {
			attrs = append(attrs, attribute.String("db.name", l.DBName))
		}

		spanName := "DB"

		needStmt := l.TraceStatementAlways ||
			(isErr && l.TraceStatementOnError) ||
			((isSlow || isVerySlow) && l.TraceStatementOnSlow)

		if needStmt {
			s, r := getSQL()
			attrs = append(attrs,
				attribute.String("db.statement", s),
				attribute.Int64("db.rows_affected", r),
			)
			if op := guessDBOperation(s); op != "" {
				spanName = "DB " + op
			}
		}

		_, sp, end := tracer.NewSpan(ctx, tracer.SpanOptions{
			Name:       spanName,
			StartTime:  begin,
			Attributes: attrs,
		})

		// Lightweight event (without SQL by default)
		sp.AddEvent("db.query", trace.WithAttributes(
			attribute.Int64("db.elapsed_ms", elapsed.Milliseconds()),
		))

		code := codes.Ok
		msg := "OK"

		if isErr {
			sp.RecordError(err)

			sp.AddEvent("db.error", trace.WithAttributes(
				attribute.String("error.message", err.Error()),
			))

			code = codes.Error
			msg = err.Error()
		}

		end(tracer.SpanEndOptions{
			Code:    code,
			Message: msg,
		})
	}

	// ---- Logging policy ----
	if tracer.Enabled() && l.DisableLogsWhenTracing {
		// optional safety logs
		if isErr && l.LogErrorEvenWhenTracing && l.LogLevel >= gormlogger.Error {
			l.emitErrorLog(ctx, elapsed, getSQL, err)
		}
		if isVerySlow && l.LogVerySlowEvenWhenTracing && l.LogLevel >= gormlogger.Warn {
			l.emitVerySlowLog(ctx, elapsed, getSQL)
		}
		return
	}

	// ---- Normal logs ----
	switch {
	case isErr && l.LogLevel >= gormlogger.Error:
		l.emitErrorLog(ctx, elapsed, getSQL, err)

	case isVerySlow && l.LogLevel >= gormlogger.Warn:
		l.emitVerySlowLog(ctx, elapsed, getSQL)

	case (isSlow || isVerySlow) && l.LogSlowQueries && l.LogLevel >= gormlogger.Warn:
		l.emitSlowLog(ctx, elapsed, getSQL)

	case l.LogLevel >= gormlogger.Info:
		l.emitInfoLog(ctx, elapsed, getSQL)
	}
}

// emitErrorLog logs an error with detailed information
func (l *DBLogger) emitErrorLog(
	ctx context.Context,
	elapsed time.Duration,
	getSQL func() (string, int64),
	err error,
) {
	if l.LogSQL {
		s, r := getSQL()
		logger.LogCtx(ctx).Errorf("[DBLogger:emitErrorLog][elapsed:%dms rows:%d sql:%s][err:%s]",
			elapsed.Milliseconds(), r, s, err)
		return
	}
	_, r := getSQL() // rows sometimes useful even without sql
	logger.LogCtx(ctx).Errorf("[DBLogger:emitErrorLog][elapsed:%dms rows:%d][err:%s]",
		elapsed.Milliseconds(), r, err)
}

// emitSlowLog logs a slow query with detailed information
func (l *DBLogger) emitSlowLog(ctx context.Context, elapsed time.Duration, getSQL func() (string, int64)) {
	if l.LogSQL {
		s, r := getSQL()
		logger.LogCtx(ctx).Warnf("[DBLogger:emitSlowLog][slow][elapsed:%dms rows:%d sql:%s]",
			elapsed.Milliseconds(), r, s)
		return
	}
	_, r := getSQL()
	logger.LogCtx(ctx).Warnf("[DBLogger:emitSlowLog][slow][elapsed:%dms rows:%d]",
		elapsed.Milliseconds(), r)
}

// emitVerySlowLog logs a very slow query with detailed information
func (l *DBLogger) emitVerySlowLog(ctx context.Context, elapsed time.Duration, getSQL func() (string, int64)) {
	if l.LogSQL {
		s, r := getSQL()
		logger.LogCtx(ctx).Warnf("[DBLogger:emitVerySlowLog][VERY_SLOW][elapsed:%dms rows:%d sql:%s]",
			elapsed.Milliseconds(), r, s)
		return
	}
	_, r := getSQL()
	logger.LogCtx(ctx).Warnf("[DBLogger:emitVerySlowLog][VERY_SLOW][elapsed:%dms rows:%d]",
		elapsed.Milliseconds(), r)
}

// emitInfoLog logs a normal info query with detailed information
func (l *DBLogger) emitInfoLog(ctx context.Context, elapsed time.Duration, getSQL func() (string, int64)) {
	if l.LogSQL {
		s, r := getSQL()
		logger.LogCtx(ctx).Infof("[DBLogger:emitInfoLog][elapsed:%dms rows:%d sql:%s]",
			elapsed.Milliseconds(), r, s)
		return
	}
	_, r := getSQL()
	logger.LogCtx(ctx).Infof("[DBLogger:emitInfoLog][elapsed:%dms rows:%d]",
		elapsed.Milliseconds(), r)
}

// guessDBOperation tries to guess the operation from the sql statement
func guessDBOperation(sql string) string {
	s := strings.TrimSpace(sql)
	if s == "" {
		return ""
	}
	i := strings.IndexByte(s, ' ')
	if i == -1 {
		return strings.ToUpper(s)
	}
	return strings.ToUpper(s[:i])
}
