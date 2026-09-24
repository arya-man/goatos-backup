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

// Execer is the subset of pgx.Tx the notify needs.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

var _ Execer = (pgx.Tx)(nil)

type evictPayload struct {
	TenantID string   `json:"tenant_id"`
	ParkIDs  []string `json:"park_ids,omitempty"`
}

// NotifyTx queues a scoped eviction on tx. Call it inside the write transaction, then Evict
// locally after Commit so the writing instance reads its own write immediately.
func NotifyTx(ctx context.Context, tx Execer, tenantID string, parkIDs ...string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil
	}
	parks := Key{Parks: parkIDs}.normalizedParks()
	q := sqlbind.MustBind(`SELECT pg_notify($1, json_build_object('tenant_id', $2::text, 'park_ids', $3::text[])::text)`,
		NotifyChannel, tenantID, parks)
	if _, err := tx.Exec(ctx, q.SQL(), q.Args()...); err != nil {
		return fmt.Errorf("readcache: notify eviction: %w", err)
	}
	return nil
}

func parsePayload(payload string) (evictPayload, bool) {
	var p evictPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return p, false
	}
	p.TenantID = strings.TrimSpace(p.TenantID)
	return p, p.TenantID != ""
}
