-- +goose Up
-- +goose NO TRANSACTION
-- CompleteProof serializes task/goat video replacement with an advisory lock and
-- then asks for the newest completed video in the same tenant/task/goat scope.
-- Keep that post-upload transaction on a narrow partial index so low-count proof
-- completions do not turn into slow scans while the shared API/DB lane is busy.

CREATE INDEX CONCURRENTLY IF NOT EXISTS proof_artifacts_task_goat_video_current_idx
  ON public.proof_artifacts (tenant_id, scope_id, subject_id, created_at DESC, proof_id DESC)
  WHERE scope_type = 'task'
    AND subject_type = 'goat'
    AND subject_id IS NOT NULL
    AND proof_type = 'video'
    AND upload_state = 'completed';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.proof_artifacts_task_goat_video_current_idx;
