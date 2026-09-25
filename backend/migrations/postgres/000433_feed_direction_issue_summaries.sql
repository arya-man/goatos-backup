-- +goose Up
--
-- STORED PER-ISSUE COLLAPSES OF feed_direction_issue_rows FOR THE FEED ANALYTICS READS.
--
-- GET /feed-analytics/directed and GET /feed-analytics/execution re-totalled every frozen sheet
-- cell of the window on every request: ~50k of the 87k feed_direction_issue_rows on STG for a
-- 30-day window, hash-aggregated (and spilled to disk at the 4 MB work_mem) to pen, item and
-- session grains -- 150-215 ms per statement, the slowest reads on both routes. Banned pattern #1
-- in docs/perf/2026-09-24-stg-latency/README.md (re-aggregating a growing history per request).
--
-- A frozen sheet's rows only change when that ISSUE is issued, re-issued or amended, so each
-- table below holds one issue's rows already collapsed to the grain a read needs. Every column is
-- a SUM / MIN / MAX of that issue's rows with quantity_kg IS NOT NULL (blocked cells never
-- contribute), so a read that combines several issues of one day (normal + experiment sheets)
-- re-aggregates with the same function and gets exactly the old answer:
--
--   feed_direction_issue_pens           (issue, shed, partition_key, shed_tag_key, breed_key)
--       quantity_kg = SUM(quantity_kg), head_count = MAX(head_count), shed_tag = MIN(shed_tag)
--   feed_direction_issue_items          (issue, feed_item_key)
--       quantity_kg = SUM(quantity_kg), feed_item_label = MIN(feed_item_label),
--       head_count  = SUM over the issue's pens of that pen-item's MAX(head_count)
--   feed_direction_issue_tag_items      (issue, shed_tag_key, feed_item_key)
--       quantity_kg = SUM(quantity_kg)
--   feed_direction_issue_session_items  (issue, shed, partition_key, session_no, workflow, feed_item_key)
--       quantity_kg = SUM(quantity_kg), session_label = MAX(session_label),
--       breed_min / breed_max = MIN / MAX(COALESCE(NULLIF(breed, ''), 'Unspecified'))
--
-- projection-review: membership=feed_direction_issue_rows with quantity_kg IS NOT NULL of ONE issue, reached by the (tenant_id, feed_direction_issue_id) prefix of the natural key; group_key=each table's primary key, which is that issue's rows collapsed to the named grain (pen, item, pen tag x item, session x item) -- every grain is a subset of the natural key (tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, shed_tag_key, breed_key, feed_item_key) plus workflow, so no row lands in two groups; join_cardinality=the only join is rows to the unnest of touched (tenant, issue) keys, 1:1 per key (DISTINCT before unnest), and the items head count pre-aggregates pen-items (MAX over sessions) before summing over pens so a pen's heads never repeat per session; pagination=none, whole-issue re-derive with no limit/offset; scope=tenant_id + feed_direction_issue_id on every statement, no cross-tenant read
--
-- MAINTENANCE: statement-level AFTER INSERT / UPDATE / DELETE triggers on
-- feed_direction_issue_rows re-derive every touched issue from its remaining rows in the writer's
-- own transaction (a re-issue deletes and re-inserts an issue's rows; an amend upserts and
-- deletes cells; an issue delete cascades). The triggers first lock the touched
-- feed_direction_issues rows in key order -- the lock every writer (IssueRepository) already
-- takes first -- so two writers of one issue re-derive one after the other and the later one
-- sees the earlier one's committed rows. No issue-level column is copied: state, workflow,
-- feed_day and park_id are read from feed_direction_issues at query time.
--
-- LOCKS / BACKFILL: the triggers are created BEFORE the backfill in this one transaction.
-- CREATE TRIGGER holds SHARE ROW EXCLUSIVE on feed_direction_issue_rows until commit, so no row
-- can be written between the backfill snapshot and the triggers going live; the backfill of the
-- 87k STG rows takes well under a second. lock_timeout bounds the wait for that lock.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '120s';

