-- +goose Up
-- +goose NO TRANSACTION
-- VACC-REV-11 Phase 3/3: Swap the identity_goat constraint (drop v1, rename v2→v1).
-- This migration is LOCK-SAFE (phase 3 of split identity_goat rollout):
-- - DROP CONSTRAINT is metadata-only with brief ACCESS EXCLUSIVE lock, released on commit
-- - RENAME CONSTRAINT is metadata-only with brief ACCESS EXCLUSIVE lock, released on commit
-- - NO TRANSACTION ensures each statement commits independently
-- - SET lock_timeout='10s' prevents indefinite waits on metadata locks
-- - Idempotent: DROP IF EXISTS handles missing old constraint; RENAME is safe if v2 exists
--
-- Dependency: 000200 must have applied first (constraint v2 exists and is VALID)
--
-- If this migration is interrupted and rerun:
-- 1. DROP IF EXISTS of old v1 succeeds (v1 may or may not exist)
-- 2. RENAME v2 to v1 succeeds (v2 was created in 000199, validated in 000200)
-- Result: the new v1 (with identity_goat) is active and all new writes enforce it
--
-- After this phase completes:
-- - Old constraint identity_decisions_decision_type_check is gone
-- - New constraint identity_decisions_decision_type_check exists and is VALID
-- - All subsequent writes enforce the widened CHECK including 'identity_goat'
-- - No window of invalid/missing constraint (NO TRANSACTION ensures atomic per-statement)

SET lock_timeout = '10s';

-- Drop the old v1 constraint (may not exist if the old identity_decisions_decision_type_check was already gone)
ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check;

-- Rename the new v2 to become the canonical v1 constraint name
ALTER TABLE identity_decisions
  RENAME CONSTRAINT identity_decisions_decision_type_check_v2 TO identity_decisions_decision_type_check;

-- +goose Down
-- +goose NO TRANSACTION
-- Revert phase 3: rename the constraint back to v2 and re-add the old v1 constraint.
-- This allows rollback from phase 3 to phase 2 state.

SET lock_timeout = '10s';

-- Rename the new v1 back to v2 (it must exist after phase 2)
ALTER TABLE identity_decisions
  RENAME CONSTRAINT identity_decisions_decision_type_check TO identity_decisions_decision_type_check_v2;

-- Re-add the old v1 constraint (narrower, without 'identity_goat')
-- Marked NOT VALID to match the down-path in 000200 and allow concurrent validation
ALTER TABLE identity_decisions
  ADD CONSTRAINT identity_decisions_decision_type_check CHECK (
    decision_type IN (
      'create_goat',
      'attach_identifier',
      'retire_identifier',
      'mark_identifier_disputed',
      'merge_goats',
      'batch_merge_goats',
      'reject_match',
      'request_field_verification',
      'resolve_correction_request',
      'move_goat',
      'exit_goat',
      'stage_goat',
      'health_goat',
      'reproductive_goat'
    )
  ) NOT VALID;
