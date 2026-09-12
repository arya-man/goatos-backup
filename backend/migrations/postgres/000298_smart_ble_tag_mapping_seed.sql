-- +goose Up
-- BLE smart tags are physical identifiers, but not the same objects as the two
-- RFID slots. Add a dedicated third identifier type and seed the first 19
-- farm-assigned BLE tags from the 2026-09-13 photo/RFID capture.

SET lock_timeout = '5s';

ALTER TABLE public.goat_identifiers DROP CONSTRAINT goat_identifiers_type_check;
ALTER TABLE public.goat_identifiers
  ADD CONSTRAINT goat_identifiers_type_check
  CHECK (identifier_type = ANY (ARRAY[
    'animal_identifier_1'::text,
    'animal_identifier_2'::text,
    'temporary_tag'::text,
    'smart_ble_tag'::text
  ]))
  NOT VALID;
ALTER TABLE public.goat_identifiers VALIDATE CONSTRAINT goat_identifiers_type_check;

ALTER TABLE public.identifier_policies DROP CONSTRAINT identifier_policies_identifier_type_check;
ALTER TABLE public.identifier_policies
  ADD CONSTRAINT identifier_policies_identifier_type_check
  CHECK (identifier_type = ANY (ARRAY[
    'animal_identifier_1'::text,
    'animal_identifier_2'::text,
    'temporary_tag'::text,
    'smart_ble_tag'::text
  ]))
  NOT VALID;
ALTER TABLE public.identifier_policies VALIDATE CONSTRAINT identifier_policies_identifier_type_check;

INSERT INTO public.identifier_policies
  (policy_version, identifier_type, default_scope_type, scope_required, active_uniqueness,
   auto_link_allowed, primary_allowed, unknown_scope_action, missing_or_conflicting_scope_action,
   normalizer_version, format_validator_version, invalid_value_action, created_at, approved_by)
VALUES
  ('phase1-identifier-v1', 'smart_ble_tag', 'global', true, 'global',
   false, false, 'reject', 'reject',
   'identifier_normalizer_v1', NULL, 'reject', now(), NULL)
ON CONFLICT DO NOTHING;

DO $$
DECLARE
  seed_tenant uuid := '00000000-0000-4000-8000-000000000001';
  bad_rfid text;
  bad_group text;
  bad_existing text;
  matched_rfid_count integer;
  group_count integer;
  distinct_goat_count integer;
