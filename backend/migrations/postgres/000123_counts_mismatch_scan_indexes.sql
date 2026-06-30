-- +goose Up
-- Feed Direction G2 / CSG6: bounded source-side scans for comparing a new
-- physical Base Count anchor against applied ShiftingEvents since the previous
-- anchor.

CREATE INDEX shifting_events_source_window_idx
  ON shifting_events (
    tenant_id,
    event_status,
    effective_at,
    source_park_id,
    source_shed_id,
    shifting_event_id
  )
  WHERE source_shed_id IS NOT NULL;

CREATE INDEX shifting_event_impacts_event_breed_idx
  ON shifting_event_impacts (tenant_id, shifting_event_id, lower(breed_key));

-- +goose Down
DROP INDEX IF EXISTS shifting_event_impacts_event_breed_idx;
DROP INDEX IF EXISTS shifting_events_source_window_idx;
