package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// Leave requests (docs/features/leave-requests/plan.md, maintainer decisions
// 2026-09-10). This file extends the same *Repository with the
// workforce_leave_requests write path: every write is ONE transaction --
// idempotency reservation, the row change, the workforce_absences mirror on
// final approval, the audit row and the outbox event -- so a committed
// request always announces itself and a rolled-back one never does.

var _ ports.LeaveRepository = (*Repository)(nil)

const (
	idemScopeLeaveRequestCreate   = "leave_request.create"
	idemScopeLeaveRequestWithdraw = "leave_request.withdraw"
	idemScopeLeaveRequestDecide   = "leave_request.decide"

	// Domain events, registered in context/architecture/domain-event-registry.json
	// and consumed by notificationbridge.LeaveRequestNotifyConsumer.
	EventLeaveRequested = "workforce.leave.requested"
	EventLeaveApproved  = "workforce.leave.approved"
	EventLeaveRejected  = "workforce.leave.rejected"
	EventLeaveWithdrawn = "workforce.leave.withdrawn"

	leaveEventTopic     = "workforce.events"
	leaveAggregateType  = "leave_request"
	leaveEventSchemaRef = "contracts/jsonschema/domain-event-envelope.schema.json"
)

// LeaveEventPayload is the inner payload every leave event carries: enough for
// the notifier to compose a specific message without a second read.
type LeaveEventPayload struct {
	LeaveRequestID    string   `json:"leave_request_id"`
	WorkforceMemberID string   `json:"workforce_member_id"`
	PersonName        string   `json:"person_name"`
	ParkID            string   `json:"park_id,omitempty"`
	ParkLabel         string   `json:"park_label,omitempty"`
	StartsOn          string   `json:"starts_on"`
	EndsOn            string   `json:"ends_on"`
	DayCount          int      `json:"day_count"`
	Reason            string   `json:"reason"`
	Status            string   `json:"status"`
	DecidedSlot       string   `json:"decided_slot,omitempty"`
	DecidedByUserID   string   `json:"decided_by_user_id,omitempty"`
	DecidedByName     string   `json:"decided_by_name,omitempty"`
	DecisionNote      string   `json:"decision_note,omitempty"`
	ParkHeadRequired  bool     `json:"park_head_required"`
	HRRequired        bool     `json:"hr_required"`
	ParkHeadMemberIDs []string `json:"park_head_member_ids"`
	HRMemberIDs       []string `json:"hr_member_ids"`
	OccurredAt        string   `json:"occurred_at"`
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	// One row per request; every join is 1:1 (requester profile, park, and the
	// two decider profiles resolved by user id with LIMIT 1 laterals so a user
	// carrying two profiles cannot fan a request into two rows).
	leaveRequestSelectSQL = `
SELECT r.leave_request_id::text, r.workforce_member_id::text,
       COALESCE(m.display_name, ''), COALESCE(m.primary_role_hint, ''), COALESCE(m.hr_designation_grade, ''), COALESCE(dc.label, ''),
       COALESCE(r.park_id::text, ''), COALESCE(l.name, ''),
       r.starts_on::text, r.ends_on::text, r.reason, r.status,
       r.park_head_required, r.hr_required,
       COALESCE(r.park_head_decision, ''), COALESCE(r.park_head_decided_by::text, ''), COALESCE(ph.display_name, ''),
       r.park_head_decided_at, COALESCE(r.park_head_note, ''),
       COALESCE(r.hr_decision, ''), COALESCE(r.hr_decided_by::text, ''), COALESCE(hr.display_name, ''),
       r.hr_decided_at, COALESCE(r.hr_note, ''),
       r.decided_at, COALESCE(r.absence_id::text, ''), r.raised_by_user_id::text, r.raised_at, r.row_version
FROM workforce_leave_requests r
JOIN workforce_members m
  ON m.tenant_id = r.tenant_id AND m.workforce_member_id = r.workforce_member_id
LEFT JOIN person_access pa
  ON pa.tenant_id = m.tenant_id AND pa.workforce_member_id = m.workforce_member_id
-- The person's designation in farm words ("Feed Manager"): 1:1 on the catalog PK. Only an
-- ACTIVE catalog row names anybody, so a retired designation (operator, 000394) never does.
LEFT JOIN designation_catalog dc
  ON dc.designation_code = pa.designation_code AND dc.status = 'active'
LEFT JOIN locations l
  ON l.tenant_id = r.tenant_id AND l.location_id = r.park_id
LEFT JOIN LATERAL (
  SELECT display_name FROM workforce_members x
  WHERE x.tenant_id = r.tenant_id AND x.user_id = r.park_head_decided_by
  ORDER BY (x.status = 'active') DESC, x.updated_at DESC LIMIT 1
) ph ON r.park_head_decided_by IS NOT NULL
LEFT JOIN LATERAL (
  SELECT display_name FROM workforce_members x
  WHERE x.tenant_id = r.tenant_id AND x.user_id = r.hr_decided_by
  ORDER BY (x.status = 'active') DESC, x.updated_at DESC LIMIT 1
) hr ON r.hr_decided_by IS NOT NULL
`

	sqlLeaveInsert = `
INSERT INTO workforce_leave_requests (
  tenant_id, workforce_member_id, park_id, starts_on, ends_on, reason,
  park_head_required, hr_required, raised_by_user_id, idempotency_key, request_fingerprint
) VALUES (
  $1::uuid, $2::uuid, nullif($3, '')::uuid, $4::date, $5::date, $6,
  $7, $8, $9::uuid, $10, $11
)
RETURNING leave_request_id::text`

	// Overlap guard: any pending/approved window of the same person touching
	// the new one. Serialized per person by the FOR UPDATE on the member row.
	sqlLeaveOverlap = `
SELECT 1
FROM workforce_leave_requests
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid
  AND status IN ('pending', 'approved')
  AND starts_on <= $4::date AND ends_on >= $3::date
LIMIT 1`

	sqlLeaveLockMember = `
SELECT COALESCE(display_name, ''), COALESCE(primary_location_id::text, '')
FROM workforce_members
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid
FOR UPDATE`

	sqlLeaveLock = `
SELECT status, workforce_member_id::text, park_head_required, hr_required,
       COALESCE(park_head_decision, ''), COALESCE(hr_decision, ''), COALESCE(park_id::text, ''),
       starts_on::text, ends_on::text, raised_by_user_id::text
FROM workforce_leave_requests
WHERE tenant_id = $1::uuid AND leave_request_id = $2::uuid
FOR UPDATE`

	sqlLeaveWithdraw = `
UPDATE workforce_leave_requests
SET status = 'withdrawn', decided_at = now(), updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND leave_request_id = $2::uuid AND status = 'pending'`

	sqlLeaveDecideParkHead = `
UPDATE workforce_leave_requests
SET park_head_decision = $3, park_head_decided_by = $4::uuid, park_head_decided_at = now(),
    park_head_note = nullif($5, ''),
    status = $6, decided_at = CASE WHEN $6 = 'pending' THEN NULL ELSE now() END,
    absence_id = nullif($7, '')::uuid,
    updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND leave_request_id = $2::uuid AND status = 'pending' AND park_head_decision IS NULL`

	sqlLeaveDecideHR = `
UPDATE workforce_leave_requests
SET hr_decision = $3, hr_decided_by = $4::uuid, hr_decided_at = now(),
    hr_note = nullif($5, ''),
    status = $6, decided_at = CASE WHEN $6 = 'pending' THEN NULL ELSE now() END,
    absence_id = nullif($7, '')::uuid,
    updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND leave_request_id = $2::uuid AND status = 'pending' AND hr_decision IS NULL`

	// The workforce_absences mirror on final approval: the canonical "this
	// person is absent" fact every existing approved-leave read consults.
	// ends_at is EXCLUSIVE on that table, so the inclusive ends_on becomes
	// the following midnight IST. No replacement is resolved here (roster
	// rewrite is the next step).
	sqlLeaveMirrorAbsence = `
INSERT INTO workforce_absences (
  tenant_id, workforce_member_id, scope_type, scope_id, starts_at, ends_at, reason_code, status, created_by, approved_by
) VALUES (
  $1::uuid, $2::uuid, $3, $4::uuid,
  ($5::date::timestamp AT TIME ZONE 'Asia/Kolkata'),
  (($6::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata'),
  'planned_leave', 'approved', $7::uuid, $8::uuid
)
RETURNING absence_id::text`

	sqlLeaveForMember = `
WHERE r.tenant_id = $1::uuid AND r.workforce_member_id = $2::uuid
ORDER BY r.starts_on DESC, r.leave_request_id DESC
LIMIT $3`

	sqlLeaveQueue = `
WHERE r.tenant_id = $1::uuid AND r.status = 'pending'
  AND (
    $2::boolean
    OR ($3::boolean AND r.hr_required AND r.hr_decision IS NULL)
    OR (r.park_head_required AND r.park_head_decision IS NULL AND r.park_id = ANY($4::uuid[]))
  )
  AND ($5::timestamptz IS NULL OR (r.raised_at, r.leave_request_id) < ($5::timestamptz, $6::uuid))
ORDER BY r.raised_at DESC, r.leave_request_id DESC
LIMIT $7`

	sqlLeaveAdminList = `
WHERE r.tenant_id = $1::uuid
  AND ($2 = '' OR r.status = $2)
  AND ($3 = '' OR r.park_id = $3::uuid)
  AND ($4::timestamptz IS NULL OR (r.raised_at, r.leave_request_id) < ($4::timestamptz, $5::uuid))
ORDER BY r.raised_at DESC, r.leave_request_id DESC
LIMIT $6`

	sqlLeaveToday = `
WHERE r.tenant_id = $1::uuid AND r.workforce_member_id = $2::uuid
  AND r.status = 'approved' AND r.starts_on <= $3::date AND r.ends_on >= $3::date
ORDER BY r.starts_on DESC
LIMIT 1`

	sqlLeaveByID = `
WHERE r.tenant_id = $1::uuid AND r.leave_request_id = $2::uuid
LIMIT 1`

	sqlLeaveConfigGet = `
SELECT c.park_head_required, c.hr_required, COALESCE(c.updated_by::text, ''), COALESCE(u.display_name, ''),
       c.updated_at, c.row_version
FROM workforce_leave_approval_config c
LEFT JOIN LATERAL (
  SELECT display_name FROM workforce_members x
  WHERE x.tenant_id = c.tenant_id AND x.user_id = c.updated_by
  ORDER BY (x.status = 'active') DESC, x.updated_at DESC LIMIT 1
) u ON c.updated_by IS NOT NULL
WHERE c.tenant_id = $1::uuid`

	// Optimistic upsert: the first write expects row_version 0 (no row), a
	// later one the version the screen loaded with.
	sqlLeaveConfigSet = `
INSERT INTO workforce_leave_approval_config (tenant_id, park_head_required, hr_required, updated_by, updated_at, row_version)
VALUES ($1::uuid, $2, $3, $4::uuid, now(), 1)
ON CONFLICT (tenant_id) DO UPDATE
SET park_head_required = EXCLUDED.park_head_required,
    hr_required = EXCLUDED.hr_required,
    updated_by = EXCLUDED.updated_by,
    updated_at = now(),
    row_version = workforce_leave_approval_config.row_version + 1
WHERE workforce_leave_approval_config.row_version = $5
RETURNING row_version`

	// Approver members: ACTIVE role grants -> active profiles. A park desk is a
	// park_head grant either scoped to the park or -- the shape the live roster
	// actually carries -- tenant-scoped and seated by the holder's own
	// primary_location_id; HR is a tenant-scoped per-person grant.
	sqlLeaveApprovers = `
SELECT DISTINCT m.workforce_member_id::text, g.role
FROM user_scope_grants g
JOIN workforce_members m
  ON m.tenant_id = g.tenant_id AND m.user_id = g.user_id AND m.status = 'active'
WHERE g.tenant_id = $1::uuid
  AND g.status = 'active'
  AND g.valid_from <= now() AND (g.valid_to IS NULL OR g.valid_to > now())
  AND (
    (g.role = 'park_head' AND $2 <> '' AND (
      (g.scope_type = 'park' AND g.scope_id = $2::uuid)
      OR (g.scope_type = 'tenant' AND m.primary_location_id = $2::uuid)
    ))
    OR g.role = 'hr'
  )
LIMIT 500`

	sqlLeaveOutbox = `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, '1.0.0', $4, $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)`
)

