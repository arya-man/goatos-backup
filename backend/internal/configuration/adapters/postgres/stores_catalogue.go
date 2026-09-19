package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

// ---------------------------------------------------------------------------------------------
// Item categories: the editable tree (migration 000346). A row's KIND is its root's item_kind,
// resolved by the recursive walk below, so the whole subtree is one kind and inventory_items can
// keep its enum column for the consumers that key on it.

type categoryStore struct{}

var categoryProjection = projection{parentField: "parent_id", kindField: "kind", sql: `
WITH RECURSIVE tree AS (
  SELECT c.category_id, c.tenant_id, c.parent_category_id, c.name, c.item_kind, c.sort_order, c.status, c.is_builtin, c.row_version,
         c.name::text AS path, c.item_kind AS kind, 1 AS depth,
         lpad(c.sort_order::text, 6, '0') || ' ' || lower(c.name) AS sort_key
  FROM item_categories c
  WHERE c.tenant_id = $1 AND c.parent_category_id IS NULL
  UNION ALL
  SELECT c.category_id, c.tenant_id, c.parent_category_id, c.name, c.item_kind, c.sort_order, c.status, c.is_builtin, c.row_version,
         t.path || ' › ' || c.name, t.kind, t.depth + 1,
         t.sort_key || ' / ' || lpad(c.sort_order::text, 6, '0') || ' ' || lower(c.name)
  FROM item_categories c
  JOIN tree t ON t.tenant_id = c.tenant_id AND t.category_id = c.parent_category_id
  WHERE t.depth < 8
)
SELECT t.category_id::text AS id,
       t.path AS display,
       t.status,
       t.row_version,
       t.is_builtin,
       jsonb_build_object('name', t.name, 'parent_id', t.parent_category_id::text, 'kind', t.kind, 'sort_order', t.sort_order, 'depth', t.depth) AS fields,
       jsonb_strip_nulls(jsonb_build_object('parent_id', p.name)) AS labels,
       jsonb_build_object(
         'items', (SELECT count(*) FROM inventory_items i WHERE i.tenant_id = t.tenant_id AND i.category_id = t.category_id AND i.status = 'active')
                  + (SELECT count(*) FROM feed_item_catalog f WHERE f.tenant_id = t.tenant_id AND f.status = 'active' AND t.parent_category_id IS NULL AND t.item_kind = 'feed' AND t.is_builtin),
         'subcategories', (SELECT count(*) FROM item_categories x WHERE x.tenant_id = t.tenant_id AND x.parent_category_id = t.category_id AND x.status = 'active')
       ) AS counts,
       t.sort_key
FROM tree t
LEFT JOIN item_categories p ON p.tenant_id = t.tenant_id AND p.category_id = t.parent_category_id`}

func (categoryStore) count(ctx context.Context, q querier, t string) (int, error) {
	return categoryProjection.count(ctx, q, t)
}
func (categoryStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return categoryProjection.list(ctx, q, t, p)
}
func (categoryStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	if !isUUID(id) {
		return domain.Row{}, ports.ErrNotFound
	}
	return categoryProjection.get(ctx, q, t, id)
}
func (categoryStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return categoryProjection.options(ctx, q, t)
}
func (categoryStore) usage(ctx context.Context, q querier, t, id string) (domain.Usage, error) {
	if !isUUID(id) {
		return domain.Usage{}, ports.ErrNotFound
	}
	return usageOf(ctx, q, t, id,
		usageCheck{"items", `SELECT count(*) FROM inventory_items WHERE tenant_id = $1 AND category_id = $2::uuid AND status = 'active'`},
		usageCheck{"subcategories", `SELECT count(*) FROM item_categories WHERE tenant_id = $1 AND parent_category_id = $2::uuid AND status = 'active'`},
	)
}

// categoryKind is the effective kind of a category (its root's), or ErrNotFound.
func categoryKind(ctx context.Context, q querier, t, id string) (string, error) {
	if !isUUID(id) {
		return "", ports.ErrNotFound
	}
	var kind string
	err := q.QueryRow(ctx, sqlCatalogue1, t, id).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrNotFound
	}
	return kind, err
}

