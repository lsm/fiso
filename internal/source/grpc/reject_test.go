package grpc

import (
	"net/http"
	"testing"

	"github.com/lsm/fiso/internal/interceptor"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestGRPCStatus_Mapping pins the HTTP-to-gRPC translation of rejections: a
// refusal must surface as the closest code with the reason preserved —
// never as an Unknown internal error (ADR 0007).
func TestGRPCStatus_Mapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   codes.Code
	}{
		{"unauthorized", http.StatusUnauthorized, codes.Unauthenticated},
		{"forbidden", http.StatusForbidden, codes.PermissionDenied},
		{"bad request", http.StatusBadRequest, codes.InvalidArgument},
		{"not found", http.StatusNotFound, codes.NotFound},
		{"conflict", http.StatusConflict, codes.AlreadyExists},
		{"gone", http.StatusGone, codes.NotFound},
		{"payload too large", http.StatusRequestEntityTooLarge, codes.ResourceExhausted},
		{"too many requests", http.StatusTooManyRequests, codes.ResourceExhausted},
		{"not implemented", http.StatusNotImplemented, codes.Unimplemented},
		{"unavailable", http.StatusServiceUnavailable, codes.Unavailable},
		{"gateway timeout", http.StatusGatewayTimeout, codes.DeadlineExceeded},
		{"teapot falls back to refusal", http.StatusTeapot, codes.PermissionDenied},
		{"server error falls back to refusal", http.StatusInternalServerError, codes.PermissionDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := grpcStatus(&interceptor.RejectedError{Status: tt.status, Reason: "because"})
			got := status.Code(err)
			if got != tt.want {
				t.Fatalf("code = %v, want %v", got, tt.want)
			}
			if status.Convert(err).Message() != "because" {
				t.Fatalf("reason must be preserved, got %q", status.Convert(err).Message())
			}
		})
	}
}
