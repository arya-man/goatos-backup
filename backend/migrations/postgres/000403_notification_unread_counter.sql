-- +goose Up
--
-- STORED UNREAD COUNTER FOR THE NOTIFICATION CENTRE (stg latency incident 2026-09-24).
--
-- GET /app/notifications computed the bell badge as COUNT(DISTINCT dedupe key) over the
-- caller's WHOLE unread history on every page load: 96,171 heap rows -> 19,241 keys -> 1 number
-- for the worst member, ~78k buffers (~610 MB) per call, 282-368 ms warm, 4.1 s cold, stg p95
-- 15 s. That is banned pattern #1 in docs/perf/2026-09-24-stg-latency/README.md.
--
-- This migration replaces it with:
--   notification_member_unread_keys   one row per (tenant, member, dedupe key) that has at least
--                                     one unread delivery row -- exactly the set the legacy
--                                     query counted.
--   notification_member_unread_counts one row per member = count of that member's keys. The
--                                     request path reads it with one primary-key lookup.
--   notification_unread_counter_state the backfill gate. Until the one-shot backfill
--                                     (cmd/backfill-notification-unread-counters) has covered the
--                                     rows written BEFORE this migration, the API keeps using the
--                                     legacy query. A database with no notification rows (fresh
--                                     dev/test) is complete at once.
--
-- MAINTENANCE IS IN THE SAME TRANSACTION AS EVERY WRITE, BY EVERY WRITER. Statement-level
-- AFTER INSERT / UPDATE / DELETE triggers with transition tables cover all producers (the five
-- calendar insert sites and anything added later), MarkRead, the calendar escalation read
-- stamp, the dispatcher's status moves, the verification auto-resolve, and future retention.
--
-- STATE-BASED, NOT DELTA-BASED. For each (tenant, member, key) a statement touched, the
-- function re-derives "does an unread row with this key exist?" and inserts/deletes the key row
-- to match, adjusting the count by the number of key rows actually inserted/deleted. So:
--   * a second delivery row of an already-unread key adds nothing (legacy dedupe grain);
--   * a replayed MarkRead finds nothing to delete and never double-decrements;
--   * an escalation stamp on ONE delivery row of a key keeps the key while a sibling is unread.
-- RACES: before re-deriving, the function locks the affected members' count rows in sorted
-- order, and the re-derivation runs as a NEW statement (fresh READ COMMITTED snapshot) after the
-- lock is held. A mark-read that races a new delivery row of the same key therefore sees that
-- row once its producer commits, and keeps the key.
--
-- LOCKS: CREATE TRIGGER takes SHARE ROW EXCLUSIVE on notification_requests for a catalog-only
-- instant; lock_timeout bounds the wait so a busy dispatcher cannot queue traffic behind it.
-- No table rewrite, no index build on notification_requests. Additive only.
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS public.notification_member_unread_keys (
  tenant_id uuid NOT NULL,
  -- text, not uuid: context->>'member_id' is free text on notification_requests, and a
  -- non-uuid value there must never make a producer's INSERT fail inside the trigger.
  member_id text NOT NULL,
  dedupe_key text NOT NULL,
  PRIMARY KEY (tenant_id, member_id, dedupe_key)
);

CREATE TABLE IF NOT EXISTS public.notification_member_unread_counts (
  tenant_id uuid NOT NULL,
  member_id text NOT NULL,
  unread_count integer NOT NULL DEFAULT 0 CHECK (unread_count >= 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, member_id)
);

CREATE TABLE IF NOT EXISTS public.notification_unread_counter_state (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  backfill_complete_at timestamptz
);

INSERT INTO public.notification_unread_counter_state (singleton, backfill_complete_at)
SELECT true, CASE WHEN EXISTS (SELECT 1 FROM public.notification_requests) THEN NULL ELSE now() END
ON CONFLICT (singleton) DO NOTHING;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.notification_unread_sync(p_tenant uuid[], p_member text[], p_key text[])
RETURNS void
LANGUAGE plpgsql
VOLATILE
AS $fn$
DECLARE
  m record;
