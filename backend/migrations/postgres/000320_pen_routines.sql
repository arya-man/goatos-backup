-- +goose Up
-- seed-fixture-guard:ignore: routine definitions are authored by the CEO on /routines and their tasks are written by the kernel materializer and the park head's submit; nothing here is Vaccination HRMS seed contract
--
-- PEN ROUTINES (maintainer instruction 2026-09-16): configurable recurring pen checks for the
-- park head -- "each pen cleaned?", per park, daily / weekly / monthly / every N days or
-- triggered after work, with the ROLES who owe it, the questions, the photo/video expectation
-- and the check-in all authored as data. Canonical prose: docs/decisions/pen-routines.md
-- (2026-09-17 revision: assign by ROLE, whole-park tasks, every N days).
--
-- Four tables, one shape borrowed from three places:
--   * pen_routine_definitions + pen_routine_versions: the RULE, versioned and never edited in
--     place (the health-protocol rule) so an open task pins the form it was raised under.
--   * pen_routine_pens: the ticked pens of a selected_pens routine. WHO owes the routine is
--     definitions.assignee_roles, resolved per read against the live role grants (any holder
--     doing it is enough; roles nobody holds in the park raise no task, loudly).
--   * pen_routine_tasks: the PC Care / pen-visit two-dimension row -- the kernel clock
--     (work_state, planned immutable, due rolls forward only) and the verifier gate (status).
--   * pen_routine_task_presence: the "someone entered the pen" clock, one row per punch with
--     the workforce clock's honest-capture fields.

CREATE TABLE public.pen_routine_definitions (
    routine_id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES public.tenants (tenant_id),
    park_id          uuid NOT NULL REFERENCES public.locations (location_id),
    name             text NOT NULL CHECK (btrim(name) <> ''),
    -- scope_kind: every ACTIVE pen of the park (occupied_only skips pens holding no live
    -- animals on the day), the ticked list in pen_routine_pens, or 'park' -- ONE task for the
    -- whole park per occurrence, with no pen ("Check the medicine store").
    scope_kind       text NOT NULL CHECK (scope_kind IN ('all_pens', 'selected_pens', 'park')),
    occupied_only    boolean NOT NULL DEFAULT true,
    -- cadence_kind decides which business dates raise a task; the arrays are its parameters.
    -- weekdays: ISO 1=Mon..7=Sun. month_days: 1..31, a day past the month's end is its last day.
    -- after_work_kinds: the closed vocabulary in penroutines/domain (vaccination, deworming,
    -- ... weighing, feed_distribution, shifting) matched to verification item categories.
    -- interval_days: every_n_days only, the N (2..90); the occurrences are start_date,
    -- start_date + N, start_date + 2N, ...
    cadence_kind     text NOT NULL CHECK (cadence_kind IN ('daily', 'weekly', 'monthly', 'every_n_days', 'after_work')),
    weekdays         smallint[] NOT NULL DEFAULT '{}',
    month_days       smallint[] NOT NULL DEFAULT '{}',
    after_work_kinds text[] NOT NULL DEFAULT '{}',
    interval_days    integer,
    -- start_date: nothing raises before it, for every cadence. Supplied by the writer (the app
    -- layer defaults it to today IST), never a DB clock.
    start_date       date NOT NULL,
    -- assignee_roles: WHO owes the routine, by role, from the closed vocabulary in
    -- penroutines/domain.AssignableRoles. Whoever holds one of these roles FOR THE ROUTINE'S
    -- PARK gets the task (a tenant-scoped park_head covers only his HRMS home park).
    assignee_roles   text[] NOT NULL,
    -- due_offset_days: planned date = cadence day (or the work day) + this many days.
    due_offset_days  integer NOT NULL DEFAULT 0 CHECK (due_offset_days BETWEEN 0 AND 30),
    -- LOCAL Asia/Kolkata wall-clock time the day's push goes out; the business day is IST by rule.
    notify_time      time NOT NULL DEFAULT TIME '07:00',
    -- review_kind: 'verifier' hands the submit to the verifier queue; 'none' completes on submit.
    review_kind      text NOT NULL CHECK (review_kind IN ('verifier', 'none')),
    status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'retired')),
    -- current_version is the version the NEXT occurrence is raised under (pen_routine_versions).
    current_version  integer NOT NULL DEFAULT 1 CHECK (current_version >= 1),
    created_by       uuid,
    updated_by       uuid,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    row_version      integer NOT NULL DEFAULT 1,
    CONSTRAINT pen_routine_definitions_weekly_shape CHECK (
        cadence_kind <> 'weekly' OR cardinality(weekdays) >= 1),
    CONSTRAINT pen_routine_definitions_monthly_shape CHECK (
        cadence_kind <> 'monthly' OR cardinality(month_days) >= 1),
    CONSTRAINT pen_routine_definitions_after_work_shape CHECK (
        cadence_kind <> 'after_work' OR cardinality(after_work_kinds) >= 1),
    CONSTRAINT pen_routine_definitions_every_n_days_shape CHECK (
        (cadence_kind = 'every_n_days' AND interval_days BETWEEN 2 AND 90)
        OR (cadence_kind <> 'every_n_days' AND interval_days IS NULL)),
    -- after_work happens IN a pen, so a whole-park routine cannot trigger on it.
    CONSTRAINT pen_routine_definitions_park_scope_shape CHECK (
        NOT (scope_kind = 'park' AND cadence_kind = 'after_work')),
    CONSTRAINT pen_routine_definitions_assignee_roles_shape CHECK (
        cardinality(assignee_roles) >= 1
        AND assignee_roles <@ ARRAY['park_head', 'pc_director', 'breeding_director', 'growth_director',
                                    'feed_director', 'health_director', 'procurement_director',
                                    'ceo_internal']::text[])
);
-- One name per park among the routines still in use; a retired routine frees its name.
CREATE UNIQUE INDEX pen_routine_definitions_park_name_uq
    ON public.pen_routine_definitions (tenant_id, park_id, lower(btrim(name)))
    WHERE status <> 'retired';