BEGIN
  CREATE TEMP TABLE _smart_ble_seed (
    ble_tag text NOT NULL,
    ble_mac text NOT NULL,
    rfid text NOT NULL,
    slot integer NOT NULL
  ) ON COMMIT DROP;

  INSERT INTO _smart_ble_seed (ble_tag, ble_mac, rfid, slot) VALUES
    ('A00041','F0:C9:90:A0:00:41','901007000504387',1),('A00041','F0:C9:90:A0:00:41','901007000506078',2),
    ('A00031','F0:C9:90:A0:00:31','901007000504394',1),('A00031','F0:C9:90:A0:00:31','901007000506033',2),
    ('A0002C','F0:C9:90:A0:00:2C','901007000504141',1),('A0002C','F0:C9:90:A0:00:2C','901007000506071',2),
    ('A0002F','F0:C9:90:A0:00:2F','901007000506051',1),('A0002F','F0:C9:90:A0:00:2F','901007000504192',2),
    ('A0002D','F0:C9:90:A0:00:2D','901007000504154',1),
    ('A00034','F0:C9:90:A0:00:34','901007000504386',1),
    ('A00030','F0:C9:90:A0:00:30','901007000504072',1),
    ('A00038','F0:C9:90:A0:00:38','901007000504106',1),
    ('A0002B','F0:C9:90:A0:00:2B','901007000504067',1),
    ('A00040','F0:C9:90:A0:00:40','901007000504111',1),
    ('A00035','F0:C9:90:A0:00:35','901007000504197',1),
    ('A0003A','F0:C9:90:A0:00:3A','901007000504194',1),
    ('A0003C','F0:C9:90:A0:00:3C','901007000505115',1),
    ('A0002E','F0:C9:90:A0:00:2E','901007000504342',1),
    ('A0003E','F0:C9:90:A0:00:3E','901007000505093',1),
    ('A0002A','F0:C9:90:A0:00:2A','901007000504378',1),
    ('A0003F','F0:C9:90:A0:00:3F','901007000505106',1),
    ('A00033','F0:C9:90:A0:00:33','901007000506132',1),
    ('A00036','F0:C9:90:A0:00:36','901007000504155',1);

  SELECT count(DISTINCT s.rfid)
    INTO matched_rfid_count
  FROM _smart_ble_seed s
  JOIN public.goat_identifiers gi
    ON gi.tenant_id = seed_tenant
   AND gi.normalized_value = upper(btrim(s.rfid))
   AND gi.status = 'active'
   AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2');

  IF matched_rfid_count = 0 THEN
    RAISE NOTICE 'smart BLE seed skipped: staging RFIDs are not present in this database';
    RETURN;
  END IF;

  SELECT string_agg(s.rfid, ', ' ORDER BY s.rfid)
    INTO bad_rfid
  FROM _smart_ble_seed s
  LEFT JOIN public.goat_identifiers gi
    ON gi.tenant_id = seed_tenant
   AND gi.normalized_value = upper(btrim(s.rfid))
   AND gi.status = 'active'
   AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
  GROUP BY s.rfid
  HAVING count(gi.identifier_id) <> 1
  LIMIT 1;

  IF bad_rfid IS NOT NULL THEN
    RAISE EXCEPTION 'smart BLE seed aborted: RFID(s) missing, duplicated, inactive, or not RFID type: %', bad_rfid;
  END IF;

  SELECT string_agg(ble_tag, ', ' ORDER BY ble_tag)
    INTO bad_group
  FROM (
    SELECT s.ble_tag, count(DISTINCT gi.goat_id) AS goats
    FROM _smart_ble_seed s
    JOIN public.goat_identifiers gi
      ON gi.tenant_id = seed_tenant
     AND gi.normalized_value = upper(btrim(s.rfid))
     AND gi.status = 'active'
     AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    GROUP BY s.ble_tag
    HAVING count(DISTINCT gi.goat_id) <> 1
  ) grouped;

  IF bad_group IS NOT NULL THEN
    RAISE EXCEPTION 'smart BLE seed aborted: RFID pair(s) do not resolve to exactly one goat: %', bad_group;
  END IF;

  CREATE TEMP TABLE _smart_ble_group AS
  SELECT DISTINCT s.ble_tag, s.ble_mac, gi.tenant_id, gi.goat_id
  FROM _smart_ble_seed s
  JOIN public.goat_identifiers gi
    ON gi.tenant_id = seed_tenant
   AND gi.normalized_value = upper(btrim(s.rfid))
   AND gi.status = 'active'
   AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2');

  SELECT count(*), count(DISTINCT goat_id)
    INTO group_count, distinct_goat_count
  FROM _smart_ble_group;

  IF group_count <> 19 OR distinct_goat_count <> 19 THEN
    RAISE EXCEPTION 'smart BLE seed aborted: expected 19 BLE tags mapped to 19 distinct goats, got groups %, distinct goats %',
      group_count, distinct_goat_count;
  END IF;

  SELECT string_agg(e.normalized_value, ', ' ORDER BY e.normalized_value)
    INTO bad_existing
  FROM (
    SELECT g.tenant_id, g.goat_id, upper(btrim(g.ble_tag)) AS normalized_value
    FROM _smart_ble_group g
    UNION ALL
    SELECT g.tenant_id, g.goat_id, upper(btrim(g.ble_mac)) AS normalized_value
    FROM _smart_ble_group g
  ) intended
  JOIN public.goat_identifiers e
    ON e.tenant_id = intended.tenant_id
   AND e.normalized_value = intended.normalized_value
  WHERE e.goat_id <> intended.goat_id;

  IF bad_existing IS NOT NULL THEN
    RAISE EXCEPTION 'smart BLE seed aborted: BLE identifier(s) already claimed by another goat: %', bad_existing;
  END IF;

  INSERT INTO public.goat_identifiers (
    tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
    scope_key, is_primary_for_goat, status, valid_from, normalizer_version,
    smart_tag_capable, smart_tag_mapped_at, mapped_by, mapped_at, source_system, source_record_id
  )
  SELECT tenant_id, goat_id, 'smart_ble_tag', ble_tag, upper(btrim(ble_tag)),
         'global', false, 'active', now(), 'identifier_normalizer_v1',
         true, now(), 'migration:000298_smart_ble_tag_mapping_seed', now(), 'herd_signals', '2026-09-13:' || ble_tag
  FROM _smart_ble_group
  ON CONFLICT (tenant_id, normalized_value) DO UPDATE
    SET goat_id = EXCLUDED.goat_id,
        identifier_type = 'smart_ble_tag',
        status = 'active',
        valid_to = NULL,
        smart_tag_capable = true,
        smart_tag_mapped_at = COALESCE(public.goat_identifiers.smart_tag_mapped_at, EXCLUDED.smart_tag_mapped_at),
        mapped_by = EXCLUDED.mapped_by,
        mapped_at = EXCLUDED.mapped_at,
        source_system = EXCLUDED.source_system,
        source_record_id = EXCLUDED.source_record_id,
        updated_at = now()
  WHERE public.goat_identifiers.goat_id = EXCLUDED.goat_id
     OR public.goat_identifiers.source_record_id = EXCLUDED.source_record_id;

  INSERT INTO public.goat_identifiers (
    tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
    scope_key, is_primary_for_goat, status, valid_from, normalizer_version,
    smart_tag_capable, smart_tag_mapped_at, mapped_by, mapped_at, source_system, source_record_id
  )
  SELECT tenant_id, goat_id, 'smart_ble_tag', ble_mac, upper(btrim(ble_mac)),
         'global', false, 'active', now(), 'identifier_normalizer_v1',
         true, now(), 'migration:000298_smart_ble_tag_mapping_seed', now(), 'herd_signals', '2026-09-13:' || ble_mac
  FROM _smart_ble_group
  ON CONFLICT (tenant_id, normalized_value) DO UPDATE
    SET goat_id = EXCLUDED.goat_id,
        identifier_type = 'smart_ble_tag',
        status = 'active',
        valid_to = NULL,
        smart_tag_capable = true,
        smart_tag_mapped_at = COALESCE(public.goat_identifiers.smart_tag_mapped_at, EXCLUDED.smart_tag_mapped_at),
        mapped_by = EXCLUDED.mapped_by,
        mapped_at = EXCLUDED.mapped_at,
        source_system = EXCLUDED.source_system,
        source_record_id = EXCLUDED.source_record_id,
        updated_at = now()
  WHERE public.goat_identifiers.goat_id = EXCLUDED.goat_id
     OR public.goat_identifiers.source_record_id = EXCLUDED.source_record_id;

  UPDATE public.herd_signal_tag_latest tl
     SET mapping_state = 'mapped',
         animal_monitoring_since = COALESCE(tl.animal_monitoring_since, g.mapped_since),
         updated_at = now()
    FROM (
      SELECT gi.tenant_id, sg.ble_tag, sg.ble_mac, min(gi.smart_tag_mapped_at) AS mapped_since
      FROM _smart_ble_group sg
      JOIN public.goat_identifiers gi
        ON gi.tenant_id = sg.tenant_id
       AND gi.normalized_value IN (upper(btrim(sg.ble_tag)), upper(btrim(sg.ble_mac)))
       AND gi.status = 'active'
       AND gi.smart_tag_capable IS TRUE
      GROUP BY gi.tenant_id, sg.ble_tag, sg.ble_mac
    ) g
   WHERE tl.tenant_id = g.tenant_id
     AND (
       upper(btrim(coalesce(tl.tag_id, ''))) IN (upper(btrim(g.ble_tag)), upper(btrim(g.ble_mac)))
       OR upper(btrim(coalesce(tl.tag_mac, ''))) IN (upper(btrim(g.ble_tag)), upper(btrim(g.ble_mac)))
     );
