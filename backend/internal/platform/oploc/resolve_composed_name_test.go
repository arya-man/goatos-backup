package oploc

import "testing"

// TestResolveComposedNameNeverDoublesThePen is the regression for the label an operator read on
// the weighing schedule on 2026-09-21: "Mandela 1 - Part 1 - Part 1".
//
// Every case here is a REAL row shape taken from the live register, not an invention:
// the legacy partition-alias locations ("Godel 2 - Part 1" as its own `locations` row) and the
// canonical-shed-plus-catalog-partition shape ("Castro" + "1") both exist in the same park today.
func TestResolveComposedNameNeverDoublesThePen(t *testing.T) {
	for _, tc := range []struct {
		name          string
		shedName      string
		storedLabel   string
		wantParent    string
		wantPartition string
		wantDisplay   string
	}{
		// The reported defect: name already composed, label repeats it.
		{"worded name and matching stored label", "Mandela 1 - Part 1", "Part 1", "Mandela 1", "Part 1", "Mandela 1 - Part 1"},
		{"worded two digit pen", "Mandela 1 - Part 10", "Part 10", "Mandela 1", "Part 10", "Mandela 1 - Part 10"},
		{"worded name with no stored label", "Godel 2 - Part 1", "", "Godel 2", "Part 1", "Godel 2 - Part 1"},

		// The bare-numeric arm the Kotlin guard used to miss entirely.
		{"numeric name and matching stored label", "Castro 1", "1", "Castro", "1", "Castro 1"},

		// The ordinary, correct caller must be untouched: a real physical shed name plus its pen.
		{"physical shed name composes normally", "Mandela 1", "Part 3", "Mandela 1", "Part 3", "Mandela 1 - Part 3"},
		{"physical shed name numeric pen", "Castro", "1", "Castro", "1", "Castro 1"},

		// An undivided shed whose NAME ends in a digit is never split (AGENTS.md rule 1).
		{"undivided numeric shed name", "Yashoda 9", "", "Yashoda 9", "", "Yashoda 9"},
		{"undivided shed", "Yashoda", "", "Yashoda", "", "Yashoda"},

		// The 'whole' sentinel is a matching key and must never reach a screen.
		{"whole sentinel never renders", "Yashoda", "whole", "Yashoda", "whole", "Yashoda"},

		// Stored label is authoritative when it disagrees with the name: compose once, not twice.
		{"stored label disagrees with the name", "Godel 2 - Part 1", "Part 7", "Godel 2", "Part 7", "Godel 2 - Part 7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, partition, display := ResolveComposedName("shed-id", tc.shedName, tc.storedLabel)
			if parent != tc.wantParent {
				t.Errorf("parent = %q, want %q", parent, tc.wantParent)
			}
			if partition != tc.wantPartition {
				t.Errorf("partition = %q, want %q", partition, tc.wantPartition)
			}
			if display != tc.wantDisplay {
				t.Errorf("display = %q, want %q", display, tc.wantDisplay)
			}
		})
	}
}

// TestResolveComposedNameIsIdempotent states the property the four hand-rolled guards were each
// reaching for: composing an already-composed answer must be a no-op. A caller that runs a row
// through two layers (a read model that stamps the display, then a card builder that stamps it
// again) is the shape that shipped the defect, so the property is pinned rather than the sites.
func TestResolveComposedNameIsIdempotent(t *testing.T) {
	for _, start := range []struct{ name, label string }{
		{"Mandela 1 - Part 1", "Part 1"},
		{"Castro 1", "1"},
		{"Godel 2", "Part 4"},
		{"Yashoda 9", ""},
	} {
		_, partition, once := ResolveComposedName("shed-id", start.name, start.label)
		_, _, twice := ResolveComposedName("shed-id", once, partition)
		if once != twice {
			t.Errorf("not idempotent for (%q, %q): once=%q twice=%q", start.name, start.label, once, twice)
		}
	}
}

// TestResolveComposedNameAmbiguityIsUnreachableOnTheLiveConventions pins the register property
// that keeps the one ambiguous shape out of reach: a parent shed whose name ends in a digit always
// uses the WORDED pen convention, and the bare-numeric convention is only ever used under a parent
// with no trailing digit. Both halves are asserted, because it is their COMBINATION that makes the
// resolution safe -- either one alone still admits "Mandela 1" + "1".
func TestResolveComposedNameAmbiguityIsUnreachableOnTheLiveConventions(t *testing.T) {
	// Bare-numeric pens, as the live catalog holds them: parent name never ends in a digit.
	for _, parent := range []string{"Castro", "Gandhi", "Yashoda", "Ho Chi Minh", "Old Yashoda"} {
		for _, pen := range []string{"1", "2", "10"} {
			_, _, composed := ResolveComposedName("shed-id", parent, pen)
			// Composing the answer again must return it unchanged -- the property that fails
			// exactly when a parent's own name ends in the pen number.
			_, _, again := ResolveComposedName("shed-id", composed, pen)
			if composed != again {
				t.Errorf("%q + %q: composed=%q but recomposed=%q", parent, pen, composed, again)
			}
		}
	}
	// Digit-terminated parents use the worded convention, which is unambiguous either way.
	for _, parent := range []string{"Godel 1", "Godel 2", "Mandela 1", "Mandela 2"} {
		_, _, composed := ResolveComposedName("shed-id", parent, "Part 1")
		if composed != parent+" - Part 1" {
			t.Errorf("%q: composed=%q", parent, composed)
		}
		if _, _, again := ResolveComposedName("shed-id", composed, "Part 1"); again != composed {
			t.Errorf("%q: recomposed=%q, want %q", parent, again, composed)
		}
	}
}
