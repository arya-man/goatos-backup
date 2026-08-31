-- +goose Up
-- Health diagnosis engine: the advisory layer upstream of the treatment course.
--
-- Two maintainer decisions (2026-08-14) land here.
--
-- D. duration_days becomes NULLABLE and closure is driven by exit_type.
--    15 of the 34 register rules have no honest fixed length: 11 Type V close when
--    the Director looks, 2 Type T close on a test result (CMT negative twice,
--    FAMACHA 1-2), and 2 Supportive courses are daily and ongoing until
--    containment is cleared. The column was NOT NULL CHECK (1..90) and the
--    three-day default would have stamped a close date on a wound course that the
--    clinical contract explicitly refuses to state.
--
-- C. A case now pins TWO versions. health_protocol_version_id already pinned the
--    TREATMENT (what dose this animal receives). register_version on the
--    diagnosis run pins the DIAGNOSIS (which rules named the disease). They move
--    independently: a dosage correction must not re-date a diagnosis, and a rule
--    fix must not rewrite a course already being administered.
--
-- The engine itself is advisory. Nothing here closes a problem, culls an animal,
-- or moves it between sheds -- the housing directive is RECORDED so the Director
-- can act on it, and the policy-pack workflow that owns location stays the only
-- writer. That is why housing_* below are plain columns and not a foreign key
-- into any location table.

-- LOCK NOTE. The CHECK constraints below are added in-transaction and therefore
-- validate with a full scan under ACCESS EXCLUSIVE. That is acceptable here and
-- only here: health_cases is a young, low-volume table (Health opens one row per
-- diagnosed animal per episode, and the module is barely in service), so the
-- scan is sub-millisecond. `validate-migrations` does not list health_cases as a
-- hot table for the same reason. If Health reaches production volume, a later
-- constraint on this table needs ADD CONSTRAINT ... NOT VALID in a
-- no-transaction migration followed by a separately reviewed VALIDATE.

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. Closure model on the case
-- ---------------------------------------------------------------------------

ALTER TABLE public.health_cases
  ALTER COLUMN duration_days DROP NOT NULL;

ALTER TABLE public.health_cases
  ADD COLUMN IF NOT EXISTS exit_type text NOT NULL DEFAULT 'F'
    CHECK (exit_type IN ('F', 'T', 'V', 'Supportive'));

-- Fixed-length courses carry a day count; the other three do not, and must not
-- acquire one by default. Written as an equivalence so neither half can drift:
-- a Type F with no duration cannot close, and a Type V with one would close on a
-- calendar the Director never agreed to.
--
-- Every pre-existing row has a duration and defaults to 'F', so this holds on
-- the existing data without a backfill.
ALTER TABLE public.health_cases
  ADD CONSTRAINT health_cases_duration_matches_exit_type
    CHECK ((exit_type = 'F') = (duration_days IS NOT NULL));

COMMENT ON COLUMN public.health_cases.duration_days IS
  'Closure day count. NON-NULL only for exit_type=F. NULL for T (closes on a test), V (Director looks) and Supportive (daily, ongoing). This is NOT the protocol step count -- health_protocol_versions.duration_days still says how many days of steps were authored.';

COMMENT ON COLUMN public.health_cases.exit_type IS
  'How this course closes: F fixed days + criteria, T on a test result, V on Director review, Supportive daily until cleared. Snapshotted from the register rule at diagnosis.';

-- ---------------------------------------------------------------------------
-- 2. The diagnosis run
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.health_diagnosis_runs (
  health_diagnosis_run_id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  tenant_id uuid NOT NULL,
  goat_id uuid NOT NULL,

  -- The diagnosis pin (decision C). Every run records the rule table that
  -- produced it, so an old proposal stays interpretable after the register is
  -- edited.
  register_version text NOT NULL,

  observed_by uuid NOT NULL,
  observed_at timestamptz NOT NULL DEFAULT now(),
  business_date date NOT NULL,

  -- The observation exactly as submitted, and the engine's whole output. Stored
  -- verbatim because the override log is the improvement loop: when the Director
  -- overrides, the question is always "what did the form say and what did the
  -- rules make of it".
  form jsonb NOT NULL,
  proposal jsonb NOT NULL,

  valid boolean NOT NULL,
  reject_reason text,
  scope text NOT NULL,

  -- Housing DIRECTIVE. Recorded, never applied by Health.
  housing_acuity text,
  housing_containment text,
  housing_low_competition boolean NOT NULL DEFAULT false,

  status text NOT NULL DEFAULT 'proposed'
    CHECK (status IN ('proposed', 'confirmed', 'superseded')),
  confirmed_by uuid,
  confirmed_at timestamptz,
  confirmation_idempotency_key text,
  confirmation_fingerprint text,

  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  row_version integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT health_diagnosis_runs_goat_fk FOREIGN KEY (tenant_id, goat_id)
    REFERENCES public.goats (tenant_id, goat_id),
  UNIQUE (tenant_id, health_diagnosis_run_id),
  UNIQUE (tenant_id, idempotency_key),

  -- A confirmed run names who confirmed it and when; an unconfirmed one must not
  -- carry either. This is the advisory boundary expressed as a constraint.
  CONSTRAINT health_diagnosis_runs_confirmation_complete
    CHECK ((status = 'confirmed') = (
      confirmed_by IS NOT NULL
      AND confirmed_at IS NOT NULL
      AND confirmation_idempotency_key IS NOT NULL
      AND confirmation_fingerprint IS NOT NULL
    )),

  CONSTRAINT health_diagnosis_runs_confirmation_empty_until_confirmed
    CHECK (status = 'confirmed' OR (
      confirmed_by IS NULL
      AND confirmed_at IS NULL
      AND confirmation_idempotency_key IS NULL
      AND confirmation_fingerprint IS NULL
    )),

  -- An invalid form is not diagnosed at all, so it can never be confirmed.
  CONSTRAINT health_diagnosis_runs_invalid_is_terminal
    CHECK (valid OR status <> 'confirmed'),

  -- A rejected run names its reason; an accepted one has none to give.
  CONSTRAINT health_diagnosis_runs_reject_reason_matches_valid
    CHECK (valid = (reject_reason IS NULL))
);

