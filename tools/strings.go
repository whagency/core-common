package tools

import "strings"

// TrimmedStringOrNil trims a string value and returns nil when the trimmed value is empty.
func TrimmedStringOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}

// TrimmedOrNil trims an optional string and returns nil when the trimmed value is empty.
func TrimmedOrNil(value *string) *string {
	if value == nil {
		return nil
	}

	return TrimmedStringOrNil(*value)
}
