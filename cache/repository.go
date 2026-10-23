package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/whagency/core-common/logger"
)

type _ctxKey string

const _cleanCtxKey _ctxKey = "cleanCtxKey"

const _localCacheKeyCost = 1

const _nonExpiringTTL time.Duration = 0

// New create a new cache repo
func New[T Cacheable](options RepoOptions) (*Repository[T], error) {
	repo := &Repository[T]{
		name:    options.Name,
		options: options.Options,

		cacheLocalGlobal: options.CacheLocalGlobal,
		cacheRemote:      options.RemoteCache,
	}

	if !(options.IsGlobalLocalCache || options.IsIsolatedLocalCache || options.IsRemoteCache) {
		return nil, fmt.Errorf("[cache][New][cacheName: %s][err: at least one cache should be enabled]", options.Name)
	}
	if options.IsGlobalLocalCache && options.CacheLocalGlobal == nil {
		return nil, fmt.Errorf("[cache][New][cacheName:%s][err: global local cache enabled but manager is nil]", options.Name)
	}
	if options.IsIsolatedLocalCache && (options.IsolatedCacheConfig.MaxKeys <= 0 || options.IsolatedCacheConfig.Size <= 0) {
		return nil, fmt.Errorf("[cache][New][cacheName:%s][err: invalid isolated cache config]", options.Name)
	}
	if options.IsRemoteCache && options.RemoteCache == nil {
		return nil, fmt.Errorf("[cache][New][cacheName:%s][err: remote cache enabled but manager is nil]", options.Name)
	}

	if repo.options.TTL <= 0 {
		repo.options.TTL = Config().TTL
	}

	if options.IsIsolatedLocalCache {
		isolatedCache, err := ristretto.NewCache(&ristretto.Config{
			NumCounters: options.IsolatedCacheConfig.MaxKeys,
			MaxCost:     options.IsolatedCacheConfig.Size,
			BufferItems: Config().DefaultBufferSizeLocalCache,
		})
		if err != nil {
			return nil, fmt.Errorf("[cache][New][cacheName:%+v][err:%w]", options.Name, err)
		}

		repo.isolatedLocalCache = isolatedCache
	}

	return repo, nil
}

// SetRefreshFn set the refresh function for the cache repo
func (r *Repository[T]) SetRefreshFn(fn RefreshFn[T]) {
	r.refreshFn = fn
}

// Get return the cached value without refresh if not exists
func (r *Repository[T]) Get(ctx context.Context, key string) (T, error) {
	logger.LogCtx(ctx).Debugf("[cache][Repository:Get][key:%s]", key)

	data, isFound := r.tryGet(ctx, key)
	if !isFound {
		return data, fmt.Errorf("[cache][Repository:Get][err: %w]", ValueNotFoundErr)
	}

	return data, nil
}

// GetWithRefresh as get data by using a Key (if the data is not there, then it will be refreshed)
func (r *Repository[T]) GetWithRefresh(ctx context.Context, key string) (T, error) {
	logger.LogCtx(ctx).Debugf("[cache][Repository:GetWithRefresh][key:%s]", key)

	if data, isFound := r.tryGet(ctx, key); isFound {
		return data, nil
	}

	data, err := r.refreshAndGet(ctx, key)
	if err != nil {
		return data, fmt.Errorf("[cache][Repository:GetWithRefresh]%w", err)
	}

	return data, nil
}

// Exists check if the key exists in the cache, no need to get the data
func (r *Repository[T]) Exists(ctx context.Context, key string) (bool, error) {
	logger.LogCtx(ctx).Debugf("[cache][Repository:Exists][key:%s]", key)

	if r.options.IsGlobalLocalCache {
		if _, isFound := r.cacheLocalGlobal.Get(r.localKey(key)); isFound {
			return true, nil
		}
	}

	if r.options.IsIsolatedLocalCache {
		if _, isFound := r.isolatedLocalCache.Get(r.localKey(key)); isFound {
			return true, nil
		}
	}

	if !r.options.IsRemoteCache {
		return false, nil
	}
	if r.cacheRemote == nil {
		return false, fmt.Errorf("[cache][Repository:Exists][err: remote cache enabled but manager is nil]")
	}

	isFound, err := r.cacheRemote.ContainsKey(ctx, r.name, key)
	if err != nil {
		return false, fmt.Errorf("[cache][Repository:Exists]%w", err)
	}

	return isFound, nil
}

