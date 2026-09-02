// Command auth is a Fiso wasm interceptor guest that authenticates
// requests: it verifies the Authorization bearer token as a JWT against
// keys delivered through interceptor env configuration (ADR 0008) and
// refuses unauthenticated traffic through the rejection contract
// (ADR 0007).
//
// Configuration (environment variables, set via the interceptor's
// config.env map):
//
//	AUTH_HS256_SECRET        HMAC-SHA256 shared secret (enables HS256)
//	AUTH_RS256_PUBLIC_KEY    PEM-encoded PKIX RSA public key (enables RS256)
//	AUTH_ED25519_PUBLIC_KEY  base64 (std) raw 32-byte public key (enables EdDSA)
//	AUTH_ALLOW_MISSING_EXPIRY  set to "true" to accept tokens without exp
//
// At least one key must be configured. exp is enforced when present and
// required by default; nbf is honored when present; alg "none" and any
// algorithm without a configured key are refused.
//
// On success the payload passes through byte-identically (including
// non-JSON and binary bodies), the Authorization header is stripped, and
// X-Authenticated/X-Auth-Subject carry the verdict downstream.
package main

import (
	"crypto"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// envelope is the guest side of the interceptor ABI.
type envelope struct {
	Payload   json.RawMessage   `json:"payload"`
	Headers   map[string]string `json:"headers"`
	Direction string            `json:"direction"`
}

type output struct {
	Payload json.RawMessage   `json:"payload"`
	Headers map[string]string `json:"headers"`
	Reject  *rejection        `json:"reject,omitempty"`
}

type rejection struct {
	Status int    `json:"status"`
	Reason string `json:"reason"`
}

// authConfig holds the parsed verification keys; the algorithms it enables
// are exactly the ones with configured key material.
type authConfig struct {
	hs256Secret        string
	rs256PublicKey     *rsa.PublicKey
	ed25519PublicKey   ed25519.PublicKey
	allowMissingExpiry bool
	expectedAudience   string
}

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Getenv, os.Stdout, os.Stderr))
}

// run is the testable module entry point: it returns the process exit
// code. Exit 1 is an interceptor error (failOpen applies); a rejection is
// normal output with exit 0.
func run(args []string, stdin io.Reader, getenv func(string) string, stdout, stderr io.Writer) int {
	// The two engines deliver input differently: wazero pipes JSON to
	// stdin; wasmer writes it to a file passed as --stdin-file. Args are
	// parsed manually: the flag package exits(2) on any unexpected
	// argument, which engines may pass.
	var input []byte
	var err error
	stdinArg := ""
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "--stdin-file" {
			stdinArg = args[i+1]
		}
	}
	if stdinArg != "" {
		input, err = os.ReadFile(stdinArg)
	} else {
		input, err = io.ReadAll(stdin)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "auth: read input: %v\n", err)
		return 1
	}

	var req envelope
	if err := json.Unmarshal(input, &req); err != nil {
		_, _ = fmt.Fprintf(stderr, "auth: parse input: %v\n", err)
		return 1
	}

	// Misconfigured key material is an interceptor error, not a rejection:
	// the module must not answer every request with 401 and pretend to be
	// deciding.
	cfg, err := loadConfig(getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "auth: configuration: %v\n", err)
		return 1
	}

	dec := authenticate(cfg, req.Headers, time.Now())
	if !dec.ok {
		_ = json.NewEncoder(stdout).Encode(output{
			Reject: &rejection{Status: dec.status, Reason: dec.reason},
		})
		return 0
	}

	headers := make(map[string]string, len(req.Headers)+2)
	for k, v := range req.Headers {
		// Strip every casing of the credential header and of the reserved
		// verdict headers: downstream systems must not receive the raw
		// token, nor a caller-forged authentication verdict that verified
		// claims did not produce.
		if strings.EqualFold(k, "Authorization") ||
			strings.EqualFold(k, "X-Authenticated") ||
			strings.EqualFold(k, "X-Auth-Subject") {
			continue
		}
		headers[k] = v
	}
	headers["X-Authenticated"] = "true"
	if dec.subject != "" {
		headers["X-Auth-Subject"] = dec.subject
	}

	// The payload is echoed as received — never parsed or re-encoded — so
	// non-JSON and binary bodies survive interception byte-identically.
	_ = json.NewEncoder(stdout).Encode(output{
		Payload: req.Payload,
		Headers: headers,
	})
	return 0
}

// loadConfig reads and parses the key material. Failing here is a startup
// error: the module refuses to run rather than refusing all traffic.
func loadConfig(getenv func(string) string) (*authConfig, error) {
	cfg := &authConfig{
		hs256Secret:        getenv("AUTH_HS256_SECRET"),
		allowMissingExpiry: getenv("AUTH_ALLOW_MISSING_EXPIRY") == "true",
		expectedAudience:   getenv("AUTH_EXPECTED_AUDIENCE"),
	}

	if pemKey := getenv("AUTH_RS256_PUBLIC_KEY"); pemKey != "" {
		block, _ := pem.Decode([]byte(pemKey))
		if block == nil {
			return nil, fmt.Errorf("AUTH_RS256_PUBLIC_KEY is not valid PEM")
		}
		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("AUTH_RS256_PUBLIC_KEY: %w", err)
		}
		rsaPub, isRSA := pub.(*rsa.PublicKey)
		if !isRSA {
			return nil, fmt.Errorf("AUTH_RS256_PUBLIC_KEY is a %T, not an RSA key", pub)
		}
		cfg.rs256PublicKey = rsaPub
	}

	if b64Key := getenv("AUTH_ED25519_PUBLIC_KEY"); b64Key != "" {
		raw, err := base64.StdEncoding.DecodeString(b64Key)
		if err != nil {
			return nil, fmt.Errorf("AUTH_ED25519_PUBLIC_KEY: %w", err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("AUTH_ED25519_PUBLIC_KEY decodes to %d bytes, want %d", len(raw), ed25519.PublicKeySize)
		}
		cfg.ed25519PublicKey = ed25519.PublicKey(raw)
	}

	if cfg.hs256Secret == "" && cfg.rs256PublicKey == nil && cfg.ed25519PublicKey == nil {
		return nil, fmt.Errorf("no verification key configured: set AUTH_HS256_SECRET, AUTH_RS256_PUBLIC_KEY, or AUTH_ED25519_PUBLIC_KEY")
	}
	return cfg, nil
}

