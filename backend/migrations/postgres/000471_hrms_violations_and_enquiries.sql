-- +goose Up
-- seed-fixture-guard:ignore: HRMS violations + enquiries tables, the HRMS SOP seed document and
-- per-person access ticks; no source fixture schema, no vaccination/herd seed contract.
--
-- HRMS VIOLATIONS AND ENQUIRIES (maintainer decisions 2026-09-30).
--
--  * A VIOLATION is recorded against one person with a fine in rupees. It is FINAL when
--    recorded (maintainer answer 2026-09-30, superseding -- for hand-recorded violations only --
--    the notice/appeal path of docs/decisions/task-timing-alerting-violations-and-appeals.md; a
--    late-task breach still follows that path). A mistaken one is WITHDRAWN with a reason, never
--    deleted. The fine is recorded only: nothing deducts it from pay.
--  * An ENQUIRY is opened by an event on the farm. The first trigger is an animal's DEATH, once it
--    is approved (every death, not only non-ICU ones: the park head decides whether anyone is
--    penalised). The park head of that park fills the report (phone, phase 2) or HR on the web;
--    it names the people responsible and a violation type + fine for each, and submitting it
--    records those violations in the same transaction. Due 48 hours after it opens.
--  * EVERYTHING LISTED IS AUTHORED (maintainer instruction 2026-09-30: "every violation type,
--    everything is SOP driven; in future every list should be changeable"). The violation types,
--    their default fines, each enquiry's questions and its deadline are the published
--    `hrms.violations` SOP, edited by HR and the CEO on People / HRMS > HRMS SOP. Every violation
--    and every enquiry is stamped with the SOP version it was recorded / opened on.

CREATE TABLE IF NOT EXISTS public.workforce_enquiries (
  enquiry_id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id           uuid NOT NULL,
  -- The authored trigger that opened it ('animal_death'); one enquiry per trigger subject.
  trigger_key         text NOT NULL,
  subject_type        text NOT NULL,
  subject_id          uuid NOT NULL,
  park_id             uuid NOT NULL,
  -- Backend-composed at open time ("Death · 1234567 · Castro 1"), so the list never re-derives it.
  subject_label       text NOT NULL,
  occurred_at         timestamptz NOT NULL,
  opened_at           timestamptz NOT NULL DEFAULT now(),
  due_at              timestamptz NOT NULL,
  sop_version         integer NOT NULL,
  status              text NOT NULL DEFAULT 'open',
  answers             jsonb NOT NULL DEFAULT '{}'::jsonb,
  submitted_by        uuid,
  submitted_at        timestamptz,
  row_version         integer NOT NULL DEFAULT 1,
  CONSTRAINT workforce_enquiries_status_check CHECK (status IN ('open', 'submitted')),
  CONSTRAINT workforce_enquiries_subject_type_check CHECK (subject_type IN ('goat')),
  CONSTRAINT workforce_enquiries_submitted_check
    CHECK (status = 'open' OR (submitted_by IS NOT NULL AND submitted_at IS NOT NULL)),
  CONSTRAINT workforce_enquiries_park_fk FOREIGN KEY (park_id) REFERENCES public.locations (location_id),
  CONSTRAINT workforce_enquiries_row_version_check CHECK (row_version >= 1)
);

-- The natural key: one death opens ONE enquiry, however often the event is replayed.
CREATE UNIQUE INDEX IF NOT EXISTS workforce_enquiries_natural_uq
  ON public.workforce_enquiries (tenant_id, trigger_key, subject_type, subject_id);
-- The list: one tenant, optionally one park, open first then newest.
CREATE INDEX IF NOT EXISTS workforce_enquiries_list_idx
  ON public.workforce_enquiries (tenant_id, status, opened_at DESC, enquiry_id DESC);
CREATE INDEX IF NOT EXISTS workforce_enquiries_park_idx
  ON public.workforce_enquiries (tenant_id, park_id, status, opened_at DESC);

COMMENT ON TABLE public.workforce_enquiries IS
  'HRMS enquiries opened by farm events (first: an approved animal death). Filled by the park head (phone) or HR (web); submitting records violations. 2026-09-30.';

CREATE TABLE IF NOT EXISTS public.workforce_violations (
  violation_id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id            uuid NOT NULL,
  workforce_member_id  uuid NOT NULL,
  -- The person's home park when it was recorded (the page filters by it).
  park_id              uuid,
  -- The authored type's stable key AND its label as it read then: a later rename or retire on the
  -- SOP never rewrites what a person was fined for.
  type_key             text NOT NULL,
  type_label           text NOT NULL,
  fine_rupees          integer NOT NULL,
  occurred_on          date NOT NULL,
  note                 text NOT NULL DEFAULT '',
  source               text NOT NULL,
  enquiry_id           uuid,
  sop_version          integer NOT NULL,
  recorded_by          uuid NOT NULL,
  recorded_at          timestamptz NOT NULL DEFAULT now(),
  status               text NOT NULL DEFAULT 'recorded',
  withdrawn_by         uuid,
  withdrawn_at         timestamptz,
  withdraw_reason      text,
  idempotency_key      text,
  request_fingerprint  text,
  row_version          integer NOT NULL DEFAULT 1,
  CONSTRAINT workforce_violations_member_fk
    FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members (workforce_member_id),
  CONSTRAINT workforce_violations_enquiry_fk
    FOREIGN KEY (enquiry_id) REFERENCES public.workforce_enquiries (enquiry_id),
  CONSTRAINT workforce_violations_fine_check CHECK (fine_rupees >= 0 AND fine_rupees <= 10000000),
  CONSTRAINT workforce_violations_source_check CHECK (source IN ('manual', 'enquiry')),
  CONSTRAINT workforce_violations_source_enquiry_check
    CHECK ((source = 'enquiry') = (enquiry_id IS NOT NULL)),
  CONSTRAINT workforce_violations_status_check CHECK (status IN ('recorded', 'withdrawn')),
  CONSTRAINT workforce_violations_withdrawn_check
    CHECK (status = 'recorded' OR (withdrawn_by IS NOT NULL AND withdrawn_at IS NOT NULL AND length(btrim(withdraw_reason)) > 0)),
  CONSTRAINT workforce_violations_note_check CHECK (length(note) <= 2000),
  CONSTRAINT workforce_violations_row_version_check CHECK (row_version >= 1)
);

-- A hand-recorded violation's replay key (the enquiry path is keyed by the enquiry itself).
CREATE UNIQUE INDEX IF NOT EXISTS workforce_violations_idempotency_uq
  ON public.workforce_violations (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
-- The page: one tenant, a month of occurred_on, optionally one park.
CREATE INDEX IF NOT EXISTS workforce_violations_list_idx
  ON public.workforce_violations (tenant_id, occurred_on DESC, violation_id DESC);
CREATE INDEX IF NOT EXISTS workforce_violations_park_idx
  ON public.workforce_violations (tenant_id, park_id, occurred_on DESC);
CREATE INDEX IF NOT EXISTS workforce_violations_member_idx
  ON public.workforce_violations (tenant_id, workforce_member_id, occurred_on DESC);
CREATE INDEX IF NOT EXISTS workforce_violations_enquiry_idx
  ON public.workforce_violations (enquiry_id) WHERE enquiry_id IS NOT NULL;

COMMENT ON TABLE public.workforce_violations IS
  'HRMS violations: one person, one authored type, a fine in rupees. Final when recorded; withdrawn with a reason, never deleted. Never deducted from pay. 2026-09-30.';

-- THE HRMS SOP (the 000376 shape). Violation types start EMPTY: the maintainer has not named any
-- and a fine is not ours to invent -- HR adds them on the page. The death enquiry is seeded with
-- the maintainer's stated 48 hours and one compulsory question.
-- seed-migration-guard:ignore owner=manohark issue=hrms-violations reason=hrms-sop-seed-document-no-fixture-carries-it expiry=2026-12-31
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'hrms.violations', 'Violations and enquiries',
       'The violation types and their fines, and the enquiries farm events open: who fills them, their questions and deadline.',
       'active', 'action', 'module', 'hrms'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

-- seed-migration-guard:ignore owner=manohark issue=hrms-violations reason=hrms-sop-seed-document-no-fixture-carries-it expiry=2026-12-31
INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Violations and enquiries v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'hrms.violations',
         'title', 'Violations and enquiries',
         'fields', jsonb_build_array(),
         'violations', $seed${
  "schema_version": "goatos.sop-hrms-violations.v1",
  "violation_types": [],
  "enquiries": [
    {"trigger": "animal_death", "title": "Death enquiry", "due_hours": 48, "questions": [
      {"id": "what_happened", "kind": "text", "title": "What happened", "required": true}
    ]}
  ]
}$seed$::jsonb
       ),
       '{"subject_scope": "task", "types": ["photo", "video"], "required": false, "minimum_count": 0, "verify_before_apply": false, "approval_before_apply": false}'::jsonb,
       '{"min_app_version": "0.2.0", "supported_field_types": ["boolean", "select", "multiselect", "number", "text"], "supported_proof_actions": [], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded HRMS violations and enquiries (migration 000471)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'hrms.violations'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- PER-PERSON ACCESS (the 000470 shape): HR and the CEO get the web-only `violations` module at
-- configure, ledgered so Down removes exactly these rows.
CREATE TABLE IF NOT EXISTS public.person_module_access_violations_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'web', 'violations', ARRAY['view', 'configure']::text[], now(), '{}'::text[]
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role IN ('hr', 'ceo_internal')
  WHERE m.status = 'active'
    AND m.user_id IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM public.person_access pa
      WHERE pa.tenant_id = m.tenant_id
        AND pa.workforce_member_id = m.workforce_member_id
    )
  ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING
  RETURNING tenant_id, workforce_member_id
)
INSERT INTO public.person_module_access_violations_backfill (tenant_id, workforce_member_id)
SELECT tenant_id, workforce_member_id FROM inserted
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS public.designation_module_defaults_violations_backfill (
  designation_code text PRIMARY KEY
);

