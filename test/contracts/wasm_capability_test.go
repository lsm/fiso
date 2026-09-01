package contracts

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The WASM/Wasmer capability contract: authoritative documentation may only
// claim what the shipped runtimes execute. wasmer-go v1.0.4 exposes no
// WASIX socket or thread imports, so WASM guests cannot open network
// connections, spawn threads, or keep state between invocations — every
// "app" is a per-request function invoked over a host-side HTTP facade
// (internal/wasm/runtime_wasmer_app.go). Until that changes with executable
// evidence, capability claims must stay out of authoritative docs.

// wasmCapabilityClaims are phrases that only appear when a doc claims
// guest-level networking, threading, or full-application support.
var wasmCapabilityClaims = regexp.MustCompile(`(?i)(WASIX|Django|FastAPI|Next\.js|pthreads|database connectivity|poolSize)`)

// networkAccessClaim matches "network access" used affirmatively; negated
// statements ("no network access", "without network access") are the required
// limitation wording and must not be rejected.
var networkAccessClaim = regexp.MustCompile(`(?i)network access`)

func isNegatedNetworkAccess(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "no network access") || strings.Contains(lower, "without network access")
}

// TestAuthoritativeDocsDoNotClaimWasmNetworkingOrThreads rejects
// socket/threading/database/full-application capability claims for WASM in
// current authoritative documentation.
func TestAuthoritativeDocsDoNotClaimWasmNetworkingOrThreads(t *testing.T) {
	for _, path := range authoritativeDocPaths(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if loc := wasmCapabilityClaims.FindStringIndex(line); loc != nil {
				t.Errorf("%s:%d: authoritative doc claims unsupported WASM capability (%q) — WASM guests have no sockets, threads, or persistent state",
					path, i+1, strings.TrimSpace(line[loc[0]:loc[1]]))
			}
			if networkAccessClaim.MatchString(line) && !isNegatedNetworkAccess(line) {
				t.Errorf("%s:%d: authoritative doc claims WASM network access — guests cannot open sockets; negate the claim (\"no network access\") if describing the limitation",
					path, i+1)
			}
		}
	}
}

// TestWasmerGuideStatesExecutableContract requires the Wasmer guide to
// describe the executable contract explicitly: per-request invocation with no
// network access.
func TestWasmerGuideStatesExecutableContract(t *testing.T) {
	data, err := os.ReadFile("../../docs/wasmer-integration.md")
	if err != nil {
		t.Fatalf("read wasmer-integration.md: %v", err)
	}
	guide := string(data)
	if !strings.Contains(guide, "per-request") {
		t.Error("Wasmer guide must state that WASM modules are invoked per request")
	}
	if !strings.Contains(guide, "no network access") {
		t.Error("Wasmer guide must state that WASM modules have no network access")
	}
	if !strings.Contains(guide, "host-side") {
		t.Error("Wasmer guide must state that app HTTP serving is host-side, not in-guest")
	}
}
