package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// This file extends the same *Repository (see repository.go) with the HR
// roster surface approved in docs/hr/roster-rbac-design.md: the
// workforce_positions seat catalog and workforce_absences-reuse for leave
// coverage. It satisfies ports.RosterRepository the same way repository.go
// satisfies ports.Repository (which already covers the workforce_member_
// capabilities reads/writes the roster service needs via ports.CapabilityGranter),
// so a single workforcepg.NewRepository(pool, timeout) wires both services in
// bootstrap/api.go.

var _ ports.RosterRepository = (*Repository)(nil)
var _ ports.CapabilityGranter = (*Repository)(nil)

// Request-level idempotency scopes for the roster write paths (repo mandatory
// write-path contract, see idempotency.go). Each namespaces the shared
// idempotency_keys table so the same client key cannot collide across roster
// operations.
const (
	idemScopePositionCreate = "position.create"
	idemScopeLeaveApply     = "leave.apply"
	idemScopeLeaveApprove   = "leave.approve"
	idemScopeLeaveResolve   = "leave.resolve_coverage"
)

// ---- Positions --------------------------------------------------------

func positionSelectSQL(where string) string {
	return `
SELECT p.position_id::text, p.workforce_member_id::text, p.scope_type, p.scope_id::text, p.position_code,
       p.position_tier, p.is_backup_slot, p.backup_group_code, p.week_off_weekday, p.status,
       p.valid_from, p.valid_to, p.row_version, p.created_at, p.updated_at,
       COALESCE(wm.display_name, NULL) as person_display_name,
       COALESCE(wm.hr_designation_grade, NULL) as hr_designation_grade,
       COALESCE(l.name, NULL) as center_label
FROM workforce_positions p
LEFT JOIN workforce_members wm ON wm.workforce_member_id = p.workforce_member_id AND wm.tenant_id = p.tenant_id
LEFT JOIN locations l ON l.location_id = p.scope_id AND p.scope_type = 'center'
` + where
}

