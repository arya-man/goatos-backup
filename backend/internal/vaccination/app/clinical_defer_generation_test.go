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

// C35-010 counter-review repair: under a CANONICAL rule (lifecycle=alive,
// health=healthy), a goat whose clinical state is carried on lifecycle_status
// (not health_status) must still be DEFERRED, not excluded by the lifecycle
// selector before the defer logic runs. True exit states stay excluded.
func TestGoatMatchesEligibilityDefersClinicalLifecycleUnderCanonicalRule(t *testing.T) {
	// Canonical health/lifecycle targeting; other structural selectors left empty
	// (match-all) so this isolates the lifecycle-vs-clinical-defer interaction.
	canonical := genEligibility{
		Lifecycle:   genStringList{"alive"},
		Health:      genStringList{"healthy"},
		DeferStates: []string{"icu", "quarantine"}, // partial authored; union enforces the rest
	}
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)

	// Every clinical state carried on lifecycle_status must be deferred (match).
	for _, clinical := range []string{"sick", "under_treatment", "quarantine", "icu"} {
		g := domain.EligibleGoat{GoatID: "g-" + clinical, LifecycleStatus: clinical, HealthStatus: "healthy"}
		if !goatMatchesEligibility(g, canonical, genPregnancyPolicy{}, asOf) {
			t.Fatalf("lifecycle_status=%q under canonical lifecycle=alive rule was excluded; must be deferred", clinical)
		}
		if reason := deferredReason(g, canonical.DeferStates); reason == "" {
			t.Fatalf("lifecycle_status=%q produced no defer reason", clinical)
		}
	}

	// A healthy alive goat matches normally (will be scheduled, not deferred).
	alive := domain.EligibleGoat{GoatID: "g-alive", LifecycleStatus: "alive", HealthStatus: "healthy"}
	if !goatMatchesEligibility(alive, canonical, genPregnancyPolicy{}, asOf) {
		t.Fatalf("healthy alive goat was excluded under canonical rule")
	}
	if deferredReason(alive, canonical.DeferStates) != "" {
		t.Fatalf("healthy alive goat should not be deferred")
	}

	// True exit states remain EXCLUDED even with a stale clinical health signal.
	for _, exit := range []string{"dead", "sold", "culled", "transferred", "lost", "merged", "inactive"} {
		g := domain.EligibleGoat{GoatID: "g-" + exit, LifecycleStatus: exit, HealthStatus: "sick"}
		if goatMatchesEligibility(g, canonical, genPregnancyPolicy{}, asOf) {
			t.Fatalf("exit state lifecycle_status=%q with stale health=sick was INCLUDED; must be excluded", exit)
		}
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

// Finding #4 regression: inCare is the prefilter used by activeGenerationGoats
// and the single-goat short-circuit (GenerateForGoat). It MUST be case/
// whitespace-insensitive so a legacy/imported row carrying "ICU",
// "Quarantine", or surrounding whitespace on lifecycle_status is not dropped
// BEFORE the mandatory clinical defer path runs. Schema constraints are NOT
// VALID, so such non-canonical rows can exist. If inCare excluded them, the
// generator would create NO deferred obligation, violating the non-negotiable
// clinical defer rule.
func TestInCareIsCaseAndWhitespaceInsensitive(t *testing.T) {
	// Non-canonical clinical states (uppercase / mixed-case / whitespace) must
	// stay IN care so downstream defer logic can hold their open work.
	kept := []string{
		"ICU", "Quarantine", "SICK", "Under_Treatment",
		" icu ", "  quarantine", "Icu\t", " alive ", "ALIVE",
	}
	for _, status := range kept {
		if !inCare(status) {
			t.Fatalf("inCare(%q) = false; a clinical/alive row must not be dropped before the defer path", status)
		}
	}

	// True exit states remain excluded regardless of case/whitespace.
	dropped := []string{"DEAD", " sold ", "Culled", "transferred", "LOST", "Merged", "inactive", "", "  "}
	for _, status := range dropped {
		if inCare(status) {
			t.Fatalf("inCare(%q) = true; exit/empty state must be excluded", status)
		}
	}
}

// Finding #4 end-to-end: a goat whose lifecycle_status is a non-canonical
// clinical token ("ICU" uppercase, or whitespace/mixed-case) must survive the
// inCare prefilter AND still resolve to a deferred reason under a canonical
// rule, i.e. reach deferredReason rather than being silently excluded.
func TestNonCanonicalClinicalLifecycleReachesDeferPath(t *testing.T) {
	canonical := genEligibility{
		Lifecycle:   genStringList{"alive"},
		Health:      genStringList{"healthy"},
		DeferStates: []string{"icu", "quarantine"}, // partial; union enforces the rest
	}
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)

	for _, status := range []string{"ICU", " ICU ", "Quarantine", "SICK", " under_treatment "} {
		if !inCare(status) {
			t.Fatalf("inCare(%q) dropped the row before the defer path could run", status)
		}
		g := domain.EligibleGoat{GoatID: "g", LifecycleStatus: status, HealthStatus: "healthy"}
		if !goatMatchesEligibility(g, canonical, genPregnancyPolicy{}, asOf) {
			t.Fatalf("lifecycle_status=%q was excluded under canonical rule; must be deferred", status)
		}
		if reason := deferredReason(g, canonical.DeferStates); reason == "" {
			t.Fatalf("lifecycle_status=%q produced no defer reason; mandatory clinical hold violated", status)
		}
	}
}
