package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// Read-only support for the operator-facing single-animal shifting flow.
//
// Both queries in this file READ locations/goats rows that other modules own. That is deliberate
// and stays within the module boundary rule, which forbids counts from WRITING another module's
// tables -- counts already reads locations for park/shed labels in the herd-register rollups
// (repository.go) and reads goats for the census aggregates. Nothing here mutates.

// shiftingDestinationCatalogQuery returns every active park with its active OPERATIONAL LOCATIONS
// (physical shed, or one row per real partition of that shed) in one round trip.
//
// Shape notes:
//   - LEFT JOIN, not INNER: a park with no sheds yet must still appear in the dropdown, otherwise a
//     newly-created park is invisible and an operator cannot report a movement into it.
//   - The status/retired_at filters live in the JOIN's ON clause for the shed side. Putting them in
//     WHERE would silently convert the LEFT JOIN back into an inner join and drop exactly those
//     empty parks.
//   - legacy partition-alias locations are suppressed at the shed JOIN through the SHARED
//     oploc.PartitionAliasExclusionSQL, not a local copy. If an active sibling parent shed
//     ("Castro") has an active partition catalog row "1", the old active shed row named "Castro 1"
//     is not a separate destination; the parent+partition row renders as "Castro - 1". The local
//     copy this replaced handled only that spelling: for "Mandela 1 - Part 3" the remainder is
//     "part3" against a normalized_label of "3", so every "- Part N" alias survived and the picker
//     served 195 rows for 120 real locations -- 75 exact-duplicate labels behind two shed ids.
//     Genuine same-named sheds in different parks remain because the sibling check is park-local.
//   - partitions comes from the shed_partitions CATALOG (status='active'), LEFT JOINed so a shed
//     with no catalog rows still yields exactly ONE destination row with partition_label NULL --
//     the bare, non-partitioned shed. A shed WITH real partitions returns one row per partition.
//     This is the critical difference from the prior goat_shed_partitions LATERAL: the catalog
//     includes EMPTY partitions (e.g. Yashoda 5) that no goat currently occupies, making them
//     reachable as shifting destinations. The partition_label returned to callers is the catalog's
//     human label; normalized_label remains an internal matching key and never drives display copy.
//   - animal_count is computed per operational location using the SAME normalization:
//     count of goats whose goat_shed_partitions.partition_label matches, zero for empty partitions.
//   - management_stages is computed per OPERATIONAL LOCATION (per pen for a partitioned shed, per
//     shed otherwise), on the same goat_shed_partitions normalization as animal_count. It was
//     originally shed-grain; once the typed-raise rulebook (2026-08-20) started resolving THE
//     PEN's adoptable tag from this field, shed grain made a homogeneous pen inside a mixed shed
//     read as "a mix of tags" and refused legitimate health raises (live incident: Godel 1 -
//     Part 1, CBE, 2026-08-29). A partitioned shed's animal with no partition row contributes to
//     no pen -- consistent with the head count, never smeared across every pen.
//   - ORDER BY name then id then partition_label: name is the human sort, the id tiebreak keeps the
//     order stable across calls when two sheds in the same park share a name (which happens), and
//     partition_label last keeps a partitioned shed's rows adjacent and stably ordered.
//
// The query materializes the bounded location/partition catalog once, and aggregates live residents
// once per (shed, normalized partition key). The older shape used two correlated goat scans and
// repeated the legacy partition-alias anti-join for every destination row; on the OCI staging clone
// that made the destination picker spend hundreds of milliseconds before any HTTP overhead.
//
// mobile-guard:ignore: bounded location catalog cached on-device, not a paginated feed
// scale-guard:ignore: bounded location catalog cached on-device, not a paginated feed
var shiftingDestinationCatalogQuery = `
WITH active_parks AS MATERIALIZED (
    SELECT location_id, tenant_id, name
    FROM locations
    WHERE tenant_id = $1::uuid
      AND location_type = 'park'
      AND status = 'active'
      AND retired_at IS NULL
),
active_sheds AS MATERIALIZED (
    SELECT location_id, tenant_id, parent_location_id, name
    FROM locations
    WHERE tenant_id = $1::uuid
      AND location_type = 'shed'
      AND status = 'active'
      AND retired_at IS NULL
),
active_partitions AS MATERIALIZED (
    SELECT tenant_id, shed_id, partition_label, normalized_label, animal_stage_id
    FROM shed_partitions
    WHERE tenant_id = $1::uuid
      AND status = 'active'
),
partition_alias_keys AS MATERIALIZED (
    SELECT
      parent_shed.parent_location_id,
      parent_shed.location_id,
      parent_shed.name,
      parent_partition.normalized_label
    FROM active_sheds parent_shed
    JOIN active_partitions parent_partition
      ON parent_partition.tenant_id = parent_shed.tenant_id
     AND parent_partition.shed_id = parent_shed.location_id
),
canonical_sheds AS MATERIALIZED (
    SELECT shed.*
    FROM active_sheds shed
    WHERE NOT EXISTS (
      SELECT 1
      FROM partition_alias_keys alias_key
      WHERE alias_key.parent_location_id = shed.parent_location_id
        AND alias_key.location_id <> shed.location_id
        AND starts_with(BTRIM(shed.name), BTRIM(alias_key.name))
        AND NULLIF(
          regexp_replace(
            BTRIM(replace(BTRIM(shed.name), BTRIM(alias_key.name), '')),
            '^\s*-\s*part\s*|\s+',
            '',
            'gi'
          ),
          ''
        ) = alias_key.normalized_label
    )
),
resident_agg AS MATERIALIZED (
    SELECT
      g.tenant_id,
      g.shed_id,
      regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') AS partition_key,
      COUNT(DISTINCT g.goat_id) AS animal_count,
      array_agg(DISTINCT btrim(g.management_stage) ORDER BY btrim(g.management_stage))
        FILTER (WHERE btrim(COALESCE(g.management_stage, '')) <> '') AS stages
    FROM goats g
    LEFT JOIN goat_shed_partitions gsp
           ON gsp.tenant_id = g.tenant_id
          AND gsp.goat_id = g.goat_id
    WHERE g.tenant_id = $1::uuid
      AND g.lifecycle_status = 'alive'
      AND g.exited_at IS NULL
    GROUP BY g.tenant_id, g.shed_id, regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
)
SELECT
    park.location_id::text,
    park.name,
    shed.location_id::text,
    shed.name,
    partitions.partition_label,
    COALESCE(residents.animal_count, 0),
    COALESCE(residents.stages, ARRAY[]::text[]),
    -- The cohort AUTHORED for this exact operational location: the pen's own tag when this row is
    -- a pen, the shed's profile when the shed has none. This is what a movement adopts; the
    -- resident-derived stages above remain only as the fallback for a location nobody has
    -- configured yet.
    COALESCE(CASE WHEN partitions.shed_id IS NOT NULL THEN pen_stage.stage_code ELSE shed_stage.stage_code END, '')
FROM active_parks park
LEFT JOIN canonical_sheds shed
       ON shed.tenant_id = park.tenant_id
      AND shed.parent_location_id = park.location_id
-- projection-review: membership=active parent sheds for the tenant LEFT JOINed to the shed_partitions CATALOG, which is the authoritative list of pens that physically exist (goat-derived membership would hide an EMPTY pen and make it unreachable as a destination); group_key=(shed_id, normalized partition label) -- the catalog's own primary key, so a pen appears at most once and a shed with no catalog rows still yields exactly one bare-shed row; join_cardinality=1:N by design (one shed -> its pens) with the animal count computed in a correlated subquery per pen rather than by joining goats, so no goat row can fan the catalog out; pagination=none, this catalog is bounded (two parks, ~154 sheds) and is returned whole; scope=tenant_id plus active/non-retired locations, which is what keeps inactive partition-alias rows out of the picker
LEFT JOIN active_partitions partitions
       ON partitions.tenant_id = park.tenant_id
      AND partitions.shed_id = shed.location_id
LEFT JOIN resident_agg residents
       ON residents.tenant_id = park.tenant_id
      AND residents.shed_id = shed.location_id
      AND residents.partition_key = COALESCE(partitions.normalized_label, 'whole')
LEFT JOIN shed_profiles destination_profile
       ON destination_profile.tenant_id = park.tenant_id
      AND destination_profile.location_id = shed.location_id
LEFT JOIN animal_stage_lookup shed_stage
       ON shed_stage.tenant_id = destination_profile.tenant_id
      AND shed_stage.animal_stage_id = destination_profile.animal_stage_id
      AND shed_stage.status = 'active'
LEFT JOIN animal_stage_lookup pen_stage
       ON pen_stage.tenant_id = partitions.tenant_id
      AND pen_stage.animal_stage_id = partitions.animal_stage_id
      AND pen_stage.status = 'active'
-- projection-review: membership=live non-exited goats standing in THIS operational location -- the
-- same per-pen partition normalization the resident_agg CTE above uses, so the stages a pen
-- offers and the heads a pen counts come from the SAME animal set; group_key=(shed_id, normalized
-- partition label) via the joined partitions row; join_cardinality=goats 1:0..1
-- goat_shed_partitions (PK tenant_id, goat_id), so no fan-out; a partitioned shed's animal with NO
-- partition row matches no pen and contributes to neither stages nor count -- excluded on the same
-- grain rather than smeared across every pen.
ORDER BY park.name, park.location_id, shed.name, shed.location_id, partitions.normalized_label NULLS FIRST`

