-- +goose Up
-- Bounded incremental shed-projection dirty-scope queue + per-shed shard freshness state (P0-B,
-- context/execution/api-projection-performance-handoff-2026-07-13.md). Replaces the every-5-min
-- full-tenant vaccination_shed_projection_rows rebuild (RecomputeShedProjection) with per-shed
-- incremental maintenance: a write enqueues ONLY the affected shed(s); a leased worker
-- (cmd/vaccination-projection-worker) rebuilds ONLY that shed's row into the CURRENT serving
-- projection_version (see incremental_shed_projection.go RebuildShedShard). RecomputeShedProjection
-- remains the explicit bootstrap/repair path, unchanged.
--
-- Two REJECTED designs this schema must not reproduce (handoff doc, "Rejected Unsafe Work"):
--   1. a "bounded" worker that copied every unchanged tenant row into a new global version per
--      dirty shed (still O(total tenant rows) per dirty shed);
--   2. an in-place refresh that stamped ONE tenant-wide as_of/freshness over sheds rebuilt at
--      different times (untouched sheds silently missed scheduled->due->overdue while the API
--      reported green).
-- This design tracks freshness PER SHED (vaccination_shed_shard_state) and rebuilds ONLY the
-- claimed shed's row.

CREATE TABLE vaccination_projection_dirty_scopes (
  dirty_scope_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  projection_kind text NOT NULL,
  shed_id text NOT NULL,
  reason text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending',
  lease_owner text,
  leased_at timestamptz,
  lease_expires_at timestamptz,
  attempt_count int NOT NULL DEFAULT 0,
  max_attempts int NOT NULL DEFAULT 8,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  last_error text,
  enqueued_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT vaccination_projection_dirty_scopes_kind_check CHECK (projection_kind IN ('shed')),
  CONSTRAINT vaccination_projection_dirty_scopes_status_check CHECK (status IN ('pending', 'leased', 'done', 'failed', 'dead_letter')),
  CONSTRAINT vaccination_projection_dirty_scopes_attempt_check CHECK (attempt_count >= 0 AND max_attempts > 0)
);

COMMENT ON TABLE vaccination_projection_dirty_scopes IS
  'Durable coalescing queue for the bounded incremental vaccination-shed projector. One row = one shed needs its projection row rebuilt. A write enqueues the affected shed(s) (EnqueueDirtyShed/EnqueueDirtySheds); a leased worker (cmd/vaccination-projection-worker) claims pending rows FOR UPDATE SKIP LOCKED (mirrors backend/internal/outbox ClaimPending) and calls RebuildShedShard for exactly that shed. projection_kind is reserved for execution/operations/process-integrity dirty-scope kinds later; only shed is implemented today.';

-- Coalescing: re-enqueuing an already-queued shed is a no-op that just bumps reason/updated_at and
-- (if it had drifted to failed/dead_letter) resets status back to pending -- never a duplicate row,
-- so a busy shed with many writes queues at most one outstanding rebuild.
CREATE UNIQUE INDEX vaccination_projection_dirty_scopes_coalesce_uidx
  ON vaccination_projection_dirty_scopes (tenant_id, projection_kind, shed_id)
  WHERE status IN ('pending', 'leased');

-- Claim index: keyset-chunked `FOR UPDATE SKIP LOCKED` claim ordered by readiness then id, the same
-- shape backend/internal/outbox/adapters/postgres/repository.go ClaimPending uses.
CREATE INDEX vaccination_projection_dirty_scopes_claim_idx
  ON vaccination_projection_dirty_scopes (status, next_attempt_at, dirty_scope_id);

CREATE TABLE vaccination_shed_shard_state (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  shed_id text NOT NULL,
  projected_at timestamptz NOT NULL,
  as_of timestamptz NOT NULL,
  next_transition_at timestamptz,
  source_watermark timestamptz,
  row_present boolean NOT NULL DEFAULT true,
  serving_state text NOT NULL DEFAULT 'fresh',
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, shed_id),
  CONSTRAINT vaccination_shed_shard_state_serving_check CHECK (serving_state IN ('fresh', 'stale', 'rebuilding', 'failed'))
);

COMMENT ON TABLE vaccination_shed_shard_state IS
  'PER-SHED freshness truth for the incremental vaccination-shed projector -- never a tenant-wide timestamp (the rejected in-place-refresh design). RebuildShedShard stamps exactly the shed it rebuilt; a stale row here means only that shed may be behind, not the whole tenant. next_transition_at is the shed''s earliest upcoming scheduled->due->overdue boundary, so EnqueueDueTransitions can enqueue time-driven work without replaying every stable shed. row_present=false marks a shed whose projection row was deleted (no animals/obligations) without deleting the shard-state history row itself.';

CREATE INDEX vaccination_shed_shard_state_next_transition_idx
  ON vaccination_shed_shard_state (tenant_id, next_transition_at);

CREATE INDEX vaccination_shed_shard_state_projected_idx
  ON vaccination_shed_shard_state (tenant_id, projected_at);

-- +goose Down
DROP TABLE IF EXISTS vaccination_shed_shard_state;
DROP TABLE IF EXISTS vaccination_projection_dirty_scopes;
