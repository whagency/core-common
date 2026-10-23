package errorxtest

import (
	"testing"

	"github.com/whagency/core-common/errorsx"
)

// AssertErrorCode fails the test if err is nil, is not an ErrorResponse,
// or if its code does not match the expected value.
func AssertErrorCode(t testing.TB, err error, code int) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	appErr, ok := errorsx.AsErrorResponse(err)
	if !ok {
		t.Fatalf("expected ErrorResponse, got %T: %v", err, err)
	}
	if appErr.Code != code {
		t.Fatalf("unexpected error code: got %d, want %d", appErr.Code, code)
	}
}
