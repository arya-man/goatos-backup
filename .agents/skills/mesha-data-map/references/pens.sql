-- PEN RESOLVER (model-agnostic). The ONE way to turn any (location_id, partition_label) into a pen.
-- Pen = G1P3 "Godel 1 Part 3", C1 "Castro 1". Group = Godel 1 / Castro (a grouping, never call it a shed).
-- Works today AND after either migration (docs/agent-rules/ask-mesha.md "Pens (model-agnostic)"):
--   o=1 group row + catalog label ('Part 3' or '3')             (label model: today's animals)
--   o=2 catalog alias row "Godel 1 - Part 3"/"Castro 1"        (legacy pen rows: weighing buckets, verification items; model-2 pens)
--   o=3 any other shed row: group = shed parent (hierarchy) else longest same-park shed name prefixing it, remainder = part
--       (fcr.go / shed_partition_resolve.go rule), else the text before ' - '; no group = the row itself is the pen (undivided shed / own-name pen row).
-- Retired/inactive rows + parts still resolve (act=false) for history. pen_key starts with the park code (names repeat in both parks).
-- grp_only = record sits on a group row with no/unknown part -> say "Godel 1, part not recorded", never call it a pen.
-- COPY the 3 CTEs below (pl, pk, pens) + pa if you need animals. Resolve ANY other record with this lateral (LIMIT 1 is
-- mandatory, without it counts double):
--   JOIN LATERAL (SELECT * FROM pens k WHERE k.loc = X.shed_id AND k.lbl IN (regexp_replace(regexp_replace(lower(btrim(coalesce(
--     X.partition_label,''))),'^[- ]*(part|pt)[-. ]*',''),'^whole$',''), '') ORDER BY k.lbl = '', k.o LIMIT 1) pen ON true
--   weighing bucket: weighing_campaign_sheds.location_id; verification_items / pc_care_tasks / feed rows: shed_id.
-- Never assume records sit on the group row OR the pen row; this handles both. Answer with pen.display + park, GROUP BY pen_key.
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
pa AS (SELECT g.goat_id, g.species, g.sex, g.lifecycle_status, pen.* FROM goats g
  LEFT JOIN goat_shed_partitions gsp ON gsp.goat_id = g.goat_id AND gsp.tenant_id = g.tenant_id
  JOIN LATERAL (SELECT * FROM pens k WHERE k.loc = g.shed_id AND k.lbl IN (regexp_replace(regexp_replace(lower(btrim(coalesce(
    gsp.partition_label,''))),'^[- ]*(part|pt)[-. ]*',''),'^whole$',''), '') ORDER BY k.lbl = '', k.o LIMIT 1) pen ON true
  WHERE g.merged_into_goat_id IS NULL)
-- DEMO = animals per pen now (headcount / "which pen has most" / "pens in Godel 1"): add WHERE pen_code='G1P3' AND park_code='CBE'
-- or grp='Godel 1'. Lists empty active pens too; alive on a group with no part shows under grp_only (none today).
SELECT p.park, p.pen_name || ' (' || p.pen_code || ')' pen, count(a.goat_id) alive,
       count(a.goat_id) FILTER (WHERE a.species = 'goat') goats, count(a.goat_id) FILTER (WHERE a.species = 'sheep') sheep
FROM (SELECT park, pen_key, min(pen_name) pen_name, min(pen_code) pen_code, bool_or(act) act FROM pens WHERE NOT grp_only GROUP BY 1, 2) p
LEFT JOIN pa a ON a.pen_key = p.pen_key AND a.lifecycle_status = 'alive'
GROUP BY 1, 2, p.pen_key, p.act HAVING p.act OR count(a.goat_id) > 0 ORDER BY alive DESC, 1, 2;
