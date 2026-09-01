package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// loadWeights returns growth per PROCUREMENT LOAD for the Weights screen.
//
// WEIGHING ISOLATION: reads weighing_campaign_sheds, weighing_campaigns,
// weighing_observations, weighing_shed_observations and weighing_shed_load_tags.
// Every one is a weighing_* table — the load mapping is weighing-owned precisely
// so this read needs no procurement table and no exception to the isolation lock.
// See 000131_weighing_shed_load_tags.sql for why the mapping lives here.
//
// projection-review: membership=one row per load_ref having at least one weighed operational shed row tagged to EXACTLY that load's physical location; group_key=(t.load_ref, t.owner_name), where `tag` has already collapsed to one row per location_id via HAVING count(*) = 1; join_cardinality=daily is 1 row per (location_id, partition_label, d) because it GROUPs on that tuple, so ranked/shed_latest are 1 per operational row at rn = 1 and shed_gain is 0..1 per operational row, while `tag` is 0..1 per physical location_id — the final GROUP deliberately blends the tagged location's measured partitions; pagination=NONE, bounded by the authored tag estate; scope=tenant_id + park_id = ANY($2) through weighing_campaigns, plus the half-open accepted_at window
//
//	PRODUCER UNIQUENESS vs CONSUMER MATCH KEYS, side by side:
//	  daily          unique on (location_id, partition_label, d) [its own GROUP BY]
//	  shed_latest    unique on (location_id, partition_label)    [ranked rn = 1]
//	  shed_gain      unique on (location_id, partition_label)    [its own GROUP BY]
//	  tag            unique on (location_id)               [GROUP BY + HAVING count(*) = 1]
//	  final SELECT   groups on (load_ref, owner_name)      [matches tag's carried columns]
//
//	ROW MULTIPLICITY OF EVERY JOINED SIDE (all 0..1 against a tagged shed):
//	  shed_latest    1   per measured partition — the JOIN is what restricts loads to weighed rows
//	  shed_gain      0..1 per measured partition — absent when that row was weighed only once
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
func (r *Repository) loadWeights(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sexFiltered bool, scope SexScope, weighingCategory string) ([]domain.LoadGainBucket, int, error) {
	out := []domain.LoadGainBucket{}
	if len(parkIDs) == 0 {
		return out, 0, nil
	}

	const q = `
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, c.park_id,
         COALESCE(cs.partition_label, '') AS partition_label,
         cs.weighing_category
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c
    ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND cs.status <> 'canceled'
    AND ($9::text = '' OR cs.weighing_category = $9::text)
),
lump_daily AS (
  -- withdrawn_at IS REQUIRED: 000067 made this table's uniqueness PARTIAL over live
  -- rows, so a reopened+resubmitted bucket legitimately keeps superseded rows and
  -- joining them all fans the shed-day out.
  SELECT s.location_id, s.partition_label,
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
    -- Sex filter, same rule as the shed table: a load's growth is read from whole-shed weighs,
    -- so a shed is counted only when its cohort is entirely this sex. $5 is FALSE for the
    -- unfiltered page, which therefore runs the query unchanged.
    AND (NOT $5::bool OR EXISTS (
      SELECT 1 FROM unnest($6::uuid[], $7::text[]) AS b(loc, part)
      WHERE b.loc = s.location_id AND b.part = COALESCE(s.partition_label, '')
    ))
),
ind_daily AS (
  -- projection-review: membership=one row per scanned tag and business day from scoped individual-animal observations; group_key=(location_id, partition_label, d) after DISTINCT ON tag/day; join_cardinality=scoped is 1 row per campaign shed, individual observations are narrowed by scanned tag scope before grouping, tag is 0..1 per physical shed after HAVING count(*) = 1; pagination=NONE, whole filtered load chart for the selected window; scope=tenant_id + authorized park ids + half-open accepted_at window + optional sex/origin tag scope
  -- ONE ROW PER ANIMAL PER DAY, not one per capture: weighing_observations keeps
  -- superseded rows (000061/000073, and a reopened bucket re-scans a tag that
  -- already has one), so a bare avg() over captures weights re-scanned animals
  -- twice. Identity is the TAG, matched case- and whitespace-insensitively — the
  -- same grain as 000073's uidx — because 000078 dropped animal_id and weighing is
  -- free-flow. A blank tag cannot be collapsed with other blank tags, so it keys on
  -- its own row.
  SELECT s.location_id, s.partition_label, x.d,
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
      -- Cohort filter, individual half. Lump-sum rows are narrowed by bucket above; scanned
      -- rows must be narrowed by tag here, or the load chart keeps all individual animals while
      -- the rest of the Weights page reports the selected sex/origin.
      AND (NOT $5::bool OR lower(btrim(o.scanned_identifier)) = ANY($8::text[]))
    ORDER BY o.campaign_shed_id,
             COALESCE(NULLIF(lower(btrim(o.scanned_identifier)), ''), o.observation_id::text),
             (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date,
             o.accepted_at DESC, o.observation_id DESC
  ) x ON x.campaign_shed_id = s.campaign_shed_id
  WHERE s.weighing_category = 'individual_animal'
  GROUP BY s.location_id, s.partition_label, x.d
),
daily AS (
  -- BOTH capture modes on one timeline. The two sources are disjoint per bucket
  -- (weighing_category is fixed at creation and the write path fills exactly one),
  -- but a SHED can change mode between campaigns, so its history legitimately spans
  -- both tables and a mode-specific view would see one point and no trend.
  --
  -- The GROUP BY also guarantees ONE row per (location_id, partition_label, d),
  -- which is what makes the row_number() below a clean first/latest pick: a shed
  -- weighed twice in a day across two buckets would otherwise occupy both ranks
  -- and yield a zero-day span.
  SELECT location_id, partition_label, d,
         sum(avg_kg * animals) / NULLIF(sum(animals), 0) AS avg_kg,
         sum(animals)::int                               AS animals
  FROM (
    SELECT location_id, partition_label, d, avg_kg, animals FROM lump_daily
    UNION ALL
    SELECT location_id, partition_label, d, avg_kg, animals FROM ind_daily
  ) u
  GROUP BY location_id, partition_label, d
),
ranked AS (
  SELECT location_id, partition_label, d, avg_kg, animals,
         row_number() OVER (PARTITION BY location_id, partition_label ORDER BY d DESC) AS rn
  FROM daily
),
shed_latest AS (
  SELECT location_id, partition_label, avg_kg, animals
  FROM ranked
  WHERE rn = 1
    AND d >= ($3::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
),
shed_gain AS (
  -- Selected-range movement matching the shed chart: first weighed date in the
  -- selected window to latest weighed date in the same selected window.
  SELECT latest.location_id, latest.partition_label,
         (latest.avg_kg - first.avg_kg) * 1000.0
           / NULLIF(latest.d - first.d, 0) AS g_per_day,
         latest.d - first.d                AS span_days
  FROM ranked latest
  JOIN (
    SELECT location_id, partition_label, d, avg_kg,
           row_number() OVER (PARTITION BY location_id, partition_label ORDER BY d ASC) AS rn
    FROM daily
  ) first ON first.location_id = latest.location_id
    AND first.partition_label = latest.partition_label
    AND first.rn = 1
  WHERE latest.rn = 1 AND latest.d > first.d
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
         WHERE NOT EXISTS (SELECT 1 FROM tag t2 WHERE t2.location_id = sl2.location_id))::int,
       -- WHERE the load's weighed animals are. Aggregated over the SAME joined rows
       -- the figures above blend, so sum(animals) here equals column 4 and the entry
       -- count equals column 3 by construction rather than by a second query that
       -- could drift from it.
       COALESCE(jsonb_agg(jsonb_build_object(
         'park_name', COALESCE(NULLIF(pk.location_code, ''), pk.name, ''),
         'shed_display_name', COALESCE(sh.name, ''),
         'partition_label', sl.partition_label,
         'animals', sl.animals
       ) ORDER BY COALESCE(NULLIF(pk.location_code, ''), pk.name, ''),
                  COALESCE(sh.name, ''), sl.partition_label), '[]'::jsonb)
FROM tag t
JOIN shed_latest sl ON sl.location_id = t.location_id
LEFT JOIN shed_gain sg
  ON sg.location_id = sl.location_id
 AND sg.partition_label = sl.partition_label
-- Both joins are 0..1 per tagged shed: shed_park GROUPs on location_id, and the two
-- locations joins are on that table's primary key. Neither widens the key set the
-- ratios above range over.
LEFT JOIN LATERAL (
  -- The park this shed belongs to, from the CAMPAIGN that scoped it -- the same
  -- authority $2 already filters on, rather than locations.parent_location_id, which is
  -- not guaranteed to be a park row.
  --
  -- A LATERAL rather than a CTE deliberately: the aggregate makes it EXACTLY ONE row per
  -- tagged shed, so it cannot widen the key set the ratios above range over, and it keeps
  -- this query under the CTE count at which a read stops being reviewable.
  SELECT min(s2.park_id::text)::uuid AS park_id
  FROM scoped s2
  WHERE s2.location_id = t.location_id
) sp ON TRUE
LEFT JOIN locations sh ON sh.location_id = t.location_id AND sh.tenant_id = $1::uuid
LEFT JOIN locations pk ON pk.location_id = sp.park_id AND pk.tenant_id = $1::uuid
GROUP BY t.load_ref, t.owner_name
ORDER BY 6 DESC NULLS LAST, t.load_ref`

	rows, err := r.pool.Query(ctx, q, tenantID, parkIDs, periodStart, periodEnd, sexFiltered,
		scope.LocationIDs, scope.PartitionLabels, scope.Tags, weighingCategory)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var unattributed int
	for rows.Next() {
		var (
			bucket     domain.LoadGainBucket
			avgKg      *float64
			gain       *float64
			spanDays   int
			placements []byte
		)
		if err := rows.Scan(&bucket.LoadRef, &bucket.OwnerName, &bucket.Sheds, &bucket.Animals,
			&avgKg, &gain, &spanDays, &unattributed, &placements); err != nil {
			return nil, 0, err
		}
		if avgKg != nil {
			bucket.AverageWeightKg = *avgKg
		}
		bucket.GainGPerDay = gain
		if gain != nil {
			bucket.GainSpanDays = spanDays
		}
		var err error
		if bucket.Placements, err = decodeLoadPlacements(placements); err != nil {
			return nil, 0, err
		}
		out = append(out, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// The unattributed count rides on every row, so a result with NO loads at all
	// leaves it at zero above. Read it on its own in that case, or the screen cannot
	// distinguish "no mapping authored yet" from "nothing weighed".
	//
	// projection-review: membership=same shed_latest population as the main load query when no uniquely-tagged load rows exist; group_key=none, scalar count over distinct measured operational rows; join_cardinality=scoped is 1 row per campaign bucket, each arm returns distinct (location_id, partition_label), tag is 0..1 uniquely tagged physical shed and is anti-joined; pagination=NONE, bounded by authorized park scope and selected half-open window; scope=tenant_id + authorized park ids + half-open accepted_at window + optional sex/origin tags and bucket scope
	if len(out) == 0 {
		if err := r.pool.QueryRow(ctx, `
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id,
         COALESCE(cs.partition_label, '') AS partition_label,
         cs.weighing_category
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c
    ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND cs.status <> 'canceled'
    AND ($9::text = '' OR cs.weighing_category = $9::text)
),
shed_latest AS (
  SELECT DISTINCT s.location_id, s.partition_label
  FROM scoped s
  JOIN weighing_shed_observations o
    ON o.campaign_shed_id = s.campaign_shed_id
   AND o.tenant_id = s.tenant_id
   AND o.withdrawn_at IS NULL
   AND o.verification_status <> 'rejected'
   AND o.accepted_at >= $3::timestamptz
   AND o.accepted_at < $4::timestamptz
  WHERE s.weighing_category = 'per_shed_partition'
    AND (NOT $5::bool OR EXISTS (
      SELECT 1 FROM unnest($6::uuid[], $7::text[]) AS b(loc, part)
      WHERE b.loc = s.location_id AND b.part = s.partition_label
    ))
  UNION
  SELECT DISTINCT s.location_id, s.partition_label
  FROM scoped s
  JOIN weighing_observations o
    ON o.campaign_shed_id = s.campaign_shed_id
   AND o.tenant_id = s.tenant_id
   AND o.verification_status <> 'rejected'
   AND o.accepted_at >= $3::timestamptz
   AND o.accepted_at < $4::timestamptz
  WHERE s.weighing_category = 'individual_animal'
    AND (NOT $5::bool OR lower(btrim(o.scanned_identifier)) = ANY($8::text[]))
),
tag AS (
  SELECT location_id
  FROM weighing_shed_load_tags
  WHERE tenant_id = $1::uuid
  GROUP BY location_id
  HAVING count(*) = 1
)
SELECT count(*)::int
FROM shed_latest sl
WHERE NOT EXISTS (SELECT 1 FROM tag t WHERE t.location_id = sl.location_id)`,
			tenantID, parkIDs, periodStart, periodEnd, sexFiltered,
			scope.LocationIDs, scope.PartitionLabels, scope.Tags, weighingCategory).Scan(&unattributed); err != nil {
			return nil, 0, err
		}
	}
	return out, unattributed, nil
}

// decodeLoadPlacements turns the query's jsonb placement array into the domain rows,
// composing each operational-location display through oploc rather than in SQL.
//
// The doubling guard mirrors shed_weights.go and growth.go: a partitioned weighing
// bucket is routinely NAMED for the pen it covers, and locations rows carry the same
// legacy shapes ("Castro 1", "Godel 2 - Part 2"), so handing an already-partitioned
// name to oploc -- which APPENDS the partition to a SHED name -- produced "Castro 2 2"
// and "Godel 2 - Part 2 - Part 2" on the sibling surfaces.
func decodeLoadPlacements(raw []byte) ([]domain.LoadPlacement, error) {
	out := []domain.LoadPlacement{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("weighing: decode load placements: %w", err)
	}
	for i := range out {
		p := &out[i]
		if p.PartitionLabel != "" && strings.HasSuffix(p.ShedDisplayName, p.PartitionLabel) {
			p.OperationalLocationDisplay = p.ShedDisplayName
			continue
		}
		p.OperationalLocationDisplay = (oploc.OperationalLocation{
			ShedName:       p.ShedDisplayName,
			PartitionLabel: p.PartitionLabel,
		}).Display()
	}
	return mergeSameOperationalLocation(out), nil
}

// mergeSameOperationalLocation collapses placement rows that are the SAME PEN, summing
// their head counts.
//
// WHY THIS IS NEEDED, and why it is not the banned name-keying. The farm's pens exist
// twice in `locations`: the canonical shed plus a legacy row literally named for the pen
// ("Castro 1"). A load tagged to the legacy row is weighed under buckets whose own
// partition_label is sometimes blank and sometimes the pen number, and BOTH compose --
// correctly, through the doubling guard -- to the same display. The chart therefore
// rendered "Castro 1 · 63" and "Castro 1 · 31" side by side: one pen, shown twice, as if
// the load sat in two places.
//
// Merging is the honest fix rather than dropping the alias row. Both rows are real
// measured buckets and both are already inside the load's blended average and its animal
// total, so suppressing one would leave the placements no longer summing to `animals` --
// breaking the exact reconciliation this list is trusted for. Summing keeps
// 63 + 31 = 94 visible against a single "Castro 1".
//
// The key is (park, composed display) and NOT the shed name: that is the whole point --
// two rows only merge when they resolve to the same OPERATIONAL LOCATION, which is the
// canonical identity the convention defines. Two genuinely different pens compose to
// different displays and never merge, and two parks that both own a "Castro" stay apart
// because the park is in the key.
func mergeSameOperationalLocation(in []domain.LoadPlacement) []domain.LoadPlacement {
	type key struct{ park, display string }
	index := make(map[key]int, len(in))
	out := make([]domain.LoadPlacement, 0, len(in))
	for _, p := range in {
		k := key{park: p.ParkName, display: p.OperationalLocationDisplay}
		if at, ok := index[k]; ok {
			out[at].Animals += p.Animals
			continue
		}
		index[k] = len(out)
		out = append(out, p)
	}
	return out
}
