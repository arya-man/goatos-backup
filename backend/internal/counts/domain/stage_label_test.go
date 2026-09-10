package domain

import "testing"

// The fattening family reads as words; every other stage keeps its code. That asymmetry IS the
// maintainer's 2026-09-10 decision, so it is asserted in both directions -- a change that starts
// rendering names throughout turns the second half of this test red.
func TestStageDisplayLabelRenamesOnlyTheFatteningFamily(t *testing.T) {
	for _, tc := range []struct{ code, configured, want string }{
		{"F2", "Fattening", "Fattening"},
		{"F2-Male", "Fattening male", "Fattening male"},
		{"F2-Female", "Fattening female", "Fattening female"},
		// The column has no CHECK constraint and the importer writes the source cell verbatim,
		// so casing and stray spacing are real.
		{"f2-male", "Fattening male", "Fattening male"},
		{"  F2  ", "Fattening", "Fattening"},

		// Everything else keeps its code even though the lookup has a name for it. The farm reads,
		// says and writes these BY CODE on its own sheets.
		{"K0", "Newborn", "K0"},
		{"K1", "Milk training", "K1"},
		{"K2", "Milk drinking", "K2"},
		{"K3", "Weaned kids", "K3"},
		{"M0", "Mother newborn", "M0"},
		{"Mother", "Mother", "Mother"},
		{"Buck", "Buck", "Buck"},
		{"Non-Pregnant", "Non-pregnant", "Non-Pregnant"},

		// A prefix match would claim these; whole-code matching does not, because the farm may
		// mean something else entirely by them.
		{"F2X", "Something else", "F2X"},
		{"F2-Trial", "Trial cohort", "F2-Trial"},

		// No configured name falls back to the code rather than to an invented word, and the
		// unrecorded bucket stays empty so the client renders its own copy for it.
		{"F2", "", "F2"},
		{"F2", "   ", "F2"},
		{"", "", ""},
	} {
		if got := StageDisplayLabel(tc.code, tc.configured); got != tc.want {
			t.Errorf("StageDisplayLabel(%q, %q) = %q, want %q", tc.code, tc.configured, got, tc.want)
		}
	}
}
