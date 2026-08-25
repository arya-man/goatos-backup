package postgres

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/economics/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// SQL row caps, kept in lockstep with domain.MaxAnimalRows / domain.MaxSoldRows
// (queries are consts, so the literals live here; a drift is caught by
// TestRowCapLiteralsMatchDomain).
const (
	maxAnimalRowsSQL = "200"
	maxSoldRowsSQL   = "200"
)

// The CTE chain below mirrors the Growth Director module's weighing reads
// (same identity, round and pair semantics) and the feed-analytics pricing
// LATERAL, so the three screens agree on what a weigh, a gain and a rupee of
// feed mean.
//
// Shared params on every query in this file:
//
//	$1 tenant_id uuid, $2 park_ids uuid[],
//	$3 period start date (inclusive), $4 period end date (EXCLUSIVE)

// weighingObsCTE selects the window's individual weighing scans by
// campaign-week overlap (weighing campaigns are week-grain). Rework captures
// are excluded from all growth math; identity is lower(btrim(tag)).
const weighingObsCTE = `
obs AS (
  SELECT o.observation_id,
         lower(btrim(o.scanned_identifier)) AS tag_key,
         o.weight_kg, o.accepted_at, o.campaign_id,
         c.period_start_date
  FROM weighing_observations o
  JOIN weighing_campaigns c
    ON c.tenant_id = o.tenant_id AND c.campaign_id = o.campaign_id
  WHERE o.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND c.status <> 'canceled'
    AND c.period_end_date >= $3::date
    AND c.period_start_date < $4::date
    AND btrim(o.scanned_identifier) <> ''
    AND o.verification_status <> 'rework'
)`

// roundLatestCTE collapses repeat scans to ONE weigh per (identity, campaign
// week): newest capture wins — the 000073 grain and 000080 reporting convention.
const roundLatestCTE = `
round_latest AS (
  SELECT DISTINCT ON (tag_key, campaign_id)
         tag_key, campaign_id, period_start_date, weight_kg, accepted_at, observation_id
  FROM obs
  ORDER BY tag_key, campaign_id, accepted_at DESC, observation_id DESC
)`

// pairsCTE reduces each identity with >=2 campaign rounds to its first and
// last round-latest weigh.
const pairsCTE = `
pairs AS (
  SELECT tag_key,
         (array_agg(weight_kg   ORDER BY period_start_date, accepted_at, observation_id))[1]                 AS w_first,
         (array_agg(accepted_at ORDER BY period_start_date, accepted_at, observation_id))[1]                 AS t_first,
         (array_agg(weight_kg   ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC))[1]  AS w_last,
         (array_agg(accepted_at ORDER BY period_start_date DESC, accepted_at DESC, observation_id DESC))[1]  AS t_last
  FROM round_latest
  GROUP BY tag_key
  HAVING count(*) >= 2
)`

// adgCTE derives each pair's daily gain. Losses steeper than 0.30 kg/day are
// excluded as bad scans, matching the Growth Director convention.
const adgCTE = `
adg AS (
  SELECT p.*,
         (w_last - w_first) * 1000.0 / (t_last::date - t_first::date) AS adg_g_day,
         (t_last::date - t_first::date) AS span_days
  FROM pairs p
  WHERE t_last::date > t_first::date
    AND (w_last - w_first) * 1000.0 / (t_last::date - t_first::date) > -300
)`

