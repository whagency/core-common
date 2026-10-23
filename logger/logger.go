package logger

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ctxKey string

// String returns the string representation of the context key.
func (k ctxKey) String() string {
	return string(k)
}

const (
	// ContextKeyRequestID is the key used to store the request ID in context
	ContextKeyRequestID ctxKey = "request_id"
	// ContextKeyInternalTraceID is the key used to store internal trace ID in context
	ContextKeyInternalTraceID ctxKey = "internal_trace_id"
	// ContextKeyLogger is the key used to store the logger in a context
	ContextKeyLogger ctxKey = "logger"
	// DefaultRequestIDHeader is the header to extract request ID from
	DefaultRequestIDHeader = "X-Request-ID"
	// FieldErrorCode is a structured field key (`error_code`) for machine-readable error classification.
	FieldErrorCode = "error_code"
)

var (
	// globalLogger is the package-level logger instance
	globalLogger *zap.Logger // global is the global logger instance, initialized by Init()
	initOnce     sync.Once   // ensures Init is only called  once
	initErr      error       // stores initialization error if any
)

// Init configures the global logger. Safe for concurrent calls; only first invocation takes effect.
func Init(cfg Config) error {
	initOnce.Do(func() {
		initErr = validateAndSetConfig(cfg)
		if initErr != nil {
			return
		}
		globalLogger, initErr = buildLogger(cfg)
	})
	return initErr
}

// buildLogger assembles cores and returns a configured zap.Logger.
func buildLogger(cfg Config) (*zap.Logger, error) {
	// parse log level
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("[buildLogger][err: invalid log level '%s': %w]", cfg.Level, err)
	}
	// encoder config
	encCfg := zapcore.EncoderConfig{
		MessageKey:     "message",
		LevelKey:       "level",
		TimeKey:        "@timestamp",
		CallerKey:      "caller",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.RFC3339TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// core that writes to stdout
	stdoutEncoder := zapcore.NewConsoleEncoder(encCfg)
	if cfg.StdoutFormat == StdoutFormatJSON {
		stdoutEncoder = zapcore.NewJSONEncoder(encCfg)
	}

	stdoutCore := zapcore.NewCore(
		stdoutEncoder,
		zapcore.Lock(os.Stdout),
		level,
	)

	cores := []zapcore.Core{stdoutCore}

	// optionally add Logstash core
	if cfg.LogstashEnabled {
		logstashWriter, errNew := NewLogstashWriter(cfg)
		if errNew != nil {
			// log as warning to stdout and proceed without Logstash
			fmt.Printf("[logger:buildLogger]%s\n", errNew)
		} else {
			jsonCore := zapcore.NewCore(
				zapcore.NewJSONEncoder(encCfg),
				zapcore.AddSync(logstashWriter),
				level,
			)

			cores = append(cores, jsonCore)
		}
	}

	logger := zap.New(
		zapcore.NewTee(cores...),
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
		zap.Fields(
			zap.String("app_name", cfg.AppName),
			zap.String("app_environment", cfg.Environment),
			zap.String("app_version", cfg.Version),
		),
	)

	return logger, nil
}

// Sync flushes any buffered log entries.
func Sync() {
	if globalLogger != nil {
		_ = globalLogger.Sync()
	}
}

// NewInternalLogCtx creates a new context with a logger and trace identifiers.
func NewInternalLogCtx(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	// Generate new trace identifiers
	internalID := uuid.New().String()
	reqID := uuid.New().String()

	// Create a logger with standard fields
	newLogger := Log().With(
		zap.String(ContextKeyInternalTraceID.String(), internalID),
		zap.String(ContextKeyRequestID.String(), reqID),
	)

	ctx = context.WithValue(ctx, ContextKeyRequestID, reqID)
	ctx = context.WithValue(ctx, ContextKeyInternalTraceID, internalID)
	ctx = context.WithValue(ctx, ContextKeyLogger, newLogger)

	return ctx
}

// Log returns the global logger instance. Panics if not initialized.
func Log() *zap.SugaredLogger {
	if globalLogger == nil {
		panic("[logger:Log][err: global logger not initialized]")
	}

	return globalLogger.Sugar()
}

// LogWithErrorCode returns a logger with the structured error_code field attached.
func LogWithErrorCode(errorCode int) *zap.SugaredLogger {
	return Log().With(FieldErrorCode, errorCode)
}

// LogCtx returns a logger from the context, or the global logger if not found.
func LogCtx(ctx context.Context) *zap.SugaredLogger {
	if ctx != nil {
		if v := ctx.Value(ContextKeyLogger); v != nil {
			if sugar, ok := v.(*zap.SugaredLogger); ok {
				return sugar
			}
		}
	}
	return Log()
}

// LogCtxWithErrorCode returns a logger with the structured error_code field attached.
func LogCtxWithErrorCode(ctx context.Context, errorCode int) *zap.SugaredLogger {
	return LogCtx(ctx).With(FieldErrorCode, errorCode)
}

// GetRequestID returns the request ID from the context, or an empty string if not found.
func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if reqID, ok := ctx.Value(ContextKeyRequestID).(string); ok {
		return reqID
	}
	return ""
}

// GetInternalTraceID returns the internal trace ID from the context, or an empty string if not found.
func GetInternalTraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if internalID, ok := ctx.Value(ContextKeyInternalTraceID).(string); ok {
		return internalID
	}
	return ""
}
