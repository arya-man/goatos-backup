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
alias_target AS (
  -- The bucket's location is resolved to a pen by EXACT id, never by parsing its
  -- name. shed_partitions.alias_location_id says which pen a legacy location row
  -- stands for (migration 000239); nothing here infers it from a name suffix,
  -- which is why a genuinely standalone shed whose name merely ends in a pen-like
  -- suffix ('Yashoda 2', 'Q1 2') is still refused rather than silently reporting
  -- some other shed's animals as weighed.
  -- The MAPPING is the source of truth, not the bucket's own partition_label. An
  -- alias location IS one pen, so a label carried alongside it is redundant at
  -- best and contradictory at worst -- letting it win would make a stale label on
  -- a 'Castro 1' bucket count 'Castro' pen 2.
  SELECT sp.shed_id, sp.normalized_label AS normalized_partition
  FROM shed_partitions sp
  WHERE sp.tenant_id = $1::uuid
    AND sp.alias_location_id = $2::uuid
    AND sp.status = 'active'
    AND (SELECT animals FROM direct_count) = 0
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
  WHERE a.normalized_partition <> ''
    AND regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
        = a.normalized_partition
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
