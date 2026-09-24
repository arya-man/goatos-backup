-- Self-test for pens.sql. Run (from this folder):
--   { sed -n '/^WITH pl AS/,/^  WHERE g.merged_into_goat_id IS NULL)$/p' pens.sql; cat pens-selftest.sql; } | psql -X -A
-- Every row must say ok=t. Re-run after ANY pen-model migration (docs/agent-rules/ask-mesha.md "Pens (model-agnostic)").
, alive AS (SELECT * FROM pa WHERE lifecycle_status = 'alive')
, direct AS (   -- today's label-model truth: group row + catalog label, counted WITHOUT the resolver
  SELECT CASE pk.name WHEN 'Coimbatore' THEN 'CBE' WHEN 'Channapatna' THEN 'CPT' ELSE upper(left(pk.name,3)) END -- operational-location:ignore: owner=ravi issue=vgoats/goatos#388 scope=pen_key-is-a-match-key-not-a-display-label expiry=2027-03-31
         || ':' || lower(s.name) || ':' || sp.normalized_label pen_key, count(*) n -- operational-location:ignore: owner=ravi issue=vgoats/goatos#388 scope=pen_key-is-a-match-key-not-a-display-label expiry=2027-03-31
  FROM goats g JOIN goat_shed_partitions gsp ON gsp.goat_id = g.goat_id JOIN locations s ON s.location_id = g.shed_id
  JOIN locations pk ON pk.location_id = s.parent_location_id
  JOIN shed_partitions sp ON sp.shed_id = g.shed_id AND sp.normalized_label = regexp_replace(lower(btrim(gsp.partition_label)),'^part[[:space:]]*','')
  WHERE g.lifecycle_status = 'alive' AND g.merged_into_goat_id IS NULL GROUP BY 1)
, per_pen AS (SELECT coalesce(a.pen_key, d.pen_key) pen_key, a.n resolved, d.n direct
  FROM (SELECT pen_key, count(*) n FROM alive GROUP BY 1) a FULL JOIN direct d USING (pen_key))
, recs AS (   -- every record type that points at a pen, last 30 days
  SELECT 'weighing bucket' kind, location_id loc, partition_label lbl FROM weighing_campaign_sheds WHERE created_at >= now() - interval '30 days'
  UNION ALL SELECT 'verification item', shed_id, partition_label FROM verification_items WHERE shed_id IS NOT NULL AND created_at >= now() - interval '30 days'
  UNION ALL SELECT 'pc care task', shed_id, partition_label FROM pc_care_tasks WHERE shed_id IS NOT NULL AND created_at >= now() - interval '30 days'
  UNION ALL SELECT 'feed row', shed_id, partition_label FROM feed_direction_issue_rows WHERE shed_id IS NOT NULL AND created_at >= now() - interval '30 days')
, resolved AS (SELECT r.kind, pen.pen_key, pen.grp_only,
    (SELECT count(*) FROM pens k WHERE k.loc = r.loc AND k.lbl = regexp_replace(regexp_replace(lower(btrim(coalesce(r.lbl,''))),'^[- ]*(part|pt)[-. ]*',''),'^whole$','')) exact_keys
  FROM recs r LEFT JOIN LATERAL (SELECT * FROM pens k WHERE k.loc = r.loc AND k.lbl IN (regexp_replace(regexp_replace(lower(btrim(coalesce(
    r.lbl,''))),'^[- ]*(part|pt)[-. ]*',''),'^whole$',''), '') ORDER BY k.lbl = '', k.o LIMIT 1) pen ON true)
SELECT 'every alive animal resolves to a real pen' test, count(*) FILTER (WHERE pen_key IS NULL OR grp_only) = 0 ok,
       count(*) FILTER (WHERE pen_key IS NULL OR grp_only) bad, count(*) total FROM alive
UNION ALL SELECT 'alive total = goats register', (SELECT count(*) FROM alive) = count(*), (SELECT count(*) FROM alive) - count(*), count(*)
  FROM goats WHERE lifecycle_status = 'alive' AND merged_into_goat_id IS NULL
UNION ALL SELECT 'animals per pen = partition counts (label model)', count(*) FILTER (WHERE resolved IS DISTINCT FROM direct) = 0,
       count(*) FILTER (WHERE resolved IS DISTINCT FROM direct), count(*) FROM per_pen
UNION ALL SELECT kind || 's (30d) resolve to exactly one pen', count(*) FILTER (WHERE pen_key IS NULL OR exact_keys > 1) = 0,
       count(*) FILTER (WHERE pen_key IS NULL OR exact_keys > 1), count(*) FROM resolved GROUP BY kind
UNION ALL SELECT 'weighing buckets (30d) land on a pen, not a bare group', count(*) FILTER (WHERE grp_only) = 0,
       count(*) FILTER (WHERE grp_only), count(*) FROM resolved WHERE kind = 'weighing bucket'
UNION ALL SELECT 'no duplicate (location, label) keys', count(*) = count(DISTINCT (loc, lbl)), count(*) - count(DISTINCT (loc, lbl)), count(*) FROM pens
UNION ALL SELECT 'model 2 ready: pen row and group+label give the same pen', count(*) FILTER (WHERE a.pen_key IS DISTINCT FROM g.pen_key) = 0,
       count(*) FILTER (WHERE a.pen_key IS DISTINCT FROM g.pen_key), count(*)
  FROM shed_partitions sp JOIN pens g ON g.loc = sp.shed_id AND g.lbl = sp.normalized_label
  LEFT JOIN pens a ON a.loc = sp.alias_location_id AND a.lbl = '' WHERE sp.alias_location_id IS NOT NULL
UNION ALL SELECT 'legacy pen rows not in the catalog split to a group + part', count(*) FILTER (WHERE k.pen_key ~ ':$') = 0,
       count(*) FILTER (WHERE k.pen_key ~ ':$'), count(*)
  FROM pens k JOIN locations l ON l.location_id = k.loc WHERE k.o = 3 AND l.name ~* ' - part [0-9]+$';
