package domain

import "testing"

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
		"mesha_kids_goat_concentrate",
		"mesha_kids_sheep_concentrate",
	} {
		if !seen[key] {
			t.Fatalf("Mesha concentrate stock key %q missing from bounded per-farm table scope", key)
		}
	}
	if len(MeshaConcentrateStockKeys) != 5 {
		t.Fatalf("Mesha concentrate per-farm table scope should remain the named five-key set, got %d keys: %+v", len(MeshaConcentrateStockKeys), MeshaConcentrateStockKeys)
	}
}
