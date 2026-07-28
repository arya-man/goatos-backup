-- +goose Up
-- +goose NO TRANSACTION
-- Collapsed clean-slate baseline generated from migrations 000001..000046;
-- this file is the ordered forward-compatibility path for existing databases
-- that already applied an older 000001 before those deltas were folded in.
-- Restore the standalone overflow-policy rename that was folded into the
-- baseline but still needs a forward path for databases that already ran the
-- historical 000001 baseline.
ALTER TABLE public.vaccination_capacity_config
  DROP CONSTRAINT IF EXISTS vaccination_capacity_config_overflow_check;

UPDATE public.vaccination_capacity_config
SET overflow_policy = 'split_within_safe_window_last_safe_may_exceed_cap'
WHERE overflow_policy = 'split_within_safe_window_then_mark_needs_review';

ALTER TABLE public.vaccination_capacity_config
  ADD CONSTRAINT vaccination_capacity_config_overflow_check
  CHECK (overflow_policy = 'split_within_safe_window_last_safe_may_exceed_cap');

SET lock_timeout = '5s';

ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

ALTER TABLE public.notification_requests
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

ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;

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

ALTER TABLE public.verification_items ADD COLUMN IF NOT EXISTS subject_label text;
ALTER TABLE public.verification_items ADD COLUMN IF NOT EXISTS closed_by uuid;
ALTER TABLE public.verification_items ADD COLUMN IF NOT EXISTS closed_at timestamp with time zone;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'verification_items_subject_label_check') THEN
    ALTER TABLE public.verification_items
      ADD CONSTRAINT verification_items_subject_label_check
      CHECK (((subject_label IS NULL) OR (btrim(subject_label) <> ''::text)));
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'verification_items_closed_pair_check') THEN
    ALTER TABLE public.verification_items
      ADD CONSTRAINT verification_items_closed_pair_check
      CHECK (((closed_by IS NULL) = (closed_at IS NULL)));
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'verification_items_closed_approved_check') THEN
    ALTER TABLE public.verification_items
      ADD CONSTRAINT verification_items_closed_approved_check
      CHECK (((closed_at IS NULL) OR (status = 'approved'::text)));
  END IF;
END $$;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_verification_idempotency_idx_v2
  ON public.outbox_messages USING btree (tenant_id, idempotency_key)
  WHERE (event_type = ANY (ARRAY[
    'verification.item.pending'::text,
    'verification.verdict.approved'::text,
    'verification.verdict.rework'::text,
    'verification.item.closed'::text
  ]));

DROP INDEX CONCURRENTLY IF EXISTS public.outbox_messages_verification_idempotency_idx;

CREATE INDEX CONCURRENTLY IF NOT EXISTS verification_items_leadership_queue_idx
  ON public.verification_items USING btree (tenant_id, park_id, source_submission_id, captured_at, item_id)
  WHERE ((status = 'approved'::text) AND (closed_at IS NULL));

SET lock_timeout = '5s';

ALTER TABLE public.obligation_status_events
  DROP CONSTRAINT IF EXISTS obligation_status_events_type_check;

ALTER TABLE public.obligation_status_events
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

ALTER TABLE public.obligation_status_events
  VALIDATE CONSTRAINT obligation_status_events_type_check;

WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
),
rewritten AS (
  SELECT
    sv.sop_version_id,
    (
      jsonb_set(
        jsonb_set(
          (sv.form_dsl - 'goat_row_proof') || jsonb_build_object(
            'shed_video',
            jsonb_build_object(
              'subject_scope', 'shed',
              'capture_source', 'in_app_camera',
              'allowed_capture_sources', jsonb_build_array('in_app_camera', 'gallery_picker'),
              'minimum_clips', 1,
              'maximum_clips', 5
            )
          ),
          '{fields}',
          COALESCE((
            SELECT jsonb_agg(field ORDER BY ordinal)
            FROM (
              SELECT field, ordinal
              FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb))
                WITH ORDINALITY AS entries(field, ordinal)
              WHERE field ->> 'key' <> 'goat_row_proof'
                AND field ->> 'key' <> 'shed_video'
                AND field ->> 'key' <> 'goat_ids'
              UNION ALL
              SELECT jsonb_build_object(
                'key', 'goat_ids',
                'label', 'Goats vaccinated',
                'type', 'goat_scan',
                'required', true,
                'repeat', true,
                'description', 'Scan each goat RFID exactly when the vaccine is given. The scan timestamp is the vaccination timestamp.'
              ), 9998
              UNION ALL
              SELECT jsonb_build_object(
                'key', 'shed_video',
                'label', 'Shed proof videos',
                'type', 'video_proof',
                'required', true,
                'repeat', true,
                'proof_subject', 'shed',
                'help_text', 'Add 1 required shed-level video before submit; camera or gallery allowed, up to 5 videos.'
              ), 9999
            ) fields
          ), '[]'::jsonb),
          true
        ),
        '{rules}',
        COALESCE((sv.form_dsl -> 'rules'), '[]'::jsonb),
        true
      )
    ) AS form_dsl
  FROM public.sop_versions sv
  JOIN vaccination_sops ids ON ids.sop_version_id = sv.sop_version_id
)
UPDATE public.sop_versions sv
SET form_dsl = rewritten.form_dsl,
    proof_policy = jsonb_build_object(
      'types', jsonb_build_array('video'),
      'required', true,
      'proof_mode', 'shed_level_video',
      'subject_scope', 'shed',
      'expected_subjects', jsonb_build_array('shed'),
      'minimum_count', 1,
      'maximum_count', 5,
      'maximum_count_per_subject', 5,
      'capture_source', 'in_app_camera',
      'allowed_capture_sources', jsonb_build_array('in_app_camera', 'gallery_picker'),
      'verify_capability', 'proof.verify',
      'verify_before_apply', true,
      'retention_policy', 'operational_90d'
    ),
    compatibility = jsonb_set(
      COALESCE(sv.compatibility, '{}'::jsonb),
      '{supported_proof_actions}',
      jsonb_build_array('video.capture', 'video.pick'),
      true
    ),
    updated_at = now()
FROM rewritten
WHERE sv.sop_version_id = rewritten.sop_version_id;

-- Existing databases have already recorded 000001, so destination profile
-- snapshot columns must ship as a forward migration as well as in the folded
-- clean-slate baseline.
ALTER TABLE public.shifting_events
    ADD COLUMN IF NOT EXISTS destination_profile_id uuid,
    ADD COLUMN IF NOT EXISTS destination_profile_row_version integer,
    ADD COLUMN IF NOT EXISTS destination_stage text;

-- +goose Down
-- +goose NO TRANSACTION
SET lock_timeout = '5s';

ALTER TABLE public.shifting_events
    DROP COLUMN IF EXISTS destination_stage,
    DROP COLUMN IF EXISTS destination_profile_row_version,
    DROP COLUMN IF EXISTS destination_profile_id;

ALTER TABLE public.vaccination_capacity_config
  DROP CONSTRAINT IF EXISTS vaccination_capacity_config_overflow_check;

ALTER TABLE public.vaccination_capacity_config
  ADD CONSTRAINT vaccination_capacity_config_overflow_check
  CHECK (overflow_policy = 'split_within_safe_window_last_safe_may_exceed_cap');
