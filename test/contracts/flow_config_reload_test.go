// Package contracts pins repository documentation contracts: statements
// current authoritative docs make about runtime behavior must match what the
// shipped binaries actually do. These are documentation-contract tests by
// design — a runtime test expecting stale behavior would canonize the defect.
package contracts

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// nonFisoMentions lists wording that mentions reload but refers to something
// other than Fiso's own behavior (the user's host service during fiso dev).
// Only the phrase itself is masked before matching, so a Fiso claim sharing
// the same line is still caught.
var nonFisoMentions = []string{
	"your service runs on the host for fast iteration with live reload",
}

// reloadPhrase matches hot-, live-, auto-, and automatic-reload wording
// (hyphenated, spaced, or wrapped) and unqualified reload-on-change claims —
// all promise the same unsupported runtime behavior. Negated statements
// ("does not reload") are a conscious limitation: none exist in authoritative
// docs today, and the guard errs toward flagging.
var reloadPhrase = regexp.MustCompile(`(?i)((hot|live|auto|automatic(ally)?)[-?\s]?reloads?|reloads?[^.]{0,60}?on changes)`)

var mentionRegex = func() *regexp.Regexp {
	parts := make([]string, len(nonFisoMentions))
	for i, m := range nonFisoMentions {
		parts[i] = regexp.QuoteMeta(m)
	}
	return regexp.MustCompile(`(?i)` + strings.Join(parts, "|"))
}()

// authoritativeDocPaths returns the README and exactly the guides the
// documentation index (docs/README.md "Current Guides") classifies as current.
// Directional documents (product vision, roadmap) may legitimately name
// reload as proposed work, so they are not scanned; the contract governs
// current-behavior claims only.
func authoritativeDocPaths(t *testing.T) []string {
	t.Helper()
	index, err := os.ReadFile("../../docs/README.md")
	if err != nil {
		t.Fatalf("read docs index: %v", err)
	}
	section := headingSection(string(index), "Current Guides")
	link := regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	var guides []string
	for _, m := range link.FindAllStringSubmatch(section, -1) {
		target := m[1]
		if i := strings.Index(target, "#"); i >= 0 {
			target = target[:i] // strip any section fragment
		}
		if !strings.HasSuffix(target, ".md") {
			continue
		}
		guides = append(guides, filepath.Join("../../docs", target))
	}
	if len(guides) == 0 {
		t.Fatal("docs index lists no current guides — update the contract if the index moved")
	}
	sort.Strings(guides)
	return append([]string{"../../README.md"}, guides...)
}

// TestAuthoritativeDocsDoNotClaimConfigReload rejects hot-reload and
// live-reload claims about Fiso configuration in current authoritative
// documentation. The Loader watches the config directory and reparses changed
// files into its in-memory definitions, but no Flow-capable binary registers
// an OnChange callback or rebuilds, replaces, or stops running pipelines.
// Until live reload ships with its own contract, such claims must stay out of
// authoritative docs.
func TestAuthoritativeDocsDoNotClaimConfigReload(t *testing.T) {
	for _, path := range authoritativeDocPaths(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, loc := range findReloadClaims(string(data)) {
			t.Errorf("%s:%d: authoritative doc claims config reload (%q) — running pipelines are not rebuilt; restart is required to apply config changes",
				path, loc.line, loc.match)
		}
	}
}

// claimLoc reports a matched reload claim and the source line it starts on.
type claimLoc struct {
	line  int
	match string
}

// findReloadClaims normalizes the document onto one text stream (so a claim
// or an allowed mention wrapped across Markdown lines behaves as its single
// rendered sentence), masks the known non-Fiso mentions with equal-length
// spaces (preserving offsets for line reporting), and reports each match at
// its starting source line.
func findReloadClaims(doc string) []claimLoc {
	type lineSpan struct {
		start int // offset in normalized text
		line  int
	}
	var spans []lineSpan
	var norm strings.Builder
	for i, line := range strings.Split(doc, "\n") {
		spans = append(spans, lineSpan{start: norm.Len(), line: i + 1})
		norm.WriteString(line)
		norm.WriteString(" ")
	}
	text := mentionRegex.ReplaceAllStringFunc(norm.String(), func(m string) string {
		return strings.Repeat(" ", len(m))
	})
	var claims []claimLoc
	for _, m := range reloadPhrase.FindAllStringIndex(text, -1) {
		line := 1
		for _, s := range spans {
			if s.start <= m[0] {
				line = s.line
			}
		}
		claims = append(claims, claimLoc{line: line, match: strings.TrimSpace(text[m[0]:m[1]])})
	}
	return claims
}

// TestReadmeStatesRestartRequirement requires README's Flow configuration
// section to state the limitation explicitly: file watching reparses
// definitions only, running pipelines are not rebuilt, and a process restart
// applies configuration changes.
func TestReadmeStatesRestartRequirement(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	section := readmeSection(t, string(data), "Flow Definition")
	if section == "" {
		t.Fatal("README Flow Definition section not found")
	}

	// Both assertions pin the connected contract, not scattergun substrings:
	// the restart must be tied to applying configuration changes, and the
	// no-rebuild statement must be about running pipelines. Unrelated
	// sentences elsewhere in the section must not satisfy either check.
	if !strings.Contains(section, "running pipelines are not rebuilt") {
		t.Error("README Flow section must state that running pipelines are not rebuilt on config changes")
	}
	if !strings.Contains(strings.ToLower(section), "restart the process to apply configuration changes") {
		t.Error("README Flow section must tie a process restart to applying configuration changes")
	}
}

// readmeSection returns the text of the heading section whose title contains
// the given string, including nested child subsections: the section ends only
// at a heading of the same or higher level. Only Markdown headings outside
// fenced code blocks bound a section — column-zero `#` comments inside YAML
// or shell examples are not headings.
func readmeSection(t *testing.T, doc, title string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	var out []string
	in := false
	inFence := false
	level := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(line, "#") {
			lvl := headingLevel(line)
			if in {
				if lvl <= level {
					break
				}
			} else if strings.Contains(line, title) {
				in = true
				level = lvl
			}
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// headingLevel counts the leading '#' of a Markdown heading.
func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	return n
}

// headingSection extracts a "## " section from the docs index (headings of
// the same level end it).
func headingSection(doc, title string) string {
	lines := strings.Split(doc, "\n")
	var out []string
	in := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if in {
				break
			}
			in = strings.Contains(line, title)
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
