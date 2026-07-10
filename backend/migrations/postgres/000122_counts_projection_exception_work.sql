-- +goose Up
-- Feed Direction G2: make Counts/Shifting projection exceptions visible as
-- owner-routable process work, not only opaque blocker rows.

ALTER TABLE count_projection_exceptions
  ADD COLUMN work_type text NOT NULL DEFAULT 'counts_projection_exception',
  ADD COLUMN work_state text NOT NULL DEFAULT 'blocked',
  ADD COLUMN due_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN next_action text NOT NULL DEFAULT 'Review Counts/Shifting projection exception',
  ADD COLUMN evidence_link text NOT NULL DEFAULT '/feed-direction/counts-projection/exceptions';

ALTER TABLE count_projection_exceptions
  ADD CONSTRAINT count_projection_exceptions_work_type_check
    CHECK (work_type IN ('counts_projection_exception')),
  ADD CONSTRAINT count_projection_exceptions_work_state_check
    CHECK (work_state IN ('blocked', 'resolved', 'dismissed')),
  ADD CONSTRAINT count_projection_exceptions_next_action_check
    CHECK (btrim(next_action) <> ''),
  ADD CONSTRAINT count_projection_exceptions_evidence_link_check
    CHECK (btrim(evidence_link) <> '');

CREATE INDEX count_projection_exceptions_work_queue_idx
  ON count_projection_exceptions (
    tenant_id,
    status,
    work_state,
    severity,
    due_at,
    updated_at DESC,
    count_projection_exception_id DESC
  )
  WHERE status = 'open';

-- +goose Down
DROP INDEX IF EXISTS count_projection_exceptions_work_queue_idx;

ALTER TABLE count_projection_exceptions
  DROP CONSTRAINT IF EXISTS count_projection_exceptions_evidence_link_check,
  DROP CONSTRAINT IF EXISTS count_projection_exceptions_next_action_check,
  DROP CONSTRAINT IF EXISTS count_projection_exceptions_work_state_check,
  DROP CONSTRAINT IF EXISTS count_projection_exceptions_work_type_check,
  DROP COLUMN IF EXISTS evidence_link,
  DROP COLUMN IF EXISTS next_action,
  DROP COLUMN IF EXISTS due_at,
  DROP COLUMN IF EXISTS work_state,
  DROP COLUMN IF EXISTS work_type;
