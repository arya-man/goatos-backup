-- LATEST WEIGHING PER PEN (individual scans + whole-pen weighs), via pens.sql so it works in any pen model.
-- "G1P3 last weighing" = run THIS once, edit only the filter in the last WHERE (pen_code+park_code, or grp='Godel 1', or drop it).
-- Individual: weighing_observations in the pen's bucket, non-rejected, weight_kg > 0 (weight_kg = corrected value): latest IST day
--   -> animals scanned that day + avg kg + how many still pending verification.
-- Whole pen: weighing_shed_observations non-withdrawn, non-rejected, average_weight_kg > 0 -> latest one's head count + avg + status.
-- Answer: pen + park, latest date DD/MM/YYYY, individual and/or whole-pen line, say "pending verification" if any.
WITH pl AS (SELECT l.location_id id, l.name, l.status = 'active' AND l.retired_at IS NULL act, coalesce(pk.name, gp.name) park,
    CASE WHEN par.location_type = 'shed' THEN par.name ELSE coalesce((SELECT g.name FROM locations g WHERE g.location_type = 'shed'
      AND g.parent_location_id = l.parent_location_id AND g.location_id <> l.location_id AND l.name LIKE g.name || ' %'
      ORDER BY g.status = 'active' DESC, length(g.name) DESC LIMIT 1), substring(l.name FROM '^(.+?) - ')) END grp
  FROM locations l JOIN locations par ON par.location_id = l.parent_location_id
  LEFT JOIN locations pk ON pk.location_id = par.location_id AND par.location_type = 'park'
  LEFT JOIN locations gp ON gp.location_id = par.parent_location_id AND par.location_type = 'shed'
  WHERE l.location_type = 'shed'),
pk AS (SELECT DISTINCT ON (loc, lbl) * FROM (
    SELECT sp.shed_id loc, sp.normalized_label lbl, 1 o, g.park, g.name grp, sp.partition_label part, sp.status = 'active' AND g.act act
    FROM shed_partitions sp JOIN pl g ON g.id = sp.shed_id
    UNION ALL SELECT sp.alias_location_id, '', 2, g.park, g.name, sp.partition_label, sp.status = 'active' AND a.act
    FROM shed_partitions sp JOIN pl g ON g.id = sp.shed_id JOIN pl a ON a.id = sp.alias_location_id
    UNION ALL SELECT id, '', 3, park, coalesce(grp, name), CASE WHEN grp IS NULL THEN '' ELSE btrim(substr(name, length(grp) + 1), ' -') END, act
    FROM pl) s ORDER BY loc, lbl, o),
pens AS (SELECT loc, lbl, o, park, grp, act, pc || ':' || lower(grp) || ':' || n pen_key, pc park_code,
    replace(upper(regexp_replace(grp, '([A-Za-z])[A-Za-z]*', '\1', 'g')), ' ', '') || CASE WHEN n = '' THEN '' WHEN part ~* '^p' THEN 'P' ELSE '' END || n pen_code,
    grp || CASE WHEN n = '' THEN '' WHEN part ~* '^p' THEN ' Part ' ELSE ' ' END || n pen_name,
    n = '' AND EXISTS (SELECT 1 FROM shed_partitions sp WHERE sp.shed_id = loc) grp_only
  FROM (SELECT *, regexp_replace(lower(part), '^(part|pt)[-. ]*', '') n,
      CASE park WHEN 'Coimbatore' THEN 'CBE' WHEN 'Channapatna' THEN 'CPT' ELSE upper(left(park, 3)) END pc FROM pk) x),
b AS (SELECT wcs.campaign_shed_id, pen.* FROM weighing_campaign_sheds wcs
  JOIN LATERAL (SELECT * FROM pens k WHERE k.loc = wcs.location_id AND k.lbl IN (regexp_replace(regexp_replace(lower(btrim(coalesce(
    wcs.partition_label,''))),'^[- ]*(part|pt)[-. ]*',''),'^whole$',''), '') ORDER BY k.lbl = '', k.o LIMIT 1) pen ON true),
ind AS (SELECT DISTINCT ON (pen_key) * FROM (SELECT b.pen_key, (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d,
    count(DISTINCT lower(btrim(o.scanned_identifier))) animals, round(avg(o.weight_kg), 2) avg_kg,
    count(*) FILTER (WHERE o.verification_status = 'pending') pending
  FROM weighing_observations o JOIN b USING (campaign_shed_id)
  WHERE o.verification_status <> 'rejected' AND o.weight_kg > 0 GROUP BY 1, 2) x ORDER BY pen_key, d DESC),
lump AS (SELECT DISTINCT ON (b.pen_key) b.pen_key, (s.accepted_at AT TIME ZONE 'Asia/Kolkata')::date d, s.animal_count animals,
    round(s.average_weight_kg, 2) avg_kg, s.verification_status
  FROM weighing_shed_observations s JOIN b USING (campaign_shed_id)
  WHERE s.withdrawn_at IS NULL AND s.verification_status <> 'rejected' AND s.average_weight_kg > 0 ORDER BY b.pen_key, s.accepted_at DESC),
p AS (SELECT DISTINCT ON (pen_key) pen_key, park, park_code, grp, pen_code, pen_name FROM b ORDER BY pen_key, o)
SELECT p.park, p.pen_name || ' (' || p.pen_code || ')' pen, to_char(greatest(i.d, l.d), 'DD/MM/YYYY') latest,
  to_char(i.d, 'DD/MM/YYYY') indiv_date, i.animals indiv_animals, i.avg_kg indiv_avg_kg, i.pending indiv_pending,
  to_char(l.d, 'DD/MM/YYYY') pen_date, l.animals pen_animals, l.avg_kg pen_avg_kg, l.verification_status pen_status
FROM p LEFT JOIN ind i USING (pen_key) LEFT JOIN lump l USING (pen_key)
WHERE (i.d IS NOT NULL OR l.d IS NOT NULL)
  AND p.pen_code = 'G1P3' AND p.park_code = 'CBE'   -- <- the only line to edit
ORDER BY p.park, p.pen_key;
