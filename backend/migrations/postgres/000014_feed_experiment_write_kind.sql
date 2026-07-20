-- +goose Up
-- Experiment sheds become AUTHORABLE. Until now feed_experiment_config existed (migration 000003)
-- and the generator honoured it (domain.ExperimentPlanner), but nothing could write it: the module
-- had no endpoint, the seed had no source rows, and the write ledger could not even record such an
-- edit. This migration opens the last of those three.
--
-- WHY A FORWARD MIGRATION (not an edit to 000004):
-- the migrator is forward-only. 000001-000005 are applied and checksum-tracked on dev/stg, so
-- editing historical SQL would leave those databases with a ledger that rejects every experiment
-- edit while the API happily accepts one. Same rule that produced 000002-000005.
--
-- ===========================================================================
-- WHAT THIS UNBLOCKS, AND WHY IT IS A CORRECTNESS FIX RATHER THAN A FEATURE
-- ===========================================================================
-- Membership in feed_experiment_config IS what makes a shed an experiment shed -- there is no
-- separate flag (see ExperimentPlanner.Applies). With the table empty, EVERY shed fell through to
-- NormalPlanner and was fed head_count x grams_per_head x shed_factor off the ration grid.
--
-- For the 34 sheds the farm actually runs as experiments that is the wrong arithmetic, not merely
-- the wrong label: their real feed is hand-entered ABSOLUTE kg per shed, and the source workbook
-- writes them into normal Feed Direction as EMPTY placeholders precisely so nobody computes them
-- per head. Measured against the workbook's own Feed Direction tab for 2026-07-20, feeding those
-- 34 sheds from the per-head grid inflated CBE's concentrate total from 182.0 kg to 398.8 kg --
-- about 2.2x. Excluding them, per-shed parity is already good (42 of 46 CBE sheds within 0.35 kg),
-- so the ration maths was never the defect; the missing experiment rows were.
--
-- ===========================================================================
-- THE CHANGE: one more write_kind
-- ===========================================================================
-- feed_config_write_log is the idempotency ledger EVERY authored feed-config write commits inside
-- its own transaction (see 000004 and feedconfig/adapters/postgres.runWrite). Its write_kind CHECK
-- is a closed vocabulary, so an experiment edit would fail the ledger insert -- and because the
-- ledger row and the side effect share one transaction, that failure correctly rolls the whole edit
-- back. The API would return a 500 on every experiment write. Widening the vocabulary is therefore
-- a precondition of the endpoint, not a cosmetic addition.
--
-- ONE kind covers both experiment writes ('experiment_config'):
--
--   * authoring one (park, shed, feed_item) cell -- absolute kg, informational head count, arm;
--   * switching a whole shed between the experiment and the normal grid, which is implemented as a
--     status flip on that shed's rows rather than a DELETE, so the authored quantities survive a
--     withdraw-and-restore and the audit trail keeps the numbers that were in force.
--
-- They are one kind because they are one authoring surface with one identity space; the ledger's
-- result_row_id and outcome already distinguish what an individual edit did.
--
-- NO NEW INDEX. The experiment listing reads
--   WHERE tenant_id = $1 AND park_id = $2 [AND status = $3] ORDER BY shed_id, feed_item_key
-- which is served by feed_experiment_config_natural_key_uidx
-- (tenant_id, park_id, shed_id, feed_item_key) -- leading columns match the predicate and the sort
-- is a prefix-ordered walk of the same index. The read is also bounded by construction: it is one
-- park's hand-authored sheds (34 rows x 5 items across BOTH live parks today), and the service
-- rejects an offset past 5000.
--
-- NOT CHANGED, deliberately: feed_experiment_config is NOT effective-dated, unlike feed_ration_rates
-- / feed_shed_factors / feed_schedule_config. An experiment quantity is a hand-entered figure for a
-- running trial, corrected in place while the trial runs, rather than a standing rule whose past
-- values must stay reconstructable to explain an old feed sheet. Adding valid_from/valid_to here
-- would imply an audit guarantee this table does not make; the write ledger records who changed
-- what and when, which is the guarantee it does make.

-- feed_config_write_log is an append-only, unboundedly-growing idempotency ledger (every authored
-- feed-config write commits a row here). A plain DROP + ADD CONSTRAINT re-validates the widened
-- CHECK against every existing row under an ACCESS EXCLUSIVE lock -- a full-table scan that blocks
-- reads/writes on the ledger for its duration and gets worse every day the ledger grows. Split it:
-- add the new CHECK NOT VALID (enforced on every subsequent INSERT immediately, no scan, brief
-- ACCESS EXCLUSIVE just to add the constraint metadata), then VALIDATE CONSTRAINT separately, which
-- takes only SHARE UPDATE EXCLUSIVE (blocks other DDL, not reads/writes) while it scans existing
-- rows. Unlike shifting_events (see 000007), feed_config_write_log has no forbid on direct
-- VALIDATE from validate-postgres-migrations -- it is a narrower, purely-additive ledger table, so
-- validating in the same migration is safe and keeps the constraint fully enforced immediately
-- rather than leaving it silently NOT VALID indefinitely.
ALTER TABLE feed_config_write_log
    DROP CONSTRAINT feed_config_write_log_kind_check;

ALTER TABLE feed_config_write_log
    ADD CONSTRAINT feed_config_write_log_kind_check
    CHECK (write_kind = ANY (ARRAY[
        'ration_rate'::text,
        'shed_factor'::text,
        'schedule_config'::text,
        'experiment_config'::text
    ])) NOT VALID;

ALTER TABLE feed_config_write_log
    VALIDATE CONSTRAINT feed_config_write_log_kind_check;

COMMENT ON COLUMN feed_config_write_log.write_kind IS
  'Which authored feed-config surface this ledger row describes. ''experiment_config'' covers both authoring an experiment shed''s absolute-kg cell and switching a shed between the experiment workflow and the normal ration grid -- one authoring surface, one identity space.';


-- +goose Down
-- Reversible, but NOT loss-free, and that is stated rather than hidden.
--
-- Narrowing the CHECK back to three kinds cannot succeed while experiment ledger rows exist, so they
-- are removed first. That discards the audit trail of any experiment edit made while this migration
-- was applied. It does NOT touch feed_experiment_config itself: the authored quantities survive, and
-- only the record of who last changed them is lost.
--
-- This is acceptable only because a down migration runs in development. Do not run it against a
-- database whose experiment edit history matters -- roll forward instead.
DELETE FROM feed_config_write_log WHERE write_kind = 'experiment_config';

ALTER TABLE feed_config_write_log
    DROP CONSTRAINT feed_config_write_log_kind_check;

ALTER TABLE feed_config_write_log
    ADD CONSTRAINT feed_config_write_log_kind_check
    CHECK (write_kind = ANY (ARRAY[
        'ration_rate'::text,
        'shed_factor'::text,
        'schedule_config'::text
    ]));

COMMENT ON COLUMN feed_config_write_log.write_kind IS NULL;
