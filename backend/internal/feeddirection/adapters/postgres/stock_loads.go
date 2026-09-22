package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// Purchased vs consumed, per load (maintainer request 2026-09-19). See domain/stock_loads.go for
// the rule; this file is the FIFO arithmetic in SQL.
//
// projection-review: membership=feed_purchases at its (tenant, farm_label, feed_item_key, batch_no)
// natural key -- one row per load, every load, nothing joined at row grain; group_key=(farm_label,
// feed_item_key) for every window and aggregate: the FIFO position (prior_net_kg) is a running SUM
// over loads ordered within that pair, consumption cells are pre-collapsed to ONE row per (park,
// feed_item_key, feed_day) before the running total, so the per-day cumulative sees each day once,
// and the per-load day counts / first day / finish day aggregate those cells back to one row per
// load; join_cardinality=loads LEFT JOIN cells 1:0..N then GROUP BY load id (many side aggregated),
// loads LEFT JOIN family_total 1:1 (one row per pair), loads LEFT JOIN burn 1:0..1 (one row per
// pair), and the page rows and total range over the same filtered load set
// (the `page` CTE) so numerator and denominator share one key set. The DAYS-LEFT arithmetic adds a
// second group_key, (farm_label, family_key), and both of its sides range over it identically: the
// running kg left walks that family's loads in arrival order and the rate is that family's kg per
// calendar day, regrouped to the family BEFORE averaging so a substituted day counts once -- byte
// for byte the key set the stock cards use, which is what makes the two surfaces agree.
// merge_map is unique on member_key and rate_override on (farm, feed), so neither LEFT JOIN can fan
// a load out; pagination=LIMIT/OFFSET bounded
// by domain.NormaliseStockLoadsPage (offset capped at 10000) with whole-filter counts computed
// beside the page, never page-local; scope=tenant_id everywhere plus the caller's authorized park
// set on both purchases and sheets.
//
// Bounded by construction: a tenant's purchase ledger is a few hundred loads and the consumption
// side is collapsed to one row per (park, item, day) before any window runs, so the whole query
// is small-N at the 5k-50k envelope; the OFFSET is capped by the same page rule the packing
// mismatch list and the purchase ledger already use.
// scale-guard:ignore: 5k-50k-envelope bounded purchase-ledger aggregate with capped ledger-style offset paging
const stockLoadsSQL = `
WITH merge_map AS (
    -- The SAME transitional split-concentrate fold the stock cards use
    -- (domain.StockFamilyMerge). It never changes a load's own kg -- each item keeps its own
    -- ledger, exactly as the cards do -- it decides which loads share one runway.
    SELECT m.member_key, m.family_key
    FROM unnest($7::text[], $8::text[]) AS m(member_key, family_key)
),
rate_override AS (
    -- The same pinned burn rates the cards divide by (domain.StockRateOverrides), keyed on
    -- (farm, family). An empty list is the behaviour without it.
    SELECT o.farm_label, o.feed_item_key, o.kg_per_day::numeric AS kg_per_day
    FROM unnest($9::text[], $10::text[], $11::text[]) AS o(farm_label, feed_item_key, kg_per_day)
),
loads AS (
    SELECT p.feed_purchase_id, p.farm_label, p.feed_item_label, p.feed_item_key, p.park_id,
           COALESCE(mm.family_key, p.feed_item_key) AS family_key,
           p.batch_no, p.vendor, p.purchase_date, p.delivery_status, p.days_of_stock,
           CASE WHEN p.delivery_status = 'reached' THEN COALESCE(p.reached_on, p.purchase_date) END AS reached_on,
           p.depletes_from, p.quantity_kg,
           p.stock_kg - p.consumed_at_import_kg AS net_kg,
           -- RETIRED feeds are the ones the farm no longer buys. Their loads still COUNT (their
           -- leftover sacks are part of the family's runway, the same rule the cards apply) but
           -- they are not shown: the table answers for the feeds the farm buys today.
           (COALESCE(c.status, 'active') = 'retired' OR COALESCE(fc.status, 'active') = 'retired') AS retired
    FROM feed_purchases p
    LEFT JOIN merge_map mm ON mm.member_key = p.feed_item_key
    LEFT JOIN feed_item_catalog c
      ON c.tenant_id = $1 AND c.feed_item_key = p.feed_item_key
    LEFT JOIN feed_item_catalog fc
      ON fc.tenant_id = $1 AND fc.feed_item_key = COALESCE(mm.family_key, p.feed_item_key)
    WHERE p.tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR p.park_id = ANY ($2::uuid[]))
),
-- FIFO position within (farm, feed): the kg of every REACHED load that arrived before this one.
-- Ordered by arrival day, then purchase day, then batch, so two loads reaching on one day keep
-- the order they were bought in.
positioned AS (
    SELECT l.*,
           COALESCE(SUM(net_kg) OVER (
               PARTITION BY farm_label, feed_item_key
               ORDER BY depletes_from, purchase_date, batch_no
               ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING), 0) AS prior_net_kg,
           ROW_NUMBER() OVER (
               PARTITION BY farm_label, feed_item_key
               ORDER BY depletes_from DESC, purchase_date DESC, batch_no DESC) AS recency
    FROM loads l
    WHERE l.delivery_status = 'reached'
),
family AS (
    SELECT farm_label, feed_item_key, MIN(park_id::text) AS park_id_text, MIN(depletes_from) AS ledger_from,
           SUM(net_kg) AS family_net_kg
    FROM positioned
    GROUP BY farm_label, feed_item_key
),
-- Consumption from BOTH sources at one grain, the same union the stock cards read: locked-sheet
-- directed kg plus externally-tracked consumption (milk). One row per (park, item, day).
cells AS (
    SELECT park_id, feed_item_key, feed_day, SUM(kg) AS kg
    FROM (
        SELECT i.park_id, r.feed_item_key, i.feed_day, SUM(r.quantity_kg) AS kg
        FROM feed_direction_issues i
        JOIN feed_direction_issue_rows r
          ON r.tenant_id = $1 AND r.feed_direction_issue_id = i.feed_direction_issue_id
        WHERE i.tenant_id = $1
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR i.park_id = ANY ($2::uuid[]))
          AND i.state = 'locked'
          AND r.quantity_kg IS NOT NULL
        GROUP BY i.park_id, r.feed_item_key, i.feed_day
        UNION ALL
        SELECT x.park_id, x.feed_item_key, x.feed_day, SUM(x.quantity_kg) AS kg
        FROM feed_effective_external_consumption x
        WHERE x.tenant_id = $1
          AND x.park_id IS NOT NULL
          AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR x.park_id = ANY ($2::uuid[]))
        GROUP BY x.park_id, x.feed_item_key, x.feed_day
    ) both_sources
    GROUP BY park_id, feed_item_key, feed_day
),
-- Running total of everything directed against the family since its ledger start. cum_before is
-- the total BEFORE the day, so a day that straddles two loads is counted for both.
family_cells AS (
    SELECT f.farm_label, f.feed_item_key, c.feed_day, c.kg,
           SUM(c.kg) OVER (PARTITION BY f.farm_label, f.feed_item_key ORDER BY c.feed_day) AS cum_kg
    FROM family f
    JOIN cells c
      ON f.park_id_text IS NOT NULL
     AND c.park_id = f.park_id_text::uuid
     AND c.feed_item_key = f.feed_item_key
     AND c.feed_day >= f.ledger_from
),
family_total AS (
    SELECT farm_label, feed_item_key, COALESCE(SUM(kg), 0) AS directed_kg
    FROM family_cells
    GROUP BY farm_label, feed_item_key
),
-- Burn rate, at the FAMILY grain and with the pinned overrides: byte for byte the divisor the
-- stock card above this table quotes, so the two cannot disagree about the runway. Regrouping to
-- the family BEFORE averaging is the whole point -- a day the pens ate the successor INSTEAD of a
-- retired member is one day's draw on one family, never two feeds' worth.
family_day AS (
    SELECT fc.farm_label, COALESCE(mm.family_key, fc.feed_item_key) AS family_key, fc.feed_day,
           SUM(fc.kg) AS kg
    FROM family_cells fc
    LEFT JOIN merge_map mm ON mm.member_key = fc.feed_item_key
    GROUP BY fc.farm_label, COALESCE(mm.family_key, fc.feed_item_key), fc.feed_day
),
burn AS (
    SELECT b.farm_label, b.family_key,
           COALESCE(ov.kg_per_day, b.avg_kg) AS avg_daily_kg
    FROM (
        SELECT farm_label, family_key, AVG(kg) FILTER (WHERE rn <= 3) AS avg_kg
        FROM (
            SELECT farm_label, family_key, kg,
                   ROW_NUMBER() OVER (PARTITION BY farm_label, family_key ORDER BY feed_day DESC) AS rn
            FROM family_day
        ) ranked
        GROUP BY farm_label, family_key
    ) b
    LEFT JOIN rate_override ov
      ON ov.farm_label = b.farm_label AND ov.feed_item_key = b.family_key
),
-- Which days drew on which load: a day touches load L when its running total passes L's start
-- and the total before the day is still inside L's range. Never before the load's own arrival.
-- The load that is newest AS OF THAT DAY takes any overrun above its own kg, so a later-arriving
-- load is not charged for feed that had already gone out before it reached the farm.
load_days AS (
    SELECT p.feed_purchase_id,
           SUM(
               GREATEST(0,
                   LEAST(
                       fc.cum_kg,
                       CASE WHEN EXISTS (
                           SELECT 1
                           FROM positioned next_load
                           WHERE next_load.farm_label = p.farm_label
                             AND next_load.feed_item_key = p.feed_item_key
                             AND next_load.depletes_from <= fc.feed_day
                             AND (next_load.depletes_from, next_load.purchase_date, next_load.batch_no) >
                                 (p.depletes_from, p.purchase_date, p.batch_no)
                       )
                       THEN p.prior_net_kg + p.net_kg
                       ELSE fc.cum_kg
                       END
                   ) - GREATEST(fc.cum_kg - fc.kg, p.prior_net_kg)
               )
           )                                                            AS consumed_kg,
           COUNT(*)                                                     AS days_consumed,
           MIN(fc.feed_day)                                             AS consumption_from,
           MIN(fc.feed_day) FILTER (WHERE fc.cum_kg >= p.prior_net_kg + p.net_kg) AS finished_on
    FROM positioned p
    JOIN family_cells fc
      ON fc.farm_label = p.farm_label
     AND fc.feed_item_key = p.feed_item_key
     AND fc.feed_day >= p.depletes_from
     AND fc.cum_kg > p.prior_net_kg
     AND (
         fc.cum_kg - fc.kg < p.prior_net_kg + p.net_kg
         OR NOT EXISTS (
             SELECT 1
             FROM positioned next_load
             WHERE next_load.farm_label = p.farm_label
               AND next_load.feed_item_key = p.feed_item_key
               AND next_load.depletes_from <= fc.feed_day
               AND (next_load.depletes_from, next_load.purchase_date, next_load.batch_no) >
                   (p.depletes_from, p.purchase_date, p.batch_no)
         )
     )
    GROUP BY p.feed_purchase_id
),
scored AS (
    SELECT p.*,
           -- FIFO split from actual feed days. Any overrun belongs to the load that was newest when
           -- the feed went out, not to a future load that reached after those days.
           COALESCE(ld.consumed_kg, 0) AS consumed_kg,
           ld.days_consumed, ld.consumption_from, ld.finished_on,
           b.avg_daily_kg
    FROM positioned p
    LEFT JOIN family_total ft ON ft.farm_label = p.farm_label AND ft.feed_item_key = p.feed_item_key
    LEFT JOIN load_days ld ON ld.feed_purchase_id = p.feed_purchase_id
    LEFT JOIN burn b ON b.farm_label = p.farm_label AND b.family_key = p.family_key
),
-- DAYS LEFT IS THE RUNWAY, NOT THE SACK. A load is not eaten alone: everything ahead of it in the
-- FIFO queue goes out first, so "days left" on a load is the day the store reaches it and finishes
-- it -- the running kg left, oldest load first, over the family's rate. The NEWEST load of a family
-- therefore reads the family's whole remaining stock over that same rate, which is exactly the
-- figure the stock card above quotes; the two cannot disagree.
running AS (
    SELECT s.*,
           SUM(s.net_kg - s.consumed_kg) OVER (
               PARTITION BY s.farm_label, s.family_key
               ORDER BY s.depletes_from, s.purchase_date, s.batch_no
               ROWS UNBOUNDED PRECEDING) AS runway_kg
    FROM scored s
),
rows_all AS (
    SELECT s.feed_purchase_id, s.farm_label, s.feed_item_label, s.feed_item_key, s.batch_no, s.vendor,
           s.purchase_date, s.reached_on, s.consumption_from, s.finished_on, s.retired,
           CASE
             -- An overrun load -- more directed against it than it held -- is the one being fed
             -- from right now, so it reads as IN USE. Its negative kg left is the finding.
             WHEN s.net_kg - s.consumed_kg < 0 THEN 'in_use'
             WHEN s.consumed_kg >= s.net_kg THEN 'finished'
             WHEN s.consumed_kg > 0 THEN 'in_use'
             ELSE 'not_started'
           END AS status,
           s.net_kg AS purchased_kg,
           s.consumed_kg,
           s.net_kg - s.consumed_kg AS left_kg,
           s.days_of_stock,
           COALESCE(s.days_consumed, 0) AS days_consumed,
           CASE
             WHEN s.avg_daily_kg IS NULL OR s.avg_daily_kg <= 0 THEN NULL
             ELSE GREATEST(floor(s.runway_kg / s.avg_daily_kg), 0)::bigint
           END AS days_left,
           -- The CHECK stays about this load alone: the buyer said how many days THIS load would
           -- cover, so it is compared against this load's own kg over the same rate.
           CASE
             WHEN s.avg_daily_kg IS NULL OR s.avg_daily_kg <= 0 THEN NULL
             ELSE GREATEST(floor((s.net_kg - s.consumed_kg) / s.avg_daily_kg), 0)::bigint
           END AS own_days_left,
           s.avg_daily_kg
    FROM running s
    UNION ALL
    -- On the road: stock_kg is 0 by design (not stock yet), so the row shows the BOUGHT quantity as
    -- what is coming, nothing consumed, and no rate to project days left from.
    SELECT l.feed_purchase_id, l.farm_label, l.feed_item_label, l.feed_item_key, l.batch_no, l.vendor,
           l.purchase_date, NULL, NULL, NULL, l.retired,
           'in_transit', l.quantity_kg, 0, l.quantity_kg, l.days_of_stock, 0, NULL, NULL, NULL
    FROM loads l
    WHERE l.delivery_status <> 'reached'
),
page AS (
    SELECT r.*,
           CASE WHEN r.days_of_stock IS NULL OR r.own_days_left IS NULL THEN NULL
                ELSE r.days_of_stock - r.days_consumed - r.own_days_left END AS gap_days
    FROM rows_all r
    WHERE ($3::text = '' OR r.farm_label = $3)
      AND ($4::text = '' OR r.feed_item_key = $4)
      -- A load the store has finished is history: the table answers what is in the store now.
      AND r.status <> 'finished'
      AND NOT r.retired
)
SELECT feed_purchase_id::text, farm_label, feed_item_label, feed_item_key, batch_no, vendor,
       purchase_date::text,
       COALESCE(reached_on::text, ''), COALESCE(consumption_from::text, ''), COALESCE(finished_on::text, ''),
       status,
       round(purchased_kg, 1)::text, round(consumed_kg, 1)::text, round(left_kg, 1)::text,
       days_of_stock, days_consumed, days_left, gap_days,
       COALESCE(round(avg_daily_kg, 1)::text, ''),
       COUNT(*) OVER () AS total
FROM page
ORDER BY purchase_date DESC, farm_label, feed_item_key, batch_no DESC
LIMIT $5 OFFSET $6`

