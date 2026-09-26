package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// ---------------------------------------------------------------------------------------------
// Species and sexes share one shape: a per-tenant code lookup (migration 000346) whose code is
// what goats.species / goats.sex store. The built-in rows are flagged and never leave.

type codeLookupStore struct {
	register string // the register key, for domain.IsBuiltinCode
	table    string
	codeCol  string
	goatCol  string // the goats column AND the protocol_rule_dimensions column naming the code
	breedCol string // set when breeds also name the code (species)
}

func (s codeLookupStore) projection() projection {
	return projection{sql: fmt.Sprintf(sqlCodeLookupProjection, s.table, s.codeCol, s.goatCol), decorate: s.decorate}
}

// decorate marks a row the product names literally as built in even when its is_builtin flag was
// never set (a tenant whose lookup rows were not seeded by 000346), so archive and delete refuse
// it the same way.
func (s codeLookupStore) decorate(row *domain.Row) {
	if domain.IsBuiltinCode(s.register, row.ID) {
		row.IsBuiltin = true
	}
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
	checks := []usageCheck{
		{"animals", fmt.Sprintf(`SELECT count(*) FROM goats WHERE tenant_id = $1 AND %s = $2 AND lifecycle_status = 'alive'`, s.goatCol)},
		// A published vaccination rule aimed at this code would silently match nobody once the code
		// is gone, so it holds the row like an animal does.
		{"vaccination rules", fmt.Sprintf(sqlPublishedRuleUsage, s.goatCol, "$2")},
	}
	if s.breedCol != "" {
		// A farm's breeds name their species by its code (breeds are per farm since 000442).
		checks = append(checks, usageCheck{"breeds", fmt.Sprintf(`SELECT count(*) FROM breeds WHERE tenant_id = $1 AND %s = $2 AND status = 'active'`, s.breedCol)})
	}
	return usageOf(ctx, q, t, id, checks...)
}

func (s codeLookupStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	code := domain.FieldString(f, "code")
	sort, ok := domain.FieldInt(f, "sort_order")
	if !ok {
		sort = 100
	}
	q, err := sqlbind.Bind(fmt.Sprintf(`INSERT INTO %s (tenant_id, %s, name, sort_order) VALUES ($1, $2, $3, $4)`, s.table, s.codeCol), t, code, domain.FieldString(f, "name"), sort)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, q.SQL(), q.Args()...); err != nil {
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
	q, err := sqlbind.Bind(fmt.Sprintf(`UPDATE %s SET %s, updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1 AND %s = $2 AND ($3 = 0 OR row_version = $3)`, s.table, set, s.codeCol), append([]any{t, id, rv}, args...)...)
	if err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return "", err
	}
	return "", fenced(ctx, tx, tag.RowsAffected(), fmt.Sprintf(`SELECT 1 FROM %s WHERE tenant_id = $1 AND %s = $2`, s.table, s.codeCol), t, id)
}

func (s codeLookupStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	q, err := sqlbind.Bind(fmt.Sprintf(`UPDATE %s SET status = $4, updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1 AND %s = $2 AND ($3 = 0 OR row_version = $3)`, s.table, s.codeCol), t, id, rv, status)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), fmt.Sprintf(`SELECT 1 FROM %s WHERE tenant_id = $1 AND %s = $2`, s.table, s.codeCol), t, id)
}

