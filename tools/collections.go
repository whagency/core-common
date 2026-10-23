package tools

import "fmt"

// UniqueStableNonZero removes duplicates and zero values while preserving first-seen order.
// It returns the filtered slice and a flag indicating whether any zero value was present.
func UniqueStableNonZero[T comparable](values []T) ([]T, bool) {
	if len(values) == 0 {
		return nil, false
	}

	var zero T
	seen := make(map[T]struct{}, len(values))
	out := make([]T, 0, len(values))
	hasZero := false
	for i := range values {
		value := values[i]
		if value == zero {
			hasZero = true
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}
		out = append(out, value)
	}

	return out, hasZero
}

// StringValues converts Stringer values to their string representations.
func StringValues[T fmt.Stringer](values []T) []string {
	result := make([]string, 0, len(values))
	for i := range values {
		result = append(result, values[i].String())
	}

	return result
}
