-- +goose Up
-- +goose NO TRANSACTION
-- seed-migration-guard:ignore owner=ravi issue=R50-015 reason=lock-safety-refactor-of-existing-CHECK-constraints-no-new-seed-data-or-read-model expiry=2026-10-31
-- R50-015 P0 fix: this migration was previously wrapped in goose's default single
-- transaction. Even though the CHECK-constraint deltas below already used the
-- DROP CONSTRAINT + ADD CONSTRAINT ... NOT VALID + VALIDATE CONSTRAINT pattern,
-- wrapping all three steps in ONE transaction defeats the purpose of NOT VALID:
-- Postgres locks are held at their strongest level for the lifetime of the
-- transaction, not the statement. DROP CONSTRAINT acquires ACCESS EXCLUSIVE on the
-- hot table; inside a single transaction that ACCESS EXCLUSIVE lock is still held
-- (it never downgrades) while VALIDATE CONSTRAINT performs its full-table scan,
-- so concurrent inserts/updates on notification_requests / obligation_status_events
-- are blocked for the entire scan duration -- exactly the outage NOT VALID is
-- supposed to avoid. `-- +goose NO TRANSACTION` makes goose apply every statement
-- below in its own autocommit transaction, so each ACCESS EXCLUSIVE catalog-only
-- lock (DROP CONSTRAINT, ADD CONSTRAINT ... NOT VALID, CREATE TABLE, ADD COLUMN)
-- is acquired and released in milliseconds, and each VALIDATE CONSTRAINT then only
-- ever needs to hold the much weaker SHARE UPDATE EXCLUSIVE lock (which permits
-- concurrent reads AND writes, only blocking other DDL) for the duration of its
-- scan. Every statement in this file is already idempotent/re-runnable (IF EXISTS /
-- IF NOT EXISTS / DO $$ existence checks), which is required for NO TRANSACTION
-- migrations: a failure partway through does not roll back earlier statements, so a
-- retry must be a safe no-op for everything that already applied.
-- Forward-compatibility catch-up for dev/stg databases that already applied the ORIGINAL
-- 000001 clean-slate baseline before it was edited in place by later commits. Every
-- statement below is idempotent/re-runnable: a database that already has a delta (via the
-- edited baseline, via a clean install, or via a prior partial run of this migration) is
-- left unchanged, and one that is missing a delta is caught up to match the current
-- baseline shape exactly. Does NOT include the vaccination_capacity_config overflow_policy
-- rename -- that delta is already covered by 000002_vaccination_capacity_overflow_policy.sql.

-- 1) notification_requests.notification_type: allow the verification lifecycle values that
--    the edited baseline added ('verification_approved', 'verification_closed').
--    Lock-safe on hot table: NOT VALID + concurrent VALIDATE for hot tables (notification_requests).
--    Bounded lock_timeout so a lock-contended DROP/ADD/VALIDATE fails fast (statement error,
--    retried on the next migration run) instead of hanging indefinitely behind a long-lived
--    reader/writer transaction.
SET lock_timeout = '5s';

ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK ((notification_type = ANY (ARRAY[
    'reminder'::text,
    'nudge'::text,
    'escalation'::text,
    'verification_pending'::text,
    'verification_approved'::text,
    'verification_closed'::text,
    'rework'::text,
    'advance_notice'::text,
    'due_today'::text
  ]))) NOT VALID;

ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;

-- 2) SOP RFID scan-capture tables (goat-scan proof capture + attempt audit trail). These are
--    wholly new tables added by the edited baseline; an old-baseline database has neither.
CREATE TABLE IF NOT EXISTS public.sop_task_scan_captures (
    capture_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL,
    field_key text NOT NULL,
    tag text NOT NULL,
    normalized_tag text NOT NULL,
    goat_id uuid,
    obligation_id uuid,
    captured_by uuid NOT NULL,
    idempotency_key text NOT NULL,
    captured_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT sop_task_scan_captures_pkey PRIMARY KEY (capture_id),
    CONSTRAINT sop_task_scan_captures_field_key_check CHECK ((btrim(field_key) <> ''::text)),
    CONSTRAINT sop_task_scan_captures_idempotency_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT sop_task_scan_captures_normalized_tag_check CHECK ((btrim(normalized_tag) <> ''::text)),
    CONSTRAINT sop_task_scan_captures_tag_check CHECK ((btrim(tag) <> ''::text))
);

