package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the read-only Postgres adapter for the pen growth-vs-feed
// comparison. It owns no table and writes nothing: every method here is a SELECT.
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

// ListParkIDs enumerates the tenant's parks, for a caller whose monitor capability
// is tenant-wide rather than granted park by park.
func (r *Repository) ListParkIDs(ctx context.Context, tenantID string) ([]string, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx, `
SELECT location_id
FROM locations
WHERE tenant_id = $1::uuid AND location_type = 'park'
ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
