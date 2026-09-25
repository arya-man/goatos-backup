package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// LOAD-WISE SALES read + load-cost write (maintainer decision 2026-08-31,
// docs/decisions/sales-loadwise.md).
//
// RECORDED CROSS-MODULE REPORTING READ. Procurement owns procurement_loads /
// procurement_load_goats and joins OUT to goats (each accepted animal's terminal outcome), and to
// goat_sale_allocations + sales_deals (the revenue its sold animals brought in). It is read-only
// over those tables and reporting-grain only: nothing here gates a sale, an exit, or any pipeline
// step, and the sales module's own lock (migration 000173: sales reads nothing from
// herd/procurement) is untouched because this dependency points the other way.

// loadwiseSalesSQL is the one statement behind the load-wise read.
//
// projection-review: membership=procurement_load_goats accepted rows deduped DISTINCT ON (tenant, goat) by the TOTAL order (intake_accepted_at DESC NULLS LAST, created_at DESC, load_goat_id DESC) so a tie or null instant still resolves to one stable load; group_key=load_id on both sides (stats GROUP BY m.load_id attaching 1:1 to procurement_loads PK); join_cardinality=goats 1:1 on PK, deal_share at most 1:1 via the tagged partial unique index with the per-deal tagged count pre-aggregated, park lateral 1:1 collapsed agree-or-go-bare; pagination=LIMIT $2 newest loads after the optional $3 park filter, with total_loads a window count over the same filtered pre-LIMIT set; scope=tenant_id on every branch
//
// The prose form of that proof: membership = procurement_load_goats at (tenant_id, goat_id), deduplicated by
// DISTINCT ON (goat_id) over current_state = 'accepted_herd_intake' rows, ordered by
// (intake_accepted_at DESC NULLS LAST, created_at DESC, load_goat_id DESC) -- a TOTAL order, so one
// animal counts on exactly ONE load and always the SAME one, even when the table holds two accepted
// rows for it with tied or null acceptance instants (the schema's uniqueness is per
// (tenant, load, goat), not per goat). group_key = load_id on both the stats producer (GROUP BY m.load_id) and the consumer row
// (procurement_loads.load_id, its PK) — a 1:1 attach. Join cardinalities inside stats: goats is
// 1:1 with member on (tenant_id, goat_id) (goats PK); deal_share is at most 1:1 with member
// because goat_sale_allocations carries a live-uniqueness partial index on (tenant_id, goat_id)
// WHERE status = 'tagged'; the park label lateral is 1:1 on locations PK and collapses
// agree-or-go-bare through the (animals_with_park = purchased AND distinct_parks = 1) pair, never
// a majority pick and never count(DISTINCT) alone -- that skips NULLs and would read an unknown
// park as agreement. Inside deal_share the
// per-deal tagged count pre-aggregates the many side at (tenant_id, sales_deal_id) before the
// join, so a deal's value divides over exactly its tagged animals and sums back to at most the
// deal's value. Every count the row reports (purchased / sold / mortality / other exits /
// remaining / tagged-not-closed) FILTERs the SAME member-x-goats row set, so purchased = sold +
// mortality + other + remaining + tagged-not-closed + unaccounted holds by construction;
// unaccounted is derived in the domain from these six, never counted separately. The read serves the newest $2 loads while summary totals are
// derived in the domain from exactly the served rows (the section's stated scope), and
// total_loads is the whole-tenant count so the screen can say when older loads are not shown.
//
// scale-guard:ignore: bounded reporting read over the procurement pipeline's own tables — member
// count grows with procured animals (the 5k–50k envelope), aggregated once per request in one
// set-based statement over indexed tenant scans (procurement_load_goats_goat_state_idx, goats PK,
// the goat_sale_allocations live-uniqueness index), and the row set served is LIMITed. Same shape
// and reasoning as the sales overview read.
//
// The ninth CTE (remaining_mix, 2026-09-24) reads no new table: it regroups the rows the outcomes
// CTE already built, so the statement's scans are the same as with eight.
// scale-guard:ignore: god-cte -- one bounded, LIMITed, set-based reporting read (reasons above); remaining_mix regroups the outcomes CTE and adds no scan
const loadwiseSalesSQL = `
WITH member AS (
    SELECT DISTINCT ON (plg.goat_id)
           plg.goat_id, plg.load_id
    FROM public.procurement_load_goats plg
    WHERE plg.tenant_id = $1 AND plg.current_state = 'accepted_herd_intake'
    -- The ORDER BY must produce a TOTAL order, not just a business-preferred one. Ranking on the
    -- acceptance instant alone is nondeterministic exactly where it matters: intake_accepted_at is
    -- nullable, and AcceptIntake stamps ONE timestamp across a whole batch, so two accepted rows
    -- for the same goat can tie or both be null. Postgres would then pick either load per read,
    -- silently moving that animal's outcome and its deal share between loads.
    --
    -- created_at is the tie-breaker rather than updated_at: updated_at carries no trigger here but
    -- IS rewritten by other paths, so ordering on it would let an unrelated edit move a goat
    -- between loads. load_goat_id is the final backstop -- immutable and unique, so the order is
    -- total even when every timestamp ties. Arbitrary in that last case, but STABLE, which is the
    -- property the read needs.
    ORDER BY plg.goat_id, plg.intake_accepted_at DESC NULLS LAST, plg.created_at DESC, plg.load_goat_id DESC
),
` + saleLineShareCTEs + // scale-guard:ignore: god-cte -- the loadwiseSalesSQL tail after the shared pricing CTEs; same bounded, LIMITed reporting read annotated on the const
	`,
deal_share AS (
    -- Each animal's price comes from ITS OWN SALE LINE through the shared saleLineShareCTEs (the
    -- same CTE Farm born reads), and only a CLOSED deal is a sale (maintainer decisions
    -- 2026-09-25). closed says whether the allocation's deal is closed; share is NULL otherwise.
    -- projection-review: membership=animal_share, one row per live tagged animal; group_key=goat_id, priced per (deal, species, bucket) inside saleLineShareCTEs; join_cardinality=1:{0,1} per member goat through the tagged partial unique index; pagination=priced before the LIMITed load window; scope=tenant_id
    SELECT goat_id, closed, share, sale_date FROM animal_share
),
sale_weight_sample AS (
    -- Newer GoatOS sales DO have per-animal sale weights on goat_sale_allocations. The legacy
    -- procurement_loads.sold_* sample still wins when it exists; this fallback fills the same
    -- sample shape for tagged, closed live-animal deals so a partially sold load can show what
    -- its weighed sale kilograms actually fetched. Value is weight-proportional on the SAME
    -- allocation rows as the kg, never the load's full sold value.
    SELECT m.load_id,
           sum(a.weight_kg)::float8 AS sold_weight_kg,
           count(*)::int AS sold_weighed_animals,
           sum(a.weight_kg * lp.price_per_kg)::float8 AS sold_weighed_value
    FROM member m
    JOIN public.goat_sale_allocations a
      ON a.tenant_id = $1
     AND a.goat_id = m.goat_id
     AND a.status = 'tagged'
     AND a.weight_kg > 0
    JOIN public.sales_deals d
     ON d.tenant_id = a.tenant_id
     AND d.id = a.sales_deal_id
     AND d.status = 'Deal Closed'
     AND d.total_weight_kg > 0
     AND d.sales_value > 0
    -- projection-review: membership=tagged positive-weight allocations on closed sales with stamped animal lines; group_key=lines grouped by (tenant_id, deal_id), then allocated samples by load_id; join_cardinality=one deduplicated load member per goat, one tagged allocation per goat, at most one lateral rate per deal; pagination=sample computed before final load window; scope=tenant constrained allocations, deals and lines, park filter applied to served loads
    JOIN LATERAL (
        -- Allocations point at the deal, not a specific product/breed line. A mixed deal can still
        -- feed a truthful per-kg sample only when the live-animal lines share one recorded price.
        -- Stamped kinds, not editable labels, identify animals.
        -- Non-live lines (for example manure) have no allocation target, so they do not make a
        -- live allocation ambiguous. If goat and sheep lines differ, the allocation has no line key
        -- to choose the right price, so the fallback stays absent instead of blending two markets.
        SELECT min(l.sales_value / l.total_weight_kg) FILTER (
                   WHERE l.product_kind = 'animal'
                     AND l.total_weight_kg > 0
                     AND l.sales_value > 0
               )::float8 AS price_per_kg
        FROM public.sales_deal_lines l
        WHERE l.tenant_id = d.tenant_id
          AND l.deal_id = d.id
        GROUP BY l.tenant_id, l.deal_id
        HAVING count(*) FILTER (
               WHERE l.product_kind = 'animal'
                 AND l.total_weight_kg > 0
                 AND l.sales_value > 0
           ) > 0
           AND min(l.sales_value / l.total_weight_kg) FILTER (
                   WHERE l.product_kind = 'animal'
                     AND l.total_weight_kg > 0
                     AND l.sales_value > 0
               ) =
               max(l.sales_value / l.total_weight_kg) FILTER (
                   WHERE l.product_kind = 'animal'
                     AND l.total_weight_kg > 0
                     AND l.sales_value > 0
               )
           AND (
               d.product_type = 'Mixed'
               OR (
                   count(*) FILTER (WHERE l.product_kind = 'animal') = 1
                   AND min(l.product_type) FILTER (WHERE l.product_kind = 'animal') = d.product_type
               )
           )
    ) lp ON true
    GROUP BY m.load_id
),
outcomes AS (
    -- One DISJOINT outcome bucket per member animal, so the five counts partition purchased by
    -- construction. Sold and dead are checked on lifecycle OR exit_reason (belt and braces for
    -- seeded history where only one side is stamped); the live set is the alive + clinical states
    -- (an animal in ICU is still on farm); merged/inactive and anything unclassifiable fall to
    -- 'unaccounted', which the domain derives as the arithmetic gap over these same rows.
    SELECT m.load_id,
           ds.share,
           -- The CLOSED deal's sale date, for the fattening span a GoatOS-era sale has no imported
           -- figure for. NULL for anything but a closed tagged sale.
           CASE WHEN ds.closed THEN ds.sale_date END AS sale_date,
           gp.park_code,
           lower(g.species) AS species,
           COALESCE(g.management_stage, '') AS stage,
           COALESCE(lower(g.sex), '') AS sex,
           CASE
               -- Sold only when the sale is real: no tagged allocation (a pre-tagging or
               -- register-only exit), or one on a CLOSED deal. An animal exited against a deal
               -- still open is 'tagged_open' below -- the register says sold, the ledger says not
               -- yet, and that is shown as its own bucket rather than counted as a sale.
               WHEN (g.lifecycle_status = 'sold' OR g.exit_reason = 'sold')
                    AND (ds.goat_id IS NULL OR ds.closed) THEN 'sold'
               -- Tagged to a sale whose deal is NOT closed yet (maintainer decision 2026-09-25):
               -- the animal left the herd when it was tagged, but it is not a sale until the deal
               -- closes. Its own bucket, so the load balances instead of reading Unaccounted; it
               -- moves to 'sold' when the deal closes, and a failed deal releases it to the herd.
               WHEN (g.lifecycle_status = 'sold' OR g.exit_reason = 'sold')
                    AND ds.goat_id IS NOT NULL AND NOT ds.closed THEN 'tagged_open'
               WHEN g.lifecycle_status = 'dead' OR g.exit_reason = 'died' THEN 'mortality'
               WHEN g.lifecycle_status IN ('culled', 'transferred', 'lost')
                    OR g.exit_reason IN ('culled', 'transferred', 'lost') THEN 'other'
               WHEN g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu') THEN 'remaining'
               ELSE 'unaccounted'
           END AS outcome
    FROM member m
    JOIN public.goats g ON g.tenant_id = $1 AND g.goat_id = m.goat_id
    LEFT JOIN deal_share ds ON ds.goat_id = m.goat_id
    LEFT JOIN LATERAL (
        SELECT upper(l.location_code) AS park_code
        FROM public.locations l
        WHERE l.tenant_id = $1 AND l.location_id = g.park_id
    ) gp ON true
),
-- The remaining animals per (species, management stage, sex) (maintainer decision 2026-09-24):
-- the Weighing Load-wise chart combines this mix with its latest-weight read and the stage x sex
-- sale price. Grouped to that grain first, then folded to ONE jsonb array per load, so it attaches
-- 1:1 like stats; the same outcome rule as stats, so the mix always sums to that load's remaining
-- count.
remaining_mix AS (
    SELECT load_id,
           jsonb_agg(jsonb_build_object('species', species, 'management_stage', stage, 'sex', sex, 'animals', n)
                     ORDER BY species, stage, sex) AS mix
    FROM (
        SELECT o.load_id, o.species, o.stage, o.sex, count(*)::int AS n
        FROM outcomes o
        WHERE o.outcome = 'remaining'
        GROUP BY o.load_id, o.species, o.stage, o.sex
    ) x
    GROUP BY load_id
),
prior AS (
    -- Pre-GoatOS outcomes, one aggregate per load. The natural key is (tenant, load, outcome), so
    -- this pre-aggregation is 1:1 with the load row it attaches to.
    SELECT load_id,
           COALESCE(sum(animal_count) FILTER (WHERE outcome = 'sold'), 0)::int AS prior_sold,
           (sum(sales_value) FILTER (WHERE outcome = 'sold'))::float8 AS prior_sold_value,
           min(first_on) FILTER (WHERE outcome = 'sold') AS prior_sold_first,
           max(last_on) FILTER (WHERE outcome = 'sold') AS prior_sold_last,
           COALESCE(sum(animal_count) FILTER (WHERE outcome = 'died'), 0)::int AS prior_dead,
           min(first_on) FILTER (WHERE outcome = 'died') AS prior_dead_first,
           max(last_on) FILTER (WHERE outcome = 'died') AS prior_dead_last
    FROM public.procurement_load_prior_outcomes
    WHERE tenant_id = $1
    GROUP BY load_id
),
stats AS (
    SELECT o.load_id,
           count(*)::int AS purchased,
           (count(*) FILTER (WHERE o.outcome = 'sold'))::int AS sold,
           (count(*) FILTER (WHERE o.outcome = 'mortality'))::int AS mortality,
           (count(*) FILTER (WHERE o.outcome = 'other'))::int AS other_exits,
           (count(*) FILTER (WHERE o.outcome = 'remaining'))::int AS remaining,
           (count(*) FILTER (WHERE o.outcome = 'tagged_open'))::int AS tagged_open,
           -- The remaining animals BY SPECIES (maintainer request 2026-09-03): the Weighing
           -- Comparison tab values stock at rates that differ between species/stages, so a load's
           -- remaining head count must arrive already split. Same rows,
           -- same outcome rule, so the two never add up to more than the remaining count.
           (count(*) FILTER (WHERE o.outcome = 'remaining' AND o.species = 'sheep'))::int AS remaining_sheep,
           (count(*) FILTER (WHERE o.outcome = 'remaining' AND o.species = 'goat'))::int AS remaining_goats,
           -- Animal-weighted mean sale DAY over the sold animals whose closed deal names one, as
           -- days since a fixed epoch; the reconciled row subtracts the load's arrival day from it
           -- (fattening span, maintainer request 2026-09-25). Same rows as sold, so it can never
           -- describe an animal the sold count does not.
           (avg(o.sale_date - DATE '2000-01-01') FILTER (WHERE o.outcome = 'sold' AND o.sale_date IS NOT NULL))::float8 AS sold_day_avg,
           COALESCE(sum(o.share) FILTER (WHERE o.outcome = 'sold'), 0)::float8 AS sold_value,
           (count(*) FILTER (WHERE o.outcome = 'sold' AND o.share IS NOT NULL))::int AS sold_priced,
           -- Agree-or-go-bare needs THREE facts, not one. count(DISTINCT) SKIPS NULLS, so a load
           -- holding one CBE animal and one animal whose park is unknown would report a single
           -- distinct park and claim CBE -- asserting an agreement that was never established.
           -- animals_with_park is what catches that: an unknown park is a DISAGREEMENT, because
           -- an animal that cannot name its park cannot vouch for the others.
           count(o.park_code)::int AS animals_with_park,
           count(DISTINCT o.park_code) AS distinct_parks,
           min(o.park_code) AS park_code
    FROM outcomes o
    GROUP BY o.load_id
)
-- projection-review: membership=procurement_loads (the served window itself); group_key=load_id
-- (procurement_loads PK) on every attached side; join_cardinality=parties 1:1 on source_party_id,
-- stats 1:1 by construction (GROUP BY m.load_id), remaining_mix 1:1 (grouped per (load, species, stage, sex) then folded GROUP BY load_id), prior 1:1 (GROUP BY load_id), and the 2026-09-01
-- cost columns are PLAIN COLUMNS of procurement_loads -- 1:1 with the row by definition, adding no
-- join and no fan-out; pagination=LIMIT $2 newest loads AFTER the park filter, with total_loads a
-- window count over the SAME filtered set (pre-LIMIT) so the count never means "of this page" and
-- never counts loads the filter hides; scope=tenant_id on the outer WHERE and on every CTE.
--
-- The PARK FILTER ($3, optional, maintainer report 2026-09-03: the top-bar park selector changed
-- nothing on Purchase and Born) matches the SAME agree-or-go-bare farm label the row already
-- reports, so the filter can never disagree with the Farm cell beside it. A load whose animals
-- disagree about their park, or whose park is unknown (bare farm), is claimed by NEITHER park and
-- appears only under All Parks -- the honest gap, same shape as the weighing sex/origin filters.
-- A park with no location_code resolves to NULL and matches nothing rather than matching the
-- bare-farm loads.
--
-- The ITEMISATION is deliberately NOT joined here. Cost lines are 1:N against a load, so joining
-- them into this statement would multiply every row -- purchased counts, sold value, the lot --
-- which is the classic fan-out this marker exists to refuse. They are fetched by
-- loadCostLinesSQL as a separate set-based read and attached in Go by load_id, so a load with
-- five cost lines is still exactly one row here.
, reconciled AS (
SELECT pl.load_id::text AS load_id, COALESCE(pl.context->>'load_ref', '') AS load_ref,
       COALESCE(p.display_name, '') AS vendor_name, pl.purchase_date, pl.status,
       pl.animal_cost::float8 AS animal_cost, pl.transport_cost::float8 AS transport_cost,
       pl.other_cost::float8 AS other_cost,
       pl.purchase_weight_kg::float8 AS purchase_weight_kg,
       pl.sold_weight_kg::float8 AS sold_weight_kg, pl.sold_weighed_animals,
       pl.sold_weighed_value::float8 AS sold_weighed_value,
       sw.sold_weight_kg AS sample_sold_weight_kg,
       sw.sold_weighed_animals AS sample_sold_weighed_animals,
       sw.sold_weighed_value AS sample_sold_weighed_value,
       pl.arrived_on,
       -- Arrival to sale, animal-weighted: the imported legacy span when the load carries one,
       -- else derived from the load's own animals on CLOSED deals. A span that runs backwards (a
       -- sale dated before arrival) is a bad date, not a fact about the load, and stays absent.
       COALESCE(pl.fattening_days,
                CASE WHEN s.sold_day_avg IS NOT NULL AND pl.arrived_on IS NOT NULL
                          AND round(s.sold_day_avg - (pl.arrived_on - DATE '2000-01-01')) >= 0
                     THEN round(s.sold_day_avg - (pl.arrived_on - DATE '2000-01-01'))::int END) AS fattening_days,
       pl.row_version,
       pl.expected_count,
       COALESCE(s.purchased, 0) AS purchased, COALESCE(s.sold, 0) AS sold,
       COALESCE(s.mortality, 0) AS mortality,
       COALESCE(s.other_exits, 0) AS other_exits, COALESCE(s.remaining, 0) AS remaining,
       COALESCE(s.tagged_open, 0) AS tagged_open,
       COALESCE(s.remaining_sheep, 0) AS remaining_sheep, COALESCE(s.remaining_goats, 0) AS remaining_goats,
       COALESCE(rm.mix, '[]'::jsonb) AS remaining_mix,
       COALESCE(s.sold_value, 0) AS sold_value, COALESCE(s.sold_priced, 0) AS sold_priced,
       -- The park every accepted animal agrees on; when none is attributed (a sold-out legacy
       -- load has no residents left) the load's OWN recorded farm answers instead. Both are the
       -- same fact stated by different sources, and neither is a majority pick.
       CASE
            -- NO animals attributed at all (a sold-out legacy load): the animals cannot answer, so
            -- the load's OWN recorded farm does. Nothing is being overruled here.
            WHEN COALESCE(s.purchased, 0) = 0 THEN COALESCE(upper(pl.context->>'farm'), '')
            -- Animals answer only when EVERY one of them names a park and they all name the SAME
            -- one. Any unknown park, or any disagreement, goes bare.
            WHEN s.animals_with_park = s.purchased AND s.distinct_parks = 1 THEN COALESCE(s.park_code, '')
            ELSE ''
       END AS farm,
       COALESCE(pr.prior_sold, 0) AS prior_sold, pr.prior_sold_value, pr.prior_sold_first, pr.prior_sold_last,
       COALESCE(pr.prior_dead, 0) AS prior_dead, pr.prior_dead_first, pr.prior_dead_last,
       pl.created_at
FROM public.procurement_loads pl
LEFT JOIN public.parties p ON p.party_id = pl.source_party_id
LEFT JOIN stats s ON s.load_id = pl.load_id
LEFT JOIN remaining_mix rm ON rm.load_id = pl.load_id
LEFT JOIN sale_weight_sample sw ON sw.load_id = pl.load_id
LEFT JOIN prior pr ON pr.load_id = pl.load_id
WHERE pl.tenant_id = $1
),
filtered AS (
    SELECT r.*
    FROM reconciled r
    WHERE nullif($3, '') IS NULL
       OR r.farm = (SELECT nullif(upper(l.location_code), '')
                    FROM public.locations l
                    WHERE l.tenant_id = $1 AND l.location_id = nullif($3, '')::uuid)
),
ranked AS (
    SELECT f.*,
           row_number() OVER (
               PARTITION BY CASE WHEN nullif($3, '') IS NULL THEN f.farm ELSE '' END
               ORDER BY f.purchase_date DESC NULLS LAST, f.created_at DESC, f.load_id
           ) AS farm_rank,
           count(*) OVER ()::int AS total_loads
    FROM filtered f
)
SELECT r.load_id, r.load_ref, r.vendor_name, r.purchase_date, r.status,
       r.animal_cost, r.transport_cost, r.other_cost,
       r.purchase_weight_kg,
       CASE WHEN r.sold_weight_kg IS NOT NULL THEN r.sold_weight_kg ELSE r.sample_sold_weight_kg END,
       CASE WHEN r.sold_weight_kg IS NOT NULL THEN r.sold_weighed_animals ELSE r.sample_sold_weighed_animals END,
       CASE WHEN r.sold_weight_kg IS NOT NULL THEN r.sold_weighed_value ELSE r.sample_sold_weighed_value END,
       r.arrived_on, r.fattening_days,
       r.row_version,
       r.expected_count,
       r.purchased, r.sold, r.mortality,
       r.other_exits, r.remaining, r.tagged_open, r.remaining_sheep, r.remaining_goats, r.remaining_mix,
       r.sold_value, r.sold_priced,
       r.farm,
       r.prior_sold, r.prior_sold_value, r.prior_sold_first, r.prior_sold_last,
       r.prior_dead, r.prior_dead_first, r.prior_dead_last,
       r.total_loads
FROM ranked r
ORDER BY
    CASE WHEN nullif($3, '') IS NULL THEN r.farm_rank ELSE 0 END,
    r.purchase_date DESC NULLS LAST, r.created_at DESC, r.load_id
LIMIT NULLIF($2, 0)`

