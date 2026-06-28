-- +goose Up
-- Missed work remains actionable for reminders/escalations, so keep it inside the hot reminder index.
DROP INDEX IF EXISTS calendar_event_projections_due_reminder_idx;
CREATE INDEX calendar_event_projections_due_reminder_idx
  ON calendar_event_projections (tenant_id, slice_key, reminder_state, due_at, event_id)
  WHERE system = false
    AND due_at IS NOT NULL
    AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending',
                   'verification_pending', 'rework_due', 'deferred', 'blocked');

-- +goose Down
DROP INDEX IF EXISTS calendar_event_projections_due_reminder_idx;
CREATE INDEX calendar_event_projections_due_reminder_idx
  ON calendar_event_projections (tenant_id, slice_key, reminder_state, due_at, event_id)
  WHERE system = false
    AND due_at IS NOT NULL
    AND status IN ('scheduled', 'due', 'overdue', 'in_progress', 'proof_pending',
                   'verification_pending', 'rework_due', 'deferred', 'blocked');
