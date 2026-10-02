package postgres

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/farmvaluation"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// ASSUMED VALUE = LATEST WEIGHT x SALES CONFIG ₹/KG (maintainer decision 2026-10-02, replacing the
// 2026-09-25 "by weight only when every animal is weighed, else a per-animal sold price" rule).
//
// Load wise carries the animals a load still holds at an ASSUMED value: each live animal's latest
// weight x the ₹/kg of its stage-and-sex bucket on Sales Config's Farm valuation. An animal with no
// weight yet carries the CURRENT AVERAGE weight of its species in the load (else the whole load's
// when none of its species is weighed). No sold price is ever spread over the animals left. This read supplies
// the facts per load at request time -- nothing is stored or backfilled.
//
// RECORDED CROSS-MODULE REPORTING READ, pointing OUT of procurement the same way loadwiseSalesSQL
// already reads goats: it reads weighing's observation tables and Sales Config's valuation
// assumptions.
// Weighing is not changed and reads nothing new (its isolation rule binds files under
// backend/internal/weighing); nothing here gates a scan, a submit or a sale.
//
// A live animal's LATEST WEIGHT is the newer of:
//   - its own scan: a weighing_observations row whose tag resolves to it (goat_identifiers, active
//     animal_identifier_1/2), on or after the purchase date, not sent back for rework (a bounced
//     proof is an untrusted weight -- the growth director's rule);
//   - the latest whole-pen weigh of the pen it STANDS IN NOW (weighing_shed_observations, not
//     withdrawn, not rework), taken on or after the day it last moved (else its purchase date), so a
//     pen average from before the animal arrived in that pen is never credited to it.
//
// A scan wins a tie, being the animal's own figure.
//
// PRICE per kg: Sales Config's Farm valuation, through farmvaluation's SHARED fragments -- the very
// SQL Farm value runs -- so the two pages cannot price one animal two ways: authored stage (milk
// cohort first, then management stage), SPECIES (goat or sheep, 2026-10-02), gender (Mother is
// female; none on file reads female), bucket `<stage>_<species>_<gender>`, that bucket's ₹/kg. The
// bucket's fixed_weight_kg is NOT used here: Load wise values by the animal's own weight. An animal
// whose stage or species has no price is left out of the value, never priced at a guess.
//
// PEN IDENTITY: a weighing bucket often names a legacy per-pen location ("Godel 2 - Part 1", no
// partition) while the register names the building plus a partition ("Godel 2" + "Part 1"). Both
// resolve to (building, scrubbed pen key) with the same bridge growthdirector fcr.go uses.
//
// projection-review: membership=live accepted load animals of the served loads, one row per goat via the loadwise DISTINCT ON total order; group_key=load_id on the output, 1:1 with loadwiseSalesSQL's served rows; join_cardinality=goats PK and goat_shed_partitions PK 1:1, alias 0..1 per location (LIMIT 1 lateral), latest scan and latest pen weigh each collapsed to 0..1 per animal by DISTINCT ON / ORDER BY LIMIT 1, price 0..1 per animal (stage_by_match DISTINCT ON match_norm for the cohort and the stage joins, rates DISTINCT ON bucket; the load average is a window over the same per-goat rows, so it adds no row); pagination=bound to the served load ids ($2); scope=tenant_id on every table
//
// scale-guard:plan-proof-exempt: PENDING at-scale plan test (docs/progress/plan-proof-backlog.md); guarded by served load ids but not yet proven at 500k rows.
var loadStockWeightSQL = /* scale-guard:ignore: god-cte -- one bounded reporting read over the served loads' live animals (the 5k-50k envelope), their weighs and one price table, run once per Load wise request */ `
WITH member AS (
    SELECT DISTINCT ON (plg.goat_id) plg.goat_id, plg.load_id
    FROM public.procurement_load_goats plg
    WHERE plg.tenant_id = $1 AND plg.current_state = 'accepted_herd_intake'
    ORDER BY plg.goat_id, plg.intake_accepted_at DESC NULLS LAST, plg.created_at DESC, plg.load_goat_id DESC
),
alias AS (
    SELECT s.location_id, p.location_id AS phys_id,
           regexp_replace(lower(btrim(substr(s.name, length(p.name) + 1))), '^[-\s]*(part|pt)?[\s.-]*', '') AS pen_key
    FROM public.locations s
    JOIN LATERAL (
        SELECT b.location_id, b.name
        FROM public.locations b
        WHERE b.tenant_id = $1
          AND b.parent_location_id = s.parent_location_id
          AND b.location_type = 'shed'
          AND b.status = 'active'
          AND b.retired_at IS NULL
          AND b.name <> s.name
          AND (s.name LIKE b.name || ' %' OR s.name LIKE b.name || ' - %')
        ORDER BY length(b.name) DESC
        LIMIT 1
    ) p ON true
    WHERE s.tenant_id = $1 AND s.location_type = 'shed'
),
live AS (
    SELECT m.load_id, g.goat_id,
           g.management_stage AS stage_raw, g.milk_cohort AS cohort_raw, g.species AS species_raw, g.sex AS sex_raw,
           CASE WHEN COALESCE(NULLIF(lower(btrim(gsp.partition_label)), 'whole'), '') <> '' THEN g.shed_id
                ELSE COALESCE(a.phys_id, g.shed_id) END AS phys_id,
           CASE WHEN COALESCE(NULLIF(lower(btrim(gsp.partition_label)), 'whole'), '') <> ''
                THEN regexp_replace(lower(btrim(gsp.partition_label)), '^[-\s]*(part|pt)?[\s.-]*', '')
                ELSE COALESCE(a.pen_key, '') END AS pen_key,
           COALESCE(
               (SELECT max((h.occurred_at AT TIME ZONE 'Asia/Kolkata')::date)
                FROM public.goat_location_history h
                WHERE h.tenant_id = $1 AND h.goat_id = g.goat_id),
               pl.purchase_date) AS in_pen_since,
           pl.purchase_date
    FROM member m
    JOIN public.procurement_loads pl ON pl.tenant_id = $1 AND pl.load_id = m.load_id
    JOIN public.goats g ON g.tenant_id = $1 AND g.goat_id = m.goat_id
    LEFT JOIN public.goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
    LEFT JOIN alias a ON a.location_id = g.shed_id
    WHERE m.load_id = ANY($2::uuid[])
      -- The SAME live set as loadwiseSalesSQL's 'remaining' outcome.
      AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
      AND COALESCE(g.exit_reason, '') NOT IN ('sold', 'died', 'culled', 'transferred', 'lost')
),
scan AS (
    SELECT DISTINCT ON (l.goat_id) l.goat_id, o.weight_kg::float8 AS w, o.accepted_at AS at
    FROM live l
    JOIN public.goat_identifiers gi
      ON gi.tenant_id = $1 AND gi.goat_id = l.goat_id AND gi.status = 'active'
     AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    JOIN public.weighing_observations o
      ON o.tenant_id = $1 AND upper(btrim(o.scanned_identifier)) = gi.normalized_value
    WHERE o.verification_status <> 'rework'
      AND (l.purchase_date IS NULL OR (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date >= l.purchase_date)
    ORDER BY l.goat_id, o.accepted_at DESC, o.observation_id DESC
),
pen_obs AS (
    SELECT pk.phys_id, pk.pen_key, (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           so.average_weight_kg::float8 AS avg_kg, so.accepted_at, so.shed_observation_id
    FROM public.weighing_shed_observations so
    JOIN public.weighing_campaign_sheds cs ON cs.tenant_id = so.tenant_id AND cs.campaign_shed_id = so.campaign_shed_id
    LEFT JOIN alias a ON a.location_id = cs.location_id
    CROSS JOIN LATERAL (
        SELECT CASE WHEN COALESCE(NULLIF(lower(btrim(cs.partition_label)), 'whole'), '') <> '' THEN cs.location_id
                    ELSE COALESCE(a.phys_id, cs.location_id) END AS phys_id,
               CASE WHEN COALESCE(NULLIF(lower(btrim(cs.partition_label)), 'whole'), '') <> ''
                    THEN regexp_replace(lower(btrim(cs.partition_label)), '^[-\s]*(part|pt)?[\s.-]*', '')
                    ELSE COALESCE(a.pen_key, '') END AS pen_key
    ) pk
    WHERE so.tenant_id = $1 AND so.withdrawn_at IS NULL AND so.verification_status <> 'rework'
      AND cs.status <> 'canceled' AND so.average_weight_kg > 0
),
pen AS (
    SELECT l.goat_id, p.w, p.at
    FROM live l
    JOIN LATERAL (
        SELECT po.avg_kg AS w, po.accepted_at AS at
        FROM pen_obs po
        WHERE po.phys_id = l.phys_id AND po.pen_key = l.pen_key
          AND (l.in_pen_since IS NULL OR po.d >= l.in_pen_since)
        ORDER BY po.accepted_at DESC, po.shed_observation_id DESC
        LIMIT 1
    ) p ON true
),
latest AS (
    SELECT DISTINCT ON (goat_id) goat_id, w
    FROM (SELECT goat_id, w, at, 0 AS pref FROM scan UNION ALL SELECT goat_id, w, at, 1 FROM pen) x
    ORDER BY goat_id, at DESC, pref
),
-- THE PRICING RULE IS SHARED with Farm value (farmvaluation.PricingCTEs): authored stages, the
-- species x gender buckets and their ₹/kg. Spliced, never copied (loadwise-stock-valuation-guard).
` + farmvaluation.PricingCTEs + `,
-- projection-review: membership=live accepted animals of the served loads, one row per goat (member DISTINCT ON goat, live filter = loadwiseSalesSQL's remaining outcome); group_key=load_id, and the load average is a window over those same per-goat rows; join_cardinality=fv_stage_by_match 0..1 per register entry for the cohort and the stage joins, fv_rates 0..1 per stage_species_gender bucket (DISTINCT ON), latest 0..1 per goat; pagination=bound to the served load ids ($2), so a smaller page never changes a load's value; scope=tenant_id on every table, park narrowing happens upstream on the served loads
priced AS (
    SELECT l.load_id, l.goat_id, lt.w, r.price_per_kg AS price,
           CASE WHEN lower(btrim(COALESCE(l.species_raw, ''))) NOT IN ('goat', 'sheep') THEN 'animals with no species recorded'
                ELSE COALESCE(NULLIF(btrim(COALESCE(l.stage_raw, '')), ''), NULLIF(btrim(COALESCE(l.cohort_raw, '')), ''), 'no stage') END AS stage_label,
           -- An unweighed animal carries its OWN SPECIES' average in the load (sheep and goats grow
           -- to different weights), else the whole load's when no animal of its species is weighed.
           COALESCE(avg(lt.w) OVER (PARTITION BY l.load_id, lower(btrim(COALESCE(l.species_raw, '')))),
                    avg(lt.w) OVER (PARTITION BY l.load_id)) AS load_avg
    FROM live l
    ` + farmvaluation.StageJoinsSQL(farmvaluation.NormSQL("l.cohort_raw"), farmvaluation.NormSQL("l.stage_raw")) + `
    LEFT JOIN fv_rates r ON r.bucket = ` + farmvaluation.BucketKeySQL(farmvaluation.NormSQL("l.stage_raw"), "l.species_raw", "l.sex_raw") + `
    LEFT JOIN latest lt ON lt.goat_id = l.goat_id
)
SELECT load_id::text,
       count(*)::int AS live_animals,
       count(w)::int AS weighed_animals,
       COALESCE(sum(w), 0)::float8 AS total_kg,
       max(load_avg)::float8 AS avg_kg,
       COALESCE(sum(COALESCE(w, load_avg) * price), 0)::float8 AS value,
       count(*) FILTER (WHERE price IS NULL)::int AS unpriced_animals,
       COALESCE(array_agg(DISTINCT stage_label) FILTER (WHERE price IS NULL), '{}') AS unpriced_stages,
       COALESCE(sum(price) FILTER (WHERE w IS NULL AND load_avg IS NULL), 0)::float8 AS unweighed_price_sum
FROM priced
GROUP BY load_id`

