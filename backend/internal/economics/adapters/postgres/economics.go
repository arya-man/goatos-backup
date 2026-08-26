package postgres

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/economics/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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

// adgCTE derives each pair's daily gain, applying the Growth Director's TWO
// weighing conventions. Both matter more here than anywhere else, because this
// module DIVIDES BY the gain and a near-zero denominator turns scale noise into
// a confident-looking rupee figure:
//
//  1. A change within 3% of starting body weight is gut fill or scale drift,
//     not growth, and is scored FLAT (0 g/day) — the same rule and threshold
//     as growthdirector's slow-growth read. On the live herd this is 21% of
//     all pairs (68 of 326); without the floor those rendered as "₹10,149 per
//     kg of gain", which reads as precision and is noise.
//  2. Losses steeper than 0.30 kg/day are excluded as bad scans.
//
// A flat animal KEEPS its row — its feed cost is real and worth seeing — but
// gets no cost-per-kg and no value-added, because a gain that was not measured
// cannot be priced. That is what the "Watch" verdict says.
const adgCTE = `
adg AS (
  SELECT p.*,
         CASE WHEN abs(w_last - w_first) <= 0.03 * w_first THEN 0
              ELSE (w_last - w_first) * 1000.0 / (t_last::date - t_first::date) END AS adg_g_day,
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
// 'whole' sentinel normalizes to the ” the feed sheet rows carry.
const matchedCTE = `
matched AS (
  SELECT a.tag_key, a.w_last, a.adg_g_day, a.span_days,
         COALESCE(g.display_id, '') AS display_id,
         btrim(COALESCE(COALESCE(canon.breed, g.breed), ''))                       AS breed,
         btrim(COALESCE(COALESCE(canon.sex, g.sex), ''))                           AS sex,
         btrim(COALESCE(COALESCE(canon.management_stage, g.management_stage), '')) AS stage,
         -- feed_config_norm() ONCE PER ROW here, never inside a join condition:
         -- as a join-time expression the planner re-evaluates it per probe pair,
         -- which turned this read into minutes of plpgsql calls.
         feed_config_norm(COALESCE(COALESCE(canon.management_stage, g.management_stage), '')) AS stage_key,
         feed_config_norm(COALESCE(COALESCE(canon.breed, g.breed), ''))                       AS breed_key,
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
// is SET MEMBERSHIP: feed_cell_members unnests each cell's two key lists into
// plain (tag member × breed member) rows so the membership test becomes a
// HASHABLE equi-join — the array-membership form evaluated these per probe
// pair and was catastrophically slow under a generic plan. The rare animal
// matching more than one of its pen's cells collapses with avg() so econ stays
// one row per identity. A miss leaves cost NULL (honest: no authored+priced
// ration reaches this animal).
const econCTE = `
feed_cell_members AS (
  SELECT fc.shed_id, fc.pen, tag_member, breed_member, fc.cost_per_head_day
  FROM feed_cell fc
  CROSS JOIN LATERAL unnest(string_to_array(fc.shed_tag_key, '_+_')) AS tag_member
  CROSS JOIN LATERAL unnest(string_to_array(fc.breed_key, '_+_'))    AS breed_member
),
econ AS (
  SELECT m.tag_key, m.w_last, m.adg_g_day, m.span_days,
         m.display_id, m.breed, m.sex, m.stage, m.shed_id, m.park_id, m.pen,
         avg(fcm.cost_per_head_day) AS cost_per_head_day
  FROM matched m
  LEFT JOIN feed_cell_members fcm
    ON fcm.shed_id = m.shed_id
   AND fcm.pen = m.pen
   AND fcm.tag_member = m.stage_key
   AND fcm.breed_member = m.breed_key
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
		Sheds:    []domain.ShedEconomics{},
		Breeds:   []domain.BreedEconomics{},
		Bands:    emptyBands(),
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

	// ONE execution of the heavy weighing+feed chain serves the animal table,
	// the bands and the pulse growth figures alike: econ is at most one row per
	// paired identity (bounded by the 5k-50k envelope), so aggregating those
	// rows in Go is cheap, while re-running the chain per widget was three
	// multi-second scans for the same answer.
	econRows, err := r.econRows(ctx, tenantID, parkIDs, startDate, endExclusiveDate)
	if err != nil {
		return out, err
	}
	assembleGrowth(&out, econRows, realizedPerKg)

	if out.Pulse.WeighedIdentities, err = r.weighedIdentities(ctx, tenantID, parkIDs, startDate, endExclusiveDate); err != nil {
		return out, err
	}
	if err := r.pulseBurn(ctx, &out.Pulse, tenantID, parkIDs, startDate, endExclusiveDate); err != nil {
		return out, err
	}
	if err := r.pulseDeals(ctx, &out.Pulse, tenantID, startDate, endExclusiveDate); err != nil {
		return out, err
	}
	return out, nil
}

// econRow is one paired, matched, live animal with its (possibly missing) feed
// cost — the single grain every growth-side widget is derived from.
type econRow struct {
	tag            string
	shedID         string
	displayID      string
	breed          string
	sex            string
	stage          string
	shedName       string
	pen            string
	parkName       string
	latestWeightKg float64
	adgGPerDay     float64
	spanDays       int
	costPerHeadDay *float64
}

// econRows runs the shared chain ONCE and returns every econ row with its
// display names resolved.
//
// projection-review: membership=paired matched live animals of the window;
// group_key=tag_key — econ is one row per identity by construction (pairs
// groups by tag_key, every later join is 0..1: goat_identifiers lifetime-
// unique, goats PK, canon PK, gsp PK, feed_cell_members grouped back to one
// row by the avg() over the animal's own grain-cell memberships, shed/park
// locations PK); join_cardinality=all 0..1 as listed, no fan-out;
// pagination=NONE here — the full bounded row set is fetched once and the
// 200-row table cap is applied in Go AFTER the whole-filter aggregates are
// taken from the same rows, so the cap can never bend a summary;
// scope=tenant + park ANY + campaign-week overlap window.
//
// scale-guard:ignore: 5k-50k-envelope — bounded windowed read, same chain
// shape as the Growth Director widgets.
func (r *Repository) econRows(ctx context.Context, tenantID string, parkIDs []string, startDate, endExclusiveDate string) ([]econRow, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	const q = econChain + `
SELECT e.tag_key, e.display_id, e.breed, e.sex, e.stage,
       COALESCE(e.shed_id::text, '') AS shed_id,
       COALESCE(shed.name, '') AS shed_name, e.pen, COALESCE(park.name, '') AS park_name,
       e.w_last::float8, e.adg_g_day::float8, e.span_days,
       e.cost_per_head_day
FROM econ e
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = e.shed_id
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = e.park_id`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, startDate, endExclusiveDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []econRow{}
	for rows.Next() {
		var row econRow
		if err := rows.Scan(
			&row.tag, &row.displayID, &row.breed, &row.sex, &row.stage,
			&row.shedID, &row.shedName, &row.pen, &row.parkName,
			&row.latestWeightKg, &row.adgGPerDay, &row.spanDays,
			&row.costPerHeadDay,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// assembleGrowth derives the shed table, the breed comparison, the bands and
// the pulse growth figures from the ONE econ row set, so every surface on the
// page agrees by construction.
func assembleGrowth(out *domain.BusinessEconomics, rows []econRow, realizedPerKg *float64) {
	costPerKg := make([]float64, 0, len(rows))
	// The two comparable tiles range over the PRICED set: every animal whose
	// ration cell resolved and priced. A priced animal that did not measurably
	// grow contributes its full cost and ZERO value — that is the point.
	var pricedFeedPerDay, pricedGainKgPerDay float64
	var pricedAnimals int

	bandRows := make([][]econRow, len(domain.BandLabels))
	shedGroups := map[string][]econRow{}
	shedOrder := []string{}
	breedGroups := map[string][]econRow{}
	breedOrder := []string{}

	for _, row := range rows {
		idx := weightBandIndex(row.latestWeightKg)
		bandRows[idx] = append(bandRows[idx], row)

		// Group keys are the STABLE identity, never the display label: shed name
		// repeats across parks, so keying on it would merge two parks' pens into
		// one row (the operational-location rule's name-keying defect).
		shedKey := row.shedID + "\x00" + row.pen
		if _, seen := shedGroups[shedKey]; !seen {
			shedOrder = append(shedOrder, shedKey)
		}
		shedGroups[shedKey] = append(shedGroups[shedKey], row)

		if breed := strings.TrimSpace(row.breed); breed != "" {
			if _, seen := breedGroups[breed]; !seen {
				breedOrder = append(breedOrder, breed)
			}
			breedGroups[breed] = append(breedGroups[breed], row)
		}

		if row.costPerHeadDay != nil {
			pricedAnimals++
			pricedFeedPerDay += *row.costPerHeadDay
			if row.adgGPerDay > 0 {
				pricedGainKgPerDay += row.adgGPerDay / 1000.0
				costPerKg = append(costPerKg, *row.costPerHeadDay/(row.adgGPerDay/1000.0))
			}
		}
	}

	out.Pulse.PairedAnimals = len(rows)
	out.Pulse.PricedAnimals = pricedAnimals
	out.Pulse.CostAnimals = len(costPerKg)
	if median, ok := medianOf(costPerKg); ok {
		out.Pulse.MedianCostPerKgGain = &median
	}
	if pricedAnimals > 0 {
		feed := pricedFeedPerDay
		out.Pulse.FeedCostPerDayRupees = &feed
		if realizedPerKg != nil {
			value := pricedGainKgPerDay * *realizedPerKg
			out.Pulse.ValueAddedPerDayRupees = &value
			net := value - feed
			out.Pulse.NetPerDayRupees = &net
		}
	}

	// Sheds, worst net first — the pens costing money lead.
	sheds := make([]domain.ShedEconomics, 0, len(shedOrder))
	for _, key := range shedOrder {
		members := shedGroups[key]
		group := groupEconomics(members, realizedPerKg)
		if group.Animals == 0 {
			continue
		}
		sheds = append(sheds, domain.ShedEconomics{
			LocationID:     members[0].shedID,
			PartitionLabel: members[0].pen,
			ShedDisplay:    operationalLabel(members[0].parkName, members[0].shedName, members[0].pen),
			GroupEconomics: group,
		})
	}
	sort.SliceStable(sheds, func(i, j int) bool {
		if less, decided := netLess(sheds[i].GroupEconomics, sheds[j].GroupEconomics); decided {
			return less
		}
		return sheds[i].ShedDisplay < sheds[j].ShedDisplay
	})
	if len(sheds) > domain.MaxGroupRows {
		sheds = sheds[:domain.MaxGroupRows]
	}
	out.Sheds = sheds

	// Breeds, BEST net first: this is the "which breed pays for its feed"
	// question, so the answer leads.
	breeds := make([]domain.BreedEconomics, 0, len(breedOrder))
	for _, breed := range breedOrder {
		group := groupEconomics(breedGroups[breed], realizedPerKg)
		if group.Animals == 0 {
			continue
		}
		breeds = append(breeds, domain.BreedEconomics{Breed: breed, GroupEconomics: group})
	}
	sort.SliceStable(breeds, func(i, j int) bool {
		if less, decided := netLess(breeds[j].GroupEconomics, breeds[i].GroupEconomics); decided {
			return less
		}
		return breeds[i].Breed < breeds[j].Breed
	})
	if len(breeds) > domain.MaxGroupRows {
		breeds = breeds[:domain.MaxGroupRows]
	}
	out.Breeds = breeds

	bands := emptyBands()
	for idx, members := range bandRows {
		band := &bands[idx]
		band.Animals = len(members)
		adgs := make([]float64, 0, len(members))
		costs := make([]float64, 0, len(members))
		for _, row := range members {
			adgs = append(adgs, row.adgGPerDay)
			if row.costPerHeadDay != nil {
				costs = append(costs, *row.costPerHeadDay)
			}
		}
		if median, ok := medianOf(adgs); ok {
			band.MedianADGGPerDay = &median
		}
		if median, ok := medianOf(costs); ok {
			band.FeedCostPerDayRupees = &median
		}
		if band.MedianADGGPerDay != nil && realizedPerKg != nil && *band.MedianADGGPerDay > 0 {
			v := (*band.MedianADGGPerDay / 1000.0) * *realizedPerKg
			band.ValueAddedPerDayRupees = &v
		}
		if band.ValueAddedPerDayRupees != nil && band.FeedCostPerDayRupees != nil {
			net := *band.ValueAddedPerDayRupees - *band.FeedCostPerDayRupees
			band.NetPerDayRupees = &net
			band.SellSignal = net <= 0
		}
	}
	out.Bands = bands
}

// groupEconomics reduces one pen's or breed's animals to PER-HEAD-PER-DAY
// means. Only PRICED animals count: an animal whose ration never resolved has
// no cost, so including it would divide a real cost across a head it cannot
// account for. A flat animal IS included, at its full cost and zero gain —
// a pen that is not growing must read as not growing.
func groupEconomics(members []econRow, realizedPerKg *float64) domain.GroupEconomics {
	var feedSum, gainSum float64
	n := 0
	for _, row := range members {
		if row.costPerHeadDay == nil {
			continue
		}
		n++
		feedSum += *row.costPerHeadDay
		if row.adgGPerDay > 0 {
			gainSum += row.adgGPerDay
		}
	}
	group := domain.GroupEconomics{Animals: n, Signal: domain.SignalWatch}
	if n == 0 {
		return group
	}
	feed := feedSum / float64(n)
	gain := gainSum / float64(n)
	group.FeedCostPerDayRupees = &feed
	group.ADGGPerDay = &gain
	if gain > 0 {
		costPerKg := feed / (gain / 1000.0)
		group.CostPerKgGainRupees = &costPerKg
	}
	if realizedPerKg != nil {
		value := (gain / 1000.0) * *realizedPerKg
		group.ValueAddedPerDayRupees = &value
		net := value - feed
		group.NetPerDayRupees = &net
		if net > 0 {
			group.Signal = domain.SignalEarning
		} else {
			group.Signal = domain.SignalBurning
		}
	}
	return group
}

// netLess orders two groups worst-net-first, reporting whether the comparison
// was decided so the caller can fall back to a stable label order.
func netLess(a, b domain.GroupEconomics) (less bool, decided bool) {
	switch {
	case a.NetPerDayRupees != nil && b.NetPerDayRupees != nil:
		if *a.NetPerDayRupees == *b.NetPerDayRupees {
			return false, false
		}
		return *a.NetPerDayRupees < *b.NetPerDayRupees, true
	case (a.NetPerDayRupees != nil) != (b.NetPerDayRupees != nil):
		return a.NetPerDayRupees != nil, true
	default:
		return false, false
	}
}

// weightBandIndex mirrors SQL width_bucket over the 15/20/25/30/35 thresholds.
func weightBandIndex(weightKg float64) int {
	idx := 0
	for _, threshold := range [...]float64{15, 20, 25, 30, 35} {
		if weightKg >= threshold {
			idx++
		}
	}
	return idx
}

// medianOf is percentile_cont(0.5): the middle value, linearly interpolated
// for an even count.
func medianOf(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid], true
	}
	return (sorted[mid-1] + sorted[mid]) / 2, true
}

// weighedIdentities counts the window's distinct weighed tag identities — the
// honest outer denominator the paired count is read against.
//
// projection-review: membership=window scans (obs CTE); group_key=whole-set
// count(DISTINCT tag_key); join_cardinality=obs joins campaigns 1:1 per scan;
// pagination=NONE (one row); scope=tenant + park ANY + campaign-week overlap.
func (r *Repository) weighedIdentities(ctx context.Context, tenantID string, parkIDs []string, startDate, endExclusiveDate string) (int, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	const q = `WITH ` + weighingObsCTE + `
SELECT count(DISTINCT tag_key) FROM obs`
	var count int
	err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, startDate, endExclusiveDate).Scan(&count)
	return count, err
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

// pulseBurn fills the FARM-scope disclosure line: what the farm spends on feed
// on its LATEST sheet day, how much feed on that sheet could not be priced, and
// how many live animals that covers.
//
// LATEST DAY, NOT A WINDOW AVERAGE — and this is a correction, not a
// preference. Feed spend on the live herd climbed ₹27,648 -> ₹64,676 across the
// 18 sheet days of a 90-day window (sheets start partial, then the herd and
// ration grow). Averaging those gave ₹54,106, a figure that describes NO day
// the farm ever had and understates the current rate by ₹10,000. "What do we
// spend per day" means today's rate, so that is what this reports, with the
// day named beside it.
//
// UNPRICED KG IS REPORTED IN KG, NOT SWALLOWED. An item with no purchase row
// prices nothing and silently leaves the rupee total short — on the live herd
// Vijay Concentrate and RGS Concentrate have no purchase rows at all, so ~86 kg
// a day sits outside the money. The kg is surfaced so the reader can see the
// figure is short and why, rather than being handed a confident total that
// quietly omits real feed. It is never estimated from another item's price:
// inventing a rate would make the total look complete when it is not.
//
// Same day_item ∪ external-consumption shape and pricing LATERAL as the Feed
// Analytics expenditure read, so the two screens agree on a given day's rupees.
//
// projection-review: membership=the latest sheet day's priced (park, item)
// cells; group_key=none, plain SUMs over that one day; join_cardinality=issues
// to rows 1:N aggregated before the union, price LATERAL 0..1 per
// (park, item, day); pagination=NONE (one row); scope=tenant + park ANY +
// feed_day window.
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
latest AS (SELECT max(feed_day) AS d FROM priced)
SELECT (SELECT d FROM latest)::text,
       (SELECT SUM(rupees)::float8 FROM priced WHERE feed_day = (SELECT d FROM latest)),
       -- Real feed on that sheet that no purchase can price. Reported, never estimated.
       (SELECT COALESCE(SUM(kg), 0)::float8 FROM priced
         WHERE feed_day = (SELECT d FROM latest) AND rupees IS NULL AND kg > 0),
       -- An authored zero directs no kg, so it needs no price either.
       (SELECT count(DISTINCT feed_item_key) FROM priced WHERE rupees IS NULL AND kg > 0),
       (SELECT count(*) FROM goats g
         WHERE g.tenant_id = $1::uuid AND g.exited_at IS NULL AND g.park_id = ANY($2::uuid[]))`
	var feedDay *string
	err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, startDate, endExclusiveDate).Scan(
		&feedDay, &out.FarmFeedCostPerDayRupees, &out.FarmUnpricedKg,
		&out.UnpricedFeedItems, &out.FarmAnimals,
	)
	if feedDay != nil {
		out.FarmFeedDay = *feedDay
	}
	return err
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
