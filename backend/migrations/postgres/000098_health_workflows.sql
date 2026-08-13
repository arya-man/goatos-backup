-- +goose Up
-- Health module: disease-versioned treatment courses and per-day/session mobile work.
-- Canonical contract: docs/decisions/health-workflows.md.

CREATE TABLE public.health_protocol_versions (
  health_protocol_version_id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  disease_key text NOT NULL CHECK (disease_key ~ '^[a-z][a-z0-9_]*$'),
  display_name text NOT NULL CHECK (length(btrim(display_name)) > 0),
  age_band text NOT NULL CHECK (age_band IN ('adult','kid')),
  version integer NOT NULL CHECK (version >= 1),
  duration_days integer NOT NULL DEFAULT 3 CHECK (duration_days BETWEEN 1 AND 90),
  status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','retired')),
  source_ref text,
  content_hash text NOT NULL,
  published_at timestamptz,
  published_by uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, health_protocol_version_id),
  UNIQUE (tenant_id, disease_key, age_band, version)
);
CREATE UNIQUE INDEX health_protocol_versions_one_published_uq
  ON public.health_protocol_versions (tenant_id, disease_key, age_band)
  WHERE status = 'published';
CREATE INDEX health_protocol_versions_catalog_idx
  ON public.health_protocol_versions (tenant_id, age_band, display_name, health_protocol_version_id)
  WHERE status = 'published';

CREATE TABLE public.health_protocol_steps (
  health_protocol_step_id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  tenant_id uuid NOT NULL,
  health_protocol_version_id uuid NOT NULL,
  day_no integer NOT NULL CHECK (day_no BETWEEN 1 AND 90),
  session text NOT NULL CHECK (session IN ('morning','afternoon','evening','unscheduled')),
  seq integer NOT NULL CHECK (seq >= 1),
  record_type text NOT NULL CHECK (record_type IN ('action','medication','critical_action')),
  medicine_name text,
  dosage_text text,
  dosage_denominator text,
  medicine_route text,
  instruction text,
  critical_action_type text CHECK (critical_action_type IS NULL OR critical_action_type IN ('lifecycle_exit','quarantine_or_movement')),
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT health_protocol_steps_protocol_fk FOREIGN KEY (tenant_id, health_protocol_version_id)
    REFERENCES public.health_protocol_versions(tenant_id, health_protocol_version_id) ON DELETE CASCADE,
  CONSTRAINT health_protocol_steps_content_check CHECK (
    (record_type = 'medication' AND length(btrim(coalesce(medicine_name,''))) > 0)
    OR (record_type <> 'medication' AND length(btrim(coalesce(instruction,''))) > 0)
  ),
  CONSTRAINT health_protocol_steps_critical_check CHECK (
    (record_type = 'critical_action') = (critical_action_type IS NOT NULL)
  ),
  UNIQUE (health_protocol_version_id, seq)
);
CREATE INDEX health_protocol_steps_order_idx
  ON public.health_protocol_steps (health_protocol_version_id, day_no, session, seq);

CREATE TABLE public.health_cases (
  health_case_id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  tenant_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  health_protocol_version_id uuid NOT NULL,
  disease_key text NOT NULL,
  disease_name text NOT NULL,
  age_band text NOT NULL CHECK (age_band IN ('adult','kid')),
  start_date date NOT NULL,
  duration_days integer NOT NULL CHECK (duration_days BETWEEN 1 AND 90),
  status text NOT NULL DEFAULT 'active'
    CHECK (status IN ('active','recovered','continued','referred','held_death_review','closed_dead','canceled')),
  status_before_death_hold text,
  park_id uuid,
  shed_id uuid,
  diagnosed_by uuid,
  diagnosed_at timestamptz NOT NULL DEFAULT now(),
  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  row_version integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT health_cases_goat_fk FOREIGN KEY (tenant_id, goat_id)
    REFERENCES public.goats(tenant_id, goat_id),
  CONSTRAINT health_cases_protocol_fk FOREIGN KEY (tenant_id, health_protocol_version_id)
    REFERENCES public.health_protocol_versions(tenant_id, health_protocol_version_id),
  UNIQUE (tenant_id, health_case_id),
  UNIQUE (tenant_id, idempotency_key)
);
CREATE INDEX health_cases_goat_open_idx
  ON public.health_cases (tenant_id, goat_id, status, start_date DESC, health_case_id);

