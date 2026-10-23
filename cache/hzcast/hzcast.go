package hzcast

import (
	"context"
	"fmt"
	"time"

	"github.com/hazelcast/hazelcast-go-client"
)

type RemoteCacheManager struct {
	namespace  string
	cache      *hazelcast.Client
	defaultTTL time.Duration
}

// NewRemoteCacheManager creates a new RemoteCacheManager instance
func NewRemoteCacheManager(name string, client *hazelcast.Client, defaultTTL time.Duration) *RemoteCacheManager {
	return &RemoteCacheManager{
		namespace:  name,
		cache:      client,
		defaultTTL: defaultTTL,
	}
}

// Get retrieves data from cache
func (r RemoteCacheManager) Get(ctx context.Context, mapName, key string) (any, error) {
	cacheMap, err := r.cache.GetMap(ctx, r.getMapName(mapName))
	if err != nil {
		return nil, fmt.Errorf("[hzcast:Get][GetMap][err: %w]", err)
	}

	data, err := cacheMap.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("[hzcast:Get][No Data for key %s for MapName %s][err: %w]", key, r.getMapName(mapName), err)
	}

	return data, nil
}

// Save saves data in cache
func (r RemoteCacheManager) Save(ctx context.Context, mapName, key string, data any) error {
	cacheMap, err := r.cache.GetMap(ctx, r.getMapName(mapName))
	if err != nil {
		return fmt.Errorf("[hzcast:Save][GetMap][mapName:%s][err:%w]", mapName, err)
	}

	if err = cacheMap.Set(ctx, key, data); err != nil {
		return fmt.Errorf("[hzcast:Save][mapName:%s][key:%s][err:%w]", mapName, key, err)
	}

	return nil
}

// SaveWithTTL saves data with ttl. If ttl is 0, defaultTTL is used.
func (r RemoteCacheManager) SaveWithTTL(ctx context.Context, mapName, key string, data any, ttl time.Duration) error {
	cacheMap, err := r.cache.GetMap(ctx, r.getMapName(mapName))
	if err != nil {
		return fmt.Errorf("[hzcast:SaveWithTTL][GetMap][mapName:%s][err:%w]", mapName, err)
	}

	if ttl == 0 {
		ttl = r.defaultTTL
	}

	if err = cacheMap.SetWithTTL(ctx, key, data, ttl); err != nil {
		return fmt.Errorf("[hzcast:SaveWithTTL][mapName:%s][key:%s][err:%w]", mapName, key, err)
	}

	return nil
}

// ContainsKey checks if key exists in cache.
func (r RemoteCacheManager) ContainsKey(ctx context.Context, mapName, key string) (bool, error) {
	cacheMap, err := r.cache.GetMap(ctx, r.getMapName(mapName))
	if err != nil {
		return false, fmt.Errorf("[hzcast:ContainsKey][GetMap][err: %s]", err)
	}

	found, err := cacheMap.ContainsKey(ctx, key)
	if err != nil {
		return false, fmt.Errorf("[hzcast:ContainsKey][err: %s]", err)
	}

	return found, nil
}

// DeleteKey removes key from cache.
func (r RemoteCacheManager) DeleteKey(ctx context.Context, mapName, key string) error {
	cacheMap, err := r.cache.GetMap(ctx, r.getMapName(mapName))
	if err != nil {
		return fmt.Errorf("[hzcast:DeleteKey][GetMap][err: %w]", err)
	}

	if err = cacheMap.Delete(ctx, key); err != nil {
		return fmt.Errorf("[hzcast:DeleteKey][err: %w]", err)
	}

	return nil
}

// PutIfAbsentWithTTL puts data in cache if key does not exist and returns the previous value if any.
func (r RemoteCacheManager) PutIfAbsentWithTTL(ctx context.Context, mapName, key string, data any, ttl time.Duration) (any, error) {
	cacheMap, err := r.cache.GetMap(ctx, r.getMapName(mapName))
	if err != nil {
		return nil, fmt.Errorf("[hzcast:PutIfAbsentWithTTL][GetMap][mapName:%s][err:%w]", mapName, err)
	}

	if ttl == 0 {
		ttl = r.defaultTTL
	}

	previousValue, err := cacheMap.PutIfAbsentWithTTL(ctx, key, data, ttl)
	if err != nil {
		return nil, fmt.Errorf("[hzcast:PutIfAbsentWithTTL][mapName:%s][key:%s][err:%w]", mapName, key, err)
	}

	return previousValue, nil
}

// getMapName returns the map name with namespace
func (r RemoteCacheManager) getMapName(mapName string) string {
	return r.namespace + ":" + mapName
}
