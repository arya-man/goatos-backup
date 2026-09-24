package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// The stored unread counter (migration 000403).
//
// The bell badge used to be sqlUnreadCountLegacy: COUNT(DISTINCT dedupe key) over the caller's
// whole unread history on every page load (96k rows / ~610 MB of buffers for the worst member,
// stg p95 15 s). It is now one primary-key lookup on notification_member_unread_counts, which
// statement-level triggers on notification_requests keep equal to that COUNT in the same
// transaction as every write (insert, MarkRead, escalation stamp, auto-resolve, delete).
//
// HOT ROWS: the trigger locks each affected member's counter row until COMMIT. Leadership members
// (park head, director, CEO) receive a copy of most pushes, so their rows are the hottest: every
// producer statement and every verification decision that touches one of them serialises on it.
// Rules that keep this cheap and deadlock-free: (1) at most ONE counter-touching statement per
// transaction (members are locked in sorted order within a statement, never across statements);
// (2) make it the LAST write before commit and keep that transaction short -- no network calls or
// slow reads after it. If the lock ever shows up in waits, shard the row by (member, bucket).
//
// Until the one-shot backfill has covered the rows that predate 000403, the counter tables only
// know about keys touched since; notification_unread_counter_state.backfill_complete_at is the
// gate, and while it is NULL the read falls back to the legacy query.

// sqlUnreadCountStored reads the caller's stored unread total plus the backfill gate.
//
// projection-review: membership=notification_member_unread_counts at its (tenant_id, member_id)
// primary key, where member_id is the caller's own resolved ACTIVE workforce_member_id via the
// same fail-closed target_member CTE the page read uses -- an unresolvable caller yields no row
// and therefore 0; grain=one row per member, whose value is maintained at the SAME
// notification (dedupe-key) grain as sqlUnreadCountLegacy by the 000403 triggers;
// group_key=none; join_cardinality=none (scalar subqueries on primary keys); pagination=none,
// a single number; scope=tenant_id AND the caller's own member id.
//
// scale-guard:ignore: O(1) primary-key lookups; does not grow with history or herd size.
const sqlUnreadCountStored = sqlTargetMemberCTE + `
SELECT
  COALESCE((
    SELECT c.unread_count
    FROM notification_member_unread_counts c, target_member tm
    WHERE c.tenant_id = $1::uuid
      AND tm.workforce_member_id IS NOT NULL
      AND c.member_id = tm.workforce_member_id::text
  ), 0) AS unread_count,
  EXISTS (
    SELECT 1 FROM notification_unread_counter_state s
    WHERE s.singleton AND s.backfill_complete_at IS NOT NULL
  ) AS counter_ready`

// unreadCount answers the badge from the stored counter, or from history while the backfill
// gate is still closed.
func (r *Repository) unreadCount(ctx context.Context, tenantID, memberOrUserID string) (int, error) {
	var (
		unread int
		ready  bool
	)
	storedQuery := sqlbind.MustBind(sqlUnreadCountStored, tenantID, memberOrUserID)
	if err := r.pool.QueryRow(ctx, storedQuery.SQL(), storedQuery.Args()...).Scan(&unread, &ready); err != nil {
		return 0, fmt.Errorf("notificationcentre: unread count: %w", err)
	}
	if ready {
		return unread, nil
	}
	legacyQuery := sqlbind.MustBind(sqlUnreadCountLegacy, tenantID, memberOrUserID)
	if err := r.pool.QueryRow(ctx, legacyQuery.SQL(), legacyQuery.Args()...).Scan(&unread); err != nil {
		return 0, fmt.Errorf("notificationcentre: unread count (pre-backfill): %w", err)
	}
	return unread, nil
}

// ---------------------------------------------------------------------------
// Backfill and reconcile (cmd/backfill-notification-unread-counters)
// ---------------------------------------------------------------------------

// BackfillStats reports one backfill run.
type BackfillStats struct {
	Members int
	Keys    int
	Batches int
}

// UnreadMismatch is a member whose stored counter differs from the legacy COUNT.
type UnreadMismatch struct {
	TenantID string
	MemberID string
	Stored   int
	Legacy   int
}

