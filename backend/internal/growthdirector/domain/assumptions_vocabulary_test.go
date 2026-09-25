package domain

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
)

// OPEN UP TO NEW SPECIES (maintainer decision 2026-09-25): a sale price may be set for a species
// and gender the farm added on Configuration, and one it never added is refused.
func TestSalePriceAcceptsTheFarmsConfiguredSpeciesAndGenders(t *testing.T) {
	vocab := animalvocab.Vocabulary{
		Species: []animalvocab.Entry{{Code: "goat"}, {Code: "sheep"}, {Code: "alpaca"}},
		Sexes:   []animalvocab.Entry{{Code: "female"}, {Code: "male"}, {Code: "castrated"}},
	}
	price := 480.0
	ok := AssumptionsUpdate{SalePrices: []SalePriceUpdate{
		{Species: "alpaca", PricePerKgINR: &price},
		{Species: "alpaca", ManagementStage: "K3", Sex: "castrated", PricePerKgINR: &price},
	}}
	if err := ValidateAssumptionsUpdate(ok, vocab); err != nil {
		t.Fatalf("a configured third species/gender price was refused: %v", err)
	}
	for _, bad := range []SalePriceUpdate{
		{Species: "camel", PricePerKgINR: &price},
		{Species: "goat", ManagementStage: "K3", Sex: "hermaphrodite", PricePerKgINR: &price},
	} {
		err := ValidateAssumptionsUpdate(AssumptionsUpdate{SalePrices: []SalePriceUpdate{bad}}, vocab)
		if err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("%+v: err = %v, want an unknown species/sex refusal", bad, err)
		}
	}
	// The built-ins behave exactly as before.
	if err := ValidateAssumptionsUpdate(AssumptionsUpdate{SalePrices: []SalePriceUpdate{{Species: "alpaca", PricePerKgINR: &price}}}, animalvocab.Builtins()); err == nil {
		t.Fatal("an unconfigured tenant must still refuse a species it does not keep")
	}
}