// loadCostLinesSQL reads the itemisation for a whole page of loads at once. Ordered by the kind's
// own position in domain.CostLineKinds, so the breakdown reads animal -> transport -> booking ->
// labour -> transit -> feed rather than alphabetically; an unknown kind sorts last instead of
// disappearing.
const loadCostLinesSQL = `
SELECT l.load_id::text, l.line_id::text, l.kind, l.amount::float8, l.note, l.source
FROM public.procurement_load_cost_lines l
WHERE l.tenant_id = $1 AND l.load_id = ANY($2::uuid[])
ORDER BY l.load_id, COALESCE(array_position($3::text[], l.kind), 999), l.recorded_at, l.line_id`

// attachCostLines fills every row's CostLines in ONE query.
//
// A failure here is NOT fatal to the page: the three bucket figures are already scanned and they
// are what the list renders. Losing the breakdown costs the reader the detail behind a number,
// while failing the whole read costs them the number itself -- so this degrades rather than takes
// Purchase and Born down.
func (r *Repository) attachCostLines(ctx context.Context, tenantID string, loads []domain.LoadwiseLoad) error {
	if len(loads) == 0 {
		return nil
	}
	ids := make([]string, 0, len(loads))
	for _, load := range loads {
		ids = append(ids, load.LoadID)
	}

	rows, err := r.pool.Query(ctx, loadCostLinesSQL, tenantID, ids, domain.CostLineKinds)
	if err != nil {
		return fmt.Errorf("procurement: loadwise cost lines: %w", err)
	}
	defer rows.Close()

	byLoad := make(map[string][]domain.LoadCostLine, len(loads))
	for rows.Next() {
		var (
			loadID string
			line   domain.LoadCostLine
		)
		if err := rows.Scan(&loadID, &line.LineID, &line.Kind, &line.Amount, &line.Note, &line.Source); err != nil {
			return fmt.Errorf("procurement: loadwise cost lines scan: %w", err)
		}
		byLoad[loadID] = append(byLoad[loadID], line)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("procurement: loadwise cost lines rows: %w", err)
	}
	for i := range loads {
		lines := byLoad[loads[i].LoadID]
		loads[i].CostLines = lines
		if len(lines) > 0 {
			loads[i].AnimalCost, loads[i].TransportCost, loads[i].OtherCost = domain.RollUpCostLines(lines)
		}
	}
	return nil
}

