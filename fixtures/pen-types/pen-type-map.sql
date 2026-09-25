-- Pen -> pen type mapping, so a fresh or reseeded database carries the farm's classification.
--
-- SOURCE: goatos-stg, read-only, 26/09/2026 -- every shed_partitions row that carried a type
-- (130 rows: 117 active, 13 retired; 100 active elevated, 17 active non_elevated). The farm sets
-- these on Configuration -> Items and settings -> Partitions; this file is the last known copy of
-- that answer, so a reseed does not come back with every pen unclassified.
--
-- WHY A FIXTURE AND NOT A MIGRATION: migrations run BEFORE the seed creates pens, so a migration
-- backfill on a fresh database has nothing to classify (that is how 000385's own backfill ends up
-- doing nothing on a reseed). This runs in seed closeout, after the pens exist.
--
-- SAFE TO RE-RUN, AND NEVER OVERWRITES THE FARM. It only fills a pen whose type is NULL: a pen the
-- farm has typed -- including one it moved to a pen type added after this file was written -- is
-- left exactly as it is. The two base pen types are created per tenant if missing (the same two
-- migration 000437 seeds), because shed_partitions.shed_type is a foreign key into pen_types.
--
-- RESOLUTION: park CODE + pen (shed) NAME + normalized partition label -- the pen's identity on
-- screen, which survives a reseed where ids do not. A pen this file names that the database does
-- not have is simply skipped. Tenant comes from the matched rows, never hardcoded.

INSERT INTO pen_types (tenant_id, pen_type_key, name, sort_order)
SELECT t.tenant_id, v.pen_type_key, v.name, v.sort_order
FROM (SELECT DISTINCT tenant_id FROM shed_partitions) t
CROSS JOIN (VALUES ('elevated', 'Elevated', 10), ('non_elevated', 'Non-elevated', 20)) AS v(pen_type_key, name, sort_order)
ON CONFLICT (tenant_id, pen_type_key) DO NOTHING;

