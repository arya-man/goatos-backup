package postgres

// The FCR statements AS THEY WERE before the feed-day rollup (migration 000402): every feed cell
// re-derived from feed_direction_issue_rows and priced per request. Kept verbatim, test-only, as
// the equivalence oracle -- the rollup path must return the same pens and segments (float
// tolerance) for any window, park set and weighing category.

var fcrLiveScopeCTEs = `
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
-- Only the sheds the scoped buckets resolved to, and the key from the sheet's own GENERATED
-- partition_key ('part 3' / '3' / 'whole') rather than a regexp over every row's label: the same
-- scrub result at a fraction of the cost on a 50k-row sheet.
feed_rows AS (
  SELECT r.shed_id AS pen_shed_id,
         CASE WHEN r.partition_key = 'whole' THEN ''
              WHEN r.partition_key LIKE 'part %' THEN btrim(substr(r.partition_key, 6))
              WHEN r.partition_key LIKE 'pt %' THEN btrim(substr(r.partition_key, 4))
              ELSE r.partition_key END AS pen_key,
         COALESCE(NULLIF(r.partition_label, 'whole'), '') AS partition_label,
         i.feed_day, i.park_id, r.feed_item_key, r.quantity_kg, r.head_count, r.shed_tag_key, r.breed_key
  FROM feed_direction_issue_rows r
  JOIN feed_direction_issues i
    ON i.tenant_id = r.tenant_id AND i.feed_direction_issue_id = r.feed_direction_issue_id
  WHERE r.tenant_id = $1::uuid
    AND i.park_id = ANY($2::uuid[])
    AND i.feed_day >= $8::date AND i.feed_day < $9::date
    AND i.state IN ('issued','amended','locked')
    AND r.shed_id IN (SELECT pen_shed_id FROM pen_map)
)`

