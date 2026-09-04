-- +goose Up
-- LEADERSHIP TASKS MODULE (maintainer decision 2026-09-04): a DIRECTOR raises a task for ONE
-- CXO on the phone -- a title, a written brief and optional attachments (a voice note recorded
-- in the app, photos or videos picked from the gallery, any file). It is not field work: no
-- shed, no animal, no proof of work. The CXO it is addressed to opens it (SEEN -- the fact the
-- drawer badge counts) and moves it open -> in_progress -> done; the raiser may edit or cancel
-- it while it is still open for work. Every task carries a per-tenant running NUMBER ("#12"),
-- minted inside the raise transaction under an advisory lock and never reused.
--
-- Attachments are POINTERS into the proof store (proof_artifacts, proof_type 'attachment'):
-- the bytes ride the existing signed-upload pipeline and the task row records only what the
-- screen labels them with. Deleting an attachment row never deletes the artifact -- the proof
-- store owns retention.
--
-- Seed coupling note (docs/runbooks/initial-seed-migration-coupling.md): these tables are
-- OPERATIONAL, born at runtime from a director's phone; no seed command hand-fills them.
-- seed-fixture-guard:ignore: runtime leadership-raised task tables, not seed input.

CREATE TABLE public.leadership_tasks (
    tenant_id uuid NOT NULL REFERENCES public.tenants (tenant_id),
    task_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The farm's own reference: "#12". Per tenant, monotonic, minted under
    -- pg_advisory_xact_lock(hashtext('leadership_tasks:' || tenant_id)) in the raise write.
    task_no bigint NOT NULL CHECK (task_no >= 1),
    title text NOT NULL CHECK (btrim(title) <> ''),
    body text NOT NULL DEFAULT '',
    -- open        -> raised, not started
    -- in_progress -> the CXO started on it
    -- done        -> the CXO finished it (terminal for the raiser; the CXO may reopen)
    -- cancelled   -> the raiser withdrew it (terminal)
    status text NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'in_progress', 'done', 'cancelled')),
    raised_by uuid NOT NULL,
    assignee_user_id uuid NOT NULL,
    raised_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    done_at timestamptz,
    cancelled_at timestamptz,
    -- Stamped ONCE, the first time the assignee opens the task. The drawer badge is
    -- count(*) of the assignee's rows where this is NULL and status <> 'cancelled'.
    seen_at timestamptz,
    row_version integer NOT NULL DEFAULT 1,
    CONSTRAINT leadership_tasks_no_uq UNIQUE (tenant_id, task_no),
    CONSTRAINT leadership_tasks_not_self CHECK (raised_by <> assignee_user_id)
);

-- The raiser's list and the assignee's list each page this keyset; the badge counts the
-- assignee's unseen rows.
CREATE INDEX leadership_tasks_assignee_idx
    ON public.leadership_tasks (tenant_id, assignee_user_id, raised_at DESC, task_id DESC);
CREATE INDEX leadership_tasks_raiser_idx
    ON public.leadership_tasks (tenant_id, raised_by, raised_at DESC, task_id DESC);
CREATE INDEX leadership_tasks_unseen_idx
    ON public.leadership_tasks (tenant_id, assignee_user_id)
    WHERE seen_at IS NULL AND status <> 'cancelled';

CREATE TABLE public.leadership_task_attachments (
    tenant_id uuid NOT NULL,
    attachment_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id uuid NOT NULL REFERENCES public.leadership_tasks (task_id) ON DELETE CASCADE,
    proof_id uuid NOT NULL REFERENCES public.proof_artifacts (proof_id),
    -- What the phone captured; the detail screen groups by it.
    kind text NOT NULL CHECK (kind IN ('audio', 'video', 'photo', 'file')),
    -- Read from the stored artifact at write time, never trusted from the request.
    mime_type text NOT NULL DEFAULT '',
    file_name text NOT NULL DEFAULT '',
    size_bytes bigint NOT NULL DEFAULT 0,
    duration_ms bigint,
    position integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT leadership_task_attachments_uq UNIQUE (tenant_id, task_id, proof_id)
);

