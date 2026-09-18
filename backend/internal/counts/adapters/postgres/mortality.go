package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// Mortality — the Counts leadership read on deaths. Three canonical SQL reads in one
// batch: the live population with the window's deaths flagged (every RATE series), the deaths
// alone sliced by facts about the death (every COUNT series, the months and the cross
// tabs), and a bounded most-recent list.
//
// scale-guard:ignore: 5k-50k-envelope — canonical indexed reads per
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md. Each statement is a bounded
// aggregation over the tenant's own rows with no per-row fan-out and no page walk; this
// screen earns its own projection only under that ADR's scale-out ladder.

// projection-review: membership=canonical goats rows for the tenant with merged_into_goat_id IS NULL that are either LIVE today (the Counts Breakdown head count) or died on an IST day inside the window, each row flagged live / died so a bucket's animals and its deaths are counted off the same rows; group_key=kid/adult band | management_stage | breed | sex | species | park_id | (shed_id, normalized partition) | load_id, one dimension per UNION branch, each ranging over the SAME per-animal key set as the total branch, with the death flag evaluated per animal BEFORE grouping; join_cardinality=goat_shed_partitions is PK (tenant_id, goat_id) so 1:{0,1}, member is DISTINCT ON goat_id so 1:{0,1}, and locations / procurement_loads are primary-key label lookups AFTER aggregation; pagination=none, every series is a whole-scope rollup; scope=tenant_id plus one optional park equality predicate
//
// Expanded rationale:
//
//	producer key = goats primary key (tenant_id, goat_id). The CTE emits one row per animal;
//	               the two LEFT JOINs are each 1:{0,1} by primary key / DISTINCT ON, so no
//	               animal can appear twice and deaths/animals range over identical rows.
//	ratio keys   = every branch's deaths FILTER and its live FILTER range over the same `pop`
//	               rows grouped by the same attribute, so rate_pct = deaths / animals divides a
//	               section's deaths by that same section's head count (maintainer decision
//	               2026-09-18: the denominator is "how many animals are in that section", i.e.
//	               the live count Counts Breakdown reports, not an at-risk population).
//	status matrix= died is exit_reason='died' OR (NULL exit_reason AND lifecycle 'dead'), the
//	               identical predicate Herd Analytics' deaths column uses, so the two Counts
//	               screens cannot disagree about how many died. Pinned by
//	               TestMortalityDeathsMatchHerdAnalytics.
const mortalityPopulationSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
),
member AS (
  SELECT DISTINCT ON (plg.goat_id) plg.goat_id, plg.load_id
  FROM procurement_load_goats plg
  WHERE plg.tenant_id = $1::uuid AND plg.current_state = 'accepted_herd_intake'
  ORDER BY plg.goat_id, plg.intake_accepted_at DESC NULLS LAST, plg.created_at DESC, plg.load_goat_id DESC
),
pop AS MATERIALIZED (
  SELECT
    g.goat_id,
    (g.exit_reason = 'died' OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))
      AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                   (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN b.from_date AND b.to_date AS died,
    g.lifecycle_status NOT IN ('dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive') AS live,
    COALESCE(btrim(g.breed), '')            AS breed,
    COALESCE(btrim(g.management_stage), '') AS stage,
    COALESCE(g.sex, '')                     AS sex,
    COALESCE(lower(g.species), '')          AS species,
    COALESCE(herd_register_is_kid(g.age_band, g.management_stage), false) AS is_kid,
    g.park_id,
    g.shed_id,
    COALESCE(gsp.partition_label, '')       AS partition_label,
    CASE WHEN g.origin_type = 'birth' THEN 'farm_born'
         WHEN m.load_id IS NOT NULL THEN m.load_id::text
         ELSE 'no_load' END                 AS load_key
  FROM goats g
  CROSS JOIN bounds b
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1::uuid AND gsp.goat_id = g.goat_id
  LEFT JOIN member m ON m.goat_id = g.goat_id
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
    AND (
      g.lifecycle_status NOT IN ('dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive')
      OR
      ((g.exit_reason = 'died' OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))
         AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                      (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN b.from_date AND b.to_date)
    )
)
SELECT 'total'::text AS dim, ''::text AS key, ''::text AS label, ''::text AS extra,
       count(*) FILTER (WHERE died)::bigint AS deaths, count(*) FILTER (WHERE live)::bigint AS animals
  FROM pop
