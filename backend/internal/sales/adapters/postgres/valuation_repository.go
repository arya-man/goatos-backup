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
// request by the overview's valuation and Load wise's stock value, written from Sales
// Config under a row_version fence with one audit row per write.

const valuationReadSQL = `
SELECT a.buckets, a.stages, a.unsold_stock_price_rupees::float8, a.row_version,
       to_char(a.updated_at AT TIME ZONE 'Asia/Kolkata', 'DD/MM/YYYY HH24:MI'),
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
		raw       []byte
		rawStages []byte
		out       domain.ValuationAssumptions
		unsold    *float64
	)
	err := r.pool.QueryRow(ctx, valuationReadSQL, tenantID).Scan(&raw, &rawStages, &unsold, &out.RowVersion, &out.UpdatedAt, &out.UpdatedByName)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultValuationAssumptions(), nil
	}
	if err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: read valuation assumptions: %w", err)
	}
	if err := json.Unmarshal(raw, &out.Buckets); err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: decode valuation buckets: %w", err)
	}
	if err := json.Unmarshal(rawStages, &out.Stages); err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: decode valuation stages: %w", err)
	}
	// A row written before 000425 carries no stages; it is valued on the seeded six, which are
	// what its buckets were keyed against.
	if len(out.Stages) == 0 {
		out.Stages = domain.SeededValuationStages
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
	rawStages, err := json.Marshal(write.Stages)
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
INSERT INTO public.sales_valuation_assumptions (tenant_id, buckets, stages, unsold_stock_price_rupees, row_version, updated_at, updated_by)
VALUES ($1::uuid, $2::jsonb, $5::jsonb, $3, 1, now(), nullif($4, '')::uuid)
ON CONFLICT (tenant_id) DO UPDATE SET
  buckets = EXCLUDED.buckets,
  stages = EXCLUDED.stages,
  unsold_stock_price_rupees = EXCLUDED.unsold_stock_price_rupees,
  row_version = public.sales_valuation_assumptions.row_version + 1,
  updated_at = now(),
  updated_by = EXCLUDED.updated_by`,
		tenantID, raw, write.UnsoldStockPriceRupees, actorID, rawStages); err != nil {
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
			"stages":                    json.RawMessage(rawStages),
		},
	}); err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: audit valuation write: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ValuationAssumptions{}, fmt.Errorf("sales: commit valuation write: %w", err)
	}
	return r.GetValuationAssumptions(ctx, tenantID)
}

// stageRegisterSQL is the herd's own stage register with each entry's live head count, offered to
// the valuation screen to pick from (maintainer instruction 2026-09-24). The count is the point:
// it is what tells the farm that Warmup has 58 animals standing in a stage nothing prices.
//
// The join normalizes both sides the one way the valuation SQL and domain.NormalizeStageMatch
// normalize them, so an entry the register spells 'ICU-Kid' counts the animals tagged 'ICU- kid'.
// An animal is counted under its management stage, or its milk cohort when the register lost the
// stage -- the same two columns the valuation files it by.
//
// A nineteen-row register against the live herd, read once when Sales Config opens.
//
// projection-review: membership=public.animal_stage_lookup at (tenant_id, stage_code) grain, one
// row per active register entry -- the register IS the list being shown, so it is also the
// membership source; group_key=the normalized stage text, which `live` groups by BEFORE the join
// so the many side arrives pre-aggregated at exactly one row per key (the unique index
// animal_stage_lookup_code_unique gives the other side one row per key too, making the join 1:0..1
// in both directions); join_cardinality=1:0..1, so no register row can be listed twice and no
// animal can be counted into two entries -- an animal is keyed by its management stage, falling
// back to its milk cohort only when the stage is blank, so it contributes to exactly one key;
// pagination=none, the whole register is the answer; scope=tenant_id on both sides, with the live
// side additionally excluding terminal and merged animals so a sold animal never reads as one
// still standing in a stage.
// scale-guard:ignore: one register read per config page load, both sides grouped before the join
const stageRegisterSQL = `
WITH live AS (
	SELECT upper(regexp_replace(btrim(coalesce(nullif(btrim(g.management_stage), ''), g.milk_cohort, '')), '[^A-Za-z0-9]+', '', 'g')) AS norm,
		count(*)::int AS live_animals
	FROM public.goats g
	WHERE g.tenant_id = $1::uuid
		AND g.lifecycle_status NOT IN ('dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive')
		AND g.merged_into_goat_id IS NULL
	GROUP BY 1
)
SELECT l.stage_code, COALESCE(NULLIF(btrim(l.name), ''), l.stage_code), COALESCE(v.live_animals, 0)
FROM public.animal_stage_lookup l
LEFT JOIN live v ON v.norm = upper(regexp_replace(btrim(l.stage_code), '[^A-Za-z0-9]+', '', 'g'))
WHERE l.tenant_id = $1::uuid AND l.status = 'active'
ORDER BY l.sort_order, l.stage_code`

// ListStageRegister implements ports.SalesRepository.
func (r *Repository) ListStageRegister(ctx context.Context, tenantID string) ([]domain.StageRegisterEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, stageRegisterSQL, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sales: read stage register: %w", err)
	}
	defer rows.Close()
	out := []domain.StageRegisterEntry{}
	for rows.Next() {
		var e domain.StageRegisterEntry
		if err := rows.Scan(&e.Code, &e.Label, &e.LiveAnimals); err != nil {
			return nil, fmt.Errorf("sales: scan stage register: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
