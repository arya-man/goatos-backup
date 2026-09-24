package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
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

// breedsBySpeciesSQL reads the active breeds of every species the registry names, in ONE round
// trip: a query per product would be one round trip per row of a list that grows with the farm.
const breedsBySpeciesSQL = `
SELECT species, canonical_name
FROM public.breeds
WHERE species = ANY($1::text[]) AND status = 'active'
ORDER BY species, lower(canonical_name)`

// activeFeedItemsSQL is the feed a farm may sell: its own live catalogue, in the order it keeps it.
const activeFeedItemsSQL = `
SELECT feed_item_label
FROM public.feed_item_catalog
WHERE tenant_id = $1 AND status = 'active'
ORDER BY display_order, lower(feed_item_label)`

// ListProductVariants answers what each product may be sold AS -- the line's second dimension.
//
// Every list comes from a LIVE vocabulary the farm already maintains, never from a list typed into
// this module: an animal product offers the breeds of its own species, a feed product offers the
// active feed catalogue, and an `other` product offers its own name, which is what manure has
// always stored. That is the same rule the procurement forms follow, and it is why adding a feed
// to the catalogue makes it sellable the same day with no deploy.
//
// These are READ-ONLY vocabulary reads for a form. Sales writes the chosen word as text and joins
// neither table for anything else.
//
// projection-review: membership=breeds keyed by species and feed_item_catalog keyed by tenant,
// each narrowed to status='active'; group_key=none, both reads return their own rows ungrouped;
// join_cardinality=no joins on either side, so neither can fan out; pagination=none -- these are
// the choices a dropdown must offer in full, and a page of a vocabulary is a vocabulary with a
// piece missing; scope=tenant_id for the feed catalogue, and the product's own species for breeds,
// which is a product-wide table with no tenant column.
func (r *Repository) ListProductVariants(ctx context.Context, tenantID string, products []domain.Product) (map[string][]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Which vocabularies this registry actually needs, gathered BEFORE any query. A query per
	// product inside the loop is one round trip per row of a list that grows with the farm; both
	// reads below are therefore made at most once, whatever the registry holds.
	species := []string{}
	seenSpecies := map[string]bool{}
	wantsFeed := false
	for _, p := range products {
		switch p.Kind {
		case domain.KindAnimal:
			if p.SpeciesCode != "" && !seenSpecies[p.SpeciesCode] {
				seenSpecies[p.SpeciesCode] = true
				species = append(species, p.SpeciesCode)
			}
		case domain.KindFeed:
			wantsFeed = true
		}
	}

	breedsBySpecies := map[string][]string{}
	if len(species) > 0 {
		rows, err := r.pool.Query(ctx, breedsBySpeciesSQL, species)
		if err != nil {
			return nil, fmt.Errorf("sales: list breeds: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var sp, name string
			if err := rows.Scan(&sp, &name); err != nil {
				return nil, fmt.Errorf("sales: scan breed: %w", err)
			}
			breedsBySpecies[sp] = append(breedsBySpecies[sp], name)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("sales: breed rows: %w", err)
		}
	}

	feeds := []string{}
	if wantsFeed {
		rows, err := r.pool.Query(ctx, activeFeedItemsSQL, tenantID)
		if err != nil {
			return nil, fmt.Errorf("sales: list feed items: %w", err)
		}
		feeds, err = scanStrings(rows)
		if err != nil {
			return nil, err
		}
	}

	out := make(map[string][]string, len(products))
	for _, p := range products {
		switch p.Kind {
		case domain.KindAnimal:
			// A product that names no species has no breed list to offer. Left ABSENT rather than
			// filled with every breed of every species, which would offer a sheep breed on a goat
			// sale.
			if p.SpeciesCode == "" {
				continue
			}
			out[p.Name] = append([]string(nil), breedsBySpecies[p.SpeciesCode]...)
		case domain.KindFeed:
			out[p.Name] = append([]string(nil), feeds...)
		default:
			// `other` sells as itself. Manure's line has stored the word 'Manure' in this column
			// since the ledger was imported, and a farm adding "Hay" gets the same shape.
			out[p.Name] = []string{p.Name}
		}
	}
	return out, nil
}

func scanStrings(rows pgx.Rows) ([]string, error) {
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("sales: scan vocabulary row: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sales: vocabulary rows: %w", err)
	}
	return out, nil
}

