// Package postgres compiles DB-backed admin-web UI contract families.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/adminui/app"
)

const defaultQueryTimeout = 3 * time.Second

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ app.ReferenceRepository = (*Repository)(nil)
var _ app.ReferenceRevisionRepository = (*Repository)(nil)

func (r *Repository) LoadContractFamilies(ctx context.Context, tenantID string) (app.ReferenceFamilies, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	out := app.ReferenceFamilies{RevisionInputs: map[string]string{}}
	var err error
	if out.Parks, out.RevisionInputs["locations"], err = r.listParks(ctx, tenantID); err != nil {
		return out, err
	}
	if out.RuleCategories, out.RevisionInputs["protocol-categories"], err = r.listProtocolCategories(ctx, tenantID); err != nil {
		return out, err
	}
	if out.Breeds, out.RevisionInputs["breeds"], err = r.listBreeds(ctx); err != nil {
		return out, err
	}
	if out.HealthStatuses, out.RevisionInputs["health-statuses"], err = r.listStatuses(ctx, "health", ""); err != nil {
		return out, err
	}
	if out.ReproductiveStates, out.RevisionInputs["reproductive-statuses"], err = r.listStatuses(ctx, "reproductive", ""); err != nil {
		return out, err
	}
	if out.DeferStates, out.RevisionInputs["defer-states"], err = r.listStatuses(ctx, "health", "warn"); err != nil {
		return out, err
	}
	if out.SOPLabels, out.RevisionInputs["sop-labels"], err = r.listSOPLabels(ctx, tenantID); err != nil {
		return out, err
	}
	if out.WeighingWeightsPages, out.RevisionInputs["weighing-weights-pages"], err = r.loadWeighingWeightsPages(ctx, tenantID); err != nil {
		return out, err
	}
	if out.FeedItems, out.RevisionInputs["feed-items"], err = r.listFeedItems(ctx, tenantID); err != nil {
		return out, err
	}
	if out.SOPTaskTypes, out.SOPTaskTypeAnswerKinds, out.RevisionInputs["sop-task-types"], err = r.listSOPTaskTypes(ctx, tenantID); err != nil {
		return out, err
	}
	if out.Designations, out.RevisionInputs["designations"], err = r.listDesignations(ctx); err != nil {
		return out, err
	}
	if out.PenTypes, out.RevisionInputs["pen-types"], err = r.listPenTypes(ctx, tenantID); err != nil {
		return out, err
	}
	if out.UIConfig, out.RevisionInputs["admin-ui-config-values"], err = r.listUIConfigEntries(ctx, tenantID); err != nil {
		return out, err
	}
	revisions, err := r.listConfigFamilyRevisions(ctx, tenantID)
	if err != nil {
		return out, err
	}
	for key, value := range revisions {
		out.RevisionInputs["admin-ui:"+key] = value
	}
	return out, nil
}

