package envtest

import (
	"os"
	"testing"
)

func TestSetDefaultVarsDoesNotOverrideExistingValues(t *testing.T) {
	t.Setenv("APP_TESTKIT_EXISTING", "custom")

	SetDefaultVars(t, map[string]string{
		"APP_TESTKIT_EXISTING": "default",
		"APP_TESTKIT_MISSING":  "created",
	})

	if got := os.Getenv("APP_TESTKIT_EXISTING"); got != "custom" {
		t.Fatalf("expected existing value to stay custom, got %q", got)
	}
	if got := os.Getenv("APP_TESTKIT_MISSING"); got != "created" {
		t.Fatalf("expected missing value to be created, got %q", got)
	}
}