// upsertSellableProductSQL adds an item or edits the one that already carries its code.
//
// is_builtin is NEVER written here: the three rows every tenant starts with are seeded by
// migration 000393 and may be renamed on screen, but a person cannot create a built-in, and an
// edit cannot promote a row into one. The row's code is its identity and is fixed at creation, so
// a rename updates the row rather than making a second one -- which is what keeps the sales
// already recorded under it pointing at the same item.
const upsertSellableProductSQL = `
INSERT INTO public.sellable_product_catalog (
	tenant_id, product_code, name, kind, unit, species_code, sort_order, status, updated_by
) VALUES ($1, $2, $3, $4, $5, nullif(btrim($6), ''), $7, $8, nullif(btrim($9), '')::uuid)
ON CONFLICT (tenant_id, product_code) DO UPDATE SET
	name         = EXCLUDED.name,
	kind         = EXCLUDED.kind,
	unit         = EXCLUDED.unit,
	species_code = EXCLUDED.species_code,
	sort_order   = EXCLUDED.sort_order,
	status       = EXCLUDED.status,
	updated_by   = EXCLUDED.updated_by,
	row_version  = public.sellable_product_catalog.row_version + 1,
	updated_at   = now()
RETURNING product_code, name, kind, unit, COALESCE(species_code, ''), sort_order`

// readSellableProductSQL reads one row of the registry, whatever its status.
const readSellableProductSQL = `
SELECT product_code, name, kind, unit, COALESCE(species_code, ''), sort_order, status, is_builtin
FROM public.sellable_product_catalog
WHERE tenant_id = $1 AND product_code = $2`

