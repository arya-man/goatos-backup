-- +goose Up
/*
CAL-MAIN-03 (P1): persist the calendar reconciler's keyset cursor across runs.

The CalendarReconcilerStage inspects a bounded number of orphan pages per daily
run (perRunCap). Migration 000202 gave the reconcile function a keyset cursor so
a single run no longer rescans with LIMIT/OFFSET, but without persisting the
cursor every daily run would still restart at the beginning and records ordered
beyond the per-run cap would never be inspected.

This operational table stores the last (source_table, record_id) the stage
processed for each tenant. The stage resumes from that cursor on the next run and
wraps back to the start once it exhausts the current orphan set, so every record
is eventually inspected. It is operational progress state, not an app-visible
read model, and is not seeded (an absent row means "start from the beginning").
*/

CREATE TABLE IF NOT EXISTS calendar_reconciler_progress (
  tenant_id            uuid PRIMARY KEY,
  cursor_source_table  text NOT NULL DEFAULT '',
  cursor_record_id     text NOT NULL DEFAULT '',
  updated_at           timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS calendar_reconciler_progress;
