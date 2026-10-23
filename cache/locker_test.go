package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewLocker_UsesProvidedTTL(t *testing.T) {
	l := NewLocker(LockerOptions{
		Name: "test-locker",
		TTL:  2 * time.Second,
	})

	assert.Equal(t, 2*time.Second, l.ttl)
}

func TestNewLocker_UsesDefaultTTLFromConfig(t *testing.T) {
	prevSettings := settings
	t.Cleanup(func() {
		settings = prevSettings
	})

	settings = Settings{
		TTL: 7 * time.Second,
	}

	l := NewLocker(LockerOptions{
		Name: "test-locker",
		TTL:  0,
	})

	assert.Equal(t, 7*time.Second, l.ttl)
}

func TestLocker_TryLock_NilRemoteCacheReturnsError(t *testing.T) {
	l := NewLocker(LockerOptions{
		Name:        "test-locker",
		RemoteCache: nil,
		TTL:         2 * time.Second,
	})

	err := l.TryLock(context.Background(), "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "remote cache manager is nil")
}

func TestLocker_Unlock_NilRemoteCacheReturnsError(t *testing.T) {
	l := NewLocker(LockerOptions{
		Name:        "test-locker",
		RemoteCache: nil,
		TTL:         2 * time.Second,
	})

	err := l.Unlock(context.Background(), "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "remote cache manager is nil")
}
