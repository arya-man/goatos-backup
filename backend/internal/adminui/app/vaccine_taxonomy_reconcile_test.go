package app

import (
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