// loadCostBucketSQL maps a line's kind onto the bucket column it rolls into, in SQL. It MIRRORS
// domain.CostBucketForKind (animal, transport, everything else -> other); the two must agree or a
// bucket edit would compare against, and delete, a different set of lines than the list rolls up.
const loadCostBucketSQL = `CASE WHEN kind IN ('animal', 'transport') THEN kind ELSE 'other' END`

// loadCostBucketSumsSQL is the current sum of each bucket's lines, for the lines of one load.
const loadCostBucketSumsSQL = `
SELECT ` + loadCostBucketSQL + ` AS bucket, sum(amount)::float8
FROM public.procurement_load_cost_lines
WHERE tenant_id = $1 AND load_id = $2
GROUP BY 1`

// clearLoadCostBucketsSQL removes the lines of the buckets whose total the edit CHANGED, and only
// those -- the write half of the itemisation, with insertLoadCostLinesSQL below.
const clearLoadCostBucketsSQL = `
DELETE FROM public.procurement_load_cost_lines
WHERE tenant_id = $1 AND load_id = $2 AND ` + loadCostBucketSQL + ` = ANY($3::text[])`

// loadCostBucketSums reads the current per-bucket line totals of one load under the edit's
// transaction. A bucket with no lines is absent from the map (nil), which is how "not recorded" is
// told apart from zero.
func loadCostBucketSums(ctx context.Context, tx pgx.Tx, tenantID, loadID string) (map[string]*float64, error) {
	rows, err := tx.Query(ctx, loadCostBucketSumsSQL, tenantID, loadID)
	if err != nil {
		return nil, fmt.Errorf("procurement: read load cost buckets: %w", err)
	}
	defer rows.Close()
	out := map[string]*float64{}
	for rows.Next() {
		var bucket string
		var sum float64
		if err := rows.Scan(&bucket, &sum); err != nil {
			return nil, fmt.Errorf("procurement: scan load cost bucket: %w", err)
		}
		out[bucket] = &sum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("procurement: load cost bucket rows: %w", err)
	}
	return out, nil
}

