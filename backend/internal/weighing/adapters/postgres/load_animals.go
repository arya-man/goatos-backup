package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/animalorigin"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// A LOAD IS ITS ANIMALS (maintainer decision 2026-09-24, SUPERSEDING the fixed load-to-pen tag list
// of 000131 and the 2026-09-22 "the weighing charts and Counts must not be reconciled" rule).
//
// The farm's words: "we have mapped those animals to that load; use ADG based on that load. It
// should not always stick to one particular pen -- initially the load may be in Castro 1, after
// some days we move it to Castro 2, then Castro 3. If I add a load and weigh, it should come
// directly; there is no need of adding anything separately."
//
// So a load's figures are read from ITS OWN ANIMALS -- procurement_load_goats, written when the
// load is bought -- wherever those animals were when they were weighed, and never from a pen list
// someone has to maintain. That is the whole point of reading the herd here: the old
// weighing_shed_load_tags mapping was typed once from the 08/08 load sheet, never followed a move,
// and silently left out every load bought after it (Load 136, 58 animals, bought 16/09).
//
// HOW A WEIGH COUNTS FOR A LOAD ANIMAL:
//
//	scanned    the weigh's tag resolves to the animal (goat_identifiers, status 'active'), so its
//	           consecutive weighs are its own legs, in whatever pen each was taken;
//	whole pen  the weigh has no tag, so it counts for every load animal that was IN THAT PEN at
//	           BOTH ends of the leg (the pen's previous weigh and this one), at the pen's average
//	           change. An animal that moved in or out between the two weighs was not there for the
//	           whole leg and does not get it. A pen holding two loads credits each load's own
//	           animals with the same pen average -- the pen average is the only measured fact.
//
// An animal's gain is its total grams over its total days across those legs (the headline's
// statistic), and a load's gain is the mean over its animals -- so one load reads one figure
// whichever pens it passed through.
//
// WHERE AN ANIMAL WAS ON A DAY: the stay covering that day -- in the pen its last move on or before
// that day put it in; before its first recorded move, the pen that move took it FROM; with no move
// at all, where the register has it now. Move history starts 2026-08-13, so earlier days fall back
// the same way.
//
// PEN IDENTITY: the same pen reaches this file three ways -- a weighing bucket often names a legacy
// per-pen location ("Castro 1", no partition), the register names the building plus a partition
// ("Castro" + "1"), and history uses either. All three are resolved to (building, scrubbed pen key)
// with the FCR tab's bridge (growthdirector fcr.go): a labelled location is its own building; an
// unlabelled one resolves to the longest active shed in the same park whose name prefixes it.
//
// ISOLATION: this is a RECORDED, FILE-SCOPED exemption (check-weighing-free-flow-guard.mjs), and it
// is READ-ONLY and REPORTING-ONLY -- no capture, submit, close or verdict path calls it, and no scan
// is gated on load, pen or identity.

// loadPenKeySQL resolves (location, partition label) to (building, scrubbed pen key). %[1]s is the
// location id column, %[2]s the partition label column, %[3]s the alias alias. 'whole' is a
// matching key for "the whole shed", never a pen name, so it reads as no partition.
const loadPenKeySQL = `CASE WHEN COALESCE(NULLIF(lower(btrim(%[2]s)), 'whole'), '') <> '' THEN %[1]s ELSE COALESCE(%[3]s.phys_id, %[1]s) END,
       CASE WHEN COALESCE(NULLIF(lower(btrim(%[2]s)), 'whole'), '') <> ''
            THEN regexp_replace(lower(btrim(%[2]s)), '^[-\s]*(part|pt)?[\s.-]*', '')
            ELSE COALESCE(%[3]s.pen_key, '') END`

func loadPenKey(locCol, partCol, aliasName string) string {
	return fmt.Sprintf(loadPenKeySQL, locCol, partCol, aliasName)
}

