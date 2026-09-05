package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// Health Analytics — the Health vertical's leadership read.
//
// Seven canonical aggregations in ONE pgx batch, then a second round trip that
// resolves the death list's pen names through the canonical oploc batch query.
// Two round trips total; nothing here loops a query per row.
//
// scale-guard:ignore: 5k-50k-envelope — canonical indexed reads per
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md. Health opens one
// case row per diagnosed animal per episode and the module is young, so every
// query below is a bounded aggregation over a small tenant-scoped table with no
// per-row fan-out and no page walk. This screen earns its own projection only
// under that ADR's scale-out ladder.
//
// EVERY QUERY TAKES THE SAME FOUR PARAMETERS, in the same order, so a future
// edit cannot shift one query's park filter onto another's window:
//
//	$1 tenant_id · $2 from date · $3 to date · $4 park id ('' = every park)
//
// The death list takes a fifth, its row cap.

// healthAnalyticsCaseTotalsSQL is the KPI strip's case half.
//
// projection-review: membership=health_cases rows for the tenant, optionally narrowed to one park; group_key=none, this is a single-row rollup of FILTERed counts over that one set; join_cardinality=nothing is joined, so no aggregate can be multiplied; pagination=none, a single row; scope=tenant_id plus one optional park equality predicate.
//
// Expanded rationale:
//
//	producer key = health_cases (tenant_id, health_case_id). One row per episode,
//	               nothing joined before aggregation, so no case is counted twice.
//	ratio keys   = open_adults and open_kids range over the SAME filtered set as
//	               open_cases and are split on age_band, which is NOT NULL and
//	               CHECK-constrained to exactly 'adult'/'kid' (migration 000098),
//	               so the two halves always sum to the whole with no third bucket
//	               able to appear.
//	window       = the OPEN counts are deliberately UNWINDOWED — "open right now"
//	               is a census, and the page's own copy says so. Only the new /
//	               closed / recovered counts carry the window.
const healthAnalyticsCaseTotalsSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
),
scoped AS (
  SELECT hc.status, hc.age_band, hc.start_date,
         (hc.closed_at AT TIME ZONE 'Asia/Kolkata')::date AS closed_date
  FROM health_cases hc
  WHERE hc.tenant_id = $1::uuid
    AND ($4 = '' OR hc.park_id = NULLIF($4, '')::uuid)
)
SELECT
  count(*) FILTER (WHERE s.status IN ('active','continued','referred'))::bigint,
  count(*) FILTER (WHERE s.status IN ('active','continued','referred') AND s.age_band = 'adult')::bigint,
  count(*) FILTER (WHERE s.status IN ('active','continued','referred') AND s.age_band = 'kid')::bigint,
  count(*) FILTER (WHERE s.start_date BETWEEN b.from_date AND b.to_date)::bigint,
  count(*) FILTER (WHERE s.closed_date BETWEEN b.from_date AND b.to_date)::bigint,
  count(*) FILTER (WHERE s.status = 'recovered' AND s.closed_date BETWEEN b.from_date AND b.to_date)::bigint
FROM scoped s CROSS JOIN bounds b
`

// healthAnalyticsMonthsSQL is the flow series: new cases and deaths per India
// calendar month, on a generated spine so a quiet month is a real zero rather
// than a dropped row.
//
// projection-review: membership=two DISJOINT canonical sources never joined to each other -- health_cases for a new case, and an animal's own exit columns on goats for a death; group_key=the IST calendar month of the row's own date on both branches, LEFT JOINed onto a generated month spine 1:{0,1}; join_cardinality=neither branch joins anything before aggregating, and the attribution test is an EXISTS rather than a join so a goat with two dead-closed cases still counts as ONE death; pagination=none, the whole window is returned and the handler caps it at 1150 days; scope=tenant_id plus one optional park predicate on each branch.
//
// Expanded rationale:
//
//	producer key = health_cases (tenant_id, health_case_id) for the case branch;
//	               goats (tenant_id, goat_id) for the death branch. An animal has
//	               at most one exit, so it contributes at most one death.
//	ratio keys   = deaths_attributed and the deaths total range over the identical
//	               goat key set; deaths_unattributed is derived in Go as the
//	               remainder, so the two buckets cannot fail to sum to the total.
//	EXISTS       = the attribution test MUST NOT be a join. Joining health_cases
//	               to count "died under treatment" would count an animal once per
//	               dead-closed case, and a co-morbid animal really can have two.
const healthAnalyticsMonthsSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
),
month_spine AS (
  SELECT gs::date AS month_start, to_char(gs, 'YYYY-MM') AS month_key
  FROM bounds b,
       generate_series(date_trunc('month', b.from_date), date_trunc('month', b.to_date), interval '1 month') gs
),
new_cases AS (
  SELECT to_char(hc.start_date, 'YYYY-MM') AS month_key, count(*)::bigint AS new_cases
  FROM health_cases hc, bounds b
  WHERE hc.tenant_id = $1::uuid
    AND ($4 = '' OR hc.park_id = NULLIF($4, '')::uuid)
    AND hc.start_date BETWEEN b.from_date AND b.to_date
  GROUP BY 1
),
deaths AS (
  SELECT to_char((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM') AS month_key,
         count(*)::bigint AS deaths,
         -- ATTRIBUTED means the death carries a disease. Two ways it can, and the order
         -- matters: a RECORDED cause is what the operator actually named on the form and
         -- is authoritative; the open-case test is the INFERENCE that was the only thing
         -- available before causes existed, and it still serves every death recorded
         -- before this shipped. A recorded cause never needs the inference, so the two can
         -- never disagree about one animal.
         count(*) FILTER (WHERE g.death_cause_key IS NOT NULL OR EXISTS (
           SELECT 1 FROM health_cases hc
           WHERE hc.tenant_id = g.tenant_id
             AND hc.goat_id = g.goat_id
             AND hc.status IN ('closed_dead','held_death_review')
         ))::bigint AS deaths_attributed
  FROM goats g, bounds b
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND g.exit_reason = 'died'
    AND g.exited_at IS NOT NULL
    AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
    AND (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN b.from_date AND b.to_date
  GROUP BY 1
)
SELECT m.month_key,
       m.month_start,
       COALESCE(new_cases.new_cases, 0),
       COALESCE(deaths.deaths, 0),
       COALESCE(deaths.deaths_attributed, 0)
FROM month_spine m
LEFT JOIN new_cases ON new_cases.month_key = m.month_key
LEFT JOIN deaths    ON deaths.month_key    = m.month_key
ORDER BY m.month_start
`

