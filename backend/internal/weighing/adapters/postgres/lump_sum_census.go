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
SELECT COUNT(*)
FROM goats g
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id
 AND gsp.goat_id = g.goat_id
WHERE g.tenant_id = $1::uuid
  AND g.shed_id = $2::uuid
  AND g.lifecycle_status = 'alive'
  AND g.exited_at IS NULL
  AND (
    $3::text = ''
    OR regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
       = regexp_replace(lower(btrim($3::text)), '^part[[:space:]]+', '')
  )`

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
