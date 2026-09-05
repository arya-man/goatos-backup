-- +goose Up
-- seed-fixture-guard:ignore: operational PC Care round/removal rows are written by the planner create transaction, the operator submit route and the verifier verdict consumer; they do not change the Vaccination HRMS seed contract
--
-- PC CARE PLANS A ROUND, NOT A PEN (maintainer decision 2026-09-05, SUPERSEDING the
-- one-pen-per-create half of 000183).
--
-- The planner already ticked several pens in the wizard; the phone then fired ONE
-- CREATE PER PEN, so a CEO who planned deworming for four pens got four unrelated
-- cards and — with the feed & water removal toggle on — four more. Nothing recorded
-- that they were one round of work, so nothing could show it as one.
--
-- This is the WEIGHING SHAPE, and deliberately so: weighing_campaigns +
-- weighing_campaign_sheds is the verified template for "one plan, many pens, each
-- pen carrying its own execution and its own review". Read across:
--
--   weighing_campaigns          -> pc_care_rounds       (the card the planner creates)
--   weighing_campaign_sheds     -> pc_care_tasks        (the pen bucket; ALREADY exists)
--   weighing_fasting_tasks      -> the round-grain feed_water_removal task
--   weighing_fasting_shed_proofs-> pc_care_removal_pen_proofs
--
-- The pen bucket is NOT a new table. pc_care_tasks is already exactly that row —
-- one pen, its own scanned animals, its own submit, its own verification item
-- (000183: "THE TASK ROW IS ALSO THE COMPLETION ROW"). It gains a round_id and
-- nothing else changes about how a pen is worked, proved or judged. Rebuilding the
-- bucket would have thrown away the whole execution path to win a rename.
--
-- round_id is NULLABLE and stays that way. Every task planned before this migration
-- is a legitimate one-pen task with no round, and backfilling a synthetic round per
-- historical task would invent rounds the CEO never planned. Reads treat a NULL
-- round as a round of one.
--
-- GRAIN PROOF (projection-review:)
--   producer pc_care_rounds       unique on (round_id); (tenant_id, idempotency_key)
--   consumer pc_care_tasks.round_id                     many tasks : one round
--   join     tasks -> rounds on (tenant_id, round_id)   N:1, never fans a round out
--   A round's pen count is COUNT(*) over its tasks; a task belongs to at most one
--   round, so the count cannot double. Two rounds can never claim the same pen-day:
--   pc_care_tasks_natural_uq (tenant, category, park, shed, partition_key, planned
--   date) WHERE work_state <> 'canceled' already forbids it, round or no round.

CREATE TABLE IF NOT EXISTS public.pc_care_rounds (
  round_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  category text NOT NULL,
  park_id uuid NOT NULL REFERENCES public.locations(location_id),
  -- The date the CEO chose. IMMUTABLE audit anchor, like pc_care_tasks.planned_business_date:
  -- a pen that rolls forward moves its OWN due date and never rewrites the round's.
  planned_business_date date NOT NULL,
  idempotency_key text NOT NULL,
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  -- Only the four PLANNER categories can be rounded. inventory_vaccine is per vaccine
  -- (000218) and has no pen to multiply; feed_water_removal is born inside a deworming
  -- create and gates a round rather than being one.
  CONSTRAINT pc_care_rounds_category_check CHECK (
    category IN ('deworming', 'ticks_removal', 'hoof_trimming', 'hair_trimming')
  ),
  -- Request-level idempotency: one create key mints one round, so a retried tap after a
  -- network blip replays the same round instead of planning the pens twice.
  CONSTRAINT pc_care_rounds_idempotency_uq UNIQUE (tenant_id, idempotency_key)
);

-- Serving read: the planner/monitor list pages one park's rounds by date, and the
-- operator worklist resolves rounds from the pens assigned to them.
CREATE INDEX IF NOT EXISTS pc_care_rounds_serving_idx
  ON public.pc_care_rounds (tenant_id, park_id, planned_business_date, round_id);

ALTER TABLE public.pc_care_tasks
  ADD COLUMN IF NOT EXISTS round_id uuid;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_round_fk
    FOREIGN KEY (round_id) REFERENCES public.pc_care_rounds(round_id) ON DELETE RESTRICT;

