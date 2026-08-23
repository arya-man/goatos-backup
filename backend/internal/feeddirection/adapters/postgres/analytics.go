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

// projection-review: membership=feed_packing_verified_quantities at its (tenant_id, completion_id,
// feed_item_key) natural key, joined 1:1 to its owning feed_packing_completions row by (tenant_id,
// completion_id) -- the readings table's PK prefix -- restricted to status='completed' so only
// verdicts that stand are compared; group_key=the planned CTE groups issue-row cells to (feed_day,
// park_id, shed_id, partition_key, session_no, workflow, feed_item_key), exactly the completion's
// own natural-key coordinates plus the item, pre-aggregating the N ration-grain side (SUM of the
// already-rounded session quantities, the same summation BuildPackingRows does for the packer's
// worklist) BEFORE the join, so readings LEFT JOIN planned is 1:0..1 per reading and can never fan
// out; join_cardinality=readings:completions 1:1 by PK prefix, readings:locations 1:1 by the
// locations (tenant_id, location_id) PK for park and shed names (labels come from the completion's
// own canonical locations, so a reading whose planned row is absent -- "not on sheet" -- still
// names its farm and shed), readings:planned 1:0..1 by the full
// grain; producer unique columns (tenant, completion, feed_item_key) vs consumer match columns
// (feed_day=target_date, park, shed, partition_key, session_no, workflow, feed_item_key) -- the
// numerator (entered_kg) and the compared-against planned_kg both range over that one grain, no
// ratio spans key sets; pagination=none, whole-window mismatch list bounded by the park's pens x
// sessions x items x window days -- physical infrastructure, never herd size; scope=tenant_id on
// every table plus the caller's authorized park set on both sides.
//
// MISMATCHES BEYOND TOLERANCE ONLY (maintainer decision 2026-08-21, second same-day decision
// superseding the initial any-mismatch rule): a row pops only when |entered - planned| exceeds
// domain.PackingVarianceToleranceKg ($5), strictly greater-than so a difference of exactly 0.2 kg
// stays quiet. This comparison must NEVER reach a verifier surface: she enters blind, and the page
// serving this payload is leadership-gated.
//
// scale-guard:ignore: 5k-50k-envelope -- bounded windowed comparison over the
// same indexed date columns as the status counts above.
const executionPackingVarianceSQL = `
WITH readings AS (
    SELECT c.completion_id, c.target_date, c.park_id, c.shed_id, c.partition_key, c.session_no,
           c.workflow, q.feed_item_key, q.feed_item_label, q.entered_kg,
           lp.name                              AS park_label,
           ls.name                              AS shed_label,
           COALESCE(c.partition_label, '')      AS partition_label
    FROM feed_packing_verified_quantities q
    JOIN feed_packing_completions c
      ON c.tenant_id = q.tenant_id AND c.completion_id = q.completion_id
    JOIN locations lp
      ON lp.tenant_id = c.tenant_id AND lp.location_id = c.park_id
    JOIN locations ls
      ON ls.tenant_id = c.tenant_id AND ls.location_id = c.shed_id
    WHERE q.tenant_id = $1
      AND ($2::uuid[] IS NULL OR c.park_id = ANY ($2::uuid[]))
      AND c.target_date BETWEEN $3 AND $4
      AND c.status = 'completed'
),
planned AS (
    SELECT i.feed_day, i.park_id, r.shed_id, r.partition_key, r.session_no, r.workflow,
           r.feed_item_key,
           SUM(r.quantity_kg)                       AS planned_kg,
           MAX(r.session_label)                     AS session_label
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1
     AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1
      AND ($2::uuid[] IS NULL OR i.park_id = ANY ($2::uuid[]))
      AND i.feed_day BETWEEN $3 AND $4
      AND i.state IN ('issued', 'amended', 'locked')
      AND i.workflow IN ('normal', 'experiment')
    GROUP BY i.feed_day, i.park_id, r.shed_id, r.partition_key, r.session_no, r.workflow, r.feed_item_key
)
SELECT rd.target_date::text,
       rd.park_label,
       rd.shed_id::text,
       rd.shed_label,
       rd.partition_label,
       rd.session_no,
       COALESCE(p.session_label, ''),
       rd.workflow,
       rd.feed_item_key,
       rd.feed_item_label,
       COALESCE(p.planned_kg::text, ''),
       rd.entered_kg::text,
       (rd.entered_kg - COALESCE(p.planned_kg, 0))::text
FROM readings rd
LEFT JOIN planned p
  ON p.feed_day = rd.target_date
 AND p.park_id = rd.park_id
 AND p.shed_id = rd.shed_id
 AND p.partition_key = rd.partition_key
 AND p.session_no = rd.session_no
 AND p.workflow = rd.workflow
 AND p.feed_item_key = rd.feed_item_key
WHERE abs(rd.entered_kg - COALESCE(p.planned_kg, 0)) > $5
ORDER BY rd.target_date DESC, rd.park_label, rd.shed_label, rd.partition_label, rd.session_no, rd.feed_item_label`