// The facets name the feeds and farms the table can actually show: the ledger's loads, minus the
// feeds the farm has retired, so the filter cannot select a feed with no row behind it.
const stockLoadFacetScopeSQL = `
    FROM feed_purchases p
    LEFT JOIN unnest($3::text[], $4::text[]) AS m(member_key, family_key) ON m.member_key = p.feed_item_key
    LEFT JOIN feed_item_catalog c
      ON c.tenant_id = $1 AND c.feed_item_key = p.feed_item_key
    LEFT JOIN feed_item_catalog fc
      ON fc.tenant_id = $1 AND fc.feed_item_key = COALESCE(m.family_key, p.feed_item_key)
    WHERE p.tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR p.park_id = ANY ($2::uuid[]))
      AND COALESCE(c.status, 'active') <> 'retired'
      AND COALESCE(fc.status, 'active') <> 'retired'`

// stockLoadFeedItemsSQL lists the feeds the ledger holds loads for, in the caller's park scope.
const stockLoadFeedItemsSQL = `
SELECT p.feed_item_key, MAX(p.feed_item_label)` + stockLoadFacetScopeSQL + `
GROUP BY p.feed_item_key
ORDER BY MAX(p.feed_item_label)`

// stockLoadFarmsSQL lists the farms the ledger holds loads for, in the caller's park scope.
const stockLoadFarmsSQL = `
SELECT DISTINCT p.farm_label` + stockLoadFacetScopeSQL + `
ORDER BY p.farm_label`

