-- Shed -> procurement-load mapping for the Weights page's load-wise growth chart.
--
-- SOURCE: the farm's own load sheet (Farm / Shed / Load ID + owner), supplied by the
-- maintainer 2026-08-08. It is the only record of which load went into which shed;
-- procurement_loads / procurement_load_goats are empty, which is why the mapping is
-- weighing-owned (see migration 000131 for the full reasoning).
--
-- WHY THIS IS A FIXTURE AND NOT A MIGRATION: these are live counterparty names
-- belonging to one tenant, not schema. Migration 000131 ships the table EMPTY on
-- purpose so real supplier names are not committed into every checkout of the repo.
-- This file is applied deliberately, per environment.
--
-- SAFE TO RE-RUN. Idempotent on (tenant_id, location_id, load_ref); a re-run refreshes
-- owner_name and updated_at and inserts nothing new.
--
-- RESOLUTION: sheds are matched by PARK CODE + SHED NAME, and narrowed to locations
-- that weighing actually books buckets against (the join to weighing_campaign_sheds).
-- That last join matters: `locations` also holds inactive partition rows carrying the
-- same names, and tagging one of those would attach the load to a shed no weigh is
-- ever recorded under. Tenant comes from the bucket rather than being hardcoded.
--
-- KNOWN AND INTENTIONAL: CPT Mandela 1 - Part 1 carries BOTH load 100 and load 101.
-- The primary key admits that because it is the truth on the ground. The read then
-- drops that shed from every load and counts it in load_unattributed_sheds rather than
-- splitting one shed average between two suppliers. Do not "fix" the duplicate.
--
-- Load 113 (CBE, "All Sold") is deliberately absent: it maps to no shed.

WITH m(park_code, shed_name, load_ref, owner_name) AS (VALUES
  ('CPT','Castro 1',          '131','Krishnamorrthy'),
  ('CPT','Castro 2',          '131','Krishnamorrthy'),
  ('CBE','Castro 2',          '130','Green Fresh Farm'),
  ('CPT','Godel 2 - Part 1',  '129','Krishnamorrthy'),
  ('CPT','Godel 2 - Part 2',  '129','Krishnamorrthy'),
  ('CBE','Castro 3',          '128','Krishnamorrthy'),
  ('CBE','Castro 1',          '126','Ramesh Reddy'),
  ('CPT','Mandela 1 - Part 1','101','Dr Praneeth'),
  ('CPT','Mandela 1 - Part 1','100','Green Fresh Farm')
)
INSERT INTO weighing_shed_load_tags (tenant_id, location_id, load_ref, owner_name, notes)
SELECT DISTINCT cs.tenant_id, l.location_id, m.load_ref, m.owner_name, 'load sheet 2026-08-08'
FROM m
JOIN locations pk ON COALESCE(NULLIF(pk.location_code, ''), pk.name) = m.park_code
JOIN locations l  ON l.parent_location_id = pk.location_id AND l.name = m.shed_name
JOIN weighing_campaign_sheds cs ON cs.location_id = l.location_id
ON CONFLICT (tenant_id, location_id, load_ref) DO UPDATE
  SET owner_name = EXCLUDED.owner_name,
      updated_at = now();