func scanPositions(rows pgx.Rows) ([]domain.Position, error) {
	defer rows.Close()
	items := []domain.Position{}
	for rows.Next() {
		var item domain.Position
		var backupGroup, weekOff, personDisplayName, hrDesignationGrade, centerLabel pgtype.Text
		var validFrom time.Time
		var validTo pgtype.Timestamptz
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&item.PositionID, &item.WorkforceMemberID, &item.ScopeType, &item.ScopeID, &item.PositionCode,
			&item.PositionTier, &item.IsBackupSlot, &backupGroup, &weekOff, &item.Status,
			&validFrom, &validTo, &item.RowVersion, &createdAt, &updatedAt,
			&personDisplayName, &hrDesignationGrade, &centerLabel); err != nil {
			return nil, err
		}
		item.BackupGroupCode = textPtr(backupGroup)
		item.WeekOffWeekday = textPtr(weekOff)
		item.PersonDisplayName = textPtr(personDisplayName)
		item.HrDesignationGrade = textPtr(hrDesignationGrade)
		item.CenterLabel = textPtr(centerLabel)
		item.ValidFrom = validFrom.UTC().Format(time.RFC3339)
		item.ValidTo = timePtr(validTo)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) queryOnePosition(ctx context.Context, where string, args ...any) (domain.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, positionSelectSQL(where), args...)
	if err != nil {
		return domain.Position{}, err
	}
	items, err := scanPositions(rows)
	if err != nil {
		return domain.Position{}, err
	}
	if len(items) == 0 {
		return domain.Position{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) CreatePosition(ctx context.Context, cmd ports.CreatePositionCommand) (domain.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Position{}, err
	}
	defer rollback(ctx, tx)

	// Request-level idempotency: reserve the client key + a semantic fingerprint
	// in the SAME tx as the insert. Exact replay returns the original position
	// without re-running the replace/insert; same-key/different-payload rejects.
	idemKey := ptrValue(cmd.Body.IdempotencyKey)
	fingerprint := requestFingerprint(
		cmd.Body.WorkforceMemberID, cmd.Body.ScopeType, cmd.Body.ScopeID, cmd.Body.PositionCode,
		cmd.Body.PositionTier, fmt.Sprintf("%t", cmd.Body.IsBackupSlot),
		ptrValue(cmd.Body.BackupGroupCode), ptrValue(cmd.Body.WeekOffWeekday), ptrValue(cmd.Body.ValidTo),
	)
	reservation, err := reserveIdempotency(ctx, tx, cmd.TenantID, idemScopePositionCreate, idemKey, fingerprint)
	if err != nil {
		return domain.Position{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Position{}, err
		}
		return r.queryOnePosition(contextWithoutCancel(ctx), `
WHERE p.tenant_id = $1::uuid AND p.position_id = $2::uuid
LIMIT 1`, cmd.TenantID, reservation.resultID)
	}

	// Replace semantics: end the current active holder of this named seat (if
	// any) before inserting the new one, so the partial unique index (at most
	// one active holder per seat) is always satisfiable on reassignment.
	if _, err := tx.Exec(ctx, `
UPDATE workforce_positions
SET status = 'ended', valid_to = COALESCE(valid_to, now()), updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND scope_type = $2 AND scope_id = $3::uuid AND position_code = $4 AND status = 'active'`,
		cmd.TenantID, cmd.Body.ScopeType, cmd.Body.ScopeID, cmd.Body.PositionCode); err != nil {
		return domain.Position{}, err
	}

	var positionID string
	err = tx.QueryRow(ctx, `
INSERT INTO workforce_positions (
  tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier,
  is_backup_slot, backup_group_code, week_off_weekday, valid_to, created_by
) VALUES (
  $1::uuid, $2::uuid, $3, $4::uuid, $5, $6,
  $7, nullif($8, ''), nullif($9, ''), nullif($10, '')::timestamptz, $11::uuid
)
RETURNING position_id::text`,
		cmd.TenantID, cmd.Body.WorkforceMemberID, cmd.Body.ScopeType, cmd.Body.ScopeID, cmd.Body.PositionCode, cmd.Body.PositionTier,
		cmd.Body.IsBackupSlot, ptrValue(cmd.Body.BackupGroupCode), ptrValue(cmd.Body.WeekOffWeekday), ptrValue(cmd.Body.ValidTo), cmd.ActorID).
		Scan(&positionID)
	if err != nil {
		return domain.Position{}, mapWriteErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "roster.position.assign", "workforce_position", positionID, &cmd.Body.ScopeType, map[string]any{
		"position_code": cmd.Body.PositionCode, "workforce_member_id": cmd.Body.WorkforceMemberID, "scope_id": cmd.Body.ScopeID,
	}); err != nil {
		return domain.Position{}, err
	}
	if err := completeIdempotency(ctx, tx, cmd.TenantID, idemScopePositionCreate, idemKey, "workforce_position", positionID); err != nil {
		return domain.Position{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Position{}, err
	}
	return r.queryOnePosition(contextWithoutCancel(ctx), `
WHERE p.tenant_id = $1::uuid AND p.position_id = $2::uuid
LIMIT 1`, cmd.TenantID, positionID)
}

func (r *Repository) ListPositions(ctx context.Context, params ports.ListPositionsParams) ([]domain.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, positionSelectSQL(`
WHERE p.tenant_id = $1::uuid
  AND ($2 = '' OR p.workforce_member_id = $2::uuid)
  AND ($3 = '' OR p.scope_type = $3)
  AND ($4 = '' OR p.scope_id = $4::uuid)
  AND ($5 = '' OR p.position_code = $5)
  AND ($6 = '' OR p.status = $6)
ORDER BY p.status, p.scope_type, p.scope_id, p.position_code
LIMIT $7`), params.TenantID, params.WorkforceMemberID, params.ScopeType, params.ScopeID, params.PositionCode, params.Status, params.Limit)
	if err != nil {
		return nil, err
	}
	return scanPositions(rows)
}

func (r *Repository) GetActivePositionByCode(ctx context.Context, tenantID, scopeType, scopeID, positionCode string, at time.Time) (domain.Position, error) {
	return r.queryOnePosition(ctx, `
WHERE p.tenant_id = $1::uuid AND p.scope_type = $2 AND p.scope_id = $3::uuid AND p.position_code = $4
  AND p.status = 'active' AND p.valid_from <= $5::timestamptz AND (p.valid_to IS NULL OR p.valid_to > $5::timestamptz)
LIMIT 1`, tenantID, scopeType, scopeID, positionCode, at)
}

