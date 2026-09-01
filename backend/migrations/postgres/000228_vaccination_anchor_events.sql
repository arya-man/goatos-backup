-- +goose Up
-- seed-fixture-guard:ignore: additive operational anchor table; no seed source or fixture data changes.

CREATE TABLE IF NOT EXISTS vaccination_anchor_events (
  vaccination_anchor_event_id uuid PRIMARY KEY,
  tenant_id uuid NOT NULL,
  protocol_version_id uuid NULL,
  vaccine_code text NOT NULL,
  dose_code text NULL,
  anchor_date date NOT NULL,
  scope_type text NOT NULL,
  scope_payload jsonb NOT NULL,
  suppress_before_anchor boolean NOT NULL DEFAULT true,
  chain_future_from_anchor boolean NOT NULL DEFAULT true,
  enforce_age_eligibility boolean NOT NULL DEFAULT true,
  reason text NOT NULL,
  source_system text NOT NULL,
  source_ref text NULL,
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  canceled_at timestamptz NULL,
  canceled_by uuid NULL,
  cancel_reason text NULL,
  idempotency_key text NOT NULL,
  request_hash text NOT NULL DEFAULT '',
  CONSTRAINT vaccination_anchor_events_scope_type_chk
    CHECK (scope_type IN ('animal_set', 'park', 'shed', 'partition', 'tenant')),
  CONSTRAINT vaccination_anchor_events_vaccine_code_chk
    CHECK (btrim(vaccine_code) <> ''),
  CONSTRAINT vaccination_anchor_events_reason_chk
    CHECK (btrim(reason) <> ''),
  CONSTRAINT vaccination_anchor_events_source_system_chk
    CHECK (btrim(source_system) <> ''),
  CONSTRAINT vaccination_anchor_events_idempotency_key_chk
    CHECK (btrim(idempotency_key) <> ''),
  CONSTRAINT vaccination_anchor_events_request_hash_chk
    CHECK (request_hash = '' OR length(request_hash) = 64),
  CONSTRAINT vaccination_anchor_events_cancel_state_chk
    CHECK (
      (canceled_at IS NULL AND canceled_by IS NULL AND cancel_reason IS NULL)
      OR (canceled_at IS NOT NULL AND cancel_reason IS NOT NULL AND btrim(cancel_reason) <> '')
    ),
  CONSTRAINT vaccination_anchor_events_scope_payload_object_chk
    CHECK (jsonb_typeof(scope_payload) = 'object')
);

CREATE UNIQUE INDEX IF NOT EXISTS vaccination_anchor_events_tenant_idempotency_uidx
  ON vaccination_anchor_events (tenant_id, idempotency_key);

CREATE INDEX IF NOT EXISTS vaccination_anchor_events_tenant_vaccine_date_idx
  ON vaccination_anchor_events (tenant_id, vaccine_code, anchor_date);

CREATE INDEX IF NOT EXISTS vaccination_anchor_events_active_idx
  ON vaccination_anchor_events (tenant_id, lower(btrim(vaccine_code)), anchor_date)
  WHERE canceled_at IS NULL;

CREATE INDEX IF NOT EXISTS vaccination_anchor_events_scope_payload_gin_idx
  ON vaccination_anchor_events USING gin (scope_payload)
  WHERE canceled_at IS NULL;

CREATE INDEX IF NOT EXISTS vaccination_anchor_events_active_chain_idx
  ON vaccination_anchor_events (tenant_id, protocol_version_id, lower(btrim(vaccine_code)), lower(btrim(dose_code)), anchor_date)
  WHERE canceled_at IS NULL AND chain_future_from_anchor AND protocol_version_id IS NOT NULL AND dose_code IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS vaccination_anchor_events;
