package postgres

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// loadWeights returns growth per PROCUREMENT LOAD for the Weights screen.
//
// WEIGHING ISOLATION: reads weighing_campaign_sheds, weighing_campaigns,
// weighing_observations, weighing_shed_observations and weighing_shed_load_tags.
// Every one is a weighing_* table — the load mapping is weighing-owned precisely
// so this read needs no procurement table and no exception to the isolation lock.
// See 000123_weighing_shed_load_tags.sql for why the mapping lives here.
//
// projection-review: membership=one row per load_ref having at least one weighed shed tagged to EXACTLY that load; group_key=(t.load_ref, t.owner_name), where `tag` has already collapsed to one row per location_id via HAVING count(*) = 1; join_cardinality=daily is 1 row per (location_id, d) because it GROUPs on that pair, so ranked/shed_latest are 1 per location_id at rn = 1 and shed_gain is 0..1 per location_id (GROUP BY location_id), and `tag` is 0..1 per location_id — no side can multiply a shed into a load twice; pagination=NONE, bounded by the authored tag estate; scope=tenant_id + park_id = ANY($2) through weighing_campaigns, plus the half-open accepted_at window
//
//	PRODUCER UNIQUENESS vs CONSUMER MATCH KEYS, side by side:
//	  daily          unique on (location_id, d)            [its own GROUP BY]
//	  shed_latest    unique on (location_id)               [ranked rn = 1]
//	  shed_gain      unique on (location_id)               [its own GROUP BY]
//	  tag            unique on (location_id)               [GROUP BY + HAVING count(*) = 1]
//	  final SELECT   groups on (load_ref, owner_name)      [matches tag's carried columns]
//
//	ROW MULTIPLICITY OF EVERY JOINED SIDE (all 0..1 against a tagged shed):
//	  shed_latest    1   per location_id — the JOIN is what restricts loads to weighed sheds
//	  shed_gain      0..1 per location_id — absent when the shed was weighed only once
//
//	RATIO KEY SETS, shown identical:
//	  average_weight_kg = sum(avg_kg * animals) / sum(animals). Both range over the
//	  SAME key set: tagged sheds present in shed_latest.
//	  gain_g_per_day    = sum(g_per_day * animals) FILTER (WHERE g_per_day IS NOT NULL)
//	                    / sum(animals)             FILTER (WHERE g_per_day IS NOT NULL).
//	  The FILTER is repeated on BOTH sides deliberately, so numerator and denominator
//	  range over the identical narrower key set — tagged sheds weighed TWICE. Dropping
//	  it from the denominator would divide a two-shed gain by a five-shed head count
//	  and silently under-report every partly-weighed load.
//
//	WHY A SHED WITH TWO LOADS IS DROPPED RATHER THAN SPLIT: `tag`'s
//	HAVING count(*) = 1 excludes it, and it is counted in the unattributed scalar
//	instead. One shed average cannot be divided between two suppliers; apportioning
//	it by head count would invent a distribution nobody measured.
func (r *Repository) loadWeights(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) ([]domain.LoadGainBucket, int, error) {
	out := []domain.LoadGainBucket{}
	if len(parkIDs) == 0 {
		return out, 0, nil
	}

	const q = `
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, cs.weighing_category
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c
    ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND cs.status <> 'canceled'
),
lump_daily AS (
  -- withdrawn_at IS REQUIRED: 000067 made this table's uniqueness PARTIAL over live
  -- rows, so a reopened+resubmitted bucket legitimately keeps superseded rows and
  -- joining them all fans the shed-day out.
  SELECT s.location_id,
         (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
         o.average_weight_kg                               AS avg_kg,
         o.animal_count                                    AS animals
  FROM scoped s
  JOIN weighing_shed_observations o
    ON o.campaign_shed_id = s.campaign_shed_id
   AND o.tenant_id        = s.tenant_id
   AND o.withdrawn_at IS NULL
   AND o.verification_status <> 'rejected'
   AND o.accepted_at >= $3::timestamptz
   AND o.accepted_at <  $4::timestamptz
  WHERE s.weighing_category = 'per_shed_partition'
),
ind_daily AS (
  -- ONE ROW PER ANIMAL PER DAY, not one per capture: weighing_observations keeps
  -- superseded rows (000061/000073, and a reopened bucket re-scans a tag that
  -- already has one), so a bare avg() over captures weights re-scanned animals
  -- twice. Identity is the TAG, matched case- and whitespace-insensitively — the
  -- same grain as 000073's uidx — because 000078 dropped animal_id and weighing is
  -- free-flow. A blank tag cannot be collapsed with other blank tags, so it keys on
  -- its own row.
  SELECT s.location_id, x.d,
         avg(x.weight_kg) AS avg_kg,
         count(*)::int    AS animals
  FROM scoped s
  JOIN (
    SELECT DISTINCT ON (o.campaign_shed_id,
                        COALESCE(NULLIF(lower(btrim(o.scanned_identifier)), ''), o.observation_id::text),
                        (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date)
           o.campaign_shed_id,
           (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
           o.weight_kg
    FROM weighing_observations o
    WHERE o.tenant_id = $1::uuid
      AND o.accepted_at >= $3::timestamptz
      AND o.accepted_at <  $4::timestamptz
      -- A rejected proof is not a real weight. Pending IS included: an unverified
      -- weight is still a measurement, matching shed_weights.go and growth.go.
      AND o.verification_status <> 'rejected'
    ORDER BY o.campaign_shed_id,
             COALESCE(NULLIF(lower(btrim(o.scanned_identifier)), ''), o.observation_id::text),
             (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date,
             o.accepted_at DESC, o.observation_id DESC
  ) x ON x.campaign_shed_id = s.campaign_shed_id
  WHERE s.weighing_category = 'individual_animal'
  GROUP BY s.location_id, x.d
),
daily AS (
  -- BOTH capture modes on one timeline. The two sources are disjoint per bucket
  -- (weighing_category is fixed at creation and the write path fills exactly one),
  -- but a SHED can change mode between campaigns, so its history legitimately spans
  -- both tables and a mode-specific view would see one point and no trend.
  --
  -- The GROUP BY also guarantees ONE row per (location_id, d), which is what makes
  -- the row_number() below a clean last-two-weighs pick: a shed weighed twice in a
  -- day across two buckets would otherwise occupy both ranks and yield a zero-day
  -- span.
  SELECT location_id, d,
         sum(avg_kg * animals) / NULLIF(sum(animals), 0) AS avg_kg,
         sum(animals)::int                               AS animals
  FROM (
    SELECT location_id, d, avg_kg, animals FROM lump_daily
    UNION ALL
    SELECT location_id, d, avg_kg, animals FROM ind_daily
  ) u
  GROUP BY location_id, d
),
ranked AS (
  SELECT location_id, d, avg_kg, animals,
         row_number() OVER (PARTITION BY location_id ORDER BY d DESC) AS rn
  FROM daily
),
shed_latest AS (
  SELECT location_id, avg_kg, animals FROM ranked WHERE rn = 1
),
shed_gain AS (
  -- LAST TWO WEIGHS and the days between them, matching the shed chart and the
  -- legacy dashboard's adg_goat_last2. A full-span figure averages away the present:
  -- a shed that stalled last week still reads well if it grew a month ago.
  SELECT location_id,
         (max(avg_kg) FILTER (WHERE rn = 1) - max(avg_kg) FILTER (WHERE rn = 2)) * 1000.0
           / NULLIF(max(d) FILTER (WHERE rn = 1) - max(d) FILTER (WHERE rn = 2), 0) AS g_per_day,
         max(d) FILTER (WHERE rn = 1) - max(d) FILTER (WHERE rn = 2)                AS span_days
  FROM ranked
  WHERE rn <= 2
  GROUP BY location_id
  HAVING count(*) = 2
),
tag AS (
  -- EXACTLY ONE load per shed, or the shed is not attributed at all. See the
  -- doc comment: a shed holding two loads cannot have its single average split.
  SELECT location_id, min(load_ref) AS load_ref, min(owner_name) AS owner_name
  FROM weighing_shed_load_tags
  WHERE tenant_id = $1::uuid
  GROUP BY location_id
  HAVING count(*) = 1
)
SELECT t.load_ref,
       COALESCE(t.owner_name, ''),
       count(*)::int,
       sum(sl.animals)::int,
       sum(sl.avg_kg * sl.animals) / NULLIF(sum(sl.animals), 0),
       sum(sg.g_per_day * sl.animals) FILTER (WHERE sg.g_per_day IS NOT NULL)
         / NULLIF(sum(sl.animals) FILTER (WHERE sg.g_per_day IS NOT NULL), 0),
       COALESCE(max(sg.span_days), 0),
       (SELECT count(*) FROM shed_latest sl2
         WHERE NOT EXISTS (SELECT 1 FROM tag t2 WHERE t2.location_id = sl2.location_id))::int
FROM tag t
JOIN shed_latest sl ON sl.location_id = t.location_id
LEFT JOIN shed_gain sg ON sg.location_id = t.location_id
GROUP BY t.load_ref, t.owner_name
ORDER BY 6 DESC NULLS LAST, t.load_ref`

	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, periodStart, periodEnd)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var unattributed int
	for rows.Next() {
		var (
			bucket   domain.LoadGainBucket
			avgKg    *float64
			gain     *float64
			spanDays int
		)
		if err := rows.Scan(&bucket.LoadRef, &bucket.OwnerName, &bucket.Sheds, &bucket.Animals,
			&avgKg, &gain, &spanDays, &unattributed); err != nil {
			return nil, 0, err
		}
		if avgKg != nil {
			bucket.AverageWeightKg = *avgKg
		}
		bucket.GainGPerDay = gain
		if gain != nil {
			bucket.GainSpanDays = spanDays
		}
		out = append(out, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// The unattributed count rides on every row, so a result with NO loads at all
	// leaves it at zero above. Read it on its own in that case, or the screen cannot
	// distinguish "no mapping authored yet" from "nothing weighed".
	if len(out) == 0 {
		if err := r.pool.QueryRow(ctx, `
SELECT count(DISTINCT cs.location_id)::int
FROM weighing_campaign_sheds cs
JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
WHERE cs.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[]) AND cs.status <> 'canceled'
  AND (
    EXISTS (SELECT 1 FROM weighing_shed_observations o
             WHERE o.campaign_shed_id = cs.campaign_shed_id AND o.tenant_id = cs.tenant_id
               AND o.withdrawn_at IS NULL AND o.verification_status <> 'rejected'
               AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz)
    OR EXISTS (SELECT 1 FROM weighing_observations o
                WHERE o.campaign_shed_id = cs.campaign_shed_id AND o.tenant_id = cs.tenant_id
                  AND o.verification_status <> 'rejected'
                  AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz)
  )`, tenantID, parkIDs, periodStart, periodEnd).Scan(&unattributed); err != nil {
			return nil, 0, err
		}
	}
	return out, unattributed, nil
}