// sqlNextNotificationMember walks the distinct (tenant, member) pairs of notification_requests
// as a loose index scan on notification_requests_member_feed_idx (tenant_id,
// (context->>'member_id'), ...): one index descent per member, never a scan of the table.
//
// scale-guard:ignore: offline backfill/reconcile job, not a request path; one LIMIT 1 probe per member.
const sqlNextNotificationMember = `
SELECT nr.tenant_id::text, nr.context->>'member_id'
FROM notification_requests nr
WHERE nr.context->>'member_id' IS NOT NULL
  AND (nr.tenant_id, nr.context->>'member_id') > ($1::uuid, $2::text)
ORDER BY nr.tenant_id, nr.context->>'member_id'
LIMIT 1`

// sqlMemberUnreadKeyBatch is one keyset batch of a member's distinct unread dedupe keys, walked
// on notification_requests_member_dedupe_idx in key order.
//
// scale-guard:ignore: offline backfill job; each batch is bounded by LIMIT and resumes by keyset.
const sqlMemberUnreadKeyBatch = `
SELECT DISTINCT ` + dedupeKeyExpr + ` AS dedupe_key
FROM notification_requests nr
WHERE nr.tenant_id = $1::uuid
  AND nr.context->>'member_id' = $2
  AND ` + dedupeKeyExpr + ` > $3
  AND ` + unreadPredicate + `
ORDER BY 1
LIMIT $4`

// sqlMemberStoredKeyBatch is one keyset batch of the keys the counter already holds for a
// member, re-synced so a repair run also removes keys that are no longer unread.
//
// scale-guard:ignore: offline backfill job; primary-key range, bounded by LIMIT.
const sqlMemberStoredKeyBatch = `
SELECT k.dedupe_key
FROM notification_member_unread_keys k
WHERE k.tenant_id = $1::uuid AND k.member_id = $2 AND k.dedupe_key > $3
ORDER BY k.dedupe_key
LIMIT $4`

const sqlUnreadSync = `SELECT notification_unread_sync($1::uuid[], $2::text[], $3::text[])`

const sqlMarkBackfillComplete = `
UPDATE notification_unread_counter_state
SET backfill_complete_at = now()
WHERE singleton AND backfill_complete_at IS NULL`

// sqlReconcileMember reads one member's stored number AND the legacy history COUNT in ONE
// statement, i.e. from one snapshot. The triggers keep the counter in the same transaction as
// every row write, so within a single snapshot the two are exactly equal even while writes are in
// flight: the reconciler never reports a transient mismatch.
//
// scale-guard:ignore: offline reconcile job, one member at a time; the history aggregate this
// replaces on the request path is exactly what a reconciler has to recompute.
const sqlReconcileMember = `
SELECT
  COALESCE((SELECT unread_count FROM notification_member_unread_counts
            WHERE tenant_id = $1::uuid AND member_id = $2), 0) AS stored,
  (SELECT count(*) FROM (
     SELECT DISTINCT ` + dedupeKeyExpr + `
     FROM notification_requests nr
     WHERE nr.tenant_id = $1::uuid
       AND nr.context->>'member_id' = $2
       AND ` + unreadPredicate + `
   ) unread_keys) AS legacy`

// sqlCounterGateOpen reports whether the backfill has completed.
const sqlCounterGateOpen = `
SELECT EXISTS (SELECT 1 FROM notification_unread_counter_state WHERE singleton AND backfill_complete_at IS NOT NULL)`

// CounterGateOpen reports whether the stored counter is live on the read path.
func (r *Repository) CounterGateOpen(ctx context.Context) (bool, error) {
	var open bool
	if err := r.pool.QueryRow(ctx, sqlCounterGateOpen).Scan(&open); err != nil {
		return false, fmt.Errorf("notificationcentre: counter gate: %w", err)
	}
	return open, nil
}

