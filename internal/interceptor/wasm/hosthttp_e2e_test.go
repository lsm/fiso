package wasm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lsm/fiso/internal/interceptor"
)

// buildHTTPCallModule compiles the guest fixture that imports
// fiso.http_call, mirroring the repo's buildTestWASMModule idiom.
func buildHTTPCallModule(t *testing.T) []byte {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "httpcall.wasm")
	cmd := exec.Command("go", "build", "-o", outPath, ".")
	cmd.Dir = "testdata/httpcall"
	cmd.Env = append(cmd.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile httpcall fixture: %v\n%s", err, out)
	}
	return mustRead(t, outPath)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// TestInterceptor_HTTPCallGuest pins the full path: a real wasip1 guest
// imports fiso.http_call, the host routes the allowed target through Link,
// and the enriched payload flows back through the interceptor envelope.
func TestInterceptor_HTTPCallGuest(t *testing.T) {
	var gotPath, gotMethod, gotHeader string
	var gotBody []byte
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Caller")
		gotBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(gotBody)
		w.Header().Set("X-Score", "7")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"risk":"low","id":"x-1"}`))
	}))
	defer link.Close()

	wasmBytes := buildHTTPCallModule(t)
	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), wasmBytes, HostHTTPConfig{
		LinkAddr:       link.URL,
		AllowedTargets: []string{"enrich-api"},
		Client:         link.Client(),
	})
	if err != nil {
		t.Fatalf("NewWazeroRuntimeWithHTTP: %v", err)
	}
	defer func() { _ = rt.Close() }()

	ict := New(rt, "httpcall.wasm")
	req := &interceptor.Request{
		Payload:   json.RawMessage(`{"customer_id":"c-9"}`),
		Headers:   map[string]string{"origin": "http"},
		Direction: "inbound",
	}
	resp, err := ict.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if gotPath != "/link/enrich-api/lookup" {
		t.Errorf("guest call not routed through Link: %q", gotPath)
	}
	if gotMethod != "POST" || gotHeader != "wasm" {
		t.Errorf("method/header not propagated: %q %q", gotMethod, gotHeader)
	}
	if string(gotBody) != `{"customer_id":"c-9"}` {
		t.Errorf("payload not passed to the target: %s", gotBody)
	}
	if resp.Headers["X-Api-Status"] != "200" {
		t.Errorf("guest did not surface the API status: %v", resp.Headers)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("response payload: %v", err)
	}
	if payload["risk"] != "low" {
		t.Errorf("enriched payload not returned: %v", payload)
	}
}

// TestInterceptor_HTTPCallGuestDenied pins that the same guest, run without
// the target on its allowlist, receives the denial error code and never
// reaches the network.
func TestInterceptor_HTTPCallGuestDenied(t *testing.T) {
	requests := 0
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer link.Close()

	wasmBytes := buildHTTPCallModule(t)
	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), wasmBytes, HostHTTPConfig{
		LinkAddr:       link.URL,
		AllowedTargets: []string{"some-other-api"},
		Client:         link.Client(),
	})
	if err != nil {
		t.Fatalf("NewWazeroRuntimeWithHTTP: %v", err)
	}
	defer func() { _ = rt.Close() }()

	ict := New(rt, "httpcall.wasm")
	resp, err := ict.Process(context.Background(), &interceptor.Request{
		Payload:   json.RawMessage(`{}`),
		Headers:   map[string]string{},
		Direction: "inbound",
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := resp.Headers["X-Host-Error"]; got != "-2" {
		t.Fatalf("expected denial code -2 surfaced by the guest, got %q (headers %v)", got, resp.Headers)
	}
	if requests != 0 {
		t.Fatalf("denied guest call reached the network: %d requests", requests)
	}
}

// TestWazeroRuntime_NoHostModuleWithoutOptIn pins that the fiso import does
// not exist unless the interceptor opted in: a module importing it fails to
// instantiate (capability absent, not merely unchecked).
func TestWazeroRuntime_NoHostModuleWithoutOptIn(t *testing.T) {
	wasmBytes := buildHTTPCallModule(t)
	rt, err := NewWazeroRuntime(context.Background(), wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	if _, err := rt.Call(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected instantiation failure for a module importing fiso.http_call without opt-in")
	}
}

// TestInterceptor_HTTPCallGuestInvalidRequest pins the -1 error path: the
// guest sends malformed request JSON to the host function.
func TestInterceptor_HTTPCallGuestInvalidRequest(t *testing.T) {
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer link.Close()

	wasmBytes := buildHTTPCallModule(t)
	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), wasmBytes, HostHTTPConfig{
		LinkAddr:       link.URL,
		AllowedTargets: []string{"enrich-api"},
		Client:         link.Client(),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	ict := New(rt, "httpcall.wasm")
	resp, err := ict.ProcessWithEnv(context.Background(), &interceptor.Request{
		Payload:   json.RawMessage(`{}`),
		Headers:   map[string]string{},
		Direction: "inbound",
	}, map[string]string{"FISO_TEST_MODE": "badreq"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := resp.Headers["X-Host-Error"]; got != "-1" {
		t.Fatalf("expected invalid-request code -1, got %q", got)
	}
}

// TestInterceptor_HTTPCallGuestSmallBuffer pins the -3 error path: the
// response does not fit the guest-provided buffer.
func TestInterceptor_HTTPCallGuestSmallBuffer(t *testing.T) {
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"risk":"low"}`))
	}))
	defer link.Close()

	wasmBytes := buildHTTPCallModule(t)
	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), wasmBytes, HostHTTPConfig{
		LinkAddr:       link.URL,
		AllowedTargets: []string{"enrich-api"},
		Client:         link.Client(),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	ict := New(rt, "httpcall.wasm")
	resp, err := ict.ProcessWithEnv(context.Background(), &interceptor.Request{
		Payload:   json.RawMessage(`{}`),
		Headers:   map[string]string{},
		Direction: "inbound",
	}, map[string]string{"FISO_TEST_MODE": "smallbuf"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := resp.Headers["X-Host-Error"]; got != "-3" {
		t.Fatalf("expected buffer-size code -3, got %q", got)
	}
}