func (categoryStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	name := domain.FieldString(f, "name")
	parent := nullText(f, "parent_id")
	var kind *string
	if parent == nil {
		k := domain.FieldString(f, "kind")
		if k == "" {
			return "", &domain.ValidationError{Fields: []domain.FieldError{{Field: "kind", Code: "required", Message: "Kind is required for a top-level category."}}}
		}
		kind = &k
	} else {
		parentKind, err := categoryKind(ctx, tx, t, *parent)
		if err != nil {
			if errors.Is(err, ports.ErrNotFound) {
				return "", &ports.RefError{Field: "parent_id", Label: "Category"}
			}
			return "", err
		}
		if k := domain.FieldString(f, "kind"); k != "" && k != parentKind {
			return "", &domain.ValidationError{Fields: []domain.FieldError{{Field: "kind", Code: "invalid", Message: "A subcategory is the kind of the category above it."}}}
		}
	}
	sort, ok := domain.FieldInt(f, "sort_order")
	if !ok {
		sort = 100
	}
	var id string
	err := tx.QueryRow(ctx, sqlCatalogue2, t, parent, name, kind, sort).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", &ports.DuplicateError{Field: "name", Message: "A category with that name already exists at this level."}
		}
		return "", err
	}
	return id, nil
}

func (categoryStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	if !isUUID(id) {
		return "", ports.ErrNotFound
	}
	if sent(f, "parent_id") {
		parent := nullText(f, "parent_id")
		if parent != nil {
			if *parent == id {
				return "", &domain.ValidationError{Fields: []domain.FieldError{{Field: "parent_id", Code: "invalid", Message: "A category cannot sit under itself."}}}
			}
			// The new parent must not be a descendant, or the tree loops.
			var loops bool
			if err := tx.QueryRow(ctx, sqlCatalogue3, t, id, *parent).Scan(&loops); err != nil {
				return "", err
			}
			if loops {
				return "", &domain.ValidationError{Fields: []domain.FieldError{{Field: "parent_id", Code: "invalid", Message: "That category is already under this one."}}}
			}
			parentKind, err := categoryKind(ctx, tx, t, *parent)
			if err != nil {
				if errors.Is(err, ports.ErrNotFound) {
					return "", &ports.RefError{Field: "parent_id", Label: "Category"}
				}
				return "", err
			}
			ownKind, err := categoryKind(ctx, tx, t, id)
			if err != nil {
				return "", err
			}
			if ownKind != parentKind {
				return "", &domain.ValidationError{Fields: []domain.FieldError{{Field: "parent_id", Code: "invalid", Message: "A category can only move under one of the same kind (" + parentKind + " vs " + ownKind + ")."}}}
			}
			if _, err := tx.Exec(ctx, `UPDATE item_categories SET parent_category_id = $3::uuid, item_kind = NULL WHERE tenant_id = $1 AND category_id = $2::uuid`, t, id, *parent); err != nil {
				return "", err
			}
		} else {
			// Becoming a root: it keeps the kind it had.
			ownKind, err := categoryKind(ctx, tx, t, id)
			if err != nil {
				return "", err
			}
			if _, err := tx.Exec(ctx, `UPDATE item_categories SET parent_category_id = NULL, item_kind = $3 WHERE tenant_id = $1 AND category_id = $2::uuid`, t, id, ownKind); err != nil {
				return "", err
			}
		}
	}
	if sent(f, "kind") {
		var isRoot bool
		if err := tx.QueryRow(ctx, `SELECT parent_category_id IS NULL FROM item_categories WHERE tenant_id = $1 AND category_id = $2::uuid`, t, id).Scan(&isRoot); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", ports.ErrNotFound
			}
			return "", err
		}
		if isRoot {
			k := domain.FieldString(f, "kind")
			if k == "" {
				return "", &domain.ValidationError{Fields: []domain.FieldError{{Field: "kind", Code: "required", Message: "Kind is required for a top-level category."}}}
			}
			var items int
			if err := tx.QueryRow(ctx, sqlCatalogue4, t, id, k).Scan(&items); err != nil {
				return "", err
			}
			if items > 0 {
				return "", &ports.InUseError{Usage: domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "items of the current kind; move them first", Count: items}}}}
			}
			if _, err := tx.Exec(ctx, `UPDATE item_categories SET item_kind = $3 WHERE tenant_id = $1 AND category_id = $2::uuid`, t, id, k); err != nil {
				return "", err
			}
		}
	}
	set, args := setClause(f, []colBind{
		{"name", "name", textOrEmpty("name")},
		{"sort_order", "sort_order", func(m map[string]any) any {
			if v, ok := domain.FieldInt(m, "sort_order"); ok {
				return v
			}
			return int64(100)
		}},
	}, 4)
	if sent(f, "name") {
		set += ", normalized_name = lower(btrim($4))"
	}
	if set == "" {
		set = "updated_at = now(), row_version = row_version + 1"
	} else {
		set += ", updated_at = now(), row_version = row_version + 1"
	}
	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE item_categories SET %s WHERE tenant_id = $1 AND category_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, set), append([]any{t, id, rv}, args...)...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", &ports.DuplicateError{Field: "name", Message: "A category with that name already exists at this level."}
		}
		return "", err
	}
	return "", fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM item_categories WHERE tenant_id = $1 AND category_id = $2::uuid`, t, id)
}

func (categoryStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	if !isUUID(id) {
		return ports.ErrNotFound
	}
	tag, err := tx.Exec(ctx, `UPDATE item_categories SET status = $4, updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1 AND category_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, t, id, rv, status)
	if err != nil {
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM item_categories WHERE tenant_id = $1 AND category_id = $2::uuid`, t, id)
}

func (categoryStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	if !isUUID(id) {
		return ports.ErrNotFound
	}
	tag, err := tx.Exec(ctx, `DELETE FROM item_categories WHERE tenant_id = $1 AND category_id = $2::uuid AND ($3 = 0 OR row_version = $3) AND NOT is_builtin`, t, id, rv)
	if err != nil {
		return mapWriteError(err)
	}
	return fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM item_categories WHERE tenant_id = $1 AND category_id = $2::uuid`, t, id)
}

