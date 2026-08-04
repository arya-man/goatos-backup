package notificationbridge

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LocationNameResolver resolves human place names (park/shed) for a small, bounded set of location
// ids in ONE batched query, backed directly by the `locations` table (name column). Every module's
// park/shed id IS a location_id, and no module owns a friendlier display name for it, so this lives
// here as a tiny, dependency-free enrichment used only to compose push copy.
//
// This exists to close the maintainer's confirmed defect: push copy such as "Vaccination due today"
// or "3 vaccination(s) due soon" names nothing a farm worker can act on. A park/shed NAME is the
// minimum specific fact every push in this package must carry.
type LocationNameResolver struct {
	pool *pgxpool.Pool
}

// NewLocationNameResolver builds a resolver over the shared connection pool. A nil pool is accepted
// (e.g. a construction path with no database, such as certain unit tests): ResolveNames then always
// returns an empty map rather than panicking, so name enrichment is optional decoration, never a
// precondition for actually sending a notification.
func NewLocationNameResolver(pool *pgxpool.Pool) *LocationNameResolver {
	return &LocationNameResolver{pool: pool}
}

// ResolveNames returns location_id -> human name for every id in ids that exists for tenantID, via
// ONE query -- never one lookup per id, and never one per notification recipient. Call sites pass the
// small deduped set of park/shed ids a single business event or a single batched sweep page actually
// needs. Errors are swallowed to an empty map: a transient lookup failure degrades the copy (falls
// back to the generic wording) rather than blocking the notification the maintainer needs delivered.
func (r *LocationNameResolver) ResolveNames(ctx context.Context, tenantID string, ids ...string) map[string]string {
	out := map[string]string{}
	if r == nil || r.pool == nil || strings.TrimSpace(tenantID) == "" {
		return out
	}
	seen := make(map[string]bool, len(ids))
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return out
	}
	rows, err := r.pool.Query(ctx, `
SELECT location_id::text, name
FROM locations
WHERE tenant_id = $1::uuid AND location_id = ANY($2::uuid[])`, tenantID, clean)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		out[id] = name
	}
	return out
}

// locationLabelOrFallback renders a resolved name, or a neutral farm-language fallback ("this park")
// when the lookup is unavailable/empty -- never a raw UUID and never a blank segment in the copy.
func locationLabelOrFallback(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "this park"
	}
	return name
}
