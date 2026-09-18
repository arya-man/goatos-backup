package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

// projection is the ONE shape every register's read SQL produces, so list / get / count /
// options are written once. Column order is the contract:
//
//	id text, display text, status text ('active' | 'archived'), row_version int,
//	is_builtin bool, fields jsonb, labels jsonb, counts jsonb, sort_key text
//
// $1 is always the tenant. A register's SQL may join what it likes as long as it lands on
// these columns; the wrapper below adds status / search / filter / keyset on top.
type projection struct {
	sql string
	// parentField is the fields key whose value becomes RefOption.ParentID (a pen's park).
	parentField string
	// kindField is the fields key whose value becomes RefOption.Kind (a category's kind).
	kindField string
	// decorate runs on every read row (a partition's display is composed in Go by oploc, never
	// by SQL, so the same animal never reads two ways across surfaces).
	decorate func(*domain.Row)
}

// The registers are tenant-scoped reference catalogs, bounded by the farm's own vocabulary
// (a few hundred places, a few dozen items), never a per-animal or per-event table, so the
// ILIKE search and jsonb filters below run over a bounded set and the page is keyset.
// scale-guard:ignore: bounded tenant reference catalog, keyset paged, never a fact table
const listWrapSQL = `
SELECT id, display, status, row_version, is_builtin, fields, labels, counts, sort_key
FROM (%s) r
WHERE ($2 = 'all' OR r.status = $2)
  AND ($3 = '' OR r.display ILIKE '%%' || $3 || '%%')
  AND ($4::jsonb = '{}'::jsonb OR r.fields @> $4::jsonb)
  AND ($5 = '' OR (r.sort_key, r.id) > (split_part($5, E'\x1f', 1), split_part($5, E'\x1f', 2)))
ORDER BY r.sort_key, r.id
LIMIT $6`

// scale-guard:ignore: bounded tenant reference catalog (see listWrapSQL)
const countWrapSQL = `
SELECT count(*)
FROM (%s) r
WHERE ($2 = 'all' OR r.status = $2)
  AND ($3 = '' OR r.display ILIKE '%%' || $3 || '%%')
  AND ($4::jsonb = '{}'::jsonb OR r.fields @> $4::jsonb)`

const getWrapSQL = `
SELECT id, display, status, row_version, is_builtin, fields, labels, counts, sort_key
FROM (%s) r
WHERE r.id = $2`

const activeCountSQL = `SELECT count(*) FROM (%s) r WHERE r.status = 'active'`

const optionsWrapSQL = `
SELECT id, display, fields
FROM (%s) r
WHERE r.status = 'active'
ORDER BY r.sort_key, r.id`

func scanRow(rows interface{ Scan(dest ...any) error }) (domain.Row, string, error) {
	var (
		row                             domain.Row
		sortKey                         string
		fieldsRaw, labelsRaw, countsRaw []byte
	)
	if err := rows.Scan(&row.ID, &row.Display, &row.Status, &row.RowVersion, &row.IsBuiltin, &fieldsRaw, &labelsRaw, &countsRaw, &sortKey); err != nil {
		return domain.Row{}, "", err
	}
	row.Fields = map[string]any{}
	row.Labels = map[string]string{}
	if len(fieldsRaw) > 0 {
		if err := json.Unmarshal(fieldsRaw, &row.Fields); err != nil {
			return domain.Row{}, "", fmt.Errorf("decode fields: %w", err)
		}
	}
	if len(labelsRaw) > 0 {
		if err := json.Unmarshal(labelsRaw, &row.Labels); err != nil {
			return domain.Row{}, "", fmt.Errorf("decode labels: %w", err)
		}
	}
	if len(countsRaw) > 0 && string(countsRaw) != "null" {
		row.Counts = map[string]int{}
		if err := json.Unmarshal(countsRaw, &row.Counts); err != nil {
			return domain.Row{}, "", fmt.Errorf("decode counts: %w", err)
		}
	}
	return row, sortKey, nil
}

func (p projection) count(ctx context.Context, q querier, tenantID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, fmt.Sprintf(activeCountSQL, p.sql), tenantID).Scan(&n)
	return n, err
}

func (p projection) list(ctx context.Context, q querier, tenantID string, lp ports.ListParams) (ports.Page, error) {
	filters := map[string]any{}
	for k, v := range lp.Filters {
		if strings.TrimSpace(v) != "" {
			filters[k] = v
		}
	}
	filterJSON, err := json.Marshal(filters)
	if err != nil {
		return ports.Page{}, err
	}
	status := lp.Status
	if status == "" {
		status = domain.StatusActive
	}
	rows, err := q.Query(ctx, fmt.Sprintf(listWrapSQL, p.sql), tenantID, status, strings.TrimSpace(lp.Query), string(filterJSON), lp.Cursor, lp.Limit+1)
	if err != nil {
		return ports.Page{}, err
	}
	defer rows.Close()
	out := ports.Page{Rows: make([]domain.Row, 0, lp.Limit)}
	lastSort := ""
	for rows.Next() {
		row, sortKey, err := scanRow(rows)
		if err != nil {
			return ports.Page{}, err
		}
		if len(out.Rows) == lp.Limit {
			out.NextCursor = lastSort
			break
		}
		if p.decorate != nil {
			p.decorate(&row)
		}
		out.Rows = append(out.Rows, row)
		lastSort = sortKey + "\x1f" + row.ID
	}
	if err := rows.Err(); err != nil {
		return ports.Page{}, err
	}
	if err := q.QueryRow(ctx, fmt.Sprintf(countWrapSQL, p.sql), tenantID, status, strings.TrimSpace(lp.Query), string(filterJSON)).Scan(&out.Total); err != nil {
		return ports.Page{}, err
	}
	return out, nil
}

