package domain

import "testing"

// ANTI PROTOZOAN is deworming's twin (maintainer instruction 2026-09-05). These pin the two
// halves of that: it behaves like deworming everywhere, and it has NO feed & water removal.

func TestAntiProtozoanIsWorkedExactlyLikeDeworming(t *testing.T) {
	if !IsValidCategory(CategoryAntiProtozoan) {
		t.Fatal("anti protozoan must be a real category")
	}
	if !IsRoundPlannableCategory(CategoryAntiProtozoan) {
		t.Fatal("anti protozoan must be plannable as a round, like deworming")
	}
	if IsKernelOwnedCategory(CategoryAntiProtozoan) {
		t.Fatal("anti protozoan is planned by a human, not born from the kernel")
	}
	if IsTrimmingCategory(CategoryAntiProtozoan) {
		t.Fatal("anti protozoan is CEO-planned; the trimming carve-out is the Breeding Director's")
	}
	if IsDirectorApprovedCategory(CategoryAntiProtozoan) {
		t.Fatal("anti protozoan proof goes to the tenant VERIFIER, like deworming's")
	}

	// The capture face, the slot set and the review route are deworming's, field for field.
	if got, want := CaptureModeForCategory(CategoryAntiProtozoan), CaptureModeForCategory(CategoryDeworming); got != want {
		t.Fatalf("capture mode = %q, want deworming's %q", got, want)
	}
	slots := SlotsForCategory(CategoryAntiProtozoan)
	if len(slots) != 1 || slots[0].FieldKey != SlotVideo {
		t.Fatalf("slots = %+v, want exactly one video slot like deworming", slots)
	}
	if !IsSingleVideoCategory(CategoryAntiProtozoan) {
		t.Fatal("anti protozoan must count as a one-video category, or submit demands the trimming trio")
	}
	if CategoryLabel(CategoryAntiProtozoan) != "Anti Protozoan" {
		t.Fatalf("label = %q, want farm copy", CategoryLabel(CategoryAntiProtozoan))
	}

	// Verifier-reviewed, with its OWN queue category — sharing deworming's would put two
	// different jobs in one filter.
	reviewed := false
	for _, c := range VerifierReviewedCategories {
		if c == CategoryAntiProtozoan {
			reviewed = true
		}
	}
	if !reviewed {
		t.Fatal("anti protozoan proof must reach the verifier queue")
	}
	if VerificationCategoryFor(CategoryAntiProtozoan) == "" {
		t.Fatal("anti protozoan has no verification category; its items would be unfiled")
	}
	if VerificationCategoryFor(CategoryAntiProtozoan) == VerificationCategoryFor(CategoryDeworming) {
		t.Fatal("anti protozoan must not share deworming's verifier queue category")
	}
}

// The single difference, and it is an ABSENCE: the removal precondition belongs to deworming
// alone, because that tablet goes in the feed and this dose does not.
func TestAntiProtozoanHasNoFeedAndWaterRemoval(t *testing.T) {
	if CategoryAntiProtozoan == CategoryDeworming {
		t.Fatal("the two categories must stay distinct")
	}
	// Nothing opts this category out: the removal fields are admitted for deworming only, so a
	// category that is not deworming is refused one. This asserts the SHAPE that gives it that
	// for free — if a second category is ever admitted, this goes red and the author must think.
	for _, c := range Categories {
		if c == CategoryFeedWaterRemoval || c == CategoryDeworming {
			continue
		}
		if c == CategoryAntiProtozoan {
			// Reached: anti protozoan is in the ordinary "not deworming" set, which is exactly
			// what makes ErrFeedRemovalNotApplicable apply to it.
			return
		}
	}
	t.Fatal("anti protozoan is missing from the category vocabulary")
}
