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
	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
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

	// The fourteen family reads are independent, tiny lookups. They go to the server as ONE
	// pipelined batch on one connection -- one round trip -- instead of fourteen queries four at
	// a time (four round trips and four pool acquisitions on every cold bootstrap). Results are
	// read in queue order: the first failing read (in that order) is the returned error and the
	// families before it are filled exactly as the sequential version filled them.
	localScope := localVaccinationParkScope()
	batch := &pgx.Batch{}
	batch.Queue(sqlListParks, tenantID, localScope)
	batch.Queue(sqlListProtocolCategories, tenantID)
	batch.Queue(sqlListBreeds, tenantID)
	batch.Queue(sqlListStatuses, "health")
	batch.Queue(sqlListStatuses, "reproductive")
	batch.Queue(sqlListStatuses, "lifecycle")
	batch.Queue(sqlListStatuses, "health")
	batch.Queue(sqlListSOPLabels, tenantID)
	batch.Queue(sqlWeighingCalendarConfig, tenantID)
	batch.Queue(sqlListFeedItems, tenantID)
	batch.Queue(listSOPTaskTypesSQL, tenantID)
	batch.Queue(listDesignationsSQL)
	batch.Queue(sqlListUIConfigEntries, tenantID)
	batch.Queue(sqlListConfigFamilyRevisions, tenantID)
	batch.Queue(listPenTypesSQL, tenantID)
	batch.Queue(allBreedsSQL, tenantID)
	queued := make([]string, 0, len(batch.QueuedQueries))
	for _, qq := range batch.QueuedQueries {
		queued = append(queued, qq.SQL)
	}
	results := r.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()
	q := &batchReader{results: results, queued: queued}

	out := app.ReferenceFamilies{RevisionInputs: map[string]string{}}
	var (
		rev string
		err error
	)
	// Each read assigns its (possibly zero) result before its error is checked, as before.
	out.Parks, rev, err = r.listParks(ctx, q, tenantID)
	out.RevisionInputs["locations"] = rev
	if err != nil {
		return out, err
	}
	out.RuleCategories, rev, err = r.listProtocolCategories(ctx, q, tenantID)
	out.RevisionInputs["protocol-categories"] = rev
	if err != nil {
		return out, err
	}
	out.Breeds, rev, err = r.listBreeds(ctx, q, tenantID)
	out.RevisionInputs["breeds"] = rev
	if err != nil {
		return out, err
	}
	out.HealthStatuses, rev, err = r.listStatuses(ctx, q, "health", "")
	out.RevisionInputs["health-statuses"] = rev
	if err != nil {
		return out, err
	}
	out.ReproductiveStates, rev, err = r.listStatuses(ctx, q, "reproductive", "")
	out.RevisionInputs["reproductive-statuses"] = rev
	if err != nil {
		return out, err
	}
	out.LifecycleStates, rev, err = r.listStatuses(ctx, q, "lifecycle", "")
	out.RevisionInputs["lifecycle-statuses"] = rev
	if err != nil {
		return out, err
	}
	out.DeferStates, rev, err = r.listStatuses(ctx, q, "health", "warn")
	out.RevisionInputs["defer-states"] = rev
	if err != nil {
		return out, err
	}
	out.SOPLabels, rev, err = r.listSOPLabels(ctx, q, tenantID)
	out.RevisionInputs["sop-labels"] = rev
	if err != nil {
		return out, err
	}
	out.WeighingWeightsPages, rev, err = r.loadWeighingWeightsPages(ctx, q, tenantID)
	out.RevisionInputs["weighing-weights-pages"] = rev
	if err != nil {
		return out, err
	}
	out.FeedItems, rev, err = r.listFeedItems(ctx, q, tenantID)
	out.RevisionInputs["feed-items"] = rev
	if err != nil {
		return out, err
	}
	out.SOPTaskTypes, out.SOPTaskTypeAnswerKinds, rev, err = r.listSOPTaskTypes(ctx, q, tenantID)
	out.RevisionInputs["sop-task-types"] = rev
	if err != nil {
		return out, err
	}
	out.Designations, rev, err = r.listDesignations(ctx, q)
	out.RevisionInputs["designations"] = rev
	if err != nil {
		return out, err
	}
	out.UIConfig, rev, err = r.listUIConfigEntries(ctx, q, tenantID)
	out.RevisionInputs["admin-ui-config-values"] = rev
	if err != nil {
		return out, err
	}
	configRevisions, err := r.listConfigFamilyRevisions(ctx, q, tenantID)
	if err != nil {
		return out, err
	}
	for key, value := range configRevisions {
		out.RevisionInputs["admin-ui:"+key] = value
	}
	out.PenTypes, rev, err = r.listPenTypes(ctx, q, tenantID)
	out.RevisionInputs["pen-types"] = rev
	if err != nil {
		return out, err
	}
	out.AllBreeds, rev, err = r.listAllBreeds(ctx, q, tenantID)
	out.RevisionInputs["all-breeds"] = rev
	if err != nil {
		return out, err
	}
	// Species and sexes go through the shared animalvocab reader (the one every write path
	// validates against), so they are read on the pool rather than queued in the batch.
	out.Species, rev, err = r.listAnimalVocabulary(ctx, animalvocab.ListSpecies, tenantID)
	out.RevisionInputs["species"] = rev
	if err != nil {
		return out, err
	}
	out.Sexes, rev, err = r.listAnimalVocabulary(ctx, animalvocab.ListSexes, tenantID)
	out.RevisionInputs["sexes"] = rev
	if err != nil {
		return out, err
	}
	return out, nil
}

