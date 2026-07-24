-- +goose Up
-- Idempotency dedupe table for the operator-config auto-cascade consumer
-- (backend/internal/obligation/app/operator_config_replan.go). On
-- vaccination.capacity.changed / vaccination.roster.changed / vaccination.leave.changed, the consumer
-- claims a row here keyed by (tenant_id, event_id) BEFORE calling the existing
-- RecomputeFutureVaccinationDrives release path. A replay of the SAME event_id is a no-op (ON CONFLICT
-- DO NOTHING => 0 rows affected => consumer returns without re-invoking recompute), matching the
-- watermark-claim-in-tx pattern used by obligation_goat_shift_watermarks for goat.location.changed.
CREATE TABLE IF NOT EXISTS public.obligation_operator_config_replan_watermarks (
  tenant_id uuid NOT NULL,
  event_id text NOT NULL,
  park_id uuid NOT NULL,
  event_type text NOT NULL,
  processed_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT obligation_operator_config_replan_watermarks_pkey PRIMARY KEY (tenant_id, event_id)
);

CREATE INDEX IF NOT EXISTS obligation_operator_config_replan_watermarks_park_idx
  ON public.obligation_operator_config_replan_watermarks (tenant_id, park_id);

-- +goose Down
DROP TABLE IF EXISTS public.obligation_operator_config_replan_watermarks;