// Binds, shared by both queries:
//
//	$1 tenant, $2 park ids, $3 window start, $4 window end (exclusive), $5 sex ('' = all),
//	$6 origin ('' = all), $7 weighing category ('' = both), $8/$9 the selected pen (Time-wise
//	scope, '' = every pen) as a weighing bucket key, and -- month variant only -- $10 the anchor.
var loadAnimalLegsCTE = `
alias AS (
  SELECT s.location_id, p.location_id AS phys_id,
         regexp_replace(lower(btrim(substr(s.name, length(p.name) + 1))), '^[-\s]*(part|pt)?[\s.-]*', '') AS pen_key
  FROM locations s
  JOIN LATERAL (
    SELECT b.location_id, b.name
    FROM locations b
    WHERE b.tenant_id = $1::uuid
      AND b.parent_location_id = s.parent_location_id
      AND b.location_type = 'shed'
      AND b.status = 'active'
      AND b.retired_at IS NULL
      AND b.name <> s.name
      AND (s.name LIKE b.name || ' %' OR s.name LIKE b.name || ' - %')
    ORDER BY length(b.name) DESC
    LIMIT 1
  ) p ON true
  WHERE s.tenant_id = $1::uuid AND s.location_type = 'shed'
),
-- One row per load animal, its load and when it can have been on the farm. A load is named by
-- the farm's own load number; an animal on two loads (re-bought) belongs to the latest purchase.
-- projection-review: membership=procurement_load_goats joined to its load and animal; group_key=goat_id via DISTINCT ON; join_cardinality=procurement_loads PK, goats PK, parties PK: each 0..1 per load animal; pagination=NONE, bounded by the tenant's purchased animals; scope=tenant + animal park ANY($2)
load_goats AS (
  SELECT DISTINCT ON (plg.goat_id)
         plg.goat_id, pl.context->>'load_ref' AS load_ref, COALESCE(pt.display_name, '') AS owner_name,
         pl.purchase_date, g.exited_at
  FROM procurement_load_goats plg
  JOIN procurement_loads pl ON pl.tenant_id = plg.tenant_id AND pl.load_id = plg.load_id
  JOIN goats g ON g.tenant_id = plg.tenant_id AND g.goat_id = plg.goat_id
  LEFT JOIN parties pt ON pt.party_id = pl.source_party_id
  WHERE plg.tenant_id = $1::uuid
    AND g.park_id = ANY($2::uuid[])
    AND COALESCE(pl.context->>'load_ref', '') <> ''
    -- Sex is the animal's own, from the register: a load animal weighed only inside a whole pen
    -- carries no scanned tag, so a tag list could never place it.
    AND ($5::text = '' OR lower(btrim(g.sex)) = $5::text)
    -- Every load animal is Procured (load) by the origin rule (platform/animalorigin: a load wins
    -- over the recorded origin), so only that cohort -- or no origin filter -- keeps them.
    AND $6::text IN ('', 'procured_load')
  ORDER BY plg.goat_id, pl.purchase_date DESC NULLS LAST, pl.load_id
),
-- A load is shown while at least one of its animals is still on the farm (maintainer decision
-- 2026-09-24: "show until all animals are sold"). A sold-out load leaves every load chart.
live_loads AS (
  SELECT load_ref FROM load_goats GROUP BY load_ref
  HAVING count(*) FILTER (WHERE exited_at IS NULL) > 0
),
-- Scanned weighs of load animals: one per animal per day (the day's last capture).
-- projection-review: membership=weighing_observations whose tag resolves to a load animal; group_key=(goat_id, d) via DISTINCT ON; join_cardinality=goat_identifiers 0..1 active identifier per normalized value, campaign sheds and campaigns PK; pagination=NONE, bounded by tenant + park + window; scope=tenant + park ANY($2) + half-open window
scan_pts AS (
  SELECT DISTINCT ON (lg.goat_id, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date)
         lg.goat_id, lg.load_ref, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d, o.weight_kg::float8 AS w,
         cs.location_id AS bloc, COALESCE(cs.partition_label, '') AS bpart
  FROM weighing_observations o
  JOIN weighing_campaign_sheds cs ON cs.campaign_shed_id = o.campaign_shed_id AND cs.tenant_id = o.tenant_id
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = o.tenant_id
  JOIN goat_identifiers gi
    ON gi.tenant_id = o.tenant_id AND gi.status = 'active'
   AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
   AND gi.normalized_value = lower(btrim(o.scanned_identifier))
  JOIN load_goats lg ON lg.goat_id = gi.goat_id
  WHERE o.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND cs.status <> 'canceled'
    AND cs.weighing_category = 'individual_animal'
    AND $7::text <> 'per_shed_partition'
    AND o.verification_status <> 'rejected'
    AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
  ORDER BY lg.goat_id, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date, o.accepted_at DESC, o.observation_id DESC
),
scan_legs AS (
  SELECT goat_id, load_ref, d, bloc, bpart,
         (w - LAG(w) OVER x) * 1000.0 AS grams,
         d - LAG(d) OVER x AS days
  FROM scan_pts
  WINDOW x AS (PARTITION BY goat_id ORDER BY d)
),
-- Whole-pen weighs, one per pen per day, keyed on the resolved (building, pen key).
pen_obs AS (
  SELECT DISTINCT ON (pk.phys_id, pk.pen_key, pk.d)
         pk.phys_id, pk.pen_key, pk.d, pk.avg_kg, pk.bloc, pk.bpart
  FROM (
    SELECT ` + loadPenKey("cs.location_id", "cs.partition_label", "a") + `,
           (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           so.average_weight_kg::float8 AS avg_kg, so.accepted_at, so.shed_observation_id,
           cs.location_id AS bloc, COALESCE(cs.partition_label, '') AS bpart
    FROM weighing_shed_observations so
    JOIN weighing_campaign_sheds cs ON cs.campaign_shed_id = so.campaign_shed_id AND cs.tenant_id = so.tenant_id
    JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = so.tenant_id
    LEFT JOIN alias a ON a.location_id = cs.location_id
    WHERE so.tenant_id = $1::uuid
      AND c.park_id = ANY($2::uuid[])
      AND cs.status <> 'canceled'
      AND cs.weighing_category = 'per_shed_partition'
      AND $7::text <> 'individual_animal'
      AND so.withdrawn_at IS NULL
      AND so.verification_status <> 'rejected'
      AND so.accepted_at >= $3::timestamptz AND so.accepted_at < $4::timestamptz
  ) pk(phys_id, pen_key, d, avg_kg, accepted_at, shed_observation_id, bloc, bpart)
  ORDER BY pk.phys_id, pk.pen_key, pk.d, pk.accepted_at DESC, pk.shed_observation_id DESC
),
pen_legs AS (
  SELECT phys_id, pen_key, bloc, bpart, d,
         LAG(d) OVER w AS d1,
         (avg_kg - LAG(avg_kg) OVER w) * 1000.0 AS grams
  FROM pen_obs
  WINDOW w AS (PARTITION BY phys_id, pen_key ORDER BY d)
),
-- Where each load animal was, as STAYS: "in pen X from day A until day B". A stay starts on the
-- day of the move that put the animal there and ends on the day of its next move; before its first
-- recorded move it was in the pen that move took it FROM; with no move at all, where the register
-- has it now. Kept as a few hundred intervals rather than one row per (animal, weigh day): the
-- planner cannot estimate a per-day position table, and joining one twice cost seconds on STG.
-- projection-review: membership=goat_location_history of load animals plus one register row per never-moved load animal; group_key=(goat_id, from_day); join_cardinality=alias 0..1 per location, goats/goat_shed_partitions PK; pagination=NONE, bounded by the load animals' moves; scope=tenant
moves AS (
  SELECT h.goat_id, h.from_location_id, h.from_partition_label, h.to_location_id, h.to_partition_label,
         (h.occurred_at AT TIME ZONE 'Asia/Kolkata')::date AS day,
         row_number() OVER w AS n,
         LEAD((h.occurred_at AT TIME ZONE 'Asia/Kolkata')::date) OVER w AS next_day
  FROM goat_location_history h
  JOIN load_goats lg ON lg.goat_id = h.goat_id
  WHERE h.tenant_id = $1::uuid
  WINDOW w AS (PARTITION BY h.goat_id ORDER BY h.occurred_at, h.location_history_id)
),
stays_raw AS (
  SELECT goat_id, to_location_id AS loc, to_partition_label AS part, day AS from_day,
         COALESCE(next_day, DATE '9999-12-31') AS to_day
  FROM moves
  UNION ALL
  SELECT goat_id, from_location_id, from_partition_label, DATE '0001-01-01', day
  FROM moves WHERE n = 1 AND from_location_id IS NOT NULL
  UNION ALL
  SELECT lg.goat_id, g.shed_id, gsp.partition_label, DATE '0001-01-01', DATE '9999-12-31'
  FROM load_goats lg
  JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = lg.goat_id
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  WHERE NOT EXISTS (SELECT 1 FROM moves m WHERE m.goat_id = lg.goat_id)
),
stays AS MATERIALIZED (
  SELECT st.goat_id, st.from_day, st.to_day, ` + loadPenKey("st.loc", "st.part", "ha") + `
  FROM stays_raw st
  LEFT JOIN alias ha ON ha.location_id = st.loc
),
-- A stay covers day d when from_day <= d < to_day; an animal counts for a day only while it was on
-- the farm (bought on or before, not yet exited).
covered AS (
  SELECT s.goat_id, lg.load_ref, s.from_day, s.to_day, s.phys_id, s.pen_key, lg.purchase_date,
         (lg.exited_at AT TIME ZONE 'Asia/Kolkata')::date AS exit_day
  FROM stays s(goat_id, from_day, to_day, phys_id, pen_key)
  JOIN load_goats lg ON lg.goat_id = s.goat_id
),
-- A pen leg counts for a load animal whose ONE stay covers both ends of it.
legs AS (
  -- Each leg carries its animal's LOAD, so no read downstream joins back to the animals.
  SELECT goat_id, load_ref, d, bloc, bpart, grams, days FROM scan_legs WHERE days > 0
  UNION ALL
  SELECT c.goat_id, c.load_ref, pl.d, pl.bloc, pl.bpart, pl.grams, pl.d - pl.d1
  FROM pen_legs pl
  JOIN covered c
    ON c.phys_id = pl.phys_id AND c.pen_key = pl.pen_key
   AND c.from_day <= pl.d1 AND pl.d < c.to_day
   AND (c.purchase_date IS NULL OR pl.d1 >= c.purchase_date)
   AND (c.exit_day IS NULL OR c.exit_day >= pl.d)
  WHERE pl.d1 IS NOT NULL AND pl.d > pl.d1
),
scoped_legs AS (
  SELECT * FROM legs
  WHERE $8::text = '' OR (bloc::text = $8::text AND bpart = $9::text)
)`

