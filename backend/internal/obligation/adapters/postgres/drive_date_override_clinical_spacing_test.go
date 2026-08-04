package postgres

import (
	"testing"
	"time"
)

func TestVaccinationSpacingRuleAllowsApprovedSameDayCombos(t *testing.T) {
	day := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		a    vaccinationClinicalRule
		b    vaccinationClinicalRule
	}{
		{
			name: "ET+TT and PPR",
			a:    vaccinationClinicalRule{code: "ET+TT", vaccineType: "killed", pathogenClass: "bacterial"},
			b:    vaccinationClinicalRule{code: "PPR", vaccineType: "live", pathogenClass: "viral"},
		},
		{
			name: "PPR and Blue Tongue",
			a:    vaccinationClinicalRule{code: "PPR", vaccineType: "live", pathogenClass: "viral"},
			b:    vaccinationClinicalRule{code: "BLUE_TONGUE", vaccineType: "killed", pathogenClass: "viral"},
		},
		{
			name: "Sheep Pox and Blue Tongue",
			a:    vaccinationClinicalRule{code: "Sheep Pox", vaccineType: "live", pathogenClass: "viral"},
			b:    vaccinationClinicalRule{code: "Blue Tongue", vaccineType: "killed", pathogenClass: "viral"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rule, gap := vaccinationSpacingRule(tc.a, tc.b, day, day); rule != "" || gap != 0 {
				t.Fatalf("vaccinationSpacingRule(%s) = %q/%d, want compatible", tc.name, rule, gap)
			}
		})
	}
}

func TestVaccinationSpacingRuleRejectsUnrelatedSameDayLiveLive(t *testing.T) {
	day := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	rule, gap := vaccinationSpacingRule(
		vaccinationClinicalRule{code: "PPR", vaccineType: "live", pathogenClass: "viral"},
		vaccinationClinicalRule{code: "Sheep Pox", vaccineType: "live", pathogenClass: "viral"},
		day,
		day,
	)
	if rule != "live_live_min_gap" || gap != 28 {
		t.Fatalf("unrelated same-day live/live = %q/%d, want live_live_min_gap/28", rule, gap)
	}
}