CREATE TABLE public.health_treatment_sessions (
  health_session_id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  tenant_id uuid NOT NULL,
  health_case_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  day_no integer NOT NULL CHECK (day_no BETWEEN 1 AND 90),
  business_date date NOT NULL,
  session text NOT NULL CHECK (session IN ('morning','afternoon','evening','unscheduled')),
  due_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'scheduled'
    CHECK (status IN ('scheduled','due','in_progress','completed','rework','held_death_review','canceled_death')),
  status_before_death_hold text,
  completed_by uuid,
  completed_at timestamptz,
  proof_ref text,
  completion_idempotency_key text,
  completion_fingerprint text,
  row_version integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT health_sessions_case_fk FOREIGN KEY (tenant_id, health_case_id)
    REFERENCES public.health_cases(tenant_id, health_case_id) ON DELETE CASCADE,
  CONSTRAINT health_sessions_goat_fk FOREIGN KEY (tenant_id, goat_id)
    REFERENCES public.goats(tenant_id, goat_id),
  UNIQUE (tenant_id, health_session_id),
  UNIQUE (health_case_id, day_no, session)
);
-- projection-review:
-- producer unique columns: health_case_id + day_no + session; consumer match/group:
-- tenant_id + business_date + age_band from health_cases + due_at + health_session_id.
-- joined sides: session -> case is N:1; case -> goat is N:1; no many side is aggregated here.
-- summary numerator/denominator keys are both health_session_id over the identical filtered set.
CREATE INDEX health_sessions_worklist_idx
  ON public.health_treatment_sessions (tenant_id, business_date, due_at, health_session_id);
CREATE INDEX health_sessions_goat_open_idx
  ON public.health_treatment_sessions (tenant_id, goat_id, status, business_date, health_session_id);
CREATE UNIQUE INDEX health_sessions_completion_idempotency_uq
  ON public.health_treatment_sessions (tenant_id, completion_idempotency_key)
  WHERE completion_idempotency_key IS NOT NULL;

CREATE TABLE public.health_session_steps (
  health_session_step_id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  tenant_id uuid NOT NULL,
  health_session_id uuid NOT NULL,
  source_protocol_step_id uuid,
  seq integer NOT NULL CHECK (seq >= 1),
  record_type text NOT NULL CHECK (record_type IN ('action','medication','critical_action')),
  medicine_name text,
  dosage_text text,
  dosage_denominator text,
  medicine_route text,
  instruction text,
  critical_action_type text,
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','completed','guarded')),
  completed_at timestamptz,
  CONSTRAINT health_session_steps_session_fk FOREIGN KEY (tenant_id, health_session_id)
    REFERENCES public.health_treatment_sessions(tenant_id, health_session_id) ON DELETE CASCADE,
  UNIQUE (health_session_id, seq)
);
CREATE INDEX health_session_steps_order_idx
  ON public.health_session_steps (health_session_id, seq);

CREATE TABLE public.health_medicine_administrations (
  health_administration_id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  tenant_id uuid NOT NULL,
  health_case_id uuid NOT NULL,
  health_session_id uuid NOT NULL,
  health_session_step_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  disease_key text NOT NULL,
  medicine_name text NOT NULL,
  dosage_text text,
  dosage_denominator text,
  medicine_route text,
  administered_by uuid,
  administered_at timestamptz NOT NULL,
  proof_ref text,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT health_administrations_case_fk FOREIGN KEY (tenant_id, health_case_id)
    REFERENCES public.health_cases(tenant_id, health_case_id),
  CONSTRAINT health_administrations_session_fk FOREIGN KEY (tenant_id, health_session_id)
    REFERENCES public.health_treatment_sessions(tenant_id, health_session_id),
  CONSTRAINT health_administrations_goat_fk FOREIGN KEY (tenant_id, goat_id)
    REFERENCES public.goats(tenant_id, goat_id),
  UNIQUE (health_session_id, health_session_step_id)
);
CREATE INDEX health_administrations_goat_timeline_idx
  ON public.health_medicine_administrations (tenant_id, goat_id, administered_at DESC, health_administration_id);

-- Health belongs to the same departments already granted preventive-care Vaccination.
INSERT INTO public.department_module_grants (tenant_id, department_id, module_key, status)
SELECT tenant_id, department_id, 'aas_health', status
FROM public.department_module_grants
WHERE module_key = 'vaccination'
ON CONFLICT (tenant_id, department_id, module_key)
DO UPDATE SET status = EXCLUDED.status, updated_at = now();

-- Extend the outbox tenant validator for the Health aggregate. This is the latest full validator
-- body plus one explicit health_case branch; falling through to goat identity would reject valid
-- Health events or tempt a cross-module identity-event write.
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

-- +goose Down
-- Forward-only operational module. Dropping medicine and treatment history is unsafe.
SELECT 1;
