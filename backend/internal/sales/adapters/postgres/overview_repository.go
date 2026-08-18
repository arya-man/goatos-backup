package postgres

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
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

	overview := domain.Overview{}

	closed, err := r.closedDeals(ctx, tenantID, farm)
	if err != nil {
		return domain.Overview{}, err
	}
	overview.Summary, overview.Monthly, overview.PriceBands, overview.Buyers = domain.BuildDealAggregates(closed)

	if overview.BuyerPipeline, err = r.buyerPipeline(ctx, tenantID, farm); err != nil {
		return domain.Overview{}, err
	}
	if overview.FPOPipeline, err = r.fpoPipeline(ctx, tenantID); err != nil {
		return domain.Overview{}, err
	}
	if overview.TagRoster, err = r.tagRoster(ctx, tenantID, farm); err != nil {
		return domain.Overview{}, err
	}
	if overview.WeightAudit, err = r.weightAudit(ctx, tenantID); err != nil {
		return domain.Overview{}, err
	}
	if overview.MarketBenchmarks, err = r.marketBenchmarks(ctx, tenantID); err != nil {
		return domain.Overview{}, err
	}
	return overview, nil
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
