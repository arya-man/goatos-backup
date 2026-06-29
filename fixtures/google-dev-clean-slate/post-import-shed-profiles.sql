\set ON_ERROR_STOP on

BEGIN;

INSERT INTO animal_stage_lookup (
  animal_stage_id, tenant_id, stage_code, name, min_age_days, max_age_days,
  sort_order, status
) VALUES
  ('00000000-0000-4000-8000-00000000b030', :'tenant_id'::uuid, 'K0', 'Newborn', 0, 1, 0, 'active'),
  ('00000000-0000-4000-8000-00000000b031', :'tenant_id'::uuid, 'K1', 'Milk training', 2, 7, 10, 'active'),
  ('00000000-0000-4000-8000-00000000b032', :'tenant_id'::uuid, 'K2', 'Milk drinking', 8, 42, 20, 'active'),
  ('00000000-0000-4000-8000-00000000b033', :'tenant_id'::uuid, 'K3', 'Weaned kids', 43, NULL, 30, 'active')
ON CONFLICT (tenant_id, stage_code) DO UPDATE
SET name = EXCLUDED.name,
    min_age_days = EXCLUDED.min_age_days,
    max_age_days = EXCLUDED.max_age_days,
    sort_order = EXCLUDED.sort_order,
    status = 'active',
    updated_at = now();

WITH desired (
  shed_code, stage_code, sex, capacity, has_icu, usable_for_vaccination,
  is_quarantine, is_icu, notes
) AS (
  VALUES
    ('GDEV-CBE-K1-A', 'K1', 'female', 80, false, true, false, false, 'google-dev sample K1 vaccination shed'),
    ('GDEV-CBE-K0-HOLD', 'K0', 'mixed', 25, false, true, false, false, 'google-dev age-ineligible newborn hold'),
    ('GDEV-CBE-K2-B', 'K2', 'mixed', 60, false, true, false, false, 'google-dev shed-profile mismatch test'),
    ('GDEV-CBE-NOVAX', 'K1', 'mixed', 35, false, false, false, false, 'google-dev location-ineligible vaccination test'),
    ('GDEV-CBE-QUAR', 'K1', 'mixed', 20, true, false, true, false, 'google-dev quarantine defer test'),
    ('GDEV-CPT-K1-A', 'K1', 'female', 75, false, true, false, false, 'google-dev shift target'),
    ('GDEV-CPT-RECOVERY', 'K1', 'mixed', 30, false, true, false, false, 'google-dev recovery validation')
),
resolved AS (
  SELECT
    l.tenant_id,
    l.location_id,
    s.animal_stage_id,
    d.sex,
    d.capacity,
    d.has_icu,
    d.usable_for_vaccination,
    d.is_quarantine,
    d.is_icu,
    d.notes
  FROM desired d
  JOIN locations l
    ON l.tenant_id = :'tenant_id'::uuid
   AND l.location_type = 'shed'
   AND l.location_code = d.shed_code
  JOIN animal_stage_lookup s
    ON s.tenant_id = l.tenant_id
   AND s.stage_code = d.stage_code
)
INSERT INTO shed_profiles (
  location_id, tenant_id, animal_stage_id, sex, capacity, has_icu, notes, context
)
SELECT
  location_id, tenant_id, animal_stage_id, sex, capacity, has_icu, notes,
  jsonb_build_object('seed', 'google-dev-clean-slate-vaccination', 'fixture', 'post-import-shed-profiles.sql')
FROM resolved
ON CONFLICT (location_id) DO UPDATE
SET animal_stage_id = EXCLUDED.animal_stage_id,
    sex = EXCLUDED.sex,
    capacity = EXCLUDED.capacity,
    has_icu = EXCLUDED.has_icu,
    notes = EXCLUDED.notes,
    context = EXCLUDED.context,
    updated_at = now(),
    row_version = shed_profiles.row_version + 1;

WITH desired (
  shed_code, usable_for_vaccination, is_quarantine, is_icu, notes, display_order
) AS (
  VALUES
    ('GDEV-CBE-K1-A', true, false, false, 'google-dev sample K1 vaccination shed', 10),
    ('GDEV-CBE-K0-HOLD', true, false, false, 'google-dev age-ineligible newborn hold', 20),
    ('GDEV-CBE-K2-B', true, false, false, 'google-dev shed-profile mismatch test', 30),
    ('GDEV-CBE-NOVAX', false, false, false, 'google-dev location-ineligible vaccination test', 40),
    ('GDEV-CBE-QUAR', false, true, false, 'google-dev quarantine defer test', 50),
    ('GDEV-CPT-K1-A', true, false, false, 'google-dev shift target', 60),
    ('GDEV-CPT-RECOVERY', true, false, false, 'google-dev recovery validation', 70)
),
resolved AS (
  SELECT l.tenant_id, l.location_id, d.*
  FROM desired d
  JOIN locations l
    ON l.tenant_id = :'tenant_id'::uuid
   AND l.location_type = 'shed'
   AND l.location_code = d.shed_code
)
INSERT INTO location_operational_attributes (
  tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination,
  usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes, updated_at
)
SELECT
  tenant_id, location_id, true, false, usable_for_vaccination, true, false,
  is_quarantine, is_icu, display_order, notes, now()
FROM resolved
ON CONFLICT (location_id) DO UPDATE
SET usable_for_counts = EXCLUDED.usable_for_counts,
    usable_for_feed = EXCLUDED.usable_for_feed,
    usable_for_vaccination = EXCLUDED.usable_for_vaccination,
    usable_for_sop = EXCLUDED.usable_for_sop,
    is_holding = EXCLUDED.is_holding,
    is_quarantine = EXCLUDED.is_quarantine,
    is_icu = EXCLUDED.is_icu,
    display_order = EXCLUDED.display_order,
    notes = EXCLUDED.notes,
    updated_at = now();

SELECT
  CASE WHEN count(*) = 7 THEN 'true' ELSE 'false' END AS gdev_shed_profile_ok,
  count(*) AS gdev_shed_profile_count
FROM shed_profiles sp
JOIN locations l
  ON l.tenant_id = sp.tenant_id
 AND l.location_id = sp.location_id
WHERE sp.tenant_id = :'tenant_id'::uuid
  AND l.location_code LIKE 'GDEV-%'
\gset

\if :gdev_shed_profile_ok
\else
  \echo 'expected 7 google-dev shed profile rows but found' :gdev_shed_profile_count
  \quit 1
\endif

COMMIT;
