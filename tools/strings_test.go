package tools

import "testing"

func TestTrimmedOrNil(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if got := TrimmedOrNil(nil); got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("blank becomes nil", func(t *testing.T) {
		value := "   "
		if got := TrimmedOrNil(&value); got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("trimmed value", func(t *testing.T) {
		value := "  abc  "
		got := TrimmedOrNil(&value)
		if got == nil || *got != "abc" {
			t.Fatalf("expected trimmed pointer, got %#v", got)
		}
	})
}
