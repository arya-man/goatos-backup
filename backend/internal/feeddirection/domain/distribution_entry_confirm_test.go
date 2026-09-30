package domain

import "testing"

func TestDistributionEntryVarianceIsFivePercentOfThePlanEitherSide(t *testing.T) {
	for _, tc := range []struct {
		name             string
		entered, planned float64
		wantDirection    string
		wantExceeds      bool
	}{
		{"on plan", 100, 100, "", false},
		{"exactly 5% over is the tolerance", 105, 100, "", false},
		{"exactly 5% under is the tolerance", 95, 100, "", false},
		{"just over 5% above", 105.1, 100, DistributionEntryAbovePlan, true},
		{"just over 5% below", 94.9, 100, DistributionEntryBelowPlan, true},
		// The band scales with the pen: 600 g on a 10 kg pen is 6%, while 3 kg on a 100 kg pen is 3%.
		{"small pen, 600 g over", 10.6, 10, DistributionEntryAbovePlan, true},
		{"large pen, 3 kg over", 103, 100, "", false},
		{"zero reading on a fed pen", 0, 40, DistributionEntryBelowPlan, true},
		{"no readable plan checks nothing", 55, 0, "", false},
		{"negative plan checks nothing", 55, -1, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			direction, exceeds := DistributionEntryVariance(tc.entered, tc.planned)
			if direction != tc.wantDirection || exceeds != tc.wantExceeds {
				t.Fatalf("DistributionEntryVariance(%v, %v) = (%q, %v), want (%q, %v)",
					tc.entered, tc.planned, direction, exceeds, tc.wantDirection, tc.wantExceeds)
			}
		})
	}
}

// The distribution codes must never collide with packing's: both clients render copy KEYED BY THE
// CODE, and packing's copy says "500 g".
func TestDistributionVarianceCodesAreNotPackingCodes(t *testing.T) {
	for _, code := range []string{DistributionEntryAbovePlan, DistributionEntryBelowPlan} {
		if code == PackingEntryAbovePlan || code == PackingEntryBelowPlan {
			t.Fatalf("distribution code %q collides with a packing code", code)
		}
		if DistributionEntryVarianceMessage(code) == "" {
			t.Fatalf("code %q has no farm sentence", code)
		}
	}
}
