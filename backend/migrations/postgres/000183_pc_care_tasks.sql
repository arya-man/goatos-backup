-- +goose Up
-- seed-fixture-guard:ignore: operational PC Care rows are written by the planner/operator/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- PC CARE module (maintainer decision 2026-08-21).
--
-- PC Care is a NEW assigned-task module (module_key pc_care, owner pc_director) with
-- four work categories: deworming, ticks_removal, hoof_trimming, hair_trimming.
-- The CEO plans a task for ONE pen on ONE business date and assigns ONE OR MORE
-- operators (multi-operator by design -- deliberately unlike weighing's
-- one-operator-per-bucket). Assigned operators scan RFIDs free-flow (the tag is
-- stored VERBATIM, never resolved against the herd) to add animals, then record
-- mandatory live-camera videos per animal:
--   deworming / ticks_removal  -> ONE video  (slot: video)
--   hoof_trimming / hair_trimming -> THREE videos (slots: before_video, during_video, after_video)
-- Any assignee may fill any missing slot on any scanned animal; any assignee submits
-- the WHOLE task once every scanned animal's slot set is complete.
--
-- THE TASK ROW IS ALSO THE COMPLETION ROW. Feed needed a separate completions table
-- because its worklist derives from a frozen sheet with no durable planned row; here
-- the CEO's plan already creates the durable row and the submit grain equals the task
-- grain, so a second table would duplicate this row's identity 1:1. Two ORTHOGONAL
-- state columns keep the kernel and the verification gate apart:
--   work_state (weighing 000059 kernel shape) scheduled|delayed|completed|closed|canceled
--   status     (feed 000176 gate shape)       open|pending_verification|completed|rework
-- Submit flips status -> 'pending_verification' and enqueues ONE verification item for
-- the whole task. Verifier approve flips status AND work_state -> 'completed' in one
-- transaction (this is where pc_care.task.completed is emitted -- ONLY here). Reject
-- flips status -> 'rework'; operators re-record slots on the SAME animal rows and
-- resubmit, bumping row_version so the re-enqueue idempotency key
-- ("pc-care-verification:<task_id>:<row_version>") mints a fresh verification item.
CREATE TABLE IF NOT EXISTS public.pc_care_tasks (
  task_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  category text NOT NULL,
  park_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  -- partition_label is the pen ("2", "Part 3"), NULL/'' for an undivided shed;
  -- partition_key normalizes it for the natural key, same shape as 000137/000176.
  partition_label text,
  partition_key text GENERATED ALWAYS AS (
    CASE WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
         ELSE lower(btrim(partition_label)) END
  ) STORED,
  -- planned_business_date is IMMUTABLE (the audit anchor the CEO chose);
  -- due_business_date rolls FORWARD ONLY when the kernel carries unfinished work.
  planned_business_date date NOT NULL,
  due_business_date date NOT NULL,
  work_state text DEFAULT 'scheduled' NOT NULL,
  status text DEFAULT 'open' NOT NULL,
  rolled_forward_count integer DEFAULT 0 NOT NULL,
  delayed_since_business_date date,
  terminal_at timestamp with time zone,
  submitted_by uuid,
  submitted_at timestamp with time zone,
  -- verified_by / verified_at are stamped when a verifier approves (status -> 'completed').
  verified_by uuid,
  verified_at timestamp with time zone,
  -- rework_reason is the verifier's rejection reason (status -> 'rework'); clients
  -- render it verbatim (backend-owned copy).
  rework_reason text,
  row_version integer DEFAULT 1 NOT NULL,
  idempotency_key text NOT NULL,
  created_by uuid NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT pc_care_tasks_pkey PRIMARY KEY (task_id),
  CONSTRAINT pc_care_tasks_category_check CHECK (
    category IN ('deworming', 'ticks_removal', 'hoof_trimming', 'hair_trimming')
  ),
  CONSTRAINT pc_care_tasks_work_state_check CHECK (
    work_state IN ('scheduled', 'delayed', 'completed', 'closed', 'canceled')
  ),
  CONSTRAINT pc_care_tasks_status_check CHECK (
    status IN ('open', 'pending_verification', 'completed', 'rework')
  ),
  CONSTRAINT pc_care_tasks_due_not_before_planned CHECK (due_business_date >= planned_business_date),
  CONSTRAINT pc_care_tasks_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id),
  CONSTRAINT pc_care_tasks_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id)
);

