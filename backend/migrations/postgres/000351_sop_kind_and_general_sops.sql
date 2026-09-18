-- +goose Up
-- seed-fixture-guard:ignore: SOP registry columns plus one seeded general SOP; no vaccination/HRMS
-- seed contract, source fixture schema, or read-model change.
--
-- TWO KINDS OF SOP (maintainer instruction 2026-09-18, docs/decisions/sop-studio.md): a
-- MODULE-LEVEL SOP is owned and run by a module (birth by counts, a weighing session by weighing,
-- packing by feed); a GENERAL SOP is farm-wide, tied to no module, and started by hand. The kind
-- is a first-class column, not a naming convention. module_key names the owning module of a
-- module-level SOP and is backfilled from the code prefix the pages already slice on.
ALTER TABLE public.sop_definitions
  ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'module',
  ADD COLUMN IF NOT EXISTS module_key text NOT NULL DEFAULT '';
ALTER TABLE public.sop_definitions DROP CONSTRAINT IF EXISTS sop_definitions_kind_check;
ALTER TABLE public.sop_definitions
  ADD CONSTRAINT sop_definitions_kind_check CHECK (kind IN ('module', 'general'));
UPDATE public.sop_definitions
SET module_key = CASE
  WHEN code LIKE 'counts.%' OR code = 'shifting' THEN 'counts'
  WHEN code LIKE 'feed.%' THEN 'feed'
  WHEN code LIKE 'milk.%' THEN 'milk'
  WHEN code = 'weighing' OR code LIKE 'weighing.%' THEN 'weighing'
  WHEN code LIKE 'procurement.%' THEN 'procurement'
  WHEN code LIKE 'vaccination.%' OR code LIKE 'vaccine.%' OR code LIKE 'vacc%' THEN 'vaccination'
  ELSE module_key END
WHERE module_key = '';
UPDATE public.sop_definitions SET kind = 'general' WHERE code LIKE 'general.%';

-- A general SOP run has no animal: the workflow's subject goat becomes optional. Every existing
-- row keeps its goat; the unique (tenant, template_key, subject_goat_id) index ignores NULLs, and
-- a general run is keyed by subject_ref_id instead (one fresh id per start).
ALTER TABLE public.workflow_instances ALTER COLUMN subject_goat_id DROP NOT NULL;
-- A general run's template key is "general:<sop code>" -- one per general SOP, so the closed list
-- becomes a shape check for that prefix beside the fixed herd-operations keys.
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_template_key_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_template_key_check
  CHECK (template_key IN ('birth_kid', 'birth_mother', 'death', 'reconcile', 'shifting') OR template_key ~ '^general:general\.[a-z][a-z0-9_]*$');
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_module_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_module_check
  CHECK (module IN ('birth', 'death', 'reconcile', 'shifting', 'general'));

-- The first general SOP: the gate visitor check, published v1 for every tenant, carrying an
-- answer-driven branch (footwear disinfection and overshoes only for a visitor who has been on
-- another livestock farm). The document is embedded verbatim from tasks/domain/sopseed
-- (pinned by TestMigrationEmbedsTheGeneralSeed).
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'general.gate_visitor_check', 'Gate visitor check',
       'What the gate does before any visitor walks in: who they are, where they have been, and the biosecurity steps a visit from another farm needs.',
       'active', 'action', 'general', ''
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Gate visitor check v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'general.gate_visitor_check',
         'title', 'Gate visitor check',
         'fields', jsonb_build_array(),
         'follow_up', $seed${
  "schema_version": "goatos.sop-followup.v1",
  "tracks": [
    {
      "key": "main", "module": "general", "label": "Gate visitor check", "subject": "visit",
      "steps": [
        {"key": "visitor_name", "task_type": "record_text", "title": "Who is visiting?", "detail": "Record the visitor's name and where they have come from.", "proof": {}, "schedule": {"kind": "immediately"}},
        {"key": "from_other_farm", "task_type": "record_yes_no", "title": "Has the visitor been on another livestock farm this week?", "detail": "Ask before the visitor enters. Answer Yes if they have been near other goats, sheep or cattle in the last seven days.", "proof": {}, "schedule": {"kind": "immediately"}},
        {"key": "disinfect_footwear", "task_type": "video_record", "title": "Disinfect the visitor's footwear", "detail": "Walk the visitor through the footbath and record one video of both boots being dipped.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}, "when_answer": {"step": "from_other_farm", "op": "eq", "value": ["yes"]}},
        {"key": "issue_overshoes", "task_type": "do_and_confirm", "title": "Issue farm overshoes", "detail": "Hand the visitor a pair of farm overshoes and confirm they are wearing them.", "proof": {"photo": 1}, "schedule": {"kind": "immediately"}, "when_answer": {"step": "from_other_farm", "op": "eq", "value": ["yes"]}},
        {"key": "log_visit", "task_type": "do_and_confirm", "title": "Log the visit at the gate", "detail": "Enter the visitor in the gate register with the time in and the pens they will see.", "proof": {}, "schedule": {"kind": "immediately"}}
      ]
    }
  ]
}$seed$::jsonb
       ),
       '{"subject_scope": "task", "types": ["video", "photo"], "required": false, "minimum_count": 0, "verify_before_apply": false, "approval_before_apply": false}'::jsonb,
       '{"min_app_version": "0.2.0", "supported_field_types": ["boolean", "select", "multiselect", "number", "text", "video_proof", "photo_proof"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded general SOP (migration 000351)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'general.gate_visitor_check'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
DELETE FROM public.sop_versions sv USING public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'general.gate_visitor_check';
DELETE FROM public.sop_definitions WHERE code = 'general.gate_visitor_check';
ALTER TABLE public.sop_definitions DROP CONSTRAINT IF EXISTS sop_definitions_kind_check;
ALTER TABLE public.sop_definitions DROP COLUMN IF EXISTS kind, DROP COLUMN IF EXISTS module_key;