const executionConsumptionSQL = `
WITH planned AS (
    SELECT i.feed_day,
           i.park_id,
           lp.name AS park_label,
           r.shed_id,
           ls.name AS shed_label,
           r.partition_key,
           COALESCE(MAX(r.partition_label), '') AS partition_label,
           r.session_no,
           r.workflow,
           r.feed_item_key,
           MAX(r.feed_item_label) AS feed_item_label,
           COALESCE(NULLIF(MAX(r.breed), ''), 'Unspecified') AS breed_label,
           CASE
             WHEN feed_config_norm(COALESCE(MAX(r.ration_group), MAX(r.shed_tag), '')) LIKE '%kid%' THEN 'Kid'
             ELSE 'Adult'
           END AS age_group,
           SUM(r.quantity_kg) AS target_kg
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1
     AND r.feed_direction_issue_id = i.feed_direction_issue_id
    JOIN locations lp
      ON lp.tenant_id = i.tenant_id AND lp.location_id = i.park_id
    JOIN locations ls
      ON ls.tenant_id = i.tenant_id AND ls.location_id = r.shed_id
    WHERE i.tenant_id = $1
      AND ($2::uuid[] IS NULL OR i.park_id = ANY ($2::uuid[]))
      AND i.feed_day BETWEEN $3 AND $4
      AND i.state IN ('issued', 'amended', 'locked')
      AND i.workflow IN ('normal', 'experiment')
      AND r.quantity_kg IS NOT NULL
    GROUP BY i.feed_day, i.park_id, lp.name, r.shed_id, ls.name, r.partition_key,
             r.session_no, r.workflow, r.feed_item_key
),
readings AS (
    SELECT c.target_date,
           c.park_id,
           c.shed_id,
           c.partition_key,
           c.session_no,
           c.workflow,
           q.feed_item_key,
           SUM(q.entered_kg) AS actual_kg
    FROM feed_packing_verified_quantities q
    JOIN feed_packing_completions c
      ON c.tenant_id = q.tenant_id AND c.completion_id = q.completion_id
    WHERE q.tenant_id = $1
      AND ($2::uuid[] IS NULL OR c.park_id = ANY ($2::uuid[]))
      AND c.target_date BETWEEN $3 AND $4
      AND c.status = 'completed'
    GROUP BY c.target_date, c.park_id, c.shed_id, c.partition_key, c.session_no, c.workflow, q.feed_item_key
),
comparison AS (
    SELECT p.feed_day,
           p.park_label,
           p.shed_id,
           p.shed_label,
           p.partition_label,
           p.breed_label,
           p.age_group,
           p.feed_item_key,
           p.feed_item_label,
           p.target_kg,
           COALESCE(r.actual_kg, 0) AS actual_kg,
           COALESCE(r.actual_kg, 0) - p.target_kg AS variance_kg
    FROM planned p
    LEFT JOIN readings r
      ON r.target_date = p.feed_day
     AND r.park_id = p.park_id
     AND r.shed_id = p.shed_id
     AND r.partition_key = p.partition_key
     AND r.session_no = p.session_no
     AND r.workflow = p.workflow
     AND r.feed_item_key = p.feed_item_key
)
SELECT 'row' AS kind,
       feed_day::text,
       park_label,
       shed_id::text,
       shed_label,
       partition_label,
       breed_label,
       age_group,
       feed_item_key,
       feed_item_label,
       target_kg::text,
       actual_kg::text,
       variance_kg::text,
       (abs(variance_kg) > $5)::text,
       ''::text,
       ''::text
FROM comparison
WHERE feed_day = $4::date
UNION ALL
SELECT 'trend' AS kind,
       feed_day::text,
       '' AS park_label,
       '' AS shed_id,
       '' AS shed_label,
       '' AS partition_label,
       '' AS breed_label,
       '' AS age_group,
       '' AS feed_item_key,
       '' AS feed_item_label,
       SUM(target_kg)::text,
       SUM(actual_kg)::text,
       SUM(variance_kg)::text,
       COUNT(*) FILTER (WHERE abs(variance_kg) > $5)::text,
       COUNT(*)::text,
       ''::text
FROM comparison
GROUP BY feed_day
ORDER BY 1, 2 DESC, 3, 4, 5, 6, 9`

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

	consRows, err := r.pool.Query(ctx, executionConsumptionSQL, tenantID, parkIDs, fromArg, toArg, domain.PackingVarianceToleranceKg)
	if err != nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics consumption comparison: %w", err)
	}
	defer consRows.Close()
	out.ConsumptionRows = []domain.FeedConsumptionRow{}
	out.ConsumptionTrend = []domain.FeedConsumptionTrendDay{}
	for consRows.Next() {
		var kind, feedDay, parkLabel, shedID, shedLabel, partitionLabel, breedLabel, ageGroup string
		var feedItemKey, feedItemLabel, targetKg, actualKg, varianceKg, flagText, comparedText, unused string
		if err := consRows.Scan(
			&kind, &feedDay, &parkLabel, &shedID, &shedLabel, &partitionLabel, &breedLabel, &ageGroup,
			&feedItemKey, &feedItemLabel, &targetKg, &actualKg, &varianceKg, &flagText, &comparedText, &unused,
		); err != nil {
			return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics consumption comparison scan: %w", err)
		}
		switch kind {
		case "row":
			v := domain.FeedConsumptionRow{
				FeedDay:        feedDay,
				ParkLabel:      parkLabel,
				ShedID:         shedID,
				ShedLabel:      shedLabel,
				PartitionLabel: partitionLabel,
				BreedLabel:     breedLabel,
				AgeGroup:       ageGroup,
				FeedItemKey:    feedItemKey,
				FeedItemLabel:  feedItemLabel,
				TargetKg:       targetKg,
				ActualKg:       actualKg,
				VarianceKg:     varianceKg,
				HasVariance:    flagText == "true",
			}
			v.OperationalLocationDisplay = oploc.OperationalLocation{
				ShedName:       v.ShedLabel,
				PartitionLabel: v.PartitionLabel,
			}.Display()
			out.ConsumptionRows = append(out.ConsumptionRows, v)
		case "trend":
			var varianceRows, comparedRows int64
			if flagText != "" {
				if _, err := fmt.Sscan(flagText, &varianceRows); err != nil {
					return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics consumption variance row count: %w", err)
				}
			}
			if comparedText != "" {
				if _, err := fmt.Sscan(comparedText, &comparedRows); err != nil {
					return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics consumption compared row count: %w", err)
				}
			}
			out.ConsumptionTrend = append(out.ConsumptionTrend, domain.FeedConsumptionTrendDay{
				FeedDay:      feedDay,
				TargetKg:     targetKg,
				ActualKg:     actualKg,
				VarianceRows: varianceRows,
				ComparedRows: comparedRows,
			})
		}
	}
	if err := consRows.Err(); err != nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics consumption comparison rows: %w", err)
	}
	sort.Slice(out.ConsumptionTrend, func(i, j int) bool {
		return out.ConsumptionTrend[i].FeedDay < out.ConsumptionTrend[j].FeedDay
	})

	varRows, err := r.pool.Query(ctx, executionPackingVarianceSQL, tenantID, parkIDs, fromArg, toArg, domain.PackingVarianceToleranceKg)
	if err != nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics packing variance: %w", err)
	}
	defer varRows.Close()
	out.PackingVariance = []domain.PackingVarianceRow{}
	for varRows.Next() {
		var v domain.PackingVarianceRow
		if err := varRows.Scan(
			&v.FeedDay, &v.ParkLabel, &v.ShedID, &v.ShedLabel, &v.PartitionLabel,
			&v.SessionNo, &v.SessionLabel, &v.Workflow, &v.FeedItemKey, &v.FeedItemLabel,
			&v.PlannedKg, &v.VerifiedKg, &v.VarianceKg,
		); err != nil {
			return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics packing variance scan: %w", err)
		}
		// Canonical composition, never hand-rolled (operational-location rule).
		v.OperationalLocationDisplay = oploc.OperationalLocation{
			ShedName:       v.ShedLabel,
			PartitionLabel: v.PartitionLabel,
		}.Display()
		out.PackingVariance = append(out.PackingVariance, v)
	}
	if err := varRows.Err(); err != nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics packing variance rows: %w", err)
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

