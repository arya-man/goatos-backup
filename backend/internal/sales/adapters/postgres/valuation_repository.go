package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// FARM VALUATION ASSUMPTIONS (maintainer instruction 2026-09-19): one row per tenant, read per
// request by the overview's valuation and Load wise's unsold-stock price, written from Sales
// Config under a row_version fence with one audit row per write.

const valuationReadSQL = `
SELECT a.buckets, a.unsold_stock_price_rupees::float8, a.row_version,
       to_char(a.updated_at AT TIME ZONE 'Asia/Kolkata', 'DD-MM-YYYY HH24:MI'),
       COALESCE(m.display_name, '')
FROM public.sales_valuation_assumptions a
LEFT JOIN public.workforce_members m ON m.tenant_id = a.tenant_id AND m.user_id = a.updated_by AND m.status = 'active'
WHERE a.tenant_id = $1::uuid`

// GetValuationAssumptions implements ports.SalesRepository. A tenant with no row (created after
// migration 000367) reads the seeded defaults, row_version 0, so the page always has figures.
func (r *Repository) GetValuationAssumptions(ctx context.Context, tenantID string) (domain.ValuationAssumptions, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var (
		raw    []byte
		out    domain.ValuationAssumptions
		unsold *float64
	)
	err := r.pool.QueryRow(ctx, valuationReadSQL, tenantID).Scan(&raw, &unsold, &out.RowVersion, &out.UpdatedAt, &out.UpdatedByName)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultValuationAssumptions(), nil
	}
	if err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: read valuation assumptions: %w", err)
	}
	if err := json.Unmarshal(raw, &out.Buckets); err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: decode valuation buckets: %w", err)
	}
	out.UnsoldStockPriceRupees = unsold
	return out, nil
}

// PutValuationAssumptions implements ports.SalesRepository: a whole-row replace, fenced on the
// row_version the screen loaded (ErrValuationVersionConflict on a stale one), inserting the row
// for a tenant the migration did not reach. The audit row and the write commit together.
func (r *Repository) PutValuationAssumptions(ctx context.Context, tenantID string, write domain.ValuationAssumptions, actorID string) (domain.ValuationAssumptions, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	raw, err := json.Marshal(write.Buckets)
	if err != nil {
		return domain.ValuationAssumptions{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: begin valuation write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current int
	err = tx.QueryRow(ctx, `SELECT row_version FROM public.sales_valuation_assumptions WHERE tenant_id = $1::uuid FOR UPDATE`, tenantID).Scan(&current)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		current = 0
	case err != nil:
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: lock valuation row: %w", err)
	}
	if current != write.RowVersion {
		return domain.ValuationAssumptions{}, ports.ErrValuationVersionConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO public.sales_valuation_assumptions (tenant_id, buckets, unsold_stock_price_rupees, row_version, updated_at, updated_by)
VALUES ($1::uuid, $2::jsonb, $3, 1, now(), nullif($4, '')::uuid)
ON CONFLICT (tenant_id) DO UPDATE SET
  buckets = EXCLUDED.buckets,
  unsold_stock_price_rupees = EXCLUDED.unsold_stock_price_rupees,
  row_version = public.sales_valuation_assumptions.row_version + 1,
  updated_at = now(),
  updated_by = EXCLUDED.updated_by`,
		tenantID, raw, write.UnsoldStockPriceRupees, actorID); err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: write valuation assumptions: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "sales.valuation_assumptions.update",
		ResourceType: "sales_valuation_assumptions",
		ResourceID:   tenantID,
		Metadata: map[string]any{
			"domain":                    "sales",
			"module":                    "sales",
			"unsold_stock_price_rupees": write.UnsoldStockPriceRupees,
			"buckets":                   json.RawMessage(raw),
		},
	}); err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: audit valuation write: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: commit valuation write: %w", err)
	}
	return r.GetValuationAssumptions(ctx, tenantID)
}
