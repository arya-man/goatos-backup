package postgres

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

type GrantSource struct {
	pool     *pgxpool.Pool
	timeout  time.Duration
	cacheMu  sync.Mutex
	grants   map[string]cachedGrants
	cacheTTL time.Duration
}

type cachedGrants struct {
	expiresAt time.Time
	grants    []permissions.ActiveGrant
}

func NewGrantSource(pool *pgxpool.Pool, timeout time.Duration) *GrantSource {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &GrantSource{pool: pool, timeout: timeout, grants: map[string]cachedGrants{}, cacheTTL: 30 * time.Second}
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
	if cached, ok := g.cachedTenantGrants(userID, tenantID); ok {
		return cached, nil
	}
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
	g.storeTenantGrants(userID, tenantID, grants)
	return grants, nil
}

func (g *GrantSource) cachedTenantGrants(userID, tenantID string) ([]permissions.ActiveGrant, bool) {
	key := userID + "|" + tenantID
	now := time.Now()
	g.cacheMu.Lock()
	defer g.cacheMu.Unlock()
	entry, ok := g.grants[key]
	if !ok || now.After(entry.expiresAt) {
		if ok {
			delete(g.grants, key)
		}
		return nil, false
	}
	return cloneActiveGrants(entry.grants), true
}

func (g *GrantSource) storeTenantGrants(userID, tenantID string, grants []permissions.ActiveGrant) {
	key := userID + "|" + tenantID
	g.cacheMu.Lock()
	defer g.cacheMu.Unlock()
	if len(g.grants) > 512 {
		g.grants = map[string]cachedGrants{}
	}
	g.grants[key] = cachedGrants{
		expiresAt: time.Now().Add(g.cacheTTL),
		grants:    cloneActiveGrants(grants),
	}
}

func cloneActiveGrants(grants []permissions.ActiveGrant) []permissions.ActiveGrant {
	if len(grants) == 0 {
		return nil
	}
	out := make([]permissions.ActiveGrant, len(grants))
	copy(out, grants)
	return out
}
