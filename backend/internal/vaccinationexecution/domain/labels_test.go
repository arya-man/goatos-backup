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
		{name: "kid first dose keeps four week timing internal", protocolName: "Preventive Care Vaccination Matrix", doseCode: "ET_TT_4W", want: "ET+TT · First dose"},
		{name: "kid booster explains the dose instead of leaking seven week code", protocolName: "Preventive Care Vaccination Matrix", doseCode: "ET_TT_7W", want: "ET+TT · Booster"},
		{name: "renamed three week booster is equivalent", protocolName: "Preventive Care Vaccination Matrix", doseCode: "ET_TT_3W", want: "ET+TT · Booster"},
		{name: "generic booster is readable", protocolName: "Preventive Care Vaccination Matrix", doseCode: "PPR_BOOSTER", want: "PPR · Booster"},
		{name: "adult matrix wave does not leak protocol family", protocolName: "Preventive Care Vaccination Matrix", doseCode: "et_tt_adult_w2", want: "ET+TT · Dose 2"},
		{name: "goat pox matrix wave is human readable", protocolName: "Preventive Care Vaccination Matrix", doseCode: "goat_pox_adult_w1", want: "Goat Pox · Dose 1"},
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