// One set-based insert over the named buckets, never an Exec per bucket.
const insertLoadCostLinesSQL = `
INSERT INTO public.procurement_load_cost_lines (tenant_id, load_id, kind, amount, source, recorded_by)
SELECT $1, $2, k, a, 'app', nullif($5, '')::uuid
FROM unnest($3::text[], $4::numeric[]) AS t(k, a)`

// loadwiseOverallAvgSQL prices the remaining-stock fallback: the average per-animal share across
// EVERY priced tagged animal on a CLOSED deal, each priced from its own sale line by the shared
// saleLineShareCTEs (only a closed deal is a sale, the same rule deal_share and Summary use) (farm-born sales included — a realized animal
// price is a price whatever the animal's origin). The per-deal tagged count pre-aggregates the
// many side exactly as in loadwiseSalesSQL, and the partial live-uniqueness index keeps one share
// per animal.
//
// projection-review: membership=animal_share rows with a share (closed deals, priced from their own line); group_key=none, one tenant-wide average; join_cardinality=one row per live tagged animal via the tagged partial unique index; pagination=none, whole-tenant average independent of the load window; scope=tenant_id
//
// scale-guard:ignore: god-cte -- the shared saleLineShareCTEs pricing (one set-based pass over the tenant's tagged allocations and animal lines, both indexed on tenant) plus one aggregate; bounded at the 5k-50k envelope like loadwiseSalesSQL
const loadwiseOverallAvgSQL = `
WITH ` + saleLineShareCTEs + `
SELECT COALESCE(avg(share), 0)::float8, count(*)::int
FROM animal_share
WHERE share IS NOT NULL`

