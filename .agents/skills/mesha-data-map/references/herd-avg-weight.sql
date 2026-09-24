-- Average herd weight now (no single app tile; this is the agreed definition). Per park + species.
-- Arm 1 "weighed individually": latest non-rejected individual weigh per ALIVE animal (weight_kg is the corrected,
--   authoritative value; operator_weight_kg is the raw one). Tags resolve to animals via goat_identifiers (either RFID).
-- Arm 2 "pen-level": animals never weighed individually but in a pen part whose latest whole-pen weigh
--   (non-withdrawn, non-rejected) exists -> that pen average applies to them.
-- Answer: combined avg per park (+species), how many animals each arm covers, date range of weighs, 1 method line.
WITH today AS (SELECT (now() AT TIME ZONE 'Asia/Kolkata')::date d),
ident AS (SELECT DISTINCT ON (lower(btrim(identifier_value))) lower(btrim(identifier_value)) tag, goat_id
  FROM goat_identifiers WHERE status='active' AND identifier_type IN ('animal_identifier_1','animal_identifier_2') AND btrim(identifier_value)<>''
  ORDER BY lower(btrim(identifier_value)), created_at DESC),
alive AS (SELECT a.animal_id goat_id, a.park_label, a.species, a.shed_label,
    regexp_replace(regexp_replace(lower(btrim(coalesce(a.partition_label,''))),'^(part|pt)[\s\-]*',''),'[^a-z0-9]','','g') part
  FROM ceo_ai.animal_current_scope a WHERE a.lifecycle_status='alive'),
ind AS (SELECT DISTINCT ON (i.goat_id) i.goat_id, o.weight_kg, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date wd
  FROM weighing_observations o JOIN ident i ON i.tag = lower(btrim(o.scanned_identifier))
  WHERE o.verification_status <> 'rejected' AND o.weight_kg > 0
  ORDER BY i.goat_id, o.accepted_at DESC),
-- whole-pen weighs: legacy alias rows "Godel 1 - Part 3" / "Castro 1" resolve to parent shed "Godel 1" / "Castro" + part "3" / "1" (fcr.go pen_map)
bucket AS (SELECT s.accepted_at, s.average_weight_kg, pk.name park_label, wc.park_id,
         coalesce(parent.name, l.name) shed_label,
         regexp_replace(regexp_replace(lower(btrim(coalesce(nullif(wcs.partition_label,''),
           CASE WHEN parent.name IS NOT NULL THEN regexp_replace(btrim(substr(l.name, length(parent.name)+1)), '^[-\s]+', '') END, ''))),
           '^(part|pt)[\s\-]*',''),'[^a-z0-9]','','g') part
  FROM weighing_shed_observations s JOIN weighing_campaign_sheds wcs ON wcs.campaign_shed_id = s.campaign_shed_id
  JOIN weighing_campaigns wc ON wc.campaign_id = s.campaign_id
  JOIN locations l ON l.location_id = wcs.location_id JOIN locations pk ON pk.location_id = wc.park_id
  LEFT JOIN LATERAL (SELECT p.name FROM locations p WHERE p.location_type='shed' AND p.parent_location_id IS NOT DISTINCT FROM l.parent_location_id
       AND p.location_id <> l.location_id AND l.name LIKE p.name || ' %' ORDER BY length(p.name) DESC LIMIT 1) parent ON true
  WHERE s.withdrawn_at IS NULL AND s.verification_status <> 'rejected' AND s.average_weight_kg > 0),
pen AS (SELECT DISTINCT ON (park_label, shed_label, part) park_label, shed_label, part, average_weight_kg, (accepted_at AT TIME ZONE 'Asia/Kolkata')::date wd
  FROM bucket ORDER BY park_label, shed_label, part, accepted_at DESC),
x AS (SELECT a.park_label, a.species, coalesce(ind.weight_kg, pen.average_weight_kg) kg,
         CASE WHEN ind.goat_id IS NOT NULL THEN 'individual' WHEN pen.average_weight_kg IS NOT NULL THEN 'pen' END arm,
         coalesce(ind.wd, pen.wd) wd
  FROM alive a LEFT JOIN ind USING (goat_id)
  LEFT JOIN pen ON pen.park_label=a.park_label AND pen.shed_label=a.shed_label AND pen.part=a.part)
SELECT coalesce(park_label,'ALL') park, coalesce(species,'all') species, count(*) alive,
  count(kg) weighed, count(*) FILTER (WHERE arm='individual') by_individual, count(*) FILTER (WHERE arm='pen') by_pen_avg,
  round(avg(kg)::numeric,1) avg_kg, min(wd) oldest_weigh, max(wd) newest_weigh
FROM x GROUP BY ROLLUP(park_label, species) ORDER BY 1,2;
