package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// BUYER ANALYTICS read (maintainer request 2026-09-15, docs/decisions/sales-buyer-analytics.md).
//
// RECORDED CROSS-MODULE REPORTING READ, the load-wise shape. Procurement owns procurement_vendors
// and joins OUT to sales_deals / sales_deal_lines to list every closed deal per buyer. Read-only
// over the ledger and reporting grain only: nothing here gates a sale, edits a vendor, or feeds a
// write; the sales module's own lock (migration 000173) is untouched because the dependency points
// the other way.

var _ ports.BuyerAnalyticsRepository = (*Repository)(nil)

// closedBuyerDealsSQL resolves WHO BOUGHT for every closed deal, once, and hands the domain one
// fact per deal. The grouping into buyers happens in domain.BuildBuyerAnalytics.
//
// projection-review: membership=sales_deals at ROW grain (one recorded deal / one sheet row),
// filtered to tenant, status = 'Deal Closed' and the optional farm; group_key=deal id on every
// side (lines are pre-aggregated to ONE row per deal_id before they join, and the vendor
// resolution attaches at most one vendor per deal), so the read returns exactly one row per
// closed deal and can never fan out; join_cardinality=lines 1:1 after the GROUP BY deal_id,
// direct vendor at most 1:1 on its PK, name match at most 1:1 because name_match is grouped on
// the normalized name and a name held by two vendors resolves to NULL (agree-or-go-bare) rather
// than to either; pagination=none, whole-filter read (the page contract is whole-filter
// aggregates, computed in the domain); scope=tenant_id on every branch.
//
// The name fold is deliberately on the register's BUSINESS name alone, normalized exactly as
// domain.NormalizeBuyerName normalizes the typed name (whitespace collapsed, lower case). The
// contact person's name is not matched: "Mahendran" the business and "Mahendran" the contact of
// some other firm would otherwise both claim the same sheet deals.
//
// scale-guard:ignore: whole-filter read of an authored commercial ledger (70 rows today, grows by
// deals closed and never with herd size), joined to the vendor register (a few hundred rows) once
// per request in one set-based statement over the tenant indexes. Same shape and reasoning as the
// sales overview's closedDeals read.
const closedBuyerDealsSQL = `
WITH deal AS (
    SELECT d.id, d.sale_date::text AS sale_date, d.farm, d.buyer_name,
           lower(regexp_replace(btrim(d.buyer_name), '\s+', ' ', 'g')) AS name_key,
           coalesce(d.buyer_place, '') AS buyer_place,
           d.buyer_vendor_id, d.product_type, d.animal_count, d.male_count, d.female_count,
           d.sales_value, coalesce(d.payment_received, 0) AS payment_received
    FROM public.sales_deals d
    WHERE d.tenant_id = $1 AND d.status = 'Deal Closed' %s
),
line_rollup AS (
    SELECT l.deal_id,
           sum(CASE WHEN l.product_type IN ('Sheep', 'Goat')
                    THEN coalesce(l.animal_count, coalesce(l.male_count, 0) + coalesce(l.female_count, 0))
                    ELSE 0 END) AS animals,
           array_agg(DISTINCT l.product_type ORDER BY l.product_type) AS product_types
    FROM public.sales_deal_lines l
    WHERE l.tenant_id = $1 AND l.deal_id IN (SELECT id FROM deal)
    GROUP BY l.deal_id
),
vendor AS (
    SELECT v.vendor_id, v.business_name, coalesce(v.phone_number, '') AS phone_number, v.record_type,
           coalesce(nullif(btrim(v.city), ''), '') AS city, coalesce(btrim(v.state), '') AS state,
           lower(regexp_replace(btrim(v.business_name), '\s+', ' ', 'g')) AS name_key
    FROM public.procurement_vendors v
    WHERE v.tenant_id = $1
),
name_match AS (
    -- One vendor per normalized business name, or NULL when the name is held by several: a
    -- typed name that could mean two register rows is claimed by neither.
    SELECT name_key,
           CASE WHEN count(*) = 1 THEN min(vendor_id::text)::uuid END AS vendor_id
    FROM vendor
    GROUP BY name_key
),
resolved AS (
    SELECT deal.*, coalesce(direct.vendor_id, nm.vendor_id) AS vendor_id
    FROM deal
    LEFT JOIN vendor direct ON direct.vendor_id = deal.buyer_vendor_id
    LEFT JOIN name_match nm ON direct.vendor_id IS NULL AND nm.name_key = deal.name_key
)
SELECT r.id::text, r.sale_date, r.farm, r.buyer_name, r.name_key, r.buyer_place,
       coalesce(r.vendor_id::text, '') AS vendor_id,
       coalesce(v.business_name, '') AS vendor_name,
       coalesce(v.phone_number, '') AS vendor_phone,
       coalesce(v.record_type, '') AS vendor_category,
       coalesce(v.city, '') AS vendor_city,
       coalesce(v.state, '') AS vendor_state,
       coalesce(lr.animals,
                CASE WHEN r.product_type IN ('Sheep', 'Goat')
                     THEN coalesce(r.animal_count, coalesce(r.male_count, 0) + coalesce(r.female_count, 0))
                     ELSE 0 END)::float8 AS animals,
       r.sales_value::float8 AS revenue,
       greatest(r.sales_value - r.payment_received, 0)::float8 AS outstanding,
       coalesce(lr.product_types, ARRAY[r.product_type]) AS product_types
FROM resolved r
LEFT JOIN vendor v ON v.vendor_id = r.vendor_id
LEFT JOIN line_rollup lr ON lr.deal_id = r.id
ORDER BY r.sale_date, r.id`

// ClosedBuyerDeals implements ports.BuyerAnalyticsRepository.
func (r *Repository) ClosedBuyerDeals(ctx context.Context, tenantID, farm string) ([]domain.BuyerDealFact, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	farmPredicate := ""
	args := []any{tenantID}
	if farm != "" {
		farmPredicate = "AND d.farm = $2"
		args = append(args, farm)
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(closedBuyerDealsSQL, farmPredicate), args...)
	if err != nil {
		return nil, fmt.Errorf("procurement: buyer analytics deals: %w", err)
	}
	defer rows.Close()

	facts := make([]domain.BuyerDealFact, 0, 128)
	for rows.Next() {
		var (
			f            domain.BuyerDealFact
			nameKey      string
			city, state  string
			productTypes []string
		)
		if err := rows.Scan(
			&f.DealID, &f.SaleDate, &f.Farm, &f.BuyerName, &nameKey, &f.BuyerPlace,
			&f.VendorID, &f.VendorName, &f.VendorPhone, &f.VendorCategory, &city, &state,
			&f.Animals, &f.Revenue, &f.Outstanding, &productTypes,
		); err != nil {
			return nil, fmt.Errorf("procurement: buyer analytics deals scan: %w", err)
		}
		if f.VendorID != "" {
			f.BuyerKey = "vendor:" + f.VendorID
			f.VendorPlace = joinPlace(city, state)
		} else {
			f.BuyerKey = "name:" + nameKey
		}
		f.ProductTypes = productTypes
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("procurement: buyer analytics deals rows: %w", err)
	}
	return facts, nil
}

// joinPlace composes city and state the way the vendor register's table shows them.
func joinPlace(city, state string) string {
	city, state = strings.TrimSpace(city), strings.TrimSpace(state)
	switch {
	case city == "":
		return state
	case state == "":
		return city
	default:
		return city + ", " + state
	}
}
