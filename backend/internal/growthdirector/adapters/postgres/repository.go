// Package postgres implements the Growth Director read-only repository.
package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/readcache"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
)

type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
	// cache is the shared platform read cache (readcache), the same instance the weighing
	// repository uses in the API. NewRepository gives a private non-coherent one (30 s, no stale).
	cache *readcache.Cache
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	return &Repository{
		pool:         pool,
		queryTimeout: queryTimeout,
		cache:        readcache.New(readcache.DefaultOptions("growthdirector")),
	}
}

// WithReadCache swaps in the process-wide coherent read cache.
func (r *Repository) WithReadCache(cache *readcache.Cache) *Repository {
	if cache != nil {
		r.cache = cache
	}
	return r
}

// gdReadKey scopes a Growth Director read in the shared read cache by tenant + parks (what a
// weighing, feed-issue or assumptions write evicts by) plus the normalized params.
func gdReadKey(tenantID string, parkIDs []string, params string) readcache.Key {
	return readcache.Key{Tenant: tenantID, Parks: parkIDs, Params: "growthdirector:" + params}
}

func (r *Repository) timeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.queryTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.queryTimeout)
}

func growthDirectorReadKey(parts ...string) string {
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, "|")
}

// ListParks returns all active parks for a tenant, for resolving a tenant-wide
// monitor's "all parks" scope. Unpaged by design with a loud overflow error,
// matching the weighing module's park vocabulary contract.
func (r *Repository) ListParks(ctx context.Context, tenantID string) ([]domain.Park, error) {
	return r.parks(ctx, tenantID, nil)
}

// parks builds the park vocabulary. A nil authorized slice means unrestricted
// (tenant-wide); an empty one would match nothing, so the caller must pass nil
// for the unrestricted arm.
// parks is read on every Growth Director / FCR request; it rides the shared read cache keyed by
// tenant + the authorized park set (locations/configuration writes evict the tenant).
func (r *Repository) parks(ctx context.Context, tenantID string, authorized []string) ([]domain.Park, error) {
	params := "parks:all"
	if authorized != nil {
		params = "parks:scoped"
	}
	parks, err := readcache.Load(ctx, r.cache, gdReadKey(tenantID, authorized, params), func(ctx context.Context) ([]domain.Park, error) {
		return r.parksUncached(ctx, tenantID, authorized)
	})
	return append([]domain.Park(nil), parks...), err
}

func (r *Repository) parksUncached(ctx context.Context, tenantID string, authorized []string) ([]domain.Park, error) {
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
ORDER BY park.display_order, COALESCE(NULLIF(park.location_code, ''), park.name), park.location_id
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
		return nil, fmt.Errorf("growthdirector: tenant %s has more than %d parks; the park vocabulary is unpaged and would silently omit the rest", tenantID, domain.MaxParks)
	}
	return out, nil
}
