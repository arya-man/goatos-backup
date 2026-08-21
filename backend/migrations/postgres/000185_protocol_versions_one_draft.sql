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
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS protocol_versions_one_draft_per_scope_idx
  ON protocol_versions (tenant_id, protocol_id, scope_type, scope_id)
  NULLS NOT DISTINCT
  WHERE status = 'draft';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS protocol_versions_one_draft_per_scope_idx;