-- ONE live task per (category, pen, planned day). Canceled tasks fall out of the key
-- so the CEO can replan the same pen-day after a cancel.
CREATE UNIQUE INDEX IF NOT EXISTS pc_care_tasks_natural_uq
  ON public.pc_care_tasks (tenant_id, category, park_id, shed_id, partition_key, planned_business_date)
  WHERE work_state <> 'canceled';

-- Request-level idempotency: same planner-create key never inserts twice.
CREATE UNIQUE INDEX IF NOT EXISTS pc_care_tasks_idempotency_uq
  ON public.pc_care_tasks (tenant_id, idempotency_key);

-- Kernel sweep index trio (weighing 000059 shape): open-work keyset walk, due-date
-- roll-forward sweep, planned-date reporting sweep.
CREATE INDEX IF NOT EXISTS pc_care_tasks_open_keyset_idx
  ON public.pc_care_tasks (tenant_id, task_id)
  WHERE work_state IN ('scheduled', 'delayed');
CREATE INDEX IF NOT EXISTS pc_care_tasks_sweep_due_idx
  ON public.pc_care_tasks (tenant_id, work_state, due_business_date, task_id);
CREATE INDEX IF NOT EXISTS pc_care_tasks_sweep_planned_idx
  ON public.pc_care_tasks (tenant_id, work_state, planned_business_date, task_id);

-- Serving read: monitor list and worklist batch-read one park-day in one indexed
-- read, bounded by the park's pen catalog x categories -- never by herd size.
CREATE INDEX IF NOT EXISTS pc_care_tasks_serving_idx
  ON public.pc_care_tasks (tenant_id, park_id, due_business_date, work_state);

-- Assignees: the CEO names one or more operators PER TASK. Authority to write into a
-- task is membership here (plus pc_care.execute); the permission alone never
-- authorizes a write.
CREATE TABLE IF NOT EXISTS public.pc_care_task_assignees (
  tenant_id uuid NOT NULL,
  task_id uuid NOT NULL REFERENCES public.pc_care_tasks(task_id) ON DELETE CASCADE,
  operator_user_id uuid NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT pc_care_task_assignees_pkey PRIMARY KEY (tenant_id, task_id, operator_user_id)
);

-- Operator worklist: "my tasks" resolves from this index, then joins the task row.
CREATE INDEX IF NOT EXISTS pc_care_task_assignees_operator_idx
  ON public.pc_care_task_assignees (tenant_id, operator_user_id, task_id);

