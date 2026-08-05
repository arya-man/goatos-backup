package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// growthLookbackDays bounds how far BEFORE the requested period the query looks for a prior
// weigh to pair against the first weigh inside the period. Without a bound, a tenant with years
// of weighing history would force a full-history scan per animal every time leadership opened
// this screen for a one-week window. 400 days comfortably covers a goat's whole growth-tracked
// life (kid to sale) while keeping the scan bounded.
const growthLookbackDays = 400

// growthPairsCTE is the shared base: resolve each accepted, non-rejected individual observation
// to an animal BY ITS RAW SCANNED TAG ALONE, then pair each observation with the PRECEDING one
// for the same tag to derive an ADG.
//
// Weighing is FREE-FLOW and SELF-CONTAINED: it reads ONLY weighing tables. It must never join
// goat_identifiers, goats, or any other herd/vaccination table. An earlier version resolved the
// tag to a goat_id through goat_identifiers -- a shared herd table used by 13 other modules --
// which quietly made weighing depend on herd identity data and on whatever is wrong in it.
//
// The cost of not joining is real and is accepted: if an animal is re-tagged, its history splits
// into two tags. That is honest -- weighing only ever knew the tag that was scanned. It expects bound parameters, in order:
//
//	$1 tenant_id (uuid)
//	$2 park_ids (uuid[]) -- one park, or every park the caller is authorized to monitor; the
//	   caller (service layer) has already authorization-checked this set, this file only filters
//	   by it
//	$3 lookback_start (timestamptz, inclusive) -- observations before this are never read
//	$4 period_end (timestamptz, exclusive)
//
// EVERY caller-supplied value reaching this file arrives as a bound parameter, never string-
// concatenated -- see the injection-incident comment in weight_history.go for why that rule is
// non-negotiable here.
//
// Pending/unverified observations are DELIBERATELY included (only 'rejected' is excluded): see
// weight_history.go's "Weights AWAITING VERIFICATION are included" comment, which settled this
// for the sibling weight-history read and applies identically here -- a pending weight is a real
// measurement, not a reason to hide it from an aggregate.
const growthPairsCTE = `
base AS (
  SELECT wo.observation_id, wo.weight_kg::float8 AS weight_kg, wo.accepted_at,
         wo.verification_status, wcs.location_id, wcs.display_name AS shed_name,
         lower(btrim(wo.scanned_identifier)) AS animal_key
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status <> 'rejected'
    AND wo.accepted_at >= $3::timestamptz
    AND wo.accepted_at < $4::timestamptz
),
ordered AS (
  SELECT *,
    LAG(weight_kg) OVER w AS prev_weight,
    LAG(accepted_at) OVER w AS prev_accepted_at,
    LAG(observation_id) OVER w AS prev_observation_id,
    LAG(verification_status) OVER w AS prev_verification_status
  FROM base
  WINDOW w AS (PARTITION BY animal_key ORDER BY accepted_at, observation_id)
),
pairs AS (
  SELECT animal_key, location_id, shed_name, observation_id, prev_observation_id,
         verification_status, prev_verification_status, accepted_at, prev_accepted_at,
         -- Carried so a caller can state the actual change ("19.0 -> 18.2 kg"), not just a rate.
         weight_kg, prev_weight,
         -- WHOLE BUSINESS DAYS between the two weighs, not elapsed seconds. ADG is a per-DAY
         -- rate, and dividing by a fractional day turns ordinary scale noise into a headline
         -- number: two weighs of one tag 111 seconds apart (15.0 kg then 11.0 kg, a duplicate
         -- scan across two sheds) divided by 0.0013 days and reported -3,108,762 g/day on the
         -- leadership Growth screen. An animal cannot gain or lose meaningfully within a day,
         -- so a same-day pair is a re-weigh, a correction, or a double scan -- never growth.
         ((accepted_at AT TIME ZONE 'Asia/Kolkata')::date
            - (prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date) AS days_between,
         (weight_kg - prev_weight) * 1000.0
           / NULLIF(
               (accepted_at AT TIME ZONE 'Asia/Kolkata')::date
                 - (prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date,
               0
             ) AS adg_g_per_day
  FROM ordered
  WHERE prev_weight IS NOT NULL
),
qualifying AS (
  -- Excludes any pair whose two weighs fall on the SAME business day: there is no whole day
  -- between them to average over, so no daily rate exists to report. Weighing stays free-flow
  -- and uncapped -- an operator may weigh a tag as often as they like and every one of those
  -- rows is kept -- but only pairs that actually span days produce an ADG.
  SELECT * FROM pairs WHERE days_between > 0`

