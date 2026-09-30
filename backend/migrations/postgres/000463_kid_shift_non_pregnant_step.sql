-- +goose Up
-- seed-fixture-guard:ignore: adds one step to the published counts.birth litter track and one column;
-- no vaccination/HRMS seed contract or source fixture schema changes.
--
-- FARM-BORN FEMALES TO NON-PREGNANT AT 10 WEEKS (maintainer instruction 2026-10-01,
-- docs/decisions/kid-stage-shift-tasks.md -> "The Non-Pregnant step"). The litter track opened by
-- 000462 gains a third park-head step: the litter's FEMALE kids to Non-Pregnant 70 days after
-- birth, whatever stage they are on, owed only while one is still before Non-Pregnant.

-- 1. Which kids a shift step judges ('female' / 'male'); NULL = every kid of the litter. A nullable
--    column add is metadata-only.
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS target_sex text;

-- 2. The step, appended IN PLACE to the litter track of each tenant's PUBLISHED counts.birth version
--    that carries the track but not the step (the 000462 shape). A workflow already open keeps the
--    steps it started with. Embedded verbatim from line 4 of
--    tasks/domain/sopseed/counts_birth_litter_track.json (pinned by TestMigrationEmbedsTheKidShiftSeed).
-- seed-migration-guard:ignore owner=manohark issue=kid-stage-shift-tasks reason=in-place-step-on-published-birth-sop-document expiry=2026-12-31
UPDATE public.sop_versions v
SET form_dsl = jsonb_set(v.form_dsl, '{follow_up,tracks}', (
      SELECT jsonb_agg(CASE WHEN t.track->>'key' = 'birth_litter'
        THEN jsonb_set(t.track, '{steps}', (t.track->'steps') || jsonb_build_array($seed${"key": "shift_to_non_pregnant", "task_type": "shift_kids_stage", "title": "Shift the female kids to Non-Pregnant", "detail": "Raise a growth shifting that moves this litter's female kids into a Non-Pregnant pen, from whatever stage they are on. This step completes on its own once every female kid is on Non-Pregnant or past it.", "proof": {}, "schedule": {"kind": "after_event", "offset_minutes": 100800}, "owner": "park_head", "target_stage": "Non-Pregnant", "target_sex": "female"}$seed$::jsonb))
        ELSE t.track END ORDER BY t.ord)
      FROM jsonb_array_elements(v.form_dsl->'follow_up'->'tracks') WITH ORDINALITY AS t(track, ord))),
    updated_at = now()
FROM public.sop_definitions sd
WHERE sd.tenant_id = v.tenant_id AND sd.sop_id = v.sop_id
  AND sd.code = 'counts.birth'
  AND v.status = 'published'
  AND jsonb_typeof(v.form_dsl->'follow_up'->'tracks') = 'array'
  AND EXISTS (
    SELECT 1 FROM jsonb_array_elements(v.form_dsl->'follow_up'->'tracks') AS t(track)
    WHERE t.track->>'key' = 'birth_litter')
  AND NOT EXISTS (
    SELECT 1 FROM jsonb_array_elements(v.form_dsl->'follow_up'->'tracks') AS t(track),
                  jsonb_array_elements(t.track->'steps') AS s(step)
    WHERE t.track->>'key' = 'birth_litter' AND s.step->>'key' = 'shift_to_non_pregnant');

-- 3. Non-Pregnant's "From (days)" (Items & settings): the age at which a growth shifting may move a
--    female straight into Non-Pregnant from any earlier rung (counts/domain growthAgeEntryAllowed).
--    70 days matches the step above. Set only where the farm has not set its own.
UPDATE public.animal_stage_lookup
SET min_age_days = 70
WHERE stage_code = 'Non-Pregnant' AND min_age_days IS NULL;

-- +goose Down
-- Forward-only: removing the step would strand open litter workflows' steps. The column is inert
-- without the code that reads it.
SELECT 1;
