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
)

// buildHTTPCallModule compiles the guest fixture that imports
// fiso.http_call.
func buildHTTPCallModule(t *testing.T) []byte {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "httpcall.wasm")
	cmd := exec.Command("go", "build", "-o", outPath, ".")
	cmd.Dir = "../interceptor/wasm/testdata/httpcall"
	cmd.Env = append(cmd.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile httpcall fixture: %v\n%s", err, out)
	}
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

// guestEnvelope is the interceptor ABI the fixture speaks.
type guestEnvelope struct {
	Payload   json.RawMessage   `json:"payload"`
	Headers   map[string]string `json:"headers"`
	Direction string            `json:"direction"`
}

// runGuest executes the fixture with the envelope on stdin and parses the
// envelope from stdout, optionally setting guest env vars.
func runGuest(t *testing.T, rt *WazeroRuntime, env map[string]string, payload string) guestEnvelope {
	t.Helper()
	input, _ := json.Marshal(guestEnvelope{
		Payload: json.RawMessage(payload), Headers: map[string]string{}, Direction: "inbound",
	})
	out, err := rt.CallWithEnv(context.Background(), input, env)
	if err != nil {
		t.Fatalf("guest execution: %v", err)
	}
	var env2 guestEnvelope
	if err := json.Unmarshal(out, &env2); err != nil {
		t.Fatalf("guest output: %v\n%s", err, out)
	}
	return env2
}

// TestHTTPCallGuest_RoundTrip pins the full path: a real wasip1 guest
// imports fiso.http_call, the host routes the allowed target through Link,
// and the enriched payload flows back through the interceptor envelope.
func TestHTTPCallGuest_RoundTrip(t *testing.T) {
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

	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), buildHTTPCallModule(t), HostHTTPConfig{
		LinkAddr: link.URL, AllowedTargets: []string{"enrich-api"}, Client: link.Client(),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	out := runGuest(t, rt, nil, `{"customer_id":"c-9"}`)

	if gotPath != "/link/enrich-api/lookup" {
		t.Errorf("guest call not routed through Link: %q", gotPath)
	}
	if gotMethod != "POST" || gotHeader != "wasm" {
		t.Errorf("method/header not propagated: %q %q", gotMethod, gotHeader)
	}
	if string(gotBody) != `{"customer_id":"c-9"}` {
		t.Errorf("payload not passed to the target: %s", gotBody)
	}
	if out.Headers["X-Api-Status"] != "200" {
		t.Errorf("guest did not surface the API status: %v", out.Headers)
	}
	if string(out.Payload) != `{"risk":"low","id":"x-1"}` {
		t.Errorf("enriched payload not returned: %s", out.Payload)
	}
}

// TestHTTPCallGuest_Denied pins the denial path: no network activity.
func TestHTTPCallGuest_Denied(t *testing.T) {
	requests := 0
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer link.Close()

	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), buildHTTPCallModule(t), HostHTTPConfig{
		LinkAddr: link.URL, AllowedTargets: []string{"some-other-api"}, Client: link.Client(),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	out := runGuest(t, rt, nil, `{}`)
	if got := out.Headers["X-Host-Error"]; got != "-2" {
		t.Fatalf("expected denial code -2, got %q (headers %v)", got, out.Headers)
	}
	if requests != 0 {
		t.Fatalf("denied guest call reached the network: %d requests", requests)
	}
}

// TestHTTPCallGuest_InvalidRequestJSON pins the -1 path for malformed JSON.
func TestHTTPCallGuest_InvalidRequestJSON(t *testing.T) {
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer link.Close()
	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), buildHTTPCallModule(t), HostHTTPConfig{
		LinkAddr: link.URL, AllowedTargets: []string{"enrich-api"}, Client: link.Client(),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	out := runGuest(t, rt, map[string]string{"FISO_TEST_MODE": "badreq"}, `{}`)
	if got := out.Headers["X-Host-Error"]; got != "-1" {
		t.Fatalf("expected invalid-request -1, got %q", got)
	}
}

