-- +goose Up
-- +goose NO TRANSACTION
--
-- /app/proofs/uploads (ListUploadedProofs) reads one task's completed captures inside one scope:
--   WHERE tenant_id, scope_type, scope_id, upload_state = 'completed',
--         metadata->>'client_task_key' = $4 ORDER BY created_at DESC, proof_id DESC LIMIT 20
-- proof_artifacts_scope_idx (tenant_id, scope_type, scope_id, created_at DESC) has no task key,
-- so the key was a Filter over the scope's WHOLE proof history, growing with every capture.
-- Throwaway copy of the OCI clone's proof_artifacts (44,065 rows), busiest shed: 3,639 rows
-- removed by filter for 1 returned, 3,077 buffers, 3.56 ms -> with this index an Index Scan on
-- the exact key, 4 buffers, 0.02 ms, and no sort (the index order is the ORDER BY).
--
-- Partial on 'completed' because that is the only state the read accepts; the upload pipeline's
-- pending/uploading churn never touches this index.
--
-- LOCK SAFETY: CONCURRENTLY + NO TRANSACTION, so the build takes no ACCESS EXCLUSIVE lock on
-- proof_artifacts, which every capture upload writes to.
CREATE INDEX CONCURRENTLY IF NOT EXISTS proof_artifacts_scope_task_key_idx
ON public.proof_artifacts (
  tenant_id,
  scope_type,
  scope_id,
  (metadata->>'client_task_key'),
  created_at DESC,
  proof_id DESC
)
WHERE upload_state = 'completed';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.proof_artifacts_scope_task_key_idx;