// LoadwiseSales returns the newest maxLoads loads reconciled: counts, attributed sold value,
// recorded costs, the filtered load count and the overall average sold price. parkID optionally
// narrows to loads whose agree-or-go-bare farm label names that park; empty means no filter.
func (r *Repository) LoadwiseSales(ctx context.Context, tenantID, parkID string, maxLoads int) (domain.LoadwiseSales, error) {
	if maxLoads <= 0 {
		maxLoads = 60
	}
	return r.loadwiseSales(ctx, tenantID, parkID, maxLoads, biztime.BusinessDate(time.Now()))
}

// OverdueLoadCandidates returns every overdue-load candidate in the tenant, not just the newest UI
// page. The alert is specifically about OLD loads, so applying the page's newest-first LIMIT before
// filtering would hide the very rows this scan exists to find once a tenant has enough newer loads.
func (r *Repository) OverdueLoadCandidates(ctx context.Context, tenantID, asOf string) ([]domain.OverdueLoad, error) {
	if asOf == "" {
		asOf = biztime.BusinessDate(time.Now())
	}
	// No park filter: the overdue-load alert is whole-tenant by design.
	read, err := r.loadwiseSales(ctx, tenantID, "", 0, asOf)
	if err != nil {
		return nil, err
	}
	threshold, err := r.loadAgeAlertDays(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return domain.OverdueLoads(read.Loads, threshold), nil
}

// loadAgeAlertDays reads the tenant's load-age alert line from growth_assumptions (maintainer
// decision 2026-09-19). Not an aggregate: one row by primary key.
// projection-review: membership=one growth_assumptions row per (tenant_id, key); group_key=none, a
// primary-key point read; join_cardinality=none; pagination=none; scope=tenant.
// decision 2026-09-19: the figure is edited from the ADG Analytics Assumptions drawer). A tenant
// with no row gets the default the constant carried; a read error is an error, never a silent 90.
func (r *Repository) loadAgeAlertDays(ctx context.Context, tenantID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var value float64
	err := r.pool.QueryRow(ctx, `SELECT value::float8 FROM growth_assumptions WHERE tenant_id = $1::uuid AND key = 'load_age_alert_days'`, tenantID).Scan(&value)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.LoadAgeAlertDays, nil
	case err != nil:
		return 0, fmt.Errorf("procurement: load age alert days: %w", err)
	}
	return int(value), nil
}

