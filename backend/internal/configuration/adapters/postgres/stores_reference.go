package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

// Reference lists (maintainer instruction 2026-09-18, migration 000348): the farm's small
// vocabularies. Three keep tables of their own (status_definitions, sop_categories,
// sop_task_types); everything else -- and every list the farm adds on screen -- lives in
// reference_lists + reference_list_entries, and each list renders as its own register.

// ---------------------------------------------------------------------------------------------
// Status definitions: product-wide (no tenant column), codes named in Go -> every row is built
// in (rename only). $1 is consumed so the shared wrapper's arguments line up.

type statusDefinitionStore struct{}

var statusDefinitionProjection = projection{sql: sqlStatusDefinitionProjection}

const sqlStatusDefinitionProjection = `
SELECT s.status_code AS id,
       s.display_name AS display,
       CASE WHEN s.active THEN 'active' ELSE 'archived' END AS status,
       0 AS row_version,
       true AS is_builtin,
       jsonb_build_object('name', s.display_name, 'axis', s.axis, 'code', s.status_code, 'short_label', s.short_label,
                          'description', NULLIF(s.description, ''), 'expected_duration_days', s.expected_duration_days, 'sort_order', s.sort_order) AS fields,
       '{}'::jsonb AS labels,
       NULL::jsonb AS counts,
       s.axis || ' ' || lpad(s.sort_order::text, 6, '0') || ' ' || lower(s.display_name) AS sort_key
FROM status_definitions s
WHERE $1::uuid IS NOT NULL`

func (statusDefinitionStore) count(ctx context.Context, q querier, t string) (int, error) {
	return statusDefinitionProjection.count(ctx, q, t)
}
func (statusDefinitionStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return statusDefinitionProjection.list(ctx, q, t, p)
}
func (statusDefinitionStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	return statusDefinitionProjection.get(ctx, q, t, id)
}
func (statusDefinitionStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return statusDefinitionProjection.options(ctx, q, t)
}
func (statusDefinitionStore) usage(context.Context, querier, string, string) (domain.Usage, error) {
	return domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "product rules", Count: 1}}}, nil
}

func (statusDefinitionStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	sort, ok := domain.FieldInt(f, "sort_order")
	if !ok {
		sort = 100
	}
	code := domain.FieldString(f, "code")
	if _, err := tx.Exec(ctx, sqlStatusDefinitionInsert, code, domain.FieldString(f, "axis"), domain.FieldString(f, "name"), domain.FieldString(f, "short_label"), domain.FieldString(f, "description"), sort, nullInt(f, "expected_duration_days")); err != nil {
		return "", mapWriteError(err)
	}
	return code, nil
}

