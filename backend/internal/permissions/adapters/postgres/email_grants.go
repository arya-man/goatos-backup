package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
)

type PendingEmailGrantClaimer struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewPendingEmailGrantClaimer(pool *pgxpool.Pool, timeout time.Duration) *PendingEmailGrantClaimer {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &PendingEmailGrantClaimer{pool: pool, timeout: timeout}
}

func (c *PendingEmailGrantClaimer) ClaimPendingEmailGrant(ctx context.Context, claim permissions.PendingEmailGrantClaim) (permissions.PendingEmailGrantResult, error) {
	normalizedEmail := authallow.NormalizeEmail(claim.Email)
	if !strings.Contains(normalizedEmail, "@") || strings.TrimSpace(claim.TenantID) == "" || strings.TrimSpace(claim.UserID) == "" {
		return permissions.PendingEmailGrantResult{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return permissions.PendingEmailGrantResult{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	rows, err := tx.Query(ctx, `
SELECT pending_grant_id::text, role, scope_type, scope_id::text, department_code
FROM auth_pending_email_grants
WHERE tenant_id = $1
  AND normalized_email = $2
  AND status = 'active'
  AND valid_from <= now()
  AND (valid_to IS NULL OR valid_to > now())
ORDER BY created_at ASC, pending_grant_id ASC
FOR UPDATE`, claim.TenantID, normalizedEmail)
	if err != nil {
		return permissions.PendingEmailGrantResult{}, err
	}
	defer rows.Close()

	type pendingRow struct {
		pendingGrantID string
		role           string
		scopeType      string
		scopeID        string
	}
	var pending []pendingRow
	// departmentCode is the HR department the approved email belongs to (nullable,
	// non-PII code). It drives one-time member provisioning below; the first
	// non-empty value wins if multiple grants disagree.
	var departmentCode string
	for rows.Next() {
		var row pendingRow
		var deptCode *string
		if err := rows.Scan(&row.pendingGrantID, &row.role, &row.scopeType, &row.scopeID, &deptCode); err != nil {
			return permissions.PendingEmailGrantResult{}, err
		}
		if departmentCode == "" && deptCode != nil && strings.TrimSpace(*deptCode) != "" {
			departmentCode = strings.TrimSpace(*deptCode)
		}
		pending = append(pending, row)
	}
	if err := rows.Err(); err != nil {
		return permissions.PendingEmailGrantResult{}, err
	}
	if len(pending) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return permissions.PendingEmailGrantResult{}, err
		}
		return permissions.PendingEmailGrantResult{}, nil
	}

	result := permissions.PendingEmailGrantResult{Matched: true, PendingGrantIDs: make([]string, 0, len(pending))}
	for _, row := range pending {
		result.PendingGrantIDs = append(result.PendingGrantIDs, row.pendingGrantID)
		grant, inserted, err := c.ensureUserGrant(ctx, tx, claim, normalizedEmail, row)
		if err != nil {
			return permissions.PendingEmailGrantResult{}, err
		}
		if inserted {
			result.InsertedGrants = append(result.InsertedGrants, grant)
			continue
		}
		result.ExistingGrants = append(result.ExistingGrants, grant)
	}
	// Email-sign-in department provisioning: make the member->department->grants
	// ownership chain real for this actor so department-driven nav works on both
	// bootstraps. Opt-in: only when the approved email grant carries a
	// department_code. Idempotent and non-destructive (never overrides an
	// HR-assigned department).
	if departmentCode != "" {
		primaryRole := ""
		if len(pending) > 0 {
			primaryRole = pending[0].role
		}
		if err := c.provisionDepartmentMember(ctx, tx, claim, normalizedEmail, primaryRole, departmentCode); err != nil {
			return permissions.PendingEmailGrantResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return permissions.PendingEmailGrantResult{}, err
	}
	return result, nil
}

// provisionDepartmentMember upserts a workforce_member for the claiming actor
// into the given HR department. It resolves the department by (tenant, code),
// then: if an active member for the actor exists, it only fills a NULL
// department_id (never overrides an HR assignment); otherwise it inserts a new
// member keyed by the actor's user_id. No-op if the department code is unknown.
// Runs inside the claim transaction so provisioning and the grant claim commit
// atomically.
func (c *PendingEmailGrantClaimer) provisionDepartmentMember(ctx context.Context, tx pgx.Tx, claim permissions.PendingEmailGrantClaim, normalizedEmail, primaryRole, departmentCode string) error {
	var departmentID string
	err := tx.QueryRow(ctx, `
SELECT department_id::text
FROM departments
WHERE tenant_id = $1 AND code = $2 AND status = 'active'`, claim.TenantID, departmentCode).Scan(&departmentID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	var memberID string
	var existingDepartment *string
	err = tx.QueryRow(ctx, `
SELECT workforce_member_id::text, department_id::text
FROM workforce_members
WHERE tenant_id = $1 AND user_id = $2 AND status = 'active'
ORDER BY created_at DESC, workforce_member_id DESC
LIMIT 1`, claim.TenantID, claim.UserID).Scan(&memberID, &existingDepartment)
	if err == nil {
		if existingDepartment != nil {
			return nil // already in a department; do not override HR assignment
		}
		_, err = tx.Exec(ctx, `
UPDATE workforce_members
SET department_id = $3, updated_at = now(), row_version = row_version + 1
WHERE workforce_member_id = $1 AND tenant_id = $2 AND department_id IS NULL`, memberID, claim.TenantID, departmentID)
		return err
	}
	if err != pgx.ErrNoRows {
		return err
	}

	// No existing member: provision one keyed by user_id. display_code is a stable
	// non-colliding auth code; display_name carries the verified email (runtime
	// operational data, never committed).
	_, err = tx.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status, primary_role_hint, department_id)
VALUES ($1, $2, $3, $4, 'active', $5, $6)`,
		claim.TenantID, claim.UserID, "auth:"+claim.UserID, normalizedEmail, memberRoleHint(primaryRole), departmentID)
	return err
}

// memberRoleHint maps an RBAC grant role to a workforce_members.primary_role_hint
// value (the allowed set differs; pc_director/ceo_internal fold to admin).
func memberRoleHint(role string) string {
	switch role {
	case permissions.RoleOperator:
		return "operator"
	case permissions.RoleParkHead:
		return "park_head"
	case permissions.RoleVerifier:
		return "verifier"
	case permissions.RoleAdmin, permissions.RoleCEOInternal, permissions.RolePCDirector:
		return "admin"
	default:
		return "other"
	}
}

func (c *PendingEmailGrantClaimer) ensureUserGrant(ctx context.Context, tx pgx.Tx, claim permissions.PendingEmailGrantClaim, normalizedEmail string, row struct {
	pendingGrantID string
	role           string
	scopeType      string
	scopeID        string
}) (permissions.ClaimedEmailGrant, bool, error) {
	claimed := permissions.ClaimedEmailGrant{
		PendingGrantID: row.pendingGrantID,
		Role:           row.role,
		ScopeType:      row.scopeType,
		ScopeID:        row.scopeID,
	}

	err := tx.QueryRow(ctx, `
SELECT grant_id::text
FROM user_scope_grants
WHERE tenant_id = $1
  AND user_id = $2
  AND role = $3
  AND scope_type = $4
  AND scope_id = $5
  AND status = 'active'
  AND valid_from <= now()
  AND (valid_to IS NULL OR valid_to > now())
ORDER BY valid_from DESC, grant_id DESC
LIMIT 1`, claim.TenantID, claim.UserID, row.role, row.scopeType, row.scopeID).Scan(&claimed.GrantID)
	if err == nil {
		return claimed, false, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return permissions.ClaimedEmailGrant{}, false, err
	}

	if err := tx.QueryRow(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1, $2, $3, $4, $5, 'active', now())
RETURNING grant_id::text`, claim.TenantID, claim.UserID, row.role, row.scopeType, row.scopeID).Scan(&claimed.GrantID); err != nil {
		return permissions.ClaimedEmailGrant{}, false, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE auth_pending_email_grants
SET last_claimed_user_id = $2,
    last_claimed_external_subject = NULLIF($3, ''),
    last_claimed_at = now(),
    claim_count = claim_count + 1,
    updated_at = now()
WHERE pending_grant_id = $1`, row.pendingGrantID, claim.UserID, claim.ExternalSubject); err != nil {
		return permissions.ClaimedEmailGrant{}, false, err
	}
	if err := c.recordClaimAudit(ctx, tx, claim, normalizedEmail, claimed); err != nil {
		return permissions.ClaimedEmailGrant{}, false, err
	}
	return claimed, true, nil
}

func (c *PendingEmailGrantClaimer) recordClaimAudit(ctx context.Context, tx pgx.Tx, claim permissions.PendingEmailGrantClaim, normalizedEmail string, grant permissions.ClaimedEmailGrant) error {
	metadata := map[string]any{
		"email":            normalizedEmail,
		"external_subject": claim.ExternalSubject,
		"issuer":           claim.Issuer,
		"source":           claim.Source,
		"grant_id":         grant.GrantID,
		"role":             grant.Role,
		"scope_type":       grant.ScopeType,
		"scope_id":         grant.ScopeID,
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id,
  actor_id,
  actor_type,
  action,
  resource_type,
  resource_id,
  scope_type,
  scope_id,
  metadata,
  trace_id
) VALUES (
  $1,
  $2,
  'user',
  'auth.pending_email_grant_claimed',
  'auth_pending_email_grant',
  $3,
  $4,
  $5,
  $6::jsonb,
  NULLIF($7, '')
)`,
		claim.TenantID,
		claim.UserID,
		grant.PendingGrantID,
		grant.ScopeType,
		grant.ScopeID,
		metadataBytes,
		claim.TraceID,
	)
	return err
}