// projection-review: membership=feed_purchases at its (tenant, farm_label, feed_item_key, batch_no) natural key, locked feed_direction_issue_rows reached through the at-most-one live issue per (tenant, park, feed_day, workflow), and feed_external_consumption at its (tenant, farm_label, feed_item_key, feed_day) natural key (sheet-tracked feeds GoatOS does not direct — UHT Milk); the two consumption sources pre-aggregate to the same (park_id, feed_item_key, feed_day) grain and the UNION ALL is re-grouped on that key, so a day contributes once; group_key=(farm_label, feed_item_key) on every side — purchases, depletion and the recent-day average all collapse to the farm-item before joining (the sheet side collapses to (park_id, feed_item_key) and one farm_label resolves to exactly one park), so the three sides meet strictly 1:1; join_cardinality=bought JOIN directed 1:1, LEFT JOIN recent 1:0..1, each pre-aggregated to one row per farm-item; pagination=none, a tenant's feed catalog across its farms is a bounded card list; scope=tenant_id everywhere plus the caller's authorized park set on both purchases and sheets. Both workflows deplete stock — experiment feed leaves the same store.
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
    -- Consumption from BOTH sources at one grain: locked-sheet directed kg,
    -- plus the feed_external_consumption ledger for sheet-tracked feeds GoatOS
    -- does not direct (UHT Milk; migration 000185). The outer GROUP BY
    -- collapses the union so a feed appearing in both sources on one day sums
    -- once per (park, item, day) — total consumed, never a duplicate row.
    SELECT park_id, feed_item_key, feed_day, SUM(kg) AS kg
    FROM (
        SELECT i.park_id, r.feed_item_key, i.feed_day, SUM(r.quantity_kg) AS kg
        FROM feed_direction_issues i
        JOIN feed_direction_issue_rows r
          ON r.tenant_id = $1 AND r.feed_direction_issue_id = i.feed_direction_issue_id
        WHERE i.tenant_id = $1
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR i.park_id = ANY ($2::uuid[]))
          AND i.state = 'locked'
        GROUP BY i.park_id, r.feed_item_key, i.feed_day
        UNION ALL
        SELECT x.park_id, x.feed_item_key, x.feed_day, SUM(x.quantity_kg) AS kg
        FROM feed_external_consumption x
        WHERE x.tenant_id = $1
          AND x.park_id IS NOT NULL
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR x.park_id = ANY ($2::uuid[]))
        GROUP BY x.park_id, x.feed_item_key, x.feed_day
    ) both_sources
    GROUP BY park_id, feed_item_key, feed_day
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
            THEN GREATEST(floor((b.net_kg - d.kg) / r.avg_kg), 0)::bigint
       END                                                AS days_left,
       b.latest_batch
