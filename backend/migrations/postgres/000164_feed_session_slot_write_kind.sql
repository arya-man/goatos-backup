-- +goose Up
-- seed-fixture-guard:ignore: widens an audit vocabulary only; no seed contract or business data change
--
-- Let the feed-config write log record a SESSION SLOT write.
--
-- Declaring a feed on a session's recipe is the write that decides whether that feed reaches an
-- animal at all: generation walks a session's declared slots and looks each one up in the ration
-- grid, so a feed with a grid quantity but no slot is never looked up and is silently absent from
-- the sheet. That is a feeding decision of the same weight as authoring a quantity, and it must be
-- as auditable — which means its own write_kind rather than borrowing 'feed_item', where it would be
-- indistinguishable from adding a catalog entry.
--
-- WIDENING ONLY. Every existing row already satisfies the new list, so this cannot reject stored
-- data; NOT VALID + VALIDATE is used anyway so the table is not held under an ACCESS EXCLUSIVE lock
-- for a full scan while the new constraint is proven. No row is read, changed or removed.
--
-- feed_config_write_log is not in the hot-table set (validate-hot-index-migrations.sh), so the
-- ordinary DROP/ADD shape is permitted here.
ALTER TABLE public.feed_config_write_log
    DROP CONSTRAINT feed_config_write_log_kind_check;

ALTER TABLE public.feed_config_write_log
    ADD CONSTRAINT feed_config_write_log_kind_check
    CHECK (write_kind = ANY (ARRAY[
        'ration_rate'::text,
        'shed_factor'::text,
        'schedule_config'::text,
        'experiment_config'::text,
        'feed_item'::text,
        'session_template_item'::text
    ])) NOT VALID;

ALTER TABLE public.feed_config_write_log
    VALIDATE CONSTRAINT feed_config_write_log_kind_check;

-- +goose Down
-- Narrowing back would reject any slot write already recorded, so the log is cleared of that kind
-- first. Rolling back the ability to declare a slot means those audit rows describe a write the
-- schema no longer admits; the authored slots themselves live in feed_session_template_items and are
-- NOT touched here.
DELETE FROM public.feed_config_write_log WHERE write_kind = 'session_template_item';

ALTER TABLE public.feed_config_write_log
    DROP CONSTRAINT feed_config_write_log_kind_check;

ALTER TABLE public.feed_config_write_log
    ADD CONSTRAINT feed_config_write_log_kind_check
    CHECK (write_kind = ANY (ARRAY[
        'ration_rate'::text,
        'shed_factor'::text,
        'schedule_config'::text,
        'experiment_config'::text,
        'feed_item'::text
    ]));
