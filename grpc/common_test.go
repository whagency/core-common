package grpc

import (
	"net/http"
	"testing"

	"google.golang.org/grpc/codes"
)

func TestMapHTTPStatusToCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       codes.Code
	}{
		{
			name:       "204 maps to ok",
			statusCode: http.StatusNoContent,
			want:       codes.OK,
		},
		{
			name:       "400 maps to invalid argument",
			statusCode: http.StatusBadRequest,
			want:       codes.InvalidArgument,
		},
		{
			name:       "401 maps to unauthenticated",
			statusCode: http.StatusUnauthorized,
			want:       codes.Unauthenticated,
		},
		{
			name:       "403 maps to permission denied",
			statusCode: http.StatusForbidden,
			want:       codes.PermissionDenied,
		},
		{
			name:       "404 maps to not found",
			statusCode: http.StatusNotFound,
			want:       codes.NotFound,
		},
		{
			name:       "409 maps to failed precondition",
			statusCode: http.StatusConflict,
			want:       codes.FailedPrecondition,
		},
		{
			name:       "422 maps to invalid argument",
			statusCode: http.StatusUnprocessableEntity,
			want:       codes.InvalidArgument,
		},
		{
			name:       "429 maps to resource exhausted",
			statusCode: http.StatusTooManyRequests,
			want:       codes.ResourceExhausted,
		},
		{
			name:       "unknown maps to internal",
			statusCode: http.StatusTeapot,
			want:       codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapHTTPStatusToCode(tt.statusCode)
			if got != tt.want {
				t.Fatalf("MapHTTPStatusToCode(%d) = %s, want %s", tt.statusCode, got, tt.want)
			}
		})
	}
}
