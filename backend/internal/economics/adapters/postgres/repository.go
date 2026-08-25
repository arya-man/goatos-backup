// Package postgres implements the Business Economics read-only repository.
package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/economics/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	return &Repository{pool: pool, queryTimeout: queryTimeout}
}

func (r *Repository) timeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.queryTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.queryTimeout)
}

// ListParks returns all active parks for a tenant. Unpaged by design with a
// loud overflow error, matching the weighing module's park vocabulary contract.
func (r *Repository) ListParks(ctx context.Context, tenantID string) ([]domain.Park, error) {
	return r.parks(ctx, tenantID, nil)
}

func (r *Repository) parks(ctx context.Context, tenantID string, authorized []string) ([]domain.Park, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT park.location_id::text, park.name
FROM locations park
WHERE park.tenant_id = $1::uuid
  AND park.location_type = 'park'
  AND park.status = 'active'
  AND park.retired_at IS NULL
  AND ($2::uuid[] IS NULL OR park.location_id = ANY($2::uuid[]))
ORDER BY park.display_order, park.name, park.location_id
LIMIT $3`, tenantID, authorized, domain.MaxParks+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Park{}
	for rows.Next() {
		var park domain.Park
		if err := rows.Scan(&park.ParkID, &park.Name); err != nil {
			return nil, err
		}
		out = append(out, park)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > domain.MaxParks {
		return nil, fmt.Errorf("economics: tenant %s has more than %d parks; the park vocabulary is unpaged and would silently omit the rest", tenantID, domain.MaxParks)
	}
	return out, nil
}

// operationalLabel composes the park-prefixed shed + pen display, routed
// through the canonical oploc composition per the operational-location rule.
func operationalLabel(parkName, shedLabel, partitionLabel string) string {
	label := strings.TrimSpace(shedLabel)
	partition := strings.TrimSpace(partitionLabel)
	if partition != "" && !strings.HasSuffix(label, partition) {
		label = (oploc.OperationalLocation{ShedName: label, PartitionLabel: partition}).Display()
	}
	if parkName = strings.TrimSpace(parkName); parkName != "" {
		return parkName + " · " + label
	}
	return label
}