BEGIN
  IF p_key IS NULL OR cardinality(p_key) = 0 THEN
    RETURN;
  END IF;

  -- 1. Serialise per member, in a stable order (no lock-order deadlock between two statements).
  FOR m IN
    SELECT DISTINCT u.t, u.m FROM unnest(p_tenant, p_member) AS u(t, m) ORDER BY u.t, u.m
  LOOP
    INSERT INTO public.notification_member_unread_counts AS c (tenant_id, member_id)
    VALUES (m.t, m.m)
    ON CONFLICT (tenant_id, member_id) DO UPDATE SET updated_at = c.updated_at;
  END LOOP;

  -- projection-review: membership=the (tenant, member, dedupe key) triples this statement touched, each re-derived from notification_requests; group_key=(tenant_id, member_id) for the count delta; join_cardinality=keys join 1:1 on their primary key, delta is one row per member; pagination=none, bounded by the triggering statement's rows; scope=tenant_id AND member_id on every table
  -- 2. A new statement = a new snapshot: re-derive each touched key from the rows themselves.
  --    The EXISTS probe is notification_requests_member_dedupe_idx (000396); its key expression
  --    must stay character-identical to dedupeKeyExpr in notificationcentre.
  WITH aff AS (
    SELECT DISTINCT u.t, u.m, u.k FROM unnest(p_tenant, p_member, p_key) AS u(t, m, k)
  ),
  want AS (
    SELECT a.t, a.m, a.k,
           EXISTS (
             SELECT 1 FROM public.notification_requests nr
             WHERE nr.tenant_id = a.t
               AND nr.context->>'member_id' = a.m
               AND COALESCE(NULLIF(nr.context->>'event_key', ''), nr.notification_request_id::text) = a.k
               AND nr.read_at IS NULL AND nr.status <> 'read'
           ) AS unread
    FROM aff a
  ),
  ins AS (
    INSERT INTO public.notification_member_unread_keys (tenant_id, member_id, dedupe_key)
    SELECT w.t, w.m, w.k FROM want w WHERE w.unread
    ON CONFLICT (tenant_id, member_id, dedupe_key) DO NOTHING
    RETURNING tenant_id, member_id
  ),
  del AS (
    DELETE FROM public.notification_member_unread_keys k
    USING want w
    WHERE k.tenant_id = w.t AND k.member_id = w.m AND k.dedupe_key = w.k AND NOT w.unread
    RETURNING k.tenant_id, k.member_id
  ),
  delta AS (
    SELECT d.tenant_id, d.member_id, sum(d.n)::int AS n
    FROM (
      SELECT tenant_id, member_id, 1 AS n FROM ins
      UNION ALL
      SELECT tenant_id, member_id, -1 AS n FROM del
    ) d
    GROUP BY d.tenant_id, d.member_id
  )
  UPDATE public.notification_member_unread_counts c
  SET unread_count = c.unread_count + delta.n,
      updated_at = now()
  FROM delta
  WHERE c.tenant_id = delta.tenant_id AND c.member_id = delta.member_id AND delta.n <> 0;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.notification_unread_after_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid[]; m text[]; k text[];
