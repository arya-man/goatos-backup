-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing free-flow tables are written by the Weighing planner/mobile/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- Two additions, both Weighing-owned:
--
-- 1. EXPLICIT CLOSE. Until now a weighing bucket (campaign shed) or a whole
--    campaign could only reach a terminal state automatically, when every
--    expected animal resolved (completeIndividualScopeIfDone /
--    completeCampaignIfDone). Leadership needs to end work that will never
--    finish. Close is a DISTINCT terminal status from 'completed': closing a
--    bucket with work that was never accepted must never read back as accepted
--    work, so we add 'closed' rather than reusing 'completed', and we record the
--    reason/actor/closed_at on the row.
--
-- 2. VERIFICATION VERDICT STATE. Weighing enqueues a generic verification item
--    for every observation but had nowhere to store the verifier's answer, so
--    every approve/rework verdict was a silent drop. Observations now carry
--    their own verdict state; 'rework' is what makes the owning bucket
--    operator-actionable again.
--
-- Free-flow is preserved: nothing here references goats, herd rosters, or
-- vaccination, nothing requires animal_id, and no uniqueness is added over
-- scanned_identifier.

ALTER TABLE public.weighing_campaign_sheds
  DROP CONSTRAINT IF EXISTS weighing_campaign_sheds_status_check;
ALTER TABLE public.weighing_campaign_sheds
  ADD CONSTRAINT weighing_campaign_sheds_status_check
  CHECK (status = ANY (ARRAY['pending','in_progress','completed','closed','canceled']));

ALTER TABLE public.weighing_campaign_sheds
  ADD COLUMN IF NOT EXISTS closed_at timestamptz,
  ADD COLUMN IF NOT EXISTS closed_by uuid,
  ADD COLUMN IF NOT EXISTS close_reason text,
  ADD COLUMN IF NOT EXISTS closed_not_accepted_count integer;

ALTER TABLE public.weighing_campaigns
  DROP CONSTRAINT IF EXISTS weighing_campaigns_status_check;
ALTER TABLE public.weighing_campaigns
  ADD CONSTRAINT weighing_campaigns_status_check
  CHECK (status = ANY (ARRAY['draft','published','in_progress','delayed','completed','closed','canceled']));

ALTER TABLE public.weighing_campaigns
  ADD COLUMN IF NOT EXISTS closed_at timestamptz,
  ADD COLUMN IF NOT EXISTS closed_by uuid,
  ADD COLUMN IF NOT EXISTS close_reason text,
  ADD COLUMN IF NOT EXISTS closed_not_accepted_count integer;

-- Verdict state on both observation grains. 'pending' = enqueued for
-- verification and not yet answered; 'verified' = verifier approved;
-- 'rework' = verifier bounced it and the operator owns the next action.
ALTER TABLE public.weighing_observations
  ADD COLUMN IF NOT EXISTS verification_status text NOT NULL DEFAULT 'pending',
  ADD COLUMN IF NOT EXISTS verified_by uuid,
  ADD COLUMN IF NOT EXISTS verified_at timestamptz,
  ADD COLUMN IF NOT EXISTS rework_reason text;

ALTER TABLE public.weighing_observations
  DROP CONSTRAINT IF EXISTS weighing_observations_verification_status_check;
ALTER TABLE public.weighing_observations
  ADD CONSTRAINT weighing_observations_verification_status_check
  CHECK (verification_status = ANY (ARRAY['pending','verified','rework']));

ALTER TABLE public.weighing_shed_observations
  ADD COLUMN IF NOT EXISTS verification_status text NOT NULL DEFAULT 'pending',
  ADD COLUMN IF NOT EXISTS verified_by uuid,
  ADD COLUMN IF NOT EXISTS verified_at timestamptz,
  ADD COLUMN IF NOT EXISTS rework_reason text;

ALTER TABLE public.weighing_shed_observations
  DROP CONSTRAINT IF EXISTS weighing_shed_observations_verification_status_check;
