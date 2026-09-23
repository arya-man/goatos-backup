package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// sellableProductsSQL reads a tenant's ACTIVE registry of what the farm sells (migration 000393)
// in farm order.
//
// projection-review: membership=sellable_product_catalog at its own (tenant_id, product_code)
// primary-key grain, one row per product, narrowed to status='active'; group_key=none, this read
// is ungrouped and returns the rows themselves; join_cardinality=no joins, so nothing can fan out;
// pagination=none -- a farm's product list is a handful of rows and every one of them must be
// offered, a page of it would be a vocabulary with a piece missing; scope=tenant_id.
const sellableProductsSQL = `
SELECT product_code, name, kind, unit, COALESCE(species_code, ''), sort_order
FROM public.sellable_product_catalog
WHERE tenant_id = $1 AND status = 'active'
ORDER BY sort_order, name`

// ListSellableProducts serves the registry.
func (r *Repository) ListSellableProducts(ctx context.Context, tenantID string) ([]domain.Product, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sellableProductsSQL, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sales: list sellable products: %w", err)
	}
	defer rows.Close()
	return scanSellableProducts(rows)
}

// sellableProductsInTx is the SAME read under the writing transaction. It exists so a product
// archived between the form opening and the save landing is caught where it matters: the
// service's read is for the error MESSAGE, this one is the guarantee.
func sellableProductsInTx(ctx context.Context, tx pgx.Tx, tenantID string) (domain.ProductCatalog, error) {
	rows, err := tx.Query(ctx, sellableProductsSQL, tenantID)
	if err != nil {
		return domain.ProductCatalog{}, fmt.Errorf("sales: read sellable products in tx: %w", err)
	}
	defer rows.Close()
	products, err := scanSellableProducts(rows)
	if err != nil {
		return domain.ProductCatalog{}, err
	}
	return domain.NewProductCatalog(products), nil
}

func scanSellableProducts(rows pgx.Rows) ([]domain.Product, error) {
	out := []domain.Product{}
	for rows.Next() {
		var p domain.Product
		if err := rows.Scan(&p.Code, &p.Name, &p.Kind, &p.Unit, &p.SpeciesCode, &p.SortOrder); err != nil {
			return nil, fmt.Errorf("sales: scan sellable product: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sales: sellable product rows: %w", err)
	}
	return out, nil
}

// confirmProductsStillSellable re-resolves every line's product under the writing transaction.
//
// It compares the CODE and the KIND, not the name: a product renamed while the form was open is
// the same row and the sale goes through under the name the farm now keeps, but a product whose
// kind changed is a different thing entirely -- feed that became an animal would draw stock the
// farm no longer means to draw -- and a product no longer active is not sellable at all.
func confirmProductsStillSellable(ctx context.Context, tx pgx.Tx, tenantID string, lines []domain.DealLineWrite) error {
	catalog, err := sellableProductsInTx(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	for _, l := range lines {
		code, kind, _ := l.ResolvedProduct()
		current, ok := catalog.Lookup(l.ProductType)
		if !ok || current.Code != code || current.Kind != kind {
			return fmt.Errorf("%w: %s", ports.ErrProductNotSellable, l.ProductType)
		}
	}
	return nil
}
