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

// historicalDocs are explicitly non-authoritative (docs/README.md lists them
// under Historical Documents); editing them is out of scope for current
// contracts.
var historicalDocs = map[string]bool{
	"hld-specification.md": true,
}

// nonFisoMentions lists wording that mentions reload but refers to something
// other than Fiso's own behavior (the user's host service during fiso dev).
// Each entry must stay narrowly scoped to its sentence.
var nonFisoMentions = []string{
	"your service runs on the host for fast iteration with live reload",
}

// reloadPhrase matches hot-reload and live-reload wording in either word
// order split across a hyphen or space.
var reloadPhrase = regexp.MustCompile(`(?i)(hot|live)[-?\s]?reload`)

// authoritativeDocPaths returns the README and every current guide under
// docs/, excluding explicitly historical documents.
func authoritativeDocPaths(t *testing.T) []string {
	t.Helper()
	paths := []string{"../../README.md"}
	entries, err := os.ReadDir("../../docs")
	if err != nil {
		t.Fatalf("read docs dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || historicalDocs[e.Name()] {
			continue
		}
		paths = append(paths, filepath.Join("../../docs", e.Name()))
	}
	sort.Strings(paths[1:])
	return paths
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
		for i, line := range strings.Split(string(data), "\n") {
			if mentionsNonFisoReload(line) {
				continue
			}
			if loc := reloadPhrase.FindStringIndex(line); loc != nil {
				t.Errorf("%s:%d: authoritative doc claims config reload (%q) — running pipelines are not rebuilt; restart is required to apply config changes",
					path, i+1, strings.TrimSpace(line[loc[0]:loc[1]]))
			}
		}
	}
}

// mentionsNonFisoReload reports whether the line's reload wording refers to
// something other than Fiso (see nonFisoMentions).
func mentionsNonFisoReload(line string) bool {
	lower := strings.ToLower(line)
	for _, mention := range nonFisoMentions {
		if strings.Contains(lower, mention) {
			return true
		}
	}
	return false
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

	if !strings.Contains(section, "Restart the process") {
		t.Error("README Flow section must state that a process restart is required to apply configuration changes")
	}
	if !strings.Contains(section, "not rebuilt") {
		t.Error("README Flow section must state that running pipelines are not rebuilt on config changes")
	}
}

// readmeSection returns the text of the heading section whose title contains
// the given string. Only Markdown headings outside fenced code blocks bound a
// section — column-zero `#` comments inside YAML or shell examples are not
// headings.
func readmeSection(t *testing.T, doc, title string) string {
	t.Helper()
	var out []string
	in := false
	inFence := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(line, "#") {
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
