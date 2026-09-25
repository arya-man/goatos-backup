-- +goose Up
--
-- STORED PER-(ITEM, ACTOR) WATCH SUMMARY FOR THE OVERSIGHT ANALYTICS WATCH-INTEGRITY FIGURES.
--
-- GET /verification/oversight-analytics computed items_tracked / watched_to_end /
-- verdict_without_play by joining every item decided in the last 14 days to ALL of its
-- verification_review_events for the deciding actor and re-aggregating them on every request:
-- ~5.3k decided items -> ~15k event rows (114k-row, 61 MB table on STG), 68 ms warm and 177 ms
-- with cold buffers -- the slowest read on the route. Banned pattern #1 in
-- docs/perf/2026-09-24-stg-latency/README.md (re-aggregating a growing history per request).
--
-- verification_review_item_watch holds exactly the four per-(tenant, item, actor) facts that
-- aggregate derived, maintained in the SAME transaction as every event write:
--   opened           = bool_or(event_type = 'item_opened')
--   played           = bool_or(event_type = 'video_play')
--   max_position_ms  = max((payload->>'video_position_ms')::bigint)
--   max_duration_ms  = max((payload->>'video_duration_ms')::bigint)
-- A row exists iff the (item, actor) pair has at least one event, so a LEFT JOIN from the
-- decided items reproduces the old LEFT JOIN + GROUP BY exactly (no events -> all NULL).
--
-- MAINTENANCE:
--   INSERT (the only production write, ReviewEventRepository.InsertReviewEvents): a
--     statement-level trigger folds the new rows in with INSERT ... ON CONFLICT DO UPDATE using
--     OR / GREATEST. Both are commutative and idempotent, and ON CONFLICT re-reads the latest
--     committed row under its row lock, so concurrent flushes for the same pair never lose an event.
--   UPDATE / DELETE (none today; a future retention job): the touched pairs are re-derived from
--     the remaining events, so the summary always equals the live aggregate.
--   queue_opened rows carry no item (item_id NULL) and never contributed to the per-item join.
--
-- LOCKS / BACKFILL: the trigger is created BEFORE the backfill in this one transaction.
-- CREATE TRIGGER holds SHARE ROW EXCLUSIVE on verification_review_events until commit, so no
-- event can be written between the backfill snapshot and the trigger going live; the backfill of
-- the ~15k STG pairs takes well under a second. lock_timeout bounds the wait for that lock.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '120s';

