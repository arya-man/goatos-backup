-- ADG + average weight BY BREED = admin-web Weighing > ADG Analytics > Breed-wise tab, EXACTLY
-- (apps/admin-web/features/weighing/weights-analytics.tsx BreedTab <- GET /weighing/weight-demographics
--  sections=dimensions -> backend/internal/weighing/adapters/postgres/weight_demographics.go
--  GetWeightDemographics gain_by_breed + by_breed; identity_scope.go same-animal map;
--  origin_scope.go origin tags/buckets; sex_scope.go normalizeSexFilter). Read-only. Verified 24/09/2026
-- against the live dashboard (4-22 Sep and 3 Aug-22 Sep 2026, Male).
-- Same filters as the screen. Run: run_reference('adg-by-breed.sql', params={from_date:'2026-09-04',
-- to_date:'2026-09-22', sex:'male'}). Display rounding: gain_g_per_day whole g, avg_weight_kg 1 dp.
-- param: from_date date  first IST business date (default: 14 days before today = the backend's 15-day default window)
-- param: to_date date    last IST business date, inclusive (default: today IST)
-- param: park_code text  CBE | CPT | PARIGI (park code or name); empty/all = every active park
-- param: sex text        male | female; empty/all = both sexes
-- param: origin text     farm_born | purchased; empty/all = both
-- param: weighing text   all | individual | whole_pen (default all)
-- Statistic (app's): gain = animal-weighted mean. Each scanned animal (same-animal key: two active
-- tags of one goat merge) with >=2 weigh DAYS inside the window (last weigh of each IST day; pairs of
-- consecutive days) counts once at sum(grams)/sum(days). Each whole-pen (pen+partition) weighed on
-- >=2 days in the window counts (last avg - first avg)*1000/days once PER ANIMAL of its latest head
-- count, attributed to a breed only when every live resident of the pen is that one breed (and, under
-- a Sex filter, that one sex). Withdrawn/rejected pen weighs and rejected scans are excluded.
-- Weight = each scanned animal's latest weigh in the window + each pen's latest whole-pen weigh
-- (avg x head count), same breed/sex rule. Animals whose tag resolves to no goat have no breed.
WITH w AS (
  SELECT f, t, park_code, sexf, originf,
         CASE weighingf WHEN 'individual' THEN 'individual_animal' WHEN 'individual_animal' THEN 'individual_animal'
                        WHEN 'whole_pen' THEN 'per_shed_partition' WHEN 'per_shed_partition' THEN 'per_shed_partition'
                        ELSE '' END AS cat
  FROM (SELECT /*param:from_date*/((now() AT TIME ZONE 'Asia/Kolkata')::date - 14)/*end*/::date AS f,
               /*param:to_date*/(now() AT TIME ZONE 'Asia/Kolkata')::date/*end*/::date AS t,
               lower(btrim(/*param:park_code*/''/*end*/::text)) AS park_code,
               CASE lower(btrim(/*param:sex*/''/*end*/::text)) WHEN 'male' THEN 'male' WHEN 'female' THEN 'female' ELSE '' END AS sexf,
               CASE replace(lower(btrim(/*param:origin*/''/*end*/::text)), ' ', '_') WHEN 'farm_born' THEN 'farm_born'
                    WHEN 'born' THEN 'farm_born' WHEN 'purchased' THEN 'purchased' WHEN 'bought' THEN 'purchased' ELSE '' END AS originf,
               replace(lower(btrim(/*param:weighing*/'all'/*end*/::text)), '-', '_') AS weighingf) p
),
b AS (SELECT (w.f::timestamp AT TIME ZONE 'Asia/Kolkata') AS s, ((w.t + 1)::timestamp AT TIME ZONE 'Asia/Kolkata') AS e,
             w.sexf, w.originf, w.originf <> '' AS origin_on, w.cat FROM w),
parks AS (SELECT pk.location_id, pk.tenant_id FROM locations pk, w
  WHERE pk.location_type = 'park' AND pk.status = 'active' AND pk.retired_at IS NULL
    AND (w.park_code IN ('', 'all') OR lower(pk.location_code) = w.park_code OR lower(pk.name) = w.park_code)),
-- identity_scope.go: tags of the same goat weighed in [start-90d, end) collapse to one canonical tag.
id_scoped AS (SELECT cs.campaign_shed_id FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE c.park_id IN (SELECT location_id FROM parks) AND cs.status <> 'canceled'),
id_weighed AS (SELECT DISTINCT lower(btrim(o.scanned_identifier)) AS tag FROM weighing_observations o, b
  WHERE o.campaign_shed_id IN (SELECT campaign_shed_id FROM id_scoped)
    AND o.accepted_at >= b.s - interval '90 days' AND o.accepted_at < b.e
    AND o.verification_status <> 'rejected' AND btrim(o.scanned_identifier) <> ''),
id_ident AS (SELECT DISTINCT ON (lower(btrim(gi.identifier_value))) lower(btrim(gi.identifier_value)) AS tag, gi.goat_id, gi.identifier_type
  FROM goat_identifiers gi WHERE gi.status = 'active' AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    AND btrim(gi.identifier_value) <> '' AND gi.tenant_id IN (SELECT tenant_id FROM parks)
  ORDER BY lower(btrim(gi.identifier_value)), gi.created_at DESC),
akmap AS (SELECT tag, canonical_tag FROM (
  SELECT i.tag, first_value(i.tag) OVER (PARTITION BY i.goat_id ORDER BY (i.identifier_type = 'animal_identifier_1') DESC, i.tag) AS canonical_tag,
         count(*) OVER (PARTITION BY i.goat_id) AS n
  FROM id_ident i WHERE i.goat_id IN (SELECT i2.goat_id FROM id_ident i2 JOIN id_weighed USING (tag))) x WHERE n > 1),
-- herd register lookup (weight_demographics.go ident: any status, newest row per tag)
ident AS (SELECT DISTINCT ON (lower(btrim(gi.identifier_value))) lower(btrim(gi.identifier_value)) AS tag, gi.goat_id
  FROM goat_identifiers gi WHERE gi.tenant_id IN (SELECT tenant_id FROM parks)
  ORDER BY lower(btrim(gi.identifier_value)), gi.created_at DESC),
-- origin_scope.go (only consulted when an origin filter is set)
bought AS (SELECT DISTINCT goat_id FROM procurement_load_goats WHERE tenant_id IN (SELECT tenant_id FROM parks)),
origin_tags AS (SELECT wd.tag FROM id_weighed wd JOIN ident i ON i.tag = wd.tag, b
  WHERE b.origin_on AND (EXISTS (SELECT 1 FROM bought x WHERE x.goat_id = i.goat_id)) = (b.originf = 'purchased')),
scoped AS (SELECT cs.campaign_shed_id, cs.tenant_id, cs.location_id, c.park_id, COALESCE(cs.partition_label, '') AS partition_label, cs.weighing_category
  FROM weighing_campaign_sheds cs JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id, b
  WHERE c.park_id IN (SELECT location_id FROM parks) AND cs.status <> 'canceled'
    AND (b.cat = '' OR cs.weighing_category = b.cat)),
o_occupied AS (SELECT DISTINCT gg.shed_id FROM goats gg WHERE gg.tenant_id IN (SELECT tenant_id FROM parks) AND gg.lifecycle_status = 'alive' AND gg.shed_id IS NOT NULL),
o_parent_sheds AS (SELECT DISTINCT ON (ps.parent_location_id, ps.name) ps.location_id, ps.parent_location_id, ps.name
  FROM locations ps WHERE ps.tenant_id IN (SELECT tenant_id FROM parks) AND ps.location_type = 'shed'
  ORDER BY ps.parent_location_id, ps.name, ps.location_id),
o_targets AS (SELECT DISTINCT s.location_id, s.partition_label,
         COALESCE(CASE WHEN occ.shed_id IS NOT NULL THEN s.location_id END, phys.location_id) AS resolved_id,
         COALESCE(NULLIF(s.partition_label, ''), NULLIF((regexp_match(s.location_name, '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1], ''), '') AS resolved_partition_label
  FROM (SELECT DISTINCT cs.location_id, COALESCE(cs.partition_label, '') AS partition_label, loc.tenant_id AS location_tenant_id,
               loc.parent_location_id, loc.name AS location_name, regexp_replace(loc.name, '\s*(-\s*)?(Part\s*)?[0-9]+$', '') AS parent_name
        FROM weighing_campaign_sheds cs JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
        LEFT JOIN locations loc ON loc.location_id = cs.location_id, b
        WHERE b.origin_on AND c.park_id IN (SELECT location_id FROM parks) AND cs.status <> 'canceled'
          AND cs.weighing_category = 'per_shed_partition') s
  LEFT JOIN o_occupied occ ON occ.shed_id = s.location_id
  LEFT JOIN o_parent_sheds phys ON s.location_tenant_id IN (SELECT tenant_id FROM parks)
   AND phys.parent_location_id = s.parent_location_id AND phys.name = s.parent_name),
origin_buckets AS (SELECT src.location_id, src.partition_label
  FROM o_targets src JOIN goats g ON g.shed_id = src.resolved_id AND g.lifecycle_status = 'alive'
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id, b
  WHERE src.resolved_partition_label = ''
     OR regexp_replace(lower(btrim(gsp.partition_label)), '^(part|pt)[\s.-]*', '')
        = regexp_replace(lower(btrim(src.resolved_partition_label)), '^(part|pt)[\s.-]*', '')
  GROUP BY src.location_id, src.partition_label, b.originf
  HAVING count(*) > 0 AND CASE WHEN b.originf = 'purchased'
      THEN count(*) FILTER (WHERE EXISTS (SELECT 1 FROM bought x WHERE x.goat_id = g.goat_id)) = count(*)
      ELSE count(*) FILTER (WHERE EXISTS (SELECT 1 FROM bought x WHERE x.goat_id = g.goat_id)) = 0 END),
-- scanned (individual) arm
latest AS (SELECT DISTINCT ON (COALESCE(ak.canonical_tag, lower(btrim(o.scanned_identifier))))
         COALESCE(ak.canonical_tag, lower(btrim(o.scanned_identifier))) AS tag, o.weight_kg
  FROM weighing_observations o JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id AND s.tenant_id = o.tenant_id
  LEFT JOIN akmap ak ON ak.tag = lower(btrim(o.scanned_identifier)), b
  WHERE o.accepted_at >= b.s AND o.accepted_at < b.e AND o.verification_status <> 'rejected'
    AND btrim(o.scanned_identifier) <> '' AND b.cat IN ('', 'individual_animal')
    AND (NOT b.origin_on OR lower(btrim(o.scanned_identifier)) IN (SELECT tag FROM origin_tags))
  ORDER BY COALESCE(ak.canonical_tag, lower(btrim(o.scanned_identifier))), o.accepted_at DESC, o.observation_id DESC),
raw_obs AS (SELECT COALESCE(ak.canonical_tag, lower(btrim(o.scanned_identifier))) AS tag, o.observation_id, o.weight_kg, o.accepted_at,
         (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM weighing_observations o JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id AND s.tenant_id = o.tenant_id
  LEFT JOIN akmap ak ON ak.tag = lower(btrim(o.scanned_identifier)), b
  WHERE o.accepted_at >= b.s AND o.accepted_at < b.e AND o.verification_status <> 'rejected'
    AND btrim(o.scanned_identifier) <> '' AND b.cat IN ('', 'individual_animal')
    AND (NOT b.origin_on OR lower(btrim(o.scanned_identifier)) IN (SELECT tag FROM origin_tags))),
obs AS (SELECT DISTINCT ON (tag, d) tag, weight_kg, accepted_at, d FROM raw_obs ORDER BY tag, d, accepted_at DESC, observation_id DESC),
paired AS (SELECT tag, weight_kg, accepted_at, d, lag(weight_kg) OVER (PARTITION BY tag ORDER BY d) AS prev_w,
         lag(d) OVER (PARTITION BY tag ORDER BY d) AS prev_d FROM obs),
animal_gain AS (SELECT tag, sum((weight_kg - prev_w) * 1000.0)::float8 / NULLIF(sum(d - prev_d), 0) AS g
  FROM paired WHERE prev_d IS NOT NULL AND d > prev_d GROUP BY tag),
resolved AS (SELECT l.weight_kg, g.breed FROM latest l LEFT JOIN ident i ON i.tag = l.tag
  LEFT JOIN goats g ON g.goat_id = i.goat_id, b WHERE b.sexf = '' OR lower(btrim(g.sex)) = b.sexf),
resolved_gain AS (SELECT ag.g, gt.breed FROM animal_gain ag LEFT JOIN ident i ON i.tag = ag.tag
  LEFT JOIN goats gt ON gt.goat_id = i.goat_id, b WHERE b.sexf = '' OR lower(btrim(gt.sex)) = b.sexf),
-- whole-pen arm: a pen resolves to its own live residents, else its parent shed + partition
shed_targets AS (SELECT DISTINCT s.location_id, s.partition_label,
         COALESCE(CASE WHEN live.present THEN s.location_id END, -- operational-location:ignore: owner=ravi issue=PR396-adg-by-breed scope=mirrors-weight_demographics.go-cohort-key-to-match-dashboard expiry=2026-11-30
           (SELECT phys.location_id FROM locations phys -- operational-location:ignore: owner=ravi issue=PR396-adg-by-breed scope=mirrors-weight_demographics.go-cohort-key-to-match-dashboard expiry=2026-11-30
            WHERE l.tenant_id = s.tenant_id AND phys.tenant_id = l.tenant_id AND phys.parent_location_id = l.parent_location_id -- operational-location:ignore: owner=ravi issue=PR396-adg-by-breed scope=mirrors-weight_demographics.go-cohort-key-to-match-dashboard expiry=2026-11-30
              AND phys.location_type = 'shed' AND phys.name = regexp_replace(l.name, '\s*(-\s*)?(Part\s*)?[0-9]+$', '') -- operational-location:ignore: owner=ravi issue=PR396-adg-by-breed scope=mirrors-weight_demographics.go-cohort-key-to-match-dashboard expiry=2026-11-30
              AND EXISTS (SELECT 1 FROM goat_shed_partitions gsp WHERE gsp.tenant_id = l.tenant_id AND gsp.shed_id = phys.location_id
                AND regexp_replace(lower(btrim(gsp.partition_label)), '^(part|pt)[\s.-]*', '')
                    = regexp_replace(lower(btrim(COALESCE(NULLIF(s.partition_label, ''),
                        NULLIF((regexp_match(l.name, '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1], '')))), '^(part|pt)[\s.-]*', '')) -- operational-location:ignore: owner=ravi issue=PR396-adg-by-breed scope=mirrors-weight_demographics.go-cohort-key-to-match-dashboard expiry=2026-11-30
            LIMIT 1)) AS resolved_id, -- operational-location:ignore: owner=ravi issue=PR396-adg-by-breed scope=mirrors-weight_demographics.go-cohort-key-to-match-dashboard expiry=2026-11-30
         COALESCE(NULLIF(s.partition_label, ''), CASE WHEN live.present THEN '' ELSE -- operational-location:ignore: owner=ravi issue=PR396-adg-by-breed scope=mirrors-weight_demographics.go-cohort-key-to-match-dashboard expiry=2026-11-30
                  NULLIF((regexp_match(l.name, '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1], '') END, '') AS resolved_partition_label
  FROM (SELECT DISTINCT tenant_id, location_id, partition_label FROM scoped) s
  LEFT JOIN locations l ON l.location_id = s.location_id
  LEFT JOIN LATERAL (SELECT true AS present FROM goats gg WHERE gg.tenant_id = s.tenant_id
                     AND gg.lifecycle_status = 'alive' AND gg.shed_id = s.location_id LIMIT 1) live ON true, b
  WHERE NOT b.origin_on OR EXISTS (SELECT 1 FROM origin_buckets ob WHERE ob.location_id = s.location_id AND ob.partition_label = s.partition_label)), -- operational-location:ignore: owner=ravi issue=PR396-adg-by-breed scope=mirrors-weight_demographics.go-cohort-key-to-match-dashboard expiry=2026-11-30
shed_cohort AS (SELECT src.location_id, src.partition_label, min(g.breed) AS breed, min(g.sex) AS sex,
         count(DISTINCT g.breed) AS breeds, count(DISTINCT g.sex) AS sexes
  FROM shed_targets src JOIN goats g ON g.shed_id = src.resolved_id AND g.lifecycle_status = 'alive'
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  WHERE src.resolved_partition_label = ''
     OR regexp_replace(lower(btrim(gsp.partition_label)), '^(part|pt)[\s.-]*', '')
        = regexp_replace(lower(btrim(src.resolved_partition_label)), '^(part|pt)[\s.-]*', '')
  GROUP BY src.location_id, src.partition_label),
claim AS (SELECT sc.location_id, sc.partition_label, sc.breed FROM shed_cohort sc, b
  WHERE sc.breeds = 1 AND (b.sexf = '' OR (sc.sexes = 1 AND lower(btrim(sc.sex)) = b.sexf))),
pen_obs AS (SELECT s.location_id, s.partition_label, so.shed_observation_id, so.accepted_at, so.average_weight_kg, so.animal_count,
         (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS d
  FROM weighing_shed_observations so JOIN scoped s ON s.campaign_shed_id = so.campaign_shed_id AND s.tenant_id = so.tenant_id, b
  WHERE s.weighing_category = 'per_shed_partition' AND so.withdrawn_at IS NULL AND so.verification_status <> 'rejected'
    AND so.accepted_at >= b.s AND so.accepted_at < b.e
    AND (NOT b.origin_on OR EXISTS (SELECT 1 FROM origin_buckets ob WHERE ob.location_id = s.location_id AND ob.partition_label = s.partition_label))),
lump AS (SELECT DISTINCT ON (location_id, partition_label) location_id, partition_label, animal_count, average_weight_kg
  FROM pen_obs ORDER BY location_id, partition_label, accepted_at DESC, shed_observation_id DESC),
pen_ranked AS (SELECT *, row_number() OVER (PARTITION BY location_id, partition_label ORDER BY accepted_at DESC) AS z,
                         row_number() OVER (PARTITION BY location_id, partition_label ORDER BY accepted_at ASC) AS a FROM pen_obs),
lump_span AS (SELECT l.location_id, l.partition_label, (l.average_weight_kg - f.average_weight_kg) * 1000.0 / NULLIF(l.d - f.d, 0) AS g_per_day,
         l.animal_count AS animals
  FROM pen_ranked l JOIN pen_ranked f ON f.location_id = l.location_id AND f.partition_label = l.partition_label AND f.a = 1
  WHERE l.z = 1 AND l.d > f.d),
gain_by_breed AS (SELECT breed, sum(n)::bigint AS n, sum(gsum) / NULLIF(sum(n), 0) AS g FROM (
    SELECT breed, count(*)::float8 AS n, sum(g)::float8 AS gsum FROM resolved_gain WHERE breed IS NOT NULL GROUP BY breed
    UNION ALL
    SELECT c.breed, sum(ls.animals)::float8, sum(ls.animals * ls.g_per_day)::float8
    FROM lump_span ls JOIN claim c ON c.location_id = ls.location_id AND c.partition_label = ls.partition_label GROUP BY c.breed) x
  GROUP BY breed),
weight_by_breed AS (SELECT breed, sum(n)::bigint AS n, sum(total) / NULLIF(sum(n), 0) AS avg FROM (
    SELECT breed, count(*)::float8 AS n, sum(weight_kg)::float8 AS total FROM resolved WHERE breed IS NOT NULL GROUP BY breed
    UNION ALL
    SELECT c.breed, sum(l.animal_count)::float8, sum(l.animal_count * l.average_weight_kg)::float8
    FROM lump l JOIN claim c ON c.location_id = l.location_id AND c.partition_label = l.partition_label GROUP BY c.breed) x
  GROUP BY breed),
-- exact binary value of each float8 (float8send bits), so rounding matches the screen's JS exactly:
-- Math.round(gain) = floor(x + 0.5); weight.toFixed(1) rounds the exact double (19.65 -> 19.6).
ex AS (SELECT COALESCE(gb.breed, wb.breed) AS breed, gb.n AS gn, wb.n AS wn, gbits, wbits
  FROM gain_by_breed gb FULL JOIN weight_by_breed wb ON wb.breed = gb.breed
  CROSS JOIN LATERAL (SELECT ('x' || encode(float8send(gb.g), 'hex'))::bit(64)::bigint AS gbits,
                             ('x' || encode(float8send(wb.avg), 'hex'))::bit(64)::bigint AS wbits) z),
exn AS (SELECT breed, gn, wn,
         (CASE WHEN gbits < 0 THEN -1 ELSE 1 END) * ((gbits & 4503599627370495) | 4503599627370496)::numeric
           / (2::numeric ^ (1075 - ((gbits >> 52) & 2047))) AS g,
         ((wbits & 4503599627370495) | 4503599627370496)::numeric / (2::numeric ^ (1075 - ((wbits >> 52) & 2047))) AS avg
  FROM ex)
SELECT breed,
       floor(g + 0.5)::int AS gain_g_per_day, gn AS gain_animals,
       round(avg, 1) AS avg_weight_kg, wn AS weight_animals,
       (SELECT f FROM w) AS date_from, (SELECT t FROM w) AS date_to,
       (SELECT concat_ws(' / ', 'park=' || COALESCE(NULLIF(park_code, ''), 'all'), 'sex=' || COALESCE(NULLIF(sexf, ''), 'all'),
               'origin=' || COALESCE(NULLIF(originf, ''), 'all'), 'weighing=' || COALESCE(NULLIF(cat, ''), 'all')) FROM w) AS filters
FROM exn
ORDER BY 1
