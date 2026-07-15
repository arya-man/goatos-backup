package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestGetRuleVaccineIdentityFallsBackWhenCachedCodeBlank guards BUG #2: a cached RuleVaccineIDs
// entry that carries a BLANK vaccine code (what ExtractRuleVaccineIdentity returns for a
// non-matrix published rule whose eligibility JSON has no `vaccine` object) must NOT shadow the
// version-level fallback. getRuleVaccineIdentity must treat the blank cached entry exactly like a
// missing key and return the version-level identity (cfg.VaccineCode + its normalized priority).
//
// FAILING-FIRST: before the fix the cache-hit branch was `if id, ok := cfg.RuleVaccineIDs[ruleID];
// ok`, so a present-but-blank entry returned VaccineCode "" at priority 0, masking cross-vaccine
// ties and losing real priority/session behavior for every non-matrix sweep.
func TestGetRuleVaccineIdentityFallsBackWhenCachedCodeBlank(t *testing.T) {
	planner := domain.DrivePlannerSettings{Enabled: true}
	cfg := SweepConfig{
		VaccineCode:  "PPR",
		DrivePlanner: planner,
		RuleVaccineIDs: map[string]RuleVaccineIdentity{
			// Non-matrix rule cached with a zero-value identity (blank code, priority 0).
			"rule-blank": {},
			// A blank code with surrounding whitespace must also be treated as missing.
			"rule-ws": {VaccineCode: "   "},
			// A genuine matrix rule keeps its own identity.
			"rule-matrix": {VaccineCode: "FMD", VaccinePriority: 5},
		},
	}

	wantPriority := normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode).VaccinePriority

	t.Run("blank_cached_code_falls_back", func(t *testing.T) {
		got := cfg.getRuleVaccineIdentity("rule-blank")
		if got.VaccineCode != "PPR" {
			t.Fatalf("VaccineCode = %q, want version-level %q", got.VaccineCode, "PPR")
		}
		if got.VaccinePriority != wantPriority {
			t.Fatalf("VaccinePriority = %d, want version-level %d", got.VaccinePriority, wantPriority)
		}
	})

	t.Run("whitespace_cached_code_falls_back", func(t *testing.T) {
		got := cfg.getRuleVaccineIdentity("rule-ws")
		if got.VaccineCode != "PPR" {
			t.Fatalf("VaccineCode = %q, want version-level %q", got.VaccineCode, "PPR")
		}
		if got.VaccinePriority != wantPriority {
			t.Fatalf("VaccinePriority = %d, want version-level %d", got.VaccinePriority, wantPriority)
		}
	})

	t.Run("missing_key_falls_back", func(t *testing.T) {
		got := cfg.getRuleVaccineIdentity("rule-absent")
		if got.VaccineCode != "PPR" {
			t.Fatalf("VaccineCode = %q, want version-level %q", got.VaccineCode, "PPR")
		}
	})

	t.Run("matrix_rule_keeps_cached_identity", func(t *testing.T) {
		got := cfg.getRuleVaccineIdentity("rule-matrix")
		if got.VaccineCode != "FMD" {
			t.Fatalf("VaccineCode = %q, want cached %q", got.VaccineCode, "FMD")
		}
		if got.VaccinePriority != 5 {
			t.Fatalf("VaccinePriority = %d, want cached 5", got.VaccinePriority)
		}
	})
}

// stubRule is a minimal rule type standing in for protocol/domain.Rule for BuildRuleVaccineIdentities.
type stubRule struct {
	id   string
	elig []byte
}

// TestBuildRuleVaccineIdentities guards BUG #3 / the shared helper: matrix rules (eligibility JSON
// with a `vaccine` object) are cached with their real code/priority, while non-matrix rules (no
// vaccine object -> blank extracted code) are SKIPPED so the version-level fallback applies.
func TestBuildRuleVaccineIdentities(t *testing.T) {
	rules := []stubRule{
		{id: "rule-fmd", elig: []byte(`{"vaccine":{"code":"FMD","compatibility_group":"grp-1"}}`)},
		{id: "rule-pprws", elig: []byte(`{"vaccine":{"code":"  PPR  "}}`)},
		{id: "rule-nonmatrix", elig: []byte(`{"some_other":"thing"}`)},
		{id: "rule-empty", elig: nil},
		{id: "rule-blankcode", elig: []byte(`{"vaccine":{"code":"  "}}`)},
	}

	got := BuildRuleVaccineIdentities(
		rules,
		func(r stubRule) string { return r.id },
		func(r stubRule) []byte { return r.elig },
	)

	if _, ok := got["rule-nonmatrix"]; ok {
		t.Fatalf("non-matrix rule was cached, want skipped: %+v", got["rule-nonmatrix"])
	}
	if _, ok := got["rule-empty"]; ok {
		t.Fatalf("empty-eligibility rule was cached, want skipped")
	}
	if _, ok := got["rule-blankcode"]; ok {
		t.Fatalf("blank-code rule was cached, want skipped")
	}

	fmd, ok := got["rule-fmd"]
	if !ok {
		t.Fatalf("matrix rule rule-fmd missing from cache")
	}
	if fmd.VaccineCode != "FMD" {
		t.Fatalf("rule-fmd VaccineCode = %q, want FMD", fmd.VaccineCode)
	}
	if fmd.VaccinePriority != VaccineMatrixPriority("FMD") {
		t.Fatalf("rule-fmd VaccinePriority = %d, want %d", fmd.VaccinePriority, VaccineMatrixPriority("FMD"))
	}
	if fmd.CompatibilityGrp != "grp-1" {
		t.Fatalf("rule-fmd CompatibilityGrp = %q, want grp-1", fmd.CompatibilityGrp)
	}

	ppr, ok := got["rule-pprws"]
	if !ok {
		t.Fatalf("matrix rule rule-pprws missing from cache")
	}
	if ppr.VaccineCode != "PPR" {
		t.Fatalf("rule-pprws VaccineCode = %q, want trimmed PPR", ppr.VaccineCode)
	}
}