func (r *Repository) CreateLeaveRequest(ctx context.Context, cmd ports.CreateLeaveRequestCommand) (ports.LeaveRequestWrite, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	defer rollback(ctx, tx)

	fingerprint := requestFingerprint(cmd.WorkforceMemberID, cmd.StartsOn, cmd.EndsOn, strings.TrimSpace(cmd.Reason))
	reservation, err := reserveIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveRequestCreate, cmd.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return ports.LeaveRequestWrite{}, err
		}
		return r.replayLeaveRequest(ctx, cmd.TenantID, reservation)
	}

	// Serialize this person's overlap check against a concurrent raise.
	var personName, parkID string
	if err := tx.QueryRow(ctx, sqlLeaveLockMember, cmd.TenantID, cmd.WorkforceMemberID).Scan(&personName, &parkID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.LeaveRequestWrite{}, ports.ErrNotFound
		}
		return ports.LeaveRequestWrite{}, err
	}
	if cmd.ParkID == "" {
		cmd.ParkID = parkID
	}
	var overlap int
	err = tx.QueryRow(ctx, sqlLeaveOverlap, cmd.TenantID, cmd.WorkforceMemberID, cmd.StartsOn, cmd.EndsOn).Scan(&overlap)
	if err == nil {
		return ports.LeaveRequestWrite{}, ports.ErrLeaveOverlap
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ports.LeaveRequestWrite{}, err
	}

	var id string
	if err := tx.QueryRow(ctx, sqlLeaveInsert,
		cmd.TenantID, cmd.WorkforceMemberID, cmd.ParkID, cmd.StartsOn, cmd.EndsOn, strings.TrimSpace(cmd.Reason),
		cmd.ParkHeadRequired, cmd.HRRequired, cmd.ActorUserID, cmd.IdempotencyKey, fingerprint,
	).Scan(&id); err != nil {
		return ports.LeaveRequestWrite{}, mapWriteErr(err)
	}
	scope := "park"
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorUserID, "workforce.leave.request", "leave_request", id, &scope, map[string]any{
		"workforce_member_id": cmd.WorkforceMemberID, "park_id": cmd.ParkID,
		"starts_on": cmd.StartsOn, "ends_on": cmd.EndsOn,
	}); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	row, err := txLeaveRequest(ctx, tx, cmd.TenantID, id)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if err := emitLeaveEvent(ctx, tx, EventLeaveRequested, row, cmd.TenantID, cmd.ActorUserID, "", "", cmd.BusinessDate); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if err := completeIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveRequestCreate, cmd.IdempotencyKey, "leave_request", id, row); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	return ports.LeaveRequestWrite{Row: row}, nil
}

