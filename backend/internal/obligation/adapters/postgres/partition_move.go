package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

// SyncPartitionMoveForGoat handles a same-shed partition move: the goat's shed_id is unchanged
// but its goat_shed_partitions.partition_label moves (e.g. "Part 1" -> "Part 2"). Before this
// fix, nothing re-derived vaccination_drive_assignment_members after such a move: the animal kept
// counting against its OLD partition's assignment arm/operator forever, because
// syncVaccinationDriveAssignmentMembersTx (the only writer of that membership) was never re-run.
//
// This method performs the partition_label write and the membership re-derivation in ONE
// transaction, following the same "state transition + owned read model = one atomic txn" rule
// used by reScopeOpenForGoatInTx for cross-shed shifts. It is idempotent (a no-op when the label
// does not actually change) and set-based (no per-goat loop -- the batch-level
// syncVaccinationDriveAssignmentMembersTx recomputes an entire batch's membership from current
// goat_shed_partitions in one statement).
func (r *Repository) SyncPartitionMoveForGoat(ctx context.Context, tenantID, goatID, shedID, partitionLabel, sourceShedName string) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return 0, fmt.Errorf("obligation: goat id: %w", err)
	}
	shed, err := pgconv.UUID(shedID)
	if err != nil {
		return 0, fmt.Errorf("obligation: shed id: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	changed, err := syncPartitionMoveForGoatInTx(ctx, tx, tenant, goat, shed, partitionLabel, sourceShedName)
	if err != nil {
		return 0, err
	}
	if !changed {
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("obligation: commit no-op partition move: %w", err)
		}
		return 0, nil
	}

	batchIDs, err := openObligationBatchIDsForGoatTx(ctx, tx, tenant, goat)
	if err != nil {
		return 0, err
	}
	if len(batchIDs) > 0 {
		if err := syncVaccinationDriveAssignmentMembersTx(ctx, tx, tenant, batchIDs); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit partition move: %w", err)
	}
	return len(batchIDs), nil
}

// syncPartitionMoveForGoatInTx writes the new partition_label for a goat whose shed_id is
// unchanged and reports whether the label actually changed. It fails if the caller's shedID does
// not match the goat's current goat_shed_partitions row -- a cross-shed move must go through the
// existing shed re-scope path (ReScopeOpenForGoatShift), which unbatches obligations instead of
// re-deriving membership in place.
func syncPartitionMoveForGoatInTx(ctx context.Context, tx pgx.Tx, tenant, goat, shed pgtype.UUID, partitionLabel, sourceShedName string) (bool, error) {
	var oldLabel string
	var oldShed pgtype.UUID
	err := tx.QueryRow(ctx, `
UPDATE goat_shed_partitions AS gsp
SET partition_label = $3,
    source_shed_name = COALESCE(NULLIF($4, ''), gsp.source_shed_name),
    updated_at = now()
FROM (
  SELECT partition_label, shed_id
  FROM goat_shed_partitions
  WHERE tenant_id = $1::uuid AND goat_id = $2::uuid
  FOR UPDATE
) AS before
WHERE gsp.tenant_id = $1::uuid AND gsp.goat_id = $2::uuid
RETURNING before.partition_label, before.shed_id`,
		tenant, goat, partitionLabel, sourceShedName,
	).Scan(&oldLabel, &oldShed)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, fmt.Errorf("obligation: partition move: no goat_shed_partitions row for goat")
		}
		return false, fmt.Errorf("obligation: partition move: update goat_shed_partitions: %w", err)
	}
	if oldShed != shed {
		return false, fmt.Errorf("obligation: partition move: shed_id changed (%s -> %s); use the shed re-scope path, not a partition move", pgconv.UUIDString(oldShed), pgconv.UUIDString(shed))
	}
	return oldLabel != partitionLabel, nil
}

// openObligationBatchIDsForGoatTx returns the distinct batch IDs of a goat's still-open,
// batched vaccination obligations -- exactly the batches whose drive-assignment membership can be
// bound to this goat and therefore must be re-derived after its partition changes.
func openObligationBatchIDsForGoatTx(ctx context.Context, tx pgx.Tx, tenant, goat pgtype.UUID) ([]pgtype.UUID, error) {
	rows, err := tx.Query(ctx, `
SELECT DISTINCT batch_id
FROM obligation_instances
WHERE tenant_id = $1::uuid
  AND target_type = 'goat'
  AND target_id = $2::uuid
  AND status IN ('scheduled', 'due', 'deferred')
  AND batch_id IS NOT NULL`, tenant, goat)
	if err != nil {
		return nil, fmt.Errorf("obligation: partition move: list open batches: %w", err)
	}
	defer rows.Close()
	ids := make([]pgtype.UUID, 0)
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("obligation: partition move: scan batch id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: partition move: batch id rows: %w", err)
	}
	return ids, nil
}