func (r *Repository) loadwiseSales(ctx context.Context, tenantID, parkID string, maxLoads int, asOf string) (domain.LoadwiseSales, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	boundSales := sqlbind.MustBind(loadwiseSalesSQL, tenantID, maxLoads, parkID)
	rows, err := r.pool.Query(ctx, boundSales.SQL(), boundSales.Args()...)
	if err != nil {
		return domain.LoadwiseSales{}, fmt.Errorf("procurement: loadwise sales: %w", err)
	}
	defer rows.Close()

	var totalLoads int
	loads := make([]domain.LoadwiseLoad, 0, maxLoads)
	for rows.Next() {
		var (
			row            domain.LoadwiseLoad
			purchaseDate   *time.Time
			arrivedOn      *time.Time
			priorSoldFirst *time.Time
			priorSoldLast  *time.Time
			priorDeadFirst *time.Time
			priorDeadLast  *time.Time
			remainingMix   []byte
		)
		if err := rows.Scan(
			&row.LoadID, &row.LoadRef, &row.VendorName, &purchaseDate, &row.Status,
			&row.AnimalCost, &row.TransportCost, &row.OtherCost, &row.PurchaseWeightKg,
			&row.SoldWeightKg, &row.SoldWeighedAnimals, &row.SoldWeighedValue,
			&arrivedOn, &row.FatteningDays,
			&row.RowVersion,
			&row.DeclaredCount,
			&row.Purchased, &row.Sold, &row.Mortality,
			&row.OtherExits, &row.Remaining, &row.TaggedNotClosed, &row.RemainingSheep, &row.RemainingGoats, &remainingMix,
			&row.SoldValue, &row.SoldPriced,
			&row.Farm,
			&row.PriorSold.Count, &row.PriorSold.Value, &priorSoldFirst, &priorSoldLast,
			&row.PriorDead.Count, &priorDeadFirst, &priorDeadLast,
			&totalLoads,
		); err != nil {
			return domain.LoadwiseSales{}, fmt.Errorf("procurement: loadwise sales scan: %w", err)
		}
		if purchaseDate != nil {
			// A business DATE: formatted as its calendar day, never shifted through a timezone.
			row.PurchaseDate = purchaseDate.Format("2006-01-02")
		}
		// A business DATE, like purchase_date: its calendar day, never shifted through a timezone.
		row.ArrivedOn = bizDate(arrivedOn)
		row.PriorSold.FirstOn, row.PriorSold.LastOn = bizDate(priorSoldFirst), bizDate(priorSoldLast)
		row.PriorDead.FirstOn, row.PriorDead.LastOn = bizDate(priorDeadFirst), bizDate(priorDeadLast)
		if len(remainingMix) > 0 {
			if err := json.Unmarshal(remainingMix, &row.RemainingMix); err != nil {
				return domain.LoadwiseSales{}, fmt.Errorf("procurement: loadwise remaining mix: %w", err)
			}
		}
		if row.RemainingMix == nil {
			row.RemainingMix = []domain.LoadHeadMix{}
		}
		// Unaccounted is derived in the domain AFTER the prior outcomes fold in (FinalizeLoadwise).
		loads = append(loads, row)
	}
	if err := rows.Err(); err != nil {
		return domain.LoadwiseSales{}, fmt.Errorf("procurement: loadwise sales rows: %w", err)
	}

	// The itemisation for EVERY served load in ONE set-based read keyed on the ids just scanned --
	// never a query per load, which is the banned n+1 fan-out (a 60-load page would be 61 round
	// trips). The kind ordering is fixed server-side so the client renders a stable breakdown
	// without sorting business vocabulary itself.
	if err := r.attachCostLines(ctx, tenantID, loads); err != nil {
		return domain.LoadwiseSales{}, err
	}
	// The weight fact behind "assumed value by weight when we have it" (2026-09-25), one read.
	if err := r.attachStockWeight(ctx, tenantID, asOf, loads); err != nil {
		return domain.LoadwiseSales{}, err
	}

	// totalLoads rides each served row as a window count over the filtered pre-LIMIT set, so no
	// second statement runs; zero served rows honestly means zero loads match the filter.

	var (
		overallAvg  float64
		pricedCount int
	)
	boundAvg := sqlbind.MustBind(loadwiseOverallAvgSQL, tenantID)
	if err := r.pool.QueryRow(ctx, boundAvg.SQL(), boundAvg.Args()...).Scan(&overallAvg, &pricedCount); err != nil {
		return domain.LoadwiseSales{}, fmt.Errorf("procurement: loadwise overall avg: %w", err)
	}
	var overall *float64
	if pricedCount > 0 && overallAvg > 0 {
		overall = &overallAvg
	}
	// The farm's own unsold-stock price (Sales Config, migration 000367), when set: one PK read.
	var assumed *float64
	if err := r.pool.QueryRow(ctx, `SELECT unsold_stock_price_rupees::float8 FROM public.sales_valuation_assumptions WHERE tenant_id = $1::uuid`, tenantID).Scan(&assumed); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.LoadwiseSales{}, fmt.Errorf("procurement: loadwise unsold price assumption: %w", err)
	}
	return domain.FinalizeLoadwise(loads, totalLoads, overall, asOf, assumed), nil
}

