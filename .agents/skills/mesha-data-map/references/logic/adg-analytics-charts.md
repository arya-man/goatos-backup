# ADG Analytics — charts, cards and drawers not fully covered by adg-analytics.md

Route /weighing/analytics (apps/admin-web/features/weighing/weights-analytics.tsx). Read adg-analytics.md §0 for page scope, filters and the two ADG engines first; FCR tab = fcr.md. This file only adds what those cards leave formula-only or undocumented. goatos-stg, verified 2026-09-25 unless stated.

Index: C1 page header "Sheds weighed N / M · period" · C2 pens-table weight filter + pager · C3 Weight-wise feed-by-band card (tiles, matched table, Not shown table, sheet recon line) · C4 Exited-in-period drawer · C5 Time-wise controls (bucket + pen picker) · C6 Pen-wise hover list · C7 Export / Assumptions / tab loading (pointers)

## C1 Page header line "Sheds weighed N / M · from – to" (every tab)
- Endpoint: GET /weighing/shed-weights (weighing handler.go:230 -> shed_weights.go:71) summary.sheds_weighed / summary.sheds_in_scope, period_start / period_end. UI weights-analytics.tsx:586-590.
- Formula: M = non-canceled (park, location, partition) rows in scope (park + weighing filters only — sex/origin never shrink M); N = rows whose newest bucket has kids>0 under the sex/origin filters. Period = the read window (landing default 2026-08-03 → latest weighing date).
- SQL: weighing.md W14 query -> `SELECT count(*), count(*) FILTER (WHERE kids>0)` over its rows (sex=all). Verified 2026-09-25 sex=all 2026-08-03..09-24: **45 / 47**.
- Trap: on the Comparison tab the shed read is all-time (2024-01-01 → today, park only), so the header counts differ there.
- CEO: "How many pens got weighed this period?" / "Is period mein kitne pen tole gaye?"

## C2 General pens table: weight filter + pager (UI-only)
- `w_op`/`w_kg` (gt|gte|eq|lte|lt|neq; eq = ±0.05 kg) applied in the browser to rows[].average_weight_kg (weights-analytics.tsx:680-690, compareKg); rows with 0 kids hidden; offset/limit 10|25|50. No backend field; SQL = weighing.md W14 rows `WHERE avg_kg > :kg`.
- CEO: "Which pens average over 30 kg?" / "Kaunse pen ka average 30 kilo se upar hai?"

