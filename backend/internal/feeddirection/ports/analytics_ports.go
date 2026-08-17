package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// DirectedAnalyticsReader serves the Feed Analytics windowed rollup over the
// frozen issue rows. Read-only, bounded by domain.MaxAnalyticsWindowDays, and
// scoped to the NORMAL workflow — experiment cells author absolute kg with
// informational head counts, so folding them into per-head math would divide a
// shed total by heads it was never multiplied by. The experiment series gets its
// own read.
type DirectedAnalyticsReader interface {
	// DirectedAnalytics returns day totals and per-item day series for the window.
	// A day with no issued sheet simply has no rows — absence is "nothing issued",
	// never a fabricated zero.
	DirectedAnalytics(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.DirectedAnalytics, error)
}