// healthAnalyticsDiseasesSQL is the disease board.
//
// projection-review: membership=health_cases whose start_date falls inside the window, optionally narrowed to one park; group_key=COALESCE(register_rule_id, disease_key) -- the diagnosis RULE where one exists, because disease_key names the treatment card and is many-to-one; join_cardinality=nothing is joined, so no case is counted twice; pagination=ordered by new_cases DESC and capped at $5 rules, and the page's copy states the cap; scope=tenant_id plus one optional park predicate.
//
// Expanded rationale:
//
//	producer key = health_cases (tenant_id, health_case_id). GRAIN IS THE CASE:
//	               one animal treated twice for the same illness is two rows,
//	               because counting animals hides a relapse and counting sessions
//	               multiplies each disease by the length of its course.
//	ratio keys   = died and new_cases range over the identical case key set, so
//	               the case-fatality rate computed in Go from them is a share of
//	               a set that actually contains it.
//	key_kind     = min() over the group is safe because the group KEY is the same
//	               string that decides key_kind, so a group cannot mix the two.
const healthAnalyticsDiseasesSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
),
scoped AS (
  SELECT COALESCE(NULLIF(btrim(hc.register_rule_id), ''), hc.disease_key) AS key,
         CASE WHEN COALESCE(btrim(hc.register_rule_id), '') <> ''
              THEN 'register_rule' ELSE 'disease_key' END AS key_kind,
         hc.disease_name,
         hc.age_band,
         hc.status,
         -- DID THIS DISEASE KILL THE ANIMAL, or was it merely open when something else
         -- did? Before causes existed the two were indistinguishable, so an animal that
         -- died of mastitis while also being treated for bloat put a death on BOTH boards
         -- and inflated bloat's case fatality with a death it had no part in.
         --
         -- A case counts as a death when it is the NAMED cause; and, where the animal's
         -- death records no cause at all, every dead-closed case still counts exactly as
         -- it did before, so nothing about a legacy death changes shape.
         (hc.status = 'closed_dead' AND (
            hc.is_death_cause
            OR NOT EXISTS (
              SELECT 1 FROM goats dg
              WHERE dg.tenant_id = hc.tenant_id
                AND dg.goat_id = hc.goat_id
                AND dg.death_cause_key IS NOT NULL
            )
         )) AS died_of_this
  FROM health_cases hc, bounds b
  WHERE hc.tenant_id = $1::uuid
    AND ($4 = '' OR hc.park_id = NULLIF($4, '')::uuid)
    AND hc.start_date BETWEEN b.from_date AND b.to_date
)
SELECT s.key,
       min(s.key_kind),
       min(s.disease_name),
       count(*) FILTER (WHERE s.age_band = 'adult')::bigint,
       count(*) FILTER (WHERE s.age_band = 'kid')::bigint,
       count(*)::bigint AS new_cases,
       count(*) FILTER (WHERE s.status IN ('active','continued','referred'))::bigint,
       count(*) FILTER (WHERE s.status = 'recovered')::bigint,
       count(*) FILTER (WHERE s.died_of_this)::bigint
