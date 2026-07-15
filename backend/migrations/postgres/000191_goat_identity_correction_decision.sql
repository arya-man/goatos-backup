-- +goose Up
-- +goose NO TRANSACTION
-- Add the canonical identity-correction decision type used by goat.identity.changed producers. The
-- DOB/entry-date correction command (IdentityGoat) records an identity_decisions row with
-- decision_type='identity_goat', so the check constraint must admit it.
--
-- LOCK-SAFE (VACC-REV-11): identity_decisions is a hot operational table, so we DO NOT drop and
-- re-add a VALIDATED CHECK (that validation scans every existing row under an ACCESS EXCLUSIVE lock,
-- risking a write outage at scale). This migration runs NO TRANSACTION so each statement commits
-- independently — the ADD ... NOT VALID takes only a brief metadata ACCESS EXCLUSIVE and RELEASES it
-- on commit; the separate VALIDATE then scans under SHARE UPDATE EXCLUSIVE (concurrent reads/writes
-- continue) without holding the ADD's lock; the DROP old + RENAME are brief metadata-only swaps.
-- Because 'identity_goat' only WIDENS the allowed set, VALIDATE cannot fail on existing rows, so a
-- non-transactional run cannot leave the table in a half-constrained state.
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
-- +goose NO TRANSACTION
-- Restore the pre-000191 constraint (without identity_goat) using the same lock-safe NOT VALID swap.
-- VACC-REV-13: the narrower v1 set EXCLUDES 'identity_goat'. Once the up-migration has been live, real
-- identity_decisions rows with decision_type='identity_goat' exist, and a VALIDATE of v1 would scan
-- those rows and FAIL — a down migration must never fail on legitimately-produced data. So the down is
-- deliberately left NOT VALID (unvalidated): a NOT VALID CHECK still ENFORCES on every new INSERT/UPDATE
-- (blocking new 'identity_goat' writes, which is the intended rollback), while tolerating the existing
-- identity_goat rows that the up-migration's feature legitimately created. Reverting the app code is the
-- caller's responsibility; the schema rollback stays data-safe and non-failing regardless of row content.
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

ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check;

ALTER TABLE identity_decisions
  RENAME CONSTRAINT identity_decisions_decision_type_check_v1 TO identity_decisions_decision_type_check;
