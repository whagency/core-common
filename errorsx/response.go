package errorsx

import (
	"errors"
	"fmt"
)

// ErrorData represents a key-value map for business-specific error details.
// Example: {"current_balance": 10000, "required_balance": 15000}
type ErrorData map[string]any

// ErrorResponse represents a structured error response.
type ErrorResponse struct {
	Code       int
	HTTPStatus int
	Message    string
	Data       ErrorData
	Cause      error
}

// Error implements the error interface for ErrorResponse.
func (e *ErrorResponse) Error() string {
	if e.HasCause() {
		return e.Cause.Error()
	}
	return fmt.Sprintf("code=%d http=%d message=%s", e.Code, e.HTTPStatus, e.Message)
}

// Unwrap allows errors.Is / errors.As.
func (e *ErrorResponse) Unwrap() error { return e.Cause }

// HasCause returns true if Cause is set.
func (e *ErrorResponse) HasCause() bool { return e.Cause != nil }

// SetCause sets the underlying cause of the error.
func (e *ErrorResponse) SetCause(err error) {
	e.Cause = err
}

// WrapCause wraps the existing cause with a new error message, preserving the original error chain.
func (e *ErrorResponse) WrapCause(prefix string) {
	if e == nil || e.Cause == nil {
		return
	}
	e.Cause = fmt.Errorf("%s%w", prefix, e.Cause)
}

// AsErrorResponse attempts to cast an error to an *ErrorResponse.
func AsErrorResponse(err error) (*ErrorResponse, bool) {
	var dst *ErrorResponse
	if errors.As(err, &dst) {
		return dst, true
	}
	return nil, false
}