// decision is authenticate's verdict: either a pass with the verified
// subject, or a refusal with the caller-facing status and reason.
type decision struct {
	ok      bool
	status  int
	reason  string
	subject string
}

func refuse(status int, reason string) decision {
	return decision{ok: false, status: status, reason: reason}
}

func authenticate(cfg *authConfig, headers map[string]string, now time.Time) decision {
	token := bearerToken(headers)
	if token == "" {
		return refuse(401, "missing credentials")
	}

	parts := strings.Split(token, ".")
	// The signature segment may legitimately be empty — that is the shape
	// of an alg:none token — so emptiness alone is not malformation; the
	// algorithm dispatch below refuses it as "not allowed" instead.
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return refuse(401, "malformed token")
	}
	var head struct {
		Alg string `json:"alg"`
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(headerJSON, &head) != nil {
		return refuse(401, "malformed token")
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return refuse(401, "malformed token")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return refuse(401, "malformed token")
	}
	signingInput := []byte(parts[0] + "." + parts[1])

	switch head.Alg {
	case "HS256":
		if cfg.hs256Secret == "" {
			return refuse(401, "algorithm not allowed")
		}
		mac := hmac.New(sha256.New, []byte(cfg.hs256Secret))
		mac.Write(signingInput)
		if !hmac.Equal(mac.Sum(nil), signature) {
			return refuse(401, "invalid signature")
		}
	case "RS256":
		if cfg.rs256PublicKey == nil {
			return refuse(401, "algorithm not allowed")
		}
		digest := sha256.Sum256(signingInput)
		if err := rsa.VerifyPKCS1v15(cfg.rs256PublicKey, crypto.SHA256, digest[:], signature); err != nil {
			return refuse(401, "invalid signature")
		}
	case "EdDSA":
		if cfg.ed25519PublicKey == nil {
			return refuse(401, "algorithm not allowed")
		}
		if !ed25519.Verify(cfg.ed25519PublicKey, signingInput, signature) {
			return refuse(401, "invalid signature")
		}
	default:
		// Includes "none": an unsigned token is never a credential.
		return refuse(401, "algorithm not allowed")
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return refuse(401, "malformed claims")
	}

	// An expected audience prevents cross-service token replay when an
	// issuer's signing key is shared between APIs: a token signed for a
	// different service is not a credential here.
	if cfg.expectedAudience != "" && !audienceMatches(claims, cfg.expectedAudience) {
		return refuse(401, "invalid audience")
	}

	// Presence is tracked separately from numeric parsing: a present but
	// non-numeric exp/nbf is malformed, not absent — treating it as absent
	// would honor an attacker-chosen interpretation.
	expRaw, hasExp := claims["exp"]
	if hasExp {
		exp, isNumeric := expRaw.(float64)
		if !isNumeric {
			return refuse(401, "malformed claims")
		}
		if now.After(time.Unix(int64(exp), 0)) {
			return refuse(401, "token expired")
		}
	} else if !cfg.allowMissingExpiry {
		// exp is required by default: a credential without an expiry
		// outlives every rotation policy.
		return refuse(401, "token has no expiry")
	}
	if nbfRaw, hasNbf := claims["nbf"]; hasNbf {
		nbf, isNumeric := nbfRaw.(float64)
		if !isNumeric {
			return refuse(401, "malformed claims")
		}
		if now.Before(time.Unix(int64(nbf), 0)) {
			return refuse(401, "token not yet valid")
		}
	}

	dec := decision{ok: true}
	if sub, isStr := claims["sub"].(string); isStr {
		dec.subject = sub
	}
	return dec
}

// bearerToken extracts the Authorization bearer credential, tolerating
// header-name and scheme casing differences across sources (scheme names
// are case-insensitive per RFC 9110).
func bearerToken(headers map[string]string) string {
	for k, v := range headers {
		if !strings.EqualFold(k, "Authorization") {
			continue
		}
		scheme, rest, found := strings.Cut(v, " ")
		if !found || !strings.EqualFold(scheme, "Bearer") {
			return ""
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

// audienceMatches reports whether the aud claim — a string or an array of
// strings in JWT — contains the expected audience.
func audienceMatches(claims map[string]interface{}, expected string) bool {
	switch aud := claims["aud"].(type) {
	case string:
		return aud == expected
	case []interface{}:
		for _, entry := range aud {
			if s, isStr := entry.(string); isStr && s == expected {
				return true
			}
		}
	}
	return false
}