var (
	fcrPensLiveSQL = `WITH ` + fcrLiveScopeCTEs + `,
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
         sum(fr.quantity_kg) FILTER (WHERE fr.quantity_kg IS NOT NULL) AS feed_kg,
         count(fr.feed_day) FILTER (WHERE fr.quantity_kg IS NULL)::int AS blocked_cells,
         min(fr.partition_label) FILTER (WHERE fr.partition_label <> '') AS feed_label
  FROM pens p
  LEFT JOIN feed_rows fr ON fr.pen_shed_id = p.pen_shed_id AND fr.pen_key = p.pen_key
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
	fcrSegmentsLiveSQL = `WITH ` + fcrLiveScopeCTEs + `,
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
-- Same-farm latest load on or before the feed day: the price rule Feed analytics expenditure
-- already applies, so Feed and Weighing can never disagree about what a day of feed cost.
feed_price AS (
  SELECT fr.park_id, fr.feed_item_key, fr.feed_day, price.per_kg
  FROM (SELECT DISTINCT park_id, feed_item_key, feed_day FROM feed_rows) fr
  LEFT JOIN LATERAL (
    SELECT COALESCE(p.per_kg_cost, p.total_cost / NULLIF(p.quantity_kg, 0))::float8 AS per_kg
    FROM feed_purchases p
    WHERE p.tenant_id = $1::uuid AND p.park_id = fr.park_id
      AND p.feed_item_key = fr.feed_item_key
      AND p.purchase_date <= fr.feed_day
    ORDER BY p.purchase_date DESC, p.batch_no DESC
    LIMIT 1
  ) price ON true
),
-- THE PRICE IS ATTACHED TO THE FEED ROW BEFORE THE SEGMENT JOIN, not after it. feed_price is 1:1
-- on (park_id, feed_item_key, feed_day) -- it is built from the DISTINCT of exactly that key -- so
-- this LEFT JOIN neither multiplies nor drops a feed row, and seg_feed sees the same rows with the
-- same per_kg it saw when the price was joined downstream. The order is the fix: joined after
-- segments (a UNION ALL of window aggregates the planner estimates at ~2 rows), the price join
-- ran as a NESTED LOOP of every in-segment feed row against every price row -- 16,922 x 649 =
-- 11M join-filter comparisons, 1.7s of a 1.9s statement on the 2026-09-24 OCI clone, and the
-- /growth-director/fcr 15s timeouts under a dashboard reload burst. Here both inputs carry real
-- estimates (the feed sheet and its distinct keys), so the planner hashes them.
-- projection-review: membership=every feed_rows row, unchanged; group_key=none here (seg_feed
-- groups by the segment key as before); join_cardinality=feed_price is unique on (park_id,
-- feed_item_key, feed_day) -- the DISTINCT of that key with a LIMIT 1 lateral -- so the LEFT JOIN is
-- 1:0..1 per feed row; pagination=NONE, one bounded window read; scope=tenant + park ANY, the price
-- joins on the feed row's own park_id so a load never prices another park's feed.
priced_feed_rows AS MATERIALIZED (
  SELECT fr.pen_shed_id, fr.pen_key, fr.feed_day, fr.quantity_kg, fp.per_kg
  FROM feed_rows fr
  LEFT JOIN feed_price fp
    ON fp.park_id = fr.park_id AND fp.feed_item_key = fr.feed_item_key AND fp.feed_day = fr.feed_day
),
seg_feed AS (
  SELECT sg.pen_shed_id, sg.pen_key, sg.d_prev, sg.d,
         -- NEVER COALESCE quantity_kg: NULL is a blocked cell and stays out of the sum.
         sum(fr.quantity_kg) FILTER (WHERE fr.quantity_kg IS NOT NULL)::float8 AS feed_kg,
         sum(fr.quantity_kg * fr.per_kg) FILTER (WHERE fr.quantity_kg IS NOT NULL AND fr.per_kg IS NOT NULL)::float8 AS feed_cost,
         COALESCE(sum(fr.quantity_kg) FILTER (WHERE fr.quantity_kg IS NOT NULL AND fr.per_kg IS NULL), 0)::float8 AS unpriced_kg,
         count(*) FILTER (WHERE fr.quantity_kg IS NULL AND fr.feed_day IS NOT NULL)::int AS blocked_cells
  FROM segments sg
  LEFT JOIN priced_feed_rows fr
    ON fr.pen_shed_id = sg.pen_shed_id AND fr.pen_key = sg.pen_key
   AND fr.feed_day >= sg.d_prev AND fr.feed_day < sg.d
  GROUP BY sg.pen_shed_id, sg.pen_key, sg.d_prev, sg.d
),
seg_heads AS (
  SELECT sg.pen_shed_id, sg.pen_key, sg.d_prev, sg.d, sum(g.heads)::float8 AS head_days
  FROM segments sg
  JOIN (
    SELECT pen_shed_id, pen_key, feed_day, shed_tag_key, breed_key, max(head_count) AS heads
    FROM feed_rows
    WHERE head_count IS NOT NULL
    GROUP BY pen_shed_id, pen_key, feed_day, shed_tag_key, breed_key
  ) g ON g.pen_shed_id = sg.pen_shed_id AND g.pen_key = sg.pen_key
     AND g.feed_day >= sg.d_prev AND g.feed_day < sg.d
  GROUP BY sg.pen_shed_id, sg.pen_key, sg.d_prev, sg.d
)
SELECT sg.pen_shed_id::text, sg.pen_key, sg.d_prev::text, sg.d::text, sg.animals, sg.adg_g, sg.mode, sg.paired,
       f.feed_kg, f.feed_cost, COALESCE(f.unpriced_kg, 0), COALESCE(f.blocked_cells, 0), h.head_days
FROM segments sg
LEFT JOIN seg_feed  f ON f.pen_shed_id = sg.pen_shed_id AND f.pen_key = sg.pen_key AND f.d_prev = sg.d_prev AND f.d = sg.d
LEFT JOIN seg_heads h ON h.pen_shed_id = sg.pen_shed_id AND h.pen_key = sg.pen_key AND h.d_prev = sg.d_prev AND h.d = sg.d
ORDER BY sg.pen_shed_id, sg.pen_key, sg.d`
)
