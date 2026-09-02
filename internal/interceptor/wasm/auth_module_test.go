package wasm

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsm/fiso/internal/interceptor"
	"github.com/lsm/fiso/internal/wasm"
)

// buildAuthFixture compiles the supported authentication guest
// (examples/interceptors/auth) to a wasip1 .wasm and returns its path.
func buildAuthFixture(t *testing.T) string {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "auth.wasm")
	cmd := exec.Command("go", "build", "-o", outPath, ".")
	cmd.Dir = filepath.Join("..", "..", "..", "examples", "interceptors", "auth")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compile auth module: %v\n%s", err, out)
	}
	return outPath
}

// newAuthInterceptor loads the auth guest with the supplied env and wraps
// it in the interceptor ABI.
func newAuthInterceptor(t *testing.T, env map[string]string) (*Interceptor, error) {
	t.Helper()
	wasmBytes, err := os.ReadFile(buildAuthFixture(t))
	if err != nil {
		t.Fatalf("read auth module: %v", err)
	}
	rt, err := wasm.NewWazeroRuntimeWithOptions(context.Background(), wasmBytes, wasm.WazeroOptions{Env: env})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return New(rt, "auth"), nil
}

// mintHS256 signs a minimal JWT with the supplied claims.
func mintHS256(t *testing.T, secret string, claims map[string]interface{}) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func authedRequest(token string, payload []byte) *interceptor.Request {
	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return &interceptor.Request{Payload: payload, Headers: headers, Direction: interceptor.Inbound}
}

// TestAuthModule_AuthenticatesThroughGuest pins the full contract through
// the real wasm boundary: key material arrives via env (ADR 0008), the
// verdict arrives via the rejection ABI (ADR 0007), the credential header
// is stripped, and the payload survives byte-identically.
func TestAuthModule_AuthenticatesThroughGuest(t *testing.T) {
	ic, err := newAuthInterceptor(t, map[string]string{"AUTH_HS256_SECRET": "guest-secret"})
	if err != nil {
		t.Fatal(err)
	}

	valid := mintHS256(t, "guest-secret", map[string]interface{}{
		"sub": "alice",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	t.Run("valid token passes with rewritten headers", func(t *testing.T) {
		result, err := ic.Process(context.Background(), authedRequest(valid, []byte(`{"order":"123"}`)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(result.Payload) != `{"order":"123"}` {
			t.Fatalf("payload = %s, want byte-identical", result.Payload)
		}
		if _, has := result.Headers["Authorization"]; has {
			t.Fatal("Authorization must be stripped")
		}
		if result.Headers["X-Authenticated"] != "true" || result.Headers["X-Auth-Subject"] != "alice" {
			t.Fatalf("verdict headers missing: %+v", result.Headers)
		}
	})

	t.Run("missing credentials reject 401", func(t *testing.T) {
		_, err := ic.Process(context.Background(), authedRequest("", []byte(`{}`)))
		rej, ok := interceptor.AsRejection(err)
		if !ok {
			t.Fatalf("expected rejection, got %v", err)
		}
		if rej.Status != 401 || rej.Reason != "missing credentials" {
			t.Fatalf("rejection = %+v", rej)
		}
	})

	t.Run("wrong signature rejects 401", func(t *testing.T) {
		forged := mintHS256(t, "attacker-secret", map[string]interface{}{
			"sub": "mallory",
			"exp": float64(time.Now().Add(time.Hour).Unix()),
		})
		_, err := ic.Process(context.Background(), authedRequest(forged, []byte(`{}`)))
		rej, ok := interceptor.AsRejection(err)
		if !ok {
			t.Fatalf("expected rejection, got %v", err)
		}
		if rej.Status != 401 || rej.Reason != "invalid signature" {
			t.Fatalf("rejection = %+v", rej)
		}
	})

	t.Run("expired token rejects 401", func(t *testing.T) {
		expired := mintHS256(t, "guest-secret", map[string]interface{}{
			"sub": "alice",
			"exp": float64(time.Now().Add(-time.Minute).Unix()),
		})
		_, err := ic.Process(context.Background(), authedRequest(expired, []byte(`{}`)))
		rej, ok := interceptor.AsRejection(err)
		if !ok {
			t.Fatalf("expected rejection, got %v", err)
		}
		if rej.Status != 401 || rej.Reason != "token expired" {
			t.Fatalf("rejection = %+v", rej)
		}
	})

	t.Run("binary payload survives byte-identically", func(t *testing.T) {
		raw := []byte{0x89, 0x50, 0x4e, 0xff, 0x00, 0xfe} // invalid UTF-8
		result, err := ic.Process(context.Background(), authedRequest(valid, raw))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(result.Payload) != string(raw) {
			t.Fatalf("payload = %x, want %x", result.Payload, raw)
		}
	})
}

// TestAuthModule_MissingKeyIsInterceptorError pins the failure-mode
// split: absent key material is an interceptor error (failOpen applies,
// status 500 path), never a 401 masquerading as a verdict.
func TestAuthModule_MissingKeyIsInterceptorError(t *testing.T) {
	ic, err := newAuthInterceptor(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	token := mintHS256(t, "any", map[string]interface{}{
		"sub": "alice",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	_, err = ic.Process(context.Background(), authedRequest(token, []byte(`{}`)))
	if err == nil {
		t.Fatal("expected configuration error")
	}
	if _, isRej := interceptor.AsRejection(err); isRej {
		t.Fatalf("misconfiguration must not surface as a rejection: %v", err)
	}
	if !strings.Contains(err.Error(), "wasm module auth") {
		t.Fatalf("error should name the module: %v", err)
	}
}
