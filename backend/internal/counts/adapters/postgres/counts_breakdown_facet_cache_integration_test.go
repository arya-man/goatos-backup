package postgres

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
)

// TestCountsBreakdownFacetCacheIsExactAndEvictedOnGoatWrites: with the facet cache wired, a
// breakdown read returns exactly what the uncached read returns (every paging / filter click
// then skips the 6x goats facet scan), and a committed goat write -- from ANY writer, here raw
// SQL -- evicts the facets through the 000417 triggers so the next read shows the new animal.
func TestCountsBreakdownFacetCacheIsExactAndEvictedOnGoatWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	plain, pool := newBreakdownRepo(t, ctx)
	for i, stage := range []string{"K1", "K2", "F2"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", stage, strp(countsPark), strp(countsShedA), nil)
	}
	cache := readcache.New(readcache.DefaultOptions("counts"))
	readcache.NewListener(pool, slog.New(slog.NewTextHandler(io.Discard, nil)), cache).Start(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for !cache.Coherent() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cached := NewRepository(pool, 10*time.Second).WithFacetCache(cache)

	for _, q := range []domain.CountsBreakdownQuery{
		{TenantID: countsTenant, Limit: 50},
		{TenantID: countsTenant, ManagementStages: []string{"K1"}, Limit: 1, Offset: 1},
		{TenantID: countsTenant, Limit: 50, GroupByPen: true},
	} {
		want, err := plain.GetCountsBreakdown(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ { // miss, then hit
			got, err := cached.GetCountsBreakdown(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			got.ProjectedAt, want.ProjectedAt = time.Time{}, time.Time{}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("read %d of %+v differs from the uncached read:\n got %+v\nwant %+v", i, q, got, want)
			}
		}
	}
	if st := cache.Stats(); st.Hits < 2 {
		t.Fatalf("facets must be served from the cache on repeat reads, stats %+v", st)
	}

	// A committed goat write evicts: the fourth stage appears in the facets.
	insertBreakdownGoat(t, ctx, pool, goatUUID(9), goatDisplayID(9),
		"female", "Beetal", "alive", "F3", strp(countsPark), strp(countsShedA), nil)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := cached.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Facets.Stages) == 4 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a committed goat write never evicted the cached facets")
}
