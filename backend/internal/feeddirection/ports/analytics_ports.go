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
	// ExecutionAnalytics returns per-day completion-status counts for packing,
	// distribution and transport, plus the daily median submit→verdict latency.
	// Status counts only — completions carry proofs, never kg.
	ExecutionAnalytics(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.ExecutionAnalytics, error)
	// ExperimentAnalytics returns the experiment workflow's authored absolute kg
	// per (feed day, arm). No per-head figure exists for these rows.
	ExperimentAnalytics(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.ExperimentAnalytics, error)
	// StockAnalytics returns per-item stock positions from the bootstrapped
	// purchase ledger (depleting at sheet lock) and the daily expenditure
	// series for the query window. Empty when the ledger is unpopulated.
	StockAnalytics(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.StockAnalytics, error)
	// ShedFeedAnalytics returns every pen (shed + optional partition) the frozen
	// sheet directed feed to in the window, with per-feed-item kg totals and the
	// pen's total. Same membership and predicates as DirectedAnalytics, so the
	// per-item sums across pens agree with the per-item chart series.
	ShedFeedAnalytics(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.ShedFeedAnalytics, error)
}
