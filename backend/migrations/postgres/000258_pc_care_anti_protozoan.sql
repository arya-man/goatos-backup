-- +goose Up
-- seed-fixture-guard:ignore: adds an operational PC Care work category; no vaccination HRMS seed rows are changed
--
-- ANTI PROTOZOAN (maintainer instruction 2026-09-05): a fifth PC Care work category, planned
-- and worked EXACTLY like deworming — one dose per animal, scanned free-flow, one live-camera
-- video each, submitted whole and reviewed by the tenant verifier.
--
-- The ONE difference is the feed & water removal precondition, and it is an ABSENCE rather
-- than a variant: deworming's tablet goes in the feed, so its pens are emptied the evening
-- before. An anti-protozoan dose does not, so there is no removal to plan, no evening crew,
-- and no 20:00 IST planning cutoff. Nothing here opts out of that logic — the service already
-- admits the removal fields for `deworming` and refuses every other category, so this category
-- gets the refusal for free and cannot be planned with one by mistake.

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_category_check;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_category_check CHECK (
    category IN ('deworming', 'anti_protozoan', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'inventory_vaccine', 'feed_water_removal')
  );

ALTER TABLE public.pc_care_rounds
  DROP CONSTRAINT IF EXISTS pc_care_rounds_category_check;

ALTER TABLE public.pc_care_rounds
  ADD CONSTRAINT pc_care_rounds_category_check CHECK (
    category IN ('deworming', 'anti_protozoan', 'ticks_removal', 'hoof_trimming', 'hair_trimming')
  );

-- +goose Down
DELETE FROM public.pc_care_tasks WHERE category = 'anti_protozoan';
DELETE FROM public.pc_care_rounds WHERE category = 'anti_protozoan';

ALTER TABLE public.pc_care_rounds
  DROP CONSTRAINT IF EXISTS pc_care_rounds_category_check;

ALTER TABLE public.pc_care_rounds
  ADD CONSTRAINT pc_care_rounds_category_check CHECK (
    category IN ('deworming', 'ticks_removal', 'hoof_trimming', 'hair_trimming')
  );

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_category_check;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_category_check CHECK (
    category IN ('deworming', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'inventory_vaccine', 'feed_water_removal')
  );
