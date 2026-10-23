package envtest

import (
	"os"
	"testing"
)

// SetDefaultVars sets test environment defaults without overriding caller-provided values.
func SetDefaultVars(t testing.TB, vars map[string]string) {
	t.Helper()

	for key, value := range vars {
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("set env %s failed: %v", key, err)
		}
	}
}