func (r *Repository) WithdrawLeaveRequest(ctx context.Context, cmd ports.WithdrawLeaveRequestCommand) (ports.LeaveRequestWrite, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	defer rollback(ctx, tx)

	fingerprint := requestFingerprint(cmd.LeaveRequestID, "withdraw")
	reservation, err := reserveIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveRequestWithdraw, cmd.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return ports.LeaveRequestWrite{}, err
		}
		return r.replayLeaveRequest(ctx, cmd.TenantID, reservation)
	}

	lock, err := lockLeaveRequest(ctx, tx, cmd.TenantID, cmd.LeaveRequestID)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if lock.memberID != cmd.WorkforceMemberID {
		return ports.LeaveRequestWrite{}, ports.ErrNotFound
	}
	if lock.status != domain.LeaveRequestStatusPending {
		return ports.LeaveRequestWrite{}, ports.ErrLeaveNotPending
	}
	tag, err := tx.Exec(ctx, sqlLeaveWithdraw, cmd.TenantID, cmd.LeaveRequestID)
	if err != nil {
		return ports.LeaveRequestWrite{}, mapWriteErr(err)
	}
	if tag.RowsAffected() != 1 {
		return ports.LeaveRequestWrite{}, ports.ErrLeaveNotPending
	}
	scope := "park"
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorUserID, "workforce.leave.withdraw", "leave_request", cmd.LeaveRequestID, &scope, nil); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	row, err := txLeaveRequest(ctx, tx, cmd.TenantID, cmd.LeaveRequestID)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if err := emitLeaveEvent(ctx, tx, EventLeaveWithdrawn, row, cmd.TenantID, cmd.ActorUserID, "", "", cmd.BusinessDate); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if err := completeIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveRequestWithdraw, cmd.IdempotencyKey, "leave_request", cmd.LeaveRequestID, row); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	return ports.LeaveRequestWrite{Row: row}, nil
}

