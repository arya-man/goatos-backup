-- +goose Up
/*
CAL-MAIN-02 (P1): persist the reminder-cadence sweep's keyset cursor across ticks.

The ReminderCadenceStage sweeps a bounded number of candidate pages per tick
(perTickLimit x maxIterations). Migration-free code change gave the candidate
scan a keyset cursor (ORDER BY (due_at, event_id), resume from a cursor) so a
single tick no longer wastes its LIMIT re-reading a completed first page, but
without persisting the cursor every tick would still restart at the beginning
and candidates ordered beyond a single tick's page cap would starve.

This operational table stores the last (due_at, event_id) the stage processed
for each tenant. The stage resumes from that cursor on the next tick and wraps
back to the start once it exhausts the current candidate set, so every candidate
is eventually reached. It is operational progress state, not an app-visible read
model, and is not seeded (an absent row means "start from the beginning"). The
zero-cursor sentinel is an empty cursor_event_id.
*/

CREATE TABLE IF NOT EXISTS reminder_cadence_progress (
  tenant_id         uuid PRIMARY KEY,
  cursor_due_at     timestamptz NOT NULL DEFAULT to_timestamp(0),
  cursor_event_id   text NOT NULL DEFAULT '',
  updated_at        timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS reminder_cadence_progress;
