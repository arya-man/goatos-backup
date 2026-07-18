package app

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
)

// TestConfigVaccineOptionGroupsMatchPublishTaxonomy is the R2-07b guard: every ENABLED vaccine
// taxonomy option the Config UI offers (vaccine_types / vaccine_pathogen_classes /
// vaccine_course_types) must be a value the backend publish validation accepts. This locks the
// option groups, publish.go's valid sets, and (by mirroring) the frontend taxonomy constants to ONE
// reconciled taxonomy so the UI can never again present an option its own Save + server publication
// reject.
func TestConfigVaccineOptionGroupsMatchPublishTaxonomy(t *testing.T) {
	validators := map[string]func(string) bool{
		"vaccine_types":            protocolapp.IsValidVaccineType,
		"vaccine_pathogen_classes": protocolapp.IsValidPathogenClass,
		"vaccine_course_types":     protocolapp.IsValidCourseType,
	}

	seen := map[string]bool{}
	for _, group := range configOptionGroups() {
		validate, ok := validators[group.ID]
		if !ok {
			continue
		}
		seen[group.ID] = true
		for _, opt := range group.Options {
			if !opt.Enabled {
				continue
			}
			if !validate(opt.Key) {
				t.Errorf("option group %q offers enabled option %q that backend publish validation rejects", group.ID, opt.Key)
			}
		}
	}

	for id := range validators {
		if !seen[id] {
			t.Errorf("config option group %q not found; taxonomy reconciliation guard cannot run", id)
		}
	}
}

// TestFrontendVaccineTaxonomyMatchesBackendOptionGroups is the R2-07b-GUARD fix: the Go guard above
// only compared two BACKEND lists, so the frontend vaccine-taxonomy.ts constants could drift back out
// of sync without failing anything. This parity check reads the actual TypeScript file and asserts
// each exported list equals the ENABLED keys of the matching backend Config option group -- the UI
// can no longer offer (or reject) a taxonomy value the backend does not.
func TestFrontendVaccineTaxonomyMatchesBackendOptionGroups(t *testing.T) {
	// backend/internal/adminui/app -> repo root -> apps/admin-web/...
	tsPath := filepath.Join("..", "..", "..", "..", "apps", "admin-web", "features", "config", "vaccine-taxonomy.ts")
	raw, err := os.ReadFile(tsPath)
	if err != nil {
		t.Fatalf("read %s: %v", tsPath, err)
	}
	ts := string(raw)

	backendOptionKeys := func(groupID string) []string {
		for _, group := range configOptionGroups() {
			if group.ID != groupID {
				continue
			}
			var keys []string
			for _, opt := range group.Options {
				if opt.Enabled {
					keys = append(keys, opt.Key)
				}
			}
			return keys
		}
		t.Fatalf("backend option group %q not found", groupID)
		return nil
	}

	cases := []struct {
		tsConst string
		group   string
	}{
		{"VALID_VACCINE_TYPES", "vaccine_types"},
		{"VALID_PATHOGEN_CLASSES", "vaccine_pathogen_classes"},
		{"VALID_COURSE_TYPES", "vaccine_course_types"},
	}
	for _, c := range cases {
		got := extractTSArray(t, ts, c.tsConst)
		want := backendOptionKeys(c.group)
		if !equalStringSets(got, want) {
			t.Errorf("frontend %s = %v, but backend option group %s enables %v (drifted -- keep vaccine-taxonomy.ts in sync)", c.tsConst, sortedCopy(got), c.group, sortedCopy(want))
		}
	}
}

func extractTSArray(t *testing.T, ts, constName string) []string {
	t.Helper()
	re := regexp.MustCompile(`export const ` + regexp.QuoteMeta(constName) + `\s*=\s*\[([^\]]*)\]`)
	m := re.FindStringSubmatch(ts)
	if m == nil {
		t.Fatalf("could not find export const %s in vaccine-taxonomy.ts", constName)
	}
	strRe := regexp.MustCompile(`"([^"]*)"`)
	var out []string
	for _, sm := range strRe.FindAllStringSubmatch(m[1], -1) {
		out = append(out, sm[1])
	}
	return out
}

func equalStringSets(a, b []string) bool {
	sa, sb := map[string]bool{}, map[string]bool{}
	for _, v := range a {
		sa[v] = true
	}
	for _, v := range b {
		sb[v] = true
	}
	if len(sa) != len(sb) {
		return false
	}
	for k := range sa {
		if !sb[k] {
			return false
		}
	}
	return true
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
