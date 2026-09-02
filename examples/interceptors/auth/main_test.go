package main

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Unix(1_700_000_000, 0)

// makeJWT assembles a JWT from a header and claims, signing signingInput
// with the supplied signer.
func makeJWT(t *testing.T, header map[string]any, claims map[string]any, sign func([]byte) []byte) string {
	t.Helper()
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(claimsJSON)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sign([]byte(signingInput)))
}

// signedTokenWithRawClaims builds an HS256 token whose claims segment is
// raw bytes (invalid JSON), correctly signed — reaching the claims parse
// step past signature verification.
func signedTokenWithRawClaims(t *testing.T, raw string) string {
	t.Helper()
	headerJSON, _ := json.Marshal(map[string]any{"alg": "HS256"})
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(raw))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(hmacSHA256Signer("secret")([]byte(signingInput)))
}

func hmacSHA256Signer(secret string) func([]byte) []byte {
	return func(input []byte) []byte {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(input)
		return mac.Sum(nil)
	}
}

func hs256Token(t *testing.T, claims map[string]any, secret string) string {
	t.Helper()
	return makeJWT(t, map[string]any{"alg": "HS256", "typ": "JWT"}, claims, hmacSHA256Signer(secret))
}

var (
	rsaKey    *rsa.PrivateKey
	edPriv    ed25519.PrivateKey
	edPubB64  string
	rsaPubPEM string
)

func init() {
	rsaKey, _ = rsa.GenerateKey(rand.Reader, 2048)
	var edPub ed25519.PublicKey
	edPub, edPriv, _ = ed25519.GenerateKey(rand.Reader)
	edPubB64 = base64.StdEncoding.EncodeToString(edPub)
	rsaDer, _ := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	rsaPubPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: rsaDer}))
}

func rs256Token(t *testing.T, claims map[string]any) string {
	t.Helper()
	return makeJWT(t, map[string]any{"alg": "RS256", "typ": "JWT"}, claims, func(input []byte) []byte {
		digest := sha256.Sum256(input)
		sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatalf("sign rs256: %v", err)
		}
		return sig
	})
}

func eddsaToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	return makeJWT(t, map[string]any{"alg": "EdDSA", "typ": "JWT"}, claims, func(input []byte) []byte {
		return ed25519.Sign(edPriv, input)
	})
}

// validClaims is the fixed claim set used by happy-path tests; exp is
// always far in the future relative to testNow.
func validClaims() map[string]any {
	return map[string]any{"sub": "alice", "exp": float64(testNow.Add(time.Hour).Unix())}
}

// audClaims is validClaims addressed to a specific audience.
func audClaims(audience string) map[string]any {
	claims := validClaims()
	claims["aud"] = audience
	return claims
}

func hs256Config() *authConfig {
	return &authConfig{hs256Secret: "secret"}
}

