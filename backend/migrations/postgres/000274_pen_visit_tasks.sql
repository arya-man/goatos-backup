-- +goose Up
-- seed-fixture-guard:ignore: operational pen-visit rows are written by the kernel materializer and the park head's submit; the park assignee seed is a per-person config row, not the Vaccination HRMS seed contract
--
-- PEN VISIT TASKS (maintainer decision 2026-09-07).
--
-- The day after any preventive-care work is SUBMITTED in a pen -- a vaccination shed proof or a PC
-- Care task (deworming, anti protozoan, ticks removal, hoof trimming, hair trimming) -- the park's
-- head goes to that pen, looks at the animals, records ONE live-camera video and submits it. That
-- is the whole task: no roster, no per-animal scan, no verifier. It lives on the phone's Tasks
-- module as the "For me" tab beside the director's own "Raised by me" list.
--
-- THE TASK IS SYSTEM-RAISED, NEVER TYPED. The kernel materializer reads the pens whose work landed
-- on business date D (every vaccination shed submit and every PC Care submit already writes a
-- verification_items row carrying park_id, shed_id and partition_label -- one source for both
-- triggers) and writes one task per (park, shed, pen, D) due on D+1, assigned to the person the
-- park's config row names. The natural key below is what makes the tick idempotent: the first tick
-- after the day boundary inserts, every later tick inserts nothing, and a second submit in the same
-- pen on the same day only widens `reasons`.
--
-- WHO VISITS IS A PER-PARK CONFIG ROW, NEVER A FALLBACK. pen_visit_park_assignees names the one
-- person per park (CBE -> Dinakar, CPT -> Chandrakant, seeded below by email). A park with no row
-- gets NO task and the materializer logs the gap loudly -- the vaccination operator rule: never
-- invent an assignee.
--
-- THE KERNEL SHAPE IS PC CARE'S (000183): planned_business_date is the immutable anchor and
-- due_business_date rolls FORWARD ONLY as 'delayed', so an unvisited pen keeps showing with the
-- date it was owed, never as fresh work.
CREATE TABLE public.pen_visit_park_assignees (
    tenant_id uuid NOT NULL REFERENCES public.tenants (tenant_id),
    park_id uuid NOT NULL REFERENCES public.locations (location_id),
    user_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, park_id)
);

CREATE TABLE public.pen_visit_tasks (
    task_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES public.tenants (tenant_id),
    park_id uuid NOT NULL,
    shed_id uuid NOT NULL,
    -- partition_label is the pen ("2", "Part 3"), NULL/'' for an undivided shed; partition_key
    -- normalizes it for the natural key, the 000137/000176/000183 shape. Display always goes
    -- through oploc, never this column.
    partition_label text,
    partition_key text GENERATED ALWAYS AS (
        CASE WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
             ELSE lower(btrim(partition_label)) END
    ) STORED,
    -- reasons is the closed vocabulary of WHY the pen is visited: 'vaccination' plus the five PC
    -- Care pen categories. One pen worked twice on one day carries both, never two tasks.
    reasons text[] NOT NULL CHECK (cardinality(reasons) >= 1),
    -- source_business_date is the IST day the work was submitted; the visit is owed the next day.
    source_business_date date NOT NULL,
    planned_business_date date NOT NULL,
    due_business_date date NOT NULL,
    work_state text NOT NULL DEFAULT 'scheduled'
        CHECK (work_state IN ('scheduled', 'delayed', 'completed', 'canceled')),
    assignee_user_id uuid NOT NULL,
    -- proof_ref is the ONE live-camera video; a completed task always carries it.
    proof_ref uuid REFERENCES public.proof_artifacts (proof_id),
    submitted_by uuid,
    submitted_at timestamptz,
    rolled_forward_count integer NOT NULL DEFAULT 0,
    delayed_since_business_date date,
    row_version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pen_visit_tasks_natural_uq
        UNIQUE (tenant_id, park_id, shed_id, partition_key, source_business_date),
    CONSTRAINT pen_visit_tasks_due_after_plan CHECK (due_business_date >= planned_business_date),
    CONSTRAINT pen_visit_tasks_completed_has_proof CHECK (
        (work_state = 'completed') = (proof_ref IS NOT NULL AND submitted_at IS NOT NULL AND submitted_by IS NOT NULL)
    )
);

-- The park head's "For me" list: their own tasks, open first, newest due first, keyset on
-- (due_business_date, task_id).
CREATE INDEX pen_visit_tasks_assignee_idx
    ON public.pen_visit_tasks (tenant_id, assignee_user_id, work_state, due_business_date DESC, task_id DESC);
-- The kernel sweep: unfinished work whose due date has passed.
CREATE INDEX pen_visit_tasks_sweep_due_idx
    ON public.pen_visit_tasks (tenant_id, work_state, due_business_date, task_id);

-- Seed the two park heads (maintainer instruction 2026-09-07: "Dinakar is for CBE and
-- Chandrakant is for CPT"). Matched on the roster row's login email first, the identifier
-- approvers.go keys on, and on the roster display name where a row carries no email (the
-- rosterDisplayName fallback the same file uses for Dinakar and Hemant) -- resolved to the
-- user_id the person signs in as. A park with no matching active, bound roster row seeds
-- nothing rather than guessing; the materializer then logs that park's gap.
INSERT INTO public.pen_visit_park_assignees (tenant_id, park_id, user_id)
SELECT DISTINCT ON (p.tenant_id, p.location_id) p.tenant_id, p.location_id, m.user_id
FROM public.locations p
JOIN public.workforce_members m
  ON m.tenant_id = p.tenant_id
 AND m.status = 'active'
 AND m.user_id IS NOT NULL
 AND (
      lower(btrim(COALESCE(m.email, ''))) = CASE p.location_code
        WHEN 'CBE' THEN 'babureddy315@gmail.com'
        WHEN 'CPT' THEN 'chandrakanth119527@gmail.com'
      END
   OR lower(btrim(m.display_name)) = CASE p.location_code
        WHEN 'CBE' THEN 'dinakar'
        WHEN 'CPT' THEN 'chandrakant'
      END
 )
WHERE p.location_type = 'park'
  AND p.status = 'active'
  AND p.location_code IN ('CBE', 'CPT')
ORDER BY p.tenant_id, p.location_id,
         (lower(btrim(COALESCE(m.email, ''))) <> '') DESC, m.created_at
ON CONFLICT (tenant_id, park_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS public.pen_visit_tasks;
DROP TABLE IF EXISTS public.pen_visit_park_assignees;
