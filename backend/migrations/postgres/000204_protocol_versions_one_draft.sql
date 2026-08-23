-- +goose Up
-- +goose NO TRANSACTION
-- One draft at a time, enforced by the database.
--
-- A plan being worked on is a single thing: pressing "Start a new version" either creates
-- the draft or opens the one that exists. That rule was only checked in the server action,
-- which reads the current drafts and then creates one -- two tabs, two operators, or a
-- double-click can both pass the read before either writes, and both drafts are created.
-- The list screen renders the FIRST draft it finds, so the loser becomes work nobody can
-- reach or discard.
--
-- Scoped per (tenant, protocol, scope) because that is the grain a plan is authored at: a
-- tenant-default plan and a park override are different plans and may each have a draft.
-- scope_id is NULL for tenant scope, so NULLS NOT DISTINCT is required -- without it every
-- tenant-scoped draft would look distinct from every other and the index would enforce
-- nothing at all, which is the failure mode that matters here.
--
-- CONCURRENTLY: protocol_versions is small, but a plain unique-index build takes a SHARE
-- lock, and this table is read on the publish path that retires the previous version inside
-- a transaction. Blocking that for a build is avoidable, so it is avoided (same reasoning as
-- 000154 / 000170).
-- Two things have to happen before the build, or the migration reports success while
-- enforcing nothing.
--
-- First: a CONCURRENTLY build that fails leaves an INVALID index behind, and IF NOT EXISTS
-- then SKIPS it on the retry -- goose marks the migration applied, everyone believes the
-- invariant holds, and no insert is ever refused. Dropping an invalid leftover makes the
-- retry actually rebuild.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_class c
    JOIN pg_index i ON i.indexrelid = c.oid
    WHERE c.relname = 'protocol_versions_one_draft_per_scope_idx'
      AND NOT i.indisvalid
  ) THEN
    EXECUTE 'DROP INDEX protocol_versions_one_draft_per_scope_idx';
  END IF;
END $$;

-- Second: this index exists because duplicate drafts were really created, so scopes holding
-- two of them plausibly exist right now -- and a unique build over them simply fails.
--
-- That cleanup is NOT done here. Retiring the losers is a data change on rows the seed
-- itself authors, and a migration is the wrong place for it: it cannot be dry-run, cannot be
-- reviewed row by row, and cannot be re-run selectively. It lives in
-- `repair-obligation-duplicates -mode drafts`, which defaults to reporting and only mutates
-- with --apply.
--
-- DEPLOY ORDER, therefore:
--   1. repair-obligation-duplicates -mode drafts -apply   (per tenant, retires the losers)
--   2. this migration                                     (builds the index)
-- Run out of order the index build fails, loudly, and nothing is left half-applied.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS protocol_versions_one_draft_per_scope_idx
  ON protocol_versions (tenant_id, protocol_id, scope_type, scope_id)
  NULLS NOT DISTINCT
  WHERE status = 'draft';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS protocol_versions_one_draft_per_scope_idx;
