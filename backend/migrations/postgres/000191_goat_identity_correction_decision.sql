-- +goose Up
-- Add the canonical identity-correction decision type used by goat.identity.changed producers. The
-- DOB/entry-date correction command (IdentityGoat) records an identity_decisions row with
-- decision_type='identity_goat', so the check constraint must admit it.
--
-- LOCK-SAFE (VACC-REV-11): identity_decisions is a hot operational table, so we DO NOT drop and
-- re-add a VALIDATED CHECK (that validation scans every existing row under an ACCESS EXCLUSIVE lock,
-- risking a write outage at scale). Instead we add the replacement constraint NOT VALID (a brief
-- metadata-only lock), VALIDATE it separately (SHARE UPDATE EXCLUSIVE — concurrent reads and writes
-- continue), then perform the short metadata-only swap.
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

ALTER TABLE identity_decisions VALIDATE CONSTRAINT identity_decisions_decision_type_check_v2;

ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check;

ALTER TABLE identity_decisions
  RENAME CONSTRAINT identity_decisions_decision_type_check_v2 TO identity_decisions_decision_type_check;

-- +goose Down
-- Restore the pre-000191 constraint (without identity_goat) using the same lock-safe NOT VALID swap.
ALTER TABLE identity_decisions
  ADD CONSTRAINT identity_decisions_decision_type_check_v1 CHECK (
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

ALTER TABLE identity_decisions VALIDATE CONSTRAINT identity_decisions_decision_type_check_v1;

ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check;

ALTER TABLE identity_decisions
  RENAME CONSTRAINT identity_decisions_decision_type_check_v1 TO identity_decisions_decision_type_check;
