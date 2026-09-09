package postgres

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"golang.org/x/sync/errgroup"
)

// GetOverview serves the whole sales page in one read: closed-deal aggregates computed in Go over
// ONE bounded ledger read, plus single-table GROUP BY rollups for the pipeline and evidence
// panels. Every number is a WHOLE-FILTER aggregate; nothing here is page-local.
//
// projection-review: membership=each block reads exactly ONE sales_* table at ROW grain (one
// sheet row / one recorded fact -- source_sales_id is a repeating sheet reference and is only ever
// counted DISTINCT, never grouped by); group_key=month, (product_type, breed) and buyer_name for
// the closed-deal blocks (built in domain.BuildDealAggregates from one bounded read sharing the
// ledger's WHERE), normalized call_status / buyer_place / district / animal_label for the
// single-table GROUP BY rollups; join_cardinality=no query here joins anything, so fan-out is
// structurally impossible, and the realized_price_per_kg ratio has numerator and denominator
// ranging over the identical closed-live-weighed deal set; pagination=none -- every block is a
// whole-filter aggregate over a bounded authored table and cannot change with any page size;
// scope=tenant_id on every query, plus the farm predicate on the tables that carry a farm (deals,
// buyer leads, sold tags) while fpo/audit/benchmarks stay deliberately company-wide.
//
// The farm filter applies to the tables that carry a farm -- deals, buyer leads, sold tags. The
// FPO pipeline, the weight audit and the market benchmarks are company-wide because their source
// carries no farm column, and pretending to filter them would show the same numbers under two
// different labels.
func (r *Repository) GetOverview(ctx context.Context, tenantID, farm string) (domain.Overview, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var (
		overview         domain.Overview
		closed           []domain.Deal
		buyerPipeline    domain.BuyerPipeline
		fpoPipeline      domain.FPOPipeline
		tagRoster        domain.TagRoster
		weightAudit      domain.WeightAuditSummary
		marketBenchmarks []domain.MarketBenchmark
		soldWeightBands  domain.SoldWeightBands
		farmValuation    domain.FarmValuation
	)

	group, gctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		var err error
		closed, err = r.closedDeals(gctx, tenantID, farm)
		return err
	})
	group.Go(func() error {
		var err error
		buyerPipeline, err = r.buyerPipeline(gctx, tenantID, farm)
		return err
	})
	group.Go(func() error {
		var err error
		fpoPipeline, err = r.fpoPipeline(gctx, tenantID)
		return err
	})
	group.Go(func() error {
		var err error
		tagRoster, err = r.tagRoster(gctx, tenantID, farm)
		return err
	})
	group.Go(func() error {
		var err error
		weightAudit, err = r.weightAudit(gctx, tenantID)
		return err
	})
	group.Go(func() error {
		var err error
		marketBenchmarks, err = r.marketBenchmarks(gctx, tenantID)
		return err
	})
	group.Go(func() error {
		var err error
		soldWeightBands, err = r.soldWeightBands(gctx, tenantID, farm)
		return err
	})
	group.Go(func() error {
		var err error
		farmValuation, err = r.farmValuation(gctx, tenantID, farm)
		return err
	})
	if err := group.Wait(); err != nil {
		return domain.Overview{}, err
	}
	overview.Summary, overview.Monthly, overview.PriceBands, overview.Buyers = domain.BuildDealAggregates(closed)
	overview.BuyerPipeline = buyerPipeline
	overview.FPOPipeline = fpoPipeline
	overview.TagRoster = tagRoster
	overview.WeightAudit = weightAudit
	overview.MarketBenchmarks = marketBenchmarks
	overview.SoldWeightBands = soldWeightBands
	overview.FarmValuation = farmValuation
	return overview, nil
}