FROM scoped s
GROUP BY s.key
ORDER BY new_cases DESC, s.key
LIMIT $5
`

// healthAnalyticsAdherenceSQL is the treatment execution split.
//
// projection-review: membership=health_treatment_sessions whose business_date falls inside the window and is not in the future, excluding the three stopped states; group_key=none, a single-row rollup of FILTERed counts; join_cardinality=the only join is session -> its own case on the (tenant_id, health_case_id) unique key, strictly N:1, present solely to carry the park predicate; pagination=none; scope=tenant_id plus one optional park predicate on the case.
//
// Expanded rationale:
//
//	producer key = health_treatment_sessions (tenant_id, health_session_id).
//	status matrix= the four buckets partition the surviving statuses exactly:
//	               'completed' splits into on_time/late on its own completion
//	               date, 'rework' is its own bucket, and
//	               scheduled/due/in_progress are not_done. No status can land in
//	               two buckets and none can land in none, which is what makes the
//	               four sum to sessions_due. Pinned by
//	               TestHealthAdherenceBucketsAreDisjointAndComplete.
//	subset       = awaiting_verification counts COMPLETED sessions the verifier
//	               has not stamped. It overlaps on_time and late deliberately and
//	               is never added to them; execution and verification are
//	               separate axes.
//	future work  = business_date is capped at today, so a course scheduled
//	               forward does not report as work already failed.
const healthAnalyticsAdherenceSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date,
         LEAST($3::date, (now() AT TIME ZONE 'Asia/Kolkata')::date) AS to_date
),
scoped AS (
  SELECT hs.status,
         hs.business_date,
         (hs.completed_at AT TIME ZONE 'Asia/Kolkata')::date AS done_date,
         hs.verified_at
  FROM health_treatment_sessions hs
  JOIN health_cases hc
    ON hc.tenant_id = hs.tenant_id AND hc.health_case_id = hs.health_case_id
  CROSS JOIN bounds b
  WHERE hs.tenant_id = $1::uuid
    AND ($4 = '' OR hc.park_id = NULLIF($4, '')::uuid)
    AND hs.business_date BETWEEN b.from_date AND b.to_date
    AND hs.status NOT IN ('canceled','canceled_death','held_death_review')
)
SELECT
  count(*)::bigint,
  count(*) FILTER (WHERE s.status = 'completed' AND s.done_date IS NOT NULL AND s.done_date <= s.business_date)::bigint,
  count(*) FILTER (WHERE s.status = 'completed' AND (s.done_date IS NULL OR s.done_date > s.business_date))::bigint,
  count(*) FILTER (WHERE s.status = 'rework')::bigint,
  count(*) FILTER (WHERE s.status IN ('scheduled','due','in_progress'))::bigint,
  count(*) FILTER (WHERE s.status = 'completed' AND s.verified_at IS NULL)::bigint
FROM scoped s
`

// healthAnalyticsMedicinesSQL is what was actually given, off the operator's own
// completed step — never off the authored protocol, which says what SHOULD have
// been given.
//
// projection-review: membership=health_medicine_administrations administered inside the window, optionally narrowed to one park; group_key=medicine name + route; join_cardinality=the only join is administration -> its own case on the (tenant_id, health_case_id) unique key, strictly N:1, present solely to carry the park predicate; pagination=ordered by doses DESC and capped at $5 rows; scope=tenant_id plus one optional park predicate on the case.
//
// Expanded rationale:
//
//	producer key = health_medicine_administrations (health_session_id,
//	               health_session_step_id) is UNIQUE, so one dose is one row.
//	animals      = count(DISTINCT goat_id) is a genuinely different number from
//	               doses whenever a course runs more than one day; reporting only
//	               doses would read as more animals treated than there were.
const healthAnalyticsMedicinesSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
)
SELECT a.medicine_name,
       COALESCE(NULLIF(btrim(a.medicine_route), ''), '') AS route,
       count(*)::bigint AS doses,
       count(DISTINCT a.goat_id)::bigint AS animals
FROM health_medicine_administrations a
JOIN health_cases hc
  ON hc.tenant_id = a.tenant_id AND hc.health_case_id = a.health_case_id