// StockLoads serves the purchased-vs-consumed table. Cached on the same stock revision as the
// cards, so a recorded load, an arrival or a sheet lock invalidates both together.
func (r *Repository) StockLoads(ctx context.Context, tenantID string, parkIDs []uuid.UUID, q domain.StockLoadsQuery) (domain.StockLoadsPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limit, offset, err := domain.NormaliseStockLoadsPage(q.Limit, q.Offset)
	if err != nil {
		return domain.StockLoadsPage{}, err
	}
	revision, err := r.feedStockRevision(ctx, tenantID, parkIDs)
	if err != nil {
		return domain.StockLoadsPage{}, err
	}
	cacheKey := feedAnalyticsCacheKey("stock-loads:"+revision, tenantID, domain.DirectedAnalyticsQuery{ParkIDs: parkIDs}) +
		fmt.Sprintf("|%s|%s|%d|%d", q.FarmLabel, q.FeedItemKey, limit, offset)
	cached, err := r.getOrLoadReadCache(ctx, cacheKey, func(ctx context.Context) (any, error) {
		return r.loadStockLoads(ctx, tenantID, parkIDs, q.FarmLabel, q.FeedItemKey, limit, offset)
	})
	if err != nil {
		return domain.StockLoadsPage{}, err
	}
	if out, ok := cached.(domain.StockLoadsPage); ok {
		return out, nil
	}
	return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock loads cache type %T", cached)
}

