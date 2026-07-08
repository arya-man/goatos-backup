-- +goose Up
-- Add the canonical reproductive-change decision used by goat.reproductive.changed producers.
-- Direct analog of 000099 (health_goat): mirroring the health lifecycle path requires the
-- identity_decisions decision_type check to admit the new reproductive_goat decision, otherwise
-- every reproductive status update fails the check constraint on the decision insert.

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
      'health_goat'
    )
  );
