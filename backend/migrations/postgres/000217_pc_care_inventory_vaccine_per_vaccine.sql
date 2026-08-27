-- +goose Up
-- seed-fixture-guard:ignore: reshapes the operational inventory_vaccine task grain; no vaccination HRMS seed rows are changed
--
-- Maintainer decision 2026-08-27: the PC-director fridge-stock task is PER VACCINE, never per
-- shed. Stock lives in a park's fridge, so the question the task answers is "is there enough
-- FMD for everything scheduled that day?" — the sheds the doses are for are irrelevant to the
-- fridge. The task grain becomes (tenant, park, vaccine, task date); the shed columns stay for
-- every other PC Care category and for canceled legacy per-shed stock rows.
--
-- shed_id was NOT NULL. Per-vaccine stock tasks have no shed, so the column relaxes and a
-- shape check keeps the two forms apart: a task carries a vaccine label ONLY when it is an
-- inventory_vaccine task with no shed, and every non-inventory category still requires a shed.
-- The old natural key (…, shed_id, partition_key, …) cannot collide for shed-less rows because
-- btree unique treats NULL shed_id as distinct; the per-vaccine grain gets its own partial
-- unique index instead.

ALTER TABLE public.pc_care_tasks
  ALTER COLUMN shed_id DROP NOT NULL;

ALTER TABLE public.pc_care_tasks
  ADD COLUMN IF NOT EXISTS vaccine_label text;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_vaccine_shape_check CHECK (
    (vaccine_label IS NULL AND shed_id IS NOT NULL)
    OR (
      vaccine_label IS NOT NULL
      AND btrim(vaccine_label) <> ''
      AND category = 'inventory_vaccine'
      AND shed_id IS NULL
    )
  );

CREATE UNIQUE INDEX IF NOT EXISTS pc_care_tasks_vaccine_natural_uq
  ON public.pc_care_tasks (tenant_id, category, park_id, lower(btrim(vaccine_label)), planned_business_date)
  WHERE work_state <> 'canceled' AND vaccine_label IS NOT NULL;

-- +goose Down
DELETE FROM public.pc_care_tasks WHERE vaccine_label IS NOT NULL;

DROP INDEX IF EXISTS public.pc_care_tasks_vaccine_natural_uq;

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_vaccine_shape_check;

ALTER TABLE public.pc_care_tasks
  DROP COLUMN IF EXISTS vaccine_label;

ALTER TABLE public.pc_care_tasks
  ALTER COLUMN shed_id SET NOT NULL;
