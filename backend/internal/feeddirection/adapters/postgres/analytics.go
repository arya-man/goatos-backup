package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

var _ ports.DirectedAnalyticsReader = (*Repository)(nil)

// Feed Analytics rollup over the frozen sheet.
//
// projection-review: membership=feed_direction_issue_rows at their natural key (tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, shed_tag_key, breed_key, feed_item_key), reached through the at-most-one live normal-workflow issue per (tenant, park, feed_day) enforced by feed_direction_issues_live_uidx; group_key=(feed_day, feed_item_label, feed_item_key) after first collapsing cells to the pen-grain (shed_id, partition_key, shed_tag_key, breed_key) so session and item cells cannot inflate head counts; join_cardinality=issues to rows is 1:N by feed_direction_issue_id and joins exactly once per day thanks to the live-issue partial unique index, and the pen_item CTE pre-aggregates the N side before the outer GROUP BY; pagination=none, whole-window aggregate invariant to any page size — there is no limit/offset input; scope=tenant_id on both tables plus the caller's authorized park set via park_id = ANY($2)
//
// Ratio key sets: per_head_grams divides SUM(quantity_kg) by SUM(head_count)
// where BOTH range over the same collapsed pen-grain set of that
// (feed_day, feed_item) group — never cell-level head counts, which repeat per
// session and per item and would inflate the denominator ~6×.
//
// quantity_kg is NULL IFF BLOCKED (schema CHECK). SUM skips NULLs, so blocked
// cells contribute nothing; COALESCE to 0 happens only after the aggregate,
// where "0" states "every resolved cell authored zero", never "blocked".
// Blocked counts are not reported here (maintainer decision 2026-08-17).
//
// head_count is identical across a pen-grain's sessions by generation; MAX() is
// the documented pick so a mid-day amendment that moved heads reads as the
// amended (larger-or-equal) figure rather than double-counting.
//
// scale-guard:ignore: 5k-50k-envelope — bounded windowed aggregate (≤92 days ×
// ≤2 parks × ~1 normal issue/day, rows reached via the issue-id natural-key
// prefix), the ADR's canonical-indexed-SQL default for a new read surface.
const directedAnalyticsSQL = `
WITH iss AS (
    SELECT feed_direction_issue_id, feed_day
    FROM feed_direction_issues
    WHERE tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
      AND feed_day BETWEEN $3 AND $4
      AND state IN ('issued', 'amended', 'locked')
      -- BOTH workflows (maintainer decision 2026-08-19): experiment pens are real
      -- animals eating real feed, so the overview's directed kg, animals-fed and
      -- per-head figures include them alongside the normal sheet.
      AND workflow IN ('normal', 'experiment')
),
pen_item AS (
    SELECT i.feed_day,
           r.feed_item_label,
           r.feed_item_key,
           r.shed_id,
           r.partition_key,
           r.shed_tag_key,
           r.breed_key,
           SUM(r.quantity_kg)                                   AS grain_kg,
           MAX(r.head_count)                                    AS grain_heads
    FROM iss i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1
     AND r.feed_direction_issue_id = i.feed_direction_issue_id
    GROUP BY i.feed_day, r.feed_item_label, r.feed_item_key,
             r.shed_id, r.partition_key, r.shed_tag_key, r.breed_key
)
SELECT feed_day::text,
       feed_item_label,
       feed_item_key,
       COALESCE(SUM(grain_kg), 0)::text                          AS directed_kg,
       COALESCE(SUM(grain_heads), 0)                             AS head_days,
       COALESCE(
         round(SUM(grain_kg) * 1000 / NULLIF(SUM(grain_heads), 0), 1)::text,
         ''
       )                                                         AS per_head_grams
FROM pen_item
GROUP BY feed_day, feed_item_label, feed_item_key
ORDER BY feed_day, feed_item_label`