// TestHTTPCallGuest_TraversalDenied pins the -1 path for a path that
// escapes the target prefix, and that it never reaches the network.
func TestHTTPCallGuest_TraversalDenied(t *testing.T) {
	requests := 0
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer link.Close()
	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), buildHTTPCallModule(t), HostHTTPConfig{
		LinkAddr: link.URL, AllowedTargets: []string{"enrich-api"}, Client: link.Client(),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	out := runGuest(t, rt, map[string]string{"FISO_TEST_MODE": "traversal"}, `{}`)
	if got := out.Headers["X-Host-Error"]; got != "-1" {
		t.Fatalf("expected -1 for traversal, got %q", got)
	}
	if requests != 0 {
		t.Fatalf("traversal attempt reached the network: %d requests", requests)
	}
}

// TestHTTPCallGuest_EmptyTarget pins the -1 classification for a missing
// target.
func TestHTTPCallGuest_EmptyTarget(t *testing.T) {
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer link.Close()
	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), buildHTTPCallModule(t), HostHTTPConfig{
		LinkAddr: link.URL, AllowedTargets: []string{"enrich-api"}, Client: link.Client(),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	out := runGuest(t, rt, map[string]string{"FISO_TEST_MODE": "emptytarget"}, `{}`)
	if got := out.Headers["X-Host-Error"]; got != "-1" {
		t.Fatalf("expected -1 for empty target, got %q", got)
	}
}

// TestHTTPCallGuest_SmallBuffer pins the -3 path.
func TestHTTPCallGuest_SmallBuffer(t *testing.T) {
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"risk":"low"}`))
	}))
	defer link.Close()
	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), buildHTTPCallModule(t), HostHTTPConfig{
		LinkAddr: link.URL, AllowedTargets: []string{"enrich-api"}, Client: link.Client(),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	out := runGuest(t, rt, map[string]string{"FISO_TEST_MODE": "smallbuf"}, `{}`)
	if got := out.Headers["X-Host-Error"]; got != "-3" {
		t.Fatalf("expected buffer-size -3, got %q", got)
	}
}

// TestWazeroRuntime_NoHostModuleWithoutOptIn pins that the fiso import does
// not exist unless opted in: an importing module fails to instantiate.
func TestWazeroRuntime_NoHostModuleWithoutOptIn(t *testing.T) {
	rt, err := NewWazeroRuntime(context.Background(), buildHTTPCallModule(t))
	if err != nil {
		t.Fatalf("NewWazeroRuntime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	if _, err := rt.Call(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected instantiation failure for an importing module without opt-in")
	}
}

// TestHTTPCallGuest_PlaintextBody pins that non-JSON bodies round-trip
// verbatim through the base64 ABI.
func TestHTTPCallGuest_PlaintextBody(t *testing.T) {
	var gotBody string
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte("plain response"))
	}))
	defer link.Close()
	rt, err := NewWazeroRuntimeWithHTTP(context.Background(), buildHTTPCallModule(t), HostHTTPConfig{
		LinkAddr: link.URL, AllowedTargets: []string{"enrich-api"}, Client: link.Client(),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	// The guest's envelope payload must be JSON, so only the request side
	// is asserted here; response-side verbatim round-trip is pinned at the
	// host level in TestHostHTTP_PlaintextBodyRoundTrip.
	runGuestLenient(t, rt, map[string]string{"FISO_TEST_MODE": "plaintext"}, `{}`)
	if gotBody != "hello=world" {
		t.Errorf("request body mangled: %q (want the base64-decoded verbatim bytes)", gotBody)
	}
}

// runGuestLenient runs the guest ignoring empty output (the fixture cannot
// envelope a non-JSON payload).
func runGuestLenient(t *testing.T, rt *WazeroRuntime, env map[string]string, payload string) {
	t.Helper()
	input, _ := json.Marshal(guestEnvelope{
		Payload: json.RawMessage(payload), Headers: map[string]string{}, Direction: "inbound",
	})
	_, _ = rt.CallWithEnv(context.Background(), input, env)
}
