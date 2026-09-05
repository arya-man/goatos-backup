// Package postgres implements the Growth Director read-only repository.
package postgres

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
)

type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
	cacheMu      sync.Mutex
	readCache    map[string]readCacheEntry
	readFlight   map[string]*readFlight
}

type readCacheEntry struct {
	expiresAt time.Time
	value     any
}

type readFlight struct {
	done chan struct{}
	val  any
	err  error
}

const growthDirectorReadCacheTTL = 30 * time.Second

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	return &Repository{
		pool:         pool,
		queryTimeout: queryTimeout,
		readCache:    map[string]readCacheEntry{},
		readFlight:   map[string]*readFlight{},
	}
}

func (r *Repository) timeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.queryTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.queryTimeout)
}

func (r *Repository) getCachedRead(key string) (any, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	entry, ok := r.readCache[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(r.readCache, key)
		return nil, false
	}
	return entry.value, true
}

func (r *Repository) setCachedRead(key string, value any) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if len(r.readCache) > 64 {
		r.readCache = map[string]readCacheEntry{}
	}
	r.readCache[key] = readCacheEntry{expiresAt: time.Now().Add(growthDirectorReadCacheTTL), value: value}
}

func (r *Repository) beginReadFlight(key string) (*readFlight, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if flight, ok := r.readFlight[key]; ok {
		return flight, false
	}
	flight := &readFlight{done: make(chan struct{})}
	r.readFlight[key] = flight
	return flight, true
}

func (r *Repository) finishReadFlight(key string, flight *readFlight, val any, err error) {
	r.cacheMu.Lock()
	if current := r.readFlight[key]; current == flight {
		delete(r.readFlight, key)
	}
	flight.val = val
	flight.err = err
	close(flight.done)
	r.cacheMu.Unlock()
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
		return nil, fmt.Errorf("growthdirector: tenant %s has more than %d parks; the park vocabulary is unpaged and would silently omit the rest", tenantID, domain.MaxParks)
	}
	return out, nil
}
