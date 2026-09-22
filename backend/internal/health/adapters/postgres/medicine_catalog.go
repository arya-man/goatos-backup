package postgres

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/health/domain"
)

// The authoring picker's read over the shared item registry.

const sqlListMedicines = `
SELECT i.item_id::text, i.name, i.category, COALESCE(c.name, '')
FROM inventory_items i
LEFT JOIN item_categories c
  ON c.tenant_id = i.tenant_id AND c.category_id = i.category_id
WHERE i.tenant_id = $1::uuid
  AND i.category = 'medicine'
  AND i.status = 'active'
ORDER BY lower(i.name)`

func (r *Repository) ListMedicines(ctx context.Context, tenantID string) ([]domain.CatalogItem, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, sqlListMedicines, tenantID)
	if err != nil {
		return nil, fmt.Errorf("health: list medicines: %w", err)
	}
	defer rows.Close()

	out := []domain.CatalogItem{}
	for rows.Next() {
		var item domain.CatalogItem
		if err := rows.Scan(&item.ItemID, &item.Name, &item.Category, &item.CategoryPath); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
