-- +goose Up
-- Birth & Death follow-up workflow engine (maintainer decision 2026-07-27; canonical source
-- docs/decisions/birth-death-workflows.md).
--
-- workflow_instances / workflow_actions are OPERATIONAL/EVENT tables (not initial-seed-owned) per
-- the docs/runbooks/initial-seed-migration-coupling.md classification: rows are produced ONLY by
-- the tasks module's goat.created / goat.exited consumers and the operator answer/complete APIs on
-- the production path. The seed never hand-writes a workflow row.
--
-- goats.time_of_birth is a NULLABLE ADDITIVE column that is backfilled NEVER: NULL = unknown, and
-- every reader falls back to 07:00 IST on the DOB for the birth moment. No seed change is required.
--
-- workflow_instances is one card per (tenant, template, subject goat): the operator's per-goat SOP
-- follow-up work opened by a birth (kid + mother tracks) or a death. The card fields
-- (actions_total/actions_done/next_*/awaiting_verification) are WRITE-MAINTAINED in the same
-- transaction as every action write (compute-on-write), so the hot mobile list and the chip counts
-- read workflow_instances alone with no join to workflow_actions.
CREATE TABLE IF NOT EXISTS public.workflow_instances (
  workflow_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  -- template_key selects the code-defined template (backend/internal/tasks/domain).
  template_key text NOT NULL,
  -- module is DERIVED from template_key at insert ('birth_kid'/'birth_mother' -> 'birth',
  -- 'death' -> 'death') so the list read's index key stays one plain column.
  module text NOT NULL,
  subject_goat_id uuid NOT NULL,
  -- dam_goat_id links a birth_kid workflow to its resolved mother (nullable: dam may be free text
  -- that resolves to no canonical animal).
  dam_goat_id uuid,
  -- event_at is the birth/death moment (time_of_birth-aware; 07:00 IST fallback). event_date is its
  -- Asia/Kolkata business date -- the list's date filter key.
  event_at timestamp with time zone NOT NULL,
  event_date date NOT NULL,
  park_id uuid,
  shed_id uuid,
  state text DEFAULT 'open' NOT NULL,
  -- Write-maintained card fields (see header). actions_total counts MAIN-section actions only; the
  -- colostrum_session strip deliberately does not count.
  actions_total integer DEFAULT 0 NOT NULL,
  actions_done integer DEFAULT 0 NOT NULL,
  next_action_key text,
  next_action_title text,
  next_due_at timestamp with time zone,
  awaiting_verification boolean DEFAULT false NOT NULL,
  row_version integer DEFAULT 1 NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT workflow_instances_pkey PRIMARY KEY (workflow_id),
  CONSTRAINT workflow_instances_template_key_check CHECK (template_key IN ('birth_kid', 'birth_mother', 'death')),
  CONSTRAINT workflow_instances_module_check CHECK (module IN ('birth', 'death')),
  CONSTRAINT workflow_instances_state_check CHECK (state IN ('open', 'completed', 'canceled')),
  CONSTRAINT workflow_instances_counters_check CHECK (actions_total >= 0 AND actions_done >= 0 AND actions_done <= actions_total),
  CONSTRAINT workflow_instances_row_version_check CHECK (row_version >= 1)
);

-- Natural key: one workflow per (tenant, template, subject goat). Twins share ONE birth_mother
-- workflow through this key: the second kid's consumer INSERT ... ON CONFLICT DO NOTHING lands here
-- and opens no duplicate mother track.
CREATE UNIQUE INDEX IF NOT EXISTS workflow_instances_natural_uq
  ON public.workflow_instances (tenant_id, template_key, subject_goat_id);

-- Serving read: GET /app/workflows lists one module's cards for one business date, keyset-ordered by
-- (next_due_at ASC NULLS LAST, workflow_id ASC). This index covers the exact predicate + order, so a
-- page is an index range scan bounded by page size -- never a scan over the tenant's workflow
-- history. The chips aggregate groups the same (tenant_id, module, event_date) prefix.
CREATE INDEX IF NOT EXISTS workflow_instances_list_idx
  ON public.workflow_instances (tenant_id, module, event_date, next_due_at ASC NULLS LAST, workflow_id ASC);