UNION ALL
SELECT 'kid_adult', CASE WHEN is_kid THEN 'kid' ELSE 'adult' END, '', '',
       count(*) FILTER (WHERE died)::bigint, count(*) FILTER (WHERE live)::bigint
  FROM pop GROUP BY 2
UNION ALL
SELECT 'stage', stage, stage, '', count(*) FILTER (WHERE died)::bigint, count(*) FILTER (WHERE live)::bigint
  FROM pop GROUP BY stage
UNION ALL
SELECT 'breed', breed, breed, '', count(*) FILTER (WHERE died)::bigint, count(*) FILTER (WHERE live)::bigint
  FROM pop GROUP BY breed
UNION ALL
SELECT 'sex', sex, sex, '', count(*) FILTER (WHERE died)::bigint, count(*) FILTER (WHERE live)::bigint
  FROM pop GROUP BY sex
UNION ALL
SELECT 'species', species, species, '', count(*) FILTER (WHERE died)::bigint, count(*) FILTER (WHERE live)::bigint
  FROM pop GROUP BY species
UNION ALL
SELECT 'park', COALESCE(p.park_id::text, ''), COALESCE(NULLIF(loc.location_code, ''), loc.name, ''), '',
       p.deaths, p.animals
  FROM (SELECT park_id, count(*) FILTER (WHERE died)::bigint AS deaths, count(*) FILTER (WHERE live)::bigint AS animals
          FROM pop GROUP BY park_id) p
  LEFT JOIN locations loc ON loc.tenant_id = $1::uuid AND loc.location_id = p.park_id
UNION ALL
-- Pens: only those that saw a death. 175 pens with a zero apiece is noise; the rate a pen
-- carries is what the reader compares. Key is shed_id + normalized partition so two
-- same-named sheds in different parks never merge (Rule 4), label parts compose in Go.
SELECT 'pen', COALESCE(p.shed_id::text, '') || ':' || p.partition_key,
       COALESCE(NULLIF(shed.name, ''), shed.location_code, ''), p.partition_label,
       p.deaths, p.animals
  FROM (SELECT shed_id,
               lower(regexp_replace(partition_label, '[^A-Za-z0-9]+', '', 'g')) AS partition_key,
               min(partition_label) AS partition_label,
               count(*) FILTER (WHERE died)::bigint AS deaths, count(*) FILTER (WHERE live)::bigint AS animals
          FROM pop
         GROUP BY shed_id, lower(regexp_replace(partition_label, '[^A-Za-z0-9]+', '', 'g'))
        HAVING count(*) FILTER (WHERE died) > 0) p
  LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = p.shed_id
UNION ALL
SELECT 'load', l.load_key,
       CASE WHEN l.load_key IN ('farm_born', 'no_load') THEN ''
            ELSE COALESCE(NULLIF(pl.context->>'load_ref', ''), to_char(pl.purchase_date, 'DD Mon YYYY'), '') END,
       COALESCE(pl.purchase_date::text, ''),
       l.deaths, l.animals
  FROM (SELECT load_key, count(*) FILTER (WHERE died)::bigint AS deaths, count(*) FILTER (WHERE live)::bigint AS animals
          FROM pop GROUP BY load_key
        HAVING count(*) FILTER (WHERE died) > 0 OR load_key IN ('farm_born', 'no_load')) l
  -- The synthetic keys are not uuids, and a bare cast in the ON clause is evaluated for
  -- every row regardless of the other predicate, so the cast is guarded by shape.
  LEFT JOIN procurement_loads pl
         ON pl.tenant_id = $1::uuid
        AND pl.load_id = CASE WHEN l.load_key ~ '^[0-9a-f-]{36}$' THEN l.load_key::uuid END
