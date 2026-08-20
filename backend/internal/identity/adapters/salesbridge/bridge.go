// Package salesbridge supplies identity's sale-allocation gate with the one fact it needs
// from the sales ledger: how many animals a deal is for.
//
// It exists as its OWN package, outside identity's postgres adapter, because that is the
// seam. Identity's own queries stay clear of the sales schema (migration 000177), and the
// dependency is an interface identity declares rather than a join it performs. Same shape
// as bulkstatus/identitybridge, which adapts the other direction.
package salesbridge

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

// Bridge reads the sales ledger on the allocation gate's behalf.
type Bridge struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Bridge { return &Bridge{pool: pool} }

var _ ports.SaleDealReader = (*Bridge)(nil)

// ReadSaleDeal returns the deal's declared animal count and how many animals are already
// tagged to it.
//
// The two numbers are read in ONE statement so they cannot disagree: taking them
// separately would let a concurrent confirm land between them and make the remaining
// count read high, which is exactly the over-mapping this gate exists to stop.
func (b *Bridge) ReadSaleDeal(ctx context.Context, tenantID, salesDealID string) (*ports.SaleDeal, error) {
	var declared *int
	var tagged int
	err := b.pool.QueryRow(ctx, `
SELECT
  -- animal_count is numeric in the ledger because the source sheet stores it as a decimal
  -- (23.0). A fractional count is not a real animal count, so it is truncated rather than
  -- rounded up: mapping must never be asked to hit a target the farm cannot physically meet.
  CASE WHEN d.animal_count IS NULL THEN NULL ELSE floor(d.animal_count)::int END,
  (SELECT count(*)::int FROM goat_sale_allocations a
    WHERE a.tenant_id = d.tenant_id AND a.sales_deal_id = d.id AND a.status = 'tagged')
FROM sales_deals d
WHERE d.tenant_id = $1::uuid AND d.id = $2::uuid`, tenantID, salesDealID).Scan(&declared, &tagged)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrSaleDealNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("salesbridge: read sale deal: %w", err)
	}
	if declared == nil || *declared <= 0 {
		return nil, ports.ErrSaleDealNoAnimalCount
	}
	return &ports.SaleDeal{
		SalesDealID:         salesDealID,
		DeclaredAnimalCount: *declared,
		AlreadyTagged:       tagged,
	}, nil
}
