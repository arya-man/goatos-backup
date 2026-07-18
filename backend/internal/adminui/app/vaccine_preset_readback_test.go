package app

import (
	"strings"
	"testing"

	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
)

// parsePresetMeta turns the pipe-delimited preset Title
// ("vaccine_type=killed|pathogen_class=bacterial|...") into a lookup map.
func parsePresetMeta(title string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(title, "|") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return out
}

func sourceVaccineMatrixPresets(t *testing.T) map[string]map[string]string {
	t.Helper()
	presets := map[string]map[string]string{}
	for _, group := range configOptionGroups() {
		if group.ID != "source_vaccine_matrix_presets" {
			continue
		}
		for _, opt := range group.Options {
			presets[opt.Key] = parsePresetMeta(opt.Title)
		}
	}
	if len(presets) == 0 {
		t.Fatal("source_vaccine_matrix_presets group not found in config option groups")
	}
	return presets
}

// TestETTTConfigPresetIsKilledBacterialBooster proves the Config UI preset the
// operator loads for ET+TT reads back as killed / bacterial / booster — matching
// the wiki source and the backend seed. Booster is the course type, not the
// vaccine type.
func TestETTTConfigPresetIsKilledBacterialBooster(t *testing.T) {
	presets := sourceVaccineMatrixPresets(t)
	et, ok := presets["ET+TT"]
	if !ok {
		t.Fatal("ET+TT preset missing from source_vaccine_matrix_presets")
	}
	if got := et["vaccine_type"]; got != "killed" {
		t.Errorf("ET+TT preset vaccine_type = %q, want killed", got)
	}
	if got := et["pathogen_class"]; got != "bacterial" {
		t.Errorf("ET+TT preset pathogen_class = %q, want bacterial", got)
	}
	if got := et["course_type"]; got != "booster" {
		t.Errorf("ET+TT preset course_type = %q, want booster", got)
	}
}

// TestNoConfigPresetPathogenClassIsATypeValue proves no source preset ships a
// vaccine-type value inside pathogen_class — the same invariant publish enforces.
func TestNoConfigPresetPathogenClassIsATypeValue(t *testing.T) {
	forbidden := map[string]struct{}{"live": {}, "killed": {}, "toxoid": {}, "combo": {}}
	for name, meta := range sourceVaccineMatrixPresets(t) {
		pc := strings.ToLower(meta["pathogen_class"])
		if _, bad := forbidden[pc]; bad {
			t.Errorf("preset %s pathogen_class = %q is a vaccine-type value", name, meta["pathogen_class"])
		}
	}
}

func TestSourceVaccineMatrixPresetsCarryValidTaxonomy(t *testing.T) {
	for name, meta := range sourceVaccineMatrixPresets(t) {
		if !protocolapp.IsValidVaccineType(meta["vaccine_type"]) {
			t.Errorf("preset %s vaccine_type = %q is missing or invalid", name, meta["vaccine_type"])
		}
		if !protocolapp.IsValidPathogenClass(meta["pathogen_class"]) {
			t.Errorf("preset %s pathogen_class = %q is missing or invalid", name, meta["pathogen_class"])
		}
		if !protocolapp.IsValidCourseType(meta["course_type"]) {
			t.Errorf("preset %s course_type = %q is missing or invalid", name, meta["course_type"])
		}
	}
}