CREATE INDEX leadership_task_attachments_task_idx
    ON public.leadership_task_attachments (tenant_id, task_id, position);

-- PER-PERSON ACCESS ROWS (the 000245 precedent). Since the 2026-08-24 cutover the route
-- table decides from a person's OWN person_module_access rows, and the role map in
-- capability_backfill.go is read only by the one-time backfill -- so a new module reaches
-- nobody already migrated until their rows carry it. Every director job that carries a
-- phone gets the module at view+do (raise, edit, cancel their own); the CXO desk gets it at
-- view+oversee (be assigned, move status). Keyed on the ROLE GRANT, the same population the
-- backfill would have written, and only for people the cutover already migrated (a person
-- with no rows is still on the role fallback path and must stay there). ADDITIVE ONLY: a
-- row an admin already ticked on /people is left exactly as it is.
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'mobile', 'leadership_tasks', ARRAY['view', 'do']::text[]
FROM public.workforce_members m
JOIN public.user_scope_grants g
  ON g.tenant_id = m.tenant_id
 AND g.user_id = m.user_id
 AND g.status = 'active'
 AND (g.valid_to IS NULL OR g.valid_to > now())
 AND g.role IN ('pc_director', 'growth_director', 'feed_director', 'health_director', 'breeding_director')
WHERE m.status = 'active'
  AND m.user_id IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM public.person_access pa
    WHERE pa.tenant_id = m.tenant_id
      AND pa.workforce_member_id = m.workforce_member_id
  )
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING;

INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'mobile', 'leadership_tasks', ARRAY['view', 'oversee']::text[]
FROM public.workforce_members m
JOIN public.user_scope_grants g
  ON g.tenant_id = m.tenant_id
 AND g.user_id = m.user_id
 AND g.status = 'active'
 AND (g.valid_to IS NULL OR g.valid_to > now())
 AND g.role = 'ceo_internal'
WHERE m.status = 'active'
  AND m.user_id IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM public.person_access pa
    WHERE pa.tenant_id = m.tenant_id
      AND pa.workforce_member_id = m.workforce_member_id
  )
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING;

