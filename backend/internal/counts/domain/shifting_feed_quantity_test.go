package domain

import "testing"

// Realme E2E 2026-09-17: a high-priority shifting card read "0.0000 g" -- the Postgres numeric text
// passed straight through to the operator. The exact decimal stays exact; only the padding goes.
func TestShiftingFeedGramsDisplayDropsNumericPadding(t *testing.T) {
	cases := map[string]string{
		"0.0000":   "0",
		"400.0000": "400",
		"250.5000": "250.5",
		"12.3450":  "12.345",
		"1200":     "1200",
		"100.0":    "100",
		" 7.2500 ": "7.25",
		"":         "",
	}
	for in, want := range cases {
		if got := ShiftingFeedGramsDisplay(in); got != want {
			t.Errorf("ShiftingFeedGramsDisplay(%q) = %q, want %q", in, got, want)
		}
	}
}