// Update directly update the cache with the data provided
func (r *Repository[T]) Update(ctx context.Context, key string, data T) error {
	logger.LogCtx(ctx).Debugf("[cache][Repository:Update][key:%s][data:%+v]", key, data)

	if err := r.update(ctx, key, data, _nonExpiringTTL); err != nil {
		return fmt.Errorf("[cache][Repository:Update]%w", err)
	}

	return nil
}

// UpdateWithTTL directly update the cache with the data provided and a custom TTL
func (r *Repository[T]) UpdateWithTTL(ctx context.Context, key string, data T, ttl time.Duration) error {
	logger.LogCtx(ctx).Debugf("[cache][Repository:UpdateWithTTL][key:%s][data:%+v][ttl:%s]", key, data, ttl)

	if err := r.update(ctx, key, data, ttl); err != nil {
		return fmt.Errorf("[cache][Repository:UpdateWithTTL]%w", err)
	}

	return nil
}

// RefreshAndGet force refresh the cache and return the data
func (r *Repository[T]) RefreshAndGet(ctx context.Context, key string) (T, error) {
	logger.LogCtx(ctx).Debugf("[cache][Repository:RefreshAndGet][key:%s]", key)

	data, err := r.refreshAndGet(ctx, key)
	if err != nil {
		return data, fmt.Errorf("[cache][Repository:RefreshAndGet]%w", err)
	}

	return data, nil
}

// DeleteKey delete a cache key from the cache
func (r *Repository[T]) DeleteKey(ctx context.Context, key string) error {
	logger.LogCtx(ctx).Debugf("[cache][Repository:DeleteKey][key:%s]", key)

	if r.options.IsGlobalLocalCache {
		r.cacheLocalGlobal.Del(r.localKey(key))
	}

	if r.options.IsIsolatedLocalCache {
		r.isolatedLocalCache.Del(r.localKey(key))
	}

	if r.options.IsRemoteCache {
		if err := r.cacheRemote.DeleteKey(ctx, r.name, key); err != nil {
			return fmt.Errorf("[cache][Repository:DeleteKey]%w", err)
		}
	}

	return nil
}

type cleanFunction func(context.Context, string) error

type cleanEntry struct {
	key string
	fn  cleanFunction
}

type cleanMap struct {
	mu      sync.Mutex
	entries map[string]cleanEntry
}

// NewCleanMap creates a new context with a clean map
func NewCleanMap(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, _cleanCtxKey, &cleanMap{
		entries: make(map[string]cleanEntry),
	})
}

// CleanKeys cleans the cache keys in the given context
func CleanKeys(ctx context.Context) {
	if ctx == nil {
		return
	}

	cleanMapData, ok := ctx.Value(_cleanCtxKey).(*cleanMap)
	if !ok || cleanMapData == nil {
		return
	}

	cleanMapData.mu.Lock()
	entries := make([]cleanEntry, 0, len(cleanMapData.entries))
	for _, entry := range cleanMapData.entries {
		entries = append(entries, entry)
	}
	cleanMapData.mu.Unlock()

	for _, entry := range entries {
		if entry.fn == nil {
			continue
		}
		if err := entry.fn(ctx, entry.key); err != nil {
			logger.LogCtx(ctx).Errorf("[cache][CleanKeys][key: %s]%s", entry.key, err)
		}
	}
}

// ---- helpers ----

// setCleanFunc adds a clean function for the given key to the context
func (r *Repository[T]) setCleanFunc(ctx context.Context, key string) {
	if ctx == nil {
		return
	}

	cleanMapData, ok := ctx.Value(_cleanCtxKey).(*cleanMap)
	if !ok || cleanMapData == nil {
		return
	}

	// Use local cache key to avoid collisions between different repositories with same key value.
	uniqueKey := r.localKey(key)

	cleanMapData.mu.Lock()
	cleanMapData.entries[uniqueKey] = cleanEntry{
		key: key,
		fn:  r.DeleteKey,
	}
	cleanMapData.mu.Unlock()
}

