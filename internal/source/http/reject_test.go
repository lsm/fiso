package http

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/lsm/fiso/internal/interceptor"
	"github.com/lsm/fiso/internal/source"
)

// TestSource_Rejection_MapsStatus pins the rejection contract at the HTTP
// surface: a typed rejection from the pipeline handler answers with the
// guest-chosen status and reason, not a blanket 500 (ADR 0007).
func TestSource_Rejection_MapsStatus(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(_ context.Context, _ source.Event) error {
			return &interceptor.RejectedError{Status: 401, Reason: "missing credentials"}
		})
	}()
	<-src.ready

	resp, err := http.Post("http://"+src.ListenAddr+"/", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Contains(body, []byte("missing credentials")) {
		t.Fatalf("expected the rejection reason in the body, got %q", body)
	}

	cancel()
	<-errCh
}
