package postgres

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/alerts/domain"
	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// FeedLowStockSource is the feed module's own low-stock read (its analytics repository).
type FeedLowStockSource interface {
	LowStockFeeds(ctx context.Context, tenantID string, withinDays int) ([]feeddomain.LowStockFeed, error)
}

// LowStockReader adapts the feed module's read to the alerts port, so the stock rule is
// the SAME number the Stock tab's red card and the daily push already report -- never a
// second stock calculation.
type LowStockReader struct{ source FeedLowStockSource }

// NewLowStockReader wraps the feed repository.
func NewLowStockReader(source FeedLowStockSource) *LowStockReader {
	return &LowStockReader{source: source}
}

// LowStock forwards to the feed module and copies the rows across.
func (l *LowStockReader) LowStock(ctx context.Context, tenantID string, withinDays int) ([]domain.LowStockFeed, error) {
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