END $$;

-- +goose Down
SET lock_timeout = '5s';

DO $$
DECLARE
  seed_tenant uuid := '00000000-0000-4000-8000-000000000001';
BEGIN
  CREATE TEMP TABLE _smart_ble_seed_down (
    ble_tag text NOT NULL,
    ble_mac text NOT NULL
  ) ON COMMIT DROP;

  INSERT INTO _smart_ble_seed_down (ble_tag, ble_mac) VALUES
    ('A00041','F0:C9:90:A0:00:41'),
    ('A00031','F0:C9:90:A0:00:31'),
    ('A0002C','F0:C9:90:A0:00:2C'),
    ('A0002F','F0:C9:90:A0:00:2F'),
    ('A0002D','F0:C9:90:A0:00:2D'),
    ('A00034','F0:C9:90:A0:00:34'),
    ('A00030','F0:C9:90:A0:00:30'),
    ('A00038','F0:C9:90:A0:00:38'),
    ('A0002B','F0:C9:90:A0:00:2B'),
    ('A00040','F0:C9:90:A0:00:40'),
    ('A00035','F0:C9:90:A0:00:35'),
    ('A0003A','F0:C9:90:A0:00:3A'),
    ('A0003C','F0:C9:90:A0:00:3C'),
    ('A0002E','F0:C9:90:A0:00:2E'),
    ('A0003E','F0:C9:90:A0:00:3E'),
    ('A0002A','F0:C9:90:A0:00:2A'),
    ('A0003F','F0:C9:90:A0:00:3F'),
    ('A00033','F0:C9:90:A0:00:33'),
    ('A00036','F0:C9:90:A0:00:36');

  UPDATE public.herd_signal_tag_latest tl
     SET mapping_state = 'unmapped',
         animal_monitoring_since = NULL,
         updated_at = now()
    FROM _smart_ble_seed_down s
   WHERE tl.tenant_id = seed_tenant
     AND (
       upper(btrim(coalesce(tl.tag_id, ''))) IN (upper(btrim(s.ble_tag)), upper(btrim(s.ble_mac)))
       OR upper(btrim(coalesce(tl.tag_mac, ''))) IN (upper(btrim(s.ble_tag)), upper(btrim(s.ble_mac)))
     );

  DELETE FROM public.goat_identifiers
   WHERE tenant_id = seed_tenant
     AND identifier_type = 'smart_ble_tag'
     AND source_system = 'herd_signals'
     AND source_record_id LIKE '2026-09-13:%';

  IF EXISTS (
    SELECT 1
    FROM public.goat_identifiers
    WHERE identifier_type = 'smart_ble_tag'
  ) THEN
    RAISE EXCEPTION 'cannot remove smart_ble_tag identifier type: non-seeded smart_ble_tag identifiers still exist';
  END IF;
END $$;

DELETE FROM public.identifier_policies WHERE identifier_type = 'smart_ble_tag';

ALTER TABLE public.identifier_policies DROP CONSTRAINT identifier_policies_identifier_type_check;
ALTER TABLE public.identifier_policies
  ADD CONSTRAINT identifier_policies_identifier_type_check
  CHECK (identifier_type = ANY (ARRAY[
    'animal_identifier_1'::text,
    'animal_identifier_2'::text,
    'temporary_tag'::text
  ]))
  NOT VALID;
ALTER TABLE public.identifier_policies VALIDATE CONSTRAINT identifier_policies_identifier_type_check;

ALTER TABLE public.goat_identifiers DROP CONSTRAINT goat_identifiers_type_check;
ALTER TABLE public.goat_identifiers
  ADD CONSTRAINT goat_identifiers_type_check
  CHECK (identifier_type = ANY (ARRAY[
    'animal_identifier_1'::text,
    'animal_identifier_2'::text,
    'temporary_tag'::text
  ]))
  NOT VALID;
ALTER TABLE public.goat_identifiers VALIDATE CONSTRAINT goat_identifiers_type_check;
