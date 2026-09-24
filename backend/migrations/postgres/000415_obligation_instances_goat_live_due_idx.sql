-- +goose Up
-- +goose NO TRANSACTION
--
-- THE DUE-DATE ARM OF THE WORK BOARD "ANY VACCINATION WORK TODAY?" PRECHECK.
--
-- processintegrity/adapters/boardsource/source.go:vaccinationDueWorkPrecheckSQL runs on every
-- Work Board page / summary / rows load. Its first arm asks "is any live goat obligation of this
-- tenant due inside this business day?": tenant_id = $1 AND target_type = 'goat' AND status <>
-- 'canceled' AND due_at in [$3, $4). No existing index answers that shape: the due-window index
-- leads with status (and the query's status is a <>), vaccination_drive_day_idx also excludes
-- superseded/waived, and the batch index has no due_at. Without this index the arm is a
-- tenant-wide walk: the legacy single-OR form read all 86,616 obligation rows (133k buffers,
-- 228 ms) on the stg clone for an empty day; the arm alone is 5 ms with it.
--
-- INCLUDE (target_id, protocol_version_id, status) makes the arm index-only: the precheck
-- joins goats / protocol_versions on exactly those columns and re-filters status.
--
-- The predicate matches the arm's constant filters character for character
-- (target_type = 'goat' AND status <> 'canceled'), so the planner can prove the partial index
-- applies to the prepared (generic) plan as well as custom plans.
--
-- LOCK SAFETY: CONCURRENTLY + NO TRANSACTION. obligation_instances is written by every
-- vaccination, obligation and kernel path; a blocking build would stall them.
CREATE INDEX CONCURRENTLY IF NOT EXISTS obligation_instances_goat_live_due_idx
ON public.obligation_instances (tenant_id, due_at)
INCLUDE (target_id, protocol_version_id, status)
WHERE target_type = 'goat' AND status <> 'canceled';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.obligation_instances_goat_live_due_idx;