func (r *Repository) DecideLeaveRequest(ctx context.Context, cmd ports.DecideLeaveRequestCommand) (ports.LeaveRequestWrite, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	defer rollback(ctx, tx)

	fingerprint := requestFingerprint(cmd.LeaveRequestID, cmd.Slot, cmd.Decision, strings.TrimSpace(cmd.Note))
	reservation, err := reserveIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveRequestDecide, cmd.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return ports.LeaveRequestWrite{}, err
		}
		return r.replayLeaveRequest(ctx, cmd.TenantID, reservation)
	}

	lock, err := lockLeaveRequest(ctx, tx, cmd.TenantID, cmd.LeaveRequestID)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if lock.status != domain.LeaveRequestStatusPending {
		return ports.LeaveRequestWrite{}, ports.ErrLeaveNotPending
	}
	var required bool
	var existing, otherDecision string
	var otherRequired bool
	var updateSQL string
	switch cmd.Slot {
	case domain.LeaveSlotParkHead:
		required, existing = lock.parkHeadRequired, lock.parkHeadDecision
		otherRequired, otherDecision = lock.hrRequired, lock.hrDecision
		updateSQL = sqlLeaveDecideParkHead
	case domain.LeaveSlotHR:
		required, existing = lock.hrRequired, lock.hrDecision
		otherRequired, otherDecision = lock.parkHeadRequired, lock.parkHeadDecision
		updateSQL = sqlLeaveDecideHR
	default:
		return ports.LeaveRequestWrite{}, ports.ErrLeaveSlotNotRequired
	}
	if !required {
		return ports.LeaveRequestWrite{}, ports.ErrLeaveSlotNotRequired
	}
	if existing != "" {
		return ports.LeaveRequestWrite{}, ports.ErrLeaveSlotDecided
	}

	// Final outcome: any rejection ends it; the last required approval
	// approves it and mirrors the absence.
	status := domain.LeaveRequestStatusPending
	absenceID := ""
	switch cmd.Decision {
	case domain.LeaveDecisionRejected:
		status = domain.LeaveRequestStatusRejected
	case domain.LeaveDecisionApproved:
		if !otherRequired || otherDecision == domain.LeaveDecisionApproved {
			status = domain.LeaveRequestStatusApproved
		}
	default:
		return ports.LeaveRequestWrite{}, ports.ErrLeaveSlotNotRequired
	}
	if status == domain.LeaveRequestStatusApproved {
		scopeType, scopeID := "park", lock.parkID
		if scopeID == "" {
			scopeType, scopeID = "tenant", cmd.TenantID
		}
		if err := tx.QueryRow(ctx, sqlLeaveMirrorAbsence,
			cmd.TenantID, lock.memberID, scopeType, scopeID, lock.startsOn, lock.endsOn, lock.raisedBy, cmd.ActorUserID,
		).Scan(&absenceID); err != nil {
			return ports.LeaveRequestWrite{}, mapWriteErr(err)
		}
	}
	tag, err := tx.Exec(ctx, updateSQL, cmd.TenantID, cmd.LeaveRequestID, cmd.Decision, cmd.ActorUserID, strings.TrimSpace(cmd.Note), status, absenceID)
	if err != nil {
		return ports.LeaveRequestWrite{}, mapWriteErr(err)
	}
	if tag.RowsAffected() != 1 {
		return ports.LeaveRequestWrite{}, ports.ErrLeaveSlotDecided
	}
	scope := "park"
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorUserID, "workforce.leave.decide", "leave_request", cmd.LeaveRequestID, &scope, map[string]any{
		"slot": cmd.Slot, "decision": cmd.Decision, "status": status, "absence_id": absenceID,
	}); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	row, err := txLeaveRequest(ctx, tx, cmd.TenantID, cmd.LeaveRequestID)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	switch status {
	case domain.LeaveRequestStatusApproved:
		err = emitLeaveEvent(ctx, tx, EventLeaveApproved, row, cmd.TenantID, cmd.ActorUserID, cmd.Slot, cmd.Note, cmd.BusinessDate)
	case domain.LeaveRequestStatusRejected:
		err = emitLeaveEvent(ctx, tx, EventLeaveRejected, row, cmd.TenantID, cmd.ActorUserID, cmd.Slot, cmd.Note, cmd.BusinessDate)
	}
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if err := completeIdempotency(ctx, tx, cmd.TenantID, idemScopeLeaveRequestDecide, cmd.IdempotencyKey, "leave_request", cmd.LeaveRequestID, row); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	return ports.LeaveRequestWrite{Row: row}, nil
}