func (p projection) get(ctx context.Context, q querier, tenantID, id string) (domain.Row, error) {
	row, _, err := scanRow(q.QueryRow(ctx, fmt.Sprintf(getWrapSQL, p.sql), tenantID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Row{}, ports.ErrNotFound
		}
		return domain.Row{}, err
	}
	if p.decorate != nil {
		p.decorate(&row)
	}
	return row, nil
}

func (p projection) options(ctx context.Context, q querier, tenantID string) ([]ports.RefOption, error) {
	rows, err := q.Query(ctx, fmt.Sprintf(optionsWrapSQL, p.sql), tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.RefOption{}
	for rows.Next() {
		var opt ports.RefOption
		var fieldsRaw []byte
		if err := rows.Scan(&opt.ID, &opt.Label, &fieldsRaw); err != nil {
			return nil, err
		}
		if p.decorate != nil {
			row := domain.Row{ID: opt.ID, Display: opt.Label, Fields: map[string]any{}}
			_ = json.Unmarshal(fieldsRaw, &row.Fields)
			p.decorate(&row)
			opt.Label = row.Display
		}
		if p.parentField != "" || p.kindField != "" {
			fields := map[string]any{}
			_ = json.Unmarshal(fieldsRaw, &fields)
			if p.parentField != "" {
				opt.ParentID = domain.FieldString(fields, p.parentField)
			}
			if p.kindField != "" {
				opt.Kind = domain.FieldString(fields, p.kindField)
			}
		}
		out = append(out, opt)
	}
	return out, rows.Err()
}

// usageOf runs each (noun, sql) pair with ($1 tenant, $2 id) and collects the counts.
func usageOf(ctx context.Context, q querier, tenantID, id string, checks ...usageCheck) (domain.Usage, error) {
	out := domain.Usage{Uses: make([]domain.UsageCount, 0, len(checks))}
	for _, c := range checks {
		var n int
		if err := q.QueryRow(ctx, c.sql, tenantID, id).Scan(&n); err != nil {
			return domain.Usage{}, fmt.Errorf("usage %s: %w", c.noun, err)
		}
		out.Uses = append(out.Uses, domain.UsageCount{Noun: c.noun, Count: n})
		if n > 0 {
			out.Blocked = true
		}
	}
	return out, nil
}

type usageCheck struct {
	noun string
	sql  string
}

// fenced turns a zero-row UPDATE into the right refusal: the row is gone, or its version moved.
func fenced(ctx context.Context, q querier, affected int64, existsSQL string, args ...any) error {
	if affected > 0 {
		return nil
	}
	var one int
	if err := q.QueryRow(ctx, existsSQL, args...).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.ErrNotFound
		}
		return err
	}
	return ports.ErrVersionConflict
}

// nullText is a *string for a blank field.
func nullText(fields map[string]any, key string) *string {
	s := domain.FieldString(fields, key)
	if s == "" {
		return nil
	}
	return &s
}

// nullInt is a *int64 for an absent number.
func nullInt(fields map[string]any, key string) *int64 {
	v, ok := domain.FieldInt(fields, key)
	if !ok {
		return nil
	}
	return &v
}

// sent reports whether the write named the field at all (nil means "clear it").
func sent(fields map[string]any, key string) bool {
	_, ok := fields[key]
	return ok
}

// setClause builds "col = $n" pairs for the fields the write actually sent, so an update touches
// only what changed. Each entry maps a field key to its column and the arg builder.
type colBind struct {
	field string
	col   string
	arg   func(fields map[string]any) any
}

func setClause(fields map[string]any, binds []colBind, startArg int) (string, []any) {
	parts := []string{}
	args := []any{}
	n := startArg
	for _, b := range binds {
		if !sent(fields, b.field) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s = $%d", b.col, n))
		args = append(args, b.arg(fields))
		n++
	}
	return strings.Join(parts, ", "), args
}

func textArg(key string) func(map[string]any) any {
	return func(f map[string]any) any { return nullText(f, key) }
}

func intArg(key string) func(map[string]any) any {
	return func(f map[string]any) any { return nullInt(f, key) }
}

func boolArg(key string) func(map[string]any) any {
	return func(f map[string]any) any { return domain.FieldBool(f, key) }
}

func textOrEmpty(key string) func(map[string]any) any {
	return func(f map[string]any) any { return domain.FieldString(f, key) }
}