func (s codeLookupStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	q, err := sqlbind.Bind(fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1 AND %s = $2 AND ($3 = 0 OR row_version = $3) AND NOT is_builtin`, s.table, s.codeCol), t, id, rv)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, q.SQL(), q.Args()...)
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
       l.row_version,
       false AS is_builtin,
       jsonb_strip_nulls(jsonb_build_object('name', l.name, 'code', l.stage_code, 'min_age_days', l.min_age_days, 'max_age_days', l.max_age_days, 'age_band', l.age_band, 'sort_order', l.sort_order)) AS fields,
       '{}'::jsonb AS labels,
       jsonb_build_object(
         'animals', (SELECT count(*) FROM goats g WHERE g.tenant_id = l.tenant_id AND g.management_stage = l.stage_code AND g.lifecycle_status = 'alive'),
         'pens', (SELECT count(*) FROM shed_profiles sp WHERE sp.tenant_id = l.tenant_id AND sp.animal_stage_id = l.animal_stage_id)
       ) AS counts,
       lpad(l.sort_order::text, 6, '0') || ' ' || lower(l.name) AS sort_key
FROM animal_stage_lookup l
WHERE l.tenant_id = $1`, decorate: decorateStage}

// decorateStage marks the stages the product names in code (K0, Flushing, the growth ladder) as
// built in: they may be renamed or re-banded, never archived or deleted.
func decorateStage(row *domain.Row) {
	if domain.IsBuiltinCode(domain.RegStages, domain.FieldString(row.Fields, "code")) {
		row.IsBuiltin = true
	}
}

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
		usageCheck{"vaccination rules", fmt.Sprintf(sqlPublishedRuleUsage, "animal_stage", `(SELECT stage_code FROM animal_stage_lookup WHERE tenant_id = $1 AND animal_stage_id = $2::uuid)`)},
	)
}

func (stageStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	sort, ok := domain.FieldInt(f, "sort_order")
	if !ok {
		sort = 100
	}
	var id string
	err := tx.QueryRow(ctx, sqlAnimals1, t, domain.FieldString(f, "code"), domain.FieldString(f, "name"), nullInt(f, "min_age_days"), nullInt(f, "max_age_days"), sort, nullText(f, "age_band")).Scan(&id)
	return id, err
}

func (stageStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	set, args := setClause(f, []colBind{
		{"name", "name", textOrEmpty("name")},
		{"min_age_days", "min_age_days", intArg("min_age_days")},
		{"max_age_days", "max_age_days", intArg("max_age_days")},
		{"age_band", "age_band", textArg("age_band")},
		{"sort_order", "sort_order", func(m map[string]any) any {
			if v, ok := domain.FieldInt(m, "sort_order"); ok {
				return v
			}
			return int64(100)
		}},
		// The SET list starts at $4, NOT $3. The WHERE already spends $1 tenant, $2 id and $3 the
		// row-version fence, so numbering the first assignment $3 made it collide with the fence and
		// left the statement one argument short of its placeholders: EVERY stage rename failed with
		// pgx "mismatched param and argument count", surfacing as a 500 and a "that could not be
		// saved" on screen. Found 2026-09-22 by sweeping every register's edit path.
	}, 4)
	if set == "" {
		return "", nil
	}
	// Bound rather than exec'd raw: sqlbind checks that the placeholders a composed statement
	// carries are exactly $1..$N and that N matches the arguments, so the same off-by-one comes
	// back as a named error at the seam instead of a 500 from the driver.
	q, err := sqlbind.Bind(fmt.Sprintf(`UPDATE animal_stage_lookup SET %s, updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1 AND animal_stage_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, set), append([]any{t, id, rv}, args...)...)
	if err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return "", err
	}
	return "", fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM animal_stage_lookup WHERE tenant_id = $1 AND animal_stage_id = $2::uuid`, t, id)
}

func (stageStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	dbStatus := "inactive"
	if status == domain.StatusActive {
		dbStatus = "active"
	}
	tag, err := tx.Exec(ctx, `UPDATE animal_stage_lookup SET status = $4, updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1 AND animal_stage_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, t, id, rv, dbStatus)
	if err != nil {
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM animal_stage_lookup WHERE tenant_id = $1 AND animal_stage_id = $2::uuid`, t, id)
}

func (stageStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	tag, err := tx.Exec(ctx, `DELETE FROM animal_stage_lookup WHERE tenant_id = $1 AND animal_stage_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, t, id, rv)
	if err != nil {
		return mapWriteError(err)
	}
	return fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM animal_stage_lookup WHERE tenant_id = $1 AND animal_stage_id = $2::uuid`, t, id)
}

// sqlPublishedRuleUsage counts the PUBLISHED vaccination rules one of whose compiled dimensions
// names a code. Formatted with the dimension column and the SQL expression for the code ($1 is the
// tenant). Each dimension row names exactly one value ('all' is the wildcard), so an exact,
// case-insensitive match is the whole test. Drafts and retired versions do not hold a row.
const sqlPublishedRuleUsage = `
SELECT count(DISTINCT d.rule_id) FROM protocol_rule_dimensions d
JOIN protocol_versions v ON v.tenant_id = d.tenant_id AND v.protocol_version_id = d.protocol_version_id AND v.status = 'published'
WHERE d.tenant_id = $1 AND lower(btrim(d.%[1]s)) = lower(btrim(%[2]s))`

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlAnimals1 = `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, min_age_days, max_age_days, sort_order, status, age_band)
VALUES ($1, $2, $3, $4, $5, $6, 'active', $7)
RETURNING animal_stage_id::text`
)