// matchedCTE resolves each paired identity to its LIVE animal through the herd
// register: goat_identifiers.normalized_value is UPPER(trim) while the weighing
// tag key is lower(btrim), so the join is normalized_value = upper(tag_key) —
// lifetime-unique per tenant, 0..1, so it can never fan out. One-hop merge
// redirect; exited animals drop out of the economics table (the sold panel
// carries the sold ones). The pen comes from goat_shed_partitions, whose
// 'whole' sentinel normalizes to the '' the feed sheet rows carry.
const matchedCTE = `
matched AS (
  SELECT a.tag_key, a.w_last, a.adg_g_day, a.span_days,
         COALESCE(g.display_id, '') AS display_id,
         btrim(COALESCE(COALESCE(canon.breed, g.breed), ''))                       AS breed,
         btrim(COALESCE(COALESCE(canon.sex, g.sex), ''))                           AS sex,
         btrim(COALESCE(COALESCE(canon.management_stage, g.management_stage), '')) AS stage,
         COALESCE(canon.shed_id, g.shed_id) AS shed_id,
         COALESCE(canon.park_id, g.park_id) AS park_id,
         COALESCE(NULLIF(gsp.partition_label, 'whole'), '') AS pen
  FROM adg a
  JOIN goat_identifiers gi ON gi.tenant_id = $1::uuid AND gi.normalized_value = upper(a.tag_key)
  JOIN goats g             ON g.tenant_id = $1::uuid AND g.goat_id = gi.goat_id
  LEFT JOIN goats canon    ON canon.tenant_id = $1::uuid AND canon.goat_id = g.merged_into_goat_id
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = $1::uuid AND gsp.goat_id = COALESCE(canon.goat_id, g.goat_id)
  WHERE COALESCE(canon.exited_at, g.exited_at) IS NULL
    AND COALESCE(canon.park_id, g.park_id) = ANY($2::uuid[])
)`

// pricedFeedCTE prices every directed cell in the window at the latest
// same-park purchase load on or before that feed day — the SAME pricing rule
// the Feed Analytics expenditure read uses. quantity_kg is NEVER COALESCEd:
// NULL means blocked and stays out. Experiment / informational-head-count rows
// ARE included: their authored kg is a pen total, and DIVIDING that total by
// the pen's recorded cohort size (feed_day_cell below) is an honest per-head
// estimate — what is banned for those rows is MULTIPLYING grams by heads, the
// operation that would invent a quantity nobody authored. An item with no
// purchase on record prices NOTHING (the LATERAL join drops it) and is counted
// by the unpriced-items disclosure instead of being invented.
const pricedFeedCTE = `
priced_feed AS (
  SELECT r.shed_id, COALESCE(r.partition_label, '') AS pen,
         r.shed_tag_key, r.breed_key, i.feed_day,
         r.quantity_kg * price.per_kg AS rupees, r.head_count
  FROM feed_direction_issue_rows r
  JOIN feed_direction_issues i
    ON i.tenant_id = r.tenant_id AND i.feed_direction_issue_id = r.feed_direction_issue_id
  JOIN LATERAL (
    SELECT COALESCE(p.per_kg_cost, p.total_cost / NULLIF(p.quantity_kg, 0)) AS per_kg
    FROM feed_purchases p
    WHERE p.tenant_id = $1::uuid
      AND p.park_id = i.park_id
      AND p.feed_item_key = r.feed_item_key
      AND p.purchase_date <= i.feed_day
    ORDER BY p.purchase_date DESC, p.batch_no DESC
    LIMIT 1
  ) price ON price.per_kg IS NOT NULL
  WHERE r.tenant_id = $1::uuid
    AND i.park_id = ANY($2::uuid[])
    AND i.state IN ('issued','amended','locked')
    AND i.feed_day >= $3::date
    AND i.feed_day < $4::date
    AND r.quantity_kg IS NOT NULL
)`

// feedCellCTE turns priced cells into a per-head daily cost per feed GRAIN
// CELL (shed, pen, stage tag, breed). head_count repeats on every feed-item
// cell and every session of a day, so it collapses with max() per grain-day
// BEFORE dividing — the same pre-collapse the Growth Director feed read does.
const feedCellCTE = `
feed_day_cell AS (
  SELECT shed_id, pen, shed_tag_key, breed_key, feed_day,
         sum(rupees) AS rupees, max(head_count) AS heads
  FROM priced_feed
  GROUP BY shed_id, pen, shed_tag_key, breed_key, feed_day
),
feed_cell AS (
  SELECT shed_id, pen, shed_tag_key, breed_key,
         avg(rupees / heads)::float8 AS cost_per_head_day
  FROM feed_day_cell
  WHERE heads > 0
  GROUP BY shed_id, pen, shed_tag_key, breed_key
)`

