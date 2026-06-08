-- +goose Up
ALTER TABLE identity_correction_requests
  ADD COLUMN row_version integer NOT NULL DEFAULT 1,
  ADD CONSTRAINT identity_correction_requests_row_version_check CHECK (row_version >= 1);

ALTER TABLE identity_decisions
  DROP CONSTRAINT identity_decisions_decision_type_check;

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

-- +goose Down
ALTER TABLE identity_decisions
  DROP CONSTRAINT identity_decisions_decision_type_check;

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
      'request_field_verification'
    )
  );

ALTER TABLE identity_correction_requests
  DROP CONSTRAINT IF EXISTS identity_correction_requests_row_version_check,
  DROP COLUMN IF EXISTS row_version;