// soldWeightBands counts every LIVE sale allocation (status 'tagged') by the weight recorded at
// tagging, in ONE grouped read; the bands themselves are decided in domain.SoldWeightBands so
// the edges live in exactly one place.
//
// projection-review: membership=goat_sale_allocations at (tenant_id, goat_id, sales_deal_id) grain, one
// row per animal per sale, filtered to status='tagged' where the partial unique index
// (tenant_id, goat_id) WHERE status='tagged' makes each sold animal appear exactly once;
// group_key=a.weight_kg alone, each (weight, count) group folded by domain.SoldWeightBands into one
// of four disjoint bands or the unweighed remainder, so band counts and Total range over the
// identical key set of tagged allocation rows and bands + unweighed == total by construction;
// join_cardinality=sales_deals joined 1:1 on (tenant_id, id) from (a.tenant_id, a.sales_deal_id)
// purely for the farm predicate -- every allocation names exactly one deal, so the join can neither
// fan out nor drop a row; pagination=none, whole-filter aggregate, the Sales page has no window;
// scope=tenant, optionally narrowed to one farm through the deal's farm, the same buildDealFilter the
// rest of the page uses.
func (r *Repository) soldWeightBands(ctx context.Context, tenantID, farm string) (domain.SoldWeightBands, error) {
	where, args := buildDealFilter(tenantID, farm)
	query := fmt.Sprintf(soldWeightBandsSQL, where)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return domain.SoldWeightBands{}, fmt.Errorf("sales sold weight bands: %w", err)
	}
	defer rows.Close()

	bands := domain.SoldWeightBands{}
	for rows.Next() {
		var kg *float64
		var n int
		if err := rows.Scan(&kg, &n); err != nil {
			return domain.SoldWeightBands{}, fmt.Errorf("sales sold weight bands scan: %w", err)
		}
		for i := 0; i < n; i++ {
			bands.AddSoldWeight(kg)
		}
	}
	if err := rows.Err(); err != nil {
		return domain.SoldWeightBands{}, fmt.Errorf("sales sold weight bands rows: %w", err)
	}
	return bands, nil
}

// farmValuation computes Manju's Sales farm-value cards from current live herd inventory, not the
// closed sales ledger. F2/F2-Male/F2-Female are the current fattening vocabulary; their formula
// uses the average of the latest verified RFID-linked weights and applies it to the bucket count.
//
// projection-review: membership=goats at one row per current animal, filtered to non-terminal and
// non-merged; latest_purpose is distinct on (tenant_id, goat_id), idmap is reduced by latest_weight
// back to one row per goat before joining, so RFID aliases cannot fan out animals; group_key=the
// mutually-exclusive CASE bucket; farm scope joins locations 1:1 by goat park_id/farm_id and matches
// the same CBE/CPT code the Sales filter carries; pagination=none, this is a whole-current-inventory card;
// scope=tenant_id and optional farm code.
func (r *Repository) farmValuation(ctx context.Context, tenantID, farm string) (domain.FarmValuation, error) {
	args := []any{tenantID}
	farmPredicate := ""
	if farm != "" {
		args = append(args, farm)
		farmPredicate = "AND upper(coalesce(park.location_code, farm.location_code, '')) = upper($2)"
	}

	query := fmt.Sprintf(farmValuationSQL, farmPredicate)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return domain.FarmValuation{}, fmt.Errorf("sales farm valuation: %w", err)
	}
	defer rows.Close()

	out := domain.FarmValuation{Buckets: []domain.FarmValuationBucket{}}
	for rows.Next() {
		var bucket domain.FarmValuationBucket
		var totalAnimals int
		if err := rows.Scan(
			&bucket.Bucket,
			&bucket.Label,
			&bucket.AnimalCount,
			&bucket.WeightKg,
			&bucket.PricePerKg,
			&bucket.MeatKg,
			&bucket.ValueRupees,
			&bucket.ActualWeight,
			&bucket.WeighedAnimals,
			&totalAnimals,
		); err != nil {
			return domain.FarmValuation{}, fmt.Errorf("sales farm valuation scan: %w", err)
		}
		out.TotalMeatKg += bucket.MeatKg
		out.TotalValueRupees += bucket.ValueRupees
		out.TotalAnimals = totalAnimals
		out.Buckets = append(out.Buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return domain.FarmValuation{}, fmt.Errorf("sales farm valuation rows: %w", err)
	}
	return out, nil
}

// closedDeals loads every closed deal in the filter -- the ONE bounded read behind the summary,
// monthly, price-band and buyer blocks, so the four blocks cannot range over different predicates.
//
// projection-review: producer rows are sales_deals at ROW grain; this read adds exactly one
// predicate (status = 'Deal Closed') to the shared farm filter and does no join, so no fan-out.
// The grouped consumers live in domain.BuildDealAggregates, whose own projection-review note
// names the group keys and the realized-price ratio's shared key set.
func (r *Repository) closedDeals(ctx context.Context, tenantID, farm string) ([]domain.Deal, error) {
	where, args := buildDealFilter(tenantID, farm)
	// scale-guard:ignore: whole-filter read of an authored commercial ledger (63 sheet rows today,
	// grows by deals closed, never with herd size); the page contract is whole-filter aggregates,
	// which cannot be computed from a page.
	query := fmt.Sprintf(`SELECT %s FROM public.sales_deals d WHERE %s AND d.status = 'Deal Closed' ORDER BY d.sale_date, d.id`, dealColumns, where)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sales overview deals: %w", err)
	}
	defer rows.Close()

	deals := make([]domain.Deal, 0, 128)
	for rows.Next() {
		d, err := scanDeal(rows)
		if err != nil {
			return nil, fmt.Errorf("sales overview deals scan: %w", err)
		}
		deals = append(deals, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sales overview deals rows: %w", err)
	}
	return deals, nil
}

