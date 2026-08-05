package postgres

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

// ResolveApprovalNames implements ports.ApprovalNameResolver.
//
// TWO queries for the whole page, never one per row. The queue page is capped at 20 rows and each
// row can name a raiser and a destination shed, so a per-row lookup would turn one phone screen
// into up to 41 serial reads -- the N+1 fan-out banned by docs/decisions/scale-anti-patterns.go.
// Both predicates keep the indexed uuid column BARE and cast the bound array instead
// (`location_id = ANY($2::uuid[])`), because a column-side `::text` cast would disable the index.
//
// Both id sets are bounded by the page size, so the arrays are small by construction.
func (r *Repository) ResolveApprovalNames(ctx context.Context, tenantID string, locationIDs, userIDs []string) (ports.ApprovalDisplayNames, error) {
	out := ports.ApprovalDisplayNames{
		Locations: map[string]string{},
		People:    map[string]string{},
	}
	if r == nil || r.pool == nil || strings.TrimSpace(tenantID) == "" {
		return out, nil
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if locations := dedupeNonBlank(locationIDs); len(locations) > 0 {
		rows, err := r.pool.Query(ctx, `
SELECT location_id::text, name
FROM locations
WHERE tenant_id = $1::uuid AND location_id = ANY($2::uuid[])`, tenantID, locations)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return out, err
			}
			out.Locations[id] = name
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, err
		}
	}

	if users := dedupeNonBlank(userIDs); len(users) > 0 {
		// workforce_members.user_id is the link from an authenticated principal to the named person
		// on the roster; display_name is NOT NULL and non-blank by check constraint, so a matched
		// row always yields something renderable.
		//
		// No status filter: a request raised by someone who has since gone inactive must still show
		// WHO raised it. Hiding the name of a departed operator would leave the approver with a
		// blank where the accountable person should be.
		rows, err := r.pool.Query(ctx, `
SELECT user_id::text, display_name
FROM workforce_members
WHERE tenant_id = $1::uuid AND user_id = ANY($2::uuid[])`, tenantID, users)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return out, err
			}
			out.People[id] = name
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, err
		}
	}

	return out, nil
}

// dedupeNonBlank trims, drops blanks, and dedupes while preserving order. Callers pass ids
// harvested per row, so the same shed or the same raiser routinely appears many times in one page.
func dedupeNonBlank(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