// ---------------------------------------------------------------------------------------------
// Items: inventory_items, with the kind-specific facts in its context jsonb and, for a vaccine,
// mirrored onto the vaccines detail row the vaccination module reads. On read the vaccines row
// wins for the four facts it carries, so a vaccine seeded before this screen existed shows its
// disease and doses without a rewrite.

type itemStore struct{}

// itemContextKeys are the kind-specific facts stored in inventory_items.context.
var itemContextKeys = []string{"route", "strength", "withdrawal_days", "disease", "manufacturer", "doses_per_vial", "vaccine_type", "notes"}

var itemProjection = projection{sql: itemProjectionSQL(false), getSQL: itemProjectionSQL(true)}

// sqlDepartmentForKind mirrors domain.DepartmentForKind in SQL, so the filter and the row agree.
func sqlDepartmentForKind(col string) string {
	return "CASE " + col + " WHEN 'vaccine' THEN 'preventive_care' WHEN 'medicine' THEN 'health' WHEN 'dewormer' THEN 'health' WHEN 'supplement' THEN 'health' WHEN 'feed' THEN 'feed' ELSE 'general' END"
}

// isFeedRow reports a feed_item_catalog row folded into the items read; it is edited on Feed Config.
func isFeedRow(id string) bool { return strings.HasPrefix(id, "feed:") }

// itemProjectionSQL renders the item projection; forGet puts the get-by-id predicate inside
// each branch (see projection.getSQL).
func itemProjectionSQL(forGet bool) string {
	quoted := make([]string, 0, len(itemContextKeys))
	for _, k := range itemContextKeys {
		quoted = append(quoted, "'"+k+"'")
	}
	sql := strings.Replace(itemProjectionTemplate, "__CONTEXT_KEYS__", strings.Join(quoted, ", "), 1)
	itemFilter, feedFilter := "", ""
	if forGet {
		// A get by uuid reads the one item row through its key; a feed row's get skips the item
		// branch and the item get skips the feed branch.
		itemFilter, feedFilter = "AND i.item_id = $3::uuid", "AND $3::uuid IS NULL"
	}
	sql = strings.Replace(sql, "__ID_FILTER__", itemFilter, 1)
	return strings.Replace(sql, "__FEED_ID_FILTER__", feedFilter, 1)
}

