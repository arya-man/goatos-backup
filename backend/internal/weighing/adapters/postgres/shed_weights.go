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
//	  SAME key set — every (park_id, location_id) in `scoped` that has a weigh — because
//	  both are summed from the same per-shed rows this query already emits. It is a
//	  weighted mean over ANIMALS, never an average of per-shed averages.
//	  at_or_above_30kg / 35kg range over a STRICTLY NARROWER key set: individual_animal
//	  sheds only. That is why threshold_basis_animals is returned as their own denominator
//	  rather than reusing animals_weighed, which spans both categories.
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
func (r *Repository) GetShedWeights(ctx context.Context, tenantID string, scopeParkIDs []string, selectedParkID string, periodStart, periodEnd time.Time) (domain.ShedWeights, error) {
	// The ROWS honour the selection; the VOCABULARY below is built from the whole scope. Keeping
	// them separate is the fix for a dropdown that collapsed to the park already chosen.
	parkIDs := scopeParkIDs
	if selectedParkID != "" {
		parkIDs = []string{selectedParkID}
	}
	out := domain.ShedWeights{
		Rows:  []domain.ShedWeightsRow{},
		Parks: []domain.GrowthPark{},
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

	const q = `
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
),
-- Whole-shed movement over the four-week baseline, per LOCATION rather than per
-- bucket: each weigh of a shed is its own bucket, so the history lives across
-- buckets and a per-bucket view sees one point and no trend.
--
-- Anchor on the latest weigh in the selected window, then compare it with the
-- weigh closest to 28 days before that latest date. A weekly tolerance is allowed
-- for slipped farm capture dates, but we never fall back to the immediately
-- previous row when the only older data is too recent: that recreates the noisy
-- last-two figure this screen is no longer meant to show.
shed_span AS (
  SELECT latest.location_id,
         (latest.average_weight_kg - baseline.average_weight_kg) * 1000.0
           / NULLIF(latest.d - baseline.d, 0) AS g_per_day,
         latest.d - baseline.d               AS span_days
  FROM (
    SELECT cs2.location_id, o.average_weight_kg,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           row_number() OVER (PARTITION BY cs2.location_id ORDER BY o.accepted_at DESC) AS rn
    FROM weighing_shed_observations o
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id
    WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
      AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
  ) latest
  JOIN LATERAL (
    SELECT cs2.location_id, o.average_weight_kg,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
    FROM weighing_shed_observations o
    JOIN weighing_campaign_sheds cs2 ON cs2.campaign_shed_id = o.campaign_shed_id
    JOIN weighing_campaigns c2 ON c2.campaign_id = cs2.campaign_id
    WHERE o.tenant_id = $1::uuid AND c2.park_id = ANY($2::uuid[])
      AND cs2.location_id = latest.location_id
      AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
      AND (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date < latest.d
      AND abs((o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date - (latest.d - 28)) <= 7
    ORDER BY abs((o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date - (latest.d - 28)),
             (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date DESC,
             o.accepted_at DESC
    LIMIT 1
  ) baseline ON true
  WHERE latest.rn = 1
),
per_bucket AS (
  SELECT s.*,
         COALESCE(ind.animals, lump.animals, 0)      AS animals,
         COALESCE(ind.avg_kg,  lump.avg_kg)          AS avg_kg,
         COALESCE(ind.total_kg, lump.total_kg)       AS total_kg,
         COALESCE(ind.last_weighed, lump.last_weighed) AS last_weighed,
         COALESCE(ind.ge_lower, 0)                   AS ge_lower,
         COALESCE(ind.ge_upper, 0)                   AS ge_upper,
         CASE WHEN s.weighing_category = 'individual_animal'
              THEN COALESCE(ind.animals, 0) ELSE 0 END AS threshold_basis
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
)
SELECT b.location_id, b.park_id,
       COALESCE(NULLIF(pk.location_code, ''), pk.name, ''), COALESCE(sh.name, ''),
       COALESCE(b.partition_label, ''),
       b.weighing_category, b.bucket_status,
       b.animals, b.avg_kg, b.total_kg, b.last_weighed,
       b.ge_lower, b.ge_upper, b.threshold_basis,
       count(*) OVER()::int AS summary_sheds_in_scope,
       count(*) FILTER (WHERE b.animals > 0) OVER()::int AS summary_sheds_weighed,
       COALESCE(sum(CASE WHEN b.animals > 0 THEN b.animals ELSE 0 END) OVER(), 0)::int AS summary_animals_weighed,
       COALESCE(sum(CASE WHEN b.animals > 0 THEN b.total_kg ELSE 0 END) OVER(), 0)::float8 AS summary_total_weight_kg,
       COALESCE(sum(b.ge_lower) OVER(), 0)::int AS summary_ge_lower,
       COALESCE(sum(b.ge_upper) OVER(), 0)::int AS summary_ge_upper,
       COALESCE(sum(b.threshold_basis) OVER(), 0)::int AS summary_threshold_basis,
       ss.g_per_day,
       COALESCE(ss.span_days, 0)
FROM latest_bucket b
LEFT JOIN shed_span ss ON ss.location_id = b.location_id
LEFT JOIN locations sh ON sh.location_id = b.location_id
LEFT JOIN locations pk ON pk.location_id = b.park_id
ORDER BY COALESCE(NULLIF(pk.location_code, ''), pk.name, ''), COALESCE(sh.name, '')
LIMIT $7`

	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs,
		periodStart, periodEnd,
		domain.SaleThresholdLowerKg, domain.SaleThresholdUpperKg,
		domain.MaxShedWeightsRows)
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

	if summary.AnimalsWeighed > 0 {
		avg := summary.TotalWeightKg / float64(summary.AnimalsWeighed)
		summary.AverageWeightKg = &avg
	}
	out.Summary = summary

	// Growth per procurement load, over the same tenant/park/window scope. Its own
	// read rather than another CTE here: it collapses to LOAD grain, not shed grain,
	// and folding a different grain into this query is how a shed ends up counted
	// once per load it touches.
	byLoad, unattributed, err := r.loadWeights(ctx, tenantID, parkIDs, periodStart, periodEnd)
	if err != nil {
		return domain.ShedWeights{}, err
	}
	out.ByLoad = byLoad
	out.LoadUnattributedSheds = unattributed
	return out, nil
}
