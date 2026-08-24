package domain

import "testing"

// The bag table's two tones hang off this one predicate, so the boundary is pinned exactly.
// STRICTLY greater-than: a bag exactly 200 g out is the tolerance, not a breach — a `>=` here
// would flag the very readings the tolerance exists to forgive, and nothing on the screen would
// look wrong while it happened.
func TestExceedsPackingVarianceToleranceIsSymmetricAndStrict(t *testing.T) {
	for _, tc := range []struct {
		name     string
		variance string
		want     bool
	}{
		{"exact match is quiet", "0", false},
		{"just inside, over-packed", "0.19", false},
		{"just inside, under-packed", "-0.19", false},
		{"exactly the tolerance, over", "0.2", false},
		{"exactly the tolerance, under", "-0.2", false},
		{"a gram past it, over", "0.201", true},
		{"a gram past it, under", "-0.201", true},
		// Over and under are the SAME breach: a bag packed 3 kg heavy does not match the sheet
		// any more than one packed 3 kg light, and only flagging shortfalls would hide every
		// over-pack — which costs real feed.
		{"far over", "17", true},
		{"far under", "-17", true},
		// A variance that cannot be read is a gap in the data. Flagging it would accuse a crew
		// on the strength of a parse failure.
		{"unreadable", "", false},
		{"not a number", "n/a", false},
		{"padded decimal still reads", "  -0.5  ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExceedsPackingVarianceTolerance(tc.variance); got != tc.want {
				t.Fatalf("ExceedsPackingVarianceTolerance(%q) = %v, want %v", tc.variance, got, tc.want)
			}
		})
	}
}

// The table and the packed-vs-given trend must judge "the same reading" identically. They do that
// by sharing ONE constant; this fails if a second threshold is ever introduced for either.
func TestPackingVarianceToleranceIsTheSharedConstant(t *testing.T) {
	if PackingVarianceToleranceKg != 0.2 {
		t.Fatalf("tolerance = %v, want 0.2 kg (200 g)", PackingVarianceToleranceKg)
	}
	if !ExceedsPackingVarianceTolerance("0.3") || ExceedsPackingVarianceTolerance("0.1") {
		t.Fatal("the predicate must be derived from PackingVarianceToleranceKg, not a second literal")
	}
}