func (r *Repository) GetActiveBackupSlot(ctx context.Context, tenantID, scopeType, scopeID, backupGroupCode string, at time.Time) (domain.Position, error) {
	return r.queryOnePosition(ctx, `
WHERE p.tenant_id = $1::uuid AND p.scope_type = $2 AND p.scope_id = $3::uuid AND p.backup_group_code = $4 AND p.is_backup_slot = true
  AND p.status = 'active' AND p.valid_from <= $5::timestamptz AND (p.valid_to IS NULL OR p.valid_to > $5::timestamptz)
LIMIT 1`, tenantID, scopeType, scopeID, backupGroupCode, at)
}

func (r *Repository) GetActivePositionForMember(ctx context.Context, tenantID, workforceMemberID string) (domain.Position, error) {
	return r.queryOnePosition(ctx, `
WHERE p.tenant_id = $1::uuid AND p.workforce_member_id = $2::uuid AND p.status = 'active'
ORDER BY p.valid_from DESC
LIMIT 1`, tenantID, workforceMemberID)
}

// ---- Leave / absence (workforce_absences reuse) -----------------------------

func leaveSelectSQL(where string) string {
	return `
SELECT absence_id::text, workforce_member_id::text, scope_type, scope_id::text, reason_code, status,
       starts_at, ends_at, replacement_member_id::text, coverage_override_reason,
       created_by::text, approved_by::text, row_version, created_at, updated_at
FROM workforce_absences
` + where
}