// econCTE joins each matched animal to ITS feed grain cell: the (shed, pen,
// stage tag, breed) cell is the grain the feed sheet feeds this animal under,
// keyed by feed_config_norm() of the animal's own stage and breed — the single
// normalizer the feed chain uses everywhere. A MIXED pen's sheet row carries
// '_+_'-joined composite keys ('osmanabadi_+_malai_+_sojat',
// 'f2_female_+_f2_male') covering the whole cohort one bag feeds, so the match
// is SET MEMBERSHIP, not equality — the row's per-head cost applies to every
// animal in that cohort. The rare animal matching more than one of its pen's
// cells collapses with avg() so econ stays one row per identity. A miss leaves
// cost NULL (honest: no authored+priced ration reaches this animal).
const econCTE = `
econ AS (
  SELECT m.tag_key, m.w_last, m.adg_g_day, m.span_days,
         m.display_id, m.breed, m.sex, m.stage, m.shed_id, m.park_id, m.pen,
         avg(fc.cost_per_head_day) AS cost_per_head_day
  FROM matched m
  LEFT JOIN feed_cell fc
    ON fc.shed_id = m.shed_id
   AND fc.pen = m.pen
   AND feed_config_norm(m.stage) = ANY(string_to_array(fc.shed_tag_key, '_+_'))
   AND feed_config_norm(m.breed) = ANY(string_to_array(fc.breed_key, '_+_'))
  GROUP BY m.tag_key, m.w_last, m.adg_g_day, m.span_days,
           m.display_id, m.breed, m.sex, m.stage, m.shed_id, m.park_id, m.pen
)`

// econChain is the full shared chain, WITH included.
const econChain = `WITH ` + weighingObsCTE + `,
` + roundLatestCTE + `,
` + pairsCTE + `,
` + adgCTE + `,
` + matchedCTE + `,
` + pricedFeedCTE + `,
` + feedCellCTE + `,
` + econCTE

// GetBusinessEconomics builds the whole page for one half-open window.
// parkIDs must be non-empty and already authorization-checked by the caller.
func (r *Repository) GetBusinessEconomics(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.BusinessEconomics, error) {
	loc := biztime.DefaultLocation()
	out := domain.BusinessEconomics{
		Period: domain.Period{
			Start:      periodStart.In(loc).Format("2006-01-02"),
			End:        periodEnd.In(loc).AddDate(0, 0, -1).Format("2006-01-02"),
			Resolution: domain.PeriodResolutionCampaignWeek,
		},
		Parks:    []domain.Park{},
		Pulse:    domain.Pulse{PriceBasis: domain.PriceBasisNone},
		Animals:  []domain.AnimalEconomics{},
		Bands:    emptyBands(),
		Sold:     []domain.SoldAnimal{},
		Estimate: true,
	}
	if len(parkIDs) == 0 {
		return out, nil
	}

	// Business DATES rendered in Asia/Kolkata, never timestamps: casting a
	// timestamptz param to ::date inside SQL would apply the session time zone
	// and shift the boundary a day.
	startDate := periodStart.In(loc).Format("2006-01-02")
	endExclusiveDate := periodEnd.In(loc).Format("2006-01-02")

	parks, err := r.parks(ctx, tenantID, parkIDs)
	if err != nil {
		return out, err
	}
	out.Parks = parks

	realizedPerKg, priceBasis, err := r.realizedPrice(ctx, tenantID, startDate, endExclusiveDate)
	if err != nil {
		return out, err
	}
	out.Pulse.PriceBasis = priceBasis
	out.Pulse.RealizedPricePerKg = realizedPerKg

	if err := r.pulse(ctx, &out.Pulse, tenantID, parkIDs, startDate, endExclusiveDate, realizedPerKg); err != nil {
		return out, err
	}
	if out.Animals, err = r.animals(ctx, tenantID, parkIDs, startDate, endExclusiveDate, realizedPerKg); err != nil {
		return out, err
	}
	if out.Bands, err = r.bands(ctx, tenantID, parkIDs, startDate, endExclusiveDate, realizedPerKg); err != nil {
		return out, err
	}
	if out.Sold, err = r.sold(ctx, tenantID, parkIDs, startDate, endExclusiveDate); err != nil {
		return out, err
	}
	return out, nil
}

func emptyBands() []domain.BandEconomics {
	bands := make([]domain.BandEconomics, 0, len(domain.BandLabels))
	for _, label := range domain.BandLabels {
		bands = append(bands, domain.BandEconomics{Band: label})
	}
	return bands
}