var itemProjectionTemplate = `
SELECT i.item_id::text AS id,
       i.name AS display,
       replace(replace(i.status, 'inactive', 'archived'), 'retired', 'archived') AS status,
       i.row_version,
       false AS is_builtin,
       jsonb_build_object('name', i.name, 'category_id', i.category_id::text, 'code', i.item_code, 'unit', i.base_unit, 'kind', i.category,
                          'category_ids', COALESCE(cp.ids, '[]'::jsonb),
                          'department', ` + sqlDepartmentForKind("i.category") + `,
                          'tracking', CASE WHEN i.category = 'vaccine' THEN 'Vaccination plan' ELSE '' END)
         || COALESCE((SELECT jsonb_object_agg(e.key, e.value) FROM jsonb_each(i.context) e WHERE e.key IN (__CONTEXT_KEYS__)), '{}'::jsonb)
         || COALESCE(jsonb_strip_nulls(jsonb_build_object('disease', v.disease, 'manufacturer', v.manufacturer, 'doses_per_vial', v.doses_per_vial, 'withdrawal_days', v.withdrawal_days)), '{}'::jsonb) AS fields,
       jsonb_strip_nulls(jsonb_build_object('category_id', cp.path)) AS labels,
       jsonb_build_object('stock_lots', (SELECT count(*) FROM inventory_stock s WHERE s.tenant_id = i.tenant_id AND s.item_id = i.item_id AND s.status = 'active')) AS counts,
       lower(i.name) AS sort_key
FROM inventory_items i
LEFT JOIN vaccines v ON v.tenant_id = i.tenant_id AND v.item_id = i.item_id
LEFT JOIN LATERAL (
  WITH RECURSIVE up AS (
    SELECT c.category_id, c.parent_category_id, c.name::text AS path, jsonb_build_array(c.category_id::text) AS ids, 1 AS depth
    FROM item_categories c WHERE c.tenant_id = i.tenant_id AND c.category_id = i.category_id
    UNION ALL
    SELECT c.category_id, c.parent_category_id, c.name || ' › ' || up.path, up.ids || to_jsonb(c.category_id::text), up.depth + 1
    FROM item_categories c JOIN up ON c.category_id = up.parent_category_id WHERE c.tenant_id = i.tenant_id AND up.depth < 8
  )
  SELECT path, ids FROM up WHERE parent_category_id IS NULL LIMIT 1
) cp ON true
WHERE i.tenant_id = $1 __ID_FILTER__
UNION ALL
SELECT 'feed:' || f.feed_item_id::text AS id,
       f.feed_item_label AS display,
       replace(f.status, 'retired', 'archived') AS status,
       0 AS row_version,
       false AS is_builtin,
       jsonb_build_object('name', f.feed_item_label, 'category_id', fc.category_id::text, 'code', NULL, 'unit', 'kg', 'kind', 'feed',
                          'category_ids', COALESCE(jsonb_build_array(fc.category_id::text), '[]'::jsonb),
                          'department', 'feed', 'tracking', 'Feed Config', 'read_only', true,
                          'energy_kcal_per_kg', f.energy_kcal_per_kg, 'dry_matter_factor', f.dry_matter_factor, 'wastage_factor', f.wastage_factor) AS fields,
       jsonb_strip_nulls(jsonb_build_object('category_id', fc.name)) AS labels,
       NULL::jsonb AS counts,
       lower(f.feed_item_label) AS sort_key
FROM feed_item_catalog f
LEFT JOIN item_categories fc ON fc.tenant_id = f.tenant_id AND fc.parent_category_id IS NULL AND fc.item_kind = 'feed' AND fc.is_builtin
WHERE f.tenant_id = $1 __FEED_ID_FILTER__`

