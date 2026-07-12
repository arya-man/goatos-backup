package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const defaultQueryTimeout = 3 * time.Second

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

func (r *Repository) ListOperators(ctx context.Context, params ports.ListOperatorsParams) ([]domain.OperatorProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, operatorSelectSQL(`
WHERE wm.tenant_id = $1::uuid
  AND ($2 = '' OR wm.status = $2)
  AND ($3 = '' OR wm.primary_role_hint = $3)
  AND ($4 = '' OR wm.primary_location_id = $4::uuid)
  AND (
    $5 = ''
    OR lower(wm.display_name) LIKE '%' || lower($5) || '%'
    OR lower(wm.display_code) LIKE '%' || lower($5) || '%'
  )
ORDER BY wm.status, wm.updated_at DESC, wm.workforce_member_id DESC
LIMIT $6`), params.TenantID, params.Status, params.RoleHint, params.LocationID, params.Search, params.Limit)
	if err != nil {
		return nil, err
	}
	return scanOperators(rows)
}

func (r *Repository) CreateOperator(ctx context.Context, cmd ports.CreateOperatorCommand) (domain.OperatorProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	defer rollback(ctx, tx)
	metadata, err := json.Marshal(nonNilMap(cmd.Body.Metadata))
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	var operatorID string
	err = tx.QueryRow(ctx, `
INSERT INTO workforce_members (
  tenant_id, user_id, display_code, display_name, status,
  primary_role_hint, primary_location_id, metadata, created_by
) VALUES (
  $1::uuid, nullif($2, '')::uuid, $3, $4, $5,
  $6, nullif($7, '')::uuid, $8::jsonb, $9::uuid
)
RETURNING workforce_member_id::text`,
		cmd.TenantID,
		ptrValue(cmd.Body.UserID),
		cmd.Body.DisplayCode,
		cmd.Body.DisplayName,
		cmd.Body.Status,
		cmd.Body.PrimaryRoleHint,
		ptrValue(cmd.Body.PrimaryLocationID),
		metadata,
		cmd.ActorID,
	).Scan(&operatorID)
	if err != nil {
		return domain.OperatorProfile{}, mapWriteErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "operators.create", "workforce_member", operatorID, nil, map[string]any{"display_code": cmd.Body.DisplayCode}); err != nil {
		return domain.OperatorProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.OperatorProfile{}, err
	}
	return r.GetOperator(contextWithoutCancel(ctx), cmd.TenantID, operatorID)
}

func (r *Repository) GetOperator(ctx context.Context, tenantID, operatorID string) (domain.OperatorProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, operatorSelectSQL(`
WHERE wm.tenant_id = $1::uuid
  AND wm.workforce_member_id = $2::uuid
LIMIT 1`), tenantID, operatorID)
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	items, err := scanOperators(rows)
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	if len(items) == 0 {
		return domain.OperatorProfile{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) UpdateOperator(ctx context.Context, cmd ports.UpdateOperatorCommand) (domain.OperatorProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	defer rollback(ctx, tx)
	metadataSet := cmd.Body.Metadata != nil
	metadata, err := json.Marshal(nonNilMap(cmd.Body.Metadata))
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	var operatorID string
	err = tx.QueryRow(ctx, `
UPDATE workforce_members
SET display_name = CASE WHEN $4 <> '' THEN $4 ELSE display_name END,
    primary_role_hint = CASE WHEN $5 <> '' THEN $5 ELSE primary_role_hint END,
    primary_location_id = CASE WHEN $6::bool THEN nullif($7, '')::uuid ELSE primary_location_id END,
    metadata = CASE WHEN $8::bool THEN $9::jsonb ELSE metadata END,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND workforce_member_id = $2::uuid
  AND row_version = $3
RETURNING workforce_member_id::text`,
		cmd.TenantID,
		cmd.OperatorID,
		cmd.Body.RowVersion,
		ptrValue(cmd.Body.DisplayName),
		ptrValue(cmd.Body.PrimaryRoleHint),
		cmd.Body.PrimaryLocationID != nil,
		ptrValue(cmd.Body.PrimaryLocationID),
		metadataSet,
		metadata,
	).Scan(&operatorID)
	if err != nil {
		return domain.OperatorProfile{}, mapUpdateErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "operators.update", "workforce_member", operatorID, nil, map[string]any{"row_version": cmd.Body.RowVersion}); err != nil {
		return domain.OperatorProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.OperatorProfile{}, err
	}
	return r.GetOperator(contextWithoutCancel(ctx), cmd.TenantID, operatorID)
}

func (r *Repository) SetOperatorStatus(ctx context.Context, cmd ports.StatusCommand) (domain.OperatorProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	defer rollback(ctx, tx)
	var operatorID string
	err = tx.QueryRow(ctx, `
UPDATE workforce_members
SET status = $4,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND workforce_member_id = $2::uuid
  AND row_version = $3
RETURNING workforce_member_id::text`,
		cmd.TenantID,
		cmd.OperatorID,
		cmd.RowVersion,
		cmd.Status,
	).Scan(&operatorID)
	if err != nil {
		return domain.OperatorProfile{}, mapUpdateErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "operators."+cmd.Status, "workforce_member", operatorID, nil, map[string]any{"reason": cmd.Reason}); err != nil {
		return domain.OperatorProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.OperatorProfile{}, err
	}
	return r.GetOperator(contextWithoutCancel(ctx), cmd.TenantID, operatorID)
}

func (r *Repository) ListGrants(ctx context.Context, tenantID, operatorID string) ([]domain.GrantSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, grantsSQL(`
JOIN workforce_members wm
  ON wm.tenant_id = usg.tenant_id
 AND wm.user_id = usg.user_id
WHERE usg.tenant_id = $1::uuid
  AND wm.workforce_member_id = $2::uuid
ORDER BY usg.status, usg.valid_from DESC, usg.grant_id DESC`), tenantID, operatorID)
	if err != nil {
		return nil, err
	}
	return scanGrants(rows)
}

