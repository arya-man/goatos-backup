-- +goose Up
-- +goose NO TRANSACTION
-- WEIGHING IS FREE-FLOW: there is no expected roster, so no scan can be
-- classified against one. weighing_observations.mismatch_status was defined
-- (000006) as one of 'expected_shed' / 'wrong_shed' / 'extra_scan' -- three
-- verdicts that all presuppose an expected set of animals for a shed. That set
-- was DELIBERATELY DROPPED by 000079_weighing_drop_expected_animals_table.sql
-- (and the observation's own animal_id by 000078), which left this column
-- describing a comparison the system no longer performs.
--
-- What actually happened in production: the write path stamped the literal
-- 'extra_scan' on EVERY insert (repository.go recordUnknownAnimalObservationTx).
-- On the first real device run, all ten of the operator's correct, in-shed
-- weights read back as 'extra_scan', so anyone reviewing the table saw ten
-- exceptions where there were zero. The column did not merely carry no signal;
-- it carried a false one on every row.
--
-- Per the same maintainer rule that produced 000078 ("not good enough that it is
-- dead but harmless -- delete it"), this removes the column outright rather than
-- leaving a vestige a future change could start branching on. Nothing reads it:
-- no read model, no projection, no ceo_ai view (000080 selects only weight and
-- count aggregates from this table), no API contract field, no admin-web or
-- Android surface -- the only consumers were the INSERT above and the seed
-- fixture, both updated in this same change.
--
-- DEPENDENTS, dropped explicitly and in dependency order rather than via
-- DROP COLUMN ... CASCADE (an explicit, named drop is auditable; CASCADE on a
-- hot table would silently take out anything anyone had since bolted onto the
-- column without this migration having to say so):
--   1. weighing_observations_mismatch_check -- the single-column CHECK from
--      000006 enumerating the three roster verdicts. Postgres would drop it
--      automatically with the column, but naming it keeps the audit explicit
--      and makes a partial re-run converge.
--   There is no index and no foreign key on this column.
--
-- LOCK SAFETY: SET lock_timeout/statement_timeout at SESSION level (not SET
-- LOCAL -- +goose NO TRANSACTION means each statement is its own implicit
-- transaction, so a transaction-scoped setting would not survive the statement
-- that set it; same pattern as 000005 and 000078). DROP COLUMN is metadata-only
-- (no table rewrite; Postgres marks the column dropped and reclaims storage
-- lazily), so its ACCESS EXCLUSIVE hold is brief once acquired -- the timeout
-- guards the ACQUIRE against the live capture path, not the operation.
--
-- IDEMPOTENT / RE-RUNNABLE: every step is IF EXISTS, so replaying this file
-- against a database that already applied it is a no-op.
SET lock_timeout = '5s';
SET statement_timeout = '30s';
ALTER TABLE public.weighing_observations
  DROP CONSTRAINT IF EXISTS weighing_observations_mismatch_check;
RESET lock_timeout;
RESET statement_timeout;

SET lock_timeout = '5s';
SET statement_timeout = '30s';
ALTER TABLE public.weighing_observations
  DROP COLUMN IF EXISTS mismatch_status;
RESET lock_timeout;
RESET statement_timeout;

-- +goose Down
-- +goose NO TRANSACTION
-- Restoring the column is safe (it has a default and nothing reads it), but the
-- stored values are gone. Down recreates the column with its 000006 default and
-- CHECK so the previous binary can run again; every row comes back
-- 'expected_shed', which is no less true than the 'extra_scan' the write path
-- was stamping. The CHECK is added NOT VALID then VALIDATEd -- the two-step from
-- 000005/000078 -- so the row scan takes only a light lock instead of blocking
-- concurrent capture the way a plain ADD CONSTRAINT would.
SET lock_timeout = '5s';
SET statement_timeout = '30s';
ALTER TABLE public.weighing_observations
  ADD COLUMN IF NOT EXISTS mismatch_status text NOT NULL DEFAULT 'expected_shed';
RESET lock_timeout;
RESET statement_timeout;

-- +goose StatementBegin
SET lock_timeout = '5s';
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'weighing_observations_mismatch_check'
      AND conrelid = 'public.weighing_observations'::regclass
  ) THEN
    ALTER TABLE public.weighing_observations
      ADD CONSTRAINT weighing_observations_mismatch_check
      CHECK (mismatch_status = ANY (ARRAY['expected_shed','wrong_shed','extra_scan'])) NOT VALID;
  END IF;
END $$;
-- +goose StatementEnd

SET lock_timeout = '5s';
SET statement_timeout = '30s';
ALTER TABLE public.weighing_observations
  VALIDATE CONSTRAINT weighing_observations_mismatch_check;
RESET lock_timeout;
RESET statement_timeout;
