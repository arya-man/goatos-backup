-- +goose Up
ALTER TABLE calendar_event_projections
  DROP CONSTRAINT IF EXISTS calendar_event_status_check,
  ADD CONSTRAINT calendar_event_status_check CHECK (status IN (
    'scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending',
    'verification_pending', 'rejected', 'rework_due', 'deferred', 'blocked',
    'completed', 'canceled'
  ));

-- +goose Down
ALTER TABLE calendar_event_projections
  DROP CONSTRAINT IF EXISTS calendar_event_status_check,
  ADD CONSTRAINT calendar_event_status_check CHECK (status IN (
    'scheduled', 'due', 'overdue', 'in_progress', 'proof_pending',
    'verification_pending', 'rejected', 'rework_due', 'deferred', 'blocked',
    'completed', 'canceled'
  ));
