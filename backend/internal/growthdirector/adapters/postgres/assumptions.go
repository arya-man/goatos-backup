package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// The Assumptions drawer (maintainer decision 2026-09-19). Two tables, two shapes:
//
//   growth_sale_price_assumptions  APPEND-ONLY, effective-dated (000363): a past window is valued
//                                  at the price that applied then, so a change lands as the row
//                                  effective TODAY and yesterday's window keeps yesterday's price.
//   growth_assumptions             IN PLACE under a row_version fence (000364): the sale-ready
//                                  line and the load-age alert price no past window, so history
//                                  lives in audit_log rather than in the table.
//
// projection-review: growth_assumptions is keyed (tenant_id, key) and read by key; the price read
// is the DISTINCT ON (species, management_stage, sex) already reviewed on salePricesSQL. Nothing here is aggregated.

// The write's statements, hoisted so a plan test can reach them. Each runs at most once per
// species or per key -- the loops below are bounded by the assumption catalog (two species, two
// keys), never by data rows.
const (
	// FOR UPDATE: the compare-and-set below must see the row a concurrent save is about to move.
	// A NULL price is a cleared override (000398) and reads as "no price of its own".
	currentSalePriceSQL = `
SELECT price_per_kg_inr::float8 FROM growth_sale_price_assumptions
WHERE tenant_id = $1::uuid AND species = $2 AND management_stage = $3 AND sex = $4 AND effective_from <= $5::date
ORDER BY effective_from DESC, created_at DESC LIMIT 1
FOR UPDATE`
	upsertSalePriceSQL = `
INSERT INTO growth_sale_price_assumptions (tenant_id, species, management_stage, sex, price_per_kg_inr, effective_from, set_by, note)
VALUES ($1::uuid, $2, $3, $4, $5, $6::date, $7, 'Set from the ADG Analytics Assumptions drawer.')
ON CONFLICT (tenant_id, species, management_stage, sex, effective_from)
DO UPDATE SET price_per_kg_inr = EXCLUDED.price_per_kg_inr, set_by = EXCLUDED.set_by, note = EXCLUDED.note, created_at = now()`
	// An override may only name a stage the tenant's vocabulary holds, spelled as it is stored, so
	// it can ever match an animal's goats.management_stage.
	activeStageCodeSQL  = `SELECT stage_code FROM animal_stage_lookup WHERE tenant_id = $1::uuid AND lower(stage_code) = lower($2) AND status = 'active' ORDER BY sort_order LIMIT 1`
	lockAssumptionSQL   = `SELECT value::float8, value_list::float8[], value_date::text, row_version FROM growth_assumptions WHERE tenant_id = $1::uuid AND key = $2 FOR UPDATE`
	insertAssumptionSQL = `
INSERT INTO growth_assumptions (tenant_id, key, value, value_list, value_date, unit, set_by, updated_at, row_version)
VALUES ($1::uuid, $2, $3, $4, $5::date, $6, $7, now(), 1)`
	updateAssumptionSQL = `
UPDATE growth_assumptions SET value = $3, value_list = $4, value_date = $5::date, set_by = $6, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND key = $2 AND row_version = $7`
	lockSaleReadyLinesSQL = `SELECT pg_advisory_xact_lock(hashtext($1::text || ':growth_sale_ready_lines')::bigint)`
	setterNameSQL         = `SELECT display_name FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active' ORDER BY updated_at DESC LIMIT 1`
)

// stageOptionsSQL is the tenant's active stage vocabulary, in its authored order -- the rows the
// drawer offers a stage x sex price for. Bounded by the tenant's stage catalog (a few dozen rows).
const stageOptionsSQL = `
SELECT stage_code, name FROM animal_stage_lookup
WHERE tenant_id = $1::uuid AND status = 'active'
ORDER BY sort_order, stage_code`

const assumptionValuesSQL = `
SELECT key, value::float8, value_list::float8[], value_date::text, unit, set_by, updated_at, row_version
FROM growth_assumptions
WHERE tenant_id = $1::uuid
ORDER BY key`

