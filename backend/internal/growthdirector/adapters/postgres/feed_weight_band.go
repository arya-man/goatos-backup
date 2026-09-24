package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"

	"github.com/vgoats/goatos/backend/internal/platform/readcache"

	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
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
//     the window, and it is banded on its latest average with its latest head count. The two
//     dates and the latest weigh are taken on the FOLDED pen key across every bucket spelling of
//     that pen, which is the one place this read is deliberately wider than shed_weights.go
//     (that read pairs per campaign-shed bucket); the lump total can therefore exceed the
//     General tab's by the pens whose two dates sit in differently-spelled buckets.
//
// EXITED ANIMALS (maintainer rule 2026-09-18; semantics corrected the same day, Codex P1): an
// animal whose goat has EXITED (goats.exited_at set -- sold, died, or any other exit such as an
// inactive/transferred record) is not eating today's feed, so by default it does NOT count
// toward a band's head count or average, but it is never hidden. Every per-animal band row carries
// BOTH variants -- on-farm animals (n / avg / sexes) and every weighed animal including exited
// (n_all / avg_all / sexes) -- plus the exited count split into the SOLD and DIED buckets (the
// remainder is "other"; the bucket rule is domain.FeedExitBucket, mirrored by the two flags
// below), so the screen's Animals toggle is a
// client-side flip. A band row whose animals have all exited comes back with n = 0 and n_all > 0;
// the screen shows it only under "include". The General-tab reconciliation figure (animal_latest
// count) counts exited animals, exactly as the General tab does. Lump-sum pens carry a frozen
// census and are untouched (both variants equal).
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
  -- The latest locked/amended sheet PER PARK AND WORKFLOW (Codex P1 on PR #304: a global
  -- max(feed_day) across the selected parks dropped a park whose newest sheet was a day older).
  SELECT DISTINCT ON (i.park_id, i.workflow) i.feed_direction_issue_id, i.park_id, i.workflow, i.feed_day
  FROM feed_direction_issues i
  WHERE i.tenant_id = $1::uuid
    AND i.park_id = ANY($2::uuid[])
    AND i.state IN ('amended','locked')
  ORDER BY i.park_id, i.workflow, i.feed_day DESC, COALESCE(i.locked_at, i.amended_at, i.issued_at) DESC
),
positive_rows AS (
  SELECT r.park_id, r.park_label, r.shed_label, COALESCE(r.partition_label, '') AS partition_label,
         COALESCE(r.shed_tag, '') AS shed_tag, COALESCE(r.ration_group, '') AS ration_group,
         COALESCE(r.experiment_arm, '') AS experiment_arm, COALESCE(r.breed, '') AS breed,
         r.workflow, r.feed_item_label, r.quantity_kg, r.grams_per_head, li.feed_day
  FROM feed_direction_issue_rows r
  JOIN latest_issue li ON li.feed_direction_issue_id = r.feed_direction_issue_id
  WHERE r.tenant_id = $1::uuid AND r.quantity_kg > 0
),
collapsed AS (
  SELECT park_id, park_label, shed_label, partition_label, shed_tag, ration_group, experiment_arm, breed, workflow,
         feed_item_label, SUM(quantity_kg) AS quantity_kg, MAX(grams_per_head) AS grams_per_head,
         -- one sheet per (park, workflow), so feed_day is functionally dependent on the key
         MAX(feed_day) AS feed_day
  FROM positive_rows
  GROUP BY 1,2,3,4,5,6,7,8,9,10
),
rollup AS MATERIALIZED (
  SELECT park_id, park_label, shed_tag, ration_group, experiment_arm, breed, workflow,
         ` + "__FEED_PEN__" + ` AS pen_label,
         MAX(feed_day) AS feed_day,
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
         (g.exited_at IS NOT NULL AND (g.lifecycle_status = 'sold' OR g.exit_reason = 'sold')) AS sold,
         (g.exited_at IS NOT NULL AND NOT (g.lifecycle_status = 'sold' OR g.exit_reason = 'sold')
          AND (g.lifecycle_status IN ('dead', 'died') OR g.exit_reason IN ('dead', 'died'))) AS died
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
  -- Paired and picked on the FOLDED pen key, not the campaign-shed bucket: one pen can sit in
  -- two buckets spelled differently ("Godel 2" + "Part 1" one week, "Godel 2 - Part 1" the
  -- next), and pairing per bucket discarded the newer weigh and served last week's average
  -- (judge finding, 2026-09-18). The pen's two dates may come from either bucket; the latest
  -- live weigh across them is the one banded.
  SELECT DISTINCT ON (l.park_id, l.pen_label)
         l.park_id, l.pen_label, l.average_weight_kg AS avg_kg, l.animal_count AS n
  FROM (
    SELECT park_id, pen_label, animal_count, average_weight_kg, accepted_at, shed_observation_id, d,
           min(d) OVER (PARTITION BY park_id, pen_label) AS first_d
    FROM lump_points p
  ) l
  WHERE l.d > l.first_d
  ORDER BY l.park_id, l.pen_label, l.accepted_at DESC, l.shed_observation_id DESC
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
         -- Band index over the CALLER's edges ($13, the tenant's weight_band_edges_kg assumption);
         -- the key and farm label are composed in Go from the same edges.
         width_bucket(pa.avg_kg, $13::numeric[])::text AS band,
         pa.n, pa.avg_kg, COALESCE(ps.female_count, 0) AS female_count, COALESCE(ps.male_count, 0) AS male_count,
         pa.n AS n_all, pa.avg_kg AS avg_kg_all, COALESCE(ps.female_count, 0) AS female_all, COALESCE(ps.male_count, 0) AS male_all,
         0 AS exited_n, 0 AS sold_n, 0 AS died_n
  FROM pen_avg pa
  LEFT JOIN pen_sex ps ON ps.park_id = pa.park_id AND ps.pen_label = pa.pen_label
  UNION ALL
  SELECT la.park_id, la.pen_label, 'per_animal',
         width_bucket(la.weight_kg, $13::numeric[])::text AS band,
         -- BOTH head-count variants in one row (maintainer request 2026-09-18): on-farm animals
         -- only, and every weighed animal including those since sold or dead, so the screen's
         -- Animals toggle flips without another read.
         COUNT(*) FILTER (WHERE NOT la.exited)::int AS n,
         AVG(la.weight_kg) FILTER (WHERE NOT la.exited) AS avg_kg,
         COUNT(*) FILTER (WHERE la.sex = 'female' AND NOT la.exited)::int AS female_count,
         COUNT(*) FILTER (WHERE la.sex = 'male' AND NOT la.exited)::int AS male_count,
         COUNT(*)::int AS n_all,
         AVG(la.weight_kg) AS avg_kg_all,
         COUNT(*) FILTER (WHERE la.sex = 'female')::int AS female_all,
         COUNT(*) FILTER (WHERE la.sex = 'male')::int AS male_all,
         COUNT(*) FILTER (WHERE la.exited)::int AS exited_n,
         COUNT(*) FILTER (WHERE la.sold)::int AS sold_n,
         COUNT(*) FILTER (WHERE la.died)::int AS died_n
  FROM animal_sex la
  WHERE NOT EXISTS (SELECT 1 FROM pen_avg pa WHERE pa.park_id = la.park_id AND pa.pen_label = la.pen_label)
  GROUP BY 1, 2, 3, 4
)
SELECT
  (SELECT max(feed_day) FROM latest_issue),
  (SELECT count(*) FROM positive_rows)::int,
  (SELECT count(*) FROM collapsed)::int,
  (SELECT count(*) FROM animal_latest)::int,
  (SELECT COALESCE(sum(n), 0) FROM pen_avg)::int,
  r.park_id::text, COALESCE(NULLIF(r.park_label, ''), pk.name, ''), r.pen_label, r.shed_tag, r.ration_group, r.experiment_arm, r.breed, r.workflow,
  r.feed_day, r.kg_per_day::float8, r.items::text,
  COALESCE(e.source, ''), COALESCE(e.band, ''), COALESCE(e.n, 0), COALESCE(e.avg_kg, 0)::float8,
  COALESCE(e.female_count, 0), COALESCE(e.male_count, 0), COALESCE(e.exited_n, 0), COALESCE(e.sold_n, 0), COALESCE(e.died_n, 0),
  COALESCE(e.n_all, 0), COALESCE(e.avg_kg_all, 0)::float8, COALESCE(e.female_all, 0), COALESCE(e.male_all, 0)
