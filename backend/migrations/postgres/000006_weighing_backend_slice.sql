-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing free-flow tables are written by the Weighing planner/mobile flow; they do not change the Vaccination HRMS seed contract
CREATE TABLE IF NOT EXISTS public.weighing_campaigns (
  campaign_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  park_id uuid NOT NULL REFERENCES public.locations(location_id),
  period_type text NOT NULL DEFAULT 'week',
  period_start_date date NOT NULL,
  period_end_date date NOT NULL,
  cadence_type text NOT NULL DEFAULT 'weekly_kids',
  start_business_date date NOT NULL,
  status text NOT NULL DEFAULT 'draft',
  planned_cap_per_day integer NOT NULL DEFAULT 100 CHECK (planned_cap_per_day > 0),
  operator_user_id uuid NOT NULL,
  published_at timestamptz,
  completed_at timestamptz,
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
  CONSTRAINT weighing_campaigns_period_type_check CHECK (period_type = 'week'),
  CONSTRAINT weighing_campaigns_cadence_type_check CHECK (cadence_type = 'weekly_kids'),
  CONSTRAINT weighing_campaigns_status_check CHECK (status = ANY (ARRAY['draft','published','in_progress','delayed','completed','canceled'])),
  CONSTRAINT weighing_campaigns_period_check CHECK (period_end_date >= period_start_date)
);

CREATE UNIQUE INDEX IF NOT EXISTS weighing_campaigns_one_active_week_per_park_idx
  ON public.weighing_campaigns (tenant_id, park_id, period_type, cadence_type, period_start_date)
  WHERE status <> 'canceled';