// ShiftingDestinationCatalog implements ports.Repository.ShiftingDestinationCatalog.
//
// Deliberately unpaginated. This is a bounded configuration catalog -- the current tenant has 2
// parks and ~154 sheds, and the number is governed by how many sheds the business physically builds,
// not by herd size or event volume. The client fetches it once and caches it; it is never a scrolling
// list. That is why the guard annotations above name a bounded catalog rather than disabling a guard.
func (r *Repository) ShiftingDestinationCatalog(ctx context.Context, tenantID string) (domain.ShiftingDestinationCatalog, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting destination catalog: missing tenant id")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	catalogBound := sqlbind.MustBind(shiftingDestinationCatalogQuery, tenantID)
	rows, err := r.pool.Query(ctx, catalogBound.SQL(), catalogBound.Args()...)
	if err != nil {
		return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting destination catalog: %w", err)
	}
	defer rows.Close()

	out := domain.ShiftingDestinationCatalog{Parks: []domain.ShiftingDestinationPark{}}
	// The query is already ordered by park, so a single "did the park id change" check groups the
	// flat rows without a map -- and without re-sorting, which would fight the SQL ordering.
	parkIndex := map[string]int{}
	for rows.Next() {
		var parkID, parkName string
		var shedID, shedName, partitionLabel *string
		var animalCount int
		var shedStages []string
		var configuredStage string
		if err := rows.Scan(&parkID, &parkName, &shedID, &shedName, &partitionLabel, &animalCount, &shedStages, &configuredStage); err != nil {
			return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting destination catalog scan: %w", err)
		}
		idx, ok := parkIndex[parkID]
		if !ok {
			out.Parks = append(out.Parks, domain.ShiftingDestinationPark{
				ParkID: parkID,
				Name:   parkName,
				Sheds:  []domain.ShiftingDestinationShed{},
			})
			idx = len(out.Parks) - 1
			parkIndex[parkID] = idx
		}
		// A NULL shed id is the LEFT JOIN's "this park has no active sheds" row, not a data error.
		if shedID == nil || shedName == nil {
			continue
		}
		shedStages = nonClinicalShiftingStages(shedStages)
		// partitionLabel comes from the shed_partitions catalog's display label. A non-partitioned
		// shed comes through with partitionLabel nil -- exactly one entry, never synthesized as "whole".
		// animalCount is 0 for empty partitions (e.g. Yashoda 5 with no goats) and the true count
		// of goats occupying this partition for filled ones.
		loc := oploc.OperationalLocation{
			ParkID:   parkID,
			ParkName: parkName,
			ShedID:   *shedID,
			ShedName: *shedName,
		}
		if partitionLabel != nil {
			loc.PartitionLabel = *partitionLabel
		}
		out.Parks[idx].Sheds = append(out.Parks[idx].Sheds, domain.ShiftingDestinationShed{
			ShedID:           *shedID,
			Name:             *shedName,
			ManagementStages: shedStages,
			ConfiguredStage:  strings.TrimSpace(configuredStage),
			PartitionLabel:   partitionLabel,
			Display:          loc.Display(),
			// The per-pen live population: 0 for a real-but-empty pen. The typed shifting rules
			// key emptiness checks on this (spacing/delivery/flushing into an empty pen).
			HeadCount: animalCount,
		})
	}
	if err := rows.Err(); err != nil {
		return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting destination catalog rows: %w", err)
	}
	stagesBound := sqlbind.MustBind(`SELECT stage_code FROM animal_stage_lookup
WHERE tenant_id=$1::uuid AND status='active' ORDER BY sort_order, stage_code`, tenantID)
	stageRows, err := r.pool.Query(ctx, stagesBound.SQL(), stagesBound.Args()...)
	if err != nil {
		return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting management stages: %w", err)
	}
	defer stageRows.Close()
	for stageRows.Next() {
		var stage string
		if err := stageRows.Scan(&stage); err != nil {
			return domain.ShiftingDestinationCatalog{}, err
		}
		// The picker-facing list stays clinical-stripped; the complete list feeds the typed
		// shifting rulebook, whose per-type clinical refusals do the guarding instead.
		out.AllManagementStages = append(out.AllManagementStages, stage)
		if len(nonClinicalShiftingStages([]string{stage})) == 1 {
			out.ManagementStages = append(out.ManagementStages, stage)
		}
	}
	if err := stageRows.Err(); err != nil {
		return domain.ShiftingDestinationCatalog{}, err
	}
	return out, nil
}

