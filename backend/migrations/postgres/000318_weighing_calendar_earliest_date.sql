-- +goose Up
-- Widen the seeded calendar to July 5 without changing the fixed August 3 landing date.
-- Only the exact old page-settings block is upgraded. Custom dates/modes and draft or
-- historical documents remain untouched. Page settings are explicitly unpinned; preserving
-- the published version number leaves task rules and their historical pins unchanged.
-- Future operator changes use the normal SOP draft/validate/publish version workflow.
-- seed-fixture-guard:ignore: weighing-owned published SOP page configuration only.
-- seed-migration-guard:ignore owner=codex issue=weighing-calendar-db-config reason=forward-only narrow update of the exact old seeded page-settings block; task rules and custom configurations are unchanged expiry=2026-10-31
UPDATE public.sop_versions v
SET form_dsl = jsonb_set(v.form_dsl, '{weighing,weights_pages,earliest_date}', '"2026-07-05"'::jsonb)
FROM public.sop_definitions d
WHERE d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
  AND d.code = 'weighing.session'
  AND v.status = 'published'
  AND v.form_dsl #> '{weighing,weights_pages}' =
    '{"default_from_mode":"fixed_date","default_from_date":"2026-08-03","earliest_date":"2026-08-01"}'::jsonb;

-- Documents published before page settings existed read these same defaults in code.
-- Materialize that previously implicit configuration, preserving every authored task rule.
-- seed-migration-guard:ignore owner=codex issue=weighing-calendar-db-config reason=materialize previously implicit page settings on published weighing documents without overriding authored fields expiry=2026-10-31
UPDATE public.sop_versions v
SET form_dsl = jsonb_set(v.form_dsl, '{weighing,weights_pages}',
  '{"default_from_mode":"fixed_date","default_from_date":"2026-08-03","earliest_date":"2026-07-05"}'::jsonb)
FROM public.sop_definitions d
WHERE d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
  AND d.code = 'weighing.session'
  AND v.status = 'published'
  AND jsonb_typeof(v.form_dsl -> 'weighing') = 'object'
  AND (v.form_dsl #> '{weighing,weights_pages}' IS NULL
    OR v.form_dsl #> '{weighing,weights_pages}' = 'null'::jsonb);

-- +goose Down
-- Forward-only: do not overwrite dates subsequently authored by operators.
SELECT 1;