`

// projection-review: membership=goats that died (exit_reason 'died', or NULL exit_reason with lifecycle 'dead') on an IST day inside the window, optionally one park -- the SAME predicate the population read flags `died` with, so every COUNT series here totals the deaths tile there; group_key=one derived fact of the death per UNION branch (IST month, age band, season, cause, days since arrival, days since last accepted vaccination) or one pair for a cross tab, each ranging over the identical `dead` row set; join_cardinality=health_death_causes is PK (tenant_id, goat_id) so 1:{0,1}, and the inferred-case, last-vaccination and load lookups are LATERAL subqueries returning exactly ONE row by construction (a bare aggregate over zero-or-more rows, or LIMIT 1), so none can multiply an animal; pagination=none, whole-window rollups; scope=tenant_id plus one optional park equality predicate
//
// Expanded rationale:
//
//	producer key = goats (tenant_id, goat_id): an animal has at most one exit, so it is one
//	               death and lands in exactly one bucket of every branch.
//	cause basis  = three DISJOINT bases by construction -- a recorded row wins; otherwise a
//	               closed_dead case that was open when the animal died is inferred; otherwise
//	               none. Their three totals therefore partition deaths exactly, pinned by
//	               TestMortalityCauseBasesPartitionDeaths. The inferred LATERAL AGGREGATES the
//	               case names rather than ranking them: a co-morbid animal really can die
//	               with two open cases, and ORDER BY ... LIMIT 1 would name one of two true
//	               diseases and silently flip between them.
//	season/age   = band KEYS are emitted here and LABELLED in Go (domain.Mortality*Label), so
//	               the boundaries live in one place and a test pins them without a database.
//	               The CASE thresholds below mirror domain.MortalityAgeBandKey /
//	               MortalityDaysSinceKey exactly; TestMortalityBandKeysMatchDomain pins that.
const mortalityDeathsSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
),
dead AS MATERIALIZED (
  SELECT
    g.goat_id,
    COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
             (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) AS died_on,
    COALESCE(g.exited_at, g.updated_at)     AS died_at,
    COALESCE(btrim(g.breed), '')            AS breed,
    COALESCE(btrim(g.management_stage), '') AS stage,
    COALESCE(herd_register_is_kid(g.age_band, g.management_stage), false) AS is_kid,
    g.dob,
    g.origin_type,
    g.entry_date,
    m.load_id,
    dc.cause_key,
    inferred.disease_label
  FROM goats g
  CROSS JOIN bounds b
  LEFT JOIN health_death_causes dc ON dc.tenant_id = $1::uuid AND dc.goat_id = g.goat_id
  LEFT JOIN LATERAL (
    SELECT plg.load_id
    FROM procurement_load_goats plg
    WHERE plg.tenant_id = $1::uuid AND plg.goat_id = g.goat_id AND plg.current_state = 'accepted_herd_intake'
    ORDER BY plg.intake_accepted_at DESC NULLS LAST, plg.created_at DESC, plg.load_goat_id DESC
    LIMIT 1
  ) m ON true
  LEFT JOIN LATERAL (
    SELECT string_agg(DISTINCT c.disease_name, ' · ' ORDER BY c.disease_name) AS disease_label
    FROM health_cases c
    WHERE c.tenant_id = $1::uuid AND c.goat_id = g.goat_id
      AND c.status IN ('closed_dead', 'held_death_review')
      AND c.start_date <= COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                                   (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date)
      AND (c.closed_at IS NULL OR (c.closed_at AT TIME ZONE 'Asia/Kolkata')::date >= COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                                                                                               (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date))
  ) inferred ON true
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND (g.exit_reason = 'died' OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))
    AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
    AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                 (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN b.from_date AND b.to_date
),
facts AS MATERIALIZED (
  SELECT
    d.*,
    to_char(d.died_on, 'YYYY-MM') AS month_key,
    CASE EXTRACT(MONTH FROM d.died_on)::int
      WHEN 3 THEN 'summer' WHEN 4 THEN 'summer' WHEN 5 THEN 'summer'
      WHEN 6 THEN 'monsoon' WHEN 7 THEN 'monsoon' WHEN 8 THEN 'monsoon' WHEN 9 THEN 'monsoon'
      WHEN 10 THEN 'post_monsoon' WHEN 11 THEN 'post_monsoon'
      ELSE 'winter' END AS season,
    CASE WHEN d.dob IS NULL OR d.died_on < d.dob THEN 'unknown'
         WHEN d.died_on - d.dob <= 7   THEN 'd0_7'
         WHEN d.died_on - d.dob <= 30  THEN 'd8_30'
         WHEN d.died_on - d.dob <= 90  THEN 'd31_90'
         WHEN d.died_on - d.dob <= 180 THEN 'd91_180'
         WHEN d.died_on - d.dob <= 365 THEN 'd181_365'
         ELSE 'over_1y' END AS age_band,
    -- Days on the farm before the death, procured animals only. The per-goat arrival stamp
    -- wins; the load's arrival day and the animal's own entry date are the fallbacks.
    CASE WHEN d.origin_type = 'birth' THEN NULL
         ELSE d.died_on - COALESCE((arr.arrived_at AT TIME ZONE 'Asia/Kolkata')::date, arr.arrived_on, d.entry_date) END AS days_since_arrival,
    CASE WHEN vax.last_at IS NULL THEN NULL
         ELSE d.died_on - (vax.last_at AT TIME ZONE 'Asia/Kolkata')::date END AS days_since_vaccine,
    CASE WHEN d.cause_key IS NOT NULL THEN 'recorded'
         WHEN COALESCE(d.disease_label, '') <> '' THEN 'inferred'
         ELSE 'none' END AS cause_basis,
    CASE WHEN d.cause_key IS NOT NULL THEN d.cause_key
         WHEN COALESCE(d.disease_label, '') <> '' THEN 'inferred:' || d.disease_label
         ELSE '' END AS cause_col_key,
    CASE WHEN d.cause_key IS NOT NULL THEN ''
         ELSE COALESCE(d.disease_label, '') END AS cause_col_label,
    CASE WHEN d.origin_type = 'birth' THEN 'farm_born'
         WHEN d.load_id IS NOT NULL THEN d.load_id::text
         ELSE 'no_load' END AS load_key
  FROM dead d
  LEFT JOIN LATERAL (
    SELECT plg.arrived_at, pl.arrived_on
    FROM procurement_load_goats plg
    JOIN procurement_loads pl ON pl.tenant_id = $1::uuid AND pl.load_id = plg.load_id
    WHERE plg.tenant_id = $1::uuid AND plg.goat_id = d.goat_id AND plg.load_id = d.load_id
    LIMIT 1
  ) arr ON true
  LEFT JOIN LATERAL (
    SELECT max(vc.administered_at) AS last_at
    FROM vaccination_completions vc
    WHERE vc.tenant_id = $1::uuid AND vc.goat_id = d.goat_id
      AND vc.status IN ('recorded', 'accepted')
      AND vc.administered_at <= d.died_at
  ) vax ON true
)
SELECT 'month'::text AS dim, month_key AS key, ''::text AS label, ''::text AS key2, ''::text AS label2,
       count(*)::bigint AS deaths, count(*) FILTER (WHERE is_kid)::bigint AS kids
  FROM facts GROUP BY month_key
UNION ALL
SELECT 'age', age_band, '', '', '', count(*)::bigint, 0 FROM facts GROUP BY age_band
UNION ALL
SELECT 'season', season, '', '', '', count(*)::bigint, 0 FROM facts GROUP BY season
UNION ALL
SELECT 'cause', cause_col_key, cause_col_label, cause_basis, '', count(*)::bigint, 0
  FROM facts GROUP BY cause_col_key, cause_col_label, cause_basis
UNION ALL
SELECT 'arrival',
       CASE WHEN days_since_arrival IS NULL OR days_since_arrival < 0 THEN 'unknown'
            WHEN days_since_arrival <= 7 THEN 'd0_7'
            WHEN days_since_arrival <= 30 THEN 'd8_30'
            WHEN days_since_arrival <= 90 THEN 'd31_90'
            ELSE 'over_90' END, '', '', '', count(*)::bigint, 0
  FROM facts GROUP BY 2
UNION ALL
SELECT 'vaccine',
       CASE WHEN days_since_vaccine IS NULL OR days_since_vaccine < 0 THEN 'unknown'
            WHEN days_since_vaccine <= 7 THEN 'd0_7'
            WHEN days_since_vaccine <= 30 THEN 'd8_30'
            WHEN days_since_vaccine <= 90 THEN 'd31_90'
            ELSE 'over_90' END, '', '', '', count(*)::bigint, 0
  FROM facts GROUP BY 2
UNION ALL
SELECT 'season_by_stage', season, '', stage, stage, count(*)::bigint, 0
  FROM facts GROUP BY season, stage
UNION ALL
SELECT 'load_by_cause', f.load_key,
       CASE WHEN f.load_key IN ('farm_born', 'no_load') THEN ''
            ELSE COALESCE(NULLIF(pl.context->>'load_ref', ''), to_char(pl.purchase_date, 'DD Mon YYYY'), '') END,
       f.cause_col_key, f.cause_col_label, f.deaths, 0
  FROM (SELECT load_key, cause_col_key, cause_col_label, count(*)::bigint AS deaths
          FROM facts GROUP BY load_key, cause_col_key, cause_col_label) f
  LEFT JOIN procurement_loads pl
         ON pl.tenant_id = $1::uuid
        AND pl.load_id = CASE WHEN f.load_key ~ '^[0-9a-f-]{36}$' THEN f.load_key::uuid END
UNION ALL
SELECT 'breed_by_cause', breed, breed, cause_col_key, cause_col_label, count(*)::bigint, 0
  FROM facts GROUP BY breed, cause_col_key, cause_col_label
UNION ALL
SELECT 'first_week', '', '', '', '', count(*) FILTER (WHERE age_band = 'd0_7')::bigint, 0 FROM facts
`

