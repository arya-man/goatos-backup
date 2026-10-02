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
// batch: every animal that was there in the window with its deaths flagged (every RATE series), the deaths
// alone sliced by facts about the death (every COUNT series, the months and the cross
// tabs), and a bounded most-recent list.
//
// scale-guard:ignore: 5k-50k-envelope — canonical indexed reads per
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md. Each statement is a bounded
// aggregation over the tenant's own rows with no per-row fan-out and no page walk; this
// screen earns its own projection only under that ADR's scale-out ladder.

// projection-review: membership=canonical goats rows for the tenant with merged_into_goat_id IS NULL that were ON THE FARM at some point in the window (arrived on or before its last day and had not left before its first day) plus every animal that died inside it, each row flagged died so deaths and animals are counted off the same rows; group_key=breed | sex | species | park_id | load_id | vendor party_id on the animal row (facts that never change), and management_stage | kid/adult | (park, shed_id, normalized partition) over each animal's HISTORY SPANS for the facts that do (stage_span from goat.stage_changed events, pen_span from goat_location_history), counted DISTINCT per goat so an animal is one head in a bucket however many spans it held there; join_cardinality=goat_shed_partitions is PK (tenant_id, goat_id) so 1:{0,1}, member is DISTINCT ON goat_id so 1:{0,1}, a span row is one per (goat, change) and every span count collapses to one row per (bucket, goat) and counts those rows, deaths are joined per bucket AFTER both sides are aggregated to that bucket's key, and locations / procurement_loads / parties are primary-key label lookups AFTER aggregation; pagination=none, every series is a whole-scope rollup; scope=tenant_id plus one optional park equality predicate
//
// Expanded rationale:
//
//	THE DENOMINATOR IS EVERY ANIMAL THAT WAS THERE (maintainer decision 2026-09-24,
//	superseding the 2026-09-18 "live head count today"). "Animals" in a bucket is how many
//	animals were in that bucket at any point in the window -- including those that died,
//	were shifted out or were sold inside it, and those that arrived during it. Dividing by
//	today's head count made a shift or a sale move the rate (F2: 1 death of 30, then 15
//	shifted out, read 1 / 15), made the rate vanish when the only animal in a bucket died
//	(ICU: 1 / 0), and rewrote every past window whenever animals moved today.
//
//	producer key = goats primary key (tenant_id, goat_id). pop emits one row per animal;
//	               the two LEFT JOINs are each 1:{0,1}, so no animal appears twice.
//	history      = a stage or pen is read as SPANS worked backwards from the animal's own
//	               row: the span since its last change holds the row's value (for a dead
//	               animal, the one it died in), and each earlier span holds the value the
//	               change that ENDED it replaced. Anchoring on the row means a stage written
//	               without an event cannot contradict what the page reports as current.
//	ratio keys   = a death is counted in the bucket on the animal's row (where it died), and
//	               that value is the animal's LAST span, which overlaps the window because the
//	               death is inside it -- so every death is also one of its bucket's animals
//	               and no rate can exceed 100%. Pinned by
//	               TestMortalityDeathsNeverExceedTheAnimalsThatWereThere.
//	additivity   = deaths add up across a dimension's buckets; animals do NOT for stage,
//	               kid/adult and pen, because an animal that moved counts once in every
//	               bucket it passed through. The total is its own count over pop.
//	status matrix= died is exit_reason='died' OR (NULL exit_reason AND lifecycle 'dead'), the
//	               identical predicate Herd Analytics' deaths column uses. Pinned by
//	               TestMortalityDeathsMatchHerdAnalytics.
//
// scale-guard:plan-proof-exempt: only the final aggregation over the materialized stage_there / pen_there spans changes (COUNT(DISTINCT) -> pre-grouped count, PP-1); the goats / goat_identity_events scans that build those spans are unchanged.
const mortalityPopulationSQL = `
WITH bounds AS (
  SELECT $2::date AS from_date, $3::date AS to_date
),
member AS (
  -- The load an animal came in on, and the vendor that load was bought from. The join to
  -- procurement_loads is by primary key, so it cannot multiply a membership row and the
  -- DISTINCT ON still leaves exactly one row per animal.
  SELECT DISTINCT ON (plg.goat_id) plg.goat_id, plg.load_id, pl.source_party_id
  FROM procurement_load_goats plg
  LEFT JOIN procurement_loads pl ON pl.tenant_id = $1::uuid AND pl.load_id = plg.load_id
  WHERE plg.tenant_id = $1::uuid AND plg.current_state = 'accepted_herd_intake'
  ORDER BY plg.goat_id, plg.intake_accepted_at DESC NULLS LAST, plg.created_at DESC, plg.load_goat_id DESC
),
pop AS MATERIALIZED (
  SELECT
    g.goat_id,
    (g.exit_reason = 'died' OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))
      AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                   (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN b.from_date AND b.to_date AS died,
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
         ELSE 'no_load' END                 AS load_key,
    -- projection-review: membership=one row per tenant animal that was on the farm in the
    -- window; group_key=none here, this CTE only CARRIES the vendor key its branch groups by;
    -- join_cardinality=member is DISTINCT ON goat_id and joins procurement_loads by primary key,
    -- so an animal gains no row and carries at most one vendor; pagination=none; scope=the
    -- tenant and optional park predicate below.
    CASE WHEN g.origin_type = 'birth' THEN 'farm_born'
         WHEN m.source_party_id IS NOT NULL THEN m.source_party_id::text
         ELSE 'no_vendor' END               AS vendor_key
  FROM goats g
  CROSS JOIN bounds b
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1::uuid AND gsp.goat_id = g.goat_id
  LEFT JOIN member m ON m.goat_id = g.goat_id
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND ($4 = '' OR g.park_id = NULLIF($4, '')::uuid)
    AND (
      -- Every death in the window is one of the animals that was there, whatever its dates say.
      ((g.exit_reason = 'died' OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))
         AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                      (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN b.from_date AND b.to_date)
      OR (
        -- Arrived on or before the window's last day. With no entry date or date of birth the
        -- record's creation stands in, capped at the exit: an animal imported after it left was
        -- still on the farm up to the day it left.
        LEAST(COALESCE(g.entry_date, g.dob, (g.created_at AT TIME ZONE 'Asia/Kolkata')::date),
              (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date) <= b.to_date
        -- ... and still on the farm, or left on or after its first day. A sick, ICU or
        -- quarantined animal is still on the farm.
        AND (
          g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
          OR COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                      (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) >= b.from_date
        )
      )
    )
),
stage_change AS (
  SELECT e.goat_id,
         (e.occurred_at AT TIME ZONE 'Asia/Kolkata')::date AS changed_on,
         COALESCE(btrim(e.payload->>'previous_management_stage'), '') AS stage_before,
         lag((e.occurred_at AT TIME ZONE 'Asia/Kolkata')::date)
           OVER (PARTITION BY e.goat_id ORDER BY e.occurred_at, e.recorded_at, e.identity_event_id) AS previous_change_on,
         row_number()
           OVER (PARTITION BY e.goat_id ORDER BY e.occurred_at DESC, e.recorded_at DESC, e.identity_event_id DESC) AS from_last
  FROM goat_identity_events e
  JOIN pop p ON p.goat_id = e.goat_id
  WHERE e.tenant_id = $1::uuid AND e.event_type = 'goat.stage_changed'
),
stage_span AS (
  -- The stage each change REPLACED, held from the change before it (or since the animal
  -- arrived). A stage that started with "K" is a kid stage whatever the animal is now.
  SELECT sc.goat_id, sc.stage_before AS stage, sc.previous_change_on AS span_from, sc.changed_on AS span_to,
         (p.is_kid OR upper(sc.stage_before) ~ '^K[0-9]') AS is_kid
  FROM stage_change sc
  JOIN pop p ON p.goat_id = sc.goat_id
  UNION ALL
  -- The stage on the animal's own row, held since its last change.
  SELECT p.goat_id, p.stage, lc.changed_on, NULL::date, p.is_kid
  FROM pop p
  LEFT JOIN stage_change lc ON lc.goat_id = p.goat_id AND lc.from_last = 1
),
stage_there AS (
  SELECT s.goat_id, s.stage, s.is_kid
  FROM stage_span s
  CROSS JOIN bounds b
  WHERE (s.span_from IS NULL OR s.span_from <= b.to_date)
    AND (s.span_to IS NULL OR s.span_to >= b.from_date)
),
pen_move AS (
  SELECT h.goat_id,
         (h.occurred_at AT TIME ZONE 'Asia/Kolkata')::date AS moved_on,
         h.from_location_id AS shed_before,
         COALESCE(h.from_partition_label, '') AS partition_before,
         lag((h.occurred_at AT TIME ZONE 'Asia/Kolkata')::date)
           OVER (PARTITION BY h.goat_id ORDER BY h.occurred_at, h.recorded_at, h.location_history_id) AS previous_move_on,
         row_number()
           OVER (PARTITION BY h.goat_id ORDER BY h.occurred_at DESC, h.recorded_at DESC, h.location_history_id DESC) AS from_last
  FROM goat_location_history h
  JOIN pop p ON p.goat_id = h.goat_id
  WHERE h.tenant_id = $1::uuid
),
pen_span AS (
  -- The pen each move LEFT, held from the move before it. A first placement has no pen before
  -- it, so it bounds the span after it and contributes none of its own.
  SELECT pm.goat_id, pm.shed_before AS shed_id, pm.partition_before AS partition_label,
         pm.previous_move_on AS span_from, pm.moved_on AS span_to
  FROM pen_move pm
  WHERE pm.shed_before IS NOT NULL
  UNION ALL
  -- The pen on the animal's own row, held since its last move.
  SELECT p.goat_id, p.shed_id, p.partition_label, lm.moved_on, NULL::date
  FROM pop p
  LEFT JOIN pen_move lm ON lm.goat_id = p.goat_id AND lm.from_last = 1
),
pen_there AS (
  SELECT s.goat_id, p.park_id, s.shed_id,
         lower(regexp_replace(s.partition_label, '[^A-Za-z0-9]+', '', 'g')) AS partition_key,
         s.partition_label
  FROM pen_span s
  JOIN pop p ON p.goat_id = s.goat_id
  CROSS JOIN bounds b
  WHERE (s.span_from IS NULL OR s.span_from <= b.to_date)
    AND (s.span_to IS NULL OR s.span_to >= b.from_date)
)
SELECT 'total'::text AS dim, ''::text AS key, ''::text AS label, ''::text AS extra,
       count(*) FILTER (WHERE died)::bigint AS deaths, count(*)::bigint AS animals
  FROM pop
UNION ALL
SELECT 'kid_adult', CASE WHEN t.is_kid THEN 'kid' ELSE 'adult' END, '', '',
       COALESCE(d.deaths, 0), t.animals
  FROM (SELECT is_kid, count(goat_id)::bigint AS animals
          FROM (SELECT DISTINCT is_kid, goat_id FROM stage_there) g GROUP BY is_kid) t
  LEFT JOIN (SELECT is_kid, count(*) FILTER (WHERE died)::bigint AS deaths FROM pop GROUP BY is_kid) d
         ON d.is_kid = t.is_kid
UNION ALL
SELECT 'stage', t.stage, t.stage, '', COALESCE(d.deaths, 0), t.animals
  FROM (SELECT stage, count(goat_id)::bigint AS animals
          FROM (SELECT DISTINCT stage, goat_id FROM stage_there) g GROUP BY stage) t
  LEFT JOIN (SELECT stage, count(*) FILTER (WHERE died)::bigint AS deaths FROM pop GROUP BY stage) d
         ON d.stage = t.stage
UNION ALL
SELECT 'breed', breed, breed, '', count(*) FILTER (WHERE died)::bigint, count(*)::bigint
  FROM pop GROUP BY breed
UNION ALL
SELECT 'sex', sex, initcap(sex), '', count(*) FILTER (WHERE died)::bigint, count(*)::bigint
  FROM pop GROUP BY sex
UNION ALL
SELECT 'species', species, species, '', count(*) FILTER (WHERE died)::bigint, count(*)::bigint
  FROM pop GROUP BY species
UNION ALL
SELECT 'park', COALESCE(p.park_id::text, ''), COALESCE(NULLIF(loc.location_code, ''), loc.name, ''), '',
       p.deaths, p.animals
  FROM (SELECT park_id, count(*) FILTER (WHERE died)::bigint AS deaths, count(*)::bigint AS animals
          FROM pop GROUP BY park_id) p
  LEFT JOIN locations loc ON loc.tenant_id = $1::uuid AND loc.location_id = p.park_id
UNION ALL
-- Pens: only those that saw a death. 175 pens with a zero apiece is noise; the rate a pen
-- carries is what the reader compares. Key is shed_id + normalized partition so two
-- same-named sheds in different parks never merge (Rule 4), label parts compose in Go.
-- The key LEADS with the park id so Go can put the park code in front of the pen's name: the
-- same shed names exist in both farms, and "Castro 1" alone does not say which farm's. A shed sits
-- in exactly one park, so adding park_id to the GROUP BY splits no pen.
SELECT 'pen', COALESCE(d.park_id::text, '') || ':' || COALESCE(d.shed_id::text, '') || ':' || d.partition_key,
       COALESCE(NULLIF(shed.name, ''), shed.location_code, ''), t.partition_label,
       d.deaths, t.animals
  FROM (SELECT park_id, shed_id,
               lower(regexp_replace(partition_label, '[^A-Za-z0-9]+', '', 'g')) AS partition_key,
               count(*) FILTER (WHERE died)::bigint AS deaths
          FROM pop
         GROUP BY park_id, shed_id, lower(regexp_replace(partition_label, '[^A-Za-z0-9]+', '', 'g'))
        HAVING count(*) FILTER (WHERE died) > 0) d
  -- One row per (pen, goat) first, then count those rows: the hashable collapse the
  -- count-distinct-sort rule asks for. min of the per-goat mins is the pen's min label.
  -- projection-review: membership=pen_there span rows of the window; group_key=(park_id, shed_id, partition_key) after an inner (park_id, shed_id, partition_key, goat_id) collapse, so an animal is one head per pen however many spans; join_cardinality=inner group is 1 row per (pen, goat), outer counts those rows, then 1:1 to the deaths side on the pen key; pagination=none, whole-scope rollup; scope=tenant_id plus the optional park predicate inherited from pop/pen_there
  JOIN (SELECT park_id, shed_id, partition_key, min(partition_label) AS partition_label,
               count(goat_id)::bigint AS animals
          FROM (SELECT park_id, shed_id, partition_key, goat_id, min(partition_label) AS partition_label
                  FROM pen_there
                 GROUP BY park_id, shed_id, partition_key, goat_id) g
         GROUP BY park_id, shed_id, partition_key) t
    ON t.park_id IS NOT DISTINCT FROM d.park_id
   AND t.shed_id IS NOT DISTINCT FROM d.shed_id
   AND t.partition_key = d.partition_key
  LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = d.shed_id
UNION ALL
SELECT 'load', l.load_key,
       CASE WHEN l.load_key IN ('farm_born', 'no_load') THEN ''
            ELSE COALESCE(NULLIF(pl.context->>'load_ref', ''), to_char(pl.purchase_date, 'DD/MM/YYYY'), '') END,
       COALESCE(pl.purchase_date::text, ''),
       l.deaths, l.animals
  FROM (SELECT load_key, count(*) FILTER (WHERE died)::bigint AS deaths, count(*)::bigint AS animals
          FROM pop GROUP BY load_key
        HAVING count(*) FILTER (WHERE died) > 0 OR load_key IN ('farm_born', 'no_load')) l
  -- The synthetic keys are not uuids, and a bare cast in the ON clause is evaluated for
  -- every row regardless of the other predicate, so the cast is guarded by shape.
  LEFT JOIN procurement_loads pl
         ON pl.tenant_id = $1::uuid
        AND pl.load_id = CASE WHEN l.load_key ~ '^[0-9a-f-]{36}$' THEN l.load_key::uuid END
UNION ALL
-- The pens each load's animals sit in (or last sat in before they left), so a load row can name
-- them in a bracket ("Load 128 (CPT Castro 1)") -- every load chart names its pens
-- (docs/decisions/load-charts-name-their-pens.md). Only for loads the load branch above returns
-- (a load that lost an animal), keyed load_key ':' park_id so Go can park-qualify the pen.
-- projection-review: membership=the same pop rows the load branch ranges over, one per animal,
-- narrowed to purchased loads with a death in the window; group_key=(load_key, park_id, shed_id,
-- normalized partition), the pen key the pen branch uses, so two same-named sheds in different
-- parks never merge; join_cardinality=pop is one row per animal and goat_shed_partitions is PK
-- (tenant_id, goat_id), locations is a primary-key label lookup AFTER aggregation; pagination=none;
-- scope=the tenant and optional park predicate pop already applies.
SELECT 'load_pen', lp.load_key || ':' || COALESCE(lp.park_id::text, ''),
       COALESCE(NULLIF(shed.name, ''), shed.location_code, ''), lp.partition_label,
       0::bigint, lp.animals
  FROM (SELECT load_key, park_id, shed_id, min(partition_label) AS partition_label, count(*)::bigint AS animals
          FROM pop
         WHERE shed_id IS NOT NULL
           AND load_key IN (SELECT load_key FROM pop
                             WHERE load_key NOT IN ('farm_born', 'no_load')
                             GROUP BY load_key HAVING count(*) FILTER (WHERE died) > 0)
         GROUP BY load_key, park_id, shed_id, lower(regexp_replace(partition_label, '[^A-Za-z0-9]+', '', 'g'))) lp
  LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = lp.shed_id
UNION ALL
-- Vendors: the load series rolled up to WHO the animals were bought from. A vendor sends
-- many loads, so a weakness that reads as one unlucky batch under Load reads as a pattern
-- here. Same shape as that branch, same two synthetic keys: farm-born animals have no
-- vendor, and a purchased animal whose load carries no party is 'no_vendor'.
SELECT 'vendor', v.vendor_key,
       CASE WHEN v.vendor_key IN ('farm_born', 'no_vendor') THEN ''
            ELSE COALESCE(NULLIF(btrim(pa.display_name), ''), '') END,
       '',
       v.deaths, v.animals
  -- projection-review: membership=the same pop rows every other RATE branch ranges over, one
  -- per animal, each flagged died before grouping; group_key=vendor party_id (the load's
  -- source_party_id), with farm-born and load-less animals in their two synthetic keys;
  -- join_cardinality=member is DISTINCT ON goat_id and its procurement_loads join is by primary
  -- key, so an animal carries at most one vendor and cannot fan out; parties is a primary-key
  -- label lookup AFTER aggregation; pagination=none, a whole-scope rollup; scope=the tenant and
  -- optional park predicate pop already applies.
  -- EVERY vendor, including one that has lost nothing. The load and pen series drop their
  -- quiet buckets because 175 pens of zeros is noise; a farm buys from a handful of vendors
  -- and the whole point of the series is comparing them, so a vendor whose animals are all
  -- alive must be on the board at 0% rather than missing from it.
  FROM (SELECT vendor_key, count(*) FILTER (WHERE died)::bigint AS deaths, count(*)::bigint AS animals
          FROM pop GROUP BY vendor_key) v
  -- parties is keyed by party_id alone (no tenant column); the cast is shape-guarded for
  -- the same reason the load branch guards its own.
  LEFT JOIN parties pa
         ON pa.party_id = CASE WHEN v.vendor_key ~ '^[0-9a-f-]{36}$' THEN v.vendor_key::uuid END
`