// localKey returns the local cache key for the given key
func (r *Repository[T]) localKey(key string) string {
	if key == "" {
		return r.name
	}
	return r.name + ":" + key
}

// tryGet tries to get the cached value, returns nil if not exists
func (r *Repository[T]) tryGet(ctx context.Context, key string) (T, bool) {
	if r.options.IsGlobalLocalCache {
		if data, found := r.cacheLocalGlobal.Get(r.localKey(key)); found {
			if assertedData, ok := r.assertAndReturn(ctx, data, key); ok {
				return assertedData, true
			}
		}
	}

	if r.options.IsIsolatedLocalCache {
		if data, found := r.isolatedLocalCache.Get(r.localKey(key)); found {
			if assertedData, ok := r.assertAndReturn(ctx, data, key); ok {
				return assertedData, true
			}
		}
	}

	if r.options.IsRemoteCache {
		data, err := r.cacheRemote.Get(ctx, r.name, key)
		if err != nil {
			logger.LogCtx(ctx).Warnf("[cache][Repository:tryGet][key:%s]%s", key, err)
		}

		if assertedData, ok := r.assertAndReturn(ctx, data, key); ok {
			return assertedData, true
		}
	}

	var emptyData T
	return emptyData, false
}

// assertAndReturn asserts the type of the cached value and returns it, if the type is not correct, it deletes the cache key
func (r *Repository[T]) assertAndReturn(ctx context.Context, data any, origKey string) (T, bool) {
	if data == nil {
		var emptyData T
		return emptyData, false
	}

	assertedData, ok := data.(T)
	if ok {
		return assertedData, true
	}

	if err := r.DeleteKey(ctx, origKey); err != nil {
		logger.LogCtx(ctx).Warnf("[cache][Repository:assertAndReturn][key:%s][err: failed to delete invalid cache: %s]", origKey, err)
	}

	return assertedData, false
}

// refreshAndGet updates the cache data with entry refresh function and returns the data
func (r *Repository[T]) refreshAndGet(ctx context.Context, key string) (T, error) {
	if r.refreshFn == nil {
		var emptyData T
		return emptyData, fmt.Errorf("[refreshAndGet][err: refreshFn is nil]")
	}

	data, err, _ := r.sf.Do(r.localKey(key), func() (any, error) {
		data, err := r.refreshFn(ctx, key)
		if err != nil {
			return data, fmt.Errorf("[refreshAndGet]%w", err)
		}

		if err = r.update(ctx, key, data, _nonExpiringTTL); err != nil {
			return data, fmt.Errorf("[refreshAndGet]%w", err)
		}

		return data, nil
	})
	if err != nil {
		var emptyData T
		return emptyData, err
	}

	assertedData, ok := r.assertAndReturn(ctx, data, key)
	if !ok {
		return assertedData, fmt.Errorf("[refreshAndGet][err: singleflight returned unexpected type]")
	}

	return assertedData, nil
}

// update updates the cache data with the given data
func (r *Repository[T]) update(ctx context.Context, key string, data T, ttl time.Duration) error {
	r.setCleanFunc(ctx, key)

	setTTL := r.options.TTL
	if ttl > 0 {
		setTTL = ttl
	}

	if r.options.IsGlobalLocalCache {
		if ok := r.cacheLocalGlobal.SetWithTTL(r.localKey(key), data, _localCacheKeyCost, setTTL); !ok {
			return fmt.Errorf("[update][GlobalCache][key:%s][err:%w]", r.localKey(key), NotUpdatedErr)
		}
	}

	if r.options.IsIsolatedLocalCache {
		if ok := r.isolatedLocalCache.SetWithTTL(r.localKey(key), data, _localCacheKeyCost, setTTL); !ok {
			return fmt.Errorf("[update][IsolatedCache][key:%s][err:%w]", r.localKey(key), NotUpdatedErr)
		}
	}

	if r.options.IsRemoteCache {
		if setTTL > 0 {
			if err := r.cacheRemote.SaveWithTTL(ctx, r.name, key, data, setTTL); err != nil {
				return fmt.Errorf("[update][key:%s]%w", key, err)
			}
		} else if err := r.cacheRemote.Save(ctx, r.name, key, data); err != nil {
			return fmt.Errorf("[update][key:%s]%w", key, err)
		}
	}

	return nil
}