CROSS JOIN bounds b
WHERE a.tenant_id = $1::uuid
  AND ($4 = '' OR hc.park_id = NULLIF($4, '')::uuid)
  AND (a.administered_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN b.from_date AND b.to_date
GROUP BY 1, 2
ORDER BY doses DESC, a.medicine_name
LIMIT $5
`

// healthAnalyticsEngineSQL is the diagnosis engine's own outcome counters.
//
// projection-review: membership=health_diagnosis_runs whose business_date falls inside the window, optionally narrowed to one park through the animal; group_key=none, a single-row rollup; join_cardinality=the only join is run -> its own animal on the goats primary key (tenant_id, goat_id), strictly N:1, present solely to carry the park predicate; pagination=none; scope=tenant_id plus one optional park predicate.
//
// Expanded rationale:
//
//	producer key = health_diagnosis_runs (tenant_id, health_diagnosis_run_id).
//	status matrix= the four status counters cover the whole CHECK-constrained
//	               enum (proposed/confirmed/declined/superseded, migration
//	               000229), so they sum to observations with no silent remainder.
//	               `invalid` is a SEPARATE axis (the engine refused the
//	               observation) and overlaps them; it is reported beside them,
//	               never added.
//	median       = percentile_cont over confirmed runs only. A run nobody
//	               confirmed has no elapsed time to contribute, and treating it
//	               as zero would report an instant confirmation that never
//	               happened.
const healthAnalyticsEngineSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
)
SELECT
  count(*)::bigint,
  count(*) FILTER (WHERE r.status = 'confirmed')::bigint,
  count(*) FILTER (WHERE r.status = 'declined')::bigint,
  count(*) FILTER (WHERE r.status = 'proposed')::bigint,
  count(*) FILTER (WHERE r.status = 'superseded')::bigint,
  count(*) FILTER (WHERE NOT r.valid)::bigint,
  percentile_cont(0.5) WITHIN GROUP (
    ORDER BY EXTRACT(EPOCH FROM (r.confirmed_at - r.observed_at)) / 3600.0
  ) FILTER (WHERE r.status = 'confirmed' AND r.confirmed_at IS NOT NULL)
FROM health_diagnosis_runs r
JOIN goats g ON g.tenant_id = r.tenant_id AND g.goat_id = r.goat_id
CROSS JOIN bounds b
WHERE r.tenant_id = $1::uuid
  AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
  AND r.business_date BETWEEN b.from_date AND b.to_date
`

// healthAnalyticsEngineRulesSQL is proposed-vs-opened per diagnosis rule.
//
// PROPOSED vs OPENED, never "declined": a run's status says whether the whole
// RUN was decided, and no per-problem decline is stored anywhere, so a per-rule
// override rate would be invented. Whether a case carrying that rule id was
// actually opened from that run IS canonical, and carries the same signal.
//
// projection-review: membership=the DISTINCT (run, proposed rule) pairs of runs inside the window, and the DISTINCT (run, opened rule) pairs of cases opened from those same runs; group_key=the rule id; join_cardinality=both sides are DISTINCT on (run, rule) before the join, so the LEFT JOIN on that exact pair is strictly 1:{0,1} and cannot multiply a proposal; pagination=ordered by proposed DESC and capped at $5 rules; scope=tenant_id plus one optional park predicate on the run's animal.
//
// Expanded rationale:
//
//	producer key = (health_diagnosis_run_id, rule id) on BOTH sides, which is why
//	               each side is materialized DISTINCT first. A proposal array
//	               listing the same rule twice is still ONE observation of one
//	               animal, and without the DISTINCT it would count as two.
//	jsonb        = jsonb_array_elements_text, NEVER jsonb_array_elements(...)::text.
//	               The cast form leaves JSON quoting on and turns a JSON null into
//	               the four-character string "null", which is non-empty and passes
//	               every "is there a rule here?" test — fabricating a rule nobody
//	               proposed (AGENTS.md operational-location defect class OL-5).
//	shape guard  = jsonb_typeof pins the value to an array first. A malformed
//	               proposal would otherwise raise and take the whole page down
//	               rather than reporting the rules that are readable.
const healthAnalyticsEngineRulesSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
),
scoped_runs AS (
  SELECT r.health_diagnosis_run_id, r.proposal
  FROM health_diagnosis_runs r
  JOIN goats g ON g.tenant_id = r.tenant_id AND g.goat_id = r.goat_id
  CROSS JOIN bounds b
  WHERE r.tenant_id = $1::uuid
    AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
    AND r.business_date BETWEEN b.from_date AND b.to_date
),
proposed AS (
  SELECT DISTINCT sr.health_diagnosis_run_id, btrim(prob) AS key
  FROM scoped_runs sr
  CROSS JOIN LATERAL jsonb_array_elements_text(
    CASE WHEN jsonb_typeof(sr.proposal -> 'problems') = 'array'
         THEN sr.proposal -> 'problems'
         ELSE '[]'::jsonb END
  ) AS prob
  WHERE btrim(prob) <> ''
),
opened AS (
  SELECT DISTINCT hc.health_diagnosis_run_id, btrim(hc.register_rule_id) AS key
  FROM health_cases hc
  JOIN scoped_runs sr ON sr.health_diagnosis_run_id = hc.health_diagnosis_run_id
  WHERE hc.tenant_id = $1::uuid
    AND COALESCE(btrim(hc.register_rule_id), '') <> ''
)
SELECT p.key,
       count(*)::bigint AS proposed,
       count(*) FILTER (WHERE o.health_diagnosis_run_id IS NOT NULL)::bigint AS opened