WITH inserted AS (
  INSERT INTO public.designation_module_defaults (designation_code, surface, module_key, capabilities, pages)
  SELECT d.designation_code, 'web', 'violations', ARRAY['view', 'configure']::text[], '{}'::text[]
  FROM public.designation_catalog d
  WHERE d.designation_code = 'hr'
  ON CONFLICT (designation_code, surface, module_key) DO NOTHING
  RETURNING designation_code
)
INSERT INTO public.designation_module_defaults_violations_backfill (designation_code)
SELECT designation_code FROM inserted
ON CONFLICT DO NOTHING;

-- PARK HEADS FILL ENQUIRIES ON THE PHONE (same decision): the mobile `enquiries` module at do,
-- for every park head already migrated, and as the park-head job default. Ledgered likewise.
CREATE TABLE IF NOT EXISTS public.person_module_access_enquiries_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'mobile', 'enquiries', ARRAY['do']::text[], now(), '{}'::text[]
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role = 'park_head'
  WHERE m.status = 'active'
    AND m.user_id IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM public.person_access pa
      WHERE pa.tenant_id = m.tenant_id
        AND pa.workforce_member_id = m.workforce_member_id
    )
  ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING
  RETURNING tenant_id, workforce_member_id
)
INSERT INTO public.person_module_access_enquiries_backfill (tenant_id, workforce_member_id)
SELECT tenant_id, workforce_member_id FROM inserted
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS public.designation_module_defaults_enquiries_backfill (
  designation_code text PRIMARY KEY
);