// projection-review: membership=the same window deaths as mortalityDeathsSQL, capped at $5 rows most-recent-first; group_key=none, one row per animal; join_cardinality=park/shed/partition/cause are primary-key LEFT JOINs (1:{0,1}) and the tag, load and inferred-case lookups are LATERAL subqueries returning exactly ONE row (LIMIT 1 or a bare aggregate), so no animal is multiplied; pagination=a hard LIMIT, and every count above this list is computed by its own query and does not move with it; scope=tenant_id plus one optional park predicate
const mortalityRecentSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
),
dead AS (
  SELECT g.goat_id, g.display_id, g.park_id, g.shed_id, g.dob,
         COALESCE(btrim(g.breed), '') AS breed, COALESCE(g.sex, '') AS sex,
         COALESCE(btrim(g.management_stage), '') AS stage,
         COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                  (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) AS died_on
  FROM goats g, bounds b
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND (g.exit_reason = 'died' OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))
    AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
    AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                 (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN b.from_date AND b.to_date
  ORDER BY 9 DESC, g.goat_id
  LIMIT $5
)
SELECT d.goat_id::text, d.display_id,
       COALESCE(tag.identifier_value, ''),
       d.died_on,
       d.breed, d.sex, d.stage,
       CASE WHEN d.dob IS NULL OR d.died_on < d.dob THEN NULL ELSE (d.died_on - d.dob)::bigint END AS age_days,
       COALESCE(NULLIF(pk.location_code, ''), pk.name, ''),
       COALESCE(NULLIF(shed.name, ''), shed.location_code, ''),
       COALESCE(gsp.partition_label, ''),
       COALESCE(NULLIF(pl.context->>'load_ref', ''), to_char(pl.purchase_date, 'DD Mon YYYY'), ''),
       COALESCE(dc.cause_key, ''),
       COALESCE(inferred.disease_label, '')
