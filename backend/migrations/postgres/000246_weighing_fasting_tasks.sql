-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing fasting rows are written by the campaign create transaction, the operator submit route and the kernel worker; they do not change the Vaccination HRMS seed contract
--
-- WEIGHING FASTING PRECONDITION (maintainer decision 2026-09-03).
--
-- Animals must have feed and water removed the evening BEFORE they are weighed,
-- or the weights are wrong. From this migration on, every weighing task carries
-- exactly ONE fasting task: a second operator (assigned at campaign create, same
-- park) removes feed and water the night before the weigh date and proves it
-- with TWO live-camera videos — one for feed, one for water — submitted before
-- MIDNIGHT IST. Operator SUBMISSION is what unblocks the next day's weighing;
-- the videos still go to the verifier post-hoc, and a later rejection creates
-- rework on the fasting proof without un-running the weighing.
--
-- Grain: ONE ROW PER CAMPAIGN (a campaign is one park on one weigh date, so the
-- fasting evening is one physical round of the selected sheds). Never per
-- bucket: the maintainer's spec is one card, two videos, one removal operator.
-- `UNIQUE (tenant_id, campaign_id)` makes create/replay idempotent at this
-- grain.
--
-- Time grain is the Asia/Kolkata BUSINESS DAY:
--   planned_weigh_date — the ORIGINAL weigh date chosen at create. IMMUTABLE
--                        audit anchor, mirroring weighing_work_items.
--   weigh_business_date — the CURRENT weigh date. The fasting card is visible to
--                        its operator from 20:00 IST on (weigh_business_date-1)
--                        and its submit deadline is 00:00 IST of
--                        weigh_business_date. When the kernel finds the deadline
--                        passed with no submission it rolls this date AND the
--                        campaign's work items forward one day together, so the
--                        card re-arms for the next evening's window.
--
-- status is the feed 000176 gate shape (open | pending_verification | completed
-- | rework), orthogonal to the campaign's own lifecycle. The MIDNIGHT GATE reads
-- submitted_at, NOT status: once the operator has submitted (submitted_at set),
-- a verifier rework never re-blocks the weighing — verification is post-hoc
-- review of evidence for work that already happened.
--
-- Weighing ISOLATION is preserved: nothing here references goats, herd rosters,
-- vaccination, or any other module's schema. The table knows a campaign, a
-- park, an operator and two proof refs. The park FK is to `locations`, one of
-- the four allowlisted ORG tables.
CREATE TABLE IF NOT EXISTS public.weighing_fasting_tasks (
  fasting_task_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  campaign_id uuid NOT NULL REFERENCES public.weighing_campaigns(campaign_id) ON DELETE CASCADE,
  park_id uuid NOT NULL REFERENCES public.locations(location_id),
  operator_user_id uuid NOT NULL,
  planned_weigh_date date NOT NULL,
  weigh_business_date date NOT NULL,
  status text NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'pending_verification', 'completed', 'rework')),
  feed_proof_ref uuid,
  water_proof_ref uuid,
  submitted_by uuid,
  submitted_at timestamptz,
  verified_by uuid,
  verified_at timestamptz,
  -- rework_reason is the verifier's rejection reason; clients render it
  -- verbatim (backend-owned copy).
  rework_reason text,
  rolled_forward_count integer NOT NULL DEFAULT 0 CHECK (rolled_forward_count >= 0),
  row_version integer NOT NULL DEFAULT 1,
  idempotency_key text NOT NULL,
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT weighing_fasting_tasks_campaign_uq UNIQUE (tenant_id, campaign_id),
  CONSTRAINT weighing_fasting_tasks_date_forward_only
    CHECK (weigh_business_date >= planned_weigh_date),
  -- A submission is exactly two proofs. A row cannot claim submitted while
  -- either video is missing, and a never-submitted row carries no verdict.
  CONSTRAINT weighing_fasting_tasks_submit_has_both_proofs CHECK (
    submitted_at IS NULL
    OR (feed_proof_ref IS NOT NULL AND water_proof_ref IS NOT NULL AND submitted_by IS NOT NULL)
  ),
  CONSTRAINT weighing_fasting_tasks_verdict_needs_submission CHECK (
    verified_at IS NULL OR submitted_at IS NOT NULL
  )
);

-- Exact-replay lookup for the operator submit route.
CREATE UNIQUE INDEX IF NOT EXISTS weighing_fasting_tasks_idempotency_uq
  ON public.weighing_fasting_tasks (tenant_id, idempotency_key);

-- The removal operator's card list: "my fasting tasks around today", newest
-- window first. Partial on the live statuses the list actually serves.
CREATE INDEX IF NOT EXISTS weighing_fasting_tasks_operator_idx
  ON public.weighing_fasting_tasks (tenant_id, operator_user_id, weigh_business_date DESC);

-- The kernel midnight-gate sweep: unsubmitted fasting rows whose deadline
-- (00:00 IST of weigh_business_date) has passed.
CREATE INDEX IF NOT EXISTS weighing_fasting_tasks_sweep_idx
  ON public.weighing_fasting_tasks (tenant_id, weigh_business_date)
  WHERE submitted_at IS NULL;
