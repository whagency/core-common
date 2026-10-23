package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	_defaultVisitorTTL      = 30 * time.Minute
	_defaultRPS             = 1 // default requests per second; tune per deployment
	_defaultBurst           = 2 // default maximum burst size; tune per deployment
	_defaultCleanupInterval = 10 * time.Minute
)

type LimiterConfig struct {
	RPS   rate.Limit
	Burst int
	TTL   time.Duration
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type LimiterStore struct {
	mu          sync.Mutex
	visitors    map[string]*visitor
	cfg         LimiterConfig
	lastCleanup time.Time
}

// NewLimiterStore creates a new LimiterStore instance.
func NewLimiterStore(cfg LimiterConfig) *LimiterStore {
	if cfg.TTL <= 0 {
		cfg.TTL = _defaultVisitorTTL
	}
	if cfg.RPS <= 0 {
		cfg.RPS = _defaultRPS
	}
	if cfg.Burst <= 0 {
		cfg.Burst = _defaultBurst
	}

	return &LimiterStore{
		visitors:    make(map[string]*visitor),
		cfg:         cfg,
		lastCleanup: time.Now().UTC(),
	}
}

// Get retrieves the rate limiter for the given key, creating one if it doesn't exist.
func (s *LimiterStore) Get(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()

	if now.Sub(s.lastCleanup) > _defaultCleanupInterval {
		s.cleanupExpiredUnsafe(now)
		s.lastCleanup = now
	}

	if cl, ok := s.visitors[key]; ok {
		cl.lastSeen = now
		return cl.limiter
	}

	lim := rate.NewLimiter(s.cfg.RPS, s.cfg.Burst)
	s.visitors[key] = &visitor{
		limiter:  lim,
		lastSeen: now,
	}
	return lim
}

// cleanupExpiredUnsafe removes expired visitors from the store.
func (s *LimiterStore) cleanupExpiredUnsafe(now time.Time) {
	for key, v := range s.visitors {
		if now.Sub(v.lastSeen) > s.cfg.TTL {
			delete(s.visitors, key)
		}
	}
}
