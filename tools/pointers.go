package tools

import "reflect"

// ToPtr converts a value of any type to a pointer to that value.
func ToPtr[T any](v T) *T {
	return &v
}

// PtrOrZero returns the value of the pointer or the zero value of the type if nil.
func PtrOrZero[T any](ptr *T) T {
	if ptr == nil {
		var zero T
		return zero
	}
	return *ptr
}

// ClonePtr returns a pointer to a copied value, preserving nil.
func ClonePtr[T any](ptr *T) *T {
	if ptr == nil {
		return nil
	}
	v := *ptr
	return &v
}

// PtrOr returns pointer value or fallback when pointer is nil.
func PtrOr[T any](ptr *T, fallback T) T {
	if ptr == nil {
		return fallback
	}
	return *ptr
}

// IsNil checks if the given argument is nil or a nil pointer
func IsNil(arg any) bool {
	if arg == nil {
		return true
	}

	value := reflect.ValueOf(arg)
	return value.Kind() == reflect.Ptr && value.IsNil()
}
