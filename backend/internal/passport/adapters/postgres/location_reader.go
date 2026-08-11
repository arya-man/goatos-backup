// Package postgres provides passport's read-only Postgres adapters.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// LocationReader resolves a goat's current ground location for the passport read model. It reads
// only canonical org/herd tables (goats, locations, goat_shed_partitions) -- the same tables
// vaccination and counts already join for the identical purpose -- and owns no tables of its own.
type LocationReader struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewLocationReader constructs the reader.
func NewLocationReader(pool *pgxpool.Pool, timeout time.Duration) *LocationReader {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &LocationReader{pool: pool, timeout: timeout}
}

// GoatLocation returns the goat's current park + physical shed + optional partition. found is false
// when the goat does not exist or currently has no shed assignment; callers must leave the
// passport's location fields empty in that case rather than inventing a location.
func (r *LocationReader) GoatLocation(ctx context.Context, tenantID, goatID string) (oploc.OperationalLocation, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var loc oploc.OperationalLocation
	var parkID, parkName, shedID, shedName, partitionLabel *string // operational-location:ignore: owner=ravi issue=partition-sweep-2026-08-06 scope=single-goat-lookup-keyed-by-goat_id-park-and-shed-joined-on-location_id-names-are-display-outputs-not-keys expiry=2027-08-06
	err := r.pool.QueryRow(ctx, `
SELECT g.park_id::text, park.name, g.shed_id::text, shed.name, gsp.partition_label -- operational-location:ignore: owner=ravi issue=partition-sweep-2026-08-06 scope=selects-both-ids-and-names-row-identified-by-goat_id-names-are-display-only expiry=2027-08-06
FROM goats g
LEFT JOIN locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
LEFT JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = COALESCE(g.shed_group_id, g.shed_id)
WHERE g.tenant_id = $1::uuid AND g.goat_id = $2::uuid`,
		tenantID, goatID).Scan(&parkID, &parkName, &shedID, &shedName, &partitionLabel) // operational-location:ignore: owner=ravi issue=partition-sweep-2026-08-06 scope=scan-destinations-for-the-single-goat-id-keyed-query-above-names-are-display-only expiry=2027-08-06
	if errors.Is(err, pgx.ErrNoRows) {
		return loc, false, nil
	}
	if err != nil {
		return loc, false, err
	}
	if shedID == nil || *shedID == "" {
		// Goat exists but has no current shed assignment; nothing to resolve.
		return loc, false, nil
	}
	loc = oploc.OperationalLocation{
		ParkID:         valueOrEmpty(parkID),
		ParkName:       valueOrEmpty(parkName),
		ShedID:         valueOrEmpty(shedID),
		ShedName:       valueOrEmpty(shedName),
		PartitionLabel: valueOrEmpty(partitionLabel),
	}
	return loc, true, nil
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
