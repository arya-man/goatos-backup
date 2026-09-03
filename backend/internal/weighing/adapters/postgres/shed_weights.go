package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// GetShedWeights serves the admin-web "Kids — Weights" screen: one row per shed
// with its most recent weigh, plus the whole-filter KPI rollup.
//
// WEIGHING ISOLATION: reads weighing_campaign_sheds, weighing_campaigns,
// weighing_observations, weighing_shed_observations and `locations` only.
// `locations` is one of the three allowlisted org tables; no herd, goat,
// identifier or vaccination table is touched, which is why this read can offer
// no breed, sex or stage dimension.
//
// projection-review: membership=one row per (park_id, location_id) selected from weighing_campaign_sheds; group_key=(park_id, location_id) via DISTINCT ON in latest_bucket; join_cardinality=every joined side is 0..1 against a bucket (campaigns PK, locations PK x2, LATERAL scalar aggregate, and weighing_shed_observations 0..1 GIVEN withdrawn_at IS NULL), so no side can multiply a shed row; pagination=NONE, bounded by MaxShedWeightsRows, and the summary is a whole-result aggregate over every scoped shed rather than the returned page; scope=tenant_id + park_id = ANY($2)
//
// The proof behind each field:
//
//	PRODUCER UNIQUENESS vs CONSUMER MATCH KEYS, side by side:
//	  weighing_campaign_sheds       unique on (campaign_shed_id)            [PK]
//	  latest_bucket        groups on (park_id, location_id)                 -> picks ONE bucket
//	  ind_scan             groups on (campaign_shed_id)                     [matches bucket PK]
//	  lump                 unique on (tenant_id, campaign_shed_id) PARTIAL withdrawn_at IS NULL
//	                                                                        [000067 uidx]
//	  locations pk/sh      unique on (location_id)                          [PK]
//
//	ROW MULTIPLICITY OF EVERY JOINED SIDE (all 0..1 against latest_bucket, so no
//	side can multiply a shed row):
//	  weighing_campaigns   1 per campaign_id (PK)
//	  locations sh         0..1 per location_id (PK)
//	  locations pk         0..1 per park_id (PK)
//	  ind_scan             0..1 — LATERAL scalar aggregate, collapses to exactly one row
//	  lump                 0..1 — GIVEN the withdrawn_at IS NULL predicate. WITHOUT it this
//	                       side is 0..N (a reopened+resubmitted bucket legitimately keeps its
//	                       superseded rows) and fans the shed out.
//
//	RATIO / CAP KEY SETS, shown identical:
//	  summary.average_weight_kg = total_weight_kg / animals_weighed. Both range over the
//	  SAME key set as the daily-gain headline denominator: individual tags with a
//	  selected-window growth endpoint, plus whole-shed pens with first/latest weighed dates
//	  in the selected window. It is a weighted mean over ANIMALS, never an average of
//	  per-shed averages.
//	  at_or_above_30kg / 35kg range over the SAME key set as animals_weighed: each individual
//	  tag at its latest selected-window weight, plus each whole-shed pen counted ALL-OR-NONE
//	  at the pen's latest average (maintainer decision 2026-09-03, "include lump-sum also";
//	  the identical trade the band board took on 2026-09-01). threshold_basis_animals is
//	  therefore equal to animals_weighed and travels with the counts so a renderer never
//	  pairs them with a narrower denominator, which is what the pre-2026-09-03 shape needed.
//
//	DISJOINT SOURCES, NOT DOUBLE-COUNTED: weighing_category is fixed at bucket creation and
//	the write path only ever inserts into ONE of weighing_observations (per-animal) or
//	weighing_shed_observations (lump-sum) for a given bucket. animals_weighed therefore
//	SELECTS by category rather than summing the two. It must not null-test them instead:
//	ind.scan_count is a count() and returns 0, never NULL, for a bucket with no scans — the
//	exact defect 000080 had to repair in the ceo_ai twin of this rollup.
//
// SCALE: bounded by tenant + an explicit park list + a business-date window on
// accepted_at, all of which are index-leading columns. The per-animal rollup runs
// as a correlated LATERAL carrying tenant_id so it uses
// weighing_observations_campaign_scanned_identifier_idx rather than seq-scanning
// once per bucket — the same fix measured in 000080 (3873ms -> 554ms at 400
// buckets x 300 observations).
func (r *Repository) GetShedWeights(ctx context.Context, tenantID string, scopeParkIDs []string, selectedParkID string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string, saleThresholdToleranceKg float64) (domain.ShedWeights, error) {
	weighingCategory = strings.TrimSpace(weighingCategory)
	saleThresholdLowerKg := domain.SaleThresholdLowerKg - saleThresholdToleranceKg
	if saleThresholdLowerKg < 0 {
		saleThresholdLowerKg = 0
	}
	saleThresholdUpperKg := domain.SaleThresholdUpperKg - saleThresholdToleranceKg
	if saleThresholdUpperKg < 0 {
		saleThresholdUpperKg = 0
	}
	// The ROWS honour the selection; the VOCABULARY below is built from the whole scope. Keeping
	// them separate is the fix for a dropdown that collapsed to the park already chosen.
	parkIDs := scopeParkIDs
	if selectedParkID != "" {
		parkIDs = []string{selectedParkID}
	}
	// The Sex filter narrows WHICH WEIGHS are counted, never which sheds exist: sheds_in_scope
	// stays the whole scope, so "N of 65 sheds weighed" keeps one denominator across every filter
	// position and a reader can see that the male half covers fewer sheds. Resolving identity is
	// sex_scope.go's job — nothing in THIS file knows what an animal is; it is handed a list of
	// tag strings and a list of buckets.
	sexScope, scopeErr := r.resolveSexScope(ctx, tenantID, parkIDs, sex, periodStart, periodEnd)
	if scopeErr != nil {
		return domain.ShedWeights{}, scopeErr
	}
	// Origin (farm born / purchased) narrows the SAME rows through the SAME opaque shape, so the
	// two filters compose without this file learning what either of them means. Both selected at
	// once means the rows in BOTH, which is what a reader picking "Female" and "Purchased" asks
	// for; the unfiltered page resolves neither and runs the query it always ran.
	originScope, originErr := r.resolveOriginScope(ctx, tenantID, parkIDs, origin, periodStart, periodEnd)
	if originErr != nil {
		return domain.ShedWeights{}, originErr
	}
	sexApplied := strings.TrimSpace(sex) != ""
	originApplied := strings.TrimSpace(origin) != ""
	scope := IntersectScopes(sexScope, sexApplied, originScope, originApplied)
	sexFiltered := sexApplied || originApplied

	out := domain.ShedWeights{
		Rows:              []domain.ShedWeightsRow{},
		Parks:             []domain.GrowthPark{},
		LumpWeighingDates: []string{},
		// Initialized here, not only on the success path: the no-parks early return
		// below would otherwise leave this nil and serialize `by_load: null` against a
		// contract that declares an array.
		ByLoad: []domain.LoadGainBucket{},
	}
	if len(parkIDs) == 0 {
		return out, nil
	}
	loc := biztime.DefaultLocation()
	out.PeriodStart = periodStart.In(loc).Format("2006-01-02")
	// periodEnd arrives half-open; the label the screen shows is the inclusive last day.
	out.PeriodEnd = periodEnd.In(loc).AddDate(0, 0, -1).Format("2006-01-02")

	const q = ` -- scale-guard:ignore: bounded tenant + authorized-park weighing read; capped shed snapshot plus whole-filter KPI rollup share one DB round trip and are covered by adversarial projection tests.
WITH scoped AS (
  -- Every shed bucket the filters select, weighed or not. Canceled buckets are
  -- excluded: a canceled bucket is work that was called off, so counting it in
  -- sheds_in_scope would inflate the "20 of 27" denominator with sheds nobody
  -- intended to weigh.
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, cs.partition_label, cs.weighing_category,
         cs.status AS bucket_status, c.park_id, c.period_start_date, cs.created_at
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c
    ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND cs.status <> 'canceled'
    AND ($13::text = '' OR cs.weighing_category = $13::text)
),
ind AS (
  -- projection-review: membership=scoped buckets whose weighing_category is individual_animal, each joined to its own deduplicated scans; group_key=s.campaign_shed_id (weighing_campaign_sheds PK, so one row per bucket); join_cardinality=LATERAL latest is 1 row per DISTINCT scanned tag and the GROUP BY collapses it to exactly 1 row per bucket, no side multiplies; pagination=NONE, bounded by the outer LIMIT and the tenant+park+date window; scope=s.tenant_id carried into the correlated subquery
  --
  -- ONE ROW PER ANIMAL, NOT ONE PER CAPTURE.
  --
  -- weighing_observations keeps history instead of deleting (000061 stamps
  -- submitted_at on completion, 000073 collapses duplicate losers, a reopened
  -- bucket re-inserts for a tag that already has a row), so a bare count(*) sums
  -- every superseded round on top of the live one and reports more animals than
  -- the shed holds. Deduplicating by SCANNED TAG is the only fix that survives the
  -- lifecycle: filtering on state instead ("submitted_at IS NULL OR
  -- verification_status = 'rework'") reports ZERO for a normally-finished bucket,
  -- which is the terminal state of essentially all weighing work.
  --
  -- Identity here IS the tag: weighing is free-flow and 000078 dropped animal_id.
  -- Matched case- and whitespace-insensitively, the same grain as 000073's uidx. A
  -- blank tag cannot be collapsed with other blank tags, so it keys on its own row.
  SELECT s.campaign_shed_id,
         count(*)::int              AS animals,
         avg(latest.weight_kg)      AS avg_kg,
         sum(latest.weight_kg)      AS total_kg,
         max(latest.weigh_date)     AS last_weighed,
         count(*) FILTER (WHERE latest.weight_kg >= $5::numeric)::int AS ge_lower,
         count(*) FILTER (WHERE latest.weight_kg >= $6::numeric)::int AS ge_upper
  FROM scoped s
  JOIN LATERAL (
    SELECT DISTINCT ON (COALESCE(NULLIF(lower(btrim(o.scanned_identifier)), ''), o.observation_id::text))
           o.weight_kg,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS weigh_date
    FROM weighing_observations o
    WHERE o.tenant_id = s.tenant_id
      AND o.campaign_shed_id = s.campaign_shed_id
      AND o.accepted_at >= $3::timestamptz
      AND o.accepted_at <  $4::timestamptz
      -- A rejected proof is not a real weight. Pending IS included: an unverified
      -- weight is still a measurement, matching weight_history.go and growth.go.
      AND o.verification_status <> 'rejected'
      -- Sex filter. $8 is FALSE for the unfiltered page, so this query runs exactly as it did
      -- before the filter existed; when it is on, an EMPTY tag list correctly matches nothing
      -- rather than silently meaning "everyone", which is why the flag is a separate bind.
      AND (NOT $8::bool OR lower(btrim(o.scanned_identifier)) = ANY($9::text[]))
    ORDER BY COALESCE(NULLIF(lower(btrim(o.scanned_identifier)), ''), o.observation_id::text),
             o.accepted_at DESC, o.observation_id DESC
  ) latest ON true
  WHERE s.weighing_category = 'individual_animal'
  GROUP BY s.campaign_shed_id
),
lump AS (
  -- projection-review: membership=scoped buckets whose weighing_category is per_shed_partition, joined to their LIVE shed observation; group_key=s.campaign_shed_id (no aggregation -- the uidx makes this 1:1); join_cardinality=weighing_shed_observations 0..1 GIVEN withdrawn_at IS NULL (UNIQUE weighing_shed_observations_one_open_scope_uidx, PARTIAL, per 000067); pagination=NONE, bounded by the outer LIMIT and the tenant+park+date window; scope=s.tenant_id joined explicitly
  --
  -- withdrawn_at IS REQUIRED, not decorative: 000067 replaced the total
  -- uniqueness on this table with a PARTIAL one over live rows, so a reopened and
  -- resubmitted bucket legitimately holds several rows and joining them all fans
  -- the shed out past its declared grain.
  SELECT s.campaign_shed_id,
         sh.animal_count                                          AS animals,
         sh.average_weight_kg                                     AS avg_kg,
         sh.weight_kg                                             AS total_kg,
         (sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date        AS last_weighed
  FROM scoped s
  JOIN weighing_shed_observations sh
    ON sh.campaign_shed_id = s.campaign_shed_id
   AND sh.tenant_id = s.tenant_id
   AND sh.withdrawn_at IS NULL
   AND sh.accepted_at >= $3::timestamptz
   AND sh.accepted_at <  $4::timestamptz
   AND sh.verification_status <> 'rejected'
  WHERE s.weighing_category = 'per_shed_partition'
    -- A whole-shed weigh carries no tag, so it is claimed only when its shed's cohort is
    -- entirely this sex (sex_scope.go proves that); a shed holding both is claimed by neither
    -- side rather than split, because one shed average cannot be divided between two cohorts.
    AND (NOT $8::bool OR EXISTS (
      SELECT 1 FROM unnest($10::uuid[], $11::text[]) AS b(loc, part)
      WHERE b.loc = s.location_id AND b.part = COALESCE(s.partition_label, '')
    ))
),
-- Whole-shed movement inside the selected window, per operational row
-- (location_id + partition_label) rather than per bucket: each weigh of a shed is
-- its own bucket, so the history lives across buckets and a per-bucket view sees
-- one point and no trend.
--
-- Anchor on the first and latest weighed business dates inside the reader's
-- selected window. If a shed has only one weighed date inside the window, its
-- selected-range gain is unknown rather than borrowing an older four-week baseline.
shed_span AS (
  SELECT latest.location_id, latest.partition_label,
         (latest.average_weight_kg - first.average_weight_kg) * 1000.0
           / NULLIF(latest.d - first.d, 0) AS g_per_day,
         latest.d - first.d                AS span_days
  FROM (
    SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label, o.average_weight_kg,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id, COALESCE(cs2.partition_label, '') ORDER BY o.accepted_at DESC) AS rn
    FROM weighing_shed_observations o
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id
	    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id
	    WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
	      AND ($13::text = '' OR cs2.weighing_category = $13::text)
	      AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
      AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
      AND (NOT $8::bool OR EXISTS (
        SELECT 1 FROM unnest($10::uuid[], $11::text[]) AS b(loc, part)
        WHERE b.loc = cs2.location_id AND b.part = COALESCE(cs2.partition_label, '')
      ))
  ) latest
  JOIN (
    SELECT cs2.location_id, COALESCE(cs2.partition_label, '') AS partition_label, o.average_weight_kg,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id, COALESCE(cs2.partition_label, '') ORDER BY o.accepted_at ASC) AS rn
    FROM weighing_shed_observations o
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id
	    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id
	    WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
	      AND ($13::text = '' OR cs2.weighing_category = $13::text)
	      AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
      AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
      AND (NOT $8::bool OR EXISTS (
        SELECT 1 FROM unnest($10::uuid[], $11::text[]) AS b(loc, part)
        WHERE b.loc = cs2.location_id AND b.part = COALESCE(cs2.partition_label, '')
      ))
  ) first ON first.location_id = latest.location_id
    AND first.partition_label = latest.partition_label
    AND first.rn = 1
  WHERE latest.rn = 1 AND latest.d > first.d
),
per_bucket AS (
  SELECT s.*,
         COALESCE(ind.animals, lump.animals, 0)      AS animals,
         COALESCE(ind.avg_kg,  lump.avg_kg)          AS avg_kg,
         COALESCE(ind.total_kg, lump.total_kg)       AS total_kg,
         COALESCE(ind.last_weighed, lump.last_weighed) AS last_weighed,
         -- A whole-shed pen clears a threshold with ALL its animals when its average does,
         -- and with none when it does not: one shed average cannot be split into the kids
         -- above and below a line, so the pen is counted whole or not at all.
         COALESCE(ind.ge_lower, CASE WHEN lump.avg_kg >= $5::numeric THEN lump.animals END, 0) AS ge_lower,
         COALESCE(ind.ge_upper, CASE WHEN lump.avg_kg >= $6::numeric THEN lump.animals END, 0) AS ge_upper,
         COALESCE(ind.animals, lump.animals, 0)      AS threshold_basis
  FROM scoped s
  LEFT JOIN ind  ON ind.campaign_shed_id  = s.campaign_shed_id
  LEFT JOIN lump ON lump.campaign_shed_id = s.campaign_shed_id
),
latest_bucket AS (
  -- COLLAPSE TO SHED GRAIN. A shed weighed in consecutive campaigns owns several
  -- buckets; the screen asks "what does this shed weigh now", so the newest bucket
  -- WITH DATA wins. last_weighed NULLS LAST is what makes that true — ordering by
  -- period_start_date alone would let an empty newer bucket hide a weighed older
  -- one and report the shed as never weighed.
  SELECT DISTINCT ON (park_id, location_id, COALESCE(partition_label, '')) *
  FROM per_bucket
  ORDER BY park_id, location_id, COALESCE(partition_label, ''),
           (last_weighed IS NULL), last_weighed DESC,
           period_start_date DESC, created_at DESC, campaign_shed_id DESC
),
summary_individual AS (
  -- KPI GRAIN, shared with the daily-gain cards. A tag is counted here only when
  -- it has a prior weigh and a selected-window endpoint, so every visible "kids
  -- weighed" count on the page speaks about the same population.
  SELECT count(*)::int AS animals,
         sum(latest.weight_kg) AS total_kg,
         count(*) FILTER (WHERE latest.weight_kg >= $5::numeric)::int AS ge_lower,
         count(*) FILTER (WHERE latest.weight_kg >= $6::numeric)::int AS ge_upper
  FROM (
    SELECT DISTINCT ON (animal_key) animal_key, weight_kg
    FROM (
      SELECT animal_key, weight_kg, accepted_at, observation_id
      FROM (
        SELECT o.observation_id, o.weight_kg::float8 AS weight_kg, o.accepted_at,
               lower(btrim(o.scanned_identifier)) AS animal_key,
               LAG(o.weight_kg::float8) OVER w AS prev_weight,
               LAG(o.accepted_at) OVER w AS prev_accepted_at
        FROM scoped s
        JOIN weighing_observations o
          ON o.tenant_id = s.tenant_id
         AND o.campaign_shed_id = s.campaign_shed_id
        WHERE s.weighing_category = 'individual_animal'
          AND o.accepted_at >= ($3::timestamptz - ($12::int * INTERVAL '1 day'))
          AND o.accepted_at <  $4::timestamptz
          AND o.verification_status <> 'rejected'
          AND (NOT $8::bool OR lower(btrim(o.scanned_identifier)) = ANY($9::text[]))
        WINDOW w AS (PARTITION BY lower(btrim(o.scanned_identifier)) ORDER BY o.accepted_at, o.observation_id)
      ) pairs
      WHERE prev_weight IS NOT NULL
        AND ((accepted_at AT TIME ZONE 'Asia/Kolkata')::date
             - (prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date) > 0
        AND accepted_at >= $3::timestamptz
    ) qualifying
    ORDER BY animal_key, accepted_at DESC, observation_id DESC
  ) latest
),
summary_lump_points AS (
  SELECT s.park_id, s.location_id, COALESCE(s.partition_label, '') AS partition_label,
         sh.shed_observation_id, sh.animal_count, sh.weight_kg, sh.average_weight_kg, sh.accepted_at,
         (sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM scoped s
  JOIN weighing_shed_observations sh
    ON sh.campaign_shed_id = s.campaign_shed_id
   AND sh.tenant_id = s.tenant_id
   AND sh.withdrawn_at IS NULL
   AND sh.accepted_at >= $3::timestamptz
   AND sh.accepted_at <  $4::timestamptz
   AND sh.verification_status <> 'rejected'
  WHERE s.weighing_category = 'per_shed_partition'
    AND (NOT $8::bool OR EXISTS (
      SELECT 1 FROM unnest($10::uuid[], $11::text[]) AS b(loc, part)
      WHERE b.loc = s.location_id AND b.part = COALESCE(s.partition_label, '')
    ))
),
summary_lump_latest AS (
  SELECT DISTINCT ON (park_id, location_id, partition_label)
         park_id, location_id, partition_label, animal_count, weight_kg, average_weight_kg, d
  FROM summary_lump_points
  ORDER BY park_id, location_id, partition_label, accepted_at DESC, shed_observation_id DESC
),
summary_lump_first AS (
  SELECT DISTINCT ON (park_id, location_id, partition_label)
         park_id, location_id, partition_label, d
  FROM summary_lump_points
  ORDER BY park_id, location_id, partition_label, accepted_at ASC, shed_observation_id ASC
),
summary_lump AS (
  -- Same whole-shed denominator as growth.go: a pen contributes only when there
  -- is a prior point and a latest point, and it contributes the latest head count.
  --
  -- THRESHOLDS COUNT THE PEN WHOLE OR NOT AT ALL (maintainer decision 2026-09-03). A pen
  -- whose latest average clears 30 kg puts every one of its animals over the line; a pen
  -- under it puts none. Splitting the head count by the average would invent a
  -- distribution nobody measured -- the same all-at-the-pen-average trade the band
  -- board recorded on 2026-09-01, extended to the two sale-threshold cards.
  SELECT COALESCE(sum(l.animal_count), 0)::int AS animals,
         sum(l.weight_kg) AS total_kg,
         COALESCE(sum(l.animal_count) FILTER (WHERE l.average_weight_kg >= $5::numeric), 0)::int AS ge_lower,
         COALESCE(sum(l.animal_count) FILTER (WHERE l.average_weight_kg >= $6::numeric), 0)::int AS ge_upper
  FROM summary_lump_latest l
  JOIN summary_lump_first f
    ON f.park_id = l.park_id
   AND f.location_id = l.location_id
   AND f.partition_label = l.partition_label
  WHERE l.d > f.d
),
summary_rollup AS (
  SELECT
    COALESCE(si.animals, 0) + COALESCE(sl.animals, 0) AS animals_weighed,
    COALESCE(si.animals, 0) AS individual_animals_weighed,
    COALESCE(sl.animals, 0) AS lump_sum_animals_weighed,
    COALESCE(si.total_kg, 0) + COALESCE(sl.total_kg, 0) AS total_weight_kg,
    COALESCE(si.ge_lower, 0) + COALESCE(sl.ge_lower, 0) AS ge_lower,
    COALESCE(si.ge_upper, 0) + COALESCE(sl.ge_upper, 0) AS ge_upper,
    COALESCE(si.animals, 0) + COALESCE(sl.animals, 0) AS threshold_basis
  FROM summary_individual si CROSS JOIN summary_lump sl
)
SELECT b.location_id, b.park_id,
       COALESCE(NULLIF(pk.location_code, ''), pk.name, ''), COALESCE(sh.name, ''),
       COALESCE(b.partition_label, ''),
       b.weighing_category, b.bucket_status,
       b.animals, b.avg_kg, b.total_kg, b.last_weighed,
       b.ge_lower, b.ge_upper, b.threshold_basis,
       count(*) OVER()::int AS summary_sheds_in_scope,
       count(*) FILTER (WHERE b.animals > 0) OVER()::int AS summary_sheds_weighed,
       sr.animals_weighed::int AS summary_animals_weighed,
       sr.individual_animals_weighed::int AS summary_individual_animals_weighed,
       sr.lump_sum_animals_weighed::int AS summary_lump_sum_animals_weighed,
       sr.total_weight_kg::float8 AS summary_total_weight_kg,
       sr.ge_lower::int AS summary_ge_lower,
       sr.ge_upper::int AS summary_ge_upper,
       sr.threshold_basis::int AS summary_threshold_basis,
       ss.g_per_day,
       COALESCE(ss.span_days, 0)
FROM latest_bucket b
CROSS JOIN summary_rollup sr
LEFT JOIN shed_span ss
  ON ss.location_id = b.location_id
 AND ss.partition_label = COALESCE(b.partition_label, '')
LEFT JOIN locations sh ON sh.location_id = b.location_id
LEFT JOIN locations pk ON pk.location_id = b.park_id
ORDER BY COALESCE(NULLIF(pk.location_code, ''), pk.name, ''), COALESCE(sh.name, '')
LIMIT $7`

	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs,
		periodStart, periodEnd,
		saleThresholdLowerKg, saleThresholdUpperKg,
		domain.MaxShedWeightsRows,
		sexFiltered, scope.Tags, scope.LocationIDs, scope.PartitionLabels,
		growthLookbackDays, weighingCategory)
	if err != nil {
		return domain.ShedWeights{}, err
	}
	defer rows.Close()

	var summary domain.ShedWeightsSummary
	for rows.Next() {
		var (
			row         domain.ShedWeightsRow
			animals     int
			avgKg       *float64
			totalKg     *float64
			lastWeighed *time.Time
			geLower     int
			geUpper     int
			basis       int
			summaryRows domain.ShedWeightsSummary
			shedGain    *float64
			spanDays    int
		)
		if err := rows.Scan(&row.LocationID, &row.ParkID, &row.ParkName, &row.ShedDisplayName,
			&row.PartitionLabel,
			&row.WeighingCategory, &row.BucketStatus,
			&animals, &avgKg, &totalKg, &lastWeighed,
			&geLower, &geUpper, &basis,
			&summaryRows.ShedsInScope,
			&summaryRows.ShedsWeighed,
			&summaryRows.AnimalsWeighed,
			&summaryRows.IndividualAnimalsWeighed,
			&summaryRows.LumpSumAnimalsWeighed,
			&summaryRows.TotalWeightKg,
			&summaryRows.AtOrAbove30Kg,
			&summaryRows.AtOrAbove35Kg,
			&summaryRows.ThresholdBasisAnimals,
			&shedGain, &spanDays); err != nil {
			return domain.ShedWeights{}, err
		}
		// Same doubling guard as the sibling composer in growth.go and growthdirector's
		// operationalLabel. `ShedDisplayName` is the weighing bucket's own planning label, and
		// a partitioned bucket is routinely named for the pen it covers -- "Castro 2",
		// "Godel 2 - Part 2" -- so handing it to oploc, which appends the partition to a SHED
		// name, produced "Castro 2 2" and "Godel 2 - Part 2 - Part 2". It surfaced when the
		// gain chart dropped its "(shed avg, Nd)" suffix and the bare label was all that was
		// left; the table column carried it too.
		if row.PartitionLabel != "" && strings.HasSuffix(row.ShedDisplayName, row.PartitionLabel) {
			row.OperationalLocationDisplay = row.ShedDisplayName
		} else {
			row.OperationalLocationDisplay = (oploc.OperationalLocation{
				ShedID:         row.LocationID,
				ShedName:       row.ShedDisplayName,
				PartitionLabel: row.PartitionLabel,
			}).Display()
		}
		row.AnimalsWeighed = animals
		if avgKg != nil {
			row.AverageWeightKg = *avgKg
		}
		if totalKg != nil {
			row.TotalWeightKg = *totalKg
		}
		if lastWeighed != nil {
			row.LastWeighedDate = lastWeighed.Format("2006-01-02")
		}
		row.ShedAverageGainGPerDay = shedGain
		row.GainSpanDays = spanDays
		out.Rows = append(out.Rows, row)
		summary = summaryRows
	}
	if err := rows.Err(); err != nil {
		return domain.ShedWeights{}, err
	}
	// Park vocabulary for the filter, labelled the same way the rows are. It is read
	// here rather than from ListParks because that helper is shared with the mobile
	// planner and returns the full name; the two would then disagree on screen, with
	// the dropdown saying "Coimbatore" and every row saying "CBE".
	parkRows, err := r.pool.Query(ctx, `
SELECT location_id::text, COALESCE(NULLIF(location_code, ''), name, '')
FROM locations
WHERE tenant_id = $1::uuid AND location_id = ANY($2::uuid[])
ORDER BY display_order, name, location_id`, tenantID, scopeParkIDs)
	if err != nil {
		return domain.ShedWeights{}, err
	}
	defer parkRows.Close()
	for parkRows.Next() {
		var park domain.GrowthPark
		if err := parkRows.Scan(&park.ParkID, &park.Name); err != nil {
			return domain.ShedWeights{}, err
		}
		out.Parks = append(out.Parks, park)
	}
	if err := parkRows.Err(); err != nil {
		return domain.ShedWeights{}, err
	}

	// The two date facts come from ONE helper, shared with the narrow /weighing/weighing-dates read
	// the Weights screens resolve their landing window from. Two copies of these queries would let
	// the window a page opens on disagree with the dates the same page then reports.
	dates, err := r.weighingDates(ctx, tenantID, parkIDs, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
	if err != nil {
		return domain.ShedWeights{}, err
	}
	out.LumpWeighingDates = dates.LumpWeighingDates
	out.LatestWeighingDate = dates.LatestWeighingDate

	if summary.AnimalsWeighed > 0 {
		avg := summary.TotalWeightKg / float64(summary.AnimalsWeighed)
		summary.AverageWeightKg = &avg
	}
	out.Summary = summary

	// Growth per procurement load, over the same tenant/park/window scope. Its own
	// read rather than another CTE here: it collapses to LOAD grain, not shed grain,
	// and folding a different grain into this query is how a shed ends up counted
	// once per load it touches.
	byLoad, unattributed, err := r.loadWeights(ctx, tenantID, parkIDs, periodStart, periodEnd, sexFiltered, scope, weighingCategory)
	if err != nil {
		return domain.ShedWeights{}, err
	}
	out.ByLoad = byLoad
	out.LoadUnattributedSheds = unattributed
	return out, nil
}
