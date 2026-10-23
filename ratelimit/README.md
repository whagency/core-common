# Ratelimit

A flexible rate-limiting package for Go applications, providing an in-memory store for limiters with automatic cleanup and integration with the Gin framework.

## Features

- ⏱️ **Rate Limiting**: Built on top of `golang.org/x/time/rate`.
- 🧹 **Automatic Cleanup**: Periodically removes idle visitors to prevent memory leaks.
- 🔑 **Key-based Limiting**: Limit by IP, User ID, API Key, or any custom string.
- 🔧 **Gin Middleware**: Ready-to-use middleware for the Gin web framework.
- 🛠️ **Configurable**: Easily tune RPS (Requests Per Second), Burst size, and TTL.

## Installation

```bash
go get github.com/whagency/core-common/ratelimit
```

## Quick Start

### 1. Initialize Limiter Store

```go
import (
    "github.com/whagency/core-common/ratelimit"
    "golang.org/x/time/rate"
    "time"
)

store := ratelimit.NewLimiterStore(ratelimit.LimiterConfig{
    RPS:   rate.Limit(5),      // 5 requests per second
    Burst: 10,                 // Maximum burst of 10 requests
    TTL:   10 * time.Minute,   // Keep limiter for 10 minutes after last seen
})
```

### 2. Manual Usage

```go
key := "user_123"
limiter := store.Get(key)

if !limiter.Allow() {
    // Handle rate limit exceeded
    return
}
```

## Gin Middleware

The package includes a middleware for the Gin framework to easily protect your routes.

### Usage with IP-based limiting

```go
import (
    "github.com/gin-gonic/gin"
    "github.com/whagency/core-common/http/middleware"
    "github.com/whagency/core-common/ratelimit"
)

r := gin.Default()
store := ratelimit.NewLimiterStore(ratelimit.LimiterConfig{RPS: 2, Burst: 5})

// Use default IP-based limiting
r.Use(middleware.GinRateLimitMiddleware(store, nil))
```

### Usage with Custom Key

```go
// Limit by User ID from context or header
keyFn := func(c *gin.Context) string {
    return c.GetHeader("X-User-ID")
}

r.GET("/api/resource", middleware.GinRateLimitMiddleware(store, keyFn), func(c *gin.Context) {
    c.JSON(200, gin.H{"message": "success"})
})
```

## Configuration

### `LimiterConfig`

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `RPS` | `rate.Limit` | Requests Per Second allowed per key | `1` |
| `Burst` | `int` | Maximum burst size | `2` |
| `TTL` | `time.Duration` | Time to keep a limiter in memory after its last use | `30m` |

## API Reference

### `LimiterStore` Methods

#### `NewLimiterStore(cfg LimiterConfig) *LimiterStore`
Creates a new store with the given configuration. If fields are omitted, defaults are used.

#### `Get(key string) *rate.Limiter`
Returns a `*rate.Limiter` for the specified key. If it doesn't exist, a new one is created. This method also triggers an internal cleanup of expired entries periodically.

### Middleware Functions

#### `GinRateLimitMiddleware(store *LimiterStore, keyFn LimitKeyFunc) gin.HandlerFunc`
A Gin-compatible middleware. If `keyFn` is `nil`, it defaults to client IP address.

## License

MIT