// GetAssumptions returns the prices effective on asOf plus every keyed figure the tenant holds.
// includeStages is reserved for the editor drawer; the stage discovery query walks all-time
// weighing evidence and must not ride on the normal Weights/ADG page-load assumptions read.
func (r *Repository) GetAssumptions(ctx context.Context, tenantID string, asOf time.Time, includeStages bool) (domain.Assumptions, error) {
	prices, err := r.GetSalePrices(ctx, tenantID, asOf)
	if err != nil {
		return domain.Assumptions{}, err
	}
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	values, err := readAssumptionValues(ctx, r.pool, tenantID)
	if err != nil {
		return domain.Assumptions{}, err
	}
	if !includeStages {
		return domain.Assumptions{SalePrices: prices.Prices, Values: values, Stages: []domain.StageOption{}}, nil
	}
	stages, err := readStageOptions(ctx, r.pool, tenantID)
	if err != nil {
		return domain.Assumptions{}, err
	}
	weighed, err := r.weighedStageCodes(ctx, tenantID)
	if err != nil {
		return domain.Assumptions{}, err
	}
	return domain.Assumptions{SalePrices: prices.Prices, Values: values, Stages: domain.PriceableStages(stages, weighed, prices.Prices)}, nil
}

// weighedStagesSQL answers "which stages do the weighed animals sit in" (maintainer decision
// 2026-09-24: the drawer lists "only those stages for which weighing done"). Two arms, all time,
// every weighing park:
//
//	scanned   the CURRENT stage of each live animal a scanned tag resolves to;
//	whole pen the current stage of every live animal now in a pen a whole-pen weigh landed on,
//	          through the SAME bucket -> pen bridge the FCR tab uses (fcrScopeCTEs.bucket_pen), because
//	          whole-pen buckets name legacy alias locations that hold no animal of their own.
//
// Only bucket_pen and the two arms are read; Postgres does not evaluate the fragment's other CTEs,
// so the window parameters they carry ($3/$4/$8/$9) are passed wide and cost nothing.
//
// projection-review: membership=distinct stage codes of live goats reached by either arm;
// group_key=management_stage (DISTINCT, no count is reported); join_cardinality=goat_identifiers
// 0..1 per scanned tag (lifetime-unique per tenant), goat_shed_partitions 0..1 per goat (PK), a goat
// reached twice collapses in the DISTINCT; pagination=NONE, the output is bounded by the tenant's
// stage vocabulary; scope=tenant_id on every table and the tenant's weighing parks.
var weighedStagesSQL = ` -- scale-guard:ignore: drawer-open reporting read, output bounded by the stage vocabulary; the scanned arm reads the tenant's weighing rows once, set-based
WITH ` + fcrScopeCTEs + `,
weighed_pens AS (
  SELECT DISTINCT bp.pen_shed_id, bp.pen_key
  FROM bucket_pen bp
  JOIN weighing_shed_observations so ON so.tenant_id = $1::uuid AND so.campaign_shed_id = bp.campaign_shed_id
  WHERE bp.weighing_category = 'per_shed_partition'
    AND so.withdrawn_at IS NULL
    AND so.verification_status <> 'rejected'
),
weighed_stages AS (
  SELECT g.management_stage
  FROM weighed_pens wp
  JOIN goats g ON g.tenant_id = $1::uuid AND g.lifecycle_status = 'alive' AND g.shed_id = wp.pen_shed_id
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1::uuid AND gsp.goat_id = g.goat_id
  WHERE ` + fcrScrubGoatLabel + ` = wp.pen_key
  UNION
  SELECT g.management_stage
  -- One row per distinct tag first (an animal is weighed round after round), on the predicate of
  -- weighing_observations_demo_tag_window_idx; only then resolved through the register.
  FROM (
    SELECT DISTINCT upper(btrim(o.scanned_identifier)) AS tag
    FROM weighing_observations o
    WHERE o.tenant_id = $1::uuid
      AND btrim(o.scanned_identifier) <> ''
      AND o.verification_status <> 'rejected'
  ) t
  JOIN goat_identifiers gi ON gi.tenant_id = $1::uuid AND gi.normalized_value = t.tag
  JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = gi.goat_id AND g.lifecycle_status = 'alive'
)
SELECT DISTINCT management_stage FROM weighed_stages
WHERE management_stage IS NOT NULL AND management_stage <> ''`

