package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Herd Analytics — the Counts leadership read. Two canonical SQL reads in one
// batch: the live census rolled up four ways, and one row per India-calendar
// month of herd movement.
//
// scale-guard:ignore: 5k-50k-envelope — canonical indexed reads per
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md. Both queries are
// bounded aggregations over the tenant's own rows with no per-row fan-out and
// no page walk; this screen earns its own projection only under that ADR's
// scale-out ladder.

// projection-review: membership=canonical goats rows for the tenant with merged_into_goat_id IS NULL and lifecycle_status='alive', the identical population /counts/breakdown reports; group_key=breed | management_stage | sex | kid-adult band | park_id, one dimension per UNION branch, each ranging over the SAME per-animal key set as the total branch; join_cardinality=only the park branch joins locations, AFTER aggregation, on the (tenant_id, location_id) primary key, so strict 1:{0,1} with no fan-out, and no other branch joins anything; pagination=none, every series is a whole-scope rollup and never a page; scope=tenant_id plus one optional park equality predicate
//
// Expanded rationale:
//
//	producer key = goats primary key (tenant_id, goat_id). The CTE emits one row per animal and
//	               nothing is joined to it before grouping, so no animal can appear twice.
//	ratio keys   = the kid/adult branch and the `total` branch range over the identical animal
//	               key set (`live`), so kids + adults = total exactly. COALESCE(...,false) is
//	               load-bearing: herd_register_is_kid returns NULL for an unknown age band and a
//	               bare NOT would drop those animals from BOTH buckets.
const herdAnalyticsCompositionSQL = `
WITH live AS MATERIALIZED (
  SELECT
    g.goat_id,
    COALESCE(btrim(g.breed), '')            AS breed,
    COALESCE(btrim(g.management_stage), '') AS stage,
    COALESCE(g.sex, '')                     AS sex,
    COALESCE(herd_register_is_kid(g.age_band, g.management_stage), false) AS is_kid,
    g.park_id
  FROM goats g
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND g.lifecycle_status = 'alive'
    AND ($2 = '' OR g.park_id = NULLIF($2, '')::uuid)
)
SELECT 'breed'::text AS dim, breed AS key, breed AS label, count(*)::bigint AS animals
  FROM live GROUP BY breed
UNION ALL
SELECT 'stage', stage, stage, count(*)::bigint FROM live GROUP BY stage
UNION ALL
SELECT 'sex', sex, sex, count(*)::bigint FROM live GROUP BY sex
UNION ALL
SELECT 'age', CASE WHEN is_kid THEN 'kid' ELSE 'adult' END,
              CASE WHEN is_kid THEN 'kid' ELSE 'adult' END,
              count(*)::bigint
  FROM live GROUP BY 2, 3
UNION ALL
SELECT 'park',
       COALESCE(p.park_id::text, ''),
       COALESCE(NULLIF(loc.location_code, ''), loc.name, ''),
       p.animals
  FROM (SELECT park_id, count(*)::bigint AS animals FROM live GROUP BY park_id) p
  LEFT JOIN locations loc
         ON loc.tenant_id = $1::uuid AND loc.location_id = p.park_id
UNION ALL
SELECT 'total', '', '', count(*)::bigint FROM live
UNION ALL
SELECT 'kids', '', '', count(*) FILTER (WHERE is_kid)::bigint FROM live
UNION ALL
SELECT 'adults', '', '', count(*) FILTER (WHERE NOT is_kid)::bigint FROM live
`

