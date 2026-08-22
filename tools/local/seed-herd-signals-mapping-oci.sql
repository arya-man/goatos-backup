-- Herd Signals OCI seed: tag-to-animal mappings
--
-- Maps 19 BLE ear tags to animals in Castro shed (excluding A0003B for unmapped testing).
-- Uses the backend's ResolveTagMapping contract:
--   - Matches tag_id OR tag_mac against goat_identifiers.normalized_value
--   - Case-sensitive match (backend passes raw tag_id without normalization - see DEFECT note)
--   - Requires status='active' AND smart_tag_capable=true
--
-- DEFECT NOTE: Backend ResolveTagMapping does not normalize before matching
-- (line: values = append(values, *tagID) -- passes raw tag_id without lower/upper/trim).
-- This seed uses UPPERCASE normalized_value (matching CSV tag_id like A0002A).
-- If backend is later fixed to normalize, update this seed accordingly.

\set ON_ERROR_STOP on

\set tenant_id '00000000-0000-4000-8000-000000000001'
\set castro_shed_id '62241795-628e-58ef-9591-aa384fb0f0f7'

BEGIN;

-- ============================================================================
-- Insert gateway (one-time setup)
-- ============================================================================
INSERT INTO herd_signal_gateways (
  tenant_id, gateway_id, label, ble_mac, network_mode, status, created_at, updated_at
) VALUES (
  :'tenant_id'::uuid,
  'honeycomm-gateway-001',
  'HoneyComm Reader - Castro Shed',
  'f130d402dcb4',
  'wifi',
  'active',
  now(),
  now()
) ON CONFLICT (tenant_id, gateway_id) DO NOTHING;

-- ============================================================================
-- Map 19 tags to Castro shed animals (excluding A0003B)
-- ============================================================================
WITH tags_to_map AS (
  SELECT tag_id
  FROM (VALUES
    ('A0002A'), ('A0002B'), ('A0002C'), ('A0002D'), ('A0002E'),
    ('A0002F'), ('A00030'), ('A00031'), ('A00033'), ('A00034'),
    ('A00035'), ('A00036'), ('A00038'), ('A0003A'), ('A0003C'),
    ('A0003E'), ('A0003F'), ('A00040'), ('A00041')
  ) AS t(tag_id)
),
goats_in_shed AS (
  SELECT goat_id
  FROM goats
  WHERE current_location_id = :'castro_shed_id'::uuid
    AND lifecycle_status = 'alive'
  ORDER BY created_at
  LIMIT 19
),
paired AS (
  SELECT
    t.tag_id,
    g.goat_id,
    ROW_NUMBER() OVER (ORDER BY t.tag_id) as rn
  FROM tags_to_map t
  CROSS JOIN goats_in_shed g
  WHERE ROW_NUMBER() OVER (ORDER BY t.tag_id) = ROW_NUMBER() OVER (ORDER BY g.goat_id)
)
INSERT INTO goat_identifiers (
  tenant_id,
  goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  is_primary_for_goat,
  status,
  valid_from,
  source_system,
  source_record_id,
  normalizer_version,
  smart_tag_capable,
  created_at,
  updated_at
)
SELECT
  :'tenant_id'::uuid as tenant_id,
  p.goat_id,
  'animal_identifier_1' as identifier_type,
  p.tag_id as identifier_value,
  p.tag_id as normalized_value,  -- UPPERCASE to match backend's raw tag_id (see DEFECT note)
  'ble-tag' as scope_key,
  false as is_primary_for_goat,
  'active' as status,
  now() as valid_from,
  'herd-signals-oci-seed' as source_system,
  'tag-' || p.tag_id as source_record_id,
  'identifier_normalizer_v1' as normalizer_version,
  true as smart_tag_capable,
  now() as created_at,
  now() as updated_at
FROM paired p
ON CONFLICT (tenant_id, normalized_value) DO NOTHING;

COMMIT;

\echo ''
\echo '========================================='
\echo 'Mapping seed completed'
\echo '========================================='
\echo ''
\echo 'Verify with:'
\echo "SELECT COUNT(*) FROM goat_identifiers WHERE source_system='herd-signals-oci-seed';"
\echo ''
