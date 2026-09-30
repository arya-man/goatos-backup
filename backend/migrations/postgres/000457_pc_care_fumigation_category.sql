-- +goose Up
-- +goose NO TRANSACTION
-- 000457_pc_care_fumigation_category.sql
--
-- FUMIGATION (maintainer instruction 2026-09-30, docs/decisions/pc-care-fumigation.md): the PC Care
-- category CHECKs admit 'fumigation'. Its own NO TRANSACTION migration (PR #457 review) so no lock
-- is held across statements: the drop + NOT VALID add is one short catalog change under a bounded
-- lock_timeout, and VALIDATE then scans under SHARE UPDATE EXCLUSIVE, which blocks no writes. The
-- data half (SOP card, ticks, Virufix) is 000458, which stays transactional.
--
-- seed-fixture-guard:ignore: category vocabulary widening only.
SET lock_timeout = '5s';
-- One statement: the old CHECK goes and the widened one arrives NOT VALID together, so there is
-- no instant without a constraint and the ACCESS EXCLUSIVE lock lasts only the catalog change.
ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_category_check,
  ADD CONSTRAINT pc_care_tasks_category_check CHECK (category IN ('deworming', 'anti_protozoan', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'fumigation', 'inventory_vaccine', 'feed_water_removal')) NOT VALID;
RESET lock_timeout;
-- Its own statement (NO TRANSACTION): VALIDATE takes SHARE UPDATE EXCLUSIVE, so the scan runs
-- without blocking reads or writes.
ALTER TABLE public.pc_care_tasks VALIDATE CONSTRAINT pc_care_tasks_category_check;

SET lock_timeout = '5s';
-- One statement: the old CHECK goes and the widened one arrives NOT VALID together, so there is
-- no instant without a constraint and the ACCESS EXCLUSIVE lock lasts only the catalog change.
ALTER TABLE public.pc_care_rounds
  DROP CONSTRAINT IF EXISTS pc_care_rounds_category_check,
  ADD CONSTRAINT pc_care_rounds_category_check CHECK (category IN ('deworming', 'anti_protozoan', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'fumigation')) NOT VALID;
RESET lock_timeout;
-- Its own statement (NO TRANSACTION): VALIDATE takes SHARE UPDATE EXCLUSIVE, so the scan runs
-- without blocking reads or writes.
ALTER TABLE public.pc_care_rounds VALIDATE CONSTRAINT pc_care_rounds_category_check;

-- +goose Down
-- +goose NO TRANSACTION
SET lock_timeout = '5s';
DELETE FROM public.pc_care_tasks WHERE category = 'fumigation';
DELETE FROM public.pc_care_rounds WHERE category = 'fumigation';
RESET lock_timeout;
SET lock_timeout = '5s';
-- One statement: the old CHECK goes and the widened one arrives NOT VALID together, so there is
-- no instant without a constraint and the ACCESS EXCLUSIVE lock lasts only the catalog change.
ALTER TABLE public.pc_care_rounds
  DROP CONSTRAINT IF EXISTS pc_care_rounds_category_check,
  ADD CONSTRAINT pc_care_rounds_category_check CHECK (category IN ('deworming', 'anti_protozoan', 'ticks_removal', 'hoof_trimming', 'hair_trimming')) NOT VALID;
RESET lock_timeout;
-- Its own statement (NO TRANSACTION): VALIDATE takes SHARE UPDATE EXCLUSIVE, so the scan runs
-- without blocking reads or writes.
ALTER TABLE public.pc_care_rounds VALIDATE CONSTRAINT pc_care_rounds_category_check;

SET lock_timeout = '5s';
-- One statement: the old CHECK goes and the widened one arrives NOT VALID together, so there is
-- no instant without a constraint and the ACCESS EXCLUSIVE lock lasts only the catalog change.
ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_category_check,
  ADD CONSTRAINT pc_care_tasks_category_check CHECK (category IN ('deworming', 'anti_protozoan', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'inventory_vaccine', 'feed_water_removal')) NOT VALID;
RESET lock_timeout;
-- Its own statement (NO TRANSACTION): VALIDATE takes SHARE UPDATE EXCLUSIVE, so the scan runs
-- without blocking reads or writes.
ALTER TABLE public.pc_care_tasks VALIDATE CONSTRAINT pc_care_tasks_category_check;