FROM rollup r
LEFT JOIN locations pk ON pk.tenant_id = $1::uuid AND pk.location_id = r.park_id
LEFT JOIN evidence e ON e.park_id = r.park_id AND e.pen_label = r.pen_label
ORDER BY r.park_id, r.pen_label, r.shed_tag, r.workflow, r.ration_group, r.experiment_arm, r.breed, e.source, e.band`)

// feedWeightBandExitedSQL lists EVERY animal that exited the register inside the period under
// the page's park / sex / origin filters -- the same population Herd Analytics' exits count
// (goats.exited_at inside the window, one row per goat), so the card's "exited in period" chip
// reconciles to that screen (Codex P1 on PR #304: the first cut narrowed a filtered list to
// animals with weighing evidence and the chip read as a herd figure). Sex is goats.sex, origin is
// procurement_load_goats membership (purchased) or its absence (farm born) -- per ANIMAL, the
// origin_scope.go rule -- applied here directly because this module is the sanctioned read-only
// herd join; the band rows keep taking their scope from the weighing resolvers. Each animal
// carries its last weigh in the period when it has one (weighed_in_period); one without is
// listed with its current placement as the pen and no band, and can never sit on a band row.
//
// SET-BASED, not a per-goat LATERAL: the first cut looped a campaign-shed scan per exited goat
// (121k loops, 24 s on the OCI clone). The tag -> weigh join now rides
// weighing_observations_demo_tag_window_idx once for the whole tag set.
//
// projection-review: membership=goats with exited_at inside the window in the parks, narrowed
// by goats.sex and procurement_load_goats membership when asked; group_key=goat_id;
// join_cardinality=identifiers 0..2 per goat, their weighs collapsed by DISTINCT ON (goat_id) to
// the latest, the primary tag by DISTINCT ON (goat_id), placement 1:{0,1} on the goat primary
// key, so one row per goat; pagination=NONE, bounded by exits in the window; scope=tenant + park
// ANY + exited_at window (+ sex / origin), weigh window on the weigh join.
var feedWeightBandExitedSQL = strings.NewReplacer(
	"__SHED_PEN__", penLabelSQL("cs.display_name", "cs.partition_label"),
	"__GOAT_PEN__", penLabelSQL("l.name", goatPartitionSQL),
).Replace(`
WITH exited AS (
  SELECT g.goat_id, g.park_id, g.sex, COALESCE(g.exit_reason, '') AS exit_reason, g.exited_at,
         COALESCE(g.lifecycle_status, '') AS lifecycle_status, g.current_location_id
  FROM goats g
  WHERE g.tenant_id = $1::uuid AND g.park_id = ANY($2::uuid[])
    AND g.merged_into_goat_id IS NULL
    AND g.exited_at >= $3::timestamptz AND g.exited_at < $4::timestamptz
    AND ($5::text = '' OR g.sex = $5::text)
    AND ($6::text = ''
         OR ($6::text = 'purchased') = EXISTS (SELECT 1 FROM procurement_load_goats plg WHERE plg.tenant_id = g.tenant_id AND plg.goat_id = g.goat_id))
),
-- NOT MATERIALIZED: referenced twice, so Postgres would otherwise fence it off and plan the
-- weigh join against a one-row estimate, walking the whole window's observations per tag
-- instead of probing weighing_observations_demo_tag_window_idx by tag.
tags AS NOT MATERIALIZED (
  SELECT gi.goat_id, lower(btrim(gi.identifier_value)) AS tag, btrim(gi.identifier_value) AS shown, gi.identifier_type
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    AND btrim(gi.identifier_value) <> ''
    AND gi.goat_id IN (SELECT goat_id FROM exited)
),
primary_tag AS (
  SELECT DISTINCT ON (goat_id) goat_id, shown AS tag FROM tags ORDER BY goat_id, identifier_type
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
  ORDER BY t.goat_id, o.accepted_at DESC, o.observation_id DESC
),
part_sheds AS MATERIALIZED (
  SELECT DISTINCT q.shed_id
  FROM shed_partitions q
  WHERE q.tenant_id = $1::uuid AND q.partition_label ~ '^Part [0-9]+$'
),
placement AS (
  SELECT x.goat_id, __GOAT_PEN__ AS pen_label
  FROM exited x
  JOIN locations l ON l.tenant_id = $1::uuid AND l.location_id = x.current_location_id
  LEFT JOIN goat_shed_partitions p ON p.tenant_id = $1::uuid AND p.goat_id = x.goat_id
  LEFT JOIN part_sheds ps ON ps.shed_id = p.shed_id
)
SELECT x.goat_id::text, x.park_id::text,
       COALESCE(w.scanned_identifier, pt.tag, ''),
       COALESCE(w.pen_label, pl.pen_label, ''),
       x.exit_reason, x.lifecycle_status, x.exited_at, COALESCE(x.sex, ''),
       w.accepted_at, COALESCE(w.weight_kg, 0)::float8, (w.weight_kg IS NOT NULL)