// loadAnimalsWindowSQL answers the whole window per load: its gain, its animals' latest average
// weight, and where its live animals stand today.
var loadAnimalsWindowSQL = ` -- scale-guard:ignore: bounded tenant + authorized-park reporting read over the purchased animals and the weighs of one selected window; one round trip per load chart
WITH ` + loadAnimalLegsCTE + `,
animal_gain AS (
  SELECT goat_id, sum(grams) / NULLIF(sum(days), 0) AS g, min(d - days) AS first_d, max(d) AS last_d
  FROM scoped_legs
  GROUP BY goat_id
),
-- Each load animal's LATEST weight in the window: its own scan, or the average of a pen it was in
-- on a pen-weigh day. A scan wins a tie, being the animal's own figure.
points AS (
  SELECT goat_id, d, w, 0 AS pref FROM scan_pts
  WHERE $8::text = '' OR (bloc::text = $8::text AND bpart = $9::text)
  UNION ALL
  SELECT c.goat_id, po.d, po.avg_kg, 1
  FROM pen_obs po
  JOIN covered c
    ON c.phys_id = po.phys_id AND c.pen_key = po.pen_key
   AND c.from_day <= po.d AND po.d < c.to_day
   AND (c.purchase_date IS NULL OR po.d >= c.purchase_date)
   AND (c.exit_day IS NULL OR c.exit_day >= po.d)
  WHERE $8::text = '' OR (po.bloc::text = $8::text AND po.bpart = $9::text)
),
latest AS (
  SELECT DISTINCT ON (goat_id) goat_id, w FROM points ORDER BY goat_id, d DESC, pref
),
-- Where the load's LIVE animals stand now, from the register: the pens named beside the load.
-- projection-review: membership=live load animals; group_key=(load_ref, park, shed, partition); join_cardinality=goats PK, goat_shed_partitions PK (tenant_id, goat_id), locations PK: each 0..1 per animal; pagination=NONE; scope=tenant + park ANY($2)
placed AS (
  SELECT lg.load_ref, COALESCE(NULLIF(pk.location_code, ''), pk.name, '') AS park_name,
         COALESCE(sh.name, '') AS shed_name, COALESCE(NULLIF(gsp.partition_label, 'whole'), '') AS partition_label,
         count(*)::int AS animals
  FROM load_goats lg
  JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = lg.goat_id AND g.exited_at IS NULL
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  LEFT JOIN locations sh ON sh.location_id = g.shed_id AND sh.tenant_id = $1::uuid
  LEFT JOIN locations pk ON pk.location_id = g.park_id AND pk.tenant_id = $1::uuid
  GROUP BY 1, 2, 3, 4
),
per_load AS (
  SELECT lg.load_ref, min(lg.owner_name) AS owner_name,
         count(lt.goat_id)::int AS animals,
         avg(lt.w) AS avg_kg,
         avg(gg.g) AS gain,
         count(gg.g)::int AS gain_animals,
         max(gg.last_d) - min(gg.first_d) AS span
  FROM load_goats lg
  LEFT JOIN latest lt ON lt.goat_id = lg.goat_id
  LEFT JOIN animal_gain gg ON gg.goat_id = lg.goat_id
  GROUP BY lg.load_ref
)
SELECT pl.load_ref, pl.owner_name, pl.animals, pl.avg_kg, pl.gain, pl.gain_animals, COALESCE(pl.span, 0),
       COALESCE((SELECT jsonb_agg(jsonb_build_object(
                   'park_name', p.park_name, 'shed_display_name', p.shed_name,
                   'partition_label', p.partition_label, 'animals', p.animals)
                 ORDER BY p.park_name, p.shed_name, p.partition_label)
                 FROM placed p WHERE p.load_ref = pl.load_ref), '[]'::jsonb)
FROM per_load pl
WHERE pl.animals > 0
  AND pl.load_ref IN (SELECT load_ref FROM live_loads)
ORDER BY pl.gain DESC NULLS LAST, pl.load_ref`