FROM proposed p
LEFT JOIN opened o
       ON o.health_diagnosis_run_id = p.health_diagnosis_run_id
      AND o.key = p.key
GROUP BY p.key
ORDER BY proposed DESC, p.key
LIMIT $5
`

// healthAnalyticsDeathsSQL is the bounded per-animal death list beside the
// counts.
//
// projection-review: membership=goats that exited as died inside the window, optionally narrowed to one park, capped at $5 rows most-recent-first; group_key=none, one row per animal; join_cardinality=park is a LEFT JOIN on the locations primary key (1:{0,1}); the tag and case lookups are LATERAL subqueries that each return exactly ONE row by construction (LIMIT 1, and a bare aggregate over zero rows), so neither can multiply an animal; pagination=a hard LIMIT, and the whole-window counts above this list are computed by their own queries and do not move with it; scope=tenant_id plus one optional park predicate.
//
// Expanded rationale:
//
//	producer key = goats (tenant_id, goat_id). An animal has at most one exit.
//	no ranking   = the case LATERAL deliberately AGGREGATES rather than ordering
//	               and taking the first. A co-morbid animal really can die with
//	               two open cases, and `ORDER BY ... LIMIT 1` would name one of
//	               two true diseases and silently flip between them. The row
//	               names BOTH, and days-under-treatment counts from the EARLIEST
//	               of them.
//	never vs not = ever_had_case separates an animal that never had a case at all
//	               from one whose case had already closed before it died. Both
//	               are unattributed; only the first is "never diagnosed".
//	tag          = one identifier per animal, animal_identifier_1 preferred. This
//	               is a bounded page, so a LATERAL LIMIT 1 here is a single
//	               indexed lookup per returned row, not an N+1 across a table.
const healthAnalyticsDeathsSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
),
dead AS (
  SELECT g.goat_id,
         g.display_id,
         g.shed_id,
         g.park_id,
         COALESCE(g.age_band, '') AS age_band,
         g.death_cause_key,
         g.death_cause_kind,
         (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date AS business_date
  FROM goats g, bounds b
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND g.exit_reason = 'died'
    AND g.exited_at IS NOT NULL
    AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
    AND (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN b.from_date AND b.to_date
  ORDER BY (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date DESC, g.goat_id
  LIMIT $5
)
SELECT d.goat_id::text,
       d.display_id,
       COALESCE(d.shed_id::text, ''),
       COALESCE(NULLIF(btrim(pk.name), ''), pk.location_code, ''),
       d.business_date,
       d.age_band,
       COALESCE(tag.identifier_value, ''),
       COALESCE(hc.disease_label, ''),
       hc.started,
       EXISTS (
         SELECT 1 FROM health_cases prior
         WHERE prior.tenant_id = $1::uuid AND prior.goat_id = d.goat_id
       ) AS ever_had_case,
       -- The RECORDED cause, and the name of the case that carries it. Both empty for a
       -- normal death and for every death recorded before causes existed, where the row
       -- falls back to the inferred label above.
       COALESCE(d.death_cause_key, ''),
       COALESCE(d.death_cause_kind, ''),
       COALESCE(cause_case.disease_name, '')
FROM dead d
LEFT JOIN locations pk
       ON pk.tenant_id = $1::uuid AND pk.location_id = d.park_id
LEFT JOIN LATERAL (
  SELECT gi.identifier_value
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid
    AND gi.goat_id = d.goat_id
    AND gi.status = 'active'
  ORDER BY (gi.identifier_type = 'animal_identifier_1') DESC, gi.identifier_value
  LIMIT 1
) tag ON true
LEFT JOIN LATERAL (
  SELECT string_agg(DISTINCT c.disease_name, ' · ') AS disease_label,
         min(c.start_date) AS started
  FROM health_cases c
  WHERE c.tenant_id = $1::uuid
    AND c.goat_id = d.goat_id
    AND c.status IN ('closed_dead','held_death_review')
) hc ON true
LEFT JOIN LATERAL (
  -- The one case marked as the cause. A partial unique index guarantees at most one per
  -- animal, so this cannot fan the row out.
  SELECT c.disease_name
  FROM health_cases c
  WHERE c.tenant_id = $1::uuid AND c.goat_id = d.goat_id AND c.is_death_cause
) cause_case ON true
ORDER BY d.business_date DESC, d.goat_id
`