func (statusDefinitionStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	set, args := setClause(f, []colBind{
		{"name", "display_name", textOrEmpty("name")},
		{"axis", "axis", textOrEmpty("axis")},
		{"short_label", "short_label", textOrEmpty("short_label")},
		{"description", "description", textArg("description")},
		{"expected_duration_days", "expected_duration_days", intArg("expected_duration_days")},
		{"sort_order", "sort_order", sortArg},
	}, 2)
	if set == "" {
		return "", nil
	}
	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE status_definitions SET %s WHERE status_code = $1`, set), append([]any{id}, args...)...)
	if err != nil {
		return "", mapWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return "", ports.ErrNotFound
	}
	return "", nil
}

func (statusDefinitionStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	tag, err := tx.Exec(ctx, `UPDATE status_definitions SET active = $2 WHERE status_code = $1`, id, status == domain.StatusActive)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

func (statusDefinitionStore) del(context.Context, pgx.Tx, string, string, int) error {
	return domain.ErrBuiltin
}

// ---------------------------------------------------------------------------------------------
// Tenant vocabularies keyed (tenant_id, <key column>): sop_categories, sop_task_types,
// reference_lists, and the entries of one reference list. One store shape, parametrised.

type keyedStore struct {
	table   string
	keyCol  string
	extra   []string // extra columns carried in fields verbatim
	listKey string   // reference_list_entries only: the list this register renders
	checks  []usageCheck
}

func (s keyedStore) projection() projection {
	fields := "'name', r.name, 'code', r." + s.keyCol + ", 'description', NULLIF(r.description, ''), 'sort_order', r.sort_order"
	for _, col := range s.extra {
		fields += ", '" + col + "', r." + col
	}
	where := "r.tenant_id = $1"
	if s.listKey != "" {
		where += " AND r.list_key = '" + s.listKey + "'"
	}
	builtin := "false"
	if s.table == "reference_lists" || s.table == "reference_list_entries" {
		builtin = "r.is_builtin"
	}
	return projection{sql: fmt.Sprintf(sqlKeyedProjection, s.table, s.keyCol, fields, where, builtin)}
}

// sqlKeyedProjection: %[1]s table, %[2]s key column, %[3]s field pairs, %[4]s where, %[5]s builtin expr.
const sqlKeyedProjection = `
SELECT r.%[2]s AS id,
       r.name AS display,
       replace(r.status, 'retired', 'archived') AS status,
       r.row_version,
       %[5]s AS is_builtin,
       jsonb_build_object(%[3]s) AS fields,
       '{}'::jsonb AS labels,
       NULL::jsonb AS counts,
       lpad(r.sort_order::text, 6, '0') || ' ' || lower(r.name) AS sort_key
FROM %[1]s r
WHERE %[4]s`

func (s keyedStore) count(ctx context.Context, q querier, t string) (int, error) {
	return s.projection().count(ctx, q, t)
}
func (s keyedStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return s.projection().list(ctx, q, t, p)
}
func (s keyedStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	return s.projection().get(ctx, q, t, id)
}
func (s keyedStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return s.projection().options(ctx, q, t)
}
func (s keyedStore) usage(ctx context.Context, q querier, t, id string) (domain.Usage, error) {
	if len(s.checks) == 0 {
		return domain.Usage{Uses: []domain.UsageCount{}}, nil
	}
	return usageOf(ctx, q, t, id, s.checks...)
}

func (s keyedStore) where(id string) (string, []any) {
	if s.listKey != "" {
		return "tenant_id = $1 AND list_key = $2 AND " + s.keyCol + " = $3", []any{s.listKey, id}
	}
	return "tenant_id = $1 AND " + s.keyCol + " = $2", []any{id}
}

func (s keyedStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	sort, ok := domain.FieldInt(f, "sort_order")
	if !ok {
		sort = 100
	}
	code := domain.FieldString(f, "code")
	if code == "" {
		code = domain.NormalizeCode(domain.FieldString(f, "name"))
	}
	cols := "tenant_id, " + s.keyCol + ", name, description, sort_order"
	vals := "$1, $2, $3, $4, $5"
	args := []any{t, code, domain.FieldString(f, "name"), domain.FieldString(f, "description"), sort}
	if s.listKey != "" {
		cols += ", list_key"
		vals += ", $6"
		args = append(args, s.listKey)
	}
	for _, col := range s.extra {
		if v := domain.FieldString(f, col); v != "" {
			cols += ", " + col
			vals += fmt.Sprintf(", $%d", len(args)+1)
			args = append(args, v)
		}
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, s.table, cols, vals), args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			field := "code"
			if pgErr.ConstraintName != "" && (pgErr.ConstraintName[len(pgErr.ConstraintName)-9:] == "name_uidx") {
				field = "name"
			}
			return "", &ports.DuplicateError{Field: field, Message: "An entry with that " + field + " already exists."}
		}
		return "", err
	}
	return code, nil
}

func (s keyedStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	where, whereArgs := s.where(id)
	binds := []colBind{{"name", "name", textOrEmpty("name")}, {"description", "description", func(m map[string]any) any { return domain.FieldString(m, "description") }}, {"sort_order", "sort_order", sortArg}}
	for _, col := range s.extra {
		binds = append(binds, colBind{col, col, textOrEmpty(col)})
	}
	set, args := setClause(f, binds, len(whereArgs)+2)
	if set == "" {
		return "", nil
	}
	fence := fmt.Sprintf("($%d = 0 OR row_version = $%d)", len(whereArgs)+2+len(args), len(whereArgs)+2+len(args))
	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s, updated_at = now(), row_version = row_version + 1 WHERE %s AND %s`, s.table, set, where, fence), append(append(append([]any{t}, whereArgs...), args...), rv)...)
	if err != nil {
		return "", mapWriteError(err)
	}
	return "", fenced(ctx, tx, tag.RowsAffected(), fmt.Sprintf(`SELECT 1 FROM %s WHERE %s`, s.table, where), append([]any{t}, whereArgs...)...)
}

