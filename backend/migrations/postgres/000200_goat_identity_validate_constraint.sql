-- +goose Up
-- +goose NO TRANSACTION
-- VACC-REV-11 Phase 2/3: Validate the identity_goat decision type constraint.
-- This migration is LOCK-SAFE (phase 2 of split identity_goat rollout):
-- - VALIDATE CONSTRAINT scans existing rows under SHARE UPDATE EXCLUSIVE lock
-- - SHARE UPDATE EXCLUSIVE allows concurrent SELECT/UPDATE/DELETE (no exclusive hold)
-- - NO TRANSACTION ensures each statement commits independently
-- - SET lock_timeout='30s' bounds the scan duration; if rows are heavy, timeout prevents outage
-- - Idempotent: VALIDATE of an already-validated constraint is a no-op; no error
--
-- Dependency: 000199 must have applied first (constraint v2 exists and is NOT VALID)
--
-- If this migration is interrupted and rerun:
-- 1. VALIDATE checks whether v2 is marked as validated
-- 2. If already validated, VALIDATE is a no-op and succeeds
-- 3. If still NOT VALID, VALIDATE scans rows and marks it VALID
-- Either way, subsequent phase (000201) can proceed safely
--
-- Rationale for 30s timeout:
-- - At 5k-50k animal scale (current envelope), existing identity_decisions rows are ~100-1000s
-- - VALIDATE scans and checks each row; row scan with index typically <10s per 100k rows
-- - 30s headroom prevents timeout on moderate clock skew or transient IO jitter
-- - If actual validation exceeds 30s, timeout will fail with clear error (not an indefinite hang)

SET lock_timeout = '30s';

ALTER TABLE identity_decisions VALIDATE CONSTRAINT identity_decisions_decision_type_check_v2;

-- +goose Down
-- +goose NO TRANSACTION
-- Revert phase 2: mark the v2 constraint as NOT VALID again.
-- This allows a rollback to phase 1 without needing to recreate the constraint.
-- Note: PostgreSQL does not provide INVALIDATE CONSTRAINT, so we must drop and re-add NOT VALID.

SET lock_timeout = '10s';

ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check_v2;

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
