package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// THE FCR FEED-DAY ROLLUP (migration 000418).
//
// growth_fcr_pen_feed_days holds, per (tenant, park, pen, business day), the feed the sheet
// directed to the pen that day, already priced at the same-farm latest load on or before it, and
// the day's head count. The FCR reads (fcr.go) sum these day rows over a window or a segment
// instead of re-deriving every feed cell and its price per request.
//
// REFRESH CONTRACT. Source writes never touch the rollup directly; a statement trigger on
// feed_direction_issues, feed_direction_issue_rows and feed_purchases appends (tenant, park,
// from_day) to growth_fcr_rollup_dirty inside the writer's own transaction. RefreshFCRRollup
// drains that log for a park and recomputes the park's rollup from from_day onward, in ONE
// transaction under a per-park advisory lock:
//
//  1. DELETE the park's dirty rows (RETURNING min(from_day)). Under READ COMMITTED the recompute
//     statements below take a LATER snapshot, so a write that commits in between is either in the
//     recompute or leaves its own dirty row behind -- never lost, at worst refreshed twice.
//  2. DELETE the park's rollup rows from from_day (a bounded range, never the whole tenant).
//  3. INSERT the recompute for the same range.
//
// Readers see the old rows until the transaction commits (MVCC), so a refresh never serves a
// half-built park. A failed refresh rolls back and leaves the dirty rows for the next attempt.
//
// WHO REFRESHES: the FCR read path, for exactly the parks it is about to read (so a read never
// serves a range a write has invalidated); the API warm-up and the kernel housekeeping stage, for
// every dirty park in the background; and the daily reconcile, which recomputes everything and
// records the drift it found.

// fcrRollupPenKey is the bridge's scrub of the sheet's generated partition_key, identical to the
// one the pre-rollup feed_rows CTE applied.
const fcrRollupPenKey = `CASE WHEN r.partition_key = 'whole' THEN ''
              WHEN r.partition_key LIKE 'part %' THEN btrim(substr(r.partition_key, 6))
              WHEN r.partition_key LIKE 'pt %' THEN btrim(substr(r.partition_key, 4))
              ELSE r.partition_key END`

