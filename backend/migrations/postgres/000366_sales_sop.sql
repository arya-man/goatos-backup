-- +goose Up
-- seed-fixture-guard:ignore: SOP engine columns plus one seeded module SOP and one task type;
-- no vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- SALES SOP (maintainer instruction 2026-09-19, docs/decisions/sales-sop.md). What happens after
-- a sale is recorded -- tag the animals, load them, settle the money, and whatever the farm adds
-- -- was hard-coded on the phone: the steps, their order, and who may do each. It is now the
-- published `sales.deal` SOP, authored on /sales/sops with the same List | Flow editor the herd
-- operations use, and RUN by the tasks engine: recording a sale opens one workflow keyed on the
-- deal (subject_ref_id, no animal) from the SOP's `sales_deal` track, pinned to the version in
-- force. Each step carries the DESIGNATION that does it.

-- 1. Who does a step is authored. Stamped at open from the SOP's step owner; blank = anyone who
--    can open the workflow. Read on every write (StepOwnedBy) and served to the phone.
ALTER TABLE public.workflow_actions
  ADD COLUMN IF NOT EXISTS owner_role text NOT NULL DEFAULT '';

-- 2. The sale workflow's template key and module join the closed lists beside the general shape.
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_template_key_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_template_key_check
  CHECK (template_key IN ('birth_kid', 'birth_mother', 'death', 'reconcile', 'shifting', 'sales_deal') OR template_key ~ '^general:general\.[a-z][a-z0-9_]*$');
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_module_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_module_check
  CHECK (module IN ('birth', 'death', 'reconcile', 'shifting', 'general', 'sales'));

