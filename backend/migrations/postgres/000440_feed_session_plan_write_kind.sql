-- +goose Up
-- seed-fixture-guard:ignore: widens an audit vocabulary only; no seed contract or business data change
--
-- Let the feed-config write log record a SESSION PLAN write: setting a park's feeding sessions and
-- each one's share of the day (POST /feed-config/session-templates).
--
-- Until this write existed only the seed command could create feed_session_templates rows, so a
-- park added on Configuration > Items & settings could never be given sessions and its feed sheet
-- was blocked with "no session template". The split is a feeding decision of the same weight as a
-- ration quantity, so it gets its own write_kind rather than borrowing another.
--
-- WIDENING ONLY. Every existing row already satisfies the new list; NOT VALID + VALIDATE keeps the
-- table out of an ACCESS EXCLUSIVE lock for the scan. feed_config_write_log is not a hot table.
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
        'session_template_item'::text,
        'session_template'::text
    ])) NOT VALID;

ALTER TABLE public.feed_config_write_log
    VALIDATE CONSTRAINT feed_config_write_log_kind_check;

-- +goose Down
DELETE FROM public.feed_config_write_log WHERE write_kind = 'session_template';

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
    ]));
