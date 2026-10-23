package loggertest

import (
	"sync"
	"testing"

	"github.com/whagency/core-common/logger"
)

var initOnce sync.Once

// Init initializes a minimal shared logger for tests.
func Init(t testing.TB, appName string) {
	t.Helper()

	initOnce.Do(func() {
		if err := logger.Init(logger.Config{
			AppName:      appName,
			Environment:  "test",
			Version:      "test",
			Level:        "error",
			StdoutFormat: logger.StdoutFormatConsole,
		}); err != nil {
			t.Fatalf("logger.Init failed: %v", err)
		}
	})
}
