package grpc

import (
	"net/http"

	"google.golang.org/grpc/codes"
)

// MapHTTPStatusToCode converts HTTP status codes used inside the domain error
// catalog into the closest matching gRPC status codes. This keeps the transport
// representation consistent across both HTTP and gRPC stacks.
func MapHTTPStatusToCode(statusCode int) codes.Code {
	switch statusCode {
	case http.StatusNoContent:
		return codes.OK
	case http.StatusBadRequest:
		return codes.InvalidArgument
	case http.StatusUnprocessableEntity:
		return codes.InvalidArgument
	case http.StatusUnauthorized:
		return codes.Unauthenticated
	case http.StatusForbidden:
		return codes.PermissionDenied
	case http.StatusNotFound:
		return codes.NotFound
	case http.StatusConflict:
		return codes.FailedPrecondition
	case http.StatusTooManyRequests:
		return codes.ResourceExhausted
	default:
		return codes.Internal
	}
}
