-- +goose Up
SET lock_timeout = '5s';

-- One current preparation run per physical park and India business date. Operator submission is
-- never completion: the row reaches completed only when the generic Verification module approves
-- the full per-step video set.
CREATE TABLE public.milk_preparation_completions (
  completion_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  preparation_date date NOT NULL,
  feeding_date date NOT NULL,
  status text DEFAULT 'pending_verification' NOT NULL,
  current_attempt_no integer DEFAULT 1 NOT NULL,
  goat_milk_used boolean DEFAULT false NOT NULL,
  submitted_by uuid NOT NULL,
  submitted_at timestamptz DEFAULT now() NOT NULL,
  verified_by uuid,
  verified_at timestamptz,
  rework_reason text,
  row_version integer DEFAULT 1 NOT NULL,
  created_at timestamptz DEFAULT now() NOT NULL,
  updated_at timestamptz DEFAULT now() NOT NULL,
  CONSTRAINT milk_preparation_completions_pkey PRIMARY KEY (completion_id),
  CONSTRAINT milk_preparation_completions_tenant_id_uq UNIQUE (tenant_id, completion_id),
  CONSTRAINT milk_preparation_completions_natural_uq UNIQUE (tenant_id, park_id, preparation_date),
  CONSTRAINT milk_preparation_completions_status_check
    CHECK (status IN ('pending_verification', 'completed', 'rework')),
  CONSTRAINT milk_preparation_completions_attempt_check CHECK (current_attempt_no >= 1),
  CONSTRAINT milk_preparation_completions_date_check CHECK (feeding_date = preparation_date + 1),
  CONSTRAINT milk_preparation_completions_park_fk
    FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id)
);

-- Every submission/rework is immutable evidence history. proof_refs is a step_code -> proof_id
-- object; the app layer and proof adapter require one distinct completed live-camera video for each
-- applicable step before this row can be inserted.
CREATE TABLE public.milk_preparation_proof_attempts (
  attempt_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  completion_id uuid NOT NULL,
  attempt_no integer NOT NULL,
  goat_milk_used boolean NOT NULL,
  proof_refs jsonb NOT NULL,
  submitted_by uuid NOT NULL,
  submitted_at timestamptz DEFAULT now() NOT NULL,
  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  CONSTRAINT milk_preparation_proof_attempts_pkey PRIMARY KEY (attempt_id),
  CONSTRAINT milk_preparation_proof_attempts_attempt_uq UNIQUE (tenant_id, completion_id, attempt_no),
  CONSTRAINT milk_preparation_proof_attempts_idempotency_uq UNIQUE (tenant_id, idempotency_key),
  CONSTRAINT milk_preparation_proof_attempts_attempt_check CHECK (attempt_no >= 1),
  CONSTRAINT milk_preparation_proof_attempts_proofs_check
    CHECK (jsonb_typeof(proof_refs) = 'object' AND jsonb_object_length(proof_refs) IN (2, 5)),
  CONSTRAINT milk_preparation_proof_attempts_completion_fk
    FOREIGN KEY (tenant_id, completion_id)
    REFERENCES public.milk_preparation_completions(tenant_id, completion_id)
    ON DELETE RESTRICT
);

CREATE INDEX milk_preparation_completions_serving_idx
  ON public.milk_preparation_completions (tenant_id, preparation_date, park_id);

CREATE INDEX milk_preparation_proof_attempts_completion_idx
  ON public.milk_preparation_proof_attempts (tenant_id, completion_id, attempt_no DESC);

-- +goose Down
SET lock_timeout = '5s';
DROP TABLE IF EXISTS public.milk_preparation_proof_attempts;
DROP TABLE IF EXISTS public.milk_preparation_completions;