func bearerHeader(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestAuthenticate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *authConfig
		headers map[string]string
		wantOK  bool
		wantSub string
		wantRej string
	}{
		{
			name:    "missing authorization header",
			cfg:     hs256Config(),
			headers: map[string]string{},
			wantRej: "missing credentials",
		},
		{
			name:    "non-bearer authorization",
			cfg:     hs256Config(),
			headers: map[string]string{"Authorization": "Basic dXNlcjpwYXNz"},
			wantRej: "missing credentials",
		},
		{
			name:    "empty bearer token",
			cfg:     hs256Config(),
			headers: map[string]string{"Authorization": "Bearer "},
			wantRej: "missing credentials",
		},
		{
			name:    "lowercase bearer prefix accepted",
			cfg:     hs256Config(),
			headers: map[string]string{"authorization": "bearer " + hs256Token(t, validClaims(), "secret")},
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name:    "uppercase bearer prefix accepted",
			cfg:     hs256Config(),
			headers: map[string]string{"Authorization": "BEARER " + hs256Token(t, validClaims(), "secret")},
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name:    "lowercase header name accepted",
			cfg:     hs256Config(),
			headers: map[string]string{"authorization": "Bearer " + hs256Token(t, validClaims(), "secret")},
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name:    "token is not three parts",
			cfg:     hs256Config(),
			headers: map[string]string{"Authorization": "Bearer abcdef"},
			wantRej: "malformed token",
		},
		{
			name:    "header segment not base64url",
			cfg:     hs256Config(),
			headers: map[string]string{"Authorization": "Bearer ????.????.????"},
			wantRej: "malformed token",
		},
		{
			name:    "signature segment not base64url",
			cfg:     hs256Config(),
			headers: map[string]string{"Authorization": "Bearer " + strings.TrimSuffix(hs256Token(t, validClaims(), "secret"), "AAAA") + "****"},
			wantRej: "malformed token",
		},
		{
			name:    "claims not json",
			cfg:     hs256Config(),
			headers: bearerHeader(signedTokenWithRawClaims(t, "!!!")),
			wantRej: "malformed claims",
		},
		{
			name:    "alg none refused",
			cfg:     hs256Config(),
			headers: bearerHeader(makeJWT(t, map[string]any{"alg": "none"}, validClaims(), func([]byte) []byte { return nil })),
			wantRej: "algorithm not allowed",
		},
		{
			name:    "unknown algorithm refused",
			cfg:     hs256Config(),
			headers: bearerHeader(makeJWT(t, map[string]any{"alg": "HS512"}, validClaims(), hmacSHA256Signer("secret"))),
			wantRej: "algorithm not allowed",
		},
		{
			name:    "hs256 without configured secret",
			cfg:     &authConfig{},
			headers: bearerHeader(hs256Token(t, validClaims(), "secret")),
			wantRej: "algorithm not allowed",
		},
		{
			name:    "hs256 wrong signature",
			cfg:     hs256Config(),
			headers: bearerHeader(hs256Token(t, validClaims(), "other-secret")),
			wantRej: "invalid signature",
		},
		{
			name:    "hs256 valid",
			cfg:     hs256Config(),
			headers: bearerHeader(hs256Token(t, validClaims(), "secret")),
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name:    "expired",
			cfg:     hs256Config(),
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": "alice", "exp": float64(testNow.Add(-time.Minute).Unix())}, "secret")),
			wantRej: "token expired",
		},
		{
			name:    "exp exactly now still valid",
			cfg:     hs256Config(),
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": "alice", "exp": float64(testNow.Unix())}, "secret")),
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name:    "missing exp refused by default",
			cfg:     hs256Config(),
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": "alice"}, "secret")),
			wantRej: "token has no expiry",
		},
		{
			name:    "missing exp allowed when configured",
			cfg:     &authConfig{hs256Secret: "secret", allowMissingExpiry: true},
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": "alice"}, "secret")),
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name:    "nbf in the future",
			cfg:     hs256Config(),
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": "alice", "exp": float64(testNow.Add(time.Hour).Unix()), "nbf": float64(testNow.Add(time.Minute).Unix())}, "secret")),
			wantRej: "token not yet valid",
		},
		{
			name:    "nbf in the past",
			cfg:     hs256Config(),
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": "alice", "exp": float64(testNow.Add(time.Hour).Unix()), "nbf": float64(testNow.Add(-time.Minute).Unix())}, "secret")),
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name:    "rs256 without configured key",
			cfg:     hs256Config(),
			headers: bearerHeader(rs256Token(t, validClaims())),
			wantRej: "algorithm not allowed",
		},
		{
			name:    "rs256 valid",
			cfg:     &authConfig{rs256PublicKey: &rsaKey.PublicKey},
			headers: bearerHeader(rs256Token(t, validClaims())),
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name: "rs256 signed by another key",
			cfg: func() *authConfig {
				other, _ := rsa.GenerateKey(rand.Reader, 2048)
				return &authConfig{rs256PublicKey: &other.PublicKey}
			}(),
			headers: bearerHeader(rs256Token(t, validClaims())),
			wantRej: "invalid signature",
		},
		{
			name:    "eddsa without configured key",
			cfg:     hs256Config(),
			headers: bearerHeader(eddsaToken(t, validClaims())),
			wantRej: "algorithm not allowed",
		},
		{
			name:    "eddsa valid",
			cfg:     &authConfig{ed25519PublicKey: edPriv.Public().(ed25519.PublicKey)},
			headers: bearerHeader(eddsaToken(t, validClaims())),
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name: "eddsa signed by another key",
			cfg: func() *authConfig {
				otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
				return &authConfig{ed25519PublicKey: otherPub}
			}(),
			headers: bearerHeader(eddsaToken(t, validClaims())),
			wantRej: "invalid signature",
		},
		{
			name:    "non-string sub yields empty subject",
			cfg:     hs256Config(),
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": 42, "exp": float64(testNow.Add(time.Hour).Unix())}, "secret")),
			wantOK:  true,
		},
		{
			name:    "nbf present but not numeric",
			cfg:     hs256Config(),
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": "alice", "exp": float64(testNow.Add(time.Hour).Unix()), "nbf": "1700000060"}, "secret")),
			wantRej: "malformed claims",
		},
		{
			name:    "exp present but not numeric is not a missing expiry",
			cfg:     &authConfig{hs256Secret: "secret", allowMissingExpiry: true},
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": "alice", "exp": "never"}, "secret")),
			wantRej: "malformed claims",
		},
		{
			name:    "audience matches expectation",
			cfg:     &authConfig{hs256Secret: "secret", expectedAudience: "orders-api"},
			headers: bearerHeader(hs256Token(t, audClaims("orders-api"), "secret")),
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name:    "audience array contains expectation",
			cfg:     &authConfig{hs256Secret: "secret", expectedAudience: "orders-api"},
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": "alice", "exp": float64(testNow.Add(time.Hour).Unix()), "aud": []any{"billing-api", "orders-api"}}, "secret")),
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name:    "audience mismatch",
			cfg:     &authConfig{hs256Secret: "secret", expectedAudience: "orders-api"},
			headers: bearerHeader(hs256Token(t, audClaims("billing-api"), "secret")),
			wantRej: "invalid audience",
		},
		{
			name:    "token without audience when one is expected",
			cfg:     &authConfig{hs256Secret: "secret", expectedAudience: "orders-api"},
			headers: bearerHeader(hs256Token(t, validClaims(), "secret")),
			wantRej: "invalid audience",
		},
		{
			name:    "no expected audience means aud is not enforced",
			cfg:     hs256Config(),
			headers: bearerHeader(hs256Token(t, audClaims("anything"), "secret")),
			wantOK:  true,
			wantSub: "alice",
		},
		{
			name:    "audience claim of the wrong type",
			cfg:     &authConfig{hs256Secret: "secret", expectedAudience: "orders-api"},
			headers: bearerHeader(hs256Token(t, map[string]any{"sub": "alice", "exp": float64(testNow.Add(time.Hour).Unix()), "aud": 42}, "secret")),
			wantRej: "invalid audience",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := authenticate(tt.cfg, tt.headers, testNow)
			if tt.wantOK {
				if !dec.ok {
					t.Fatalf("expected pass, got refusal %d %q", dec.status, dec.reason)
				}
				if dec.subject != tt.wantSub {
					t.Fatalf("subject = %q, want %q", dec.subject, tt.wantSub)
				}
				return
			}
			if dec.ok {
				t.Fatalf("expected refusal %q, got pass (subject %q)", tt.wantRej, dec.subject)
			}
			if dec.status != 401 {
				t.Fatalf("status = %d, want 401", dec.status)
			}
			if dec.reason != tt.wantRej {
				t.Fatalf("reason = %q, want %q", dec.reason, tt.wantRej)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("no key configured", func(t *testing.T) {
		_, err := loadConfig(func(string) string { return "" })
		if err == nil || !strings.Contains(err.Error(), "no verification key") {
			t.Fatalf("expected no-key error, got %v", err)
		}
	})
	t.Run("hs256 secret", func(t *testing.T) {
		cfg, err := loadConfig(func(k string) string {
			if k == "AUTH_HS256_SECRET" {
				return "s3cret"
			}
			return ""
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.hs256Secret != "s3cret" {
			t.Fatalf("secret = %q", cfg.hs256Secret)
		}
	})
	t.Run("rs256 pem", func(t *testing.T) {
		cfg, err := loadConfig(func(k string) string {
			if k == "AUTH_RS256_PUBLIC_KEY" {
				return rsaPubPEM
			}
			return ""
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.rs256PublicKey == nil || cfg.rs256PublicKey.N.Cmp(rsaKey.N) != 0 {
			t.Fatal("public key not parsed")
		}
	})
	t.Run("rs256 not pem", func(t *testing.T) {
		_, err := loadConfig(func(k string) string {
			if k == "AUTH_RS256_PUBLIC_KEY" {
				return "not a pem"
			}
			return ""
		})
		if err == nil || !strings.Contains(err.Error(), "AUTH_RS256_PUBLIC_KEY") {
			t.Fatalf("expected PEM error, got %v", err)
		}
	})
	t.Run("rs256 not an rsa key", func(t *testing.T) {
		edDer, _ := x509.MarshalPKIXPublicKey(edPriv.Public())
		edPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: edDer}))
		_, err := loadConfig(func(k string) string {
			if k == "AUTH_RS256_PUBLIC_KEY" {
				return edPEM
			}
			return ""
		})
		if err == nil || !strings.Contains(err.Error(), "not an RSA key") {
			t.Fatalf("expected non-RSA error, got %v", err)
		}
	})
	t.Run("ed25519 key", func(t *testing.T) {
		cfg, err := loadConfig(func(k string) string {
			if k == "AUTH_ED25519_PUBLIC_KEY" {
				return edPubB64
			}
			return ""
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.ed25519PublicKey == nil {
			t.Fatal("public key not parsed")
		}
	})
	t.Run("ed25519 wrong length", func(t *testing.T) {
		_, err := loadConfig(func(k string) string {
			if k == "AUTH_ED25519_PUBLIC_KEY" {
				return base64.StdEncoding.EncodeToString([]byte("short"))
			}
			return ""
		})
		if err == nil || !strings.Contains(err.Error(), "want 32") {
			t.Fatalf("expected length error, got %v", err)
		}
	})
	t.Run("ed25519 not base64", func(t *testing.T) {
		_, err := loadConfig(func(k string) string {
			if k == "AUTH_ED25519_PUBLIC_KEY" {
				return "!!!not-base64!!!"
			}
			return ""
		})
		if err == nil {
			t.Fatal("expected base64 error")
		}
	})
	t.Run("allow missing expiry only exact true", func(t *testing.T) {
		for value, want := range map[string]bool{"true": true, "TRUE": false, "1": false, "": false} {
			cfg, err := loadConfig(func(k string) string {
				switch k {
				case "AUTH_HS256_SECRET":
					return "s"
				case "AUTH_ALLOW_MISSING_EXPIRY":
					return value
				}
				return ""
			})
			if err != nil {
				t.Fatalf("value %q: %v", value, err)
			}
			if cfg.allowMissingExpiry != want {
				t.Fatalf("value %q: allowMissingExpiry = %v", value, cfg.allowMissingExpiry)
			}
		}
	})
	t.Run("expected audience", func(t *testing.T) {
		cfg, err := loadConfig(func(k string) string {
			switch k {
			case "AUTH_HS256_SECRET":
				return "s"
			case "AUTH_EXPECTED_AUDIENCE":
				return "orders-api"
			}
			return ""
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.expectedAudience != "orders-api" {
			t.Fatalf("expectedAudience = %q", cfg.expectedAudience)
		}
	})
}

func TestRun(t *testing.T) {
	env := func(k string) string {
		if k == "AUTH_HS256_SECRET" {
			return "secret"
		}
		return ""
	}
	// run() reads the real clock, unlike authenticate's injected now.
	runClaims := func() map[string]any {
		return map[string]any{"sub": "alice", "exp": float64(time.Now().Add(time.Hour).Unix())}
	}
	input := func(headers map[string]string, payload string) string {
		in := envelope{Payload: json.RawMessage(payload), Headers: headers}
		b, _ := json.Marshal(in)
		return string(b)
	}

	t.Run("valid token passes with rewritten headers", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"auth"}, strings.NewReader(input(
			map[string]string{"Authorization": "Bearer " + hs256Token(t, runClaims(), "secret"), "X-Keep": "yes"},
			`{"order_id":123}`)), env, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit = %d, stderr %s", code, stderr.String())
		}
		var out output
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("parse output: %v (%s)", err, stdout.String())
		}
		if out.Reject != nil {
			t.Fatalf("unexpected rejection: %+v", out.Reject)
		}
		if string(out.Payload) != `{"order_id":123}` {
			t.Fatalf("payload = %s, want byte-identical echo", out.Payload)
		}
		if _, has := out.Headers["Authorization"]; has {
			t.Fatal("Authorization must be stripped")
		}
		if out.Headers["X-Authenticated"] != "true" || out.Headers["X-Auth-Subject"] != "alice" {
			t.Fatalf("verdict headers missing: %+v", out.Headers)
		}
		if out.Headers["X-Keep"] != "yes" {
			t.Fatalf("unrelated header lost: %+v", out.Headers)
		}
	})

	t.Run("binary payload echoes byte-identically", func(t *testing.T) {
		wrapped := `{"fisoB64":"` + base64.StdEncoding.EncodeToString([]byte{0xff, 0x00, 0xfe}) + `"}`
		var stdout, stderr bytes.Buffer
		code := run([]string{"auth"}, strings.NewReader(input(bearerHeader(hs256Token(t, runClaims(), "secret")), wrapped)), env, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit = %d, stderr %s", code, stderr.String())
		}
		var out output
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("parse output: %v", err)
		}
		if string(out.Payload) != wrapped {
			t.Fatalf("payload = %s, want %s", out.Payload, wrapped)
		}
	})

	t.Run("caller-supplied verdict headers are stripped", func(t *testing.T) {
		// A valid token without a sub claim: the caller's forged verdict
		// headers must not survive to the sink alongside X-Authenticated.
		noSub := map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}
		var stdout, stderr bytes.Buffer
		code := run([]string{"auth"}, strings.NewReader(input(map[string]string{
			"Authorization":   "Bearer " + hs256Token(t, noSub, "secret"),
			"X-Auth-Subject":  "admin",
			"x-auth-subject":  "admin-lower",
			"X-Authenticated": "spoofed",
		}, `{"a":1}`)), env, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit = %d, stderr %s", code, stderr.String())
		}
		var out output
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("parse output: %v", err)
		}
		for name := range out.Headers {
			if strings.EqualFold(name, "X-Auth-Subject") {
				t.Fatalf("caller-supplied %s must be stripped; headers: %+v", name, out.Headers)
			}
		}
		if out.Headers["X-Authenticated"] != "true" {
			t.Fatalf("X-Authenticated = %q, want the module's own verdict", out.Headers["X-Authenticated"])
		}
	})

	t.Run("unauthenticated request is rejected not errored", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"auth"}, strings.NewReader(input(map[string]string{}, `{"a":1}`)), env, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("rejection must exit 0, got %d (%s)", code, stderr.String())
		}
		var out output
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("parse output: %v", err)
		}
		if out.Reject == nil || out.Reject.Status != 401 || out.Reject.Reason != "missing credentials" {
			t.Fatalf("reject = %+v", out.Reject)
		}
	})

	t.Run("missing key material is an error not a rejection", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"auth"}, strings.NewReader(input(bearerHeader(hs256Token(t, runClaims(), "secret")), `null`)), func(string) string { return "" }, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("misconfiguration must exit 1, got %d", code)
		}
		if stdout.Len() != 0 {
			t.Fatalf("no output expected on error, got %s", stdout.String())
		}
	})

	t.Run("unparseable input is an error", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"auth"}, strings.NewReader("not json"), env, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("want exit 1, got %d", code)
		}
	})

	t.Run("stdin file arg", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "input.json")
		if err := os.WriteFile(path, []byte(input(bearerHeader(hs256Token(t, runClaims(), "secret")), `null`)), 0644); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		code := run([]string{"auth", "--stdin-file", path}, strings.NewReader(""), env, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit = %d, stderr %s", code, stderr.String())
		}
		var out output
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("parse output: %v", err)
		}
		if out.Reject != nil {
			t.Fatalf("unexpected rejection: %+v", out.Reject)
		}
		if string(out.Payload) != "null" {
			t.Fatalf("payload = %s, want null", out.Payload)
		}
	})
}