// bizDate renders an optional business DATE as its calendar day, never shifted through a timezone.
func bizDate(v *time.Time) string {
	if v == nil {
		return ""
	}
	return v.Format("2006-01-02")
}

// SetLoadCost records (or clears) one load's landed cost.
//
// The row lock covers the whole edit. Naturally idempotent — writing the values the load already
// carries changes nothing and audits nothing — so no idempotency reservation is needed, the same
// contract UpdateFeedPurchase keeps for the same PUT-shaped write.
func (r *Repository) SetLoadCost(ctx context.Context, tenantID, loadID string, edit domain.LoadCostEdit, actorID string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("procurement: begin load cost: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentAnimal, currentTransport, currentOther *float64
	err = tx.QueryRow(ctx, `
SELECT animal_cost::float8, transport_cost::float8, other_cost::float8
FROM public.procurement_loads
WHERE tenant_id = $1 AND load_id = $2
FOR UPDATE`, tenantID, loadID).Scan(&currentAnimal, &currentTransport, &currentOther)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrLoadNotFound
	}
	if err != nil {
		return fmt.Errorf("procurement: lock load cost: %w", err)
	}

	unchanged := eqMoney(currentAnimal, edit.AnimalCost) &&
		eqMoney(currentTransport, edit.TransportCost) &&
		eqMoney(currentOther, edit.OtherCost)
	if unchanged {
		return tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, `
UPDATE public.procurement_loads
SET animal_cost = $3, transport_cost = $4, other_cost = $5,
    cost_recorded_by = CASE WHEN $3::numeric IS NULL THEN NULL ELSE nullif($6, '')::uuid END,
    cost_recorded_at = CASE WHEN $3::numeric IS NULL THEN NULL ELSE now() END,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1 AND load_id = $2`,
		tenantID, loadID, edit.AnimalCost, edit.TransportCost, edit.OtherCost, actorID); err != nil {
		return fmt.Errorf("procurement: update load cost: %w", err)
	}

	// THE ITEMISATION FOLLOWS THE FIGURE, BUCKET BY BUCKET (maintainer decision 2026-09-25: keep
	// the items unless 'other' changed). The drawer states three bucket totals. A bucket whose
	// submitted total equals the sum of its current lines is left exactly as it is -- so correcting
	// the transport of a sheet-imported load keeps its booking / labour / transit / feed lines. A
	// bucket whose total CHANGED has its lines replaced by one line of that bucket's own kind, so
	// the breakdown a reader opens never claims a split that does not add up to the figure beside
	// it: the person typing the total is stating the total, and a stale itemisation under a new
	// total would be a lie the screen tells confidently. For 'other' that means the itemised lines
	// collapse into one only when the other total itself was changed.
	//
	// Clearing the cost (nil animal cost) removes every line, matching the columns going NULL.
	current, err := loadCostBucketSums(ctx, tx, tenantID, loadID)
	if err != nil {
		return err
	}
	rewrite := make([]string, 0, 3)
	kinds := make([]string, 0, 3)
	amounts := make([]float64, 0, 3)
	for _, part := range []struct {
		kind   string
		amount *float64
	}{
		{domain.CostKindAnimal, edit.AnimalCost},
		{domain.CostKindTransport, edit.TransportCost},
		{domain.CostKindOther, edit.OtherCost},
	} {
		amount := part.amount
		if edit.AnimalCost == nil {
			amount = nil
		}
		if eqMoney(current[part.kind], amount) {
			continue
		}
		rewrite = append(rewrite, part.kind)
		if amount != nil {
			kinds = append(kinds, part.kind)
			amounts = append(amounts, *amount)
		}
	}
	// ONE set-based delete and ONE set-based insert over the changed buckets, never an Exec per
	// bucket -- the shape is what gets copied into the next loop.
	if len(rewrite) > 0 {
		if _, err := tx.Exec(ctx, clearLoadCostBucketsSQL, tenantID, loadID, rewrite); err != nil {
			return fmt.Errorf("procurement: clear changed load cost lines: %w", err)
		}
	}
	if len(kinds) > 0 {
		if _, err := tx.Exec(ctx, insertLoadCostLinesSQL,
			tenantID, loadID, kinds, amounts, actorID); err != nil {
			return fmt.Errorf("procurement: write load cost lines: %w", err)
		}
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "procurement.load_cost.set",
		ResourceType: "procurement_load",
		ResourceID:   loadID,
		Metadata: map[string]any{
			"domain":                  "procurement",
			"module":                  "loads",
			"category":                "cost",
			"previous_animal_cost":    currentAnimal,
			"previous_transport_cost": currentTransport,
			"previous_other_cost":     currentOther,
			"animal_cost":             edit.AnimalCost,
			"transport_cost":          edit.TransportCost,
			"other_cost":              edit.OtherCost,
		},
	}); err != nil {
		return fmt.Errorf("procurement: audit load cost: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("procurement: commit load cost: %w", err)
	}
	return nil
}

var _ ports.LoadwiseRepository = (*Repository)(nil)
