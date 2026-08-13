-- +goose Up
--
-- One more write_kind: 'feed_item'.
--
-- WHY THIS IS A PRECONDITION OF THE ENDPOINT, NOT A COSMETIC ADDITION
--
-- feed_config_write_log is the idempotency ledger EVERY authored feed-config write commits inside
-- its own transaction (see feedconfig/adapters/postgres.runWrite). Its write_kind CHECK is a CLOSED
-- vocabulary, so a feed-item write would fail the ledger insert -- and because the ledger row and
-- the side effect share one transaction, that failure correctly rolls the whole edit back. The API
-- would return 500 on every "Add feed type". This is exactly the situation the experiment-config
-- widening in the baseline describes, one kind later.
--
-- WHAT 'feed_item' COVERS: adding a row to feed_item_catalog -- the tenant's feed vocabulary that
-- the ration grid, the shed factors and the experiment sheds are all indexed by. Adding an item
-- does NOT author any quantity: a new item feeds nothing until a rate, factor or experiment cell
-- names it, which is why this is its own kind and not folded into 'ration_rate'.
--
-- NOT CHANGED, deliberately: feed_item_catalog is NOT effective-dated, like feed_experiment_config
-- and unlike feed_ration_rates / feed_shed_factors / feed_schedule_config. A catalog entry is a
-- name, not a standing quantity a past feed sheet has to be explained against -- the rates that
-- REFERENCE the item carry the effective dating, and they carry the label rather than a foreign key
-- (feed_item_label + generated feed_item_key), so a past rate stays readable regardless of what the
-- catalog holds today. The ledger still records who added what and when.
--
-- NO NEW INDEX. The catalog listing reads
--   WHERE tenant_id = $1 ORDER BY display_order, feed_item_label, feed_item_id
-- and the duplicate check on insert reads
--   WHERE tenant_id = $1 AND feed_item_key = feed_config_norm($2)
-- which is served by feed_item_catalog_natural_key_uidx (tenant_id, feed_item_key) -- the same
-- unique index that makes a concurrent duplicate insert fail closed rather than fork the vocabulary.
--
-- LOCK SAFETY: feed_config_write_log is an append-only, unboundedly-growing ledger, so a plain
-- DROP + ADD CONSTRAINT would re-validate the widened CHECK against every existing row under an
-- ACCESS EXCLUSIVE lock -- a full scan that blocks the ledger and gets worse every day. Split it,
-- exactly as the experiment_config widening did: ADD ... NOT VALID (enforced on every subsequent
-- INSERT immediately, no scan, brief ACCESS EXCLUSIVE for the metadata only), then VALIDATE
-- CONSTRAINT separately under SHARE UPDATE EXCLUSIVE, which blocks other DDL but not reads/writes.
ALTER TABLE feed_config_write_log
    DROP CONSTRAINT feed_config_write_log_kind_check;

ALTER TABLE feed_config_write_log
    ADD CONSTRAINT feed_config_write_log_kind_check
    CHECK (write_kind = ANY (ARRAY[
        'ration_rate'::text,
        'shed_factor'::text,
        'schedule_config'::text,
        'experiment_config'::text,
        'feed_item'::text
    ])) NOT VALID;

ALTER TABLE feed_config_write_log
    VALIDATE CONSTRAINT feed_config_write_log_kind_check;

COMMENT ON COLUMN feed_config_write_log.write_kind IS
  'Which authored feed-config surface this ledger row describes. ''experiment_config'' covers both authoring an experiment shed''s absolute-kg cell and switching a shed between the experiment workflow and the normal ration grid. ''feed_item'' covers adding an entry to the feed-item catalog -- a name the grid can then be indexed by, which authors no quantity of its own.';

-- +goose Down
--
-- Reverting drops 'feed_item' from the vocabulary. Any ledger row already recorded with that kind
-- would then violate the narrowed CHECK, so the constraint is re-added NOT VALID and deliberately
-- NOT validated: a down migration must not fail on data a forward migration legitimately wrote.
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
