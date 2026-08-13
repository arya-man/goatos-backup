-- +goose Up
-- +goose NO TRANSACTION
-- WEIGHING IS FREE-FLOW (maintainer decision, superseding 9ce724db3's brief
-- clinical-gate regression; see 3a4026485 and
-- repository_free_flow_no_herd_crosscheck_integration_test.go). The write path
-- has never resolved a scanned identifier to herd identity since that fix:
-- RecordAnimalObservation stores scanned_identifier and leaves animal_id NULL
-- on every insert, ignoring any animal_id the caller sends
-- (repository.go recordUnknownAnimalObservationTx, ~line 1501-1503). The
-- column has therefore been dead weight -- always NULL, never read for a
-- write decision -- since 000007_weighing_free_flow_scanned_identifier.sql
-- made it nullable.
--
-- Per the maintainer's 2026-08-03 follow-up ("not good enough that animal_id
-- is dead but harmless. Delete it. Weighing must not keep that attached to
-- anything."), this migration removes the column outright rather than leaving
-- an always-NULL vestige that a future change could start writing into again.
--
-- SCOPE: weighing_observations.animal_id ONLY. weighing_expected_animals.
-- animal_id is a DIFFERENT table and a different concern -- it is the
-- planner/roster catalog's own join to goats for population counts and roster
-- display (a label, per domain/types.go's CampaignShed/ExpectedAnimal docs),
-- not the scan write path, and is out of scope here.
--
-- DEPENDENTS, dropped explicitly and in dependency order rather than via
-- DROP COLUMN ... CASCADE (an explicit, named drop is auditable; CASCADE on a
-- hot table would silently take out anything anyone had since bolted onto the
-- column without this migration having to say so):
--   1. weighing_observations_campaign_animal_idx -- a plain index keyed
--      (tenant_id, campaign_id, animal_id, accepted_at DESC). Already useless
--      under free-flow (animal_id is always NULL, so every entry collapses
--      into one NULL bucket); dropped CONCURRENTLY so index removal itself
--      never blocks live capture traffic.
--   2. weighing_observations_animal_or_identifier_check -- the two-column
--      CHECK (animal_id IS NOT NULL OR btrim(scanned_identifier) <> '') from
--      000006/000007. Postgres refuses to drop a column a multi-column
--      constraint depends on without CASCADE, so this is dropped by name
--      first. It is REPLACED by a single-column NOT VALID + VALIDATE check
--      requiring scanned_identifier alone (the free-flow write path has set
--      animal_id NULL on every row for a long time, so the OR clause has been
--      vacuous; every existing row already satisfies the narrower rule) --
--      the same NOT VALID-then-VALIDATE two-step 000005 uses, so the
--      validation scan (which must read every row) takes only a light lock
--      and never blocks concurrent writers the way adding a plain CHECK
--      would.
--   3. The animal_id -> goats(goat_id) foreign key is a single-column
--      constraint wholly defined by animal_id; Postgres drops it
--      automatically with the column, no separate statement needed.
--
-- LOCK SAFETY: SET lock_timeout/statement_timeout (session-level, not LOCAL --
-- +goose NO TRANSACTION means each statement here is its own implicit
-- transaction, so a transaction-scoped SET LOCAL would not survive past the
-- statement that set it; see 000005_proof_artifact_retention_columns.sql for
-- the same session-level pattern) bound how long each step can contend for
-- locks against the live SERIALIZABLE capture path, instead of blocking
-- operator traffic indefinitely. DROP COLUMN itself is a metadata-only
-- operation (no table rewrite -- Postgres marks the column dropped and
-- reclaims storage lazily), so its ACCESS EXCLUSIVE hold is brief once
-- acquired; the timeout guards against the ACQUIRE, not the operation.
--
-- IDEMPOTENT / RE-RUNNABLE: every step is IF EXISTS / re-checks pg_constraint
-- before adding, so a re-run (including replaying this file against a
-- database that already applied it) is a no-op.
SET lock_timeout = '5s';
SET statement_timeout = '30s';

DROP INDEX CONCURRENTLY IF EXISTS public.weighing_observations_campaign_animal_idx;

SET lock_timeout = '5s';
SET statement_timeout = '30s';
ALTER TABLE public.weighing_observations
  DROP CONSTRAINT IF EXISTS weighing_observations_animal_or_identifier_check;

-- +goose StatementBegin
SET lock_timeout = '5s';
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'weighing_observations_scanned_identifier_required'
      AND conrelid = 'public.weighing_observations'::regclass
  ) THEN
    ALTER TABLE public.weighing_observations
      ADD CONSTRAINT weighing_observations_scanned_identifier_required
      CHECK (btrim(scanned_identifier) <> '') NOT VALID;
  END IF;
END $$;
-- +goose StatementEnd

SET lock_timeout = '5s';
SET statement_timeout = '30s';
ALTER TABLE public.weighing_observations
  VALIDATE CONSTRAINT weighing_observations_scanned_identifier_required;
RESET lock_timeout;
RESET statement_timeout;

SET lock_timeout = '5s';
SET statement_timeout = '30s';
ALTER TABLE public.weighing_observations
  DROP COLUMN IF EXISTS animal_id;
RESET lock_timeout;
RESET statement_timeout;

-- +goose Down
-- +goose NO TRANSACTION
-- Restoring the column is safe (it is nullable and was never functionally
-- load-bearing under free-flow), but restoring DATA into it is not possible --
-- the values are gone. Down recreates the column, the FK, the old two-column
-- check, and the index, all NULL-tolerant, so a rollback does not lose the
-- ability to run the previous binary, but every row's animal_id comes back
-- NULL (which is what free-flow has written for every row since 000007
-- anyway).
SET lock_timeout = '5s';
SET statement_timeout = '30s';
ALTER TABLE public.weighing_observations
  ADD COLUMN IF NOT EXISTS animal_id uuid REFERENCES public.goats(goat_id);
RESET lock_timeout;
RESET statement_timeout;

ALTER TABLE public.weighing_observations
  DROP CONSTRAINT IF EXISTS weighing_observations_scanned_identifier_required;

-- +goose StatementBegin
SET lock_timeout = '5s';
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'weighing_observations_animal_or_identifier_check'
      AND conrelid = 'public.weighing_observations'::regclass
  ) THEN
    ALTER TABLE public.weighing_observations
      ADD CONSTRAINT weighing_observations_animal_or_identifier_check
      CHECK (animal_id IS NOT NULL OR btrim(scanned_identifier) <> '') NOT VALID;
  END IF;
END $$;
-- +goose StatementEnd

SET lock_timeout = '5s';
ALTER TABLE public.weighing_observations
  VALIDATE CONSTRAINT weighing_observations_animal_or_identifier_check;
RESET lock_timeout;

CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_observations_campaign_animal_idx
  ON public.weighing_observations (tenant_id, campaign_id, animal_id, accepted_at DESC);