// realizedPrice answers "what does a kg of live animal actually sell for":
// closed live-animal revenue over live weight sold. The window's own deals are
// preferred; an empty window falls back to the trailing 365 days ending at the
// window end, disclosed via price_basis; empty even there returns nil.
//
// Deal figures are deliberately TENANT-WIDE: the sales ledger records a farm
// label, not a park id, so a park filter narrows animal and feed figures only.
//
// projection-review: membership=closed weighed live-animal deals in one date
// range; group_key=none (two plain SUMs over the same predicate — numerator
// and denominator range over the identical deal set); join_cardinality=single
// table, no join; pagination=NONE (one row); scope=tenant + sale_date range.
func (r *Repository) realizedPrice(ctx context.Context, tenantID, startDate, endExclusiveDate string) (*float64, string, error) {
	const q = `
SELECT sum(sales_value)::float8, sum(total_weight_kg)::float8
FROM sales_deals
WHERE tenant_id = $1::uuid
  AND status = 'Deal Closed'
  AND product_type IN ('Sheep','Goat')
  AND sales_value > 0
  AND total_weight_kg > 0
  AND sale_date >= $2::date
  AND sale_date < $3::date`
	windowPrice, err := r.realizedPriceOnce(ctx, q, tenantID, startDate, endExclusiveDate)
	if err != nil {
		return nil, domain.PriceBasisNone, err
	}
	if windowPrice != nil {
		return windowPrice, domain.PriceBasisWindow, nil
	}
	trailingStart, err := time.ParseInLocation("2006-01-02", endExclusiveDate, biztime.DefaultLocation())
	if err != nil {
		return nil, domain.PriceBasisNone, err
	}
	trailing, err := r.realizedPriceOnce(ctx, q, tenantID, trailingStart.AddDate(-1, 0, 0).Format("2006-01-02"), endExclusiveDate)
	if err != nil {
		return nil, domain.PriceBasisNone, err
	}
	if trailing != nil {
		return trailing, domain.PriceBasisTrailingYear, nil
	}
	return nil, domain.PriceBasisNone, nil
}

func (r *Repository) realizedPriceOnce(ctx context.Context, q, tenantID, startDate, endExclusiveDate string) (*float64, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	var value, weight *float64
	if err := r.pool.QueryRow(ctx, q, tenantID, startDate, endExclusiveDate).Scan(&value, &weight); err != nil {
		return nil, err
	}
	if value == nil || weight == nil || *weight <= 0 {
		return nil, nil
	}
	perKg := *value / *weight
	return &perKg, nil
}

// pulse fills the headline strip: growth-side denominators and medians off the
// shared econ chain, the whole-farm feed burn, the unpriced-item disclosure and
// the window's closed-deal figures.
func (r *Repository) pulse(ctx context.Context, out *domain.Pulse, tenantID string, parkIDs []string, startDate, endExclusiveDate string, realizedPerKg *float64) error {
	if err := r.pulseGrowth(ctx, out, tenantID, parkIDs, startDate, endExclusiveDate, realizedPerKg); err != nil {
		return err
	}
	if err := r.pulseBurn(ctx, out, tenantID, parkIDs, startDate, endExclusiveDate); err != nil {
		return err
	}
	return r.pulseDeals(ctx, out, tenantID, startDate, endExclusiveDate)
}

// projection-review: membership=paired matched live animals of the window
// (econ CTE) plus the distinct weighed identities of round_latest;
// group_key=whole-set aggregates, no GROUP BY — every FILTERed aggregate and
// the median range over the same econ row set; join_cardinality=goat_identifiers
// 0..1 (lifetime-unique), goats 1 (PK), canon 0..1 (PK), goat_shed_partitions
// 0..1 (PK tenant,goat), feed_cell 0..1 (grouped on its full join key), so no
// econ row multiplies; pagination=NONE (one row); scope=tenant + park ANY +
// campaign-week overlap window.
//
// scale-guard:ignore: 5k-50k-envelope — bounded windowed aggregate over the
// same chain shape as the Growth Director read.
func (r *Repository) pulseGrowth(ctx context.Context, out *domain.Pulse, tenantID string, parkIDs []string, startDate, endExclusiveDate string, realizedPerKg *float64) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	const q = econChain + `
SELECT
  (SELECT count(DISTINCT tag_key) FROM round_latest) AS weighed_identities,
  count(*) AS paired_matched,
  count(*) FILTER (WHERE cost_per_head_day IS NOT NULL AND adg_g_day > 0) AS cost_animals,
  (percentile_cont(0.5) WITHIN GROUP (ORDER BY cost_per_head_day / (adg_g_day / 1000.0))
     FILTER (WHERE cost_per_head_day IS NOT NULL AND adg_g_day > 0))::float8 AS median_cost_per_kg_gain,
  sum(adg_g_day / 1000.0) FILTER (WHERE adg_g_day > 0)::float8 AS total_gain_kg_per_day
FROM econ`
	var totalGainKgPerDay *float64
	if err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, startDate, endExclusiveDate).Scan(
		&out.WeighedIdentities, &out.PairedAnimals, &out.CostAnimals,
		&out.MedianCostPerKgGain, &totalGainKgPerDay,
	); err != nil {
		return err
	}
	if totalGainKgPerDay != nil && realizedPerKg != nil {
		v := *totalGainKgPerDay * *realizedPerKg
		out.ValueAddedPerDayRupees = &v
	}
	return nil
}