-- The kernel's read: every active routine of a tenant.
CREATE INDEX pen_routine_definitions_active_idx
    ON public.pen_routine_definitions (tenant_id, status, park_id);

-- Every edit writes a new version row; a task pins (routine_id, version) so the form the park
-- head opened is the form he submits. name/instruction/evidence/review_kind are the fields a
-- task renders; the cadence and the assignee roles are read live from the definition.
CREATE TABLE public.pen_routine_versions (
    tenant_id    uuid NOT NULL REFERENCES public.tenants (tenant_id),
    routine_id   uuid NOT NULL REFERENCES public.pen_routine_definitions (routine_id) ON DELETE CASCADE,
    version      integer NOT NULL CHECK (version >= 1),
    name         text NOT NULL,
    instruction  text NOT NULL DEFAULT '',
    -- evidence is penroutines/domain.Evidence: {questions:[...], photo:{min,max},
    -- video:{min,max}, presence:'required'|'off'}. Validated by the domain before every write.
    evidence     jsonb NOT NULL,
    review_kind  text NOT NULL CHECK (review_kind IN ('verifier', 'none')),
    created_by   uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, routine_id, version)
);

-- The ticked pens of a selected_pens routine. partition_key normalizes the label for the key,
-- the 000137/000176/000183/000277 shape; display always goes through oploc.
CREATE TABLE public.pen_routine_pens (
    tenant_id       uuid NOT NULL REFERENCES public.tenants (tenant_id),
    routine_id      uuid NOT NULL REFERENCES public.pen_routine_definitions (routine_id) ON DELETE CASCADE,
    shed_id         uuid NOT NULL,
    partition_label text,
    partition_key   text GENERATED ALWAYS AS (
        CASE WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
             ELSE lower(btrim(partition_label)) END
    ) STORED,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, routine_id, shed_id, partition_key)
);

