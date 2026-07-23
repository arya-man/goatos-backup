package domain

import "testing"

func TestVaccinationDoseDisplayLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		protocolName string
		doseCode     string
		want         string
	}{
		{name: "kid first dose keeps timing internal", protocolName: "Preventive Care Vaccination Matrix", doseCode: "ET_TT_4W", want: "ET+TT"},
		{name: "kid booster keeps timing internal", protocolName: "Preventive Care Vaccination Matrix", doseCode: "ET_TT_7W", want: "ET+TT"},
		{name: "renamed three week booster keeps timing internal", protocolName: "Preventive Care Vaccination Matrix", doseCode: "ET_TT_3W", want: "ET+TT"},
		{name: "generic booster keeps sequence internal", protocolName: "Preventive Care Vaccination Matrix", doseCode: "PPR_BOOSTER", want: "PPR"},
		{name: "adult matrix wave does not leak protocol family or wave", protocolName: "Preventive Care Vaccination Matrix", doseCode: "et_tt_adult_w2", want: "ET+TT"},
		{name: "goat pox matrix wave does not leak wave", protocolName: "Preventive Care Vaccination Matrix", doseCode: "goat_pox_adult_w1", want: "Goat Pox"},
		{name: "matrix protocol alone is not exposed as UI copy", protocolName: "Preventive Care Vaccination Matrix", doseCode: "", want: "Vaccination"},
		{name: "unknown dose keeps backend supplied context", protocolName: "Rabies", doseCode: "D1", want: "Rabies · D1"},
		{name: "protocol only", protocolName: "Rabies", doseCode: "", want: "Rabies"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := VaccinationDoseDisplayLabel(tt.protocolName, tt.doseCode); got != tt.want {
				t.Fatalf("VaccinationDoseDisplayLabel(%q, %q) = %q, want %q", tt.protocolName, tt.doseCode, got, tt.want)
			}
		})
	}
}
