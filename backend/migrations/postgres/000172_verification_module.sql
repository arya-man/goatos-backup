-- +goose Up
-- Generic, module-agnostic Verification vertical (context/architecture/verification-module-design.md).
-- Producers (vaccination first, then feed/diagnosis/death/breeding) emit ONE verification_item per
-- captured proof; a Verifier (verification.review) approves/rejects with a mandatory reject reason;
-- an authority (Head/Director/CEO) later acts on the verdict via the existing
-- /admin/tasks/{id}/verify|rework surface. This table is brand new (not yet at scale), so its own
-- indexes are created in-transaction; the outbox idempotency index on the EXISTING hot outbox_messages
-- table is added separately with CREATE INDEX CONCURRENTLY (see the next migration).
CREATE TABLE verification_items (
  item_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  -- Classification: vertical/module/category drive the plug-and-play type registry
  -- (verification-module-design.md §2.3) — a new module needs only a registry row, no new columns.
  vertical text NOT NULL,
  module text NOT NULL,
  category text NOT NULL,
  -- Back-pointer to the producing module's own record (generic — verification knows nothing about
  -- vaccination/feed/etc. internals beyond this reference tuple).
  source_module text NOT NULL,
  source_task_id uuid NULL,
  source_submission_id uuid NULL,
  source_ref_type text NOT NULL,
  source_ref_id uuid NOT NULL,
  -- Media is stored as proof_artifact id references only; signed URLs are resolved at read time via
  -- the existing proof storage port (streamed, never proxied/duplicated here).
  media_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
  status text NOT NULL DEFAULT 'pending',
  verdict_reason text NULL,
  operator_id uuid NULL,
  shed_id uuid NULL,
  park_id uuid NULL,
  captured_at timestamptz NOT NULL,
  verified_by uuid NULL,
  verified_at timestamptz NULL,
  -- Stable per-producer-event key (e.g. "vaccination:completion:<id>") so a producer replay
  -- (retry/outbox redelivery) is a no-op instead of a duplicate queue row.
  idempotency_key text NOT NULL,
  row_version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT verification_items_status_check CHECK (status IN ('pending', 'approved', 'rejected')),
  CONSTRAINT verification_items_media_refs_array_check CHECK (jsonb_typeof(media_refs) = 'array'),
  -- Reject/rework verdicts REQUIRE a non-empty reason at the storage layer too (defense in depth
  -- behind the 422 app-layer gate).
  CONSTRAINT verification_items_reject_reason_check
    CHECK (status <> 'rejected' OR (verdict_reason IS NOT NULL AND btrim(verdict_reason) <> '')),
  CONSTRAINT verification_items_row_version_check CHECK (row_version >= 1),
  CONSTRAINT verification_items_idempotency_key_check CHECK (btrim(idempotency_key) <> ''),
  CONSTRAINT verification_items_tenant_idempotency_unique UNIQUE (tenant_id, idempotency_key)
);

-- Keyset queue read: tenant + status are always filtered (status defaults to 'pending' in the app
-- layer), category is the verifier's usual assignment filter but is optional at the API. Column
-- order puts the always-present predicates first so every queue read stays an index scan; FIFO by
-- captured_at then item_id (~20/page keyset per verifier-app-and-flow.md; never OFFSET, never a
-- full-table scan).
CREATE INDEX verification_items_queue_idx
  ON verification_items (tenant_id, status, category, captured_at, item_id);

-- Producer/back-reference lookup (idempotent re-emit guard + traceability from the source module).
CREATE INDEX verification_items_source_idx
  ON verification_items (tenant_id, source_module, source_ref_type, source_ref_id);

CREATE INDEX verification_items_source_submission_idx
  ON verification_items (tenant_id, source_submission_id)
  WHERE source_submission_id IS NOT NULL;

CREATE OR REPLACE FUNCTION verification_items_touch_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;

CREATE TRIGGER verification_items_touch_updated_at
BEFORE UPDATE ON verification_items
FOR EACH ROW
EXECUTE FUNCTION verification_items_touch_updated_at();

-- validate_outbox_event_tenant (outbox_messages_validate_event_tenant_trg, most recently redefined
-- in 000138_config_changed_domain_event_envelope.sql) whitelists known aggregate_type values; any
-- type NOT explicitly matched falls through to its default branch (a goat_identity_events lookup)
-- and is rejected. Add the 'verification_item' branch so this module's outbox inserts pass — the
-- FULL function body must be repeated (CREATE OR REPLACE), matching every prior migration that added
-- an aggregate_type (000106, 000120, 000126, 000138).
CREATE OR REPLACE FUNCTION validate_outbox_event_tenant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  config_family_key text;
BEGIN
  IF NEW.aggregate_type = 'verification_item' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM verification_items
      WHERE tenant_id = NEW.tenant_id
        AND item_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'verification item outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'count_base_anchor' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM count_base_anchors
      WHERE tenant_id = NEW.tenant_id
        AND base_count_anchor_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'count base anchor outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'shifting_event' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM shifting_events
      WHERE tenant_id = NEW.tenant_id
        AND shifting_event_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'shifting event outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'count_projection_exception' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM count_projection_exceptions
      WHERE tenant_id = NEW.tenant_id
        AND count_projection_exception_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'count projection exception outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'admin_ui_config_family' THEN
    config_family_key := COALESCE(NEW.payload->>'family_key', NEW.payload #>> '{payload,family_key}');
    IF NOT EXISTS (
      SELECT 1
      FROM admin_ui_config_family_revisions
      WHERE tenant_id = NEW.tenant_id
        AND family_key = config_family_key
    ) THEN
      RAISE EXCEPTION 'admin ui config family outbox aggregate % does not exist for tenant %', config_family_key, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'calendar_notification' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM notification_requests
      WHERE tenant_id = NEW.tenant_id
        AND notification_request_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'calendar notification outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'calendar_snooze' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM calendar_snoozes
      WHERE tenant_id = NEW.tenant_id
        AND snooze_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'calendar snooze outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'obligation_escalation' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM obligation_escalations
      WHERE tenant_id = NEW.tenant_id
        AND escalation_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'obligation escalation outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'obligation_instance' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM obligation_instances
      WHERE tenant_id = NEW.tenant_id
        AND obligation_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'obligation instance outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'protocol_version' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM protocol_versions
      WHERE tenant_id = NEW.tenant_id
        AND protocol_version_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'protocol version outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'correction_request' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM identity_correction_requests
      WHERE tenant_id = NEW.tenant_id
        AND correction_request_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'correction request outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM goat_identity_events
    WHERE tenant_id = NEW.tenant_id
      AND identity_event_id = NEW.event_id
  ) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  RETURN NEW;
END;
$$;

-- +goose Down
DROP TRIGGER IF EXISTS verification_items_touch_updated_at ON verification_items;
DROP FUNCTION IF EXISTS verification_items_touch_updated_at();
DROP TABLE IF EXISTS verification_items;
-- validate_outbox_event_tenant is intentionally NOT reverted to its pre-000172 body: goose Down here
-- only undoes this migration's own objects, matching how 000106/000120/000126/000138 each left the
-- trigger function at its latest (superset) definition rather than chaining per-migration reverts.
