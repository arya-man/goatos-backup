package domain

import "testing"

// TestControlTowerDoseLabel proves the Control Tower vaccine label is composed
// in Go (no ceo_ai.vaccine_label_for SQL dependency) and preserves the antigen
// label plus the course/dose/booster qualifiers the SQL used to emit, while
// never leaking a raw dose code to the UI.
func TestControlTowerDoseLabel(t *testing.T) {
	t.Parallel()

	const matrix = "Preventive Care Vaccination Matrix"
	tests := []struct {
		name         string
		protocolName string
		doseCode     string
		want         string
	}{
		{name: "adult wave gets antigen + course + dose", protocolName: matrix, doseCode: "et_tt_adult_w2", want: "ET+TT adult course dose 2"},
		{name: "kid wave gets antigen + course + dose", protocolName: matrix, doseCode: "ppr_kid_w1", want: "PPR kid course dose 1"},
		// pr294 L-A7: the two ET+TT kid doses are told apart by the age they are given at.
		{name: "kid age dose 4w names its age", protocolName: matrix, doseCode: "et_tt_kid_4w", want: "ET+TT kid course at 4 weeks"},
		{name: "kid age dose 7w names its age", protocolName: matrix, doseCode: "et_tt_kid_7w", want: "ET+TT kid course at 7 weeks"},
		{name: "booster gets booster tag", protocolName: matrix, doseCode: "ppr_booster", want: "PPR · Booster"},
		{name: "goat pox adult wave", protocolName: matrix, doseCode: "goat_pox_adult_w1", want: "Goat Pox adult course dose 1"},
		{name: "plain antigen", protocolName: matrix, doseCode: "hs", want: "HS"},
		{name: "empty dose falls back to protocol label", protocolName: matrix, doseCode: "", want: "Vaccination"},
		{name: "unknown dose keeps protocol context, never raw underscores", protocolName: "Rabies", doseCode: "d1", want: "Rabies · d1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ControlTowerDoseLabel(tt.protocolName, tt.doseCode); got != tt.want {
				t.Fatalf("ControlTowerDoseLabel(%q, %q) = %q, want %q", tt.protocolName, tt.doseCode, got, tt.want)
			}
		})
	}
}
