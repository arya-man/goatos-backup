package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

// batchAttachGuardSQL locks, in obligation_id order, exactly the rows the attach UPDATE may take
// ($1 tenant, $2 obligation ids) and returns what the vaccination write guard needs for each.
const batchAttachGuardSQL = `
SELECT oi.obligation_id::text, oi.rule_id, oi.target_type, oi.target_id, oi.due_at, oi.schedule_basis
FROM obligation_instances oi
WHERE oi.tenant_id = $1
  AND oi.obligation_id = ANY($2::uuid[])
  AND oi.batch_id IS NULL
  AND oi.status IN ('scheduled', 'due', 'in_progress', 'missed')
ORDER BY oi.obligation_id
FOR UPDATE OF oi`

// filterVaccinationBatchAttachTx drops, inside the caller's attach transaction, every obligation
// that the vaccination write guard refuses on the date the batch will execute it.
//
// Attaching an obligation to a planned batch is how the sweeper books it onto a drive day. That day
// is a vaccination date as real as a written due_at: a kid whose rule floor is DOB+28 must not be
// given its dose on an earlier drive, whatever its persisted due_at says (a row written before the
// guard existed, or an anchor such as DOB corrected after generation, can leave due_at early). The
// date proved is the batch's planned date when it has one, else the row's own due_at -- the date
// the drive would actually vaccinate on. A refused row is left exactly where it is: unbatched, due
// date untouched, no event. Every sweep re-proves it the same way, so repeated sweeps are
// idempotent and never pull it into the drive. Non-vaccination rows pass through untouched.
//
// Returns the ids still eligible to attach, and the refused ids.
func filterVaccinationBatchAttachTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, ids []pgtype.UUID, plannedDate *time.Time) ([]pgtype.UUID, []string, error) {
	if len(ids) == 0 {
		return ids, nil, nil
	}
	rows, err := tx.Query(ctx, batchAttachGuardSQL, tenant, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("obligation: lock batch attach rows: %w", err)
	}
	type candidate struct {
		id    string
		write vaccinationWrite
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		var dueAt time.Time
		if err := rows.Scan(&c.id, &c.write.Rule, &c.write.TargetType, &c.write.Target, &dueAt, &c.write.ScheduleBasis); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("obligation: scan batch attach row: %w", err)
		}
		c.write.Tenant = tenant
		c.write.DueAt = dueAt
		if plannedDate != nil {
			c.write.DueAt = batchPlannedDateStart(*plannedDate)
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("obligation: read batch attach rows: %w", err)
	}
	if len(candidates) == 0 {
		return ids, nil, nil
	}
	writes := make([]vaccinationWrite, len(candidates))
	for i, c := range candidates {
		writes[i] = c.write
	}
	decisions, err := validateVaccinationWrites(ctx, tx, writes)
	if err != nil {
		return nil, nil, fmt.Errorf("obligation: prove batch attach vaccination dates: %w", err)
	}
	refused := map[string]bool{}
	var refusedIDs []string
	for i, decision := range decisions {
		if decision != nil {
			refused[candidates[i].id] = true
			refusedIDs = append(refusedIDs, candidates[i].id)
		}
	}
	if len(refusedIDs) == 0 {
		return ids, nil, nil
	}
	kept := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		if !refused[pgconv.UUIDString(id)] {
			kept = append(kept, id)
		}
	}
	return kept, refusedIDs, nil
}

// batchPlannedDateStart is the business-day start of a batch planned_date, the same instant the
// drive-date sync writes as due_at (planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'). The
// calendar fields are taken as-is, which is how pgx encodes the value into the date column.
func batchPlannedDateStart(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, biztime.DefaultLocation())
}

// batchDriveAssignmentIDsSQL lists the drive assignment rows of one batch.
const batchDriveAssignmentIDsSQL = `
SELECT assignment_id
FROM vaccination_drive_assignments
WHERE tenant_id = $1 AND batch_id = $2`

// recomputeBatchDriveAssignmentCountersTx re-derives animal_count/total_doses of every drive
// assignment row of one batch from its exact membership ledger (dropping rows left empty).
func recomputeBatchDriveAssignmentCountersTx(ctx context.Context, tx pgx.Tx, tenant, batch pgtype.UUID) error {
	rows, err := tx.Query(ctx, batchDriveAssignmentIDsSQL, tenant, batch)
	if err != nil {
		return fmt.Errorf("obligation: read batch drive assignments: %w", err)
	}
	var ids []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("obligation: scan batch drive assignment: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("obligation: read batch drive assignments: %w", err)
	}
	return recomputeDriveAssignmentCountersExactForAssignmentsTx(ctx, tx, tenant, ids)
}
