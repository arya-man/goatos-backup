-- +goose Up
-- +goose NO TRANSACTION
-- counts.birth.reported (SOP capture card, 2026-09-16): written in the SAME transaction as the
-- birth approval request and its canonical children, carrying the Add birth form's capture
-- snapshot so the report's own proof reaches the verifier (birth_evidence / birth_capture).
-- The idempotency key is deterministic per approval request, so a replayed submit can never
-- queue the item twice -- the same partial unique index shape counts.death.reported carries
-- (migration 000035).

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_counts_birth_reported_idempotency_idx
  ON public.outbox_messages (tenant_id, idempotency_key)
  WHERE event_type = 'counts.birth.reported';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.outbox_messages_counts_birth_reported_idempotency_idx;
