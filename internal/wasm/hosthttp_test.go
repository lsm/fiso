package wasm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	if gotMethod != "post" {
		t.Errorf("method = %q, want the guest's casing preserved", gotMethod)
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

// TestHostHTTP_EncodedTraversalRejected pins that percent-encoded dot
// segments are caught after decoding, matching Go's client behavior.
func TestHostHTTP_EncodedTraversalRejected(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	for _, path := range []string{"/api/%2e%2e/admin", "/%2e%2e", "/a/./b", "/a/%252e%252e/b"} {
		if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Path: path}); err == nil {
			t.Errorf("path %q must be rejected", path)
		}
	}
}

// TestHostHTTP_InvalidMethodRejected pins the method token validation.
func TestHostHTTP_InvalidMethodRejected(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	for _, method := range []string{"GET /x", "PUT\t", "\x00POST"} {
		if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Method: method}); err == nil {
			t.Errorf("method %q must be rejected", method)
		}
	}
}

// TestHostHTTP_StrictMethodTokens pins the RFC 7230 tchar rule: separator
// characters are valid tchars, other non-tchars are not.
func TestHostHTTP_StrictMethodTokens(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	// Valid methods.
	for _, m := range []string{"GET", "PATCH", "CUSTOM-1"} {
		if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Method: m}); err != nil {
			t.Errorf("method %q should be accepted: %v", m, err)
		}
	}
	// Invalid: colon, space, question mark, control.
	for _, m := range []string{"GE:T", "GET X", "GET?x", "A(B", "A;B"} {
		if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Method: m}); err == nil {
			t.Errorf("method %q must be rejected", m)
		}
	}
}

// TestHostHTTP_InvalidHeadersRejected pins the header field validation.
func TestHostHTTP_InvalidHeadersRejected(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Headers: map[string]string{"Bad Name": "v"}}); err == nil {
		t.Error("header name with space must be rejected")
	}
	if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Headers: map[string]string{"X-A": "bad\x01value"}}); err == nil {
		t.Error("header value with control character must be rejected")
	}
}

// TestHostHTTP_EmptyPathSegmentsRejected pins the canonical-redirect rule.
func TestHostHTTP_EmptyPathSegmentsRejected(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	for _, path := range []string{"/api//score", "/api/", "//x"} {
		if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Path: path}); err == nil {
			t.Errorf("path %q must be rejected", path)
		}
	}
	if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Path: "/"}); err != nil {
		t.Errorf("the structural root path must be accepted: %v", err)
	}
}

// TestHostHTTP_ControlCharactersRejected pins the printable-ASCII rule.
func TestHostHTTP_ControlCharactersRejected(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Path: "/api\nadmin"}); err == nil {
		t.Error("control character in path must be rejected")
	}
}

// TestHostHTTP_AnyPercentEncodingRejected pins that encoded delimiters
// (%23, %3F) are rejected before Link can reinterpret them decoded.
func TestHostHTTP_AnyPercentEncodingRejected(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	for _, path := range []string{"/a%23b", "/a%3Fb", "/a%2fb", "/api/%2e%2e/admin"} {
		if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Path: path}); err == nil {
			t.Errorf("path %q must be rejected", path)
		}
	}
}

// TestHostHTTPExport_CoversMemoryFailurePaths drives the exported function
// against a minimal wazero module so the memory-read, write, and marshal
// paths execute.
func TestHostHTTPExport_CoversMemoryFailurePaths(t *testing.T) {
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer link.Close()

	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), buildHTTPCallModule(t), HostHTTPConfig{
		LinkAddr: link.URL, AllowedTargets: []string{"enrich-api"}, Client: link.Client(),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	// A zero-length request read at offset 0: valid memory range, invalid
	// JSON → -1 from the export itself.
	input, _ := json.Marshal(guestEnvelope{Payload: json.RawMessage(`{}`), Headers: map[string]string{}, Direction: "inbound"})
	if _, err := rt.CallWithEnv(context.Background(), input, map[string]string{"FISO_TEST_MODE": "badreq"}); err != nil {
		t.Fatalf("guest: %v", err)
	}
}

// TestNewWazeroRuntimeWithHTTP_Errors pins construction failure paths.
func TestNewWazeroRuntimeWithHTTP_Errors(t *testing.T) {
	if _, err := NewWazeroRuntimeWithHTTP(context.Background(), []byte("bad"), HostHTTPConfig{LinkAddr: "http://x"}); err == nil {
		t.Fatal("expected compile failure for invalid wasm bytes")
	}
	if _, err := NewWazeroRuntimeWithHTTP(context.Background(), []byte("bad"), HostHTTPConfig{}); err == nil {
		t.Fatal("expected missing linkAddr failure before compilation")
	}
}

// TestValidHeaderPair_Boundaries pins the validator directly.
func TestValidHeaderPair_Boundaries(t *testing.T) {
	if !validHeaderPair("X-Trace-Id", "abc-123") {
		t.Error("valid pair rejected")
	}
	if validHeaderPair("", "v") {
		t.Error("empty name accepted")
	}
	if validHeaderPair("X:A", "v") {
		t.Error("separator in name accepted")
	}
	if validHeaderPair("X", "v\x7f") {
		t.Error("DEL in value accepted")
	}
}

// TestValidMethodToken_Boundaries pins the validator directly.
func TestValidMethodToken_Boundaries(t *testing.T) {
	for _, m := range []string{"GET", "A", "custom-method_1"} {
		if !validMethodToken(m) {
			t.Errorf("%q rejected", m)
		}
	}
	for _, m := range []string{"", "A B", "A(B", "\x01"} {
		if validMethodToken(m) {
			t.Errorf("%q accepted", m)
		}
	}
}

// TestHostHTTP_TargetNameWithAllowlistWord pins the typed-error
// classification: a *target named* "allowlist-service" that is denied must
// still classify correctly (no string-matching on error text).
func TestHostHTTP_TargetNameWithAllowlistWord(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"other"}, func(w http.ResponseWriter, r *http.Request) {})
	_, err := client.call(context.Background(), hostHTTPRequest{Target: "allowlist-service"})
	if err == nil {
		t.Fatal("expected denial")
	}
	var denied *targetDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected a targetDeniedError, got %T: %v", err, err)
	}
	if denied.target != "allowlist-service" {
		t.Fatalf("denied target = %q", denied.target)
	}
}

// TestHostHTTP_RawDelimitersRejected pins that ? and # are rejected.
func TestHostHTTP_RawDelimitersRejected(t *testing.T) {
	client, _ := newTestHostClient(t, []string{"safe"}, func(w http.ResponseWriter, r *http.Request) {})
	for _, path := range []string{"/a?b", "/a#b"} {
		if _, err := client.call(context.Background(), hostHTTPRequest{Target: "safe", Path: path}); err == nil {
			t.Errorf("path %q must be rejected", path)
		}
	}
}

// TestHostHTTP_LowercaseMethodPreserved pins method-casing preservation.
func TestHostHTTP_LowercaseMethodPreserved(t *testing.T) {
	var got string
	client, _ := newTestHostClient(t, []string{"api"}, func(w http.ResponseWriter, r *http.Request) {
		got = r.Method
	})
	if _, err := client.call(context.Background(), hostHTTPRequest{Target: "api", Method: "custom"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if got != "custom" {
		t.Errorf("method casing not preserved: %q", got)
	}
}