-- Drill read: one round's pens, in one indexed read bounded by the round's pen count.
CREATE INDEX IF NOT EXISTS pc_care_tasks_round_idx
  ON public.pc_care_tasks (tenant_id, round_id, task_id)
  WHERE round_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- FEED & WATER REMOVAL IS ONE CARD PER ROUND, EVIDENCE PER PEN
-- (maintainer decision 2026-09-05, SUPERSEDING the one-removal-per-deworming half
-- of 000254 for round-planned work.)
--
-- This is weighing's correction of 2026-09-03 applied here before it could be got
-- wrong a second time: the CARD is round-grain (one evening, one crew, one submit,
-- one midnight gate) while the EVIDENCE is one feed video + one water video PER PEN,
-- because one clip stretched over four pens proves nothing and the verifier cannot
-- tell which pen was actually emptied. Review follows the evidence: one verification
-- item per pen.
--
-- gates_round_id is carried by the REMOVAL row and points at the round it gates,
-- beside the existing gates_task_id which points at a single deworming task. Both
-- exist and exactly one is set: gates_task_id is the pre-round pair (000254) and
-- stays valid for every removal already planned; gates_round_id is what a round
-- create writes. The midnight gate reads whichever the row carries.
ALTER TABLE public.pc_care_tasks
  ADD COLUMN IF NOT EXISTS gates_round_id uuid;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_gates_round_fk
    FOREIGN KEY (gates_round_id) REFERENCES public.pc_care_rounds(round_id) ON DELETE RESTRICT;

-- A removal row gates ONE thing: a single deworming task (legacy) or a whole round,
-- never both and never neither-when-it-is-a-removal. Anything that is not a removal
-- gates nothing.
ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_gates_shape_check CHECK (
    CASE
      WHEN category = 'feed_water_removal'
        THEN (gates_task_id IS NOT NULL) <> (gates_round_id IS NOT NULL)
      ELSE gates_task_id IS NULL AND gates_round_id IS NULL
    END
  );

-- ONE removal card per gated round: makes the midnight-gate join provably 1:0..1,
-- exactly as pc_care_tasks_gates_task_uq does for the legacy pair.
CREATE UNIQUE INDEX IF NOT EXISTS pc_care_tasks_gates_round_uq
  ON public.pc_care_tasks (tenant_id, gates_round_id)
  WHERE gates_round_id IS NOT NULL;

-- A round-grain removal covers several pens, so it has no single shed. The shape
-- check from 000218 demanded a shed for every non-vaccine category; it now admits
-- the third legitimate shed-less form. The pens it covers are named by
-- pc_care_removal_pen_proofs, never by a shed column that could only hold one.
ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_vaccine_shape_check;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_vaccine_shape_check CHECK (
    (vaccine_label IS NULL AND shed_id IS NOT NULL AND gates_round_id IS NULL)
    OR (
      vaccine_label IS NOT NULL
      AND btrim(vaccine_label) <> ''
      AND category = 'inventory_vaccine'
      AND shed_id IS NULL
    )
    OR (
      vaccine_label IS NULL
      AND category = 'feed_water_removal'
      AND gates_round_id IS NOT NULL
      AND shed_id IS NULL
    )
  );

