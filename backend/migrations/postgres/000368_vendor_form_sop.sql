-- +goose Up
-- seed-fixture-guard:ignore: two nullable-default columns on the vendor register plus one seeded
-- module SOP; no vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- VENDOR FORM IS AUTHORED (maintainer instruction 2026-09-19, docs/decisions/sales-sop.md ->
-- "The vendor form"). What the Add / Edit vendor screens ask -- which questions, on which page,
-- compulsory or not, and any question the farm adds tomorrow -- was hard-coded on the phone and
-- in the web drawer. It is now `form_dsl.vendor_form` of the published `sales.vendor` SOP,
-- authored on /sales/sops and served to both screens per request. The register's own columns
-- stay typed questions with locked ids; every other answer is stored here with the version it
-- was answered on.

ALTER TABLE public.procurement_vendors
  ADD COLUMN IF NOT EXISTS sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS questionnaire_version integer NULL;

-- The vendor SOP, published v1 for every tenant, mirroring the three pages the phone ran
-- hard-coded. Embedded verbatim from procurement/domain/vendorformseed/vendor.json (pinned by
-- TestMigrationEmbedsTheSeededVendorForm).
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'sales.vendor', 'Vendor form',
       'What is asked when a vendor or buyer is added or edited: the pages, the questions, and which are compulsory.',
       'active', 'action', 'module', 'sales'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Vendor form v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'sales.vendor',
         'title', 'Vendor form',
         'fields', jsonb_build_array(),
         'vendor_form', $seed${
  "schema_version": "goatos.sop-vendor-form.v1",
  "pages": [
    {"key": "who", "title": "Who they are", "questions": [
      {"id": "business_name", "kind": "text", "title": "Business or person name", "required": true},
      {"id": "record_type", "kind": "choice", "title": "Type", "required": true, "catalog": "record_type"},
      {"id": "contact_person_name", "kind": "text", "title": "Contact person", "required": true},
      {"id": "phone_number", "kind": "text", "title": "Phone", "required": true},
      {"id": "state", "kind": "choice", "title": "State", "required": true, "catalog": "state"},
      {"id": "city", "kind": "text", "title": "City", "required": true},
      {"id": "status", "kind": "choice", "title": "Status", "required": true, "catalog": "status"}
    ]},
    {"key": "supply", "title": "What they supply or buy", "questions": [
      {"id": "capacity_quantity", "kind": "number", "title": "Capacity", "hint": "How much they can supply or take per period.", "required": false, "min": 0},
      {"id": "capacity_unit", "kind": "choice", "title": "Capacity unit", "required": false, "catalog": "capacity_unit"},
      {"id": "supply_frequency", "kind": "choice", "title": "Supply frequency", "required": false, "catalog": "supply_frequency"},
      {"id": "feed", "kind": "choice", "title": "Feed", "required": false, "catalog": "feed"},
      {"id": "breed", "kind": "choice", "title": "Breed", "required": false, "catalog": "breed"},
      {"id": "price_per_goat", "kind": "number", "title": "Price per goat", "required": false, "min": 0, "unit": "₹"},
      {"id": "eta_after_order_days", "kind": "number", "title": "Days to deliver after an order", "required": false, "min": 0, "unit": "days"},
      {"id": "average_animal_weight_kg", "kind": "number", "title": "Average live weight per animal", "required": false, "min": 0, "unit": "kg"}
    ]},
    {"key": "notes", "title": "Notes", "questions": [
      {"id": "comments", "kind": "text", "title": "Note", "required": false}
    ]}
  ]
}$seed$::jsonb
       ),
       '{"subject_scope": "task", "types": ["photo", "video"], "required": false, "minimum_count": 0, "verify_before_apply": false, "approval_before_apply": false}'::jsonb,
       '{"min_app_version": "0.2.0", "supported_field_types": ["boolean", "select", "multiselect", "number", "text"], "supported_proof_actions": [], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded vendor form (migration 000368)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'sales.vendor'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
DELETE FROM public.sop_versions sv USING public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'sales.vendor';
DELETE FROM public.sop_definitions WHERE code = 'sales.vendor';
ALTER TABLE public.procurement_vendors
  DROP COLUMN IF EXISTS sop_answers,
  DROP COLUMN IF EXISTS questionnaire_version;
