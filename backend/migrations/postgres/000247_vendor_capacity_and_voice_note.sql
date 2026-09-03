-- +goose Up
-- seed-fixture-guard:ignore: additive vendor-register columns and catalog vocabulary; no
-- vaccination/HRMS seed contract change.
--
-- VENDOR CAPACITY, SUPPLY FREQUENCY AND VOICE NOTE (maintainer decision 2026-09-03, with the
-- Vendors phone module).
--
-- The register said WHO a vendor is and WHAT they deal in, never HOW MUCH they can supply or HOW
-- OFTEN. The procurement desk plans loads against exactly those two facts ("5,000 kg every two
-- weeks", "300 animals once"), so a vendor now carries:
--
--   capacity_quantity   how much per delivery, in capacity_unit
--   capacity_unit       kg / tonnes / animals / litres / bags -- a catalog vocabulary
--   supply_frequency    how often that capacity is available -- a catalog vocabulary
--   voice_note_proof_ref an audio note recorded on the phone (proof_artifacts, proof_type 'audio')
--
-- Quantity and unit travel together: a number with no unit is not a capacity, and a unit with no
-- number says nothing, so the CHECK below refuses one without the other. Frequency is independent
-- (a vendor may have a known capacity and an unknown cadence).
--
-- Both vocabularies are catalog DATA, like record_type, per the AGENTS.md rule that business
-- vocabularies come from Postgres. Values are stable keys the app stores; labels are what the
-- screens render, so the farm can reword "Every 2 weeks" without a deploy. Seeded for every
-- tenant, the 000217 shape.
ALTER TABLE public.procurement_vendors
  ADD COLUMN IF NOT EXISTS capacity_quantity     numeric(14, 3),
  ADD COLUMN IF NOT EXISTS capacity_unit         text,
  ADD COLUMN IF NOT EXISTS supply_frequency      text,
  ADD COLUMN IF NOT EXISTS voice_note_proof_ref  uuid;

ALTER TABLE public.procurement_vendors
  DROP CONSTRAINT IF EXISTS procurement_vendors_capacity_check,
  ADD CONSTRAINT procurement_vendors_capacity_check
    CHECK (
      (capacity_quantity IS NULL AND capacity_unit IS NULL)
      OR (capacity_quantity > 0 AND btrim(coalesce(capacity_unit, '')) <> '')
    );

ALTER TABLE public.procurement_vendor_catalog
  DROP CONSTRAINT IF EXISTS procurement_vendor_catalog_kind_check,
  ADD CONSTRAINT procurement_vendor_catalog_kind_check
    CHECK (kind = ANY (ARRAY['record_type'::text, 'breed'::text, 'state'::text, 'city'::text,
                             'status'::text, 'feed'::text, 'capacity_unit'::text, 'supply_frequency'::text]));

INSERT INTO public.procurement_vendor_catalog (tenant_id, kind, value, label, sort_order, is_active)
SELECT t.tenant_id, 'capacity_unit', v.value, v.label, v.sort_order, true
FROM public.tenants t
CROSS JOIN (VALUES
    ('kg',      'kg',      1),
    ('tonnes',  'tonnes',  2),
    ('animals', 'animals', 3),
    ('litres',  'litres',  4),
    ('bags',    'bags',    5)
) AS v(value, label, sort_order)
ON CONFLICT (tenant_id, kind, value) DO UPDATE
SET label = EXCLUDED.label, sort_order = EXCLUDED.sort_order, is_active = true, updated_at = now();

INSERT INTO public.procurement_vendor_catalog (tenant_id, kind, value, label, sort_order, is_active)
SELECT t.tenant_id, 'supply_frequency', v.value, v.label, v.sort_order, true
FROM public.tenants t
CROSS JOIN (VALUES
    ('per_week',     'Every week',     1),
    ('per_2_weeks',  'Every 2 weeks',  2),
    ('per_month',    'Every month',    3),
    ('per_3_months', 'Every 3 months', 4),
    ('one_time',     'One time',       5)
) AS v(value, label, sort_order)
ON CONFLICT (tenant_id, kind, value) DO UPDATE
SET label = EXCLUDED.label, sort_order = EXCLUDED.sort_order, is_active = true, updated_at = now();

COMMENT ON COLUMN public.procurement_vendors.capacity_quantity IS
  'How much the vendor can supply per delivery, in capacity_unit. NULL with capacity_unit NULL = not recorded.';
COMMENT ON COLUMN public.procurement_vendors.supply_frequency IS
  'How often that capacity is available: a procurement_vendor_catalog value of kind supply_frequency.';
COMMENT ON COLUMN public.procurement_vendors.voice_note_proof_ref IS
  'An audio note about this vendor recorded on the phone: proof_artifacts.proof_id of a completed audio proof in the same tenant.';

-- +goose Down
ALTER TABLE public.procurement_vendors
  DROP CONSTRAINT IF EXISTS procurement_vendors_capacity_check,
  DROP COLUMN IF EXISTS voice_note_proof_ref,
  DROP COLUMN IF EXISTS supply_frequency,
  DROP COLUMN IF EXISTS capacity_unit,
  DROP COLUMN IF EXISTS capacity_quantity;
DELETE FROM public.procurement_vendor_catalog WHERE kind IN ('capacity_unit', 'supply_frequency');
ALTER TABLE public.procurement_vendor_catalog
  DROP CONSTRAINT IF EXISTS procurement_vendor_catalog_kind_check,
  ADD CONSTRAINT procurement_vendor_catalog_kind_check
    CHECK (kind = ANY (ARRAY['record_type'::text, 'breed'::text, 'state'::text, 'city'::text, 'status'::text, 'feed'::text]));