// loadAnimalsBucketTemplate is the Time-wise per-load series: each animal's gain inside each
// bucket (legs bucketed by their LATER weigh, the rule every Time-wise series uses), averaged per
// load.
var loadAnimalsBucketTemplate = ` -- scale-guard:ignore: bounded tenant + authorized-park reporting read; one round trip per Time-wise load table
WITH ` + loadAnimalLegsCTE + `,
bucketed AS (
  SELECT {{BUCKET_D}} AS week_start, load_ref, goat_id, sum(grams) / NULLIF(sum(days), 0) AS g
  FROM scoped_legs
  GROUP BY 1, 2, 3
),
owners AS (
  SELECT load_ref, min(owner_name) AS owner_name FROM load_goats GROUP BY load_ref
)
SELECT b.load_ref, COALESCE((SELECT o.owner_name FROM owners o WHERE o.load_ref = b.load_ref), ''),
       b.week_start::text, count(*)::bigint, avg(b.g)::float8
FROM bucketed b
WHERE b.g IS NOT NULL
  AND b.load_ref IN (SELECT load_ref FROM live_loads)
GROUP BY b.load_ref, b.week_start
ORDER BY b.load_ref, b.week_start`

// loadAnimalWindow is the per-load window read behind every load chart (the Weights load bars, the
// ADG Analytics Comparison tab, Sales › Loads).
func (r *Repository) loadAnimalWindow(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string) ([]domain.LoadGainBucket, error) {
	out := []domain.LoadGainBucket{}
	if len(parkIDs) == 0 {
		return out, nil
	}
	bound, err := sqlbind.Bind(loadAnimalsWindowSQL, tenantID, parkIDs, periodStart, periodEnd,
		strings.ToLower(strings.TrimSpace(sex)), loadAnimalOrigin(origin), weighingCategory, "", "")
	if err != nil {
		return nil, fmt.Errorf("weighing: bind load animals: %w", err)
	}
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			b           domain.LoadGainBucket
			avgKg, gain *float64
			gainAnimals int
			span        int
			placements  []byte
		)
		if err := rows.Scan(&b.LoadRef, &b.OwnerName, &b.Animals, &avgKg, &gain, &gainAnimals, &span, &placements); err != nil {
			return nil, err
		}
		if avgKg != nil {
			b.AverageWeightKg = *avgKg
		}
		b.GainGPerDay = gain
		if gain != nil {
			b.GainSpanDays = span
		}
		if b.Placements, err = decodeLoadAnimalPlacements(placements); err != nil {
			return nil, err
		}
		b.Sheds = len(b.Placements)
		out = append(out, b)
	}
	return out, rows.Err()
}