// forEachNotificationMember visits every (tenant, member) that has notification rows.
func (r *Repository) forEachNotificationMember(ctx context.Context, fn func(tenantID, memberID string) error) error {
	tenant, member := "00000000-0000-0000-0000-000000000000", ""
	for {
		var nextTenant, nextMember string
		// scale-guard:ignore: offline loose index scan; one LIMIT 1 descent per member by design.
		err := r.pool.QueryRow(ctx, sqlNextNotificationMember, tenant, member).Scan(&nextTenant, &nextMember)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("notificationcentre: next member: %w", err)
		}
		tenant, member = nextTenant, nextMember
		if member == "" {
			continue
		}
		if err := fn(tenant, member); err != nil {
			return err
		}
	}
}

// syncKeyBatches re-derives a member's keys from one key source, `batch` keys per transaction.
func (r *Repository) syncKeyBatches(ctx context.Context, query, tenantID, memberID string, batch int, stats *BackfillStats) error {
	after := ""
	for {
		batchQuery := sqlbind.MustBind(query, tenantID, memberID, after, batch)
		// scale-guard:ignore: offline backfill; one bounded keyset batch per iteration by design.
		rows, err := r.pool.Query(ctx, batchQuery.SQL(), batchQuery.Args()...)
		if err != nil {
			return fmt.Errorf("notificationcentre: key batch: %w", err)
		}
		keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("notificationcentre: key batch rows: %w", err)
		}
		if len(keys) == 0 {
			return nil
		}
		tenants := make([]string, len(keys))
		members := make([]string, len(keys))
		for i := range keys {
			tenants[i], members[i] = tenantID, memberID
		}
		// Autocommit: each batch is its own short transaction, so the member lock the sync
		// function takes is held for one batch, not the whole member.
		// scale-guard:ignore: offline backfill; one set-based sync of up to `batch` keys per iteration.
		if _, err := r.pool.Exec(ctx, sqlUnreadSync, tenants, members, keys); err != nil {
			return fmt.Errorf("notificationcentre: sync batch: %w", err)
		}
		stats.Keys += len(keys)
		stats.Batches++
		after = keys[len(keys)-1]
		if len(keys) < batch {
			return nil
		}
	}
}

// BackfillUnreadCounters brings the counter tables up to date for rows written before
// migration 000403, then opens the read gate. Idempotent and resumable: every batch re-derives
// key state from the rows, so a rerun (or a run racing live traffic) converges on the same
// numbers. Members first seen after the walk starts were written under the triggers and are
// already exact.
func (r *Repository) BackfillUnreadCounters(ctx context.Context, batch int) (BackfillStats, error) {
	if batch <= 0 {
		batch = 500
	}
	var stats BackfillStats
	err := r.forEachNotificationMember(ctx, func(tenantID, memberID string) error {
		stats.Members++
		if err := r.syncKeyBatches(ctx, sqlMemberUnreadKeyBatch, tenantID, memberID, batch, &stats); err != nil {
			return err
		}
		return r.syncKeyBatches(ctx, sqlMemberStoredKeyBatch, tenantID, memberID, batch, &stats)
	})
	if err != nil {
		return stats, err
	}
	if _, err := r.pool.Exec(ctx, sqlMarkBackfillComplete); err != nil {
		return stats, fmt.Errorf("notificationcentre: open counter gate: %w", err)
	}
	return stats, nil
}

// ReconcileUnreadCounters compares every member's stored counter with the legacy COUNT and
// returns the members that differ. Read-only.
func (r *Repository) ReconcileUnreadCounters(ctx context.Context) ([]UnreadMismatch, error) {
	var out []UnreadMismatch
	err := r.forEachNotificationMember(ctx, func(tenantID, memberID string) error {
		var stored, legacy int
		reconcileQuery := sqlbind.MustBind(sqlReconcileMember, tenantID, memberID)
		// scale-guard:ignore: offline reconciler; one single-snapshot comparison per member.
		if err := r.pool.QueryRow(ctx, reconcileQuery.SQL(), reconcileQuery.Args()...).Scan(&stored, &legacy); err != nil {
			return fmt.Errorf("notificationcentre: reconcile: %w", err)
		}
		if stored != legacy {
			out = append(out, UnreadMismatch{TenantID: tenantID, MemberID: memberID, Stored: stored, Legacy: legacy})
		}
		return nil
	})
	return out, err
}
