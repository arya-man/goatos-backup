package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/readcache"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"

	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
)

// THE WEIGHING FCR TAB (maintainer request 2026-09-07).
//
// Feed conversion ratio needs two facts nobody in this repository holds together: what a pen was
// FED (the feed-direction sheet, day grain, pen grain) and what the pen GAINED (weighing, round
// grain, bucket grain). This file joins them, read-only, in the Growth Director module -- the one
// place already allowed to read weighing, herd and feed tables side by side -- and hands the
// domain package plain rows to roll up.
//
// THE GRAIN BRIDGE IS THE HARD PART, and it is stated once here. A weighing bucket names a
// LOCATION plus an optional partition label, and on the live estates that location is very often
// a legacy alias row: "Godel 1 - Part 3" as its own locations row with a blank label, beside the
// newer "Godel 1" + label "Part 3". The feed sheet keys on the PHYSICAL shed ("Godel 1") plus the
// partition label the herd register writes ("Part 3", or "1" for Castro's pens). So every bucket
// is resolved to (physical shed, scrubbed partition key) before anything is joined:
//
//	bucket carries a label       -> its own location IS the physical shed; key = scrub(label)
//	bucket carries no label      -> the longest active shed name in the same park that prefixes
//	                                the bucket's name is the physical shed; the remainder is the
//	                                label ("Godel 1 - Part 3" - "Godel 1" = "Part 3")
//	no such prefix               -> the location is itself the physical shed, undivided
//
// scrub() is lower(btrim()) with a leading "part"/"pt" and separators removed, so "Part 3", "- 3"
// and "3" are one key -- the identical normalisation weight_demographics.go and sex_scope.go apply
// to goat_shed_partitions, which is what makes this file agree with the Weights page about which
// pen a bucket is. Verified on the local estate copy before this shipped: every bucket with feed
// rows resolves to exactly one feed pen in both parks.
//
// ROUNDS AND SEGMENTS. A weighing campaign is week-grain, so ONE round per pen per campaign
// (newest capture wins, the 000073/000080 convention). Consecutive rounds of a pen form a
// SEGMENT; feed is what the sheet directed to that pen on the days [earlier round, later round),
// head-days are the sheet's own head counts on those days. The scanned arm pairs each animal that
// was weighed in BOTH rounds of the same pen and takes the mean of their daily gains; the
// whole-shed arm takes the movement of the pen average. A pen weighed once has no segment and is
// reported as such rather than given a number.
//
// WHAT THIS FILE NEVER DOES: gate a scan, touch a weighing write, or invent a pen's feed. NULL
// quantity_kg stays NULL (a blocked cell is counted, never summed as zero), an unpriced kilogram is
// counted as unpriced rather than free, and a pen whose sheet carries no head count contributes no
// head-days and therefore no ratio.

const fcrScrub = `regexp_replace(lower(btrim(%s)), '^[-\s]*(part|pt)?[\s.-]*', '')`

