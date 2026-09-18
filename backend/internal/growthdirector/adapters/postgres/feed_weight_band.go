package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
)

// feedWeightBandLookbackDays is how far before the period a prior weigh is looked for, so an
// animal's first weigh inside the window can pair against one before it. It is the SAME
// figure shed_weights.go and growth.go use (growthLookbackDays), restated here because that
// constant is unexported; a different number would make this table and the General tab count
// different animals for one period.
const feedWeightBandLookbackDays = 400

// penLabelSQL is the ONE pen-key rule every side of this read folds its partition with
// (maintainer rule 2026-09-18), as a SQL expression over a name expression and a partition
// expression: the name alone when the partition is blank or 'whole' or the name ALREADY ends
// with it ("Godel 2 - Part 1" + "Part 1", "Castro 1" + "1"); name + ' ' + partition when the
// partition is numeric ("Castro" + "1"); name + ' - ' + partition otherwise ("Godel 2" + "Part
// 1"). Whitespace is collapsed first so a double space cannot split one pen into two keys.
// Three call sites -- campaign sheds, feed rows, goat placements -- and one expression, because
// three hand-written copies is how one side stops matching the other two. The separator rule is
// oploc.Display() mirrored in SQL (bare numeral joins with a space, a worded label with " - ",
// blank / 'whole' is not a partition); the "already ends with it" fold is the one addition,
// for campaign sheds stored as "Godel 2 - Part 1" + "Part 1".
func penLabelSQL(nameExpr, partitionExpr string) string {
	n := "regexp_replace(btrim(" + nameExpr + "), '\\s+', ' ', 'g')"
	p := "btrim(COALESCE(" + partitionExpr + ", ''))"
	return "CASE WHEN " + p + " = '' OR lower(" + p + ") = 'whole' THEN " + n +
		" WHEN right(" + n + ", length(" + p + ") + 1) = ' ' || " + p + " THEN " + n +
		" WHEN " + p + " ~ '^[0-9]+$' THEN " + n + " || ' ' || " + p +
		" ELSE " + n + " || ' - ' || " + p + " END"
}

// goatPartitionSQL is the goat placement's partition, with the register's bare-number form
// ("3") read as "Part 3" ONLY for a shed whose partitions are written "Part N" in the
// authoritative partition catalog (shed_partitions, migration 000112) -- goat_shed_partitions
// holds both spellings for one shed on STG, and a bare "3" under Godel 2 is the same pen as its
// neighbour's "Part 2". A shed whose catalogued partitions are bare numbers (Castro 1/2/3) keeps
// them bare, because there the number IS the pen name. `ps` is the part_sheds CTE, joined once
// rather than probed per goat. The label then goes through penLabelSQL, which mirrors
// oploc.Display() (bare numeral -> space, worded label -> " - "); this only picks the label.
const goatPartitionSQL = `CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END`

