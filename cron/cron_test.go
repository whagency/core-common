package cron

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whagency/core-common/logger"
)

var initTestLoggerOnce sync.Once

func initTestLogger(t *testing.T) {
	t.Helper()

	initTestLoggerOnce.Do(func() {
		err := logger.Init(logger.Config{
			AppName:     "cron-tests",
			Environment: "test",
			Level:       "debug",
		})
		require.NoError(t, err)
	})
}

func TestAddFunc_NilHandler(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()
	err := manager.AddFunc("@every 1s", "test.job", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cmd is nil")
}

func TestAddFunc_EmptyJobName(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()
	err := manager.AddFunc("@every 1s", "   ", func(context.Context) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jobName is empty")
}

func TestAddFunc_InjectsObservabilityContext(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()
	type runInfo struct {
		requestID       string
		internalTraceID string
	}
	results := make(chan runInfo, 1)

	err := manager.AddFunc("@every 20ms", "test.observability", func(ctx context.Context) error {
		select {
		case results <- runInfo{
			requestID:       logger.GetRequestID(ctx),
			internalTraceID: logger.GetInternalTraceID(ctx),
		}:
		default:
		}
		return nil
	})
	require.NoError(t, err)

	manager.Start()
	defer func() {
		stopCtx := manager.Stop()
		<-stopCtx.Done()
	}()

	select {
	case got := <-results:
		assert.NotEmpty(t, got.requestID)
		assert.NotEmpty(t, got.internalTraceID)
	case <-time.After(2 * time.Second):
		t.Fatal("expected AddFunc job to run at least once")
	}
}
