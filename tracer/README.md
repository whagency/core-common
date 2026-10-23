# Tracer

OpenTelemetry tracing integration package for Go applications with HTTP/gRPC export support to Jaeger and other compatible systems.

## Features

- 🔄 OpenTelemetry TracerProvider initialization with configuration
- 🌐 HTTP and gRPC protocol support for trace export
- 🎯 Configurable sample ratio for trace volume control
- 🔧 Gin framework middleware
- 🌍 W3C TraceContext and Baggage propagation
- 🛠️ Utilities for spans and HTTP client instrumentation

## Installation

```bash
go get github.com/whagency/core-common/tracer
```

## Quick Start

### 1. Initialization

```go
import "github.com/whagency/core-common/tracer"

cfg := tracer.Config{
    Enable:      true,
    AppName:     "my-service",
    Environment: "production",
    Version:     "1.0.0",
    Protocol:    "http",           // "http" or "grpc"
    Endpoint:    "jaeger:4318",    // host:port
    HTTPPath:    "/v1/traces",     // for HTTP only
    SampleRatio: 1.0,              // 0.0 - 1.0
}

if err := tracer.Init(cfg); err != nil {
    log.Fatal(err)
}

defer tracer.Shutdown(context.Background())
```

### 2. Gin Middleware

To enable tracing and logging for Gin, use `GinObservabilityMiddleware` from the `http/middleware` package.

```go
import (
    "github.com/gin-gonic/gin"
    "github.com/whagency/core-common/http/middleware"
)

r := gin.Default()
r.Use(middleware.GinObservabilityMiddleware())
```

Middleware automatically:
- Creates a span for each request
- Extracts trace context from headers
- Records HTTP method, route, status code
- Integrates with the internal logger (RequestID, InternalTraceID)
- Handles errors from Gin context

### 3. gRPC Interceptor

To enable tracing for gRPC servers, use `ObservabilityInterceptor` from the `grpc/middleware` package.

```go
import (
    "google.golang.org/grpc"
    "github.com/whagency/core-common/grpc/middleware"
)

s := grpc.NewServer(
    grpc.UnaryInterceptor(middleware.ObservabilityInterceptor()),
)
```

### 4. GORM Tracing

To enable tracing for database queries with GORM, use `NewDBLogger` from the `db/gormlog` package.

```go
import (
    "github.com/whagency/core-common/db/gormlog"
    "gorm.io/gorm"
)

dbLogger := gormlog.NewDBLogger(gormlog.Config{
    SlowThreshold: 200 * time.Millisecond,
    DBSystem:      "postgres",
    DBName:        "my_db",
})

db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
    Logger: dbLogger,
})
```

### 5. Creating Spans Manually

```go
import (
    "context"
    "github.com/whagency/core-common/tracer"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
)

ctx, span, end := tracer.NewSpan(ctx, tracer.SpanOptions{
    Name: "my-operation",
    Attributes: []attribute.KeyValue{
        attribute.String("user.id", "123"),
        attribute.Int("items.count", 5),
    },
})

// Your business logic
result, err := doSomething(ctx)

// Record error if any
if err != nil {
    span.RecordError(err)
    end(tracer.SpanEndOptions{
        Code:    codes.Error,
        Message: err.Error(),
    })
    return err
}

// Successful completion
end(tracer.SpanEndOptions{
    Code: codes.Ok,
    Message: "Success",
})
```

### 6. HTTP Client Instrumentation

#### Standard http.Client
```go
import (
    "net/http"
    "github.com/whagency/core-common/tracer"
)

client := &http.Client{}
tracer.WrapHTTPClient(client)

// All requests will now be automatically traced
resp, err := client.Get("https://api.example.com/data")
```

#### req/v3 Client
```go
import (
    "github.com/imroc/req/v3"
    "github.com/whagency/core-common/tracer"
)

client := req.C()
tracer.WrapReqClient(client)
```

### 7. Copying Span Context for Goroutines

```go
import (
    "context"
    "github.com/whagency/core-common/tracer"
)

// Safe copying of trace context to a new goroutine
go func() {
    newCtx := tracer.CopySpanContext(parentCtx, context.Background())
    doWork(newCtx)
}()
```

## Configuration

### Config Structure

