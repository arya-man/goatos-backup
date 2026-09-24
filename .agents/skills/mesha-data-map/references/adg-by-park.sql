-- ADG / daily gain by park = Weighing > Growth (ADG) park cards + headline
-- (backend/internal/weighing/adapters/postgres/growth.go growthParkGainsQuery / growthHeadlineStats,
--  growthPairsCTE, identity_scope.go same-animal map). No sex/origin filter, all weighing categories.
-- Window = IST business dates [:from, :to] inclusive. Default below: this calendar month to date.
-- To change it: run_reference('adg-by-park.sql', params={from_date:'2026-08-01', to_date:'2026-08-31'}) (psql: edit the two
-- /*param*/ expressions in `w`). Run as-is (read-only). Verified 24/09/2026.
-- param: from_date date  first IST business date (default: 1st of this month)
-- param: to_date date    last IST business date, inclusive (default: today)
-- Statistic: animal-weighted mean. Each individually scanned kid with >=2 weighs INSIDE the window
-- counts once at sum(grams moved)/sum(days) over its consecutive pairs (same-IST-day pairs dropped);
-- each whole-pen (pen+partition) weighed on >=2 days in the window counts (last avg - first avg)*1000/days
-- once PER ANIMAL of its latest head count. kids = that denominator. Do not re-weight or average parks.
WITH w AS (SELECT /*param:from_date*/date_trunc('month', (now() AT TIME ZONE 'Asia/Kolkata'))::date/*end*/ AS f,
                  /*param:to_date*/(now() AT TIME ZONE 'Asia/Kolkata')::date/*end*/ AS t),
b AS (SELECT (w.f::timestamp AT TIME ZONE 'Asia/Kolkata') s, ((w.t + 1)::timestamp AT TIME ZONE 'Asia/Kolkata') e FROM w),
weighed AS (SELECT DISTINCT lower(btrim(o.scanned_identifier)) tag
  FROM weighing_observations o JOIN weighing_campaign_sheds cs USING (campaign_shed_id)
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id, b
  WHERE cs.status <> 'canceled' AND o.accepted_at >= b.s AND o.accepted_at < b.e
    AND o.verification_status <> 'rejected' AND btrim(o.scanned_identifier) <> ''),
ident AS (SELECT DISTINCT ON (lower(btrim(gi.identifier_value))) lower(btrim(gi.identifier_value)) tag, gi.goat_id, gi.identifier_type
  FROM goat_identifiers gi WHERE gi.status = 'active' AND gi.identifier_type IN ('animal_identifier_1','animal_identifier_2')
    AND btrim(gi.identifier_value) <> '' ORDER BY lower(btrim(gi.identifier_value)), gi.created_at DESC),
akmap AS (SELECT tag, canonical_tag FROM (
  SELECT i.tag, first_value(i.tag) OVER (PARTITION BY i.goat_id ORDER BY (i.identifier_type='animal_identifier_1') DESC, i.tag) canonical_tag,
         count(*) OVER (PARTITION BY i.goat_id) n
  FROM ident i WHERE i.goat_id IN (SELECT i2.goat_id FROM ident i2 JOIN weighed USING (tag))) x WHERE n > 1),
base AS (SELECT wo.observation_id, wo.weight_kg::float8 weight_kg, wo.accepted_at, wc.park_id,
         coalesce(akmap.canonical_tag, lower(btrim(wo.scanned_identifier))) animal_key
  FROM weighing_observations wo JOIN weighing_campaign_sheds wcs USING (campaign_shed_id)
  JOIN weighing_campaigns wc ON wc.campaign_id = wcs.campaign_id
  LEFT JOIN akmap ON akmap.tag = lower(btrim(wo.scanned_identifier)), b
  WHERE wo.verification_status <> 'rejected' AND wo.accepted_at >= b.s AND wo.accepted_at < b.e),
pairs AS (SELECT park_id, animal_key, weight_kg - lag(weight_kg) OVER a dkg,
         (accepted_at AT TIME ZONE 'Asia/Kolkata')::date - (lag(accepted_at) OVER a AT TIME ZONE 'Asia/Kolkata')::date days
  FROM base WINDOW a AS (PARTITION BY animal_key ORDER BY accepted_at, observation_id)),
animal_gain AS (SELECT park_id, animal_key, sum(dkg*1000.0)/nullif(sum(days),0) g
  FROM pairs WHERE days > 0 GROUP BY park_id, animal_key),
so AS (SELECT c.park_id, cs.location_id, coalesce(cs.partition_label,'') part, o.average_weight_kg::float8 avg_kg, o.animal_count,
         (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d,
         row_number() OVER (PARTITION BY cs.location_id, coalesce(cs.partition_label,'') ORDER BY o.accepted_at DESC) z,
         row_number() OVER (PARTITION BY cs.location_id, coalesce(cs.partition_label,'') ORDER BY o.accepted_at ASC) a
  FROM weighing_shed_observations o JOIN weighing_campaign_sheds cs USING (campaign_shed_id)
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id, b
  WHERE o.withdrawn_at IS NULL AND o.verification_status <> 'rejected' AND o.accepted_at >= b.s AND o.accepted_at < b.e),
shed_span AS (SELECT l.park_id, l.animal_count::float8 animals, (l.avg_kg - f.avg_kg)*1000.0/(l.d - f.d) g
  FROM so l JOIN so f ON f.location_id = l.location_id AND f.part = l.part AND f.a = 1
  WHERE l.z = 1 AND l.d > f.d),
contrib AS (SELECT park_id, g wg, 1::float8 n FROM animal_gain WHERE g IS NOT NULL
  UNION ALL SELECT park_id, animals*g, animals FROM shed_span)
SELECT coalesce(coalesce(nullif(pk.location_code,''), pk.name), 'ALL') park,
       round((sum(wg)/nullif(sum(n),0))::numeric) adg_g_per_day, sum(n)::bigint kids,
       (SELECT f FROM w) date_from, (SELECT t FROM w) date_to
FROM contrib JOIN locations pk ON pk.location_id = contrib.park_id
GROUP BY ROLLUP (coalesce(nullif(pk.location_code,''), pk.name)) ORDER BY 1;