// GetLeadershipGrowthADG is the leadership ADG (Average Daily Gain) read model: herd-level
// growth signal for a park and period, built from individual weighs only (see GrowthLumpSum on
// why lump-sum totals are never used to derive per-animal ADG).
//
// periodStart/periodEnd are Asia/Kolkata business-day boundaries expressed as UTC instants by
// the caller (the service layer), half-open [periodStart, periodEnd).
func (r *Repository) GetLeadershipGrowthADG(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.GrowthADG, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	periodLen := periodEnd.Sub(periodStart)
	prevStart := periodStart.Add(-periodLen)
	prevEnd := periodStart
	lookbackStart := periodStart.Add(-growthLookbackDays * 24 * time.Hour)
	prevLookbackStart := prevStart.Add(-growthLookbackDays * 24 * time.Hour)

	headline, err := r.growthHeadlineStats(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd)
	if err != nil {
		return domain.GrowthADG{}, err
	}
	prevHeadline, err := r.growthHeadlineStats(ctx, tenantID, parkIDs, prevLookbackStart, prevStart, prevEnd)
	if err != nil {
		return domain.GrowthADG{}, err
	}
	// ZERO must never stand in for UNKNOWN: Status/PreviousStatus (set inside growthHeadlineStats)
	// are the explicit markers, and MedianADGGPerDay/PositiveADGPercent are already nil there when
	// there is no qualifying pair. The delta and the previous-median figure are derived HERE, and
	// they inherit the same rule -- a delta computed against a nil (unknown) previous median would
	// silently read as a real number derived from a fabricated 0, which is exactly the fake-delta
	// defect this guards against.
	headline.PreviousMedianADGGPerDay = prevHeadline.MedianADGGPerDay
	headline.PreviousStatus = prevHeadline.Status
	if headline.MedianADGGPerDay != nil && prevHeadline.MedianADGGPerDay != nil {
		delta := *headline.MedianADGGPerDay - *prevHeadline.MedianADGGPerDay
		headline.DeltaGPerDay = &delta
	}

	rejected, err := r.growthRejectedCount(ctx, tenantID, parkIDs, periodStart, periodEnd)
	if err != nil {
		return domain.GrowthADG{}, err
	}
	headline.RejectedObservationCount = rejected

	eligibility, err := r.growthEligibility(ctx, tenantID, parkIDs, periodStart, periodEnd)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	trend, err := r.growthTrend(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	leaderboard, err := r.growthShedLeaderboard(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	distribution, err := r.growthDistribution(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	saleReadiness, err := r.growthSaleReadiness(ctx, tenantID, parkIDs)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	lumpSum, err := r.growthLumpSumTrend(ctx, tenantID, parkIDs, periodStart, periodEnd)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	// ParkID stays the single-park value only when exactly one park was requested (the
	// pre-existing single-park contract); ParkIDs always carries the full authorized set the
	// service layer resolved, whether that is one park or the caller's whole authorized scope.
	singlePark := ""
	if len(parkIDs) == 1 {
		singlePark = parkIDs[0]
	}
	parks, err := r.growthParkNames(ctx, tenantID, parkIDs)
	if err != nil {
		return domain.GrowthADG{}, err
	}
	losing, err := r.growthLosingAnimals(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd)
	if err != nil {
		return domain.GrowthADG{}, err
	}
	// The tile drills into this list, so the count it shows must be the length of THIS list.
	headline.LosingAnimalCount = len(losing)
	return domain.GrowthADG{
		ParkID:          singlePark,
		ParkIDs:         parkIDs,
		Parks:           parks,
		LosingAnimals:   losing,
		PeriodStart:     periodStart.Format("2006-01-02"),
		PeriodEnd:       periodEnd.Add(-24 * time.Hour).Format("2006-01-02"),
		Headline:        headline,
		Eligibility:     eligibility,
		Trend:           trend,
		ShedLeaderboard: leaderboard,
		Distribution:    distribution,
		SaleReadiness:   saleReadiness,
		LumpSum:         lumpSum,
	}, nil
}

func (r *Repository) growthHeadlineStats(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time) (domain.GrowthADGHeadline, error) {
	q := `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT * FROM qualifying WHERE accepted_at >= $5::timestamptz
),
endpoint_ids AS (
  SELECT observation_id AS oid, verification_status AS status FROM inperiod
  UNION
  SELECT prev_observation_id, prev_verification_status FROM inperiod
)
SELECT
  (SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_per_day) FROM inperiod),
  (SELECT COUNT(*) FROM inperiod),
  (SELECT COUNT(*) FILTER (WHERE adg_g_per_day > 0) * 100.0 / NULLIF(COUNT(*), 0) FROM inperiod),
  (SELECT COUNT(*) FROM inperiod WHERE adg_g_per_day < 0),
  (SELECT COUNT(*) FROM endpoint_ids WHERE status = 'pending')`

	var h domain.GrowthADGHeadline
	// median/percent are left as SQL NULL (never COALESCEd to 0) when inperiod is empty, and
	// scanned straight into pointer fields -- this is the ZERO-vs-UNKNOWN fix: a park where every
	// animal was weighed exactly once must come back with these fields absent, not "0".
	err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart).Scan(
		&h.MedianADGGPerDay, &h.PairCount, &h.PositiveADGPercent, &h.NegativeADGCount,
		&h.UnverifiedObservationCount,
	)
	if err != nil {
		return h, err
	}
	if h.PairCount == 0 {
		h.Status = "insufficient_data"
	} else {
		h.Status = "ok"
	}
	return h, nil
}

func (r *Repository) growthRejectedCount(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM weighing_observations wo
JOIN weighing_campaign_sheds wcs
  ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
JOIN weighing_campaigns wc
  ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
WHERE wo.tenant_id = $1::uuid
  AND wc.park_id = ANY($2::uuid[])
  AND wo.verification_status = 'rejected'
  AND wo.accepted_at >= $3::timestamptz
  AND wo.accepted_at < $4::timestamptz`, tenantID, parkIDs, periodStart, periodEnd).Scan(&count)
	return count, err
}

func (r *Repository) growthEligibility(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.GrowthEligibility, error) {
	var e domain.GrowthEligibility
	err := r.pool.QueryRow(ctx, `
WITH obs AS (
  SELECT lower(btrim(wo.scanned_identifier)) AS animal_key
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status <> 'rejected'
    AND wo.accepted_at >= $3::timestamptz
    AND wo.accepted_at < $4::timestamptz
),
per_animal AS (
  SELECT animal_key, COUNT(*) AS n FROM obs GROUP BY animal_key
)
SELECT COUNT(*) FILTER (WHERE n >= 2), COUNT(*) FROM per_animal`,
		tenantID, parkIDs, periodStart, periodEnd).Scan(&e.AnimalsWithTwoPlusWeighs, &e.TotalAnimalsWeighed)
	return e, err
}

func (r *Repository) growthTrend(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time) ([]domain.GrowthTrendPoint, error) {
	q := `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT *, (date_trunc('week', accepted_at AT TIME ZONE 'Asia/Kolkata'))::date AS week_start
  FROM qualifying WHERE accepted_at >= $5::timestamptz
)
SELECT week_start,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_per_day) AS median_adg,
       COUNT(*) AS pair_count
FROM inperiod
GROUP BY week_start
-- This groups whatever dates actually have pairs into calendar weeks purely for display -- it is
-- NOT an assumption that weighing happens weekly. A week with no pairs simply has no row here: it
-- is never interpolated with a fabricated zero or a carried-forward value, and no week is ever
-- flagged as "missed" or "overdue".
ORDER BY week_start`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthTrendPoint{}
	for rows.Next() {
		var p domain.GrowthTrendPoint
		var weekStart time.Time
		if err := rows.Scan(&weekStart, &p.MedianADGGPerDay, &p.PairCount); err != nil {
			return nil, err
		}
		p.WeekStart = weekStart.Format("2006-01-02")
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) growthShedLeaderboard(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time) ([]domain.GrowthShedLeaderboardRow, error) {
	q := `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT * FROM qualifying WHERE accepted_at >= $5::timestamptz
),
period_weights AS (
  SELECT wcs.location_id, wcs.display_name AS shed_name, wo.weight_kg::float8 AS weight_kg,
         lower(btrim(wo.scanned_identifier)) AS animal_key
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status <> 'rejected'
    AND wo.accepted_at >= $5::timestamptz
    AND wo.accepted_at < $4::timestamptz
),
shed_weight AS (
  SELECT location_id, MAX(shed_name) AS shed_name,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY weight_kg) AS median_weight_kg,
         COUNT(DISTINCT animal_key) AS n
  FROM period_weights
  GROUP BY location_id
),
shed_adg AS (
  SELECT location_id,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_per_day) AS median_adg,
         COUNT(*) AS pair_count
  FROM inperiod
  GROUP BY location_id
)
SELECT sw.location_id, sw.shed_name, sw.n, sw.median_weight_kg,
       COALESCE(sa.median_adg, 0), COALESCE(sa.pair_count, 0)