func nonClinicalShiftingStages(stages []string) []string {
	out := make([]string, 0, len(stages))
	for _, stage := range stages {
		key := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(stage))), "_")
		clinical := false
		for _, blocked := range protocoldomain.MandatoryClinicalDeferStates {
			if key == strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(blocked))), "_") {
				clinical = true
				break
			}
		}
		if !clinical {
			out = append(out, stage)
		}
	}
	return out
}

// goatShiftingFactsQuery reads the narrow impact-shaped facts for the named animals.
//
// Predicate shape: goat_id is kept BARE and the bind array carries the cast
// (`goat_id = ANY($2::uuid[])`), never `goat_id::text = ANY($2::text[])` -- a column-side cast
// disables the ordinary index on the stored column.
//
// merged_into_goat_id IS NULL keeps merged aliases out. Terminal/exited goats deliberately remain
// visible to this narrow validator so the service can distinguish "this goat exists but cannot be
// shifted" (422) from "this goat id does not resolve in the tenant" (404). The actual relocation
// path repeats the current-membership guard under lock.
//
// scale-guard:ignore: bounded by MaxRelocateGoatsPerCommand, single set-based read by id
const goatShiftingFactsQuery = `
SELECT
    g.goat_id::text,
    g.lifecycle_status,
    g.exited_at,
    g.breed_id::text,
    COALESCE(
        NULLIF(btrim(b.canonical_name), ''),
        NULLIF(btrim(g.breed), ''),
        g.species
    ) AS breed_label,
    NULLIF(btrim(COALESCE(g.management_stage, '')), '') AS stage_tag,
    NULLIF(btrim(COALESCE(g.age_band, '')), '')         AS age_class,
    NULLIF(btrim(COALESCE(g.sex, '')), '')              AS sex,
    ((now() AT TIME ZONE 'Asia/Kolkata')::date - g.dob) AS age_days,
    g.park_id::text,
    g.shed_id::text,
    -- The goat's current partition within g.shed_id, when it sits in a partitioned shed. Excludes the
    -- 'whole' sentinel (same normalization as oploc.IsPartitioned) so a non-partitioned placement's
    -- goat_shed_partitions row (if one even exists) never surfaces as a fake partition.
    CASE
        WHEN regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') <> 'whole'
            THEN btrim(gsp.partition_label)
        ELSE NULL
    END AS shed_partition_label
FROM goats g
LEFT JOIN breeds b
       ON b.breed_id = g.breed_id
      AND b.status = 'active'
LEFT JOIN goat_shed_partitions gsp
       ON gsp.tenant_id = g.tenant_id
      AND gsp.goat_id = g.goat_id
WHERE g.tenant_id = $1::uuid
  AND g.goat_id = ANY($2::uuid[])
  AND g.merged_into_goat_id IS NULL
ORDER BY g.goat_id`