-- The outbox tenant validator learns the leadership_task aggregate. This is the 000229 body
-- with ONE branch added; the Down below restores the 000229 body verbatim.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.validate_outbox_event_tenant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  config_family_key text;
BEGIN
  IF NEW.aggregate_type = 'counts_approval_request' THEN
    IF NOT EXISTS (
      SELECT 1 FROM counts_approval_requests
      WHERE tenant_id = NEW.tenant_id AND approval_request_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'counts approval outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'verification_item' THEN
    IF NOT EXISTS (SELECT 1 FROM verification_items WHERE tenant_id = NEW.tenant_id AND item_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'verification item outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'vaccination_batch' THEN
    IF NOT EXISTS (SELECT 1 FROM obligation_batches WHERE tenant_id = NEW.tenant_id AND batch_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'vaccination batch outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'weighing' THEN
    IF NOT EXISTS (
      SELECT 1 FROM weighing_campaigns
      WHERE tenant_id = NEW.tenant_id AND campaign_id = NEW.aggregate_id
      UNION ALL
      SELECT 1 FROM weighing_observations
      WHERE tenant_id = NEW.tenant_id AND observation_id = NEW.aggregate_id
      UNION ALL
      SELECT 1 FROM weighing_shed_observations
      WHERE tenant_id = NEW.tenant_id AND shed_observation_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'weighing outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'count_base_anchor' THEN
    IF NOT EXISTS (SELECT 1 FROM count_base_anchors WHERE tenant_id = NEW.tenant_id AND base_count_anchor_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'count base anchor outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'shifting_event' THEN
    IF NOT EXISTS (SELECT 1 FROM shifting_events WHERE tenant_id = NEW.tenant_id AND shifting_event_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'shifting event outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'count_projection_exception' THEN
    IF NOT EXISTS (SELECT 1 FROM count_projection_exceptions WHERE tenant_id = NEW.tenant_id AND count_projection_exception_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'count projection exception outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'admin_ui_config_family' THEN
    config_family_key := COALESCE(NEW.payload->>'family_key', NEW.payload #>> '{payload,family_key}');
    IF NOT EXISTS (SELECT 1 FROM admin_ui_config_family_revisions WHERE tenant_id = NEW.tenant_id AND family_key = config_family_key) THEN
      RAISE EXCEPTION 'admin ui config family outbox aggregate % does not exist for tenant %', config_family_key, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'calendar_notification' THEN
    IF NOT EXISTS (SELECT 1 FROM notification_requests WHERE tenant_id = NEW.tenant_id AND notification_request_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'calendar notification outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'calendar_snooze' THEN
    IF NOT EXISTS (SELECT 1 FROM calendar_snoozes WHERE tenant_id = NEW.tenant_id AND snooze_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'calendar snooze outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'obligation_escalation' THEN
    IF NOT EXISTS (SELECT 1 FROM obligation_escalations WHERE tenant_id = NEW.tenant_id AND escalation_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'obligation escalation outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'obligation_instance' THEN
    IF NOT EXISTS (SELECT 1 FROM obligation_instances WHERE tenant_id = NEW.tenant_id AND obligation_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'obligation instance outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'protocol_version' THEN
    IF NOT EXISTS (SELECT 1 FROM protocol_versions WHERE tenant_id = NEW.tenant_id AND protocol_version_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'protocol version outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'correction_request' THEN
    IF NOT EXISTS (SELECT 1 FROM identity_correction_requests WHERE tenant_id = NEW.tenant_id AND correction_request_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'correction request outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'absence' THEN
    IF NOT EXISTS (SELECT 1 FROM workforce_absences WHERE tenant_id = NEW.tenant_id AND absence_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'absence outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'park' THEN
    IF NOT EXISTS (SELECT 1 FROM locations WHERE tenant_id = NEW.tenant_id AND location_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'park outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'feed_direction_session_completion' THEN
    IF NOT EXISTS (SELECT 1 FROM feed_direction_session_completions WHERE tenant_id = NEW.tenant_id AND completion_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'feed direction completion outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'feed_distribution_completion' THEN
    IF NOT EXISTS (SELECT 1 FROM feed_distribution_completions WHERE tenant_id = NEW.tenant_id AND completion_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'feed distribution completion outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'feed_packing_completion' THEN
    IF NOT EXISTS (SELECT 1 FROM feed_packing_completions WHERE tenant_id = NEW.tenant_id AND completion_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'feed packing completion outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'health_protocol_version' THEN
    IF NOT EXISTS (
      SELECT 1 FROM health_protocol_versions
      WHERE tenant_id = NEW.tenant_id AND health_protocol_version_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'health protocol outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'health_diagnosis_run' THEN
    IF NOT EXISTS (
      SELECT 1 FROM health_diagnosis_runs
      WHERE tenant_id = NEW.tenant_id AND health_diagnosis_run_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'health diagnosis run outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'health_case' THEN
    IF NOT EXISTS (
      SELECT 1 FROM health_cases
      WHERE tenant_id = NEW.tenant_id AND health_case_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'health case outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'feed_wastage_completion' THEN
    IF NOT EXISTS (
      SELECT 1 FROM feed_wastage_completions
      WHERE tenant_id = NEW.tenant_id AND completion_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'feed wastage completion outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'pc_care_task' THEN
    IF NOT EXISTS (
      SELECT 1 FROM pc_care_tasks
      WHERE tenant_id = NEW.tenant_id AND task_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'pc care task outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  -- feed_purchase (maintainer decision 2026-08-25, the Toxin module): recording a purchased
  -- feed load emits procurement.feed_purchase.recorded from inside the purchase transaction, so
  -- the toxin consumer can materialize the load's aflatoxin test task. Without this branch the
  -- insert fell through to the goat_identity_events fallback below and was refused 23503 --
  -- caught by TestCreateFeedPurchaseEmitsRecordedEventInTheSameTransaction.
  IF NEW.aggregate_type = 'feed_purchase' THEN
    IF NOT EXISTS (
      SELECT 1 FROM feed_purchases
      WHERE tenant_id = NEW.tenant_id AND feed_purchase_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'feed purchase outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  -- leadership_task (maintainer decision 2026-09-04, the Leadership Tasks module): raising a
  -- task for a CXO and moving its status emit leadership_task.raised / .status_changed from
  -- inside the write transaction, so the notification bridge can push to the person it
  -- concerns. Without this branch the insert falls through to the goat_identity_events
  -- fallback below and is refused 23503.
  IF NEW.aggregate_type = 'leadership_task' THEN
    IF NOT EXISTS (
      SELECT 1 FROM leadership_tasks
      WHERE tenant_id = NEW.tenant_id AND task_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'leadership task outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NOT EXISTS (SELECT 1 FROM goat_identity_events WHERE tenant_id = NEW.tenant_id AND identity_event_id = NEW.event_id) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id USING ERRCODE = '23503';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Removes only the rows this migration could have written; a row an admin ticked on
-- /people is indistinguishable from one written here, which is the honest cost of an
-- additive repair.
DELETE FROM public.person_module_access WHERE surface = 'mobile' AND module_key = 'leadership_tasks';
DROP TABLE IF EXISTS public.leadership_task_attachments;
DROP TABLE IF EXISTS public.leadership_tasks;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.validate_outbox_event_tenant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  config_family_key text;
BEGIN
  IF NEW.aggregate_type = 'counts_approval_request' THEN
    IF NOT EXISTS (
      SELECT 1 FROM counts_approval_requests
      WHERE tenant_id = NEW.tenant_id AND approval_request_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'counts approval outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'verification_item' THEN
    IF NOT EXISTS (SELECT 1 FROM verification_items WHERE tenant_id = NEW.tenant_id AND item_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'verification item outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'vaccination_batch' THEN
    IF NOT EXISTS (SELECT 1 FROM obligation_batches WHERE tenant_id = NEW.tenant_id AND batch_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'vaccination batch outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'weighing' THEN
    IF NOT EXISTS (
      SELECT 1 FROM weighing_campaigns
      WHERE tenant_id = NEW.tenant_id AND campaign_id = NEW.aggregate_id
      UNION ALL
      SELECT 1 FROM weighing_observations
      WHERE tenant_id = NEW.tenant_id AND observation_id = NEW.aggregate_id
      UNION ALL
      SELECT 1 FROM weighing_shed_observations
      WHERE tenant_id = NEW.tenant_id AND shed_observation_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'weighing outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'count_base_anchor' THEN
    IF NOT EXISTS (SELECT 1 FROM count_base_anchors WHERE tenant_id = NEW.tenant_id AND base_count_anchor_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'count base anchor outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'shifting_event' THEN
    IF NOT EXISTS (SELECT 1 FROM shifting_events WHERE tenant_id = NEW.tenant_id AND shifting_event_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'shifting event outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'count_projection_exception' THEN
    IF NOT EXISTS (SELECT 1 FROM count_projection_exceptions WHERE tenant_id = NEW.tenant_id AND count_projection_exception_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'count projection exception outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'admin_ui_config_family' THEN
    config_family_key := COALESCE(NEW.payload->>'family_key', NEW.payload #>> '{payload,family_key}');
    IF NOT EXISTS (SELECT 1 FROM admin_ui_config_family_revisions WHERE tenant_id = NEW.tenant_id AND family_key = config_family_key) THEN
      RAISE EXCEPTION 'admin ui config family outbox aggregate % does not exist for tenant %', config_family_key, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'calendar_notification' THEN
    IF NOT EXISTS (SELECT 1 FROM notification_requests WHERE tenant_id = NEW.tenant_id AND notification_request_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'calendar notification outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'calendar_snooze' THEN
    IF NOT EXISTS (SELECT 1 FROM calendar_snoozes WHERE tenant_id = NEW.tenant_id AND snooze_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'calendar snooze outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'obligation_escalation' THEN
    IF NOT EXISTS (SELECT 1 FROM obligation_escalations WHERE tenant_id = NEW.tenant_id AND escalation_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'obligation escalation outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'obligation_instance' THEN
    IF NOT EXISTS (SELECT 1 FROM obligation_instances WHERE tenant_id = NEW.tenant_id AND obligation_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'obligation instance outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'protocol_version' THEN
    IF NOT EXISTS (SELECT 1 FROM protocol_versions WHERE tenant_id = NEW.tenant_id AND protocol_version_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'protocol version outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'correction_request' THEN
    IF NOT EXISTS (SELECT 1 FROM identity_correction_requests WHERE tenant_id = NEW.tenant_id AND correction_request_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'correction request outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'absence' THEN
    IF NOT EXISTS (SELECT 1 FROM workforce_absences WHERE tenant_id = NEW.tenant_id AND absence_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'absence outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'park' THEN
    IF NOT EXISTS (SELECT 1 FROM locations WHERE tenant_id = NEW.tenant_id AND location_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'park outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'feed_direction_session_completion' THEN
    IF NOT EXISTS (SELECT 1 FROM feed_direction_session_completions WHERE tenant_id = NEW.tenant_id AND completion_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'feed direction completion outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'feed_distribution_completion' THEN
    IF NOT EXISTS (SELECT 1 FROM feed_distribution_completions WHERE tenant_id = NEW.tenant_id AND completion_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'feed distribution completion outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'feed_packing_completion' THEN
    IF NOT EXISTS (SELECT 1 FROM feed_packing_completions WHERE tenant_id = NEW.tenant_id AND completion_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'feed packing completion outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'health_protocol_version' THEN
    IF NOT EXISTS (
      SELECT 1 FROM health_protocol_versions
      WHERE tenant_id = NEW.tenant_id AND health_protocol_version_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'health protocol outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'health_diagnosis_run' THEN
    IF NOT EXISTS (
      SELECT 1 FROM health_diagnosis_runs
      WHERE tenant_id = NEW.tenant_id AND health_diagnosis_run_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'health diagnosis run outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'health_case' THEN
    IF NOT EXISTS (
      SELECT 1 FROM health_cases
      WHERE tenant_id = NEW.tenant_id AND health_case_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'health case outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'feed_wastage_completion' THEN
    IF NOT EXISTS (
      SELECT 1 FROM feed_wastage_completions
      WHERE tenant_id = NEW.tenant_id AND completion_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'feed wastage completion outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'pc_care_task' THEN
    IF NOT EXISTS (
      SELECT 1 FROM pc_care_tasks
      WHERE tenant_id = NEW.tenant_id AND task_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'pc care task outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  -- feed_purchase (maintainer decision 2026-08-25, the Toxin module): recording a purchased
  -- feed load emits procurement.feed_purchase.recorded from inside the purchase transaction, so
  -- the toxin consumer can materialize the load's aflatoxin test task. Without this branch the
  -- insert fell through to the goat_identity_events fallback below and was refused 23503 --
  -- caught by TestCreateFeedPurchaseEmitsRecordedEventInTheSameTransaction.
  IF NEW.aggregate_type = 'feed_purchase' THEN
    IF NOT EXISTS (
      SELECT 1 FROM feed_purchases
      WHERE tenant_id = NEW.tenant_id AND feed_purchase_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'feed purchase outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NOT EXISTS (SELECT 1 FROM goat_identity_events WHERE tenant_id = NEW.tenant_id AND identity_event_id = NEW.event_id) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id USING ERRCODE = '23503';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