// fcrScopeCTEs resolves every scoped bucket to its feed pen and computes the rounds. Params:
//
//	$1 tenant uuid, $2 park_ids uuid[], $3 window start timestamptz, $4 window end (exclusive)
//	$5 weighing_category ('' = both), $6/$7 the same-animal map (tags, canonical tags)
//	$8 window start date, $9 window end date (exclusive), both Asia/Kolkata business dates
var fcrScopeCTEs = `
scoped AS (
  SELECT cs.campaign_shed_id, cs.location_id, COALESCE(cs.partition_label, '') AS bucket_partition,
         cs.weighing_category, c.campaign_id, c.period_start_date, c.park_id,
         l.name AS loc_name, l.parent_location_id
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  JOIN locations l ON l.location_id = cs.location_id AND l.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND cs.status <> 'canceled'
    AND c.status <> 'canceled'
    AND ($5::text = '' OR cs.weighing_category = $5::text)
),
-- THE BRIDGE (see the file comment). One row per distinct (location, label) the buckets name.
pen_map AS (
  SELECT DISTINCT ON (s.location_id, s.bucket_partition)
         s.location_id, s.bucket_partition,
         COALESCE(labeled.pen_shed_id, parent.parent_id, s.location_id) AS pen_shed_id,
         COALESCE(labeled.pen_partition_label, parent.pen_partition_label, '') AS pen_partition_label
  FROM scoped s
  LEFT JOIN LATERAL (
    SELECT s.location_id AS pen_shed_id, s.bucket_partition AS pen_partition_label
    WHERE s.bucket_partition <> ''
  ) labeled ON true
  LEFT JOIN LATERAL (
    SELECT shed.location_id AS parent_id,
           regexp_replace(btrim(substr(s.loc_name, length(shed.name) + 1)), '^[-\s]+', '') AS pen_partition_label
    FROM locations shed
    WHERE shed.tenant_id = $1::uuid
      AND shed.parent_location_id = s.parent_location_id
      AND shed.location_type = 'shed'
      AND shed.status = 'active'
      AND shed.retired_at IS NULL
      AND shed.name <> s.loc_name
      AND ((s.loc_name LIKE shed.name || ' %') OR (s.loc_name LIKE shed.name || ' - %'))
    ORDER BY length(shed.name) DESC
    LIMIT 1
  ) parent ON labeled.pen_shed_id IS NULL
  ORDER BY s.location_id, s.bucket_partition
),
bucket_pen AS (
  SELECT s.*, pm.pen_shed_id, pm.pen_partition_label,
         ` + fcrScrubPenLabel + ` AS pen_key
  FROM scoped s
  JOIN pen_map pm ON pm.location_id = s.location_id AND pm.bucket_partition = s.bucket_partition
),
-- ONE WHOLE-SHED ROUND PER PEN PER CAMPAIGN. withdrawn_at IS NULL is not optional: live-row
-- uniqueness is a PARTIAL index (000067), so a reopened bucket keeps superseded rows.
lump_rounds AS (
  SELECT DISTINCT ON (bp.pen_shed_id, bp.pen_key, bp.campaign_id)
         bp.pen_shed_id, bp.pen_key, bp.campaign_id, bp.period_start_date,
         (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
         so.average_weight_kg::float8 AS avg_kg, so.animal_count AS animals
  FROM bucket_pen bp
  JOIN weighing_shed_observations so
    ON so.tenant_id = $1::uuid AND so.campaign_shed_id = bp.campaign_shed_id
  WHERE bp.weighing_category = 'per_shed_partition'
    AND so.withdrawn_at IS NULL
    AND so.verification_status <> 'rejected'
    AND so.accepted_at >= $3::timestamptz AND so.accepted_at < $4::timestamptz
    AND so.animal_count > 0 AND so.average_weight_kg IS NOT NULL
  ORDER BY bp.pen_shed_id, bp.pen_key, bp.campaign_id, so.accepted_at DESC, so.shed_observation_id DESC
),
-- ONE SCANNED WEIGH PER ANIMAL PER PEN PER CAMPAIGN, keyed by the same-animal map ($6/$7) so a kid
-- scanned on its second RFID in the next round is still one kid.
scan_rounds AS (
  SELECT DISTINCT ON (bp.pen_shed_id, bp.pen_key, bp.campaign_id, COALESCE(akmap.canonical_tag, lower(btrim(o.scanned_identifier))))
         bp.pen_shed_id, bp.pen_key, bp.campaign_id, bp.period_start_date,
         COALESCE(akmap.canonical_tag, lower(btrim(o.scanned_identifier))) AS animal_key,
         (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d,
         o.weight_kg::float8 AS w
  FROM bucket_pen bp
  JOIN weighing_observations o
    ON o.tenant_id = $1::uuid AND o.campaign_shed_id = bp.campaign_shed_id
  LEFT JOIN unnest($6::text[], $7::text[]) AS akmap(tag, canonical_tag)
    ON akmap.tag = lower(btrim(o.scanned_identifier))
  WHERE bp.weighing_category = 'individual_animal'
    AND o.verification_status <> 'rejected'
    AND btrim(o.scanned_identifier) <> ''
    AND o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
  ORDER BY bp.pen_shed_id, bp.pen_key, bp.campaign_id, COALESCE(akmap.canonical_tag, lower(btrim(o.scanned_identifier))),
           o.accepted_at DESC, o.observation_id DESC
),
scan_pen_rounds AS (
  SELECT pen_shed_id, pen_key, campaign_id, period_start_date,
         max(d) AS d, avg(w) AS avg_kg, count(*)::int AS animals
  FROM scan_rounds
  GROUP BY pen_shed_id, pen_key, campaign_id, period_start_date
),
-- Both arms as ONE round list per pen. The two arms are disjoint by construction (a bucket's
-- weighing_category fixes which table its write path fills), so a pen weighed both ways in one
-- campaign would be two rounds; the domain layer takes them in date order.
pen_rounds AS (
  SELECT pen_shed_id, pen_key, campaign_id, period_start_date, d, avg_kg, animals, 'per_shed_partition' AS mode FROM lump_rounds
  UNION ALL
  SELECT pen_shed_id, pen_key, campaign_id, period_start_date, d, avg_kg, animals, 'individual_animal' FROM scan_pen_rounds
),
-- The feed side, from the feed-day rollup (migration 000418): one pre-priced row per
-- (park, pen, business day) instead of every feed cell of the window. Only the sheds the scoped
-- buckets resolved to. Every column is additive over days, so a window or a segment [d_prev, d)
-- is the sum of its day rows -- see fcr_rollup.go for the grain and the refresh contract.
feed_days AS (
  SELECT fd.pen_shed_id, fd.pen_key, fd.feed_day, fd.feed_kg, fd.feed_cost, fd.unpriced_kg,
         fd.blocked_cells, fd.head_days, fd.feed_label
  FROM growth_fcr_pen_feed_days fd
  WHERE fd.tenant_id = $1::uuid
    AND fd.park_id = ANY($2::uuid[])
    AND fd.feed_day >= $8::date AND fd.feed_day < $9::date
    AND fd.pen_shed_id IN (SELECT pen_shed_id FROM pen_map)
)`

