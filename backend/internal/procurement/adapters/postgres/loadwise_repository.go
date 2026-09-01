package postgres

import (
	"context"
	"errors"
	"fmt"
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
// projection-review: membership=procurement_load_goats accepted rows deduped DISTINCT ON (tenant, goat) by the TOTAL order (intake_accepted_at DESC NULLS LAST, created_at DESC, load_goat_id DESC) so a tie or null instant still resolves to one stable load; group_key=load_id on both sides (stats GROUP BY m.load_id attaching 1:1 to procurement_loads PK); join_cardinality=goats 1:1 on PK, deal_share at most 1:1 via the tagged partial unique index with the per-deal tagged count pre-aggregated, park lateral 1:1 collapsed agree-or-go-bare; pagination=LIMIT $2 newest loads with whole-tenant total_loads reported beside the window; scope=tenant_id on every branch
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
// remaining) FILTERs the SAME member-x-goats row set, so purchased = sold + mortality + other +
// remaining + unaccounted holds by construction; unaccounted is derived in the domain from these
// five, never counted separately. The read serves the newest $2 loads while summary totals are
// derived in the domain from exactly the served rows (the section's stated scope), and
// total_loads is the whole-tenant count so the screen can say when older loads are not shown.
//
// scale-guard:ignore: bounded reporting read over the procurement pipeline's own tables — member
// count grows with procured animals (the 5k–50k envelope), aggregated once per request in one
// set-based statement over indexed tenant scans (procurement_load_goats_goat_state_idx, goats PK,
// the goat_sale_allocations live-uniqueness index), and the row set served is LIMITed. Same shape
// and reasoning as the sales overview read.
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
deal_share AS (
    SELECT a.goat_id,
           CASE WHEN d.sales_value > 0 THEN d.sales_value / cnt.tagged END AS share
    FROM public.goat_sale_allocations a
    JOIN public.sales_deals d
      ON d.tenant_id = a.tenant_id AND d.id = a.sales_deal_id
    JOIN (
        SELECT tenant_id, sales_deal_id, count(*)::numeric AS tagged
        FROM public.goat_sale_allocations
        WHERE tenant_id = $1 AND status = 'tagged'
        GROUP BY tenant_id, sales_deal_id
    ) cnt ON cnt.tenant_id = a.tenant_id AND cnt.sales_deal_id = a.sales_deal_id
    WHERE a.tenant_id = $1 AND a.status = 'tagged'
),
outcomes AS (
    -- One DISJOINT outcome bucket per member animal, so the five counts partition purchased by
    -- construction. Sold and dead are checked on lifecycle OR exit_reason (belt and braces for
    -- seeded history where only one side is stamped); the live set is the alive + clinical states
    -- (an animal in ICU is still on farm); merged/inactive and anything unclassifiable fall to
    -- 'unaccounted', which the domain derives as the arithmetic gap over these same rows.
    SELECT m.load_id,
           ds.share,
           gp.park_code,
           CASE
               WHEN g.lifecycle_status = 'sold' OR g.exit_reason = 'sold' THEN 'sold'
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
-- stats 1:1 by construction (GROUP BY m.load_id), prior 1:1 (GROUP BY load_id), and the 2026-09-01
-- cost columns are PLAIN COLUMNS of procurement_loads -- 1:1 with the row by definition, adding no
-- join and no fan-out; pagination=LIMIT $2 newest loads, whole-tenant total_loads reported beside
-- the window so the count never means "of this page"; scope=tenant_id on the outer WHERE and on
-- every CTE.
--
-- The ITEMISATION is deliberately NOT joined here. Cost lines are 1:N against a load, so joining
-- them into this statement would multiply every row -- purchased counts, sold value, the lot --
-- which is the classic fan-out this marker exists to refuse. They are fetched by
-- loadCostLinesSQL as a separate set-based read and attached in Go by load_id, so a load with
-- five cost lines is still exactly one row here.
SELECT pl.load_id::text, COALESCE(pl.context->>'load_ref', ''), COALESCE(p.display_name, ''), pl.purchase_date, pl.status,
       pl.animal_cost::float8, pl.transport_cost::float8, pl.other_cost::float8,
       pl.purchase_weight_kg::float8,
       pl.sold_weight_kg::float8, pl.sold_weighed_animals, pl.sold_weighed_value::float8,
       pl.arrived_on, pl.fattening_days,
       pl.row_version,
       pl.expected_count,
       COALESCE(s.purchased, 0), COALESCE(s.sold, 0), COALESCE(s.mortality, 0),
       COALESCE(s.other_exits, 0), COALESCE(s.remaining, 0),
       COALESCE(s.sold_value, 0), COALESCE(s.sold_priced, 0),
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
       END,
       COALESCE(pr.prior_sold, 0), pr.prior_sold_value, pr.prior_sold_first, pr.prior_sold_last,
       COALESCE(pr.prior_dead, 0), pr.prior_dead_first, pr.prior_dead_last
FROM public.procurement_loads pl
LEFT JOIN public.parties p ON p.party_id = pl.source_party_id
LEFT JOIN stats s ON s.load_id = pl.load_id
LEFT JOIN prior pr ON pr.load_id = pl.load_id
WHERE pl.tenant_id = $1
ORDER BY pl.purchase_date DESC NULLS LAST, pl.created_at DESC, pl.load_id
LIMIT $2`

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
// Purchase & barn down.
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
		loads[i].CostLines = byLoad[loads[i].LoadID]
	}
	return nil
}

// clearLoadCostLinesSQL and insertLoadCostLinesSQL are the write half of the cost itemisation. A
// hand edit through the cost drawer states BUCKET TOTALS, so it replaces this load's lines with one
// line per bucket it names -- the breakdown a reader opens can then never claim a split that does
// not add up to the figure beside it.
const clearLoadCostLinesSQL = `
DELETE FROM public.procurement_load_cost_lines
WHERE tenant_id = $1 AND load_id = $2`

// One set-based insert over the named buckets, never an Exec per bucket.
const insertLoadCostLinesSQL = `
INSERT INTO public.procurement_load_cost_lines (tenant_id, load_id, kind, amount, source, recorded_by)
SELECT $1, $2, k, a, 'app', nullif($5, '')::uuid
FROM unnest($3::text[], $4::numeric[]) AS t(k, a)`

// loadwiseOverallAvgSQL prices the remaining-stock fallback: the average per-animal share across
// EVERY tagged allocation on a positive-value deal (farm-born sales included — a realized animal
// price is a price whatever the animal's origin). The per-deal tagged count pre-aggregates the
// many side exactly as in loadwiseSalesSQL, and the partial live-uniqueness index keeps one share
// per animal.
const loadwiseOverallAvgSQL = `
SELECT COALESCE(avg(d.sales_value / cnt.tagged), 0)::float8, count(*)::int
FROM public.goat_sale_allocations a
JOIN public.sales_deals d
  ON d.tenant_id = a.tenant_id AND d.id = a.sales_deal_id AND d.sales_value > 0
JOIN (
    SELECT tenant_id, sales_deal_id, count(*)::numeric AS tagged
    FROM public.goat_sale_allocations
    WHERE tenant_id = $1 AND status = 'tagged'
    GROUP BY tenant_id, sales_deal_id
) cnt ON cnt.tenant_id = a.tenant_id AND cnt.sales_deal_id = a.sales_deal_id
WHERE a.tenant_id = $1 AND a.status = 'tagged'`

// LoadwiseSales returns the newest maxLoads loads reconciled: counts, attributed sold value,
// recorded costs, the whole-tenant load count and the overall average sold price.
func (r *Repository) LoadwiseSales(ctx context.Context, tenantID string, maxLoads int) (domain.LoadwiseSales, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if maxLoads <= 0 {
		maxLoads = 60
	}

	rows, err := r.pool.Query(ctx, loadwiseSalesSQL, tenantID, maxLoads)
	if err != nil {
		return domain.LoadwiseSales{}, fmt.Errorf("procurement: loadwise sales: %w", err)
	}
	defer rows.Close()

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
		)
		if err := rows.Scan(
			&row.LoadID, &row.LoadRef, &row.VendorName, &purchaseDate, &row.Status,
			&row.AnimalCost, &row.TransportCost, &row.OtherCost, &row.PurchaseWeightKg,
			&row.SoldWeightKg, &row.SoldWeighedAnimals, &row.SoldWeighedValue,
			&arrivedOn, &row.FatteningDays,
			&row.RowVersion,
			&row.DeclaredCount,
			&row.Purchased, &row.Sold, &row.Mortality,
			&row.OtherExits, &row.Remaining,
			&row.SoldValue, &row.SoldPriced,
			&row.Farm,
			&row.PriorSold.Count, &row.PriorSold.Value, &priorSoldFirst, &priorSoldLast,
			&row.PriorDead.Count, &priorDeadFirst, &priorDeadLast,
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

	var totalLoads int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM public.procurement_loads WHERE tenant_id = $1`, tenantID,
	).Scan(&totalLoads); err != nil {
		return domain.LoadwiseSales{}, fmt.Errorf("procurement: loadwise sales total: %w", err)
	}

	var (
		overallAvg  float64
		pricedCount int
	)
	if err := r.pool.QueryRow(ctx, loadwiseOverallAvgSQL, tenantID).Scan(&overallAvg, &pricedCount); err != nil {
		return domain.LoadwiseSales{}, fmt.Errorf("procurement: loadwise overall avg: %w", err)
	}
	var overall *float64
	if pricedCount > 0 && overallAvg > 0 {
		overall = &overallAvg
	}

	// The age clock reads against TODAY's Asia/Kolkata business date. A load is bought on a day,
	// so its age is a whole number of business days -- never a UTC instant, which would flip the
	// figure for five and a half hours every night.
	return domain.FinalizeLoadwise(loads, totalLoads, overall, biztime.BusinessDate(time.Now())), nil
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

	// THE ITEMISATION FOLLOWS THE FIGURE. A hand edit through the cost drawer replaces the edited
	// buckets' lines with ONE line of that bucket's own kind, so the breakdown a reader opens can
	// never claim a split that does not add up to the number beside it. This is the price of
	// keeping both -- and it is the right way round: the person typing the total is stating the
	// total, and a stale "transport 43,000 / labour 8,000" under a new total of 20,000 would be a
	// lie the screen tells confidently.
	//
	// Only the buckets actually written are collapsed. Clearing the cost (nil animal cost) removes
	// every line, matching the columns going NULL.
	if _, err := tx.Exec(ctx, clearLoadCostLinesSQL, tenantID, loadID); err != nil {
		return fmt.Errorf("procurement: clear load cost lines: %w", err)
	}
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
		if part.amount == nil {
			continue
		}
		kinds = append(kinds, part.kind)
		amounts = append(amounts, *part.amount)
	}
	// ONE set-based insert over the collected buckets, never an Exec per bucket. Three round trips
	// would be harmless at this size, but the rule has no size exception -- the shape is what gets
	// copied into the next loop, which is over rows rather than three fixed fields.
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