-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=derived-per-issue-collapse-of-frozen-sheet-rows-maintained-by-trigger expiry=2026-11-30
-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=feed-analytics-sheet-summary-not-a-reconciliation-queue expiry=2026-11-30
CREATE TABLE IF NOT EXISTS public.feed_direction_issue_pens (
  tenant_id uuid NOT NULL,
  feed_direction_issue_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  partition_key text NOT NULL,
  shed_tag_key text NOT NULL,
  breed_key text NOT NULL,
  shed_tag text NOT NULL,
  quantity_kg numeric NOT NULL,
  head_count bigint NOT NULL,
  PRIMARY KEY (tenant_id, feed_direction_issue_id, shed_id, partition_key, shed_tag_key, breed_key)
);

CREATE TABLE IF NOT EXISTS public.feed_direction_issue_items (
  tenant_id uuid NOT NULL,
  feed_direction_issue_id uuid NOT NULL,
  feed_item_key text NOT NULL,
  feed_item_label text NOT NULL,
  quantity_kg numeric NOT NULL,
  head_count numeric NOT NULL,
  PRIMARY KEY (tenant_id, feed_direction_issue_id, feed_item_key)
);

CREATE TABLE IF NOT EXISTS public.feed_direction_issue_tag_items (
  tenant_id uuid NOT NULL,
  feed_direction_issue_id uuid NOT NULL,
  shed_tag_key text NOT NULL,
  feed_item_key text NOT NULL,
  quantity_kg numeric NOT NULL,
  PRIMARY KEY (tenant_id, feed_direction_issue_id, shed_tag_key, feed_item_key)
);

CREATE TABLE IF NOT EXISTS public.feed_direction_issue_session_items (
  tenant_id uuid NOT NULL,
  feed_direction_issue_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  partition_key text NOT NULL,
  session_no integer NOT NULL,
  workflow text NOT NULL,
  feed_item_key text NOT NULL,
  quantity_kg numeric NOT NULL,
  session_label text NOT NULL,
  breed_min text NOT NULL,
  breed_max text NOT NULL,
  PRIMARY KEY (tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, workflow, feed_item_key)
);

