package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

// RecomputeFutureVaccinationDrives is a one-time admin operation that recomputes existing future
// vaccination drive batches + operator assignments to match the CURRENT operator-assignment config.
//
// This method:
// 1. Acquires the per-tenant advisory lock (same granularity as the sweeper)
// 2. Selects all planned-status future batches with planned_date >= effectiveFrom for the park
// 3. Releases those obligations back to unbatched (batch_id = NULL, conducted_by = NULL)
// 4. Deletes their vaccination_drive_assignments
// 5. Marks the batches as cancelled/superseded
// 6. Returns the count of released batches
//
// REQUIREMENTS (caller enforces):
// - Called OUTSIDE the live event cascade (no domain events fired)
// - Called when the sweeper is idle (no concurrent writers)
// - Caller re-sweeps after release to re-batch under current config
// - Does NOT modify clinical due dates or medical defer flags
// - Idempotent: second run with same config produces same state
//
// NOT re-planning inside this method (caller invokes the existing sweeper re-plan path separately).
func (r *Repository) RecomputeFutureVaccinationDrives(ctx context.Context, tenantID, parkID string, effectiveFrom time.Time) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	// Canonicalize tenant and park IDs to prevent advisory lock collisions (RV-03)
	tenantCanon, err := canonicalUUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: recompute tenant id: %w", err)
	}
	tenantUUID, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: recompute tenant uuid: %w", err)
	}
	parkUUID, err := pgconv.UUID(parkID)
	if err != nil {
		return 0, fmt.Errorf("obligation: recompute park uuid: %w", err)
	}

	// Acquire the per-tenant advisory lock (same as sweeper uses)
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: acquire recompute lock connection: %w", err)
	}
	defer conn.Release()

	// pg_advisory_lock blocks until acquired, returns void, so use SELECT to execute it
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", tenantSweepLockNamespace+tenantCanon); err != nil {
		return 0, fmt.Errorf("obligation: acquire tenant-sweep lock for recompute: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock(hashtext($1))", tenantSweepLockNamespace+tenantCanon)
	}()

	// Prepare effective date boundary (business day start)
	effectiveDateKey := biztime.BusinessDayStart(effectiveFrom).Format("2006-01-02")

	// Begin transaction to atomically release and update
	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: begin recompute tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Select all planned-status future batches for this park
	// that have planned_date >= effectiveFrom
	rows, err := tx.Query(ctx, `
SELECT batch_id::text
FROM obligation_batches
WHERE tenant_id = $1
  AND scope_type = 'park'
  AND scope_id = $2
  AND status = 'planned'
  AND planned_date >= $3::date
ORDER BY batch_id
`, tenantUUID, parkUUID, effectiveDateKey)
	if err != nil {
		return 0, fmt.Errorf("obligation: select future batches for recompute: %w", err)
	}

	var batchIDs []string
	for rows.Next() {
		var batchID string
		if err := rows.Scan(&batchID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("obligation: scan batch id: %w", err)
		}
		batchIDs = append(batchIDs, batchID)
	}
	rows.Close()

	if len(batchIDs) == 0 {
		// No batches to release; commit and return
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("obligation: commit empty recompute tx: %w", err)
		}
		return 0, nil
	}

	// Convert batch IDs to UUIDs
	batchUUIDs := make([]pgtype.UUID, len(batchIDs))
	for i, id := range batchIDs {
		uuid, err := pgconv.UUID(id)
		if err != nil {
			return 0, fmt.Errorf("obligation: parse batch id %s: %w", id, err)
		}
		batchUUIDs[i] = uuid
	}

	// 1. Release obligations back to unbatched (batch_id = NULL)
	_, err = tx.Exec(ctx, `
UPDATE obligation_instances
SET batch_id = NULL
WHERE tenant_id = $1 AND batch_id = ANY($2::uuid[])
`, tenantUUID, batchUUIDs)
	if err != nil {
		return 0, fmt.Errorf("obligation: release obligations from batches: %w", err)
	}

	// 2. Delete vaccination_drive_assignments for these batches
	delTag, err := tx.Exec(ctx, `
DELETE FROM vaccination_drive_assignments
WHERE tenant_id = $1 AND batch_id = ANY($2::uuid[])
`, tenantUUID, batchUUIDs)
	if err != nil {
		return 0, fmt.Errorf("obligation: delete drive assignments: %w", err)
	}
	_ = delTag.RowsAffected() // not critical, but logged for debugging

	// 3. Mark batches as cancelled/superseded (terminal status)
	markTag, err := tx.Exec(ctx, `
UPDATE obligation_batches
SET status = 'superseded', conducted_by = NULL
WHERE tenant_id = $1 AND batch_id = ANY($2::uuid[])
`, tenantUUID, batchUUIDs)
	if err != nil {
		return 0, fmt.Errorf("obligation: mark batches superseded: %w", err)
	}
	markedCount := markTag.RowsAffected()

	// Commit the transaction
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit recompute tx: %w", err)
	}

	return int(markedCount), nil
}