CREATE TABLE IF NOT EXISTS public.sop_task_scan_attempts (
    attempt_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL,
    field_key text NOT NULL,
    tag text NOT NULL,
    normalized_tag text NOT NULL,
    goat_id uuid,
    obligation_id uuid,
    outcome text NOT NULL,
    tag_role text DEFAULT 'unknown'::text NOT NULL,
    reason text,
    captured_by uuid NOT NULL,
    idempotency_key text NOT NULL,
    captured_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT sop_task_scan_attempts_pkey PRIMARY KEY (attempt_id),
    CONSTRAINT sop_task_scan_attempts_field_key_check CHECK ((btrim(field_key) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_idempotency_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_normalized_tag_check CHECK ((btrim(normalized_tag) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_outcome_check CHECK ((outcome = ANY (ARRAY['accepted'::text, 'duplicate'::text, 'not_due'::text, 'unknown'::text]))),
    CONSTRAINT sop_task_scan_attempts_tag_check CHECK ((btrim(tag) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_tag_role_check CHECK ((tag_role = ANY (ARRAY['primary'::text, 'secondary'::text, 'unknown'::text])))
);

CREATE UNIQUE INDEX IF NOT EXISTS sop_task_scan_captures_idempotency_unique_idx ON public.sop_task_scan_captures USING btree (tenant_id, idempotency_key);
CREATE UNIQUE INDEX IF NOT EXISTS sop_task_scan_captures_task_field_tag_unique_idx ON public.sop_task_scan_captures USING btree (tenant_id, task_id, field_key, normalized_tag);
CREATE INDEX IF NOT EXISTS sop_task_scan_captures_task_idx ON public.sop_task_scan_captures USING btree (tenant_id, task_id, captured_at, capture_id);

CREATE UNIQUE INDEX IF NOT EXISTS sop_task_scan_attempts_idempotency_unique_idx ON public.sop_task_scan_attempts USING btree (tenant_id, idempotency_key);
CREATE INDEX IF NOT EXISTS sop_task_scan_attempts_task_goat_idx ON public.sop_task_scan_attempts USING btree (tenant_id, task_id, goat_id, captured_at, attempt_id);
CREATE INDEX IF NOT EXISTS sop_task_scan_attempts_task_idx ON public.sop_task_scan_attempts USING btree (tenant_id, task_id, captured_at, attempt_id);

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_captures_goat_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_captures
      ADD CONSTRAINT sop_task_scan_captures_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_captures_task_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_captures
      ADD CONSTRAINT sop_task_scan_captures_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_captures_tenant_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_captures
      ADD CONSTRAINT sop_task_scan_captures_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_attempts_goat_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_attempts
      ADD CONSTRAINT sop_task_scan_attempts_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_attempts_task_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_attempts
      ADD CONSTRAINT sop_task_scan_attempts_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_attempts_tenant_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_attempts
      ADD CONSTRAINT sop_task_scan_attempts_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);
  END IF;
END $$;

-- 3) vaccination_capacity_config.overflow_policy rename: already covered by
--    000002_vaccination_capacity_overflow_policy.sql. Intentionally skipped here.

-- 4) verification_items: subject_label / closed_by / closed_at columns plus their CHECKs,
--    added by the edited baseline for the verification-closure workflow.
ALTER TABLE verification_items ADD COLUMN IF NOT EXISTS subject_label text;
ALTER TABLE verification_items ADD COLUMN IF NOT EXISTS closed_by uuid;
ALTER TABLE verification_items ADD COLUMN IF NOT EXISTS closed_at timestamp with time zone;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'verification_items_subject_label_check') THEN
    ALTER TABLE verification_items
      ADD CONSTRAINT verification_items_subject_label_check
      CHECK (((subject_label IS NULL) OR (btrim(subject_label) <> ''::text)));
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'verification_items_closed_pair_check') THEN
    ALTER TABLE verification_items
      ADD CONSTRAINT verification_items_closed_pair_check
      CHECK (((closed_by IS NULL) = (closed_at IS NULL)));
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'verification_items_closed_approved_check') THEN
    ALTER TABLE verification_items
      ADD CONSTRAINT verification_items_closed_approved_check
      CHECK (((closed_at IS NULL) OR (status = 'approved'::text)));
  END IF;
END $$;

-- 5) outbox_messages verification idempotency index: add 'verification.item.closed' to the
--    partial predicate so closure events get outbox idempotency coverage.
--    Handled concurrently in 000004_r50_forward_compat_concurrent_indexes.sql for hot-table safety.

-- 6) verification_items leadership closure queue: approved-and-not-yet-closed items, scoped
--    for the leadership review queue.
--    Handled concurrently in 000004_r50_forward_compat_concurrent_indexes.sql for large-table safety.

-- 7) Vaccination goat-scan SOP: MOVED TO 000006_r50_vaccination_sop_dml.sql for lock safety.
--    This step (step 7) is heavy DML that rewrites form_dsl/proof_policy for vaccination.drive /
--    vaccination.session SOP versions. Keeping it in this transaction with fast DDL would hold
--    ACCESS EXCLUSIVE locks on hot tables for the duration of the long UPDATE. Moved to a
--    separate migration to release DDL locks quickly (R50-015 P0).