// buyerLeadFilter is the shared predicate for the buyer-pipeline rollups, so the status buckets
// and the top-places list range over the same lead set.
func buyerLeadFilter(tenantID, farm string) (string, []any) {
	if farm == "" {
		return "tenant_id = $1", []any{tenantID}
	}
	return "tenant_id = $1 AND farm = $2", []any{tenantID, farm}
}

// buyerPipeline rolls up the buyer-lead demand pipeline.
//
// projection-review: producer rows are sales_buyer_leads at ROW grain (one lead per sheet row).
// The status rollup groups by the normalized call_status and the places rollup by buyer_place,
// each a single-table GROUP BY over the same WHERE -- no join, no fan-out. Total is the sum of
// the status buckets, which are exhaustive because NULL/blank normalizes to 'uncontacted' rather
// than being dropped.
func (r *Repository) buyerPipeline(ctx context.Context, tenantID, farm string) (domain.BuyerPipeline, error) {
	where, args := buyerLeadFilter(tenantID, farm)
	pipeline := domain.BuyerPipeline{Statuses: []domain.StatusCount{}, TopPlaces: []domain.PlaceCount{}}

	query := fmt.Sprintf(`
		SELECT COALESCE(nullif(btrim(call_status), ''), '%s') AS status, count(*)
		FROM public.sales_buyer_leads
		WHERE %s
		GROUP BY 1
		ORDER BY count(*) DESC, status`, domain.UncontactedStatusKey, where)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return domain.BuyerPipeline{}, fmt.Errorf("sales buyer pipeline statuses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket domain.StatusCount
		if err := rows.Scan(&bucket.Status, &bucket.Count); err != nil {
			return domain.BuyerPipeline{}, fmt.Errorf("sales buyer pipeline statuses scan: %w", err)
		}
		pipeline.Statuses = append(pipeline.Statuses, bucket)
		pipeline.Total += bucket.Count
	}
	if err := rows.Err(); err != nil {
		return domain.BuyerPipeline{}, fmt.Errorf("sales buyer pipeline statuses rows: %w", err)
	}

	placesQuery := fmt.Sprintf(`
		SELECT btrim(buyer_place) AS place, count(*)
		FROM public.sales_buyer_leads
		WHERE %s AND btrim(coalesce(buyer_place, '')) <> ''
		GROUP BY 1
		ORDER BY count(*) DESC, place
		LIMIT %d`, where, domain.MaxPipelinePlaces)
	pipeline.TopPlaces, err = r.placeCounts(ctx, placesQuery, args, "sales buyer pipeline places")
	if err != nil {
		return domain.BuyerPipeline{}, err
	}
	return pipeline, nil
}

