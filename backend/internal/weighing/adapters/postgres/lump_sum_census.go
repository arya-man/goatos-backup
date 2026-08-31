package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// LUMP-SUM CENSUS SNAPSHOT (maintainer decision 2026-08-24).
//
// This file is the SECOND — and only other — file-scoped exemption to the
// weighing isolation rule, recorded in AGENTS.md and allowlisted BY NAME in
// tools/agent-hooks/check-weighing-free-flow-guard.mjs (HERD_JOIN_EXEMPT_FILES).
// It exists because the operator NO LONGER TYPES the lump-sum head count: the
// farm kept receiving wrong typed counts, so the maintainer ruled that the
// submit transaction snapshots the bucket's CURRENT resident head count from
// the herd register instead, freezes it on weighing_shed_observations.These are
// the boundaries that keep it safe, and any future edit must preserve them:
//
//   - ONE read, ONE fact: COUNT of live residents of the bucket's own
//     (shed, pen). No identity, breed, sex, stage, clinical or vaccination fact
//     leaves this query — weighing still cannot say WHICH animals were weighed.
//   - It is called from exactly one place: RecordShedObservation's submit
//     transaction, to fill animal_count. It gates nothing about individual
//     free-flow capture: scans are still accepted verbatim, unknown tags are
//     still counted, and no roster/expected-set exists.
//   - The ONE gate it introduces is deliberate and maintainer-ruled: a bucket
//     whose register census is ZERO refuses the lump-sum submit
//     (ports.ErrShedCountUnavailable) — inventing a head count would store an
//     average nobody measured.
//   - The snapshot is FROZEN: nothing recomputes it after submit. A later herd
//     move does not touch the stored row, and the verifier's weight correction
//     recomputes the average against this same frozen count.
//
// Widening this — another caller, another column, another table, or any use on
// the individual-capture path — is a MAINTAINER decision, never a developer
// convenience.
//
// projection-review: producer grain is goats (PK goat_id; one row per animal,
// filtered tenant_id + shed_id + lifecycle_status='alive' + exited_at IS NULL)
// LEFT JOINed 1:{0,1} to goat_shed_partitions (PK tenant_id, goat_id — cannot
// fan out). Consumer grain is one (campaign_shed) bucket = one (shed, pen), so
// COUNT(*) ranges over exactly the animals resident in that pen; numerator and
// denominator of the derived average (weight_kg / animal_count) both range over
// the single submitted bucket.
const lumpSumCensusCountSQL = `
WITH bucket AS (
  SELECT l.tenant_id, l.location_id, l.parent_location_id, l.name,
         NULLIF(btrim($3::text), '') AS bucket_partition
  FROM locations l
  WHERE l.tenant_id = $1::uuid
    AND l.location_id = $2::uuid
),
direct_count AS (
  SELECT COUNT(*)::int AS animals
  FROM goats g
  JOIN bucket b ON b.tenant_id = g.tenant_id
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
  WHERE g.shed_id = b.location_id
    AND g.lifecycle_status = 'alive'
    AND g.exited_at IS NULL
    AND (
      COALESCE(b.bucket_partition, '') = ''
      OR regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
         = regexp_replace(lower(btrim(b.bucket_partition)), '^part[[:space:]]+', '')
    )
),
parsed_alias AS (
  SELECT parent.location_id AS shed_id,
         parent.name AS shed_name,
         b.bucket_partition,
         NULLIF(btrim(regexp_replace(
           substr(b.name, length(parent.name) + 1),
           '^[[:space:]]*-?[[:space:]]*(?:Part[[:space:]]*)?',
           ''
         )), '') AS parsed_partition
  FROM bucket b
  JOIN LATERAL (
    SELECT shed.location_id, shed.name
    FROM locations shed
    WHERE shed.tenant_id = b.tenant_id
      AND shed.parent_location_id = b.parent_location_id
      AND shed.location_type = 'shed'
      AND shed.status = 'active'
      AND shed.retired_at IS NULL
      AND shed.location_id <> b.location_id
      AND ((b.name LIKE shed.name || ' %') OR (b.name LIKE shed.name || ' - %'))
    ORDER BY length(shed.name) DESC, shed.name
    LIMIT 1
  ) parent ON true
  WHERE (SELECT animals FROM direct_count) = 0
),
alias_target AS (
  SELECT p.shed_id,
         COALESCE(p.bucket_partition, sp.partition_label) AS partition_label
  FROM parsed_alias p
  JOIN shed_partitions sp
    ON sp.tenant_id = $1::uuid
   AND sp.shed_id = p.shed_id
   AND sp.status = 'active'
   AND sp.source = 'location_alias'
   AND sp.normalized_label = regexp_replace(lower(btrim(p.parsed_partition)), '^part[[:space:]]+', '')
  WHERE COALESCE(p.bucket_partition, p.parsed_partition, '') <> ''
),
alias_count AS (
  SELECT COUNT(*)::int AS animals
  FROM alias_target a
  JOIN goats g
    ON g.tenant_id = $1::uuid
   AND g.shed_id = a.shed_id
   AND g.lifecycle_status = 'alive'
   AND g.exited_at IS NULL
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
  WHERE COALESCE(a.partition_label, '') <> ''
    AND regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
        = regexp_replace(lower(btrim(a.partition_label)), '^part[[:space:]]+', '')
)
SELECT CASE
  WHEN (SELECT animals FROM direct_count) > 0 THEN (SELECT animals FROM direct_count)
  ELSE COALESCE((SELECT animals FROM alias_count), 0)
END`

// lumpSumCensusCountTx counts the live residents of one bucket's (shed, pen) at
// the submit instant, inside the submit transaction.
//
// partitionLabel is the bucket's own weighing_campaign_sheds.partition_label:
// blank means the bucket covers the WHOLE shed, so every resident counts
// regardless of pen. A named pen matches residents under the same normalization
// the counts module's shifting-destination catalog uses (lowercase, trimmed,
// leading "part " stripped), so "Part 3", "part 3" and "3" resolve to the same
// pen and a resident with no pen row counts as 'whole'.
func (r *Repository) lumpSumCensusCountTx(ctx context.Context, tx pgx.Tx, tenantID, shedLocationID, partitionLabel string) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, lumpSumCensusCountSQL, tenantID, shedLocationID, partitionLabel).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
