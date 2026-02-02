package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/lsm/fiso/internal/source"
)

func TestSource_ReceivesEvents(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	var mu sync.Mutex
	var received []source.Event

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(_ context.Context, evt source.Event) error {
			mu.Lock()
			received = append(received, evt)
			mu.Unlock()
			return nil
		})
	}()

	<-src.ready

	resp, err := http.Post("http://"+src.ListenAddr+"/", "application/json", bytes.NewReader([]byte(`{"id":"test-1"}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	cancel()
	<-errCh

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if string(received[0].Value) != `{"id":"test-1"}` {
		t.Errorf("expected event data, got %s", received[0].Value)
	}
	if received[0].Headers["Content-Type"] != "application/json" {
		t.Errorf("expected content-type header, got %v", received[0].Headers)
	}
	if received[0].Topic != "http" {
		t.Errorf("expected topic http, got %s", received[0].Topic)
	}
}

func TestNewSource_MissingAddress(t *testing.T) {
	_, err := NewSource(Config{}, nil)
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestSource_DefaultPath(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	if src.path != "/" {
		t.Errorf("expected default path /, got %s", src.path)
	}
}

func TestSource_CustomPath(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0", Path: "/ingest"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	var received bool

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(_ context.Context, evt source.Event) error {
			received = true
			return nil
		})
	}()
	<-src.ready

	resp, err := http.Post("http://"+src.ListenAddr+"/ingest", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !received {
		t.Error("expected event to be received on custom path")
	}

	cancel()
	<-errCh
}

func TestSource_HandlerError(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(_ context.Context, evt source.Event) error {
			return errors.New("handler failed")
		})
	}()
	<-src.ready

	resp, err := http.Post("http://"+src.ListenAddr+"/", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}

	cancel()
	<-errCh
}

func TestSource_MethodNotAllowed(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(_ context.Context, evt source.Event) error {
			return nil
		})
	}()
	<-src.ready

	resp, err := http.Get("http://" + src.ListenAddr + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}

	cancel()
	<-errCh
}

func TestSource_MultipleEvents(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	var mu sync.Mutex
	var count int

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(_ context.Context, evt source.Event) error {
			mu.Lock()
			count++
			mu.Unlock()
			return nil
		})
	}()
	<-src.ready

	for i := 0; i < 5; i++ {
		resp, err := http.Post("http://"+src.ListenAddr+"/", "application/json",
			bytes.NewReader([]byte(fmt.Sprintf(`{"n":%d}`, i))))
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("post %d: expected 200, got %d", i, resp.StatusCode)
		}
	}

	cancel()
	<-errCh

	mu.Lock()
	defer mu.Unlock()
	if count != 5 {
		t.Errorf("expected 5 events, got %d", count)
	}
}

func TestSource_Close(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSource_ContextCancellation(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(_ context.Context, evt source.Event) error {
			return nil
		})
	}()
	<-src.ready

	cancel()
	err = <-errCh
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSource_CloseWithServer(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(_ context.Context, evt source.Event) error {
			return nil
		})
	}()
	<-src.ready

	// Close the server directly
	if err := src.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	cancel()
	<-errCh
}

func TestSource_InvalidListenAddress(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:99999"}, nil) // invalid port
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = src.Start(ctx, func(_ context.Context, evt source.Event) error {
		return nil
	})

	if err == nil {
		t.Fatal("expected error for invalid listen address")
	}
}
