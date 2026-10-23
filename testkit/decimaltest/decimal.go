package decimaltest

import (
	"testing"

	"github.com/shopspring/decimal"
)

// Must parses a decimal string or fails the current test.
func Must(t testing.TB, value string) decimal.Decimal {
	t.Helper()

	parsed, err := decimal.NewFromString(value)
	if err != nil {
		t.Fatalf("parse decimal %q failed: %v", value, err)
	}
	return parsed
}

// MustPtr parses a decimal string and returns a pointer to it.
func MustPtr(t testing.TB, value string) *decimal.Decimal {
	t.Helper()

	parsed := Must(t, value)
	return &parsed
}
