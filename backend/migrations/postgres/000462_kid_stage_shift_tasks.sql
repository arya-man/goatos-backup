-- +goose Up
-- seed-fixture-guard:ignore: adds a track to the published counts.birth SOP document and one task
-- type row; no vaccination/HRMS seed contract or source fixture schema changes.
--
-- KID STAGE SHIFT TASKS (maintainer decision 2026-09-30, docs/decisions/kid-stage-shift-tasks.md).
-- A birth now owes the park head two moves, authored as the Birth SOP's new `birth_litter` track:
-- the litter K0 -> K1 24 hours after birth, and K1 -> K2 seven days after the litter reached K1.
-- ONE workflow per litter (subject_ref_id = goat_births.birth_event_id). Both steps are
-- engine-completed from the herd register (goat.stage_changed), never by a tap.

-- 1. The litter workflow's template key.
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_template_key_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_template_key_check
  CHECK (template_key IN ('birth_kid', 'birth_mother', 'birth_litter', 'death', 'reconcile', 'shifting', 'sales_deal', 'animal_purchase_intake', 'feed_purchase_intake') OR template_key ~ '^general:general\.[a-z][a-z0-9_]*$');

-- 2. The stage a shift step moves the kids to, stamped from the SOP at open. NULL on every other
--    step (a nullable column add is metadata-only).
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS target_stage text;

-- 3. The kid-shift task type, for every tenant. Embedded verbatim from
--    tasks/domain/sopseed/task_types_kid_shift.json (pinned by TestMigrationEmbedsTheKidShiftSeed).
INSERT INTO public.sop_task_types (tenant_id, task_type_key, name, description, category_scope, answer_kind, engine_hook, parameter_schema, sort_order)
SELECT t.tenant_id, x.key, x.name, x.description, x.category_scope, x.answer_kind, x.engine_hook, x.parameter_schema, 140 + e.ordinality
FROM public.tenants t
CROSS JOIN LATERAL jsonb_array_elements($seed$[
  {"key": "shift_kids_stage", "name": "Shift the kids to a stage", "category_scope": ["action"], "answer_kind": "none", "engine_hook": "shift_kids_stage", "description": "Opens Raise shifting (growth) for the litter's kids still waiting; the step completes on its own once every live kid has reached its target stage, never by hand.", "parameter_schema": {"type": "object", "properties": {"target_stage": {"type": "string"}}, "required": ["target_stage"]}}
]$seed$::jsonb) WITH ORDINALITY AS e(row, ordinality)
CROSS JOIN LATERAL jsonb_to_record(e.row) AS x(key text, name text, description text, category_scope jsonb, answer_kind text, engine_hook text, parameter_schema jsonb)
ON CONFLICT (tenant_id, task_type_key) DO NOTHING;

-- 4. The litter track, appended IN PLACE to each tenant's currently PUBLISHED counts.birth version
--    that does not carry one yet (the 000443 shape): the version a farm sees on deploy is the one
--    it already had, now carrying the park head's two moves. Kid and mother tracks are untouched,
--    and a workflow already open keeps the steps it started with. A tenant running the seeded
--    document gets the same track from sopseed.FollowUpTrackAddenda. Embedded verbatim from
--    tasks/domain/sopseed/counts_birth_litter_track.json (pinned by TestMigrationEmbedsTheKidShiftSeed).
-- seed-migration-guard:ignore owner=manohark issue=kid-stage-shift-tasks reason=in-place-track-on-published-birth-sop-document expiry=2026-12-31
UPDATE public.sop_versions v
SET form_dsl = jsonb_set(v.form_dsl, '{follow_up,tracks}',
      (v.form_dsl->'follow_up'->'tracks') || jsonb_build_array($seed${"key": "birth_litter", "module": "birth", "label": "Litter", "subject": "litter", "steps": [
  {"key": "shift_to_k1", "task_type": "shift_kids_stage", "title": "Shift the kids to K1", "detail": "Raise a growth shifting that moves this litter's kids from K0 into a K1 pen. This step completes on its own once every kid is on K1.", "proof": {}, "schedule": {"kind": "after_event", "offset_minutes": 1440}, "owner": "park_head", "target_stage": "K1"},
  {"key": "shift_to_k2", "task_type": "shift_kids_stage", "title": "Shift the kids to K2", "detail": "Raise a growth shifting that moves this litter's kids from K1 into a K2 pen. This step completes on its own once every kid is on K2.", "proof": {}, "schedule": {"kind": "after_step", "step": "shift_to_k1", "offset_minutes": 10080}, "owner": "park_head", "requires": ["shift_to_k1"], "target_stage": "K2"}
]}$seed$::jsonb)),
    updated_at = now()
FROM public.sop_definitions sd
WHERE sd.tenant_id = v.tenant_id AND sd.sop_id = v.sop_id
  AND sd.code = 'counts.birth'
  AND v.status = 'published'
  AND jsonb_typeof(v.form_dsl->'follow_up'->'tracks') = 'array'
  AND NOT EXISTS (
    SELECT 1 FROM jsonb_array_elements(v.form_dsl->'follow_up'->'tracks') AS t(track)
    WHERE t.track->>'key' = 'birth_litter'
  );

-- +goose Down
-- Forward-only on the document and the registry row: removing them would strand every open litter
-- workflow's steps. The column and the key are inert without the code that reads them.
SELECT 1;