// The scrub applied to the bucket-derived label and to the feed sheet's label. Written twice as
// constants because the Go format verb cannot sit inside a raw SQL string.
var (
	fcrScrubPenLabel  = strings.Replace(fcrScrub, "%s", "pm.pen_partition_label", 1)
	fcrScrubGoatLabel = strings.Replace(fcrScrub, "%s", "COALESCE(NULLIF(gsp.partition_label, 'whole'), '')", 1)
)

// The two tab queries, package-level so a query-plan probe and the scale guard can reach them.
var (
	fcrPensSQL = ` -- scale-guard:ignore: bounded tenant + authorized-park reporting read over the pens weighed in one selected window; one round trip per FCR tab load, no pagination because the pen set is the estate's pen count
WITH ` + fcrScopeCTEs + `,
pens AS (
  SELECT pen_shed_id, pen_key,
         count(*)::int AS rounds,
         min(d) AS first_d,
         max(d) AS last_d,
         (array_agg(avg_kg ORDER BY period_start_date, d))[1] AS first_avg,
         (array_agg(animals ORDER BY period_start_date DESC, d DESC))[1] AS last_animals,
         array_agg(DISTINCT mode ORDER BY mode) AS modes
  FROM pen_rounds
  GROUP BY pen_shed_id, pen_key
),
-- The pen's live residents, agree-or-neither. The scrub on goat_shed_partitions is the one
-- weight_demographics.go and sex_scope.go apply; comparing raw would drop whole pens.
cohort AS (
  SELECT p.pen_shed_id, p.pen_key,
         count(g.goat_id)::int AS residents,
         count(DISTINCT g.breed)::int AS breeds, min(g.breed) AS breed,
         count(DISTINCT g.sex)::int AS sexes, min(g.sex) AS sex,
         count(DISTINCT g.species)::int AS species_n, min(g.species) AS species,
         jsonb_agg(jsonb_build_object('key', lower(btrim(g.breed)), 'label', g.breed, 'animals', 1) ORDER BY g.breed)
           FILTER (WHERE g.goat_id IS NOT NULL) AS breed_members,
         count(g.goat_id) FILTER (WHERE EXISTS (
           SELECT 1 FROM procurement_load_goats plg WHERE plg.tenant_id = $1::uuid AND plg.goat_id = g.goat_id))::int AS bought
  FROM pens p
  LEFT JOIN goats g ON g.tenant_id = $1::uuid AND g.lifecycle_status = 'alive' AND g.shed_id = p.pen_shed_id
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1::uuid AND gsp.goat_id = g.goat_id
  WHERE g.goat_id IS NULL OR ` + fcrScrubGoatLabel + ` = p.pen_key
  GROUP BY p.pen_shed_id, p.pen_key
),
-- THE FALLBACK COHORT: the animals scanned in this pen during the window, through the register
-- (goat_identifiers is lifetime-unique per tenant so the join is 0..1), whatever their location or
-- lifecycle today. Read only when the pen has no live residents (decided in the domain layer).
weighed_cohort AS (
  SELECT sr.pen_shed_id, sr.pen_key,
         count(g.goat_id)::int AS animals,
         count(DISTINCT g.breed)::int AS breeds, min(g.breed) AS breed,
         count(DISTINCT g.sex)::int AS sexes, min(g.sex) AS sex,
         count(DISTINCT g.species)::int AS species_n, min(g.species) AS species,
         jsonb_agg(jsonb_build_object('key', lower(btrim(g.breed)), 'label', g.breed, 'animals', 1) ORDER BY g.breed)
           FILTER (WHERE g.goat_id IS NOT NULL) AS breed_members,
         count(g.goat_id) FILTER (WHERE EXISTS (
           SELECT 1 FROM procurement_load_goats plg WHERE plg.tenant_id = $1::uuid AND plg.goat_id = g.goat_id))::int AS bought
  FROM (SELECT DISTINCT pen_shed_id, pen_key, animal_key FROM scan_rounds) sr
  JOIN goat_identifiers gi ON gi.tenant_id = $1::uuid AND gi.normalized_value = upper(sr.animal_key)
  JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = gi.goat_id
  GROUP BY sr.pen_shed_id, sr.pen_key
),
-- THE PRICE MIX (maintainer decision 2026-09-24): the same two cohorts, counted per
-- (species, management stage, sex), so the domain values each animal at its own sale price. Each is
-- grouped to that grain FIRST and only then folded to one jsonb array per pen, so the pen row it
-- joins is still 1:1 -- the inner GROUP BY is the pre-aggregation, the outer one the fold.
cohort_mix AS (
  SELECT pen_shed_id, pen_key,
         jsonb_agg(jsonb_build_object('species', species, 'management_stage', stage, 'sex', sex, 'animals', n)) AS mix
  FROM (
    SELECT p.pen_shed_id, p.pen_key, lower(g.species) AS species, COALESCE(g.management_stage, '') AS stage,
           lower(g.sex) AS sex, count(*)::int AS n
    FROM pens p
    JOIN goats g ON g.tenant_id = $1::uuid AND g.lifecycle_status = 'alive' AND g.shed_id = p.pen_shed_id
    LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1::uuid AND gsp.goat_id = g.goat_id
    WHERE ` + fcrScrubGoatLabel + ` = p.pen_key
    GROUP BY p.pen_shed_id, p.pen_key, lower(g.species), COALESCE(g.management_stage, ''), lower(g.sex)
  ) x
  GROUP BY pen_shed_id, pen_key
),
weighed_mix AS (
  SELECT pen_shed_id, pen_key,
         jsonb_agg(jsonb_build_object('species', species, 'management_stage', stage, 'sex', sex, 'animals', n)) AS mix
  FROM (
    SELECT sr.pen_shed_id, sr.pen_key, lower(g.species) AS species, COALESCE(g.management_stage, '') AS stage,
           lower(g.sex) AS sex, count(*)::int AS n
    FROM (SELECT DISTINCT pen_shed_id, pen_key, animal_key FROM scan_rounds) sr
    JOIN goat_identifiers gi ON gi.tenant_id = $1::uuid AND gi.normalized_value = upper(sr.animal_key)
    JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = gi.goat_id
    GROUP BY sr.pen_shed_id, sr.pen_key, lower(g.species), COALESCE(g.management_stage, ''), lower(g.sex)
  ) x
  GROUP BY pen_shed_id, pen_key
),
window_feed AS (
  SELECT p.pen_shed_id, p.pen_key,
         sum(fd.feed_kg) AS feed_kg,
         COALESCE(sum(fd.blocked_cells), 0)::int AS blocked_cells,
         min(fd.feed_label) AS feed_label
  FROM pens p
  LEFT JOIN feed_days fd ON fd.pen_shed_id = p.pen_shed_id AND fd.pen_key = p.pen_key
  GROUP BY p.pen_shed_id, p.pen_key
)
SELECT p.pen_shed_id::text, p.pen_key,
       shed.name, COALESCE(pk.location_id::text, ''), COALESCE(pk.name, ''), COALESCE(NULLIF(pk.location_code, ''), pk.name, ''),
       (SELECT min(bp.pen_partition_label) FROM bucket_pen bp WHERE bp.pen_shed_id = p.pen_shed_id AND bp.pen_key = p.pen_key) AS bucket_label,
       wf.feed_label,
       p.rounds, p.first_d::text, p.last_d::text, p.first_avg, p.last_animals, p.modes,
       c.residents, c.breeds, COALESCE(c.breed, ''), c.sexes, COALESCE(c.sex, ''), c.species_n, COALESCE(c.species, ''), cm.mix, c.breed_members, c.bought,
       wc.animals, wc.breeds, COALESCE(wc.breed, ''), wc.sexes, COALESCE(wc.sex, ''), wc.species_n, COALESCE(wc.species, ''), wm.mix, wc.breed_members, wc.bought,
       wf.feed_kg::float8, wf.blocked_cells
FROM pens p
JOIN locations shed ON shed.location_id = p.pen_shed_id AND shed.tenant_id = $1::uuid
LEFT JOIN locations pk ON pk.location_id = shed.parent_location_id AND pk.tenant_id = $1::uuid
LEFT JOIN cohort c ON c.pen_shed_id = p.pen_shed_id AND c.pen_key = p.pen_key
LEFT JOIN weighed_cohort wc ON wc.pen_shed_id = p.pen_shed_id AND wc.pen_key = p.pen_key
LEFT JOIN cohort_mix cm ON cm.pen_shed_id = p.pen_shed_id AND cm.pen_key = p.pen_key
LEFT JOIN weighed_mix wm ON wm.pen_shed_id = p.pen_shed_id AND wm.pen_key = p.pen_key
LEFT JOIN window_feed wf ON wf.pen_shed_id = p.pen_shed_id AND wf.pen_key = p.pen_key
ORDER BY COALESCE(NULLIF(pk.location_code, ''), pk.name), shed.name, p.pen_key`
	fcrSegmentsSQL = ` -- scale-guard:ignore: bounded tenant + authorized-park reporting read; one round trip per FCR tab load over the rounds x pens of one selected window
WITH ` + fcrScopeCTEs + `,
lump_seg AS (
  SELECT pen_shed_id, pen_key, d_prev, d, animals, campaign_id, campaign_prev,
         (avg_kg - avg_prev) * 1000.0 / (d - d_prev) AS adg_g
  FROM (
    SELECT lr.*, LAG(d) OVER w AS d_prev, LAG(avg_kg) OVER w AS avg_prev, LAG(campaign_id) OVER w AS campaign_prev
    FROM lump_rounds lr
    WINDOW w AS (PARTITION BY pen_shed_id, pen_key ORDER BY period_start_date, d)
  ) x
  WHERE d_prev IS NOT NULL AND d > d_prev
),
scan_pen_seg AS (
  SELECT pen_shed_id, pen_key, campaign_id, campaign_prev, d, d_prev, animals
  FROM (
    SELECT sr.*, LAG(campaign_id) OVER w AS campaign_prev, LAG(d) OVER w AS d_prev
    FROM scan_pen_rounds sr
    WINDOW w AS (PARTITION BY pen_shed_id, pen_key ORDER BY period_start_date, d)
  ) x
  WHERE campaign_prev IS NOT NULL AND d > d_prev
),
-- Each animal weighed in BOTH rounds of the pen, at its own two dates; the pen's gain for the
-- segment is the mean of those animals' daily gains -- the statistic the Weights headline uses.
scan_seg AS (
  SELECT s.pen_shed_id, s.pen_key, s.d_prev, s.d, s.animals,
         avg((cur.w - prev.w) * 1000.0 / (cur.d - prev.d)) AS adg_g,
         count(*)::int AS paired
  FROM scan_pen_seg s
  JOIN scan_rounds cur
    ON cur.pen_shed_id = s.pen_shed_id AND cur.pen_key = s.pen_key AND cur.campaign_id = s.campaign_id
  JOIN scan_rounds prev
    ON prev.pen_shed_id = s.pen_shed_id AND prev.pen_key = s.pen_key AND prev.campaign_id = s.campaign_prev
   AND prev.animal_key = cur.animal_key
  WHERE cur.d > prev.d
  GROUP BY s.pen_shed_id, s.pen_key, s.d_prev, s.d, s.animals
),
segments AS (
  SELECT pen_shed_id, pen_key, d_prev, d, animals, adg_g::float8 AS adg_g, 'per_shed_partition' AS mode, 0 AS paired FROM lump_seg
  UNION ALL
  SELECT pen_shed_id, pen_key, d_prev, d, animals, adg_g::float8, 'individual_animal', paired FROM scan_seg
),
-- Feed per segment from the rollup's day rows in [d_prev, d). The rows are already priced at the
-- same-farm latest load on or before their day (the rule Feed analytics expenditure applies), so
-- no price join happens here -- the 16,922 x 649 nested loop of the 2026-09-24 incident (P3) is
-- gone rather than reordered.
-- projection-review: membership=every rollup day row of the segment's pen inside [d_prev, d);
-- group_key=(pen_shed_id, pen_key, d_prev, d) on both seg_feed and seg_heads and on the final
-- join; join_cardinality=feed_days is 0..N day rows per segment, COLLAPSED by the aggregate, and
-- segments is unique on its group key (one per round pair), so no side multiplies; pagination=
-- NONE, one bounded window read; scope=tenant + park ANY + the window's business days.
seg_feed AS (
  SELECT sg.pen_shed_id, sg.pen_key, sg.d_prev, sg.d,
         -- NULL feed_kg is a day whose every cell was blocked; it stays out of the sum.
         sum(fd.feed_kg)::float8 AS feed_kg,
         sum(fd.feed_cost)::float8 AS feed_cost,
         COALESCE(sum(fd.unpriced_kg), 0)::float8 AS unpriced_kg,
         COALESCE(sum(fd.blocked_cells), 0)::int AS blocked_cells
  FROM segments sg
  LEFT JOIN feed_days fd
    ON fd.pen_shed_id = sg.pen_shed_id AND fd.pen_key = sg.pen_key
   AND fd.feed_day >= sg.d_prev AND fd.feed_day < sg.d
  GROUP BY sg.pen_shed_id, sg.pen_key, sg.d_prev, sg.d
),
seg_heads AS (
  SELECT sg.pen_shed_id, sg.pen_key, sg.d_prev, sg.d, sum(fd.head_days)::float8 AS head_days
  FROM segments sg
  JOIN feed_days fd
    ON fd.pen_shed_id = sg.pen_shed_id AND fd.pen_key = sg.pen_key
   AND fd.feed_day >= sg.d_prev AND fd.feed_day < sg.d
   AND fd.head_days IS NOT NULL
  GROUP BY sg.pen_shed_id, sg.pen_key, sg.d_prev, sg.d
)
SELECT sg.pen_shed_id::text, sg.pen_key, sg.d_prev::text, sg.d::text, sg.animals, sg.adg_g, sg.mode, sg.paired,
       f.feed_kg, f.feed_cost, COALESCE(f.unpriced_kg, 0), COALESCE(f.blocked_cells, 0), h.head_days
FROM segments sg
LEFT JOIN seg_feed  f ON f.pen_shed_id = sg.pen_shed_id AND f.pen_key = sg.pen_key AND f.d_prev = sg.d_prev AND f.d = sg.d
LEFT JOIN seg_heads h ON h.pen_shed_id = sg.pen_shed_id AND h.pen_key = sg.pen_key AND h.d_prev = sg.d_prev AND h.d = sg.d
ORDER BY sg.pen_shed_id, sg.pen_key, sg.d`
)