FROM shed_weight sw
LEFT JOIN shed_adg sa ON sa.location_id = sw.location_id
ORDER BY sw.shed_name`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthShedLeaderboardRow{}
	for rows.Next() {
		var row domain.GrowthShedLeaderboardRow
		if err := rows.Scan(&row.LocationID, &row.DisplayName, &row.AnimalCount, &row.MedianWeightKg,
			&row.MedianADGGPerDay, &row.ADGPairCount); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// growthDistributionBinWidth and growthDistributionBinCount define the FIXED histogram shape:
// 12 bins of 25 g/day each (0-300), plus an explicit negative bucket and a "300+" overflow, so
// the same bin edges apply across every park and period and results are visually comparable.
const (
	growthDistributionBinWidth = 25.0
	growthDistributionBinCount = 12
)

func (r *Repository) growthDistribution(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time) ([]domain.GrowthDistributionBucket, error) {
	var negativeCount int
	if err := r.pool.QueryRow(ctx, `WITH `+growthPairsCTE+`),
inperiod AS (SELECT * FROM qualifying WHERE accepted_at >= $5::timestamptz)
SELECT COUNT(*) FROM inperiod WHERE adg_g_per_day < 0`,
		tenantID, parkIDs, lookbackStart, periodEnd, periodStart).Scan(&negativeCount); err != nil {
		return nil, err
	}

	maxEdge := growthDistributionBinWidth * growthDistributionBinCount
	rows, err := r.pool.Query(ctx, `WITH `+growthPairsCTE+`),