// GetHealthAnalytics serves the Health -> Health Analytics page.
func (r *Repository) GetHealthAnalytics(ctx context.Context, req domain.HealthAnalyticsQuery) (domain.HealthAnalytics, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// The window is an INDIA BUSINESS DAY range, never a rolling clock instant.
	// Bounds resolve through the same exported parser the handler validated them
	// with, and an unparseable or reversed pair falls back to the default window
	// rather than erroring here: the handler already rejects those with a 400, so
	// reaching this point with one means an internal caller, and serving the
	// default beats returning nothing.
	defaultFrom, defaultTo := domain.HealthAnalyticsDefaultWindow(r.now())
	fromDate, errFrom := domain.ParseHealthAnalyticsDate(req.FromDate)
	toDate, errTo := domain.ParseHealthAnalyticsDate(req.ToDate)
	if errFrom != nil || errTo != nil || toDate.Before(fromDate) {
		fromDate, _ = domain.ParseHealthAnalyticsDate(defaultFrom)
		toDate, _ = domain.ParseHealthAnalyticsDate(defaultTo)
	}
	from := fromDate.Format(domain.HealthAnalyticsDateLayout)
	to := toDate.Format(domain.HealthAnalyticsDateLayout)

	parkID := ""
	if req.ParkID != nil {
		parkID = strings.TrimSpace(*req.ParkID)
	}

	out := domain.HealthAnalytics{
		WindowFrom:  from,
		WindowTo:    to,
		Months:      []domain.HealthAnalyticsMonth{},
		Diseases:    []domain.HealthAnalyticsDisease{},
		Medicines:   []domain.HealthAnalyticsMedicine{},
		Deaths:      []domain.HealthAnalyticsDeath{},
		GeneratedAt: r.now().In(biztime.DefaultLocation()),
	}
	out.Engine.Rules = []domain.HealthAnalyticsEngineRule{}

	batch := &pgx.Batch{}
	batch.Queue(healthAnalyticsCaseTotalsSQL, req.TenantID, from, to, parkID)
	batch.Queue(healthAnalyticsMonthsSQL, req.TenantID, from, to, parkID)
	batch.Queue(healthAnalyticsDiseasesSQL, req.TenantID, from, to, parkID, domain.HealthAnalyticsDiseaseLimit)
	batch.Queue(healthAnalyticsAdherenceSQL, req.TenantID, from, to, parkID)
	batch.Queue(healthAnalyticsMedicinesSQL, req.TenantID, from, to, parkID, domain.HealthAnalyticsMedicineLimit)
	batch.Queue(healthAnalyticsEngineSQL, req.TenantID, from, to, parkID)
	batch.Queue(healthAnalyticsEngineRulesSQL, req.TenantID, from, to, parkID, domain.HealthAnalyticsEngineRuleLimit)
	batch.Queue(healthAnalyticsDeathsSQL, req.TenantID, from, to, parkID, domain.HealthAnalyticsDeathListLimit)
	batch.Queue(healthAnalyticsNeverDiagnosedSQL, req.TenantID, from, to, parkID)

	results := r.pool.SendBatch(ctx, batch)

	shedIDs, err := r.scanHealthAnalyticsBatch(results, &out)
	if closeErr := results.Close(); closeErr != nil && err == nil {
		err = fmt.Errorf("health analytics: close batch: %w", closeErr)
	}
	if err != nil {
		return domain.HealthAnalytics{}, err
	}

	// Pen names come from THE canonical batch resolver, never a hand-written
	// partition SELECT: `partition_label` not `normalized_label`, the 'whole'
	// sentinel filtered, and agree-or-go-bare rather than a fabricated pick.
	if len(shedIDs) > 0 {
		if err := r.attachDeathLocations(ctx, req.TenantID, shedIDs, &out); err != nil {
			return domain.HealthAnalytics{}, err
		}
	}

	return out, nil
}

