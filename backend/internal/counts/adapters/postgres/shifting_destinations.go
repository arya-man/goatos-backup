package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
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
//   - partitions comes from the shed_partitions CATALOG (status='active'), LEFT JOINed so a shed
//     with no catalog rows still yields exactly ONE destination row with partition_label NULL --
//     the bare, non-partitioned shed. A shed WITH real partitions returns one row per partition.
//     This is the critical difference from the prior goat_shed_partitions LATERAL: the catalog
//     includes EMPTY partitions (e.g. Yashoda 5) that no goat currently occupies, making them
//     reachable as shifting destinations. The partition_label here is the normalized_label from
//     the catalog, never a raw 'whole' sentinel.
//   - animal_count is computed per operational location using the SAME normalization:
//     count of goats whose goat_shed_partitions.partition_label matches, zero for empty partitions.
//   - management_stages is computed per SHED (not per partition): the cohort vocabulary offered to
//     the operator is a shed-level fact today: goats do not carry a partition-scoped stage set.
//   - ORDER BY name then id then partition_label: name is the human sort, the id tiebreak keeps the
//     order stable across calls when two sheds in the same park share a name (which happens), and
//     partition_label last keeps a partitioned shed's rows adjacent and stably ordered.
//
// Index: locations_tenant_type_status_order_idx covers the park side
// (tenant_id, location_type, status, ...); shed_partitions_tenant_shed_idx covers the catalog
// (tenant_id, shed_id, status). No new index is required.
//
// mobile-guard:ignore: bounded location catalog cached on-device, not a paginated feed
// scale-guard:ignore: bounded location catalog cached on-device, not a paginated feed
const shiftingDestinationCatalogQuery = `
SELECT
    park.location_id::text,
    park.name,
    shed.location_id::text,
    shed.name,
    partitions.normalized_label,
    COALESCE(animal_count.count, 0),
    COALESCE(stage_agg.stages, ARRAY[]::text[])
FROM locations park
LEFT JOIN locations shed
       ON shed.tenant_id = park.tenant_id
      AND shed.parent_location_id = park.location_id
      AND shed.location_type = 'shed'
      AND shed.status = 'active'
      AND shed.retired_at IS NULL
-- projection-review: membership=active parent sheds for the tenant LEFT JOINed to the shed_partitions CATALOG, which is the authoritative list of pens that physically exist (goat-derived membership would hide an EMPTY pen and make it unreachable as a destination); group_key=(shed_id, normalized partition label) -- the catalog's own primary key, so a pen appears at most once and a shed with no catalog rows still yields exactly one bare-shed row; join_cardinality=1:N by design (one shed -> its pens) with the animal count computed in a correlated subquery per pen rather than by joining goats, so no goat row can fan the catalog out; pagination=none, this catalog is bounded (two parks, ~154 sheds) and is returned whole; scope=tenant_id plus active/non-retired locations, which is what keeps inactive partition-alias rows out of the picker
LEFT JOIN shed_partitions partitions
       ON partitions.tenant_id = park.tenant_id
      AND partitions.shed_id = shed.location_id
      AND partitions.status = 'active'
LEFT JOIN LATERAL (
    SELECT COUNT(DISTINCT g.goat_id) AS count
    FROM goats g
    LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
    WHERE g.tenant_id = park.tenant_id
      AND g.shed_id = shed.location_id
      AND g.lifecycle_status = 'alive'
      AND g.exited_at IS NULL
      AND (
        -- For this partition, count goats whose partition_label matches (after normalization)
        CASE WHEN partitions.normalized_label IS NOT NULL THEN
          regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') = partitions.normalized_label
        ELSE
          -- For non-partitioned shed, count all goats with whole/null partition
          regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') = 'whole'
        END
      )
) animal_count ON shed.location_id IS NOT NULL
LEFT JOIN LATERAL (
    SELECT array_agg(DISTINCT btrim(g.management_stage) ORDER BY btrim(g.management_stage)) AS stages
    FROM goats g
    WHERE g.tenant_id = park.tenant_id AND g.shed_id = shed.location_id
      AND g.lifecycle_status = 'alive' AND g.exited_at IS NULL
      AND btrim(COALESCE(g.management_stage, '')) <> ''
) stage_agg ON shed.location_id IS NOT NULL
WHERE park.tenant_id = $1::uuid
  AND park.location_type = 'park'
  AND park.status = 'active'
  AND park.retired_at IS NULL
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

	rows, err := r.pool.Query(ctx, shiftingDestinationCatalogQuery, tenantID)
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
		if err := rows.Scan(&parkID, &parkName, &shedID, &shedName, &partitionLabel, &animalCount, &shedStages); err != nil {
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
		// partitionLabel comes from the shed_partitions catalog (normalized_label). A non-partitioned
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
			PartitionLabel:   partitionLabel,
			Display:          loc.Display(),
		})
		_ = animalCount // captured for completeness; not used in this API layer
	}
	if err := rows.Err(); err != nil {
		return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting destination catalog rows: %w", err)
	}
	stageRows, err := r.pool.Query(ctx, `SELECT stage_code FROM animal_stage_lookup
WHERE tenant_id=$1::uuid AND status='active' ORDER BY sort_order, stage_code`, tenantID)
	if err != nil {
		return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting management stages: %w", err)
	}
	defer stageRows.Close()
	for stageRows.Next() {
		var stage string
		if err := stageRows.Scan(&stage); err != nil {
			return domain.ShiftingDestinationCatalog{}, err
		}
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

	rows, err := r.pool.Query(ctx, goatShiftingFactsQuery, tenantID, goatIDs)
	if err != nil {
		return nil, fmt.Errorf("counts: goat shifting facts: %w", err)
	}
	defer rows.Close()

	out := make([]domain.GoatShiftingFact, 0, len(goatIDs))
	for rows.Next() {
		var fact domain.GoatShiftingFact
		var breedLabel string
		if err := rows.Scan(&fact.GoatID, &fact.LifecycleStatus, &fact.ExitedAt,
			&fact.BreedID, &breedLabel, &fact.StageTag, &fact.AgeClass, &fact.Sex,
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
