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
var affirmativeCapabilityClaim = regexp.MustCompile(`(?i)\b(support(?:s|ed|ing)?|enable(?:s|d)?|provid(?:e|es|ing)|offer(?:s|ing)?|requir(?:e|es|ing)|with|can|may|able to|open|opens|spawn|spawns|make|makes|call|calls)\b[^.]{0,100}\b(sockets?|threads?|threading|multithreading|pthreads|database connect\w+|network access|persistent (?:in-memory )?state|full[-\s]?fledged applications?|full applications?)\b`)

// ecosystemTokens name the unsupported ecosystem explicitly; they may appear
// only in negated context ("WASIX is not supported", "no Django").
var ecosystemTokens = regexp.MustCompile(`(?i)(WASIX|Django|FastAPI|Next\.js|pthreads|poolSize|database connectivity)`)

// negatedSentence matches sentence-level limitation wording: a capability
// mention whose sentence contains one of these forms is a limitation
// statement, not a claim.
var negatedSentence = regexp.MustCompile(`(?i)((?:do(?:es)? not|don't|doesn't|cannot|can not|may not) (?:support|provide|enable|have|use|work|open|spawn|make|call)|(?:are|is|was|were) (?:not supported|unsupported)|not (?:currently )?(?:supported|implemented|applied|available)|no longer [a-z]+|\bno (?:wasm|wasmer|wazero|sockets?|threads?|threading|multithreading|pthreads|network access|database connectivity|WASIX|Django|FastAPI|Next\.js|poolSize)|lacks? |excludes? |without )`)

// clauseSplit separates contrast clauses within a sentence.
var clauseSplit = regexp.MustCompile(`(?i),?\s*(?:but|yet|however|whereas|while|though)\s+|;`)

// strongSubject unambiguously attributes a clause to WASM.
var strongSubject = regexp.MustCompile(`(?i)\b(wasm|wasmer|wazero)\b`)

// qualifiedGeneric matches a generic subject (module/app/interceptor/guest)
// only when a WASM qualifier precedes it ("WASM modules", "Wasmer guests").
// A bare generic ("The native apps support...") is out of scope.
var qualifiedGeneric = regexp.MustCompile(`(?i)\b(?:wasm|wasmer|wazero)[\w-]*\s+(?:\w+\s+){0,2}(modules?|guests?|interceptors?|apps?)\b`)

// wasmSubjects reports whether a clause attributes its capability to WASM:
// either an unambiguous engine/runtime mention or a qualified generic.
func wasmSubjects(clause string) bool {
	return strongSubject.MatchString(clause) || qualifiedGeneric.MatchString(clause)
}

// negatedMentions are phrase-level limitation markers masked before
// affirmative matching.
var negatedMentions = regexp.MustCompile(`(?i)((no|without|lacks?|excludes?) (network access|sockets?|threading|multithreading|pthreads|persistent (?:in-memory )?state|database connectivity)|(?:(?:do(?:es)? not|don't|doesn't) (?:support|provide|enable|have|use|work))|no longer \w+|not (?:currently )?(?:supported|implemented|applied|available)|unsupported|cannot |does not |doesn't |never )`)

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
	sentenceOf := func(start, end int) string {
		sentStart := strings.LastIndex(unmasked[:start], ".")
		if sentStart == -1 {
			sentStart = 0
		}
		sentEnd := strings.Index(unmasked[end:], ".")
		if sentEnd == -1 {
			sentEnd = len(unmasked)
		} else {
			sentEnd += end
		}
		return unmasked[sentStart:sentEnd]
	}
	// clauseOf narrows a sentence to the contrast clause containing the
	// match, so "wazero does not support sockets, but Wasmer supports
	// sockets" only exempts the first clause.
	clauseOf := func(start, end int) string {
		sentence := sentenceOf(start, end)
		sentStart := strings.LastIndex(unmasked[:start], ".")
		if sentStart == -1 {
			sentStart = 0
		}
		offsetInSentence := start - sentStart
		pos := 0
		for _, c := range clauseSplit.Split(sentence, -1) {
			next := pos + len(c)
			if offsetInSentence <= next {
				return c
			}
			pos = next
		}
		return sentence
	}
	var claims []claimLine
	for _, m := range affirmativeCapabilityClaim.FindAllStringIndex(text, -1) {
		clause := clauseOf(m[0], m[1])
		// Negation in the match's own clause exempts it; negation elsewhere
		// in the sentence does not.
		if negatedSentence.MatchString(clause) {
			continue
		}
		// Only claims whose own clause attributes the capability to a WASM
		// subject count; capability statements about other components are
		// not this contract's concern even when WASM is mentioned elsewhere
		// in the sentence.
		if !wasmSubjects(clause) {
			continue
		}
		claims = append(claims, claimLine{line: lineOf(m[0]), match: strings.TrimSpace(text[m[0]:m[1]])})
	}
	// Ecosystem tokens: flag only when the surrounding clause carries no
	// negation and the sentence is about WASM.
	for _, m := range ecosystemTokens.FindAllStringIndex(unmasked, -1) {
		clause := clauseOf(m[0], m[1])
		if negatedSentence.MatchString(clause) {
			continue
		}
		if !wasmSubjects(clause) {
			continue
		}
		claims = append(claims, claimLine{line: lineOf(m[0]), match: strings.TrimSpace(unmasked[m[0]:m[1]])})
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
// execution bound that does not exist. The scan is bounded by indentation —
// it stops at the first line at or left of the interceptor entry, so a
// top-level `sink:` block (with its own valid `timeout:`) ends the block.
func TestWasmInterceptorExamplesDoNotSetIgnoredTimeout(t *testing.T) {
	wasmEntry := regexp.MustCompile(`(\s*)-\s+type:\s*wasm`)
	indentOf := func(line string) int {
		for i, r := range line {
			if r != ' ' && r != '\t' {
				return i
			}
		}
		return len(line)
	}
	for _, path := range authoritativeDocPaths(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			m := wasmEntry.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			blockIndent := len(m[1])
			for j := i + 1; j < len(lines); j++ {
				trimmed := strings.TrimSpace(lines[j])
				if trimmed == "" || strings.HasPrefix(trimmed, "#") {
					continue
				}
				if indentOf(lines[j]) <= blockIndent {
					break // left the interceptor block
				}
				if strings.Contains(lines[j], "timeout:") {
					t.Errorf("%s:%d: wasm interceptor example sets timeout:, which no Flow builder applies", path, j+1)
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
