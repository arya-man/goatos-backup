package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// ModuleOwnershipSource resolves a principal's owned product modules from their
// HR department: workforce_members.department_id -> department_module_grants
// (active + in valid window). It is the single shared read behind visible-module
// filtering and the nav-chrome threshold on both /app/bootstrap and
// /admin-web/bootstrap. See docs/decisions/user-module-ownership-and-nav-chrome.md.
type ModuleOwnershipSource struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewModuleOwnershipSource(pool *pgxpool.Pool, timeout time.Duration) *ModuleOwnershipSource {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &ModuleOwnershipSource{pool: pool, timeout: timeout}
}

// ListActiveModuleGrantsForActor returns the active modules owned by the actor's
// department. It is empty when the actor has no active workforce_member, the
// member has no department, or the department owns nothing active — the caller
// treats an empty set as "no owned modules" (single/zero-module nav chrome).
// Results are ordered for stable bootstrap output and cache keys. The read is
// tenant-scoped and keyed by the IdP subject (workforce_members.user_id), the
// same actor identity the bootstraps resolve.
func (s *ModuleOwnershipSource) ListActiveModuleGrantsForActor(ctx context.Context, userID, tenantID string) ([]permissions.OwnedModule, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
SELECT g.vertical, g.module
FROM workforce_members wm
JOIN department_module_grants g
  ON g.tenant_id = wm.tenant_id
 AND g.department_id = wm.department_id
WHERE wm.tenant_id = $2
  AND wm.user_id = $1
  AND wm.status = 'active'
  AND wm.department_id IS NOT NULL
  AND g.status = 'active'
  AND g.valid_from <= now()
  AND (g.valid_to IS NULL OR g.valid_to > now())
ORDER BY g.vertical, g.module`, userID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modules []permissions.OwnedModule
	for rows.Next() {
		var m permissions.OwnedModule
		if err := rows.Scan(&m.Vertical, &m.Module); err != nil {
			return nil, err
		}
		modules = append(modules, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return modules, nil
}
