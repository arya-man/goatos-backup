// Package postgres compiles DB-backed admin-web UI contract families.
package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	if out.FeedItems, out.RevisionInputs["feed-items"], err = r.listFeedItems(ctx, tenantID); err != nil {
		return out, err
	}
	return out, nil
}

func (r *Repository) listParks(ctx context.Context, tenantID string) ([]app.ReferenceOption, string, error) {
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
ORDER BY display_order, name, location_id
LIMIT 500`, tenantID)
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