// attachStockWeight fills StockWeight for every served load that still holds animals, in ONE
// set-based read. A failure is returned: the assumed value would otherwise silently fall back.
func (r *Repository) attachStockWeight(ctx context.Context, tenantID string, loads []domain.LoadwiseLoad) error {
	ids := make([]string, 0, len(loads))
	for _, l := range loads {
		if l.Remaining > 0 {
			ids = append(ids, l.LoadID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	bound := sqlbind.MustBind(loadStockWeightSQL, tenantID, ids)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return fmt.Errorf("procurement: loadwise stock weight: %w", err)
	}
	defer rows.Close()
	byLoad := make(map[string]*domain.LoadStockWeight, len(ids))
	for rows.Next() {
		var (
			loadID string
			w      domain.LoadStockWeight
		)
		if err := rows.Scan(&loadID, &w.LiveAnimals, &w.WeighedAnimals, &w.TotalKg, &w.AvgKg, &w.Value,
			&w.UnpricedAnimals, &w.UnpricedStages, &w.UnweighedPriceSum); err != nil {
			return fmt.Errorf("procurement: loadwise stock weight scan: %w", err)
		}
		byLoad[loadID] = &w
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("procurement: loadwise stock weight rows: %w", err)
	}
	for i := range loads {
		loads[i].StockWeight = byLoad[loads[i].LoadID]
	}
	return nil
}