FROM dead d
LEFT JOIN locations pk   ON pk.tenant_id = $1::uuid AND pk.location_id = d.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = d.shed_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1::uuid AND gsp.goat_id = d.goat_id
LEFT JOIN health_death_causes dc ON dc.tenant_id = $1::uuid AND dc.goat_id = d.goat_id
LEFT JOIN LATERAL (
  SELECT gi.identifier_value
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid AND gi.goat_id = d.goat_id AND gi.status = 'active'
  ORDER BY (gi.identifier_type = 'animal_identifier_1') DESC, gi.identifier_value
  LIMIT 1
) tag ON true
LEFT JOIN LATERAL (
  SELECT plg.load_id
  FROM procurement_load_goats plg
  WHERE plg.tenant_id = $1::uuid AND plg.goat_id = d.goat_id AND plg.current_state = 'accepted_herd_intake'
  ORDER BY plg.intake_accepted_at DESC NULLS LAST, plg.created_at DESC, plg.load_goat_id DESC
  LIMIT 1
) m ON true
LEFT JOIN procurement_loads pl ON pl.tenant_id = $1::uuid AND pl.load_id = m.load_id
LEFT JOIN LATERAL (
  SELECT string_agg(DISTINCT c.disease_name, ' · ' ORDER BY c.disease_name) AS disease_label
  FROM health_cases c
  WHERE c.tenant_id = $1::uuid AND c.goat_id = d.goat_id
    AND c.status IN ('closed_dead', 'held_death_review')
    AND c.start_date <= d.died_on
    AND (c.closed_at IS NULL OR (c.closed_at AT TIME ZONE 'Asia/Kolkata')::date >= d.died_on)
) inferred ON true
ORDER BY d.died_on DESC, d.goat_id
`

// GetMortality serves the Counts -> Mortality page.
func (r *Repository) GetMortality(ctx context.Context, req domain.MortalityQuery) (domain.Mortality, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Same window resolution as Herd Analytics: the handler already rejected anything
	// malformed with a 400, so an unparseable pair here is an internal caller and the
	// default window beats returning nothing.
	defaultFrom, defaultTo := domain.HerdAnalyticsDefaultWindow(time.Now())
	fromDate, errFrom := domain.ParseHerdAnalyticsDate(req.FromDate)
	toDate, errTo := domain.ParseHerdAnalyticsDate(req.ToDate)
	if errFrom != nil || errTo != nil || toDate.Before(fromDate) {
		fromDate, _ = domain.ParseHerdAnalyticsDate(defaultFrom)
		toDate, _ = domain.ParseHerdAnalyticsDate(defaultTo)
	}
	from := fromDate.Format(domain.HerdAnalyticsDateLayout)
	to := toDate.Format(domain.HerdAnalyticsDateLayout)
	parkID := ptrValue(req.ParkID)

	batch := &pgx.Batch{}
	batch.Queue(mortalityPopulationSQL, req.TenantID, from, to, parkID)
	batch.Queue(mortalityDeathsSQL, req.TenantID, from, to, parkID)
	batch.Queue(mortalityRecentSQL, req.TenantID, from, to, parkID, domain.MortalityRecentLimit)

	results := r.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()

	out := domain.Mortality{
		WindowFrom:       from,
		WindowTo:         to,
		Months:           []domain.MortalityMonth{},
		KidAdult:         []domain.MortalityBucket{},
		Stage:            []domain.MortalityBucket{},
		Breed:            []domain.MortalityBucket{},
		Sex:              []domain.MortalityBucket{},
		Species:          []domain.MortalityBucket{},
		Park:             []domain.MortalityBucket{},
		Pen:              []domain.MortalityBucket{},
		Load:             []domain.MortalityBucket{},
		AgeAtDeath:       []domain.MortalityBucket{},
		Season:           []domain.MortalityBucket{},
		Cause:            []domain.MortalityBucket{},
		DaysSinceArrival: []domain.MortalityBucket{},
		DaysSinceVaccine: []domain.MortalityBucket{},
		SeasonByStage:    []domain.MortalityCrossCell{},
		LoadByCause:      []domain.MortalityCrossCell{},
		BreedByCause:     []domain.MortalityCrossCell{},
		Deaths:           []domain.MortalityDeath{},
		RecentLimit:      domain.MortalityRecentLimit,
		GeneratedAt:      time.Now().In(biztime.DefaultLocation()),
	}

	// ---- 1. population: every RATE series -------------------------------------------
	popRows, err := results.Query()
	if err != nil {
		return domain.Mortality{}, fmt.Errorf("mortality: population query: %w", err)
	}
	for popRows.Next() {
		var dim, key, label, extra string
		var deaths, animals int64
		if err := popRows.Scan(&dim, &key, &label, &extra, &deaths, &animals); err != nil {
			popRows.Close()
			return domain.Mortality{}, fmt.Errorf("mortality: population scan: %w", err)
		}
		bucket := domain.MortalityBucket{Key: key, Label: label, Deaths: deaths, Animals: animals, RatePct: domain.MortalityRatePct(deaths, animals)}
		switch dim {
		case "total":
			out.Totals.Deaths, out.Totals.Animals, out.Totals.RatePct = deaths, animals, bucket.RatePct
		case "kid_adult":
			bucket.Label = domain.MortalityKidAdultLabel(key)
			out.KidAdult = append(out.KidAdult, bucket)
			if key == "kid" {
				out.Totals.KidDeaths, out.Totals.KidAnimals, out.Totals.KidRatePct = deaths, animals, bucket.RatePct
			} else {
				out.Totals.AdultDeaths, out.Totals.AdultAnimals, out.Totals.AdultRatePct = deaths, animals, bucket.RatePct
			}
		case "stage":
			out.Stage = append(out.Stage, bucket)
		case "breed":
			out.Breed = append(out.Breed, bucket)
		case "sex":
			out.Sex = append(out.Sex, bucket)
		case "species":
			out.Species = append(out.Species, bucket)
		case "park":
			out.Park = append(out.Park, bucket)
		case "pen":
			// label = shed name, extra = the human partition label; composed through the ONE
			// canonical helper so "Godel 1 - Part 3" here is the same string every other
			// screen prints (Rule 5).
			bucket.Label = oploc.OperationalLocation{ShedName: label, PartitionLabel: extra}.Display()
			out.Pen = append(out.Pen, bucket)
		case "load":
			bucket.Label = loadBucketLabel(key, label)
			out.Load = append(out.Load, bucket)
		}
	}
	popRows.Close()
	if err := popRows.Err(); err != nil {
		return domain.Mortality{}, fmt.Errorf("mortality: population rows: %w", err)
	}

	// ---- 2. deaths: every COUNT series, months and cross tabs --------------------------
	monthDeaths := map[string]domain.MortalityMonth{}
	ageCounts := map[string]int64{}
	seasonCounts := map[string]int64{}
	arrivalCounts := map[string]int64{}
	vaccineCounts := map[string]int64{}
	deathRows, err := results.Query()
	if err != nil {
		return domain.Mortality{}, fmt.Errorf("mortality: deaths query: %w", err)
	}
	for deathRows.Next() {
		var dim, key, label, key2, label2 string
		var deaths, kids int64
		if err := deathRows.Scan(&dim, &key, &label, &key2, &label2, &deaths, &kids); err != nil {
			deathRows.Close()
			return domain.Mortality{}, fmt.Errorf("mortality: deaths scan: %w", err)
		}
		switch dim {
		case "month":
			monthDeaths[key] = domain.MortalityMonth{Month: key, Deaths: deaths, Kids: kids, Adults: deaths - kids}
		case "age":
			ageCounts[key] = deaths
		case "season":
			seasonCounts[key] = deaths
		case "cause":
			// key2 carries the basis. A recorded cause arrives with an EMPTY label for the
			// app layer to resolve through Health's vocabulary.
			out.Cause = append(out.Cause, domain.MortalityBucket{Key: key, Label: label, Deaths: deaths, Basis: key2})
			switch key2 {
			case domain.MortalityCauseRecorded:
				out.Totals.CauseRecorded += deaths
			case domain.MortalityCauseInferred:
				out.Totals.CauseInferred += deaths
			default:
				out.Totals.CauseNone += deaths
			}
		case "arrival":
			arrivalCounts[key] = deaths
		case "vaccine":
			vaccineCounts[key] = deaths
		case "season_by_stage":
			out.SeasonByStage = append(out.SeasonByStage, domain.MortalityCrossCell{
				RowKey: key, RowLabel: domain.MortalitySeasonLabel(key), ColKey: key2, ColLabel: label2, Deaths: deaths,
			})
		case "load_by_cause":
			out.LoadByCause = append(out.LoadByCause, domain.MortalityCrossCell{
				RowKey: key, RowLabel: loadBucketLabel(key, label), ColKey: key2, ColLabel: label2, Deaths: deaths,
			})
		case "breed_by_cause":
			out.BreedByCause = append(out.BreedByCause, domain.MortalityCrossCell{
				RowKey: key, RowLabel: label, ColKey: key2, ColLabel: label2, Deaths: deaths,
			})
		case "first_week":
			out.Totals.FirstWeekDeaths = deaths
		}
	}
	deathRows.Close()
	if err := deathRows.Err(); err != nil {
		return domain.Mortality{}, fmt.Errorf("mortality: deaths rows: %w", err)
	}

	// Month spine: a quiet month is a real zero, never a dropped point.
	for cursor := time.Date(fromDate.Year(), fromDate.Month(), 1, 0, 0, 0, 0, fromDate.Location()); !cursor.After(toDate); cursor = cursor.AddDate(0, 1, 0) {
		key := cursor.Format("2006-01")
		month := monthDeaths[key]
		month.Month = key
		month.Label = cursor.Format("Jan 2006")
		out.Months = append(out.Months, month)
	}
	// Fixed-order band series, every band present even at zero, so a reader sees "none in
	// summer" rather than a missing bar.
	for _, key := range domain.MortalityAgeBandOrder {
		out.AgeAtDeath = append(out.AgeAtDeath, domain.MortalityBucket{Key: key, Label: domain.MortalityAgeBandLabel(key), Deaths: ageCounts[key]})
	}
	for _, key := range domain.MortalitySeasonOrder {
		out.Season = append(out.Season, domain.MortalityBucket{Key: key, Label: domain.MortalitySeasonLabel(key), Deaths: seasonCounts[key]})
	}
	for _, key := range append(append([]string{}, domain.MortalityDaysSinceOrder...), "unknown") {
		out.DaysSinceArrival = append(out.DaysSinceArrival, domain.MortalityBucket{Key: key, Label: domain.MortalityDaysSinceArrivalLabel(key), Deaths: arrivalCounts[key]})
		out.DaysSinceVaccine = append(out.DaysSinceVaccine, domain.MortalityBucket{Key: key, Label: domain.MortalityDaysSinceVaccineLabel(key), Deaths: vaccineCounts[key]})
	}
	// Rate series: most deaths first, then most animals, then label, so the chart reads
	// top-down and two equal buckets keep a stable order across reloads.
	for _, series := range []*[]domain.MortalityBucket{&out.Stage, &out.Breed, &out.Sex, &out.Species, &out.Park, &out.Pen, &out.Load, &out.KidAdult} {
		sortBuckets(*series)
	}
	sortBuckets(out.Cause)

	// ---- 3. recent list ---------------------------------------------------------------
	recentRows, err := results.Query()
	if err != nil {
		return domain.Mortality{}, fmt.Errorf("mortality: recent query: %w", err)
	}
	for recentRows.Next() {
		var d domain.MortalityDeath
		var diedOn time.Time
		var ageDays *int64
		var shedName, partitionLabel, causeKey, inferredLabel string
		if err := recentRows.Scan(&d.GoatID, &d.DisplayID, &d.Tag, &diedOn, &d.Breed, &d.Sex, &d.Stage, &ageDays,
			&d.Park, &shedName, &partitionLabel, &d.LoadRef, &causeKey, &inferredLabel); err != nil {
			recentRows.Close()
			return domain.Mortality{}, fmt.Errorf("mortality: recent scan: %w", err)
		}
		d.DiedOn = diedOn.Format(domain.HerdAnalyticsDateLayout)
		d.AgeDays = ageDays
		d.AgeBandKey = domain.MortalityAgeBandKey(ageDays)
		d.AgeBandLabel = domain.MortalityAgeBandLabel(d.AgeBandKey)
		d.Pen = oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display()
		if strings.TrimSpace(d.LoadRef) != "" {
			// Same "Load 126" shape as the load series, so a row and its bucket read alike.
			d.LoadRef = "Load " + strings.TrimSpace(d.LoadRef)
		}
		d.Season = domain.MortalitySeasonLabel(domain.MortalitySeasonKey(diedOn.Month()))
		switch {
		case causeKey != "":
			d.CauseKey, d.CauseBasis = causeKey, domain.MortalityCauseRecorded
		case inferredLabel != "":
			d.CauseLabel, d.CauseBasis = inferredLabel, domain.MortalityCauseInferred
		default:
			d.CauseBasis = domain.MortalityCauseNone
		}
		out.Deaths = append(out.Deaths, d)
	}
	recentRows.Close()
	if err := recentRows.Err(); err != nil {
		return domain.Mortality{}, fmt.Errorf("mortality: recent rows: %w", err)
	}

	return out, nil
}

// loadBucketLabel names a load bucket. The two synthetic keys carry domain copy; a real
// load carries its own reference, or its id's short form when the load has neither a
// reference nor a purchase date -- never a blank bar.
func loadBucketLabel(key, label string) string {
	switch key {
	case "farm_born":
		return "Farm born"
	case "no_load":
		return "Not on a purchase load"
	}
	if strings.TrimSpace(label) != "" {
		// "Load 126", the same shape the Sales load-wise table prints a reference in.
		return "Load " + strings.TrimSpace(label)
	}
	if len(key) >= 8 {
		return "Load " + key[:8]
	}
	return key
}

func sortBuckets(buckets []domain.MortalityBucket) {
	sort.SliceStable(buckets, func(i, j int) bool {
		if buckets[i].Deaths != buckets[j].Deaths {
			return buckets[i].Deaths > buckets[j].Deaths
		}
		if buckets[i].Animals != buckets[j].Animals {
			return buckets[i].Animals > buckets[j].Animals
		}
		return buckets[i].Label < buckets[j].Label
	})
}