// loadAnimalBuckets is the Time-wise per-load series, on the same animals and the same legs.
func (r *Repository) loadAnimalBuckets(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string, scope domain.TimeScope) ([]domain.WeightGainLoadWeekBucket, error) {
	out := []domain.WeightGainLoadWeekBucket{}
	if len(parkIDs) == 0 {
		return out, nil
	}
	query := bucketedQuery(loadAnimalsBucketTemplate, scope.Bucket, 10)
	args := []any{tenantID, parkIDs, periodStart, periodEnd,
		strings.ToLower(strings.TrimSpace(sex)), loadAnimalOrigin(origin), weighingCategory,
		strings.TrimSpace(scope.PenLocationID), strings.TrimSpace(scope.PenPartitionLabel)}
	if scope.Bucket == domain.GainBucketMonth {
		args = append(args, gainBucketAnchor(periodEnd))
	}
	bound, err := sqlbind.Bind(query, args...)
	if err != nil {
		return nil, fmt.Errorf("weighing: bind load animal buckets: %w", err)
	}
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p domain.WeightGainLoadWeekBucket
		if err := rows.Scan(&p.LoadRef, &p.OwnerName, &p.WeekStart, &p.Animals, &p.AverageGainGPerDay); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// decodeLoadAnimalPlacements composes each placement's operational display from the REGISTER's own
// (building, partition), which is always the canonical pair -- so this uses oploc directly and
// then merges any two rows that resolve to the same pen.
func decodeLoadAnimalPlacements(raw []byte) ([]domain.LoadPlacement, error) {
	out := []domain.LoadPlacement{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("weighing: decode load placements: %w", err)
	}
	for i := range out {
		p := &out[i]
		_, _, p.OperationalLocationDisplay = oploc.ResolveComposedName("", p.ShedDisplayName, p.PartitionLabel)
	}
	return mergeSameOperationalLocation(out), nil
}

// loadAnimalOrigin normalizes the origin filter for the load-animal reads, so the retired
// "purchased" key still means the load cohort. An unknown value never reaches here: the service
// refuses it (validateOriginFilter) before any read runs.
func loadAnimalOrigin(origin string) string {
	normalized, _ := animalorigin.Normalize(origin)
	return normalized
}