// fpoPipeline rolls up the FPO demand pipeline. Company-wide by construction: the source carries
// no farm column.
//
// projection-review: producer rows are sales_fpo_leads at ROW grain; status and district rollups
// are single-table GROUP BYs over the same tenant predicate -- no join, no fan-out. Total sums
// the exhaustive status buckets (NULL/blank -> 'uncontacted').
func (r *Repository) fpoPipeline(ctx context.Context, tenantID string) (domain.FPOPipeline, error) {
	pipeline := domain.FPOPipeline{Statuses: []domain.StatusCount{}, Districts: []domain.PlaceCount{}}

	query := fmt.Sprintf(`
		SELECT COALESCE(nullif(btrim(call_status), ''), '%s') AS status, count(*)
		FROM public.sales_fpo_leads
		WHERE tenant_id = $1
		GROUP BY 1
		ORDER BY count(*) DESC, status`, domain.UncontactedStatusKey)
	rows, err := r.pool.Query(ctx, query, tenantID)
	if err != nil {
		return domain.FPOPipeline{}, fmt.Errorf("sales fpo pipeline statuses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket domain.StatusCount
		if err := rows.Scan(&bucket.Status, &bucket.Count); err != nil {
			return domain.FPOPipeline{}, fmt.Errorf("sales fpo pipeline statuses scan: %w", err)
		}
		pipeline.Statuses = append(pipeline.Statuses, bucket)
		pipeline.Total += bucket.Count
	}
	if err := rows.Err(); err != nil {
		return domain.FPOPipeline{}, fmt.Errorf("sales fpo pipeline statuses rows: %w", err)
	}

	districtsQuery := fmt.Sprintf(`
		SELECT btrim(district) AS district, count(*)
		FROM public.sales_fpo_leads
		WHERE tenant_id = $1 AND btrim(coalesce(district, '')) <> ''
		GROUP BY 1
		ORDER BY count(*) DESC, district
		LIMIT %d`, domain.MaxPipelinePlaces)
	pipeline.Districts, err = r.placeCounts(ctx, districtsQuery, []any{tenantID}, "sales fpo pipeline districts")
	if err != nil {
		return domain.FPOPipeline{}, err
	}
	return pipeline, nil
}

// placeCounts runs one (place, count) rollup query.
func (r *Repository) placeCounts(ctx context.Context, query string, args []any, label string) ([]domain.PlaceCount, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	defer rows.Close()
	out := []domain.PlaceCount{}
	for rows.Next() {
		var place domain.PlaceCount
		if err := rows.Scan(&place.Place, &place.Count); err != nil {
			return nil, fmt.Errorf("%s scan: %w", label, err)
		}
		out = append(out, place)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s rows: %w", label, err)
	}
	return out, nil
}

// tagRoster summarises the sold-animal tag evidence.
//
// projection-review: producer rows are sales_sold_animal_tags at ROW grain (one tag per animal
// handed over). total counts rows; sales_count counts DISTINCT source_sales_id because that
// column is a sheet REFERENCE that repeats across the tags of one sale and must never be counted
// at row grain; by_type groups by animal_label over the same WHERE. Single table, no join.
func (r *Repository) tagRoster(ctx context.Context, tenantID, farm string) (domain.TagRoster, error) {
	where, args := buyerLeadFilter(tenantID, farm) // same tenant[/farm] predicate shape
	roster := domain.TagRoster{ByType: []domain.TagTypeCount{}}

	totalsQuery := fmt.Sprintf(`
		SELECT count(*), count(DISTINCT source_sales_id)
		FROM public.sales_sold_animal_tags
		WHERE %s`, where)
	if err := r.pool.QueryRow(ctx, totalsQuery, args...).Scan(&roster.Total, &roster.SalesCount); err != nil {
		return domain.TagRoster{}, fmt.Errorf("sales tag roster totals: %w", err)
	}

	byTypeQuery := fmt.Sprintf(`
		SELECT animal_label, count(*)
		FROM public.sales_sold_animal_tags
		WHERE %s
		GROUP BY animal_label
		ORDER BY count(*) DESC, animal_label`, where)
	rows, err := r.pool.Query(ctx, byTypeQuery, args...)
	if err != nil {
		return domain.TagRoster{}, fmt.Errorf("sales tag roster by type: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket domain.TagTypeCount
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return domain.TagRoster{}, fmt.Errorf("sales tag roster by type scan: %w", err)
		}
		roster.ByType = append(roster.ByType, bucket)
	}
	if err := rows.Err(); err != nil {
		return domain.TagRoster{}, fmt.Errorf("sales tag roster by type rows: %w", err)
	}
	return roster, nil
}

// weightAudit buckets every audit row's video-vs-book gap in Go, so the bucket boundaries live in
// ONE tested place (domain.WeightAuditSummary.BucketWeightGap) rather than being re-derived in SQL.
//
// projection-review: producer rows are sales_weight_audit at ROW grain (one audited animal); the
// consumer buckets each row exactly once into three disjoint gap ranges. Single bounded table
// (81 rows), tenant-scoped, no join.
func (r *Repository) weightAudit(ctx context.Context, tenantID string) (domain.WeightAuditSummary, error) {
	// scale-guard:ignore: whole-filter read of a bounded authored evidence table (81 rows), whose
	// disjoint gap buckets are a whole-filter aggregate.
	rows, err := r.pool.Query(ctx, `
		SELECT video_weight_kg, book_weight_kg
		FROM public.sales_weight_audit
		WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return domain.WeightAuditSummary{}, fmt.Errorf("sales weight audit: %w", err)
	}
	defer rows.Close()

	summary := domain.WeightAuditSummary{}
	for rows.Next() {
		var videoKg, bookKg float64
		if err := rows.Scan(&videoKg, &bookKg); err != nil {
			return domain.WeightAuditSummary{}, fmt.Errorf("sales weight audit scan: %w", err)
		}
		summary.BucketWeightGap(videoKg, bookKg)
	}
	if err := rows.Err(); err != nil {
		return domain.WeightAuditSummary{}, fmt.Errorf("sales weight audit rows: %w", err)
	}
	return summary, nil
}

// marketBenchmarks lists the comparable market quotes, priciest first so the screen reads as a
// ladder.
func (r *Repository) marketBenchmarks(ctx context.Context, tenantID string) ([]domain.MarketBenchmark, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT market, category, breed, source, ex_farm_rate, transport_rate, landing_cost_per_kg, market_price_per_kg
		FROM public.sales_market_benchmarks
		WHERE tenant_id = $1
		ORDER BY market_price_per_kg DESC NULLS LAST, breed`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sales market benchmarks: %w", err)
	}
	defer rows.Close()

	out := []domain.MarketBenchmark{}
	for rows.Next() {
		var b domain.MarketBenchmark
		if err := rows.Scan(&b.Market, &b.Category, &b.Breed, &b.Source, &b.ExFarmRate, &b.TransportRate, &b.LandingCostPerKg, &b.MarketPricePerKg); err != nil {
			return nil, fmt.Errorf("sales market benchmarks scan: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sales market benchmarks rows: %w", err)
	}
	return out, nil
}

// soldWeightBandsSQL is the one grouped read behind the Sales page's weight bands. %s is the
// shared deal filter (tenant, optional farm) so the bands range over the same deals as the rest
// of the page.
//
// projection-review: membership=goat_sale_allocations at (tenant_id, goat_id, sales_deal_id) grain, one
// row per animal per sale, filtered to status='tagged' where the partial unique index
// (tenant_id, goat_id) WHERE status='tagged' makes each sold animal appear exactly once;
// group_key=a.weight_kg alone, each (weight, count) group folded by domain.SoldWeightBands into one
// of four disjoint bands or the unweighed remainder, so band counts and Total range over the
// identical key set of tagged allocation rows and bands + unweighed == total by construction;
// join_cardinality=sales_deals joined 1:1 on (tenant_id, id) from (a.tenant_id, a.sales_deal_id)
// purely for the farm predicate -- every allocation names exactly one deal, so the join can neither
// fan out nor drop a row; pagination=none, whole-filter aggregate, the Sales page has no window;
// scope=tenant, optionally narrowed to one farm through the deal's farm, the same buildDealFilter the
// rest of the page uses.
//
// scale-guard:ignore: grouped read over the sale-allocation register (one row per animal SOLD,
// hundreds today, grows with sales rather than herd size) keyed by the indexed
// (tenant_id, sales_deal_id); a whole-register count cannot be served from a page.
const soldWeightBandsSQL = `
		SELECT a.weight_kg::float8, count(*)
		FROM public.goat_sale_allocations a
		JOIN public.sales_deals d ON d.id = a.sales_deal_id AND d.tenant_id = a.tenant_id
		WHERE %s AND a.tenant_id = $1 AND a.status = 'tagged'
		GROUP BY a.weight_kg`

// farmValuationSQL is the one live-inventory rollup behind the Farm Value cards. %s is the optional
// farm-code predicate.
const farmValuationSQL = `
	WITH latest_purpose AS (
		SELECT DISTINCT ON (tenant_id, goat_id)
			tenant_id, goat_id, purpose
		FROM public.procurement_load_goats
		WHERE tenant_id = $1
		ORDER BY tenant_id, goat_id, updated_at DESC NULLS LAST
	),
	idmap AS (
		SELECT tenant_id, goat_id, animal_identifier_1 AS identifier
		FROM public.procurement_load_goats
		WHERE tenant_id = $1 AND btrim(coalesce(animal_identifier_1, '')) <> ''
		UNION ALL
		SELECT tenant_id, goat_id, animal_identifier_2 AS identifier
		FROM public.procurement_load_goats
		WHERE tenant_id = $1 AND btrim(coalesce(animal_identifier_2, '')) <> ''
	),
	latest_weight AS (
		SELECT DISTINCT ON (tenant_id, scanned_identifier)
			tenant_id, scanned_identifier, weight_kg::float8 AS weight_kg, accepted_at
		FROM public.weighing_observations
		WHERE tenant_id = $1 AND verification_status = 'verified'
		ORDER BY tenant_id, scanned_identifier, accepted_at DESC
	),
	goat_weight AS (
		SELECT DISTINCT ON (i.tenant_id, i.goat_id)
			i.tenant_id, i.goat_id, w.weight_kg
		FROM idmap i
		JOIN latest_weight w ON w.tenant_id = i.tenant_id AND w.scanned_identifier = i.identifier
		ORDER BY i.tenant_id, i.goat_id, w.accepted_at DESC
	),
	classified AS (
		SELECT
			CASE
				WHEN coalesce(lp.purpose, '') = 'fattening' OR g.management_stage IN ('F2', 'F2-Male', 'F2-Female') THEN 'fattening'
				WHEN g.age_band = 'adult' AND g.sex = 'female' THEN 'adult_female'
				WHEN g.age_band = 'adult' AND g.sex = 'male' THEN 'adult_male_buck'
				WHEN g.milk_cohort = 'K1' OR g.management_stage = 'K1' THEN 'K1'
				WHEN g.milk_cohort = 'K2' OR g.management_stage = 'K2' THEN 'K2'
				WHEN g.milk_cohort = 'K3' OR g.management_stage = 'K3' THEN 'K3'
				WHEN g.milk_cohort = 'K0' OR g.management_stage = 'K0' THEN 'K0'
				ELSE 'unmapped'
			END AS bucket,
			gw.weight_kg
		FROM public.goats g
		LEFT JOIN latest_purpose lp ON lp.tenant_id = g.tenant_id AND lp.goat_id = g.goat_id
		LEFT JOIN goat_weight gw ON gw.tenant_id = g.tenant_id AND gw.goat_id = g.goat_id
		LEFT JOIN public.locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
		LEFT JOIN public.locations farm ON farm.tenant_id = g.tenant_id AND farm.location_id = g.farm_id
		WHERE g.tenant_id = $1
			AND g.lifecycle_status NOT IN ('dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive')
			AND g.merged_into_goat_id IS NULL
			%s
	),
	fattening_weight AS (
		SELECT avg(weight_kg) AS avg_weight_kg, count(weight_kg)::int AS weighed_animals
		FROM classified
		WHERE bucket = 'fattening'
	),
	rates(bucket, label, fixed_weight_kg, price_per_kg, display_order) AS (
		VALUES
			('fattening', 'Fattening animals', NULL::float8, 450::float8, 1),
			('adult_female', 'Adult females', 40::float8, 600::float8, 2),
			('adult_male_buck', 'Adult males / bucks', 60::float8, 500::float8, 3),
			('K0', 'K0', 3::float8, 500::float8, 4),
			('K1', 'K1', 3::float8, 500::float8, 5),
			('K2', 'K2', 8::float8, 500::float8, 6),
			('K3', 'K3', 15::float8, 500::float8, 7)
	),
	counts AS (
		SELECT bucket, count(*)::int AS animal_count
		FROM classified
		WHERE bucket <> 'unmapped'
		GROUP BY bucket
	),
	total_inventory AS (
		SELECT count(*)::int AS live_animals
		FROM classified
	)
	SELECT
		r.bucket,
		r.label,
		coalesce(c.animal_count, 0) AS animal_count,
		coalesce(r.fixed_weight_kg, fw.avg_weight_kg, 0)::float8 AS weight_kg,
		r.price_per_kg,
		(coalesce(c.animal_count, 0) * coalesce(r.fixed_weight_kg, fw.avg_weight_kg, 0))::float8 AS meat_kg,
		(coalesce(c.animal_count, 0) * coalesce(r.fixed_weight_kg, fw.avg_weight_kg, 0) * r.price_per_kg)::float8 AS value_rupees,
		(r.fixed_weight_kg IS NULL) AS actual_weight,
		CASE WHEN r.fixed_weight_kg IS NULL THEN coalesce(fw.weighed_animals, 0) ELSE 0 END AS weighed_animals,
		ti.live_animals
	FROM rates r
	LEFT JOIN counts c ON c.bucket = r.bucket
	CROSS JOIN fattening_weight fw
	CROSS JOIN total_inventory ti
	ORDER BY r.display_order`
