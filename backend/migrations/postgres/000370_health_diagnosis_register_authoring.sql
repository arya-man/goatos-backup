-- +goose Up
-- The diagnosis register becomes an AUTHORED document on /health/config.
-- Canonical contract: docs/decisions/health-diagnosis-register-authoring.md.
--
-- WHAT THIS REPLACES
-- ---------------------------------------------------------------------------
-- The rule table has always been data -- four committed YAML files, version-pinned,
-- loaded through a function that takes BYTES precisely so the source could move
-- (backend/internal/health/diagnosis/embed.go, which named itself the starting point
-- in 2026-08-14 decision E). What was NOT data was the FORM that feeds it: ~45
-- questions spread across a Go struct, an OpenAPI schema, a Kotlin DTO and a
-- thousand lines of Compose, plus a 150-line Go switch mapping each ticked answer to
-- a rule token. So adding a DISEASE over existing symptoms was a YAML edit, and
-- adding a SYMPTOM was a four-file change across two languages and a release.
--
-- A version here carries all three -- the questions, the mapping, the rules -- and
-- that is deliberate rather than convenient. A question whose answer emits a token no
-- rule reads is a question that does nothing; a rule naming a token no question emits
-- is a disease that can never be diagnosed. Those are exactly the two states two
-- independently versioned halves would let a farm publish, and the place they would
-- be discovered is in front of a sick animal. Publish validates both directions.
--
-- WHY IT MIRRORS health_protocol_versions RATHER THAN INVENTING A SHAPE
-- ---------------------------------------------------------------------------
-- The treatment protocols on this same screen already solved this problem in 000098
-- and 000121: version, status draft/published/retired, one published and one draft
-- per identity, a content hash so a re-save of identical content is recognised as a
-- no-op, and a write-log ledger because a publish spans two rows and a discard leaves
-- none. A diagnosis register is the same artifact with a different body, so it gets
-- the same shape -- and an author who has learned one editor has learned both.
--
-- The IDENTITY is the animal class, not a disease. One register serves each of the
-- four classes, and the loudest rule in the whole clinical spec is that a milk kid is
-- never diagnosed against the adult table.
--
-- THE BODY IS jsonb, ON PURPOSE. A register is published and read WHOLE -- the engine
-- loads one version and evaluates every rule in parallel against one evidence set --
-- and it is validated in Go, where the two-direction check lives, before it is
-- written. Normalising questions, options, bands, corrections, rules and clauses into
-- six child tables would buy a query nobody runs and cost the atomicity that makes
-- the form and the rules one version.

BEGIN;

CREATE TABLE IF NOT EXISTS public.health_diagnosis_register_versions (
  health_diagnosis_register_version_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     uuid NOT NULL REFERENCES public.tenants (tenant_id),

  -- The animal class this register serves: adult, kid_milk, kid_weaning,
  -- kid_fattening. Text rather than an enum for the same reason the class is text
  -- everywhere else in health: the slices are a clinical decision the farm may extend.
  animal_class  text NOT NULL,

  version       integer NOT NULL,
  status        text NOT NULL,

  -- The author's own label for this revision, carried INTO the document and pinned on
  -- every run that used it (health_diagnosis_runs.register_version). It is what makes
  -- an old proposal interpretable after the table is edited.
  register_label text NOT NULL,

  -- The whole authored document: questions, corrections, vocabulary, rules.
  document      jsonb NOT NULL,

  -- Hash of the document. A save whose content matches the open draft writes no new
  -- version and reports 'unchanged'; without this, every keystroke-save would churn
  -- out revisions a reviewer cannot tell apart.
  content_hash  text NOT NULL,

  created_by    uuid,
  updated_by    uuid,
  published_by  uuid,
  published_at  timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT health_diagnosis_register_versions_status_check
    CHECK (status IN ('draft','published','retired')),
  CONSTRAINT health_diagnosis_register_versions_version_check
    CHECK (version >= 1),
  CONSTRAINT health_diagnosis_register_versions_class_check
    CHECK (btrim(animal_class) <> ''),
  CONSTRAINT health_diagnosis_register_versions_label_check
    CHECK (btrim(register_label) <> ''),
  CONSTRAINT health_diagnosis_register_versions_hash_check
    CHECK (btrim(content_hash) <> ''),
  -- A published version must say who published it and when. A row claiming to be live
  -- with no signature is exactly the row an audit cannot answer for.
  CONSTRAINT health_diagnosis_register_versions_published_shape_check
    CHECK (status <> 'published' OR (published_by IS NOT NULL AND published_at IS NOT NULL)),
  CONSTRAINT health_diagnosis_register_versions_identity_uq
    UNIQUE (tenant_id, animal_class, version)
);