// fcrRollupComputeSQL recomputes one park's day rows from $3 onward. $1 tenant, $2 park, $3 from
// business day. Bounded by the park's feed sheet from that day; one statement per refresh.
//
// projection-review: membership=every live (issued/amended/locked) feed-direction row of the park
// on or after $3; group_key=(pen_shed_id, pen_key, feed_day) on day_feed, day_heads and the insert
// (the rollup PK adds the fixed tenant + park); join_cardinality=price is unique on (feed_item_key,
// feed_day) -- built from the DISTINCT of that key with a LIMIT 1 lateral -- so the LEFT JOIN is
// 1:0..1 per feed row, and day_heads pre-collapses head_count with max() per (pen, day,
// shed_tag_key, breed_key) because it repeats on every item cell and session; pagination=NONE (a
// maintenance write, not a request read); scope=tenant + one park + feed_day >= $3.
const fcrRollupComputeSQL = ` -- scale-guard:ignore: incremental rollup maintenance for one park from its oldest dirty day; bounded by that park's feed sheet, run off the request path except for a just-invalidated range
WITH fr AS (
  SELECT r.shed_id AS pen_shed_id,
         ` + fcrRollupPenKey + ` AS pen_key,
         COALESCE(NULLIF(r.partition_label, 'whole'), '') AS partition_label,
         i.feed_day, r.feed_item_key, r.quantity_kg, r.head_count, r.shed_tag_key, r.breed_key
  FROM feed_direction_issue_rows r
  JOIN feed_direction_issues i
    ON i.tenant_id = r.tenant_id AND i.feed_direction_issue_id = r.feed_direction_issue_id
  WHERE r.tenant_id = $1::uuid
    AND i.park_id = $2::uuid
    AND i.feed_day >= $3::date
    AND i.state IN ('issued','amended','locked')
),
price AS (
  SELECT k.feed_item_key, k.feed_day, p.per_kg
  FROM (SELECT DISTINCT feed_item_key, feed_day FROM fr) k
  LEFT JOIN LATERAL (
    SELECT COALESCE(fp.per_kg_cost, fp.total_cost / NULLIF(fp.quantity_kg, 0))::float8 AS per_kg
    FROM feed_purchases fp
    WHERE fp.tenant_id = $1::uuid AND fp.park_id = $2::uuid
      AND fp.feed_item_key = k.feed_item_key
      AND fp.purchase_date <= k.feed_day
    ORDER BY fp.purchase_date DESC, fp.batch_no DESC
    LIMIT 1
  ) p ON true
),
day_feed AS (
  SELECT fr.pen_shed_id, fr.pen_key, fr.feed_day,
         sum(fr.quantity_kg) FILTER (WHERE fr.quantity_kg IS NOT NULL) AS feed_kg,
         sum(fr.quantity_kg * pr.per_kg) FILTER (WHERE fr.quantity_kg IS NOT NULL AND pr.per_kg IS NOT NULL)::float8 AS feed_cost,
         COALESCE(sum(fr.quantity_kg) FILTER (WHERE fr.quantity_kg IS NOT NULL AND pr.per_kg IS NULL), 0) AS unpriced_kg,
         count(*) FILTER (WHERE fr.quantity_kg IS NULL)::int AS blocked_cells,
         min(fr.partition_label) FILTER (WHERE fr.partition_label <> '') AS feed_label
  FROM fr
  LEFT JOIN price pr ON pr.feed_item_key = fr.feed_item_key AND pr.feed_day = fr.feed_day
  GROUP BY fr.pen_shed_id, fr.pen_key, fr.feed_day
),
day_heads AS (
  SELECT g.pen_shed_id, g.pen_key, g.feed_day, sum(g.heads) AS head_days
  FROM (
    SELECT pen_shed_id, pen_key, feed_day, shed_tag_key, breed_key, max(head_count) AS heads
    FROM fr
    WHERE head_count IS NOT NULL
    GROUP BY pen_shed_id, pen_key, feed_day, shed_tag_key, breed_key
  ) g
  GROUP BY g.pen_shed_id, g.pen_key, g.feed_day
)
SELECT f.pen_shed_id, f.pen_key, f.feed_day, f.feed_kg, f.feed_cost, f.unpriced_kg, f.blocked_cells,
       h.head_days, f.feed_label
FROM day_feed f
LEFT JOIN day_heads h ON h.pen_shed_id = f.pen_shed_id AND h.pen_key = f.pen_key AND h.feed_day = f.feed_day`

const fcrRollupInsertSQL = `INSERT INTO growth_fcr_pen_feed_days
  (tenant_id, park_id, pen_shed_id, pen_key, feed_day, feed_kg, feed_cost, unpriced_kg, blocked_cells, head_days, feed_label, refreshed_at)
SELECT $1::uuid, $2::uuid, c.pen_shed_id, c.pen_key, c.feed_day, c.feed_kg, c.feed_cost, c.unpriced_kg, c.blocked_cells, c.head_days, c.feed_label, now()
FROM (` + fcrRollupComputeSQL + `) c`

const fcrRollupDrainSQL = `WITH drained AS (
  DELETE FROM growth_fcr_rollup_dirty
  WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  RETURNING from_day
)
SELECT min(from_day)::text FROM drained`

const fcrRollupDeleteRangeSQL = `DELETE FROM growth_fcr_pen_feed_days
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND feed_day >= $3::date`

const fcrRollupDirtyParksSQL = `SELECT tenant_id::text, park_id::text
FROM growth_fcr_rollup_dirty
WHERE ($1::uuid IS NULL OR tenant_id = $1::uuid)
  AND (cardinality($2::uuid[]) = 0 OR park_id = ANY($2::uuid[]))
GROUP BY tenant_id, park_id
ORDER BY tenant_id, park_id
LIMIT $3::int`

// fcrRollupMaxParksPerPass bounds one background drain; the next pass continues.
const fcrRollupMaxParksPerPass = 200

type fcrDirtyPark struct{ tenantID, parkID string }