func (s keyedStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	where, whereArgs := s.where(id)
	dbStatus := status
	if s.table == "sop_categories" || s.table == "sop_task_types" {
		// Those two keep the older active|retired vocabulary.
		if status == domain.StatusArchived {
			dbStatus = "retired"
		}
	}
	n := len(whereArgs) + 2
	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET status = $%d, updated_at = now(), row_version = row_version + 1 WHERE %s AND ($%d = 0 OR row_version = $%d)`, s.table, n, where, n+1, n+1), append(append([]any{t}, whereArgs...), dbStatus, rv)...)
	if err != nil {
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), fmt.Sprintf(`SELECT 1 FROM %s WHERE %s`, s.table, where), append([]any{t}, whereArgs...)...)
}

func (s keyedStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	where, whereArgs := s.where(id)
	n := len(whereArgs) + 2
	tag, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s AND ($%d = 0 OR row_version = $%d)`, s.table, where, n, n), append(append([]any{t}, whereArgs...), rv)...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return &ports.InUseError{Usage: domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "records; archive it instead", Count: 1}}}}
		}
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), fmt.Sprintf(`SELECT 1 FROM %s WHERE %s`, s.table, where), append([]any{t}, whereArgs...)...)
}

func sortArg(m map[string]any) any {
	if v, ok := domain.FieldInt(m, "sort_order"); ok {
		return v
	}
	return int64(100)
}

// The concrete keyed stores.
var (
	sopCategoryStore   = keyedStore{table: "sop_categories", keyCol: "category_key"}
	taskTypeStore      = keyedStore{table: "sop_task_types", keyCol: "task_type_key", extra: []string{"answer_kind"}}
	referenceListStore = keyedStore{table: "reference_lists", keyCol: "list_key", checks: []usageCheck{{"entries", `SELECT count(*) FROM reference_list_entries WHERE tenant_id = $1 AND list_key = $2`}}}
)

func referenceEntryStore(listKey string) store {
	return keyedStore{table: "reference_list_entries", keyCol: "entry_code", listKey: listKey}
}

// ReferenceLists is the tenant's own vocabularies, for the register catalog.
func (r *Repository) ReferenceLists(ctx context.Context, tenantID string) ([]domain.ReferenceList, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlReferenceLists, tenantID)
	if err != nil {
		return nil, fmt.Errorf("configuration: reference lists: %w", err)
	}
	defer rows.Close()
	out := []domain.ReferenceList{}
	for rows.Next() {
		var l domain.ReferenceList
		if err := rows.Scan(&l.Key, &l.Name, &l.Description, &l.SortOrder, &l.IsBuiltin); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlReferenceLists = `
SELECT list_key, name, description, sort_order, is_builtin
FROM reference_lists
WHERE tenant_id = $1 AND status = 'active'
ORDER BY sort_order, lower(name)`
	sqlStatusDefinitionInsert = `
INSERT INTO status_definitions (status_code, axis, display_name, short_label, description, sort_order, expected_duration_days, active)
VALUES ($1, $2, $3, $4, $5, $6, $7, true)`
)