## C3 Weight-wise: Feed by weight band card
- Endpoint: GET /growth-director/feed-by-weight-band?park_id&sex&origin&weighing_category&from&to (growthdirector handler.go:47/:113 -> app/feed_weight_band.go -> postgres/feed_weight_band.go:122 feedWeightBandSQL, exits :333, domain/feed_weight_band.go:213 reconciliation). UI feed-weight-band-card.tsx (tiles :200-235), feed-weight-band-table.tsx (contract `feed-weight-band`: park, weight_source, band, pen, group, gender, breed, feed_type, feed_given, pen_kg_per_day, weight_animals, average_weight; `feed-weight-band-unmatched`: park, pen, shed_tag, ration, breed, feed_type, feed_given, pen_kg_per_day).
- Feed side (never narrowed by period or sex): latest `amended|locked` feed_direction_issues per (park, workflow) by feed_day; rows with quantity_kg>0; session duplicates SUM(qty), MAX(grams_per_head) per item; rollup per (park, pen label, shed tag, ration group, arm, breed, workflow): kg/day = Σqty, feed_given = "item Ng/head" list.
- Weight side: scanned = each animal key whose LATEST weigh is in the window AND has an earlier weigh on an earlier IST day (up to 400 days before `from`) — banded (width_bucket on growth_assumptions edges) at that latest weight, filed under that weigh's pen label; whole pen = pens weighed on >=2 IST days in window, banded on latest average with its latest head count (a pen with a pen average hides its scanned rows). Exited animals (goats.exited_at set) stay in n_all/avg_all only.
- Join: rollup pen label = weighing pen label (text, penLabelSQL: "Godel 2" + "1" -> "Godel 2 1", "Part 3" -> "Godel 1 - Part 3") within park. Matched = rollup with an evidence row with n>0 (n_all>0 under "Include exited"); Not shown = rollups with none.
- Tiles (matched view, after the card's own filters): Rows = matched rows; one tile per park; Pens = distinct park|pen; Lump rows = source pen_average; Per-animal rows; Animals weighed = Σn over distinct (park, pen, band, source) with sub "X weighed" = reconciliation individual_animals_weighed + lump_sum_animals_weighed; Exited in period (C4). Not-shown view: Rows, per park, Pens.
- Card filters (client-side, URL fb_*): fb_view matched|unmatched, fb_type feed type, fb_src weight source, fb_band, fb_pen, fb_group, fb_q search, fb_animals on-farm|all, fb_limit/fb_offset.
- SQL (the Go query with binds filled; sex=male scope resolved inline as sex_scope.go does; drop the `sx*` CTEs and set `$5` false for sex=all):
```sql
WITH sx_scoped AS (SELECT cs.campaign_shed_id, cs.location_id, COALESCE(cs.partition_label,'') partition_label, cs.weighing_category FROM weighing_campaign_sheds cs JOIN weighing_campaigns c USING (campaign_id) WHERE cs.status<>'canceled'),
sx_ident AS (SELECT DISTINCT ON (lower(btrim(identifier_value))) lower(btrim(identifier_value)) tag, goat_id FROM goat_identifiers ORDER BY 1, created_at DESC),
sx_tags AS (SELECT DISTINCT lower(btrim(o.scanned_identifier)) tag FROM weighing_observations o JOIN sx_scoped s USING (campaign_shed_id) JOIN sx_ident i ON i.tag=lower(btrim(o.scanned_identifier)) JOIN goats g ON g.goat_id=i.goat_id
  WHERE o.accepted_at >= ('2026-08-03'::date::timestamp AT TIME ZONE 'Asia/Kolkata') - interval '90 days' AND o.accepted_at < ('2026-09-23'::date::timestamp AT TIME ZONE 'Asia/Kolkata') AND o.verification_status<>'rejected' AND btrim(o.scanned_identifier)<>'' AND lower(btrim(g.sex))='male'),
sx_occ AS (SELECT DISTINCT shed_id FROM goats WHERE lifecycle_status='alive' AND shed_id IS NOT NULL),
sx_par AS (SELECT DISTINCT ON (parent_location_id, name) location_id, parent_location_id, name FROM locations WHERE location_type='shed' ORDER BY parent_location_id, name, location_id),
sx_targets AS (SELECT DISTINCT s.location_id, s.partition_label, COALESCE(CASE WHEN occ.shed_id IS NOT NULL THEN s.location_id END, phys.location_id) resolved_id,
   COALESCE(NULLIF(s.partition_label,''), NULLIF((regexp_match(loc.name, '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1],''),'') rpl
  FROM sx_scoped s LEFT JOIN locations loc ON loc.location_id=s.location_id LEFT JOIN sx_occ occ ON occ.shed_id=s.location_id
  LEFT JOIN sx_par phys ON phys.parent_location_id=loc.parent_location_id AND phys.name=regexp_replace(loc.name, '\s*(-\s*)?(Part\s*)?[0-9]+$', '')
  WHERE s.weighing_category='per_shed_partition'),
sx_buckets AS (SELECT src.location_id, src.partition_label FROM sx_targets src JOIN goats g ON g.shed_id=src.resolved_id AND g.lifecycle_status='alive'
  LEFT JOIN goat_shed_partitions gsp ON gsp.goat_id=g.goat_id
  WHERE src.rpl='' OR regexp_replace(lower(btrim(gsp.partition_label)), '^(part|pt)[\s.-]*', '')=regexp_replace(lower(btrim(src.rpl)), '^(part|pt)[\s.-]*', '')
  GROUP BY 1,2 HAVING count(DISTINCT lower(btrim(g.sex)))=1 AND min(lower(btrim(g.sex)))='male'),
sx AS (SELECT (SELECT coalesce(array_agg(tag),'{}') FROM sx_tags) tags, (SELECT coalesce(array_agg(location_id ORDER BY location_id::text, partition_label),'{}') FROM sx_buckets) locs,
  (SELECT coalesce(array_agg(partition_label ORDER BY location_id::text, partition_label),'{}') FROM sx_buckets) parts),
idm0 AS (SELECT tag, first_value(tag) OVER (PARTITION BY goat_id ORDER BY (identifier_type='animal_identifier_1') DESC, tag) ct, count(*) OVER (PARTITION BY goat_id) n
  FROM (SELECT DISTINCT ON (lower(btrim(identifier_value))) lower(btrim(identifier_value)) tag, goat_id, identifier_type FROM goat_identifiers
        WHERE status='active' AND identifier_type IN ('animal_identifier_1','animal_identifier_2') AND btrim(identifier_value)<>'' ORDER BY 1, created_at DESC) i),
idm AS (SELECT coalesce(array_agg(tag),'{}') tags, coalesce(array_agg(ct),'{}') cts FROM idm0 WHERE n>1),
latest_issue AS (
  SELECT DISTINCT ON (i.park_id, i.workflow) i.feed_direction_issue_id, i.park_id, i.workflow, i.feed_day
  FROM feed_direction_issues i
  WHERE i.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND i.park_id = ANY((SELECT array_agg(location_id) FROM locations WHERE location_type='park' AND status='active')::uuid[])
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
  WHERE r.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND r.quantity_kg > 0
),
collapsed AS (
  SELECT park_id, park_label, shed_label, partition_label, shed_tag, ration_group, experiment_arm, breed, workflow,
         feed_item_label, SUM(quantity_kg) AS quantity_kg, MAX(grams_per_head) AS grams_per_head,
         MAX(feed_day) AS feed_day
  FROM positive_rows
  GROUP BY 1,2,3,4,5,6,7,8,9,10
),
rollup AS MATERIALIZED (
  SELECT park_id, park_label, shed_tag, ration_group, experiment_arm, breed, workflow,
         CASE WHEN btrim(COALESCE(partition_label, '')) = '' OR lower(btrim(COALESCE(partition_label, ''))) = 'whole' THEN regexp_replace(btrim(shed_label), '\s+', ' ', 'g') WHEN right(regexp_replace(btrim(shed_label), '\s+', ' ', 'g'), length(btrim(COALESCE(partition_label, ''))) + 1) = ' ' || btrim(COALESCE(partition_label, '')) THEN regexp_replace(btrim(shed_label), '\s+', ' ', 'g') WHEN btrim(COALESCE(partition_label, '')) ~ '^[0-9]+$' THEN regexp_replace(btrim(shed_label), '\s+', ' ', 'g') || ' ' || btrim(COALESCE(partition_label, '')) ELSE regexp_replace(btrim(shed_label), '\s+', ' ', 'g') || ' - ' || btrim(COALESCE(partition_label, '')) END AS pen_label,
         MAX(feed_day) AS feed_day,
         SUM(quantity_kg) AS kg_per_day,
         json_agg(json_build_object('label', feed_item_label, 'grams_per_head', COALESCE(grams_per_head, 0)) ORDER BY feed_item_label) AS items
  FROM collapsed
  GROUP BY 1,2,3,4,5,6,7,8
),
scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, COALESCE(cs.partition_label, '') AS partition_label,
         cs.weighing_category, c.park_id,
         CASE WHEN btrim(COALESCE(cs.partition_label, '')) = '' OR lower(btrim(COALESCE(cs.partition_label, ''))) = 'whole' THEN regexp_replace(btrim(cs.display_name), '\s+', ' ', 'g') WHEN right(regexp_replace(btrim(cs.display_name), '\s+', ' ', 'g'), length(btrim(COALESCE(cs.partition_label, ''))) + 1) = ' ' || btrim(COALESCE(cs.partition_label, '')) THEN regexp_replace(btrim(cs.display_name), '\s+', ' ', 'g') WHEN btrim(COALESCE(cs.partition_label, '')) ~ '^[0-9]+$' THEN regexp_replace(btrim(cs.display_name), '\s+', ' ', 'g') || ' ' || btrim(COALESCE(cs.partition_label, '')) ELSE regexp_replace(btrim(cs.display_name), '\s+', ' ', 'g') || ' - ' || btrim(COALESCE(cs.partition_label, '')) END AS pen_label
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND c.park_id = ANY((SELECT array_agg(location_id) FROM locations WHERE location_type='park' AND status='active')::uuid[])
    AND cs.status <> 'canceled'
    AND ('' = '' OR cs.weighing_category = '')
),
animal_latest AS (
  SELECT DISTINCT ON (animal_key) animal_key, weight_kg, park_id, pen_label, scanned_identifier
  FROM (
    SELECT o.observation_id, o.weight_kg::float8 AS weight_kg, o.accepted_at, s.park_id, s.pen_label, o.scanned_identifier,
           COALESCE(akmap.canonical_tag, lower(btrim(o.scanned_identifier))) AS animal_key,
           LAG(o.accepted_at) OVER w AS prev_accepted_at
    FROM scoped s
    JOIN weighing_observations o ON o.tenant_id = s.tenant_id AND o.campaign_shed_id = s.campaign_shed_id
    LEFT JOIN unnest((SELECT tags FROM idm)::text[], (SELECT cts FROM idm)::text[]) AS akmap(tag, canonical_tag)
      ON akmap.tag = lower(btrim(o.scanned_identifier))
    WHERE s.weighing_category = 'individual_animal'
      AND '' <> 'per_shed_partition'
      AND o.accepted_at >= (('2026-08-03'::date::timestamp AT TIME ZONE 'Asia/Kolkata') - (400 * INTERVAL '1 day'))
      AND o.accepted_at <  ('2026-09-23'::date::timestamp AT TIME ZONE 'Asia/Kolkata')
      AND o.verification_status <> 'rejected'
      AND btrim(o.scanned_identifier) <> ''
      AND (NOT true OR lower(btrim(o.scanned_identifier)) = ANY((SELECT tags FROM sx)::text[]))
    WINDOW w AS (PARTITION BY COALESCE(akmap.canonical_tag, lower(btrim(o.scanned_identifier))) ORDER BY o.accepted_at, o.observation_id)
  ) pairs
  WHERE prev_accepted_at IS NOT NULL
    AND ((accepted_at AT TIME ZONE 'Asia/Kolkata')::date - (prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date) > 0
    AND accepted_at >= ('2026-08-03'::date::timestamp AT TIME ZONE 'Asia/Kolkata')
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
    WHERE gi.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND gi.status = 'active'
      AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
      AND lower(btrim(gi.identifier_value)) = lower(btrim(la.scanned_identifier))
    ORDER BY gi.created_at DESC
    LIMIT 1
  ) ident ON TRUE
  LEFT JOIN goats g ON g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND g.goat_id = ident.goat_id
),
lump_points AS (
  SELECT s.park_id, s.location_id, s.partition_label, s.pen_label,
         sh.shed_observation_id, sh.animal_count, sh.average_weight_kg, sh.accepted_at,
         (sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM scoped s
  JOIN weighing_shed_observations sh
    ON sh.campaign_shed_id = s.campaign_shed_id AND sh.tenant_id = s.tenant_id
   AND sh.withdrawn_at IS NULL
   AND sh.accepted_at >= ('2026-08-03'::date::timestamp AT TIME ZONE 'Asia/Kolkata')
   AND sh.accepted_at <  ('2026-09-23'::date::timestamp AT TIME ZONE 'Asia/Kolkata')
   AND sh.verification_status <> 'rejected'
  WHERE s.weighing_category = 'per_shed_partition'
    AND (NOT true OR EXISTS (
      SELECT 1 FROM unnest((SELECT locs FROM sx)::uuid[], (SELECT parts FROM sx)::text[]) AS b(loc, part)
      WHERE b.loc = s.location_id AND b.part = s.partition_label
    ))
),
pen_avg AS (
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
  WHERE q.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND q.partition_label ~ '^Part [0-9]+$'
),
pen_sex AS MATERIALIZED (
  SELECT g.park_id,
         CASE WHEN btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, '')) = '' OR lower(btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, ''))) = 'whole' THEN regexp_replace(btrim(l.name), '\s+', ' ', 'g') WHEN right(regexp_replace(btrim(l.name), '\s+', ' ', 'g'), length(btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, ''))) + 1) = ' ' || btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, '')) THEN regexp_replace(btrim(l.name), '\s+', ' ', 'g') WHEN btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, '')) ~ '^[0-9]+$' THEN regexp_replace(btrim(l.name), '\s+', ' ', 'g') || ' ' || btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, '')) ELSE regexp_replace(btrim(l.name), '\s+', ' ', 'g') || ' - ' || btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, '')) END AS pen_label,
         COUNT(*) FILTER (WHERE g.sex = 'female')::int AS female_count,
         COUNT(*) FILTER (WHERE g.sex = 'male')::int AS male_count
  FROM goats g
  JOIN locations l ON l.tenant_id = g.tenant_id AND l.location_id = g.current_location_id
  LEFT JOIN goat_shed_partitions p ON p.tenant_id = g.tenant_id AND p.goat_id = g.goat_id
  LEFT JOIN part_sheds ps ON ps.shed_id = p.shed_id
  WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND g.park_id = ANY((SELECT array_agg(location_id) FROM locations WHERE location_type='park' AND status='active')::uuid[]) AND g.exited_at IS NULL
  GROUP BY 1, 2
),
evidence AS MATERIALIZED (
  SELECT pa.park_id, pa.pen_label, 'pen_average' AS source,
         width_bucket(pa.avg_kg, '{15,20,25,30,35}'::numeric[])::text AS band,
         pa.n, pa.avg_kg, COALESCE(ps.female_count, 0) AS female_count, COALESCE(ps.male_count, 0) AS male_count,
         pa.n AS n_all, pa.avg_kg AS avg_kg_all, COALESCE(ps.female_count, 0) AS female_all, COALESCE(ps.male_count, 0) AS male_all,
         0 AS exited_n, 0 AS sold_n, 0 AS died_n
  FROM pen_avg pa
  LEFT JOIN pen_sex ps ON ps.park_id = pa.park_id AND ps.pen_label = pa.pen_label
  UNION ALL
  SELECT la.park_id, la.pen_label, 'per_animal',
         width_bucket(la.weight_kg, '{15,20,25,30,35}'::numeric[])::text AS band,
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
),
final(feed_day, positive_rows, collapsed_rows, individual_weighed, lump_weighed, park_id, park_name, pen, shed_tag, ration_group, arm, breed, workflow, rollup_day, kg_per_day, items, source, band, n, avg_kg, female, male, exited_n, sold_n, died_n, n_all, avg_all, female_all, male_all) AS (
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
LEFT JOIN locations pk ON pk.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND pk.location_id = r.park_id
LEFT JOIN evidence e ON e.park_id = r.park_id AND e.pen_label = r.pen_label
ORDER BY r.park_id, r.pen_label, r.shed_tag, r.workflow, r.ration_group, r.experiment_arm, r.breed, e.source, e.band)
SELECT max(feed_day) sheet_day, count(*) rollup_rows, count(*) FILTER (WHERE source<>'' AND n>0) matched_on_farm, count(*) FILTER (WHERE source<>'' AND n_all>0) matched_all,
 count(DISTINCT park_id||'|'||pen) FILTER (WHERE source<>'' AND n>0) pens_on_farm, max(individual_weighed) ind_weighed, max(lump_weighed) lump_weighed,
 (SELECT sum(n) FROM (SELECT DISTINCT park_id, pen, band, source, n FROM final WHERE source<>'' AND n>0) z) animals_on_farm,
 count(*) FILTER (WHERE source='pen_average' AND n>0) lump_rows, count(*) FILTER (WHERE source='per_animal' AND n>0) per_animal_rows
FROM final;
```
- Verified 2026-09-25 (male, 2026-08-03..09-22, all parks, sheet 2026-09-25, 173 rollups): matched **45** rows on farm (56 incl. exited), **21** pens, lump rows 8, per-animal rows 37, Animals weighed **463**, sub "520 weighed" = 185 individual + 335 lump (= General KPI strip). Earlier 48/488/"551 = 216+335" were read against the 2026-09-24 sheet (140 rollups); numbers move with every new locked sheet.
- Traps: feed is TODAY's sheet, weights are the period's — not a time-matched ratio (use FCR tab for that). Label-text join: a renamed pen falls into Not shown. A pen whose weighed animals all exited appears only under Include exited.
- CEO: "How much feed does each weight band get?" / "Har weight band ko kitna feed ja raha hai?"; "Which fed pens were never weighed?" / "Kin pens ko feed mil raha hai par tole nahi gaye?"

## C4 Exited-in-period drawer (tile "Exited in period", panel #fb_exit=all|pen×band)
- Endpoint: same read, reconciliation.exited_animals / exited_sold / exited_died / exited_other / exited_weighed / exited_not_weighed + exits[] (feed_weight_band.go:333 feedWeightBandExitedSQL, :623). Contract `feed-weight-band-exits`: park, tag, pen, gender, reason, exited_at, last_weighed, last_band, last_kg, feed_type, feed_given. UI feed-weight-band-exits-drawer.tsx (search, pager).
- Formula: goats with exited_at in [from, to+1) IST, merged_into_goat_id NULL, park in scope, goats.sex = filter, origin = procurement_load_goats membership. Bucket (domain FeedExitBucket): sold if lifecycle_status or exit_reason = 'sold'; died if 'dead'/'died'; else other. Last weigh = newest non-rejected scan of any of its tags in the window (pen label of that weigh), else current placement pen. Feed columns = today's rollup for that pen.
- SQL (male):
```sql
WITH exited AS (
  SELECT g.goat_id, g.park_id, g.sex, COALESCE(g.exit_reason, '') AS exit_reason, g.exited_at,
         COALESCE(g.lifecycle_status, '') AS lifecycle_status, g.current_location_id
  FROM goats g
  WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND g.park_id = ANY((SELECT array_agg(location_id) FROM locations WHERE location_type='park' AND status='active')::uuid[])
    AND g.merged_into_goat_id IS NULL
    AND g.exited_at >= ('2026-08-03'::date::timestamp AT TIME ZONE 'Asia/Kolkata') AND g.exited_at < ('2026-09-23'::date::timestamp AT TIME ZONE 'Asia/Kolkata')
    AND ('male'::text = '' OR g.sex = 'male'::text)
    AND (''::text = ''
         OR (''::text = 'purchased') = EXISTS (SELECT 1 FROM procurement_load_goats plg WHERE plg.tenant_id = g.tenant_id AND plg.goat_id = g.goat_id))
),



tags AS NOT MATERIALIZED (
  SELECT gi.goat_id, lower(btrim(gi.identifier_value)) AS tag, btrim(gi.identifier_value) AS shown, gi.identifier_type
  FROM goat_identifiers gi
  WHERE gi.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    AND btrim(gi.identifier_value) <> ''
    AND gi.goat_id IN (SELECT goat_id FROM exited)
),
primary_tag AS (
  SELECT DISTINCT ON (goat_id) goat_id, shown AS tag FROM tags ORDER BY goat_id, identifier_type
),
scoped AS (
  SELECT cs.campaign_shed_id, CASE WHEN btrim(COALESCE(cs.partition_label, '')) = '' OR lower(btrim(COALESCE(cs.partition_label, ''))) = 'whole' THEN regexp_replace(btrim(cs.display_name), '\s+', ' ', 'g') WHEN right(regexp_replace(btrim(cs.display_name), '\s+', ' ', 'g'), length(btrim(COALESCE(cs.partition_label, ''))) + 1) = ' ' || btrim(COALESCE(cs.partition_label, '')) THEN regexp_replace(btrim(cs.display_name), '\s+', ' ', 'g') WHEN btrim(COALESCE(cs.partition_label, '')) ~ '^[0-9]+$' THEN regexp_replace(btrim(cs.display_name), '\s+', ' ', 'g') || ' ' || btrim(COALESCE(cs.partition_label, '')) ELSE regexp_replace(btrim(cs.display_name), '\s+', ' ', 'g') || ' - ' || btrim(COALESCE(cs.partition_label, '')) END AS pen_label
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND c.park_id = ANY((SELECT array_agg(location_id) FROM locations WHERE location_type='park' AND status='active')::uuid[]) AND cs.status <> 'canceled'
    AND (''::text = '' OR cs.weighing_category = ''::text)
),
weighs AS (
  SELECT DISTINCT ON (t.goat_id) t.goat_id, o.scanned_identifier, o.weight_kg, o.accepted_at, s.pen_label
  FROM tags t
  JOIN weighing_observations o ON o.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND lower(btrim(o.scanned_identifier)) = t.tag
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id
  WHERE o.accepted_at >= ('2026-08-03'::date::timestamp AT TIME ZONE 'Asia/Kolkata') AND o.accepted_at < ('2026-09-23'::date::timestamp AT TIME ZONE 'Asia/Kolkata')
    AND o.verification_status <> 'rejected'
  ORDER BY t.goat_id, o.accepted_at DESC, o.observation_id DESC
),
part_sheds AS MATERIALIZED (
  SELECT DISTINCT q.shed_id
  FROM shed_partitions q
  WHERE q.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND q.partition_label ~ '^Part [0-9]+$'
),
placement AS (
  SELECT x.goat_id, CASE WHEN btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, '')) = '' OR lower(btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, ''))) = 'whole' THEN regexp_replace(btrim(l.name), '\s+', ' ', 'g') WHEN right(regexp_replace(btrim(l.name), '\s+', ' ', 'g'), length(btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, ''))) + 1) = ' ' || btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, '')) THEN regexp_replace(btrim(l.name), '\s+', ' ', 'g') WHEN btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, '')) ~ '^[0-9]+$' THEN regexp_replace(btrim(l.name), '\s+', ' ', 'g') || ' ' || btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, '')) ELSE regexp_replace(btrim(l.name), '\s+', ' ', 'g') || ' - ' || btrim(COALESCE(CASE WHEN p.partition_label ~ '^[0-9]+$' AND ps.shed_id IS NOT NULL THEN 'Part ' || p.partition_label ELSE p.partition_label END, '')) END AS pen_label
  FROM exited x
  JOIN locations l ON l.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND l.location_id = x.current_location_id
  LEFT JOIN goat_shed_partitions p ON p.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND p.goat_id = x.goat_id
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
ORDER BY x.exited_at DESC, x.goat_id
-- tile: SELECT count(*), count(*) FILTER (WHERE bucket='sold'), ... over the rows above
-- (bucket = CASE WHEN lifecycle_status='sold' OR exit_reason='sold' THEN 'sold' WHEN lifecycle_status IN ('dead','died') OR exit_reason IN ('dead','died') THEN 'died' ELSE 'other' END)
```
- Verified 2026-09-25 (query above, male 03/08–22/09): **76** exited (66 sold, 5 died, 5 other; 59 weighed in window).
- CEO: "How many males left the farm this period, sold or died?" / "Is period mein kitne male bike ya mare?"

## C5 Time-wise controls
- `tw_bucket` week (default, Monday weeks) | month (rolling 30-day blocks back from the window's last day) and `tw_pen` = location::partition from the shed read's rows (weights-analytics.tsx:204-215, :1273-1480). Both are sent to growth sections=weekly_gain and demographics sections=weekly_gain (bucket, pen_location_id, pen_partition_label); SQL = adg-analytics.md §6 with the pen predicate.
- CEO: "Show Castro 1 month by month" / "Castro 1 ka mahine-wise gain dikhao."

## C6 Pen-wise hover list (shed_type_members)
- weight-demographics sections=shed_type -> shed_type_members[] (weight_demographics.go:1230): per breed × elevated/non-elevated bar, the pens contributing and their animals. SQL = adg-analytics.md §4 `stype` joined back to lump_span/resolved_gain grouped by location. Formula-only here (PARTIAL).

## C7 Pointers
- Export drawer (header button) -> weighing.md W21. Assumptions drawer / sale-price captions -> weighing.md W22, fcr.md §6. Tab loading skeleton (weights-analytics-tab-loading.tsx) shows no figures. Comparison tab -> adg-analytics.md §7; FCR -> fcr.md.
