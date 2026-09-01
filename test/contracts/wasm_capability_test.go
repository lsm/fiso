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
// connections, spawn threads, or keep in-memory state between invocations —
// every "app" is a per-request function invoked over a host-side HTTP facade
// (internal/wasm/runtime_wasmer_app.go). Until that changes with executable
// evidence, capability claims must stay out of authoritative docs.

// affirmativeCapabilityClaim matches capability verbs joined to guest-level
// networking/threading/application capabilities, e.g. "supports sockets and
// threading", "enables database connectivity", "apps with network access".
var affirmativeCapabilityClaim = regexp.MustCompile(`(?i)\b(support(?:s|ed|ing)?|enable(?:s|d)?|provid(?:e|es|ing)|offer(?:s|ing)?|requir(?:e|es|ing)|with)\b[^.]{0,100}\b(sockets?|threading|multithreading|pthreads|database connect\w+|network access|full[-\s]?fledged applications?|full applications?)\b`)

// ecosystemTokens name the unsupported ecosystem explicitly; they may appear
// only in negated context ("WASIX is not supported", "no Django").
var ecosystemTokens = regexp.MustCompile(`(?i)(WASIX|Django|FastAPI|Next\.js|pthreads|poolSize|database connectivity)`)

// negatedMentions are phrases marking a capability mention as a limitation
// statement; they are masked before affirmative matching.
var negatedMentions = regexp.MustCompile(`(?i)((no|without|lacks?|excludes?) (network access|sockets?|threading|multithreading|pthreads|persistent state|database connectivity)|no longer \w+|not (?:currently )?(?:supported|implemented|applied|available)|unsupported|cannot |does not |doesn't |never )`)

// claimLine maps a match offset in normalized text back to its source line.
type claimLine struct {
	line  int
	match string
}

// findWasmClaims normalizes the document onto one text stream (so phrases
// wrapped across Markdown lines still match), masks negated limitation
// wording with equal-length spaces, and reports remaining affirmative
// capability claims at their starting source line. Ecosystem tokens count
// as claims only when their sentence carries no negation.
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
	masked := negatedMentions.ReplaceAllStringFunc(norm.String(), func(m string) string {
		return strings.Repeat(" ", len(m))
	})
	// Affirmative claims are matched against the masked text; ecosystem
	// tokens are checked against the unmasked text so their own negation
	// context ("Django is unsupported") is still visible.
	text := masked
	lineOf := func(off int) int {
		line := 1
		for _, s := range spans {
			if s.start <= off {
				line = s.line
			}
		}
		return line
	}
	unmasked := norm.String()
	var claims []claimLine
	for _, m := range affirmativeCapabilityClaim.FindAllStringIndex(text, -1) {
		claims = append(claims, claimLine{line: lineOf(m[0]), match: strings.TrimSpace(text[m[0]:m[1]])})
	}
	// Ecosystem tokens: flag only when the surrounding sentence (text between
	// periods) contains no negation marker.
	for _, m := range ecosystemTokens.FindAllStringIndex(text, -1) {
		sentStart := strings.LastIndex(text[:m[0]], ".")
		if sentStart == -1 {
			sentStart = 0
		}
		sentEnd := strings.Index(text[m[1]:], ".")
		if sentEnd == -1 {
			sentEnd = len(text)
		} else {
			sentEnd += m[1]
		}
		sentence := unmasked[sentStart:sentEnd]
		if negatedMentions.MatchString(sentence) || strings.Contains(strings.ToLower(sentence), "unsupported") {
			continue
		}
		claims = append(claims, claimLine{line: lineOf(m[0]), match: strings.TrimSpace(text[m[0]:m[1]])})
	}
	return claims
}

// TestAuthoritativeDocsDoNotClaimWasmNetworkingOrThreads rejects
// socket/threading/database/full-application capability claims for WASM in
// current authoritative documentation, while allowing negated limitation
// statements.
func TestAuthoritativeDocsDoNotClaimWasmNetworkingOrThreads(t *testing.T) {
	for _, path := range authoritativeDocPaths(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, c := range findWasmClaims(string(data)) {
			t.Errorf("%s:%d: authoritative doc claims unsupported WASM capability (%q) — WASM guests have no sockets, threads, or in-memory persistent state",
				path, c.line, c.match)
		}
	}
}

// TestWasmInterceptorExamplesDoNotSetIgnoredTimeout rejects `timeout:` keys
// inside `type: wasm` interceptor config blocks in authoritative docs: no
// Flow builder reads the key, so examples configuring it would advertise an
// execution bound that does not exist.
func TestWasmInterceptorExamplesDoNotSetIgnoredTimeout(t *testing.T) {
	for _, path := range authoritativeDocPaths(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if !regexp.MustCompile(`- type:\s*wasm`).MatchString(line) {
				continue
			}
			for j := i + 1; j < len(lines) && j <= i+8; j++ {
				if strings.Contains(lines[j], "timeout:") {
					t.Errorf("%s:%d: wasm interceptor example sets timeout:, which no Flow builder applies", path, j+1)
				}
				if strings.Contains(lines[j], "- type:") {
					break
				}
			}
		}
	}
}

// TestWasmerGuideStatesExecutableContract requires the Wasmer guide to
// describe the executable contract explicitly: per-request invocation with no
// network access, served host-side, and the per-engine input mechanism.
func TestWasmerGuideStatesExecutableContract(t *testing.T) {
	data, err := os.ReadFile("../../docs/wasmer-integration.md")
	if err != nil {
		t.Fatalf("read wasmer-integration.md: %v", err)
	}
	guide := string(data)
	for _, want := range []string{"per-request", "no network access", "host-side", "--stdin-file"} {
		if !strings.Contains(guide, want) {
			t.Errorf("Wasmer guide must state %q as part of the executable contract", want)
		}
	}
}