```go
type Config struct {
    AppName     string  // Service name (required)
    Environment string  // Environment (dev, staging, prod) (required)
    Version     string  // Application version
    Enable      bool    // Enable tracing
    Protocol    string  // "http" or "grpc" (default "http")
    Endpoint    string  // host:port of collector (required)
    HTTPPath    string  // Path for HTTP (default "/v1/traces")
    SampleRatio float64 // Trace sampling ratio 0.0-1.0 (default 1.0)

    // TLS configuration
    TLSEnabled            bool
    CAPath                string // Path to CA cert
    CertPath              string // Path to client cert (mTLS)
    KeyPath               string // Path to client key (mTLS)
    ServerName            string // SNI override
    TLSInsecureSkipVerify bool

    // Paths to skip tracing (comma-separated)
    HTTPNotTraceablePaths string
}
```

### Environment Variables

```bash
TRACER_ENABLE=true
TRACER_APP_NAME=my-service
TRACER_ENVIRONMENT=production
TRACER_VERSION=1.0.0
TRACER_PROTOCOL=http
TRACER_ENDPOINT=jaeger:4318
TRACER_SAMPLE_RATIO=1.0
TRACER_HTTP_NOT_TRACEABLE_PATHS="/health,/metrics"
```

## Usage Examples

### Database Operations (manual span)

```go
ctx, span, end := tracer.NewSpan(ctx, tracer.SpanOptions{
    Name: "db.query.users",
    Attributes: []attribute.KeyValue{
        attribute.String("db.system", "postgresql"),
        attribute.String("db.operation", "SELECT"),
    },
})

users, err := db.GetUsers(ctx)
if err != nil {
    span.RecordError(err)
    end(tracer.SpanEndOptions{Code: codes.Error, Message: err.Error()})
    return err
}

end(tracer.SpanEndOptions{Code: codes.Ok})
```

### External HTTP Request

```go
ctx, span, end := tracer.NewSpan(ctx, tracer.SpanOptions{
    Name: "http.client.get-user",
    Attributes: []attribute.KeyValue{
        attribute.String("http.url", "https://api.example.com/users/123"),
    },
})

resp, err := httpClient.Get("https://api.example.com/users/123")
if err != nil {
    span.RecordError(err)
    end(tracer.SpanEndOptions{Code: codes.Error})
    return err
}

end(tracer.SpanEndOptions{Code: codes.Ok})
```

## API Reference

### Functions

#### `Init(cfg Config) error`
Initializes the tracer with the given configuration. Safe for concurrent calls; only first invocation takes effect.

#### `Shutdown(ctx context.Context) error`
Gracefully shuts down the tracer provider. Call this when stopping the application.

#### `Enabled() bool`
Returns true if tracing is enabled.

#### `Tracer() trace.Tracer`
Returns the tracer instance for direct use of OpenTelemetry API.

#### `NewSpan(ctx context.Context, opts SpanOptions) (context.Context, trace.Span, SpanEnd)`
Creates a new span with options. Returns new context, span, and a function to end the span.

#### `WrapHTTPClient(client *http.Client)`
Wraps standard HTTP client for automatic tracing of outgoing requests.

#### `WrapReqClient(client *req.Client)`
Wraps req.Client for automatic tracing of outgoing requests.

#### `CopySpanContext(parentCtx, newCtx context.Context) context.Context`
Copies span context from parent context to new context. Safe for use in goroutines.

## Jaeger Integration

### Docker Compose Example

```yaml
services:
  jaeger:
    image: jaegertracing/all-in-one:latest
    ports:
      - "16686:16686"  # Jaeger UI
      - "4318:4318"    # OTLP HTTP receiver
    environment:
      - COLLECTOR_OTLP_ENABLED=true
```

### Application Configuration

```go
cfg := tracer.Config{
    Enable:      true,
    AppName:     "my-service",
    Environment: "development",
    Version:     "1.0.0",
    Protocol:    "http",
    Endpoint:    "localhost:4318",
    HTTPPath:    "/v1/traces",
    SampleRatio: 1.0,
}
```

Open Jaeger UI at `http://localhost:16686` to view traces.

## Dependencies

- `go.opentelemetry.io/otel` v1.x
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace`
- `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`
- `github.com/gin-gonic/gin` (for middleware)
- `github.com/whagency/core-common/logger` (for request ID)

## License

MIT
