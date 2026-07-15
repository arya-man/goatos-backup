-- +goose Up
-- Add the canonical identity-correction decision used by goat.identity.changed producers. Direct
-- analog of 000146 (reproductive_goat) / 000099 (health_goat): the DOB/entry-date correction command
-- (IdentityGoat) records an identity_decisions row with decision_type='identity_goat', so the check
-- constraint must admit it, otherwise every correction fails the decision insert.

ALTER TABLE identity_decisions
  DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check;

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
      'reproductive_goat',
      'identity_goat'
    )
  );

-- +goose Down
ALTER TABLE identity_decisions
  DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check;

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
  );
