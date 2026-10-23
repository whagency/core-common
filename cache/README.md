# Cache

A comprehensive caching package for Go applications providing multi-level caching (local & remote), typed repositories, and distributed locking.

## Features

- 🚀 **Multi-level Caching**: Support for isolated local cache, shared global local cache, and remote cache (Hazelcast).
- 🏗️ **Typed Repositories**: Generic-based `Repository[T]` for type-safe data access.
- 🔄 **Auto-Refresh**: Configurable refresh functions to automatically update stale data.
- 🔒 **Distributed Locking**: Simple `Locker` implementation for managing shared resources.
- 🛠️ **Flexible Configuration**: YAML and environment variable support for fine-grained control.
- 🛡️ **Singleflight**: Prevents cache stampede by ensuring only one in-flight request for the same key.

## Installation

```bash
go get github.com/whagency/core-common/cache
```

## Quick Start

### 1. Initialize Configuration

The cache system requires environment variables and a YAML configuration file for entity-specific settings.

```go
import "github.com/whagency/core-common/cache"

if err := cache.LoadCacheConfig(); err != nil {
    log.Fatal(err)
}
```

### 2. Using Typed Repository

```go
import (
    "context"
    "github.com/whagency/core-common/cache"
)

type User struct {
    ID   string
    Name string
}

// Define refresh function (fetch from DB if not in cache)
refreshFn := func(ctx context.Context, id string) (*User, error) {
    return db.FetchUser(id)
}

// Create repository
userRepo, _ := cache.New[*User](cache.RepoOptions{
    Name: "users",
    Options: cache.Options{
        TTL: 5 * time.Minute,
        IsRemoteCache: true,
    },
    RemoteCache: remoteManager, // Implementation of RemoteCacheManager
})

userRepo.SetRefreshFn(refreshFn)

// Get data
user, err := userRepo.Get(ctx, "user_123")
```

## Caching Levels

The package supports three levels of caching that can be combined:

1.  **Isolated Local Cache**: Private cache for a specific repository instance.
2.  **Global Local Cache**: Shared local cache accessible across different repositories in the same application.
3.  **Remote Cache**: Distributed cache (e.g., Hazelcast) shared across multiple application instances.

### Configuration Example (`cache_manager.yml`)

```yaml
global_cache_config:
  size: 1000
  max_keys: 5000

cached_entities:
  users:
    ttl: 10m
    remote_cache: true
    isolated_local_cache: true
    isolated_cache_config:
      size: 100
      max_keys: 500
```

## Distributed Locking

Use `Locker` to prevent concurrent operations across different service instances.

```go
import "github.com/whagency/core-common/cache"

locker := cache.NewLocker(cache.LockerOptions{
    Name:        "order_processing",
    RemoteCache: remoteManager,
    TTL:         30 * time.Second,
})

// Try to acquire lock
if err := locker.TryLock(ctx, "order_789"); err != nil {
    return fmt.Errorf("resource is busy: %w", err)
}

defer locker.Unlock(ctx, "order_789")

// Perform critical section logic
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CACHE_HOST` | Remote cache host (required) | - |
| `CACHE_CLUSTER_NAME` | Hazelcast cluster name | "" |
| `CACHE_MANAGER_CONFIG_FILE` | Path to YAML config | `./config/cache_manager.yml` |
| `CACHE_TTL` | Default TTL for all keys | `1m` |
| `CACHE_INVOCATION_TIMEOUT` | Timeout for cache operations | `10s` |

### Repository Options

When creating a `Repository`, you can configure its behavior via `Options`:

```go
type Options struct {
    TTL                  time.Duration
    IsGlobalLocalCache   bool
    IsIsolatedLocalCache bool
    IsRemoteCache        bool
    IsolatedCacheConfig  LocalCacheConfig
}
```

## API Reference

### `Repository[T]` Methods

- `Get(ctx, key)`: Returns data from cache or calls refresh function.
- `GetWithRefresh(ctx, key)`: Same as `Get` but specifically handles refresh logic.
- `Update(ctx, key, data)`: Manually updates data in enabled cache levels.
- `DeleteKey(ctx, key)`: Removes key from all cache levels.
- `Exists(ctx, key)`: Checks if key exists in any cache level.

### Interfaces

#### `RemoteCacheManager`
Defines operations for distributed cache (implemented by `hzcast` sub-package).

#### `LocalCacheManager`
Defines operations for in-memory cache.

## Dependencies

- `golang.org/x/sync/singleflight`
- `github.com/hazelcast/hazelcast-go-client`
- `github.com/kelseyhightower/envconfig`
- `gopkg.in/yaml.v3`

## License

MIT
