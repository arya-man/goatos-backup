package postgres

import (
	"context"
	"encoding/json"
	"fmt"
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

type pendingEmailGrantRow struct {
	pendingGrantID string
	role           string
	scopeType      string
	scopeID        string
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
SELECT pending_grant_id::text, role, scope_type, scope_id::text
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

	var pending []pendingEmailGrantRow
	for rows.Next() {
		var row pendingEmailGrantRow
		if err := rows.Scan(&row.pendingGrantID, &row.role, &row.scopeType, &row.scopeID); err != nil {
			return permissions.PendingEmailGrantResult{}, err
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
		} else {
			result.ExistingGrants = append(result.ExistingGrants, grant)
		}
		if err := c.ensureWorkforceMember(ctx, tx, claim, normalizedEmail, row.role); err != nil {
			return permissions.PendingEmailGrantResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return permissions.PendingEmailGrantResult{}, err
	}
	return result, nil
}

func (c *PendingEmailGrantClaimer) ensureUserGrant(ctx context.Context, tx pgx.Tx, claim permissions.PendingEmailGrantClaim, normalizedEmail string, row pendingEmailGrantRow) (permissions.ClaimedEmailGrant, bool, error) {
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

func (c *PendingEmailGrantClaimer) ensureWorkforceMember(ctx context.Context, tx pgx.Tx, claim permissions.PendingEmailGrantClaim, normalizedEmail string, role string) error {
	displayCode := "auth:" + claim.UserID
	displayName := pendingEmailGrantDisplayName(normalizedEmail, role)
	roleHint := pendingEmailGrantRoleHint(role)
	grade := pendingEmailGrantDesignationGrade(role)

	_, err := tx.Exec(ctx, `
INSERT INTO workforce_members (
  tenant_id,
  user_id,
  display_code,
  display_name,
  status,
  primary_role_hint,
  hr_designation_grade,
  metadata
)
SELECT $1, $2, $3, $4, 'active', $5, $6, jsonb_build_object(
  'source', 'auth_pending_email_grant',
  'normalized_email', $7::text,
  'role', $8::text
)
WHERE NOT EXISTS (
  SELECT 1
  FROM workforce_members
  WHERE tenant_id = $1
    AND user_id = $2
    AND status = 'active'
)`, claim.TenantID, claim.UserID, displayCode, displayName, roleHint, grade, normalizedEmail, role)
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "workforce_members_code_unique_idx") {
		return fmt.Errorf("active workforce profile missing for auth user %s, but display_code %q is already used in tenant %s: %w", claim.UserID, displayCode, claim.TenantID, err)
	}
	return err
}

func pendingEmailGrantDisplayName(normalizedEmail string, role string) string {
	if role == permissions.RoleCEOInternal {
		return "CEO/CXO"
	}
	if normalizedEmail != "" {
		return "Auth user"
	}
	return "Granted user"
}

func pendingEmailGrantDesignationGrade(role string) any {
	if role == permissions.RoleCEOInternal {
		return "cxo"
	}
	if tier, _, ok := permissions.ParseRoleKey(role); ok {
		switch tier {
		case permissions.TierDirector:
			return "director"
		case permissions.TierManager:
			return "manager"
		case permissions.TierAssistantManager:
			return "assistant_manager"
		}
	}
	return nil
}

func pendingEmailGrantRoleHint(role string) string {
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
		if tier, _, ok := permissions.ParseRoleKey(role); ok {
			if tier == permissions.TierAssistantManager {
				return "operator"
			}
			return "supervisor"
		}
		return "other"
	}
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
