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
)

// The Assumptions drawer (maintainer decision 2026-09-19). Two tables, two shapes:
//
//   growth_sale_price_assumptions  APPEND-ONLY, effective-dated (000249): a past window is valued
//                                  at the price that applied then, so a change lands as the row
//                                  effective TODAY and yesterday's window keeps yesterday's price.
//   growth_assumptions             IN PLACE under a row_version fence (000252): the sale-ready
//                                  line and the load-age alert price no past window, so history
//                                  lives in audit_log rather than in the table.
//
// projection-review: growth_assumptions is keyed (tenant_id, key) and read by key; the price read
// is the DISTINCT ON (species) already reviewed on salePricesSQL. Nothing here is aggregated.

// The write's statements, hoisted so a plan test can reach them. Each runs at most once per
// species or per key -- the loops below are bounded by the assumption catalog (two species, two
// keys), never by data rows.
const (
	// FOR UPDATE: the compare-and-set below must see the row a concurrent save is about to move.
	currentSalePriceSQL = `
SELECT price_per_kg_inr::float8 FROM growth_sale_price_assumptions
WHERE tenant_id = $1::uuid AND species = $2 AND effective_from <= $3::date
ORDER BY effective_from DESC, created_at DESC LIMIT 1
FOR UPDATE`
	upsertSalePriceSQL = `
INSERT INTO growth_sale_price_assumptions (tenant_id, species, price_per_kg_inr, effective_from, set_by, note)
VALUES ($1::uuid, $2, $3, $4::date, $5, 'Set from the ADG Analytics Assumptions drawer.')
ON CONFLICT (tenant_id, species, effective_from)
DO UPDATE SET price_per_kg_inr = EXCLUDED.price_per_kg_inr, set_by = EXCLUDED.set_by, note = EXCLUDED.note, created_at = now()`
	lockAssumptionSQL   = `SELECT value::float8, value_list::float8[], value_date::text, row_version FROM growth_assumptions WHERE tenant_id = $1::uuid AND key = $2 FOR UPDATE`
	insertAssumptionSQL = `
INSERT INTO growth_assumptions (tenant_id, key, value, value_list, value_date, unit, set_by, updated_at, row_version)
VALUES ($1::uuid, $2, $3, $4, $5::date, $6, $7, now(), 1)`
	updateAssumptionSQL = `
UPDATE growth_assumptions SET value = $3, value_list = $4, value_date = $5::date, set_by = $6, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND key = $2 AND row_version = $7`
	setterNameSQL = `SELECT display_name FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active' ORDER BY updated_at DESC LIMIT 1`
)

const assumptionValuesSQL = `
SELECT key, value::float8, value_list::float8[], value_date::text, unit, set_by, updated_at, row_version
FROM growth_assumptions
WHERE tenant_id = $1::uuid
ORDER BY key`

// GetAssumptions returns the prices effective on asOf plus every keyed figure the tenant holds.
func (r *Repository) GetAssumptions(ctx context.Context, tenantID string, asOf time.Time) (domain.Assumptions, error) {
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
	return domain.Assumptions{SalePrices: prices.Prices, Values: values}, nil
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

	for _, p := range update.SalePrices { // scale-guard:ignore: bounded by domain.SalePriceSpecies (2), validated distinct; one row per species, never per data row
		species := strings.ToLower(strings.TrimSpace(p.Species))
		var before *float64
		var prev float64
		err := tx.QueryRow(ctx, currentSalePriceSQL, tenantID, species, effective).Scan(&prev) // scale-guard:ignore: bounded by domain.SalePriceSpecies (2)
		switch {
		case err == nil:
			before = &prev
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return domain.Assumptions{}, err
		}
		if before != nil && *before == p.PricePerKgINR {
			continue // unchanged (or an exact replay): nothing to append, nothing to audit
		}
		// THE FENCE (PR #320 review): the drawer says which price it loaded, and the save lands
		// only if that is still the price in force. Two editors who both opened ₹425 and both
		// typed a new figure: the first lands, the second is told to reload. The price table is
		// append-only and effective-dated, so the loaded FIGURE is the version.
		switch {
		case before == nil && p.LoadedPricePerKgINR != nil:
			return domain.Assumptions{}, fmt.Errorf("%w: sale price %s", ports.ErrAssumptionConflict, species)
		case before != nil && (p.LoadedPricePerKgINR == nil || *p.LoadedPricePerKgINR != *before):
			return domain.Assumptions{}, fmt.Errorf("%w: sale price %s", ports.ErrAssumptionConflict, species)
		}
		if _, err := tx.Exec(ctx, upsertSalePriceSQL, tenantID, species, p.PricePerKgINR, effective, setByName); err != nil { // scale-guard:ignore: bounded by domain.SalePriceSpecies (2)
			return domain.Assumptions{}, err
		}
		if err := rec.Record(ctx, audit.Event{
			TenantID: tenantID, ActorID: setBy, ActorType: "user",
			// resource_id is a uuid column: the resource is the tenant's price table, the species
			// travels in metadata and both states.
			Action: "growth.sale_price.set", ResourceType: "growth_sale_price_assumption", ResourceID: tenantID,
			ScopeType: "tenant", ScopeID: tenantID,
			BeforeState: map[string]any{"species": species, "price_per_kg_inr": before},
			AfterState:  map[string]any{"species": species, "price_per_kg_inr": p.PricePerKgINR, "effective_from": effective},
			Metadata:    map[string]any{"species": species},
		}); err != nil {
			return domain.Assumptions{}, err
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
	return r.GetAssumptions(ctx, tenantID, asOf)
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