func (r *Repository) CreateGrant(ctx context.Context, cmd ports.CreateGrantCommand) (domain.GrantSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.GrantSummary{}, err
	}
	defer rollback(ctx, tx)
	userID, err := lookupOperatorUserID(ctx, tx, cmd.TenantID, cmd.OperatorID)
	if err != nil {
		return domain.GrantSummary{}, err
	}
	var grantID string
	err = tx.QueryRow(ctx, `
INSERT INTO user_scope_grants (
  tenant_id, user_id, role, scope_type, scope_id, status, valid_from, valid_to, created_by
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5::uuid, 'active', now(), nullif($6, '')::timestamptz, $7::uuid
)
RETURNING grant_id::text`, cmd.TenantID, userID, cmd.Body.Role, cmd.Body.ScopeType, cmd.Body.ScopeID, ptrValue(cmd.Body.ValidTo), cmd.ActorID).Scan(&grantID)
	if err != nil {
		return domain.GrantSummary{}, mapWriteErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "operators.grant.create", "user_scope_grant", grantID, &cmd.Body.ScopeType, map[string]any{"operator_id": cmd.OperatorID, "role": cmd.Body.Role, "scope_id": cmd.Body.ScopeID}); err != nil {
		return domain.GrantSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.GrantSummary{}, err
	}
	grants, err := r.ListGrants(contextWithoutCancel(ctx), cmd.TenantID, cmd.OperatorID)
	if err != nil {
		return domain.GrantSummary{}, err
	}
	for _, grant := range grants {
		if grant.GrantID == grantID {
			return grant, nil
		}
	}
	return domain.GrantSummary{}, ports.ErrNotFound
}

func (r *Repository) AssignCapability(ctx context.Context, cmd ports.CapabilityCommand) (domain.CapabilityAssignment, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.CapabilityAssignment{}, err
	}
	defer rollback(ctx, tx)
	if err := ensureOperatorExists(ctx, tx, cmd.TenantID, cmd.OperatorID); err != nil {
		return domain.CapabilityAssignment{}, err
	}
	var capabilityID string
	err = tx.QueryRow(ctx, `
INSERT INTO workforce_capabilities (tenant_id, capability_code, description, status)
VALUES ($1::uuid, $2, '', 'active')
ON CONFLICT (tenant_id, capability_code)
DO UPDATE SET status = 'active', updated_at = now()
RETURNING capability_id::text`, cmd.TenantID, cmd.Body.CapabilityCode).Scan(&capabilityID)
	if err != nil {
		return domain.CapabilityAssignment{}, mapWriteErr(err)
	}
	var assignmentID string
	err = tx.QueryRow(ctx, `
INSERT INTO workforce_member_capabilities (
  tenant_id, workforce_member_id, capability_id, scope_type, scope_id, status, valid_from, valid_to, assigned_by
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4, $5::uuid, 'active', now(), nullif($6, '')::timestamptz, $7::uuid
)
ON CONFLICT (tenant_id, workforce_member_id, capability_id, scope_type, scope_id)
  WHERE status = 'active' AND valid_to IS NULL
DO UPDATE SET status = 'active', updated_at = now(), assigned_by = EXCLUDED.assigned_by
RETURNING member_capability_id::text`,
		cmd.TenantID,
		cmd.OperatorID,
		capabilityID,
		cmd.Body.ScopeType,
		cmd.Body.ScopeID,
		ptrValue(cmd.Body.ValidTo),
		cmd.ActorID,
	).Scan(&assignmentID)
	if err != nil {
		return domain.CapabilityAssignment{}, mapWriteErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "operators.capability.assign", "workforce_member_capability", assignmentID, &cmd.Body.ScopeType, map[string]any{"operator_id": cmd.OperatorID, "capability_code": cmd.Body.CapabilityCode, "scope_id": cmd.Body.ScopeID}); err != nil {
		return domain.CapabilityAssignment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CapabilityAssignment{}, err
	}
	items, err := r.ListCapabilities(contextWithoutCancel(ctx), cmd.TenantID, cmd.OperatorID)
	if err != nil {
		return domain.CapabilityAssignment{}, err
	}
	for _, item := range items {
		if item.MemberCapabilityID == assignmentID {
			return item, nil
		}
	}
	return domain.CapabilityAssignment{}, ports.ErrNotFound
}

