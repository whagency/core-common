package envtest

import (
	"os"
	"testing"
)

func TestSetDefaultVarsDoesNotOverrideExistingValues(t *testing.T) {
	t.Setenv("FINOVA_TESTKIT_EXISTING", "custom")

	SetDefaultVars(t, map[string]string{
		"FINOVA_TESTKIT_EXISTING": "default",
		"FINOVA_TESTKIT_MISSING":  "created",
	})

	if got := os.Getenv("FINOVA_TESTKIT_EXISTING"); got != "custom" {
		t.Fatalf("expected existing value to stay custom, got %q", got)
	}
	if got := os.Getenv("FINOVA_TESTKIT_MISSING"); got != "created" {
		t.Fatalf("expected missing value to be created, got %q", got)
	}
}
