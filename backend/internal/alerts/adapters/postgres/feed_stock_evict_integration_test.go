package postgres

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
)

// TestLowStockCacheIsEvictedByCommittedStockTableWrites: any committed write to a table the
// low-stock value reads (here feed_item_catalog, written outside the feed-direction issue path)
// evicts the cached value on the API's listener via the 000416 triggers; a rolled-back write
// does not.
func TestLowStockCacheIsEvictedByCommittedStockTableWrites(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	const tenant = "00000000-0000-4000-8000-00000000a1e1"
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'alerts cache', 'active')`, tenant); err != nil {
		t.Fatal(err)
	}
	cache := readcache.New(readcache.DefaultOptions("alerts"))
	readcache.NewListener(pool, slog.New(slog.NewTextHandler(io.Discard, nil)), cache).Start(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for !cache.Coherent() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !cache.Coherent() {
		t.Fatal("listener never connected")
	}
	src := &countingLowStock{rows: []feeddomain.LowStockFeed{{ParkID: "p1", FeedItemKey: "c", DaysLeft: 1}}}
	r := NewLowStockReader(src).WithCache(cache)
	read := func() {
		t.Helper()
		if _, err := r.LowStock(ctx, tenant, 3); err != nil {
			t.Fatal(err)
		}
	}
	read()
	read()
	if src.calls != 1 {
		t.Fatalf("warm read must hit, calls=%d", src.calls)
	}

	// Rolled back: no eviction.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `INSERT INTO feed_item_catalog (tenant_id, feed_item_label) VALUES ($1::uuid, 'Rolled back')`, tenant); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	read()
	if src.calls != 1 {
		t.Fatalf("a rolled-back write must not evict, calls=%d", src.calls)
	}

	// Committed: evicted on the listener.
	if _, err := pool.Exec(ctx, `INSERT INTO feed_item_catalog (tenant_id, feed_item_label) VALUES ($1::uuid, 'Committed')`, tenant); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		read()
		if src.calls == 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("committed stock-table write never evicted the low-stock cache, calls=%d", src.calls)
}
