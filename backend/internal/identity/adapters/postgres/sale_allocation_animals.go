package postgres

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// saleAllocationAnimalsSQL is the one-per-animal read-back of a sale's allocation: the tag and
// pen SNAPSHOTTED at tagging, plus the weight (000282) typed for each. Bounded
// by the deal's own animal count (at most MaxSaleAllocationGoatsPerCommand per confirm), served
// by the (tenant_id, sales_deal_id) index the shed-wise read-back already uses. The numerics come
// back as text so nothing round-trips through a float.
const saleAllocationAnimalsSQL = `
SELECT a.goat_id::text,
       COALESCE(a.tag_number, ''),
       COALESCE(a.shed_id::text, ''),
       COALESCE(sh.name, ''),
       COALESCE(a.partition_label, ''),
       COALESCE(a.weight_kg::text, '')
FROM goat_sale_allocations a
LEFT JOIN locations sh ON sh.location_id = a.shed_id AND sh.tenant_id = a.tenant_id
WHERE a.tenant_id = $1::uuid AND a.sales_deal_id = $2::uuid AND a.status = 'tagged'
ORDER BY sh.name, a.partition_label, a.tag_number, a.goat_id`

// ListSaleAllocationAnimals serves the phone's "already tagged" list for a park head resuming a
// half-tagged sale (maintainer decision 2026-09-11).
func (r *Repository) ListSaleAllocationAnimals(ctx context.Context, tenantID, salesDealID string) ([]ports.SaleAllocationAnimal, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	rows, err := r.pool.Query(ctx, saleAllocationAnimalsSQL, tenantID, salesDealID)
	if err != nil {
		return nil, fmt.Errorf("identity: list sale allocation animals: %w", err)
	}
	defer rows.Close()

	out := []ports.SaleAllocationAnimal{}
	for rows.Next() {
		var a ports.SaleAllocationAnimal
		if err := rows.Scan(&a.GoatID, &a.TagNumber, &a.ShedID, &a.ShedName, &a.PartitionLabel, &a.WeightKg); err != nil {
			return nil, fmt.Errorf("identity: scan sale allocation animal: %w", err)
		}
		// Composed through the canonical helper, never hand-rolled in SQL.
		a.OperationalLocationDisplay = (oploc.OperationalLocation{
			ShedID: a.ShedID, ShedName: a.ShedName, PartitionLabel: a.PartitionLabel,
		}).Display()
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: list sale allocation animals: %w", err)
	}
	return out, nil
}