-- ONE LIVE REGISTER PER CLASS. Without this a second publish leaves two live tables
-- for the same animals and the engine's choice between them is whichever the query
-- happens to order first -- which is to say, a clinical decision made by a sort.
CREATE UNIQUE INDEX IF NOT EXISTS health_diagnosis_register_one_published_uq
  ON public.health_diagnosis_register_versions (tenant_id, animal_class)
  WHERE status = 'published';

-- ONE OPEN DRAFT PER CLASS. The draft twin, and it prevents the same lost-update this
-- index prevents for treatment protocols: two vets open the adult register, both edit,
-- both publish, and the second silently retires the first's version seconds after it
-- went live -- with no error and with a live rule table nobody reviewed.
CREATE UNIQUE INDEX IF NOT EXISTS health_diagnosis_register_one_draft_uq
  ON public.health_diagnosis_register_versions (tenant_id, animal_class)
  WHERE status = 'draft';

-- The version-history read on the editor: every revision of one class, newest first.
CREATE INDEX IF NOT EXISTS health_diagnosis_register_history_idx
  ON public.health_diagnosis_register_versions (tenant_id, animal_class, version DESC);

COMMENT ON TABLE public.health_diagnosis_register_versions IS
  'Authored diagnosis registers, one live per animal class. Each version carries the observation form, the mapping from each answer to a rule token, and the rules those tokens fire -- published together because a question no rule reads and a rule no question can fire are the two failures that versioning them apart would allow.';

-- ---------------------------------------------------------------------------
-- health_register_write_log -- idempotency + audit ledger
-- ---------------------------------------------------------------------------
-- A separate ledger from health_config_write_log rather than a widening of it. That
-- table's disease_key is NOT NULL and its CHECK refuses a blank, because every write
-- it records addresses one disease. A register write addresses an animal CLASS and no
-- disease at all, so reusing the table would mean writing a disease key that is not
-- true -- and a ledger that lies about what a write touched is worse than two ledgers.
CREATE TABLE IF NOT EXISTS public.health_register_write_log (
  health_register_write_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id             uuid NOT NULL REFERENCES public.tenants (tenant_id),
  write_kind            text NOT NULL,
  idempotency_key       text NOT NULL,
  request_fingerprint   text NOT NULL,
  outcome               text NOT NULL,
  animal_class          text NOT NULL,
  result_version_id     uuid,
  retired_version_id    uuid,
  actor_ref             text NOT NULL,
  created_at            timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT health_register_write_log_kind_check
    CHECK (write_kind IN ('draft_save','draft_publish','draft_discard')),
  CONSTRAINT health_register_write_log_outcome_check
    CHECK (outcome IN ('saved','unchanged','published','discarded')),
  CONSTRAINT health_register_write_log_idem_check
    CHECK (btrim(idempotency_key) <> '' AND btrim(request_fingerprint) <> ''),
  CONSTRAINT health_register_write_log_actor_check CHECK (btrim(actor_ref) <> ''),
  CONSTRAINT health_register_write_log_class_check CHECK (btrim(animal_class) <> ''),
  CONSTRAINT health_register_write_log_publish_shape_check
    CHECK (outcome <> 'published' OR result_version_id IS NOT NULL),
  CONSTRAINT health_register_write_log_retire_shape_check
    CHECK (retired_version_id IS NULL OR outcome = 'published'),
  CONSTRAINT health_register_write_log_saved_shape_check
    CHECK (outcome NOT IN ('saved','unchanged') OR result_version_id IS NOT NULL)
);

-- THE idempotency index. A concurrent duplicate is detected by its violation: the
-- second transaction's INSERT fails here, re-reads the committed entry and returns the
-- original result rather than applying the edit twice.
CREATE UNIQUE INDEX IF NOT EXISTS health_register_write_log_idempotency_uidx
  ON public.health_register_write_log (tenant_id, idempotency_key);

CREATE INDEX IF NOT EXISTS health_register_write_log_class_idx
  ON public.health_register_write_log (tenant_id, animal_class, created_at DESC);

COMMENT ON TABLE public.health_register_write_log IS
  'Idempotency + audit ledger for authored diagnosis-register edits. One entry per accepted write, written in the same transaction as its side effects. A ledger rather than columns on the version row because a publish spans two rows and a discard leaves none.';

COMMIT;

-- +goose Down
BEGIN;
DROP INDEX IF EXISTS public.health_register_write_log_class_idx;
DROP INDEX IF EXISTS public.health_register_write_log_idempotency_uidx;
DROP TABLE IF EXISTS public.health_register_write_log;
DROP INDEX IF EXISTS public.health_diagnosis_register_history_idx;
DROP INDEX IF EXISTS public.health_diagnosis_register_one_draft_uq;
DROP INDEX IF EXISTS public.health_diagnosis_register_one_published_uq;
DROP TABLE IF EXISTS public.health_diagnosis_register_versions;
COMMIT;