const weighingParksSQL = `SELECT COALESCE(array_agg(DISTINCT park_id::text), '{}') FROM weighing_campaigns WHERE tenant_id = $1::uuid AND park_id IS NOT NULL`

func (r *Repository) weighedStageCodes(ctx context.Context, tenantID string) ([]string, error) {
	var parks []string
	if err := r.pool.QueryRow(ctx, weighingParksSQL, tenantID).Scan(&parks); err != nil {
		return nil, err
	}
	if len(parks) == 0 {
		return []string{}, nil
	}
	from := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	bound, err := sqlbind.Bind(weighedStagesSQL, tenantID, parks, from, to, "", []string{}, []string{}, "2000-01-01", "2100-01-01")
	if err != nil {
		return nil, fmt.Errorf("weighed stages bind: %w", err)
	}
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

func readStageOptions(ctx context.Context, q querier, tenantID string) ([]domain.StageOption, error) {
	rows, err := q.Query(ctx, stageOptionsSQL, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.StageOption{}
	for rows.Next() {
		var o domain.StageOption
		if err := rows.Scan(&o.Code, &o.Name); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func readAssumptionValues(ctx context.Context, q querier, tenantID string) ([]domain.AssumptionValue, error) {
	rows, err := q.Query(ctx, assumptionValuesSQL, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AssumptionValue{}
	for rows.Next() {
		var v domain.AssumptionValue
		var updatedAt time.Time
		var value *float64
		var list []float64
		var date *string
		if err := rows.Scan(&v.Key, &value, &list, &date, &v.Unit, &v.SetBy, &updatedAt, &v.RowVersion); err != nil {
			return nil, err
		}
		key, known := domain.LookupAssumptionKey(v.Key)
		if !known {
			continue // a row this build does not know: never rendered, never editable
		}
		v.Kind = key.Kind
		if value != nil {
			v.Value = *value
		}
		v.Values = list
		if date != nil {
			v.Date = *date
		}
		v.UpdatedAt = updatedAt.In(biztime.DefaultLocation()).Format(time.RFC3339)
		out = append(out, v)
	}
	return out, rows.Err()
}

// GrowthSettings resolves the figures the Growth Director and FCR reads take from the
// assumptions, defaulted per key when the tenant has no row. One tiny keyed read per request.
func (r *Repository) GrowthSettings(ctx context.Context, tenantID string) (domain.GrowthSettings, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	values, err := readAssumptionValues(ctx, r.pool, tenantID)
	if err != nil {
		return domain.GrowthSettings{}, err
	}
	return domain.SettingsFrom(values), nil
}

// PutAssumptions lands a validated update in one transaction. The caller has already run
// domain.ValidateAssumptionsUpdate; this only touches rows. Every changed figure writes one
// audit_log row naming before and after, in the same transaction, so a change that cannot be
// audited is not made.
func (r *Repository) PutAssumptions(ctx context.Context, tenantID, setBy string, asOf time.Time, update domain.AssumptionsUpdate) (domain.Assumptions, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Assumptions{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rec := audit.NewTxRecorder(tx)
	effective := asOf.In(biztime.DefaultLocation()).Format("2006-01-02")
	// set_by is rendered verbatim on the drawer ("set by Ravi"), so it carries the person's
	// display name, never their user id: an id on a screen is the copy firewall's ban. The
	// audit row keeps the id as ActorID. A person with no roster name falls back to "".
	setByName := ""
	if err := tx.QueryRow(ctx, setterNameSQL, tenantID, setBy).Scan(&setByName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.Assumptions{}, err
	}

	for _, p := range update.SalePrices { // scale-guard:ignore: bounded by domain.MaxSalePriceUpdates and validated distinct; one row per (species, stage, sex), never per data row
		species, stage, sex := p.Normalized()
		if stage != "" {
			// The stored spelling of the stage, so the override matches goats.management_stage.
			var code string
			err := tx.QueryRow(ctx, activeStageCodeSQL, tenantID, stage).Scan(&code) // scale-guard:ignore: bounded by domain.MaxSalePriceUpdates
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.Assumptions{}, fmt.Errorf("%w: unknown stage %q", ports.ErrInvalidArgument, stage)
			}
			if err != nil {
				return domain.Assumptions{}, err
			}
			stage = code
		}
		var before *float64
		err := tx.QueryRow(ctx, currentSalePriceSQL, tenantID, species, stage, sex, effective).Scan(&before) // scale-guard:ignore: bounded by domain.MaxSalePriceUpdates
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return domain.Assumptions{}, err
		}
		if samePrice(before, p.PricePerKgINR) {
			continue // unchanged (or an exact replay): nothing to append, nothing to audit
		}
		// THE FENCE (PR #320 review): the drawer says which price it loaded, and the save lands
		// only if that is still the price in force. Two editors who both opened ₹425 and both
		// typed a new figure: the first lands, the second is told to reload. The price table is
		// append-only and effective-dated, so the loaded FIGURE is the version.
		if !samePrice(before, p.LoadedPricePerKgINR) {
			return domain.Assumptions{}, fmt.Errorf("%w: sale price %s %s %s", ports.ErrAssumptionConflict, species, stage, sex)
		}
		if _, err := tx.Exec(ctx, upsertSalePriceSQL, tenantID, species, stage, sex, p.PricePerKgINR, effective, setByName); err != nil { // scale-guard:ignore: bounded by domain.MaxSalePriceUpdates
			return domain.Assumptions{}, err
		}
		if err := rec.Record(ctx, audit.Event{
			TenantID: tenantID, ActorID: setBy, ActorType: "user",
			// resource_id is a uuid column: the resource is the tenant's price table, the species,
			// stage and sex travel in metadata and both states.
			Action: "growth.sale_price.set", ResourceType: "growth_sale_price_assumption", ResourceID: tenantID,
			ScopeType: "tenant", ScopeID: tenantID,
			BeforeState: map[string]any{"species": species, "management_stage": stage, "sex": sex, "price_per_kg_inr": before},
			AfterState:  map[string]any{"species": species, "management_stage": stage, "sex": sex, "price_per_kg_inr": p.PricePerKgINR, "effective_from": effective},
			Metadata:    map[string]any{"species": species, "management_stage": stage, "sex": sex},
		}); err != nil {
			return domain.Assumptions{}, err
		}
	}

	if touchesSaleReadyLine(update) {
		if _, err := tx.Exec(ctx, lockSaleReadyLinesSQL, tenantID); err != nil {
			return domain.Assumptions{}, err
		}
		values, err := readAssumptionValues(ctx, tx, tenantID)
		if err != nil {
			return domain.Assumptions{}, err
		}
		settings := domain.SettingsFrom(values)
		lower, threshold := settings.SaleReadyLowerKg, settings.SaleReadyThresholdKg
		for _, v := range update.Values {
			key, _ := domain.LookupAssumptionKey(v.Key)
			switch key.Key {
			case domain.AssumptionSaleReadyLowerKg:
				lower = v.Value
			case domain.AssumptionSaleReadyThresholdKg:
				threshold = v.Value
			}
		}
		if lower >= threshold {
			return domain.Assumptions{}, fmt.Errorf("%w: sale_ready_lower_kg must be below sale_ready_threshold_kg", ports.ErrInvalidArgument)
		}
	}

	for _, v := range update.Values { // scale-guard:ignore: bounded by domain.AssumptionKeys, validated distinct; one row per key, never per data row
		key, _ := domain.LookupAssumptionKey(v.Key)
		// The three typed columns; exactly one is non-nil, by Kind (CHECK growth_assumptions_one_value).
		var newValue *float64
		var newList []float64
		var newDate *string
		var after any
		switch key.Kind {
		case domain.AssumptionKindNumberList:
			newList = append([]float64{}, v.Values...)
			after = newList
		case domain.AssumptionKindDate:
			d := strings.TrimSpace(v.Date)
			newDate = &d
			after = d
		default:
			val := v.Value
			newValue = &val
			after = val
		}
		var before *float64
		var beforeList []float64
		var beforeDate *string
		var version int
		err := tx.QueryRow(ctx, lockAssumptionSQL, tenantID, key.Key).Scan(&before, &beforeList, &beforeDate, &version) // scale-guard:ignore: bounded by domain.AssumptionKeys
		if errors.Is(err, pgx.ErrNoRows) {
			// A tenant seeded before the migrations: the row is born from this write, at version 1.
			if _, err := tx.Exec(ctx, insertAssumptionSQL, tenantID, key.Key, newValue, newList, newDate, key.Unit, setByName); err != nil { // scale-guard:ignore: bounded by domain.AssumptionKeys
				return domain.Assumptions{}, err
			}
			if err := rec.Record(ctx, audit.Event{
				TenantID: tenantID, ActorID: setBy, ActorType: "user",
				Action: "growth.assumption.set", ResourceType: "growth_assumption", ResourceID: tenantID,
				ScopeType: "tenant", ScopeID: tenantID,
				BeforeState: map[string]any{"key": key.Key, "value": nil},
				AfterState:  map[string]any{"key": key.Key, "value": after, "unit": key.Unit},
				Metadata:    map[string]any{"key": key.Key},
			}); err != nil {
				return domain.Assumptions{}, err
			}
			continue
		}
		if err != nil {
			return domain.Assumptions{}, err
		}
		unchanged := sameAssumption(before, beforeList, beforeDate, newValue, newList, newDate)
		if version != v.RowVersion {
			// The fence: someone else changed it since the caller loaded it. A replay of an
			// already-landed update also lands here, and is a no-op rather than a conflict.
			if unchanged {
				continue
			}
			return domain.Assumptions{}, fmt.Errorf("%w: %s", ports.ErrAssumptionConflict, key.Key)
		}
		if unchanged {
			continue
		}
		if _, err := tx.Exec(ctx, updateAssumptionSQL, tenantID, key.Key, newValue, newList, newDate, setByName, v.RowVersion); err != nil { // scale-guard:ignore: bounded by domain.AssumptionKeys
			return domain.Assumptions{}, err
		}
		var beforeAny any
		switch {
		case before != nil:
			beforeAny = *before
		case beforeList != nil:
			beforeAny = beforeList
		case beforeDate != nil:
			beforeAny = *beforeDate
		}
		if err := rec.Record(ctx, audit.Event{
			TenantID: tenantID, ActorID: setBy, ActorType: "user",
			Action: "growth.assumption.set", ResourceType: "growth_assumption", ResourceID: tenantID,
			ScopeType: "tenant", ScopeID: tenantID,
			BeforeState: map[string]any{"key": key.Key, "value": beforeAny},
			AfterState:  map[string]any{"key": key.Key, "value": after, "unit": key.Unit},
			Metadata:    map[string]any{"key": key.Key},
		}); err != nil {
			return domain.Assumptions{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Assumptions{}, err
	}
	return r.GetAssumptions(ctx, tenantID, asOf, true)
}

// samePrice compares two optional prices: both absent, or both present and equal.
func samePrice(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func touchesSaleReadyLine(update domain.AssumptionsUpdate) bool {
	for _, v := range update.Values {
		key, _ := domain.LookupAssumptionKey(v.Key)
		if key.Key == domain.AssumptionSaleReadyLowerKg || key.Key == domain.AssumptionSaleReadyThresholdKg {
			return true
		}
	}
	return false
}

func sameAssumption(bv *float64, bl []float64, bd *string, nv *float64, nl []float64, nd *string) bool {
	switch {
	case nv != nil:
		return bv != nil && *bv == *nv
	case nl != nil:
		if len(bl) != len(nl) {
			return false
		}
		for i := range nl {
			if bl[i] != nl[i] {
				return false
			}
		}
		return true
	case nd != nil:
		return bd != nil && *bd == *nd
	}
	return false
}
