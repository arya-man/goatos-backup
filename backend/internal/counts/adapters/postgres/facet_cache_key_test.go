package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// The Counts Breakdown facets are park-scoped, so two different park selections must never share
// a cache entry (regression: the rebase onto c0b37abf2 kept a lifecycle-only key and served the
// first park's Stage/Breed/Pen options to every park).
func TestCountsBreakdownFacetCacheKeyIncludesParks(t *testing.T) {
	cbe := countsBreakdownFacetCacheParams("alive", []string{"park-cbe"})
	cpt := countsBreakdownFacetCacheParams("alive", []string{"park-cpt"})
	all := countsBreakdownFacetCacheParams("alive", nil)
	if cbe == cpt || cbe == all || cpt == all {
		t.Fatalf("facet cache keys collide across parks: cbe=%q cpt=%q all=%q", cbe, cpt, all)
	}
	if a, b := countsBreakdownFacetCacheParams("alive", []string{"b", "a"}), countsBreakdownFacetCacheParams("alive", []string{"a", "b"}); a != b {
		t.Fatalf("park order changed the key: %q vs %q", a, b)
	}
	if countsBreakdownFacetCacheParams("alive", []string{"p"}) == countsBreakdownFacetCacheParams("sold", []string{"p"}) {
		t.Fatal("lifecycle dropped from the key")
	}
}

// Parks are ordered by code (CBE before CPT) on BOTH the batched and the cached path.
func TestSortCountsBreakdownParkFacetsByCode(t *testing.T) {
	parks := []domain.CountsBreakdownSeriesPoint{{Key: "z-uuid", Label: "CPT"}, {Key: "a-uuid", Label: "CBE"}}
	sortCountsBreakdownParkFacets(parks)
	if parks[0].Label != "CBE" || parks[1].Label != "CPT" {
		t.Fatalf("parks not ordered by code: %+v", parks)
	}
}