-- 8) obligation_status_events.event_type: allow 'in_progress' so MarkInProgress (PEND-1: the
--    obligation_instances/obligation_batches 'in_progress' writer that was missing entirely,
--    which left MarkMissedBefore's in_progress-batch safety guard permanently dead) can record a
--    durable per-obligation status event when SOP-submit-time capture begins, mirroring
--    MarkCompleted's existing event pattern.
--    Lock-safe on hot table: NOT VALID + concurrent VALIDATE for hot tables (obligation_status_events).
--    Bounded lock_timeout (see notification_requests comment above for rationale).
SET lock_timeout = '5s';

ALTER TABLE obligation_status_events
  DROP CONSTRAINT IF EXISTS obligation_status_events_type_check;

ALTER TABLE obligation_status_events
  ADD CONSTRAINT obligation_status_events_type_check
  CHECK ((event_type = ANY (ARRAY[
    'scheduled'::text,
    'became_due'::text,
    'dispatched'::text,
    'in_progress'::text,
    'completed'::text,
    'missed'::text,
    'waived'::text,
    'escalated'::text,
    'escalation_acknowledged'::text,
    'escalation_resolved'::text,
    'canceled'::text,
    'deferred'::text,
    'rescoped'::text
  ]))) NOT VALID;

ALTER TABLE obligation_status_events
  VALIDATE CONSTRAINT obligation_status_events_type_check;

-- +goose Down
-- +goose NO TRANSACTION
-- Same R50-015 P0 lock-safety rationale as the Up section above: run every
-- statement autocommit so DROP/ADD CONSTRAINT + VALIDATE CONSTRAINT never share a
-- transaction and never accumulate an ACCESS EXCLUSIVE hold across a table scan.
-- Reverses the deltas above in reverse order. Steps 6/5/2/1 are structural and fully
-- reversible. Step 4 drops columns/checks added above -- lossy if a downstream release
-- wrote real subject_label/closed_by/closed_at data after this migration ran (matches how
-- 000002's Down accepts lossy data reversion for its overflow_policy rewrite). Step 7's DML
-- revert is BEST-EFFORT/LOSSY: it restores an approximate pre-goat-scan form_dsl/proof_policy
-- shape for the vaccination.drive / vaccination.session SOP versions rather than replaying the
-- exact historical batch-level JSON, consistent with how 000002's Down does a best-effort data
-- UPDATE rather than a byte-exact restore.

-- 8) Restore obligation_status_events_type_check without 'in_progress'.
--    Lock-safe on hot table: NOT VALID + concurrent VALIDATE for hot tables (obligation_status_events).
--    Bounded lock_timeout (see notification_requests comment in goose Up for rationale).
SET lock_timeout = '5s';

ALTER TABLE obligation_status_events
  DROP CONSTRAINT IF EXISTS obligation_status_events_type_check;

ALTER TABLE obligation_status_events
  ADD CONSTRAINT obligation_status_events_type_check
  CHECK ((event_type = ANY (ARRAY[
    'scheduled'::text,
    'became_due'::text,
    'dispatched'::text,
    'completed'::text,
    'missed'::text,
    'waived'::text,
    'escalated'::text,
    'escalation_acknowledged'::text,
    'escalation_resolved'::text,
    'canceled'::text,
    'deferred'::text,
    'rescoped'::text
  ]))) NOT VALID;

ALTER TABLE obligation_status_events
  VALIDATE CONSTRAINT obligation_status_events_type_check;

-- 7) Best-effort revert of the goat-scan SOP rewrite: MOVED TO 000006_r50_vaccination_sop_dml.sql.
--    The revert logic is in that separate migration's Down section.

-- 6) Drop the leadership closure queue index.
--    Handled concurrently in 000004_r50_forward_compat_concurrent_indexes.sql for large-table safety.

-- 5) Restore the outbox idempotency index predicate without the closure event type.
--    Handled concurrently in 000004_r50_forward_compat_concurrent_indexes.sql for hot-table safety.

-- 4) Drop the verification_items closure columns/checks (lossy: see header comment).
ALTER TABLE verification_items DROP CONSTRAINT IF EXISTS verification_items_closed_approved_check;
ALTER TABLE verification_items DROP CONSTRAINT IF EXISTS verification_items_closed_pair_check;
ALTER TABLE verification_items DROP CONSTRAINT IF EXISTS verification_items_subject_label_check;
ALTER TABLE verification_items DROP COLUMN IF EXISTS closed_at;
ALTER TABLE verification_items DROP COLUMN IF EXISTS closed_by;
ALTER TABLE verification_items DROP COLUMN IF EXISTS subject_label;

-- 3) (skipped in Up; nothing to revert.)

-- 2) Drop the SOP scan-capture tables (drops their indexes/FKs/checks with them).
DROP TABLE IF EXISTS public.sop_task_scan_attempts;
DROP TABLE IF EXISTS public.sop_task_scan_captures;

-- 1) Restore the notification_requests_type_check without the verification lifecycle values.
--    Lock-safe on hot table: NOT VALID + concurrent VALIDATE for hot tables (notification_requests).
--    Bounded lock_timeout (see notification_requests comment in goose Up for rationale).
SET lock_timeout = '5s';

ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK ((notification_type = ANY (ARRAY[
    'reminder'::text,
    'nudge'::text,
    'escalation'::text,
    'verification_pending'::text,
    'rework'::text,
    'advance_notice'::text,
    'due_today'::text
  ]))) NOT VALID;

ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
