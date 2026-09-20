-- +goose Up
-- seed-fixture-guard:ignore: one seeded module SOP row plus its published v1; no vaccination/HRMS
-- seed contract, source fixture schema, or read-model change.
--
-- THE SUPPLY REGISTER GETS ITS OWN VENDOR FORM (maintainer decision 2026-09-20: "split:
-- procurement.vendor for suppliers"). One register, two documents. Until now both halves of
-- procurement_vendors rendered `sales.vendor`, so a question the buying desk needed of a feed
-- supplier also appeared in front of the sales desk asking a butcher -- and a form that asks
-- everyone everything is a form nobody fills. The SIDE a record type belongs to
-- (procurement_vendor_catalog.register_side) decides which document is rendered and which one a
-- write is checked against; that is the register's own data, never a client claim.
--
-- Day one is exactly today: the seeded supply document is the SAME document the supply side
-- already rendered (procurement/domain/vendorformseed/vendor.json, embedded verbatim and pinned
-- by TestMigrationEmbedsTheSeededProcurementVendorForm), so nothing on any screen moves until
-- somebody edits it on Procurement > Procurement SOP.

INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'procurement.vendor', 'Supplier form',
       'What is asked when a supplier is added or edited on the buying side of the vendor register: the pages, the questions, and which are compulsory.',
       'active', 'action', 'module', 'procurement'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Supplier form v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'procurement.vendor',
         'title', 'Supplier form',
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
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded supplier form (migration 000370)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'procurement.vendor'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
DELETE FROM public.sop_versions sv USING public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'procurement.vendor';
DELETE FROM public.sop_definitions WHERE code = 'procurement.vendor';
