-- +goose Up
-- Daily Feed Transport is one task per active physical shed, never per feed session.
-- The 15:30 IST materializer inserts the stable task row; every operator submission appends a
-- separate attempt so rejected proof remains immutable history while rework gets a fresh video.
CREATE TABLE IF NOT EXISTS public.feed_transport_tasks (
  task_id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  business_date date NOT NULL,
  scheduled_at timestamptz NOT NULL,
  status text DEFAULT 'due' NOT NULL,
  operator_id uuid,
  current_attempt_id uuid,
  completed_at timestamptz,
  created_at timestamptz DEFAULT now() NOT NULL,
  updated_at timestamptz DEFAULT now() NOT NULL,
  row_version integer DEFAULT 1 NOT NULL,
  CONSTRAINT feed_transport_tasks_status_check CHECK (status IN ('due','verification_due','rework','completed')),
  CONSTRAINT feed_transport_tasks_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id),
  CONSTRAINT feed_transport_tasks_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS feed_transport_tasks_daily_shed_uq
  ON public.feed_transport_tasks (tenant_id, business_date, shed_id);
CREATE UNIQUE INDEX IF NOT EXISTS feed_transport_tasks_tenant_task_uq
  ON public.feed_transport_tasks (tenant_id, task_id);
CREATE INDEX IF NOT EXISTS feed_transport_tasks_today_idx
  ON public.feed_transport_tasks (tenant_id, business_date, status, shed_id);
CREATE INDEX IF NOT EXISTS feed_transport_tasks_operator_idx
  ON public.feed_transport_tasks (tenant_id, operator_id, business_date, status);

CREATE TABLE IF NOT EXISTS public.feed_transport_attempts (
  attempt_id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  tenant_id uuid NOT NULL,
  task_id uuid NOT NULL,
  attempt_no integer NOT NULL,
  proof_ref text NOT NULL,
  operator_id uuid NOT NULL,
  status text DEFAULT 'verification_due' NOT NULL,
  rejection_reason text,
  verified_by uuid,
  verified_at timestamptz,
  idempotency_key text NOT NULL,
  submitted_at timestamptz DEFAULT now() NOT NULL,
  updated_at timestamptz DEFAULT now() NOT NULL,
  CONSTRAINT feed_transport_attempts_status_check CHECK (status IN ('verification_due','approved','rejected')),
  CONSTRAINT feed_transport_attempts_number_check CHECK (attempt_no >= 1),
  CONSTRAINT feed_transport_attempts_proof_check CHECK (btrim(proof_ref) <> ''),
  CONSTRAINT feed_transport_attempts_task_fk FOREIGN KEY (tenant_id, task_id) REFERENCES public.feed_transport_tasks(tenant_id, task_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS feed_transport_attempts_tenant_attempt_uq
  ON public.feed_transport_attempts (tenant_id, attempt_id);
CREATE UNIQUE INDEX IF NOT EXISTS feed_transport_attempts_number_uq
  ON public.feed_transport_attempts (tenant_id, task_id, attempt_no);
CREATE UNIQUE INDEX IF NOT EXISTS feed_transport_attempts_idempotency_uq
  ON public.feed_transport_attempts (tenant_id, idempotency_key);
CREATE INDEX IF NOT EXISTS feed_transport_attempts_task_history_idx
  ON public.feed_transport_attempts (tenant_id, task_id, attempt_no DESC);

ALTER TABLE public.feed_transport_tasks
  ADD CONSTRAINT feed_transport_tasks_current_attempt_fk
  FOREIGN KEY (tenant_id, current_attempt_id) REFERENCES public.feed_transport_attempts(tenant_id, attempt_id);

-- +goose Down
DROP TABLE IF EXISTS public.feed_transport_attempts, public.feed_transport_tasks;
