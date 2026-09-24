package readcache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// NotifyChannel carries committed-write evictions between API instances. pg_notify inside the
// writer's transaction is delivered only on COMMIT (and dropped on rollback), so a sibling
// instance can never evict for -- or miss -- a write that did not happen.
const NotifyChannel = "goatos_read_cache_evict"

const notifySQL = `SELECT pg_notify($1, json_build_object('tenant_id', $2::text, 'park_ids', $3::text[])::text)`

// Execer is the subset of pgx.Tx the notify needs.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

var _ Execer = (pgx.Tx)(nil)

type evictPayload struct {
	TenantID string   `json:"tenant_id"`
	ParkIDs  []string `json:"park_ids,omitempty"`
	// Caches, when set, names the caches (Options.Name) the write affects; every other cache
	// keeps its entries. Empty means every cache, which is what NotifyTx sends. The vaccination
	// write triggers (migration 000411) send ["vaccination"] so a dose record does not evict the
	// weighing/growth analytics entries.
	Caches []string `json:"caches,omitempty"`
}

// affects reports whether a payload applies to the named cache.
func (p evictPayload) affects(cacheName string) bool {
	if len(p.Caches) == 0 {
		return true
	}
	for _, name := range p.Caches {
		if strings.TrimSpace(name) == cacheName {
			return true
		}
	}
	return false
}

// NotifyTx queues a scoped eviction on tx. Call it inside the write transaction, then Evict
// locally after Commit so the writing instance reads its own write immediately.
func NotifyTx(ctx context.Context, tx Execer, tenantID string, parkIDs ...string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil
	}
	parks := Key{Parks: parkIDs}.normalizedParks()
	q := sqlbind.MustBind(notifySQL, NotifyChannel, tenantID, parks)
	if _, err := tx.Exec(ctx, q.SQL(), q.Args()...); err != nil {
		return fmt.Errorf("readcache: notify eviction: %w", err)
	}
	return nil
}

// QueueNotify appends the eviction to a pgx.Batch whose statements run as ONE implicit
// transaction (a CLI importer's bulk upsert), so it is delivered only if the batch commits. The
// caller drains one extra result for it.
func QueueNotify(b *pgx.Batch, tenantID string, parkIDs ...string) bool {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return false
	}
	q := sqlbind.MustBind(notifySQL, NotifyChannel, tenantID, Key{Parks: parkIDs}.normalizedParks())
	b.Queue(q.SQL(), q.Args()...)
	return true
}

func parsePayload(payload string) (evictPayload, bool) {
	var p evictPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return p, false
	}
	p.TenantID = strings.TrimSpace(p.TenantID)
	return p, p.TenantID != ""
}

// Invalidator is the ONE port a writer takes to drop this process's cached reads after its
// commit. *Cache implements it (nil-safe); a worker or CLI with no cache passes nil and still
// publishes to every API instance through NotifyTx.
type Invalidator interface {
	Evict(ctx context.Context, tenantID string, parkIDs ...string) int
}

// Committer is the subset of pgx.Tx CommitAndEvict needs.
type Committer interface {
	Execer
	Commit(ctx context.Context) error
}

// CommitAndEvict is the write-side contract in one call: publish the eviction INSIDE tx (so it is
// delivered to every instance on commit and dropped on rollback), commit, then evict this
// process's entries so its very next read sees the write. parkIDs empty = the whole tenant; pass
// every park the write touched, including a park something moved OUT of.
func CommitAndEvict(ctx context.Context, tx Committer, inv Invalidator, tenantID string, parkIDs ...string) error {
	if err := NotifyTx(ctx, tx, tenantID, parkIDs...); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if inv != nil && strings.TrimSpace(tenantID) != "" {
		inv.Evict(ctx, tenantID, parkIDs...)
	}
	return nil
}