// scanHealthAnalyticsBatch reads the eight queued results in order and returns
// the distinct shed ids the death list needs pen names for.
func (r *Repository) scanHealthAnalyticsBatch(results pgx.BatchResults, out *domain.HealthAnalytics) ([]string, error) {
	if err := results.QueryRow().Scan(
		&out.Totals.OpenCases,
		&out.Totals.OpenAdults,
		&out.Totals.OpenKids,
		&out.Totals.NewCases,
		&out.Totals.ClosedCases,
		&out.Totals.Recovered,
	); err != nil {
		return nil, fmt.Errorf("health analytics: case totals: %w", err)
	}

	monthRows, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("health analytics: months query: %w", err)
	}
	for monthRows.Next() {
		var month domain.HealthAnalyticsMonth
		var monthStart time.Time
		if err := monthRows.Scan(&month.Month, &monthStart, &month.NewCases, &month.Deaths, &month.DeathsAttributed); err != nil {
			monthRows.Close()
			return nil, fmt.Errorf("health analytics: months scan: %w", err)
		}
		month.Label = monthStart.Format("Jan 2006")
		// Derived rather than counted, so the two buckets cannot fail to sum to
		// the total no matter what the attribution predicate does.
		month.DeathsUnattributed = month.Deaths - month.DeathsAttributed
		out.Months = append(out.Months, month)

		out.Totals.Deaths += month.Deaths
		out.Totals.DeathsAttributed += month.DeathsAttributed
	}
	monthRows.Close()
	if err := monthRows.Err(); err != nil {
		return nil, fmt.Errorf("health analytics: months rows: %w", err)
	}
	out.Totals.DeathsUnattributed = out.Totals.Deaths - out.Totals.DeathsAttributed

	diseaseRows, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("health analytics: diseases query: %w", err)
	}
	for diseaseRows.Next() {
		var row domain.HealthAnalyticsDisease
		var adults, kids int64
		if err := diseaseRows.Scan(
			&row.Key, &row.KeyKind, &row.Label,
			&adults, &kids,
			&row.NewCases, &row.OpenCases, &row.Recovered, &row.Died,
		); err != nil {
			diseaseRows.Close()
			return nil, fmt.Errorf("health analytics: diseases scan: %w", err)
		}
		row.AgeBands = domain.AgeBandSummary(adults, kids)
		row.CaseFatalityPct = domain.HealthAnalyticsPct(row.Died, row.NewCases)
		out.Diseases = append(out.Diseases, row)
	}
	diseaseRows.Close()
	if err := diseaseRows.Err(); err != nil {
		return nil, fmt.Errorf("health analytics: diseases rows: %w", err)
	}

	if err := results.QueryRow().Scan(
		&out.Adherence.SessionsDue,
		&out.Adherence.OnTime,
		&out.Adherence.Late,
		&out.Adherence.Rework,
		&out.Adherence.NotDone,
		&out.Adherence.AwaitingVerification,
	); err != nil {
		return nil, fmt.Errorf("health analytics: adherence: %w", err)
	}
	out.Adherence.OnTimePct = domain.HealthAnalyticsPct(out.Adherence.OnTime, out.Adherence.SessionsDue)

	medicineRows, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("health analytics: medicines query: %w", err)
	}
	for medicineRows.Next() {
		var row domain.HealthAnalyticsMedicine
		if err := medicineRows.Scan(&row.Name, &row.Route, &row.Doses, &row.Animals); err != nil {
			medicineRows.Close()
			return nil, fmt.Errorf("health analytics: medicines scan: %w", err)
		}
		out.Medicines = append(out.Medicines, row)
	}
	medicineRows.Close()
	if err := medicineRows.Err(); err != nil {
		return nil, fmt.Errorf("health analytics: medicines rows: %w", err)
	}

	var medianHours *float64
	if err := results.QueryRow().Scan(
		&out.Engine.Observations,
		&out.Engine.Confirmed,
		&out.Engine.Declined,
		&out.Engine.Pending,
		&out.Engine.Superseded,
		&out.Engine.Invalid,
		&medianHours,
	); err != nil {
		return nil, fmt.Errorf("health analytics: engine: %w", err)
	}
	// Decided runs only: a superseded run is a re-observation, not a director
	// disagreeing with the engine, and a pending one has not been judged at all.
	out.Engine.ConfirmedPct = domain.HealthAnalyticsPct(out.Engine.Confirmed, out.Engine.Confirmed+out.Engine.Declined)
	if medianHours != nil {
		rounded := int64(*medianHours + 0.5)
		out.Engine.MedianHoursToConfirm = &rounded
	}

	ruleRows, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("health analytics: engine rules query: %w", err)
	}
	for ruleRows.Next() {
		var row domain.HealthAnalyticsEngineRule
		if err := ruleRows.Scan(&row.Key, &row.Proposed, &row.Opened); err != nil {
			ruleRows.Close()
			return nil, fmt.Errorf("health analytics: engine rules scan: %w", err)
		}
		row.NotTakenUpPct = domain.HealthAnalyticsPct(row.Proposed-row.Opened, row.Proposed)
		out.Engine.Rules = append(out.Engine.Rules, row)
	}
	ruleRows.Close()
	if err := ruleRows.Err(); err != nil {
		return nil, fmt.Errorf("health analytics: engine rules rows: %w", err)
	}

	deathRows, err := results.Query()
	if err != nil {
		return nil, fmt.Errorf("health analytics: deaths query: %w", err)
	}
	shedSeen := map[string]struct{}{}
	shedIDs := []string{}
	for deathRows.Next() {
		var row domain.HealthAnalyticsDeath
		var shedID string
		var businessDate time.Time
		var diseaseLabel string
		var caseStart *time.Time
		var everHadCase bool
		var causeKey, causeKind, causeCaseDiseaseName string
		if err := deathRows.Scan(
			&row.GoatID, &row.DisplayID, &shedID, &row.ParkLabel,
			&businessDate, &row.AgeBand, &row.Tag,
			&diseaseLabel, &caseStart, &everHadCase,
			&causeKey, &causeKind, &causeCaseDiseaseName,
		); err != nil {
			deathRows.Close()
			return nil, fmt.Errorf("health analytics: deaths scan: %w", err)
		}
		row.BusinessDate = businessDate.Format(domain.HealthAnalyticsDateLayout)
		switch {
		case causeKey != "":
			// THE RECORDED CAUSE WINS. The operator named this disease on the death form;
			// nothing inferred may override or contradict it.
			row.Attribution = "attributed"
			row.CauseRecorded = true
			// A register rule is labelled from the shared vocabulary, so the death list and
			// the dropdown that produced it read the same words. A treatment-card cause
			// borrows the name off the case it came from, because the card key is not in
			// that vocabulary.
			if causeKind == domain.DeathCauseKindRegisterRule {
				row.DiseaseLabel = domain.DeathCauseLabel(causeKey)
			} else {
				row.DiseaseLabel = causeCaseDiseaseName
			}
			if caseStart != nil {
				row.DaysUnderTreatment = daysBetween(businessDate, *caseStart)
			}
		case diseaseLabel != "":
			// INFERRED, and only for a death recorded before causes existed: a case was
			// open when the animal died, which is co-incidence rather than causation. The
			// row says so through CauseRecorded, so a reader is never shown a guess and a
			// recorded fact as if they were the same thing.
			row.Attribution = "attributed"
			row.DiseaseLabel = diseaseLabel
			if caseStart != nil {
				row.DaysUnderTreatment = daysBetween(businessDate, *caseStart)
			}
		default:
			row.Attribution = "unattributed"
			row.NeverDiagnosed = !everHadCase
		}
		out.Deaths = append(out.Deaths, row)

		if shedID != "" {
			if _, ok := shedSeen[shedID]; !ok {
				shedSeen[shedID] = struct{}{}
				shedIDs = append(shedIDs, shedID)
			}
			// Parked on the row until the location round trip resolves it.
			out.Deaths[len(out.Deaths)-1].OperationalLocationDisplay = shedID
		}
	}
	deathRows.Close()
	if err := deathRows.Err(); err != nil {
		return nil, fmt.Errorf("health analytics: deaths rows: %w", err)
	}

	// Counted over the WHOLE window by its own query, never off the bounded list
	// above: a page cap must not be able to change a headline figure.
	if err := results.QueryRow().Scan(&out.Totals.DeathsNeverDiagnosed); err != nil {
		return nil, fmt.Errorf("health analytics: never-diagnosed deaths: %w", err)
	}

	return shedIDs, nil
}

