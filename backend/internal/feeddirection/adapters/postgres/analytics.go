package postgres

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
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
      AND ($2::uuid[] IS NULL OR park_id = ANY ($2::uuid[]))
      AND feed_day BETWEEN $3 AND $4
      AND state IN ('issued', 'amended', 'locked')
      AND workflow = 'normal'
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
      AND ($2::uuid[] IS NULL OR park_id = ANY ($2::uuid[]))
      AND feed_day BETWEEN $3 AND $4
      AND state IN ('issued', 'amended', 'locked')
      AND workflow = 'normal'
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
  AND ($2::uuid[] IS NULL OR park_id = ANY ($2::uuid[]))
  AND target_date BETWEEN $3 AND $4
GROUP BY target_date, status`

const executionTransportSQL = `
SELECT business_date::text AS d, status, COUNT(*)
FROM feed_transport_tasks
WHERE tenant_id = $1
  AND ($2::uuid[] IS NULL OR park_id = ANY ($2::uuid[]))
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
    WHERE tenant_id = $1 AND ($2::uuid[] IS NULL OR park_id = ANY ($2::uuid[]))
      AND verified_at IS NOT NULL
      AND (verified_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN $3 AND $4
    UNION ALL
    SELECT verified_at, created_at FROM feed_distribution_completions
    WHERE tenant_id = $1 AND ($2::uuid[] IS NULL OR park_id = ANY ($2::uuid[]))
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

// projection-review: membership=issue rows of the at-most-one live EXPERIMENT issue per (tenant, park, feed_day), same natural key as the directed read; group_key=(feed_day, experiment_arm) after collapsing to the pen-grain, so pens counts DISTINCT collapsed grains and kg sums resolved cells over the SAME grain set; join_cardinality=issues to rows 1:N pre-aggregated in the pen CTE before the arm GROUP BY; pagination=none, whole-window aggregate with no limit/offset input; scope=tenant_id plus the caller's authorized park set. Head counts on experiment rows are informational and deliberately absent from this read
//
// scale-guard:ignore: 5k-50k-envelope — bounded windowed aggregate, same shape
// as the directed rollup above.
const experimentAnalyticsSQL = `
WITH iss AS (
    SELECT feed_direction_issue_id, feed_day
    FROM feed_direction_issues
    WHERE tenant_id = $1
      AND ($2::uuid[] IS NULL OR park_id = ANY ($2::uuid[]))
      AND feed_day BETWEEN $3 AND $4
      AND state IN ('issued', 'amended', 'locked')
      AND workflow = 'experiment'
),
pen AS (
    SELECT i.feed_day,
           r.experiment_arm,
           r.shed_id, r.partition_key, r.shed_tag_key, r.breed_key,
           SUM(r.quantity_kg) AS grain_kg
    FROM iss i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1
     AND r.feed_direction_issue_id = i.feed_direction_issue_id
    GROUP BY i.feed_day, r.experiment_arm,
             r.shed_id, r.partition_key, r.shed_tag_key, r.breed_key
)
SELECT feed_day::text,
       experiment_arm,
       COALESCE(SUM(grain_kg), 0)::text AS absolute_kg,
       COUNT(*)                         AS pens
FROM pen
GROUP BY feed_day, experiment_arm
ORDER BY feed_day, experiment_arm`

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
	out := domain.ExperimentAnalytics{Arms: []domain.ExperimentDayArm{}}
	for rows.Next() {
		var a domain.ExperimentDayArm
		if err := rows.Scan(&a.FeedDay, &a.ExperimentArm, &a.AbsoluteKg, &a.Pens); err != nil {
			return domain.ExperimentAnalytics{}, fmt.Errorf("feed analytics experiment scan: %w", err)
		}
		out.Arms = append(out.Arms, a)
	}
	if err := rows.Err(); err != nil {
		return domain.ExperimentAnalytics{}, fmt.Errorf("feed analytics experiment rows: %w", err)
	}
	return out, nil
}
