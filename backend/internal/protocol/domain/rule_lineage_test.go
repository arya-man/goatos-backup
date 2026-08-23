package domain

import "testing"

func baseRule() NewRule {
	return NewRule{
		DoseCode:            "ppr_primary",
		Sequence:            1,
		TriggerType:         "birth_age",
		OffsetDays:          112,
		DueWindowDays:       7,
		MinGapDays:          14,
		Repeat:              "yearly",
		RepeatUntilAfterAge: "",
		CatchUp:             "immediate",
		EligibilityJSON:     []byte(`{"vaccine":{"code":"PPR"},"eligibility":{"sex":["male","female"]}}`),
		ProofPolicy:         []byte(`{"mode":"per_goat"}`),
	}
}

func TestRuleIdentityKeyIgnoresCaseAndSpaceButNotTheVaccine(t *testing.T) {
	same := RuleIdentityKey("  PPR ", "PPR_Primary", 1)
	if got := RuleIdentityKey("ppr", "ppr_primary", 1); got != same {
		t.Fatalf("identity key is case/space sensitive: %q vs %q", got, same)
	}
	if RuleIdentityKey("fmd", "ppr_primary", 1) == same {
		t.Fatalf("two vaccines share one identity key")
	}
	if RuleIdentityKey("ppr", "ppr_primary", 2) == same {
		t.Fatalf("two sequences share one identity key")
	}
}

// A dose code containing the separator must not be able to impersonate another rule.
func TestRuleIdentityKeySeparatorCannotBeForged(t *testing.T) {
	forged := RuleIdentityKey("ppr", "primary\x001", 1)
	real := RuleIdentityKey("ppr", "primary", 1)
	if forged == real {
		t.Fatalf("a dose code carrying the separator collided with another rule")
	}
}

func TestRuleContentFingerprintIsStableAcrossRepublish(t *testing.T) {
	a := RuleContentFingerprint(baseRule())
	b := RuleContentFingerprint(baseRule())
	if a == "" || a != b {
		t.Fatalf("fingerprint not stable: %q vs %q", a, b)
	}
}

// Guardrail 1 in additive-publish.md: republishing an identical plan must be a no-op, and
// reordering the vaccine list is not a content change.
func TestRuleContentFingerprintIgnoresSortOrderAndKeyOrder(t *testing.T) {
	want := RuleContentFingerprint(baseRule())

	reordered := baseRule()
	reordered.SortOrder = 97
	if got := RuleContentFingerprint(reordered); got != want {
		t.Fatalf("sort_order changed the fingerprint; reordering the list would reschedule animals")
	}

	shuffled := baseRule()
	shuffled.EligibilityJSON = []byte(`{"eligibility":{"sex":["male","female"]},"vaccine":{"code":"PPR"}}`)
	if got := RuleContentFingerprint(shuffled); got != want {
		t.Fatalf("JSON key order changed the fingerprint")
	}

	spaced := baseRule()
	spaced.EligibilityJSON = []byte("{\n  \"vaccine\": { \"code\": \"PPR\" },\n  \"eligibility\": { \"sex\": [\"male\", \"female\"] }\n}")
	if got := RuleContentFingerprint(spaced); got != want {
		t.Fatalf("whitespace changed the fingerprint")
	}
}

// Guardrail 2: every field that can move a due date must move the fingerprint, or an edit would
// silently carry over as if nothing had changed.
func TestRuleContentFingerprintCoversEverySchedulingField(t *testing.T) {
	want := RuleContentFingerprint(baseRule())
	sop := "sop-9"
	withdrawal := int32(21)

	for name, mutate := range map[string]func(*NewRule){
		"trigger_type":           func(r *NewRule) { r.TriggerType = "post_arrival" },
		"offset_days":            func(r *NewRule) { r.OffsetDays = 113 },
		"due_window_days":        func(r *NewRule) { r.DueWindowDays = 8 },
		"min_gap_days":           func(r *NewRule) { r.MinGapDays = 15 },
		"repeat":                 func(r *NewRule) { r.Repeat = "every_n_days" },
		"repeat_until_after_age": func(r *NewRule) { r.RepeatUntilAfterAge = "adult" },
		"catch_up":               func(r *NewRule) { r.CatchUp = "next_cycle" },
		"sop_version_id":         func(r *NewRule) { r.SopVersionID = &sop },
		"withdrawal_days":        func(r *NewRule) { r.WithdrawalDays = &withdrawal },
		"eligibility_json":       func(r *NewRule) { r.EligibilityJSON = []byte(`{"vaccine":{"code":"PPR"},"eligibility":{"sex":["female"]}}`) },
		"proof_policy":           func(r *NewRule) { r.ProofPolicy = []byte(`{"mode":"per_shed"}`) },
	} {
		rule := baseRule()
		mutate(&rule)
		if got := RuleContentFingerprint(rule); got == want {
			t.Fatalf("changing %s left the fingerprint unchanged: an edit would carry over as unchanged", name)
		}
	}
}

// The revaccination interval is the edit the maintainer named explicitly.
func TestRuleContentFingerprintChangesWhenRevaccinationIntervalChanges(t *testing.T) {
	before := baseRule()
	before.Repeat = "every_n_days"
	before.MinGapDays = 180

	after := before
	after.MinGapDays = 274

	if RuleContentFingerprint(before) == RuleContentFingerprint(after) {
		t.Fatalf("changing the revaccination interval did not change the fingerprint")
	}
}

// Guardrail 5: unparseable content yields a blank fingerprint, which never matches, so the rule
// falls back to cancel-and-regenerate rather than carrying over unverified content.
func TestRuleContentFingerprintFailsSafeOnUnparseableJSON(t *testing.T) {
	broken := baseRule()
	broken.EligibilityJSON = []byte(`{"vaccine":`)
	if got := RuleContentFingerprint(broken); got != "" {
		t.Fatalf("fingerprint = %q, want empty so the rule cannot pair", got)
	}
}

// An omitted document and an explicit null are the same absence.
func TestRuleContentFingerprintTreatsAbsentAndNullAlike(t *testing.T) {
	absent := baseRule()
	absent.ProofPolicy = nil

	explicit := baseRule()
	explicit.ProofPolicy = []byte("null")

	if RuleContentFingerprint(absent) != RuleContentFingerprint(explicit) {
		t.Fatalf("absent and null proof policies produced different fingerprints")
	}
}

func TestVaccineCodeForRuleReadsTheSameFieldGenerationReads(t *testing.T) {
	if got := VaccineCodeForRule([]byte(`{"vaccine":{"code":" FMD "}}`)); got != "FMD" {
		t.Fatalf("vaccine code = %q, want FMD", got)
	}
	for _, raw := range []string{"", "null", "{}", `{"vaccine":{}}`, `{"vaccine":`} {
		if got := VaccineCodeForRule([]byte(raw)); got != "" {
			t.Fatalf("vaccine code for %q = %q, want empty", raw, got)
		}
	}
}