// healthAnalyticsNeverDiagnosedSQL counts, over the WHOLE window, the deaths of
// animals that never had a health case at all.
//
// It is a SUBSET of the unattributed bucket and is therefore never added to the
// other two. It is its own whole-window query rather than a count off the
// bounded death list, so the list's row cap can never move a headline figure.
//
// projection-review: membership=goats that exited as died inside the window, optionally narrowed to one park; group_key=none, a single count; join_cardinality=the attribution test is a NOT EXISTS, so no row can be multiplied; pagination=none; scope=tenant_id plus one optional park predicate.
const healthAnalyticsNeverDiagnosedSQL = `
SELECT count(*)::bigint
FROM goats g
WHERE g.tenant_id = $1::uuid
  AND g.merged_into_goat_id IS NULL
  AND g.exit_reason = 'died'
  AND g.exited_at IS NOT NULL
  AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
  AND (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN $2::date AND $3::date
  AND NOT EXISTS (
    SELECT 1 FROM health_cases hc
    WHERE hc.tenant_id = g.tenant_id AND hc.goat_id = g.goat_id
  )
`

// attachDeathLocations resolves the death list's shed ids to pen names in ONE
// round trip and swaps them onto the rows.
//
// A shed that resolves to nothing leaves the row's location EMPTY rather than
// rendering the raw uuid it was parked as — a sibling queue once shipped
// `Raised by <uuid>` to operators, and that is the defect being avoided.
func (r *Repository) attachDeathLocations(ctx context.Context, tenantID string, shedIDs []string, out *domain.HealthAnalytics) error {
	rows, err := r.pool.Query(ctx, oploc.ShedScopedLocationBatchSQL, tenantID, shedIDs)
	if err != nil {
		return fmt.Errorf("health analytics: pen names query: %w", err)
	}
	locations, err := oploc.ResolveShedLocations(ctx, rows)
	rows.Close()
	if err != nil {
		return fmt.Errorf("health analytics: pen names: %w", err)
	}
	for i := range out.Deaths {
		shedID := out.Deaths[i].OperationalLocationDisplay
		if shedID == "" {
			continue
		}
		location, ok := locations[shedID]
		if !ok {
			out.Deaths[i].OperationalLocationDisplay = ""
			continue
		}
		out.Deaths[i].OperationalLocationDisplay = location.Display()
	}
	return nil
}

// daysBetween is whole days from a case start to the death date, never negative: a case
// opened on the day the animal died is 0 days, not -0 or a negative number from a clock
// skew.
func daysBetween(death time.Time, start time.Time) *int64 {
	days := int64(death.Sub(start).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return &days
}
