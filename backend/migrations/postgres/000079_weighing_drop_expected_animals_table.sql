-- +goose Up
-- +goose NO TRANSACTION
-- WEIGHING IS FREE-FLOW, PERIOD (maintainer mandate, 2026-08-03). Free-flow has
-- no expected set by definition: a weighing scan dumps whatever RFID the reader
-- gives, with no herd read, no clinical/health state, no roster, no "X of Y",
-- no wrong-shed, no missing-animal, no availability. See AGENTS.md:40,
-- SKILLS.md:169, migration 000059 ("nothing here references goats, herd
-- rosters, or vaccination"), and
-- context/repo-audits/weighing-implementation-do-not-reopen-ledger.md:117-125.
--
-- weighing_expected_animals (000006_weighing_backend_slice.sql) was the SECOND
-- expected-set model that kept reintroducing herd coupling: CreateCampaign /
-- UpdateCampaign populated it from `goats` + herd_register_is_kid(), the
-- planner read kid counts from it, ListScopeRoster joined it to `goats` +
-- goat_identifiers to show the operator a roster, and a dead RefreshAvailability
-- function wrote goats.health_status/lifecycle_status into it as
-- 'icu'/'quarantine'/'dead'/'moved_other_shed'. Two prior fixes (2d7d722d1,
-- 36b209d3b) only GATED that population behind a "campaign source is not
-- free-flow" condition instead of deleting it -- which is why the coupling
-- kept coming back. This migration removes the table itself so the second
-- model cannot be selected again.
--
-- All Go callers were removed in the same change that ships this migration:
--   - CreateCampaign / UpdateCampaign no longer populate this table at all
--     (backend/internal/weighing/adapters/postgres/repository.go).
--   - RefreshAvailability and completeResolvedIndividualScopes were deleted
--     outright (dead code -- neither had a production caller).
--   - ListScopeRoster no longer reads a per-animal roster; it serves the
--     bucket's scan/observation history only.
--   - PlannerCatalog / PlannerParkBuckets no longer compute a kid count from
--     goats via this table.
--   - completeIndividualScopeIfDone's NOT EXISTS gate against this table was
--     removed (free-flow: submit itself completes the bucket).
--   - close.go's scopeNotAcceptedWork uses the bucket's own status instead of
--     a per-animal roster query.
--
-- DEPENDENTS, dropped explicitly and in dependency order (see 000078's identical
-- rationale for why this is explicit statements rather than DROP ... CASCADE):
--   1. weighing_expected_animals_location_idx (000006) -- plain index.
--   2. weighing_expected_animals_scope_status_idx (000058) -- plain index.
--   3. weighing_expected_animals_pk (000006) -- PRIMARY KEY (campaign_id, animal_id).
--   4. The table itself. Its FKs to weighing_campaigns, tenants, goats, and
--      weighing_campaign_sheds are all single-table-scoped and drop
--      automatically with the table; nothing else references this table's
--      rows (no other table has a FK pointing AT weighing_expected_animals).
--
-- LOCK SAFETY: same session-level SET lock_timeout/statement_timeout pattern as
-- 000078 (NO TRANSACTION means each statement is its own implicit transaction,
-- so SET LOCAL would not survive past the statement that set it). Index drops
-- use CONCURRENTLY so they never block live capture/close/verdict traffic; the
-- final DROP TABLE takes a brief ACCESS EXCLUSIVE lock, guarded by the timeout
-- against a long wait to ACQUIRE it, not against the drop itself being slow.
--
-- IDEMPOTENT / RE-RUNNABLE: every statement is IF EXISTS, so replaying this
-- file against a database that already applied it is a no-op.
SET lock_timeout = '5s';
SET statement_timeout = '30s';
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_expected_animals_location_idx;

SET lock_timeout = '5s';
SET statement_timeout = '30s';
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_expected_animals_scope_status_idx;

SET lock_timeout = '5s';
SET statement_timeout = '30s';
ALTER TABLE IF EXISTS public.weighing_expected_animals
  DROP CONSTRAINT IF EXISTS weighing_expected_animals_pk;

SET lock_timeout = '5s';
SET statement_timeout = '30s';
DROP TABLE IF EXISTS public.weighing_expected_animals;
RESET lock_timeout;
RESET statement_timeout;

-- +goose Down
-- +goose NO TRANSACTION
-- Restores the table shape from 000006 plus the 000058 index, empty. Data is
-- NOT recoverable -- weighing is free-flow going forward and nothing will
-- repopulate this table, so a rollback restores the SCHEMA only (so an old
-- binary that still reads/writes it does not crash), never the rows.
SET lock_timeout = '5s';
SET statement_timeout = '30s';
CREATE TABLE IF NOT EXISTS public.weighing_expected_animals (
  campaign_id uuid NOT NULL REFERENCES public.weighing_campaigns(campaign_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  animal_id uuid REFERENCES public.goats(goat_id),
  scanned_identifier text NOT NULL DEFAULT '',
  expected_location_id uuid NOT NULL REFERENCES public.locations(location_id),
  expected_location_label text NOT NULL,
  campaign_shed_id uuid NOT NULL REFERENCES public.weighing_campaign_sheds(campaign_shed_id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'pending',
  availability_status text NOT NULL DEFAULT 'expected_shed',
  current_location_id uuid,
  current_location_label text,
  current_lifecycle_status text,
  availability_checked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT weighing_expected_animals_status_check CHECK (status = ANY (ARRAY['pending','weighed','unavailable','missed','canceled','closed_by_override'])),
  CONSTRAINT weighing_expected_animals_availability_check CHECK (availability_status = ANY (ARRAY['expected_shed','moved_other_shed','icu','quarantine','dead','culled','sold_transferred','exited','unknown_review']))
);
RESET lock_timeout;
RESET statement_timeout;

SET lock_timeout = '5s';
SET statement_timeout = '30s';
ALTER TABLE public.weighing_expected_animals
  ADD CONSTRAINT weighing_expected_animals_pk PRIMARY KEY (campaign_id, animal_id);
RESET lock_timeout;
RESET statement_timeout;

SET lock_timeout = '5s';
SET statement_timeout = '30s';
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_expected_animals_location_idx
  ON public.weighing_expected_animals (tenant_id, campaign_id, expected_location_id, status, animal_id);

SET lock_timeout = '5s';
SET statement_timeout = '30s';
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_expected_animals_scope_status_idx
  ON public.weighing_expected_animals (tenant_id, campaign_id, campaign_shed_id, status);
