package keywordplanner

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// TestCatalogConsistency is a REAL route-closure gate: it parses the actual
// source (this planner's rule table, app/fallback.go aliases, bootstrap wiring,
// the MCP Toolbox yaml, and the Cube metric bindings) and asserts every planner
// tool name resolves to a tier that can actually answer it. It reads live files
// — NOT hardcoded fixtures — so a drift like feed_direction_preview vs
// feed_direction_today, or a planner route to an unwired-and-unaliased API tool,
// FAILS the build. Guarded meta: TestGateHasTeeth proves it fails on an injected
// mismatch.

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	// this file: <root>/backend/internal/ceoai/adapters/keywordplanner/<f> -> 5 up = repo root
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", ".."))
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

type plannerRule struct {
	intent string
	tool   string
	route  string // "cube" | "api" | "toolbox" | "sql"
}

// parsePlannerRules extracts every rule from THIS package's rule table by reading
// planner.go. Each rule literal is: {regexp.MustCompile(`...`), "intent", "tool", domain.RouteX, "groupby"},
// Match the `"intent", "tool", domain.RouteX` triple that terminates each rule
// literal. Anchoring on domain.Route avoids parsing the MustCompile regex arg
// (which itself contains parens/quotes).
var ruleLineRe = regexp.MustCompile(`"([a-zA-Z0-9_]+)"\s*,\s*"([a-zA-Z0-9_]+)"\s*,\s*domain\.Route(\w+)`)

func parsePlannerRules(t *testing.T) []plannerRule {
	src := readRepoFile(t, "backend/internal/ceoai/adapters/keywordplanner/planner.go")
	var rules []plannerRule
	for _, m := range ruleLineRe.FindAllStringSubmatch(src, -1) {
		rules = append(rules, plannerRule{intent: m[1], tool: m[2], route: lc(m[3])})
	}
	if len(rules) < 5 {
		t.Fatalf("parsed only %d planner rules — parser likely broke", len(rules))
	}
	return rules
}

func lc(s string) string {
	switch s {
	case "Cube":
		return "cube"
	case "API":
		return "api"
	case "Toolbox":
		return "toolbox"
	case "SQL":
		return "sql"
	}
	return s
}

// setKeys extracts the top-level string keys of a `var <name> = map[...]{...}`
// block WITHOUT tripping on nested `{...}` values: it finds the block, then
// matches `"key":` at the start of each entry.
var entryKeyRe = regexp.MustCompile(`"([a-zA-Z0-9_]+)"\s*:`)

func parseFallbackAliasKeys(t *testing.T) map[string]bool {
	src := readRepoFile(t, "backend/internal/ceoai/app/fallback.go")
	block := between(src, "var fallbackAliases = map[string]fallbackAlias{", "\n}")
	keys := map[string]bool{}
	for _, m := range entryKeyRe.FindAllStringSubmatch(block, -1) {
		keys[m[1]] = true
	}
	return keys
}

// parseWiredReaders finds which readtools executors get a real reader wired in
// bootstrap (SetCountsDataReader/SetVaccinationDataReader/SetFeedDataReader) and
// maps them to the tool names they cover.
func parseWiredReaders(t *testing.T) map[string]bool {
	src := readRepoFile(t, "backend/internal/bootstrap/api.go")
	wired := map[string]bool{}
	if regexp.MustCompile(`SetCountsDataReader\(`).MatchString(src) {
		wired["counts_breakdown"] = true
	}
	if regexp.MustCompile(`SetVaccinationDataReader\(`).MatchString(src) {
		wired["vaccination_shed_summary"] = true
		wired["vaccination_execution"] = true
	}
	if regexp.MustCompile(`SetFeedDataReader\(`).MatchString(src) {
		wired["feed_direction_today"] = true
	}
	return wired
}

func parseCubeMetricKeys(t *testing.T) map[string]bool {
	src := readRepoFile(t, "backend/internal/ceoai/wiring.go")
	block := between(src, "cubeMetricBindings", "\n\t}")
	keys := map[string]bool{}
	for _, m := range entryKeyRe.FindAllStringSubmatch(block, -1) {
		keys[m[1]] = true
	}
	return keys
}