// feedWeightBandSQL is the one query behind GetFeedWeightBandSource. Package-level so a
// query-plan test and the scale guard can reach it. Bounded: one day's sheet (a few hundred
// rows), the non-canceled campaign sheds of the authorized parks in the window, and the latest
// qualifying weigh per animal / per pen; no obligation or kernel hot table is touched.
//
// THE WEIGHING SIDE IS THE GENERAL TAB'S OWN GRAIN (maintainer rule 2026-09-18): this table's
// animal counts must add up to the same "Individual" and "Lump sum" figures the ADG Analytics
// General tab reports for the same filters, so the two arms below mirror shed_weights.go's
// summary_individual and summary_lump clause for clause --
//
//   - scoped buckets: non-canceled campaign sheds of the parks, under the Weighing filter;
//   - per animal: the SAME-ANIMAL key (identity_scope.go's map, $9/$10) over accepted weighs
//     (verification_status <> 'rejected') inside the window plus the lookback, narrowed by the
//     sex/origin scope's tag list ($5/$6); an animal COUNTS only when it has a prior weigh on an
//     earlier date and a weigh inside the window, and it is banded on its LATEST such weigh,
//     attributed to the pen of that weigh;
//   - whole pen: the LIVE (withdrawn_at IS NULL) accepted shed weighs inside the window, narrowed
//     by the scope's bucket list ($7/$8); a pen COUNTS only when it was weighed on two dates in
//     the window, and it is banded on its latest average with its latest head count.
//
// SOLD AND DEAD ANIMALS (maintainer rule 2026-09-18): an animal whose goat has exited
// (goats.exited_at set -- sold, dead) is not eating today's feed, so by default it does NOT count
// toward a band's head count or average, but it is never hidden: every band row carries how many
// of its weighed animals have since exited ($13 = false), and the caller may ask for them to be
// counted ($13 = true). A pen whose weighed animals have ALL exited has no band row left and is
// therefore excluded. The General-tab reconciliation figure (animal_latest count) still counts
// them, exactly as the General tab does. Lump-sum pens carry a frozen census and are untouched.
//
// GENDER comes from the ANIMALS, never from the feed sheet's shed tag: a per-animal row counts the
// sexes of the animals in that pen and band, resolved through the ACTIVE goat_identifiers row
// (the identity_scope.go convention: newest row per normalized value) to goats.sex; a pen-average
// row counts the sexes of the goats currently placed in that pen (goats.current_location_id +
// goat_shed_partitions). This module may read goats and goat_identifiers; it is read-only
// reporting and gates nothing.
//
// FEED DAY: the latest feed_day over locked/amended issues; within it, ONE issue per park +
// workflow (latest by coalesce(locked_at, amended_at, issued_at)). Rows with quantity_kg > 0 only.
// SESSION DUPLICATES collapse by summing quantity_kg per (pen, cohort, item) with
// max(grams_per_head), because a day's sheet repeats the item across sessions.
//
// PEN LABEL (operator-facing): shed_label || ' ' || partition when the partition is numeric
// ("Castro 1"), shed_label || ' - ' || partition otherwise ("Godel 1 - Part 3"). The weighing side
// is the campaign shed's display_name, plus its partition when that is stored separately; both
// sides collapse whitespace before matching.
//
// projection-review: membership=every rollup on the latest locked/amended sheet
// (feed_direction_issue_rows with quantity_kg > 0 on ONE issue per park+workflow), LEFT joined to
// evidence so an unweighed pen is still counted and excluded by the service with its count;
// group_key=(park_id, pen_label, shed_tag, ration_group, experiment_arm, breed, workflow) on the
// feed side, (park_id, pen_label) on the weighing side and the placement side, matched on the
// whitespace-normalised pen label; join_cardinality=collapsed items pre-aggregated per rollup
// before the join, evidence 0..1 rows for a pen-average pen (DISTINCT ON park+pen over latest
// live shed weighs, sex counts pre-aggregated per pen from live placements) and 0..6 for a
// per-animal pen (one per band, DISTINCT ON animal_key before banding, tag -> goat 0..1 through
// the newest active identifier) so no side multiplies a feed row; the identity map join is 0..1
// per observation because its tag column is unique by construction; pagination=NONE, bounded by
// the pens on one day's sheet; scope=tenant + park ANY on every side, the accepted_at window
// (plus lookback for the prior weigh) on the weighing side only, and the caller-resolved
// sex/origin scope as two opaque bind lists.
var feedWeightBandSQL = strings.NewReplacer(
	"__FEED_PEN__", penLabelSQL("shed_label", "partition_label"),
	"__SHED_PEN__", penLabelSQL("cs.display_name", "cs.partition_label"),
	"__GOAT_PEN__", penLabelSQL("l.name", goatPartitionSQL),
).Replace(`
WITH latest_issue AS (
  SELECT DISTINCT ON (i.park_id, i.workflow) i.feed_direction_issue_id, i.park_id, i.workflow, i.feed_day
  FROM feed_direction_issues i
  WHERE i.tenant_id = $1::uuid
    AND i.park_id = ANY($2::uuid[])
    AND i.state IN ('amended','locked')
    AND i.feed_day = (SELECT max(x.feed_day) FROM feed_direction_issues x
                      WHERE x.tenant_id = $1::uuid AND x.park_id = ANY($2::uuid[]) AND x.state IN ('amended','locked'))
  ORDER BY i.park_id, i.workflow, COALESCE(i.locked_at, i.amended_at, i.issued_at) DESC
),
positive_rows AS (
  SELECT r.park_id, r.park_label, r.shed_label, COALESCE(r.partition_label, '') AS partition_label,
         COALESCE(r.shed_tag, '') AS shed_tag, COALESCE(r.ration_group, '') AS ration_group,
         COALESCE(r.experiment_arm, '') AS experiment_arm, COALESCE(r.breed, '') AS breed,
         r.workflow, r.feed_item_label, r.quantity_kg, r.grams_per_head
  FROM feed_direction_issue_rows r
  JOIN latest_issue li ON li.feed_direction_issue_id = r.feed_direction_issue_id
  WHERE r.tenant_id = $1::uuid AND r.quantity_kg > 0
),
collapsed AS (
  SELECT park_id, park_label, shed_label, partition_label, shed_tag, ration_group, experiment_arm, breed, workflow,
         feed_item_label, SUM(quantity_kg) AS quantity_kg, MAX(grams_per_head) AS grams_per_head
  FROM positive_rows
  GROUP BY 1,2,3,4,5,6,7,8,9,10
),
rollup AS MATERIALIZED (
  SELECT park_id, park_label, shed_tag, ration_group, experiment_arm, breed, workflow,
         ` + "__FEED_PEN__" + ` AS pen_label,
         SUM(quantity_kg) AS kg_per_day,
         json_agg(json_build_object('label', feed_item_label, 'grams_per_head', COALESCE(grams_per_head, 0)) ORDER BY feed_item_label) AS items
  FROM collapsed
  GROUP BY 1,2,3,4,5,6,7,8
),
scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, COALESCE(cs.partition_label, '') AS partition_label,
         cs.weighing_category, c.park_id,
         ` + "__SHED_PEN__" + ` AS pen_label
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND cs.status <> 'canceled'
    AND ($12::text = '' OR cs.weighing_category = $12::text)
),
animal_latest AS (
  SELECT DISTINCT ON (animal_key) animal_key, weight_kg, park_id, pen_label, scanned_identifier
  FROM (
    SELECT o.observation_id, o.weight_kg::float8 AS weight_kg, o.accepted_at, s.park_id, s.pen_label, o.scanned_identifier,
           COALESCE(akmap.canonical_tag, lower(btrim(o.scanned_identifier))) AS animal_key,
           LAG(o.accepted_at) OVER w AS prev_accepted_at
    FROM scoped s
    JOIN weighing_observations o ON o.tenant_id = s.tenant_id AND o.campaign_shed_id = s.campaign_shed_id
    LEFT JOIN unnest($9::text[], $10::text[]) AS akmap(tag, canonical_tag)
      ON akmap.tag = lower(btrim(o.scanned_identifier))
    WHERE s.weighing_category = 'individual_animal'
      AND $12::text <> 'per_shed_partition'
      AND o.accepted_at >= ($3::timestamptz - ($11::int * INTERVAL '1 day'))
      AND o.accepted_at <  $4::timestamptz
      AND o.verification_status <> 'rejected'
      AND btrim(o.scanned_identifier) <> ''
      AND (NOT $5::bool OR lower(btrim(o.scanned_identifier)) = ANY($6::text[]))
    WINDOW w AS (PARTITION BY COALESCE(akmap.canonical_tag, lower(btrim(o.scanned_identifier))) ORDER BY o.accepted_at, o.observation_id)
  ) pairs
  WHERE prev_accepted_at IS NOT NULL
    AND ((accepted_at AT TIME ZONE 'Asia/Kolkata')::date - (prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date) > 0
    AND accepted_at >= $3::timestamptz
  ORDER BY animal_key, accepted_at DESC, observation_id DESC
),
animal_sex AS (
  SELECT la.park_id, la.pen_label, la.weight_kg, g.sex, (g.exited_at IS NOT NULL) AS exited,
         (g.exited_at IS NOT NULL AND (g.lifecycle_status = 'sold' OR g.exit_reason = 'sold')) AS sold
  FROM animal_latest la
  LEFT JOIN LATERAL (
    SELECT gi.goat_id
    FROM goat_identifiers gi
    WHERE gi.tenant_id = $1::uuid AND gi.status = 'active'
      AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
      AND lower(btrim(gi.identifier_value)) = lower(btrim(la.scanned_identifier))
    ORDER BY gi.created_at DESC
    LIMIT 1
  ) ident ON TRUE
  LEFT JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = ident.goat_id
),
lump_points AS (
  SELECT s.park_id, s.location_id, s.partition_label, s.pen_label,
         sh.shed_observation_id, sh.animal_count, sh.average_weight_kg, sh.accepted_at,
         (sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM scoped s
  JOIN weighing_shed_observations sh
    ON sh.campaign_shed_id = s.campaign_shed_id AND sh.tenant_id = s.tenant_id
   AND sh.withdrawn_at IS NULL
   AND sh.accepted_at >= $3::timestamptz
   AND sh.accepted_at <  $4::timestamptz
   AND sh.verification_status <> 'rejected'
  WHERE s.weighing_category = 'per_shed_partition'
    AND (NOT $5::bool OR EXISTS (
      SELECT 1 FROM unnest($7::uuid[], $8::text[]) AS b(loc, part)
      WHERE b.loc = s.location_id AND b.part = s.partition_label
    ))
),
pen_avg AS (
  SELECT DISTINCT ON (l.park_id, l.pen_label)
         l.park_id, l.pen_label, l.average_weight_kg AS avg_kg, l.animal_count AS n
  FROM (
    SELECT DISTINCT ON (park_id, location_id, partition_label)
           park_id, location_id, partition_label, pen_label, animal_count, average_weight_kg, accepted_at, d,
           min(d) OVER (PARTITION BY park_id, location_id, partition_label) AS first_d
    FROM lump_points p
    ORDER BY park_id, location_id, partition_label, accepted_at DESC, shed_observation_id DESC
  ) l
  WHERE l.d > l.first_d
  ORDER BY l.park_id, l.pen_label, l.accepted_at DESC
),
part_sheds AS MATERIALIZED (
  SELECT DISTINCT q.shed_id
  FROM shed_partitions q
  WHERE q.tenant_id = $1::uuid AND q.partition_label ~ '^Part [0-9]+$'
),
pen_sex AS MATERIALIZED (
  SELECT g.park_id,
         ` + "__GOAT_PEN__" + ` AS pen_label,
         COUNT(*) FILTER (WHERE g.sex = 'female')::int AS female_count,
         COUNT(*) FILTER (WHERE g.sex = 'male')::int AS male_count
  FROM goats g
  JOIN locations l ON l.tenant_id = g.tenant_id AND l.location_id = g.current_location_id
  LEFT JOIN goat_shed_partitions p ON p.tenant_id = g.tenant_id AND p.goat_id = g.goat_id
  LEFT JOIN part_sheds ps ON ps.shed_id = p.shed_id
  WHERE g.tenant_id = $1::uuid AND g.park_id = ANY($2::uuid[]) AND g.exited_at IS NULL
  GROUP BY 1, 2
),
evidence AS MATERIALIZED (
  SELECT pa.park_id, pa.pen_label, 'pen_average' AS source,
         CASE WHEN pa.avg_kg < 15 THEN 'under_15' WHEN pa.avg_kg < 20 THEN '15_20' WHEN pa.avg_kg < 25 THEN '20_25'
              WHEN pa.avg_kg < 30 THEN '25_30' WHEN pa.avg_kg < 35 THEN '30_35' ELSE '35_plus' END AS band,
         pa.n, pa.avg_kg, COALESCE(ps.female_count, 0) AS female_count, COALESCE(ps.male_count, 0) AS male_count,
         0 AS exited_n, 0 AS sold_n
  FROM pen_avg pa
  LEFT JOIN pen_sex ps ON ps.park_id = pa.park_id AND ps.pen_label = pa.pen_label
  UNION ALL
  SELECT la.park_id, la.pen_label, 'per_animal',
         CASE WHEN la.weight_kg < 15 THEN 'under_15' WHEN la.weight_kg < 20 THEN '15_20' WHEN la.weight_kg < 25 THEN '20_25'
              WHEN la.weight_kg < 30 THEN '25_30' WHEN la.weight_kg < 35 THEN '30_35' ELSE '35_plus' END AS band,
         COUNT(*) FILTER (WHERE $13::bool OR NOT la.exited)::int AS n,
         AVG(la.weight_kg) FILTER (WHERE $13::bool OR NOT la.exited) AS avg_kg,
         COUNT(*) FILTER (WHERE la.sex = 'female' AND ($13::bool OR NOT la.exited))::int AS female_count,
         COUNT(*) FILTER (WHERE la.sex = 'male' AND ($13::bool OR NOT la.exited))::int AS male_count,
         COUNT(*) FILTER (WHERE la.exited)::int AS exited_n,
         COUNT(*) FILTER (WHERE la.sold)::int AS sold_n
  FROM animal_sex la
  WHERE NOT EXISTS (SELECT 1 FROM pen_avg pa WHERE pa.park_id = la.park_id AND pa.pen_label = la.pen_label)
  GROUP BY 1, 2, 3, 4
  HAVING COUNT(*) FILTER (WHERE $13::bool OR NOT la.exited) > 0
)
SELECT
  (SELECT max(feed_day) FROM latest_issue),
  (SELECT count(*) FROM positive_rows)::int,
  (SELECT count(*) FROM collapsed)::int,
  (SELECT count(*) FROM animal_latest)::int,
  (SELECT COALESCE(sum(n), 0) FROM pen_avg)::int,
  r.park_id::text, COALESCE(pk.name, r.park_label, ''), r.pen_label, r.shed_tag, r.ration_group, r.experiment_arm, r.breed, r.workflow,
  r.kg_per_day::float8, r.items::text,
  COALESCE(e.source, ''), COALESCE(e.band, ''), COALESCE(e.n, 0), COALESCE(e.avg_kg, 0)::float8,
  COALESCE(e.female_count, 0), COALESCE(e.male_count, 0), COALESCE(e.exited_n, 0), COALESCE(e.sold_n, 0)
FROM rollup r
LEFT JOIN locations pk ON pk.tenant_id = $1::uuid AND pk.location_id = r.park_id
LEFT JOIN evidence e ON e.park_id = r.park_id AND e.pen_label = r.pen_label
ORDER BY r.park_id, r.pen_label, r.shed_tag, r.workflow, r.ration_group, r.experiment_arm, r.breed, e.source, e.band`)