type leaveRequestLock struct {
	status, memberID, parkHeadDecision, hrDecision, parkID, startsOn, endsOn, raisedBy string
	parkHeadRequired, hrRequired                                                       bool
}

func lockLeaveRequest(ctx context.Context, tx pgx.Tx, tenantID, id string) (leaveRequestLock, error) {
	var l leaveRequestLock
	err := tx.QueryRow(ctx, sqlLeaveLock, tenantID, id).Scan(&l.status, &l.memberID, &l.parkHeadRequired, &l.hrRequired,
		&l.parkHeadDecision, &l.hrDecision, &l.parkID, &l.startsOn, &l.endsOn, &l.raisedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ports.ErrNotFound
	}
	return l, err
}

func (r *Repository) replayLeaveRequest(ctx context.Context, tenantID string, reservation idemReservation) (ports.LeaveRequestWrite, error) {
	if len(reservation.snapshot) > 0 {
		var row ports.LeaveRequestRow
		if err := json.Unmarshal(reservation.snapshot, &row); err == nil && row.LeaveRequestID != "" {
			return ports.LeaveRequestWrite{Row: row, Replayed: true}, nil
		}
	}
	if reservation.resultID == "" {
		return ports.LeaveRequestWrite{}, ports.ErrNotFound
	}
	row, err := r.GetLeaveRequest(contextWithoutCancel(ctx), tenantID, reservation.resultID)
	if err != nil {
		return ports.LeaveRequestWrite{}, err
	}
	return ports.LeaveRequestWrite{Row: row, Replayed: true}, nil
}