// Day totals reuse the same pen-grain collapse but count each pen-grain's heads
// ONCE ACROSS ITEMS: the same animals eat every item on the sheet, so summing
// per-item head-days into a day figure would multiply the herd by the number of
// feed items.
//
// projection-review: membership=same issue-row natural-key set as above; group_key=feed_day alone, after collapsing to the pen-grain (shed_id, partition_key, shed_tag_key, breed_key) WITHOUT the feed item, so a day's heads count each pen once across items and sessions; join_cardinality=issues to rows 1:N pre-aggregated in the pen CTE before the outer day GROUP BY; pagination=none, whole-window aggregate with no limit/offset input; scope=tenant_id plus the caller's authorized park set
const directedAnalyticsDaysSQL = `
WITH iss AS (
    SELECT feed_direction_issue_id, feed_day
    FROM feed_direction_issues
    WHERE tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
      AND feed_day BETWEEN $3 AND $4
      AND state IN ('issued', 'amended', 'locked')
      -- BOTH workflows (maintainer decision 2026-08-19): experiment pens are real
      -- animals eating real feed, so the overview's directed kg, animals-fed and
      -- per-head figures include them alongside the normal sheet.
      AND workflow IN ('normal', 'experiment')
),
pen AS (
    SELECT i.feed_day,
           r.shed_id,
           r.partition_key,
           r.shed_tag_key,
           r.breed_key,
           SUM(r.quantity_kg)                                   AS grain_kg,
           MAX(r.head_count)                                    AS grain_heads
    FROM iss i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1
     AND r.feed_direction_issue_id = i.feed_direction_issue_id
    GROUP BY i.feed_day, r.shed_id, r.partition_key, r.shed_tag_key, r.breed_key
)
SELECT feed_day::text,
       COALESCE(SUM(grain_kg), 0)::text                          AS directed_kg,
       COALESCE(SUM(grain_heads), 0)                             AS head_days,
       COALESCE(
         round(SUM(grain_kg) * 1000 / NULLIF(SUM(grain_heads), 0), 1)::text,
         ''
       )                                                         AS per_head_grams
FROM pen
GROUP BY feed_day
ORDER BY feed_day`

