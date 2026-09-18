package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

// ---------------------------------------------------------------------------------------------
// Species and sexes share one shape: a per-tenant code lookup (migration 000346) whose code is
// what goats.species / goats.sex store. The built-in rows are flagged and never leave.

type codeLookupStore struct {
	table    string
	codeCol  string
	goatCol  string
	breedCol string // set when breeds also name the code (species)
}

func (s codeLookupStore) projection() projection {
	return projection{sql: fmt.Sprintf(sqlCodeLookupProjection, s.table, s.codeCol, s.goatCol)}
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const sqlCodeLookupProjection = `
SELECT l.%[2]s AS id,
       l.name AS display,
       l.status,
       l.row_version,
       l.is_builtin,
       jsonb_build_object('name', l.name, 'code', l.%[2]s, 'sort_order', l.sort_order) AS fields,
       '{}'::jsonb AS labels,
       jsonb_build_object('animals', (SELECT count(*) FROM goats g WHERE g.tenant_id = l.tenant_id AND g.%[3]s = l.%[2]s AND g.lifecycle_status = 'alive')) AS counts,
       lpad(l.sort_order::text, 6, '0') || ' ' || lower(l.name) AS sort_key
FROM %[1]s l
WHERE l.tenant_id = $1`

func (s codeLookupStore) count(ctx context.Context, q querier, t string) (int, error) {
	return s.projection().count(ctx, q, t)
}
func (s codeLookupStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return s.projection().list(ctx, q, t, p)
}
func (s codeLookupStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	return s.projection().get(ctx, q, t, id)
}
func (s codeLookupStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return s.projection().options(ctx, q, t)
}

func (s codeLookupStore) usage(ctx context.Context, q querier, t, id string) (domain.Usage, error) {
	checks := []usageCheck{{"animals", fmt.Sprintf(`SELECT count(*) FROM goats WHERE tenant_id = $1 AND %s = $2 AND lifecycle_status = 'alive'`, s.goatCol)}}
	if s.breedCol != "" {
		// breeds is not tenant-scoped: its species column is the code alone.
		checks = append(checks, usageCheck{"breeds", fmt.Sprintf(`SELECT count(*) FROM breeds WHERE %s = $2 AND status = 'active' AND $1::uuid IS NOT NULL`, s.breedCol)})
	}
	return usageOf(ctx, q, t, id, checks...)
}

func (s codeLookupStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	code := domain.FieldString(f, "code")
	sort, ok := domain.FieldInt(f, "sort_order")
	if !ok {
		sort = 100
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (tenant_id, %s, name, sort_order) VALUES ($1, $2, $3, $4)`, s.table, s.codeCol), t, code, domain.FieldString(f, "name"), sort); err != nil {
		return "", err
	}
	return code, nil
}

func (s codeLookupStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	set, args := setClause(f, []colBind{{"name", "name", textOrEmpty("name")}, {"sort_order", "sort_order", func(m map[string]any) any {
		if v, ok := domain.FieldInt(m, "sort_order"); ok {
			return v
		}
		return int64(100)
	}}}, 4)
	if set == "" {
		return "", nil
	}
	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s, updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1 AND %s = $2 AND ($3 = 0 OR row_version = $3)`, s.table, set, s.codeCol), append([]any{t, id, rv}, args...)...)
	if err != nil {
		return "", err
	}
	return "", fenced(ctx, tx, tag.RowsAffected(), fmt.Sprintf(`SELECT 1 FROM %s WHERE tenant_id = $1 AND %s = $2`, s.table, s.codeCol), t, id)
}

func (s codeLookupStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET status = $4, updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1 AND %s = $2 AND ($3 = 0 OR row_version = $3)`, s.table, s.codeCol), t, id, rv, status)
	if err != nil {
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), fmt.Sprintf(`SELECT 1 FROM %s WHERE tenant_id = $1 AND %s = $2`, s.table, s.codeCol), t, id)
}

func (s codeLookupStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	tag, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1 AND %s = $2 AND ($3 = 0 OR row_version = $3) AND NOT is_builtin`, s.table, s.codeCol), t, id, rv)
	if err != nil {
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), fmt.Sprintf(`SELECT 1 FROM %s WHERE tenant_id = $1 AND %s = $2`, s.table, s.codeCol), t, id)
}