func scanLeaves(rows pgx.Rows) ([]domain.StaffLeave, error) {
	defer rows.Close()
	items := []domain.StaffLeave{}
	for rows.Next() {
		var item domain.StaffLeave
		var replacementMember, overrideReason, createdBy, approvedBy pgtype.Text
		var startsAt, endsAt, createdAt, updatedAt time.Time
		if err := rows.Scan(&item.AbsenceID, &item.WorkforceMemberID, &item.ScopeType, &item.ScopeID, &item.ReasonCode, &item.Status,
			&startsAt, &endsAt, &replacementMember, &overrideReason, &createdBy, &approvedBy, &item.RowVersion, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.ReplacementMemberID = textPtr(replacementMember)
		item.CoverageOverrideReason = textPtr(overrideReason)
		item.CreatedBy = textPtr(createdBy)
		item.ApprovedBy = textPtr(approvedBy)
		item.StartsAt = startsAt.UTC().Format(time.RFC3339)
		item.EndsAt = endsAt.UTC().Format(time.RFC3339)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ApplyLeave(ctx context.Context, cmd ports.ApplyLeaveCommand) (domain.StaffLeave, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.StaffLeave{}, err
	}
	defer rollback(ctx, tx)

	// Request-level idempotency in the same tx as the insert. Exact replay
	// returns the original absence without inserting a second reported leave.
	idemKey := ptrValue(cmd.Body.IdempotencyKey)
	fingerprint := requestFingerprint(
		cmd.Body.WorkforceMemberID, cmd.Body.ScopeType, cmd.Body.ScopeID, cmd.Body.ReasonCode,
		cmd.StartsAt.UTC().Format(time.RFC3339Nano), cmd.EndsAt.UTC().Format(time.RFC3339Nano),
	)
	reservation, err := reserveIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveApply, idemKey, fingerprint)
	if err != nil {
		return domain.StaffLeave{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.StaffLeave{}, err
		}
		return r.GetLeave(contextWithoutCancel(ctx), cmd.TenantID, reservation.resultID)
	}

	var absenceID string
	err = tx.QueryRow(ctx, `
INSERT INTO workforce_absences (
  tenant_id, workforce_member_id, scope_type, scope_id, starts_at, ends_at, reason_code, status, created_by
) VALUES (
  $1::uuid, $2::uuid, $3, $4::uuid, $5::timestamptz, $6::timestamptz, $7, 'reported', $8::uuid
)
RETURNING absence_id::text`,
		cmd.TenantID, cmd.Body.WorkforceMemberID, cmd.Body.ScopeType, cmd.Body.ScopeID, cmd.StartsAt, cmd.EndsAt, cmd.Body.ReasonCode, cmd.ActorID).Scan(&absenceID)
	if err != nil {
		return domain.StaffLeave{}, mapWriteErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "roster.leave.apply", "workforce_absence", absenceID, &cmd.Body.ScopeType, map[string]any{
		"workforce_member_id": cmd.Body.WorkforceMemberID, "scope_id": cmd.Body.ScopeID,
	}); err != nil {
		return domain.StaffLeave{}, err
	}
	if err := completeIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveApply, idemKey, "workforce_absence", absenceID); err != nil {
		return domain.StaffLeave{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.StaffLeave{}, err
	}
	return r.GetLeave(contextWithoutCancel(ctx), cmd.TenantID, absenceID)
}

func (r *Repository) ApproveLeave(ctx context.Context, cmd ports.ApproveLeaveCommand) (domain.StaffLeave, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.StaffLeave{}, false, err
	}
	defer rollback(ctx, tx)

	// Request-level idempotency in the same tx as the status transition. Exact
	// replay returns the original (already-approved-and-resolved) leave without
	// re-running the transition -- the service uses the replay flag to also skip
	// re-running coverage resolution / the capability grant (downstream dup
	// prevention).
	idemKey := cmd.IdempotencyKey
	fingerprint := requestFingerprint(cmd.AbsenceID, fmt.Sprintf("%d", cmd.RowVersion))
	reservation, err := reserveIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveApprove, idemKey, fingerprint)
	if err != nil {
		return domain.StaffLeave{}, false, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.StaffLeave{}, false, err
		}
		leave, err := r.GetLeave(contextWithoutCancel(ctx), cmd.TenantID, cmd.AbsenceID)
		return leave, true, err
	}

	tag, err := tx.Exec(ctx, `
UPDATE workforce_absences
SET status = 'approved', approved_by = $4::uuid, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND absence_id = $2::uuid AND row_version = $3 AND status = 'reported'`,
		cmd.TenantID, cmd.AbsenceID, cmd.RowVersion, cmd.ActorID)
	if err != nil {
		return domain.StaffLeave{}, false, mapWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.StaffLeave{}, false, ports.ErrConflict
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "roster.leave.approve", "workforce_absence", cmd.AbsenceID, nil, map[string]any{"row_version": cmd.RowVersion}); err != nil {
		return domain.StaffLeave{}, false, err
	}
	if err := completeIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveApprove, idemKey, "workforce_absence", cmd.AbsenceID); err != nil {
		return domain.StaffLeave{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.StaffLeave{}, false, err
	}
	leave, err := r.GetLeave(contextWithoutCancel(ctx), cmd.TenantID, cmd.AbsenceID)
	return leave, false, err
}

func (r *Repository) ResolveLeaveCoverage(ctx context.Context, cmd ports.ResolveLeaveCoverageCommand) (domain.StaffLeave, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.StaffLeave{}, false, err
	}
	defer rollback(ctx, tx)

	// Request-level idempotency (explicit resolve-coverage endpoint only; the
	// approve-driven auto path passes an empty key and is guarded by the approve
	// key instead). Reserved in the same tx as the coverage UPDATE; exact replay
	// returns the original resolved leave without re-writing, and the service
	// uses the replay flag to skip re-granting the temporary capability.
	idemKey := cmd.IdempotencyKey
	fingerprint := requestFingerprint(cmd.AbsenceID, cmd.Status, ptrValue(cmd.ReplacementMemberID), ptrValue(cmd.OverrideReason))
	reservation, err := reserveIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveResolve, idemKey, fingerprint)
	if err != nil {
		return domain.StaffLeave{}, false, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.StaffLeave{}, false, err
		}
		leave, err := r.GetLeave(contextWithoutCancel(ctx), cmd.TenantID, cmd.AbsenceID)
		return leave, true, err
	}

	tag, err := tx.Exec(ctx, `
UPDATE workforce_absences
SET status = $3,
    replacement_member_id = nullif($4, '')::uuid,
    coverage_override_reason = COALESCE(nullif($5, ''), coverage_override_reason),
    updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND absence_id = $2::uuid AND status IN ('approved', 'escalation_required')`,
		cmd.TenantID, cmd.AbsenceID, cmd.Status, ptrValue(cmd.ReplacementMemberID), ptrValue(cmd.OverrideReason))
	if err != nil {
		return domain.StaffLeave{}, false, mapWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.StaffLeave{}, false, ports.ErrConflict
	}
	action := "roster.leave.coverage_resolved"
	if cmd.Status == domain.LeaveStatusEscalationRequired {
		action = "roster.leave.coverage_escalated"
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, action, "workforce_absence", cmd.AbsenceID, nil, map[string]any{
		"status": cmd.Status, "replacement_member_id": ptrValue(cmd.ReplacementMemberID),
	}); err != nil {
		return domain.StaffLeave{}, false, err
	}
	if err := completeIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveResolve, idemKey, "workforce_absence", cmd.AbsenceID); err != nil {
		return domain.StaffLeave{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.StaffLeave{}, false, err
	}
	leave, err := r.GetLeave(contextWithoutCancel(ctx), cmd.TenantID, cmd.AbsenceID)
	return leave, false, err
}