func (r *Repository) fcrDirtyParks(ctx context.Context, tenantID string, parkIDs []string, limit int) ([]fcrDirtyPark, error) {
	var tenant any
	if tenantID != "" {
		tenant = tenantID
	}
	if parkIDs == nil {
		parkIDs = []string{}
	}
	q := sqlbind.MustBind(fcrRollupDirtyParksSQL, tenant, parkIDs, limit)
	rows, err := r.pool.Query(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fcrDirtyPark
	for rows.Next() {
		var d fcrDirtyPark
		if err := rows.Scan(&d.tenantID, &d.parkID); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// refreshFCRRollupForRead makes the rollup current for exactly the parks a read is about to sum.
// A clean park costs one indexed probe of the (usually empty) dirty log.
func (r *Repository) refreshFCRRollupForRead(ctx context.Context, tenantID string, parkIDs []string) error {
	if len(parkIDs) == 0 {
		return nil
	}
	dirty, err := r.fcrDirtyParks(ctx, tenantID, parkIDs, len(parkIDs))
	if err != nil {
		return fmt.Errorf("growthdirector: fcr rollup dirty probe: %w", err)
	}
	for _, d := range dirty { // scale-guard:ignore: bounded by the parks in the read's scope (a handful); each is one refresh transaction
		if _, err := r.refreshFCRRollupPark(ctx, d.tenantID, d.parkID); err != nil {
			return err
		}
	}
	return nil
}

// RefreshFCRRollup drains the dirty log for every tenant (tenantID empty) or one tenant, at most
// fcrRollupMaxParksPerPass parks per call. Returns the number of parks refreshed.
func (r *Repository) RefreshFCRRollup(ctx context.Context, tenantID string) (int, error) {
	dirty, err := r.fcrDirtyParks(ctx, tenantID, nil, fcrRollupMaxParksPerPass)
	if err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, d := range dirty { // scale-guard:ignore: bounded background drain, <= fcrRollupMaxParksPerPass parks, one transaction each
		if _, err := r.refreshFCRRollupPark(ctx, d.tenantID, d.parkID); err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// refreshFCRRollupPark is steps 1-3 of the refresh contract for one park. Returns the day it
// recomputed from, or "" when another refresher had already drained the park.
func (r *Repository) refreshFCRRollupPark(ctx context.Context, tenantID, parkID string) (string, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	// Serialize refreshers of one park across API instances and the worker. A reader that waits
	// here then finds the log drained and reads the rows the winner committed.
	lock := sqlbind.MustBind(`SELECT pg_advisory_xact_lock(hashtextextended('growth_fcr_rollup:' || $1::text || ':' || $2::text, 0))`, tenantID, parkID)
	if _, err := tx.Exec(ctx, lock.SQL(), lock.Args()...); err != nil {
		return "", fmt.Errorf("growthdirector: fcr rollup lock: %w", err)
	}
	drain := sqlbind.MustBind(fcrRollupDrainSQL, tenantID, parkID)
	var fromDay *string
	if err := tx.QueryRow(ctx, drain.SQL(), drain.Args()...).Scan(&fromDay); err != nil {
		return "", fmt.Errorf("growthdirector: fcr rollup drain: %w", err)
	}
	if fromDay == nil {
		return "", tx.Commit(ctx)
	}
	del := sqlbind.MustBind(fcrRollupDeleteRangeSQL, tenantID, parkID, *fromDay)
	if _, err := tx.Exec(ctx, del.SQL(), del.Args()...); err != nil {
		return "", fmt.Errorf("growthdirector: fcr rollup clear range: %w", err)
	}
	ins := sqlbind.MustBind(fcrRollupInsertSQL, tenantID, parkID, *fromDay)
	if _, err := tx.Exec(ctx, ins.SQL(), ins.Args()...); err != nil {
		return "", fmt.Errorf("growthdirector: fcr rollup recompute: %w", err)
	}
	return *fromDay, tx.Commit(ctx)
}

// fcrRollupDriftSQL counts the park's stored day rows that disagree with a fresh recompute
// (missing on either side, or any value differing beyond float noise). Read-only.
const fcrRollupDriftSQL = `WITH fresh AS (` + fcrRollupComputeSQL + `),
stored AS (
  SELECT pen_shed_id, pen_key, feed_day, feed_kg, feed_cost, unpriced_kg, blocked_cells, head_days, feed_label
  FROM growth_fcr_pen_feed_days
  WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND feed_day >= $3::date
)
SELECT count(*)::int
FROM fresh f
FULL JOIN stored s ON s.pen_shed_id = f.pen_shed_id AND s.pen_key = f.pen_key AND s.feed_day = f.feed_day
WHERE f.feed_day IS NULL OR s.feed_day IS NULL
   OR f.feed_kg IS DISTINCT FROM s.feed_kg
   OR f.unpriced_kg IS DISTINCT FROM s.unpriced_kg
   OR f.blocked_cells IS DISTINCT FROM s.blocked_cells
   OR f.head_days IS DISTINCT FROM s.head_days
   OR f.feed_label IS DISTINCT FROM s.feed_label
   OR (f.feed_cost IS NULL) <> (s.feed_cost IS NULL)
   OR abs(f.feed_cost - s.feed_cost) > 1e-6 * greatest(1, abs(f.feed_cost))`

const fcrRollupParksSQL = `SELECT DISTINCT tenant_id::text, park_id::text FROM feed_direction_issues
WHERE ($1::uuid IS NULL OR tenant_id = $1::uuid)
ORDER BY 1, 2`

const fcrRollupReconciledSQL = `SELECT count(*)::int FROM growth_fcr_rollup_reconcile
WHERE tenant_id = $1::uuid AND reconciled_on >= $2::date`

const fcrRollupRecordReconcileSQL = `INSERT INTO growth_fcr_rollup_reconcile (tenant_id, reconciled_on, drift_rows, reconciled_at)
VALUES ($1::uuid, $2::date, $3::int, now())
ON CONFLICT (tenant_id) DO UPDATE SET reconciled_on = EXCLUDED.reconciled_on, drift_rows = EXCLUDED.drift_rows, reconciled_at = now()`

// ReconcileFCRRollup is the daily safety net: once per business day per tenant it compares every
// park's stored rollup with a fresh recompute, counts the drifted rows, and re-marks any drifted
// park dirty from its first day so the normal refresh repairs it. Drift should be zero; a
// non-zero count means a writer bypassed the triggers and is logged by the caller.
func (r *Repository) ReconcileFCRRollup(ctx context.Context, tenantID string, now time.Time) (int, error) {
	today := biztime.BusinessDate(now)
	var tenant any
	if tenantID != "" {
		tenant = tenantID
	}
	q := sqlbind.MustBind(fcrRollupParksSQL, tenant)
	rows, err := r.pool.Query(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return 0, err
	}
	var parks []fcrDirtyPark
	for rows.Next() {
		var d fcrDirtyPark
		if err := rows.Scan(&d.tenantID, &d.parkID); err != nil {
			rows.Close()
			return 0, err
		}
		parks = append(parks, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	drift := map[string]int{}
	done := map[string]bool{}
	for _, p := range parks { // scale-guard:ignore: daily reconcile over the estate's parks (a handful per tenant), one read-only comparison each
		if _, seen := done[p.tenantID]; !seen {
			check := sqlbind.MustBind(fcrRollupReconciledSQL, p.tenantID, today)
			var already int
			if err := r.pool.QueryRow(ctx, check.SQL(), check.Args()...).Scan(&already); err != nil { // scale-guard:ignore: once per tenant in the daily reconcile
				return 0, err
			}
			done[p.tenantID] = already > 0
		}
		if done[p.tenantID] {
			continue
		}
		cmp := sqlbind.MustBind(fcrRollupDriftSQL, p.tenantID, p.parkID, "0001-01-01")
		var n int
		if err := r.pool.QueryRow(ctx, cmp.SQL(), cmp.Args()...).Scan(&n); err != nil { // scale-guard:ignore: one read-only comparison per park, daily
			return 0, fmt.Errorf("growthdirector: fcr rollup drift: %w", err)
		}
		drift[p.tenantID] += n
		if n > 0 {
			mark := sqlbind.MustBind(`INSERT INTO growth_fcr_rollup_dirty (tenant_id, park_id, from_day)
SELECT $1::uuid, $2::uuid, COALESCE(min(feed_day), CURRENT_DATE) FROM feed_direction_issues WHERE tenant_id = $1::uuid AND park_id = $2::uuid`, p.tenantID, p.parkID)
			if _, err := r.pool.Exec(ctx, mark.SQL(), mark.Args()...); err != nil { // scale-guard:ignore: only for a drifted park, daily
				return 0, err
			}
		}
	}
	total := 0
	for t, isDone := range done {
		if isDone {
			continue
		}
		rec := sqlbind.MustBind(fcrRollupRecordReconcileSQL, t, today, drift[t])
		if _, err := r.pool.Exec(ctx, rec.SQL(), rec.Args()...); err != nil { // scale-guard:ignore: one bookkeeping row per tenant, daily
			return 0, err
		}
		total += drift[t]
	}
	return total, nil
}