// ---------------------------------------------------------------------------------------------
// Roles: designation_catalog, the designations /people gives a person. The catalog is not
// tenant-scoped (one product-wide list); $1 is consumed so the shared wrapper's arguments line
// up. A designation someone holds (person_access) cannot be deleted; its module defaults
// (designation_module_defaults) cascade with it when nobody holds it.

type roleStore struct{}

var roleProjection = projection{sql: sqlRoleProjection, decorate: decorateRole}

const sqlRoleProjection = `
SELECT d.designation_code AS id,
       d.label AS display,
       replace(d.status, 'retired', 'archived') AS status,
       0 AS row_version,
       false AS is_builtin,
       jsonb_build_object('name', d.label, 'code', d.designation_code, 'grade', d.grade, 'sort_order', d.sort_order) AS fields,
       '{}'::jsonb AS labels,
       jsonb_build_object('people', (SELECT count(*) FROM person_access pa WHERE pa.designation_code = d.designation_code AND ($1::uuid IS NULL OR pa.tenant_id = $1::uuid))) AS counts,
       lpad(d.sort_order::text, 6, '0') || ' ' || lower(d.label) AS sort_key
FROM designation_catalog d
WHERE $1::uuid IS NOT NULL`

func (roleStore) count(ctx context.Context, q querier, t string) (int, error) {
	return roleProjection.count(ctx, q, t)
}
func (roleStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return roleProjection.list(ctx, q, t, p)
}
func (roleStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	return roleProjection.get(ctx, q, t, id)
}

// A designation whose code is an RBAC role (ceo_internal, park_head, ...) is built in: the
// product names it in code, so it can be renamed but never archived or deleted.
func decorateRole(row *domain.Row) {
	if permissions.IsKnownRole(row.ID) {
		row.IsBuiltin = true
	}
}
func (roleStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return roleProjection.options(ctx, q, t)
}
func (roleStore) usage(ctx context.Context, q querier, t, id string) (domain.Usage, error) {
	return usageOf(ctx, q, t, id, usageCheck{"people", `SELECT count(*) FROM person_access WHERE tenant_id = $1 AND designation_code = $2`})
}

func (roleStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	sort, ok := domain.FieldInt(f, "sort_order")
	if !ok {
		sort = 100
	}
	code := domain.FieldString(f, "code")
	if _, err := tx.Exec(ctx, `INSERT INTO designation_catalog (designation_code, label, grade, sort_order) VALUES ($1, $2, $3, $4)`, code, domain.FieldString(f, "name"), nullText(f, "grade"), sort); err != nil {
		return "", err
	}
	return code, nil
}

func (roleStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	set, args := setClause(f, []colBind{
		{"name", "label", textOrEmpty("name")},
		{"grade", "grade", textArg("grade")},
		{"sort_order", "sort_order", func(m map[string]any) any {
			if v, ok := domain.FieldInt(m, "sort_order"); ok {
				return v
			}
			return int64(100)
		}},
	}, 2)
	if set == "" {
		return "", nil
	}
	q, err := sqlbind.Bind(fmt.Sprintf(`UPDATE designation_catalog SET %s WHERE designation_code = $1`, set), append([]any{id}, args...)...)
	if err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ports.ErrNotFound
	}
	return "", nil
}