// pulseBurn is the whole-farm daily feed spend: every directed cell — normal
// AND experiment, plus non-sheet external consumption — priced at the latest
// load and summed per day, averaged over the days that have a sheet. Same
// day_item ∪ external shape and pricing LATERAL as the Feed Analytics
// expenditure read, so the two screens report the same rupees. It also counts
// the feed items whose directed kg found NO purchase to price them.
//
// projection-review: membership=priced (feed_day, park, feed_item) directed
// cells of the window plus external consumption; group_key=feed_day for the
// per-day sum, then a whole-set avg — numerator days and denominator days are
// the same day_total set; join_cardinality=issues to rows 1:N aggregated
// before the union, price LATERAL 0..1 per (park,item,day); pagination=NONE
// (one row); scope=tenant + park ANY + feed_day half-open window.
//
// scale-guard:ignore: 5k-50k-envelope — bounded windowed aggregate, the same
// shape as feeddirection's stockExpenditureSQL.
func (r *Repository) pulseBurn(ctx context.Context, out *domain.Pulse, tenantID string, parkIDs []string, startDate, endExclusiveDate string) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	const q = `
WITH day_item AS (
    SELECT feed_day, park_id, feed_item_key, SUM(kg) AS kg
    FROM (
        SELECT i.feed_day, i.park_id, r.feed_item_key, SUM(r.quantity_kg) AS kg
        FROM feed_direction_issues i
        JOIN feed_direction_issue_rows r
          ON r.tenant_id = $1::uuid AND r.feed_direction_issue_id = i.feed_direction_issue_id
        WHERE i.tenant_id = $1::uuid
          AND i.park_id = ANY($2::uuid[])
          AND i.state IN ('issued','amended','locked')
          AND i.feed_day >= $3::date AND i.feed_day < $4::date
          -- A blocked cell (NULL) directed nothing: it is a feed PROBLEM, not an
          -- unpriced purchase, and must not surface in the unpriced-items count.
          AND r.quantity_kg IS NOT NULL
        GROUP BY i.feed_day, i.park_id, r.feed_item_key
        UNION ALL
        SELECT x.feed_day, x.park_id, x.feed_item_key, SUM(x.quantity_kg) AS kg
        FROM feed_external_consumption x
        WHERE x.tenant_id = $1::uuid
          AND x.park_id IS NOT NULL
          AND x.park_id = ANY($2::uuid[])
          AND x.feed_day >= $3::date AND x.feed_day < $4::date
        GROUP BY x.feed_day, x.park_id, x.feed_item_key
    ) both_sources
    GROUP BY feed_day, park_id, feed_item_key
),
priced AS (
    SELECT di.feed_day, di.feed_item_key, di.kg, di.kg * price.per_kg AS rupees
    FROM day_item di
    LEFT JOIN LATERAL (
        SELECT COALESCE(p.per_kg_cost, p.total_cost / NULLIF(p.quantity_kg, 0)) AS per_kg
        FROM feed_purchases p
        WHERE p.tenant_id = $1::uuid
          AND p.park_id = di.park_id
          AND p.feed_item_key = di.feed_item_key
          AND p.purchase_date <= di.feed_day
        ORDER BY p.purchase_date DESC, p.batch_no DESC
        LIMIT 1
    ) price ON true
),
day_total AS (
    SELECT feed_day, SUM(rupees) AS rupees
    FROM priced
    WHERE rupees IS NOT NULL
    GROUP BY feed_day
)
SELECT (SELECT avg(rupees)::float8 FROM day_total) AS burn_per_day,
       -- An authored zero directs no kg, so it needs no price either.
       (SELECT count(DISTINCT feed_item_key) FROM priced WHERE rupees IS NULL AND kg > 0) AS unpriced_items`
	return r.pool.QueryRow(ctx, q, tenantID, parkIDs, startDate, endExclusiveDate).Scan(
		&out.FeedCostPerDayRupees, &out.UnpricedFeedItems,
	)
}

