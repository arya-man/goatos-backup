-- +goose Up
-- seed-fixture-guard:ignore: two nullable-default columns on the feed purchase ledger plus one
-- seeded module SOP; no vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- THE FEED PURCHASE FORM IS AUTHORED (maintainer decision 2026-09-20: procurement SOP-driven end
-- to end -- "what questions, what to render, what type of details we need, feed purchase"). What
-- the Record purchase screens ask was hard-coded three ways: the Go write, the web drawer and the
-- phone. It is now `form_dsl.feed_purchase_form` of the published `procurement.feed_purchase_form`
-- SOP, served to both screens per request and checked against the exact version the client
-- rendered.
--
-- The ledger's own columns stay TYPED questions with locked ids -- the stock cards, the landed
-- rate and the aflatoxin task all read them -- and everything the farm adds is stored here with
-- the version it was answered on.

ALTER TABLE public.feed_purchases
  ADD COLUMN IF NOT EXISTS sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS questionnaire_version integer NULL;

INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'procurement.feed_purchase_form', 'Feed purchase form',
       'What is asked when a feed load is recorded: the pages, the questions, and which are compulsory.',
       'active', 'action', 'module', 'procurement'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Feed purchase form v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'procurement.feed_purchase_form',
         'title', 'Feed purchase form',
         'fields', jsonb_build_array(),
         'feed_purchase_form', $seed${
  "schema_version": "goatos.sop-feed-purchase-form.v1",
  "pages": [
    {"key": "load", "title": "The load", "questions": [
      {"id": "purchase_date", "kind": "text", "title": "Purchase date", "hint": "The day the money was committed, not the day it arrives.", "required": true},
      {"id": "farm_label", "kind": "choice", "title": "Farm", "required": true, "catalog": "farm"},
      {"id": "feed_item_label", "kind": "choice", "title": "Feed", "required": true, "catalog": "feed_item"},
      {"id": "quantity_kg", "kind": "number", "title": "Quantity bought", "required": true, "min": 0, "unit": "kg"},
      {"id": "vendor", "kind": "text", "title": "Vendor", "required": true}
    ]},
    {"key": "cost", "title": "What it cost", "questions": [
      {"id": "feed_cost", "kind": "number", "title": "Feed", "required": false, "min": 0, "unit": "₹"},
      {"id": "transport_cost", "kind": "number", "title": "Transport", "required": false, "min": 0, "unit": "₹"},
      {"id": "loading_cost", "kind": "number", "title": "Loading", "required": false, "min": 0, "unit": "₹"},
      {"id": "unloading_cost", "kind": "number", "title": "Unloading", "required": false, "min": 0, "unit": "₹"},
      {"id": "total_cost", "kind": "number", "title": "Total", "hint": "Leave blank to add the parts up.", "required": false, "min": 0, "unit": "₹"}
    ]},
    {"key": "money", "title": "The money", "questions": [
      {"id": "payment_released", "kind": "number", "title": "Paid so far", "required": false, "min": 0, "unit": "₹"},
      {"id": "payment_status", "kind": "choice", "title": "Payment", "required": false, "catalog": "payment_status"}
    ]},
    {"key": "arrival", "title": "If it has already arrived", "questions": [
      {"id": "reached_on", "kind": "text", "title": "Reached on", "hint": "Leave blank while the truck is still on the road.", "required": false},
      {"id": "reached_weight_kg", "kind": "number", "title": "Weight received", "required": false, "min": 0, "unit": "kg"}
    ]}
  ]
}$seed$::jsonb
       ),
       '{"subject_scope": "task", "types": ["photo", "video"], "required": false, "minimum_count": 0, "verify_before_apply": false, "approval_before_apply": false}'::jsonb,
       '{"min_app_version": "0.2.0", "supported_field_types": ["boolean", "select", "multiselect", "number", "text"], "supported_proof_actions": [], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded feed purchase form (migration 000376)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'procurement.feed_purchase_form'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
DELETE FROM public.sop_versions sv USING public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'procurement.feed_purchase_form';
DELETE FROM public.sop_definitions WHERE code = 'procurement.feed_purchase_form';
ALTER TABLE public.feed_purchases
  DROP COLUMN IF EXISTS sop_answers,
  DROP COLUMN IF EXISTS questionnaire_version;
