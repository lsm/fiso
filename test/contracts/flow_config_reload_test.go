// Package contracts pins repository documentation contracts: statements
// current authoritative docs make about runtime behavior must match what the
// shipped binaries actually do. These are documentation-contract tests by
// design — a runtime test expecting stale behavior would canonize the defect.
package contracts

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// authoritativeDocs lists current-authoritative documents (docs/README.md
// authority table) that may not overclaim Flow configuration reload. The
// historical HLD is deliberately excluded: it is explicitly non-authoritative
// and editing it is out of scope.
var authoritativeDocs = []string{
	"../../README.md",
	"../../docs/wasm-deployment.md",
}

var hotReloadPhrase = regexp.MustCompile(`(?i)hot-?\s?reload`)

// TestAuthoritativeDocsDoNotClaimHotReload rejects the "hot reload" claim in
// current authoritative documentation. The Loader watches the config
// directory and reparses changed files into its in-memory definitions, but no
// Flow-capable binary registers an OnChange callback or rebuilds, replaces,
// or stops running pipelines. Until live reload ships with its own contract,
// the phrase must stay out of authoritative docs.
func TestAuthoritativeDocsDoNotClaimHotReload(t *testing.T) {
	for _, path := range authoritativeDocs {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if loc := hotReloadPhrase.FindStringIndex(line); loc != nil {
				t.Errorf("%s:%d: authoritative doc claims hot reload (%q) — running pipelines are not rebuilt; restart is required to apply config changes",
					path, i+1, strings.TrimSpace(line[loc[0]:loc[1]]))
			}
		}
	}
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
		t.Fatal("README Flow Definitions section not found")
	}

	if !strings.Contains(section, "Restart the process") {
		t.Error("README Flow section must state that a process restart is required to apply configuration changes")
	}
	if !strings.Contains(section, "not rebuilt") {
		t.Error("README Flow section must state that running pipelines are not rebuilt on config changes")
	}
}

// readmeSection returns the text of the heading section whose title contains
// the given string. Any heading (## or deeper) ends the section.
func readmeSection(t *testing.T, doc, title string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	var out []string
	in := false
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
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
