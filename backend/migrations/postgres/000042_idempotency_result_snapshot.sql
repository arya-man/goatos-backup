-- +goose Up
-- BUG-037: an exact idempotent replay must return the ORIGINAL result, not a fresh read of the
-- record's CURRENT state. idempotency_keys only stored result_type/result_id, so every replay path
-- re-read the row by id -- which, for a MUTABLE record (workforce_positions bumps row_version on
-- every edit), hands the caller whatever the row looks like now. A replay of update A that happened
-- before unrelated updates B and C returned B/C's state under A's key.
--
-- result_snapshot stores the response body produced by the FIRST call, written inside the same
-- transaction as the side effects, so a replay can return it verbatim with no re-read and no
-- re-execution. Nullable jsonb with no default: PostgreSQL records this as a catalog-only change
-- (no table rewrite, no full-table lock held while writing), so it is safe on a hot shared table.
-- Rows written before this migration keep result_snapshot NULL and fall back to the previous
-- read-by-result_id behaviour.
SET lock_timeout = '5s';

ALTER TABLE public.idempotency_keys
  ADD COLUMN IF NOT EXISTS result_snapshot jsonb;

COMMENT ON COLUMN public.idempotency_keys.result_snapshot IS
  'Response body produced by the first call for this key, persisted in the same transaction as the side effects so an exact replay returns the original result instead of the record''s current state (BUG-037).';

-- +goose Down
SET lock_timeout = '5s';

ALTER TABLE public.idempotency_keys
  DROP COLUMN IF EXISTS result_snapshot;