ALTER TABLE public.weighing_shed_observations
  ADD CONSTRAINT weighing_shed_observations_verification_status_check
  CHECK (verification_status = ANY (ARRAY['pending','verified','rework']));

-- Close reads "what work in this bucket was never accepted" by
-- (tenant, campaign, campaign_shed, status). The pre-existing roster index is
-- keyed on expected_location_id, which does not serve a bucket-scoped read.
CREATE INDEX IF NOT EXISTS weighing_expected_animals_scope_status_idx
  ON public.weighing_expected_animals (tenant_id, campaign_id, campaign_shed_id, status);

-- Verdict consumers look an observation up by (tenant, observation id); the PK
-- covers that. These partial indexes serve the "what is still awaiting a
-- verdict / what came back as rework" bucket reads per campaign shed without a
-- sequential scan as observation volume grows.
CREATE INDEX IF NOT EXISTS weighing_observations_verification_status_idx
  ON public.weighing_observations (tenant_id, campaign_shed_id, verification_status, accepted_at DESC)
  WHERE verification_status <> 'verified';

CREATE INDEX IF NOT EXISTS weighing_shed_observations_verification_status_idx
  ON public.weighing_shed_observations (tenant_id, campaign_shed_id, verification_status)
  WHERE verification_status <> 'verified';

-- +goose Down
DROP INDEX IF EXISTS public.weighing_expected_animals_scope_status_idx;
DROP INDEX IF EXISTS public.weighing_shed_observations_verification_status_idx;
DROP INDEX IF EXISTS public.weighing_observations_verification_status_idx;

ALTER TABLE public.weighing_shed_observations
  DROP CONSTRAINT IF EXISTS weighing_shed_observations_verification_status_check;
ALTER TABLE public.weighing_shed_observations
  DROP COLUMN IF EXISTS rework_reason,
  DROP COLUMN IF EXISTS verified_at,
  DROP COLUMN IF EXISTS verified_by,
  DROP COLUMN IF EXISTS verification_status;

ALTER TABLE public.weighing_observations
  DROP CONSTRAINT IF EXISTS weighing_observations_verification_status_check;
ALTER TABLE public.weighing_observations
  DROP COLUMN IF EXISTS rework_reason,
  DROP COLUMN IF EXISTS verified_at,
  DROP COLUMN IF EXISTS verified_by,
  DROP COLUMN IF EXISTS verification_status;

UPDATE public.weighing_campaigns SET status='completed' WHERE status='closed';
ALTER TABLE public.weighing_campaigns
  DROP COLUMN IF EXISTS closed_not_accepted_count,
  DROP COLUMN IF EXISTS close_reason,
  DROP COLUMN IF EXISTS closed_by,
  DROP COLUMN IF EXISTS closed_at;
ALTER TABLE public.weighing_campaigns
  DROP CONSTRAINT IF EXISTS weighing_campaigns_status_check;
ALTER TABLE public.weighing_campaigns
  ADD CONSTRAINT weighing_campaigns_status_check
  CHECK (status = ANY (ARRAY['draft','published','in_progress','delayed','completed','canceled']));

UPDATE public.weighing_campaign_sheds SET status='completed' WHERE status='closed';
ALTER TABLE public.weighing_campaign_sheds
  DROP COLUMN IF EXISTS closed_not_accepted_count,
  DROP COLUMN IF EXISTS close_reason,
  DROP COLUMN IF EXISTS closed_by,
  DROP COLUMN IF EXISTS closed_at;
ALTER TABLE public.weighing_campaign_sheds
  DROP CONSTRAINT IF EXISTS weighing_campaign_sheds_status_check;
ALTER TABLE public.weighing_campaign_sheds
  ADD CONSTRAINT weighing_campaign_sheds_status_check
  CHECK (status = ANY (ARRAY['pending','in_progress','completed','canceled']));
