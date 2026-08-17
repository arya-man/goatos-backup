package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

var _ ports.DirectedAnalyticsReader = (*Repository)(nil)

// Feed Analytics rollup over the frozen sheet.
//
// projection-review: producer unique key = feed_direction_issue_rows natural key
// (tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no,
// shed_tag_key, breed_key, feed_item_key); consumer group keys below are
// (feed_day, feed_item) after first collapsing to the PEN-GRAIN
// (shed_id, partition_key, shed_tag_key, breed_key) per day×item.
// Join multiplicity: issues → rows is 1:N by feed_direction_issue_id, and the
// live-issue partial unique index (tenant, park, feed_day, workflow) guarantees
// at most ONE normal-workflow issue per park-day, so a day's cells join exactly
// once. Ratio key sets: PerHeadGrams divides SUM(quantity_kg) by SUM(head_count)
// where BOTH range over the same grain set — the collapsed pen-grains of that
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
// feed items. Ratio key sets: the day's kg ranges over all resolved cells, the
// day's heads over the distinct pen-grains — both keyed by feed_day alone.
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
