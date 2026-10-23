package cache

import (
	"fmt"
	"os"
	"time"

	"github.com/kelseyhightower/envconfig"
	"gopkg.in/yaml.v3"
)

var settings Settings

type Settings struct {
	Host                        string        `envconfig:"CACHE_HOST" required:"true"`
	ClusterName                 string        `envconfig:"CACHE_CLUSTER_NAME" default:""`
	CacheManagerConfigFile      string        `envconfig:"CACHE_MANAGER_CONFIG_FILE" default:"./config/cache_manager.yml"`
	TTL                         time.Duration `envconfig:"CACHE_TTL" default:"1m"`
	InvocationTimeout           time.Duration `envconfig:"CACHE_INVOCATION_TIMEOUT" default:"10s"`
	DefaultBufferSizeLocalCache int64         `envconfig:"CACHE_DEFAULT_BUFFER_SIZE_LOCAL_CACHE" default:"64"`
	CacheManager                cacheManager
}

type cacheManager struct {
	GlobalLocalCacheConfig localCacheConfig        `yaml:"global_cache_config"`
	CachedEntities         map[string]cachedEntity `yaml:"cached_entities"`
}

type cachedEntity struct {
	GlobalLocalCache    bool             `yaml:"global_local_cache"`
	IsolatedLocalCache  bool             `yaml:"isolated_local_cache"`
	RemoteCache         bool             `yaml:"remote_cache"`
	TTL                 time.Duration    `yaml:"ttl"`
	IsolatedCacheConfig localCacheConfig `yaml:"isolated_cache_config"`
}

type localCacheConfig struct {
	Size    int64 `yaml:"size"`
	MaxKeys int64 `yaml:"max_keys"`
}

// Config returns the cache settings
func Config() Settings {
	return settings
}

// LoadCacheConfig loads the cache settings from the environment variables
func LoadCacheConfig() error {
	if err := envconfig.Process("", &settings); err != nil {
		return fmt.Errorf("[cache][LoadCacheConfig][err: %w]", err)
	}

	data, err := os.ReadFile(settings.CacheManagerConfigFile)
	if err != nil {
		return fmt.Errorf("[cache][LoadCacheConfig][err: %w]", err)
	}

	var cacheManagerSettings cacheManager
	if err = yaml.Unmarshal(data, &cacheManagerSettings); err != nil {
		return fmt.Errorf("[cache][LoadCacheConfig][err: %w]", err)
	}
	settings.CacheManager = cacheManagerSettings

	return nil
}

// LoadCacheOptions is in charge of loading cache options from config file
func LoadCacheOptions(keys KeysMap, keysIDs KeyIDsMap) error {
	for k, v := range Config().CacheManager.CachedEntities {
		cacheKeyID, ok := keysIDs[k]
		if !ok {
			return fmt.Errorf("[cache][LoadCacheOptions][err: cache config key with value %s do not exists]", k)
		}

		keys[cacheKeyID].Options = Options{
			TTL:                  v.TTL,
			IsGlobalLocalCache:   v.GlobalLocalCache,
			IsIsolatedLocalCache: v.IsolatedLocalCache,
			IsRemoteCache:        v.RemoteCache,
			IsolatedCacheConfig: LocalCacheConfig{
				Size:    v.IsolatedCacheConfig.Size,
				MaxKeys: v.IsolatedCacheConfig.MaxKeys,
			},
		}
	}
	return nil
}
