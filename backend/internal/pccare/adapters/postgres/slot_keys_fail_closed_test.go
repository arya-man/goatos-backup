package postgres

import (
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
)

// A task's compulsory capture keys reach the readiness predicate as
// `an.sop_proofs ?& t.required_slot_keys`, and `?&` against an EMPTY array is vacuously TRUE.
// An empty answer from the resolver therefore does not read downstream as "nothing to prove" --
// it reads as "every animal is complete", and the submit would accept a task on which nobody
// filmed anything. UNRESOLVED and NOTHING-REQUIRED must never be spelled the same way, which is
// why the resolver refuses instead of returning the empty list.
//
// The Work Board closed the same hole on the reading side (cardinality(...) > 0 before the `?&`);
// this closes it on the writing side, at create as well as at submit, so the unprovable row is
// never stored in the first place.
func TestAnUnresolvableCardRefusesInsteadOfPassingEveryAnimal(t *testing.T) {
	// A category the seeded rules do not know. Today the pc_care_tasks CHECK constraint keeps one
	// out of the table; the refusal is what keeps the hole shut on the day a sixth category is
	// added to that constraint and its seeded card is forgotten.
	for _, requiredOnly := range []bool{true, false} {
		if keys, err := slotKeysOrSeeded("lice_treatment", nil, requiredOnly); err == nil {
			t.Fatalf("requiredOnly=%v: resolved %v with no error; an unresolvable card must refuse, because `?& '{}'` passes every animal", requiredOnly, keys)
		} else if !errors.Is(err, domain.ErrProofRulesUnresolved) {
			t.Fatalf("requiredOnly=%v: err = %v, want ErrProofRulesUnresolved", requiredOnly, err)
		}
	}

	// A caller that DOES hand the store a card is answered from it, untouched.
	keys, err := slotKeysOrSeeded("lice_treatment", []string{"dip_video"}, true)
	if err != nil || len(keys) != 1 || keys[0] != "dip_video" {
		t.Fatalf("an authored card must be used verbatim: %v, %v", keys, err)
	}
}

// TestEveryPlannableCategoryStatesACompulsoryCapture is the PREVENTION half, and it is the half
// that actually stops the regression: a category that can be planned but whose seeded card names
// no compulsory capture cannot be proven by anything, so it fails the build here rather than
// refusing in an operator's hands at the end of a day's work.
func TestEveryPlannableCategoryStatesACompulsoryCapture(t *testing.T) {
	categories := append([]string{}, domain.PlannerCategories...)
	categories = append(categories, domain.CategoryFeedWaterRemoval, domain.CategoryInventoryVaccine)
	for _, category := range categories {
		keys, err := slotKeysOrSeeded(category, nil, true)
		if err != nil {
			t.Errorf("%s: %v -- every plannable category needs a seeded compulsory capture", category, err)
			continue
		}
		if len(keys) == 0 {
			t.Errorf("%s: resolved no compulsory capture", category)
		}
	}
}
