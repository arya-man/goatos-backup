package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

type GrantSource struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewGrantSource(pool *pgxpool.Pool, timeout time.Duration) *GrantSource {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &GrantSource{pool: pool, timeout: timeout}
}

func (g *GrantSource) ActiveTenantRoles(ctx context.Context, userID, tenantID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	rows, err := g.pool.Query(ctx, `
SELECT role
FROM user_scope_grants
WHERE user_id = $1
  AND tenant_id = $2
  AND scope_type = 'tenant'
  AND scope_id = $2
  AND status = 'active'
  AND valid_from <= now()
  AND (valid_to IS NULL OR valid_to > now())
ORDER BY role`, userID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

func (g *GrantSource) ActiveTenantGrants(ctx context.Context, userID, tenantID string) ([]permissions.ActiveGrant, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	rows, err := g.pool.Query(ctx, `
SELECT role, scope_type, scope_id::text
FROM user_scope_grants
WHERE user_id = $1
  AND tenant_id = $2
  AND status = 'active'
  AND valid_from <= now()
  AND (valid_to IS NULL OR valid_to > now())
ORDER BY role, scope_type, scope_id`, userID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var grants []permissions.ActiveGrant
	for rows.Next() {
		var grant permissions.ActiveGrant
		if err := rows.Scan(&grant.Role, &grant.ScopeType, &grant.ScopeID); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return grants, nil
}
