package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/readcache"
)

// commitIssueAndEvict is commitAndInvalidateReadCache for a feed-issue mutation (issue, amend,
// lock). Growth Director's FCR tab reads feed_direction_issues / _issue_rows through the shared
// platform read cache, so the mutation also publishes a tenant+park scoped eviction INSIDE the
// transaction; pg_notify is delivered on commit only, and every API instance (this one included)
// applies it through readcache.Listener, and this instance evicts at once after commit. This
// repository's own feed-analytics cache is unchanged.
func (r *Repository) commitIssueAndEvict(ctx context.Context, tx pgx.Tx, tenantID, parkID string) error {
	if err := readcache.NotifyTx(ctx, tx, tenantID, parkID); err != nil {
		return err
	}
	if err := r.commitAndInvalidateReadCache(ctx, tx); err != nil {
		return err
	}
	// Read-your-writes on THIS instance: drop the shared analytics entries now, not when this
	// instance's own listener hears the NOTIFY (~50 ms later, or never while it is down).
	if r.readInvalidator != nil {
		r.readInvalidator.Evict(ctx, tenantID, parkID)
	}
	return nil
}

// WithReadCacheInvalidator wires the process-wide analytics read cache (Growth Director FCR).
func (r *Repository) WithReadCacheInvalidator(inv readcache.Invalidator) *Repository {
	r.readInvalidator = inv
	return r
}
