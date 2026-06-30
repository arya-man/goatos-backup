-- +goose Up
-- Feed Direction G2: close Counts/Shifting projection exceptions through a
-- durable reviewed workflow instead of manual SQL.

CREATE TABLE count_projection_exception_resolutions (
  count_projection_exception_resolution_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  count_projection_exception_id uuid NOT NULL REFERENCES count_projection_exceptions(count_projection_exception_id) ON DELETE CASCADE,
  action text NOT NULL,
  resolved_by_ref text NOT NULL,
  resolution_reason text NOT NULL,
  resolution_ref text NULL,
  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT count_projection_exception_resolutions_action_check
    CHECK (action IN ('resolve', 'dismiss')),
  CONSTRAINT count_projection_exception_resolutions_actor_check
    CHECK (btrim(resolved_by_ref) <> ''),
  CONSTRAINT count_projection_exception_resolutions_reason_check
    CHECK (btrim(resolution_reason) <> ''),
  CONSTRAINT count_projection_exception_resolutions_idem_check
    CHECK (btrim(idempotency_key) <> '' AND btrim(request_fingerprint) <> '')
);

CREATE UNIQUE INDEX count_projection_exception_resolutions_idempotency_unique
  ON count_projection_exception_resolutions (tenant_id, idempotency_key);

CREATE INDEX count_projection_exception_resolutions_exception_idx
  ON count_projection_exception_resolutions (
    tenant_id,
    count_projection_exception_id,
    created_at DESC,
    count_projection_exception_resolution_id DESC
  );

ALTER TABLE count_projection_exceptions
  ADD COLUMN resolution_id uuid NULL REFERENCES count_projection_exception_resolutions(count_projection_exception_resolution_id),
  ADD COLUMN resolved_by_ref text NULL,
  ADD COLUMN resolution_reason text NULL,
  ADD COLUMN resolution_ref text NULL;

CREATE INDEX count_projection_exceptions_closed_idx
  ON count_projection_exceptions (
    tenant_id,
    status,
    resolved_at DESC,
    count_projection_exception_id DESC
  )
  WHERE status IN ('resolved', 'dismissed');

-- +goose Down
DROP INDEX IF EXISTS count_projection_exceptions_closed_idx;

ALTER TABLE count_projection_exceptions
  DROP COLUMN IF EXISTS resolution_ref,
  DROP COLUMN IF EXISTS resolution_reason,
  DROP COLUMN IF EXISTS resolved_by_ref,
  DROP COLUMN IF EXISTS resolution_id;

DROP INDEX IF EXISTS count_projection_exception_resolutions_exception_idx;
DROP INDEX IF EXISTS count_projection_exception_resolutions_idempotency_unique;
DROP TABLE IF EXISTS count_projection_exception_resolutions;