func txLeaveRequest(ctx context.Context, tx pgx.Tx, tenantID, id string) (ports.LeaveRequestRow, error) {
	rows, err := tx.Query(ctx, leaveRequestSelectSQL+sqlLeaveByID, tenantID, id)
	if err != nil {
		return ports.LeaveRequestRow{}, err
	}
	out, err := scanLeaveRequestRows(rows)
	if err != nil {
		return ports.LeaveRequestRow{}, err
	}
	if len(out) == 0 {
		return ports.LeaveRequestRow{}, ports.ErrNotFound
	}
	return out[0], nil
}

func scanLeaveRequestRows(rows pgx.Rows) ([]ports.LeaveRequestRow, error) {
	defer rows.Close()
	out := make([]ports.LeaveRequestRow, 0, 8)
	for rows.Next() {
		var row ports.LeaveRequestRow
		if err := rows.Scan(
			&row.LeaveRequestID, &row.WorkforceMemberID,
			&row.PersonName, &row.RoleHint, &row.DesignationGrade, &row.DesignationLabel,
			&row.ParkID, &row.ParkLabel,
			&row.StartsOn, &row.EndsOn, &row.Reason, &row.Status,
			&row.ParkHeadRequired, &row.HRRequired,
			&row.ParkHeadDecision, &row.ParkHeadDecidedBy, &row.ParkHeadDeciderNm, &row.ParkHeadDecidedAt, &row.ParkHeadNote,
			&row.HRDecision, &row.HRDecidedBy, &row.HRDeciderName, &row.HRDecidedAt, &row.HRNote,
			&row.DecidedAt, &row.AbsenceID, &row.RaisedByUserID, &row.RaisedAt, &row.RowVersion,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) GetLeaveRequest(ctx context.Context, tenantID, id string) (ports.LeaveRequestRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, leaveRequestSelectSQL+sqlLeaveByID, tenantID, id)
	if err != nil {
		return ports.LeaveRequestRow{}, err
	}
	out, err := scanLeaveRequestRows(rows)
	if err != nil {
		return ports.LeaveRequestRow{}, err
	}
	if len(out) == 0 {
		return ports.LeaveRequestRow{}, ports.ErrNotFound
	}
	return out[0], nil
}

func (r *Repository) ListLeaveRequestsForMember(ctx context.Context, tenantID, memberID, today string, limit int) ([]ports.LeaveRequestRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if limit <= 0 || limit > domain.MaxLeavePageSize {
		limit = domain.MaxLeavePageSize
	}
	rows, err := r.pool.Query(ctx, leaveRequestSelectSQL+sqlLeaveForMember, tenantID, memberID, limit)
	if err != nil {
		return nil, err
	}
	return scanLeaveRequestRows(rows)
}

func (r *Repository) ListLeaveQueue(ctx context.Context, params ports.LeaveQueueParams) (ports.LeavePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := clampLeaveRequestLimit(params.Limit)
	cursorAt, cursorID, err := decodeLeaveRequestCursor(params.Cursor)
	if err != nil {
		return ports.LeavePage{}, err
	}
	parks := params.ParkHeadParks
	if parks == nil {
		parks = []string{}
	}
	rows, err := r.pool.Query(ctx, leaveRequestSelectSQL+sqlLeaveQueue,
		params.TenantID, params.Any, params.HR, parks, cursorAt, cursorID, limit+1)
	if err != nil {
		return ports.LeavePage{}, err
	}
	out, err := scanLeaveRequestRows(rows)
	if err != nil {
		return ports.LeavePage{}, err
	}
	return pageLeaveRequests(out, limit), nil
}

func (r *Repository) ListLeaveRequestsAdmin(ctx context.Context, params ports.LeaveAdminListParams) (ports.LeavePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := clampLeaveRequestLimit(params.Limit)
	cursorAt, cursorID, err := decodeLeaveRequestCursor(params.Cursor)
	if err != nil {
		return ports.LeavePage{}, err
	}
	rows, err := r.pool.Query(ctx, leaveRequestSelectSQL+sqlLeaveAdminList,
		params.TenantID, params.Status, params.ParkID, cursorAt, cursorID, limit+1)
	if err != nil {
		return ports.LeavePage{}, err
	}
	out, err := scanLeaveRequestRows(rows)
	if err != nil {
		return ports.LeavePage{}, err
	}
	return pageLeaveRequests(out, limit), nil
}

func (r *Repository) LeaveTodayForMember(ctx context.Context, tenantID, memberID, businessDate string) (*ports.LeaveRequestRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, leaveRequestSelectSQL+sqlLeaveToday, tenantID, memberID, businessDate)
	if err != nil {
		return nil, err
	}
	out, err := scanLeaveRequestRows(rows)
	if err != nil || len(out) == 0 {
		return nil, err
	}
	return &out[0], nil
}