-- The Director's queue: proposals awaiting confirmation, newest first.
CREATE INDEX IF NOT EXISTS health_diagnosis_runs_pending_idx
  ON public.health_diagnosis_runs (tenant_id, status, observed_at DESC, health_diagnosis_run_id)
  WHERE status = 'proposed';

-- One animal's diagnosis history.
CREATE INDEX IF NOT EXISTS health_diagnosis_runs_goat_idx
  ON public.health_diagnosis_runs (tenant_id, goat_id, observed_at DESC, health_diagnosis_run_id);

-- ---------------------------------------------------------------------------
-- 3. Case provenance
-- ---------------------------------------------------------------------------

-- Which observation produced this course. NULL for a case opened through the
-- pre-engine path, which stays supported: the direct disease-pick route is not
-- retired by this migration.
ALTER TABLE public.health_cases
  ADD COLUMN IF NOT EXISTS health_diagnosis_run_id uuid;

-- Which REGISTER RULE diagnosed this course, e.g. 'MASTITIS'.
--
-- This is not the same fact as disease_key, and reconcile needs exactly this
-- one. On the next observation the engine is handed the animal's open problems
-- as register ids so it can tell ongoing from new, propose a close, or spot a
-- relapse. disease_key cannot answer that: it names the treatment CARD, and the
-- mapping is many-to-one -- PPR, POX and UNDIFFERENTIATED all route to the
-- 'supportive' card, so a card key cannot say which of the three is open.
--
-- NULL for a pre-engine case, which simply does not participate in reconcile.
ALTER TABLE public.health_cases
  ADD COLUMN IF NOT EXISTS register_rule_id text;

COMMENT ON COLUMN public.health_cases.register_rule_id IS
  'The register rule that diagnosed this course (MASTITIS, PPR, ...). Fed back to the engine as an open problem on the next observation. NOT interchangeable with disease_key, which names the treatment card and is many-to-one.';

CREATE INDEX IF NOT EXISTS health_cases_open_register_rule_idx
  ON public.health_cases (tenant_id, goat_id, register_rule_id)
  WHERE register_rule_id IS NOT NULL AND status = 'active';

ALTER TABLE public.health_cases
  ADD CONSTRAINT health_cases_diagnosis_run_fk
    FOREIGN KEY (tenant_id, health_diagnosis_run_id)
    REFERENCES public.health_diagnosis_runs (tenant_id, health_diagnosis_run_id);

CREATE INDEX IF NOT EXISTS health_cases_diagnosis_run_idx
  ON public.health_cases (tenant_id, health_diagnosis_run_id)
  WHERE health_diagnosis_run_id IS NOT NULL;

-- A confirmed run opens at most one case per disease. Re-confirming is an
-- idempotent replay, not a second course on the same animal for the same
-- illness.
CREATE UNIQUE INDEX IF NOT EXISTS health_cases_run_disease_uq
  ON public.health_cases (tenant_id, health_diagnosis_run_id, disease_key)
  WHERE health_diagnosis_run_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 4. Let the outbox guard recognise a diagnosis run
-- ---------------------------------------------------------------------------
--
-- validate_outbox_event_tenant() refuses any outbox row whose aggregate does not
-- exist, per aggregate_type, and falls through to goat_identity_events for a type
-- it does not know. 'health_diagnosis_run' was unknown, so the observation event
-- was checked against the identity-event table and rejected.
--
-- The whole function is replaced rather than patched because plpgsql has no way
-- to extend one -- this is a verbatim copy of the LATEST definition (000211, toxin tasks) with the
-- health_diagnosis_run branch added beside the existing health ones. Widening the
-- guard is the fix; routing around it would let an outbox row point at nothing.

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

COMMIT;