func (r *Repository) RemoveCapability(ctx context.Context, cmd ports.RemoveCapabilityCommand) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	tag, err := tx.Exec(ctx, `
UPDATE workforce_member_capabilities
SET status = 'revoked', valid_to = COALESCE(valid_to, now()), updated_at = now()
WHERE tenant_id = $1::uuid
  AND workforce_member_id = $2::uuid
  AND member_capability_id = $3::uuid
  AND status = 'active'`, cmd.TenantID, cmd.OperatorID, cmd.CapabilityID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "operators.capability.revoke", "workforce_member_capability", cmd.CapabilityID, nil, map[string]any{"operator_id": cmd.OperatorID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) ListCapabilities(ctx context.Context, tenantID, operatorID string) ([]domain.CapabilityAssignment, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, capabilitiesSQL(`
WHERE wmc.tenant_id = $1::uuid
  AND wmc.workforce_member_id = $2::uuid
  AND (wmc.valid_to IS NULL OR wmc.valid_to > now())
ORDER BY wmc.status, wc.capability_code, wmc.created_at DESC`), tenantID, operatorID)
	if err != nil {
		return nil, err
	}
	return scanCapabilities(rows)
}

func (r *Repository) ListDevices(ctx context.Context, tenantID, operatorID string) ([]domain.DeviceSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, devicesSQL(`
WHERE tenant_id = $1::uuid
  AND workforce_member_id = $2::uuid
ORDER BY status, last_seen_at DESC
LIMIT 100`), tenantID, operatorID)
	if err != nil {
		return nil, err
	}
	return scanDevices(rows)
}

func (r *Repository) RevokeDevice(ctx context.Context, cmd ports.RevokeDeviceCommand) (domain.DeviceSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	defer rollback(ctx, tx)
	tag, err := tx.Exec(ctx, `
UPDATE workforce_member_devices
SET status = 'revoked',
    revoked_by = $4::uuid,
    revoked_at = now(),
    metadata = metadata || jsonb_build_object('revocation_reason', $5),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND workforce_member_id = $2::uuid
  AND device_id = $3::uuid
  AND row_version = $6`,
		cmd.TenantID,
		cmd.OperatorID,
		cmd.DeviceID,
		cmd.ActorID,
		cmd.Reason,
		cmd.RowVersion,
	)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.DeviceSummary{}, ports.ErrConflict
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "operators.device.revoke", "workforce_member_device", cmd.DeviceID, nil, map[string]any{"operator_id": cmd.OperatorID, "reason": cmd.Reason}); err != nil {
		return domain.DeviceSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.DeviceSummary{}, err
	}
	devices, err := r.ListDevices(contextWithoutCancel(ctx), cmd.TenantID, cmd.OperatorID)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	for _, item := range devices {
		if item.DeviceID == cmd.DeviceID {
			return item, nil
		}
	}
	return domain.DeviceSummary{}, ports.ErrNotFound
}

