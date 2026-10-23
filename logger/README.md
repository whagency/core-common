# Logger

A structured logging package for Go applications built on top of [uber-go/zap](https://github.com/uber-go/zap). It provides seamless integration with OpenTelemetry tracing, Logstash support, and easy-to-use context-aware logging.

## Features

- ⚡ **High-performance**: Structured logging with `zap`.
- 📥 **Logstash Integration**: Support for TCP/TLS export.
- 🆔 **Trace Identifiers**: Automatic Request ID and Internal Trace ID management via context.
- 🌐 **Framework Integration**: Middleware for Gin, gRPC, and Gorilla Mux.
- 🔧 **Flexible Configuration**: Environment variables for all settings.
- 🎯 **Context-aware**: Easily trace logs across service boundaries.

## Installation

```bash
go get github.com/whagency/core-common/logger
```

## Quick Start

### 1. Initialization

```go
import "github.com/whagency/core-common/logger"

cfg := logger.Config{
    AppName:     "my-service",
    Environment: "production",
    Version:     "1.0.0",
    Level:       "info",
    StdoutFormat: "json", // "console" (default) or "json"
    // Logstash configuration (optional)
    LogstashEnabled: true,
    LogstashHost:    "logstash:5044",
}

if err := logger.Init(cfg); err != nil {
    log.Fatal(err)
}
defer logger.Sync()

logger.Log().Info("Logger initialized successfully")
```

### 2. Context-Aware Logging

Use `LogCtx` to get a logger that automatically includes request-specific IDs if they are present in the context.

```go
func MyHandler(ctx context.Context) {
    // Automatically picks up RequestID and InternalTraceID from context
    logger.LogCtx(ctx).Infow("Processing request", "user_id", 123)
}
```

### 3. Creating Log Context Manually

For background tasks or internal processes, you can generate a new context with unique IDs:

```go
ctx := logger.NewInternalLogCtx(context.Background())
logger.LogCtx(ctx).Info("Task started with new trace IDs")
```

## Integration with Frameworks

### Gin Middleware

The `GinObservabilityMiddleware` (from `http/middleware`) combines logging and tracing.

```go
import (
    "github.com/gin-gonic/gin"
    "github.com/whagency/core-common/http/middleware"
)

r := gin.Default()
r.Use(middleware.GinObservabilityMiddleware())
```

### gRPC Interceptor

The `ObservabilityInterceptor` (from `grpc/middleware`) provides similar functionality for gRPC.

```go
import (
    "google.golang.org/grpc"
    "github.com/whagency/core-common/grpc/middleware"
)

s := grpc.NewServer(
    grpc.UnaryInterceptor(middleware.ObservabilityInterceptor()),
)
```

## Configuration

### Config Structure

```go
type Config struct {
    AppName     string
    Environment string
    Version     string
    Level       string // "debug", "info", "warn", "error"
    StdoutFormat string // "console" or "json"

    // Logstash settings
    LogstashEnabled    bool
    LogstashHost       string
    LogstashTLSEnabled bool
    // ... other TLS settings (CAPath, CertPath, KeyPath)
}
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LOG_LEVEL` | Logging level | `debug` |
| `LOG_STDOUT_FORMAT` | Stdout format (`console` or `json`) | `console` |
| `LOG_LOGSTASH_ENABLE` | Enable Logstash export | `false` |
| `LOG_LOGSTASH_HOST` | Logstash address (host:port) | - |
| `LOG_HTTP_NOT_LOGGABLE_PATHS` | Comma-separated paths to skip | - |

## API Reference

### Functions

#### `Init(cfg Config) error`
Initializes the global logger. Safe for concurrent calls.

#### `Log() *zap.SugaredLogger`
Returns the global sugared logger. Panics if `Init` wasn't called.

#### `LogCtx(ctx context.Context) *zap.SugaredLogger`
Returns a logger from the context (if available) or the global logger.

#### `Sync()`
Flushes any buffered log entries. Should be called before application exit.

#### `GetRequestID(ctx context.Context) string`
Extracts the Request ID from the context.

#### `GetInternalTraceID(ctx context.Context) string`
Extracts the Internal Trace ID from the context.

#### `BodyFieldValueForLog(body []byte) string`
Renders a request, response or queue payload for logging, with the values of
credential-carrying fields replaced by `RedactedBodyValue`.

This is the single choke point every body passes through: the Gin and gRPC
observability middleware and the RabbitMQ publisher and consumer all call it.
Bodies are logged whenever `IsLoggableBody()` is true, which `LOG_LEVEL=debug`
alone is enough to switch on.

Sanitising headers was never sufficient, because this platform carries its
credentials in bodies: the login request holds a password, the refresh request
holds a refresh token, the login response hands both tokens straight back, the
gRPC session validation request holds an access token, and the OTP queue message
holds the code.

Rules:

- field names are matched case-insensitively with `_` and `-` removed, so one
  entry covers `refreshToken`, `refresh_token` and `refresh-token`
- redaction is recursive, through nested objects and arrays
- `code` is redacted only when it holds a **string**. An OTP arrives as
  `"123456"` while every error response carries a numeric `code`, and redacting
  those would cost more debugging than it buys
- a body that is not JSON is reported by size only, never echoed -- that is how
  a form-encoded `secret=...` used to reach the log
- integers keep their exact representation, and `&`/`<`/`>` are not escaped, so
  log lines stay readable

Adding a field to the list is cheaper than discovering it in a log. Removing one
needs a reason.

## License

MIT
