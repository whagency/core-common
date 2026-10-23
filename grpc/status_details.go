package grpc

import (
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

// ExtractErrorInfoReason returns the first non-empty ErrorInfo reason from a gRPC error.
func ExtractErrorInfoReason(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return ""
	}

	return ExtractStatusErrorInfoReason(st)
}

// ExtractStatusErrorInfoReason returns the first non-empty ErrorInfo reason from gRPC status details.
func ExtractStatusErrorInfoReason(st *status.Status) string {
	if st == nil {
		return ""
	}

	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			if reason := strings.TrimSpace(info.GetReason()); reason != "" {
				return reason
			}
		}
	}

	return ""
}
