package wasm

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newTestHostClient(t *testing.T, allow []string, handler http.HandlerFunc) (*hostHTTPClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := newHostHTTPClient(HostHTTPConfig{
		LinkAddr:       srv.URL,
		AllowedTargets: allow,
		Client:         srv.Client(),
	})
	if err != nil {
		t.Fatalf("newHostHTTPClient: %v", err)
	}
	return client, srv
}

// TestHostHTTP_AllowlistDenyBeforeNetwork pins deny-by-default: a call to a
// target not on the allowlist is rejected without any network request.
func TestHostHTTP_AllowlistDenyBeforeNetwork(t *testing.T) {
	requests := 0
	client, _ := newTestHostClient(t, []string{"fraud-api"}, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	})

	_, err := client.call(context.Background(), hostHTTPRequest{Target: "other-api"})
	if err == nil {
		t.Fatal("expected denial")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("expected allowlist denial, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("denied call must not reach the network, saw %d requests", requests)
	}
}

// TestHostHTTP_RoundTrip pins the Link routing contract: target and path
// are composed onto /link/{target}{path}, method and headers pass through,
// and status/headers/body come back.
func TestHostHTTP_RoundTrip(t *testing.T) {
	var gotMethod, gotPath, gotHeader string
	var gotBody []byte
	client, _ := newTestHostClient(t, []string{"fraud-api"}, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Trace")
		gotBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(gotBody)
		w.Header().Set("X-Score", "42")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"risk":"low"}`))
	})

	resp, err := client.call(context.Background(), hostHTTPRequest{
		Target:  "fraud-api",
		Method:  "post",
		Path:    "/score",
		Headers: map[string]string{"X-Trace": "t-1"},
		BodyB64: "eyJhbW91bnQiOjEwMH0=",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST (normalized)", gotMethod)
	}
	if gotPath != "/link/fraud-api/score" {
		t.Errorf("path = %q, want Link routing", gotPath)
	}
	if gotHeader != "t-1" {
		t.Errorf("header not propagated: %q", gotHeader)
	}
	if resp.Status != http.StatusCreated {
		t.Errorf("status = %d", resp.Status)
	}
	if resp.Headers["X-Score"] != "42" {
		t.Errorf("response header missing: %v", resp.Headers)
	}
	if raw, _ := base64.StdEncoding.DecodeString(resp.BodyB64); string(raw) != `{"risk":"low"}` {
		t.Errorf("body = %s", resp.BodyB64)
	}
}

// TestHostHTTP_Defaults pins method and path defaults.
func TestHostHTTP_Defaults(t *testing.T) {
	var gotMethod, gotPath string
	client, _ := newTestHostClient(t, []string{"api"}, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
	})
	if _, err := client.call(context.Background(), hostHTTPRequest{Target: "api"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if gotMethod != "POST" || gotPath != "/link/api/" {
		t.Errorf("defaults: method=%q path=%q", gotMethod, gotPath)
	}
}

// TestHostHTTP_EmptyTargetRejected pins the request validation.
func TestHostHTTP_EmptyTargetRejected(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"api"}, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := client.call(context.Background(), hostHTTPRequest{}); err == nil {
		t.Fatal("expected empty target to be rejected")
	}
}

// TestNewHostHTTPClient_RequiresLinkAddr pins the config contract.
func TestNewHostHTTPClient_RequiresLinkAddr(t *testing.T) {
	if _, err := newHostHTTPClient(HostHTTPConfig{}); err == nil {
		t.Fatal("expected linkAddr requirement")
	}
}

// TestHostHTTP_UpstreamError pins the upstream-failure path.
func TestHostHTTP_UpstreamError(t *testing.T) {
	client, srv := newTestHostClient(t, []string{"api"}, func(w http.ResponseWriter, r *http.Request) {})
	// Point at a closed address to force a transport error.
	client.cfg.LinkAddr = "http://" + strings.TrimPrefix(srv.URL, "http://") // keep valid
	srv.Close()
	if _, err := client.call(context.Background(), hostHTTPRequest{Target: "api"}); err == nil {
		t.Fatal("expected upstream error after server shutdown")
	}
}

// TestHostHTTP_DefaultClientConstruction pins the default client branch.
func TestHostHTTP_DefaultClientConstruction(t *testing.T) {
	if _, err := newHostHTTPClient(HostHTTPConfig{LinkAddr: "http://127.0.0.1:1"}); err != nil {
		t.Fatalf("default client construction: %v", err)
	}
}

// TestHostHTTP_HeaderMultiValueKeepsFirst pins first-value header collapse.
func TestHostHTTP_HeaderMultiValueKeepsFirst(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"api"}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Multi", "first")
		w.Header().Add("X-Multi", "second")
	})
	resp, err := client.call(context.Background(), hostHTTPRequest{Target: "api"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.Headers["X-Multi"] != "first" {
		t.Errorf("multi-value header: got %q, want first", resp.Headers["X-Multi"])
	}
}

// TestHostHTTP_PathTraversalRejected pins that a guest cannot escape its
// target's path prefix: absolute paths without .. or encoded slashes only.
func TestHostHTTP_PathTraversalRejected(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	for _, path := range []string{"/../secret", "/a/../../b", "relative", "/%2f%2fescape"} {
		if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Path: path}); err == nil {
			t.Errorf("path %q must be rejected", path)
		}
	}
}

// TestHostHTTP_EmptyTargetIsInvalidRequest pins the error classification.
func TestHostHTTP_EmptyTargetIsInvalidRequest(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	_, err := client.call(context.Background(), hostHTTPRequest{})
	if err == nil || !strings.Contains(err.Error(), "invalid request") {
		t.Fatalf("expected invalid-request classification, got %v", err)
	}
}

// TestHostHTTP_InvalidBase64BodyRejected pins the request validation.
func TestHostHTTP_InvalidBase64BodyRejected(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	_, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", BodyB64: "!!!not-base64!!!"})
	if err == nil || !strings.Contains(err.Error(), "invalid request") {
		t.Fatalf("expected invalid-request classification, got %v", err)
	}
}

// TestNewHostHTTPClient_InvalidLinkAddr pins the constructor validation.
func TestNewHostHTTPClient_InvalidLinkAddr(t *testing.T) {
	if _, err := newHostHTTPClient(HostHTTPConfig{LinkAddr: "http://a b"}); err == nil {
		t.Fatal("expected invalid linkAddr to be rejected")
	}
}

// TestHostHTTP_PlaintextBodyRoundTrip pins that non-JSON bodies survive
// verbatim in both directions.
func TestHostHTTP_PlaintextBodyRoundTrip(t *testing.T) {
	var gotBody string
	client, _ := newTestHostClient(t, []string{"api"}, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte("plain text response"))
	})
	resp, err := client.call(context.Background(), hostHTTPRequest{
		Target:  "api",
		BodyB64: "aGVsbG89d29ybGQ=", // "hello=world"
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if gotBody != "hello=world" {
		t.Errorf("request body mangled: %q", gotBody)
	}
	if raw, _ := base64.StdEncoding.DecodeString(resp.BodyB64); string(raw) != "plain text response" {
		t.Errorf("response body mangled: %q", resp.BodyB64)
	}
}

// TestFactory_CreateWithHostHTTP pins the factory path used by the wasmer
// binaries: HostHTTP config produces an HTTP-enabled wazero runtime, and
// requesting it with the wasmer engine is rejected.
func TestFactory_CreateWithHostHTTP(t *testing.T) {
	dir := t.TempDir()
	modPath := compilePlainModule(t, dir)

	f := NewFactory()
	cfg := HostHTTPConfig{LinkAddr: "http://127.0.0.1:3500", AllowedTargets: []string{"api"}}
	rt, err := f.Create(context.Background(), Config{Type: RuntimeWazero, ModulePath: modPath, HostHTTP: &cfg})
	if err != nil {
		t.Fatalf("Create with HostHTTP: %v", err)
	}
	if rt == nil {
		t.Fatal("expected a runtime")
	}
	_ = rt.Close()

	wasmerCfg := Config{Type: RuntimeWasmer, ModulePath: modPath, HostHTTP: &cfg}
	if _, err := f.Create(context.Background(), wasmerCfg); err == nil {
		t.Fatal("expected host HTTP with wasmer engine to be rejected")
	}
}

// compilePlainModule builds a minimal wasip1 module for construction tests.
func compilePlainModule(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "plain")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module plain\n\ngo 1.25\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "plain.wasm")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = src
	cmd.Env = append(cmd.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile plain module: %v\n%s", err, b)
	}
	return out
}