// DirectedAnalytics serves the windowed directed rollup. Two set-based reads,
// no per-day fan-out.
func (r *Repository) DirectedAnalytics(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.DirectedAnalytics, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	from, to := domain.ClampAnalyticsWindow(q.DateFrom, q.DateTo)
	// nil (not an empty array) when unrestricted, so the SQL guard short-circuits.
	var parkIDs []uuid.UUID
	if len(q.ParkIDs) > 0 {
		parkIDs = q.ParkIDs
	}
	fromArg := from.Format("2006-01-02")
	toArg := to.Format("2006-01-02")

	out := domain.DirectedAnalytics{
		Days:  []domain.DirectedDayTotal{},
		Items: []domain.DirectedDayItem{},
	}

	dayRows, err := r.pool.Query(ctx, directedAnalyticsDaysSQL, tenantID, parkIDs, fromArg, toArg)
	if err != nil {
		return domain.DirectedAnalytics{}, fmt.Errorf("feed analytics day rollup: %w", err)
	}
	defer dayRows.Close()
	for dayRows.Next() {
		var d domain.DirectedDayTotal
		if err := dayRows.Scan(&d.FeedDay, &d.DirectedKg, &d.HeadDays, &d.PerHeadGrams); err != nil {
			return domain.DirectedAnalytics{}, fmt.Errorf("feed analytics day rollup scan: %w", err)
		}
		out.Days = append(out.Days, d)
	}
	if err := dayRows.Err(); err != nil {
		return domain.DirectedAnalytics{}, fmt.Errorf("feed analytics day rollup rows: %w", err)
	}

	itemRows, err := r.pool.Query(ctx, directedAnalyticsSQL, tenantID, parkIDs, fromArg, toArg)
	if err != nil {
		return domain.DirectedAnalytics{}, fmt.Errorf("feed analytics item rollup: %w", err)
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var it domain.DirectedDayItem
		if err := itemRows.Scan(&it.FeedDay, &it.FeedItemLabel, &it.FeedItemKey, &it.DirectedKg, &it.HeadDays, &it.PerHeadGrams); err != nil {
			return domain.DirectedAnalytics{}, fmt.Errorf("feed analytics item rollup scan: %w", err)
		}
		out.Items = append(out.Items, it)
	}
	if err := itemRows.Err(); err != nil {
		return domain.DirectedAnalytics{}, fmt.Errorf("feed analytics item rollup rows: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Execution analytics
// ---------------------------------------------------------------------------

// projection-review: membership=three completion tables each already at the grain being counted — feed_packing_completions and feed_distribution_completions at their (tenant, park, shed, partition, session, target_date, workflow) natural-key grain, feed_transport_tasks at task grain; group_key=(date, status) per stream, merged by date in Go with no cross-table join, so COUNT(*) fans nothing out; join_cardinality=no joins at all — three independent single-table aggregates plus a UNION ALL latency read whose numerator and denominator range over the same verdict rows; pagination=none, whole-window counts with no limit/offset input; scope=tenant_id plus the caller's authorized park set on every stream
//
// scale-guard:ignore: 5k-50k-envelope — bounded windowed status counts over
// indexed date columns, the ADR's canonical-indexed-SQL default.
const executionStatusSQL = `
SELECT target_date::text AS d, status, COUNT(*)
FROM %s
WHERE tenant_id = $1
  AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
  AND target_date BETWEEN $3 AND $4
GROUP BY target_date, status`

const executionTransportSQL = `
SELECT business_date::text AS d, status, COUNT(*)
FROM feed_transport_tasks
WHERE tenant_id = $1
  AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
  AND business_date BETWEEN $3 AND $4
GROUP BY business_date, status`

// Latency buckets by the Asia/Kolkata DATE the verdict landed (business
// meaning is the India business calendar, never the UTC day).
const executionLatencySQL = `
SELECT (verified_at AT TIME ZONE 'Asia/Kolkata')::date::text AS d,
       round(percentile_cont(0.5) WITHIN GROUP (
         ORDER BY EXTRACT(EPOCH FROM (verified_at - created_at)) / 60.0
       ))::bigint AS median_minutes
FROM (
    SELECT verified_at, created_at FROM feed_packing_completions
    WHERE tenant_id = $1 AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
      AND verified_at IS NOT NULL
      AND (verified_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN $3 AND $4
    UNION ALL
    SELECT verified_at, created_at FROM feed_distribution_completions
    WHERE tenant_id = $1 AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
      AND verified_at IS NOT NULL
      AND (verified_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN $3 AND $4
) verdicts
GROUP BY 1`

// ExecutionAnalytics merges the three status streams and the latency series by
// date. Four set-based reads, no per-day fan-out.
func (r *Repository) ExecutionAnalytics(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.ExecutionAnalytics, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	from, to := domain.ClampAnalyticsWindow(q.DateFrom, q.DateTo)
	var parkIDs []uuid.UUID
	if len(q.ParkIDs) > 0 {
		parkIDs = q.ParkIDs
	}
	fromArg, toArg := from.Format("2006-01-02"), to.Format("2006-01-02")

	days := map[string]*domain.ExecutionDay{}
	day := func(d string) *domain.ExecutionDay {
		if existing, ok := days[d]; ok {
			return existing
		}
		fresh := &domain.ExecutionDay{Date: d}
		days[d] = fresh
		return fresh
	}

	countInto := func(sql string, apply func(*domain.ExecutionDay, string, int64)) error {
		rows, err := r.pool.Query(ctx, sql, tenantID, parkIDs, fromArg, toArg)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d, status string
			var n int64
			if err := rows.Scan(&d, &status, &n); err != nil {
				return err
			}
			apply(day(d), status, n)
		}
		return rows.Err()
	}

	if err := countInto(fmt.Sprintf(executionStatusSQL, "feed_packing_completions"), func(e *domain.ExecutionDay, status string, n int64) {
		switch status {
		case "completed":
			e.PackingVerified += n
		case "pending_verification":
			e.PackingAwaiting += n
		case "rework":
			e.PackingRework += n
		}
	}); err != nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics packing statuses: %w", err)
	}
	if err := countInto(fmt.Sprintf(executionStatusSQL, "feed_distribution_completions"), func(e *domain.ExecutionDay, status string, n int64) {
		switch status {
		case "completed":
			e.DistributionVerified += n
		case "pending_verification":
			e.DistributionAwaiting += n
		case "rework":
			e.DistributionRework += n
		}
	}); err != nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics distribution statuses: %w", err)
	}
	if err := countInto(executionTransportSQL, func(e *domain.ExecutionDay, status string, n int64) {
		switch status {
		case "completed":
			e.TransportCompleted += n
		case "due":
			e.TransportOpen += n
		case "verification_due":
			e.TransportAwaitingVerdict += n
		case "rework":
			e.TransportRework += n
		}
	}); err != nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics transport statuses: %w", err)
	}

	latRows, err := r.pool.Query(ctx, executionLatencySQL, tenantID, parkIDs, fromArg, toArg)
	if err != nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics latency: %w", err)
	}
	defer latRows.Close()
	for latRows.Next() {
		var d string
		var minutes int64
		if err := latRows.Scan(&d, &minutes); err != nil {
			return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics latency scan: %w", err)
		}
		m := minutes
		day(d).MedianVerifyLatencyMinutes = &m
	}
	if err := latRows.Err(); err != nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics latency rows: %w", err)
	}

	out := domain.ExecutionAnalytics{Days: make([]domain.ExecutionDay, 0, len(days))}
	keys := make([]string, 0, len(days))
	for k := range days {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.Days = append(out.Days, *days[k])
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Experiment analytics
// ---------------------------------------------------------------------------

// projection-review: membership=issue rows of the at-most-one live EXPERIMENT issue per (tenant, park, feed_day), same natural key as the directed read; group_key=(feed_day, feed_item_key) on the one aggregated side — the kg SUM and the label MAX range over exactly the grouped rows, no ratio or cap compares across key sets; join_cardinality=issues to rows 1:N with the rows side aggregated directly, no second joined side to fan out; pagination=none, whole-window aggregate with no limit/offset input; scope=tenant_id plus the caller's authorized park set. Head counts on experiment rows are informational and deliberately absent from this read
//
// scale-guard:ignore: 5k-50k-envelope — bounded windowed aggregate, same shape
// as the directed rollup above.
const experimentAnalyticsSQL = `
WITH iss AS (
    SELECT feed_direction_issue_id, feed_day
    FROM feed_direction_issues
    WHERE tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
      AND feed_day BETWEEN $3 AND $4
      AND state IN ('issued', 'amended', 'locked')
      AND workflow = 'experiment'
)
SELECT i.feed_day::text,
       MAX(r.feed_item_label)               AS feed_item_label,
       r.feed_item_key,
       COALESCE(SUM(r.quantity_kg), 0)::text AS kg
FROM iss i
JOIN feed_direction_issue_rows r
  ON r.tenant_id = $1
 AND r.feed_direction_issue_id = i.feed_direction_issue_id
GROUP BY i.feed_day, r.feed_item_key
ORDER BY i.feed_day, r.feed_item_key`

// projection-review: membership=the selected day's experiment sheet pens — issue rows of the at-most-one live EXPERIMENT issue per (tenant, park, feed_day), collapsed to DISTINCT (shed_id, partition_key) in the pen CTE, the same derivation the operator worklist uses; group_key=(shed_id, partition_key) on both sides of the LEFT JOIN — completions carry a UNIQUE (tenant, park, shed, partition_key, target_date, workflow) natural key, so the join is at most 1:1 per pen and can never fan out; join_cardinality=pens LEFT JOIN completions 1:0..1; pagination=none, one day's experiment pen list is bounded by the sheet; scope=tenant_id plus the caller's authorized park set on BOTH sides, workflow pinned 'experiment'.
//
// scale-guard:ignore: 5k-50k-envelope — one bounded day of sheet pens joined
// 1:1 to completions, canonical-indexed-SQL default.
const experimentWastagePensSQL = `
WITH iss AS (
    SELECT feed_direction_issue_id
    FROM feed_direction_issues
    WHERE tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
      AND feed_day = $3
      AND state IN ('issued', 'amended', 'locked')
      AND workflow = 'experiment'
),
pen AS (
    SELECT r.shed_id, r.partition_key,
           MAX(r.park_label)                              AS park_label,
           MAX(r.shed_label)                              AS shed_label,
           MAX(COALESCE(r.partition_label, ''))           AS partition_label
    FROM iss i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1
     AND r.feed_direction_issue_id = i.feed_direction_issue_id
    GROUP BY r.shed_id, r.partition_key
)
SELECT p.shed_id::text,
       p.park_label,
       p.shed_label,
       p.partition_label,
       COALESCE(c.status, '')                             AS lifecycle_status,
       COALESCE(c.wastage_kg::text, '')                   AS wastage_kg
FROM pen p
LEFT JOIN feed_wastage_completions c
  ON c.tenant_id = $1
 AND c.shed_id = p.shed_id
 AND c.partition_key = p.partition_key
 AND c.target_date = $3
 AND c.workflow = 'experiment'
 AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR c.park_id = ANY ($2::uuid[]))
ORDER BY p.park_label, p.shed_label, p.partition_key`

// ExperimentAnalytics serves the trial arms' authored kg series.
func (r *Repository) ExperimentAnalytics(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.ExperimentAnalytics, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	from, to := domain.ClampAnalyticsWindow(q.DateFrom, q.DateTo)
	var parkIDs []uuid.UUID
	if len(q.ParkIDs) > 0 {
		parkIDs = q.ParkIDs
	}
	rows, err := r.pool.Query(ctx, experimentAnalyticsSQL, tenantID, parkIDs, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return domain.ExperimentAnalytics{}, fmt.Errorf("feed analytics experiment: %w", err)
	}
	defer rows.Close()
	out := domain.ExperimentAnalytics{Items: []domain.ExperimentDayItem{}}
	for rows.Next() {
		var it domain.ExperimentDayItem
		if err := rows.Scan(&it.FeedDay, &it.FeedItemLabel, &it.FeedItemKey, &it.Kg); err != nil {
			return domain.ExperimentAnalytics{}, fmt.Errorf("feed analytics experiment scan: %w", err)
		}
		out.Items = append(out.Items, it)
	}
	if err := rows.Err(); err != nil {
		return domain.ExperimentAnalytics{}, fmt.Errorf("feed analytics experiment rows: %w", err)
	}
	rows.Close()

	// The per-pen wastage table describes ONE selected business day. The
	// handler defaults it to TODAY (Asia/Kolkata) even though the kg window
	// ends yesterday: wastage is collected live DURING the feed day, and
	// leadership watching the screen at 4pm wants today's leftovers as they
	// land, not tomorrow.
	wastageDay := q.WastageDay
	if wastageDay.IsZero() {
		wastageDay = to
	}
	out.WastageDay = wastageDay.Format("2006-01-02")
	wrows, err := r.pool.Query(ctx, experimentWastagePensSQL, tenantID, parkIDs, out.WastageDay)
	if err != nil {
		return domain.ExperimentAnalytics{}, fmt.Errorf("feed analytics experiment wastage: %w", err)
	}
	defer wrows.Close()
	out.WastagePens = []domain.ExperimentWastagePen{}
	for wrows.Next() {
		var p domain.ExperimentWastagePen
		if err := wrows.Scan(&p.ShedID, &p.ParkLabel, &p.ShedLabel, &p.PartitionLabel, &p.LifecycleStatus, &p.WastageKg); err != nil {
			return domain.ExperimentAnalytics{}, fmt.Errorf("feed analytics experiment wastage scan: %w", err)
		}
		// Canonical composition, never hand-rolled (operational-location rule):
		// bare shed when undivided, "Castro 1" / "Godel 2 - Part 1" when penned.
		p.OperationalLocationDisplay = oploc.OperationalLocation{
			ShedName:       p.ShedLabel,
			PartitionLabel: p.PartitionLabel,
		}.Display()
		out.WastagePens = append(out.WastagePens, p)
	}
	if err := wrows.Err(); err != nil {
		return domain.ExperimentAnalytics{}, fmt.Errorf("feed analytics experiment wastage rows: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Stock & expenditure
// ---------------------------------------------------------------------------

// projection-review: membership=feed_purchases at its (tenant, farm_label, feed_item_key, batch_no) natural key, and locked feed_direction_issue_rows reached through the at-most-one live issue per (tenant, park, feed_day, workflow); group_key=(farm_label, feed_item_key) on every side — purchases, depletion and the recent-day average all collapse to the farm-item before joining (the sheet side collapses to (park_id, feed_item_key) and one farm_label resolves to exactly one park), so the three sides meet strictly 1:1; join_cardinality=bought JOIN directed 1:1, LEFT JOIN recent 1:0..1, each pre-aggregated to one row per farm-item; pagination=none, a tenant's feed catalog across its farms is a bounded card list; scope=tenant_id everywhere plus the caller's authorized park set on both purchases and sheets. Both workflows deplete stock — experiment feed leaves the same store.
//
// PER-FARM GRAIN (maintainer decision 2026-08-21): each farm keeps its own
// physical feed store, so a tenant-wide balance/days-left is a number nobody's
// store holds. Every stock card is one (farm, item) and names its farm.
//
// scale-guard:ignore: 5k-50k-envelope — bounded per-farm-item aggregates over the
// small purchase ledger and windowed locked sheets, canonical-indexed-SQL default.
const stockItemsSQL = `
WITH bought AS (
    SELECT farm_label, feed_item_key,
           MAX(feed_item_label)                          AS feed_item_label,
           MIN(park_id::text)                            AS park_id_text,
           SUM(quantity_kg - consumed_at_import_kg)      AS net_kg,
           MAX(batch_no)                                 AS latest_batch,
           MIN(depletes_from)                            AS depletes_from
    FROM feed_purchases
    WHERE tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
    GROUP BY farm_label, feed_item_key
),
locked_cells AS (
    SELECT i.park_id, r.feed_item_key, i.feed_day, SUM(r.quantity_kg) AS kg
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1 AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR i.park_id = ANY ($2::uuid[]))
      AND i.state = 'locked'
    GROUP BY i.park_id, r.feed_item_key, i.feed_day
),
directed AS (
    SELECT b.farm_label, b.feed_item_key, COALESCE(SUM(lc.kg), 0) AS kg
    FROM bought b
    LEFT JOIN locked_cells lc
      ON b.park_id_text IS NOT NULL
     AND lc.park_id = b.park_id_text::uuid
     AND lc.feed_item_key = b.feed_item_key
     AND lc.feed_day >= b.depletes_from
    GROUP BY b.farm_label, b.feed_item_key
),
recent AS (
    SELECT park_id, feed_item_key, AVG(kg) AS avg_kg
    FROM (
        SELECT park_id, feed_item_key, kg,
               ROW_NUMBER() OVER (PARTITION BY park_id, feed_item_key ORDER BY feed_day DESC) AS rn
        FROM locked_cells
    ) ranked
    WHERE rn <= 3
    GROUP BY park_id, feed_item_key
)
SELECT b.farm_label,
       b.feed_item_label,
       b.feed_item_key,
       round(b.net_kg - d.kg, 1)::text                    AS balance_kg,
       COALESCE(round(r.avg_kg, 1)::text, '')             AS avg_daily_kg,
       CASE WHEN COALESCE(r.avg_kg, 0) > 0
            THEN floor((b.net_kg - d.kg) / r.avg_kg)::bigint
       END                                                AS days_left,
       b.latest_batch
FROM bought b
JOIN directed d USING (farm_label, feed_item_key)
LEFT JOIN recent r
  ON b.park_id_text IS NOT NULL
 AND r.park_id = b.park_id_text::uuid
 AND r.feed_item_key = b.feed_item_key
ORDER BY days_left NULLS LAST, b.feed_item_label, b.farm_label`

// Expenditure: each (day, farm, item)'s directed kg priced at that farm's most
// recent load rate on or before that day. Park-less purchase rows have no
// execution source to price and are deliberately excluded from spend.
const stockExpenditureSQL = `
WITH day_item AS (
    SELECT i.feed_day, i.park_id, r.feed_item_key, SUM(r.quantity_kg) AS kg
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1 AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR i.park_id = ANY ($2::uuid[]))
      AND i.state IN ('issued', 'amended', 'locked')
      AND i.feed_day BETWEEN $3 AND $4
    GROUP BY i.feed_day, i.park_id, r.feed_item_key
)
SELECT di.feed_day::text,
       round(SUM(di.kg * price.per_kg), 0)::text AS rupees
FROM day_item di
JOIN LATERAL (
    SELECT COALESCE(p.per_kg_cost, p.total_cost / NULLIF(p.quantity_kg, 0)) AS per_kg
    FROM feed_purchases p
    WHERE p.tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR p.park_id = ANY ($2::uuid[]))
      AND p.park_id = di.park_id
      AND p.feed_item_key = di.feed_item_key
      AND p.purchase_date <= di.feed_day
    ORDER BY p.purchase_date DESC, p.batch_no DESC
    LIMIT 1
) price ON price.per_kg IS NOT NULL
GROUP BY di.feed_day
ORDER BY di.feed_day`

// Spend periods: same day_item × same-farm latest-load pricing as the daily
// series, one scan from Jan 1 of the current IST year, bucketed by fixed period
// starts. Every bucket's numerator and denominator (none — plain sums) range
// over the same priced (day, farm, item) set; feed_day < today keeps the still-executing day
// out, matching every other figure on the page.
const stockSpendSQL = `
WITH day_item AS (
    SELECT i.feed_day, i.park_id, r.feed_item_key, SUM(r.quantity_kg) AS kg
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1 AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR i.park_id = ANY ($2::uuid[]))
      AND i.state IN ('issued', 'amended', 'locked')
      AND i.feed_day >= date_trunc('year', $3::date)::date
      AND i.feed_day < $3::date
    GROUP BY i.feed_day, i.park_id, r.feed_item_key
),
priced AS (
    SELECT di.feed_day, di.kg * price.per_kg AS spend
    FROM day_item di
    JOIN LATERAL (
        SELECT COALESCE(p.per_kg_cost, p.total_cost / NULLIF(p.quantity_kg, 0)) AS per_kg
        FROM feed_purchases p
        WHERE p.tenant_id = $1
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR p.park_id = ANY ($2::uuid[]))
          AND p.park_id = di.park_id
          AND p.feed_item_key = di.feed_item_key
          AND p.purchase_date <= di.feed_day
        ORDER BY p.purchase_date DESC, p.batch_no DESC
        LIMIT 1
    ) price ON price.per_kg IS NOT NULL
)
SELECT
  COALESCE(round(SUM(spend) FILTER (WHERE feed_day >= date_trunc('week',  $3::date)::date), 0), 0)::text,
  COALESCE(round(SUM(spend) FILTER (WHERE feed_day >= date_trunc('month', $3::date)::date), 0), 0)::text,
  COALESCE(round(SUM(spend) FILTER (WHERE feed_day >= $3::date - 91), 0), 0)::text,
  COALESCE(round(SUM(spend), 0), 0)::text
FROM priced`

// Per-farm Mesha-concentrate purchase/consumption table (Stock tab).
//
// projection-review: membership=feed_purchases at its (tenant, farm_label, feed_item_key, batch_no) natural key, filtered to the four MeshaConcentrateStockKeys; group_key=(farm_label, feed_item_key) on both purchase sides — loads GROUP BY that pair and last_load is DISTINCT ON the same pair so they meet exactly 1:1, while the directed side collapses locked issue-row cells to (park_id, feed_item_key) before joining and one farm_label resolves to exactly one park (the importer maps CBE/CPT to the tenant's park locations); join_cardinality=loads JOIN last_load 1:1, LEFT JOIN directed 1:0..1, no side left unaggregated; pagination=none — four items across a tenant's farms is a bounded table with no limit/offset input; scope=tenant_id everywhere plus the caller's authorized park set on both purchases and sheets.
//
// scale-guard:ignore: 5k-50k-envelope — bounded four-item aggregate over the
// small purchase ledger and locked sheets, canonical-indexed-SQL default.
const stockFarmItemsSQL = `
WITH loads AS (
    SELECT farm_label, feed_item_key,
           MAX(feed_item_label) AS feed_item_label,
           MIN(park_id::text)   AS park_id_text,
           MIN(purchase_date)   AS first_purchase,
           SUM(quantity_kg - consumed_at_import_kg) AS net_kg,
           MIN(depletes_from)    AS depletes_from
    FROM feed_purchases
    WHERE tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
      AND feed_item_key = ANY ($3::text[])
    GROUP BY farm_label, feed_item_key
),
last_load AS (
    SELECT DISTINCT ON (farm_label, feed_item_key)
           farm_label, feed_item_key,
           batch_no, purchase_date, quantity_kg, vendor
    FROM feed_purchases
    WHERE tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
      AND feed_item_key = ANY ($3::text[])
    ORDER BY farm_label, feed_item_key, purchase_date DESC, batch_no DESC
),
locked_cells AS (
    SELECT i.park_id, r.feed_item_key, i.feed_day, SUM(r.quantity_kg) AS kg
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1 AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR i.park_id = ANY ($2::uuid[]))
      AND i.state = 'locked'
      AND r.feed_item_key = ANY ($3::text[])
    GROUP BY i.park_id, r.feed_item_key, i.feed_day
),
directed AS (
    SELECT park_id, feed_item_key,
           MIN(feed_day)                  AS first_directed_day,
           AVG(kg) FILTER (WHERE rn <= 3) AS recent_avg_kg
    FROM (
        SELECT park_id, feed_item_key, feed_day, kg,
               ROW_NUMBER() OVER (PARTITION BY park_id, feed_item_key ORDER BY feed_day DESC) AS rn
        FROM locked_cells
    ) ranked
    GROUP BY park_id, feed_item_key
),
depletion AS (
    SELECT l.farm_label,
           l.feed_item_key,
           COALESCE(SUM(lc.kg), 0) AS total_directed_kg
    FROM loads l
    LEFT JOIN locked_cells lc
      ON l.park_id_text IS NOT NULL
     AND lc.park_id = l.park_id_text::uuid
     AND lc.feed_item_key = l.feed_item_key
     AND lc.feed_day >= l.depletes_from
    GROUP BY l.farm_label, l.feed_item_key
),
stock_balance AS (
    SELECT l.farm_label,
           l.feed_item_key,
           round(l.net_kg - COALESCE(dep.total_directed_kg, 0), 1) AS ledger_stock_kg
    FROM loads l
    LEFT JOIN depletion dep USING (farm_label, feed_item_key)
)
SELECT l.farm_label,
       l.feed_item_label,
       l.feed_item_key,
       l.first_purchase::text,
       COALESCE(d.first_directed_day::text, '')      AS first_directed_day,
       COALESCE(round(d.recent_avg_kg, 1)::text, '') AS avg_daily_kg,
       ll.batch_no,
       ll.purchase_date::text,
       round(ll.quantity_kg, 1)::text AS last_quantity_kg,
       ll.vendor,
       sb.ledger_stock_kg::text
FROM loads l
JOIN last_load ll USING (farm_label, feed_item_key)
LEFT JOIN directed d
  ON l.park_id_text IS NOT NULL
 AND d.park_id = l.park_id_text::uuid
 AND d.feed_item_key = l.feed_item_key
LEFT JOIN stock_balance sb USING (farm_label, feed_item_key)
ORDER BY l.feed_item_label, l.farm_label`

// StockAnalytics serves the stock cards and the expenditure series.
func (r *Repository) StockAnalytics(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.StockAnalytics, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	from, to := domain.ClampAnalyticsWindow(q.DateFrom, q.DateTo)
	var parkIDs []uuid.UUID
	if len(q.ParkIDs) > 0 {
		parkIDs = q.ParkIDs
	}
	out := domain.StockAnalytics{Items: []domain.StockItem{}, Expenditure: []domain.ExpenditureDay{}}

	itemRows, err := r.pool.Query(ctx, stockItemsSQL, tenantID, parkIDs)
	if err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock items: %w", err)
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var it domain.StockItem
		if err := itemRows.Scan(&it.FarmLabel, &it.FeedItemLabel, &it.FeedItemKey, &it.BalanceKg, &it.AvgDailyKg, &it.DaysLeft, &it.LatestBatchNo); err != nil {
			return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock scan: %w", err)
		}
		it.LowStock = it.DaysLeft != nil && *it.DaysLeft < domain.LowStockDays
		out.Items = append(out.Items, it)
	}
	if err := itemRows.Err(); err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock rows: %w", err)
	}

	out.FarmItems = []domain.StockFarmItem{}
	farmRows, err := r.pool.Query(ctx, stockFarmItemsSQL, tenantID, parkIDs, domain.MeshaConcentrateStockKeys)
	if err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock farm items: %w", err)
	}
	defer farmRows.Close()
	for farmRows.Next() {
		var fi domain.StockFarmItem
		if err := farmRows.Scan(
			&fi.FarmLabel, &fi.FeedItemLabel, &fi.FeedItemKey,
			&fi.FirstPurchaseDate, &fi.FirstDirectedDay, &fi.AvgDailyKg,
			&fi.LastLoadBatchNo, &fi.LastLoadDate, &fi.LastLoadQuantityKg,
			&fi.LastLoadVendor,
			&fi.LedgerStockKg,
		); err != nil {
			return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock farm scan: %w", err)
		}
		out.FarmItems = append(out.FarmItems, fi)
	}
	if err := farmRows.Err(); err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock farm rows: %w", err)
	}

	expRows, err := r.pool.Query(ctx, stockExpenditureSQL, tenantID, parkIDs, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics expenditure: %w", err)
	}
	defer expRows.Close()
	for expRows.Next() {
		var d domain.ExpenditureDay
		if err := expRows.Scan(&d.FeedDay, &d.Rupees); err != nil {
			return domain.StockAnalytics{}, fmt.Errorf("feed analytics expenditure scan: %w", err)
		}
		out.Expenditure = append(out.Expenditure, d)
	}
	if err := expRows.Err(); err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics expenditure rows: %w", err)
	}

	today := biztime.BusinessDate(time.Now())
	if err := r.pool.QueryRow(ctx, stockSpendSQL, tenantID, parkIDs, today).
		Scan(&out.Spend.ThisWeek, &out.Spend.ThisMonth, &out.Spend.ThreeMonths, &out.Spend.ThisYear); err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics spend summary: %w", err)
	}
	return out, nil
}