-- One pen's removal evidence on a round's removal card. Mirrors
-- weighing_fasting_shed_proofs field for field, including the pair CHECK and the
-- per-pen review state.
CREATE TABLE IF NOT EXISTS public.pc_care_removal_pen_proofs (
  removal_pen_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  removal_task_id uuid NOT NULL,
  -- The pen's own work task inside the gated round. Naming the TASK rather than a
  -- (shed, partition) pair is what keeps the removal and the work it gates provably
  -- the same pen set: a pen with no task cannot get a removal slot, and a removal
  -- slot cannot point at a pen outside the round.
  gated_task_id uuid NOT NULL,
  -- Denormalized operational location display ("Godel 1 - Part 3"), composed by
  -- oploc at write time so the operator slot header and the verifier item can name
  -- the pen without a join inside the enqueue path.
  pen_label text NOT NULL,
  feed_proof_ref text,
  water_proof_ref text,
  -- Per-pen review state (feed 000176 gate shape). The PARENT removal task's status
  -- is a roll-up; the parent's submitted_at stays the ONLY fact the midnight gate reads.
  status text NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'pending_verification', 'completed', 'rework')),
  rework_reason text,
  row_version integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pc_care_removal_pen_proofs_removal_fk
    FOREIGN KEY (tenant_id, removal_task_id)
    REFERENCES public.pc_care_tasks (tenant_id, task_id) ON DELETE CASCADE,
  CONSTRAINT pc_care_removal_pen_proofs_gated_fk
    FOREIGN KEY (tenant_id, gated_task_id)
    REFERENCES public.pc_care_tasks (tenant_id, task_id) ON DELETE CASCADE,
  -- One evidence row per pen per card: what makes the verdict join 1:1 and a
  -- replayed submit an upsert instead of a duplicate.
  CONSTRAINT pc_care_removal_pen_proofs_pen_uq UNIQUE (tenant_id, removal_task_id, gated_task_id),
  -- PARTIAL CAPTURE IS ALLOWED HERE ON PURPOSE. Read this before "fixing" it.
  --
  -- The operator shoots the feed video, then the water video, so a pen sits with ONE of them
  -- for as long as it takes to walk the pen. This constraint permits that, and it is NOT an
  -- oversight: a Postgres CHECK is satisfied unless it evaluates to FALSE, and with feed set
  -- and water NULL the second branch is (TRUE AND NULL) = NULL, so FALSE OR NULL = NULL =
  -- satisfied. A single-column UPDATE therefore succeeds, which is exactly what
  -- RegisterRemovalPenProof does one slot at a time.
  --
  -- It is NOT vacuous either: an EMPTY STRING in either column still fails, which is the abuse
  -- this exists to stop (a client "recording" a slot with a blank ref).
  --
  -- BOTH videos are enforced at SUBMIT, not here, because that is where the rule belongs: a
  -- pen with one video is mid-capture, a pen SUBMITTED with one video is a lie about the
  -- evening's work. removalPenSubmitRefs refuses it with ErrRemovalProofIncomplete (422
  -- removal_proof_incomplete, "every pen needs both its feed and its water video").
  --
  -- Reported as a P1 in review on 2026-09-05 ("the first slot cannot save, the flow is
  -- unusable") and closed as working-as-designed after reproducing all three behaviours on a
  -- live database. Pinned by TestRemovalPenPartialCaptureIsAllowedAndSubmitStillDemandsBoth;
  -- see context/repo-audits/pc-care-rounds-do-not-reopen-ledger.md.
  CONSTRAINT pc_care_removal_pen_proofs_pair CHECK (
    (feed_proof_ref IS NULL AND water_proof_ref IS NULL)
    OR (btrim(feed_proof_ref) <> '' AND btrim(water_proof_ref) <> '')
  ),
  CONSTRAINT pc_care_removal_pen_proofs_label_check CHECK (btrim(pen_label) <> '')
);

CREATE INDEX IF NOT EXISTS pc_care_removal_pen_proofs_task_idx
  ON public.pc_care_removal_pen_proofs (tenant_id, removal_task_id);

-- +goose Down
DROP TABLE IF EXISTS public.pc_care_removal_pen_proofs;

DROP INDEX IF EXISTS public.pc_care_tasks_gates_round_uq;
DROP INDEX IF EXISTS public.pc_care_tasks_round_idx;

DELETE FROM public.pc_care_tasks WHERE gates_round_id IS NOT NULL;

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_vaccine_shape_check;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_vaccine_shape_check CHECK (
    (vaccine_label IS NULL AND shed_id IS NOT NULL)
    OR (
      vaccine_label IS NOT NULL
      AND btrim(vaccine_label) <> ''
      AND category = 'inventory_vaccine'
      AND shed_id IS NULL
    )
  );

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_gates_shape_check;

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_gates_round_fk;

ALTER TABLE public.pc_care_tasks
  DROP COLUMN IF EXISTS gates_round_id;

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_round_fk;

ALTER TABLE public.pc_care_tasks
  DROP COLUMN IF EXISTS round_id;

DROP TABLE IF EXISTS public.pc_care_rounds;