CREATE TABLE IF NOT EXISTS public.weighing_campaign_sheds (
  campaign_shed_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_id uuid NOT NULL REFERENCES public.weighing_campaigns(campaign_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  location_id uuid NOT NULL REFERENCES public.locations(location_id),
  location_type text NOT NULL,
  display_name text NOT NULL,
  expected_animal_count integer NOT NULL DEFAULT 0 CHECK (expected_animal_count >= 0),
  weighing_category text NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT weighing_campaign_sheds_category_check CHECK (weighing_category = ANY (ARRAY['individual_animal','per_shed_partition'])),
  CONSTRAINT weighing_campaign_sheds_status_check CHECK (status = ANY (ARRAY['pending','in_progress','completed','canceled'])),
  CONSTRAINT weighing_campaign_sheds_location_type_check CHECK (location_type = ANY (ARRAY['shed','cohort','pen']))
);

CREATE UNIQUE INDEX IF NOT EXISTS weighing_campaign_sheds_campaign_location_uidx
  ON public.weighing_campaign_sheds (tenant_id, campaign_id, location_id);

CREATE TABLE IF NOT EXISTS public.weighing_expected_animals (
  campaign_id uuid NOT NULL REFERENCES public.weighing_campaigns(campaign_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  animal_id uuid REFERENCES public.goats(goat_id),
  scanned_identifier text NOT NULL DEFAULT '',
  expected_location_id uuid NOT NULL REFERENCES public.locations(location_id),
  expected_location_label text NOT NULL,
  campaign_shed_id uuid NOT NULL REFERENCES public.weighing_campaign_sheds(campaign_shed_id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'pending',
  availability_status text NOT NULL DEFAULT 'expected_shed',
  current_location_id uuid,
  current_location_label text,
  current_lifecycle_status text,
  availability_checked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT weighing_expected_animals_pk PRIMARY KEY (campaign_id, animal_id),
  CONSTRAINT weighing_expected_animals_status_check CHECK (status = ANY (ARRAY['pending','weighed','unavailable','missed','canceled','closed_by_override'])),
  CONSTRAINT weighing_expected_animals_availability_check CHECK (availability_status = ANY (ARRAY['expected_shed','moved_other_shed','icu','quarantine','dead','culled','sold_transferred','exited','unknown_review']))
);

CREATE INDEX IF NOT EXISTS weighing_expected_animals_location_idx
  ON public.weighing_expected_animals (tenant_id, campaign_id, expected_location_id, status, animal_id);

CREATE TABLE IF NOT EXISTS public.weighing_observations (
  observation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  campaign_id uuid NOT NULL REFERENCES public.weighing_campaigns(campaign_id) ON DELETE CASCADE,
  campaign_shed_id uuid REFERENCES public.weighing_campaign_sheds(campaign_shed_id),
  animal_id uuid REFERENCES public.goats(goat_id),
  scanned_identifier text NOT NULL DEFAULT '',
  weight_kg numeric(8,3) NOT NULL CHECK (weight_kg > 0),
  proof_artifact_id uuid NOT NULL REFERENCES public.proof_artifacts(proof_id),
  expected_location_id uuid,
  expected_location_label text,
  actual_location_id uuid,
  actual_location_label text,
  mismatch_status text NOT NULL DEFAULT 'expected_shed',
  recorded_by uuid NOT NULL,
  accepted_at timestamptz NOT NULL DEFAULT now(),
  idempotency_key text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT weighing_observations_mismatch_check CHECK (mismatch_status = ANY (ARRAY['expected_shed','wrong_shed','extra_scan'])),
  CONSTRAINT weighing_observations_animal_or_identifier_check CHECK (animal_id IS NOT NULL OR btrim(scanned_identifier) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS weighing_observations_idempotency_uidx
  ON public.weighing_observations (tenant_id, idempotency_key);
CREATE INDEX IF NOT EXISTS weighing_observations_campaign_animal_idx
  ON public.weighing_observations (tenant_id, campaign_id, animal_id, accepted_at DESC);
CREATE INDEX IF NOT EXISTS weighing_observations_campaign_scanned_identifier_idx
  ON public.weighing_observations (tenant_id, campaign_id, scanned_identifier, accepted_at DESC);

CREATE TABLE IF NOT EXISTS public.weighing_shed_observations (
  shed_observation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  campaign_id uuid NOT NULL REFERENCES public.weighing_campaigns(campaign_id) ON DELETE CASCADE,
  campaign_shed_id uuid NOT NULL REFERENCES public.weighing_campaign_sheds(campaign_shed_id) ON DELETE CASCADE,
  weight_kg numeric(10,3) NOT NULL CHECK (weight_kg > 0),
  average_weight_kg numeric(8,3) NOT NULL CHECK (average_weight_kg > 0),
  animal_count integer NOT NULL CHECK (animal_count > 0),
  proof_artifact_id uuid NOT NULL REFERENCES public.proof_artifacts(proof_id),
  recorded_by uuid NOT NULL,
  accepted_at timestamptz NOT NULL DEFAULT now(),
  idempotency_key text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS weighing_shed_observations_idempotency_uidx
  ON public.weighing_shed_observations (tenant_id, idempotency_key);
CREATE UNIQUE INDEX IF NOT EXISTS weighing_shed_observations_one_active_scope_uidx
  ON public.weighing_shed_observations (tenant_id, campaign_shed_id);

CREATE TABLE IF NOT EXISTS public.weighing_shed_observation_proofs (
  shed_observation_id uuid NOT NULL REFERENCES public.weighing_shed_observations(shed_observation_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  proof_artifact_id uuid NOT NULL REFERENCES public.proof_artifacts(proof_id),
  proof_position smallint NOT NULL CHECK (proof_position BETWEEN 1 AND 5),
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT weighing_shed_observation_proofs_pk PRIMARY KEY (shed_observation_id, proof_position),
  CONSTRAINT weighing_shed_observation_proofs_unique_artifact UNIQUE (shed_observation_id, proof_artifact_id)
);

CREATE INDEX IF NOT EXISTS weighing_shed_observation_proofs_tenant_observation_idx
  ON public.weighing_shed_observation_proofs (tenant_id, shed_observation_id, proof_position);

-- +goose Down
DROP TABLE IF EXISTS public.weighing_shed_observation_proofs;
DROP TABLE IF EXISTS public.weighing_shed_observations;
DROP TABLE IF EXISTS public.weighing_observations;
DROP TABLE IF EXISTS public.weighing_expected_animals;
DROP TABLE IF EXISTS public.weighing_campaign_sheds;
DROP TABLE IF EXISTS public.weighing_campaigns;
