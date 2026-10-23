package decimaltest

import "testing"

func TestMustPtr(t *testing.T) {
	value := MustPtr(t, "10.25")

	if value == nil || value.String() != "10.25" {
		t.Fatalf("unexpected decimal pointer: %#v", value)
	}
}
