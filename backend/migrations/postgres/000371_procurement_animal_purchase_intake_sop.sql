-- +goose Up
-- seed-fixture-guard:ignore: two closed-list CHECK widenings plus one seeded module SOP and one
-- task type; no vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- PROCUREMENT IS SOP-DRIVEN END TO END (maintainer decision 2026-09-20). The animal-purchase
-- QUESTIONS were already authored (000307); what the desk and the farm actually DO with a load --
-- record the animals, get the office's decision, receive them at the farm -- was nowhere at all:
-- no step, no owner, no proof that the animals arrived. It is now the published
-- `procurement.animal_purchase_intake` SOP, authored on /procurement/sops with the same
-- List | Flow editor the herd operations and the sale use, and RUN by the tasks engine: opening a
-- purchase load opens one workflow keyed on the load (subject_ref_id, no animal) from the SOP's
-- `animal_purchase_intake` track, pinned to the version in force. Each step carries the
-- DESIGNATION that does it.
--
-- STEPS AND FORMS ARE SEPARATE DOCUMENTS, deliberately: `procurement.animal_purchase` keeps the
-- inspection and load forms, this SOP keeps the steps. That is the shape the sale already has
-- (`sales.deal` beside `sales.vendor`), and it is what lets each be edited without republishing
-- the other.

-- 1. The intake workflow's template key and module join the closed lists.
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_template_key_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_template_key_check
  CHECK (template_key IN ('birth_kid', 'birth_mother', 'death', 'reconcile', 'shifting', 'sales_deal', 'animal_purchase_intake') OR template_key ~ '^general:general\.[a-z][a-z0-9_]*$');
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_module_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_module_check
  CHECK (module IN ('birth', 'death', 'reconcile', 'shifting', 'general', 'sales', 'procurement'));

-- 2. One new Task Type Registry row, for every tenant: the office's decision step, whose engine
--    hook completes it when the load has no animal still waiting. Embedded verbatim from
--    tasks/domain/sopseed/task_types_procurement.json (pinned by
--    TestMigrationEmbedsTheProcurementSeed); the earlier task-type files are already applied and
--    are never edited.
INSERT INTO public.sop_task_types (tenant_id, task_type_key, name, description, category_scope, answer_kind, engine_hook, parameter_schema, sort_order)
SELECT t.tenant_id, x.key, x.name, x.description, x.category_scope, x.answer_kind, x.engine_hook, x.parameter_schema, 110 + e.ordinality
FROM public.tenants t
CROSS JOIN LATERAL jsonb_array_elements($seed$[
  {
    "key": "animal_purchase_decision",
    "name": "Decision on every animal",
    "category_scope": ["action", "commodity"],
    "answer_kind": "none",
    "engine_hook": "animal_purchase_decision",
    "description": "The office's accept / reject on the animals recorded in a purchase load. Completed by the engine when no animal in the load is still waiting for a decision — never by a tap, so a load cannot read \"decided\" while an animal sits unanswered.",
    "parameter_schema": {"type": "object", "properties": {}}
  }
]$seed$::jsonb) WITH ORDINALITY AS e(row, ordinality)
CROSS JOIN LATERAL jsonb_to_record(e.row) AS x(key text, name text, description text, category_scope jsonb, answer_kind text, engine_hook text, parameter_schema jsonb)
ON CONFLICT (tenant_id, task_type_key) DO NOTHING;

-- 3. The intake SOP, published v1 for every tenant. Embedded verbatim from
--    tasks/domain/sopseed/procurement_animal_purchase_intake.json.
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'procurement.animal_purchase_intake', 'Animal purchase intake',
       'What happens once a purchase load is opened: recording the animals, the office decision, and receiving them at the farm -- and who does each step.',
       'active', 'action', 'module', 'procurement'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Animal purchase intake v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'procurement.animal_purchase_intake',
         'title', 'Animal purchase intake',
         'fields', jsonb_build_array(),
         'follow_up', $seed${
  "schema_version": "goatos.sop-followup.v1",
  "tracks": [
    {
      "key": "animal_purchase_intake", "module": "procurement", "label": "Animal purchase", "subject": "load",
      "steps": [
        {"key": "record_animals", "task_type": "do_and_confirm", "title": "Record the animals in this load", "detail": "Open the load and record every animal being bought, answering the inspection the SOP asks for and taking each capture it names. Confirm here once the last animal is recorded.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "procurement_director"},
        {"key": "loading_photo", "task_type": "photo_record", "title": "Photo of the animals loaded", "detail": "One photo of the animals on the vehicle before it leaves the seller.", "proof": {"photo": 1}, "schedule": {"kind": "immediately"}, "owner": "procurement_director"},
        {"key": "decision", "task_type": "animal_purchase_decision", "title": "Decision on every animal", "detail": "The office accepts or rejects each recorded animal. This step completes on its own once no animal in the load is still waiting.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "ceo_internal"},
        {"key": "arrival_video", "task_type": "video_record", "title": "Record the animals arriving", "detail": "One live video of the animals coming off the vehicle at the farm.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}, "owner": "park_head"},
        {"key": "place_in_pen", "task_type": "do_and_confirm", "title": "Unload and place the animals in their pen", "detail": "Walk the animals into the pen they will stay in and confirm once they are all in.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "park_head"},
        {"key": "arrival_condition", "task_type": "record_yes_no", "title": "Was any animal hurt or sick on arrival?", "detail": "Answer Yes if any animal came off the vehicle injured, lame or unwell.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "park_head"},
        {"key": "tell_health_team", "task_type": "do_and_confirm", "title": "Tell the health team which animals arrived unwell", "detail": "Name the animals and what you saw, so the health desk can look at them today.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "park_head", "when_answer": {"step": "arrival_condition", "op": "eq", "value": ["yes"]}}
      ]
    }
  ]
}$seed$::jsonb
       ),
       '{"subject_scope": "task", "types": ["video", "photo"], "required": false, "minimum_count": 0, "verify_before_apply": false, "approval_before_apply": false}'::jsonb,
       '{"min_app_version": "0.2.0", "supported_field_types": ["boolean", "select", "multiselect", "number", "text", "video_proof", "photo_proof"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded animal purchase intake SOP (migration 000371)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'procurement.animal_purchase_intake'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
DELETE FROM public.workflow_actions wa USING public.workflow_instances w
WHERE w.tenant_id = wa.tenant_id AND w.workflow_id = wa.workflow_id AND w.module = 'procurement';
DELETE FROM public.workflow_instances WHERE module = 'procurement';
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_template_key_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_template_key_check
  CHECK (template_key IN ('birth_kid', 'birth_mother', 'death', 'reconcile', 'shifting', 'sales_deal') OR template_key ~ '^general:general\.[a-z][a-z0-9_]*$');
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_module_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_module_check
  CHECK (module IN ('birth', 'death', 'reconcile', 'shifting', 'general', 'sales'));
DELETE FROM public.sop_versions sv USING public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'procurement.animal_purchase_intake';
DELETE FROM public.sop_definitions WHERE code = 'procurement.animal_purchase_intake';
DELETE FROM public.sop_task_types WHERE task_type_key = 'animal_purchase_decision';
