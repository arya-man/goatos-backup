-- +goose Up
-- 000244_pen_reconciliation_enqueue_recovery.sql
--
-- Durable recovery for pen-reconciliation verification enqueue. The card transition can commit
-- before the generic verifier item is successfully created; this marker lets exact completion
-- replays retry the idempotent enqueue until it is confirmed.

ALTER TABLE pen_reconciliation_cards
  ADD COLUMN IF NOT EXISTS verification_enqueue_pending boolean NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS pen_reconciliation_cards_enqueue_recovery_idx
  ON pen_reconciliation_cards (tenant_id, updated_at, card_id)
  WHERE status = 'pending_verification' AND verification_enqueue_pending;

-- +goose Down
DROP INDEX IF EXISTS pen_reconciliation_cards_enqueue_recovery_idx;
ALTER TABLE pen_reconciliation_cards
  DROP COLUMN IF EXISTS verification_enqueue_pending;