-- Scanned animals: one row per RFID scanned into a task, written AT SCAN TIME so
-- peers see each other's scans and dedup happens at the scan, not at submit.
-- scanned_identifier is stored VERBATIM (free-flow; no herd lookup, no goat FK).
-- Slot proof columns: 1-video categories use the video_* triple; 3-video categories
-- use before_/during_/after_. Each ref carries its attribution (captured_by/at)
-- because ANY assignee may have filmed it -- "Captured by X" is a product fact.
CREATE TABLE IF NOT EXISTS public.pc_care_task_animals (
  animal_row_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  task_id uuid NOT NULL REFERENCES public.pc_care_tasks(task_id) ON DELETE CASCADE,
  scanned_identifier text NOT NULL,
  scanned_by uuid NOT NULL,
  scanned_at timestamp with time zone DEFAULT now() NOT NULL,
  video_proof_ref text,
  video_captured_by uuid,
  video_captured_at timestamp with time zone,
  before_proof_ref text,
  before_captured_by uuid,
  before_captured_at timestamp with time zone,
  during_proof_ref text,
  during_captured_by uuid,
  during_captured_at timestamp with time zone,
  after_proof_ref text,
  after_captured_by uuid,
  after_captured_at timestamp with time zone,
  submitted_at timestamp with time zone,
  idempotency_key text NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT pc_care_task_animals_pkey PRIMARY KEY (animal_row_id),
  CONSTRAINT pc_care_task_animals_tag_check CHECK (btrim(scanned_identifier) <> ''),
  -- Each slot's ref and attribution travel together or not at all: a ref with no
  -- recorder is unauditable, a recorder with no ref is a half-written act.
  CONSTRAINT pc_care_task_animals_video_slot_check CHECK (
    (video_proof_ref IS NULL AND video_captured_by IS NULL AND video_captured_at IS NULL)
    OR (video_proof_ref IS NOT NULL AND video_captured_by IS NOT NULL AND video_captured_at IS NOT NULL)
  ),
  CONSTRAINT pc_care_task_animals_before_slot_check CHECK (
    (before_proof_ref IS NULL AND before_captured_by IS NULL AND before_captured_at IS NULL)
    OR (before_proof_ref IS NOT NULL AND before_captured_by IS NOT NULL AND before_captured_at IS NOT NULL)
  ),
  CONSTRAINT pc_care_task_animals_during_slot_check CHECK (
    (during_proof_ref IS NULL AND during_captured_by IS NULL AND during_captured_at IS NULL)
    OR (during_proof_ref IS NOT NULL AND during_captured_by IS NOT NULL AND during_captured_at IS NOT NULL)
  ),
  CONSTRAINT pc_care_task_animals_after_slot_check CHECK (
    (after_proof_ref IS NULL AND after_captured_by IS NULL AND after_captured_at IS NULL)
    OR (after_proof_ref IS NOT NULL AND after_captured_by IS NOT NULL AND after_captured_at IS NOT NULL)
  )
);

-- DEDUP: one row per tag per task for the task's WHOLE LIFE. Deliberately NOT
-- partial on submitted_at like weighing 000073: a weighing bucket hosts repeated
-- capture rounds after submit, but a PC task is one-shot -- after a rework the
-- operator re-records slots on the SAME row (update in place), so a partial index
-- would let one submitted + one open row carry the same tag in one task, which is
-- exactly the duplicate this rule forbids. 23505 here is translated to a typed
-- conflict and surfaces as HTTP 409 duplicate_scan.
CREATE UNIQUE INDEX IF NOT EXISTS pc_care_task_animals_tag_uidx
  ON public.pc_care_task_animals (tenant_id, task_id, (lower(btrim(scanned_identifier))));

-- Request-level idempotency: an offline phone's scan replay never inserts twice.
CREATE UNIQUE INDEX IF NOT EXISTS pc_care_task_animals_idempotency_uq
  ON public.pc_care_task_animals (tenant_id, idempotency_key);

-- Captures keyset: the peer-visibility poll pages one task's animal rows.
CREATE INDEX IF NOT EXISTS pc_care_task_animals_task_idx
  ON public.pc_care_task_animals (tenant_id, task_id, animal_row_id);

-- Add the pc_care_task branch to the outbox tenant-parity trigger so the
-- pc_care.task.completed producer's outbox INSERT validates against pc_care_tasks.
-- Carries forward every branch present in 000176 (the latest replacement) and
-- appends pc_care_task. Function replacement only -- no table lock.
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

  IF NOT EXISTS (SELECT 1 FROM goat_identity_events WHERE tenant_id = NEW.tenant_id AND identity_event_id = NEW.event_id) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id USING ERRCODE = '23503';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Restore the trigger function to its 000176 form (drop the pc_care_task branch)
-- and drop the three tables.
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

  IF NOT EXISTS (SELECT 1 FROM goat_identity_events WHERE tenant_id = NEW.tenant_id AND identity_event_id = NEW.event_id) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id USING ERRCODE = '23503';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TABLE IF EXISTS public.pc_care_task_animals;
DROP TABLE IF EXISTS public.pc_care_task_assignees;
DROP TABLE IF EXISTS public.pc_care_tasks;