WITH inserted AS (
  INSERT INTO public.designation_module_defaults (designation_code, surface, module_key, capabilities, pages)
  SELECT d.designation_code, 'mobile', 'enquiries', ARRAY['do']::text[], '{}'::text[]
  FROM public.designation_catalog d
  WHERE d.designation_code = 'park_head'
  ON CONFLICT (designation_code, surface, module_key) DO NOTHING
  RETURNING designation_code
)
INSERT INTO public.designation_module_defaults_enquiries_backfill (designation_code)
SELECT designation_code FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM public.designation_module_defaults d
USING public.designation_module_defaults_enquiries_backfill b
WHERE d.designation_code = b.designation_code AND d.surface = 'mobile' AND d.module_key = 'enquiries';
DROP TABLE IF EXISTS public.designation_module_defaults_enquiries_backfill;

DELETE FROM public.person_module_access p
USING public.person_module_access_enquiries_backfill b
WHERE p.tenant_id = b.tenant_id AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = 'mobile' AND p.module_key = 'enquiries';
DROP TABLE IF EXISTS public.person_module_access_enquiries_backfill;

DELETE FROM public.designation_module_defaults d
USING public.designation_module_defaults_violations_backfill b
WHERE d.designation_code = b.designation_code AND d.surface = 'web' AND d.module_key = 'violations';
DROP TABLE IF EXISTS public.designation_module_defaults_violations_backfill;

DELETE FROM public.person_module_access p
USING public.person_module_access_violations_backfill b
WHERE p.tenant_id = b.tenant_id AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = 'web' AND p.module_key = 'violations';
DROP TABLE IF EXISTS public.person_module_access_violations_backfill;

-- seed-migration-guard:ignore owner=manohark issue=hrms-violations reason=hrms-sop-seed-document-no-fixture-carries-it expiry=2026-12-31
DELETE FROM public.sop_versions sv USING public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'hrms.violations';
-- seed-migration-guard:ignore owner=manohark issue=hrms-violations reason=hrms-sop-seed-document-no-fixture-carries-it expiry=2026-12-31
DELETE FROM public.sop_definitions WHERE code = 'hrms.violations';

DROP TABLE IF EXISTS public.workforce_violations;
DROP TABLE IF EXISTS public.workforce_enquiries;
