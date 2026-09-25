// Package identitybridge supplies the sales ledger with the one herd fact it needs before a sale
// is marked failed: how many animals are still tagged to it.
//
// It is its OWN package, outside the sales postgres adapter, because that is the seam: the sales
// repository stays off the herd schema (migration 000173) and depends on an interface the sales
// module declares. The mirror image of identity/adapters/salesbridge.
package identitybridge

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// Bridge reads the sale-to-animal mapping on the sales ledger's behalf.
type Bridge struct {
	pool *pgxpool.Pool
}

// New builds the bridge.
func New(pool *pgxpool.Pool) *Bridge { return &Bridge{pool: pool} }

var _ ports.SaleTaggingReader = (*Bridge)(nil)

// TaggedAnimalCount counts the deal's live ('tagged') allocations. One indexed read on
// goat_sale_allocations (tenant_id, sales_deal_id); released rows are history and do not count.
func (b *Bridge) TaggedAnimalCount(ctx context.Context, tenantID, dealID string) (int, error) {
	var n int
	if err := b.pool.QueryRow(ctx, `
SELECT count(*)::int
FROM public.goat_sale_allocations a
WHERE a.tenant_id = $1::uuid AND a.sales_deal_id = $2::uuid AND a.status = 'tagged'`, tenantID, dealID).Scan(&n); err != nil {
		return 0, fmt.Errorf("identitybridge: count tagged animals: %w", err)
	}
	return n, nil
}
