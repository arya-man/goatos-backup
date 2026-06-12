-- +goose Up
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY IF NOT EXISTS goat_identity_events_2026_06_goat_timeline_keyset_idx
  ON goat_identity_events_2026_06 (tenant_id, goat_id, occurred_at DESC, identity_event_id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS goat_identity_events_2026_07_goat_timeline_keyset_idx
  ON goat_identity_events_2026_07 (tenant_id, goat_id, occurred_at DESC, identity_event_id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS goat_identity_events_2026_08_goat_timeline_keyset_idx
  ON goat_identity_events_2026_08 (tenant_id, goat_id, occurred_at DESC, identity_event_id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS goat_identity_events_2026_09_goat_timeline_keyset_idx
  ON goat_identity_events_2026_09 (tenant_id, goat_id, occurred_at DESC, identity_event_id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS goat_identity_events_default_goat_timeline_keyset_idx
  ON goat_identity_events_default (tenant_id, goat_id, occurred_at DESC, identity_event_id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS identity_correction_requests_actor_keyset_idx
  ON identity_correction_requests (tenant_id, requested_by, created_at DESC, correction_request_id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS identity_correction_requests_admin_keyset_idx
  ON identity_correction_requests (tenant_id, created_at DESC, correction_request_id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS identity_correction_requests_admin_state_keyset_idx
  ON identity_correction_requests (tenant_id, state, created_at DESC, correction_request_id DESC);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS identity_correction_requests_admin_state_keyset_idx;
DROP INDEX CONCURRENTLY IF EXISTS identity_correction_requests_admin_keyset_idx;
DROP INDEX CONCURRENTLY IF EXISTS identity_correction_requests_actor_keyset_idx;
DROP INDEX CONCURRENTLY IF EXISTS goat_identity_events_default_goat_timeline_keyset_idx;
DROP INDEX CONCURRENTLY IF EXISTS goat_identity_events_2026_09_goat_timeline_keyset_idx;
DROP INDEX CONCURRENTLY IF EXISTS goat_identity_events_2026_08_goat_timeline_keyset_idx;
DROP INDEX CONCURRENTLY IF EXISTS goat_identity_events_2026_07_goat_timeline_keyset_idx;
DROP INDEX CONCURRENTLY IF EXISTS goat_identity_events_2026_06_goat_timeline_keyset_idx;