// ClaimOperatorConfigReplanWatermarkPending claims the watermark in PENDING state (before recompute).
// Returns true if claimed, false if already exists. A replay of the SAME event_id claims 0 rows,
// and the caller should skip re-invoking RecomputeFutureVaccinationDrives (already pending/succeeded).
// This is the first phase of the two-phase watermark: claim PENDING, run recompute, mark SUCCEEDED.
// If recompute fails, watermark stays PENDING for retriable redelivery.
func (r *Repository) ClaimOperatorConfigReplanWatermarkPending(ctx context.Context, tenantID, parkID, eventType, eventID string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := pgconv.UUID(tenantID)
	if err != nil {
		return false, fmt.Errorf("obligation: replan watermark tenant id: %w", err)
	}
	parkUUID, err := pgconv.UUID(parkID)
	if err != nil {
		return false, fmt.Errorf("obligation: replan watermark park id: %w", err)
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false, fmt.Errorf("obligation: replan watermark: empty event id")
	}

	tag, err := r.pool.Exec(ctx, `
INSERT INTO obligation_operator_config_replan_watermarks (tenant_id, event_id, park_id, event_type, status)
VALUES ($1::uuid, $2, $3::uuid, $4, 'pending')
ON CONFLICT (tenant_id, event_id) DO NOTHING
`, tenantUUID, eventID, parkUUID, eventType)
	if err != nil {
		return false, fmt.Errorf("obligation: claim replan watermark pending: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// MarkOperatorConfigReplanWatermarkSucceeded marks the watermark as SUCCEEDED (after successful recompute).
// Called only if recompute succeeds; on recompute failure, watermark stays PENDING for retriable redelivery.
// This is the second phase of the two-phase watermark: succeeds means exact replays become no-ops.
func (r *Repository) MarkOperatorConfigReplanWatermarkSucceeded(ctx context.Context, tenantID, eventID string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("obligation: mark watermark succeeded tenant id: %w", err)
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return fmt.Errorf("obligation: mark watermark succeeded: empty event id")
	}

	tag, err := r.pool.Exec(ctx, `
UPDATE obligation_operator_config_replan_watermarks
SET status = 'succeeded'
WHERE tenant_id = $1::uuid AND event_id = $2
`, tenantUUID, eventID)
	if err != nil {
		return fmt.Errorf("obligation: mark replan watermark succeeded: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("obligation: mark replan watermark succeeded: watermark not found for event %s", eventID)
	}
	return nil
}

// GetOperatorConfigReplanWatermarkStatus returns the status of a watermark (pending/succeeded) or empty string if not found.
// Used to determine whether to retry a failed recompute (pending) or skip (succeeded).
func (r *Repository) GetOperatorConfigReplanWatermarkStatus(ctx context.Context, tenantID, eventID string) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", fmt.Errorf("obligation: get watermark status tenant id: %w", err)
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return "", fmt.Errorf("obligation: get watermark status: empty event id")
	}

	var status string
	err = r.pool.QueryRow(ctx, `
SELECT status FROM obligation_operator_config_replan_watermarks
WHERE tenant_id = $1::uuid AND event_id = $2
`, tenantUUID, eventID).Scan(&status)
	if err == pgx.ErrNoRows {
		// Watermark doesn't exist - first time seeing this event
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("obligation: get replan watermark status: %w", err)
	}
	return status, nil
}

// ParkIDForShed resolves a shed location id to its parent park location id (locations.parent_location_id),
// used by the operator-config auto-cascade consumer to translate a shed-scoped
// vaccination.leave.changed event into the park RecomputeFutureVaccinationDrives needs.
func (r *Repository) ParkIDForShed(ctx context.Context, tenantID, shedID string) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", fmt.Errorf("obligation: park-for-shed tenant id: %w", err)
	}
	shedUUID, err := pgconv.UUID(shedID)
	if err != nil {
		return "", fmt.Errorf("obligation: park-for-shed shed id: %w", err)
	}

	var parkID pgtype.UUID
	err = r.pool.QueryRow(ctx, `
SELECT parent_location_id
FROM locations
WHERE tenant_id = $1::uuid AND location_id = $2::uuid AND location_type = 'shed'
`, tenantUUID, shedUUID).Scan(&parkID)
	if err != nil {
		return "", fmt.Errorf("obligation: park-for-shed lookup shed %s: %w", shedID, err)
	}
	if !parkID.Valid {
		return "", fmt.Errorf("obligation: shed %s has no parent park", shedID)
	}
	return pgconv.UUIDString(parkID), nil
}