inperiod AS (SELECT * FROM qualifying WHERE accepted_at >= $5::timestamptz AND adg_g_per_day >= 0)
SELECT LEAST(width_bucket(adg_g_per_day, 0, $6::float8, $7::int), $7::int) AS bucket, COUNT(*)
FROM inperiod
GROUP BY bucket
ORDER BY bucket`,
		tenantID, parkIDs, lookbackStart, periodEnd, periodStart, maxEdge, growthDistributionBinCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[int]int, growthDistributionBinCount)
	for rows.Next() {
		var bucket, count int
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, err
		}
		counts[bucket] += count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]domain.GrowthDistributionBucket, 0, growthDistributionBinCount+2)
	out = append(out, domain.GrowthDistributionBucket{Label: "negative", Count: negativeCount})
	for i := 1; i <= growthDistributionBinCount; i++ {
		lo := float64(i-1) * growthDistributionBinWidth
		hi := float64(i) * growthDistributionBinWidth
		loCopy, hiCopy := lo, hi
		out = append(out, domain.GrowthDistributionBucket{
			Label: fmt.Sprintf("%.0f-%.0f", lo, hi), MinGPerDay: &loCopy, MaxGPerDay: &hiCopy, Count: counts[i],
		})
	}
	// width_bucket returns bin count+1 for any value >= the top edge -- that overflow bin is
	// folded into an explicit "300+" bucket rather than silently dropped or mis-binned into the
	// last regular bucket.
	topEdge := maxEdge
	out = append(out, domain.GrowthDistributionBucket{
		Label: fmt.Sprintf("%.0f+", topEdge), MinGPerDay: &topEdge, Count: counts[growthDistributionBinCount+1],
	})
	return out, nil
}

func (r *Repository) growthSaleReadiness(ctx context.Context, tenantID string, parkIDs []string) (domain.GrowthSaleReadiness, error) {
	// "Latest weight" here is the animal's LATEST-EVER accepted individual weigh, not bounded to
	// the requested period: sale readiness is a point-in-time fact about the animal today, and
	// bounding it to a reporting window would make an animal that was not weighed this month
	// (but is definitely heavy enough to sell, per its last known weight) invisible to the
	// exact question this section answers.
	rows, err := r.pool.Query(ctx, `
WITH obs AS (
  SELECT wo.observation_id, wo.weight_kg::float8 AS weight_kg, wo.accepted_at,
         lower(btrim(wo.scanned_identifier)) AS animal_key
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status <> 'rejected'
),
latest AS (
  SELECT DISTINCT ON (animal_key) animal_key, weight_kg
  FROM obs
  ORDER BY animal_key, accepted_at DESC, observation_id DESC
)
SELECT COUNT(*), COUNT(*) FILTER (WHERE weight_kg >= 30), COUNT(*) FILTER (WHERE weight_kg >= 35)
FROM latest`, tenantID, parkIDs)
	var s domain.GrowthSaleReadiness
	if err != nil {
		return s, err
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&s.AnimalsConsidered, &s.AtOrAbove30Kg, &s.AtOrAbove35Kg); err != nil {
			return s, err
		}
	}
	return s, rows.Err()
}

func (r *Repository) growthLumpSumTrend(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.GrowthLumpSum, error) {
	// Lump-sum shed totals are read HERE, entirely separately from the individual-observation
	// queries above -- see the GrowthLumpSum domain comment for why a per-animal ADG must never
	// be derived from the delta between two shed-level averages.
	rows, err := r.pool.Query(ctx, `
