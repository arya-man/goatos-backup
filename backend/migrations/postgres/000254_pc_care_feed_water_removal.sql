-- +goose Up
-- seed-fixture-guard:ignore: adds an operational PC Care task category; no vaccination HRMS seed rows are changed
--
-- FEED & WATER REMOVAL precondition for tablet-in-feed deworming (maintainer decision
-- 2026-09-03). A deworming planned with feed_removal_required creates, in the SAME
-- transaction, one linked feed_water_removal task: same pen, planned/due one day BEFORE
-- the deworming date, its own operators, task-level proof (two live-camera videos:
-- feed_video + water_video on pc_care_task_proofs). gates_task_id on the REMOVAL row
-- points at the deworming it gates; the kernel's midnight gate pushes an unfed-precondition
-- deworming (linked removal never submitted) to tomorrow instead of letting it run.
--
-- Natural-key note: pc_care_tasks_natural_uq (tenant, category, park, shed, partition_key,
-- planned date, live) already keeps removal rows apart from deworming rows (category is in
-- the key) and two dewormings on consecutive dates produce removal rows on DIFFERENT dates,
-- so no widening is needed. A stale live removal row (its deworming canceled) colliding with
-- a replanned pair is refused at create time (task_already_planned).

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_category_check;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_category_check CHECK (
    category IN ('deworming', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'inventory_vaccine', 'feed_water_removal')
  );

-- gates_task_id: carried by the REMOVAL row, pointing at the deworming task it gates.
-- The deworming row leaves it NULL.
ALTER TABLE public.pc_care_tasks
  ADD COLUMN IF NOT EXISTS gates_task_id uuid;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_gates_task_fk
    FOREIGN KEY (tenant_id, gates_task_id)
    REFERENCES public.pc_care_tasks (tenant_id, task_id);

-- ONE removal row per gated deworming: makes the midnight-gate join provably 1:0..1
-- (the gate's claim subquery joins removal.gates_task_id = deworming.task_id and this
-- index is exactly those columns), and doubles as the gate lookup's index.
CREATE UNIQUE INDEX IF NOT EXISTS pc_care_tasks_gates_task_uq
  ON public.pc_care_tasks (tenant_id, gates_task_id)
  WHERE gates_task_id IS NOT NULL;

-- Task-level proof slots for the removal videos join the fridge-stock slots.
ALTER TABLE public.pc_care_task_proofs
  DROP CONSTRAINT IF EXISTS pc_care_task_proofs_slot_check;

ALTER TABLE public.pc_care_task_proofs
  ADD CONSTRAINT pc_care_task_proofs_slot_check CHECK (
    slot_key IN ('stock_fridge_photo', 'stock_fridge_video', 'feed_video', 'water_video')
  );

-- The 20:00 IST list-visibility predicate is a computed expression evaluated over a page
-- already narrowed by pc_care_tasks_serving_idx (tenant_id, park_id, due_business_date,
-- work_state) to one park-day, so it needs no index of its own.

-- +goose Down
DROP INDEX IF EXISTS pc_care_tasks_gates_task_uq;

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_gates_task_fk;

DELETE FROM public.pc_care_task_proofs
WHERE slot_key IN ('feed_video', 'water_video');

ALTER TABLE public.pc_care_task_proofs
  DROP CONSTRAINT IF EXISTS pc_care_task_proofs_slot_check;

ALTER TABLE public.pc_care_task_proofs
  ADD CONSTRAINT pc_care_task_proofs_slot_check CHECK (
    slot_key IN ('stock_fridge_photo', 'stock_fridge_video')
  );

DELETE FROM public.pc_care_tasks WHERE category = 'feed_water_removal';

ALTER TABLE public.pc_care_tasks
  DROP COLUMN IF EXISTS gates_task_id;

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_category_check;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_category_check CHECK (
    category IN ('deworming', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'inventory_vaccine')
  );