// projection-review: membership=three DISJOINT canonical sources never joined to each other -- an animal's own origin columns (a birth), an animal's own exit columns (a death, sale or other exit), and an APPLIED shifting_events row (a movement); group_key=the IST calendar month of the event's own date on every branch, LEFT JOINed onto a generated month spine so a quiet month is a real zero rather than a dropped row; join_cardinality=imp pre-aggregates shifting_event_impacts to ONE row per event before it is joined, so the many side is collapsed and never counted through, and the month spine joins each aggregate 1:{0,1} on the month key; pagination=none, the whole window is returned and bounded to at most 36 months; scope=tenant_id plus one optional park predicate, matched on EITHER end for a movement because a movement out of a park is that park's movement too
//
// Expanded rationale:
//
//	producer key = goats (tenant_id, goat_id) for both animal branches; shifting_events
//	               (tenant_id, shifting_event_id) for the movement branch. A single animal
//	               therefore contributes at most one birth and at most one exit, and no row of
//	               any source can be counted under two kinds.
//	status matrix= the exit branch's three FILTERs are mutually exclusive by construction:
//	               exit_reason wins when present, and the lifecycle_status fallback applies only
//	               when exit_reason IS NULL. Every exit row therefore lands in exactly one of
//	               deaths / sold / other_exits, which is what makes net_change reconcile with the
//	               columns beside it. Pinned by
//	               TestHerdAnalyticsEveryStatusBucketIsDisjointAndComplete.
const herdAnalyticsFlowSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
),
month_spine AS (
  SELECT gs::date AS month_start, to_char(gs, 'YYYY-MM') AS month_key
  FROM bounds b,
       generate_series(date_trunc('month', b.from_date), date_trunc('month', b.to_date), interval '1 month') gs
),
births AS (
  SELECT to_char(COALESCE(g.dob, g.entry_date, (g.created_at AT TIME ZONE 'Asia/Kolkata')::date), 'YYYY-MM') AS month_key,
         count(*)::bigint AS births
  FROM goats g, bounds b
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND g.origin_type = 'birth'
    AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
    AND COALESCE(g.dob, g.entry_date, (g.created_at AT TIME ZONE 'Asia/Kolkata')::date)
        BETWEEN b.from_date AND b.to_date
  GROUP BY 1
),
exits AS (
  SELECT to_char(COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                          (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date), 'YYYY-MM') AS month_key,
         count(*) FILTER (WHERE g.exit_reason = 'died'  OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))::bigint AS deaths,
         count(*) FILTER (WHERE g.exit_reason = 'sold'  OR (g.exit_reason IS NULL AND g.lifecycle_status = 'sold'))::bigint AS sold,
         count(*) FILTER (WHERE g.exit_reason IN ('culled', 'transferred', 'lost')
                             OR (g.exit_reason IS NULL AND g.lifecycle_status IN ('culled', 'transferred', 'lost')))::bigint AS other_exits
  FROM goats g, bounds b
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND g.lifecycle_status IN ('dead', 'sold', 'culled', 'transferred', 'lost')
    AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
    AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                 (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date)
        BETWEEN b.from_date AND b.to_date
  GROUP BY 1
),
imp AS (
  SELECT i.shifting_event_id, sum(i.head_count)::bigint AS head_count
  FROM shifting_event_impacts i
  WHERE i.tenant_id = $1::uuid
  GROUP BY i.shifting_event_id
),
moves AS (
  SELECT to_char((se.completed_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM') AS month_key,
         count(*)::bigint AS movements,
         COALESCE(sum(imp.head_count), 0)::bigint AS animals_moved
  FROM shifting_events se
  LEFT JOIN imp ON imp.shifting_event_id = se.shifting_event_id
  CROSS JOIN bounds b
  WHERE se.tenant_id = $1::uuid
    AND se.event_status = 'applied'
    AND se.completed_at IS NOT NULL
    AND ($4 = '' OR se.destination_park_id = NULLIF($4, '')::uuid
                 OR se.source_park_id = NULLIF($4, '')::uuid)
    AND (se.completed_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN b.from_date AND b.to_date
  GROUP BY 1
)
SELECT
  m.month_key,
  m.month_start,
  COALESCE(births.births, 0)         AS births,
  COALESCE(exits.deaths, 0)          AS deaths,
  COALESCE(exits.sold, 0)            AS sold,
  COALESCE(exits.other_exits, 0)     AS other_exits,
  COALESCE(moves.movements, 0)       AS movements,
  COALESCE(moves.animals_moved, 0)   AS animals_moved
FROM month_spine m
LEFT JOIN births ON births.month_key = m.month_key
LEFT JOIN exits  ON exits.month_key  = m.month_key
LEFT JOIN moves  ON moves.month_key  = m.month_key
ORDER BY m.month_start
`

// GetHerdAnalytics serves the Counts -> Herd Analytics page: the live census
// composition beside the month-by-month herd movement over the requested window.
func (r *Repository) GetHerdAnalytics(ctx context.Context, req domain.HerdAnalyticsQuery) (domain.HerdAnalytics, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// The window is an INDIA BUSINESS DAY range, never a rolling clock instant.
	// Bounds resolve through the same exported parser the handler validated them
	// with, and an unparseable or reversed pair falls back to the default window
	// rather than erroring here: the handler already rejects those with a 400, so
	// reaching this point with one means an internal caller, and serving the
	// default beats returning nothing.
	defaultFrom, defaultTo := domain.HerdAnalyticsDefaultWindow(time.Now())
	fromDate, errFrom := domain.ParseHerdAnalyticsDate(req.FromDate)
	toDate, errTo := domain.ParseHerdAnalyticsDate(req.ToDate)
	if errFrom != nil || errTo != nil || toDate.Before(fromDate) {
		fromDate, _ = domain.ParseHerdAnalyticsDate(defaultFrom)
		toDate, _ = domain.ParseHerdAnalyticsDate(defaultTo)
	}

	parkID := ptrValue(req.ParkID)

	batch := &pgx.Batch{}
	batch.Queue(herdAnalyticsCompositionSQL, req.TenantID, parkID)
	batch.Queue(herdAnalyticsFlowSQL, req.TenantID, fromDate.Format(domain.HerdAnalyticsDateLayout), toDate.Format(domain.HerdAnalyticsDateLayout), parkID)

	results := r.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()

	out := domain.HerdAnalytics{
		WindowFrom:  fromDate.Format(domain.HerdAnalyticsDateLayout),
		WindowTo:    toDate.Format(domain.HerdAnalyticsDateLayout),
		Months:      []domain.HerdAnalyticsMonth{},
		Breed:       []domain.HerdAnalyticsSeriesPoint{},
		Stage:       []domain.HerdAnalyticsSeriesPoint{},
		Sex:         []domain.HerdAnalyticsSeriesPoint{},
		AgeBand:     []domain.HerdAnalyticsSeriesPoint{},
		Park:        []domain.HerdAnalyticsSeriesPoint{},
		GeneratedAt: time.Now().In(biztime.DefaultLocation()),
	}

	compositionRows, err := results.Query()
	if err != nil {
		return domain.HerdAnalytics{}, fmt.Errorf("herd analytics: composition query: %w", err)
	}
	for compositionRows.Next() {
		var dim string
		var point domain.HerdAnalyticsSeriesPoint
		if err := compositionRows.Scan(&dim, &point.Key, &point.Label, &point.Count); err != nil {
			compositionRows.Close()
			return domain.HerdAnalytics{}, fmt.Errorf("herd analytics: composition scan: %w", err)
		}
		switch dim {
		case "breed":
			out.Breed = append(out.Breed, point)
		case "stage":
			out.Stage = append(out.Stage, point)
		case "sex":
			out.Sex = append(out.Sex, point)
		case "age":
			out.AgeBand = append(out.AgeBand, point)
		case "park":
			out.Park = append(out.Park, point)
		case "total":
			out.Totals.LiveAnimals = point.Count
		case "kids":
			out.Totals.Kids = point.Count
		case "adults":
			out.Totals.Adults = point.Count
		}
	}
	compositionRows.Close()
	if err := compositionRows.Err(); err != nil {
		return domain.HerdAnalytics{}, fmt.Errorf("herd analytics: composition rows: %w", err)
	}

	flowRows, err := results.Query()
	if err != nil {
		return domain.HerdAnalytics{}, fmt.Errorf("herd analytics: flow query: %w", err)
	}
	for flowRows.Next() {
		var month domain.HerdAnalyticsMonth
		var monthStart time.Time
		if err := flowRows.Scan(
			&month.Month,
			&monthStart,
			&month.Births,
			&month.Deaths,
			&month.Sold,
			&month.OtherExits,
			&month.Movements,
			&month.AnimalsMoved,
		); err != nil {
			flowRows.Close()
			return domain.HerdAnalytics{}, fmt.Errorf("herd analytics: flow scan: %w", err)
		}
		month.Label = monthStart.Format("Jan 2006")
		month.NetChange = month.Births - (month.Deaths + month.Sold + month.OtherExits)
		out.Months = append(out.Months, month)

		out.Totals.Births += month.Births
		out.Totals.Deaths += month.Deaths
		out.Totals.Sold += month.Sold
		out.Totals.OtherExits += month.OtherExits
		out.Totals.Movements += month.Movements
		out.Totals.AnimalsMoved += month.AnimalsMoved
	}
	flowRows.Close()
	if err := flowRows.Err(); err != nil {
		return domain.HerdAnalytics{}, fmt.Errorf("herd analytics: flow rows: %w", err)
	}
	out.Totals.NetChange = out.Totals.Births - (out.Totals.Deaths + out.Totals.Sold + out.Totals.OtherExits)

	return out, nil
}