BEGIN
  SELECT array_agg(s.tenant_id), array_agg(s.member_id), array_agg(s.dedupe_key)
  INTO t, m, k
  FROM (
    SELECT DISTINCT n.tenant_id, n.context->>'member_id' AS member_id,
           COALESCE(NULLIF(n.context->>'event_key', ''), n.notification_request_id::text) AS dedupe_key
    FROM new_rows n
    WHERE NULLIF(n.context->>'member_id', '') IS NOT NULL
      AND n.read_at IS NULL AND n.status <> 'read'
  ) s;
  PERFORM public.notification_unread_sync(t, m, k);
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.notification_unread_after_update()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid[]; m text[]; k text[];
BEGIN
  -- Only rows whose unread-ness or grouping changed. The dispatcher's queued -> sending -> sent
  -- moves change neither and fall out here without touching the counter tables.
  WITH changed AS (
    SELECT o.tenant_id AS o_t, o.context->>'member_id' AS o_m,
           COALESCE(NULLIF(o.context->>'event_key', ''), o.notification_request_id::text) AS o_k,
           n.tenant_id AS n_t, n.context->>'member_id' AS n_m,
           COALESCE(NULLIF(n.context->>'event_key', ''), n.notification_request_id::text) AS n_k
    FROM old_rows o
    JOIN new_rows n ON n.notification_request_id = o.notification_request_id
    WHERE (o.read_at IS NULL AND o.status <> 'read') IS DISTINCT FROM (n.read_at IS NULL AND n.status <> 'read')
       OR o.tenant_id <> n.tenant_id
       OR (o.context->>'member_id') IS DISTINCT FROM (n.context->>'member_id')
       OR (o.context->>'event_key') IS DISTINCT FROM (n.context->>'event_key')
  ),
  touched AS (
    SELECT o_t AS tenant_id, o_m AS member_id, o_k AS dedupe_key FROM changed
    UNION
    SELECT n_t, n_m, n_k FROM changed
  )
  SELECT array_agg(tenant_id), array_agg(member_id), array_agg(dedupe_key)
  INTO t, m, k
  FROM touched
  WHERE NULLIF(member_id, '') IS NOT NULL;
  PERFORM public.notification_unread_sync(t, m, k);
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.notification_unread_after_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid[]; m text[]; k text[];
BEGIN
  SELECT array_agg(s.tenant_id), array_agg(s.member_id), array_agg(s.dedupe_key)
  INTO t, m, k
  FROM (
    SELECT DISTINCT o.tenant_id, o.context->>'member_id' AS member_id,
           COALESCE(NULLIF(o.context->>'event_key', ''), o.notification_request_id::text) AS dedupe_key
    FROM old_rows o
    WHERE NULLIF(o.context->>'member_id', '') IS NOT NULL
      AND o.read_at IS NULL AND o.status <> 'read'
  ) s;
  PERFORM public.notification_unread_sync(t, m, k);
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.notification_unread_after_truncate()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
  DELETE FROM public.notification_member_unread_keys;
  UPDATE public.notification_member_unread_counts SET unread_count = 0, updated_at = now() WHERE unread_count <> 0;
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS notification_unread_ins ON public.notification_requests;
CREATE TRIGGER notification_unread_ins
AFTER INSERT ON public.notification_requests
REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT EXECUTE FUNCTION public.notification_unread_after_insert();

DROP TRIGGER IF EXISTS notification_unread_upd ON public.notification_requests;
CREATE TRIGGER notification_unread_upd
AFTER UPDATE ON public.notification_requests
REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows
FOR EACH STATEMENT EXECUTE FUNCTION public.notification_unread_after_update();

DROP TRIGGER IF EXISTS notification_unread_del ON public.notification_requests;
CREATE TRIGGER notification_unread_del
AFTER DELETE ON public.notification_requests
REFERENCING OLD TABLE AS old_rows
FOR EACH STATEMENT EXECUTE FUNCTION public.notification_unread_after_delete();

DROP TRIGGER IF EXISTS notification_unread_trunc ON public.notification_requests;
CREATE TRIGGER notification_unread_trunc
AFTER TRUNCATE ON public.notification_requests
FOR EACH STATEMENT EXECUTE FUNCTION public.notification_unread_after_truncate();

-- +goose Down
SET LOCAL lock_timeout = '5s';
DROP TRIGGER IF EXISTS notification_unread_trunc ON public.notification_requests;
DROP TRIGGER IF EXISTS notification_unread_del ON public.notification_requests;
DROP TRIGGER IF EXISTS notification_unread_upd ON public.notification_requests;
DROP TRIGGER IF EXISTS notification_unread_ins ON public.notification_requests;
DROP FUNCTION IF EXISTS public.notification_unread_after_truncate();
DROP FUNCTION IF EXISTS public.notification_unread_after_delete();
DROP FUNCTION IF EXISTS public.notification_unread_after_update();
DROP FUNCTION IF EXISTS public.notification_unread_after_insert();
DROP FUNCTION IF EXISTS public.notification_unread_sync(uuid[], text[], text[]);
DROP TABLE IF EXISTS public.notification_unread_counter_state;
DROP TABLE IF EXISTS public.notification_member_unread_counts;
DROP TABLE IF EXISTS public.notification_member_unread_keys;
