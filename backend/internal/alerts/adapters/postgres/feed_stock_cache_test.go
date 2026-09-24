package postgres

import (
	"context"
	"reflect"
	"testing"

	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
)

type countingLowStock struct {
	calls int
	rows  []feeddomain.LowStockFeed
}

func (c *countingLowStock) LowStockFeeds(context.Context, string, int) ([]feeddomain.LowStockFeed, error) {
	c.calls++
	return append([]feeddomain.LowStockFeed(nil), c.rows...), nil
}

// TestLowStockReaderCachesPerTenantAndEvictsOnFeedWrites: the whole-tenant low-stock value
// (an aggregate over every locked feed issue ever) is read once per tenant and window, served
// identically from the cache, and dropped by ANY eviction for the tenant -- a park-scoped feed
// issue write included, because the value spans every park.
func TestLowStockReaderCachesPerTenantAndEvictsOnFeedWrites(t *testing.T) {
	ctx := context.Background()
	src := &countingLowStock{rows: []feeddomain.LowStockFeed{{ParkID: "p1", FarmLabel: "CBE", FeedItemLabel: "Concentrate", FeedItemKey: "c", BalanceKg: "10", AvgDailyKg: "9", DaysLeft: 1}}}
	cache := readcache.New(readcache.DefaultOptions("alerts"))
	cache.SetCoherent(true)
	r := NewLowStockReader(src).WithCache(cache)

	first, err := r.LowStock(ctx, "t1", 3)
	if err != nil {
		t.Fatal(err)
	}
	uncached, _ := NewLowStockReader(src).LowStock(ctx, "t1", 3)
	src.calls = 1
	second, err := r.LowStock(ctx, "t1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if src.calls != 1 {
		t.Fatalf("second read must be a cache hit, source called %d times", src.calls)
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first, uncached) {
		t.Fatalf("cached value differs: %+v vs %+v vs %+v", first, second, uncached)
	}
	// A caller mutating its slice must not corrupt the cached value.
	second[0].DaysLeft = 99
	third, _ := r.LowStock(ctx, "t1", 3)
	if third[0].DaysLeft != 1 {
		t.Fatal("cached rows were shared with a caller")
	}
	// Different window or tenant: separate entries.
	if _, err := r.LowStock(ctx, "t1", 7); err != nil || src.calls != 2 {
		t.Fatalf("window must be part of the key: calls=%d err=%v", src.calls, err)
	}
	if _, err := r.LowStock(ctx, "t2", 3); err != nil || src.calls != 3 {
		t.Fatalf("tenant must be part of the key: calls=%d err=%v", src.calls, err)
	}
	// A park-scoped write eviction (feed issue lock in p9) drops the tenant-wide value.
	cache.Evict(ctx, "t1", "p9")
	src.rows[0].DaysLeft = 0
	fresh, _ := r.LowStock(ctx, "t1", 3)
	if src.calls != 4 || fresh[0].DaysLeft != 0 {
		t.Fatalf("eviction must force a fresh read: calls=%d days=%d", src.calls, fresh[0].DaysLeft)
	}
}