-- Re-derive the four collapses for the given issues from their current rows.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_direction_issue_summaries_rederive(p_tenant_ids uuid[], p_issue_ids uuid[])
RETURNS void
LANGUAGE plpgsql
AS $fn$
BEGIN
  -- Serialize with every other writer of these issues (IssueRepository locks the issue first).
  PERFORM 1 FROM public.feed_direction_issues i
  JOIN unnest(p_tenant_ids, p_issue_ids) AS k(tenant_id, issue_id)
    ON i.tenant_id = k.tenant_id AND i.feed_direction_issue_id = k.issue_id
  ORDER BY i.tenant_id, i.feed_direction_issue_id
  FOR UPDATE OF i;

  DELETE FROM public.feed_direction_issue_pens t
  USING unnest(p_tenant_ids, p_issue_ids) AS k(tenant_id, issue_id)
  WHERE t.tenant_id = k.tenant_id AND t.feed_direction_issue_id = k.issue_id;
  DELETE FROM public.feed_direction_issue_items t
  USING unnest(p_tenant_ids, p_issue_ids) AS k(tenant_id, issue_id)
  WHERE t.tenant_id = k.tenant_id AND t.feed_direction_issue_id = k.issue_id;
  DELETE FROM public.feed_direction_issue_tag_items t
  USING unnest(p_tenant_ids, p_issue_ids) AS k(tenant_id, issue_id)
  WHERE t.tenant_id = k.tenant_id AND t.feed_direction_issue_id = k.issue_id;
  DELETE FROM public.feed_direction_issue_session_items t
  USING unnest(p_tenant_ids, p_issue_ids) AS k(tenant_id, issue_id)
  WHERE t.tenant_id = k.tenant_id AND t.feed_direction_issue_id = k.issue_id;

  INSERT INTO public.feed_direction_issue_pens
    (tenant_id, feed_direction_issue_id, shed_id, partition_key, shed_tag_key, breed_key, shed_tag, quantity_kg, head_count)
  SELECT tenant_id, feed_direction_issue_id, shed_id, partition_key, shed_tag_key, breed_key,
         MIN(shed_tag), SUM(quantity_kg), MAX(head_count)
  FROM (
    SELECT r.* FROM public.feed_direction_issue_rows r
    JOIN unnest(p_tenant_ids, p_issue_ids) AS k(tenant_id, issue_id)
      ON r.tenant_id = k.tenant_id AND r.feed_direction_issue_id = k.issue_id
    WHERE r.quantity_kg IS NOT NULL
  ) src
  GROUP BY tenant_id, feed_direction_issue_id, shed_id, partition_key, shed_tag_key, breed_key;

  INSERT INTO public.feed_direction_issue_items
    (tenant_id, feed_direction_issue_id, feed_item_key, feed_item_label, quantity_kg, head_count)
  SELECT tenant_id, feed_direction_issue_id, feed_item_key, MIN(feed_item_label), SUM(kg), SUM(heads)
  FROM (
    SELECT tenant_id, feed_direction_issue_id, feed_item_key,
           MIN(feed_item_label) AS feed_item_label, SUM(quantity_kg) AS kg, MAX(head_count) AS heads
    FROM (
    SELECT r.* FROM public.feed_direction_issue_rows r
    JOIN unnest(p_tenant_ids, p_issue_ids) AS k(tenant_id, issue_id)
      ON r.tenant_id = k.tenant_id AND r.feed_direction_issue_id = k.issue_id
    WHERE r.quantity_kg IS NOT NULL
  ) src
    GROUP BY tenant_id, feed_direction_issue_id, feed_item_key, shed_id, partition_key, shed_tag_key, breed_key
  ) pen_item
  GROUP BY tenant_id, feed_direction_issue_id, feed_item_key;

  INSERT INTO public.feed_direction_issue_tag_items
    (tenant_id, feed_direction_issue_id, shed_tag_key, feed_item_key, quantity_kg)
  SELECT tenant_id, feed_direction_issue_id, shed_tag_key, feed_item_key, SUM(quantity_kg)
  FROM (
    SELECT r.* FROM public.feed_direction_issue_rows r
    JOIN unnest(p_tenant_ids, p_issue_ids) AS k(tenant_id, issue_id)
      ON r.tenant_id = k.tenant_id AND r.feed_direction_issue_id = k.issue_id
    WHERE r.quantity_kg IS NOT NULL
  ) src
  GROUP BY tenant_id, feed_direction_issue_id, shed_tag_key, feed_item_key;

  INSERT INTO public.feed_direction_issue_session_items
    (tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, workflow, feed_item_key,
     quantity_kg, session_label, breed_min, breed_max)
  SELECT tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, workflow, feed_item_key,
         SUM(quantity_kg), MAX(session_label),
         MIN(COALESCE(NULLIF(breed, ''), 'Unspecified')), MAX(COALESCE(NULLIF(breed, ''), 'Unspecified'))
  FROM (
    SELECT r.* FROM public.feed_direction_issue_rows r
    JOIN unnest(p_tenant_ids, p_issue_ids) AS k(tenant_id, issue_id)
      ON r.tenant_id = k.tenant_id AND r.feed_direction_issue_id = k.issue_id
    WHERE r.quantity_kg IS NOT NULL
  ) src
  GROUP BY tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, workflow, feed_item_key;
END;
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_direction_issue_summaries_from_new()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid[];
  i uuid[];
BEGIN
  SELECT array_agg(tenant_id ORDER BY tenant_id, feed_direction_issue_id),
         array_agg(feed_direction_issue_id ORDER BY tenant_id, feed_direction_issue_id)
  INTO t, i
  FROM (SELECT DISTINCT tenant_id, feed_direction_issue_id FROM new_rows) k;
  IF i IS NOT NULL THEN
    PERFORM public.feed_direction_issue_summaries_rederive(t, i);
  END IF;
  RETURN NULL;
END;
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_direction_issue_summaries_from_old()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid[];
  i uuid[];
