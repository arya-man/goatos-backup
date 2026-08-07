package postgres

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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
func (r *Repository) GetShedWeights(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.ShedWeights, error) {
	out := domain.ShedWeights{
		Rows:  []domain.ShedWeightsRow{},
		Parks: []domain.GrowthPark{},
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
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, cs.weighing_category,
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
  SELECT DISTINCT ON (park_id, location_id) *
  FROM per_bucket
  ORDER BY park_id, location_id,
           (last_weighed IS NULL), last_weighed DESC,
           period_start_date DESC, created_at DESC, campaign_shed_id DESC
)
SELECT b.location_id, b.park_id,
       COALESCE(pk.name, ''), COALESCE(sh.name, ''),
       b.weighing_category, b.bucket_status,
       b.animals, b.avg_kg, b.total_kg, b.last_weighed,
       b.ge_lower, b.ge_upper, b.threshold_basis,
       count(*) OVER()::int AS summary_sheds_in_scope,
       count(*) FILTER (WHERE b.animals > 0) OVER()::int AS summary_sheds_weighed,
       COALESCE(sum(CASE WHEN b.animals > 0 THEN b.animals ELSE 0 END) OVER(), 0)::int AS summary_animals_weighed,
       COALESCE(sum(CASE WHEN b.animals > 0 THEN b.total_kg ELSE 0 END) OVER(), 0)::float8 AS summary_total_weight_kg,
       COALESCE(sum(b.ge_lower) OVER(), 0)::int AS summary_ge_lower,
       COALESCE(sum(b.ge_upper) OVER(), 0)::int AS summary_ge_upper,
       COALESCE(sum(b.threshold_basis) OVER(), 0)::int AS summary_threshold_basis
FROM latest_bucket b
LEFT JOIN locations sh ON sh.location_id = b.location_id
LEFT JOIN locations pk ON pk.location_id = b.park_id
ORDER BY COALESCE(pk.name, ''), COALESCE(sh.name, '')
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
		)
		if err := rows.Scan(&row.LocationID, &row.ParkID, &row.ParkName, &row.ShedDisplayName,
			&row.WeighingCategory, &row.BucketStatus,
			&animals, &avgKg, &totalKg, &lastWeighed,
			&geLower, &geUpper, &basis,
			&summaryRows.ShedsInScope,
			&summaryRows.ShedsWeighed,
			&summaryRows.AnimalsWeighed,
			&summaryRows.TotalWeightKg,
			&summaryRows.AtOrAbove30Kg,
			&summaryRows.AtOrAbove35Kg,
			&summaryRows.ThresholdBasisAnimals); err != nil {
			return domain.ShedWeights{}, err
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
		out.Rows = append(out.Rows, row)
		summary = summaryRows
	}
	if err := rows.Err(); err != nil {
		return domain.ShedWeights{}, err
	}
	if summary.AnimalsWeighed > 0 {
		avg := summary.TotalWeightKg / float64(summary.AnimalsWeighed)
		summary.AverageWeightKg = &avg
	}
	out.Summary = summary
	return out, nil
}