func (r *Repository) GetLeave(ctx context.Context, tenantID, absenceID string) (domain.StaffLeave, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, leaveSelectSQL(`
WHERE tenant_id = $1::uuid AND absence_id = $2::uuid
LIMIT 1`), tenantID, absenceID)
	if err != nil {
		return domain.StaffLeave{}, err
	}
	items, err := scanLeaves(rows)
	if err != nil {
		return domain.StaffLeave{}, err
	}
	if len(items) == 0 {
		return domain.StaffLeave{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) ListLeave(ctx context.Context, params ports.ListLeaveParams) ([]domain.StaffLeave, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, leaveSelectSQL(`
WHERE tenant_id = $1::uuid
  AND ($2 = '' OR workforce_member_id = $2::uuid)
  AND ($3 = '' OR scope_type = $3)
  AND ($4 = '' OR scope_id = $4::uuid)
  AND ($5 = '' OR status = $5)
ORDER BY starts_at DESC, absence_id DESC
LIMIT $6`), params.TenantID, params.WorkforceMemberID, params.ScopeType, params.ScopeID, params.Status, params.Limit)
	if err != nil {
		return nil, err
	}
	return scanLeaves(rows)
}

func (r *Repository) IsMemberOnApprovedLeave(ctx context.Context, tenantID, workforceMemberID string, at time.Time) (bool, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var absenceID string
	err := r.pool.QueryRow(ctx, `
SELECT absence_id::text
FROM workforce_absences
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND status IN ('approved', 'escalation_required')
  AND starts_at <= $3::timestamptz AND ends_at > $3::timestamptz
ORDER BY absence_id
LIMIT 1`, tenantID, workforceMemberID, at).Scan(&absenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, absenceID, nil
}

// ---- Backup config + coverage lists -----------------------------------------

func (r *Repository) ListBackupConfig(ctx context.Context, params ports.ListBackupConfigParams) ([]domain.BackupConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT
  p.scope_id::text as center_id,
  l.name as center_label,
  p.backup_group_code,
  p.position_code,
  p.position_code as position_title,
  COALESCE(wm.workforce_member_id::text, NULL) as configured_holder_id,
  COALESCE(wm.display_name, NULL) as configured_holder_name,
  p.status
FROM workforce_positions p
LEFT JOIN workforce_members wm ON p.workforce_member_id = wm.workforce_member_id
LEFT JOIN locations l ON p.scope_id = l.location_id AND p.scope_type = 'center'
WHERE p.tenant_id = $1::uuid
  AND p.is_backup_slot = true
  AND ($2 = '' OR p.scope_type = $2)
  AND ($3 = '' OR p.scope_id = $3::uuid)
ORDER BY p.scope_id, p.backup_group_code, p.position_code
LIMIT $4`, params.TenantID, params.ScopeType, params.ScopeID, params.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.BackupConfig
	for rows.Next() {
		var item domain.BackupConfig
		err := rows.Scan(&item.CenterID, &item.CenterLabel, &item.BackupGroupCode,
			&item.BackupPositionCode, &item.BackupPositionTitle, &item.ConfiguredHolderID,
			&item.ConfiguredHolderName, &item.Status)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListCoverage(ctx context.Context, params ports.ListCoverageParams) ([]domain.Coverage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	statusFilter := ""
	if params.Active {
		statusFilter = " AND wa.status = 'approved'"
	}

	rows, err := r.pool.Query(ctx, `
SELECT
  p.position_id::text,
  p.position_code,
  p.position_code as position_title,
  COALESCE(wm.workforce_member_id::text, NULL) as covering_member_id,
  COALESCE(wm.display_name, NULL) as covering_member_name,
  wa.starts_at::text,
  wa.ends_at::text,
  'leave' as source,
  NULL as escalation_state,
  wa.status
FROM workforce_absences wa
JOIN workforce_positions p ON wa.workforce_member_id = p.workforce_member_id
  AND wa.scope_type = p.scope_type AND wa.scope_id = p.scope_id
LEFT JOIN workforce_members wm ON wa.replacement_member_id = wm.workforce_member_id
WHERE wa.tenant_id = $1::uuid
  AND ($2 = '' OR p.scope_type = $2)
  AND ($3 = '' OR p.scope_id = $3::uuid)` + statusFilter + `
ORDER BY wa.starts_at DESC, wa.absence_id DESC
LIMIT $4`, params.TenantID, params.ScopeType, params.ScopeID, params.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.Coverage
	for rows.Next() {
		var item domain.Coverage
		err := rows.Scan(&item.PositionID, &item.CoveredPositionCode, &item.CoveredPositionTitle,
			&item.CoveringMemberID, &item.CoveringMemberName, &item.StartDate, &item.EndDate,
			&item.Source, &item.EscalationState, &item.Status)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ---- Operator timetable + my-coverage  -----------------------------------------------

func (r *Repository) GetCenterTimetable(ctx context.Context, tenantID, centerID string, limit int) ([]domain.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, positionSelectSQL(`
WHERE p.tenant_id = $1::uuid
  AND p.scope_type = 'center'
  AND p.scope_id = $2::uuid
  AND p.status = 'active'
  AND (p.valid_from <= NOW() AT TIME ZONE 'Asia/Kolkata'::text)::date
  AND (p.valid_to IS NULL OR (p.valid_to > NOW() AT TIME ZONE 'Asia/Kolkata'::text)::date)
ORDER BY p.position_tier DESC, p.position_code
LIMIT $3`), tenantID, centerID, limit)
	if err != nil {
		return nil, err
	}
	return scanPositions(rows)
}

func (r *Repository) GetOperatorCoverage(ctx context.Context, tenantID, workforceMemberID string, at time.Time) (*domain.Coverage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, `
SELECT
  p.position_id::text,
  p.position_code,
  p.position_code as position_title,
  COALESCE(wm.workforce_member_id::text, NULL) as covering_member_id,
  COALESCE(wm.display_name, NULL) as covering_member_name,
  wa.starts_at::text,
  wa.ends_at::text,
  'leave' as source,
  NULL as escalation_state,
  wa.status
FROM workforce_absences wa
JOIN workforce_positions p ON wa.workforce_member_id = p.workforce_member_id
  AND wa.scope_type = p.scope_type AND wa.scope_id = p.scope_id
LEFT JOIN workforce_members wm ON wa.replacement_member_id = wm.workforce_member_id
WHERE wa.tenant_id = $1::uuid
  AND wa.workforce_member_id = $2::uuid
  AND wa.status IN ('approved', 'escalation_required')
  AND wa.starts_at <= $3::timestamptz AND wa.ends_at > $3::timestamptz
ORDER BY wa.starts_at DESC, wa.absence_id DESC
LIMIT 1`, tenantID, workforceMemberID, at)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items, err := scanCoverage(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return &items[0], nil
}

// ---- Coverage scanning helper ------------------------------------------------

func scanCoverage(rows pgx.Rows) ([]domain.Coverage, error) {
	var items []domain.Coverage
	for rows.Next() {
		var item domain.Coverage
		err := rows.Scan(&item.PositionID, &item.CoveredPositionCode, &item.CoveredPositionTitle,
			&item.CoveringMemberID, &item.CoveringMemberName, &item.StartDate, &item.EndDate,
			&item.Source, &item.EscalationState, &item.Status)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
