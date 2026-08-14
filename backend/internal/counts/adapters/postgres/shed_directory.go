package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// shedDirectoryQuery reads every active OPERATIONAL LOCATION with the cohort and capacity
// configured for it. Read-only, and read-only over tables other modules own (locations,
// shed_partitions, shed_profiles, animal_stage_lookup) exactly as this package's other census
// reads already do.
//
// Four shape notes, each of which a hand-written version of this query has got wrong before:
//
//   - GRAIN is the operational location: LEFT JOIN to the shed_partitions CATALOG yields one row
//     per active pen, and exactly one row with a NULL pen for a shed that has none. The catalog is
//     the membership source on purpose -- deriving pens from goat_shed_partitions would hide an
//     EMPTY pen, and an empty pen is still a place the farm built and configured.
//   - LEGACY PARTITION-ALIAS rows are excluded through the SHARED
//     oploc.PartitionAliasExclusionSQL. The farm's pens exist twice in `locations`: as the
//     canonical shed plus a catalog row ("Castro" + pen "1"), and as an old, still-active row
//     literally named "Castro 1". Without the exclusion every pen appears twice -- once properly
//     and once as a phantom shed holding no configuration at all.
//   - partition_label, never normalized_label. The first is the human label the screen renders
//     ("Part 3"); the second is a scrubbed matching key ("3") and rendering it produced
//     "Mandela 2 - 3" in a sibling module. The 'whole' sentinel cannot appear here because the
//     catalog forbids it (shed_partitions_not_whole).
//   - CAPACITY AND TAG both follow the grain: the pen's own value, falling back to the shed's
//     profile ONLY for a shed with no pens. Neither is a COALESCE onto the parent for a pen whose
//     own value is unset -- that would repeat the shed's figure on each of its ten pens and read as
//     though every pen held the whole shed, or carried a cohort nobody configured for it. A pen's
//     cohort lives on shed_partitions.animal_stage_id (migration 000161) and is what the directory
//     retags; shed_profiles.animal_stage_id remains the SHED-level cohort that shifting,
//     vaccination and the feed projection read, so the two can legitimately differ.
//
// Index: locations_tenant_parent_status_idx covers the shed side (tenant_id, parent_location_id,
// status, ...), locations_tenant_type_status_order_idx the park side, and
// shed_partitions_tenant_shed_idx the catalog (tenant_id, shed_id, status); shed_profiles is hit
// by primary key. No new index is required.
//
// projection-review: membership=active non-retired shed locations for the tenant (minus legacy
// partition aliases) LEFT JOINed to the active shed_partitions catalog, which is the same
// membership the shifting destination catalog uses, so the two screens name the same places;
// group_key=(park.location_id, shed.location_id, partitions.normalized_label) on the producer side
// and the composed operational-location display on the consumer side, where the consumer is the
// in-process pivot and the label key is deliberate and park-safe because every cell keeps its own
// park_id and shed_id; join_cardinality=shed_partitions is 1:N by design (one shed -> its pens,
// which IS the row grain), shed_profiles is 1:{0,1} on its location_id PRIMARY KEY and
// animal_stage_lookup is 1:{0,1} on animal_stage_id, so neither lookup can fan a pen into two rows
// and no aggregate is computed here at all; pagination=none, bounded configuration catalog of ~120
// pens governed by construction, not by herd size; scope=tenant_id on every table plus
// active/non-retired status on the park, the shed and the pen.
//
// scale-guard:ignore: bounded pen configuration catalog (~120 rows), not a herd-sized feed
var shedDirectoryQuery = `
SELECT
    park.location_id::text,
    COALESCE(park.location_code, ''),
    park.name,
    shed.location_id::text,
    BTRIM(shed.name),
    COALESCE(partitions.partition_label, ''),
    COALESCE(CASE WHEN partitions.shed_id IS NOT NULL THEN pen_stage.stage_code ELSE shed_stage.stage_code END, ''),
    CASE WHEN partitions.shed_id IS NOT NULL THEN partitions.capacity ELSE profile.capacity END
FROM locations shed
JOIN locations park
  ON park.tenant_id = shed.tenant_id
 AND park.location_id = shed.parent_location_id
 AND park.location_type = 'park'
 AND park.status = 'active'
 AND park.retired_at IS NULL
LEFT JOIN shed_partitions partitions
  ON partitions.tenant_id = shed.tenant_id
 AND partitions.shed_id = shed.location_id
 AND partitions.status = 'active'
LEFT JOIN shed_profiles profile
  ON profile.tenant_id = shed.tenant_id
 AND profile.location_id = shed.location_id
LEFT JOIN animal_stage_lookup shed_stage
  ON shed_stage.tenant_id = profile.tenant_id
 AND shed_stage.animal_stage_id = profile.animal_stage_id
 AND shed_stage.status = 'active'
LEFT JOIN animal_stage_lookup pen_stage
  ON pen_stage.tenant_id = partitions.tenant_id
 AND pen_stage.animal_stage_id = partitions.animal_stage_id
 AND pen_stage.status = 'active'
WHERE shed.tenant_id = $1::uuid
  AND shed.location_type = 'shed'
  AND shed.status = 'active'
  AND shed.retired_at IS NULL
  AND ` + oploc.PartitionAliasExclusionSQL("shed") + `
ORDER BY BTRIM(shed.name), park.name, park.location_id, shed.location_id, partitions.normalized_label NULLS FIRST`

// ShedDirectory implements ports.Repository.ShedDirectory.
//
// Deliberately unpaginated: this is a bounded configuration catalog, the same posture as
// ShiftingDestinationCatalog above it. The number of rows is how many pens the business has built.
func (r *Repository) ShedDirectory(ctx context.Context, tenantID string) (domain.ShedDirectory, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ShedDirectory{}, fmt.Errorf("counts: shed directory: missing tenant id")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, shedDirectoryQuery, tenantID)
	if err != nil {
		return domain.ShedDirectory{}, fmt.Errorf("counts: shed directory: %w", err)
	}
	defer rows.Close()

	entries := []domain.ShedDirectoryEntry{}
	for rows.Next() {
		var entry domain.ShedDirectoryEntry
		if err := rows.Scan(
			&entry.ParkID, &entry.ParkCode, &entry.ParkLabel,
			&entry.ShedID, &entry.ShedName, &entry.PartitionLabel,
			&entry.Tag, &entry.Capacity,
		); err != nil {
			return domain.ShedDirectory{}, fmt.Errorf("counts: shed directory scan: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return domain.ShedDirectory{}, fmt.Errorf("counts: shed directory: %w", err)
	}

	return domain.PivotShedDirectory(entries), nil
}