func (r *Repository) ListSourceCandidates(ctx context.Context, params ports.ListSourceCandidatesParams) ([]domain.SourceCandidate, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT
  external_identity_id::text,
  workforce_member_id::text,
  source_system,
  source_flow,
  external_ref_type,
  external_ref_hash,
  status,
  confidence::float8,
  first_seen_at,
  last_seen_at,
  observation_count,
  review_reason,
  metadata->>'observed_role_hint',
  metadata->>'observed_scope_hint',
  metadata,
  row_version
FROM workforce_external_identities
WHERE tenant_id = $1::uuid
  AND ($2 = '' OR status = $2)
  AND ($3 = '' OR source_system = $3)
ORDER BY status, last_seen_at DESC, external_identity_id DESC
LIMIT $4`, params.TenantID, params.Status, params.SourceSystem, params.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.SourceCandidate{}
	for rows.Next() {
		var item domain.SourceCandidate
		var operatorID, reviewReason, roleHint, scopeHint pgtype.Text
		var metadata []byte
		var firstSeen, lastSeen time.Time
		if err := rows.Scan(&item.CandidateID, &operatorID, &item.SourceSystem, &item.SourceFlow, &item.ExternalRefType, &item.ExternalRefHash, &item.Status, &item.Confidence, &firstSeen, &lastSeen, &item.ObservationCount, &reviewReason, &roleHint, &scopeHint, &metadata, &item.RowVersion); err != nil {
			return nil, err
		}
		item.OperatorID = textPtr(operatorID)
		item.ReviewReason = textPtr(reviewReason)
		item.ObservedRoleHint = textPtr(roleHint)
		item.ObservedScopeHint = textPtr(scopeHint)
		item.Metadata = decodeMap(metadata)
		item.FirstSeenAt = firstSeen.UTC().Format(time.RFC3339)
		item.LastSeenAt = lastSeen.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) MapSourceCandidate(ctx context.Context, cmd ports.MapSourceCandidateCommand) (domain.SourceCandidate, error) {
	return r.updateSourceCandidate(ctx, cmd.TenantID, cmd.ActorID, cmd.CandidateID, cmd.Body.RowVersion, "mapped", &cmd.Body.OperatorID, cmd.Body.Reason)
}

func (r *Repository) RejectSourceCandidate(ctx context.Context, cmd ports.RejectSourceCandidateCommand) (domain.SourceCandidate, error) {
	return r.updateSourceCandidate(ctx, cmd.TenantID, cmd.ActorID, cmd.CandidateID, cmd.Body.RowVersion, "rejected", nil, cmd.Body.Reason)
}

func (r *Repository) GetMemberForActor(ctx context.Context, tenantID, actorID string) (domain.OperatorProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, operatorSelectSQL(`
WHERE wm.tenant_id = $1::uuid
  AND wm.user_id = $2::uuid
LIMIT 1`), tenantID, actorID)
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	items, err := scanOperators(rows)
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	if len(items) == 0 {
		return domain.OperatorProfile{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) ListActiveGrantsForActor(ctx context.Context, tenantID, actorID string) ([]domain.GrantSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, grantsSQL(`
WHERE usg.tenant_id = $1::uuid
  AND usg.user_id = $2::uuid
  AND usg.status = 'active'
  AND usg.valid_from <= now()
  AND (usg.valid_to IS NULL OR usg.valid_to > now())
ORDER BY usg.valid_from DESC, usg.grant_id DESC`), tenantID, actorID)
	if err != nil {
		return nil, err
	}
	return scanGrants(rows)
}

// DeregisterDevice is the app-facing logout decouple: clears the FCM push binding and revokes the
// caller's OWN device (scoped by registered_by = actor), so a logged-out user stops receiving pushes
// on that device. Idempotent-ish: an unknown/foreign device_id affects 0 rows -> ErrNotFound.
func (r *Repository) DeregisterDevice(ctx context.Context, cmd ports.DeregisterDeviceCommand) (domain.DeviceSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	defer rollback(ctx, tx)
	// Capture the device's CURRENT fcm_token before it is cleared below, locking the row (FOR UPDATE)
	// so no concurrent register/heartbeat can rotate the token between this read and the revoke. This
	// is the match key for the suppress step further down. RowsAffected() on the UPDATE below (not
	// this SELECT) remains the sole source of truth for the idempotent/not-found branch, so a miss here
	// (ErrNoRows: already revoked or foreign device) is not treated as an error -- fcmToken just stays
	// nil and the UPDATE's own 0-rows check short-circuits before the suppress step runs.
	var fcmToken *string
	if err := tx.QueryRow(ctx, `
SELECT fcm_token
FROM workforce_member_devices
WHERE tenant_id = $1::uuid
  AND device_id = $2::uuid
  AND registered_by = $3::uuid
  AND status <> 'revoked'
FOR UPDATE`,
		cmd.TenantID,
		cmd.DeviceID,
		cmd.ActorID,
	).Scan(&fcmToken); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.DeviceSummary{}, err
	}
	// Idempotent by design (logout is best-effort/replay-prone): only the FIRST deregister for the
	// caller's own device flips state + audits. `status <> 'revoked'` makes a retry a no-op — no
	// revoked_at rewrite, no row_version bump, no duplicate audit row.
	tag, err := tx.Exec(ctx, `
UPDATE workforce_member_devices
SET status = 'revoked',
    push_token_hash = NULL,
    fcm_token = NULL,
    revoked_by = $3::uuid,
    revoked_at = now(),
    metadata = metadata || jsonb_build_object('revocation_reason', 'app_logout_decouple'),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND device_id = $2::uuid
  AND registered_by = $3::uuid
  AND status <> 'revoked'`,
		cmd.TenantID,
		cmd.DeviceID,
		cmd.ActorID,
	)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	if tag.RowsAffected() == 0 {
		// 0 rows = either an unknown/foreign device (GetDeviceForActor -> ErrNotFound = 404) or an
		// already-revoked device the actor owns (idempotent replay -> return it, no side effects).
		// The untouched tx rolls back via the deferred rollback.
		return r.GetDeviceForActor(contextWithoutCancel(ctx), cmd.TenantID, cmd.ActorID, cmd.DeviceID)
	}
	// Close the offline-logout / shared-phone delivery window: a notification_requests row snapshots
	// the recipient's raw FCM token into recipient_ref at QUEUE time (calendar's QueueRoleNotifications
	// -- see docs/decisions/vaccination-notification-rules.md §4c). If the client's deleteToken() never
	// reached FCM (offline at logout), rows already queued against this token could still deliver to
	// the phone after a different user logs in and the token is reused, before the OS rotates it. In
	// the SAME transaction as the revoke, suppress this device's still-pending rows keyed by the exact
	// token being cleared. Only 'queued'/'failed' rows are touched -- 'sent'/'read' rows are audit
	// history and must never be rewritten. This complements, not replaces, the mobile deleteToken()
	// best-effort call.
	if fcmToken != nil {
		if _, err := tx.Exec(ctx, `
UPDATE notification_requests
SET status = 'suppressed',
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND recipient_ref = $2
  AND status IN ('queued', 'failed')`,
			cmd.TenantID,
			*fcmToken,
		); err != nil {
			return domain.DeviceSummary{}, err
		}
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "app.device.deregister", "workforce_member_device", cmd.DeviceID, nil, map[string]any{"reason": "app_logout_decouple"}); err != nil {
		return domain.DeviceSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.DeviceSummary{}, err
	}
	return r.GetDeviceForActor(contextWithoutCancel(ctx), cmd.TenantID, cmd.ActorID, cmd.DeviceID)
}

func (r *Repository) RegisterDevice(ctx context.Context, cmd ports.RegisterDeviceCommand) (domain.DeviceSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	defer rollback(ctx, tx)
	profile, err := r.getMemberForActorTx(ctx, tx, cmd.TenantID, cmd.ActorID)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	metadata, err := json.Marshal(nonNilMap(cmd.Body.Metadata))
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	var deviceID string
	err = tx.QueryRow(ctx, `
INSERT INTO workforce_member_devices (
  tenant_id, workforce_member_id, platform, app_install_id, device_public_key_hash,
  push_token_hash, fcm_token, app_version, os_version, status, last_seen_at, registered_by, metadata
) VALUES (
  $1::uuid, $2::uuid, 'android', $3, nullif($4, ''), nullif($5, ''), nullif($6, ''), $7, $8, 'active', now(), $9::uuid, $10::jsonb
)
ON CONFLICT (tenant_id, app_install_id)
DO UPDATE SET workforce_member_id = EXCLUDED.workforce_member_id,
              device_public_key_hash = EXCLUDED.device_public_key_hash,
              push_token_hash = EXCLUDED.push_token_hash,
              fcm_token = EXCLUDED.fcm_token,
              app_version = EXCLUDED.app_version,
              os_version = EXCLUDED.os_version,
              status = 'active',
              last_seen_at = now(),
              metadata = EXCLUDED.metadata,
              row_version = workforce_member_devices.row_version + 1
RETURNING device_id::text`,
		cmd.TenantID,
		profile.OperatorID,
		cmd.Body.AppInstallID,
		ptrValue(cmd.Body.DevicePublicKeyHash),
		ptrValue(cmd.Body.PushTokenHash),
		ptrValue(cmd.Body.FcmToken),
		cmd.Body.AppVersion,
		cmd.Body.OSVersion,
		cmd.ActorID,
		metadata,
	).Scan(&deviceID)
	if err != nil {
		return domain.DeviceSummary{}, mapWriteErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "app.device.register", "workforce_member_device", deviceID, nil, map[string]any{"operator_id": profile.OperatorID, "app_version": cmd.Body.AppVersion}); err != nil {
		return domain.DeviceSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.DeviceSummary{}, err
	}
	return r.GetDeviceForActor(contextWithoutCancel(ctx), cmd.TenantID, cmd.ActorID, deviceID)
}

func (r *Repository) HeartbeatDevice(ctx context.Context, cmd ports.HeartbeatDeviceCommand) (domain.DeviceSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	defer rollback(ctx, tx)
	profile, err := r.getMemberForActorTx(ctx, tx, cmd.TenantID, cmd.ActorID)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	metadata, err := json.Marshal(nonNilMap(cmd.Body.Metadata))
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	tag, err := tx.Exec(ctx, `
UPDATE workforce_member_devices
SET app_version = CASE WHEN $4 <> '' THEN $4 ELSE app_version END,
    os_version = CASE WHEN $5 <> '' THEN $5 ELSE os_version END,
    push_token_hash = CASE WHEN $6 <> '' THEN $6 ELSE push_token_hash END,
    fcm_token = CASE WHEN $9 <> '' THEN $9 ELSE fcm_token END,
    metadata = CASE WHEN $7::bool THEN $8::jsonb ELSE metadata END,
    last_seen_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND workforce_member_id = $2::uuid
  AND device_id = $3::uuid`,
		cmd.TenantID,
		profile.OperatorID,
		cmd.DeviceID,
		strings.TrimSpace(cmd.Body.AppVersion),
		strings.TrimSpace(cmd.Body.OSVersion),
		ptrValue(cmd.Body.PushTokenHash),
		cmd.Body.Metadata != nil,
		metadata,
		ptrValue(cmd.Body.FcmToken),
	)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.DeviceSummary{}, ports.ErrNotFound
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "app.device.heartbeat", "workforce_member_device", cmd.DeviceID, nil, map[string]any{"operator_id": profile.OperatorID}); err != nil {
		return domain.DeviceSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.DeviceSummary{}, err
	}
	return r.GetDeviceForActor(contextWithoutCancel(ctx), cmd.TenantID, cmd.ActorID, cmd.DeviceID)
}

func (r *Repository) GetDeviceForActor(ctx context.Context, tenantID, actorID, deviceID string) (domain.DeviceSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, devicesSQL(`
JOIN workforce_members wm
  ON wm.tenant_id = d.tenant_id
 AND wm.workforce_member_id = d.workforce_member_id
WHERE d.tenant_id = $1::uuid
  AND wm.user_id = $2::uuid
  AND d.device_id = $3::uuid
LIMIT 1`), tenantID, actorID, deviceID)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	items, err := scanDevices(rows)
	if err != nil {
		return domain.DeviceSummary{}, err
	}
	if len(items) == 0 {
		return domain.DeviceSummary{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) updateSourceCandidate(ctx context.Context, tenantID, actorID, candidateID string, rowVersion int, status string, operatorID *string, reason string) (domain.SourceCandidate, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.SourceCandidate{}, err
	}
	defer rollback(ctx, tx)
	tag, err := tx.Exec(ctx, `
UPDATE workforce_external_identities
SET status = $4,
    workforce_member_id = CASE WHEN $5 <> '' THEN $5::uuid ELSE workforce_member_id END,
    reviewed_by = $6::uuid,
    reviewed_at = now(),
    review_reason = nullif($7, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND external_identity_id = $2::uuid
  AND row_version = $3
  AND status IN ('candidate', 'conflict')`,
		tenantID,
		candidateID,
		rowVersion,
		status,
		ptrValue(operatorID),
		actorID,
		strings.TrimSpace(reason),
	)
	if err != nil {
		return domain.SourceCandidate{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.SourceCandidate{}, ports.ErrConflict
	}
	action := "operators.source_candidate." + status
	if err := insertAudit(ctx, tx, tenantID, actorID, action, "workforce_external_identity", candidateID, nil, map[string]any{"operator_id": ptrValue(operatorID), "reason": reason}); err != nil {
		return domain.SourceCandidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SourceCandidate{}, err
	}
	items, err := r.ListSourceCandidates(contextWithoutCancel(ctx), ports.ListSourceCandidatesParams{TenantID: tenantID, Limit: 500})
	if err != nil {
		return domain.SourceCandidate{}, err
	}
	for _, item := range items {
		if item.CandidateID == candidateID {
			return item, nil
		}
	}
	return domain.SourceCandidate{}, ports.ErrNotFound
}

func (r *Repository) getMemberForActorTx(ctx context.Context, tx pgx.Tx, tenantID, actorID string) (domain.OperatorProfile, error) {
	rows, err := tx.Query(ctx, operatorSelectSQL(`
WHERE wm.tenant_id = $1::uuid
  AND wm.user_id = $2::uuid
LIMIT 1`), tenantID, actorID)
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	items, err := scanOperators(rows)
	if err != nil {
		return domain.OperatorProfile{}, err
	}
	if len(items) == 0 {
		return domain.OperatorProfile{}, ports.ErrNotFound
	}
	return items[0], nil
}

func operatorSelectSQL(where string) string {
	return `
SELECT
  wm.workforce_member_id::text,
  wm.user_id::text,
  wm.display_code,
  wm.display_name,
  wm.status,
  wm.primary_role_hint,
  wm.primary_location_id::text,
  l.name,
  (
    SELECT count(*)::int
    FROM user_scope_grants usg
    WHERE usg.tenant_id = wm.tenant_id
      AND usg.user_id = wm.user_id
      AND usg.status = 'active'
      AND usg.valid_from <= now()
      AND (usg.valid_to IS NULL OR usg.valid_to > now())
  ) AS grant_count,
  (
    SELECT count(*)::int
    FROM workforce_member_capabilities wmc
    WHERE wmc.tenant_id = wm.tenant_id
      AND wmc.workforce_member_id = wm.workforce_member_id
      AND wmc.status = 'active'
      AND (wmc.valid_to IS NULL OR wmc.valid_to > now())
  ) AS capability_count,
  (
    SELECT count(*)::int
    FROM workforce_member_devices d
    WHERE d.tenant_id = wm.tenant_id
      AND d.workforce_member_id = wm.workforce_member_id
      AND d.status = 'active'
  ) AS active_device_count,
  wm.metadata,
  wm.row_version,
  wm.created_at,
  wm.updated_at
FROM workforce_members wm
LEFT JOIN locations l
  ON l.tenant_id = wm.tenant_id
 AND l.location_id = wm.primary_location_id
` + where
}

func scanOperators(rows pgx.Rows) ([]domain.OperatorProfile, error) {
	defer rows.Close()
	items := []domain.OperatorProfile{}
	for rows.Next() {
		var item domain.OperatorProfile
		var userID, locationID, locationName pgtype.Text
		var metadata []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&item.OperatorID, &userID, &item.DisplayCode, &item.DisplayName, &item.Status, &item.PrimaryRoleHint, &locationID, &locationName, &item.GrantCount, &item.CapabilityCount, &item.ActiveDeviceCount, &metadata, &item.RowVersion, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.UserID = textPtr(userID)
		item.PrimaryLocationID = textPtr(locationID)
		item.PrimaryLocation = textPtr(locationName)
		item.Metadata = decodeMap(metadata)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func grantsSQL(where string) string {
	return `
SELECT
  usg.grant_id::text,
  usg.user_id::text,
  usg.role,
  usg.scope_type,
  usg.scope_id::text,
  usg.status,
  usg.valid_from,
  usg.valid_to,
  usg.created_at,
  usg.created_by::text
FROM user_scope_grants usg
` + where
}

func scanGrants(rows pgx.Rows) ([]domain.GrantSummary, error) {
	defer rows.Close()
	items := []domain.GrantSummary{}
	for rows.Next() {
		var item domain.GrantSummary
		var validFrom, createdAt time.Time
		var validTo pgtype.Timestamptz
		var createdBy pgtype.Text
		if err := rows.Scan(&item.GrantID, &item.UserID, &item.Role, &item.ScopeType, &item.ScopeID, &item.Status, &validFrom, &validTo, &createdAt, &createdBy); err != nil {
			return nil, err
		}
		item.ValidFrom = validFrom.UTC().Format(time.RFC3339)
		item.ValidTo = timePtr(validTo)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.CreatedBy = textPtr(createdBy)
		items = append(items, item)
	}
	return items, rows.Err()
}

func capabilitiesSQL(where string) string {
	return `
SELECT
  wmc.member_capability_id::text,
  wc.capability_id::text,
  wc.capability_code,
  wc.description,
  wmc.scope_type,
  wmc.scope_id::text,
  wmc.status,
  wmc.valid_from,
  wmc.valid_to,
  wmc.created_at
FROM workforce_member_capabilities wmc
JOIN workforce_capabilities wc
  ON wc.tenant_id = wmc.tenant_id
 AND wc.capability_id = wmc.capability_id
` + where
}

func scanCapabilities(rows pgx.Rows) ([]domain.CapabilityAssignment, error) {
	defer rows.Close()
	items := []domain.CapabilityAssignment{}
	for rows.Next() {
		var item domain.CapabilityAssignment
		var validFrom, createdAt time.Time
		var validTo pgtype.Timestamptz
		if err := rows.Scan(&item.MemberCapabilityID, &item.CapabilityID, &item.CapabilityCode, &item.Description, &item.ScopeType, &item.ScopeID, &item.Status, &validFrom, &validTo, &createdAt); err != nil {
			return nil, err
		}
		item.ValidFrom = validFrom.UTC().Format(time.RFC3339)
		item.ValidTo = timePtr(validTo)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func devicesSQL(where string) string {
	return `
SELECT
  d.device_id::text,
  d.workforce_member_id::text,
  d.platform,
  d.app_install_id,
  d.device_public_key_hash,
  d.push_token_hash,
  d.fcm_token,
  d.app_version,
  d.os_version,
  d.status,
  d.last_seen_at,
  d.registered_at,
  d.revoked_at,
  d.metadata,
  d.row_version
FROM workforce_member_devices d
` + where
}

func scanDevices(rows pgx.Rows) ([]domain.DeviceSummary, error) {
	defer rows.Close()
	items := []domain.DeviceSummary{}
	for rows.Next() {
		var item domain.DeviceSummary
		var publicKey, pushToken, fcmToken pgtype.Text
		var lastSeen, registeredAt time.Time
		var revokedAt pgtype.Timestamptz
		var metadata []byte
		if err := rows.Scan(&item.DeviceID, &item.OperatorID, &item.Platform, &item.AppInstallID, &publicKey, &pushToken, &fcmToken, &item.AppVersion, &item.OSVersion, &item.Status, &lastSeen, &registeredAt, &revokedAt, &metadata, &item.RowVersion); err != nil {
			return nil, err
		}
		item.DevicePublicKeyHash = textPtr(publicKey)
		item.PushTokenHash = textPtr(pushToken)
		item.FCMToken = textPtr(fcmToken)
		item.LastSeenAt = lastSeen.UTC().Format(time.RFC3339)
		item.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
		item.RevokedAt = timePtr(revokedAt)
		item.Metadata = decodeMap(metadata)
		items = append(items, item)
	}
	return items, rows.Err()
}

func lookupOperatorUserID(ctx context.Context, tx pgx.Tx, tenantID, operatorID string) (string, error) {
	var userID pgtype.Text
	err := tx.QueryRow(ctx, `
SELECT user_id::text
FROM workforce_members
WHERE tenant_id = $1::uuid
  AND workforce_member_id = $2::uuid`, tenantID, operatorID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !userID.Valid || strings.TrimSpace(userID.String) == "" {
		return "", ports.ErrDenied
	}
	return userID.String, nil
}

func ensureOperatorExists(ctx context.Context, tx pgx.Tx, tenantID, operatorID string) error {
	var exists bool
	err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM workforce_members
  WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid
)`, tenantID, operatorID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ports.ErrNotFound
	}
	return nil
}

func insertAudit(ctx context.Context, tx pgx.Tx, tenantID, actorID, action, resourceType, resourceID string, scopeType *string, metadata map[string]any) error {
	scopeID := ""
	if metadata != nil {
		if v, ok := metadata["scope_id"].(string); ok {
			scopeID = v
		}
	}
	metadataBytes, err := json.Marshal(nonNilMap(metadata))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id, actor_id, actor_type, action, resource_type, resource_id,
  scope_type, scope_id, metadata
) VALUES (
  $1::uuid, $2::uuid, 'human', $3, $4, $5::uuid,
  nullif($6, ''), nullif($7, '')::uuid, $8::jsonb
)`,
		tenantID,
		actorID,
		action,
		resourceType,
		resourceID,
		ptrValue(scopeType),
		scopeID,
		metadataBytes,
	)
	return err
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func mapWriteErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", "23514", "23503":
			return ports.ErrConflict
		}
	}
	return err
}

func mapUpdateErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrConflict
	}
	return mapWriteErr(err)
}

func ptrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func timePtr(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	v := value.Time.UTC().Format(time.RFC3339)
	return &v
}

func decodeMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func contextWithoutCancel(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}