FROM bought b
JOIN directed d USING (farm_label, feed_item_key)
LEFT JOIN recent r
  ON b.park_id_text IS NOT NULL
 AND r.park_id = b.park_id_text::uuid
 AND r.feed_item_key = b.feed_item_key
ORDER BY days_left NULLS LAST, b.feed_item_label, b.farm_label`

// Next-7-days requirement and cost (maintainer decision 2026-08-23), at
// (park, feed item) grain.
//
// Keyed on CONSUMPTION, not on the purchase ledger, so every feed the farm
// actually feeds gets a row -- sheet-directed feeds and the external ledger
// (UHT Milk) alike, including a feed never purchased at that park. Purchases
// only decorate the row with a balance and a rate.
//
// projection-review: membership=fed items at (park_id, feed_item_key) from
// locked feed_direction_issue_rows UNION feed_external_consumption, collapsed
// to one row per (park_id, feed_item_key, feed_day) BEFORE ranking so a feed
// carried by both sources on one day averages once; group_key=(park_id,
// feed_item_key) on every side -- recent/first_day GROUP BY that pair, the
// purchase side aggregates feed_purchases to the same pair before joining, and
// the rate LATERAL returns one row by construction; join_cardinality=fed LEFT
// JOIN purchased 1:0..1, LEFT JOIN LATERAL rate 1:0..1, no side left
// unaggregated -- and required_kg and required_cost range over the IDENTICAL
// (park_id, feed_item_key) key set, cost being a scalar multiple of the same
// avg rather than a differently-grouped sum; pagination=none, a tenant's feeds
// across its parks is a bounded table with no limit/offset input, so no
// summary can disagree with a page; scope=tenant_id everywhere plus the
// caller's authorized park set on consumption and purchases alike.
//
// scale-guard:ignore: 5k-50k-envelope -- bounded per-(park,item) aggregate over
// locked sheets and the small purchase ledger, canonical-indexed-SQL default.
const stockForecastSQL = `
WITH fed_days AS (
    SELECT park_id, feed_item_key, feed_day, SUM(kg) AS kg,
           MAX(feed_item_label) AS feed_item_label
    FROM (
        SELECT i.park_id, r.feed_item_key, i.feed_day,
               SUM(r.quantity_kg) AS kg,
               MAX(r.feed_item_label) AS feed_item_label
        FROM feed_direction_issues i
        JOIN feed_direction_issue_rows r
          ON r.tenant_id = $1 AND r.feed_direction_issue_id = i.feed_direction_issue_id
        WHERE i.tenant_id = $1
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR i.park_id = ANY ($2::uuid[]))
          AND i.state = 'locked'
        GROUP BY i.park_id, r.feed_item_key, i.feed_day
        UNION ALL
        SELECT x.park_id, x.feed_item_key, x.feed_day,
               SUM(x.quantity_kg) AS kg,
               MAX(x.feed_item_label) AS feed_item_label
        FROM feed_external_consumption x
        WHERE x.tenant_id = $1
          AND x.park_id IS NOT NULL
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR x.park_id = ANY ($2::uuid[]))
        GROUP BY x.park_id, x.feed_item_key, x.feed_day
    ) both_sources
    GROUP BY park_id, feed_item_key, feed_day
),
recent AS (
    SELECT park_id, feed_item_key,
           AVG(kg)              AS avg_kg,
           MAX(feed_item_label) AS feed_item_label
    FROM (
        SELECT park_id, feed_item_key, feed_day, kg, feed_item_label,
               ROW_NUMBER() OVER (PARTITION BY park_id, feed_item_key ORDER BY feed_day DESC) AS rn
        FROM fed_days
    ) ranked
    WHERE rn <= 3
    GROUP BY park_id, feed_item_key
),
-- Ledger balance at the SAME (park, item) grain the stock cards use: purchased
-- net of import-time consumption, minus everything fed since the bootstrap
-- cutoff. Park-less purchase rows have no park to attribute to and are excluded,
-- exactly as the expenditure series excludes them.
purchased AS (
    SELECT p.park_id, p.feed_item_key,
           SUM(p.quantity_kg - p.consumed_at_import_kg) AS net_kg,
           MIN(p.depletes_from)                         AS depletes_from
    FROM feed_purchases p
    WHERE p.tenant_id = $1
      AND p.park_id IS NOT NULL
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR p.park_id = ANY ($2::uuid[]))
    GROUP BY p.park_id, p.feed_item_key
),
depleted AS (
    SELECT pu.park_id, pu.feed_item_key,
           pu.net_kg - COALESCE(SUM(fd.kg), 0) AS balance_kg
    FROM purchased pu
    LEFT JOIN fed_days fd
      ON fd.park_id = pu.park_id
     AND fd.feed_item_key = pu.feed_item_key
     AND fd.feed_day >= pu.depletes_from
    GROUP BY pu.park_id, pu.feed_item_key, pu.net_kg
)
SELECT lp.name                                            AS farm_label,
       COALESCE(NULLIF(r.feed_item_label, ''), r.feed_item_key) AS feed_item_label,
       r.feed_item_key,
       round(r.avg_kg, 1)::text                           AS avg_daily_kg,
       round(r.avg_kg * $3::numeric, 1)::text             AS required_kg,
       COALESCE(round(d.balance_kg, 1)::text, '')         AS stock_kg,
       CASE WHEN d.balance_kg IS NOT NULL
            THEN round(GREATEST(r.avg_kg * $3::numeric - d.balance_kg, 0), 1)::text
            ELSE '' END                                   AS shortfall_kg,
       COALESCE(round(rate.per_kg, 2)::text, '')          AS per_kg_cost,
       COALESCE(round(r.avg_kg * $3::numeric * rate.per_kg, 0)::text, '') AS required_cost