-- 3. One new Task Type Registry row, for every tenant: the sale's tag-animals step, whose engine
--    hook completes it from the allocation confirm. Embedded verbatim from
--    tasks/domain/sopseed/task_types_sales.json (pinned by TestMigrationEmbedsTheSalesSeed);
--    000308's task_types.json is already applied and is never edited.
INSERT INTO public.sop_task_types (tenant_id, task_type_key, name, description, category_scope, answer_kind, engine_hook, parameter_schema, sort_order)
SELECT t.tenant_id, x.key, x.name, x.description, x.category_scope, x.answer_kind, x.engine_hook, x.parameter_schema, 100 + e.ordinality
FROM public.tenants t
CROSS JOIN LATERAL jsonb_array_elements($seed$[
  {"key": "sale_tag_animals", "name": "Tag the animals sold", "category_scope": ["action"], "answer_kind": "none", "engine_hook": "sale_tag_animals", "description": "Opens the sale-tagging screen; the step completes on its own when the tagging is confirmed, never by hand.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}}
]$seed$::jsonb) WITH ORDINALITY AS e(row, ordinality)
CROSS JOIN LATERAL jsonb_to_record(e.row) AS x(key text, name text, description text, category_scope jsonb, answer_kind text, engine_hook text, parameter_schema jsonb)
ON CONFLICT (tenant_id, task_type_key) DO NOTHING;

-- 4. The sale SOP, published v1 for every tenant, mirroring what the phone ran hard-coded plus the
--    two tasks the farm did not have (loading video, gate pass). Embedded verbatim from
--    tasks/domain/sopseed/sales_deal.json (pinned by TestMigrationEmbedsTheSalesSeed).
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'sales.deal', 'Sale',
       'What happens after a sale is recorded: tagging the animals, loading them, the money -- and who does each step.',
       'active', 'action', 'module', 'sales'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Sale v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'sales.deal',
         'title', 'Sale',
         'fields', jsonb_build_array(),
         'follow_up', $seed${
  "schema_version": "goatos.sop-followup.v1",
  "tracks": [
    {
      "key": "sales_deal", "module": "sales", "label": "Sale", "subject": "sale",
      "steps": [
        {"key": "tag_animals", "task_type": "sale_tag_animals", "title": "Tag the animals sold", "detail": "Scan or type the tag of every animal on this sale, enter its weight, and confirm. This step completes on its own once the tagging is confirmed.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "park_head"},
        {"key": "loading_video", "task_type": "video_record", "title": "Record the animals being loaded", "detail": "One live video of the tagged animals walking onto the buyer's vehicle, ear tags visible.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}, "owner": "park_head"},
        {"key": "dispatch_note", "task_type": "photo_record", "title": "Photo of the gate pass", "detail": "One photo of the signed gate pass or dispatch note handed to the buyer's driver.", "proof": {"photo": 1}, "schedule": {"kind": "immediately"}, "owner": "park_head"},
        {"key": "full_payment", "task_type": "record_yes_no", "title": "Has the buyer paid in full?", "detail": "Answer Yes when the receipts on the sale cover its value. Answer No if a balance is still due.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "procurement_director"},
        {"key": "collect_balance", "task_type": "do_and_confirm", "title": "Collect the balance and record the receipt", "detail": "Follow up with the buyer for the amount still due and record each receipt on the sale as it comes in.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "procurement_director", "when_answer": {"step": "full_payment", "op": "eq", "value": ["no"]}}
      ]
    }
  ]
}$seed$::jsonb
       ),
       '{"subject_scope": "task", "types": ["video", "photo"], "required": false, "minimum_count": 0, "verify_before_apply": false, "approval_before_apply": false}'::jsonb,
       '{"min_app_version": "0.2.0", "supported_field_types": ["boolean", "select", "multiselect", "number", "text", "video_proof", "photo_proof"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded sale SOP (migration 000366)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'sales.deal'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- 5. The Sales SOP page (/sales/sops, key sales-sops) reaches everyone already holding the Sales
--    web module with explicit page ticks -- the 000357 shape, additive only. An empty tick list
--    already means every page; the CEO floor needs no row.
-- seed-migration-guard:ignore owner=manohark issue=sales-sop reason=additive-page-tick-for-existing-sales-module-holders expiry=2026-12-31
UPDATE public.person_module_access
   SET pages = array_append(pages, 'sales-sops')
 WHERE surface = 'web'
   AND module_key = 'sales'
   AND pages IS NOT NULL
   AND array_length(pages, 1) > 0
   AND NOT ('sales-sops' = ANY (pages));
-- seed-migration-guard:ignore owner=manohark issue=sales-sop reason=additive-page-tick-for-designation-templates expiry=2026-12-31
UPDATE public.designation_module_defaults
   SET pages = array_append(pages, 'sales-sops')
 WHERE surface = 'web'
   AND module_key = 'sales'
   AND pages IS NOT NULL
   AND array_length(pages, 1) > 0
   AND NOT ('sales-sops' = ANY (pages));

-- +goose Down
UPDATE public.person_module_access SET pages = array_remove(pages, 'sales-sops')
 WHERE surface = 'web' AND module_key = 'sales' AND 'sales-sops' = ANY (pages);
UPDATE public.designation_module_defaults SET pages = array_remove(pages, 'sales-sops')
 WHERE surface = 'web' AND module_key = 'sales' AND 'sales-sops' = ANY (pages);
DELETE FROM public.workflow_actions wa USING public.workflow_instances w
WHERE w.tenant_id = wa.tenant_id AND w.workflow_id = wa.workflow_id AND w.module = 'sales';
DELETE FROM public.workflow_instances WHERE module = 'sales';
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_template_key_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_template_key_check
  CHECK (template_key IN ('birth_kid', 'birth_mother', 'death', 'reconcile', 'shifting') OR template_key ~ '^general:general\.[a-z][a-z0-9_]*$');
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_module_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_module_check
  CHECK (module IN ('birth', 'death', 'reconcile', 'shifting', 'general'));
DELETE FROM public.sop_versions sv USING public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'sales.deal';
DELETE FROM public.sop_definitions WHERE code = 'sales.deal';
DELETE FROM public.sop_task_types WHERE task_type_key = 'sale_tag_animals';
ALTER TABLE public.workflow_actions DROP COLUMN IF EXISTS owner_role;
