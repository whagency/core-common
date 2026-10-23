package cache

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCleanMap_StoresCleanMap(t *testing.T) {
	ctx := NewCleanMap(context.Background())

	cm, ok := ctx.Value(_cleanCtxKey).(*cleanMap)
	assert.True(t, ok)
	if !assert.NotNil(t, cm) {
		return
	}
	assert.Len(t, cm.entries, 0)
}

func TestNewCleanMap_NilContextDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewCleanMap(nil) panicked: %v", r)
		}
	}()

	ctx := NewCleanMap(nil)
	assert.NotNil(t, ctx)
}

func TestSetCleanFunc_NoCollisionForSameKeyAcrossRepos(t *testing.T) {
	ctx := NewCleanMap(context.Background())

	repoA := &Repository[int]{name: "repo-a"}
	repoB := &Repository[int]{name: "repo-b"}

	repoA.setCleanFunc(ctx, "same-key")
	repoB.setCleanFunc(ctx, "same-key")

	cm, ok := ctx.Value(_cleanCtxKey).(*cleanMap)
	if !assert.True(t, ok) || !assert.NotNil(t, cm) {
		return
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	assert.Len(t, cm.entries, 2)
	entryA, okA := cm.entries["repo-a:same-key"]
	entryB, okB := cm.entries["repo-b:same-key"]
	assert.True(t, okA)
	assert.True(t, okB)
	assert.Equal(t, "same-key", entryA.key)
	assert.Equal(t, "same-key", entryB.key)
}

func TestCleanKeys_InvokesEachRegisteredEntryWithOriginalKey(t *testing.T) {
	called := make(map[string]int)
	var calledMu sync.Mutex

	ctx := context.WithValue(context.Background(), _cleanCtxKey, &cleanMap{
		entries: map[string]cleanEntry{
			"repo-a:same-key": {
				key: "same-key",
				fn: func(_ context.Context, key string) error {
					calledMu.Lock()
					called["repo-a:"+key]++
					calledMu.Unlock()
					return nil
				},
			},
			"repo-b:same-key": {
				key: "same-key",
				fn: func(_ context.Context, key string) error {
					calledMu.Lock()
					called["repo-b:"+key]++
					calledMu.Unlock()
					return nil
				},
			},
		},
	})

	CleanKeys(ctx)

	assert.Equal(t, 1, called["repo-a:same-key"])
	assert.Equal(t, 1, called["repo-b:same-key"])
}

func TestCleanKeys_NilContextDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CleanKeys(nil) panicked: %v", r)
		}
	}()

	CleanKeys(nil)
}
