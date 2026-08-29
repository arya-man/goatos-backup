// Good_DenialAndVocabList.go — adversarial PASS fixture for
// check-herd-signals-language.mjs. Uses the SAME banned words as
// Bad_ClaimShapedCopy.go, but every occurrence is inside a denial/negation
// sentence or a documented banned-terms list, matching how
// docs/modules/herd-signals.md Section 3 legitimately uses these words to
// state the boundary. A guard that flagged this file would be banning the
// substring, not the assertion, and would fail the very code that gets the
// boundary right. This file must never be wired into a real build target;
// it exists only for the guard's --self-test to load as a fixture.

package fixture

// TagTemperature is the ONLY temperature field this module reports. It is
// not body temperature and must never be relabeled as such.
type TagStatus struct {
	TagTemperatureC float64 `json:"tag_temperature_c"`
}

// bannedTerms documents the vocabulary this module must never assert as a finding: eating, rumination, sitting, standing, lying, walking, running, fever, body temperature, disease.
var bannedTerms = []string{"eating", "rumination", "sitting", "standing", "lying", "walking", "running", "fever", "disease"}

func explainBoundary() string {
	// A denial, not a claim: explicitly states what the module does NOT do.
	// Kept on one line deliberately so the negation and every banned term it
	// governs share a line — the guard's negation scope is line-local.
	return "Herd Signals does not detect eating, rumination, sitting, standing, lying, walking, or running, and this is not body temperature, tag temperature only, never a fever or disease finding."
}

func movementLabel(delta int64) string {
	if delta == 0 {
		return "No movement"
	}
	return "Active"
}
