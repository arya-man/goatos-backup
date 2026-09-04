-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing fasting shed-proof rows are written by the operator submit route and the verifier verdict consumer; they do not change the Vaccination HRMS seed contract
--
-- FASTING EVIDENCE IS PER SHED (maintainer correction 2026-09-03, same day,
-- SUPERSEDING the one-clip-per-card evidence half of the original decision).
--
-- The first cut stored ONE feed video and ONE water video on the fasting task
-- itself. The maintainer rejected that on the first phone rehearsal: a
-- campaign covers SEVERAL sheds, and one clip stretched over four sheds
-- proves nothing — the verifier cannot tell which shed was actually emptied,
-- and the operator is never told which sheds the work covers. The card and
-- the deadline stay CAMPAIGN-grain (one operator, one evening, one submit,
-- one midnight gate), but the EVIDENCE is now one feed video + one water
-- video PER SHED, and verification follows the evidence: one item per shed.
-- This is the same grain rule weighing capture already lives by (ledger B-5:
-- the review grain follows the evidence) and the same shape feed
-- distribution uses per pen.
--
-- weighing_fasting_tasks keeps its feed_proof_ref/water_proof_ref columns for
-- rows submitted before this migration (historical reads only); new submits
-- leave them NULL and write here instead.
CREATE TABLE IF NOT EXISTS public.weighing_fasting_shed_proofs (
  fasting_shed_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  fasting_task_id uuid NOT NULL REFERENCES public.weighing_fasting_tasks(fasting_task_id) ON DELETE CASCADE,
  campaign_shed_id uuid NOT NULL REFERENCES public.weighing_campaign_sheds(campaign_shed_id) ON DELETE CASCADE,
  -- Denormalized so the verifier item and the operator slot can name the shed
  -- without a join inside the enqueue path; refreshed on every submit.
  shed_label text NOT NULL,
  feed_proof_ref uuid,
  water_proof_ref uuid,
  -- Per-shed review state, the feed 000176 gate shape. The PARENT task's
  -- status is a roll-up (completed only when every shed is completed; rework
  -- if any shed is rework); the parent's submitted_at stays the ONLY fact the
  -- midnight gate reads.
  status text NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'pending_verification', 'completed', 'rework')),
  rework_reason text,
  row_version integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  -- One evidence row per shed per card: what makes the verdict join 1:1 and a
  -- replayed submit an upsert instead of a duplicate.
  CONSTRAINT weighing_fasting_shed_proofs_shed_uq UNIQUE (tenant_id, fasting_task_id, campaign_shed_id),
  CONSTRAINT weighing_fasting_shed_proofs_pair CHECK (
    (feed_proof_ref IS NULL AND water_proof_ref IS NULL)
    OR (feed_proof_ref IS NOT NULL AND water_proof_ref IS NOT NULL)
  )
);

CREATE INDEX IF NOT EXISTS weighing_fasting_shed_proofs_task_idx
  ON public.weighing_fasting_shed_proofs (tenant_id, fasting_task_id);

-- The parent card no longer stores the pair itself (evidence moved to the
-- per-shed table above), so the both-proofs CHECK on the parent must go; a
-- submission still always names who submitted.
ALTER TABLE public.weighing_fasting_tasks
  DROP CONSTRAINT IF EXISTS weighing_fasting_tasks_submit_has_both_proofs;
ALTER TABLE public.weighing_fasting_tasks
  ADD CONSTRAINT weighing_fasting_tasks_submit_names_submitter CHECK (
    submitted_at IS NULL OR submitted_by IS NOT NULL
  );
