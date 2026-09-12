-- +goose Up
-- seed-fixture-guard:ignore: operational pen-visit rows are written by the kernel materializer, the park head's submit and the verifier's verdict; the park visitor config is a per-person HRMS row, not the Vaccination HRMS seed contract
--
-- THE PEN VISIT IS THE LAST STEP OF THE CARE WORK, NOT A TASK OF ITS OWN (maintainer decision
-- 2026-09-12, SUPERSEDING the 2026-09-07 "For me" standalone task in three ways).
--
-- 1. THE VISIT VIDEO GOES TO THE VERIFIER. 000277 made submit = completion ("no verifier").
--    The maintainer's rule is that the care task closes only "when all these videos are
--    verified by the verifier" -- removal, the work itself, AND the next-day visit. So the
--    row gains the PC Care gate: status open -> pending_verification -> completed | rework,
--    and work_state reaches 'completed' ONLY on the verifier's approval.
-- 2. THE VISIT BELONGS TO ITS PARENT. pen_visit_task_sources links a visit to the PC Care
--    task(s) and vaccination shed submission(s) that raised it (one pen worked twice on one
--    day is still ONE visit, with several sources). The parent's closure reads this link:
--    a PC Care task whose own videos are approved stays open until the visit is approved.
-- 3. WHO VISITS IS PER-PARK HRMS CONFIG, ONE OR MORE PEOPLE. 000277 allowed one person per
--    park and stamped that person on every row. Now pen_visit_park_assignees is keyed per
--    (park, person), the People editor edits it, and ANY configured person may record the
--    visit -- "if multiple people are there, if anyone does then enough". assignee_user_id
--    is therefore dropped from the row; submitted_by records who actually went.

-- 1. Several visitors per park.
ALTER TABLE public.pen_visit_park_assignees
  DROP CONSTRAINT IF EXISTS pen_visit_park_assignees_pkey;
ALTER TABLE public.pen_visit_park_assignees
  ADD CONSTRAINT pen_visit_park_assignees_pkey PRIMARY KEY (tenant_id, park_id, user_id);
CREATE INDEX IF NOT EXISTS pen_visit_park_assignees_user_idx
  ON public.pen_visit_park_assignees (tenant_id, user_id);

-- 2. The verifier gate on the visit row.
ALTER TABLE public.pen_visit_tasks
  DROP CONSTRAINT IF EXISTS pen_visit_tasks_completed_has_proof;
ALTER TABLE public.pen_visit_tasks
  ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'open',
  ADD COLUMN IF NOT EXISTS verified_by uuid,
  ADD COLUMN IF NOT EXISTS verified_at timestamptz,
  ADD COLUMN IF NOT EXISTS rework_reason text;
-- Rows completed under the old rule were never reviewed; they stay completed (history is not
-- rewritten) and read as completed on the gate too, so a parent that closed on them stays closed.
UPDATE public.pen_visit_tasks SET status = 'completed' WHERE work_state = 'completed' AND status = 'open';
ALTER TABLE public.pen_visit_tasks
  ADD CONSTRAINT pen_visit_tasks_status_check
    CHECK (status IN ('open', 'pending_verification', 'completed', 'rework')),
  -- The two dimensions agree: completed on the kernel means completed on the gate, with the
  -- video and the submit that carried it. A rework keeps its last clip as history.
  ADD CONSTRAINT pen_visit_tasks_completed_shape_check CHECK (
    (work_state = 'completed') = (status = 'completed')
  ),
  ADD CONSTRAINT pen_visit_tasks_submitted_has_proof CHECK (
    status = 'open' OR (proof_ref IS NOT NULL AND submitted_at IS NOT NULL AND submitted_by IS NOT NULL)
  );
ALTER TABLE public.pen_visit_tasks DROP COLUMN IF EXISTS assignee_user_id;
DROP INDEX IF EXISTS public.pen_visit_tasks_assignee_idx;
-- The visitor's list: the parks they are configured for, open first, keyset on (due, id).
CREATE INDEX IF NOT EXISTS pen_visit_tasks_park_idx
  ON public.pen_visit_tasks (tenant_id, park_id, work_state, due_business_date DESC, task_id DESC);
-- The verdict consumer's lookup and the parent read.
CREATE INDEX IF NOT EXISTS pen_visit_tasks_pen_source_idx
  ON public.pen_visit_tasks (tenant_id, shed_id, partition_key, source_business_date DESC);

-- 3. The link to the parents.
CREATE TABLE IF NOT EXISTS public.pen_visit_task_sources (
    tenant_id uuid NOT NULL REFERENCES public.tenants (tenant_id),
    task_id uuid NOT NULL REFERENCES public.pen_visit_tasks (task_id) ON DELETE CASCADE,
    -- source_kind names the parent's table: 'pc_care_task' (pc_care_tasks.task_id) or
    -- 'vaccination_submission' (sop_submissions.submission_id). The materializer takes both
    -- from the verification item the parent's submit raised.
    source_kind text NOT NULL CHECK (source_kind IN ('pc_care_task', 'vaccination_submission')),
    source_ref_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, task_id, source_kind, source_ref_id)
);
-- The parent's read: which visit does THIS task / submission owe.
CREATE UNIQUE INDEX IF NOT EXISTS pen_visit_task_sources_parent_uq
  ON public.pen_visit_task_sources (tenant_id, source_kind, source_ref_id);

-- +goose Down
DROP TABLE IF EXISTS public.pen_visit_task_sources;
DROP INDEX IF EXISTS public.pen_visit_tasks_pen_source_idx;
DROP INDEX IF EXISTS public.pen_visit_tasks_park_idx;
ALTER TABLE public.pen_visit_tasks
  ADD COLUMN IF NOT EXISTS assignee_user_id uuid;
ALTER TABLE public.pen_visit_tasks
  DROP CONSTRAINT IF EXISTS pen_visit_tasks_submitted_has_proof,
  DROP CONSTRAINT IF EXISTS pen_visit_tasks_completed_shape_check,
  DROP CONSTRAINT IF EXISTS pen_visit_tasks_status_check;
ALTER TABLE public.pen_visit_tasks
  DROP COLUMN IF EXISTS rework_reason,
  DROP COLUMN IF EXISTS verified_at,
  DROP COLUMN IF EXISTS verified_by,
  DROP COLUMN IF EXISTS status;
DROP INDEX IF EXISTS public.pen_visit_park_assignees_user_idx;
ALTER TABLE public.pen_visit_park_assignees
  DROP CONSTRAINT IF EXISTS pen_visit_park_assignees_pkey;
ALTER TABLE public.pen_visit_park_assignees
  ADD CONSTRAINT pen_visit_park_assignees_pkey PRIMARY KEY (tenant_id, park_id);
