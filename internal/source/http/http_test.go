package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

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

type errorReader struct {
	err error
}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, e.err
}

func (e *errorReader) Close() error {
	return nil
}

func TestSource_ReadBodyError(t *testing.T) {
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

	// Create a custom request with a body that returns an error on Read
	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodPost, "http://"+src.ListenAddr+"/", &errorReader{err: errors.New("read error")})
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	}

	cancel()
	<-errCh
}

func TestSource_MultipleHeaders(t *testing.T) {
	// Test that multiple header values are handled correctly
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

	// Send request with multiple values for same header
	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodPost, "http://"+src.ListenAddr+"/", bytes.NewReader([]byte(`test`)))
	req.Header.Add("X-Custom", "value1")
	req.Header.Add("X-Custom", "value2")  // Second value will be ignored, only first is taken

	resp, err := client.Do(req)
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
	// Should get first value when multiple values exist
	if received[0].Headers["X-Custom"] != "value1" {
		t.Errorf("expected X-Custom: value1, got %s", received[0].Headers["X-Custom"])
	}
}

func TestSource_AddressAlreadyInUse(t *testing.T) {
	// Start first source to occupy an address
	src1, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source 1: %v", err)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	errCh1 := make(chan error, 1)
	go func() {
		errCh1 <- src1.Start(ctx1, func(_ context.Context, evt source.Event) error {
			return nil
		})
	}()
	<-src1.ready

	// Try to start second source on the same address
	src2, err := NewSource(Config{ListenAddr: src1.ListenAddr}, nil)
	if err != nil {
		t.Fatalf("new source 2: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	err = src2.Start(ctx2, func(_ context.Context, evt source.Event) error {
		return nil
	})

	if err == nil {
		t.Fatal("expected error when address is already in use")
	}

	// Clean up first source
	cancel1()
	<-errCh1
}

func TestSource_EmptyHeaderValue(t *testing.T) {
	// Test handling of headers with empty values
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

	// Send request with header that has no value (empty slice)
	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodPost, "http://"+src.ListenAddr+"/", bytes.NewReader([]byte(`test`)))
	// Manually create a header with empty values to test the len(v) > 0 check
	req.Header["X-Empty"] = []string{}

	resp, err := client.Do(req)
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
	// Empty header should not be included
	if _, exists := received[0].Headers["X-Empty"]; exists {
		t.Errorf("expected X-Empty header to be excluded, but it exists")
	}
}

func TestSource_ConcurrentRequests(t *testing.T) {
	// Test that multiple concurrent requests are handled properly
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

	// Send multiple concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			resp, err := http.Post("http://"+src.ListenAddr+"/", "application/json",
				bytes.NewReader([]byte(fmt.Sprintf(`{"id":%d}`, n))))
			if err != nil {
				t.Errorf("post %d: %v", n, err)
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("post %d: expected 200, got %d", n, resp.StatusCode)
			}
		}(i)
	}

	wg.Wait()

	cancel()
	<-errCh

	mu.Lock()
	defer mu.Unlock()
	if count != 10 {
		t.Errorf("expected 10 events, got %d", count)
	}
}

func TestSource_RapidStartStop(t *testing.T) {
	// Test rapid start/stop cycles to potentially trigger edge cases
	for i := 0; i < 5; i++ {
		src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
		if err != nil {
			t.Fatalf("iteration %d: new source: %v", i, err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- src.Start(ctx, func(_ context.Context, evt source.Event) error {
				return nil
			})
		}()
		<-src.ready

		// Immediately cancel
		cancel()
		err = <-errCh
		if err != context.Canceled {
			t.Errorf("iteration %d: expected context.Canceled, got %v", i, err)
		}
	}
}

// failingListener wraps a real listener but fails after accepting one connection
type failingListener struct {
	net.Listener
	acceptCount int
	failAfter   int
}

func (f *failingListener) Accept() (net.Conn, error) {
	f.acceptCount++
	if f.acceptCount > f.failAfter {
		return nil, errors.New("listener forced failure for testing")
	}
	return f.Listener.Accept()
}

func TestSource_ListenerFailureDuringServe(t *testing.T) {
	// Test that errors from listener.Accept() during Serve are propagated correctly
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	// Replace the listener factory with one that returns a failing listener
	src.listenerFactory = func(network, address string) (net.Listener, error) {
		lis, err := net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		// Wrap with a listener that will fail after 1 successful accept
		return &failingListener{Listener: lis, failAfter: 1}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(_ context.Context, evt source.Event) error {
			return nil
		})
	}()
	<-src.ready

	// Make one successful request (will be accepted)
	resp, err := http.Post("http://"+src.ListenAddr+"/", "application/json",
		bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("first post: %v", err)
	}
	_ = resp.Body.Close()

	// Try another request that should trigger the listener failure
	// Give it a short timeout since the listener might not accept it
	client := &http.Client{Timeout: 2 * time.Second}
	go func() {
		_, _ = client.Post("http://"+src.ListenAddr+"/", "application/json",
			bytes.NewReader([]byte(`{}`)))
	}()

	// The listener failure should be propagated through errCh
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error from listener failure, got nil")
		}
		if err == context.Canceled {
			t.Error("expected listener error, got context.Canceled")
		}
		// Success - we got an error from the failing listener
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for listener failure error")
		cancel()
		<-errCh
	}
}
