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
// applies it through readcache.Listener. This repository's own feed-analytics cache is unchanged.
func (r *Repository) commitIssueAndEvict(ctx context.Context, tx pgx.Tx, tenantID, parkID string) error {
	if err := readcache.NotifyTx(ctx, tx, tenantID, parkID); err != nil {
		return err
	}
	return r.commitAndInvalidateReadCache(ctx, tx)
}
