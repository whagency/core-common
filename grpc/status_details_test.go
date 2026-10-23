package grpc

import (
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestExtractErrorInfoReason(t *testing.T) {
	st, err := status.New(codes.InvalidArgument, "bad request").WithDetails(&errdetails.ErrorInfo{Reason: "merchant_disabled"})
	if err != nil {
		t.Fatalf("WithDetails() error = %v", err)
	}

	got := ExtractErrorInfoReason(st.Err())
	if got != "merchant_disabled" {
		t.Fatalf("ExtractErrorInfoReason() = %q, want %q", got, "merchant_disabled")
	}
}

func TestExtractErrorInfoReasonEmpty(t *testing.T) {
	if got := ExtractErrorInfoReason(status.Error(codes.Internal, "boom")); got != "" {
		t.Fatalf("ExtractErrorInfoReason() = %q, want empty", got)
	}
}
