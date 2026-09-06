package domain

import (
	"strconv"
	"testing"
)

func TestStockRateOverridesArePinnedPerFarmFeedAndParseAsNumbers(t *testing.T) {
	farms, feeds, rates := StockRateOverrideArrays()
	if len(farms) != len(StockRateOverrides) || len(feeds) != len(farms) || len(rates) != len(farms) {
		t.Fatalf("the three arrays are bound as parallel columns: %d/%d/%d", len(farms), len(feeds), len(rates))
	}
	seen := map[string]bool{}
	for i := range farms {
		key := farms[i] + "|" + feeds[i]
		if seen[key] {
			// unnest would emit the pair twice and the LEFT JOIN would fan the card out.
			t.Errorf("%q is overridden twice", key)
		}
		seen[key] = true
		// The text is cast to numeric in SQL, where a bad value is a runtime
		// error on a live page rather than a compile failure here.
		v, err := strconv.ParseFloat(rates[i], 64)
		if err != nil {
			t.Errorf("%q rate %q is not a number: %v", key, rates[i], err)
		}
		if v <= 0 {
			// A zero or negative divisor would make days-left infinite or negative.
			t.Errorf("%q rate %q must be positive", key, rates[i])
		}
	}
	// The pin the maintainer asked for, named explicitly so removing it is a
	// deliberate act rather than a silent list edit.
	if !seen["CBE|concentrate"] {
		t.Errorf("CBE Concentrate is the pinned feed (55 kg/day, maintainer instruction 2026-09-06); it is not in the list")
	}
}