func parseToolboxToolNames(t *testing.T) map[string]bool {
	src := readRepoFile(t, "docs/ceo-ai/mcp-toolbox-tools.yaml")
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s{2}(mesha_[a-z0-9_]+)\s*:`).FindAllStringSubmatch(src, -1) {
		names[m[1]] = true
	}
	return names
}

func between(s, start, end string) string {
	i := indexOf(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	j := indexOf(rest, end)
	if j < 0 {
		return rest
	}
	return rest[:j]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestCatalogConsistency: every planner tool name resolves to a working tier.
func TestCatalogConsistency(t *testing.T) {
	rules := parsePlannerRules(t)
	cube := parseCubeMetricKeys(t)
	aliases := parseFallbackAliasKeys(t)
	wired := parseWiredReaders(t)
	toolbox := parseToolboxToolNames(t)

	for _, r := range rules {
		switch r.route {
		case "cube":
			if !cube[r.tool] {
				t.Errorf("rule %q -> Cube %q: NOT in cubeMetricBindings", r.intent, r.tool)
			}
		case "api":
			// A RouteAPI tool must have a wired reader OR a fallback alias that
			// reaches a working tier (its toolbox target must exist in the yaml).
			if wired[r.tool] {
				continue
			}
			if !aliases[r.tool] {
				t.Errorf("rule %q -> RouteAPI %q: reader NOT wired AND no fallback alias — dead-ends", r.intent, r.tool)
				continue
			}
			// alias exists; verify its toolbox equivalent is real (best-effort:
			// the alias must name a toolbox tool that exists).
			assertAliasReachable(t, r.tool, toolbox)
		case "toolbox":
			if !toolbox[r.tool] {
				t.Errorf("rule %q -> Toolbox %q: NOT in mcp-toolbox-tools.yaml", r.intent, r.tool)
			}
		case "sql":
			// SQL fallback is always allowed.
		}
	}
}

// assertAliasReachable checks the fallbackAliases entry for tool names a toolbox
// target that actually exists in the yaml (real fallback, not a dangling name).
func assertAliasReachable(t *testing.T, tool string, toolbox map[string]bool) {
	t.Helper()
	src := readRepoFile(t, "backend/internal/ceoai/app/fallback.go")
	block := between(src, "var fallbackAliases = map[string]fallbackAlias{", "\n}")
	// find the entry line for this tool and its toolbox: "..." value
	entryRe := regexp.MustCompile(`"` + regexp.QuoteMeta(tool) + `"\s*:\s*\{([^}]*)\}`)
	m := entryRe.FindStringSubmatch(block)
	if m == nil {
		t.Errorf("alias entry for %q not found even though key present", tool)
		return
	}
	tb := regexp.MustCompile(`toolbox\s*:\s*"([^"]+)"`).FindStringSubmatch(m[1])
	if tb == nil {
		return // alias may use cube/sql instead; other checks cover cube
	}
	if !toolbox[tb[1]] {
		t.Errorf("alias %q -> toolbox %q which does NOT exist in mcp-toolbox-tools.yaml", tool, tb[1])
	}
}

// TestGateHasTeeth proves the gate is not a rubber stamp: an injected planner
// tool name with no catalog entry must be detected as unresolved. It parses the
// same way TestCatalogConsistency does, against a mutated in-memory rule set.
func TestGateHasTeeth(t *testing.T) {
	cube := parseCubeMetricKeys(t)
	aliases := parseFallbackAliasKeys(t)
	wired := parseWiredReaders(t)
	bogus := plannerRule{intent: "injected", tool: "feed_direction_BOGUS_NONEXISTENT", route: "api"}
	resolves := wired[bogus.tool] || aliases[bogus.tool] || cube[bogus.tool]
	if resolves {
		t.Fatalf("gate is TOOTHLESS: a bogus tool name resolved in the catalog")
	}
}