-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=derived-summary-of-append-only-telemetry-maintained-by-trigger expiry=2026-11-30
-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=verifier-watch-telemetry-summary-not-a-reconciliation-queue expiry=2026-11-30
CREATE TABLE IF NOT EXISTS public.verification_review_item_watch (
  tenant_id uuid NOT NULL,
  item_id uuid NOT NULL,
  actor_id uuid NOT NULL,
  opened boolean NOT NULL,
  played boolean NOT NULL,
  max_position_ms bigint,
  max_duration_ms bigint,
  PRIMARY KEY (tenant_id, item_id, actor_id)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.verification_review_item_watch_fold_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
  INSERT INTO public.verification_review_item_watch AS w
    (tenant_id, item_id, actor_id, opened, played, max_position_ms, max_duration_ms)
  SELECT n.tenant_id, n.item_id, n.actor_id,
         bool_or(n.event_type = 'item_opened'),
         bool_or(n.event_type = 'video_play'),
         max((n.payload->>'video_position_ms')::bigint),
         max((n.payload->>'video_duration_ms')::bigint)
  FROM new_events n
  WHERE n.item_id IS NOT NULL
  GROUP BY n.tenant_id, n.item_id, n.actor_id
  ORDER BY n.tenant_id, n.item_id, n.actor_id
  ON CONFLICT (tenant_id, item_id, actor_id) DO UPDATE
    SET opened = w.opened OR EXCLUDED.opened,
        played = w.played OR EXCLUDED.played,
        max_position_ms = GREATEST(w.max_position_ms, EXCLUDED.max_position_ms),
        max_duration_ms = GREATEST(w.max_duration_ms, EXCLUDED.max_duration_ms);
  RETURN NULL;
END;
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.verification_review_item_watch_rederive()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
  CREATE TEMP TABLE IF NOT EXISTS verification_review_item_watch_keys (
    tenant_id uuid, item_id uuid, actor_id uuid
  ) ON COMMIT DROP;
  TRUNCATE verification_review_item_watch_keys;
  INSERT INTO verification_review_item_watch_keys
  SELECT DISTINCT o.tenant_id, o.item_id, o.actor_id FROM old_events o WHERE o.item_id IS NOT NULL;
  IF TG_OP = 'UPDATE' THEN
    INSERT INTO verification_review_item_watch_keys
    SELECT DISTINCT n.tenant_id, n.item_id, n.actor_id FROM new_events n WHERE n.item_id IS NOT NULL;
  END IF;

  -- Lock the affected summary rows in key order, then re-derive in a new statement.
  PERFORM 1 FROM public.verification_review_item_watch w
  JOIN verification_review_item_watch_keys k USING (tenant_id, item_id, actor_id)
  ORDER BY w.tenant_id, w.item_id, w.actor_id
  FOR UPDATE OF w;

  DELETE FROM public.verification_review_item_watch w
  USING verification_review_item_watch_keys k
  WHERE w.tenant_id = k.tenant_id AND w.item_id = k.item_id AND w.actor_id = k.actor_id;

  INSERT INTO public.verification_review_item_watch
    (tenant_id, item_id, actor_id, opened, played, max_position_ms, max_duration_ms)
  SELECT e.tenant_id, e.item_id, e.actor_id,
         bool_or(e.event_type = 'item_opened'),
         bool_or(e.event_type = 'video_play'),
         max((e.payload->>'video_position_ms')::bigint),
         max((e.payload->>'video_duration_ms')::bigint)
  FROM public.verification_review_events e
  JOIN (SELECT DISTINCT tenant_id, item_id, actor_id FROM verification_review_item_watch_keys) k
    ON e.tenant_id = k.tenant_id AND e.item_id = k.item_id AND e.actor_id = k.actor_id
  GROUP BY e.tenant_id, e.item_id, e.actor_id
  ON CONFLICT (tenant_id, item_id, actor_id) DO UPDATE
    SET opened = EXCLUDED.opened,
        played = EXCLUDED.played,
        max_position_ms = EXCLUDED.max_position_ms,
        max_duration_ms = EXCLUDED.max_duration_ms;
  RETURN NULL;
END;
$fn$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS verification_review_item_watch_insert ON public.verification_review_events;
CREATE TRIGGER verification_review_item_watch_insert
  AFTER INSERT ON public.verification_review_events
  REFERENCING NEW TABLE AS new_events
  FOR EACH STATEMENT EXECUTE FUNCTION public.verification_review_item_watch_fold_insert();

DROP TRIGGER IF EXISTS verification_review_item_watch_update ON public.verification_review_events;
CREATE TRIGGER verification_review_item_watch_update
  AFTER UPDATE ON public.verification_review_events
  REFERENCING OLD TABLE AS old_events NEW TABLE AS new_events
  FOR EACH STATEMENT EXECUTE FUNCTION public.verification_review_item_watch_rederive();

DROP TRIGGER IF EXISTS verification_review_item_watch_delete ON public.verification_review_events;
CREATE TRIGGER verification_review_item_watch_delete
  AFTER DELETE ON public.verification_review_events
  REFERENCING OLD TABLE AS old_events
  FOR EACH STATEMENT EXECUTE FUNCTION public.verification_review_item_watch_rederive();

-- REBUILD: tools/dev/seed-closeout.sh (run_derived_summary_rebuilds) calls
-- verification_review_item_watch_rebuild(tenant) to re-derive one tenant's rows from
-- verification_review_events; a TRUNCATE of the source empties the summary.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.verification_review_item_watch_rebuild(p_tenant_id uuid)
RETURNS bigint
LANGUAGE plpgsql
AS $fn$
DECLARE
  n bigint;
BEGIN
  LOCK TABLE public.verification_review_events IN SHARE ROW EXCLUSIVE MODE;
  DELETE FROM public.verification_review_item_watch WHERE tenant_id = p_tenant_id;
  INSERT INTO public.verification_review_item_watch
    (tenant_id, item_id, actor_id, opened, played, max_position_ms, max_duration_ms)
  SELECT e.tenant_id, e.item_id, e.actor_id,
         bool_or(e.event_type = 'item_opened'),
         bool_or(e.event_type = 'video_play'),
         max((e.payload->>'video_position_ms')::bigint),
         max((e.payload->>'video_duration_ms')::bigint)
  FROM public.verification_review_events e
  WHERE e.tenant_id = p_tenant_id
    AND e.item_id IS NOT NULL
  GROUP BY e.tenant_id, e.item_id, e.actor_id;
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END;
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.verification_review_item_watch_truncate()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
  TRUNCATE public.verification_review_item_watch;
  RETURN NULL;
END;
$fn$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS verification_review_item_watch_trunc ON public.verification_review_events;
CREATE TRIGGER verification_review_item_watch_trunc
  AFTER TRUNCATE ON public.verification_review_events
  FOR EACH STATEMENT EXECUTE FUNCTION public.verification_review_item_watch_truncate();

-- Backfill every existing pair (the triggers above already hold the table lock).
INSERT INTO public.verification_review_item_watch
  (tenant_id, item_id, actor_id, opened, played, max_position_ms, max_duration_ms)
SELECT e.tenant_id, e.item_id, e.actor_id,
       bool_or(e.event_type = 'item_opened'),
       bool_or(e.event_type = 'video_play'),
       max((e.payload->>'video_position_ms')::bigint),
       max((e.payload->>'video_duration_ms')::bigint)
FROM public.verification_review_events e
WHERE e.item_id IS NOT NULL
GROUP BY e.tenant_id, e.item_id, e.actor_id
ON CONFLICT (tenant_id, item_id, actor_id) DO NOTHING;

-- +goose Down
DROP TRIGGER IF EXISTS verification_review_item_watch_trunc ON public.verification_review_events;
DROP FUNCTION IF EXISTS public.verification_review_item_watch_truncate();
DROP FUNCTION IF EXISTS public.verification_review_item_watch_rebuild(uuid);
DROP TRIGGER IF EXISTS verification_review_item_watch_delete ON public.verification_review_events;
DROP TRIGGER IF EXISTS verification_review_item_watch_update ON public.verification_review_events;
DROP TRIGGER IF EXISTS verification_review_item_watch_insert ON public.verification_review_events;
DROP FUNCTION IF EXISTS public.verification_review_item_watch_rederive();
DROP FUNCTION IF EXISTS public.verification_review_item_watch_fold_insert();
DROP TABLE IF EXISTS public.verification_review_item_watch;