FROM recent r
JOIN locations lp
  ON lp.tenant_id = $1 AND lp.location_id = r.park_id
LEFT JOIN depleted d
  ON d.park_id = r.park_id AND d.feed_item_key = r.feed_item_key
LEFT JOIN LATERAL (
    SELECT COALESCE(p.per_kg_cost, p.total_cost / NULLIF(p.quantity_kg, 0)) AS per_kg
    FROM feed_purchases p
    WHERE p.tenant_id = $1
      AND p.park_id = r.park_id
      AND p.feed_item_key = r.feed_item_key
      AND COALESCE(p.per_kg_cost, p.total_cost / NULLIF(p.quantity_kg, 0)) IS NOT NULL
    ORDER BY p.purchase_date DESC, p.batch_no DESC
    LIMIT 1
) rate ON TRUE
WHERE r.avg_kg > 0
ORDER BY lp.name, feed_item_label`

// Expenditure: each (day, farm, item)'s directed kg — plus external
// consumption of sheet-tracked feeds (UHT Milk) — priced at that farm's most
// recent load rate on or before that day, matching the sheet's daily feed
// cost, which has always included the milk. Park-less purchase rows have no
// execution source to price and are deliberately excluded from spend.
const stockExpenditureSQL = `
WITH day_item AS (
    SELECT feed_day, park_id, feed_item_key, SUM(kg) AS kg
    FROM (
        SELECT i.feed_day, i.park_id, r.feed_item_key, SUM(r.quantity_kg) AS kg
        FROM feed_direction_issues i
        JOIN feed_direction_issue_rows r
          ON r.tenant_id = $1 AND r.feed_direction_issue_id = i.feed_direction_issue_id
        WHERE i.tenant_id = $1
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR i.park_id = ANY ($2::uuid[]))
          AND i.state IN ('issued', 'amended', 'locked')
          AND i.feed_day BETWEEN $3 AND $4
        GROUP BY i.feed_day, i.park_id, r.feed_item_key
        UNION ALL
        SELECT x.feed_day, x.park_id, x.feed_item_key, SUM(x.quantity_kg) AS kg
        FROM feed_external_consumption x
        WHERE x.tenant_id = $1
          AND x.park_id IS NOT NULL
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR x.park_id = ANY ($2::uuid[]))
          AND x.feed_day BETWEEN $3 AND $4
        GROUP BY x.feed_day, x.park_id, x.feed_item_key
    ) both_sources
    GROUP BY feed_day, park_id, feed_item_key
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
    SELECT feed_day, park_id, feed_item_key, SUM(kg) AS kg
    FROM (
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
        UNION ALL
        SELECT x.feed_day, x.park_id, x.feed_item_key, SUM(x.quantity_kg) AS kg
        FROM feed_external_consumption x
        WHERE x.tenant_id = $1
          AND x.park_id IS NOT NULL
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR x.park_id = ANY ($2::uuid[]))
          AND x.feed_day >= date_trunc('year', $3::date)::date
          AND x.feed_day < $3::date
        GROUP BY x.feed_day, x.park_id, x.feed_item_key
    ) both_sources
    GROUP BY feed_day, park_id, feed_item_key
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
           batch_no, purchase_date, quantity_kg, vendor, total_cost, per_kg_cost
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
    LEFT JOIN depletion dep
      ON dep.farm_label = l.farm_label
     AND dep.feed_item_key = l.feed_item_key
)
-- projection-review: membership=feed_purchases at (tenant, farm_label, feed_item_key, batch_no)
-- filtered to MeshaConcentrateStockKeys; group_key=(farm_label, feed_item_key) on every side,
-- loads GROUP BY that pair and last_load is DISTINCT ON the same pair; join_cardinality=loads
-- JOIN last_load 1:1, LEFT JOIN directed 1:0..1, LEFT JOIN stock_balance 1:0..1, no side left
-- unaggregated, and weekly_required_kg is a scalar multiple of the same recent_avg_kg rather than
-- a differently-grouped sum; pagination=none, four items across a tenant's farms is bounded with
-- no limit/offset input; scope=tenant_id everywhere plus the caller's authorized park set.
SELECT l.farm_label,
       l.feed_item_label,
       l.feed_item_key,
       l.first_purchase::text,
       COALESCE(d.first_directed_day::text, '')      AS first_directed_day,
       COALESCE(round(d.recent_avg_kg, 1)::text, '') AS avg_daily_kg,
       COALESCE(round(d.recent_avg_kg * 7, 1)::text, '') AS weekly_required_kg,
       ll.batch_no,
       ll.purchase_date::text,
       round(ll.quantity_kg, 1)::text AS last_quantity_kg,
       ll.vendor,
       COALESCE(round(ll.total_cost, 0)::text, '') AS last_total_cost,
       COALESCE(round(ll.per_kg_cost, 2)::text, '') AS last_per_kg_cost,
       sb.ledger_stock_kg::text
