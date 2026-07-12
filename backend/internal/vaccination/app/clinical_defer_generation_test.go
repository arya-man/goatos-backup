package app

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// C35-010 regression: a published rule that targets healthy animals with a
// PARTIAL defer_states list (e.g. only ICU + quarantine) must still DEFER a
// sick or under-treatment animal's open work, never cancel/exclude it. Per
// docs/preventive-care-vaccination/vaccination-rules.md the four clinical
// states are mandatory safety blocks, so the generator unions them in
// regardless of what the authored list contains.
func TestDeferStateSetAlwaysCoversMandatoryClinicalStates(t *testing.T) {
	for _, authored := range [][]string{
		nil,
		{"icu"},
		{"icu", "quarantine"},
		{"sick", "quarantine", "ICU"}, // missing under_treatment
	} {
		set := deferStateSet(authored)
		for _, mandatory := range []string{"sick", "under_treatment", "quarantine", "icu"} {
			if !set[mandatory] {
				t.Fatalf("deferStateSet(%v) does not cover mandatory clinical state %q", authored, mandatory)
			}
		}
	}
}

func TestDeferredReasonHoldsSickGoatUnderPartialAuthoring(t *testing.T) {
	sick := domain.EligibleGoat{GoatID: "goat-sick", HealthStatus: "sick", LifecycleStatus: "alive"}
	if reason := deferredReason(sick, []string{"icu", "quarantine"}); reason != "sick" {
		t.Fatalf("sick goat under partial defer_states got reason %q, want %q (must be deferred, not cancelled)", reason, "sick")
	}

	underTreatment := domain.EligibleGoat{GoatID: "goat-ut", HealthStatus: "under_treatment", LifecycleStatus: "alive"}
	if reason := deferredReason(underTreatment, []string{"icu", "quarantine"}); reason != "under_treatment" {
		t.Fatalf("under-treatment goat under partial defer_states got reason %q, want %q", reason, "under_treatment")
	}
}

// The clinical safety hold must survive the full eligibility gate: a healthy-only
// rule with a partial defer list must MATCH a sick goat (so it is scheduled as
// deferred), not drop it out of eligibility (which cancels its open work).
func TestGoatMatchesEligibilityDefersSickUnderHealthyOnlyRule(t *testing.T) {
	// Only Health + DeferStates are set; every other selector is empty, which
	// selectorMatches treats as match-all, so this isolates the health/defer gate.
	elig := genEligibility{
		Health:      genStringList{"healthy"},
		DeferStates: []string{"icu", "quarantine"},
	}
	sick := domain.EligibleGoat{
		GoatID:          "goat-sick",
		LifecycleStatus: "alive",
		HealthStatus:    "sick",
	}
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	if !goatMatchesEligibility(sick, elig, genPregnancyPolicy{}, asOf) {
		t.Fatalf("sick goat under healthy-only rule with partial defer_states was excluded; it must be deferred, not cancelled")
	}
}
