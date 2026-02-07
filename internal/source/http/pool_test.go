package http

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/lsm/fiso/internal/source"
)

func TestServerPool_SingleServer_MultiplePaths(t *testing.T) {
	pool := NewServerPool(nil)

	var muA, muB sync.Mutex
	var countA, countB int

	// Register two paths on the same address
	handleA, err := pool.Register("127.0.0.1:0", "/path-a", func(_ context.Context, evt source.Event) error {
		muA.Lock()
		countA++
		muA.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("register path-a: %v", err)
	}

	handleB, err := pool.Register("127.0.0.1:0", "/path-b", func(_ context.Context, evt source.Event) error {
		muB.Lock()
		countB++
		muB.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("register path-b: %v", err)
	}

	if handleA.addr != handleB.addr {
		t.Errorf("expected same address, got %s and %s", handleA.addr, handleB.addr)
	}

	if pool.ServerCount() != 1 {
		t.Errorf("expected 1 server, got %d", pool.ServerCount())
	}

	if pool.RouteCount() != 2 {
		t.Errorf("expected 2 routes, got %d", pool.RouteCount())
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- pool.Start(ctx)
	}()

	// Wait for server to be ready
	pool.WaitReady()

	// Get the actual listen address from the pool
	addr := pool.ListenAddr("127.0.0.1:0")

	// Send requests to both paths
	resp, err := http.Post("http://"+addr+"/path-a", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post path-a: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for path-a, got %d", resp.StatusCode)
	}

	resp, err = http.Post("http://"+addr+"/path-b", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post path-b: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for path-b, got %d", resp.StatusCode)
	}

	cancel()
	<-errCh

	muA.Lock()
	muB.Lock()
	if countA != 1 {
		t.Errorf("expected countA=1, got %d", countA)
	}
	if countB != 1 {
		t.Errorf("expected countB=1, got %d", countB)
	}
	muB.Unlock()
	muA.Unlock()
}

func TestServerPool_MultipleServers(t *testing.T) {
	pool := NewServerPool(nil)

	// Register paths on different addresses
	_, err := pool.Register("127.0.0.1:0", "/path-a", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("register path-a: %v", err)
	}

	_, err = pool.Register("127.0.0.1:0", "/path-b", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("register path-b: %v", err)
	}

	// Different address creates new server
	_, err = pool.Register("127.0.0.2:0", "/path-c", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("register path-c: %v", err)
	}

	if pool.ServerCount() != 2 {
		t.Errorf("expected 2 servers, got %d", pool.ServerCount())
	}

	if pool.RouteCount() != 3 {
		t.Errorf("expected 3 routes, got %d", pool.RouteCount())
	}
}

func TestServerPool_DuplicatePath(t *testing.T) {
	pool := NewServerPool(nil)

	_, err := pool.Register("127.0.0.1:0", "/path", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("first register: %v", err)
	}

	_, err = pool.Register("127.0.0.1:0", "/path", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for duplicate path")
	}
}

func TestServerPool_EmptyAddress(t *testing.T) {
	pool := NewServerPool(nil)

	_, err := pool.Register("", "/path", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for empty address")
	}
}

func TestServerPool_DefaultPath(t *testing.T) {
	pool := NewServerPool(nil)

	_, err := pool.Register("127.0.0.1:0", "", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("register default path: %v", err)
	}

	// Empty path should default to "/"
	pool.mu.RLock()
	srv := pool.servers["127.0.0.1:0"]
	pool.mu.RUnlock()

	srv.mu.Lock()
	_, exists := srv.handlers["/"]
	srv.mu.Unlock()

	if !exists {
		t.Error("expected handler registered at /")
	}
}

func TestServerPool_HandlerError(t *testing.T) {
	pool := NewServerPool(nil)

	_, err := pool.Register("127.0.0.1:0", "/error", func(_ context.Context, evt source.Event) error {
		return errors.New("handler failed")
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- pool.Start(ctx)
	}()

	pool.WaitReady()
	addr := pool.ListenAddr("127.0.0.1:0")

	resp, err := http.Post("http://"+addr+"/error", "application/json", bytes.NewReader([]byte(`{}`)))
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

func TestServerPool_MethodNotAllowed(t *testing.T) {
	pool := NewServerPool(nil)

	_, err := pool.Register("127.0.0.1:0", "/", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- pool.Start(ctx)
	}()

	pool.WaitReady()
	addr := pool.ListenAddr("127.0.0.1:0")

	resp, err := http.Get("http://" + addr + "/")
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

func TestServerPool_NoServers(t *testing.T) {
	pool := NewServerPool(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := pool.Start(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestServerPool_Close(t *testing.T) {
	pool := NewServerPool(nil)

	_, err := pool.Register("127.0.0.1:0", "/", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- pool.Start(ctx)
	}()

	pool.WaitReady()

	// Close should work
	if err := pool.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	cancel()
	<-errCh
}

func TestServerPool_ConcurrentRequests(t *testing.T) {
	pool := NewServerPool(nil)

	var mu sync.Mutex
	counts := make(map[string]int)

	_, err := pool.Register("127.0.0.1:0", "/a", func(_ context.Context, evt source.Event) error {
		mu.Lock()
		counts["a"]++
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("register a: %v", err)
	}

	_, err = pool.Register("127.0.0.1:0", "/b", func(_ context.Context, evt source.Event) error {
		mu.Lock()
		counts["b"]++
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("register b: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- pool.Start(ctx)
	}()

	pool.WaitReady()
	addr := pool.ListenAddr("127.0.0.1:0")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			path := "/a"
			if n%2 == 1 {
				path = "/b"
			}
			resp, err := http.Post("http://"+addr+path, "application/json", bytes.NewReader([]byte(`{}`)))
			if err != nil {
				t.Errorf("post %d: %v", n, err)
				return
			}
			_ = resp.Body.Close()
		}(i)
	}

	wg.Wait()

	cancel()
	<-errCh

	mu.Lock()
	defer mu.Unlock()

	if counts["a"] != 10 {
		t.Errorf("expected 10 requests to /a, got %d", counts["a"])
	}
	if counts["b"] != 10 {
		t.Errorf("expected 10 requests to /b, got %d", counts["b"])
	}
}

func TestServerPool_HeadersPassed(t *testing.T) {
	pool := NewServerPool(nil)

	var receivedHeaders map[string]string
	var mu sync.Mutex

	_, err := pool.Register("127.0.0.1:0", "/", func(_ context.Context, evt source.Event) error {
		mu.Lock()
		receivedHeaders = evt.Headers
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- pool.Start(ctx)
	}()

	pool.WaitReady()
	addr := pool.ListenAddr("127.0.0.1:0")

	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Custom-Header", "test-value")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()

	cancel()
	<-errCh

	mu.Lock()
	defer mu.Unlock()

	if receivedHeaders["X-Custom-Header"] != "test-value" {
		t.Errorf("expected header X-Custom-Header=test-value, got %v", receivedHeaders)
	}
}

func TestServerPool_ListenAddr_NotStarted(t *testing.T) {
	pool := NewServerPool(nil)

	_, err := pool.Register("127.0.0.1:0", "/", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// ListenAddr should return empty before start
	addr := pool.ListenAddr("127.0.0.1:0")
	if addr != "" {
		t.Errorf("expected empty addr before start, got %s", addr)
	}

	// ListenAddr for non-existent server
	addr = pool.ListenAddr("127.0.0.2:0")
	if addr != "" {
		t.Errorf("expected empty addr for non-existent server, got %s", addr)
	}
}

func TestPooledSource_Start(t *testing.T) {
	pool := NewServerPool(nil)

	src, err := NewPooledSource(pool, Config{ListenAddr: "127.0.0.1:0", Path: "/test"})
	if err != nil {
		t.Fatalf("new pooled source: %v", err)
	}

	var received bool
	var mu sync.Mutex

	ctx, cancel := context.WithCancel(context.Background())

	// Start the source in background (registers handler and blocks)
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(_ context.Context, evt source.Event) error {
			mu.Lock()
			received = true
			mu.Unlock()
			return nil
		})
	}()

	// Give time for registration to complete
	time.Sleep(10 * time.Millisecond)

	// Now start the pool
	go func() {
		_ = pool.Start(ctx)
	}()

	// Wait for pool to be ready
	pool.WaitReady()
	addr := pool.ListenAddr("127.0.0.1:0")

	// Send request
	resp, err := http.Post("http://"+addr+"/test", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	cancel()
	err = <-errCh
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !received {
		t.Error("expected handler to be called")
	}
}

func TestPooledSource_EmptyAddress(t *testing.T) {
	pool := NewServerPool(nil)

	_, err := NewPooledSource(pool, Config{ListenAddr: "", Path: "/test"})
	if err == nil {
		t.Fatal("expected error for empty address")
	}
}

func TestPooledSource_DefaultPath(t *testing.T) {
	pool := NewServerPool(nil)

	src, err := NewPooledSource(pool, Config{ListenAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("new pooled source: %v", err)
	}

	if src.path != "/" {
		t.Errorf("expected default path /, got %s", src.path)
	}
}

func TestPooledSource_Close(t *testing.T) {
	pool := NewServerPool(nil)

	src, err := NewPooledSource(pool, Config{ListenAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("new pooled source: %v", err)
	}

	// Close is a no-op for pooled sources
	if err := src.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPooledSource_DuplicateRegistration(t *testing.T) {
	pool := NewServerPool(nil)

	// Register first source
	_, err := pool.Register("127.0.0.1:0", "/path", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("first register: %v", err)
	}

	// Try to register pooled source with same path
	src, err := NewPooledSource(pool, Config{ListenAddr: "127.0.0.1:0", Path: "/path"})
	if err != nil {
		t.Fatalf("new pooled source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start should fail due to duplicate path
	err = src.Start(ctx, func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err == nil {
		t.Error("expected error for duplicate path")
	}
}

func TestServerPool_Start_InvalidAddress(t *testing.T) {
	pool := NewServerPool(nil)

	// Register with an invalid address that will fail on listen
	_, err := pool.Register("999.999.999.999:99999", "/", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start should fail due to listen error
	err = pool.Start(ctx)
	if err == nil {
		t.Error("expected error for invalid address")
	}
}

func TestServerPool_Close_NotStarted(t *testing.T) {
	pool := NewServerPool(nil)

	_, err := pool.Register("127.0.0.1:0", "/", func(_ context.Context, evt source.Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Close before start should work (no server to shutdown)
	if err := pool.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}
