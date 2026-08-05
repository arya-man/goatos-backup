package postgres

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// ResolveApprovalNames implements ports.ApprovalNameResolver.
//
// THREE queries for the whole page, never one per row. The queue page is capped at 20 rows and
// each row can name a raiser, a destination shed, and (for a death) the subject animal's location,
// so a per-row lookup would turn one phone screen into dozens of serial reads -- the N+1 fan-out
// banned by docs/decisions/scale-anti-patterns.go. Every predicate keeps the indexed uuid column
// BARE and casts the bound array instead (`location_id = ANY($2::uuid[])`), because a column-side
// `::text` cast would disable the index.
//
// All three id sets are bounded by the page size, so the arrays are small by construction.
func (r *Repository) ResolveApprovalNames(ctx context.Context, tenantID string, locationIDs, userIDs, goatIDs []string) (ports.ApprovalDisplayNames, error) {
	out := ports.ApprovalDisplayNames{
		Locations:       map[string]string{},
		People:          map[string]string{},
		AnimalLocations: map[string]string{},
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

	if goats := dedupeNonBlank(goatIDs); len(goats) > 0 {
		// A death is terminal and ExitGoat never touches goats.shed_id/park_id, so the animal's
		// CURRENT row is still where it was at time of death -- read-time resolution, no payload
		// snapshot needed. goat_shed_partitions is PK (tenant_id, goat_id), a 1:{0,1} join, so this
		// stays a single set-based read with no fan-out.
		rows, err := r.pool.Query(ctx, `
SELECT g.goat_id::text,
       COALESCE(NULLIF(park.name, ''), park.location_code, '') AS park_name,
       COALESCE(NULLIF(shed.name, ''), shed.location_code, '') AS shed_name,
       gsp.partition_label
FROM goats g
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = g.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = g.shed_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1::uuid AND gsp.goat_id = g.goat_id
WHERE g.tenant_id = $1::uuid AND g.goat_id = ANY($2::uuid[])`, tenantID, goats)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var goatID, parkName, shedName string
			var partitionLabel *string
			if err := rows.Scan(&goatID, &parkName, &shedName, &partitionLabel); err != nil {
				rows.Close()
				return out, err
			}
			label := ""
			if partitionLabel != nil {
				label = *partitionLabel
			}
			shedDisplay := oploc.OperationalLocation{ShedName: shedName, PartitionLabel: label}.Display()
			out.AnimalLocations[goatID] = joinNonBlank(parkName, shedDisplay)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, err
		}
	}

	return out, nil
}

// joinNonBlank composes "park, shed[+partition]", dropping either side that could not be
// resolved rather than printing an empty half of the clause (e.g. "" ⇒ "Castro 2", never
// ", Castro 2").
func joinNonBlank(park, shedDisplay string) string {
	park = strings.TrimSpace(park)
	shedDisplay = strings.TrimSpace(shedDisplay)
	switch {
	case park != "" && shedDisplay != "":
		return park + ", " + shedDisplay
	case shedDisplay != "":
		return shedDisplay
	default:
		return park
	}
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
