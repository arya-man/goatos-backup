package postgres

import (
	"context"
	"strconv"

	"github.com/vgoats/goatos/backend/internal/alerts/domain"
	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
)

// FeedLowStockSource is the feed module's own low-stock read (its analytics repository).
type FeedLowStockSource interface {
	LowStockFeeds(ctx context.Context, tenantID string, withinDays int) ([]feeddomain.LowStockFeed, error)
}

// LowStockReader adapts the feed module's read to the alerts port, so the stock rule is
// the SAME number the Stock tab's red card and the daily push already report -- never a
// second stock calculation.
type LowStockReader struct {
	source FeedLowStockSource
	cache  *readcache.Cache
}

// NewLowStockReader wraps the feed repository.
func NewLowStockReader(source FeedLowStockSource) *LowStockReader {
	return &LowStockReader{source: source}
}

// WithCache serves the low-stock value from a per-tenant read cache. The value is a whole-tenant
// aggregate over every reached purchase and every locked feed issue ever (feedLowStockSQL: an
// 83k-row scan on the stg clone, ~70 ms of the /alerts/rows budget) and depends on no clock or
// park parameter, so one entry per (tenant, window) is exact until a feed write lands. Feed
// issue writes publish readcache.NotifyTx and the stock tables announce their own writes
// (migration 000416 triggers); any eviction for the tenant, whatever its park scope, drops the
// entry because the key spans the whole tenant. nil keeps the uncached read.
func (l *LowStockReader) WithCache(cache *readcache.Cache) *LowStockReader {
	l.cache = cache
	return l
}

// LowStock forwards to the feed module and copies the rows across.
func (l *LowStockReader) LowStock(ctx context.Context, tenantID string, withinDays int) ([]domain.LowStockFeed, error) {
	key := readcache.Key{Tenant: tenantID, Params: "alerts_feed_low_stock|" + strconv.Itoa(withinDays)}
	rows, err := readcache.Load(ctx, l.cache, key, func(ctx context.Context) ([]domain.LowStockFeed, error) {
		return l.read(ctx, tenantID, withinDays)
	})
	if err != nil {
		return nil, err
	}
	// Callers own their slice; the cached one is never handed out.
	return append([]domain.LowStockFeed(nil), rows...), nil
}

func (l *LowStockReader) read(ctx context.Context, tenantID string, withinDays int) ([]domain.LowStockFeed, error) {
	rows, err := l.source.LowStockFeeds(ctx, tenantID, withinDays)
	if err != nil {
		return nil, err
	}
	out := make([]domain.LowStockFeed, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.LowStockFeed{
			ParkID: r.ParkID, FarmLabel: r.FarmLabel, FeedItemLabel: r.FeedItemLabel, FeedItemKey: r.FeedItemKey,
			BalanceKg: r.BalanceKg, AvgDailyKg: r.AvgDailyKg, DaysLeft: r.DaysLeft,
		})
	}
	return out, nil
}