FROM loads l
JOIN last_load ll
  ON ll.farm_label = l.farm_label
 AND ll.feed_item_key = l.feed_item_key
LEFT JOIN directed d
  ON l.park_id_text IS NOT NULL
 AND d.park_id = l.park_id_text::uuid
 AND d.feed_item_key = l.feed_item_key
LEFT JOIN stock_balance sb
  ON sb.farm_label = l.farm_label
 AND sb.feed_item_key = l.feed_item_key
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
			&fi.FirstPurchaseDate, &fi.FirstDirectedDay, &fi.AvgDailyKg, &fi.WeeklyRequiredKg,
			&fi.LastLoadBatchNo, &fi.LastLoadDate, &fi.LastLoadQuantityKg,
			&fi.LastLoadVendor, &fi.LastLoadTotalCost, &fi.LastLoadPerKgCost,
			&fi.LedgerStockKg,
		); err != nil {
			return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock farm scan: %w", err)
		}
		out.FarmItems = append(out.FarmItems, fi)
	}
	if err := farmRows.Err(); err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock farm rows: %w", err)
	}

	// projection-review: membership=stockForecastSQL's fed (park_id, feed_item_key) set;
	// group_key=(park_id, feed_item_key), one row per pair straight from the query with no
	// client-side regrouping; join_cardinality=1:1 row-to-struct, nothing fanned out here;
	// pagination=none, the whole bounded result is scanned; scope=tenantID plus parkIDs
	// passed straight through to the query.
	out.Forecast = []domain.StockForecastItem{}
	fcRows, err := r.pool.Query(ctx, stockForecastSQL, tenantID, parkIDs, domain.StockForecastDays)
	if err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock forecast: %w", err)
	}
	defer fcRows.Close()
	for fcRows.Next() {
		var f domain.StockForecastItem
		if err := fcRows.Scan(
			&f.FarmLabel, &f.FeedItemLabel, &f.FeedItemKey,
			&f.AvgDailyKg, &f.RequiredKg, &f.StockKg, &f.ShortfallKg,
			&f.PerKgCost, &f.RequiredCost,
		); err != nil {
			return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock forecast scan: %w", err)
		}
		out.Forecast = append(out.Forecast, f)
	}
	if err := fcRows.Err(); err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics stock forecast rows: %w", err)
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
