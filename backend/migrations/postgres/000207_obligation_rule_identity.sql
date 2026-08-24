-- +goose Up
-- +goose NO TRANSACTION
-- An obligation follows a rule's IDENTITY, not the version UUID that happened to mint it.
--
-- Obligations were keyed by (goat, protocol_version_id, rule_id, due_at, sequence). Publishing
-- rewrites every rule row, so the same ET+TT rule is a different rule_id in every version, and
-- database-wise the animal's existing work looked like it belonged to something that no longer
-- existed. Everything downstream of that -- cancel-and-re-mint, carry-over, the churn this whole
-- change set is about -- follows from treating a version pointer as identity.
--
-- rule_identity_key is that identity: vaccine|dose|sequence, the same value the publisher writes
-- to protocol_rule_lineage. A version becomes what it always should have been -- history -- and
-- the question "is this the animal's existing ET+TT work?" stops depending on which version is
-- current.
--
-- The partial unique index is the point. At most ONE open obligation per (goat, identity,
-- sequence) is enforced by the database rather than by generation remembering to check, so a
-- second row for a dose the animal already owes cannot be written at all. Before this, a rule
-- whose content was unchanged but whose computed due date moved -- because the ANIMAL's history
-- moved, which is routine -- produced two open rows for one dose. That is worse than the churn
-- it replaced: churn loses continuity, double-booking books medical work twice.
--
-- Scoped to rows that carry an identity, so existing rows (identity NULL until backfilled) can
-- neither block the index build nor be silently constrained by it.
SET lock_timeout = '3s';

ALTER TABLE obligation_instances
  ADD COLUMN IF NOT EXISTS rule_identity_key text;

RESET lock_timeout;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS obligation_open_rule_identity_unique_idx
  ON obligation_instances (tenant_id, target_type, target_id, rule_identity_key, "sequence")
  WHERE rule_identity_key IS NOT NULL
    AND status IN ('scheduled', 'due', 'in_progress', 'deferred');

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS obligation_open_rule_identity_unique_idx;

SET lock_timeout = '3s';
ALTER TABLE obligation_instances
  DROP COLUMN IF EXISTS rule_identity_key;
RESET lock_timeout;
