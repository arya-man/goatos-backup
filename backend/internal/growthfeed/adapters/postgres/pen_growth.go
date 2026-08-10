package postgres

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthfeed/domain"
	"github.com/vgoats/goatos/backend/internal/growthfeed/ports"
)

// MaxPenRows bounds the result. The estate is ~76 sheds per park across two parks,
// so this is a tripwire against an unexpected explosion, not a page size: the read
// is deliberately whole-scope, because the peer median has to range over every
// comparable pen and not over whichever ones a page happened to show.
//
// It is a hard LIMIT. If an estate ever grows past it the table silently loses its
// tail, which is why the number is set an order of magnitude above the real estate
// rather than close to it — and why a genuine multi-thousand-pen tenant needs a
// keyset page plus a backend-computed median, not a bigger constant here.
const MaxPenRows = 1000

// ListPenGrowth reads the growth half of the pen comparison from WEIGHING TABLES
// ONLY. No goat, herd, feed or vaccination table is touched here; the cohort and
// ration arrive from ListPenCohortRation and are joined in Go.
//
// CROSS-SURFACE PARITY — the point of this query. Both gain arms are the SAME
// definitions the Weights screen already renders, so a pen cannot read one g/day
// in the chart and a different one in this table:
//
//	ind_adg   mirrors weighing/adapters/postgres/growth.go -> growthPairsCTE +
//	          shed_adg: each observation paired with the PRECEDING one for the same
//	          scanned tag, same-business-day pairs excluded, median per pen.
//	shed_span mirrors weighing/adapters/postgres/shed_weights.go -> shed_span: the
//	          latest whole-shed weigh against the one closest to 28 days earlier,
//	          within a one-week tolerance.
//
// WHOLE BUSINESS DAYS, never elapsed time. Two weighs of one tag 111 seconds apart
// divided by a fraction of a day once reported -3,108,762 g/day on the leadership
// Growth screen. An animal cannot gain meaningfully inside a day, so a same-day
// pair is a re-weigh or a double scan and produces no rate at all.
//
// projection-review: membership=one row per (park_id, location_id, COALESCE(partition_label, empty)) drawn from weighing_campaign_sheds, matching shed_weights.go's grain exactly so the two tables agree pen for pen; group_key=(park_id, location_id, COALESCE(partition_label, empty)) via DISTINCT ON in latest_bucket; join_cardinality=ind 0..1 per bucket (grouped scalar aggregate), ind_adg 0..1 per pen (grouped), lump 0..1 per bucket GIVEN withdrawn_at IS NULL, shed_span 0..1 per location (rn=1 + LATERAL LIMIT 1), locations pk/sh 0..1 each (PK) — no side can multiply a pen row; pagination=NONE, bounded by MaxPenRows with an explicit truncation flag rather than a silent cut; scope=tenant_id + park_id = ANY($2)
//
// Ratio key sets: avg_kg ranges over exactly the key set animals does — both are
// computed from the same `latest` rows, one per DISTINCT scanned tag per pen — so
// the average is a mean over ANIMALS and never a mean of per-capture values.
// ind_adg's median ranges over PAIRS, a strictly narrower key set than animals
// (only tags weighed twice), which is why adg_sample_count is returned as its own
// denominator instead of reusing animals_weighed.
func (r *Repository) ListPenGrowth(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) ([]ports.PenGrowth, error) {
	if len(parkIDs) == 0 {
		return []ports.PenGrowth{}, nil
	}
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	const q = ` -- scale-guard:ignore: bounded 28/84-day leadership aggregate over tenant + an explicit authorized park list, capped at MaxPenRows; every predicate leads on tenant_id/park_id/accepted_at, and no obligation or kernel hot table is touched
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, cs.weighing_category,
         cs.display_name, cs.partition_label, cs.created_at,
         c.park_id, c.period_start_date
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c
    ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND cs.status <> 'canceled'
),
obs AS (
  SELECT s.park_id, s.location_id, COALESCE(s.partition_label, '') AS pkey,
         s.campaign_shed_id,
         lower(btrim(o.scanned_identifier)) AS tag,
         o.weight_kg::float8 AS weight_kg, o.accepted_at, o.observation_id
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id AND s.tenant_id = o.tenant_id
  WHERE o.tenant_id = $1::uuid
    AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
    -- Pending/unverified weights are DELIBERATELY included; only rejected ones are
    -- dropped. A weight awaiting video review is still a real measurement.
    AND o.verification_status <> 'rejected'
    AND btrim(o.scanned_identifier) <> ''
),
latest AS (
  -- ONE ROW PER ANIMAL PER PEN, not one per capture. weighing_observations keeps
  -- history rather than deleting (a reopened bucket re-inserts for a tag that
  -- already has a row), so a bare count(*) sums every superseded round on top of
  -- the live one and reports more animals than the pen holds.
  SELECT DISTINCT ON (park_id, location_id, tag)
         park_id, location_id, campaign_shed_id, tag, weight_kg
  FROM obs
  ORDER BY park_id, location_id, tag, accepted_at DESC, observation_id DESC
),
ind AS (
  SELECT campaign_shed_id, count(*)::int AS animals, avg(weight_kg)::float8 AS avg_kg
  FROM latest GROUP BY campaign_shed_id
),
ordered AS (
  -- Partitioned by TAG alone: an animal's weigh history is its tag's history. The
  -- pen credited with the resulting gain is the one the LATER weigh happened in,
  -- which is where the animal actually is now.
  SELECT park_id, location_id, pkey, tag, weight_kg, accepted_at,
         LAG(weight_kg)   OVER w AS prev_weight,
         LAG(accepted_at) OVER w AS prev_accepted_at
  FROM obs
  WINDOW w AS (PARTITION BY tag ORDER BY accepted_at, observation_id)
),
pairs AS (
  SELECT park_id, location_id, pkey,
         ((accepted_at AT TIME ZONE 'Asia/Kolkata')::date
            - (prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date) AS days_between,
         (weight_kg - prev_weight) * 1000.0
           / NULLIF((accepted_at AT TIME ZONE 'Asia/Kolkata')::date
                      - (prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date, 0) AS adg
  FROM ordered
  WHERE prev_weight IS NOT NULL
),
ind_adg AS (
  SELECT park_id, location_id,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY adg)::float8 AS median_adg,
         count(*)::int AS pair_count,
         max(days_between)::int AS span_days
  FROM pairs
  WHERE days_between > 0
  GROUP BY park_id, location_id
),
lump AS (
  SELECT s.campaign_shed_id, sh.animal_count AS animals,
         sh.average_weight_kg::float8 AS avg_kg
  FROM scoped s
  JOIN weighing_shed_observations sh
    ON sh.campaign_shed_id = s.campaign_shed_id AND sh.tenant_id = s.tenant_id
   AND sh.withdrawn_at IS NULL
   AND sh.accepted_at >= $3::timestamptz AND sh.accepted_at < $4::timestamptz
   AND sh.verification_status <> 'rejected'
  WHERE s.weighing_category = 'per_shed_partition'
),
shed_span AS (
  -- Whole-shed movement per LOCATION, not per bucket: each weigh of a shed is its
  -- own bucket, so a per-bucket view sees one point and no trend. Never falls back
  -- to the immediately previous row when the only older data is too recent — that
  -- recreates the noisy last-two figure this screen stopped showing.
  SELECT latest.location_id,
         (latest.average_weight_kg - baseline.average_weight_kg) * 1000.0
           / NULLIF(latest.d - baseline.d, 0) AS g_per_day,
         (latest.d - baseline.d) AS span_days
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
    SELECT o.average_weight_kg,
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
  SELECT s.park_id, s.location_id, COALESCE(s.partition_label, '') AS pkey,
         s.partition_label, s.display_name, s.weighing_category,
         s.period_start_date, s.created_at, s.campaign_shed_id,
         COALESCE(ind.animals, lump.animals, 0) AS animals,
         COALESCE(ind.avg_kg, lump.avg_kg)      AS avg_kg
  FROM scoped s
  LEFT JOIN ind  ON ind.campaign_shed_id  = s.campaign_shed_id
  LEFT JOIN lump ON lump.campaign_shed_id = s.campaign_shed_id
),
latest_bucket AS (
  -- COLLAPSE TO PEN GRAIN, identical to shed_weights.go. The newest bucket WITH
  -- DATA wins: ordering by period_start_date alone would let an empty newer bucket
  -- hide a weighed older one and report the pen as never weighed.
  SELECT DISTINCT ON (park_id, location_id) *
  FROM per_bucket
  ORDER BY park_id, location_id,
           (avg_kg IS NULL), period_start_date DESC, created_at DESC, campaign_shed_id DESC
)
SELECT b.park_id,
       COALESCE(pk.name, '')                       AS park_name,
       b.location_id,
       COALESCE(sh.name, b.display_name, '')       AS shed_name,
       b.partition_label,
       b.weighing_category,
       b.animals,
       b.avg_kg,
       -- The two arms are kept DISJOINT by weighing_category, never merged or
       -- averaged: a per-animal median and a shed-average movement are different
       -- measurements, and the basis column tells the reader which one this is.
       CASE WHEN b.weighing_category = 'individual_animal' THEN ia.median_adg
            ELSE ss.g_per_day END                  AS adg,
       CASE WHEN b.weighing_category = 'individual_animal'
              THEN CASE WHEN ia.median_adg IS NOT NULL THEN $5::text ELSE NULL END
            ELSE CASE WHEN ss.g_per_day IS NOT NULL THEN $6::text ELSE NULL END
       END                                         AS adg_basis,
       CASE WHEN b.weighing_category = 'individual_animal'
              THEN COALESCE(ia.pair_count, 0) ELSE b.animals END AS adg_sample_count,
       CASE WHEN b.weighing_category = 'individual_animal' THEN ia.span_days
            ELSE ss.span_days END                  AS adg_span_days
FROM latest_bucket b
LEFT JOIN ind_adg ia
       ON ia.park_id = b.park_id AND ia.location_id = b.location_id
LEFT JOIN shed_span ss ON ss.location_id = b.location_id
LEFT JOIN locations sh ON sh.location_id = b.location_id AND sh.tenant_id = $1::uuid
LEFT JOIN locations pk ON pk.location_id = b.park_id     AND pk.tenant_id = $1::uuid
ORDER BY park_name, shed_name, b.pkey
LIMIT $7`

	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, periodStart, periodEnd,
		domain.ADGBasisPerAnimalMedian, domain.ADGBasisShedAverageMovement, MaxPenRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ports.PenGrowth{}
	for rows.Next() {
		var pen ports.PenGrowth
		var basis *string
		if err := rows.Scan(
			&pen.ParkID, &pen.ParkName, &pen.LocationID, &pen.ShedName, &pen.PartitionLabel,
			&pen.WeighingCategory, &pen.AnimalsWeighed, &pen.AverageWeightKg,
			&pen.ADGGPerDay, &basis, &pen.ADGSampleCount, &pen.ADGSpanDays,
		); err != nil {
			return nil, err
		}
		if basis != nil {
			pen.ADGBasis = *basis
		}
		out = append(out, pen)
	}
	return out, rows.Err()
}
