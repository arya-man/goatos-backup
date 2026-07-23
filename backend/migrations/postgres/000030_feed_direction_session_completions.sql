-- +goose Up
-- feed_direction_session_completions records that ONE shed's ONE feeding session on ONE feed day was
-- carried out by an operator ("feed direction completed"). It is the first write path the
-- feed-direction module owns: until now the module was a pure read-only generator, and completion
-- state was derived, never stored. The grain is the operator's unit of work -- a shed-session --
-- exactly (tenant, park, shed, session_no, target_date, workflow), NOT the ration grain: one shed's
-- session is done or it is not, regardless of how many ration grains it printed.
--
-- This is a canonical producer of the feed.direction.completed domain event (see
-- context/architecture/domain-event-registry.json). It is deliberately NOT feed_direction_completions
-- (that table is the obligation-keyed SM-5 verify path in backend/internal/feed and requires an
-- obligation_id the generator has none of).
CREATE TABLE IF NOT EXISTS public.feed_direction_session_completions (
  completion_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  session_no integer NOT NULL,
  target_date date NOT NULL,
  workflow text NOT NULL,
  status text DEFAULT 'completed' NOT NULL,
  -- proof_refs is the OPTIONAL video proof, same JSONB shape used by sop_submissions/procurement:
  -- an array of proof references, each carrying a server-minted proof_id from /app/proofs/*. Empty
  -- array = completed with no video (the "video optional" contract). The bytes live in GCS; only the
  -- reference is stored here.
  proof_refs jsonb DEFAULT '[]'::jsonb NOT NULL,
  -- completed_by is the operator principal when the caller carries one. Nullable because local bearer
  -- sessions may not resolve a principal uuid; the audit_log row is the authoritative actor trail.
  completed_by uuid,
  completed_at timestamp with time zone DEFAULT now() NOT NULL,
  -- idempotency_key is the client-supplied request key. The request-level guard reserves it in the
  -- same transaction as the write (idempotency_keys), so an exact replay returns the original result
  -- and reruns no side effects.
  idempotency_key text NOT NULL,
  row_version integer DEFAULT 1 NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT feed_direction_session_completions_pkey PRIMARY KEY (completion_id),
  CONSTRAINT feed_direction_session_completions_workflow_check CHECK (workflow IN ('normal', 'experiment')),
  CONSTRAINT feed_direction_session_completions_status_check CHECK (status IN ('completed')),
  CONSTRAINT feed_direction_session_completions_session_no_check CHECK (session_no >= 1),
  CONSTRAINT feed_direction_session_completions_proof_array_check CHECK (jsonb_typeof(proof_refs) = 'array'),
  CONSTRAINT feed_direction_session_completions_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id),
  CONSTRAINT feed_direction_session_completions_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id)
);

-- Natural key: a shed-session on a feed day for a workflow is completed at most once. A second
-- completion (any client key) conflicts here and is served as a no-op return of the existing row, so
-- the flip to "completed" is itself idempotent independent of the request idempotency ledger.
CREATE UNIQUE INDEX IF NOT EXISTS feed_direction_session_completions_natural_uq
  ON public.feed_direction_session_completions (tenant_id, park_id, shed_id, session_no, target_date, workflow);

-- Request-level idempotency: same client key never inserts twice.
CREATE UNIQUE INDEX IF NOT EXISTS feed_direction_session_completions_idempotency_uq
  ON public.feed_direction_session_completions (tenant_id, idempotency_key);

-- Serving read: the generator's serve path batch-reads every completed (shed, session) for one
-- park-day so it can flip row status to "completed". One indexed read per request, bounded by the
-- park's shed catalog x sessions -- never by herd size. Covers the (tenant, park, target_date)
-- prefix and the workflow narrowing.
CREATE INDEX IF NOT EXISTS feed_direction_session_completions_serving_idx
  ON public.feed_direction_session_completions (tenant_id, park_id, target_date, workflow);

-- The outbox tenant-parity trigger (validate_outbox_event_tenant) dispatches by aggregate_type and
-- validates each outbox row against its aggregate's canonical table; an unknown type falls through
-- to a goat_identity_events check that a feed completion has no row in. Add the
-- feed_direction_session_completion branch so the feed.direction.completed producer's outbox INSERT
-- validates against THIS table (same pattern as shifting_event, obligation_instance, etc.). This is
-- a function replacement only -- no table lock.
CREATE OR REPLACE FUNCTION public.validate_outbox_event_tenant() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  config_family_key text;
BEGIN
  IF NEW.aggregate_type = 'verification_item' THEN
    IF NOT EXISTS (SELECT 1 FROM verification_items WHERE tenant_id = NEW.tenant_id AND item_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'verification item outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
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

  IF NEW.aggregate_type = 'feed_direction_session_completion' THEN
    IF NOT EXISTS (SELECT 1 FROM feed_direction_session_completions WHERE tenant_id = NEW.tenant_id AND completion_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'feed direction completion outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  IF NOT EXISTS (SELECT 1 FROM goat_identity_events WHERE tenant_id = NEW.tenant_id AND identity_event_id = NEW.event_id) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id USING ERRCODE = '23503';
  END IF;

  RETURN NEW;
END;
$$;

-- +goose Down
-- Restore the trigger function to its pre-feed-completion form (drop the feed_direction_session_completion branch).
CREATE OR REPLACE FUNCTION public.validate_outbox_event_tenant() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  config_family_key text;
BEGIN
  IF NEW.aggregate_type = 'verification_item' THEN
    IF NOT EXISTS (SELECT 1 FROM verification_items WHERE tenant_id = NEW.tenant_id AND item_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'verification item outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
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

  IF NOT EXISTS (SELECT 1 FROM goat_identity_events WHERE tenant_id = NEW.tenant_id AND identity_event_id = NEW.event_id) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id USING ERRCODE = '23503';
  END IF;

  RETURN NEW;
END;
$$;

DROP TABLE IF EXISTS public.feed_direction_session_completions;
