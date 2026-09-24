package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/readcache"
)

// HERD WRITES AND THE ANALYTICS READ CACHE.
//
// The Weights / ADG / demographics reads and the Growth Director (FCR, feed-by-weight-band) join
// goats, goat_identifiers and goat_shed_partitions by TAG, not by the goat's current park: an
// animal weighed in one park and moved to another still colours the first park's report. So a
// herd write evicts the whole TENANT's cached reads -- never just the goat's park -- by
// publishing readcache.NotifyTx inside its transaction and evicting this process after commit.

// WithReadCacheInvalidator wires the process-wide read cache so this repository's own next read
// after a write is fresh without waiting for the NOTIFY round trip.
func (r *Repository) WithReadCacheInvalidator(inv readcache.Invalidator) *Repository {
	r.readInvalidator = inv
	return r
}

// commitHerdWrite commits a herd write and evicts the tenant's cached analytics reads.
func (r *Repository) commitHerdWrite(ctx context.Context, tx pgx.Tx, tenantID string) error {
	return readcache.CommitAndEvict(ctx, tx, r.readInvalidator, tenantID)
}

// notifyHerdWriteTx queues the tenant eviction on a transaction the CALLER commits (a deferred
// commit); every instance, this one included, applies it from the NOTIFY on commit.
func notifyHerdWriteTx(ctx context.Context, tx pgx.Tx, tenantID string) error {
	return readcache.NotifyTx(ctx, tx, tenantID)
}
