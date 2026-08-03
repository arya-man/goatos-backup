-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: operational Weighing kernel table only; no Vaccination HRMS seed contract change
--
-- CARRY-OVER THAT LANDS ON SOMEBODY ELSE'S SHED (maintainer decision, 2026-08-03).
--
-- Unfinished weighing rolls forward: its due_business_date moves to today. That
-- roll can land on a day when the SAME shed is already somebody's open work --
-- a second task, planned for today, holding the same (park, shed, date).
--
-- Two people owing the same shed on the same real day is the one thing weighing
-- must never produce: they scan the same animals twice, or each assumes the
-- other did it. But refusing the roll is just as wrong -- the work does not
-- disappear because the calendar is busy.
--
-- The rule: the claim ALREADY PLANNED for that day carries on, because its
-- operator is the person who will be standing at that shed. The rolled-over item
-- is simply CLOSED and linked to it.
--
-- NOTHING IS TRANSFERRED. No work item changes operator, and no capture moves:
-- whatever the closed task already weighed stays its own history, exactly as it
-- happened. The surviving task then does its own weighing -- free-flow, so its
-- operator may scan the same tags again or different ones, and neither is a
-- duplicate of the closed task's record. Same-operator collisions behave
-- identically; there is no special case for "it was mine anyway".
--
-- These two columns make that outcome auditable instead of a silent
-- disappearance. `closed_reason` records WHY in machine terms; the farm-readable
-- sentence is composed by the notification consumer, which can resolve operator
-- names. `merged_into_work_item_id` links to the task that took the shed over, so
-- "where did my carry-over go?" is answerable from one row.
--
-- Lock safety: two nullable ADD COLUMNs, metadata-only on Postgres (no table
-- rewrite, no default backfill), plus one CONCURRENTLY-built partial index. NO
-- TRANSACTION is required for the concurrent build.
ALTER TABLE public.weighing_work_items
  ADD COLUMN IF NOT EXISTS closed_reason text,
  ADD COLUMN IF NOT EXISTS merged_into_work_item_id uuid REFERENCES public.weighing_work_items(work_item_id);

-- The collision lookup: "is this shed already somebody's OPEN work on this date?"
-- Keyed exactly as the carry-over pass asks it, so the check is an index probe per
-- rolled item rather than a scan of the day's work.
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_work_items_open_shed_date_idx
  ON public.weighing_work_items (tenant_id, park_id, shed_location_id, due_business_date)
  INCLUDE (work_item_id, operator_user_id, campaign_shed_id)
  WHERE work_state IN ('scheduled', 'delayed');

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_work_items_open_shed_date_idx;

ALTER TABLE public.weighing_work_items
  DROP COLUMN IF EXISTS merged_into_work_item_id,
  DROP COLUMN IF EXISTS closed_reason;
