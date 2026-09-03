package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

var (
	// ErrLoadNotFound is returned when a load id does not resolve inside the caller's tenant.
	// Deliberately indistinguishable from "belongs to another tenant".
	ErrLoadNotFound = errors.New("procurement: load not found")
)

// LoadwiseRepository is the load-wise sales reporting read plus the load-cost write.
//
// This is a RECORDED cross-module reporting read (maintainer decision 2026-08-31,
// docs/decisions/sales-loadwise.md): procurement owns the loads and joins OUT to goats (each
// animal's terminal outcome), goat_sale_allocations and sales_deals (the revenue its sold animals
// brought in) to reconcile a load. It is read-only over those tables, reporting grain only —
// nothing here gates a sale, an exit, or any procurement pipeline step. The sales module's own
// lock (migration 000173: sales reads nothing from herd/procurement) is untouched: the dependency
// points the other way.
type LoadwiseRepository interface {
	// LoadwiseSales returns the newest maxLoads loads with their reconciliation counts, attributed
	// sold value and recorded costs, plus the filtered load count and the overall average sold
	// price (the remaining-stock fallback basis). parkID optionally narrows the rows (and the
	// count) to loads whose agree-or-go-bare farm label names that park; empty means no filter.
	LoadwiseSales(ctx context.Context, tenantID, parkID string, maxLoads int) (domain.LoadwiseSales, error)
	// OverdueLoadCandidates returns every load candidate for the daily age alert. It is deliberately
	// not clipped to the UI page window: an old load can sit behind hundreds of newer purchases and
	// still need the CXO alert.
	OverdueLoadCandidates(ctx context.Context, tenantID, asOf string) ([]domain.OverdueLoad, error)
	// SetLoadCost records (or clears) one load's landed cost under the load's row lock. Naturally
	// idempotent: writing the values the load already carries changes nothing and audits nothing.
	SetLoadCost(ctx context.Context, tenantID, loadID string, edit domain.LoadCostEdit, actorID string) error
}
