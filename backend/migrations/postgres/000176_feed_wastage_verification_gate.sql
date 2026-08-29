-- +goose Up
-- seed-fixture-guard:ignore: operational Feed Wastage rows are written by the feed operator/verifier flow; they do not change the Vaccination HRMS seed contract
--
-- FEED WASTAGE verification gate (maintainer decision 2026-08-18).
--
-- Feed Wastage is a NEW daily task on EXPERIMENT pens only: every feed day, each pen
-- that is on a hand-authored feed experiment owes ONE wastage video -- the operator
-- films the leftover feed for that pen and submits. The verifier watches the clip;
-- when she can read the leftover weight in it she RECORDS that weight in kg (the
-- measurement lives on this row -- see wastage_kg below) and approves; when she
-- cannot, she rejects and the operator re-shoots.
--
-- Grain is the PEN-DAY: (tenant, park, shed, partition_key, target_date, workflow).
-- There is deliberately NO session_no. Packing and distribution are per-bag work
-- (morning and evening are two bags, two videos), but wastage is measured once a day
-- -- what is left over after the day's feeding -- so one pen owes exactly one wastage
-- video per feed day. workflow is constrained to 'experiment': the worklist is
-- derived from the day's EXPERIMENT sheet and a 'normal' row here would be a row no
-- worklist line ever matches.
--
-- Same two-phase, verifier-gated lifecycle as the packing gate (000033):
--   operator completes with ONE MANDATORY wastage VIDEO -> 'pending_verification'
--   verifier approve -> 'completed' (feed.wastage.completed is emitted HERE only)
--   verifier reject  -> 'rework' (operator re-shoots + re-submits)
--
-- wastage_kg / wastage_recorded_by / wastage_recorded_at are the VERIFIER'S measured
-- value -- the number she read off the video. They are NULL until she records one,
-- travel together or not at all (see the CHECK), and a later entry REPLACES the
-- value (recorded_by/at move with it). Unlike the weighing weight correction
-- (000172) there is no operator_* original to preserve: the operator never types a
-- number in this flow, the video is the only thing they submit.
--
-- This is the canonical producer of the feed.wastage.completed domain event (see
-- context/architecture/domain-event-registry.json). Emitted ONLY at verifier
-- approval, never at operator completion and never at measurement recording.
CREATE TABLE IF NOT EXISTS public.feed_wastage_completions (
  completion_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  -- partition_label is the pen ("2", "Part 3"), NULL/'' for an undivided shed;
  -- partition_key normalizes it for the natural key, same shape as 000137.
  partition_label text,
  partition_key text GENERATED ALWAYS AS (
    CASE WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
         ELSE lower(btrim(partition_label)) END
  ) STORED,
  target_date date NOT NULL,
  workflow text NOT NULL DEFAULT 'experiment',
  status text DEFAULT 'pending_verification' NOT NULL,
  -- wastage_proof_ref is the ONE MANDATORY wastage VIDEO proof_id (server-minted,
  -- from /app/proofs/*). Bytes live in GCS; only the reference is stored here.
  wastage_proof_ref text,
  -- verified_by / verified_at are stamped when a verifier approves (status -> 'completed').
  verified_by uuid,
  verified_at timestamp with time zone,
  -- rework_reason is the verifier's rejection reason (status -> 'rework').
  rework_reason text,
  completed_by uuid,
  idempotency_key text NOT NULL,
  -- The verifier's measured wastage, read off the approved-or-pending video. numeric(8,3)
  -- matches the weighing observation grain: nothing a pen wastes in a day exceeds it.
  -- ZERO IS A LEGITIMATE VALUE -- an empty trough is a real, good measurement -- so the
  -- range check is >= 0, not > 0 (deliberately different from weighing 000172).
  wastage_kg numeric(8,3),
  wastage_recorded_by uuid,
  wastage_recorded_at timestamp with time zone,
  row_version integer DEFAULT 1 NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT feed_wastage_completions_pkey PRIMARY KEY (completion_id),
  -- EXPERIMENT ONLY. The worklist triggers rows for experiment pens alone
  -- (maintainer decision 2026-08-18); a 'normal' row would be unreachable work.
  CONSTRAINT feed_wastage_completions_workflow_check CHECK (workflow = 'experiment'),
  CONSTRAINT feed_wastage_completions_status_check CHECK (status IN ('pending_verification', 'completed', 'rework')),
  -- A row awaiting verification OR completed MUST carry the mandatory wastage video.
  CONSTRAINT feed_wastage_completions_proof_check CHECK (
    (status NOT IN ('pending_verification', 'completed'))
    OR (wastage_proof_ref IS NOT NULL AND btrim(wastage_proof_ref) <> '')
  ),
  -- The three measurement facts travel together or not at all: a value with no
  -- recorder is unauditable, a recorder with no value is a half-written act.
  CONSTRAINT feed_wastage_completions_measurement_check CHECK (
    (wastage_kg IS NULL AND wastage_recorded_by IS NULL AND wastage_recorded_at IS NULL)
    OR (wastage_kg IS NOT NULL AND wastage_recorded_by IS NOT NULL AND wastage_recorded_at IS NOT NULL)
  ),
  CONSTRAINT feed_wastage_completions_kg_range_check CHECK (wastage_kg IS NULL OR wastage_kg >= 0),
  CONSTRAINT feed_wastage_completions_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id),
  CONSTRAINT feed_wastage_completions_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id)
);

-- Natural key: ONE wastage completion per pen per feed day. A rework re-submit
-- updates this same row in place (status back to 'pending_verification',
-- row_version bumped), so the key stays single-valued across the whole
-- verify -> rework -> re-submit -> approve cycle.
CREATE UNIQUE INDEX IF NOT EXISTS feed_wastage_completions_natural_uq
  ON public.feed_wastage_completions (tenant_id, park_id, shed_id, partition_key, target_date, workflow);

-- Request-level idempotency: same client key never inserts twice.
CREATE UNIQUE INDEX IF NOT EXISTS feed_wastage_completions_idempotency_uq
  ON public.feed_wastage_completions (tenant_id, idempotency_key);

-- Serving read: the wastage worklist overlay batch-reads every pen's row for one
-- park-day in one indexed read, bounded by the park's pen catalog -- never by herd
-- size. Same shape as feed_packing_completions_serving_idx.
CREATE INDEX IF NOT EXISTS feed_wastage_completions_serving_idx
  ON public.feed_wastage_completions (tenant_id, park_id, target_date, workflow);

-- Add the feed_wastage_completion branch to the outbox tenant-parity trigger so the
-- feed.wastage.completed producer's outbox INSERT validates against THIS table.
-- Carries forward every branch present in 000098 (the latest replacement) and
-- appends feed_wastage_completion. Function replacement only -- no table lock.
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

-- +goose Down
-- Restore the trigger function to its 000098 form (drop the feed_wastage_completion
-- branch) and drop the table.
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

  IF NOT EXISTS (SELECT 1 FROM goat_identity_events WHERE tenant_id = NEW.tenant_id AND identity_event_id = NEW.event_id) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id USING ERRCODE = '23503';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TABLE IF EXISTS public.feed_wastage_completions;
