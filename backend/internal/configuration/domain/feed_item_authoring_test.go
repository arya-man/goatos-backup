package domain

import (
	"errors"
	"testing"
)

// A FEED ITEM IS AUTHORED ON ITEMS & CATEGORIES (5090bf8cb).
//
// Hand-merging registers.go carried the ImpliedOutOfKind flag but not the three feed columns it
// exists for, so the Feed item drawer asked for a Unit and offered no energy, dry matter or
// wastage. A browser run caught it; every Go test stayed green, and the commit's own message says
// why -- they all assert a register the merge had left INTERNALLY CONSISTENT. A column that
// vanished from the register is perfectly consistent with itself, so no fixture built out of the
// register could represent the failure. That is the shape to watch for: a self-referential
// assertion cannot see a deletion.
//
// So this test names the three keys as LITERALS and drives the real validator. The keys are not
// decoration -- `stores_catalogue.go` writes feed_item_catalog.energy_kcal_per_kg,
// dry_matter_factor and wastage_factor straight out of the row's fields, so a key the register
// stops declaring is stripped by ValidateWrite as unknown and the column is written NULL for
// ever after, silently, with the ration maths reading the hole.
func TestFeedItemCarriesTheThreeRationNumbersAndIsNotAskedForAUnit(t *testing.T) {
	items, ok := RegisterByKey(RegItems)
	if !ok {
		t.Fatal("the items register must exist")
	}
	raw := map[string]any{
		"name":               "Maize",
		"category_id":        "cat-feed",
		"energy_kcal_per_kg": 3200,
		"dry_matter_factor":  0.88,
		"wastage_factor":     0.05,
	}
	clean, err := ValidateWrite(items, raw, nil, "feed")
	if err != nil {
		// Reaching here with a `unit` required error is the exact 5090bf8cb defect: a drawer
		// demanding a field it never shows. Reaching here with an `unknown` error on one of the
		// three numbers is the other half of it.
		t.Fatalf("a feed item must be authorable with its three ration numbers and no unit: %v", err)
	}
	for key, want := range map[string]float64{"energy_kcal_per_kg": 3200, "dry_matter_factor": 0.88, "wastage_factor": 0.05} {
		got, present := clean[key]
		if !present {
			t.Errorf("%s never reached the write: the register does not declare it, so feed_item_catalog gets NULL and the ration maths reads a hole", key)
			continue
		}
		if f, convErr := toFloat(got); convErr != nil || f != want {
			t.Errorf("%s came through as %v (%T), want the number that was authored", key, got, got)
		}
	}
	if _, asked := clean["unit"]; asked {
		t.Error("a feed item is weighed in kg and has no unit; writing one would put a value in a column the drawer never shows")
	}
}

// The other side of the same flag, so the test above cannot be satisfied by dropping `unit` from
// the register altogether: a medicine still must carry one.
func TestAMedicineIsStillRequiredToCarryItsUnit(t *testing.T) {
	items, _ := RegisterByKey(RegItems)
	_, err := ValidateWrite(items, map[string]any{"name": "Oxytet", "category_id": "cat-med"}, nil, "medicine")
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("a medicine with no unit must be refused, got %v", err)
	}
	for _, f := range ve.Fields {
		if f.Field == "unit" && f.Code == "required" {
			return
		}
	}
	t.Fatalf("the refusal must name unit as required, got %+v", ve.Fields)
}

// A feed row in a mixed items sheet naturally repeats the unit every other row carries. That is an
// AGREEMENT, not a conflict, and ImpliedOutOfKind is what makes it dropped rather than refused --
// without it the whole row failed. Pinning it here keeps the flag and the columns it guards from
// drifting apart again, which is how they were merged apart in the first place.
func TestAFeedRowRepeatingTheSheetsUnitIsDroppedNotRefused(t *testing.T) {
	items, _ := RegisterByKey(RegItems)
	clean, err := ValidateWrite(items, map[string]any{"name": "Maize", "category_id": "cat-feed", "unit": "kg"}, nil, "feed")
	if err != nil {
		t.Fatalf("a feed row repeating the sheet's unit must not fail the row: %v", err)
	}
	if _, asked := clean["unit"]; asked {
		t.Error("the repeated unit must be dropped, not written")
	}
}
