package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/whagency/core-common/tools"
)

type LockerOptions struct {
	Name        string
	RemoteCache RemoteCacheManager
	TTL         time.Duration
}

type Locker struct {
	name        string
	remoteCache RemoteCacheManager
	ttl         time.Duration
}

// NewLocker creates a new Locker instance
func NewLocker(o LockerOptions) *Locker {
	ttl := o.TTL
	if ttl <= 0 {
		ttl = Config().TTL
	}

	return &Locker{
		name:        o.Name,
		remoteCache: o.RemoteCache,
		ttl:         ttl,
	}
}

// TryLock returns true if the key is locked. Otherwise, it locks it and returns false
func (l Locker) TryLock(ctx context.Context, key string) error {
	if l.remoteCache == nil {
		return fmt.Errorf("[cache][Locker:TryLock][err: remote cache manager is nil]")
	}

	wasLocked, err := l.remoteCache.PutIfAbsentWithTTL(ctx, l.name, key, struct{}{}, l.ttl)
	if err != nil {
		return fmt.Errorf("[cache][Locker:TryLock]%w", err)
	}

	// if there is any previous value, it means the key was already locked
	if !tools.IsNil(wasLocked) {
		return fmt.Errorf("[cache][Locker:TryLock][err: %w]", KeyAlreadyLockedErr)
	}

	return nil
}

// Unlock unlocks the key
func (l Locker) Unlock(ctx context.Context, key string) error {
	if l.remoteCache == nil {
		return fmt.Errorf("[cache][Locker:Unlock][err: remote cache manager is nil]")
	}

	if err := l.remoteCache.DeleteKey(ctx, l.name, key); err != nil {
		return fmt.Errorf("[cache][Locker:Unlock]%w", err)
	}

	return nil
}
