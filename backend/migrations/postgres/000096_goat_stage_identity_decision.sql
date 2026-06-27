-- +goose Up
-- Add the canonical stage-change decision used by goat.stage_changed producers.

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
      'stage_goat'
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
      'exit_goat'
    )
  );