// projection-review: membership=closed live-animal deals of the window;
// group_key=none, three plain aggregates over the same predicate;
// join_cardinality=single table; pagination=NONE (one row); scope=tenant +
// sale_date range (tenant-wide by design — see realizedPrice).
func (r *Repository) pulseDeals(ctx context.Context, out *domain.Pulse, tenantID, startDate, endExclusiveDate string) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	const q = `
SELECT count(*),
       COALESCE(sum(animal_count), 0)::float8,
       COALESCE(sum(sales_value), 0)::float8
FROM sales_deals
WHERE tenant_id = $1::uuid
  AND status = 'Deal Closed'
  AND product_type IN ('Sheep','Goat')
  AND sale_date >= $2::date
  AND sale_date < $3::date`
	var animals float64
	if err := r.pool.QueryRow(ctx, q, tenantID, startDate, endExclusiveDate).Scan(
		&out.ClosedDeals, &animals, &out.SoldRevenueRupees,
	); err != nil {
		return err
	}
	out.AnimalsSold = int(animals)
	return nil
}

// animals serves the per-animal economics table: worst daily net first (the
// burners the CEO should look at), capped at MaxAnimalRows with the pulse
// carrying the uncapped denominators. $5 is the realized price per kg (may be
// NULL), used only for the ORDER so SQL and Go agree on "worst".
//
// projection-review: membership=paired matched live animals (econ CTE);
// group_key=tag_key — econ is one row per identity by construction (pairs
// groups by tag_key, every later join is 0..1: goat_identifiers lifetime-
// unique, goats PK, canon PK, gsp PK, feed_cell grouped on its full join key,
// shed/park locations PK); join_cardinality=all 0..1 as listed, no fan-out;
// pagination=LIMIT MaxAnimalRows worst-net-first, summaries computed
// independently in pulseGrowth so the cap never bends a total; scope=tenant +
// park ANY + campaign-week overlap window.
//
// scale-guard:ignore: 5k-50k-envelope — bounded windowed read, same chain
// shape as the Growth Director widgets.
func (r *Repository) animals(ctx context.Context, tenantID string, parkIDs []string, startDate, endExclusiveDate string, realizedPerKg *float64) ([]domain.AnimalEconomics, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	out := []domain.AnimalEconomics{}
	const q = econChain + `
SELECT e.tag_key, e.display_id, e.breed, e.sex, e.stage,
       COALESCE(shed.name, '') AS shed_name, e.pen, COALESCE(park.name, '') AS park_name,
       e.w_last::float8, e.adg_g_day::float8, e.span_days,
       e.cost_per_head_day
FROM econ e
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = e.shed_id
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = e.park_id
ORDER BY CASE WHEN $5::float8 IS NOT NULL AND e.cost_per_head_day IS NOT NULL AND e.adg_g_day > 0
              THEN (e.adg_g_day / 1000.0) * $5::float8 - e.cost_per_head_day END ASC NULLS LAST,
         e.cost_per_head_day DESC NULLS LAST,
         e.tag_key
LIMIT ` + maxAnimalRowsSQL
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endExclusiveDate, realizedPerKg)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var row domain.AnimalEconomics
		var shedName, parkName, pen string
		var spanDays int
		if err := rows.Scan(
			&row.TagDisplay, &row.DisplayID, &row.Breed, &row.Sex, &row.Stage,
			&shedName, &pen, &parkName,
			&row.LatestWeightKg, &row.ADGGPerDay, &spanDays,
			&row.FeedCostPerDayRupees,
		); err != nil {
			return out, err
		}
		row.SpanDays = spanDays
		row.ShedDisplay = operationalLabel(parkName, shedName, pen)
		deriveAnimalMoney(&row, realizedPerKg)
		out = append(out, row)
	}
	return out, rows.Err()
}