func (itemStore) count(ctx context.Context, q querier, t string) (int, error) {
	return itemProjection.count(ctx, q, t)
}
func (itemStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	// A category filter selects the whole SUBTREE: every row carries its category and its
	// ancestors in category_ids, and jsonb containment on an array is "contains this element".
	if cat, ok := p.Filters["category_id"]; ok {
		filters := map[string]string{}
		for k, v := range p.Filters {
			if k != "category_id" {
				filters[k] = v
			}
		}
		p.Filters = filters
		p.FilterJSON = map[string]any{"category_ids": []string{cat}}
	}
	return itemProjection.list(ctx, q, t, p)
}
func (itemStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	if !isUUID(id) && !isFeedRow(id) {
		return domain.Row{}, ports.ErrNotFound
	}
	return itemProjection.get(ctx, q, t, id)
}
func (itemStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return itemProjection.options(ctx, q, t)
}
func (itemStore) usage(ctx context.Context, q querier, t, id string) (domain.Usage, error) {
	if isFeedRow(id) {
		return domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "Feed Config", Count: 1}}}, nil
	}
	if !isUUID(id) {
		return domain.Usage{}, ports.ErrNotFound
	}
	return usageOf(ctx, q, t, id,
		usageCheck{"stock lots", `SELECT count(*) FROM inventory_stock WHERE tenant_id = $1 AND item_id = $2::uuid`},
	)
}

func itemContext(f map[string]any, existing map[string]any) ([]byte, error) {
	ctxMap := map[string]any{}
	for k, v := range existing {
		ctxMap[k] = v
	}
	for _, k := range itemContextKeys {
		if !sent(f, k) {
			continue
		}
		if f[k] == nil {
			delete(ctxMap, k)
			continue
		}
		ctxMap[k] = f[k]
	}
	return json.Marshal(ctxMap)
}

func itemCode(f map[string]any) string {
	if c := domain.FieldString(f, "code"); c != "" {
		return strings.ToUpper(strings.TrimSpace(c))
	}
	return "ITM-" + strings.ToUpper(strings.ReplaceAll(domain.NormalizeCode(domain.FieldString(f, "name")), "_", "-"))
}

func (itemStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	kind, err := categoryKind(ctx, tx, t, domain.FieldString(f, "category_id"))
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return "", &ports.RefError{Field: "category_id", Label: "Category"}
		}
		return "", err
	}
	contextJSON, err := itemContext(f, nil)
	if err != nil {
		return "", err
	}
	var id string
	if err := tx.QueryRow(ctx, sqlCatalogue5, t, itemCode(f), domain.FieldString(f, "name"), kind, domain.FieldString(f, "category_id"), domain.FieldString(f, "unit"), contextJSON).Scan(&id); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", &ports.DuplicateError{Field: "code", Message: "An item with that code already exists."}
		}
		return "", err
	}
	if kind == "vaccine" {
		if err := upsertVaccine(ctx, tx, t, id, f); err != nil {
			return "", err
		}
	}
	return id, nil
}

func upsertVaccine(ctx context.Context, tx pgx.Tx, t, itemID string, f map[string]any) error {
	_, err := tx.Exec(ctx, sqlCatalogue6, t, itemID, nullText(f, "disease"), nullText(f, "manufacturer"), nullInt(f, "doses_per_vial"), nullInt(f, "withdrawal_days"))
	return err
}

