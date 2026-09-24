-- +goose Up
-- +goose NO TRANSACTION
--
-- VACCINATION COMMAND BOARD: READ THE LIVE OBLIGATIONS, NOT THE CANCELED 88%.
--
-- Maintainer decision (2026-09-24): canceled obligations do NOT count in the command-board totals.
-- Every board fold (KPI, cohort matrix, shed-dose matrix, shed-vaccine matrix, closed-without-dose
-- and shed-vaccine drawers) now filters `oi.status <> 'canceled'`. 76,041 of 86,616 rows (88%) are
-- canceled reconciler leftovers, so without an index that matches the predicate each statement
-- still seq-scans the whole 105 MB heap on db-g1-small (128 MB shared_buffers) to throw 88% away.
--
-- The predicate is spelled EXACTLY as the queries spell it so the planner proves the partial index
-- applies; INCLUDE carries every column the folds read so the scan is index-only once the
-- visibility map is set (autovacuum below). Measured on a vacuumed copy of the OCI clone
-- (tenant 00000000-0000-4000-8000-000000000001, as_of 2026-09-24 12:00 IST):
--   KPI 60 -> 19 ms, cohort 148 -> 36 ms, shed-vaccine 118 -> 32 ms, closed-without-dose 204 -> 22 ms.
CREATE INDEX CONCURRENTLY IF NOT EXISTS obligation_instances_board_live_idx
  ON public.obligation_instances (tenant_id, batch_id)
  INCLUDE (scope_id, scope_type, target_id, target_type, obligation_id, rule_id, protocol_version_id, status, due_at)
  WHERE status <> 'canceled';

-- Per-table autovacuum. At the global 0.2 scale factor a 86k-row table waits for ~17k dead
-- tuples before a vacuum, and the index-only scan above degrades to heap fetches while the
-- visibility map is stale; the planner also ran on 2.5x-high row estimates (n_mod_since_analyze
-- 693k on the OCI clone). Reloptions only: a brief SHARE UPDATE EXCLUSIVE lock, no rewrite.
ALTER TABLE public.obligation_instances SET (
  autovacuum_vacuum_scale_factor = 0.02,
  autovacuum_analyze_scale_factor = 0.01
);

-- +goose Down
-- +goose NO TRANSACTION
ALTER TABLE public.obligation_instances RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor);
DROP INDEX CONCURRENTLY IF EXISTS public.obligation_instances_board_live_idx;
