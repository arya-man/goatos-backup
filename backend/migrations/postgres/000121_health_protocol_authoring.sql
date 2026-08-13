-- +goose Up
-- Health Config authoring (/health/config -> /health-config/*).
-- Canonical contract: docs/decisions/health-config-authoring.md.
--
-- WHAT CHANGES, AND WHAT DELIBERATELY DOES NOT
-- ---------------------------------------------------------------------------
-- Health treatment protocols were until now IMPORT-ONLY: ReplacePublishedProtocols read the
-- maintainer's Google Sheet, retired every published row and republished all 54 as version 1.
-- The maintainer decision of 2026-08-06 moves authorship to the web (`/health/config`), so the
-- sheet becomes a one-time bootstrap and the screen becomes the source of truth.
--
-- Almost nothing about the SHAPE of a protocol needs to change to allow that, because 000098
-- already built the right model: `version`, `status IN ('draft','published','retired')`, one
-- published row per (tenant, disease, age band), and `health_cases.health_protocol_version_id`
-- pinning the exact version a goat was diagnosed under. An edit is therefore a NEW VERSION, and
-- a goat mid-treatment finishes on the version it started on. That property is what makes an
-- authored dosage change safe, and it is already enforced by the 000098 foreign key.
--
-- What was missing is only what AUTHORING needs on top of that model:
--
--   1. At most ONE open draft per (tenant, disease, age band). Without it, two authors editing
--      the same protocol each create a draft, both publish, and the second silently retires the
--      first author's just-published version. `health_protocol_versions_one_published_uq` already
--      says a disease has one published version; this is its draft twin.
--
--   2. An idempotency + audit ledger for authored edits (`health_config_write_log`), for exactly
--      the reason feed_config_write_log exists: a publish CLOSES one row (retire) and OPENS
--      another (publish), so the write's identity spans two rows and belongs to neither, and a
--      'unchanged' outcome writes no version row at all yet must still replay identically.
--
--   3. `created_by` / `updated_by` on the version. 000098 recorded `published_by` only, which
--      cannot answer "who drafted this dosage" while the draft is still unpublished -- and an
--      unpublished draft is precisely the state in which a wrong dosage is easiest to catch.
--
-- NOT changed here, on purpose:
--
--   * `health_protocol_steps` keeps `UNIQUE (health_protocol_version_id, seq)`. Reordering steps
--     through in-place seq UPDATEs would transiently collide on that constraint; the adapter
--     instead DELETEs the draft's steps and reinserts the whole ordered list, assigning seq
--     1..N server-side. The client sends order, never seq. Keeping the constraint means a
--     half-applied reorder cannot commit.
--
--   * `content_hash` stays NOT NULL and keeps its column name, but its MEANING is narrowed for
--     rows written by the web: the importer stores one hash for the whole sheet snapshot (all 54
--     rows share it, which is why it cannot distinguish two versions of one disease), whereas an
--     authored version stores a hash of THAT protocol's own content. Both are legitimate values
--     of "what content produced this row"; only the authored one is per-protocol, and only rows
--     the web wrote are compared by it.

ALTER TABLE public.health_protocol_versions
  ADD COLUMN IF NOT EXISTS created_by uuid,
  ADD COLUMN IF NOT EXISTS updated_by uuid;

COMMENT ON COLUMN public.health_protocol_versions.created_by IS
  'Actor who created this version row -- for a draft, the author who opened it. Distinct from published_by, which stays NULL until the draft is published and therefore cannot attribute an unpublished dosage change.';
COMMENT ON COLUMN public.health_protocol_versions.updated_by IS
  'Actor who last saved this version. Only meaningful while status = draft: a published version is immutable and a retired one is history.';
COMMENT ON COLUMN public.health_protocol_versions.content_hash IS
  'Hash of the content that produced this row. Rows written by the SHEET IMPORTER carry one hash for the entire import snapshot (every protocol in that import shares it). Rows written by /health-config/* carry a hash of that single protocol -- display name, duration and ordered steps -- so an authored save can tell "same content, no new version" from a real edit.';

-- At most one open draft per protocol identity. The published twin of
-- health_protocol_versions_one_published_uq (000098).
--
-- Concurrency this actually prevents: two authors open the same disease, both edit, both publish.
-- Without this index each gets their own draft version and the second publish retires the first
-- author's version seconds after it went live -- with no error, and with a live protocol nobody
-- reviewed. With it, the second author's draft creation fails and the UI can tell them a draft is
-- already open.
CREATE UNIQUE INDEX IF NOT EXISTS health_protocol_versions_one_draft_uq
  ON public.health_protocol_versions (tenant_id, disease_key, age_band)
  WHERE status = 'draft';

-- The draft worklist read: "which protocols have an open draft", scoped to a tenant and ordered
-- the way the screen lists them. Narrow partial index rather than a general status index because
-- drafts are a handful of rows against a table that accumulates every retired version forever.
CREATE INDEX IF NOT EXISTS health_protocol_versions_draft_catalog_idx
  ON public.health_protocol_versions (tenant_id, age_band, display_name, health_protocol_version_id)
  WHERE status = 'draft';

-- The version-history read on the detail screen: every version of one disease/age band, newest
-- first. Without it this is a scan of the whole versions table filtered by disease.
CREATE INDEX IF NOT EXISTS health_protocol_versions_history_idx
  ON public.health_protocol_versions (tenant_id, disease_key, age_band, version DESC);

-- ---------------------------------------------------------------------------
-- health_config_write_log -- idempotency + audit ledger for authored edits
-- ---------------------------------------------------------------------------
-- Same rationale as feed_config_write_log, for the same reason: AGENTS.md makes idempotency a
-- mandatory write-path contract, and an authored protocol edit cannot carry its key on "the row
-- it wrote" because it does not write exactly one row.
--
--   save_draft   -- upserts the draft version and REPLACES its whole step list (N deletes + M
--                   inserts). The key belongs to the save, not to any one step.
--   publish      -- flips the draft to published AND retires the previously published version:
--                   two rows, neither of which owns the write.
--   discard      -- deletes the draft and its steps: no surviving row to carry a key at all.
--   create       -- opens drafts for BOTH age bands of a new disease in one transaction, so the
--                   write spans two version rows by construction.
--
-- The ledger row is written INSIDE the same transaction as the side effects. An entry that exists
-- is proof the edit committed; there is no best-effort-afterwards path.
CREATE TABLE IF NOT EXISTS public.health_config_write_log (
  health_config_write_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id              uuid NOT NULL REFERENCES public.tenants (tenant_id),
  -- Which authored act this was. Audit detail, NOT part of the idempotency identity: uniqueness
  -- is (tenant, key) alone, so one client key can never mean two different writes.
  write_kind             text NOT NULL,
  idempotency_key        text NOT NULL,
  request_fingerprint    text NOT NULL,
  -- created    -- a disease that did not exist now has drafts.
  -- saved      -- the draft's content changed.
  -- unchanged  -- the submitted content already matched the draft; nothing was written.
  -- published  -- a draft became the live protocol (and the prior live version was retired).
  -- discarded  -- a draft was deleted without publishing.
  outcome                text NOT NULL,
  disease_key            text NOT NULL,
  -- NULL for a create, which addresses two age bands at once.
  age_band               text,
  -- The version row this write left in place: the saved draft, or the newly published version.
  -- NULL for 'discarded' (the row is gone) and for a 'created' pair addressed below.
  result_version_id      uuid,
  -- The version this write RETIRED, for 'published'. NULL otherwise -- this is what makes the
  -- "what was the live dosage before this edit" question answerable from the ledger alone.
  retired_version_id     uuid,
  actor_ref              text NOT NULL,
  created_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT health_config_write_log_kind_check
    CHECK (write_kind IN ('disease_create','draft_save','draft_publish','draft_discard')),
  CONSTRAINT health_config_write_log_outcome_check
    CHECK (outcome IN ('created','saved','unchanged','published','discarded')),
  CONSTRAINT health_config_write_log_age_band_check
    CHECK (age_band IS NULL OR age_band IN ('adult','kid')),
  CONSTRAINT health_config_write_log_idem_check
    CHECK (btrim(idempotency_key) <> '' AND btrim(request_fingerprint) <> ''),
  CONSTRAINT health_config_write_log_actor_check CHECK (btrim(actor_ref) <> ''),
  CONSTRAINT health_config_write_log_disease_check CHECK (btrim(disease_key) <> ''),
  -- A publish must name BOTH ends of the swap it performed, EXCEPT the very first publish of a
  -- disease, which retires nothing. So: a publish always has a result; only the retired side is
  -- optional, and only then.
  CONSTRAINT health_config_write_log_publish_shape_check
    CHECK (outcome <> 'published' OR result_version_id IS NOT NULL),
  -- A retired version can only be named by a publish; any other outcome claiming one is a
  -- malformed ledger entry rather than a harmless extra field.
  CONSTRAINT health_config_write_log_retire_shape_check
    CHECK (retired_version_id IS NULL OR outcome = 'published'),
  CONSTRAINT health_config_write_log_saved_shape_check
    CHECK (outcome NOT IN ('saved','unchanged') OR result_version_id IS NOT NULL)
);

-- THE idempotency index. Its violation is how a concurrent duplicate is detected: the second
-- transaction's INSERT fails here, re-reads the committed entry, and returns the original result
-- instead of applying the edit twice.
CREATE UNIQUE INDEX IF NOT EXISTS health_config_write_log_idempotency_uidx
  ON public.health_config_write_log (tenant_id, idempotency_key);

-- The audit read: "every authored change to this disease, newest first".
CREATE INDEX IF NOT EXISTS health_config_write_log_disease_idx
  ON public.health_config_write_log (tenant_id, disease_key, created_at DESC);

COMMENT ON TABLE public.health_config_write_log IS
  'Idempotency + audit ledger for authored health-protocol edits made through /health-config/*. One entry per accepted write, written in the same transaction as its side effects. Exists as a ledger rather than as idempotency columns on health_protocol_versions because a publish spans two version rows and a discard leaves none.';

-- +goose Down
DROP INDEX IF EXISTS public.health_config_write_log_disease_idx;
DROP INDEX IF EXISTS public.health_config_write_log_idempotency_uidx;
DROP TABLE IF EXISTS public.health_config_write_log;
DROP INDEX IF EXISTS public.health_protocol_versions_history_idx;
DROP INDEX IF EXISTS public.health_protocol_versions_draft_catalog_idx;
DROP INDEX IF EXISTS public.health_protocol_versions_one_draft_uq;
ALTER TABLE public.health_protocol_versions
  DROP COLUMN IF EXISTS updated_by,
  DROP COLUMN IF EXISTS created_by;