func (itemStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	if isFeedRow(id) {
		return "", domain.ErrReadOnlyRegister
	}
	if !isUUID(id) {
		return "", ports.ErrNotFound
	}
	var existingCtx map[string]any
	var ctxRaw []byte
	var currentKind string
	if err := tx.QueryRow(ctx, `SELECT context, category FROM inventory_items WHERE tenant_id = $1 AND item_id = $2::uuid`, t, id).Scan(&ctxRaw, &currentKind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ports.ErrNotFound
		}
		return "", err
	}
	_ = json.Unmarshal(ctxRaw, &existingCtx)
	kind := currentKind
	if sent(f, "category_id") {
		k, err := categoryKind(ctx, tx, t, domain.FieldString(f, "category_id"))
		if err != nil {
			if errors.Is(err, ports.ErrNotFound) {
				return "", &ports.RefError{Field: "category_id", Label: "Category"}
			}
			return "", err
		}
		if k != currentKind {
			var lots int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM inventory_stock WHERE tenant_id = $1 AND item_id = $2::uuid`, t, id).Scan(&lots); err != nil {
				return "", err
			}
			if lots > 0 {
				return "", &ports.InUseError{Usage: domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "stock lots; an item with stock cannot change kind", Count: lots}}}}
			}
		}
		kind = k
	}
	contextJSON, err := itemContext(f, existingCtx)
	if err != nil {
		return "", err
	}
	set, args := setClause(f, []colBind{
		{"name", "name", textOrEmpty("name")},
		{"code", "item_code", func(m map[string]any) any { return itemCode(m) }},
		{"unit", "base_unit", textOrEmpty("unit")},
		{"category_id", "category_id", func(m map[string]any) any { return domain.FieldString(m, "category_id") }},
	}, 5)
	if set != "" {
		set += ", "
	}
	set += "category = $4, context = $3::jsonb, updated_at = now(), row_version = row_version + 1"
	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE inventory_items SET %s WHERE tenant_id = $1 AND item_id = $2::uuid AND ($%d = 0 OR row_version = $%d)`, set, 5+len(args), 5+len(args)), append(append([]any{t, id, contextJSON, kind}, args...), rv)...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", &ports.DuplicateError{Field: "code", Message: "An item with that code already exists."}
		}
		return "", err
	}
	if err := fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM inventory_items WHERE tenant_id = $1 AND item_id = $2::uuid`, t, id); err != nil {
		return "", err
	}
	if kind == "vaccine" {
		return "", upsertVaccine(ctx, tx, t, id, f)
	}
	return "", nil
}

func (itemStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	if isFeedRow(id) {
		return domain.ErrReadOnlyRegister
	}
	if !isUUID(id) {
		return ports.ErrNotFound
	}
	dbStatus := "inactive"
	if status == domain.StatusActive {
		dbStatus = "active"
	}
	tag, err := tx.Exec(ctx, `UPDATE inventory_items SET status = $4, updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1 AND item_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, t, id, rv, dbStatus)
	if err != nil {
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM inventory_items WHERE tenant_id = $1 AND item_id = $2::uuid`, t, id)
}

func (itemStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	if isFeedRow(id) {
		return domain.ErrReadOnlyRegister
	}
	if !isUUID(id) {
		return ports.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM vaccines WHERE tenant_id = $1 AND item_id = $2::uuid`, t, id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM inventory_items WHERE tenant_id = $1 AND item_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, t, id, rv)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return &ports.InUseError{Usage: domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "past records; archive it instead", Count: 1}}}}
		}
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM inventory_items WHERE tenant_id = $1 AND item_id = $2::uuid`, t, id)
}

// ---------------------------------------------------------------------------------------------
// Feed items: read-only here. feed_item_catalog is Feed Config's, where the ration grid depends
// on it; this register lists it so the catalogue is complete and links there to edit.

type feedItemStore struct{}

var feedItemProjection = projection{sql: `
SELECT f.feed_item_id::text AS id,
       f.feed_item_label AS display,
       CASE WHEN f.status = 'active' THEN 'active' ELSE 'archived' END AS status,
       0 AS row_version,
       false AS is_builtin,
       jsonb_build_object('name', f.feed_item_label, 'energy_kcal_per_kg', f.energy_kcal_per_kg, 'dry_matter_factor', f.dry_matter_factor, 'wastage_factor', f.wastage_factor) AS fields,
       '{}'::jsonb AS labels,
       NULL::jsonb AS counts,
       lpad(f.display_order::text, 6, '0') || ' ' || lower(f.feed_item_label) AS sort_key
