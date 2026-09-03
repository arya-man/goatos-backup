package app

import "testing"

// Position TITLES are prettified from position_code, so the pen vocabulary has to be applied
// where the title is built rather than in a copy map. The CODES are untouched: shed_manager and
// shed_backup_manager are the stored contract that grants, duties and roster lookups key on, and
// renaming them would be a data change rather than a copy change.
func TestPositionTitlesSayPenWhileTheCodesStayShed(t *testing.T) {
	for code, want := range map[string]string{
		"shed_manager":        "Pen Manager",
		"shed_backup_manager": "Pen Backup Manager",

		// Untouched neighbours: the substitution is one word, not a new humanisation rule.
		"park_head":         "Park Head",
		"operator":          "Operator",
		"assistant_manager": "Assistant Manager",
	} {
		if got := formatPositionCode(code); got != want {
			t.Errorf("formatPositionCode(%q) = %q, want %q", code, got, want)
		}
	}
}
