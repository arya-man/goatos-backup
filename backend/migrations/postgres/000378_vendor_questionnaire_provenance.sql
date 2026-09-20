-- +goose Up
-- All pre-split vendor forms used sales.vendor, including supplier records.
ALTER TABLE public.procurement_vendors ADD COLUMN questionnaire_sop_code text NOT NULL DEFAULT 'sales.vendor'
CHECK (questionnaire_sop_code IN ('sales.vendor', 'procurement.vendor', ''));

-- Preserve the form suppliers were actually seeing at upgrade, when the split's
-- procurement v1 remains the untouched seed. Never replace an authored supplier form.
UPDATE public.sop_versions supplier
SET form_dsl = jsonb_set(supplier.form_dsl, '{vendor_form}', source.form_dsl->'vendor_form')
FROM public.sop_definitions supply_def,
     public.sop_definitions sales_def,
     public.sop_versions source
WHERE supply_def.tenant_id = supplier.tenant_id AND supply_def.sop_id = supplier.sop_id
  AND supply_def.code = 'procurement.vendor' AND supplier.version = 1
  AND supplier.validation_report->'warnings' @> '[{"code":"seeded","message":"Seeded supplier form (migration 000370)."}]'::jsonb
  AND sales_def.tenant_id = supply_def.tenant_id AND sales_def.code = 'sales.vendor'
  AND source.tenant_id = sales_def.tenant_id AND source.sop_id = sales_def.sop_id
  AND source.status = 'published' AND source.form_dsl ? 'vendor_form'
  AND source.version = (SELECT max(v.version) FROM public.sop_versions v
    WHERE v.tenant_id = sales_def.tenant_id AND v.sop_id = sales_def.sop_id AND v.status = 'published');

-- +goose Down
ALTER TABLE public.procurement_vendors DROP COLUMN questionnaire_sop_code;