// deriveAnimalMoney fills the derived money fields and the signal. Null never
// means zero: a missing side leaves the derived field nil and the signal on
// "watch".
func deriveAnimalMoney(row *domain.AnimalEconomics, realizedPerKg *float64) {
	gainKgPerDay := row.ADGGPerDay / 1000.0
	if row.FeedCostPerDayRupees != nil && gainKgPerDay > 0 {
		v := *row.FeedCostPerDayRupees / gainKgPerDay
		row.CostPerKgGainRupees = &v
	}
	if realizedPerKg != nil && gainKgPerDay > 0 {
		v := gainKgPerDay * *realizedPerKg
		row.ValueAddedPerDayRupees = &v
	}
	row.Signal = domain.SignalWatch
	if row.FeedCostPerDayRupees != nil && row.ValueAddedPerDayRupees != nil {
		net := *row.ValueAddedPerDayRupees - *row.FeedCostPerDayRupees
		row.NetPerDayRupees = &net
		if net > 0 {
			row.Signal = domain.SignalEarning
		} else {
			row.Signal = domain.SignalBurning
		}
	}
}

// bands is the break-even read at weight-band grain, always all six bands.
//
// projection-review: membership=paired matched live animals (econ CTE), same
// one-row-per-identity grain proven on animals(); group_key=width_bucket of
// the latest weight — count, both medians and the filtered median all range
// over exactly the band's econ rows; join_cardinality=inherited from econ, no
// new join; pagination=NONE, at most six band rows; scope=tenant + park ANY +
// campaign-week overlap window.
//
// scale-guard:ignore: 5k-50k-envelope — bounded windowed aggregate, same chain
// shape as the Growth Director widgets.
func (r *Repository) bands(ctx context.Context, tenantID string, parkIDs []string, startDate, endExclusiveDate string, realizedPerKg *float64) ([]domain.BandEconomics, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	out := emptyBands()
	const q = econChain + `
SELECT width_bucket(w_last, ARRAY[15,20,25,30,35]::numeric[]) AS band_idx,
       count(*) AS animals,
       (percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_day))::float8 AS median_adg,
       (percentile_cont(0.5) WITHIN GROUP (ORDER BY cost_per_head_day)
          FILTER (WHERE cost_per_head_day IS NOT NULL))::float8 AS median_cost_per_day
FROM econ
GROUP BY band_idx
ORDER BY band_idx`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endExclusiveDate)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var bandIdx, animals int
		var medianADG, medianCost *float64
		if err := rows.Scan(&bandIdx, &animals, &medianADG, &medianCost); err != nil {
			return out, err
		}
		if bandIdx < 0 || bandIdx >= len(out) {
			continue
		}
		band := &out[bandIdx]
		band.Animals = animals
		band.MedianADGGPerDay = medianADG
		band.FeedCostPerDayRupees = medianCost
		if medianADG != nil && realizedPerKg != nil && *medianADG > 0 {
			v := (*medianADG / 1000.0) * *realizedPerKg
			band.ValueAddedPerDayRupees = &v
		}
		if band.ValueAddedPerDayRupees != nil && medianCost != nil {
			net := *band.ValueAddedPerDayRupees - *medianCost
			band.NetPerDayRupees = &net
			band.SellSignal = net <= 0
		}
	}
	return out, rows.Err()
}

