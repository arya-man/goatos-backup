// Package postgres reads the tenant's feed & water removal cutoff from
// feed_water_removal_config (migration 000276). This is the ONLY file that
// names that table: weighing and PC Care receive the value through
// feedwaterremoval/ports.CutoffReader and bind it into their own SQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feedwaterremoval/domain"
	"github.com/vgoats/goatos/backend/internal/feedwaterremoval/ports"
)

// Reader is the Postgres CutoffReader. One primary-key lookup per call; the
// callers are the plan/edit writes and the per-request list reads, each of
// which asks once.
type Reader struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewReader(pool *pgxpool.Pool, queryTimeout time.Duration) *Reader {
	return &Reader{pool: pool, queryTimeout: queryTimeout}
}

// cutoffSQL renders the stored local time as HH24:MI text so the domain parses
// exactly the minute-grained value the farm authored.
const cutoffSQL = `SELECT to_char(cutoff_time, 'HH24:MI')
FROM feed_water_removal_config
WHERE tenant_id = $1::uuid`

func (r *Reader) FeedWaterRemovalCutoff(ctx context.Context, tenantID string) (domain.Cutoff, error) {
	if r.queryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.queryTimeout)
		defer cancel()
	}
	var raw string
	if err := r.pool.QueryRow(ctx, cutoffSQL, tenantID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Cutoff{}, ports.ErrCutoffNotConfigured
		}
		return domain.Cutoff{}, fmt.Errorf("feedwaterremoval: read cutoff: %w", err)
	}
	cutoff, err := domain.ParseCutoff(raw)
	if err != nil {
		return domain.Cutoff{}, fmt.Errorf("feedwaterremoval: stored cutoff %q: %w", raw, err)
	}
	return cutoff, nil
}
