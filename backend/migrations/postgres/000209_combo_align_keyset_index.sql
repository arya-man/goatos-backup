-- +goose Up
-- +goose NO TRANSACTION
-- R2-06b: back ListPlannedComboBatchesKeyset (internal/obligation/adapters/postgres/repository.go)
-- with a matching partial composite index. That query drives AlignComboDrives every sweep and
-- keyset-pages planned combo batches ordered by (scope_type, scope_id, session, batch_id) filtered to
-- status='planned', sop_task_id IS NULL, session LIKE 'combo:%'. With no matching index Postgres did a
-- tenant-wide scan of obligation_batches plus an in-memory sort on every sweep -- unacceptable at
-- 1-5M-animal scale where a tenant accumulates large batch history.
--
-- The index leads with the exact ORDER BY tuple (after tenant_id) so the keyset walk is index-ordered
-- with NO sort, and is PARTIAL on the near-static predicates (planned + not-yet-finalized combo
-- batches) so it stays tiny relative to the full batch table and only indexes the rows the query can
-- return. planned_date <= $2 and NOT (context ? 'stock_reservation') remain residual filters on the
-- already index-narrowed candidate set. Lock-safe: built CONCURRENTLY, out of transaction, with a
-- statement timeout for the hot table.
SET statement_timeout = '30min';
CREATE INDEX CONCURRENTLY IF NOT EXISTS obligation_batches_combo_align_keyset_idx
  ON obligation_batches (tenant_id, scope_type, scope_id, session, batch_id)
  WHERE status = 'planned' AND sop_task_id IS NULL AND session LIKE 'combo:%';
RESET statement_timeout;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS obligation_batches_combo_align_keyset_idx;