-- projection-review: membership=the VALUES map below; group_key=(park_code, pen, normalized_label),
-- unique in the map (checked when it was generated) and matched to shed_partitions' primary key
-- (tenant_id, shed_id, normalized_label) through the pen's own location and its park, so each map
-- row touches at most one partition per tenant; join_cardinality=locations by (tenant, shed name,
-- parent park) could match a legacy alias location of the same name, which is why the partition is
-- matched on shed_id = that location AND its normalized label -- an alias location carries no
-- partition rows of its own under that label; pagination=NONE, 130 rows; scope=tenant_id carried
-- through every join and the UPDATE predicate.
WITH m(park_code, pen, normalized_label, pen_type) AS (VALUES
  ('CBE', 'Castro', '1', 'non_elevated'),
  ('CBE', 'Castro', '2', 'non_elevated'),
  ('CBE', 'Castro', '3', 'non_elevated'),
  ('CBE', 'Gandhi', '1', 'non_elevated'),
  ('CBE', 'Gandhi', '2', 'non_elevated'),
  ('CBE', 'Gandhi', '3', 'non_elevated'),
  ('CBE', 'Godel 1', '1', 'elevated'),
  ('CBE', 'Godel 1', '2', 'elevated'),
  ('CBE', 'Godel 1', '3', 'elevated'),
  ('CBE', 'Godel 1', '4', 'elevated'),
  ('CBE', 'Godel 1', '5', 'elevated'),
  ('CBE', 'Godel 1', '6', 'elevated'),
  ('CBE', 'Godel 1', '7', 'elevated'),
  ('CBE', 'Godel 1', '8', 'elevated'),
  ('CBE', 'Godel 1', '9', 'elevated'),
  ('CBE', 'Godel 1', '10', 'elevated'),
  ('CBE', 'Godel 2', '1', 'elevated'),
  ('CBE', 'Godel 2', '2', 'elevated'),
  ('CBE', 'Godel 2', '3', 'elevated'),
  ('CBE', 'Godel 2', '4', 'elevated'),
  ('CBE', 'Godel 2', '5', 'elevated'),
  ('CBE', 'Godel 2', '6', 'elevated'),
  ('CBE', 'Godel 2', '7', 'elevated'),
  ('CBE', 'Godel 2', '8', 'elevated'),
  ('CBE', 'Godel 2', '9', 'elevated'),
  ('CBE', 'Godel 2', '10', 'elevated'),
  ('CBE', 'Ho Chi Minh', '1', 'non_elevated'),
  ('CBE', 'Ho Chi Minh', '2', 'non_elevated'),
  ('CBE', 'Mandela 1', '1', 'elevated'),
  ('CBE', 'Mandela 1', '2', 'elevated'),
  ('CBE', 'Mandela 1', '3', 'elevated'),
  ('CBE', 'Mandela 1', '4', 'elevated'),
  ('CBE', 'Mandela 1', '5', 'elevated'),
  ('CBE', 'Mandela 1', '6', 'elevated'),
  ('CBE', 'Mandela 1', '7', 'elevated'),
  ('CBE', 'Mandela 1', '8', 'elevated'),
  ('CBE', 'Mandela 1', '9', 'elevated'),
  ('CBE', 'Mandela 1', '10', 'elevated'),
  ('CBE', 'Mandela 2', '1', 'elevated'),
  ('CBE', 'Mandela 2', '2', 'elevated'),
  ('CBE', 'Mandela 2', '3', 'elevated'),
  ('CBE', 'Mandela 2', '4', 'elevated'),
  ('CBE', 'Mandela 2', '5', 'elevated'),
  ('CBE', 'Mandela 2', '6', 'elevated'),
  ('CBE', 'Mandela 2', '7', 'elevated'),
  ('CBE', 'Mandela 2', '8', 'elevated'),
  ('CBE', 'Sumathi 1', '1', 'elevated'),
  ('CBE', 'Sumathi 1', '2', 'elevated'),
  ('CBE', 'Sumathi 1', '3', 'elevated'),
  ('CBE', 'Sumathi 1', '4', 'elevated'),
  ('CBE', 'Sumathi 1', '5', 'elevated'),
  ('CBE', 'Sumathi 1', '6', 'elevated'),
  ('CBE', 'Sumathi 1', '7', 'elevated'),
  ('CBE', 'Sumathi 1', '8', 'elevated'),
  ('CBE', 'Sumathi 1', '9', 'elevated'),
  ('CBE', 'Sumathi 1', '10', 'elevated'),
  ('CBE', 'Sumathi 2', '1', 'elevated'),
  ('CBE', 'Sumathi 2', '2', 'elevated'),
  ('CBE', 'Sumathi 2', '3', 'elevated'),
  ('CBE', 'Sumathi 2', '4', 'elevated'),
  ('CBE', 'Sumathi 2', '5', 'elevated'),
  ('CBE', 'Sumathi 2', '6', 'elevated'),
  ('CBE', 'Sumathi 2', '7', 'elevated'),
  ('CBE', 'Sumathi 2', '8', 'elevated'),
  ('CBE', 'Sumathi 2', '9', 'elevated'),
  ('CBE', 'Sumathi 2', '10', 'elevated'),
  ('CBE', 'Yashoda', '1', 'elevated'),
  ('CBE', 'Yashoda', '2', 'elevated'),
  ('CBE', 'Yashoda', '3', 'elevated'),
  ('CBE', 'Yashoda', '4', 'elevated'),
  ('CBE', 'Yashoda', '5', 'elevated'),
  ('CBE', 'Yashoda', '6', 'elevated'),
  ('CBE', 'Yashoda', '7', 'elevated'),
  ('CBE', 'Yashoda', '8', 'elevated'),
  ('CBE', 'Yashoda', '9', 'elevated'),
  ('CBE', 'Yashoda', '10', 'elevated'),
  ('CPT', 'Castro', '1', 'non_elevated'),
  ('CPT', 'Castro', '2', 'non_elevated'),
  ('CPT', 'Gandhi', '1', 'non_elevated'),
  ('CPT', 'Gandhi', '2', 'non_elevated'),
  ('CPT', 'Gandhi', '3', 'non_elevated'),
  ('CPT', 'Godel 1', '1', 'elevated'),
  ('CPT', 'Godel 1', '2', 'elevated'),
  ('CPT', 'Godel 1', '3', 'elevated'),
  ('CPT', 'Godel 1', '4', 'elevated'),
  ('CPT', 'Godel 1', '5', 'elevated'),
  ('CPT', 'Godel 1', '6', 'elevated'),
  ('CPT', 'Godel 1', '7', 'elevated'),
  ('CPT', 'Godel 1', '8', 'elevated'),
  ('CPT', 'Godel 1', '9', 'elevated'),
  ('CPT', 'Godel 1', '10', 'elevated'),
  ('CPT', 'Godel 2', '1', 'elevated'),
  ('CPT', 'Godel 2', '2', 'elevated'),
  ('CPT', 'Godel 2', '3', 'elevated'),
  ('CPT', 'Godel 2', '4', 'elevated'),
  ('CPT', 'Godel 2', '5', 'elevated'),
  ('CPT', 'Godel 2', '6', 'elevated'),
  ('CPT', 'Godel 2', '7', 'elevated'),
  ('CPT', 'Godel 2', '8', 'elevated'),
  ('CPT', 'Godel 2', '9', 'elevated'),
  ('CPT', 'Godel 2', '10', 'elevated'),
  ('CPT', 'Mandela 1', '1', 'elevated'),
  ('CPT', 'Mandela 1', '2', 'elevated'),
  ('CPT', 'Mandela 1', '3', 'elevated'),
  ('CPT', 'Mandela 1', '4', 'elevated'),
  ('CPT', 'Mandela 1', '5', 'elevated'),
  ('CPT', 'Mandela 1', '6', 'elevated'),
  ('CPT', 'Mandela 1', '7', 'elevated'),
  ('CPT', 'Mandela 1', '8', 'elevated'),
  ('CPT', 'Mandela 1', '9', 'elevated'),
  ('CPT', 'Mandela 1', '10', 'elevated'),
  ('CPT', 'Mandela 2', '1', 'elevated'),
  ('CPT', 'Mandela 2', '2', 'elevated'),
  ('CPT', 'Mandela 2', '3', 'elevated'),
  ('CPT', 'Mandela 2', '4', 'elevated'),
  ('CPT', 'Mandela 2', '5', 'elevated'),
  ('CPT', 'Mandela 2', '6', 'elevated'),
  ('CPT', 'Mandela 2', '7', 'elevated'),
  ('CPT', 'Mandela 2', '8', 'elevated'),
  ('CPT', 'Mandela 2', '9', 'elevated'),
  ('CPT', 'Mandela 2', '10', 'elevated'),
  ('CPT', 'Old Yashoda', '1', 'non_elevated'),
  ('CPT', 'Old Yashoda', '2', 'non_elevated'),
  ('CPT', 'Old Yashoda', '3', 'non_elevated'),
  ('CPT', 'Old Yashoda', '4', 'non_elevated'),
  ('CPT', 'Old Yashoda', '5', 'non_elevated'),
  ('CPT', 'Yashoda', '1', 'elevated'),
  ('CPT', 'Yashoda', '2', 'elevated'),
  ('CPT', 'Yashoda', '3', 'elevated'),
  ('CPT', 'Yashoda', '4', 'elevated')
)
UPDATE shed_partitions sp
SET shed_type = m.pen_type, updated_at = now()
FROM m
JOIN locations park ON park.location_type = 'park'
 AND upper(coalesce(nullif(park.location_code, ''), park.name)) = upper(m.park_code)
JOIN locations pen ON pen.tenant_id = park.tenant_id
 AND pen.parent_location_id = park.location_id
 AND pen.name = m.pen
WHERE sp.tenant_id = pen.tenant_id
  AND sp.shed_id = pen.location_id
  AND sp.normalized_label = m.normalized_label
  AND sp.shed_type IS NULL;
