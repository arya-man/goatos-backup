package domain

import "testing"

func TestVaccinesShareApprovedComboHandlesMultiSessionMembership(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    string
		b    string
	}{
		{name: "ET+TT and PPR", a: "ET+TT", b: "PPR"},
		{name: "PPR and Blue Tongue", a: "PPR", b: "BLUE_TONGUE"},
		{name: "PPR and FMD", a: "PPR", b: "FMD"},
		{name: "PPR and HS", a: "PPR", b: "HS"},
		{name: "FMD and HS", a: "FMD", b: "HS"},
		{name: "Sheep Pox and Blue Tongue", a: "Sheep Pox", b: "Blue Tongue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !VaccinesShareApprovedCombo(tc.a, tc.b) {
				t.Fatalf("VaccinesShareApprovedCombo(%q, %q) = false, want true", tc.a, tc.b)
			}
			if !VaccinesShareApprovedCombo(tc.b, tc.a) {
				t.Fatalf("VaccinesShareApprovedCombo(%q, %q) = false, want symmetric true", tc.b, tc.a)
			}
		})
	}
}

func TestVaccinesShareApprovedComboRejectsUnrelatedLivePair(t *testing.T) {
	if VaccinesShareApprovedCombo("PPR", "Sheep Pox") {
		t.Fatal("PPR and Sheep Pox must not be same-day compatible")
	}
}
