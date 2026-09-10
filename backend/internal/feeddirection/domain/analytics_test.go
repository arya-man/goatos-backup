package domain

import "testing"

// The read scope is BOTH successors and every retired member they fold in
// (maintainer decision 2026-09-10). A missing successor would drop its own
// loads and report a store assembled from the retired members alone; a missing
// member would lose the sacks still sitting in the store.
func TestMeshaConcentrateStockKeysOneToManyParkScopePageBoundaryIncludesSpecieslessAdult(t *testing.T) {
	seen := map[string]bool{}
	for _, key := range MeshaConcentrateStockKeys {
		if seen[key] {
			t.Fatalf("duplicate Mesha concentrate stock key %q", key)
		}
		seen[key] = true
	}

	for _, key := range []string{
		"mesha_adult_concentrate",
		"mesha_adult_concentrate_goat",
		"mesha_adult_concentrate_sheep",
		"mesha_kids_concentrate",
		"mesha_kids_goat_concentrate",
		"mesha_kids_sheep_concentrate",
	} {
		if !seen[key] {
			t.Fatalf("Mesha concentrate stock key %q missing from bounded per-farm table scope", key)
		}
	}
	if len(MeshaConcentrateStockKeys) != 6 {
		t.Fatalf("Mesha concentrate per-farm table scope should remain the named six-key set, got %d keys: %+v", len(MeshaConcentrateStockKeys), MeshaConcentrateStockKeys)
	}

	// Every family the fold can produce must be readable, or the table would
	// carry a row whose own loads were filtered out before it was assembled.
	for _, row := range StockFamilyMerge {
		if !seen[row.FamilyKey] {
			t.Errorf("family %q is a fold target but is not read by the table", row.FamilyKey)
		}
		if !seen[row.MemberKey] {
			t.Errorf("member %q folds into a family but its leftover stock is not read", row.MemberKey)
		}
	}
}