func (roleStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	dbStatus := "retired"
	if status == domain.StatusActive {
		dbStatus = "active"
	}
	tag, err := tx.Exec(ctx, `UPDATE designation_catalog SET status = $2 WHERE designation_code = $1`, id, dbStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

func (roleStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	tag, err := tx.Exec(ctx, `DELETE FROM designation_catalog WHERE designation_code = $1`, id)
	if err != nil {
		return mapWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Breeds: each farm's own list (breeds.tenant_id, migration 000442), edited here like any other
// register. A breed is keyed by (tenant, species, canonical_name); goats name it by breed_id and by
// the text column, so usage counts both.

type breedStore struct{}

var breedProjection = projection{sql: sqlBreedProjection}

const sqlBreedProjection = `
SELECT b.breed_id::text AS id,
       b.canonical_name AS display,
       CASE WHEN b.status = 'active' THEN 'active' ELSE 'archived' END AS status,
       0 AS row_version,
       false AS is_builtin,
       jsonb_build_object('name', b.canonical_name, 'species', b.species, 'notes', NULLIF(b.review_notes, '')) AS fields,
       jsonb_strip_nulls(jsonb_build_object('species', sl.name)) AS labels,
       jsonb_build_object('animals', (SELECT count(*) FROM goats g WHERE g.tenant_id = $1::uuid AND g.lifecycle_status = 'alive' AND (g.breed_id = b.breed_id OR lower(g.breed) = lower(b.canonical_name)))) AS counts,
       b.species || ' ' || lower(b.canonical_name) AS sort_key
FROM breeds b
LEFT JOIN species_lookup sl ON sl.tenant_id = $1::uuid AND sl.species_code = b.species
WHERE b.tenant_id = $1::uuid`

func (breedStore) count(ctx context.Context, q querier, t string) (int, error) {
	return breedProjection.count(ctx, q, t)
}
func (breedStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return breedProjection.list(ctx, q, t, p)
}
func (breedStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	if !isUUID(id) {
		return domain.Row{}, ports.ErrNotFound
	}
	return breedProjection.get(ctx, q, t, id)
}
func (breedStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return breedProjection.options(ctx, q, t)
}
func (breedStore) usage(ctx context.Context, q querier, t, id string) (domain.Usage, error) {
	if !isUUID(id) {
		return domain.Usage{}, ports.ErrNotFound
	}
	return usageOf(ctx, q, t, id,
		usageCheck{"animals", sqlBreedUsage},
		usageCheck{"vaccination rules", fmt.Sprintf(sqlPublishedRuleUsage, "breed", `(SELECT canonical_name FROM breeds WHERE tenant_id = $1::uuid AND breed_id = $2::uuid)`)},
	)
}

func (breedStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	species := domain.FieldString(f, "species")
	var one int
	if err := tx.QueryRow(ctx, sqlBreedSpeciesExists, t, species).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", &ports.RefError{Field: "species", Label: "Species"}
		}
		return "", err
	}
	var id string
	if err := tx.QueryRow(ctx, sqlBreedInsert, t, species, domain.FieldString(f, "name"), nullText(f, "notes")).Scan(&id); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", &ports.DuplicateError{Field: "name", Message: "That species already has a breed with this name."}
		}
		return "", err
	}
	return id, nil
}