// feedWeightBandExitedSQL lists the animals that were sold or died inside the period, with
// their last weigh in the period (if any) -- the "Sold / dead in period" list under the table.
// Bounded by the goats that exited in the window in the parks (STG: 128 over six weeks). Under
// a sex/origin filter only animals whose tag is in the scope list are listed, the same list that
// narrows the band rows; an exited animal never weighed in the period has no tag to test and is
// left out of a filtered list rather than guessed at.
//
// SET-BASED, not a per-goat LATERAL: the first cut looped a campaign-shed scan per exited goat
// (121k loops, 24 s on the OCI clone). The tag -> weigh join now rides
// weighing_observations_demo_tag_window_idx once for the whole tag set.
//
// projection-review: membership=goats with exited_at inside the window in the parks;
// group_key=goat_id; join_cardinality=identifiers 0..2 per goat, their weighs collapsed by
// DISTINCT ON (goat_id) to the latest, the primary tag by DISTINCT ON (goat_id), so one row per
// goat; pagination=NONE, bounded by exits in the window; scope=tenant + park ANY + exited_at
// window, weigh window and the scope tag list on the weigh join.
var feedWeightBandExitedSQL = strings.NewReplacer(
	"__SHED_PEN__", penLabelSQL("cs.display_name", "cs.partition_label"),
).Replace(`
WITH exited AS (
  SELECT g.goat_id, g.park_id, g.sex, COALESCE(g.exit_reason, '') AS exit_reason, g.exited_at, COALESCE(g.lifecycle_status, '') AS lifecycle_status
  FROM goats g
  WHERE g.tenant_id = $1::uuid AND g.park_id = ANY($2::uuid[])
    AND g.exited_at >= $3::timestamptz AND g.exited_at < $4::timestamptz
),
-- NOT MATERIALIZED: referenced twice, so Postgres would otherwise fence it off and plan the
-- weigh join against a one-row estimate, walking the whole window's observations per tag
-- instead of probing weighing_observations_demo_tag_window_idx by tag.
tags AS NOT MATERIALIZED (
  SELECT gi.goat_id, lower(btrim(gi.identifier_value)) AS tag, gi.identifier_type
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    AND btrim(gi.identifier_value) <> ''
    AND gi.goat_id IN (SELECT goat_id FROM exited)
),
primary_tag AS (
  SELECT DISTINCT ON (goat_id) goat_id, tag FROM tags ORDER BY goat_id, identifier_type
),
scoped AS (
  SELECT cs.campaign_shed_id, __SHED_PEN__ AS pen_label
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[]) AND cs.status <> 'canceled'
    AND ($7::text = '' OR cs.weighing_category = $7::text)
),
weighs AS (
  SELECT DISTINCT ON (t.goat_id) t.goat_id, o.scanned_identifier, o.weight_kg, o.accepted_at, s.pen_label
  FROM tags t
  JOIN weighing_observations o ON o.tenant_id = $1::uuid AND lower(btrim(o.scanned_identifier)) = t.tag
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id
  WHERE o.accepted_at >= $3::timestamptz AND o.accepted_at < $4::timestamptz
    AND o.verification_status <> 'rejected'
    AND (NOT $5::bool OR lower(btrim(o.scanned_identifier)) = ANY($6::text[]))
  ORDER BY t.goat_id, o.accepted_at DESC, o.observation_id DESC
)
SELECT x.goat_id::text, x.park_id::text,
       COALESCE(w.scanned_identifier, pt.tag, ''),
       COALESCE(w.pen_label, ''),
       x.exit_reason, x.lifecycle_status, x.exited_at, COALESCE(x.sex, ''),
       w.accepted_at, COALESCE(w.weight_kg, 0)::float8, (w.weight_kg IS NOT NULL)
FROM exited x
LEFT JOIN weighs w ON w.goat_id = x.goat_id
LEFT JOIN primary_tag pt ON pt.goat_id = x.goat_id
WHERE NOT $5::bool OR w.goat_id IS NOT NULL
ORDER BY x.exited_at DESC, x.goat_id`)