// GoatShiftingFacts implements ports.Repository.GoatShiftingFacts.
//
// BREED LABEL FALLBACK (deliberate, and the one derivation judgement here): the label prefers the
// canonical breeds.canonical_name, falls back to the goat's free-text goats.breed, and finally to
// goats.species (NOT NULL, defaults 'goat'). The species fallback exists so a missing breed
// attribute cannot BLOCK an operator from reporting a real movement. It is a truthful degradation
// to a coarser grain -- the animal genuinely is a goat -- not an invented label, and it keeps the
// derived impact satisfying the non-blank breed_key CHECK on shifting_event_impacts instead of
// failing at the database with an opaque error.
func (r *Repository) GoatShiftingFacts(ctx context.Context, tenantID string, goatIDs []string) ([]domain.GoatShiftingFact, error) {
	if strings.TrimSpace(tenantID) == "" || len(goatIDs) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	factsBound := sqlbind.MustBind(goatShiftingFactsQuery, tenantID, goatIDs)
	rows, err := r.pool.Query(ctx, factsBound.SQL(), factsBound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("counts: goat shifting facts: %w", err)
	}
	defer rows.Close()

	out := make([]domain.GoatShiftingFact, 0, len(goatIDs))
	for rows.Next() {
		var fact domain.GoatShiftingFact
		var breedLabel string
		if err := rows.Scan(&fact.GoatID, &fact.LifecycleStatus, &fact.ExitedAt,
			&fact.BreedID, &breedLabel, &fact.StageTag, &fact.AgeClass, &fact.Sex, &fact.AgeDays,
			&fact.ParkID, &fact.ShedID, &fact.ShedPartitionLabel); err != nil {
			return nil, fmt.Errorf("counts: goat shifting facts scan: %w", err)
		}
		fact.BreedLabel = strings.TrimSpace(breedLabel)
		// countAliasNorm is the SAME normalization the counts breed-alias resolver applies
		// (lowercase, whitespace runs collapsed to '_'). Reusing it is what makes a derived
		// single-animal impact group onto the same grain as an imported cohort impact for the same
		// breed, instead of forking the projection into two near-duplicate rows.
		fact.BreedKey = countAliasNorm(fact.BreedLabel)
		out = append(out, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("counts: goat shifting facts rows: %w", err)
	}
	return out, nil
}

// stageMinAgeDaysQuery reads each active stage's "From (days)" (Items & settings), the age the
// growth age-entry rule judges against. animal_stage_lookup is a tens-of-rows tenant catalog.
const stageMinAgeDaysQuery = `
SELECT stage_code, min_age_days
FROM animal_stage_lookup
WHERE tenant_id = $1::uuid AND status = 'active' AND min_age_days IS NOT NULL`

// StageMinAgeDays implements ports.Repository.StageMinAgeDays.
func (r *Repository) StageMinAgeDays(ctx context.Context, tenantID string) (map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	minAgesBound := sqlbind.MustBind(stageMinAgeDaysQuery, tenantID)
	rows, err := r.pool.Query(ctx, minAgesBound.SQL(), minAgesBound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("counts: stage min ages: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var code string
		var days int
		if err := rows.Scan(&code, &days); err != nil {
			return nil, fmt.Errorf("counts: stage min ages scan: %w", err)
		}
		out[code] = days
	}
	return out, rows.Err()
}