func (r *Repository) GetLeaveApprovalConfig(ctx context.Context, tenantID string) (ports.LeaveApprovalConfigRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	row := ports.LeaveApprovalConfigRow{ParkHeadRequired: true, HRRequired: true}
	var updatedAt *time.Time
	err := r.pool.QueryRow(ctx, sqlLeaveConfigGet, tenantID).Scan(
		&row.ParkHeadRequired, &row.HRRequired, &row.UpdatedBy, &row.UpdatedByName, &updatedAt, &row.RowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, nil
	}
	if err != nil {
		return ports.LeaveApprovalConfigRow{}, err
	}
	row.UpdatedAt = updatedAt
	row.Stored = true
	return row, nil
}

func (r *Repository) SetLeaveApprovalConfig(ctx context.Context, tenantID, actorUserID string, update domain.LeaveApprovalConfigUpdate) (ports.LeaveApprovalConfigRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.LeaveApprovalConfigRow{}, err
	}
	defer rollback(ctx, tx)
	var version int
	err = tx.QueryRow(ctx, sqlLeaveConfigSet, tenantID, update.ParkHeadRequired, update.HRRequired, actorUserID, update.RowVersion).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.LeaveApprovalConfigRow{}, ports.ErrLeaveConfigConflict
	}
	if err != nil {
		return ports.LeaveApprovalConfigRow{}, mapWriteErr(err)
	}
	scope := "tenant"
	if err := insertAudit(ctx, tx, tenantID, actorUserID, "workforce.leave.config", "leave_approval_config", tenantID, &scope, map[string]any{
		"park_head_required": update.ParkHeadRequired, "hr_required": update.HRRequired,
	}); err != nil {
		return ports.LeaveApprovalConfigRow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.LeaveApprovalConfigRow{}, err
	}
	return r.GetLeaveApprovalConfig(contextWithoutCancel(ctx), tenantID)
}

func (r *Repository) ResolveLeaveApproverMembers(ctx context.Context, tenantID, parkID string) (ports.LeaveApproverMembers, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return resolveLeaveApprovers(ctx, r.pool, tenantID, parkID)
}

type leaveRequestQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func resolveLeaveApprovers(ctx context.Context, q leaveRequestQuerier, tenantID, parkID string) (ports.LeaveApproverMembers, error) {
	rows, err := q.Query(ctx, sqlLeaveApprovers, tenantID, parkID)
	if err != nil {
		return ports.LeaveApproverMembers{}, err
	}
	defer rows.Close()
	out := ports.LeaveApproverMembers{ParkHead: []string{}, HR: []string{}}
	for rows.Next() {
		var memberID, role string
		if err := rows.Scan(&memberID, &role); err != nil {
			return ports.LeaveApproverMembers{}, err
		}
		switch role {
		case "park_head":
			out.ParkHead = append(out.ParkHead, memberID)
		case "hr":
			out.HR = append(out.HR, memberID)
		}
	}
	return out, rows.Err()
}

func clampLeaveRequestLimit(limit int) int {
	if limit <= 0 || limit > domain.MaxLeavePageSize {
		return domain.MaxLeavePageSize
	}
	return limit
}

func pageLeaveRequests(rows []ports.LeaveRequestRow, limit int) ports.LeavePage {
	page := ports.LeavePage{Rows: rows}
	if len(rows) > limit {
		page.Rows = rows[:limit]
		last := page.Rows[limit-1]
		page.NextCursor = encodeLeaveRequestCursor(last.RaisedAt, last.LeaveRequestID)
	}
	return page
}

