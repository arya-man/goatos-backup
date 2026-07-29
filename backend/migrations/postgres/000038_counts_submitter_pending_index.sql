-- +goose Up
-- +goose NO TRANSACTION

-- Supports the submitter-scoped pending approval lookup without scanning every approval request.
-- This migration existed in the goatosdb ledger but was lost from the branch before commit; keep
-- the original index name and shape so restored databases and clean installs converge.
CREATE INDEX CONCURRENTLY IF NOT EXISTS counts_approval_requests_submitter_pending_idx
  ON public.counts_approval_requests (
    tenant_id,
    raised_by_user_id,
    request_type,
    raised_at DESC,
    approval_request_id DESC
  )
  WHERE status = 'pending';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.counts_approval_requests_submitter_pending_idx;
