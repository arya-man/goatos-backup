-- +goose Up
-- seed-fixture-guard:ignore: operational pen-visit rows are written by the kernel materializer and the park head's submit; the park visitor config is a per-person HRMS row, not the Vaccination HRMS seed contract
--
-- PEN VISIT TASKS (maintainer decision 2026-09-07, final HRMS shape).
--
-- The day after preventive-care work is submitted in a pen, the configured park visitor records
-- one live-camera visit video. Visitors are configured per park in HRMS through
-- pen_visit_park_assignees. A park with no row gets no task; there is no fallback assignee.
CREATE TABLE IF NOT EXISTS public.pen_visit_park_assignees (
    tenant_id uuid NOT NULL REFERENCES public.tenants (tenant_id),
    park_id uuid NOT NULL REFERENCES public.locations (location_id),
    user_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, park_id, user_id)
);

CREATE INDEX IF NOT EXISTS pen_visit_park_assignees_user_idx
  ON public.pen_visit_park_assignees (tenant_id, user_id);

CREATE TABLE IF NOT EXISTS public.pen_visit_tasks (
    task_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES public.tenants (tenant_id),
    park_id uuid NOT NULL,
    shed_id uuid NOT NULL,
    partition_label text,
    partition_key text GENERATED ALWAYS AS (
        CASE WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
             ELSE lower(btrim(partition_label)) END
    ) STORED,
    reasons text[] NOT NULL CHECK (cardinality(reasons) >= 1),
    source_business_date date NOT NULL,
    planned_business_date date NOT NULL,
    due_business_date date NOT NULL,
    work_state text NOT NULL DEFAULT 'scheduled'
        CHECK (work_state IN ('scheduled', 'delayed', 'completed', 'canceled')),
    status text NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'pending_verification', 'completed', 'rework')),
    proof_ref uuid REFERENCES public.proof_artifacts (proof_id),
    submitted_by uuid,
    submitted_at timestamptz,
    verified_by uuid,
    verified_at timestamptz,
    rework_reason text,
    rolled_forward_count integer NOT NULL DEFAULT 0,
    delayed_since_business_date date,
    row_version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pen_visit_tasks_natural_uq
        UNIQUE (tenant_id, park_id, shed_id, partition_key, source_business_date),
    CONSTRAINT pen_visit_tasks_due_after_plan CHECK (due_business_date >= planned_business_date),
    CONSTRAINT pen_visit_tasks_completed_shape_check CHECK (
      (work_state = 'completed') = (status = 'completed')
    ),
    CONSTRAINT pen_visit_tasks_submitted_has_proof CHECK (
      status = 'open' OR (proof_ref IS NOT NULL AND submitted_at IS NOT NULL AND submitted_by IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS pen_visit_tasks_park_idx
  ON public.pen_visit_tasks (tenant_id, park_id, work_state, due_business_date DESC, task_id DESC);

CREATE INDEX IF NOT EXISTS pen_visit_tasks_sweep_due_idx
  ON public.pen_visit_tasks (tenant_id, work_state, due_business_date, task_id);

CREATE INDEX IF NOT EXISTS pen_visit_tasks_pen_source_idx
  ON public.pen_visit_tasks (tenant_id, shed_id, partition_key, source_business_date DESC);

CREATE TABLE IF NOT EXISTS public.pen_visit_task_sources (
    tenant_id uuid NOT NULL REFERENCES public.tenants (tenant_id),
    task_id uuid NOT NULL REFERENCES public.pen_visit_tasks (task_id) ON DELETE CASCADE,
    source_kind text NOT NULL CHECK (source_kind IN ('pc_care_task', 'sop_submission')),
    source_ref_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, task_id, source_kind, source_ref_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS pen_visit_task_sources_parent_uq
  ON public.pen_visit_task_sources (tenant_id, source_kind, source_ref_id);

-- Seed the known park visitors from current HRMS rows. Missing rows are intentionally not
-- guessed; the kernel/read path will only use explicit configuration.
INSERT INTO public.pen_visit_park_assignees (tenant_id, park_id, user_id)
SELECT DISTINCT ON (p.tenant_id, p.location_id, m.user_id) p.tenant_id, p.location_id, m.user_id
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
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS public.pen_visit_task_sources;
DROP TABLE IF EXISTS public.pen_visit_tasks;
DROP TABLE IF EXISTS public.pen_visit_park_assignees;