// GetFeedWeightBandSource reads the latest locked feed direction per park and workflow, rolls it
// up per pen and cohort, and attaches each pen's weight evidence at the General tab's own grain
// (see feedWeightBandSQL). The sex/origin scope and the same-animal map come from the weighing
// module's ONE resolver each, exactly as the Growth Director widgets resolve them, so this table
// and the Weights pages can never disagree about which animals are male or which two tags are
// one animal.
func (r *Repository) GetFeedWeightBandSource(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string, includeExited bool) (ports.FeedWeightBandSource, error) {
	out := ports.FeedWeightBandSource{Rollups: []ports.FeedRollup{}}
	if len(parkIDs) == 0 {
		return out, nil
	}
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	weighingCategory = strings.TrimSpace(weighingCategory)
	if weighingCategory == "all" {
		weighingCategory = ""
	}
	sexApplied := strings.TrimSpace(sex) != ""
	originApplied := strings.TrimSpace(origin) != ""
	var scope weighingpg.ReportScope
	if sexApplied {
		resolved, err := weighingpg.ResolveSexScope(ctx, r.pool, tenantID, parkIDs, sex, periodStart, periodEnd)
		if err != nil {
			return out, err
		}
		scope = resolved
	}
	var originScope weighingpg.ReportScope
	if originApplied {
		resolved, err := weighingpg.ResolveOriginScope(ctx, r.pool, tenantID, parkIDs, origin, periodStart, periodEnd)
		if err != nil {
			return out, err
		}
		originScope = resolved
	}
	scope = weighingpg.IntersectScopes(scope, sexApplied, originScope, originApplied)
	filtered := sexApplied || originApplied
	// Window-bounded and widened by the SAME lookback the pairing arm reads, the shed_weights.go
	// shape: an animal whose previous weigh sits before the window must merge here exactly as it
	// merges on the General tab.
	idMap := weighingpg.EmptyAnimalIdentityMap()
	if weighingCategory != "per_shed_partition" {
		resolved, err := weighingpg.ResolveAnimalIdentityMap(ctx, r.pool, tenantID, parkIDs, periodStart.AddDate(0, 0, -feedWeightBandLookbackDays), periodEnd)
		if err != nil {
			return out, err
		}
		idMap = resolved
	}

	rows, err := r.pool.Query(ctx, feedWeightBandSQL,
		tenantID, parkIDs, periodStart, periodEnd,
		filtered, scope.Tags, scope.LocationIDs, scope.PartitionLabels,
		idMap.Tags, idMap.CanonicalTags, feedWeightBandLookbackDays, weighingCategory, includeExited)
	if err != nil {
		return out, fmt.Errorf("growthdirector: feed weight band: %w", err)
	}
	defer rows.Close()

	type rollupKey struct{ park, pen, tag, ration, arm, breed, workflow string }
	index := map[rollupKey]int{}
	for rows.Next() {
		var (
			feedDay                 *time.Time
			positive, collapsed     int
			individual, lump        int
			parkID, parkName, pen   string
			tag, ration, arm, breed string
			workflow, itemsJSON     string
			kgPerDay, avgKg         float64
			source, band            string
			animals, female, male   int
			exited, sold            int
		)
		if err := rows.Scan(&feedDay, &positive, &collapsed, &individual, &lump, &parkID, &parkName, &pen, &tag, &ration, &arm, &breed, &workflow,
			&kgPerDay, &itemsJSON, &source, &band, &animals, &avgKg, &female, &male, &exited, &sold); err != nil {
			return out, fmt.Errorf("growthdirector: feed weight band scan: %w", err)
		}
		if feedDay != nil {
			out.FeedDay = feedDay.Format("2006-01-02")
		}
		out.PositiveRows, out.CollapsedItems = positive, collapsed
		out.IndividualAnimalsWeighed, out.LumpSumAnimalsWeighed = individual, lump
		key := rollupKey{parkID, pen, tag, ration, arm, breed, workflow}
		i, ok := index[key]
		if !ok {
			var raw []struct {
				Label        string  `json:"label"`
				GramsPerHead float64 `json:"grams_per_head"`
			}
			if err := json.Unmarshal([]byte(itemsJSON), &raw); err != nil {
				return out, fmt.Errorf("growthdirector: feed weight band items: %w", err)
			}
			items := make([]ports.FeedRollupItem, 0, len(raw))
			for _, item := range raw {
				items = append(items, ports.FeedRollupItem{Label: item.Label, GramsPerHead: item.GramsPerHead})
			}
			out.Rollups = append(out.Rollups, ports.FeedRollup{
				ParkID: parkID, ParkName: parkName, Pen: strings.TrimSpace(pen), ShedTag: tag, RationGroup: ration,
				ExperimentArm: arm, Breed: breed, Workflow: workflow, KgPerDay: kgPerDay, Items: items,
				Evidence: []ports.FeedWeightEvidence{},
			})
			i = len(out.Rollups) - 1
			index[key] = i
		}
		if source != "" {
			out.Rollups[i].Evidence = append(out.Rollups[i].Evidence, ports.FeedWeightEvidence{
				Source: source, Band: band, Animals: animals, AverageWeightKg: avgKg, FemaleCount: female, MaleCount: male,
				ExitedAnimals: exited, ExitedSold: sold,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("growthdirector: feed weight band rows: %w", err)
	}
	rows.Close()

	exitedRows, err := r.pool.Query(ctx, feedWeightBandExitedSQL, tenantID, parkIDs, periodStart, periodEnd, filtered, scope.Tags, weighingCategory)
	if err != nil {
		return out, fmt.Errorf("growthdirector: feed weight band exits: %w", err)
	}
	defer exitedRows.Close()
	out.Exited = []ports.FeedExitedAnimal{}
	for exitedRows.Next() {
		var (
			x         ports.FeedExitedAnimal
			exitedAt  time.Time
			lastWeigh *time.Time
			weightKg  float64
			hasWeigh  bool
		)
		if err := exitedRows.Scan(&x.GoatID, &x.ParkID, &x.Tag, &x.Pen, &x.ExitReason, &x.LifecycleStatus, &exitedAt, &x.Sex, &lastWeigh, &weightKg, &hasWeigh); err != nil {
			return out, fmt.Errorf("growthdirector: feed weight band exits scan: %w", err)
		}
		x.ExitedAt = exitedAt
		if hasWeigh && lastWeigh != nil {
			x.LastWeighedAt = lastWeigh
			x.LastWeightKg = weightKg
		}
		out.Exited = append(out.Exited, x)
	}
	if err := exitedRows.Err(); err != nil {
		return out, fmt.Errorf("growthdirector: feed weight band exits rows: %w", err)
	}
	return out, nil
}
