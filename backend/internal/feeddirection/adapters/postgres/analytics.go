package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
// projection-review: membership=feed_direction_issue_rows at their natural key (tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, shed_tag_key, breed_key, feed_item_key), reached through the at-most-one live normal-workflow issue per (tenant, park, feed_day) enforced by feed_direction_issues_live_uidx, UNION ALL feed_effective_external_consumption at (tenant_id, park_id, feed_item_key, feed_day) — one row per key by migration 000216, pre-aggregated to (feed_day, feed_item) before the union so neither side can fan the other out; group_key=(feed_day, feed_item_label, feed_item_key) on BOTH sides after first collapsing sheet cells to the pen-grain (shed_id, partition_key, shed_tag_key, breed_key) so session and item cells cannot inflate head counts; join_cardinality=issues to rows is 1:N by feed_direction_issue_id and joins exactly once per day thanks to the live-issue partial unique index, and the pen_item CTE pre-aggregates the N side before the outer GROUP BY; the union is a row concatenation, not a join, so an item carried by both sources on one day SUMS to what the animals actually ate rather than duplicating a row; pagination=none, whole-window aggregate invariant to any page size — there is no limit/offset input; scope=tenant_id on both tables plus the caller's authorized park set via park_id = ANY($2)
//
// Ratio key sets, second reading: per_head_grams for a (day, item) group divides the
// group's kg by the SAME group's heads. The external side carries zero heads, so a
// milk-only group divides by zero heads and reports EMPTY — never a per-head figure
// over a herd nobody counted for it. The DAY totals (directedAnalyticsDaysSQL below)
// are deliberately left on the sheet alone, because they are what the "Directed
// yesterday" / "Avg ration per animal" tiles read and those tiles say "on the issued
// sheet".
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
),
ext AS (
    -- Feeds the ration grid does not direct -- UHT Milk, from the Milk Preparation
    -- operator's submitted litres (read as kg 1:1) with the 000185 ledger as the
    -- fallback. The animals drink it, so it belongs on the chart beside the sheet's
    -- items (maintainer decision 2026-09-02); leaving it out drew a picture of the
    -- farm's feeding with one real feed missing from it.
    --
    -- NO HEADS. The ledger records a park's litres for a day, not which pens drank
    -- them, so there is no pen-grain head count to divide by: this side contributes
    -- kg and a ZERO head count, which makes its per-head figure empty rather than a
    -- number computed from a denominator nobody measured.
    SELECT x.feed_day,
           x.feed_item_label,
           x.feed_item_key,
           SUM(x.quantity_kg) AS grain_kg,
           0::bigint          AS grain_heads
    FROM feed_effective_external_consumption x
    WHERE x.tenant_id = $1
      AND x.park_id IS NOT NULL
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR x.park_id = ANY ($2::uuid[]))
      AND x.feed_day BETWEEN $3 AND $4
    GROUP BY x.feed_day, x.feed_item_label, x.feed_item_key
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
FROM (
    SELECT feed_day, feed_item_label, feed_item_key, grain_kg, grain_heads FROM pen_item
    UNION ALL
    SELECT feed_day, feed_item_label, feed_item_key, grain_kg, grain_heads FROM ext
) both_sources
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
// EVERY MEASURED BAG, difference reported as it stands (maintainer decision 2026-08-24, superseding
// the beyond-tolerance flag that followed the original outliers-only rule). This comparison must
// NEVER reach a verifier surface: she enters blind, and the page serving this payload is
// leadership-gated.
//
// scale-guard:ignore: 5k-50k-envelope -- bounded windowed comparison over the
// same indexed date columns as the status counts above.
// Bounded LIMIT/OFFSET over one capped window (92 days max) of MEASURED bags -- readings a verifier
// entered, not herd data -- and the service REJECTS an offset past domain.MaxPackingVarianceOffset,
// so the offset cannot grow with the herd. Keyset is not usable here: the sort key is a COMPUTED
// absolute difference, neither unique nor indexable. The ORDER BY ends in the row's own identity,
// so a page boundary never splits or repeats a bag.
//
// scale-guard:ignore: bounded offset over a capped window, rejected past 5000; see above.
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
      AND ($8::text = '' OR lp.name = $8::text)
      AND ($9::text = '' OR q.feed_item_key = $9::text)
),
planned AS (
    SELECT i.feed_day, i.park_id, r.shed_id, r.partition_key, r.session_no, r.workflow,
           r.feed_item_key,
           SUM(r.quantity_kg)                       AS planned_kg,
           MAX(r.session_label)                     AS session_label,
           -- The cohort of the bag, agree-or-go-bare: a pen-session-item whose sheet rows carry
           -- more than one breed reports 'Mixed' rather than naming one, which would be a cohort
           -- nobody recorded.
           CASE WHEN COUNT(DISTINCT COALESCE(NULLIF(r.breed, ''), 'Unspecified')) = 1
                THEN MAX(COALESCE(NULLIF(r.breed, ''), 'Unspecified')) ELSE $5::text END AS breed_label
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
       -- The PACKING day, which is what this table is about: a packer works day P on the sheet the
       -- animals eat on day P+1 (maintainer decision 2026-07-27, the axis Feed Packing already
       -- browses by). Derived here rather than in the client, because IST day arithmetic done on a
       -- browser clock is exactly how this page once dropped a day.
       (rd.target_date - 1)::text AS packing_day,
       rd.park_label,
       rd.shed_id::text,
       rd.shed_label,
       rd.partition_label,
       rd.session_no,
       COALESCE(p.session_label, ''),
       rd.workflow,
       rd.feed_item_key,
       rd.feed_item_label,
       COALESCE(p.breed_label, ''),
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
-- EVERY measured bag, biggest difference first (maintainer decision 2026-08-24, replacing the
-- outliers-only list). A bag that matched is evidence too -- the verifier entered it blind, so a
-- match is independent confirmation and hiding it left the reader unable to see how much of the
-- day was confirmed. The difference is reported as it stands, with no tolerance flag on the row.
-- The sort is by absolute difference so an over-pack and an equal short-pack rank together; the
-- remaining keys are the row's own identity, so a page boundary is stable between reads.
ORDER BY abs(rd.entered_kg - COALESCE(p.planned_kg, 0)) DESC, rd.target_date DESC,
         rd.park_label, rd.shed_label, rd.partition_label, rd.session_no, rd.feed_item_label
LIMIT $6 OFFSET $7`

// Target vs actual feed, at SHED grain (maintainer decision 2026-08-23).
//
// Three things this query must keep straight, each of which reads as a detail and is not:
//
//  1. A shed with NO packing reading is not a shed that got zero feed. `readings` is LEFT JOINed
//     and its sum kept NULL, so an unverified shed reports a blank actual and NO variance. The
//     older shape coalesced it to 0, which made every not-yet-verified shed a red row claiming
//     the animals were fed nothing.
//  2. Breed and age group are resolved AGREE-OR-GO-BARE across the shed's sheet rows. A pen whose
//     rows disagree reports 'Mixed'; picking the first would invent a cohort.
//  3. The item/session detail is summed away ON PURPOSE. Which feed was off is the packing
//     mismatch table's question; this table answers whether the shed got its day's feed.
//
// projection-review: membership=feed_direction_issue_rows of issued/amended/locked sheets in the
// window at their (issue, shed, partition, session, item) natural grain, LEFT JOINed to
// feed_packing_verified_quantities through completed feed_packing_completions;
// group_key=(feed_day, park_id, shed_id, partition_key) on every side -- both sides are collapsed
// to the item/session grain FIRST and joined on the full (date, park, shed, partition, session,
// workflow, item) key, then summed to the shed key, so a shed fed two items in two sessions
// contributes each bag exactly once; join_cardinality=planned LEFT JOIN readings 1:0..1 at the
// item/session key (feed_packing_verified_quantities is unique per completion+item and
// completions are unique per that key), no side left unaggregated; pagination=none, one day's
// sheds for the caller's parks is a bounded set with no limit/offset input, and the trend arm
// ranges over the SAME comparison rows as the table so a day total can never disagree with the
// rows it summarises; scope=tenant_id on both sides plus the caller's authorized park set.
//
// scale-guard:ignore: 5k-50k-envelope -- bounded per-(day, shed) aggregate over one window of
// frozen sheets and their readings, canonical-indexed-SQL default.
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
-- One row per SHED per day: every item and session summed. actual_kg stays NULL when the shed has
-- no reading at all, which is what keeps "not verified yet" apart from "given nothing".
comparison AS (
    SELECT p.feed_day,
           p.park_id,
           MAX(p.park_label)      AS park_label,
           p.shed_id,
           MAX(p.shed_label)      AS shed_label,
           p.partition_key,
           MAX(p.partition_label) AS partition_label,
           CASE WHEN COUNT(DISTINCT p.breed_label) = 1 THEN MAX(p.breed_label) ELSE $6::text END AS breed_label,
           CASE WHEN COUNT(DISTINCT p.age_group) = 1 THEN MAX(p.age_group) ELSE $6::text END     AS age_group,
           SUM(p.target_kg)       AS target_kg,
           SUM(r.actual_kg)       AS actual_kg
    FROM planned p
    LEFT JOIN readings r
      ON r.target_date = p.feed_day
     AND r.park_id = p.park_id
     AND r.shed_id = p.shed_id
     AND r.partition_key = p.partition_key
     AND r.session_no = p.session_no
     AND r.workflow = p.workflow
     AND r.feed_item_key = p.feed_item_key
    GROUP BY p.feed_day, p.park_id, p.shed_id, p.partition_key
)
SELECT feed_day::text,
       (feed_day - 1)::text AS packing_day,
       SUM(target_kg)::text,
       COALESCE(SUM(actual_kg)::text, '')                                   AS actual_kg,
       COUNT(*) FILTER (WHERE actual_kg IS NOT NULL AND abs(actual_kg - target_kg) > $5)::text,
       COUNT(*) FILTER (WHERE actual_kg IS NOT NULL)::text
FROM comparison
GROUP BY feed_day
ORDER BY feed_day`

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

	// Each arm runs only when asked for. A page that needs one array from a second, differently
	// scoped read fetches THAT array, not the whole payload.
	if q.Wants(domain.ExecutionSectionDays) {
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

	if q.Wants(domain.ExecutionSectionConsumption) {
		// ONE FEED DAY PAST THE WINDOW, so the trend's PACKING-day axis is not cut short.
		//
		// A bag is packed the day BEFORE the feed day it serves, and this arm plots packing days.
		// Reading only the caller's feed-day window therefore ended the axis a day early: with the
		// page's window closing on yesterday's feed day, the newest packing day it could ever draw
		// was the day before yesterday. YESTERDAY's packing -- a finished day, already weighed by a
		// verifier -- was never visible on the one chart built to show it. Observed on 2026-08-25:
		// 199 measured bags totalling 2,107 kg hidden, while the mismatch table beside it already
		// offered that day, because the page translates ITS packing-day picker with the same +1.
		//
		// Only the FAR end moves. `from` is untouched, and the window keeps meaning FEED days for
		// every other arm and for this query's own predicate -- the extra day is one more sheet
		// read, not a second grain. Today's packing still cannot appear: it serves tomorrow's feed
		// day, which is past even the extended end, so a day mid-pack never lands half-finished.
		consTo := to.AddDate(0, 0, 1).Format("2006-01-02")
		consRows, err := r.pool.Query(ctx, executionConsumptionSQL, tenantID, parkIDs, fromArg, consTo, domain.PackingVarianceToleranceKg, domain.MixedCohortLabel)
		if err != nil {
			return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics consumption trend: %w", err)
		}
		defer consRows.Close()
		out.ConsumptionTrend = []domain.FeedConsumptionTrendDay{}
		for consRows.Next() {
			var day domain.FeedConsumptionTrendDay
			var varianceText, comparedText string
			if err := consRows.Scan(&day.FeedDay, &day.PackingDay, &day.TargetKg, &day.ActualKg, &varianceText, &comparedText); err != nil {
				return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics consumption trend scan: %w", err)
			}
			if _, err := fmt.Sscan(varianceText, &day.VarianceRows); err != nil {
				return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics consumption variance row count: %w", err)
			}
			if _, err := fmt.Sscan(comparedText, &day.ComparedRows); err != nil {
				return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics consumption compared row count: %w", err)
			}
			out.ConsumptionTrend = append(out.ConsumptionTrend, day)
		}
		if err := consRows.Err(); err != nil {
			return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics consumption trend rows: %w", err)
		}
	}

	if q.Wants(domain.ExecutionSectionPackingVariance) {
		// One row MORE than the page is asked for: if it comes back there is a next page. A COUNT(*)
		// over the same predicate would be a second scan to learn one bit.
		varLimit, varOffset, err := domain.NormalisePackingVariancePage(q.PackingVarianceLimit, q.PackingVarianceOffset)
		if err != nil {
			return domain.ExecutionAnalytics{}, err
		}
		varRows, err := r.pool.Query(ctx, executionPackingVarianceSQL, tenantID, parkIDs, fromArg, toArg,
			domain.MixedCohortLabel, varLimit+1, varOffset,
			q.PackingVarianceParkLabel, q.PackingVarianceFeedItemKey)
		if err != nil {
			return domain.ExecutionAnalytics{}, fmt.Errorf("feed analytics packing variance: %w", err)
		}
		defer varRows.Close()
		out.PackingVariance = []domain.PackingVarianceRow{}
		for varRows.Next() {
			var v domain.PackingVarianceRow
			if err := varRows.Scan(
				&v.FeedDay, &v.PackingDay, &v.ParkLabel, &v.ShedID, &v.ShedLabel, &v.PartitionLabel,
				&v.SessionNo, &v.SessionLabel, &v.Workflow, &v.FeedItemKey, &v.FeedItemLabel,
				&v.BreedLabel, &v.PlannedKg, &v.VerifiedKg, &v.VarianceKg,
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
		if len(out.PackingVariance) > varLimit {
			out.PackingVariance = out.PackingVariance[:varLimit]
			out.PackingVarianceHasMore = true
		}
	}

	if q.Wants(domain.ExecutionSectionDistributionCompletions) {
		if err := r.distributionCompletions(ctx, tenantID, parkIDs, to, q, &out); err != nil {
			return domain.ExecutionAnalytics{}, err
		}
	}
	return out, nil
}

// distributionCompletions serves the completion arm: one PAGE of pen-sessions, the whole-day status
// totals behind the tiles, and the day's place vocabulary for the filter selects.
//
// Proof REFERENCES only. The uploader name and upload time behind each reference belong to the proof
// module and are resolved by the app service through its ProofUploadDescriber port, so this adapter
// never reads proof_artifacts.
func (r *Repository) distributionCompletions(
	ctx context.Context, tenantID string, parkIDs []uuid.UUID, windowEnd time.Time,
	q domain.DirectedAnalyticsQuery, out *domain.ExecutionAnalytics,
) error {
	// The table describes ONE business day, defaulting to the window's last day -- YESTERDAY, the
	// same "up to yesterday" basis the rest of this page states in its banner. Today is deliberately
	// not the default: a day still being worked would list every pen not yet fed as untouched, which
	// reads as a failure rather than as work in progress.
	completionDay := q.CompletionDay
	if completionDay.IsZero() {
		completionDay = windowEnd
	}
	day := completionDay.Format("2006-01-02")
	out.CompletionDay = day

	limit, offset, err := domain.NormaliseCompletionPage(q.CompletionLimit, q.CompletionOffset)
	if err != nil {
		return err
	}
	if !domain.IsValidDistributionCompletionStatus(q.CompletionStatus) {
		// Rejected, never ignored: silently dropping an unknown status would answer a filtered
		// request with every row, under the heading of the filter the caller asked for.
		return domain.ErrInvalidCompletionStatus
	}
	parkFilter := nullableUUID(q.CompletionParkID)
	shedFilter := nullableUUID(q.CompletionShedID)

	// One row MORE than the page: if it comes back there is a next page, without a second scan.
	rows, err := r.pool.Query(ctx, distributionCompletionRowsSQL,
		tenantID, parkIDs, day, parkFilter, shedFilter, q.CompletionStatus, limit+1, offset)
	if err != nil {
		return fmt.Errorf("feed analytics distribution completions: %w", err)
	}
	defer rows.Close()
	out.DistributionCompletions = []domain.DistributionCompletionRow{}
	for rows.Next() {
		var (
			row                          domain.DistributionCompletionRow
			weightRef, feedRef, waterRef string
		)
		if err := rows.Scan(
			&row.FeedDay, &row.ParkID, &row.ParkLabel, &row.ShedID, &row.ShedLabel,
			&row.PartitionLabel, &row.SessionNo, &row.SessionLabel, &row.Workflow,
			&row.Status, &row.ReworkReason,
			&weightRef, &feedRef, &waterRef,
			&row.SubmittedAt, &row.VerifiedAt, &row.SubmittedByName, &row.VerifiedByName,
		); err != nil {
			return fmt.Errorf("feed analytics distribution completions scan: %w", err)
		}
		// Canonical composition, never hand-rolled (operational-location rule).
		row.OperationalLocationDisplay = oploc.OperationalLocation{
			ShedName:       row.ShedLabel,
			PartitionLabel: row.PartitionLabel,
		}.Display()
		// Always three slots, in the order they are shot. An empty ProofRef IS the missing video.
		refs := map[string]string{
			domain.DistributionSlotFeedWeightPhoto: weightRef,
			domain.DistributionSlotFeedVideo:       feedRef,
			domain.DistributionSlotWaterVideo:      waterRef,
		}
		row.Proofs = make([]domain.DistributionProofSlot, 0, len(domain.DistributionSlotOrder))
		for _, slot := range domain.DistributionSlotOrder {
			row.Proofs = append(row.Proofs, domain.DistributionProofSlot{FieldKey: slot, ProofRef: refs[slot]})
		}
		out.DistributionCompletions = append(out.DistributionCompletions, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("feed analytics distribution completions rows: %w", err)
	}
	if len(out.DistributionCompletions) > limit {
		out.DistributionCompletions = out.DistributionCompletions[:limit]
		out.DistributionCompletionsHasMore = true
	}

	// Totals follow the PLACE filters and ignore the STATUS one.
	totalRows, err := r.pool.Query(ctx, distributionCompletionTotalsSQL,
		tenantID, parkIDs, day, parkFilter, shedFilter)
	if err != nil {
		return fmt.Errorf("feed analytics distribution completion totals: %w", err)
	}
	defer totalRows.Close()
	for totalRows.Next() {
		var status string
		var n int64
		if err := totalRows.Scan(&status, &n); err != nil {
			return fmt.Errorf("feed analytics distribution completion totals scan: %w", err)
		}
		switch status {
		case domain.DistributionCompletionNotStarted:
			out.CompletionTotals.NotStarted = n
		case domain.DistributionCompletionAwaitingVerification:
			out.CompletionTotals.AwaitingVerification = n
		case domain.DistributionCompletionRework:
			out.CompletionTotals.Rework = n
		case domain.DistributionCompletionCompleted:
			out.CompletionTotals.Completed = n
		}
	}
	if err := totalRows.Err(); err != nil {
		return fmt.Errorf("feed analytics distribution completion totals rows: %w", err)
	}

	// Filter vocabulary: the whole day, unnarrowed by either filter.
	optRows, err := r.pool.Query(ctx, distributionCompletionOptionsSQL, tenantID, parkIDs, day, nil, nil)
	if err != nil {
		return fmt.Errorf("feed analytics distribution completion options: %w", err)
	}
	defer optRows.Close()
	out.CompletionFilterOptions = []domain.CompletionFilterOption{}
	for optRows.Next() {
		var opt domain.CompletionFilterOption
		if err := optRows.Scan(&opt.ParkID, &opt.ParkLabel, &opt.ShedID, &opt.ShedLabel); err != nil {
			return fmt.Errorf("feed analytics distribution completion options scan: %w", err)
		}
		out.CompletionFilterOptions = append(out.CompletionFilterOptions, opt)
	}
	if err := optRows.Err(); err != nil {
		return fmt.Errorf("feed analytics distribution completion options rows: %w", err)
	}
	return nil
}

// nullableUUID turns an optional filter id into a bind that is either a uuid or SQL NULL. An empty
// string must not reach a ::uuid cast -- it errors rather than meaning "no filter".
func nullableUUID(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ---------------------------------------------------------------------------
// Distribution completion table (maintainer decision 2026-08-26): every pen-session ONE feed day
// directed, with its proof state -- including the ones NOBODY TOUCHED, which is the whole reason it
// exists (operators were skipping proof uploads and no screen could show it).
// ---------------------------------------------------------------------------

// distributionCompletionScopeSQL is the shared FROM/WHERE of the three completion reads below: the
// day's expected pen-sessions UNION the day's completions, labelled and filtered.
//
// projection-review: grain=pen-session (feed_day, park_id, shed_id, partition_key, session_no,
// workflow) on BOTH sides.
//
//	producer `expected` unique columns after GROUP BY: (feed_day, park_id, shed_id, partition_key,
//	  session_no, workflow) -- exactly the group key, so it is one row per pen-session by
//	  construction; the N feed-item/ration-grain side is collapsed by that GROUP BY BEFORE any join.
//	producer `done` unique columns: (tenant_id, park_id, shed_id, partition_key, session_no,
//	  target_date, workflow) = feed_distribution_completions_natural_uq (migration 000137) -- the
//	  same six coordinates plus the tenant this query already fixes, so one row per pen-session.
//	consumer `keys` match columns: (feed_day, park_id, shed_id, partition_key, session_no, workflow)
//	  -- identical list on both LEFT JOINs, so each is 1:0..1 and neither can fan out.
//	row multiplicity: keys:expected 1:0..1, keys:done 1:0..1, keys:locations 1:0..1 twice by the
//	  locations (tenant_id, location_id) PK, keys:workforce_members 1:0..1 twice by (tenant_id,
//	  user_id). The rows read and the totals read range over this SAME key set -- the totals are a
//	  GROUP BY over it with no LIMIT -- so the tiles and the table can never describe different sets.
//	pagination=rows arm only (LIMIT/OFFSET over a stable ORDER BY); the totals and the filter
//	  options are whole-scope aggregates and are invariant to page size, per the operational
//	  read-model contract.
//	scope=tenant_id on every table plus the caller's authorized park set; the park/shed/status
//	  filters can only narrow within that.
//
// The UNION keys on the six identity columns ONLY: including session_label would make the same
// pen-session appear twice whenever the sheet and a completion disagree on the label, and each half
// would then read as a separate bag of work.
//
// A completion with no sheet row still lists (the UNION's second leg). That is not hypothetical -- a
// sheet can be re-issued after a pen was already fed -- and dropping it would hide work that was
// actually done, the opposite failure to the one this table addresses.
//
// The location joins are LEFT and fall back to the sheet's own snapshotted labels. An INNER join
// reads as harmless -- a completion carries an FK to locations -- but it lets a missing or retired
// shed row DELETE a pen-session from a table whose entire purpose is showing pen-sessions nobody
// touched. Falling back to a stale label is honest; dropping the line is not.
//
// scale-guard:ignore: 5k-50k-envelope -- ONE park-day, bounded by the park's pens x sessions
// (physical infrastructure, never herd size), over the same indexed date columns as the status
// counts above; binds are cast and the indexed columns stay bare.
const distributionCompletionScopeSQL = `
WITH expected AS (
    SELECT i.feed_day, i.park_id, r.shed_id, r.partition_key, r.session_no, r.workflow,
           MAX(COALESCE(r.partition_label, ''))  AS partition_label,
           MAX(r.session_label)                  AS session_label,
           MAX(r.park_label)                     AS park_label,
           MAX(r.shed_label)                     AS shed_label
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1
     AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1
      AND ($2::uuid[] IS NULL OR i.park_id = ANY ($2::uuid[]))
      AND i.feed_day = $3
      AND i.state IN ('issued', 'amended', 'locked')
      AND i.workflow IN ('normal', 'experiment')
    GROUP BY i.feed_day, i.park_id, r.shed_id, r.partition_key, r.session_no, r.workflow
),
done AS (
    SELECT c.target_date AS feed_day, c.park_id, c.shed_id, c.partition_key, c.session_no, c.workflow,
           COALESCE(c.partition_label, '') AS partition_label,
           c.status, COALESCE(c.rework_reason, '') AS rework_reason,
           COALESCE(c.feed_weight_proof_ref, '')  AS feed_weight_proof_ref,
           COALESCE(c.distribution_proof_ref, '') AS distribution_proof_ref,
           COALESCE(c.water_proof_ref, '')        AS water_proof_ref,
           c.created_at, c.verified_at, c.completed_by, c.verified_by
    FROM feed_distribution_completions c
    WHERE c.tenant_id = $1
      AND ($2::uuid[] IS NULL OR c.park_id = ANY ($2::uuid[]))
      AND c.target_date = $3
),
keys AS (
    SELECT feed_day, park_id, shed_id, partition_key, session_no, workflow FROM expected
    UNION
    SELECT feed_day, park_id, shed_id, partition_key, session_no, workflow FROM done
),
scoped AS (
    SELECT k.feed_day,
           k.park_id,
           COALESCE(lp.name, e.park_label, '')                    AS park_label,
           k.shed_id,
           COALESCE(ls.name, e.shed_label, '')                    AS shed_label,
           COALESCE(e.partition_label, d.partition_label, '')     AS partition_label,
           k.session_no,
           COALESCE(e.session_label, '')                          AS session_label,
           k.workflow,
           COALESCE(d.status, '')                                 AS raw_status,
           COALESCE(d.rework_reason, '')                          AS rework_reason,
           COALESCE(d.feed_weight_proof_ref, '')                  AS feed_weight_proof_ref,
           COALESCE(d.distribution_proof_ref, '')                 AS distribution_proof_ref,
           COALESCE(d.water_proof_ref, '')                        AS water_proof_ref,
           d.created_at                                           AS submitted_at,
           d.verified_at,
           COALESCE(wc.display_name, '')                          AS submitted_by_name,
           COALESCE(wv.display_name, '')                          AS verified_by_name
    FROM keys k
    LEFT JOIN expected e
      ON e.feed_day = k.feed_day AND e.park_id = k.park_id AND e.shed_id = k.shed_id
     AND e.partition_key = k.partition_key AND e.session_no = k.session_no AND e.workflow = k.workflow
    LEFT JOIN done d
      ON d.feed_day = k.feed_day AND d.park_id = k.park_id AND d.shed_id = k.shed_id
     AND d.partition_key = k.partition_key AND d.session_no = k.session_no AND d.workflow = k.workflow
    LEFT JOIN locations lp ON lp.tenant_id = $1 AND lp.location_id = k.park_id
    LEFT JOIN locations ls ON ls.tenant_id = $1 AND ls.location_id = k.shed_id
    LEFT JOIN workforce_members wc ON wc.tenant_id = $1 AND wc.user_id = d.completed_by
    LEFT JOIN workforce_members wv ON wv.tenant_id = $1 AND wv.user_id = d.verified_by
    WHERE ($4::uuid IS NULL OR k.park_id = $4::uuid)
      AND ($5::uuid IS NULL OR k.shed_id = $5::uuid)
),
bucketed AS (
    SELECT scoped.*,
           CASE raw_status
               WHEN 'completed' THEN 'completed'
               WHEN 'pending_verification' THEN 'pending_verification'
               WHEN 'rework' THEN 'rework'
               ELSE 'not_started'
           END AS status
    FROM scoped
)`

// distributionCompletionRowsSQL is the PAGE. The status filter applies here and NOT to the totals.
//
// The result set is ONE park-day of pen-sessions -- bounded by the park's pens x sessions, physical
// infrastructure that does not grow with the herd -- and the offset is rejected past 5000, so it
// cannot walk a deep scan. Keyset is not usable here: the sort key is the composed farm/shed/pen
// label, neither unique nor indexable, and the ORDER BY ends in the row's own identity so a page
// boundary never splits or repeats a pen-session.
//
// scale-guard:ignore: bounded offset over one park-day, rejected past 5000; see above.
const distributionCompletionRowsSQL = distributionCompletionScopeSQL + `
SELECT feed_day::text, park_id::text, park_label, shed_id::text, shed_label, partition_label,
       session_no, session_label, workflow, status, rework_reason,
       feed_weight_proof_ref, distribution_proof_ref, water_proof_ref,
       submitted_at, verified_at, submitted_by_name, verified_by_name
FROM bucketed
WHERE ($6 = '' OR status = $6)
ORDER BY park_label, shed_label, partition_label, session_no, workflow
LIMIT $7 OFFSET $8`

// distributionCompletionTotalsSQL counts the whole day at the selected PLACE scope, deliberately
// ignoring the status filter -- see CompletionTotals for why both exclusions matter.
const distributionCompletionTotalsSQL = distributionCompletionScopeSQL + `
SELECT status, count(*) FROM bucketed GROUP BY status`

// distributionCompletionOptionsSQL is the day's (park, shed) vocabulary for the filter selects. The
// caller passes NIL for the place binds ($4/$5) on purpose: a select whose options are narrowed by
// its own current value cannot be widened back, so the reader would be stuck on the farm they
// picked.
const distributionCompletionOptionsSQL = distributionCompletionScopeSQL + `
SELECT DISTINCT park_id::text, park_label, shed_id::text, shed_label
FROM bucketed
ORDER BY park_label, shed_label`

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

// projection-review: membership=feed_purchases at its (tenant, farm_label, feed_item_key, batch_no) natural key, locked feed_direction_issue_rows reached through the at-most-one live issue per (tenant, park, feed_day, workflow), and feed_effective_external_consumption at (tenant, park_id, feed_item_key, feed_day) (feeds the ration grid does not direct — UHT Milk, resolved from the Milk Preparation workflow on submit, with the feed_external_consumption ledger as the fallback for days that workflow does not cover; migration 000216 guarantees at most one row per key, so the two sources cannot both contribute); the two consumption sources pre-aggregate to the same (park_id, feed_item_key, feed_day) grain and the UNION ALL is re-grouped on that key, so a day contributes once; group_key=(farm_label, feed_item_key) on every side — purchases, depletion and the recent-day average all collapse to the farm-item before joining (the sheet side collapses to (park_id, feed_item_key) and one farm_label resolves to exactly one park), so the three sides meet strictly 1:1; join_cardinality=bought JOIN directed 1:1, LEFT JOIN recent 1:0..1, each pre-aggregated to one row per farm-item; pagination=none, a tenant's feed catalog across its farms is a bounded card list; scope=tenant_id everywhere plus the caller's authorized park set on both purchases and sheets. Both workflows deplete stock — experiment feed leaves the same store.
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
    -- plus feed_effective_external_consumption for feeds the ration grid does
    -- not direct (UHT Milk: the Milk Preparation operator's submitted litres,
    -- read as kg 1:1, with the 000185 ledger as fallback; migration 000216).
    -- That view is already one row per (tenant, park, item, day). The outer GROUP BY
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
        FROM feed_effective_external_consumption x
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
// locked feed_direction_issue_rows UNION feed_effective_external_consumption, collapsed
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
        FROM feed_effective_external_consumption x
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

// Feeds whose stock will run out inside the notification horizon, for the daily low-stock alert
// (maintainer decision 2026-08-24). Same balance and burn-rate arithmetic as the Stock cards --
// purchased net of import consumption, minus everything fed since the bootstrap cutoff, over the
// three most recent fed days -- so a card and an alert can never disagree about days left.
//
// The alert threshold is SEPARATE from domain.LowStockDays, which stays the legacy sheet's 5-day
// red card: leadership is told a week out so a purchase order can still be raised, while the card
// keeps meaning "nearly out". Two thresholds, deliberately.
//
// park_id is returned because a notification must name the park -- a farm label alone does not
// deep-link, and a feed name alone does not tell a director which store to check.
//
// projection-review: membership=feed_purchases at (tenant, farm_label, feed_item_key, batch_no),
// aggregated to (farm_label, feed_item_key); group_key=(farm_label, feed_item_key) on every side --
// the purchase side GROUPs BY that pair and the consumption side collapses to (park_id,
// feed_item_key, feed_day) before averaging, joined through the farm's single resolved park;
// join_cardinality=bought LEFT JOIN directed 1:0..1 and LEFT JOIN recent 1:0..1, no side left
// unaggregated; pagination=none -- the result is at most one row per (farm, feed) the tenant buys,
// bounded by the catalog, and every row is delivered; scope=tenant_id throughout.
//
// scale-guard:ignore: 5k-50k-envelope -- bounded per-(farm, item) aggregate over the small purchase
// ledger and locked sheets, canonical-indexed-SQL default.
const feedLowStockSQL = `
WITH bought AS (
    SELECT farm_label, feed_item_key,
           MAX(feed_item_label)                     AS feed_item_label,
           MIN(park_id::text)                       AS park_id_text,
           SUM(quantity_kg - consumed_at_import_kg) AS net_kg,
           MIN(depletes_from)                       AS depletes_from
    FROM feed_purchases
    WHERE tenant_id = $1
    GROUP BY farm_label, feed_item_key
),
fed AS (
    SELECT park_id, feed_item_key, feed_day, SUM(kg) AS kg
    FROM (
        SELECT i.park_id, r.feed_item_key, i.feed_day, SUM(r.quantity_kg) AS kg
        FROM feed_direction_issues i
        JOIN feed_direction_issue_rows r
          ON r.tenant_id = $1 AND r.feed_direction_issue_id = i.feed_direction_issue_id
        WHERE i.tenant_id = $1 AND i.state = 'locked'
        GROUP BY i.park_id, r.feed_item_key, i.feed_day
        UNION ALL
        SELECT x.park_id, x.feed_item_key, x.feed_day, SUM(x.quantity_kg) AS kg
        FROM feed_effective_external_consumption x
        WHERE x.tenant_id = $1 AND x.park_id IS NOT NULL
        GROUP BY x.park_id, x.feed_item_key, x.feed_day
    ) both_sources
    GROUP BY park_id, feed_item_key, feed_day
),
directed AS (
    SELECT b.farm_label, b.feed_item_key, COALESCE(SUM(f.kg), 0) AS kg
    FROM bought b
    LEFT JOIN fed f
      ON b.park_id_text IS NOT NULL
     AND f.park_id = b.park_id_text::uuid
     AND f.feed_item_key = b.feed_item_key
     AND f.feed_day >= b.depletes_from
    GROUP BY b.farm_label, b.feed_item_key
),
recent AS (
    SELECT park_id, feed_item_key, AVG(kg) AS avg_kg
    FROM (
        SELECT park_id, feed_item_key, kg,
               ROW_NUMBER() OVER (PARTITION BY park_id, feed_item_key ORDER BY feed_day DESC) AS rn
        FROM fed
    ) ranked
    WHERE rn <= 3
    GROUP BY park_id, feed_item_key
)
SELECT COALESCE(b.park_id_text, ''),
       b.farm_label,
       b.feed_item_label,
       b.feed_item_key,
       round(b.net_kg - d.kg, 1)::text                    AS balance_kg,
       round(r.avg_kg, 1)::text                           AS avg_daily_kg,
       GREATEST(floor((b.net_kg - d.kg) / r.avg_kg), 0)::bigint AS days_left
FROM bought b
JOIN directed d USING (farm_label, feed_item_key)
JOIN recent r
  ON b.park_id_text IS NOT NULL
 AND r.park_id = b.park_id_text::uuid
 AND r.feed_item_key = b.feed_item_key
-- A feed with no recent consumption has no burn rate to divide by, so it has no days-left to be
-- low: it is joined INNER on purpose. Alerting on it would be a guess.
WHERE r.avg_kg > 0
  AND floor((b.net_kg - d.kg) / r.avg_kg) < $2
ORDER BY days_left, b.farm_label, b.feed_item_label`

// LowStockFeeds lists the feeds whose stock runs out inside withinDays, for the daily alert.
func (r *Repository) LowStockFeeds(ctx context.Context, tenantID string, withinDays int) ([]domain.LowStockFeed, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, feedLowStockSQL, tenantID, withinDays)
	if err != nil {
		return nil, fmt.Errorf("feed low stock: %w", err)
	}
	defer rows.Close()
	out := []domain.LowStockFeed{}
	for rows.Next() {
		var f domain.LowStockFeed
		if err := rows.Scan(&f.ParkID, &f.FarmLabel, &f.FeedItemLabel, &f.FeedItemKey,
			&f.BalanceKg, &f.AvgDailyKg, &f.DaysLeft); err != nil {
			return nil, fmt.Errorf("feed low stock scan: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feed low stock rows: %w", err)
	}
	return out, nil
}

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
        FROM feed_effective_external_consumption x
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

// Per-ITEM expenditure: the SAME day_item × same-farm latest-load pricing as
// the daily series above, kept at (feed_day, feed_item_key) grain instead of
// collapsing to the day. Each feed's kg and rupees are summed over the
// caller's farms, so a feed bought at two rates on two farms reads as one row
// priced farm by farm -- never repriced at either farm's rate alone.
//
// projection-review: membership=the same both_sources set as stockExpenditureSQL -- issue rows
// through their live issue plus feed_effective_external_consumption, each pre-aggregated to
// (feed_day, park_id, feed_item_key) before pricing; group_key=(feed_day, feed_item_key), one row
// per feed per day; join_cardinality=day_item to price is 1:0..1 via the LIMIT 1 lateral, so no
// fan-out -- the day series and this series range over the identical priced (day, farm, item)
// set and differ only by the grain they round at; pagination=none, bounded by window days × the
// feed catalog; scope=tenant_id plus the caller's authorized park set on every source.
//
// scale-guard:ignore: 5k-50k-envelope -- bounded windowed aggregate over the
// indexed issue/consumption date columns, the ADR's canonical-indexed-SQL default.
const stockItemExpenditureSQL = `
WITH day_item AS (
    SELECT feed_day, park_id, feed_item_key, MAX(feed_item_label) AS feed_item_label, SUM(kg) AS kg
    FROM (
        SELECT i.feed_day, i.park_id, r.feed_item_key, MAX(r.feed_item_label) AS feed_item_label,
               SUM(r.quantity_kg) AS kg
        FROM feed_direction_issues i
        JOIN feed_direction_issue_rows r
          ON r.tenant_id = $1 AND r.feed_direction_issue_id = i.feed_direction_issue_id
        WHERE i.tenant_id = $1
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR i.park_id = ANY ($2::uuid[]))
          AND i.state IN ('issued', 'amended', 'locked')
          AND i.feed_day BETWEEN $3 AND $4
        GROUP BY i.feed_day, i.park_id, r.feed_item_key
        UNION ALL
        SELECT x.feed_day, x.park_id, x.feed_item_key, MAX(x.feed_item_label) AS feed_item_label,
               SUM(x.quantity_kg) AS kg
        FROM feed_effective_external_consumption x
        WHERE x.tenant_id = $1
          AND x.park_id IS NOT NULL
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR x.park_id = ANY ($2::uuid[]))
          AND x.feed_day BETWEEN $3 AND $4
        GROUP BY x.feed_day, x.park_id, x.feed_item_key
    ) both_sources
    GROUP BY feed_day, park_id, feed_item_key
)
SELECT di.feed_day::text,
       di.feed_item_key,
       MAX(di.feed_item_label)                    AS feed_item_label,
       round(SUM(di.kg), 1)::text                 AS directed_kg,
       round(SUM(di.kg * price.per_kg), 0)::text  AS rupees
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
GROUP BY di.feed_day, di.feed_item_key
ORDER BY di.feed_day, di.feed_item_key`

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
        FROM feed_effective_external_consumption x
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
  COALESCE(round(SUM(spend) FILTER (WHERE feed_day >= $3::date - 7), 0), 0)::text,
  COALESCE(round(SUM(spend) FILTER (WHERE feed_day >= date_trunc('month', $3::date)::date), 0), 0)::text,
  COALESCE(round(SUM(spend) FILTER (WHERE feed_day >= $3::date - 91), 0), 0)::text,
  COALESCE(round(SUM(spend), 0), 0)::text
FROM priced`

// Per-farm Mesha-concentrate purchase/consumption table (Stock tab).
//
// projection-review: membership=feed_purchases at its (tenant, farm_label, feed_item_key, batch_no) natural key, filtered to the four MeshaConcentrateStockKeys; group_key=(farm_label, feed_item_key) on both purchase sides — loads GROUP BY that pair and last_load is DISTINCT ON the same pair so they meet exactly 1:1, while the directed side collapses locked issue-row cells to (park_id, feed_item_key) before joining and one farm_label resolves to exactly one park (the importer maps CBE/CPT to the tenant's park locations); load_consumption is a FIFO crossing: locked_cells is one row per (park, feed_item_key, feed_day), so the running SUM window per (farm_label, feed_item_key) sees each day once, and MIN(feed_day) over the crossing days collapses back to one row per pair; join_cardinality=loads JOIN last_load 1:1, LEFT JOIN directed 1:0..1, LEFT JOIN load_consumption 1:0..1, no side left unaggregated; pagination=none — four items across a tenant's farms is a bounded table with no limit/offset input; scope=tenant_id everywhere plus the caller's authorized park set on both purchases and sheets.
//
// scale-guard:ignore: 5k-50k-envelope — bounded four-item aggregate over the
// small purchase ledger and locked sheets, canonical-indexed-SQL default.
const stockFarmItemsSQL = `
WITH loads AS (
    SELECT farm_label, feed_item_key,
           MAX(feed_item_label) AS feed_item_label,
           MIN(park_id::text)   AS park_id_text,
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
           batch_no, purchase_date, quantity_kg, vendor, total_cost, per_kg_cost,
           depletes_from, quantity_kg - consumed_at_import_kg AS net_kg
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
           AVG(kg) FILTER (WHERE rn <= 3) AS recent_avg_kg
    FROM (
        SELECT park_id, feed_item_key, feed_day, kg,
               ROW_NUMBER() OVER (PARTITION BY park_id, feed_item_key ORDER BY feed_day DESC) AS rn
        FROM locked_cells
    ) ranked
    GROUP BY park_id, feed_item_key
),
-- FIFO: a load is consumed only after every EARLIER load's stock is used up.
-- The latest load's consumption therefore starts on the first locked feed day
-- whose cumulative directed kg (since the ledger start) exceeds the net kg of
-- all earlier loads — and never before the load's own depletion date. No
-- crossing yet means the previous stock is still being fed: empty, not a date.
load_consumption AS (
    SELECT farm_label, feed_item_key, MIN(feed_day) AS consumption_from
    FROM (
        SELECT l.farm_label, l.feed_item_key, lc.feed_day,
               ll.depletes_from                AS last_load_from,
               l.net_kg - ll.net_kg            AS prior_net_kg,
               SUM(lc.kg) OVER (PARTITION BY l.farm_label, l.feed_item_key
                                ORDER BY lc.feed_day) AS cum_kg
        FROM loads l
        JOIN last_load ll
          ON ll.farm_label = l.farm_label
         AND ll.feed_item_key = l.feed_item_key
        JOIN locked_cells lc
          ON l.park_id_text IS NOT NULL
         AND lc.park_id = l.park_id_text::uuid
         AND lc.feed_item_key = l.feed_item_key
         AND lc.feed_day >= l.depletes_from
    ) fifo
    WHERE cum_kg > prior_net_kg
      AND feed_day >= last_load_from
    GROUP BY farm_label, feed_item_key
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
-- loads GROUP BY that pair, last_load is DISTINCT ON the same pair, and load_consumption
-- pre-aggregates its FIFO crossing days to MIN(feed_day) per pair; join_cardinality=loads
-- JOIN last_load 1:1, LEFT JOIN directed 1:0..1, LEFT JOIN load_consumption 1:0..1, LEFT JOIN
-- stock_balance 1:0..1, no side left unaggregated, and weekly_required_kg is a scalar multiple
-- of the same recent_avg_kg rather than a differently-grouped sum; pagination=none, four items
-- across a tenant's farms is bounded with no limit/offset input; scope=tenant_id everywhere
-- plus the caller's authorized park set.
SELECT l.farm_label,
       l.feed_item_label,
       l.feed_item_key,
       COALESCE(lcons.consumption_from::text, '')    AS last_load_consumption_from,
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
LEFT JOIN load_consumption lcons
  ON lcons.farm_label = l.farm_label
 AND lcons.feed_item_key = l.feed_item_key
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
	out := domain.StockAnalytics{
		Items:           []domain.StockItem{},
		Expenditure:     []domain.ExpenditureDay{},
		ItemExpenditure: []domain.ExpenditureItemDay{},
	}

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
			&fi.LastLoadConsumptionFrom, &fi.AvgDailyKg, &fi.WeeklyRequiredKg,
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

	// The DAILY series alone starts at the maintainer's floor date; the stock
	// cards above and the spend tiles below keep the caller's full window.
	expFrom, expTo := domain.ClampExpenditureWindow(from, to)
	expRows, err := r.pool.Query(ctx, stockExpenditureSQL, tenantID, parkIDs, expFrom.Format("2006-01-02"), expTo.Format("2006-01-02"))
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

	// Same floored window as the daily series: the two are one fact at two grains.
	itemExpRows, err := r.pool.Query(ctx, stockItemExpenditureSQL, tenantID, parkIDs, expFrom.Format("2006-01-02"), expTo.Format("2006-01-02"))
	if err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics item expenditure: %w", err)
	}
	defer itemExpRows.Close()
	for itemExpRows.Next() {
		var d domain.ExpenditureItemDay
		if err := itemExpRows.Scan(&d.FeedDay, &d.FeedItemKey, &d.FeedItemLabel, &d.DirectedKg, &d.Rupees); err != nil {
			return domain.StockAnalytics{}, fmt.Errorf("feed analytics item expenditure scan: %w", err)
		}
		out.ItemExpenditure = append(out.ItemExpenditure, d)
	}
	if err := itemExpRows.Err(); err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics item expenditure rows: %w", err)
	}

	today := biztime.BusinessDate(time.Now())
	if err := r.pool.QueryRow(ctx, stockSpendSQL, tenantID, parkIDs, today).
		Scan(&out.Spend.Last7Days, &out.Spend.ThisMonth, &out.Spend.ThreeMonths, &out.Spend.ThisYear); err != nil {
		return domain.StockAnalytics{}, fmt.Errorf("feed analytics spend summary: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Shed feed-mix analytics (per-pen window rollup)
// ---------------------------------------------------------------------------

// The per-pen feed-mix rollup behind the Feed Analytics overview table: for
// every operational location the frozen sheet directed feed to in the window,
// the kg of each feed item and the pen's total.
//
// projection-review: membership=the SAME issue-row set as directedAnalyticsSQL above — feed_direction_issue_rows reached through the at-most-one live issue per (tenant, park, feed_day) enforced by feed_direction_issues_live_uidx, states issued/amended/locked, workflows normal+experiment; producer natural key=(tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, shed_tag_key, breed_key, feed_item_key) vs consumer group keys: pen_item groups by (park_id, shed_id, partition_key, feed_item_key) — collapsing days, sessions, shed tags and breeds into one SUM per pen×item, which is the requested figure — and the outer query groups by (park_id, shed_id, partition_key) aggregating items via jsonb_agg; group_key=(park_id, shed_id, partition_key) at the outer grain, one row per operational location; join_cardinality=issues to rows is 1:N by feed_direction_issue_id, joined exactly once per day via the live-issue partial unique index, and pen_item pre-aggregates the N side before the outer GROUP BY so no join can inflate a SUM; ratio check=none — this read divides nothing; the pen total (SUM of item_kg) and the per-item entries range over the same pen_item key set by construction; pagination=none, whole-window aggregate with no limit/offset input — the client pages the served bounded pen set; scope=tenant_id on both tables plus the caller's authorized park set via park_id = ANY($2)
//
// Labels (park_label, shed_label, partition_label, feed_item_label) are
// denormalized snapshots on the rows; MAX() per group is the documented pick so
// a mid-window rename reads as ONE row with the latest-sorting label rather
// than splitting the pen's total in two.
//
// quantity_kg is NULL IFF BLOCKED (schema CHECK); SUM skips NULLs so blocked
// cells contribute nothing, and an all-blocked pen×item group carries NULL
// item_kg — filtered out rather than rendered as a zero the sheet never
// authored.
//
// partition_key='whole' is the non-partitioned sentinel and never reaches a
// client: the outer CASE blanks it, mirroring counts' breakdown read.
//
// scale-guard:ignore: 5k-50k-envelope — bounded windowed aggregate over ≤92
// days × ≤2 parks × ~1 live issue/day, rows reached via the issue-id
// natural-key prefix; the outer grain is the farm's pen catalog (~200 rows).
const shedFeedAnalyticsSQL = `
WITH iss AS (
    SELECT feed_direction_issue_id, feed_day
    FROM feed_direction_issues
    WHERE tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
      AND feed_day BETWEEN $3 AND $4
      AND state IN ('issued', 'amended', 'locked')
      AND workflow IN ('normal', 'experiment')
),
pen_item AS (
    SELECT r.park_id,
           r.shed_id,
           r.partition_key,
           r.feed_item_key,
           MAX(r.park_label)       AS park_label,
           MAX(r.shed_label)       AS shed_label,
           MAX(r.partition_label)  AS partition_label,
           MAX(r.feed_item_label)  AS feed_item_label,
           SUM(r.quantity_kg)      AS item_kg
    FROM iss i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1
     AND r.feed_direction_issue_id = i.feed_direction_issue_id
    GROUP BY r.park_id, r.shed_id, r.partition_key, r.feed_item_key
    -- "Feed GIVEN": an item whose whole-window total is zero (all blocked, or
    -- authored 0 every day) is omitted — a 0 kg line is not feed given, and on
    -- real sheets it drowned each pen's real mix under six zero rows. Summing
    -- the surviving items still equals the per-item chart series, because a
    -- zero total adds nothing there either.
    HAVING COALESCE(SUM(r.quantity_kg), 0) > 0
)
SELECT park_id::text,
       MAX(park_label)                                                     AS park_label,
       shed_id::text,
       MAX(shed_label)                                                     AS shed_label,
       CASE WHEN partition_key = 'whole' THEN ''
            ELSE COALESCE(MAX(partition_label), '') END                    AS partition_label,
       COALESCE(SUM(item_kg), 0)::text                                     AS pen_kg,
       COALESCE(
         jsonb_agg(
           jsonb_build_object(
             'feed_item_label', feed_item_label,
             'feed_item_key',   feed_item_key,
             'directed_kg',     item_kg::text
           )
           ORDER BY item_kg DESC, feed_item_label
         ) FILTER (WHERE item_kg IS NOT NULL),
         '[]'::jsonb
       )                                                                   AS items
FROM pen_item
GROUP BY park_id, shed_id, partition_key
ORDER BY MAX(park_label), MAX(shed_label), partition_key`

// shedFeedItemWire matches the jsonb_build_object keys above; built by this
// file's own SQL, so unknown keys cannot occur.
type shedFeedItemWire struct {
	FeedItemLabel string `json:"feed_item_label"`
	FeedItemKey   string `json:"feed_item_key"`
	DirectedKg    string `json:"directed_kg"`
}

// ShedFeedAnalytics serves the per-pen feed-mix rollup. One set-based read; the
// display string is composed by the canonical oploc helper, never hand-rolled.
func (r *Repository) ShedFeedAnalytics(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.ShedFeedAnalytics, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	from, to := domain.ClampAnalyticsWindow(q.DateFrom, q.DateTo)
	var parkIDs []uuid.UUID
	if len(q.ParkIDs) > 0 {
		parkIDs = q.ParkIDs
	}

	rows, err := r.pool.Query(ctx, shedFeedAnalyticsSQL,
		tenantID, parkIDs, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return domain.ShedFeedAnalytics{}, fmt.Errorf("feed analytics shed feed rollup: %w", err)
	}
	defer rows.Close()

	out := domain.ShedFeedAnalytics{Rows: []domain.ShedFeedPenRow{}}
	for rows.Next() {
		var row domain.ShedFeedPenRow
		var itemsJSON []byte
		if err := rows.Scan(&row.ParkID, &row.ParkLabel, &row.ShedID, &row.ShedLabel,
			&row.PartitionLabel, &row.DirectedKg, &itemsJSON); err != nil {
			return domain.ShedFeedAnalytics{}, fmt.Errorf("feed analytics shed feed scan: %w", err)
		}
		var wire []shedFeedItemWire
		if err := json.Unmarshal(itemsJSON, &wire); err != nil {
			return domain.ShedFeedAnalytics{}, fmt.Errorf("feed analytics shed feed items decode: %w", err)
		}
		row.Items = make([]domain.ShedFeedItemTotal, 0, len(wire))
		for _, it := range wire {
			row.Items = append(row.Items, domain.ShedFeedItemTotal(it))
		}
		row.OperationalLocationDisplay = oploc.OperationalLocation{
			ShedName: row.ShedLabel, PartitionLabel: row.PartitionLabel,
		}.Display()
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return domain.ShedFeedAnalytics{}, fmt.Errorf("feed analytics shed feed rows: %w", err)
	}
	return out, nil
}