func (breedStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	if !isUUID(id) {
		return "", ports.ErrNotFound
	}
	if sent(f, "species") {
		// The species is part of the breed's identity and of every animal carrying it.
		var animals int
		if err := tx.QueryRow(ctx, sqlBreedUsage, t, id).Scan(&animals); err != nil {
			return "", err
		}
		if animals > 0 {
			return "", &ports.InUseError{Usage: domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "animals; a breed's species cannot change while animals carry it", Count: animals}}}}
		}
		var one int
		if err := tx.QueryRow(ctx, sqlBreedSpeciesExists, t, domain.FieldString(f, "species")).Scan(&one); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", &ports.RefError{Field: "species", Label: "Species"}
			}
			return "", err
		}
	}
	set, args := setClause(f, []colBind{{"name", "canonical_name", textOrEmpty("name")}, {"species", "species", textOrEmpty("species")}, {"notes", "review_notes", textArg("notes")}}, 2)
	if set == "" {
		return "", nil
	}
	// The name before the rename: animals registered with the breed typed as text (no breed_id)
	// carry that old name and must follow the rename too.
	var oldName string
	if sent(f, "name") {
		if err := tx.QueryRow(ctx, sqlBreedName, t, id).Scan(&oldName); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", ports.ErrNotFound
			}
			return "", err
		}
	}
	q, err := sqlbind.Bind(fmt.Sprintf(`UPDATE breeds SET %s, updated_at = now() WHERE breed_id = $1::uuid AND tenant_id = $%d::uuid`, set, len(args)+2), append(append([]any{id}, args...), t)...)
	if err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, q.SQL(), q.Args()...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", &ports.DuplicateError{Field: "name", Message: "That species already has a breed with this name."}
		}
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ports.ErrNotFound
	}
	if sent(f, "name") {
		// goats.breed carries the name as text beside breed_id; keep them agreeing.
		if _, err := tx.Exec(ctx, sqlBreedRenameGoats, t, id, domain.FieldString(f, "name"), oldName); err != nil {
			return "", err
		}
		// Feed direction finds an adult animal's ration group by its breed TEXT
		// (feed_ration_groups.breed_key), so without this every animal of a renamed breed lost
		// its ration and was blocked off the feed sheet. The mapping follows the breed unless the
		// new name already has one of its own, which is kept.
		if _, err := tx.Exec(ctx, sqlBreedRenameRationGroup, t, oldName, domain.FieldString(f, "name")); err != nil {
			return "", err
		}
	}
	return "", nil
}

func (breedStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	if !isUUID(id) {
		return ports.ErrNotFound
	}
	dbStatus := "inactive"
	if status == domain.StatusActive {
		dbStatus = "active"
	}
	tag, err := tx.Exec(ctx, `UPDATE breeds SET status = $2, updated_at = now() WHERE breed_id = $1::uuid AND tenant_id = $3::uuid`, id, dbStatus, t)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

func (breedStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	if !isUUID(id) {
		return ports.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM breed_aliases WHERE breed_id = $1::uuid AND tenant_id = $2::uuid`, id, t); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM breeds WHERE breed_id = $1::uuid AND tenant_id = $2::uuid`, id, t)
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
	sqlBreedUsage = `
SELECT count(*) FROM goats g
WHERE g.tenant_id = $1 AND g.lifecycle_status = 'alive'
  AND (g.breed_id = $2::uuid OR lower(g.breed) = lower((SELECT canonical_name FROM breeds WHERE breed_id = $2::uuid AND tenant_id = $1::uuid)))`
	sqlBreedSpeciesExists = `SELECT 1 FROM species_lookup WHERE tenant_id = $1 AND species_code = $2 AND status = 'active'`
	// scale-guard:plan-proof-exempt: breeds is a per-farm catalogue of tens of rows; the goats statements here (usage count, rename) are rare Configuration writes/deletes already scoped to (tenant_id, breed_id) and only gain a tenant_id equality -- no serving read's access path changes.
	sqlBreedInsert      = `INSERT INTO breeds (tenant_id, species, canonical_name, status, review_notes) VALUES ($1, $2, $3, 'active', $4) RETURNING breed_id::text`
	sqlBreedName        = `SELECT canonical_name FROM breeds WHERE tenant_id = $1 AND breed_id = $2::uuid`
	sqlBreedRenameGoats = `UPDATE goats SET breed = $3 WHERE tenant_id = $1 AND (breed_id = $2::uuid OR (breed_id IS NULL AND lower(btrim(breed)) = lower(btrim($4))))`
	sqlBreedRenameRationGroup = `
UPDATE feed_ration_groups g SET breed_label = $3, updated_at = now()
WHERE g.tenant_id = $1::uuid AND g.breed_key = feed_config_norm($2)
  AND feed_config_norm($2) <> feed_config_norm($3)
  AND NOT EXISTS (SELECT 1 FROM feed_ration_groups x WHERE x.tenant_id = $1::uuid AND x.breed_key = feed_config_norm($3))`
)