func (r *Repository) loadStockLoads(ctx context.Context, tenantID string, parkIDs []uuid.UUID, farm, feedKey string, limit, offset int) (domain.StockLoadsPage, error) {
	// The same fold and the same pinned rates the stock cards bind, so the runway this table
	// projects is the runway the card above it quotes.
	mergeMembers, mergeFamilies, _ := domain.StockFamilyMergeArrays()
	rateFarms, rateFeeds, rateKg := domain.StockRateOverrideArrays()
	rows, err := r.pool.Query(ctx, stockLoadsSQL, tenantID, parkIDs, farm, feedKey, limit, offset,
		mergeMembers, mergeFamilies, rateFarms, rateFeeds, rateKg)
	if err != nil {
		return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock loads: %w", err)
	}
	defer rows.Close()
	out := domain.StockLoadsPage{Rows: []domain.StockLoadRow{}, Limit: limit, Offset: offset, FeedItems: []domain.StockLoadFeedItem{}, Farms: []string{}}
	for rows.Next() {
		var (
			row    domain.StockLoadRow
			status string
		)
		if err := rows.Scan(
			&row.FeedPurchaseID, &row.FarmLabel, &row.FeedItemLabel, &row.FeedItemKey, &row.BatchNo, &row.Vendor,
			&row.PurchaseDate, &row.ReachedOn, &row.ConsumptionFrom, &row.FinishedOn,
			&status,
			&row.PurchasedKg, &row.ConsumedKg, &row.LeftKg,
			&row.DaysSaid, &row.DaysConsumed, &row.DaysLeft, &row.GapDays,
			&row.AvgDailyKg,
			&out.Total,
		); err != nil {
			return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock loads scan: %w", err)
		}
		row.Status = domain.StockLoadStatus(status)
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock loads rows: %w", err)
	}
	items, err := r.pool.Query(ctx, stockLoadFeedItemsSQL, tenantID, parkIDs, mergeMembers, mergeFamilies)
	if err != nil {
		return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock load feed items: %w", err)
	}
	defer items.Close()
	for items.Next() {
		var item domain.StockLoadFeedItem
		if err := items.Scan(&item.Key, &item.Label); err != nil {
			return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock load feed items scan: %w", err)
		}
		out.FeedItems = append(out.FeedItems, item)
	}
	if err := items.Err(); err != nil {
		return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock load feed items rows: %w", err)
	}
	farms, err := r.pool.Query(ctx, stockLoadFarmsSQL, tenantID, parkIDs, mergeMembers, mergeFamilies)
	if err != nil {
		return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock load farms: %w", err)
	}
	defer farms.Close()
	for farms.Next() {
		var farm string
		if err := farms.Scan(&farm); err != nil {
			return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock load farms scan: %w", err)
		}
		out.Farms = append(out.Farms, farm)
	}
	if err := farms.Err(); err != nil {
		return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock load farms rows: %w", err)
	}
	if len(out.Rows) == 0 && offset > 0 {
		// A page past the end still owes the whole-filter counts.
		if err := r.pool.QueryRow(ctx, stockLoadsSQL, tenantID, parkIDs, farm, feedKey, 1, 0,
			mergeMembers, mergeFamilies, rateFarms, rateFeeds, rateKg).Scan(
			new(string), new(string), new(string), new(string), new(int64), new(string),
			new(string), new(string), new(string), new(string), new(string),
			new(string), new(string), new(string),
			new(*int64), new(int64), new(*int64), new(*int64), new(string),
			&out.Total,
		); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return domain.StockLoadsPage{}, fmt.Errorf("feed analytics stock loads totals: %w", err)
		}
	}
	return out, nil
}