FROM exited x
LEFT JOIN weighs w ON w.goat_id = x.goat_id
LEFT JOIN primary_tag pt ON pt.goat_id = x.goat_id
LEFT JOIN placement pl ON pl.goat_id = x.goat_id
ORDER BY x.exited_at DESC, x.goat_id`)

// GetFeedWeightBandSource reads the latest locked feed direction per park and workflow, rolls it
// up per pen and cohort, and attaches each pen's weight evidence at the General tab's own grain
// (see feedWeightBandSQL). The sex/origin scope and the same-animal map come from the weighing
// module's ONE resolver each, exactly as the Growth Director widgets resolve them, so this table
// and the Weights pages can never disagree about which animals are male or which two tags are
// one animal.
func (r *Repository) GetFeedWeightBandSource(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string, bandEdgesKg []float64) (ports.FeedWeightBandSource, error) {
	out := ports.FeedWeightBandSource{Rollups: []ports.FeedRollup{}}
	if len(parkIDs) == 0 {
		return out, nil
	}
	weighingCategory = strings.TrimSpace(weighingCategory)
	if weighingCategory == "all" {
		weighingCategory = ""
	}
	// The same short burst cache and single flight as GetGrowthDirectorWeights (30 s): the read is
	// five serial round trips (two scope resolvers, the identity map, the sheet query, the exits
	// query) and the page re-runs it on every top-bar filter change; without this every open of
	// the tab paid the whole chain again (latency judge, 2026-09-18).
	edgeKey := make([]string, 0, len(bandEdgesKg))
	for _, e := range bandEdgesKg {
		edgeKey = append(edgeKey, strconv.FormatFloat(e, 'f', -1, 64))
	}
	cacheKey := growthDirectorReadKey("feed_weight_band", tenantID, strings.Join(append([]string{}, parkIDs...), ","),
		periodStart.UTC().Format(time.RFC3339), periodEnd.UTC().Format(time.RFC3339), sex, origin, weighingCategory, "bands="+strings.Join(edgeKey, ","))
	return readcache.Load(ctx, r.cache, gdReadKey(tenantID, parkIDs, cacheKey), func(ctx context.Context) (ports.FeedWeightBandSource, error) {
		return r.readFeedWeightBandSource(ctx, tenantID, parkIDs, periodStart, periodEnd, sex, origin, weighingCategory, bandEdgesKg)
	})
}

// readFeedWeightBandSource is the uncached read behind GetFeedWeightBandSource. The two scope
// resolvers and the identity map are independent of each other and run concurrently, and the
// exits query (register filters only) runs beside them from the start; the sheet query follows
// the scope. Fan-out is bounded by construction to four concurrent pool queries per request, the same bound weighing's growth read takes with
// weighingGrowthReadParallelism; nothing here spawns per row.
func (r *Repository) readFeedWeightBandSource(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string, bandEdgesKg []float64) (ports.FeedWeightBandSource, error) {
	if len(bandEdgesKg) == 0 {
		bandEdgesKg = weighingdomain.DefaultWeightBandEdgesKg
	}
	out := ports.FeedWeightBandSource{Rollups: []ports.FeedRollup{}}
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	// The exits list takes the page's filters straight from the register (no weighing scope), so
	// it starts first and runs beside the resolvers and the sheet query.
	type exitsResult struct {
		rows []ports.FeedExitedAnimal
		err  error
	}
	exitsCh := make(chan exitsResult, 1)
	go func() {
		rows, err := r.readFeedWeightBandExits(ctx, tenantID, parkIDs, periodStart, periodEnd, sex, origin, weighingCategory)
		exitsCh <- exitsResult{rows: rows, err: err}
	}()

	sexApplied := strings.TrimSpace(sex) != ""
	originApplied := strings.TrimSpace(origin) != ""
	var (
		scope, originScope weighingpg.ReportScope
		idMap              = weighingpg.EmptyAnimalIdentityMap()
		wg                 sync.WaitGroup
		errMu              sync.Mutex
		firstErr           error
	)
	fail := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}
	if sexApplied {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolved, err := weighingpg.ResolveSexScope(ctx, r.pool, tenantID, parkIDs, sex, periodStart, periodEnd)
			if err != nil {
				fail(err)
				return
			}
			scope = resolved
		}()
	}
	if originApplied {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolved, err := weighingpg.ResolveOriginScope(ctx, r.pool, tenantID, parkIDs, origin, periodStart, periodEnd)
			if err != nil {
				fail(err)
				return
			}
			originScope = resolved
		}()
	}
	// Window-bounded and widened by the SAME lookback the pairing arm reads, the shed_weights.go
	// shape: an animal whose previous weigh sits before the window must merge here exactly as it
	// merges on the General tab.
	if weighingCategory != "per_shed_partition" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolved, err := weighingpg.ResolveAnimalIdentityMap(ctx, r.pool, tenantID, parkIDs, periodStart.AddDate(0, 0, -feedWeightBandLookbackDays), periodEnd)
			if err != nil {
				fail(err)
				return
			}
			idMap = resolved
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return out, firstErr
	}
	scope = weighingpg.IntersectScopes(scope, sexApplied, originScope, originApplied)
	filtered := sexApplied || originApplied

	fwbBind519 := sqlbind.MustBind(feedWeightBandSQL,
		tenantID, parkIDs, periodStart, periodEnd,
		filtered, scope.Tags, scope.LocationIDs, scope.PartitionLabels,
		idMap.Tags, idMap.CanonicalTags, feedWeightBandLookbackDays, weighingCategory, bandEdgesKg)
	rows, err := r.pool.Query(ctx, fwbBind519.SQL(), fwbBind519.Args()...)
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
			rollupDay               time.Time
			kgPerDay, avgKg         float64
			source, band            string
			animals, female, male   int
			exited, sold, died      int
			animalsAll              int
			avgKgAll                float64
			femaleAll, maleAll      int
		)
		if err := rows.Scan(&feedDay, &positive, &collapsed, &individual, &lump, &parkID, &parkName, &pen, &tag, &ration, &arm, &breed, &workflow,
			&rollupDay, &kgPerDay, &itemsJSON, &source, &band, &animals, &avgKg, &female, &male, &exited, &sold, &died, &animalsAll, &avgKgAll, &femaleAll, &maleAll); err != nil {
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
				ExperimentArm: arm, Breed: breed, Workflow: workflow, FeedDay: rollupDay.Format("2006-01-02"), KgPerDay: kgPerDay, Items: items,
				Evidence: []ports.FeedWeightEvidence{},
			})
			i = len(out.Rollups) - 1
			index[key] = i
		}
		if source != "" {
			out.Rollups[i].Evidence = append(out.Rollups[i].Evidence, ports.FeedWeightEvidence{
				Source: source, Band: feedBandKeyFromIndex(band, bandEdgesKg), Animals: animals, AverageWeightKg: avgKg, FemaleCount: female, MaleCount: male,
				ExitedAnimals: exited, ExitedSold: sold, ExitedDied: died,
				AnimalsAll: animalsAll, AverageWeightKgAll: avgKgAll, FemaleCountAll: femaleAll, MaleCountAll: maleAll,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("growthdirector: feed weight band rows: %w", err)
	}
	rows.Close()

	exits := <-exitsCh
	if exits.err != nil {
		return out, exits.err
	}
	out.Exited = exits.rows
	return out, nil
}

func (r *Repository) readFeedWeightBandExits(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string) ([]ports.FeedExitedAnimal, error) {
	fwbBind600 := sqlbind.MustBind(feedWeightBandExitedSQL, tenantID, parkIDs, periodStart, periodEnd, strings.TrimSpace(sex), strings.TrimSpace(origin), weighingCategory)
	exitedRows, err := r.pool.Query(ctx, fwbBind600.SQL(), fwbBind600.Args()...)
	if err != nil {
		return nil, fmt.Errorf("growthdirector: feed weight band exits: %w", err)
	}
	defer exitedRows.Close()
	list := []ports.FeedExitedAnimal{}
	for exitedRows.Next() {
		var (
			x         ports.FeedExitedAnimal
			exitedAt  time.Time
			lastWeigh *time.Time
			weightKg  float64
			hasWeigh  bool
		)
		if err := exitedRows.Scan(&x.GoatID, &x.ParkID, &x.Tag, &x.Pen, &x.ExitReason, &x.LifecycleStatus, &exitedAt, &x.Sex, &lastWeigh, &weightKg, &hasWeigh); err != nil {
			return nil, fmt.Errorf("growthdirector: feed weight band exits scan: %w", err)
		}
		x.ExitedAt = exitedAt
		if hasWeigh && lastWeigh != nil {
			x.LastWeighedAt = lastWeigh
			x.LastWeightKg = weightKg
		}
		list = append(list, x)
	}
	if err := exitedRows.Err(); err != nil {
		return nil, fmt.Errorf("growthdirector: feed weight band exits rows: %w", err)
	}
	return list, nil
}

// feedBandKeyFromIndex turns the width_bucket index SQL emitted into the stable band key over
// the same edges; an unparseable index keeps its text so the row is visibly odd, never silently
// filed into a neighbouring bracket.
func feedBandKeyFromIndex(idxText string, edges []float64) string {
	idx, err := strconv.Atoi(idxText)
	if err != nil || idx < 0 || idx > len(edges) {
		return idxText
	}
	return weighingdomain.WeightBandKey(edges, idx)
}
