package postgres

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// WeighingParks is the park VOCABULARY the oversight surfaces render as filter chips.
//
// Deliberately NOT derived from the loaded task rows: a vocabulary computed from the data it
// filters loses a park the moment that park's tasks fall off the current keyset page, and the
// chip then disappears with no way to get the rows back.
//
// parkIDs is the caller's capability-scoped set and is part of the QUERY. A nil/empty slice is
// the UNRESTRICTED arm (tenant-wide authority or an internal caller), matching the contract
// ListLeadershipSheds already declares on this repository -- the service is the only layer that
// can tell "authorized everywhere" from "authorized nowhere", and it resolves that before here.
//
// The read is UNPAGED by design -- a chip row that pages cannot offer the parks it has not
// reached -- but it is not unbounded: it asks for domain.MaxPlannerParks+1 rows and FAILS if the
// extra row comes back. Truncating silently at the cap would reintroduce the very defect this
// endpoint exists to remove, a filter vocabulary that quietly omits parks, and it would do it at
// the one scale nobody tests. A loud error names the cap and the tenant; a short chip row names
// nothing. Parks are physical farms, so the cap is orders of magnitude above reality and the
// error is a "this assumption died" signal, not an operating condition.
//
// projection-review: membership=active park locations of one tenant, optionally narrowed to the
// caller's authorized ids; group_key=park location_id; join_cardinality=NO joins, a single-table
// read of locations so no row can be multiplied; pagination=NONE by design (see above), bounded
// instead by a domain.MaxPlannerParks+1 overflow probe that errors rather than truncates;
// scope=tenant_id=$1 plus the optional authorized-id array $2.
//
// Grain proof:
//
//	producer locations unique: (location_id) PK, tenant-scoped (tenant_id, location_id)
//	consumer chip row  match:  the same (tenant_id, location_id)
//	1:1. No aggregate, no ratio, nothing to fan out.
//
// Served by locations_tenant_type_status_order_idx
// (tenant_id, location_type, status, display_order, name, location_id): the equality columns
// first, then exactly this ORDER BY, so the LIMIT stops the index walk.
func (r *Repository) WeighingParks(ctx context.Context, tenantID string, parkIDs []string) ([]domain.WeighingPark, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	// A nil array (not an empty one) is what makes the $2 arm mean "unrestricted": `= ANY` over
	// an EMPTY array matches nothing, so passing len(parkIDs)==0 through as an empty slice would
	// turn the unrestricted read into a silently empty one.
	var authorized []string
	if len(parkIDs) > 0 {
		authorized = parkIDs
	}
	rows, err := r.pool.Query(ctx, `
SELECT park.location_id::text, park.name
FROM locations park
WHERE park.tenant_id=$1::uuid
  AND park.location_type='park'
  AND park.status='active'
  AND park.retired_at IS NULL
  AND ($2::uuid[] IS NULL OR park.location_id = ANY($2::uuid[]))
ORDER BY park.display_order, park.name, park.location_id
LIMIT $3`, tenantID, authorized, domain.MaxPlannerParks+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.WeighingPark{}
	for rows.Next() {
		var park domain.WeighingPark
		if err := rows.Scan(&park.ParkID, &park.Name); err != nil {
			return nil, err
		}
		out = append(out, park)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > domain.MaxPlannerParks {
		return nil, fmt.Errorf("weighing: tenant %s has more than %d weighing parks; the park chip row is unpaged and would silently omit the rest", tenantID, domain.MaxPlannerParks)
	}
	return out, nil
}

// ListParks returns all active parks for a tenant.
func (r *Repository) ListParks(ctx context.Context, tenantID string) ([]domain.WeighingPark, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT park.location_id::text, park.name
FROM locations park
WHERE park.tenant_id=$1::uuid
  AND park.location_type='park'
  AND park.status='active'
  AND park.retired_at IS NULL
ORDER BY park.display_order, park.name, park.location_id
LIMIT $2`, tenantID, domain.MaxPlannerParks+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.WeighingPark{}
	for rows.Next() {
		var park domain.WeighingPark
		if err := rows.Scan(&park.ParkID, &park.Name); err != nil {
			return nil, err
		}
		out = append(out, park)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > domain.MaxPlannerParks {
		return nil, fmt.Errorf("weighing: tenant %s has more than %d weighing parks; the park chip row is unpaged and would silently omit the rest", tenantID, domain.MaxPlannerParks)
	}
	return out, nil
}
