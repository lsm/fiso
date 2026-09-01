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
// limitation wording and are masked before matching.
var networkAccessClaim = regexp.MustCompile(`(?i)network access`)

var negatedNetworkAccess = regexp.MustCompile(`(?i)(no|without) network access`)

// claimLine maps a match offset in normalized text back to its source line.
type claimLine struct {
	line  int
	match string
}

// findWasmClaims normalizes the document onto one text stream (so phrases
// wrapped across Markdown lines still match), masks the negated limitation
// wording with equal-length spaces, and reports remaining capability claims
// at their starting source line.
func findWasmClaims(doc string) []claimLine {
	type lineSpan struct {
		start int
		line  int
	}
	var spans []lineSpan
	var norm strings.Builder
	for i, line := range strings.Split(doc, "\n") {
		spans = append(spans, lineSpan{start: norm.Len(), line: i + 1})
		norm.WriteString(line)
		norm.WriteString(" ")
	}
	text := negatedNetworkAccess.ReplaceAllStringFunc(norm.String(), func(m string) string {
		return strings.Repeat(" ", len(m))
	})
	var claims []claimLine
	for _, re := range []*regexp.Regexp{wasmCapabilityClaims, networkAccessClaim} {
		for _, m := range re.FindAllStringIndex(text, -1) {
			line := 1
			for _, s := range spans {
				if s.start <= m[0] {
					line = s.line
				}
			}
			claims = append(claims, claimLine{line: line, match: strings.TrimSpace(text[m[0]:m[1]])})
		}
	}
	return claims
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
		for _, c := range findWasmClaims(string(data)) {
			t.Errorf("%s:%d: authoritative doc claims unsupported WASM capability (%q) — WASM guests have no sockets, threads, or persistent state",
				path, c.line, c.match)
		}
	}
}

// TestWasmerGuideStatesExecutableContract requires the Wasmer guide to
// describe the executable contract explicitly: per-request invocation with no
// network access, served host-side.
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
