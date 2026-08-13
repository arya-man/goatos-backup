package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// resolvePartitionLabels maps each campaign-shed location_id to its partition label, using the
// `shed_partitions` CATALOG as the authority.
//
// Why this exists: a partition label cannot be inferred from a name. "Castro 2" may be partition 2
// of Castro, or an ordinary shed whose name simply ends in 2 (the canonical fixture pins
// "Mandela 1" as exactly that). An earlier version of this code regex-split any trailing number
// and stamped false partitions on ordinary sheds; removing that inference then swung the other way
// and dropped REAL numeric partitions on every new write, because migration 000121's catalog
// backfill only ran once. Neither guess is acceptable, so the write path asks the catalog.
//
// weighing may read `shed_partitions` (ORG-scoped catalog of partitions that exist, no per-animal
// data) per the maintainer decision of 2026-08-06. `goat_shed_partitions` — which animal sits
// where — remains banned and is not touched here.
//
// One set-based query for the whole campaign, never per shed: resolving inside the shed loop would
// be an N+1 in the campaign write path.
func resolvePartitionLabels(ctx context.Context, tx pgx.Tx, tenantID string, locationIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(locationIDs))
	if len(locationIDs) == 0 {
		return out, nil
	}
	// For each requested location: take its catalog name, find the LONGEST active shed name in the
	// same park that prefixes it followed by a separator (so "Godel 1 - Part 3" resolves to parent
	// "Godel 1", never "Godel"), extract the remainder as the label, and keep it ONLY if the
	// catalog actually lists that partition for that parent. Normalization matches
	// oploc.NormalizePartition, so 'Part 3' and '3' are one partition.
	rows, err := tx.Query(ctx, `
SELECT loc.location_id::text, sp.partition_label
FROM locations loc
LEFT JOIN LATERAL (
    SELECT DISTINCT ON (loc.location_id) shed.location_id AS parent_id, shed.name AS parent_name
    FROM locations shed
    WHERE shed.tenant_id = loc.tenant_id
      AND shed.parent_location_id = loc.parent_location_id
      AND shed.location_type = 'shed'
      AND shed.status = 'active'
      AND shed.retired_at IS NULL
      AND shed.name <> loc.name
      AND ((loc.name LIKE shed.name || ' %') OR (loc.name LIKE shed.name || ' - %'))
    ORDER BY loc.location_id, length(shed.name) DESC
) parent ON true
LEFT JOIN shed_partitions sp
  ON sp.tenant_id = loc.tenant_id
 AND sp.shed_id = parent.parent_id
 AND sp.status = 'active'
 AND sp.normalized_label = lower(btrim(regexp_replace(
       btrim(substr(loc.name, length(parent.parent_name) + 1)),
       '^[[:space:]]*-?[[:space:]]*(part[[:space:]]+)?', '', 'i')))
WHERE loc.tenant_id = $1::uuid
  AND loc.location_id = ANY($2::uuid[])
  AND sp.partition_label IS NOT NULL`, tenantID, locationIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var locationID, label string
		if err := rows.Scan(&locationID, &label); err != nil {
			return nil, err
		}
		if strings.TrimSpace(label) != "" && oploc.IsPartitioned(label) {
			out[locationID] = label
		}
	}
	return out, rows.Err()
}

// partitionLabelFor returns the catalog-resolved label for a shed, falling back to the EXPLICIT
// worded suffix in the display name ("Godel 1 - Part 3"). The fallback covers a shed whose
// partition is named in the catalog under a different row shape; it never invents a numeric
// partition from a trailing number.
func partitionLabelFor(resolved map[string]string, locationID, displayName string) string {
	if label, ok := resolved[locationID]; ok && strings.TrimSpace(label) != "" {
		return label
	}
	_, label := splitShedPartitionName(displayName)
	return label
}
