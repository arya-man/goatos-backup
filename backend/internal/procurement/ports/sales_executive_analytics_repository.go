package ports

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// SalesExecutiveAnalyticsRepository is the Sales > Sales executive analytics reporting read
// (maintainer request 2026-10-02, docs/decisions/sales-executive-analytics.md).
//
// RECORDED cross-module reporting read, the buyer-analytics shape: procurement owns the vendor
// register and joins OUT, read-only, to the audit trail the sales ledger writes and to the market
// survey's entries. Nothing here gates or edits anything.
type SalesExecutiveAnalyticsRepository interface {
	// SalesActivities returns every attributed activity whose IST business date is on or after
	// since (an IST business-day start), one fact per activity at the grain documented on
	// domain.SalesActivityFact.
	SalesActivities(ctx context.Context, tenantID string, since time.Time) ([]domain.SalesActivityFact, error)
	// LatestVendors returns one page of the vendor register, newest first: at most limit rows
	// after skipping offset.
	LatestVendors(ctx context.Context, tenantID string, limit, offset int) ([]domain.LatestVendorFact, error)
	// VendorRegisterTotals returns how many vendors the register holds and how many of them carry
	// no adder (the sheet import).
	VendorRegisterTotals(ctx context.Context, tenantID string) (total, imported int, err error)
}
