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
	"strings"

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
	var status string
	err := b.pool.QueryRow(ctx, `
SELECT
  d.status,
  -- animal_count is numeric in the ledger because the source sheet stores it as a decimal
  -- (23.0). A fractional count is not a real animal count, so it is truncated rather than
  -- rounded up: mapping must never be asked to hit a target the farm cannot physically meet.
  CASE WHEN d.animal_count IS NULL THEN NULL ELSE floor(d.animal_count)::int END,
  (SELECT count(*)::int FROM goat_sale_allocations a
    WHERE a.tenant_id = d.tenant_id AND a.sales_deal_id = d.id AND a.status = 'tagged')
FROM sales_deals d
WHERE d.tenant_id = $1::uuid AND d.id = $2::uuid`, tenantID, salesDealID).Scan(&status, &declared, &tagged)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrSaleDealNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("salesbridge: read sale deal: %w", err)
	}
	if status == "Deal Failed" {
		// The sales ledger's failed-deal word (sales/domain.StatusDealFailed); a failed sale
		// takes no animals (maintainer decision 2026-09-25). The confirm re-checks under lock.
		return nil, ports.ErrSaleDealFailed
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

// ReadSaleDealFarm returns the farm code a sale was recorded at. One indexed primary-key read.
func (b *Bridge) ReadSaleDealFarm(ctx context.Context, tenantID, salesDealID string) (string, error) {
	var farm string
	err := b.pool.QueryRow(ctx, `
SELECT COALESCE(d.farm, '') FROM sales_deals d
WHERE d.tenant_id = $1::uuid AND d.id = $2::uuid`, tenantID, salesDealID).Scan(&farm)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrSaleDealNotFound
	}
	if err != nil {
		return "", fmt.Errorf("salesbridge: read sale deal farm: %w", err)
	}
	return strings.TrimSpace(farm), nil
}

// ListSaleTaggingDeals is the park head's tag-only queue (maintainer decision 2026-09-11).
//
// It reads the sales ledger through the SAME bridge ReadSaleDeal uses, for the same reason:
// identity's own SQL stays out of the sales schema (migration 000177), and the one row it
// needs -- how many animals a sale is for, and how many are done -- is a fact only the ledger
// side can state. It returns NO buyer and NO money: the queue exists so a person in a pen can
// tag animals and nothing else.
//
// KEYSET on (sale_date DESC, id DESC) with the cursor as "date|id", so page N costs what page
// 1 does; the ledger is a deals-sized table, but the phone still asks for one screenful.
func (b *Bridge) ListSaleTaggingDeals(ctx context.Context, tenantID string, farms []string, limit int, cursor string) ([]ports.SaleTaggingDeal, *string, error) {
	if limit <= 0 || limit > ports.SaleTaggingQueuePageSize {
		limit = ports.SaleTaggingQueuePageSize
	}
	cursorDate, cursorID := "", ""
	if i := strings.IndexByte(cursor, '|'); i > 0 {
		cursorDate, cursorID = cursor[:i], cursor[i+1:]
	}
	// COALESCE + cardinality: a nil Go slice encodes as SQL NULL and cardinality(NULL) is NULL,
	// which would make the whole predicate NULL and return an EMPTY queue for a tenant-wide
	// caller. Empty array means every farm.
	rows, err := b.pool.Query(ctx, `
SELECT d.id::text,
       to_char(d.sale_date, 'YYYY-MM-DD'),
       COALESCE(d.farm, ''),
       COALESCE(d.product_type, ''),
       COALESCE(d.breed, ''),
       floor(d.animal_count)::int,
       (SELECT count(*)::int FROM goat_sale_allocations a
         WHERE a.tenant_id = d.tenant_id AND a.sales_deal_id = d.id AND a.status = 'tagged') AS tagged
FROM sales_deals d
WHERE d.tenant_id = $1::uuid
  AND d.animal_count IS NOT NULL AND floor(d.animal_count) > 0
  -- Only LIVE ANIMAL sales owe animals: manure is not tagged, and a failed deal is history.
  AND COALESCE(d.product_type, '') <> 'Manure'
  AND COALESCE(d.status, '') <> 'Deal Failed'
  AND (COALESCE(cardinality($2::text[]), 0) = 0 OR d.farm = ANY($2::text[]))
  AND (NULLIF($3, '') IS NULL
       OR (d.sale_date, d.id::text) < (NULLIF($3, '')::date, NULLIF($4, '')))
  -- Still owed: fewer tagged than declared.
  AND (SELECT count(*) FROM goat_sale_allocations a
        WHERE a.tenant_id = d.tenant_id AND a.sales_deal_id = d.id AND a.status = 'tagged')
      < floor(d.animal_count)
ORDER BY d.sale_date DESC, d.id::text DESC
LIMIT $5`, tenantID, nonNil(farms), cursorDate, cursorID, limit+1)
	if err != nil {
		return nil, nil, fmt.Errorf("salesbridge: list sale tagging deals: %w", err)
	}
	defer rows.Close()

	out := make([]ports.SaleTaggingDeal, 0, limit)
	for rows.Next() {
		var d ports.SaleTaggingDeal
		if err := rows.Scan(&d.SalesDealID, &d.SaleDate, &d.Farm, &d.ProductType, &d.Breed, &d.DeclaredAnimalCount, &d.AlreadyTagged); err != nil {
			return nil, nil, fmt.Errorf("salesbridge: scan sale tagging deal: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("salesbridge: list sale tagging deals: %w", err)
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		c := last.SaleDate + "|" + last.SalesDealID
		next = &c
	}
	return out, next, nil
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
