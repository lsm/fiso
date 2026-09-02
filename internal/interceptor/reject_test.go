package interceptor

import (
	"errors"
	"fmt"
	"testing"
)

// TestRejectedError_Error pins the message shape used in logs and responses.
func TestRejectedError_Error(t *testing.T) {
	rej := &RejectedError{Status: 403, Reason: "forbidden"}
	want := "rejected with status 403: forbidden"
	if rej.Error() != want {
		t.Fatalf("Error() = %q, want %q", rej.Error(), want)
	}
}

// TestAsRejection pins the classification used by every consumer of the
// contract: direct, wrapped, and non-rejection errors (ADR 0007).
func TestAsRejection(t *testing.T) {
	rej := &RejectedError{Status: 401, Reason: "missing credentials"}

	if got, ok := AsRejection(rej); !ok || got != rej {
		t.Fatalf("direct rejection must classify, got (%v, %v)", got, ok)
	}

	wrapped := fmt.Errorf("chain: %w", rej)
	if got, ok := AsRejection(wrapped); !ok || got.Status != 401 {
		t.Fatalf("wrapped rejection must classify, got (%v, %v)", got, ok)
	}

	if _, ok := AsRejection(errors.New("module crashed")); ok {
		t.Fatal("an ordinary error must not classify as a rejection")
	}

	if _, ok := AsRejection(nil); ok {
		t.Fatal("nil must not classify as a rejection")
	}
}
