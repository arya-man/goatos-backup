-- +goose Up
CREATE TABLE IF NOT EXISTS public.weighing_idempotency_records (
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  event_type text NOT NULL,
  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  resource_type text NOT NULL,
  resource_id uuid NOT NULL,
  result_snapshot jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, event_type, idempotency_key)
);

CREATE INDEX IF NOT EXISTS weighing_idempotency_records_resource_idx
  ON public.weighing_idempotency_records (tenant_id, resource_type, resource_id);

-- +goose Down
DROP TABLE IF EXISTS public.weighing_idempotency_records;