// projection-review: membership=goats that died (exit_reason 'died', or NULL exit_reason with lifecycle 'dead') on an IST day inside the window, optionally one park -- the SAME predicate the population read flags `died` with, so every COUNT series here totals the deaths tile there; group_key=one derived fact of the death per UNION branch (IST month, age band, season, cause, days since arrival, days since last accepted vaccination) or one pair for a cross tab (season x stage, load x cause, vendor x cause, breed x cause), each ranging over the identical `dead` row set; join_cardinality=health_death_causes is PK (tenant_id, goat_id) so 1:{0,1}, and the inferred-case, last-vaccination and load/vendor lookups are LATERAL subqueries returning exactly ONE row by construction (a bare aggregate over zero-or-more rows, or LIMIT 1 -- the vendor rides on that same LIMIT 1 load row through a primary-key join, so it adds no row), so none can multiply an animal; parties is a primary-key label lookup AFTER aggregation; pagination=none, whole-window rollups; scope=tenant_id plus one optional park equality predicate
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
    m.source_party_id AS vendor_party_id,
    dc.cause_key,
    inferred.disease_label
  FROM goats g
  CROSS JOIN bounds b
  LEFT JOIN health_death_causes dc ON dc.tenant_id = $1::uuid AND dc.goat_id = g.goat_id
  LEFT JOIN LATERAL (
    SELECT plg.load_id, pl.source_party_id
    FROM procurement_load_goats plg
    LEFT JOIN procurement_loads pl ON pl.tenant_id = $1::uuid AND pl.load_id = plg.load_id
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
         ELSE 'no_load' END AS load_key,
    CASE WHEN d.origin_type = 'birth' THEN 'farm_born'
         WHEN d.vendor_party_id IS NOT NULL THEN d.vendor_party_id::text
         ELSE 'no_vendor' END AS vendor_key
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
            ELSE COALESCE(NULLIF(pl.context->>'load_ref', ''), to_char(pl.purchase_date, 'DD/MM/YYYY'), '') END,
       f.cause_col_key, f.cause_col_label, f.deaths, 0
  FROM (SELECT load_key, cause_col_key, cause_col_label, count(*)::bigint AS deaths
          FROM facts GROUP BY load_key, cause_col_key, cause_col_label) f
  LEFT JOIN procurement_loads pl
         ON pl.tenant_id = $1::uuid
        AND pl.load_id = CASE WHEN f.load_key ~ '^[0-9a-f-]{36}$' THEN f.load_key::uuid END
UNION ALL
-- projection-review: membership=the same facts rows every other COUNT branch ranges over, one
-- per death; group_key=(vendor party_id, cause column key); join_cardinality=the vendor rides the
-- LIMIT 1 load LATERAL through a primary-key join, so it adds no row, and parties is a
-- primary-key label lookup AFTER aggregation; pagination=none, a whole-window rollup;
-- scope=the tenant and optional park predicate dead already applies.
SELECT 'vendor_by_cause', f.vendor_key,
       CASE WHEN f.vendor_key IN ('farm_born', 'no_vendor') THEN ''
            ELSE COALESCE(NULLIF(btrim(pa.display_name), ''), '') END,
       f.cause_col_key, f.cause_col_label, f.deaths, 0
  FROM (SELECT vendor_key, cause_col_key, cause_col_label, count(*)::bigint AS deaths
          FROM facts GROUP BY vendor_key, cause_col_key, cause_col_label) f
  LEFT JOIN parties pa
         ON pa.party_id = CASE WHEN f.vendor_key ~ '^[0-9a-f-]{36}$' THEN f.vendor_key::uuid END
UNION ALL
SELECT 'breed_by_cause', breed, breed, cause_col_key, cause_col_label, count(*)::bigint, 0
  FROM facts GROUP BY breed, cause_col_key, cause_col_label
UNION ALL
SELECT 'first_week', '', '', '', '', count(*) FILTER (WHERE age_band = 'd0_7')::bigint, 0 FROM facts
`

// projection-review: membership=the same window deaths as mortalityDeathsSQL, capped at $5 rows most-recent-first; group_key=none, one row per animal; join_cardinality=park/shed/partition/cause are primary-key LEFT JOINs (1:{0,1}) and the tag, load and inferred-case lookups are LATERAL subqueries returning exactly ONE row (LIMIT 1 or a bare aggregate), so no animal is multiplied; pagination=LIMIT/OFFSET over the window's OWN deaths -- one window's dead animals, not the herd -- ordered by (died_on DESC, goat_id) which is total over the set, and the service clamps the offset to MortalityRecentMaxOffset; every count above this list is computed by its own whole-window query and does not move when the page turns; scope=tenant_id plus one optional park predicate
// The OFFSET below walks ONE WINDOW's deaths (tens to hundreds at this envelope), never the
// canonical goats table, on the same indexed exit predicate the tiles above it count, and
// domain.MortalityRecentMaxOffset clamps it before it reaches here, so it cannot ask for a deep tail.
// scale-guard:ignore: bounded window-deaths page set, offset clamped by MortalityRecentMaxOffset
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
  LIMIT $5 OFFSET $6
)
SELECT d.goat_id::text, d.display_id,
       COALESCE(tag.identifier_value, ''),
       d.died_on,
       d.breed, d.sex, d.stage,
       CASE WHEN d.dob IS NULL OR d.died_on < d.dob THEN NULL ELSE (d.died_on - d.dob)::bigint END AS age_days,
       COALESCE(NULLIF(pk.location_code, ''), pk.name, ''),
       COALESCE(NULLIF(shed.name, ''), shed.location_code, ''),
       COALESCE(gsp.partition_label, ''),
       COALESCE(NULLIF(pl.context->>'load_ref', ''), to_char(pl.purchase_date, 'DD/MM/YYYY'), ''),
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
	// The page of the per-animal list. Resolved rather than trusted: an unknown page size or a
	// negative/absurd offset lands on the first page at the default size, because every other
	// figure on this payload is whole-window and must not be lost to a bad pager parameter.
	recentLimit, recentOffset := domain.ResolveMortalityRecentPage(req.RecentLimit, req.RecentOffset)

	batch := &pgx.Batch{}
	batch.Queue(mortalityPopulationSQL, req.TenantID, from, to, parkID)
	batch.Queue(mortalityDeathsSQL, req.TenantID, from, to, parkID)
	batch.Queue(mortalityRecentSQL, req.TenantID, from, to, parkID, recentLimit, recentOffset)

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
		Vendor:           []domain.MortalityBucket{},
		AgeAtDeath:       []domain.MortalityBucket{},
		Season:           []domain.MortalityBucket{},
		Cause:            []domain.MortalityBucket{},
		DaysSinceArrival: []domain.MortalityBucket{},
		DaysSinceVaccine: []domain.MortalityBucket{},
		SeasonByStage:    []domain.MortalityCrossCell{},
		LoadByCause:      []domain.MortalityCrossCell{},
		VendorByCause:    []domain.MortalityCrossCell{},
		BreedByCause:     []domain.MortalityCrossCell{},
		Deaths:           []domain.MortalityDeath{},
		RecentLimit:      recentLimit,
		RecentOffset:     recentOffset,
		GeneratedAt:      time.Now().In(biztime.DefaultLocation()),
	}

	// ---- 1. population: every RATE series -------------------------------------------
	popRows, err := results.Query()
	if err != nil {
		return domain.Mortality{}, fmt.Errorf("mortality: population query: %w", err)
	}
	loadPens := map[string][]loadPen{}
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
		case "vendor":
			bucket.Label = vendorBucketLabel(key, label)
			out.Vendor = append(out.Vendor, bucket)
		case "load_pen":
			loadKey, parkID, _ := strings.Cut(key, ":")
			loadPens[loadKey] = append(loadPens[loadKey], loadPen{
				parkID: parkID,
				pen:    domain.MortalityLoadPen{Pen: oploc.OperationalLocation{ShedName: label, PartitionLabel: extra}.Display(), Animals: animals},
			})
		}
	}
	popRows.Close()
	if err := popRows.Err(); err != nil {
		return domain.Mortality{}, fmt.Errorf("mortality: population rows: %w", err)
	}
	// Every pen's park is in the park series (that branch groups the same population with no
	// HAVING), so its code is read from there rather than joined twice.
	parkCodes := make(map[string]string, len(out.Park))
	for _, park := range out.Park {
		parkCodes[park.Key] = park.Label
	}
	for i := range out.Pen {
		parkID, _, _ := strings.Cut(out.Pen[i].Key, ":")
		out.Pen[i].Label = parkQualifiedPen(parkCodes[parkID], out.Pen[i].Label)
	}
	// A load row carries its pens, park-qualified from the same park series; the bracket itself
	// is composed by the client's one load-pens helper, never here.
	for i := range out.Load {
		pens := loadPens[out.Load[i].Key]
		if len(pens) == 0 {
			continue
		}
		out.Load[i].Pens = make([]domain.MortalityLoadPen, 0, len(pens))
		for _, p := range pens {
			pen := p.pen
			pen.Park = parkCodes[p.parkID]
			out.Load[i].Pens = append(out.Load[i].Pens, pen)
		}
		sort.SliceStable(out.Load[i].Pens, func(a, b int) bool {
			return out.Load[i].Pens[a].Animals > out.Load[i].Pens[b].Animals
		})
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
		case "vendor_by_cause":
			out.VendorByCause = append(out.VendorByCause, domain.MortalityCrossCell{
				RowKey: key, RowLabel: vendorBucketLabel(key, label), ColKey: key2, ColLabel: label2, Deaths: deaths,
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
	for _, series := range []*[]domain.MortalityBucket{&out.Stage, &out.Breed, &out.Sex, &out.Species, &out.Pen, &out.Load, &out.Vendor, &out.KidAdult} {
		sortBuckets(*series)
	}
	sortBuckets(out.Cause)
	// Parks are the one series NOT ranked by deaths: under All parks every Counts read lists the
	// farms in one fixed order, by CODE (CBE, then CPT -- maintainer decision 2026-09-16), so the
	// park table here reads in the same order as Herd Analytics and the park pickers.
	sortParkBuckets(out.Park)

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

// procuredNoLoadLabel names every animal that was neither born here nor accepted onto a purchase
// load. The farm has exactly three kinds of animal -- on a load, procured with no load record, and
// farm born (maintainer, 2026-09-24) -- and each reads under its own name. This group once read
// "Farm born" too (7eceb8285), which put two "Farm born" rows on the load table, the second one
// holding no animal born here.
const procuredNoLoadLabel = "Procured, no load"

// loadPen is one load_pen row parked until the park series resolves its park code.
type loadPen struct {
	parkID string
	pen    domain.MortalityLoadPen
}

// loadBucketLabel names a load bucket. The two synthetic keys carry domain copy; a real
// load carries its own reference, or its id's short form when the load has neither a
// reference nor a purchase date -- never a blank bar.
func loadBucketLabel(key, label string) string {
	switch key {
	case "farm_born":
		return "Farm born"
	case "no_load":
		return procuredNoLoadLabel
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

// vendorBucketLabel names a vendor bucket. The two synthetic keys carry domain copy; a real
// vendor carries the display name the one vendor register prints, or its id's short form when
// the party row has no name -- never a blank row.
func vendorBucketLabel(key, label string) string {
	switch key {
	case "farm_born":
		return "Farm born"
	case "no_vendor":
		// The same animals as the load table's no_load group: a load always has a vendor, so an
		// animal with no vendor is one on no load. Both tables use one name for it.
		return procuredNoLoadLabel
	}
	if trimmed := strings.TrimSpace(label); trimmed != "" {
		return trimmed
	}
	if len(key) >= 8 {
		return "Vendor " + key[:8]
	}
	return key
}

// sortParkBuckets orders the park series by its label, which is the park CODE (the SQL labels a
// park COALESCE(NULLIF(location_code, ”), name)), with the key as a tiebreak.
func sortParkBuckets(buckets []domain.MortalityBucket) {
	sort.SliceStable(buckets, func(i, j int) bool {
		if buckets[i].Label != buckets[j].Label {
			return buckets[i].Label < buckets[j].Label
		}
		return buckets[i].Key < buckets[j].Key
	})
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
