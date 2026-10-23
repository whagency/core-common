package cache

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sync/singleflight"
)

var (
	ValueNotFoundErr    = errors.New("cache value not found")
	NotUpdatedErr       = errors.New("cache not updated")
	KeyAlreadyLockedErr = errors.New("cache key already locked")
)

type KeyID int

type KeyIDsMap map[string]KeyID

type Key struct {
	ID      KeyID
	Name    string
	Options Options
}

type KeysMap map[KeyID]*Key

type Options struct {
	TTL                  time.Duration
	IsGlobalLocalCache   bool
	IsIsolatedLocalCache bool
	IsRemoteCache        bool
	IsolatedCacheConfig  LocalCacheConfig
}

type LocalCacheConfig struct {
	Size    int64
	MaxKeys int64
}

type Cacheable interface {
	any
}

type RepoOptions struct {
	Options
	Name             string
	CacheLocalGlobal LocalCacheManager
	RemoteCache      RemoteCacheManager
}

type RefreshFn[T Cacheable] func(context.Context, string) (T, error)

type Repository[T Cacheable] struct {
	name      string
	refreshFn RefreshFn[T]
	options   Options

	isolatedLocalCache LocalCacheManager // only for this Repo
	cacheLocalGlobal   LocalCacheManager // shared between all cache repo's
	cacheRemote        RemoteCacheManager

	sf singleflight.Group
}

// RemoteCacheManager defines the operations for a remote cache manager
type RemoteCacheManager interface {
	Get(ctx context.Context, mapName string, key string) (any, error)
	Save(ctx context.Context, mapName string, key string, value any) error
	SaveWithTTL(ctx context.Context, mapName string, key string, value any, ttl time.Duration) error
	ContainsKey(ctx context.Context, mapName string, key string) (bool, error)
	DeleteKey(ctx context.Context, mapName string, key string) error
	PutIfAbsentWithTTL(ctx context.Context, mapName string, key string, value any, ttl time.Duration) (any, error)
}

// LocalCacheManager defines the operations for a local cache manager
type LocalCacheManager interface {
	Get(key any) (any, bool)
	SetWithTTL(key any, value any, cost int64, ttl time.Duration) bool
	Del(key any)
}
