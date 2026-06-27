-- +goose Up
-- Allow canonical goat lifecycle/movement identity decisions used by the vaccination kernel:
-- move events re-scope open obligations, exit events cancel them.

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
      'exit_goat'
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
      'resolve_correction_request'
    )
  );
