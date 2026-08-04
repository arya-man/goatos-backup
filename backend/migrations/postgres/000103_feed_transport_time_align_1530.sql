-- +goose Up
-- Maintainer decision 2026-07-31: the feed sheet's transport cutoff is 15:30, matching the hard
-- 15:30 IST gate in MaterializeTransportTasks that cuts the day's one-task-per-shed transport work.
--
-- These were two different times wearing one name. feed_schedule_config.transport_time was seeded
-- 15:45 and gates the direction lifecycle's LOCK step, while transport task creation hardcodes
-- 15:30 in Go. The sheet therefore stayed amendable for fifteen minutes after the transport tasks
-- for that day had already been raised -- an amendment landing in that window changed a sheet the
-- shed was already being driven from. One time, 15:30, closes it.
--
-- Scoped to rows still holding the SEEDED DEFAULT. A park whose transport_time a human authored to
-- something else is a deliberate local decision, not this inconsistency, and must not be silently
-- rewritten by a migration (AGENTS.md: authored config is validate-or-reject, never
-- silently-defaulted). Those rows are left alone and surface in the verification query below.
UPDATE public.feed_schedule_config
SET transport_time = TIME '15:30',
    updated_at = now()
WHERE transport_time = TIME '15:45';

-- Rows deliberately NOT touched (authored to a third value). Expected to be empty; if it is not,
-- each row is a park whose cutoff still disagrees with the 15:30 transport gate and needs an
-- explicit owner decision:
--   SELECT tenant_id, workflow, transport_time FROM public.feed_schedule_config
--   WHERE transport_time <> TIME '15:30';

-- +goose Down
-- Forward-only. Restoring 15:45 would reopen the amend-after-transport window this closes, and
-- would overwrite any cutoff authored after this migration was applied.
SELECT 1;