// ---------------------------------------------------------------------------------------------
// Lifecycle stages: animal_stage_lookup, whose stage_code is what goats.management_stage stores.

type stageStore struct{}

var stageProjection = projection{sql: `
SELECT l.animal_stage_id::text AS id,
       l.name AS display,
       CASE WHEN l.status = 'active' THEN 'active' ELSE 'archived' END AS status,
       0 AS row_version,
       false AS is_builtin,
       jsonb_build_object('name', l.name, 'code', l.stage_code, 'min_age_days', l.min_age_days, 'max_age_days', l.max_age_days, 'sort_order', l.sort_order) AS fields,
       '{}'::jsonb AS labels,
       jsonb_build_object(
         'animals', (SELECT count(*) FROM goats g WHERE g.tenant_id = l.tenant_id AND g.management_stage = l.stage_code AND g.lifecycle_status = 'alive'),
         'pens', (SELECT count(*) FROM shed_profiles sp WHERE sp.tenant_id = l.tenant_id AND sp.animal_stage_id = l.animal_stage_id)
       ) AS counts,
       lpad(l.sort_order::text, 6, '0') || ' ' || lower(l.name) AS sort_key
FROM animal_stage_lookup l
WHERE l.tenant_id = $1`}

func (stageStore) count(ctx context.Context, q querier, t string) (int, error) {
	return stageProjection.count(ctx, q, t)
}
func (stageStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return stageProjection.list(ctx, q, t, p)
}
func (stageStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	return stageProjection.get(ctx, q, t, id)
}
func (stageStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return stageProjection.options(ctx, q, t)
}
func (stageStore) usage(ctx context.Context, q querier, t, id string) (domain.Usage, error) {
	return usageOf(ctx, q, t, id,
		usageCheck{"animals", `SELECT count(*) FROM goats g JOIN animal_stage_lookup l ON l.tenant_id = g.tenant_id AND l.stage_code = g.management_stage WHERE g.tenant_id = $1 AND l.animal_stage_id = $2::uuid AND g.lifecycle_status = 'alive'`},
		usageCheck{"pens", `SELECT count(*) FROM shed_profiles WHERE tenant_id = $1 AND animal_stage_id = $2::uuid`},
	)
}

func (stageStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	sort, ok := domain.FieldInt(f, "sort_order")
	if !ok {
		sort = 100
	}
	var id string
	err := tx.QueryRow(ctx, sqlAnimals1, t, domain.FieldString(f, "code"), domain.FieldString(f, "name"), nullInt(f, "min_age_days"), nullInt(f, "max_age_days"), sort).Scan(&id)
	return id, err
}

func (stageStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	set, args := setClause(f, []colBind{
		{"name", "name", textOrEmpty("name")},
		{"min_age_days", "min_age_days", intArg("min_age_days")},
		{"max_age_days", "max_age_days", intArg("max_age_days")},
		{"sort_order", "sort_order", func(m map[string]any) any {
			if v, ok := domain.FieldInt(m, "sort_order"); ok {
				return v
			}
			return int64(100)
		}},
	}, 3)
	if set == "" {
		return "", nil
	}
	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE animal_stage_lookup SET %s, updated_at = now() WHERE tenant_id = $1 AND animal_stage_id = $2::uuid`, set), append([]any{t, id}, args...)...)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ports.ErrNotFound
	}
	return "", nil
}

func (stageStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	dbStatus := "inactive"
	if status == domain.StatusActive {
		dbStatus = "active"
	}
	tag, err := tx.Exec(ctx, `UPDATE animal_stage_lookup SET status = $3, updated_at = now() WHERE tenant_id = $1 AND animal_stage_id = $2::uuid`, t, id, dbStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

func (stageStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	tag, err := tx.Exec(ctx, `DELETE FROM animal_stage_lookup WHERE tenant_id = $1 AND animal_stage_id = $2::uuid`, t, id)
	if err != nil {
		return mapWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlAnimals1 = `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, min_age_days, max_age_days, sort_order, status)
VALUES ($1, $2, $3, $4, $5, $6, 'active')
RETURNING animal_stage_id::text`
)