CREATE TABLE public.pen_routine_tasks (
    task_id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES public.tenants (tenant_id),
    routine_id       uuid NOT NULL REFERENCES public.pen_routine_definitions (routine_id),
    routine_version  integer NOT NULL CHECK (routine_version >= 1),
    park_id          uuid NOT NULL,
    -- shed_id is NULL exactly for a task raised by a whole-park routine (scope_kind 'park');
    -- such a task names no pen, so it carries no partition either.
    shed_id          uuid,
    shed_key         text GENERATED ALWAYS AS (COALESCE(shed_id::text, 'park')) STORED,
    partition_label  text,
    partition_key    text GENERATED ALWAYS AS (
        CASE WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
             ELSE lower(btrim(partition_label)) END
    ) STORED,
    -- trigger_kinds: for an after_work routine, WHICH work raised this task (a pen dewormed and
    -- vaccinated on one day is ONE task carrying both). Empty for a calendar cadence.
    trigger_kinds    text[] NOT NULL DEFAULT '{}',
    -- source_business_date: the IST day that raised the task -- the cadence day itself, or the
    -- day the work was submitted. planned = source + due_offset_days, immutable. due rolls
    -- FORWARD ONLY as 'delayed'.
    source_business_date  date NOT NULL,
    planned_business_date date NOT NULL,
    due_business_date     date NOT NULL,
    work_state       text NOT NULL DEFAULT 'scheduled'
        CHECK (work_state IN ('scheduled', 'delayed', 'completed', 'canceled')),
    status           text NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'pending_verification', 'completed', 'rework')),
    -- answers: {question_id: value} keyed by the pinned version's question ids.
    answers          jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- proof_refs: [{"ref": "<proof_id>", "kind": "photo"|"video"}], every one a finished
    -- in-app-camera capture validated before the write.
    proof_refs       jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- The pen check-in the submitter made on this task (denormalized from the presence rows for
    -- the read; the rows are the record).
    entered_at       timestamptz,
    entered_by       uuid,
    left_at          timestamptz,
    submitted_by     uuid,
    submitted_at     timestamptz,
    verified_by      uuid,
    verified_at      timestamptz,
    rework_reason    text,
    rolled_forward_count        integer NOT NULL DEFAULT 0,
    delayed_since_business_date date,
    row_version      integer NOT NULL DEFAULT 1,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    -- The natural key that makes the kernel tick idempotent: one task per pen per occurrence,
    -- and one per occurrence for a whole-park routine (shed_key 'park', partition_key 'whole').
    CONSTRAINT pen_routine_tasks_natural_uq
        UNIQUE (tenant_id, routine_id, shed_key, partition_key, planned_business_date),
    CONSTRAINT pen_routine_tasks_park_task_shape CHECK (shed_id IS NOT NULL OR partition_label IS NULL),
    CONSTRAINT pen_routine_tasks_due_after_plan CHECK (due_business_date >= planned_business_date),
    -- The two dimensions agree, and a submitted task always carries who and when.
    CONSTRAINT pen_routine_tasks_completed_shape_check CHECK (
        (work_state = 'completed') = (status = 'completed')),
    CONSTRAINT pen_routine_tasks_submitted_shape_check CHECK (
        status = 'open' OR (submitted_at IS NOT NULL AND submitted_by IS NOT NULL))
);
-- The assignee's list: the routines they are on, open first, keyset on (due, id).
CREATE INDEX pen_routine_tasks_routine_idx
    ON public.pen_routine_tasks (tenant_id, routine_id, work_state, due_business_date DESC, task_id DESC);
-- The Work Board and the web Today table: one park, one due day.
CREATE INDEX pen_routine_tasks_park_day_idx
    ON public.pen_routine_tasks (tenant_id, park_id, due_business_date, work_state, task_id);
-- The kernel sweep: unfinished work whose due date has passed.
CREATE INDEX pen_routine_tasks_sweep_due_idx
    ON public.pen_routine_tasks (tenant_id, work_state, due_business_date, task_id);

-- One row per punch. location / integrity carry the workforce clock's honest-capture fields
-- ({latitude, longitude, accuracy_m, status} and {mock_location, device_id, app_version, ...});
-- V1 records them for the verifier and refuses nothing by distance (the clock's D3).
CREATE TABLE public.pen_routine_task_presence (
    presence_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES public.tenants (tenant_id),
    task_id     uuid NOT NULL REFERENCES public.pen_routine_tasks (task_id) ON DELETE CASCADE,
    user_id     uuid NOT NULL,
    event_type  text NOT NULL CHECK (event_type IN ('enter', 'leave')),
    captured_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    location    jsonb NOT NULL DEFAULT '{}'::jsonb,
    integrity   jsonb NOT NULL DEFAULT '{}'::jsonb,
    idempotency_key text NOT NULL,
    CONSTRAINT pen_routine_task_presence_idem_uq UNIQUE (tenant_id, idempotency_key)
);
CREATE INDEX pen_routine_task_presence_task_idx
    ON public.pen_routine_task_presence (tenant_id, task_id, captured_at);

-- +goose Down
DROP TABLE IF EXISTS public.pen_routine_task_presence;
DROP TABLE IF EXISTS public.pen_routine_tasks;
DROP TABLE IF EXISTS public.pen_routine_pens;
DROP TABLE IF EXISTS public.pen_routine_versions;
DROP TABLE IF EXISTS public.pen_routine_definitions;