// GetFCR builds the FCR tab. See the file comment for the grain rules.
//
// Served through the shared read cache. The key carries today's business date because the report
// is priced at TODAY's rate (below); weighing, feed-issue and assumptions writes evict it.
func (r *Repository) GetFCR(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string) (domain.FCRReport, error) {
	loc := biztime.DefaultLocation()
	today := biztime.BusinessDayStart(time.Now().In(loc)).Format("2006-01-02")
	params := growthDirectorReadKey("fcr", periodStart.UTC().Format(time.RFC3339), periodEnd.UTC().Format(time.RFC3339), sex, origin, weighingCategory, "priced="+today)
	return readcache.Load(ctx, r.cache, gdReadKey(tenantID, parkIDs, params), func(ctx context.Context) (domain.FCRReport, error) {
		return r.getFCRUncached(ctx, tenantID, parkIDs, periodStart, periodEnd, sex, origin, weighingCategory)
	})
}

func (r *Repository) getFCRUncached(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string) (domain.FCRReport, error) {
	loc := biztime.DefaultLocation()
	period := domain.Period{
		Start:      periodStart.In(loc).Format("2006-01-02"),
		End:        periodEnd.In(loc).AddDate(0, 0, -1).Format("2006-01-02"),
		Resolution: "weighing_round",
	}
	if len(parkIDs) == 0 {
		out := domain.BuildFCRReport(nil, nil, domain.SalePrices{Prices: []domain.SalePrice{}}, domain.FCRFilters{})
		out.Period = period
		out.Parks = []domain.Park{}
		return out, nil
	}
	startDate := periodStart.In(loc).Format("2006-01-02")
	endDate := periodEnd.In(loc).Format("2006-01-02")

	// The same-animal map from weighing's ONE resolver, so a double-tagged kid pairs here exactly
	// as it does on the Weights page.
	idMap, err := weighingpg.ResolveAnimalIdentityMap(ctx, r.pool, tenantID, parkIDs, periodStart, periodEnd)
	if err != nil {
		return domain.FCRReport{}, err
	}
	// Priced at TODAY's rate, not the period's (maintainer instruction 2026-09-19: a price changed
	// from the Assumptions drawer must "reflect real time"). The default window ends yesterday,
	// so pricing at the period end left a price set this morning invisible until tomorrow. The
	// table stays effective-dated for the audit trail; the tab prints which row applied.
	prices, err := r.GetSalePrices(ctx, tenantID, biztime.BusinessDayStart(time.Now().In(loc)))
	if err != nil {
		return domain.FCRReport{}, err
	}
	parks, err := r.parks(ctx, tenantID, parkIDs)
	if err != nil {
		return domain.FCRReport{}, err
	}
	// The feed side is summed from the feed-day rollup: bring the parks being read up to date
	// first, so a feed or price write committed before this read is always in its answer.
	if err := r.refreshFCRRollupForRead(ctx, tenantID, parkIDs); err != nil {
		return domain.FCRReport{}, err
	}
	pens, err := r.fcrPens(ctx, tenantID, parkIDs, periodStart, periodEnd, startDate, endDate, weighingCategory, idMap)
	if err != nil {
		return domain.FCRReport{}, err
	}
	segments, err := r.fcrSegments(ctx, tenantID, parkIDs, periodStart, periodEnd, startDate, endDate, weighingCategory, idMap)
	if err != nil {
		return domain.FCRReport{}, err
	}
	settings, err := r.GrowthSettings(ctx, tenantID)
	if err != nil {
		return domain.FCRReport{}, err
	}
	out := domain.BuildFCRReport(pens, segments, prices, domain.FCRFilters{Sex: sex, Origin: origin, BandEdgesKg: settings.BandEdgesKg})
	out.Period = period
	out.Parks = parks
	return out, nil
}

