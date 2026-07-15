-- +goose Up
-- +goose NO TRANSACTION
-- VACC-REV-11 Phase 1/3: Add the identity_goat decision type to identity_decisions CHECK.
-- This migration is LOCK-SAFE (phase 1 of split identity_goat rollout):
-- - identity_decisions is a hot operational table
-- - ADD CONSTRAINT ... NOT VALID is metadata-only with brief ACCESS EXCLUSIVE lock
-- - NO TRANSACTION ensures each statement commits independently
-- - SET lock_timeout='10s' prevents indefinite hangs on lock contention
-- - Idempotent: DROP IF EXISTS the v2 constraint before ADD, allowing safe reruns after interrupts
--
-- If this migration is interrupted and rerun:
-- 1. DROP IF EXISTS succeeds (constraint may or may not exist)
-- 2. ADD CONSTRAINT succeeds (creates the new v2 with NOT VALID)
-- 3. Next phase (000200) will VALIDATE the existing v2
--
-- Dependency: 000197 must have applied first (migration sequence enforced by goose)

SET lock_timeout = '10s';

-- Idempotent: drop any existing v2 (in case of partial prior run)
ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check_v2;

-- Add the new v2 constraint with 'identity_goat' included, marked NOT VALID for concurrent validation
ALTER TABLE identity_decisions
  ADD CONSTRAINT identity_decisions_decision_type_check_v2 CHECK (
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
      'reproductive_goat',
      'identity_goat'
    )
  ) NOT VALID;

-- +goose Down
-- +goose NO TRANSACTION
-- Revert phase 1: drop the v2 constraint (it may not yet be renamed to v1, or may be in limbo).
-- If rolledback mid-phase (e.g., during 000199 or 000200), this cleanup is safe and idempotent.

SET lock_timeout = '10s';

ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check_v2;
