-- +goose Up
-- seed-fixture-guard:ignore: operational PC Care close/reopen columns; no vaccination HRMS seed rows are changed
--
-- PC CARE HAS TWO VERBS: CLOSE AND REOPEN (maintainer decision 2026-09-05, RETIRING
-- the cancel verb added in 000183).
--
-- PC Care carried a CANCEL that weighing has never had. Weighing's lock is exact:
-- "the weighing workflow has exactly two verbs — CLOSE a task, or REOPEN it if it is
-- already closed. There is no third verb, and no force, override or skip variant of
-- close." PC Care is now on par.
--
-- The difference is not cosmetic. CANCEL erased a plan: the natural key excludes
-- canceled rows, so a canceled pen-day could be re-planned as though nothing had ever
-- been planned there. CLOSE records what actually happened — this work was planned and
-- then ended without being done — and the pen-day STAYS TAKEN, because ending a plan is
-- a fact rather than an erasure. That consequence is the decision, not a side effect:
-- pc_care_tasks_natural_uq is deliberately NOT widened to exclude 'closed'.
--
-- THE CLOSE GATE IS UNCONDITIONAL, exactly as weighing's is (ledger D-5): a task cannot
-- close while its evidence is awaiting a verdict, and there is no caller-supplied way
-- past it. If a task will not close, the answer is to RESOLVE the verification — get the
-- verdict — never to add a path around the gate. A COMPLETED task is likewise never
-- rewritten by a close: completed is accepted work.
--
-- 'canceled' stays in the work_state CHECK and in the natural key's partial predicate.
-- Rows canceled before this migration are real history and must keep reading as what
-- they were; nothing writes the value any more.

ALTER TABLE public.pc_care_tasks
  ADD COLUMN IF NOT EXISTS closed_by uuid,
  -- close_reason is the closer's own words, shown verbatim to whoever asks why a pen's
  -- work never happened. A close with no reason leaves that question unanswerable.
  ADD COLUMN IF NOT EXISTS close_reason text;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_close_shape_check CHECK (
    (work_state = 'closed' AND closed_by IS NOT NULL AND btrim(coalesce(close_reason, '')) <> '')
    OR (work_state <> 'closed' AND closed_by IS NULL AND close_reason IS NULL)
  );

-- +goose Down
ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_close_shape_check;

ALTER TABLE public.pc_care_tasks
  DROP COLUMN IF EXISTS close_reason,
  DROP COLUMN IF EXISTS closed_by;
