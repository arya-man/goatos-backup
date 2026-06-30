-- +goose Up
-- Feed Direction G2 / CSG10: park-first movement windows for Counts projection
-- recompute. The projection worker reads a single tenant/park/date window; these
-- indexes keep source and destination movement scans bounded before joining
-- structured cohort impacts.

CREATE INDEX shifting_events_destination_park_window_idx
  ON shifting_events (
    tenant_id,
    destination_park_id,
    event_status,
    effective_at,
    shifting_event_id
  )
  WHERE event_status IN ('authorized', 'applied');

CREATE INDEX shifting_events_source_park_window_idx
  ON shifting_events (
    tenant_id,
    source_park_id,
    event_status,
    effective_at,
    shifting_event_id
  )
  WHERE source_shed_id IS NOT NULL
    AND event_status IN ('authorized', 'applied');

-- +goose Down
DROP INDEX IF EXISTS shifting_events_source_park_window_idx;
DROP INDEX IF EXISTS shifting_events_destination_park_window_idx;