func (r *Repository) LoadContractFamilyRevisions(ctx context.Context, tenantID string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	revisions, err := r.listConfigFamilyRevisions(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for key, value := range revisions {
		out["admin-ui:"+key] = value
	}
	return out, nil
}

func (r *Repository) listParks(ctx context.Context, tenantID string) ([]app.ReferenceOption, string, error) {
	localVaccinationScope := os.Getenv("GOATOS_ENV") == "local"
	rows, err := r.pool.Query(ctx, `
SELECT
  location_id::text,
  COALESCE(NULLIF(location_code, ''), name) AS label,
  name,
  COALESCE(location_code, '') AS code,
  updated_at::text
FROM locations
WHERE tenant_id = $1::uuid
  AND location_type = 'park'
  AND status = 'active'
  AND (
    $2::boolean = false
    OR NOT EXISTS (
      SELECT 1
      FROM vaccination_drive_assignments any_vda
      WHERE any_vda.tenant_id = locations.tenant_id
    )
    OR EXISTS (
      SELECT 1
      FROM vaccination_drive_assignments vda
      WHERE vda.tenant_id = locations.tenant_id
        AND vda.park_id = locations.location_id
    )
  )
-- Parks sort by their CODE after display_order, never by name (maintainer decision 2026-09-16:
-- CBE, then CPT). Both farms carry display_order 0, and by name Channapatna sorts before
-- Coimbatore, so every park picker fed from here listed CPT first.
ORDER BY display_order, COALESCE(NULLIF(location_code, ''), name), location_id
LIMIT 500`, tenantID, localVaccinationScope)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: list parks: %w", err)
	}
	defer rows.Close()
	var out []app.ReferenceOption
	var rev strings.Builder
	for rows.Next() {
		var id, label, name, code, updated string
		if err := rows.Scan(&id, &label, &name, &code, &updated); err != nil {
			return nil, "", err
		}
		out = append(out, app.ReferenceOption{Key: id, Label: label, Title: name, Tone: "info"})
		rev.WriteString(id + "|" + label + "|" + name + "|" + code + "|" + updated + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

func (r *Repository) listProtocolCategories(ctx context.Context, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT category, max(updated_at)::text
FROM protocol_definitions
WHERE tenant_id = $1::uuid
  AND status <> 'retired'
GROUP BY category
ORDER BY category
LIMIT 100`, tenantID)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: list protocol categories: %w", err)
	}
	defer rows.Close()
	var out []app.ReferenceOption
	var rev strings.Builder
	for rows.Next() {
		var key, updated string
		if err := rows.Scan(&key, &updated); err != nil {
			return nil, "", err
		}
		out = append(out, app.ReferenceOption{Key: key, Label: key})
		rev.WriteString(key + "|" + updated + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

func (r *Repository) listFeedItems(ctx context.Context, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT
  item_id::text,
  COALESCE(NULLIF(name, ''), item_code) AS label,
  name,
  COALESCE(item_code, '') AS code,
  updated_at::text
FROM inventory_items
WHERE tenant_id = $1::uuid
  AND category = 'feed'
  AND status = 'active'
ORDER BY name, item_id
LIMIT 500`, tenantID)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: list feed items: %w", err)
	}
	defer rows.Close()
	var out []app.ReferenceOption
	var rev strings.Builder
	for rows.Next() {
		var id, label, name, code, updated string
		if err := rows.Scan(&id, &label, &name, &code, &updated); err != nil {
			return nil, "", err
		}
		out = append(out, app.ReferenceOption{Key: id, Label: label, Title: name})
		rev.WriteString(id + "|" + label + "|" + name + "|" + code + "|" + updated + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

func (r *Repository) listBreeds(ctx context.Context) ([]app.ReferenceOption, string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT canonical_name, status, updated_at::text
FROM breeds
WHERE species = 'goat'
  AND status IN ('active', 'review')
ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, canonical_name
LIMIT 200`)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: list breeds: %w", err)
	}
	defer rows.Close()
	var out []app.ReferenceOption
	var rev strings.Builder
	for rows.Next() {
		var name, status, updated string
		if err := rows.Scan(&name, &status, &updated); err != nil {
			return nil, "", err
		}
		tone := ""
		if status == "review" {
			tone = "warn"
		}
		out = append(out, app.ReferenceOption{Key: name, Label: name, Tone: tone})
		rev.WriteString(name + "|" + status + "|" + updated + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

func (r *Repository) listStatuses(ctx context.Context, axis, defaultTone string) ([]app.ReferenceOption, string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT status_code, short_label, COALESCE(description, ''), updated_at::text
FROM status_definitions
WHERE axis = $1
  AND active = true
ORDER BY sort_order, status_code
LIMIT 200`, axis)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: list %s statuses: %w", axis, err)
	}
	defer rows.Close()
	var out []app.ReferenceOption
	var rev strings.Builder
	for rows.Next() {
		var code, label, title, updated string
		if err := rows.Scan(&code, &label, &title, &updated); err != nil {
			return nil, "", err
		}
		out = append(out, app.ReferenceOption{Key: code, Label: label, Title: title, Tone: defaultTone})
		rev.WriteString(code + "|" + label + "|" + title + "|" + updated + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

func (r *Repository) listSOPLabels(ctx context.Context, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT
  sv.sop_version_id::text,
  sd.name || ' · ' || sv.version_label AS label,
  sd.code,
  GREATEST(sd.updated_at, sv.updated_at)::text
FROM sop_versions sv
JOIN sop_definitions sd
  ON sd.tenant_id = sv.tenant_id
 AND sd.sop_id = sv.sop_id
WHERE sv.tenant_id = $1::uuid
  AND sd.status = 'active'
  AND sv.status = 'published'
ORDER BY sd.name, sv.version DESC, sv.sop_version_id
LIMIT 200`, tenantID)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: list sop labels: %w", err)
	}
	defer rows.Close()
	var out []app.ReferenceOption
	var rev strings.Builder
	for rows.Next() {
		var id, label, code, updated string
		if err := rows.Scan(&id, &label, &code, &updated); err != nil {
			return nil, "", err
		}
		out = append(out, app.ReferenceOption{Key: id, Label: label, Title: code})
		rev.WriteString(id + "|" + label + "|" + code + "|" + updated + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

func (r *Repository) listConfigFamilyRevisions(ctx context.Context, tenantID string) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT family_key, revision::text, content_hash, changed_at::text
FROM admin_ui_config_family_revisions
WHERE tenant_id = $1::uuid
ORDER BY family_key
LIMIT 1000`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("adminui: list config family revisions: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, revision, hash, changed string
		if err := rows.Scan(&key, &revision, &hash, &changed); err != nil {
			return nil, err
		}
		out[key] = revision + "|" + hash + "|" + changed
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) listUIConfigEntries(ctx context.Context, tenantID string) ([]app.ConfigEntry, string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT
  route_id,
  config_key,
  config_value,
  row_version::text,
  updated_at::text
FROM admin_ui_config_entries
WHERE tenant_id = $1::uuid
  AND status = 'active'
  AND locale = 'default'
ORDER BY route_id, config_key
LIMIT 5000`, tenantID)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: list ui config entries: %w", err)
	}
	defer rows.Close()
	var out []app.ConfigEntry
	var rev strings.Builder
	for rows.Next() {
		var routeID, key, value, rowVersion, updated string
		if err := rows.Scan(&routeID, &key, &value, &rowVersion, &updated); err != nil {
			return nil, "", err
		}
		out = append(out, app.ConfigEntry{RouteID: routeID, Key: key, Value: value})
		rev.WriteString(routeID + "|" + key + "|" + value + "|" + rowVersion + "|" + updated + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

const listSOPTaskTypesSQL = `
SELECT task_type_key, name, COALESCE(description, ''), answer_kind, updated_at::text
FROM sop_task_types
WHERE tenant_id = $1::uuid AND status = 'active'
ORDER BY sort_order, task_type_key
LIMIT 200`

// listSOPTaskTypes reads the Task Type Registry (migration 000308) for the SOP builder's
// follow-up step editor: one option list for the picker, one metadata twin carrying answer kinds.
func (r *Repository) listSOPTaskTypes(ctx context.Context, tenantID string) ([]app.ReferenceOption, []app.ReferenceOption, string, error) {
	rows, err := r.pool.Query(ctx, listSOPTaskTypesSQL, tenantID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("adminui: list sop task types: %w", err)
	}
	defer rows.Close()
	var types, kinds []app.ReferenceOption
	var rev strings.Builder
	for rows.Next() {
		var key, name, description, answer, updated string
		if err := rows.Scan(&key, &name, &description, &answer, &updated); err != nil {
			return nil, nil, "", err
		}
		types = append(types, app.ReferenceOption{Key: key, Label: name, Title: description})
		kinds = append(kinds, app.ReferenceOption{Key: key, Label: answer})
		rev.WriteString(key + "|" + name + "|" + answer + "|" + updated + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, nil, "", err
	}
	return types, kinds, rev.String(), nil
}

// listPenTypesSQL reads the farm's Pen types register (migration 000428), archived rows included
// (Title = "archived") so a pen still carrying one keeps its name on a chart. A handful of rows;
// the LIMIT is a backstop.
const listPenTypesSQL = `
SELECT pen_type_key, name, status, updated_at::text
FROM pen_types
WHERE tenant_id = $1::uuid
ORDER BY sort_order, lower(name), pen_type_key
LIMIT 200`

func (r *Repository) listPenTypes(ctx context.Context, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := r.pool.Query(ctx, listPenTypesSQL, tenantID)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: list pen types: %w", err)
	}
	defer rows.Close()
	var out []app.ReferenceOption
	var rev strings.Builder
	for rows.Next() {
		var key, name, status, updated string
		if err := rows.Scan(&key, &name, &status, &updated); err != nil {
			return nil, "", err
		}
		title := ""
		if status != "active" {
			title = "archived"
		}
		out = append(out, app.ReferenceOption{Key: key, Label: name, Title: title})
		rev.WriteString(key + "|" + name + "|" + status + "|" + updated + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

// listDesignationsSQL reads the designation catalog (a global, tens-of-rows table) for the
// SOP step editor's "Done by" select (SALES SOP, 2026-09-19).
const listDesignationsSQL = `
SELECT designation_code, label
FROM designation_catalog
WHERE status = 'active'
ORDER BY sort_order, designation_code
LIMIT 100`

func (r *Repository) listDesignations(ctx context.Context) ([]app.ReferenceOption, string, error) {
	rows, err := r.pool.Query(ctx, listDesignationsSQL)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: list designations: %w", err)
	}
	defer rows.Close()
	var out []app.ReferenceOption
	var rev strings.Builder
	for rows.Next() {
		var code, label string
		if err := rows.Scan(&code, &label); err != nil {
			return nil, "", err
		}
		out = append(out, app.ReferenceOption{Key: code, Label: label})
		rev.WriteString(code + "|" + label + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

// loadWeighingWeightsPages reads tenant DB configuration. SOP publication is not a
// calendar write path. The revision trigger invalidates bootstrap after SQL updates.
const sqlWeighingCalendarConfig = `
SELECT default_from_mode, COALESCE(default_from_date::text, ''),
       COALESCE(default_from_days, 0), COALESCE(default_from_weeks, 0), earliest_date::text
FROM public.weighing_calendar_config WHERE tenant_id = $1::uuid`

func (r *Repository) loadWeighingWeightsPages(ctx context.Context, tenantID string) (*app.WeighingCalendarConfig, string, error) {
	var rules app.WeighingCalendarConfig
	err := r.pool.QueryRow(ctx, sqlWeighingCalendarConfig, tenantID).Scan(&rules.DefaultFromMode, &rules.DefaultFromDate, &rules.DefaultFromDays, &rules.DefaultFromWeeks, &rules.EarliestDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("adminui: load weighing calendar config: %w", err)
	}
	return &rules, fmt.Sprintf("%+v", rules), nil
}
