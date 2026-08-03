-- +goose Up
-- seed-fixture-guard:ignore: public.weighing_repair_batch_progress is written ONLY by the batched repair procedures in 000087/000088 at migrate time -- never authored as seed data, never read by the seeders, never rebuilt by seed closeout, and not a setup table any seed fixture can describe. It holds one row per repair_key with batch counters, so no fixture/manifest/runbook companion could state anything about it.
--
-- BATCHING/CHECKPOINT SUBSTRATE (M26, P2) for every Weighing forward-repair
-- from 000087 onward.
--
-- VERSION NUMBERING -- why this series starts at 000086 and not 000081.
--
-- This table and its two consumers were originally authored as 000081/000082/
-- 000083. Main independently claimed those three version numbers
-- (000081_weighing_drop_campaign_park_week_unique,
-- 000082_weighing_observations_drop_mismatch_status,
-- 000083_weighing_alerts_feed_index) and shipped them, so THOSE keep their
-- numbers -- a version key that has already been applied somewhere can never
-- move -- and this series was renumbered forward to 000086/000087/000088. It
-- had never shipped, so renumbering it costs nothing.
--
-- 000084 IS BURNED AND MUST NEVER BE REUSED. A migration
-- 000084_weighing_duplicate_loser_verification_two_row_repair.sql existed on an
-- intermediate commit of this branch and was withdrawn before merge, so some
-- databases carry an APPLIED version 000084 with no file behind it. Any new
-- file claiming 000084 would be silently skipped there (already-applied
-- version) while running everywhere else -- a schema that differs by
-- environment with no error. The gap between 000083 and 000085 is deliberate.
--
-- WHY THIS EXISTS -- the defect being answered.
--
-- 000070 (submitted_at backfill), 000071 (verification_status backfill) and
-- 000073's duplicate pre-cleanup each rewrite weighing_observations --
-- animal-grain, continuously written by the live SERIALIZABLE capture path --
-- in ONE unbounded statement. On a small database that is invisible. On a
-- production-sized one it has two failure modes:
--   1. the single UPDATE holds row locks across the whole matched set for its
--      entire runtime, so live capture/submit either blocks behind it or dies
--      on lock timeout mid-shift; and
--   2. if the statement is killed (deploy timeout, statement_timeout, operator
--      cancel, pod eviction) the transaction rolls back ENTIRELY -- every row
--      already rewritten is undone and the next migrate attempt restarts from
--      zero, so a repair that cannot finish inside one window can never finish
--      at all.
--
-- WHY 70/71/73 ARE ACCEPTED AS-IS RATHER THAN REMEDIATED.
--
-- They are NOT edited, and they are NOT re-run in a batched form. This repo's
-- migration runner (cmd/migrate/main.go) tracks each migration by a SHA-256
-- checksum of its file content; rewriting an already-applied migration's SQL
-- makes every environment that already ran it fail on the next migrate with
-- "migration %s was already applied with checksum %s, current %s". 70/71/73
-- are merged to main and applied in dev/stg, so editing them would break those
-- environments rather than protect them. Re-issuing their work as a new
-- batched migration is also wrong: their effect has ALREADY landed there, so a
-- re-run would be a no-op that costs a second full-table pass for nothing, and
-- on a FRESH database 70/71/73 run against an empty or near-empty table where
-- the unbatched form is trivially safe. The residual exposure is therefore
-- bounded to exactly one event that has already happened (their original
-- application on the existing environments) and cannot recur. It is accepted,
-- not remediated.
--
-- WHAT IS REMEDIATED: every NEW repair in this series (000087, 000088, and the
-- held-out loser-verification withdrawal that was withdrawn as 000084)
-- is written batched, lock-bounded and resumable, using this table. The rule
-- those migrations follow, and which this header defines once so they do not
-- each restate it:
--
--   * BOUNDED BATCH LOOP -- work is done in LIMIT-bounded slices inside a
--     plpgsql procedure that COMMITs after every slice, so locks are released
--     between slices and live weighing traffic interleaves instead of queueing.
--   * RESUMABLE -- because each slice is committed, a killed migration keeps
--     all completed slices. The migration is never recorded as applied unless
--     it finishes, so the next migrate re-runs it and it picks up where it
--     stopped.
--   * SELF-DRAINING PREDICATE, NOT A STORED CURSOR -- each slice selects rows
--     that STILL need repair, and repairing a row makes it stop matching. The
--     data itself is the checkpoint, so resume is correct even if this
--     bookkeeping table is truncated, and correctness never depends on a
--     cursor surviving a crash.
--   * IDEMPOTENT -- a re-run (or a fresh database that never had the defect)
--     matches zero rows and exits on the first slice.
--   * LOCK-SAFE -- SET LOCAL lock_timeout / statement_timeout are re-issued
--     inside EVERY slice (they are transaction-scoped, so the procedure's own
--     COMMIT clears them), bounding both how long a slice waits for a row lock
--     and how long it may run before yielding.
--   * NON-INFINITE -- each loop also carries a hard slice ceiling so a
--     pathological predicate can never spin forever inside a migration.
--
-- THIS TABLE is observability and operator-facing progress only: it answers
-- "did that repair finish, and how much did it actually touch?" after the
-- fact, and makes a partial run visible instead of silent. It is deliberately
-- NOT load-bearing for correctness -- no repair reads it to decide what to do.
-- It is not tenant-scoped because a repair is a database-wide maintenance
-- event, not tenant business data.
CREATE TABLE IF NOT EXISTS public.weighing_repair_batch_progress (
  repair_key text PRIMARY KEY,
  batches_run bigint NOT NULL DEFAULT 0 CHECK (batches_run >= 0),
  rows_repaired bigint NOT NULL DEFAULT 0 CHECK (rows_repaired >= 0),
  started_at timestamptz NOT NULL DEFAULT now(),
  last_batch_at timestamptz,
  completed_at timestamptz
);

COMMENT ON TABLE public.weighing_repair_batch_progress IS
  'Progress bookkeeping for batched Weighing forward-repair migrations (000087+). Observability only: no repair reads it to decide what work to do, so truncating it cannot corrupt a resume.';

-- +goose Down
-- Safe to drop: this table is pure bookkeeping and nothing reads it to make a
-- decision, so removing it cannot change the outcome of any repair. A repair
-- that ran before the drop stays repaired (its effect lives in the weighing
-- tables themselves); only the audit trail of "how many batches it took" is
-- lost.
DROP TABLE IF EXISTS public.weighing_repair_batch_progress;
