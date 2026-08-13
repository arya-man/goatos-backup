-- +goose Up
-- Feed distribution verification gate (maintainer decision, 2026-07-26). It mirrors the shifting
-- verification gate (000031) but for feed DISTRIBUTION, and it writes to a BRAND-NEW table so feed
-- PACKING's completion path (feed_direction_session_completions, migration 000030) is left EXACTLY as
-- today: instant, optional-video, status='completed', no verifier.
--
-- New flow (feed DISTRIBUTION only):
--   generated session --operator completes with MANDATORY feed-distribution VIDEO + water proof-->
--     PENDING VERIFICATION (status='pending_verification'; NOTHING is completed yet)
--   pending_verification --verifier approve--> COMPLETED (the session is done NOW; feed.distribution.completed)
--   pending_verification --verifier reject--> REWORK (operator re-shoots + re-submits)
--
-- feed_distribution_completions records that ONE shed's ONE feeding session on ONE feed day passed
-- through the DISTRIBUTION verification gate. The grain is the operator's unit of work -- a
-- shed-session -- exactly (tenant, park, shed, session_no, target_date, workflow), matching the
-- packing table's grain but held in a SEPARATE record so packing is never dragged into verification.
--
-- This is the canonical producer of the feed.distribution.completed domain event (see
-- context/architecture/domain-event-registry.json). The event is emitted ONLY at verifier approval
-- (ApplyVerifiedDistribution), never at operator completion.
CREATE TABLE IF NOT EXISTS public.feed_distribution_completions (
  completion_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  session_no integer NOT NULL,
  target_date date NOT NULL,
  workflow text NOT NULL,
  -- status is the two-phase, verifier-gated fact. An operator completion writes 'pending_verification';
  -- verifier approval flips it to 'completed'; verifier rejection flips it to 'rework'. Unlike packing
  -- (feed_direction_session_completions.status is always 'completed'), distribution is NOT done until a
  -- verifier approves the video.
  status text DEFAULT 'pending_verification' NOT NULL,
  -- distribution_proof_ref is the MANDATORY feed-distribution VIDEO proof_id (server-minted, from
  -- /app/proofs/*). water_proof_ref is the MANDATORY water-distribution proof_id (photo OR video). The
  -- bytes live in GCS; only the references are stored here. Both are required for any row that a
  -- verifier could act on -- see the proof CHECK below.
  distribution_proof_ref text,
  water_proof_ref text,
  -- verified_by / verified_at are stamped when a verifier approves the pair (status -> 'completed').
  verified_by uuid,
  verified_at timestamp with time zone,
  -- rework_reason is the verifier's rejection reason (status -> 'rework').
  rework_reason text,
  -- completed_by is the operator principal when the caller carries one. Nullable because local bearer
  -- sessions may not resolve a principal uuid; the audit_log row is the authoritative actor trail.
  completed_by uuid,
  -- idempotency_key is the client-supplied request key. The request-level guard reserves it in the
  -- same transaction as the write (idempotency_keys), so an exact replay returns the original result
  -- and reruns no side effects.
  idempotency_key text NOT NULL,
  row_version integer DEFAULT 1 NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT feed_distribution_completions_pkey PRIMARY KEY (completion_id),
  CONSTRAINT feed_distribution_completions_workflow_check CHECK (workflow IN ('normal', 'experiment')),
  CONSTRAINT feed_distribution_completions_status_check CHECK (status IN ('pending_verification', 'completed', 'rework')),
  CONSTRAINT feed_distribution_completions_session_no_check CHECK (session_no >= 1),
  -- A row awaiting verification OR already completed MUST carry BOTH proofs: the mandatory
  -- feed-distribution video and the mandatory water proof. A 'rework' row may have either cleared for a
  -- re-shoot, so the proof requirement is scoped to the two live states.
  CONSTRAINT feed_distribution_completions_proof_check CHECK (
    (status NOT IN ('pending_verification', 'completed'))
    OR (
      distribution_proof_ref IS NOT NULL AND btrim(distribution_proof_ref) <> ''
      AND water_proof_ref IS NOT NULL AND btrim(water_proof_ref) <> ''
    )
  ),
  CONSTRAINT feed_distribution_completions_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id),
  CONSTRAINT feed_distribution_completions_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id)
);

-- Natural key: a shed-session on a feed day for a workflow passes through the distribution gate at
-- most once. A re-submission after a rework updates this same row in place (status back to
-- 'pending_verification', row_version bumped), so the key stays single-valued across the whole
-- verify -> rework -> re-submit -> approve cycle.
CREATE UNIQUE INDEX IF NOT EXISTS feed_distribution_completions_natural_uq
  ON public.feed_distribution_completions (tenant_id, park_id, shed_id, session_no, target_date, workflow);

-- Request-level idempotency: same client key never inserts twice.
CREATE UNIQUE INDEX IF NOT EXISTS feed_distribution_completions_idempotency_uq
  ON public.feed_distribution_completions (tenant_id, idempotency_key);

-- Serving read: the direction overlay batch-reads every VERIFIED (status='completed') (shed, session)
-- for one park-day. One indexed read per request, bounded by the park's shed catalog x sessions --
-- never by herd size. Covers the (tenant, park, target_date) prefix and the workflow narrowing.
CREATE INDEX IF NOT EXISTS feed_distribution_completions_serving_idx
  ON public.feed_distribution_completions (tenant_id, park_id, target_date, workflow);

-- The outbox tenant-parity trigger (validate_outbox_event_tenant) dispatches by aggregate_type and
-- validates each outbox row against its aggregate's canonical table; an unknown type falls through to
-- a goat_identity_events check that a feed distribution completion has no row in. Add the
-- feed_distribution_completion branch so the feed.distribution.completed producer's outbox INSERT
-- validates against THIS table (same pattern as feed_direction_session_completion, shifting_event,
-- etc.). This is a function replacement only -- no table lock.
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

  IF NEW.aggregate_type = 'feed_distribution_completion' THEN
    IF NOT EXISTS (SELECT 1 FROM feed_distribution_completions WHERE tenant_id = NEW.tenant_id AND completion_id = NEW.aggregate_id) THEN
      RAISE EXCEPTION 'feed distribution completion outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id USING ERRCODE = '23503';
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
-- Restore the trigger function to its 000030 form (drop the feed_distribution_completion branch) and
-- drop the table. feed_direction_session_completions (packing) is untouched.
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

DROP TABLE IF EXISTS public.feed_distribution_completions;
