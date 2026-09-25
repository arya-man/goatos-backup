package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// BuyerAnalyticsRepository is the buyer analytics reporting read (maintainer request 2026-09-15).
//
// RECORDED cross-module reporting read, the load-wise shape (docs/decisions/sales-loadwise.md,
// docs/decisions/sales-buyer-analytics.md): procurement owns the vendor register and joins OUT to
// sales_deals and sales_deal_lines to read every closed deal per buyer. Read-only, reporting grain
// only -- nothing here gates a sale or edits a vendor -- and the sales module's own lock
// (migration 000173: sales reads nothing from herd/procurement) is untouched because the
// dependency points the other way.
type BuyerAnalyticsRepository interface {
	// ClosedBuyerDeals returns every closed deal in the filter with its buyer RESOLVED (vendor id
	// when the deal's vendor still exists, else the one vendor whose business name matches the
	// typed name, else the typed name), plus the deal's own animal/revenue/outstanding rollup.
	// farm is "" for the whole company or an exact active park code. The phone number is always read here;
	// the handler blanks it for a caller who may not see the register.
	ClosedBuyerDeals(ctx context.Context, tenantID, farm string) ([]domain.BuyerDealFact, error)
	// ListParkCodes returns the codes of the tenant's active parks: the farm filter's choices.
	ListParkCodes(ctx context.Context, tenantID string) ([]string, error)
}
