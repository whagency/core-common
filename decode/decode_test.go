package decode

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestParseRequiredString(t *testing.T) {
	value, err := ParseRequiredString("field", "  value  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "value" {
		t.Fatalf("unexpected value: %q", value)
	}

	_, err = ParseRequiredString("field", "   ")
	if err == nil {
		t.Fatal("expected error for blank value")
	}
}

func TestParseRequiredUpperString(t *testing.T) {
	value, err := ParseRequiredUpperString("asset_code", " usdt ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "USDT" {
		t.Fatalf("unexpected value: %q", value)
	}
}

func TestParseRequiredUUID(t *testing.T) {
	value, err := ParseRequiredUUID(" 550e8400-e29b-41d4-a716-446655440000 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != uuid.MustParse("550e8400-e29b-41d4-a716-446655440000") {
		t.Fatalf("unexpected uuid: %s", value)
	}

	_, err = ParseRequiredUUID("00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected zero uuid error")
	}

	_, err = ParseRequiredUUID("not-a-uuid")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestRequirePositiveNumbers(t *testing.T) {
	if err := RequirePositiveUint64("user_id", 1); err != nil {
		t.Fatalf("unexpected uint64 error: %v", err)
	}
	if err := RequirePositiveUint64("user_id", 0); err == nil {
		t.Fatal("expected uint64 error")
	}

	if err := RequirePositiveInt64("timestamp_ms", 1); err != nil {
		t.Fatalf("unexpected int64 error: %v", err)
	}
	if err := RequirePositiveInt64("timestamp_ms", 0); err == nil {
		t.Fatal("expected int64 error")
	}
}

func TestParseRequiredDecimal(t *testing.T) {
	value, err := ParseRequiredDecimal("amount", " 10.50 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !value.Equal(decimal.RequireFromString("10.50")) {
		t.Fatalf("unexpected decimal: %s", value.String())
	}

	_, err = ParseRequiredDecimal("amount", "bad")
	if err == nil {
		t.Fatal("expected decimal parse error")
	}
}

func TestDecimalGuards(t *testing.T) {
	if err := RequirePositiveDecimal("amount", decimal.RequireFromString("1")); err != nil {
		t.Fatalf("unexpected positive decimal error: %v", err)
	}
	if err := RequirePositiveDecimal("amount", decimal.RequireFromString("0")); err == nil {
		t.Fatal("expected positive decimal error")
	}

	if err := RequireNonNegativeDecimal("fee", decimal.RequireFromString("0")); err != nil {
		t.Fatalf("unexpected non-negative decimal error: %v", err)
	}
	if err := RequireNonNegativeDecimal("fee", decimal.RequireFromString("-1")); err == nil {
		t.Fatal("expected non-negative decimal error")
	}
}