BEGIN
  SELECT array_agg(tenant_id ORDER BY tenant_id, feed_direction_issue_id),
         array_agg(feed_direction_issue_id ORDER BY tenant_id, feed_direction_issue_id)
  INTO t, i
  FROM (SELECT DISTINCT tenant_id, feed_direction_issue_id FROM old_rows) k;
  IF i IS NOT NULL THEN
    PERFORM public.feed_direction_issue_summaries_rederive(t, i);
  END IF;
  RETURN NULL;
END;
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_direction_issue_summaries_from_old_new()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid[];
  i uuid[];
BEGIN
  SELECT array_agg(tenant_id ORDER BY tenant_id, feed_direction_issue_id),
         array_agg(feed_direction_issue_id ORDER BY tenant_id, feed_direction_issue_id)
  INTO t, i
  FROM (
    SELECT tenant_id, feed_direction_issue_id FROM old_rows
    UNION
    SELECT tenant_id, feed_direction_issue_id FROM new_rows
  ) k;
  IF i IS NOT NULL THEN
    PERFORM public.feed_direction_issue_summaries_rederive(t, i);
  END IF;
  RETURN NULL;
END;
$fn$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS feed_direction_issue_summaries_ins ON public.feed_direction_issue_rows;
CREATE TRIGGER feed_direction_issue_summaries_ins
  AFTER INSERT ON public.feed_direction_issue_rows
  REFERENCING NEW TABLE AS new_rows
  FOR EACH STATEMENT EXECUTE FUNCTION public.feed_direction_issue_summaries_from_new();

DROP TRIGGER IF EXISTS feed_direction_issue_summaries_upd ON public.feed_direction_issue_rows;
CREATE TRIGGER feed_direction_issue_summaries_upd
  AFTER UPDATE ON public.feed_direction_issue_rows
  REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows
  FOR EACH STATEMENT EXECUTE FUNCTION public.feed_direction_issue_summaries_from_old_new();

DROP TRIGGER IF EXISTS feed_direction_issue_summaries_del ON public.feed_direction_issue_rows;
CREATE TRIGGER feed_direction_issue_summaries_del
  AFTER DELETE ON public.feed_direction_issue_rows
  REFERENCING OLD TABLE AS old_rows
  FOR EACH STATEMENT EXECUTE FUNCTION public.feed_direction_issue_summaries_from_old();

-- REBUILD: tools/dev/seed-closeout.sh (run_derived_summary_rebuilds) calls
-- feed_direction_issue_summaries_rebuild(tenant) to re-derive every issue of one tenant
-- through the same rederive function the triggers use; a TRUNCATE of the source rows
-- empties the four summaries.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_direction_issue_summaries_rebuild(p_tenant_id uuid)
RETURNS bigint
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid[];
  i uuid[];
BEGIN
  SELECT array_agg(tenant_id ORDER BY feed_direction_issue_id),
         array_agg(feed_direction_issue_id ORDER BY feed_direction_issue_id)
  INTO t, i
  FROM public.feed_direction_issues
  WHERE tenant_id = p_tenant_id;
  -- Clear summary rows whose issue no longer exists, then re-derive the live issues.
  DELETE FROM public.feed_direction_issue_pens WHERE tenant_id = p_tenant_id;
  DELETE FROM public.feed_direction_issue_items WHERE tenant_id = p_tenant_id;
  DELETE FROM public.feed_direction_issue_tag_items WHERE tenant_id = p_tenant_id;
  DELETE FROM public.feed_direction_issue_session_items WHERE tenant_id = p_tenant_id;
  IF i IS NOT NULL THEN
    PERFORM public.feed_direction_issue_summaries_rederive(t, i);
  END IF;
  RETURN COALESCE(cardinality(i), 0);
END;
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_direction_issue_summaries_truncate()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
  TRUNCATE public.feed_direction_issue_pens, public.feed_direction_issue_items,
           public.feed_direction_issue_tag_items, public.feed_direction_issue_session_items;
  RETURN NULL;
END;
$fn$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS feed_direction_issue_summaries_trunc ON public.feed_direction_issue_rows;
CREATE TRIGGER feed_direction_issue_summaries_trunc
  AFTER TRUNCATE ON public.feed_direction_issue_rows
  FOR EACH STATEMENT EXECUTE FUNCTION public.feed_direction_issue_summaries_truncate();

