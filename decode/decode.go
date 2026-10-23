package decode

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ParseRequiredString trims a transport value and returns it when non-empty.
func ParseRequiredString(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("[decode][ParseRequiredString][err: %s is required]", field)
	}

	return trimmed, nil
}

// ParseRequiredUpperString trims a transport value, uppercases it and returns it when non-empty.
func ParseRequiredUpperString(field, value string) (string, error) {
	trimmed, err := ParseRequiredString(field, value)
	if err != nil {
		return "", err
	}

	return strings.ToUpper(trimmed), nil
}

// ParseRequiredUUID trims a transport value, parses it as UUID and rejects zero UUID.
func ParseRequiredUUID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("[decode][ParseRequiredUUID]%w", err)
	}
	if parsed == uuid.Nil {
		return uuid.Nil, fmt.Errorf("[decode][ParseRequiredUUID][err: uuid must not be zero]")
	}

	return parsed, nil
}

// RequirePositiveUint64 validates a required positive uint64 transport field.
func RequirePositiveUint64(field string, value uint64) error {
	if value == 0 {
		return fmt.Errorf("[decode][RequirePositiveUint64][err: %s is required]", field)
	}

	return nil
}

// RequirePositiveInt64 validates a required positive int64 transport field.
func RequirePositiveInt64(field string, value int64) error {
	if value <= 0 {
		return fmt.Errorf("[decode][RequirePositiveInt64][err: %s must be greater than zero]", field)
	}

	return nil
}

// ParseRequiredDecimal trims a transport value and parses it as decimal.Decimal.
func ParseRequiredDecimal(field, value string) (decimal.Decimal, error) {
	trimmed, err := ParseRequiredString(field, value)
	if err != nil {
		return decimal.Decimal{}, err
	}

	parsed, err := decimal.NewFromString(trimmed)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("[decode][ParseRequiredDecimal][err: invalid %s format]", field)
	}

	return parsed, nil
}

// RequirePositiveDecimal validates a decimal field that must be strictly positive.
func RequirePositiveDecimal(field string, value decimal.Decimal) error {
	if !value.IsPositive() {
		return fmt.Errorf("[decode][RequirePositiveDecimal][err: %s must be greater than zero]", field)
	}

	return nil
}

// RequireNonNegativeDecimal validates a decimal field that must not be negative.
func RequireNonNegativeDecimal(field string, value decimal.Decimal) error {
	if value.IsNegative() {
		return fmt.Errorf("[decode][RequireNonNegativeDecimal][err: %s must not be negative]", field)
	}

	return nil
}
