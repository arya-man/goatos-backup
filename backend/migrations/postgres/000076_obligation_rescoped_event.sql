-- +goose Up
-- Phase 1A · SM-2 (goat shift). Allow a 'rescoped' obligation status event so a shift that moves a
-- goat's open obligations to a new shed/park scope is visible in the obligation history, mirroring
-- the 'canceled' (SM-3) and 'deferred' (SM-1) visibility. CHECK-only change; no data migration.
ALTER TABLE obligation_status_events DROP CONSTRAINT obligation_status_events_type_check;
ALTER TABLE obligation_status_events
  ADD CONSTRAINT obligation_status_events_type_check
    CHECK (event_type IN ('scheduled', 'became_due', 'dispatched', 'completed', 'missed', 'waived',
                          'escalated', 'canceled', 'deferred', 'rescoped'));

-- +goose Down
ALTER TABLE obligation_status_events DROP CONSTRAINT obligation_status_events_type_check;
ALTER TABLE obligation_status_events
  ADD CONSTRAINT obligation_status_events_type_check
    CHECK (event_type IN ('scheduled', 'became_due', 'dispatched', 'completed', 'missed', 'waived',
                          'escalated', 'canceled', 'deferred'));