// fcrPens returns one row per pen that has at least one weighing round in the window: identity,
// display parts, its rounds, and its resident cohort.
//
// projection-review: membership=one row per (pen_shed_id, pen_key) with >=1 weighing round in the
// window; group_key=(pen_shed_id, pen_key) on every side; join_cardinality=pen_rounds is collapsed
// by the aggregate, cohort is a correlated aggregate over goats x goat_shed_partitions (0..1 per
// goat, PK (tenant_id, goat_id)), window_feed sums the pen's feed-day rollup rows (0..N per pen,
// collapsed by the aggregate; rollup PK (tenant, park, pen, day)), locations
// joins are on primary key -- no side multiplies a pen row; pagination=NONE, bounded by the pens
// weighed in one window (~60 on the live estate); scope=tenant + park ANY + accepted_at window.
//
//	PRODUCER UNIQUENESS vs CONSUMER MATCH KEYS, side by side:
//	  pen_rounds   one row per (pen_shed_id, pen_key, campaign_id, mode)
//	  pens         GROUP BY (pen_shed_id, pen_key)
//	  cohort       correlated on (pen_shed_id, pen_key), returns one row
//	  cohort_mix   GROUP BY (pen, species, stage, sex) then folded GROUP BY (pen_shed_id, pen_key): one row
//	  weighed_mix  same fold over the scanned cohort: one row
//	  window_feed  correlated on (pen_shed_id, pen_key), returns one row
//
//	Ratio key sets: none here -- every ratio is formed in the domain layer over the identical pen.
func (r *Repository) fcrPens(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, startDate, endDate, weighingCategory string, idMap weighingpg.AnimalIdentityMap) ([]domain.FCRPenRow, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	// fcrPensSQL is assembled from the shared scope CTEs, so its final SQL and its nine binds are
	// checked as one contract before it runs (postgres bind-contract rule).
	bound, err := sqlbind.Bind(fcrPensSQL, tenantID, parkIDs, periodStart, periodEnd, weighingCategory, idMap.Tags, idMap.CanonicalTags, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("fcr pens bind: %w", err)
	}
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.FCRPenRow{}
	for rows.Next() {
		var row domain.FCRPenRow
		var shedName string
		var bucketLabel, feedLabel *string
		var residents, breeds, sexes, speciesN, bought *int
		var residentMix, weighedMix, residentBreeds, weighedBreeds []byte
		var wAnimals, wBreeds, wSexes, wSpeciesN, wBought *int
		var firstAvg, windowFeed *float64
		var blocked *int
		if err := rows.Scan(&row.LocationID, &row.PenKey, &shedName, &row.ParkID, &row.ParkName, &row.ParkCode, &bucketLabel, &feedLabel,
			&row.Rounds, &row.FirstWeighDate, &row.LastWeighDate, &firstAvg, &row.LatestAnimals, &row.Modes,
			&residents, &breeds, &row.Breed, &sexes, &row.Sex, &speciesN, &row.Species, &residentMix, &residentBreeds, &bought,
			&wAnimals, &wBreeds, &row.WeighedBreed, &wSexes, &row.WeighedSex, &wSpeciesN, &row.WeighedSpecies, &weighedMix, &weighedBreeds, &wBought,
			&windowFeed, &blocked); err != nil {
			return nil, err
		}
		row.PenKey = row.LocationID + "|" + row.PenKey
		row.ShedName = shedName
		// The label the feed sheet (and therefore the herd register) writes wins for display; the
		// bucket's own spelling is the fallback for a pen no sheet has named yet.
		if feedLabel != nil && strings.TrimSpace(*feedLabel) != "" {
			row.PartitionLabel = *feedLabel
		} else if bucketLabel != nil {
			row.PartitionLabel = *bucketLabel
		}
		// Composed through the ONE canonical helper (platform/oploc via operationalLabel), never by
		// hand: park-prefixed because both parks field identically named sheds.
		row.Display = operationalLabel(row.ParkName, shedName, row.PartitionLabel)
		row.FirstAverageKg = firstAvg
		if residents != nil {
			row.Residents = *residents
		}
		if breeds != nil {
			row.Breeds = *breeds
		}
		if sexes != nil {
			row.Sexes = *sexes
		}
		if speciesN != nil {
			row.SpeciesCount = *speciesN
		}
		var mixErr error
		if row.ResidentMix, mixErr = decodeHeadMix(residentMix); mixErr != nil {
			return nil, mixErr
		}
		if row.WeighedMix, mixErr = decodeHeadMix(weighedMix); mixErr != nil {
			return nil, mixErr
		}
		if row.BreedMembers, mixErr = decodeFCRBreedMembers(residentBreeds); mixErr != nil {
			return nil, mixErr
		}
		if row.WeighedBreedMembers, mixErr = decodeFCRBreedMembers(weighedBreeds); mixErr != nil {
			return nil, mixErr
		}
		if bought != nil {
			row.BoughtResidents = *bought
		}
		for dst, src := range map[*int]*int{&row.WeighedAnimals: wAnimals, &row.WeighedBreeds: wBreeds, &row.WeighedSexes: wSexes,
			&row.WeighedSpeciesN: wSpeciesN, &row.WeighedBought: wBought} {
			if src != nil {
				*dst = *src
			}
		}
		row.WindowFeedKg = windowFeed
		if blocked != nil {
			row.WindowBlockedCells = *blocked
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// fcrSegments returns one row per consecutive-round pair of a pen, with the feed the sheet directed
// to that pen on the days between the two rounds.
//
// projection-review: membership=one row per (pen, earlier round, later round) where the later
// round's date is after the earlier's; group_key=(pen_shed_id, pen_key, d_prev, d) on the segment
// side, matched to the feed-day rollup on (pen_shed_id, pen_key) with feed_day in [d_prev, d);
// join_cardinality=rollup day rows are 0..N per segment (PK tenant, park, pen, day) and are
// COLLAPSED by the aggregate, prices are applied per cell inside the rollup refresh, scan pairs join two round rows per animal (0..1 each,
// by the DISTINCT ON) and are collapsed by avg(); head_days is the rollup's per-day sum of
// max(head_count) per (shed_tag_key, breed_key), pre-collapsed at refresh because head_count
// repeats on every feed item cell and every session of a day; pagination=NONE, bounded by rounds x pens in one window;
// scope=tenant + park ANY + accepted_at window + feed_day window.
//
//	PRODUCER UNIQUENESS vs CONSUMER MATCH KEYS, side by side:
//	  segments   unique on (pen_shed_id, pen_key, d_prev, d)      [LAG over rounds ordered by date]
//	  seg_feed   GROUP BY (pen_shed_id, pen_key, d_prev, d)        identical key
//	  seg_heads  GROUP BY (pen_shed_id, pen_key, d_prev, d)        identical key
//	  scan_pairs GROUP BY (pen_shed_id, pen_key, campaign_prev, campaign_id) -> one row per segment
//
//	RATIO KEY SETS: kg feed / kg gain is formed in the domain layer over the identical segment set,
//	numerator (seg_feed) and denominator (adg x seg_heads) each carrying the segment's own key.
func (r *Repository) fcrSegments(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, startDate, endDate, weighingCategory string, idMap weighingpg.AnimalIdentityMap) ([]domain.FCRSegmentRow, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	bound, err := sqlbind.Bind(fcrSegmentsSQL, tenantID, parkIDs, periodStart, periodEnd, weighingCategory, idMap.Tags, idMap.CanonicalTags, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("fcr segments bind: %w", err)
	}
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.FCRSegmentRow{}
	for rows.Next() {
		var seg domain.FCRSegmentRow
		var shedID string
		if err := rows.Scan(&shedID, &seg.PenKey, &seg.StartDate, &seg.EndDate, &seg.Animals, &seg.ADGGPerDay, &seg.Mode, &seg.PairedKids,
			&seg.FeedKg, &seg.FeedCostINR, &seg.UnpricedKg, &seg.BlockedCells, &seg.HeadDays); err != nil {
			return nil, err
		}
		seg.PenKey = shedID + "|" + seg.PenKey
		out = append(out, seg)
	}
	return out, rows.Err()
}

// salePricesSQL: per (species, stage, sex) -- the species default is the row with stage and sex
// both empty -- the newest row effective on or before the asked-for business date; when NO row was
// effective yet (a window that ends before the first assumption was ever set, as every historical
// window did on the day the table was seeded), the EARLIEST row applies. An assumption is a
// valuation the reader holds today, so a period older than the first one is priced at the first one
// rather than left unvalued -- the tab prints which row applied either way.
//
// An override whose winning row carries NO price was CLEARED on that date (000399): it is dropped
// here, so the animals it covered fall back to their species default in the domain resolver.
// Bounded by the (tenant, species, stage, sex, effective_from) unique key; a tenant holds a few
// dozen rows at most (two species x the stage vocabulary x two sexes).
const salePricesSQL = `
SELECT species, management_stage, sex, price_per_kg_inr, effective_from, set_by
FROM (
  SELECT DISTINCT ON (species, management_stage, sex)
         species, management_stage, sex, price_per_kg_inr::float8 AS price_per_kg_inr,
         effective_from::text AS effective_from, set_by
  FROM growth_sale_price_assumptions
  WHERE tenant_id = $1::uuid
  ORDER BY species, management_stage, sex,
           (effective_from <= $2::date) DESC,
           CASE WHEN effective_from <= $2::date THEN effective_from END DESC,
           effective_from ASC,
           created_at DESC
) latest
WHERE price_per_kg_inr IS NOT NULL
ORDER BY species, management_stage, sex`

// GetSalePrices returns the newest assumed sale price per species effective on or before asOf.
func (r *Repository) GetSalePrices(ctx context.Context, tenantID string, asOf time.Time) (domain.SalePrices, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	out := domain.SalePrices{Prices: []domain.SalePrice{}}
	rows, err := r.pool.Query(ctx, salePricesSQL, tenantID, asOf.In(biztime.DefaultLocation()).Format("2006-01-02"))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p domain.SalePrice
		if err := rows.Scan(&p.Species, &p.ManagementStage, &p.Sex, &p.PricePerKgINR, &p.EffectiveFrom, &p.SetBy); err != nil {
			return out, err
		}
		out.Prices = append(out.Prices, p)
	}
	return out, rows.Err()
}

// decodeHeadMix reads a pen's (species, stage, sex) head counts. A pen with no animals carries a
// NULL mix, which is an empty slice, never an error.
func decodeHeadMix(raw []byte) ([]domain.HeadMix, error) {
	if len(raw) == 0 {
		return []domain.HeadMix{}, nil
	}
	var out []domain.HeadMix
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode pen head mix: %w", err)
	}
	return out, nil
}

func decodeFCRBreedMembers(raw []byte) ([]domain.FCRCohortMember, error) {
	if len(raw) == 0 {
		return []domain.FCRCohortMember{}, nil
	}
	var rows []domain.FCRCohortMember
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode fcr breed members: %w", err)
	}
	byKey := map[string]*domain.FCRCohortMember{}
	order := []string{}
	for _, row := range rows {
		key := strings.ToLower(strings.TrimSpace(row.Key))
		label := strings.TrimSpace(row.Label)
		if key == "" {
			key = domain.CohortUnknown
		}
		if label == "" {
			label = key
		}
		animals := row.Animals
		if animals <= 0 {
			animals = 1
		}
		existing := byKey[key]
		if existing == nil {
			byKey[key] = &domain.FCRCohortMember{Key: key, Label: label}
			order = append(order, key)
			existing = byKey[key]
		}
		existing.Animals += animals
	}
	out := make([]domain.FCRCohortMember, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out, nil
}