-- Consumer lookups address a workflow by its subject animal (the goat.identifier.added tag-the-kid
-- completion and the mother-attach path both resolve goat -> open workflow). Bounded: a goat has at
-- most a handful of workflows over its life.
CREATE INDEX IF NOT EXISTS workflow_instances_subject_idx
  ON public.workflow_instances (tenant_id, subject_goat_id);

-- workflow_actions is the per-goat step list (<= 13 rows per workflow). Steps are instantiated from
-- the code-defined template when the workflow opens and only their status/answer/proof fields
-- mutate afterwards.
CREATE TABLE IF NOT EXISTS public.workflow_actions (
  action_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  workflow_id uuid NOT NULL,
  action_key text NOT NULL,
  seq integer NOT NULL,
  section text DEFAULT 'main' NOT NULL,
  action_type text NOT NULL,
  title text NOT NULL,
  detail text,
  requires_video boolean DEFAULT false NOT NULL,
  -- options carries the question_select bands (e.g. the Take Weight of Kid weight bands) as a JSON
  -- array of strings. NULL for every other action type.
  options jsonb,
  due_at timestamp with time zone,
  status text DEFAULT 'pending' NOT NULL,
  answer_value text,
  proof_ref text,
  completed_by uuid,
  completed_at timestamp with time zone,
  verification_item_id uuid,
  -- idempotency_key/request_fingerprint record the LAST client write applied to this action, so an
  -- exact replay returns the original result with no side effects and a same-key/different-payload
  -- replay is rejected as a conflict (request-level idempotency, same contract as the counts app
  -- writes).
  idempotency_key text,
  request_fingerprint text,
  row_version integer DEFAULT 1 NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT workflow_actions_pkey PRIMARY KEY (action_id),
  CONSTRAINT workflow_actions_workflow_fk FOREIGN KEY (workflow_id) REFERENCES public.workflow_instances(workflow_id),
  CONSTRAINT workflow_actions_section_check CHECK (section IN ('main', 'colostrum_session')),
  CONSTRAINT workflow_actions_action_type_check CHECK (action_type IN ('question', 'question_select', 'action', 'approval')),
  CONSTRAINT workflow_actions_status_check CHECK (status IN ('pending', 'in_review', 'completed', 'rework', 'canceled')),
  CONSTRAINT workflow_actions_seq_check CHECK (seq >= 1),
  CONSTRAINT workflow_actions_row_version_check CHECK (row_version >= 1)
);

-- Natural key: a template step exists once per workflow. The opener's ON CONFLICT DO NOTHING makes
-- redelivered goat.created/goat.exited events idempotent at the row level.
CREATE UNIQUE INDEX IF NOT EXISTS workflow_actions_natural_uq
  ON public.workflow_actions (workflow_id, action_key);

-- Serving read: the detail screen loads one workflow's steps ordered by seq (<= 13 rows), and the
-- card-field maintenance aggregate groups by workflow_id in the same transaction as each write.
CREATE INDEX IF NOT EXISTS workflow_actions_workflow_seq_idx
  ON public.workflow_actions (workflow_id, seq);

-- Request-level idempotency: one client Idempotency-Key claims at most one action write per tenant.
-- Partial: historical rows (and template instantiation) carry no key.
CREATE UNIQUE INDEX IF NOT EXISTS workflow_actions_idempotency_uq
  ON public.workflow_actions (tenant_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

-- goats.time_of_birth: the operator-supplied birth time (HH:MM IST) for a newborn. Nullable,
-- additive, never backfilled -- NULL = unknown, readers fall back to 07:00 IST on the DOB. Used by
-- the birth workflow opener for the EVENT+1H step and the colostrum session strip.
ALTER TABLE public.goats
    ADD COLUMN IF NOT EXISTS time_of_birth time without time zone;

-- +goose Down
ALTER TABLE public.goats DROP COLUMN IF EXISTS time_of_birth;
DROP TABLE IF EXISTS public.workflow_actions;
DROP TABLE IF EXISTS public.workflow_instances;
