// Package postgres holds the board's org-table reads. The board reads NO module table; the
// one thing it looks up itself is who a park's head is, from the grant and roster tables
// every module may read.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// ParkHeadResolver implements ports.ParkHeadResolver over user_scope_grants + workforce_members.
type ParkHeadResolver struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewParkHeadResolver constructs the resolver.
func NewParkHeadResolver(pool *pgxpool.Pool, timeout time.Duration) *ParkHeadResolver {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &ParkHeadResolver{pool: pool, timeout: timeout}
}

// parkHeadSQL resolves the park's head in the order the farm binds them:
//
//  1. an ACTIVE park_head grant scoped to THIS park (the ordinary binding);
//  2. else a tenant-scoped park_head grant whose per-person park scope names this park
//     (the post-cutover binding, person_park_scope), or is tenant-wide;
//
// preferring a head with an active workforce profile, then the earliest grant, so two
// tenant-wide heads resolve the same way every time. A park with no head is
// ErrParkHeadMissing; there is no fallback to another person.
//
// Index: user_scope_grants on (tenant_id, role, status) family; person_park_scope PK;
// workforce_members (tenant_id, user_id) partial on active.
const parkHeadSQL = `
SELECT g.user_id::text, COALESCE(m.display_name, '')
FROM user_scope_grants g
LEFT JOIN workforce_members m
  ON m.tenant_id = g.tenant_id AND m.user_id = g.user_id AND m.status = 'active'
LEFT JOIN person_access pa
  ON pa.tenant_id = m.tenant_id AND pa.workforce_member_id = m.workforce_member_id
WHERE g.tenant_id = $1::uuid
  AND g.role = 'park_head'
  AND g.status = 'active'
  AND (g.valid_to IS NULL OR g.valid_to > now())
  AND (
        (g.scope_type = 'park' AND g.scope_id = $2::uuid)
     OR (g.scope_type = 'tenant' AND (
           pa.workforce_member_id IS NULL
        OR pa.scope_mode = 'tenant'
        OR EXISTS (SELECT 1 FROM person_park_scope ps
                   WHERE ps.tenant_id = pa.tenant_id AND ps.workforce_member_id = pa.workforce_member_id
                     AND ps.park_id = $2::uuid)))
  )
ORDER BY (g.scope_type = 'park') DESC, (m.workforce_member_id IS NULL) ASC, g.valid_from ASC NULLS LAST, g.user_id ASC
LIMIT 1`

// ParkHead implements ports.ParkHeadResolver.
func (r *ParkHeadResolver) ParkHead(ctx context.Context, tenantID, parkID string) (ports.ParkHead, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var head ports.ParkHead
	err := r.pool.QueryRow(ctx, parkHeadSQL, tenantID, parkID).Scan(&head.UserID, &head.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ParkHead{}, ports.ErrParkHeadMissing
	}
	if err != nil {
		return ports.ParkHead{}, fmt.Errorf("workboard park head: %w", err)
	}
	return head, nil
}