SELECT wcs.location_id, wcs.display_name,
       (date_trunc('week', wso.accepted_at AT TIME ZONE 'Asia/Kolkata'))::date AS week_start,
       AVG(wso.average_weight_kg::float8) AS avg_weight_kg,
       SUM(wso.animal_count) AS head_count
FROM weighing_shed_observations wso
JOIN weighing_campaign_sheds wcs
  ON wcs.campaign_shed_id = wso.campaign_shed_id AND wcs.tenant_id = wso.tenant_id
JOIN weighing_campaigns wc
  ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wso.tenant_id
WHERE wso.tenant_id = $1::uuid
  AND wc.park_id = ANY($2::uuid[])
  AND wso.verification_status <> 'rejected'
  AND wso.withdrawn_at IS NULL
  AND wso.accepted_at >= $3::timestamptz
  AND wso.accepted_at < $4::timestamptz
GROUP BY wcs.location_id, wcs.display_name, week_start
ORDER BY wcs.display_name, week_start`, tenantID, parkIDs, periodStart, periodEnd)
	if err != nil {
		return domain.GrowthLumpSum{}, err
	}
	defer rows.Close()
	out := domain.GrowthLumpSum{ShedWeekTrend: []domain.GrowthLumpSumShedTrendPoint{}}
	for rows.Next() {
		var p domain.GrowthLumpSumShedTrendPoint
		var weekStart time.Time
		if err := rows.Scan(&p.LocationID, &p.DisplayName, &weekStart, &p.AverageWeightKg, &p.HeadCount); err != nil {
			return domain.GrowthLumpSum{}, err
		}
		p.WeekStart = weekStart.Format("2006-01-02")
		out.ShedWeekTrend = append(out.ShedWeekTrend, p)
	}
	return out, rows.Err()
}

// growthParkNames resolves park ids to display names from `locations` -- an allowlisted ORG
// table (where a park is), never herd or animal data. Without it a client has to invent labels
// like "Park 1", which is a lie dressed as a UI.
func (r *Repository) growthParkNames(ctx context.Context, tenantID string, parkIDs []string) ([]domain.GrowthPark, error) {
	if len(parkIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT location_id::text, name
		FROM locations
		WHERE tenant_id = $1::uuid AND location_id = ANY($2::uuid[])
		ORDER BY name ASC
	`, tenantID, parkIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthPark{}
	for rows.Next() {
		var p domain.GrowthPark
		if err := rows.Scan(&p.ParkID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// growthLosingAnimals lists the animals whose MOST RECENT pair in the period shows a loss.
//
// One row per animal (its latest pair), not one per losing pair: a reader asking "which animals
// are losing weight" wants animals, and an animal that dipped once and recovered is not currently
// losing. Bounded so a bad period cannot return the whole herd.
func (r *Repository) growthLosingAnimals(
	ctx context.Context,
	tenantID string,
	parkIDs []string,
	lookbackStart, periodStart, periodEnd time.Time,
) ([]domain.GrowthLosingAnimal, error) {
	q := `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT * FROM qualifying WHERE accepted_at >= $5::timestamptz
),
latest_pair AS (
  SELECT DISTINCT ON (animal_key) *
  FROM inperiod
  ORDER BY animal_key, accepted_at DESC
)
SELECT animal_key, shed_name, prev_weight, weight_kg,
       adg_g_per_day, days_between,
       to_char(TIMEZONE('Asia/Kolkata', accepted_at)::date, 'YYYY-MM-DD')
FROM latest_pair
WHERE adg_g_per_day < 0
ORDER BY adg_g_per_day ASC
LIMIT 200`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthLosingAnimal{}
	for rows.Next() {
		var a domain.GrowthLosingAnimal
		if err := rows.Scan(
			&a.ScannedIdentifier, &a.ShedDisplayName, &a.PreviousWeightKg, &a.LatestWeightKg,
			&a.ADGGPerDay, &a.DaysBetween, &a.LatestWeighDate,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