// querier is the read surface the family reads use: the pool, or a pipelined batch.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// batchReader hands out a sent batch's results in queue order. Each read names the SQL it
// expects, so a read taken out of order fails loudly instead of scanning another family's rows.
type batchReader struct {
	results pgx.BatchResults
	queued  []string
	next    int
}

func (b *batchReader) take(sql string) error {
	if b.next >= len(b.queued) || b.queued[b.next] != sql {
		return fmt.Errorf("adminui: family batch read out of queue order at %d", b.next)
	}
	b.next++
	return nil
}

func (b *batchReader) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if err := b.take(sql); err != nil {
		return nil, err
	}
	return b.results.Query()
}

func (b *batchReader) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if err := b.take(sql); err != nil {
		return errRow{err: err}
	}
	return b.results.QueryRow()
}

type errRow struct{ err error }

func (e errRow) Scan(...any) error { return e.err }

// localVaccinationParkScope narrows park options to vaccination-assigned parks on local stacks.
func localVaccinationParkScope() bool {
	return os.Getenv("GOATOS_ENV") == "local"
}

func (r *Repository) LoadContractFamilyRevisions(ctx context.Context, tenantID string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	revisions, err := r.listConfigFamilyRevisions(ctx, r.pool, tenantID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for key, value := range revisions {
		out["admin-ui:"+key] = value
	}
	return out, nil
}

func (r *Repository) listParks(ctx context.Context, q querier, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := q.Query(ctx, sqlListParks, tenantID, localVaccinationParkScope())
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
		out = append(out, app.ReferenceOption{Key: id, Label: label, Title: name, Tone: "info", Code: strings.TrimSpace(code)})
		rev.WriteString(id + "|" + label + "|" + name + "|" + code + "|" + updated + "\n")
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

func (r *Repository) listProtocolCategories(ctx context.Context, q querier, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := q.Query(ctx, sqlListProtocolCategories, tenantID)
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

func (r *Repository) listFeedItems(ctx context.Context, q querier, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := q.Query(ctx, sqlListFeedItems, tenantID)
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

// allBreedsSQL is every species' breeds from the farm's own breed list (breeds.tenant_id, 000442).
//
// `carried` says whether a live animal of that species carries the breed today, which is what the
// Counts breed correction offers (maintainer instruction 2026-09-26: "breeds in which we have
// animals only"). It is an index probe per breed on goats_tenant_breed_display_idx
// (tenant_id, breed) over a list of tens of breeds, and it names the breed exactly as goats.breed
// stores it -- every writer (Register animal, birth, census correction, breed rename) writes the
// register's canonical name.
// scale-guard:plan-proof-exempt: one EXISTS probe per breed row (tens per farm) on the (tenant_id, breed) prefix of goats_tenant_breed_display_idx; no scan of goats.
const allBreedsSQL = `
SELECT b.canonical_name, b.species, b.status, b.updated_at::text,
       EXISTS (SELECT 1 FROM goats g
               WHERE g.tenant_id = $1::uuid AND g.merged_into_goat_id IS NULL
                 AND g.breed = b.canonical_name AND g.species = b.species
                 AND g.lifecycle_status = 'alive') AS carried
FROM breeds b
WHERE b.tenant_id = $1::uuid
  AND b.status IN ('active', 'review')
ORDER BY CASE b.status WHEN 'active' THEN 0 ELSE 1 END, b.canonical_name
LIMIT 500`

// listAllBreeds is every species' breeds (the herd filter covers sheep as well as goats), each
// carrying its species so Register animal can offer only the chosen species' breeds.
func (r *Repository) listAllBreeds(ctx context.Context, q querier, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := q.Query(ctx, allBreedsSQL, tenantID)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: list all breeds: %w", err)
	}
	defer rows.Close()
	var out []app.ReferenceOption
	var rev strings.Builder
	seen := map[string]bool{}
	for rows.Next() {
		var name, species, status, updated string
		var carried bool
		if err := rows.Scan(&name, &species, &status, &updated, &carried); err != nil {
			return nil, "", err
		}
		rev.WriteString(fmt.Sprintf("%s|%s|%s|%s|%t\n", name, species, status, updated, carried))
		// One option per (breed, species), not per name: a farm may keep the same breed name
		// under two species, and Register animal offers only the chosen species' breeds, so
		// dropping the second copy hid it from that species entirely. Species-less lists dedupe
		// by name themselves (herdFilterBreeds).
		if seen[species+"\x00"+name] {
			continue
		}
		seen[species+"\x00"+name] = true
		tone := ""
		if status == "review" {
			tone = "warn"
		}
		out = append(out, app.ReferenceOption{Key: name, Label: name, Tone: tone, Group: species, Carried: carried})
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, rev.String(), nil
}

// listAnimalVocabulary reads one of Configuration's animal lists (species or gender) through the
// shared reader every write path validates against, so a picker never offers a code a write
// refuses. Key = code, Label = the farm's name for it.
func (r *Repository) listAnimalVocabulary(
	ctx context.Context,
	read func(context.Context, animalvocab.Querier, string) ([]animalvocab.Entry, error),
	tenantID string,
) ([]app.ReferenceOption, string, error) {
	entries, err := read(ctx, r.pool, tenantID)
	if err != nil {
		return nil, "", fmt.Errorf("adminui: %w", err)
	}
	out := make([]app.ReferenceOption, 0, len(entries))
	var rev strings.Builder
	for _, e := range entries {
		out = append(out, app.ReferenceOption{Key: e.Code, Label: e.Name})
		rev.WriteString(e.Code + "|" + e.Name + "\n")
	}
	return out, rev.String(), nil
}

func (r *Repository) listBreeds(ctx context.Context, q querier, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := q.Query(ctx, sqlListBreeds, tenantID)
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

func (r *Repository) listStatuses(ctx context.Context, q querier, axis, defaultTone string) ([]app.ReferenceOption, string, error) {
	rows, err := q.Query(ctx, sqlListStatuses, axis)
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

func (r *Repository) listSOPLabels(ctx context.Context, q querier, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := q.Query(ctx, sqlListSOPLabels, tenantID)
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

func (r *Repository) listConfigFamilyRevisions(ctx context.Context, q querier, tenantID string) (map[string]string, error) {
	rows, err := q.Query(ctx, sqlListConfigFamilyRevisions, tenantID)
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

func (r *Repository) listUIConfigEntries(ctx context.Context, q querier, tenantID string) ([]app.ConfigEntry, string, error) {
	rows, err := q.Query(ctx, sqlListUIConfigEntries, tenantID)
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
func (r *Repository) listSOPTaskTypes(ctx context.Context, q querier, tenantID string) ([]app.ReferenceOption, []app.ReferenceOption, string, error) {
	rows, err := q.Query(ctx, listSOPTaskTypesSQL, tenantID)
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

// listDesignationsSQL reads the designation catalog (a global, tens-of-rows table) for the
// SOP step editor's "Done by" select (SALES SOP, 2026-09-19).
const listDesignationsSQL = `
SELECT designation_code, label
FROM designation_catalog
WHERE status = 'active'
ORDER BY sort_order, designation_code
LIMIT 100`

func (r *Repository) listDesignations(ctx context.Context, q querier) ([]app.ReferenceOption, string, error) {
	rows, err := q.Query(ctx, listDesignationsSQL)
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

func (r *Repository) loadWeighingWeightsPages(ctx context.Context, q querier, tenantID string) (*app.WeighingCalendarConfig, string, error) {
	var rules app.WeighingCalendarConfig
	err := q.QueryRow(ctx, sqlWeighingCalendarConfig, tenantID).Scan(&rules.DefaultFromMode, &rules.DefaultFromDate, &rules.DefaultFromDays, &rules.DefaultFromWeeks, &rules.EarliestDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("adminui: load weighing calendar config: %w", err)
	}
	return &rules, fmt.Sprintf("%+v", rules), nil
}

const sqlListParks = `
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
LIMIT 500`

const sqlListProtocolCategories = `
SELECT category, max(updated_at)::text
FROM protocol_definitions
WHERE tenant_id = $1::uuid
  AND status <> 'retired'
GROUP BY category
ORDER BY category
LIMIT 100`

const sqlListFeedItems = `
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
LIMIT 500`

// sqlListBreeds is the species-less breed list (feed_breeds, counts_breed, rule_breeds): every
// species' breeds, one option per breed NAME. It was goat-only until the 2026-09-26 audit, which
// left a sheep -- or any configured species -- unable to be corrected to its own breed on Counts.
// A name the farm keeps under two species is one option; the write that takes it resolves the
// breed within each animal's own species.
const sqlListBreeds = `
SELECT canonical_name, status, updated_at::text
FROM (
  SELECT DISTINCT ON (lower(btrim(canonical_name))) canonical_name, status, updated_at
  FROM breeds
  WHERE tenant_id = $1::uuid
    AND status IN ('active', 'review')
  ORDER BY lower(btrim(canonical_name)), CASE status WHEN 'active' THEN 0 ELSE 1 END, updated_at DESC
) b
ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, canonical_name
LIMIT 500`

const sqlListStatuses = `
SELECT status_code, short_label, COALESCE(description, ''), updated_at::text
FROM status_definitions
WHERE axis = $1
  AND active = true
ORDER BY sort_order, status_code
LIMIT 200`

const sqlListSOPLabels = `
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
LIMIT 200`

const sqlListConfigFamilyRevisions = `
SELECT family_key, revision::text, content_hash, changed_at::text
FROM admin_ui_config_family_revisions
WHERE tenant_id = $1::uuid
ORDER BY family_key
LIMIT 1000`

const sqlListUIConfigEntries = `
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
LIMIT 5000`

// listPenTypesSQL reads the farm's Pen types register (migration 000437), archived rows included
// (Title = "archived") so a pen still carrying one keeps its name on a chart. A handful of rows;
// the LIMIT is a backstop.
const listPenTypesSQL = `
SELECT pen_type_key, name, status, updated_at::text
FROM pen_types
WHERE tenant_id = $1::uuid
ORDER BY sort_order, lower(name), pen_type_key
LIMIT 200`

func (r *Repository) listPenTypes(ctx context.Context, q querier, tenantID string) ([]app.ReferenceOption, string, error) {
	rows, err := q.Query(ctx, listPenTypesSQL, tenantID)
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