func encodeLeaveRequestCursor(raisedAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raisedAt.UTC().Format(time.RFC3339Nano) + "|" + id))
}

// decodeLeaveRequestCursor returns nil pointers for an empty cursor so the SQL
// predicate collapses to "no cursor"; a malformed cursor is an error, never a
// silently-unbounded page.
func decodeLeaveRequestCursor(cursor string) (*time.Time, *string, error) {
	if strings.TrimSpace(cursor) == "" {
		return nil, nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return nil, nil, errors.New("invalid cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid cursor: %w", err)
	}
	id := parts[1]
	return &at, &id, nil
}

// emitLeaveEvent writes one leave event into outbox_messages inside tx, with
// the approver member ids resolved in the same transaction so the notifier
// needs no second read.
func emitLeaveEvent(ctx context.Context, tx pgx.Tx, eventType string, row ports.LeaveRequestRow, tenantID, actorID, decidedSlot, note, businessDate string) error {
	approvers, err := resolveLeaveApprovers(ctx, tx, tenantID, row.ParkID)
	if err != nil {
		return fmt.Errorf("workforce: resolve leave approvers: %w", err)
	}
	now := time.Now().UTC()
	idempotencyKey := eventType + ":" + row.LeaveRequestID + ":" + fmt.Sprint(row.RowVersion)
	eventID := platformoutbox.DeterministicUUID(idempotencyKey)
	decidedBy, decidedName := "", ""
	switch decidedSlot {
	case domain.LeaveSlotParkHead:
		decidedBy, decidedName = row.ParkHeadDecidedBy, row.ParkHeadDeciderNm
	case domain.LeaveSlotHR:
		decidedBy, decidedName = row.HRDecidedBy, row.HRDeciderName
	}
	payload := LeaveEventPayload{
		LeaveRequestID:    row.LeaveRequestID,
		WorkforceMemberID: row.WorkforceMemberID,
		PersonName:        row.PersonName,
		ParkID:            row.ParkID,
		ParkLabel:         row.ParkLabel,
		StartsOn:          row.StartsOn,
		EndsOn:            row.EndsOn,
		DayCount:          leaveDayCount(row.StartsOn, row.EndsOn),
		Reason:            row.Reason,
		Status:            row.Status,
		DecidedSlot:       decidedSlot,
		DecidedByUserID:   decidedBy,
		DecidedByName:     decidedName,
		DecisionNote:      strings.TrimSpace(note),
		ParkHeadRequired:  row.ParkHeadRequired,
		HRRequired:        row.HRRequired,
		ParkHeadMemberIDs: approvers.ParkHead,
		HRMemberIDs:       approvers.HR,
		OccurredAt:        now.Format(time.RFC3339),
	}
	visibility := map[string]any{"tenant_id": tenantID}
	if row.ParkID != "" {
		visibility["park_id"] = row.ParkID
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": "1.0.0",
		"schema_ref":     leaveEventSchemaRef,
		"aggregate_type": leaveAggregateType,
		"aggregate_id":   row.LeaveRequestID,
		"occurred_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"recorded_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "workforce",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "human",
			"actor_id":   actorID,
			"actor_ref":  nil,
		},
		"subject_type":     leaveAggregateType,
		"subject_id":       row.LeaveRequestID,
		"visibility_scope": visibility,
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "leave_request:" + row.LeaveRequestID,
		}},
		"payload":  payload,
		"trace_id": "leave-request:" + row.LeaveRequestID,
	})
	if err != nil {
		return fmt.Errorf("workforce: marshal leave event: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":      actorID,
		"business_date": businessDate,
		"producer":      "workforce.leave",
	})
	if err != nil {
		return fmt.Errorf("workforce: marshal leave headers: %w", err)
	}
	if _, err := tx.Exec(ctx, sqlLeaveOutbox,
		tenantID, eventID, eventType, leaveAggregateType, row.LeaveRequestID,
		leaveEventTopic, envelope, headers, idempotencyKey,
	); err != nil {
		return fmt.Errorf("workforce: outbox %s: %w", eventType, err)
	}
	return nil
}

// leaveDayCount is the inclusive day span of a request; both bounds are
// business DATES, so this is calendar arithmetic and never hour math.
func leaveDayCount(startsOn, endsOn string) int {
	start, err1 := time.Parse("2006-01-02", startsOn)
	end, err2 := time.Parse("2006-01-02", endsOn)
	if err1 != nil || err2 != nil || end.Before(start) {
		return 0
	}
	return int(end.Sub(start).Hours()/24) + 1
}