// SaveSellableProduct adds an item to the farm's registry, or edits the one with its code.
func (r *Repository) SaveSellableProduct(ctx context.Context, tenantID string, write domain.ProductWrite, actorID string) (domain.Product, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Product{}, fmt.Errorf("sales: begin save sellable product: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// EVERY ROW IS EDITABLE (maintainer instruction 2026-09-23). The built-ins were locked out of
	// changing kind and being switched off; the maintainer asked why, and the honest answer is
	// that the fear was overstated. Nothing recorded moves: a sale line is STAMPED with the code,
	// name and kind it was sold under, and the Sold page's cards key on the CODE, which an edit
	// never changes. Changing what Goat IS only changes what the NEXT goat sale asks for, which
	// is exactly what somebody editing this list means to do.

	// AN ADD MUST NOT LAND ON AN EXISTING ROW. The code is derived from the name, so adding an
	// item called "Feed" when a Feed already exists derives the same code -- and the upsert below,
	// which is exactly right for an EDIT, would quietly rewrite that row instead. On this farm it
	// flipped the real Feed item from feed-from-the-store to something else, which would have
	// stopped every feed sale drawing on the store. Caught by the browser run, 2026-09-23.
	if write.Adding {
		var exists bool
		if err := tx.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM public.sellable_product_catalog WHERE tenant_id = $1 AND product_code = $2)`,
			tenantID, write.Code).Scan(&exists); err != nil {
			return domain.Product{}, fmt.Errorf("sales: check sellable product exists: %w", err)
		}
		if exists {
			return domain.Product{}, ports.ErrProductNameTaken
		}
	}

	// NOBODY NUMBERS THE LIST (maintainer instruction 2026-09-23: "just remove it"). A new item is
	// appended after the last one; an edit keeps the place the row already has. The column stays,
	// because the list still needs a stable order to be read and offered in -- it simply stopped
	// being a question anyone is asked.
	if write.SortOrder <= 0 {
		var next int
		if err := tx.QueryRow(ctx, nextProductSortOrderSQL, tenantID).Scan(&next); err != nil {
			return domain.Product{}, fmt.Errorf("sales: next sellable product order: %w", err)
		}
		write.SortOrder = next
	}

	var out domain.Product
	if err := tx.QueryRow(ctx, upsertSellableProductSQL,
		tenantID, write.Code, write.Name, write.Kind, write.Unit, write.SpeciesCode,
		write.SortOrder, write.Status, actorID,
	).Scan(&out.Code, &out.Name, &out.Kind, &out.Unit, &out.SpeciesCode, &out.SortOrder); err != nil {
		if strings.Contains(err.Error(), "sellable_product_catalog_tenant_name_uidx") {
			return domain.Product{}, ports.ErrProductNameTaken
		}
		return domain.Product{}, fmt.Errorf("sales: save sellable product: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "sales.sellable_product.save",
		ResourceType: "sellable_product",
		// ResourceID is deliberately EMPTY: the column is a uuid, and this row is keyed by a
		// derived CODE. The code rides in the metadata below, which is where a reader of the
		// audit trail will find which item was edited.
		Metadata: map[string]any{
			"domain": "sales", "module": "sales", "category": "config",
			"product_code": out.Code,
			"name":         out.Name,
			"kind":         out.Kind,
			"unit":         out.Unit,
			"status":       write.Status,
		},
	}); err != nil {
		return domain.Product{}, fmt.Errorf("sales: audit sellable product: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Product{}, fmt.Errorf("sales: commit sellable product: %w", err)
	}
	return out, nil
}

// allSellableProductsSQL reads the registry INCLUDING archived rows, for the editor -- which must
// show what is switched off so it can be switched back on. A per-tenant catalog of a few dozen
// rows, indexed on tenant_id, read once when Sales Config opens.
const allSellableProductsSQL = `
SELECT product_code, name, kind, unit, COALESCE(species_code, ''), sort_order, status, is_builtin
FROM public.sellable_product_catalog
WHERE tenant_id = $1
ORDER BY sort_order, name`

// ListAllSellableProducts implements ports.SalesRepository.
func (r *Repository) ListAllSellableProducts(ctx context.Context, tenantID string) ([]domain.ProductRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, allSellableProductsSQL, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sales: list all sellable products: %w", err)
	}
	defer rows.Close()
	out := []domain.ProductRow{}
	for rows.Next() {
		var p domain.ProductRow
		if err := rows.Scan(&p.Code, &p.Name, &p.Kind, &p.Unit, &p.SpeciesCode, &p.SortOrder, &p.Status, &p.IsBuiltin); err != nil {
			return nil, fmt.Errorf("sales: scan sellable product row: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sales: sellable product rows: %w", err)
	}
	return out, nil
}

// nextProductSortOrderSQL puts a new item after the last one. The gap of ten leaves room to
// reorder later without renumbering, if the farm ever asks for that.
const nextProductSortOrderSQL = `
SELECT COALESCE(MAX(sort_order), 0) + 10
FROM public.sellable_product_catalog
WHERE tenant_id = $1`

// countSalesUnderProductSQL is how many recorded sale lines name this item. It is the ONE question
// that decides whether an item may be deleted outright or only switched off.
const countSalesUnderProductSQL = `
SELECT count(*)
FROM public.sales_deal_lines
WHERE tenant_id = $1 AND product_code = $2`

// DeleteSellableProduct removes an item from the registry.
//
// It refuses when the farm has SOLD any of it. A sale line stores the item's name and code, so the
// row leaving would not corrupt the ledger -- but it would leave the reader of a recorded sale
// with no way to look up what they sold, and the Sold page's buckets keyed on that code with no
// item behind them. Switching the item off is the honest answer there, and it is what the refusal
// says: an archived item is gone from every dropdown and keeps its history readable.
func (r *Repository) DeleteSellableProduct(ctx context.Context, tenantID, code, actorID string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("sales: begin delete sellable product: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sold int
	if err := tx.QueryRow(ctx, countSalesUnderProductSQL, tenantID, code).Scan(&sold); err != nil {
		return fmt.Errorf("sales: count sales under product: %w", err)
	}
	if sold > 0 {
		return ports.ErrProductHasSales
	}

	tag, err := tx.Exec(ctx, `
DELETE FROM public.sellable_product_catalog
WHERE tenant_id = $1 AND product_code = $2`, tenantID, code)
	if err != nil {
		return fmt.Errorf("sales: delete sellable product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrProductNotFound
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "sales.sellable_product.delete",
		ResourceType: "sellable_product",
		Metadata: map[string]any{
			"domain": "sales", "module": "sales", "category": "config",
			"product_code": code,
		},
	}); err != nil {
		return fmt.Errorf("sales: audit sellable product delete: %w", err)
	}
	return tx.Commit(ctx)
}

// ListFeedItems is the farm's configured feed list: the same feed_item_catalog rows the ration
// grid and the feed purchases are authored against, which is the only set a feed sale may name.
func (r *Repository) ListFeedItems(ctx context.Context, tenantID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, activeFeedItemsSQL, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sales: list feed items: %w", err)
	}
	return scanStrings(rows)
}