// sold lists the window's sale-tagged animals through the identity-owned
// goat_sale_allocations mapping, joined to the deal row by its OPAQUE id — the
// exact read path migration 000177 describes; sales still reads no herd table
// and identity still stores no price.
//
// projection-review: membership=live ('tagged') allocations whose deal
// sale_date (allocation day when the deal row is missing) falls in the window;
// group_key=allocation row — deal_counts is pre-aggregated to one row per
// sales_deal_id over ALL live allocations of those deals (the apportioning
// denominator deliberately ignores the window: a deal's value splits across
// every animal on it, not the window's slice), last_weight is DISTINCT ON
// (tag_key) so 0..1, deals/locations join on PKs 0..1 — no side multiplies;
// pagination=LIMIT MaxSoldRows newest sale first, the pulse deal figures are
// computed independently; scope=tenant + snapshot park ANY (NULL-park rows
// kept: the caller here is tenant-wide by permission) + window.
//
// scale-guard:ignore: 5k-50k-envelope — the last_weight scan is one bounded
// pass over the tenant's weighing observations, the same whole-history shape
// the weighing demographics export uses.
func (r *Repository) sold(ctx context.Context, tenantID string, parkIDs []string, startDate, endExclusiveDate string) ([]domain.SoldAnimal, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	out := []domain.SoldAnimal{}
	const q = `
WITH alloc AS (
  SELECT a.tag_number, a.sales_deal_id, a.shed_id, a.park_id,
         COALESCE(NULLIF(a.partition_label, 'whole'), '') AS pen,
         a.allocated_at
  FROM goat_sale_allocations a
  WHERE a.tenant_id = $1::uuid
    AND a.status = 'tagged'
    AND (a.park_id IS NULL OR a.park_id = ANY($2::uuid[]))
),
deal_counts AS (
  SELECT sales_deal_id, count(*) AS live_allocs
  FROM goat_sale_allocations
  WHERE tenant_id = $1::uuid AND status = 'tagged'
  GROUP BY sales_deal_id
),
last_weight AS (
  SELECT DISTINCT ON (tag_key) tag_key, weight_kg
  FROM (
    SELECT lower(btrim(o.scanned_identifier)) AS tag_key, o.weight_kg, o.accepted_at, o.observation_id
    FROM weighing_observations o
    WHERE o.tenant_id = $1::uuid
      AND btrim(o.scanned_identifier) <> ''
      AND o.verification_status <> 'rework'
  ) t
  ORDER BY tag_key, accepted_at DESC, observation_id DESC
)
SELECT al.tag_number,
       COALESCE(d.sale_date::text, (al.allocated_at AT TIME ZONE 'Asia/Kolkata')::date::text) AS sale_date,
       COALESCE(d.buyer_name, '') AS buyer_name,
       COALESCE(d.farm, '') AS farm,
       COALESCE(dc.live_allocs, 0),
       d.sales_value::float8,
       COALESCE(sh.name, '') AS shed_name, al.pen, COALESCE(pk.name, '') AS park_name,
       lw.weight_kg::float8
FROM alloc al
LEFT JOIN sales_deals d  ON d.tenant_id = $1::uuid AND d.id = al.sales_deal_id
LEFT JOIN deal_counts dc ON dc.sales_deal_id = al.sales_deal_id
LEFT JOIN locations sh   ON sh.tenant_id = $1::uuid AND sh.location_id = al.shed_id
LEFT JOIN locations pk   ON pk.tenant_id = $1::uuid AND pk.location_id = al.park_id
LEFT JOIN last_weight lw ON lw.tag_key = lower(btrim(al.tag_number))
WHERE COALESCE(d.sale_date, (al.allocated_at AT TIME ZONE 'Asia/Kolkata')::date) >= $3::date
  AND COALESCE(d.sale_date, (al.allocated_at AT TIME ZONE 'Asia/Kolkata')::date) < $4::date
ORDER BY COALESCE(d.sale_date, (al.allocated_at AT TIME ZONE 'Asia/Kolkata')::date) DESC, al.allocated_at DESC, al.tag_number
LIMIT ` + maxSoldRowsSQL
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endExclusiveDate)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var row domain.SoldAnimal
		var salesValue, lastWeight *float64
		var shedName, pen, parkName string
		if err := rows.Scan(
			&row.TagNumber, &row.SaleDate, &row.BuyerName, &row.Farm,
			&row.DealAnimals, &salesValue,
			&shedName, &pen, &parkName,
			&lastWeight,
		); err != nil {
			return out, err
		}
		row.ShedDisplay = operationalLabel(parkName, shedName, pen)
		row.LastWeightKg = lastWeight
		if salesValue != nil && row.DealAnimals > 0 {
			v := *salesValue / float64(row.DealAnimals)
			row.ApportionedRevenueRupees = &v
			if lastWeight != nil && *lastWeight > 0 {
				perKg := v / *lastWeight
				row.RealizedPerKg = &perKg
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