-- Backfill every existing issue (the triggers above already hold the table lock).
INSERT INTO public.feed_direction_issue_pens
  (tenant_id, feed_direction_issue_id, shed_id, partition_key, shed_tag_key, breed_key, shed_tag, quantity_kg, head_count)
SELECT tenant_id, feed_direction_issue_id, shed_id, partition_key, shed_tag_key, breed_key,
       MIN(shed_tag), SUM(quantity_kg), MAX(head_count)
FROM public.feed_direction_issue_rows
WHERE quantity_kg IS NOT NULL
GROUP BY tenant_id, feed_direction_issue_id, shed_id, partition_key, shed_tag_key, breed_key
ON CONFLICT DO NOTHING;

INSERT INTO public.feed_direction_issue_items
  (tenant_id, feed_direction_issue_id, feed_item_key, feed_item_label, quantity_kg, head_count)
SELECT tenant_id, feed_direction_issue_id, feed_item_key, MIN(feed_item_label), SUM(kg), SUM(heads)
FROM (
  SELECT tenant_id, feed_direction_issue_id, feed_item_key,
         MIN(feed_item_label) AS feed_item_label, SUM(quantity_kg) AS kg, MAX(head_count) AS heads
  FROM public.feed_direction_issue_rows
  WHERE quantity_kg IS NOT NULL
  GROUP BY tenant_id, feed_direction_issue_id, feed_item_key, shed_id, partition_key, shed_tag_key, breed_key
) pen_item
GROUP BY tenant_id, feed_direction_issue_id, feed_item_key
ON CONFLICT DO NOTHING;

INSERT INTO public.feed_direction_issue_tag_items
  (tenant_id, feed_direction_issue_id, shed_tag_key, feed_item_key, quantity_kg)
SELECT tenant_id, feed_direction_issue_id, shed_tag_key, feed_item_key, SUM(quantity_kg)
FROM public.feed_direction_issue_rows
WHERE quantity_kg IS NOT NULL
GROUP BY tenant_id, feed_direction_issue_id, shed_tag_key, feed_item_key
ON CONFLICT DO NOTHING;

INSERT INTO public.feed_direction_issue_session_items
  (tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, workflow, feed_item_key,
   quantity_kg, session_label, breed_min, breed_max)
SELECT tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, workflow, feed_item_key,
       SUM(quantity_kg), MAX(session_label),
       MIN(COALESCE(NULLIF(breed, ''), 'Unspecified')), MAX(COALESCE(NULLIF(breed, ''), 'Unspecified'))
FROM public.feed_direction_issue_rows
WHERE quantity_kg IS NOT NULL
GROUP BY tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, workflow, feed_item_key
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TRIGGER IF EXISTS feed_direction_issue_summaries_trunc ON public.feed_direction_issue_rows;
DROP FUNCTION IF EXISTS public.feed_direction_issue_summaries_truncate();
DROP FUNCTION IF EXISTS public.feed_direction_issue_summaries_rebuild(uuid);
DROP TRIGGER IF EXISTS feed_direction_issue_summaries_del ON public.feed_direction_issue_rows;
DROP TRIGGER IF EXISTS feed_direction_issue_summaries_upd ON public.feed_direction_issue_rows;
DROP TRIGGER IF EXISTS feed_direction_issue_summaries_ins ON public.feed_direction_issue_rows;
DROP FUNCTION IF EXISTS public.feed_direction_issue_summaries_from_old_new();
DROP FUNCTION IF EXISTS public.feed_direction_issue_summaries_from_old();
DROP FUNCTION IF EXISTS public.feed_direction_issue_summaries_from_new();
DROP FUNCTION IF EXISTS public.feed_direction_issue_summaries_rederive(uuid[], uuid[]);
DROP TABLE IF EXISTS public.feed_direction_issue_session_items;
DROP TABLE IF EXISTS public.feed_direction_issue_tag_items;
DROP TABLE IF EXISTS public.feed_direction_issue_items;
DROP TABLE IF EXISTS public.feed_direction_issue_pens;
