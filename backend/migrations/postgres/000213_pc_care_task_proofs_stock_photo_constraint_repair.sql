-- +goose Up
-- seed-fixture-guard:ignore: repairs a schema constraint for an operational proof table; no seed fixtures are changed
--
-- 000212 creates pc_care_task_proofs with both inventory_vaccine slots on fresh
-- databases. Databases that created the table during an earlier review build can
-- still hold a narrower slot check, which makes stock_fridge_photo fail at
-- runtime. Replace the check constraint in a forward migration so existing
-- environments converge with fresh installs.

ALTER TABLE public.pc_care_task_proofs
  DROP CONSTRAINT IF EXISTS pc_care_task_proofs_slot_check;

ALTER TABLE public.pc_care_task_proofs
  ADD CONSTRAINT pc_care_task_proofs_slot_check CHECK (
    slot_key IN ('stock_fridge_photo', 'stock_fridge_video')
  );

-- +goose Down
ALTER TABLE public.pc_care_task_proofs
  DROP CONSTRAINT IF EXISTS pc_care_task_proofs_slot_check;

ALTER TABLE public.pc_care_task_proofs
  ADD CONSTRAINT pc_care_task_proofs_slot_check CHECK (
    slot_key IN ('stock_fridge_video')
  );
