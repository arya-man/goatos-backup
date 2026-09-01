package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
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
         COALESCE(wcs.partition_label, '') AS partition_label,
         wcs.weighing_category,
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
    -- Sex filter, applied ONCE for every read built on this CTE. $6 is FALSE for the unfiltered
    -- page, so those reads run exactly as they did before the filter existed; when it is on, an
    -- empty tag list correctly matches nothing rather than silently meaning "every kid". Which
    -- tags belong to which sex is sex_scope.go's business: this file is handed strings.
    AND (NOT $6::bool OR lower(btrim(wo.scanned_identifier)) = ANY($7::text[]))
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
  SELECT animal_key, location_id, shed_name, partition_label, weighing_category, observation_id, prev_observation_id,
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
func (r *Repository) GetLeadershipGrowthADG(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string) (domain.GrowthADG, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	// Resolved ONCE for the whole read: every widget below must talk about the same kids, and
	// resolving per helper would let a slow herd write land between two of them and show a
	// leaderboard whose animals are not the ones the headline counted.
	// WithAllTime: this read carries sale readiness, which reports latest-EVER weights and therefore
	// needs the unwindowed tag list. Every other read on the page uses the plain resolver, so the
	// all-history scan is paid once, here, by the one caller that reads it.
	sexScope, scopeErr := r.resolveSexScopeWithAllTime(ctx, tenantID, parkIDs, sex, periodStart, periodEnd)
	if scopeErr != nil {
		return domain.GrowthADG{}, scopeErr
	}
	// Origin resolves its all-time list too, for the same reason and by the same caller: sale
	// readiness below reads AllTimeTags, and intersecting a resolved list against an unresolved
	// (therefore empty) one would report zero sale-ready kids on every filtered page.
	originScope, originErr := r.resolveOriginScopeWithAllTime(ctx, tenantID, parkIDs, origin, periodStart, periodEnd)
	if originErr != nil {
		return domain.GrowthADG{}, originErr
	}
	sexApplied := strings.TrimSpace(sex) != ""
	originApplied := strings.TrimSpace(origin) != ""
	scope := IntersectScopes(sexScope, sexApplied, originScope, originApplied)
	sexFiltered := sexApplied || originApplied

	periodLen := periodEnd.Sub(periodStart)
	prevStart := periodStart.Add(-periodLen)
	prevEnd := periodStart
	lookbackStart := periodStart.Add(-growthLookbackDays * 24 * time.Hour)
	prevLookbackStart := prevStart.Add(-growthLookbackDays * 24 * time.Hour)

	headline, err := r.growthHeadlineStats(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
	if err != nil {
		return domain.GrowthADG{}, err
	}
	prevHeadline, err := r.growthHeadlineStats(ctx, tenantID, parkIDs, prevLookbackStart, prevStart, prevEnd, sexFiltered, scope, weighingCategory)
	if err != nil {
		return domain.GrowthADG{}, err
	}
	// ZERO must never stand in for UNKNOWN: Status/PreviousStatus (set inside growthHeadlineStats)
	// are the explicit markers, and AverageADGGPerDay/PositiveADGPercent are already nil there when
	// there is no qualifying pair. The delta and the previous-period figure are derived HERE, and
	// they inherit the same rule -- a delta computed against a nil (unknown) previous average would
	// silently read as a real number derived from a fabricated 0, which is exactly the fake-delta
	// defect this guards against.
	headline.PreviousAverageADGGPerDay = prevHeadline.AverageADGGPerDay
	headline.PreviousStatus = prevHeadline.Status
	if headline.AverageADGGPerDay != nil && prevHeadline.AverageADGGPerDay != nil {
		delta := *headline.AverageADGGPerDay - *prevHeadline.AverageADGGPerDay
		headline.DeltaGPerDay = &delta
	}

	rejected, err := r.growthRejectedCount(ctx, tenantID, parkIDs, periodStart, periodEnd, weighingCategory)
	if err != nil {
		return domain.GrowthADG{}, err
	}
	headline.RejectedObservationCount = rejected

	eligibility, err := r.growthEligibility(ctx, tenantID, parkIDs, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	trend, err := r.growthTrend(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
	if err != nil {
		return domain.GrowthADG{}, err
	}
	// The weekly cut of the HEADLINE statistic, beside the pair-median trend above. Both are
	// served: `trend` for the surfaces already reading it, `weeklyGain` for any chart that sits
	// next to the headline and must agree with it.
	weeklyGain, err := r.growthWeeklyGain(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	leaderboard, err := r.growthShedLeaderboard(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	distribution, err := r.growthDistribution(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	saleReadiness, err := r.growthSaleReadiness(ctx, tenantID, parkIDs, sexFiltered, scope, weighingCategory)
	if err != nil {
		return domain.GrowthADG{}, err
	}

	lumpSum, err := r.growthLumpSumTrend(ctx, tenantID, parkIDs, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
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
	losing, err := r.growthLosingAnimals(ctx, tenantID, parkIDs, lookbackStart, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
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
		WeeklyGain:      weeklyGain,
		ShedLeaderboard: leaderboard,
		Distribution:    distribution,
		SaleReadiness:   saleReadiness,
		LumpSum:         lumpSum,
	}, nil
}

func (r *Repository) growthHeadlineStats(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, weighingCategory string) (domain.GrowthADGHeadline, error) {
	q := `WITH ` + growthPairsCTE + `),
-- projection-review: membership=weighing_observations; group_key=park_aggregate; join_cardinality=one_to_many; pagination=single_row; scope=park_ids
inperiod AS (
  SELECT * FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($10::text = '' OR weighing_category = $10::text)
),
-- ONE GAIN PER ANIMAL, which is the grain the gain charts report and therefore the grain the
-- headline must report. inperiod is PAIRS: a kid weighed three times in the window contributes two
-- of them, so a herd average taken over pairs quietly counts the most-handled kids twice. It also
-- kept the headline disagreeing with the by-sex chart even after whole-shed pens were added -- 197
-- male pairs against the chart's 141 male animals.
--
-- The animal's own gain is the MEDIAN of its in-period pairs, matching weight_demographics.go's
-- animal_gain exactly; a single-pair animal is simply that pair. Median rather than latest, because
-- one bad scan among three weighs should not become the animal's whole growth story.
animal_gain AS (
  SELECT animal_key,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_per_day) AS g
  FROM inperiod GROUP BY animal_key
),
endpoint_ids AS (
  SELECT observation_id AS oid, verification_status AS status FROM inperiod
  UNION
  SELECT prev_observation_id, prev_verification_status FROM inperiod
),
-- WHOLE-SHED PENS COUNT TOWARD THE FARM'S DAILY GAIN (maintainer decision 2026-08-26).
--
-- They did not, and that is the defect this arm closes: the headline was the MEDIAN of
-- individually-scanned pairs ONLY, while the by-breed/by-sex/by-stage gain charts on the same page
-- were the weighted MEAN of those pairs PLUS whole-shed pens (the 2026-08-25 decision). Once the
-- Sex filter made both statements about the identical population, the page showed a reader two
-- different male daily gains at once -- 133 g in the headline above 200 g in the chart. Most of
-- this farm's kids are weighed by the whole shed (339 of 791 in the landing window), so the
-- headline was also answering "how fast is the herd growing" from well under half of the herd.
--
-- One row per PEN, anchored on its first and latest weighed business date INSIDE the selected
-- window -- the same rn=1 shape shed_span and lump_span already use, so every gain number on the
-- page ranges over the same pen set. A pen weighed once in the window has no movement to report and
-- is excluded by latest.d > first.d rather than counted as zero growth.
--
-- Known and accepted: a whole-shed average moves when animals ENTER OR LEAVE the pen, not only when
-- they grow, so this is a coarser measure than a scanned pair. That is the trade the maintainer took
-- rather than report the herd from a minority of it. The pair-based statistics below (positive %,
-- negative pairs, losing animals) deliberately stay individual-only: a shed average has no
-- per-animal sign to contribute, and inventing one would put animals in a losing list nobody weighed.
--
-- projection-review: producer grain is one live weighing_shed_observations row per bucket; consumer
-- grain is one row per (location_id, partition_label) -- guaranteed by latest.rn = 1 joined to
-- first.rn = 1 on that same pair, so sum(animals) ranges over disjoint pens. The weighted mean's
-- numerator and denominator range over the identical row set (same FROM, same WHERE).
shed_span AS (
  SELECT latest.animal_count::float8 AS animals,
         (latest.average_weight_kg - first.average_weight_kg) * 1000.0
           / NULLIF(latest.d - first.d, 0) AS g_per_day
  FROM (
    SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label,
           o.average_weight_kg, o.animal_count,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id, COALESCE(cs2.partition_label, '') ORDER BY o.accepted_at DESC) AS rn
    FROM weighing_shed_observations o
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id AND cs2.tenant_id = o.tenant_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id AND c2.tenant_id = o.tenant_id
    WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
      AND o.accepted_at >= $5::timestamptz AND o.accepted_at < $4::timestamptz
      AND ($10::text = '' OR cs2.weighing_category = $10::text)
      -- A whole-shed weigh carries no tag, so under a Sex filter it is claimed only when its pen's
      -- cohort is entirely that sex (sex_scope.go proves it); a mixed pen is claimed by neither
      -- side, because one shed average cannot be split between two cohorts.
      AND (NOT $6::bool OR EXISTS (
        SELECT 1 FROM unnest($8::uuid[], $9::text[]) AS b(loc, part)
        WHERE b.loc = cs2.location_id AND b.part = COALESCE(cs2.partition_label, '')
      ))
  ) latest
  JOIN (
    SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label,
           o.average_weight_kg,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id, COALESCE(cs2.partition_label, '') ORDER BY o.accepted_at ASC) AS rn
    FROM weighing_shed_observations o
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id AND cs2.tenant_id = o.tenant_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id AND c2.tenant_id = o.tenant_id
    WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
      AND o.accepted_at >= $5::timestamptz AND o.accepted_at < $4::timestamptz
      AND ($10::text = '' OR cs2.weighing_category = $10::text)
      AND (NOT $6::bool OR EXISTS (
        SELECT 1 FROM unnest($8::uuid[], $9::text[]) AS b(loc, part)
        WHERE b.loc = cs2.location_id AND b.part = COALESCE(cs2.partition_label, '')
      ))
  ) first ON first.location_id = latest.location_id
    AND first.partition_label = latest.partition_label
    AND first.rn = 1
  WHERE latest.rn = 1 AND latest.d > first.d
)
SELECT
  -- The weighted mean the gain charts report, over the same population they report it for: every
  -- scanned pair counts once, and every whole-shed pen counts once PER ANIMAL it holds, so a pen of
  -- 76 kids weighs 76 times as much as one scanned kid. NULLIF keeps an empty period NULL rather
  -- than 0 -- a farm that weighed nothing must not render as a herd that stopped growing.
  ((SELECT COALESCE(sum(g), 0) FROM animal_gain)
     + (SELECT COALESCE(sum(animals * g_per_day), 0) FROM shed_span))
  / NULLIF((SELECT COUNT(*) FROM animal_gain)
     + (SELECT COALESCE(sum(animals), 0) FROM shed_span), 0),
  (SELECT COUNT(*) FROM inperiod),
  -- The denominator behind the headline, so the card can say how many kids it speaks for.
  ((SELECT COUNT(*) FROM animal_gain)
     + (SELECT COALESCE(sum(animals), 0) FROM shed_span))::bigint,
  (SELECT COUNT(*) FILTER (WHERE adg_g_per_day > 0) * 100.0 / NULLIF(COUNT(*), 0) FROM inperiod),
  (SELECT COUNT(*) FROM inperiod WHERE adg_g_per_day < 0),
  (SELECT COUNT(*) FROM endpoint_ids WHERE status = 'pending')`

	var h domain.GrowthADGHeadline
	// median/percent are left as SQL NULL (never COALESCEd to 0) when inperiod is empty, and
	// scanned straight into pointer fields -- this is the ZERO-vs-UNKNOWN fix: a park where every
	// animal was weighed exactly once must come back with these fields absent, not "0".
	err := r.pool.QueryRow(ctx, q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags,
		scope.LocationIDs, scope.PartitionLabels, weighingCategory).Scan(
		&h.AverageADGGPerDay, &h.PairCount, &h.HeadlineAnimals, &h.PositiveADGPercent, &h.NegativeADGCount,
		&h.UnverifiedObservationCount,
	)
	if err != nil {
		return h, err
	}
	// Keyed on the HEADLINE's own denominator, not on PairCount: a park whose kids are all weighed by
	// the whole shed has zero scanned pairs and a perfectly real daily gain, and calling that
	// "insufficient_data" would blank the one number this screen exists to answer.
	if h.HeadlineAnimals == 0 {
		h.Status = "insufficient_data"
	} else {
		h.Status = "ok"
	}
	return h, nil
}

func (r *Repository) growthRejectedCount(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, weighingCategory string) (int, error) {
	var count int
	// projection-review: membership=weighing_observations + weighing_shed_observations; group_key=park_aggregate; join_cardinality=one_to_many; pagination=single_row; scope=park_ids
	err := r.pool.QueryRow(ctx, `
	-- projection-review: membership=weighing_observations + weighing_shed_observations; group_key=park_aggregate; join_cardinality=one_to_many; pagination=single_row; scope=park_ids
WITH rejected AS (
  SELECT wo.observation_id::text AS rejected_id
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status = 'rework'
    AND wo.accepted_at >= $3::timestamptz
    AND wo.accepted_at < $4::timestamptz
    AND ($5::text = '' OR wcs.weighing_category = $5::text)
  UNION ALL
  SELECT wso.shed_observation_id::text AS rejected_id
  FROM weighing_shed_observations wso
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wso.campaign_shed_id AND wcs.tenant_id = wso.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wso.tenant_id
  WHERE wso.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wso.verification_status = 'rework'
    AND wso.accepted_at >= $3::timestamptz
    AND wso.accepted_at < $4::timestamptz
    AND ($5::text = '' OR wcs.weighing_category = $5::text)
)
SELECT COUNT(*) FROM rejected`, tenantID, parkIDs, periodStart, periodEnd, weighingCategory).Scan(&count)
	return count, err
}

func (r *Repository) growthEligibility(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, weighingCategory string) (domain.GrowthEligibility, error) {
	var e domain.GrowthEligibility
	// projection-review: membership=weighing_observations; group_key=animal_key; join_cardinality=one_to_many; pagination=single_row; scope=park_ids
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
    AND ($7::text = '' OR wcs.weighing_category = $7::text)
    -- The Sex filter reaches the coverage counters too. This pair is the phone's "70/382" tile --
    -- how many kids have a second weigh out of all weighed -- and leaving it unfiltered reported
    -- the whole herd's coverage under a heading about half of it.
    AND (NOT $5::bool OR lower(btrim(wo.scanned_identifier)) = ANY($6::text[]))
),
per_animal AS (
  SELECT animal_key, COUNT(*) AS n FROM obs GROUP BY animal_key
)
SELECT COUNT(*) FILTER (WHERE n >= 2), COUNT(*) FROM per_animal`,
		tenantID, parkIDs, periodStart, periodEnd, sexFiltered, scope.Tags, weighingCategory).Scan(&e.AnimalsWithTwoPlusWeighs, &e.TotalAnimalsWeighed)
	return e, err
}

func (r *Repository) growthTrend(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, weighingCategory string) ([]domain.GrowthTrendPoint, error) {
	// projection-review: membership=weighing_observations; group_key=week_start; join_cardinality=one_to_many; pagination=multi_row; scope=park_ids
	q := `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT *, (date_trunc('week', accepted_at AT TIME ZONE 'Asia/Kolkata'))::date AS week_start
  FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($8::text = '' OR weighing_category = $8::text)
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
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, weighingCategory)
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

// growthWeeklyGain is the headline statistic cut by calendar week.
//
// IT MUST STAY THE SAME STATISTIC AS growthHeadlineStats (maintainer decision 2026-08-26). The
// two arms below mirror that function's animal_gain and shed_span exactly -- an animal-weighted
// mean over scanned kids (each once, at the median of its own pairs) PLUS whole-shed pens (each
// once per animal it holds). growthTrend beside this one is the MEDIAN over SCANNED PAIRS ONLY;
// putting that on a page next to this headline is what the lock forbids, which is why this is a
// separate query rather than a second column on that one.
//
// projection-review:
//
//	producer grain: one weighing_observations row per scan; one weighing_shed_observations row per pen weigh.
//	consumer grain: arm (a) one row per (week_start, animal_key) -- GROUP BY those two columns.
//	                arm (b) one row per (week_start, location_id, partition_label) -- rn = 1 over
//	                that triple, so a pen weighed three times in one week contributes ONE movement,
//	                not two, and sum(animals) ranges over disjoint pens within each week.
//	join_cardinality: both arms pre-aggregate to their own grain before the UNION ALL, so the
//	                final sum() never fans out; the numerator and denominator range over the
//	                identical row set (same FROM, same WHERE) in each arm.
//	ratio key set: numerator sum(total) and denominator sum(animals) both range over `weekly`
//	                grouped by week_start -- one key set, stated identical.
//	pagination: multi_row (bounded by the caller's window; 12 weeks on the Weights analytics page).
//	scope: park_ids, plus the sex/origin scope applied to BOTH arms.
//
// growthWeeklyGainQuery is hoisted to package scope rather than built inside the function so a
// query-plan test can reach it by name, which is what the scale guard is asking for.
const growthWeeklyGainQuery = `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT *, (date_trunc('week', accepted_at AT TIME ZONE 'Asia/Kolkata'))::date AS week_start
  FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($10::text = '' OR weighing_category = $10::text)
),
-- Arm (a): ONE GAIN PER ANIMAL PER WEEK, at the median of that animal's pairs landing in the week.
-- Grouping by pairs instead would count the most-handled kids twice, which is the defect the
-- headline's own animal_gain comment records.
animal_gain AS (
  SELECT week_start, animal_key,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_per_day) AS g
  FROM inperiod GROUP BY week_start, animal_key
),
-- Arm (b): whole-shed pens. Every live pen weigh in the window, scoped exactly as the headline
-- scopes it -- a pen is claimed only when the (location, partition) pair is in the resolved list,
-- so a mixed-sex or mixed-origin pen is claimed by neither cohort rather than split.
pen_obs AS (
  SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label,
         o.average_weight_kg, o.animal_count,
         (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM weighing_shed_observations o
  JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id AND cs2.tenant_id = o.tenant_id
  JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id AND c2.tenant_id = o.tenant_id
  WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
    AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
    AND o.accepted_at >= $5::timestamptz AND o.accepted_at < $4::timestamptz
    AND ($10::text = '' OR cs2.weighing_category = $10::text)
    AND (NOT $6::bool OR EXISTS (
      SELECT 1 FROM unnest($8::uuid[], $9::text[]) AS b(loc, part)
      WHERE b.loc = cs2.location_id AND b.part = COALESCE(cs2.partition_label, '')))
),
-- CONSECUTIVE pen weighs, not first-vs-latest-in-window. The headline anchors on the window's two
-- ends because it reports ONE number for the whole window; a weekly series must attribute movement
-- to the week it was observed in, so each weigh is paired with the one before it and bucketed by
-- the LATER weigh -- the identical rule arm (a) applies to a scanned pair spanning two weeks.
pen_pairs AS (
  SELECT location_id, partition_label, animal_count, d, average_weight_kg,
         LAG(average_weight_kg) OVER w AS prev_w,
         LAG(d) OVER w AS prev_d
  FROM pen_obs
  WINDOW w AS (PARTITION BY location_id, partition_label ORDER BY d)
),
-- ONE MOVEMENT PER PEN PER WEEK, the most recent in that week. A pen weighed three times inside one
-- week yields two pairs, and counting both would add its head count to that week's denominator
-- twice -- the same double-count arm (a) avoids by grouping on animal_key.
pen_ranked AS (
  SELECT (date_trunc('week', d::timestamp))::date AS week_start,
         animal_count::float8 AS animals,
         (average_weight_kg - prev_w) * 1000.0 / NULLIF(d - prev_d, 0) AS g_per_day,
         row_number() OVER (
           PARTITION BY location_id, partition_label, (date_trunc('week', d::timestamp))::date
           ORDER BY d DESC
         ) AS rn
  FROM pen_pairs
  WHERE prev_w IS NOT NULL AND d > prev_d
),
shed_span AS (
  SELECT week_start, animals, g_per_day FROM pen_ranked WHERE rn = 1
),
-- Each arm is already at its own grain, so this UNION ALL cannot fan out.
weekly AS (
  SELECT week_start, COALESCE(sum(g), 0) AS total, COUNT(*)::float8 AS animals
  FROM animal_gain GROUP BY week_start
  UNION ALL
  SELECT week_start, COALESCE(sum(animals * g_per_day), 0), COALESCE(sum(animals), 0)
  FROM shed_span GROUP BY week_start
)
SELECT week_start, sum(total) / NULLIF(sum(animals), 0), sum(animals)::bigint
FROM weekly
GROUP BY week_start
-- A week with no animals behind it is dropped rather than reported as zero growth: nobody weighed
-- then, which is not the same statement as "the herd did not grow".
HAVING sum(animals) > 0
ORDER BY week_start`

func (r *Repository) growthWeeklyGain(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, weighingCategory string) ([]domain.GrowthWeeklyGainPoint, error) {
	rows, err := r.pool.Query(ctx, growthWeeklyGainQuery, tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags,
		scope.LocationIDs, scope.PartitionLabels, weighingCategory)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Initialised, never nil, so the wire carries [] rather than null for a farm with no weighs.
	out := []domain.GrowthWeeklyGainPoint{}
	for rows.Next() {
		var p domain.GrowthWeeklyGainPoint
		var weekStart time.Time
		var animals int64
		if err := rows.Scan(&weekStart, &p.AverageADGGPerDay, &animals); err != nil {
			return nil, err
		}
		p.WeekStart = weekStart.Format("2006-01-02")
		p.Animals = int(animals)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) growthShedLeaderboard(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, weighingCategory string) ([]domain.GrowthShedLeaderboardRow, error) {
	// projection-review: membership=weighing_observations; group_key=location_id; join_cardinality=one_to_many; pagination=multi_row; scope=park_ids
	q := `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT * FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($8::text = '' OR weighing_category = $8::text)
),
period_weights AS (
  SELECT wcs.location_id, wcs.display_name AS shed_name,
         COALESCE(wcs.partition_label, '') AS partition_label,
         -- The park's SHORT CODE (CBE, CPT) falling back to its full name, which is the
         -- convention the shed-weights rows on this same page already use -- so both series
         -- of the gain chart name a park the same way. 39 shed names exist in BOTH parks, so
         -- an unqualified row on this chart is genuinely ambiguous.
         COALESCE(NULLIF(pk.location_code, ''), pk.name, '') AS park_name,
         wo.weight_kg::float8 AS weight_kg,
         lower(btrim(wo.scanned_identifier)) AS animal_key
  FROM weighing_observations wo
  JOIN weighing_campaign_sheds wcs
    ON wcs.campaign_shed_id = wo.campaign_shed_id AND wcs.tenant_id = wo.tenant_id
  JOIN weighing_campaigns wc
    ON wc.campaign_id = wcs.campaign_id AND wc.tenant_id = wo.tenant_id
  LEFT JOIN locations pk
    ON pk.tenant_id = wc.tenant_id AND pk.location_id = wc.park_id
  WHERE wo.tenant_id = $1::uuid
    AND wc.park_id = ANY($2::uuid[])
    AND wo.verification_status <> 'rejected'
    AND wo.accepted_at >= $5::timestamptz
    AND wo.accepted_at < $4::timestamptz
    AND ($8::text = '' OR wcs.weighing_category = $8::text)
    -- HALF-FILTERED IS WORSE THAN UNFILTERED. This row's daily gain already followed the Sex
    -- filter (its pairs CTE carries the predicate) while its kid count and median weight did not,
    -- so one row showed a male-only gain sitting beside an all-kids count -- two populations, one
    -- line, nothing saying so. Same predicate, same population, one row.
    AND (NOT $6::bool OR lower(btrim(wo.scanned_identifier)) = ANY($7::text[]))
),
shed_weight AS (
  SELECT location_id, partition_label, MAX(shed_name) AS shed_name, MAX(park_name) AS park_name,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY weight_kg) AS median_weight_kg,
         COUNT(DISTINCT animal_key) AS n
  FROM period_weights
  GROUP BY location_id, partition_label
),
shed_adg AS (
  SELECT location_id, partition_label,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY adg_g_per_day) AS median_adg,
         COUNT(*) AS pair_count
  FROM inperiod
  GROUP BY location_id, partition_label
)
SELECT sw.location_id, sw.shed_name, sw.partition_label, sw.park_name, sw.n, sw.median_weight_kg,
       COALESCE(sa.median_adg, 0), COALESCE(sa.pair_count, 0)
FROM shed_weight sw
LEFT JOIN shed_adg sa ON sa.location_id = sw.location_id AND COALESCE(sa.partition_label, '') = COALESCE(sw.partition_label, '')
ORDER BY sw.shed_name, sw.partition_label`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, weighingCategory)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthShedLeaderboardRow{}
	for rows.Next() {
		var row domain.GrowthShedLeaderboardRow
		if err := rows.Scan(&row.LocationID, &row.DisplayName, &row.PartitionLabel, &row.ParkName, &row.AnimalCount, &row.MedianWeightKg,
			&row.MedianADGGPerDay, &row.ADGPairCount); err != nil {
			return nil, err
		}
		// COMPOSE ONLY WHEN THE NAME DOES NOT ALREADY CARRY THE PEN. A weighing bucket is often a
		// SYNTHETIC per-partition location whose own name is already "Mandela 1 - Part 5", and
		// oploc.Display() appends unconditionally (correctly -- it is given a shed name and a
		// partition). Feeding it a name that already ends in the partition produced
		// "Mandela 1 - Part 5 - Part 5". The field was never read by a screen until the gain chart
		// started using it, so the doubling sat here unseen; the same guard is used by the sibling
		// composer in growthdirector's operationalLabel.
		if row.PartitionLabel != "" && strings.HasSuffix(row.DisplayName, row.PartitionLabel) {
			row.OperationalLocationDisplay = row.DisplayName
		} else {
			row.OperationalLocationDisplay = (oploc.OperationalLocation{
				ShedID:         row.LocationID,
				ShedName:       row.DisplayName,
				PartitionLabel: row.PartitionLabel,
			}).Display()
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

func (r *Repository) growthDistribution(ctx context.Context, tenantID string, parkIDs []string, lookbackStart, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, weighingCategory string) ([]domain.GrowthDistributionBucket, error) {
	var negativeCount int
	// projection-review: membership=weighing_observations; group_key=bucket; join_cardinality=one_to_many; pagination=multi_row; scope=park_ids
	if err := r.pool.QueryRow(ctx, `WITH `+growthPairsCTE+`),
inperiod AS (SELECT * FROM qualifying WHERE accepted_at >= $5::timestamptz AND ($8::text = '' OR weighing_category = $8::text))
SELECT COUNT(*) FROM inperiod WHERE adg_g_per_day < 0`,
		tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, weighingCategory).Scan(&negativeCount); err != nil {
		return nil, err
	}

	maxEdge := growthDistributionBinWidth * growthDistributionBinCount
	// $6/$7 belong to the shared pairs CTE (the sex flag and its tag list), so this query's own
	// two parameters start at $8. Every consumer of growthPairsCTE binds those two in the same
	// positions, which is what lets the CTE carry one predicate for all of them.
	rows, err := r.pool.Query(ctx, `WITH `+growthPairsCTE+`),
inperiod AS (SELECT * FROM qualifying WHERE accepted_at >= $5::timestamptz AND adg_g_per_day >= 0 AND ($8::text = '' OR weighing_category = $8::text))
SELECT LEAST(width_bucket(adg_g_per_day, 0, $9::float8, $10::int), $10::int) AS bucket, COUNT(*)
FROM inperiod
GROUP BY bucket
ORDER BY bucket`,
		tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags,
		weighingCategory, maxEdge, growthDistributionBinCount)
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

func (r *Repository) growthSaleReadiness(ctx context.Context, tenantID string, parkIDs []string, sexFiltered bool, scope SexScope, weighingCategory string) (domain.GrowthSaleReadiness, error) {
	// FAIL LOUD, never quietly empty. An unresolved AllTimeTags is an empty list, and an empty tag
	// list filters every animal out -- so a caller that resolved the window-only scope would report
	// zero sale-ready kids and look like a farm with nothing to sell. That is the kind of wrong
	// number nobody questions, so it is an error instead.
	if sexFiltered && !scope.allTimeResolved {
		return domain.GrowthSaleReadiness{}, fmt.Errorf(
			"weighing: sale readiness spans all time and needs the all-time sex scope; resolve it with ResolveSexScopeWithAllTime")
	}
	// "Latest weight" here is the animal's LATEST-EVER accepted individual weigh, not bounded to
	// the requested period: sale readiness is a point-in-time fact about the animal today, and
	// bounding it to a reporting window would make an animal that was not weighed this month
	// (but is definitely heavy enough to sell, per its last known weight) invisible to the
	// exact question this section answers.
	// projection-review: membership=weighing_observations; group_key=animal_key; join_cardinality=one_to_many; pagination=single_row; scope=park_ids
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
    -- The Sex filter narrows WHICH KIDS, never the time bound. This block deliberately reads each
    -- animal's latest-EVER weigh rather than the selected window (see the note above), and that is
    -- untouched here -- but a reader on Male must still be told how many MALE kids are heavy enough
    -- to sell. The parameters were previously not even passed, so this block answered about the
    -- whole herd beside a headline about half of it.
    --
    -- AllTimeTags, NOT Tags: the ordinary scope is bounded by the selected window plus the 90-day
    -- gain lookback, and using it here narrowed a latest-EVER count to "weighed recently" -- an
    -- animal last weighed a year ago and long since heavy enough to sell would have dropped out of
    -- the denominator. This farm's data cannot show that today because every weigh in it falls
    -- inside the lookback, which is exactly why the regression test below reaches outside it.
    AND (NOT $3::bool OR lower(btrim(wo.scanned_identifier)) = ANY($4::text[]))
    AND ($5::text = '' OR $5::text = 'individual_animal')
),
latest AS (
  SELECT DISTINCT ON (animal_key) animal_key, weight_kg
  FROM obs
  ORDER BY animal_key, accepted_at DESC, observation_id DESC
)
SELECT COUNT(*), COUNT(*) FILTER (WHERE weight_kg >= 30), COUNT(*) FILTER (WHERE weight_kg >= 35)
FROM latest`, tenantID, parkIDs, sexFiltered, scope.AllTimeTags, weighingCategory)
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

func (r *Repository) growthLumpSumTrend(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, weighingCategory string) (domain.GrowthLumpSum, error) {
	// Lump-sum shed totals are read HERE, entirely separately from the individual-observation
	// queries above -- see the GrowthLumpSum domain comment for why a per-animal ADG must never
	// be derived from the delta between two shed-level averages.
	// projection-review: membership=weighing_shed_observations; group_key=location_id_week; join_cardinality=one_to_many; pagination=multi_row; scope=park_ids
	rows, err := r.pool.Query(ctx, `
SELECT wcs.location_id, wcs.display_name, COALESCE(wcs.partition_label, ''),
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
  AND ($8::text = '' OR wcs.weighing_category = $8::text)
  -- The Sex filter reaches this trend too. It did NOT, and the parameters were accepted and
  -- silently ignored: every other block of this read narrowed to the selected half of the herd
  -- while the whole-shed trend kept reporting all ten pen-weeks, so a reader on Female saw a
  -- female headline above a trend of pens that hold no females at all. A whole-shed weigh carries
  -- no tag, so it is claimed only when its pen's cohort is entirely that sex (sex_scope.go proves
  -- it); a mixed pen is claimed by neither side, because one shed average cannot be split between
  -- two cohorts.
  AND (NOT $5::bool OR EXISTS (
    SELECT 1 FROM unnest($6::uuid[], $7::text[]) AS b(loc, part)
    WHERE b.loc = wcs.location_id AND b.part = COALESCE(wcs.partition_label, '')
  ))
GROUP BY wcs.location_id, wcs.display_name, COALESCE(wcs.partition_label, ''), week_start
ORDER BY wcs.display_name, COALESCE(wcs.partition_label, ''), week_start`,
		tenantID, parkIDs, periodStart, periodEnd, sexFiltered, scope.LocationIDs, scope.PartitionLabels, weighingCategory)
	if err != nil {
		return domain.GrowthLumpSum{}, err
	}
	defer rows.Close()
	out := domain.GrowthLumpSum{ShedWeekTrend: []domain.GrowthLumpSumShedTrendPoint{}}
	for rows.Next() {
		var p domain.GrowthLumpSumShedTrendPoint
		var weekStart time.Time
		if err := rows.Scan(&p.LocationID, &p.DisplayName, &p.PartitionLabel, &weekStart, &p.AverageWeightKg, &p.HeadCount); err != nil {
			return domain.GrowthLumpSum{}, err
		}
		p.OperationalLocationDisplay = (oploc.OperationalLocation{
			ShedID:         p.LocationID,
			ShedName:       p.DisplayName,
			PartitionLabel: p.PartitionLabel,
		}).Display()
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
	sexFiltered bool,
	scope SexScope,
	weighingCategory string,
) ([]domain.GrowthLosingAnimal, error) {
	q := `WITH ` + growthPairsCTE + `),
inperiod AS (
  SELECT * FROM qualifying
  WHERE accepted_at >= $5::timestamptz
    AND ($8::text = '' OR weighing_category = $8::text)
),
latest_pair AS (
  SELECT DISTINCT ON (animal_key) *
  FROM inperiod
  ORDER BY animal_key, accepted_at DESC
)
SELECT animal_key, shed_name, partition_label, location_id::text, prev_weight, weight_kg,
       adg_g_per_day, days_between,
       to_char(TIMEZONE('Asia/Kolkata', accepted_at)::date, 'YYYY-MM-DD')
FROM latest_pair
WHERE adg_g_per_day < 0
ORDER BY adg_g_per_day ASC
LIMIT 200`
	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, lookbackStart, periodEnd, periodStart, sexFiltered, scope.Tags, weighingCategory)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GrowthLosingAnimal{}
	for rows.Next() {
		var a domain.GrowthLosingAnimal
		var partitionLabel, locationID string
		if err := rows.Scan(
			&a.ScannedIdentifier, &a.ShedDisplayName, &partitionLabel, &locationID, &a.PreviousWeightKg, &a.LatestWeightKg,
			&a.ADGGPerDay, &a.DaysBetween, &a.LatestWeighDate,
		); err != nil {
			return nil, err
		}
		if partitionLabel != "" && strings.HasSuffix(a.ShedDisplayName, partitionLabel) {
			a.OperationalLocationDisplay = a.ShedDisplayName
		} else {
			a.OperationalLocationDisplay = (oploc.OperationalLocation{
				ShedID:         locationID,
				ShedName:       a.ShedDisplayName,
				PartitionLabel: partitionLabel,
			}).Display()
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
