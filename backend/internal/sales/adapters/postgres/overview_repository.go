package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
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
		measuredWeights  map[string][]float64
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
		measuredWeights, err = r.measuredSoldWeights(gctx, tenantID, farm)
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
	// The bands are folded from the SAME closed-deal rows the summary is folded from, so the
	// banded total and the headline animals sold cannot disagree.
	overview.SoldWeightBands = domain.BuildSoldWeightBands(closed, measuredWeights)
	overview.FarmValuation = farmValuation
	return overview, nil
}

// measuredSoldWeights reads the weight recorded when each animal was TAGGED to its sale, keyed
// by deal. It is the best evidence the bands have: one animal, one scale reading. Everything else
// the bands show is derived from a load, and domain.BuildSoldWeightBands keeps the two apart.
//
// Keyed by DEAL and not by line because a tag names the sale, not which product line of it the
// animal belongs to; the domain attaches them to the deal's live lines in line order.
//
// projection-review: membership=goat_sale_allocations at (tenant_id, goat_id, sales_deal_id)
// grain, one row per animal per sale, filtered to status='tagged' where the partial unique index
// (tenant_id, goat_id) WHERE status='tagged' makes each sold animal appear exactly once, and to
// weight_kg IS NOT NULL because a tag with no weight is evidence of a sale and not of a weight;
// group_key=sales_deal_id, one bucket per deal, and the rows within a bucket stay individual
// because each is one animal's own weight; join_cardinality=sales_deals joined 1:1 on
// (tenant_id, id) purely for the farm and closed-status predicates -- every allocation names
// exactly one deal, so the join can neither fan out nor drop a row; pagination=none, whole-filter
// read; scope=tenant, optionally narrowed to one farm through the deal, the same buildDealFilter
// and the same status='Deal Closed' the closed-deal read uses, so the two sides of the fold
// range over one deal set.
func (r *Repository) measuredSoldWeights(ctx context.Context, tenantID, farm string) (map[string][]float64, error) {
	where, args := buildDealFilter(tenantID, farm)
	query := fmt.Sprintf(measuredSoldWeightsSQL, where)
	boundMeasured := sqlbind.MustBind(query, args...)
	rows, err := r.pool.Query(ctx, boundMeasured.SQL(), boundMeasured.Args()...)
	if err != nil {
		return nil, fmt.Errorf("sales measured sold weights: %w", err)
	}
	defer rows.Close()

	out := map[string][]float64{}
	for rows.Next() {
		var (
			dealID string
			kg     float64
		)
		if err := rows.Scan(&dealID, &kg); err != nil {
			return nil, fmt.Errorf("sales measured sold weights scan: %w", err)
		}
		out[dealID] = append(out[dealID], kg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sales measured sold weights rows: %w", err)
	}
	return out, nil
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
	if farm != "" {
		args = append(args, farm)
	}

	query := farmValuationQuery(farm)
	boundValuation := sqlbind.MustBind(query, args...)
	rows, err := r.pool.Query(ctx, boundValuation.SQL(), boundValuation.Args()...)
	if err != nil {
		return domain.FarmValuation{}, fmt.Errorf("sales farm valuation: %w", err)
	}
	defer rows.Close()

	out := domain.FarmValuation{Buckets: []domain.FarmValuationBucket{}, NotValued: []domain.FarmValuationNotValued{}}
	for rows.Next() {
		var bucket domain.FarmValuationBucket
		var totalAnimals int
		var valuedAnimals int
		var excludedAnimals int
		var notValuedJSON []byte
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
			&bucket.MaleCount,
			&bucket.FemaleCount,
			&bucket.SexMissingCount,
			&totalAnimals,
			&valuedAnimals,
			&excludedAnimals,
			&notValuedJSON,
		); err != nil {
			return domain.FarmValuation{}, fmt.Errorf("sales farm valuation scan: %w", err)
		}
		if len(notValuedJSON) > 0 && len(out.NotValued) == 0 {
			if err := json.Unmarshal(notValuedJSON, &out.NotValued); err != nil {
				return domain.FarmValuation{}, fmt.Errorf("sales farm valuation not-valued breakdown: %w", err)
			}
		}
		out.TotalMeatKg += bucket.MeatKg
		out.TotalValueRupees += bucket.ValueRupees
		out.TotalAnimals = totalAnimals
		out.ValuedAnimals = valuedAnimals
		out.ExcludedAnimals = excludedAnimals
		out.Buckets = append(out.Buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return domain.FarmValuation{}, fmt.Errorf("sales farm valuation rows: %w", err)
	}
	return out, nil
}

func farmValuationQuery(farm string) string {
	farmPredicate := ""
	if farm != "" {
		farmPredicate = "AND (upper(park.location_code) = upper($2) OR (park.location_code IS NULL AND upper(farm.location_code) = upper($2)))"
	}
	return fmt.Sprintf(farmValuationSQL, farmPredicate)
}

// closedDeals loads every closed deal in the filter -- the ONE bounded read behind the summary,
// monthly, price-band and buyer blocks, so the four blocks cannot range over different predicates.
//
// projection-review: membership=sales_deals at ROW grain (one recorded deal), plus its
// sales_deal_lines attached by a SEPARATE keyed read (deal_id = ANY) and hung off their own deal,
// never joined into the deal rows, so a deal with three lines is still one deal here;
// group_key=deal_id for the deal blocks and (product_type, breed) at LINE grain for the price
// bands, computed in domain.BuildDealAggregates; join_cardinality=no SQL join at all -- deals
// 1:N lines is resolved in Go by attaching each line to exactly one deal, and every line's value
// is summed exactly once while the deal's own sales_value is the same total by construction
// (RollupLines, one transaction); pagination=none, whole-filter read; scope=tenant_id plus the
// shared farm predicate (buildDealFilter) and status = 'Deal Closed'.
// The grouped consumers live in domain.BuildDealAggregates, whose own projection-review note
// names the group keys and the realized-price ratio's shared key set.
func (r *Repository) closedDeals(ctx context.Context, tenantID, farm string) ([]domain.Deal, error) {
	where, args := buildDealFilter(tenantID, farm)
	// scale-guard:ignore: whole-filter read of an authored commercial ledger (63 sheet rows today,
	// grows by deals closed, never with herd size); the page contract is whole-filter aggregates,
	// which cannot be computed from a page.
	query := fmt.Sprintf(`SELECT %s FROM public.sales_deals d WHERE %s AND d.status = 'Deal Closed' ORDER BY d.sale_date, d.id`, dealColumns, where)
	boundClosed := sqlbind.MustBind(query, args...)
	rows, err := r.pool.Query(ctx, boundClosed.SQL(), boundClosed.Args()...)
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
	// The product/breed blocks are computed at LINE grain (000296): one batched read.
	// projection-review: membership=sales_deal_lines keyed by deal_id over the deals read above;
	// group_key=deal_id, each line attached to exactly one deal; join_cardinality=1:N resolved in
	// Go (no SQL join, so the deal rows cannot fan out); pagination=none; scope=tenant_id and the
	// deals' own farm/status predicate, since only their ids are handed in.
	if err := r.attachDealLines(ctx, tenantID, deals); err != nil {
		return nil, err
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
	boundStatus := sqlbind.MustBind(query, args...)
	rows, err := r.pool.Query(ctx, boundStatus.SQL(), boundStatus.Args()...)
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
	boundStatus := sqlbind.MustBind(query, tenantID)
	rows, err := r.pool.Query(ctx, boundStatus.SQL(), boundStatus.Args()...)
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
	boundPlaces := sqlbind.MustBind(query, args...)
	rows, err := r.pool.Query(ctx, boundPlaces.SQL(), boundPlaces.Args()...)
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
	boundTotals := sqlbind.MustBind(totalsQuery, args...)
	if err := r.pool.QueryRow(ctx, boundTotals.SQL(), boundTotals.Args()...).Scan(&roster.Total, &roster.SalesCount); err != nil {
		return domain.TagRoster{}, fmt.Errorf("sales tag roster totals: %w", err)
	}

	byTypeQuery := fmt.Sprintf(`
		SELECT animal_label, count(*)
		FROM public.sales_sold_animal_tags
		WHERE %s
		GROUP BY animal_label
		ORDER BY count(*) DESC, animal_label`, where)
	boundByType := sqlbind.MustBind(byTypeQuery, args...)
	rows, err := r.pool.Query(ctx, boundByType.SQL(), boundByType.Args()...)
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

// measuredSoldWeightsSQL reads one row per individually weighed sold animal. %s is the shared
// deal filter (tenant, optional farm) so it ranges over the same deals as the rest of the page.
//
// projection-review: membership=goat_sale_allocations at (tenant_id, goat_id, sales_deal_id) grain, one row per animal per sale, narrowed to status='tagged' -- where the partial unique index (tenant_id, goat_id) WHERE status='tagged' makes each sold animal appear exactly once -- and to weight_kg IS NOT NULL, because a tag with no weight is evidence of a sale and not of a weight; group_key=none in SQL, this read is deliberately UNGROUPED and returns one row per animal so each weight keeps its own band, and the only key it carries is sales_deal_id, which the domain groups by; join_cardinality=sales_deals joined 1:1 on (tenant_id, id) from (a.tenant_id, a.sales_deal_id) purely for the farm and status predicates -- every allocation names exactly one deal, so the join can neither fan out nor drop a row, and sales_deal_lines is deliberately NOT joined here because a tag names the sale and not one product line of it; pagination=none, whole-filter read feeding a whole-register aggregate that cannot be computed from a page; scope=tenant_id plus the same buildDealFilter farm predicate and the same status='Deal Closed' the closed-deal read uses, so the two sides the domain folds together range over one deal set and the banded total equals the page's animals sold.
//
// scale-guard:ignore: read over the sale-allocation register (one row per animal SOLD, 151 today,
// grows with sales rather than herd size) keyed by the indexed (tenant_id, sales_deal_id); the
// bands are a whole-register aggregate and cannot be served from a page.
const measuredSoldWeightsSQL = `
		SELECT a.sales_deal_id::text, a.weight_kg::float8
		FROM public.goat_sale_allocations a
		JOIN public.sales_deals d ON d.id = a.sales_deal_id AND d.tenant_id = a.tenant_id
		WHERE %s AND a.tenant_id = $1 AND a.status = 'tagged' AND a.weight_kg IS NOT NULL
			AND d.status = 'Deal Closed'
		ORDER BY a.sales_deal_id, a.weight_kg, a.allocation_id`

// farmValuationSQL is the one live-inventory rollup behind the Farm Value cards. %s is the optional
// farm-code predicate.
//
// CLINICALLY HOUSED ANIMALS ARE STILL INVENTORY (maintainer decision 2026-09-10). An animal in ICU
// is worth what its cohort is worth -- the tag says where it is being kept, not that it has no
// value. That is no longer three branches of a CASE: it is two entries in the seeded stage rows,
// ICU under Adult and ICU-Kid under K2, and the farm can move them.
//
// WHICH STAGE AN ANIMAL IS VALUED IN IS AUTHORED (maintainer instruction 2026-09-24, migration
// 000405, domain/valuation_stages.go). The CASE that used to decide it knew six stages while the
// farm's register carries nineteen, so Warmup -- 58 live kids the day this changed -- could not be
// valued without a deploy. The stage rows say which register entries they cover; the animal is
// filed by its own management stage or, when the register lost that, its milk cohort.
//
// A stage no row names stays 'unmapped' and stays visible in the not-valued breakdown under its own
// name. There is no fallback bucket, and adding one would undo the change: an animal valued at a
// figure nobody chose for it is the defect this replaced.
//
// A whole-herd valuation read served once per Farm value page load, indexed on (tenant_id) over
// live goats (5k-50k envelope), with the assumption buckets joined from ONE
// sales_valuation_assumptions row; the CTE count moved only because the rates now come from that
// row instead of an inline VALUES list -- shape and row counts unchanged.
// scale-guard:ignore: whole-herd valuation read, once per page load, indexed on tenant_id; the rates CTE reads one assumptions row
const farmValuationSQL = `
	WITH idmap AS (
		SELECT tenant_id, goat_id, lower(btrim(identifier_value)) AS identifier
		FROM public.goat_identifiers
		WHERE tenant_id = $1
			AND status = 'active'
			AND identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
			AND btrim(identifier_value) <> ''
	),
	latest_weight AS (
		SELECT DISTINCT ON (tenant_id, lower(btrim(scanned_identifier)))
			tenant_id, lower(btrim(scanned_identifier)) AS scanned_identifier, weight_kg::float8 AS weight_kg, accepted_at
		FROM public.weighing_observations
		WHERE tenant_id = $1 AND verification_status = 'verified' AND btrim(coalesce(scanned_identifier, '')) <> ''
		ORDER BY tenant_id, lower(btrim(scanned_identifier)), accepted_at DESC
	),
	goat_weight AS (
		SELECT DISTINCT ON (i.tenant_id, i.goat_id)
			i.tenant_id, i.goat_id, w.weight_kg
		FROM idmap i
		JOIN latest_weight w ON w.tenant_id = i.tenant_id AND w.scanned_identifier = i.identifier
		ORDER BY i.tenant_id, i.goat_id, w.accepted_at DESC
	),
	-- THE STAGES ARE AUTHORED (maintainer instruction 2026-09-24, migration 000405). A valuation
	-- stage names the register entries it covers; an animal is filed by its own management stage
	-- or, when the register lost that, its milk cohort. Both sides are normalized the one way
	-- (upper, strip non-alphanumerics) that domain.NormalizeStageMatch normalizes the authored
	-- side, because this herd carries both 'ICU- kid' and 'ICU-Kid'.
	--
	-- An animal in no named stage is 'unmapped' and stands in the not-valued breakdown under its
	-- own stage name -- which is how 58 Warmup kids asked to be priced. There is deliberately no
	-- fallback bucket.
	--
	-- A tenant with no authored row reads the seeded six, the retired CASE written out.
	stage_rules AS (
		SELECT st.stage, st.display_order,
			upper(regexp_replace(btrim(m.match), '[^A-Za-z0-9]+', '', 'g')) AS match_norm
		FROM public.sales_valuation_assumptions a
		CROSS JOIN LATERAL jsonb_to_recordset(a.stages) AS st(stage text, display_order int, matches jsonb)
		CROSS JOIN LATERAL jsonb_array_elements_text(st.matches) AS m(match)
		WHERE a.tenant_id = $1::uuid
		UNION ALL
		SELECT * FROM (VALUES
			('fattening', 1, 'F2'), ('fattening', 1, 'F2MALE'), ('fattening', 1, 'F2FEMALE'),
			('adult', 2, 'BUCK'), ('adult', 2, 'MOTHER'), ('adult', 2, 'MILKING'), ('adult', 2, 'M0'),
			('adult', 2, 'PREGNANT'), ('adult', 2, 'NONPREGNANT'), ('adult', 2, 'ICU'),
			('K0', 3, 'K0'), ('K1', 4, 'K1'),
			('K2', 5, 'K2'), ('K2', 5, 'ICUKID'), ('K3', 6, 'K3')
		) d(stage, display_order, match_norm)
		WHERE NOT EXISTS (SELECT 1 FROM public.sales_valuation_assumptions a WHERE a.tenant_id = $1::uuid)
	),
	-- One row per register entry, so the two joins below cannot fan an animal out. A register entry
	-- claimed by two valuation stages is REFUSED at the write; this keeps the read total even
	-- against a row written before that rule existed, and never picks between two live answers.
	stage_by_match AS (
		SELECT DISTINCT ON (match_norm) match_norm, stage
		FROM stage_rules
		ORDER BY match_norm, display_order, stage
	),
	classified AS (
		SELECT
				-- EVERY STAGE IS PRICED BY GENDER (maintainer instruction 2026-09-23). The stage is
				-- decided first, exactly as before, and the animal's gender is appended -- so an
				-- animal that used to land in 'K2' now lands in 'K2_female' or 'K2_male' and is
				-- carried at that row's own weight and rate.
				--
				-- A MOTHER is female by definition and stays so whatever the register says; every
				-- other stage takes the animal's own gender, and one that is NOT RECORDED is
				-- valued on the FEMALE row (maintainer decision, same day: females are the larger
				-- share, so it is the closer guess). The sex_missing count below counts those animals, so
				-- the guess is visible on the page rather than silent in the total.
				CASE WHEN coalesce(sc.stage, sm.stage) IS NULL THEN 'unmapped' ELSE coalesce(sc.stage, sm.stage) || '_' || s.sex_norm END AS bucket,
			gw.weight_kg,
			g.management_stage,
			g.milk_cohort,
			lower(btrim(coalesce(g.sex, ''))) AS sex
			FROM public.goats g
			CROSS JOIN LATERAL (
				SELECT upper(regexp_replace(btrim(coalesce(g.management_stage, '')), '[^A-Za-z0-9]+', '', 'g')) AS stage_norm,
					upper(regexp_replace(btrim(coalesce(g.milk_cohort, '')), '[^A-Za-z0-9]+', '', 'g')) AS cohort_norm,
					-- The gender half of the bucket key. Anything that is not plainly male reads as
					-- female, which is the recorded decision for an animal with no gender on file.
					CASE WHEN lower(btrim(coalesce(g.sex, ''))) = 'male' THEN 'male' ELSE 'female' END AS sex_norm
			) s
			-- The stage half, read from the farm's authored rows instead of a CASE. Both joins are
			-- 1:0..1 BY CONSTRUCTION -- stage_by_match holds one row per register entry -- so no
			-- animal can be counted twice however the farm writes its stages.
			--
			-- THE MILK COHORT WINS, which is the order the retired CASE read them in and is not an
			-- arbitrary tie-break. Its K1/K2/K3/K0 arms were tested BEFORE its clinical arms, so a
			-- kid whose management stage had been changed to ICU while the register still knew its
			-- milk band was valued as that band. Reading the stage first flipped exactly that
			-- animal from a 3-15 kg kid row to a 40-60 kg adult one -- on a herd where no animal
			-- carries both today, so nothing would have shown it.
			--
			-- A cohort is only ever a milk band, so it can only pull an animal towards a kid
			-- stage; an animal past milk carries none and is filed by its stage as before.
			LEFT JOIN stage_by_match sc ON s.cohort_norm <> '' AND sc.match_norm = s.cohort_norm
			LEFT JOIN stage_by_match sm ON sm.match_norm = s.stage_norm
			LEFT JOIN goat_weight gw ON gw.tenant_id = g.tenant_id AND gw.goat_id = g.goat_id
		LEFT JOIN public.locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
		LEFT JOIN public.locations farm ON farm.tenant_id = g.tenant_id AND farm.location_id = g.farm_id
		WHERE g.tenant_id = $1
			AND g.lifecycle_status NOT IN ('dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive')
			AND g.merged_into_goat_id IS NULL
			%s
		),
		not_valued AS (
			SELECT coalesce(
				jsonb_agg(jsonb_build_object('label', label, 'count', animal_count) ORDER BY animal_count DESC, label),
				'[]'::jsonb
			) AS breakdown
			FROM (
				SELECT
					coalesce(nullif(btrim(management_stage), ''), nullif(btrim(milk_cohort), ''), 'Unmapped') AS label,
					count(*)::int AS animal_count
				FROM classified
				WHERE bucket = 'unmapped'
				GROUP BY label
			) x
		),
		-- The measured weight a bucket left BLANK prices at, per BUCKET rather than once for the
		-- whole herd. It used to be one average over every fattening animal, which was right while
		-- fattening was one row; split by gender (2026-09-23) that same average would have priced
		-- the male and female rows identically and undone the split the farm asked for. Each row
		-- now weighs its own animals.
		measured_weight AS (
		SELECT bucket, avg(weight_kg) AS avg_weight_kg, count(weight_kg)::int AS weighed_animals
		FROM classified
		WHERE bucket <> 'unmapped'
		GROUP BY bucket
	),
	-- FARM VALUATION ASSUMPTIONS ARE DATA (maintainer instruction 2026-09-19, migration 000367):
	-- the bucket rates are the tenant's authored row, re-read per request; a tenant without a row
	-- (created after the migration) values on the seeded defaults, the same figures the VALUES
	-- table here used to carry.
	rates AS (
		SELECT b.bucket, b.label, b.fixed_weight_kg::float8, b.price_per_kg::float8, b.display_order
		FROM public.sales_valuation_assumptions a
		CROSS JOIN LATERAL jsonb_to_recordset(a.buckets) AS b(bucket text, label text, fixed_weight_kg float8, price_per_kg float8, display_order int)
		WHERE a.tenant_id = $1::uuid
		UNION ALL
		SELECT * FROM (VALUES
			('fattening_female', 'Fattening · Female', NULL::float8, 450::float8, 1),
			('fattening_male', 'Fattening · Male', NULL::float8, 450::float8, 2),
			('adult_female', 'Adult · Female', 40::float8, 600::float8, 3),
			('adult_male', 'Adult · Male', 60::float8, 500::float8, 4),
			('K0_female', 'K0 · Female', 3::float8, 500::float8, 5),
			('K0_male', 'K0 · Male', 3::float8, 500::float8, 6),
			('K1_female', 'K1 · Female', 3::float8, 500::float8, 7),
			('K1_male', 'K1 · Male', 3::float8, 500::float8, 8),
			('K2_female', 'K2 · Female', 8::float8, 500::float8, 9),
			('K2_male', 'K2 · Male', 8::float8, 500::float8, 10),
			('K3_female', 'K3 · Female', 15::float8, 500::float8, 11),
			('K3_male', 'K3 · Male', 15::float8, 500::float8, 12)
		) d(bucket, label, fixed_weight_kg, price_per_kg, display_order)
		WHERE NOT EXISTS (SELECT 1 FROM public.sales_valuation_assumptions a WHERE a.tenant_id = $1::uuid)
	),
	-- projection-review: membership=classified, one row per current live goat (goats filtered to
	-- non-terminal and non-merged, weight joined 1:1 after idmap is reduced to one row per goat);
	-- group_key=the mutually-exclusive CASE bucket, and the three sex FILTER counts ride the SAME
	-- GROUP BY so male + female + missing == animal_count row for row; join_cardinality=none inside
	-- this rollup -- rates is joined 1:1 on bucket by the outer SELECT, measured_weight is LEFT
	-- JOINed 1:0..1 on that same bucket key and total_inventory is a one-row CROSS JOIN; pagination=none, whole-current-inventory card;
	-- scope=tenant_id and the optional CBE/CPT farm code applied in classified before any count.
	counts AS (
		SELECT
			bucket,
			count(*)::int AS animal_count,
			count(*) FILTER (WHERE sex = 'male')::int AS male_count,
			count(*) FILTER (WHERE sex = 'female')::int AS female_count,
			count(*) FILTER (WHERE sex NOT IN ('male', 'female'))::int AS sex_missing_count
		FROM classified
		WHERE bucket <> 'unmapped'
		GROUP BY bucket
	),
	total_inventory AS (
		SELECT
			count(*)::int AS live_animals,
			count(*) FILTER (WHERE bucket <> 'unmapped')::int AS valued_animals,
			count(*) FILTER (WHERE bucket = 'unmapped')::int AS excluded_animals
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
		coalesce(c.male_count, 0) AS male_count,
		coalesce(c.female_count, 0) AS female_count,
		coalesce(c.sex_missing_count, 0) AS sex_missing_count,
			ti.live_animals,
			ti.valued_animals,
			ti.excluded_animals,
			nv.breakdown
		FROM rates r
		LEFT JOIN counts c ON c.bucket = r.bucket
		LEFT JOIN measured_weight fw ON fw.bucket = r.bucket
		CROSS JOIN total_inventory ti
		CROSS JOIN not_valued nv
		ORDER BY r.display_order`