FROM feed_item_catalog f
WHERE f.tenant_id = $1`}

func (feedItemStore) count(ctx context.Context, q querier, t string) (int, error) {
	return feedItemProjection.count(ctx, q, t)
}
func (feedItemStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return feedItemProjection.list(ctx, q, t, p)
}
func (feedItemStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	if !isUUID(id) {
		return domain.Row{}, ports.ErrNotFound
	}
	return feedItemProjection.get(ctx, q, t, id)
}
func (feedItemStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return feedItemProjection.options(ctx, q, t)
}
func (feedItemStore) usage(context.Context, querier, string, string) (domain.Usage, error) {
	return domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "Feed Config", Count: 1}}}, nil
}
func (feedItemStore) insert(context.Context, pgx.Tx, string, map[string]any) (string, error) {
	return "", domain.ErrReadOnlyRegister
}
func (feedItemStore) update(context.Context, pgx.Tx, string, string, map[string]any, int) (string, error) {
	return "", domain.ErrReadOnlyRegister
}
func (feedItemStore) setStatus(context.Context, pgx.Tx, string, string, string, int) error {
	return domain.ErrReadOnlyRegister
}
func (feedItemStore) del(context.Context, pgx.Tx, string, string, int) error {
	return domain.ErrReadOnlyRegister
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlCatalogue1 = `
WITH RECURSIVE up AS (
  SELECT category_id, parent_category_id, item_kind, 1 AS depth FROM item_categories WHERE tenant_id = $1 AND category_id = $2::uuid AND status = 'active'
  UNION ALL
  SELECT c.category_id, c.parent_category_id, c.item_kind, up.depth + 1 FROM item_categories c JOIN up ON c.category_id = up.parent_category_id WHERE c.tenant_id = $1 AND up.depth < 8
)
SELECT item_kind FROM up WHERE parent_category_id IS NULL LIMIT 1`
	sqlCatalogue2 = `
INSERT INTO item_categories (tenant_id, parent_category_id, name, normalized_name, item_kind, sort_order)
VALUES ($1, $2::uuid, $3, lower(btrim($3)), $4, $5)
RETURNING category_id::text`
	sqlCatalogue3 = `
WITH RECURSIVE down AS (
  SELECT category_id, 1 AS depth FROM item_categories WHERE tenant_id = $1 AND parent_category_id = $2::uuid
  UNION ALL
  SELECT c.category_id, down.depth + 1 FROM item_categories c JOIN down ON c.parent_category_id = down.category_id WHERE c.tenant_id = $1 AND down.depth < 8
)
SELECT EXISTS (SELECT 1 FROM down WHERE category_id = $3::uuid)`
	sqlCatalogue4 = `
WITH RECURSIVE down AS (
  SELECT category_id FROM item_categories WHERE tenant_id = $1 AND category_id = $2::uuid
  UNION ALL
  SELECT c.category_id FROM item_categories c JOIN down ON c.parent_category_id = down.category_id WHERE c.tenant_id = $1
)
SELECT count(*) FROM inventory_items i WHERE i.tenant_id = $1 AND i.category_id IN (SELECT category_id FROM down) AND i.category <> $3`
	sqlCatalogue5 = `
INSERT INTO inventory_items (tenant_id, item_code, name, category, category_id, base_unit, status, context)
VALUES ($1, $2, $3, $4, $5::uuid, $6, 'active', $7::jsonb)
RETURNING item_id::text`
	sqlCatalogue6 = `
INSERT INTO vaccines (tenant_id, item_id, disease, manufacturer, doses_per_vial, withdrawal_days)
VALUES ($1, $2::uuid, $3, $4, $5, $6)
ON CONFLICT (tenant_id, item_id) DO UPDATE SET
  disease = COALESCE(EXCLUDED.disease, vaccines.disease),
  manufacturer = COALESCE(EXCLUDED.manufacturer, vaccines.manufacturer),
  doses_per_vial = COALESCE(EXCLUDED.doses_per_vial, vaccines.doses_per_vial),
  withdrawal_days = COALESCE(EXCLUDED.withdrawal_days, vaccines.withdrawal_days),
  updated_at = now()`
)
