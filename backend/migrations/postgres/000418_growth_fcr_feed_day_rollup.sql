-- FCR feed-day rollup (perf, stg latency program 2026-09-24, P3/P25 + "compute-on-write").
--
-- The FCR tab (GET /growth-director/fcr) re-derived every feed cell of the window on every read:
-- feed_direction_issue_rows x feed_direction_issues (44k rows on the OCI clone), a latest-load
-- price per (park, item, day) from feed_purchases, and the per-day head counts -- about 300ms of
-- a 400ms statement, re-paid by the pens read. This table holds that answer once per
-- (tenant, park, pen, business day), so the request sums a few hundred indexed rows instead.
--
-- GRAIN: one row per (tenant_id, park_id, pen_shed_id, pen_key, feed_day) that has at least one
-- live issued/amended/locked feed-direction row. pen_key is the SAME scrub of the sheet's
-- generated partition_key the FCR bridge uses ('whole' -> '', 'part 3' -> '3', 'pt 3' -> '3').
-- Every column is additive over days (sums, counts) or decomposable (min), which is what lets a
-- segment [d_prev, d) and a window [from, to) be answered by summing day rows:
--   feed_kg        sum(quantity_kg) over rows with a quantity (NULL when every cell is blocked)
--   feed_cost      sum(quantity_kg * per_kg) over rows with a quantity AND a price
--   unpriced_kg    sum(quantity_kg) over rows with a quantity and NO price
--   blocked_cells  count of rows with NULL quantity (a blocked cell, never summed as zero)
--   head_days      sum over (shed_tag_key, breed_key) of max(head_count) that day
--   feed_label     min(partition_label) of the day's rows, '' and 'whole' excluded
-- per_kg is the same-farm latest load on or before the feed day
-- (COALESCE(per_kg_cost, total_cost / quantity_kg), newest purchase_date then batch_no).
--
-- FRESHNESS: every write to the three source tables appends a dirty range
-- (tenant, park, from_day) to growth_fcr_rollup_dirty IN THE WRITER'S TRANSACTION (statement
-- triggers below), so no writer -- API, importer, sheet sync, CLI -- can change FCR inputs without
-- the rollup knowing. The reader refreshes a dirty park before it reads (serialized per park by an
-- advisory lock), the API warm-up and the kernel housekeeping stage drain the log in the
-- background, and a daily reconcile recomputes everything and counts drift. A feed purchase marks
-- from its purchase_date because it can re-price every later day of that park.
--
-- BACKFILL: nothing is computed here. Every (tenant, park) that has feed issues is marked dirty
-- from its first feed day, and the first refresh builds it (chunked by month) outside the
-- migration; until then the reader builds it on demand. So the migration holds no long lock and
-- does no bulk write.

-- +goose Up
SET lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS public.growth_fcr_pen_feed_days (
  tenant_id      uuid        NOT NULL,
  park_id        uuid        NOT NULL,
  pen_shed_id    uuid        NOT NULL,
  pen_key        text        NOT NULL,
  feed_day       date        NOT NULL,
  feed_kg        numeric,
  feed_cost      double precision,
  unpriced_kg    numeric     NOT NULL DEFAULT 0,
  blocked_cells  integer     NOT NULL DEFAULT 0,
  head_days      numeric,
  feed_label     text,
  refreshed_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, park_id, pen_shed_id, pen_key, feed_day)
);

COMMENT ON TABLE public.growth_fcr_pen_feed_days IS
  'FCR feed-day rollup: one row per (tenant, park, pen, business day) of live feed-direction rows, priced at the same-farm latest load. Maintained incrementally from growth_fcr_rollup_dirty; see migration 000418.';

-- Window reads: tenant + park ANY + business-day range, then pen.
CREATE INDEX IF NOT EXISTS growth_fcr_pen_feed_days_window_idx
  ON public.growth_fcr_pen_feed_days (tenant_id, park_id, feed_day);

-- Append-only dirty log (insert-only, so concurrent feed writers never contend on one row).
CREATE TABLE IF NOT EXISTS public.growth_fcr_rollup_dirty (
  dirty_id   bigserial   PRIMARY KEY,
  tenant_id  uuid        NOT NULL,
  park_id    uuid        NOT NULL,
  from_day   date        NOT NULL,
  marked_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS growth_fcr_rollup_dirty_scope_idx
  ON public.growth_fcr_rollup_dirty (tenant_id, park_id);

-- High-churn queue table (P18): every refresh deletes what it drains.
ALTER TABLE public.growth_fcr_rollup_dirty SET (
  autovacuum_vacuum_scale_factor = 0.02,
  autovacuum_analyze_scale_factor = 0.02
);

-- Reconcile bookkeeping: one row per tenant, the last business day a full reconcile ran and the
-- drift it found (rows whose stored values differed from a fresh recompute).
-- no-mismatch-review-queue:ignore: owner=ravi issue=vgoats/goatos#389 scope=rollup-recompute-checkpoint-not-an-ingestion-review-queue expiry=2027-09-24
CREATE TABLE IF NOT EXISTS public.growth_fcr_rollup_reconcile (
  tenant_id        uuid        PRIMARY KEY,
  reconciled_on    date        NOT NULL,
  drift_rows       integer     NOT NULL DEFAULT 0,
  reconciled_at    timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.growth_fcr_mark_dirty_from_issues() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP IN ('INSERT', 'UPDATE') THEN
    INSERT INTO public.growth_fcr_rollup_dirty (tenant_id, park_id, from_day)
    SELECT n.tenant_id, n.park_id, min(n.feed_day) FROM new_rows n GROUP BY n.tenant_id, n.park_id;
  END IF;
  IF TG_OP IN ('UPDATE', 'DELETE') THEN
    INSERT INTO public.growth_fcr_rollup_dirty (tenant_id, park_id, from_day)
    SELECT o.tenant_id, o.park_id, min(o.feed_day) FROM old_rows o GROUP BY o.tenant_id, o.park_id;
  END IF;
  RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- The trigger functions only MARK ranges; they compute no report figure.
-- projection-review: membership=the rows the triggering statement changed (its transition table); group_key=(tenant_id, park_id) with min(feed_day / purchase_date); join_cardinality=an issue row joins exactly one feed_direction_issues row on its (tenant, issue id) key, so the join adds no rows; pagination=NONE, bounded by one statement's changed rows; scope=tenant + park of the changed rows.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.growth_fcr_mark_dirty_from_issue_rows() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  -- A row carries its park; its business day is its issue's. An issue deleted in the same
  -- statement (cascade) has already marked itself through the issues trigger.
  IF TG_OP IN ('INSERT', 'UPDATE') THEN
    INSERT INTO public.growth_fcr_rollup_dirty (tenant_id, park_id, from_day)
    SELECT n.tenant_id, i.park_id, min(i.feed_day)
    FROM new_rows n
    JOIN public.feed_direction_issues i
      ON i.tenant_id = n.tenant_id AND i.feed_direction_issue_id = n.feed_direction_issue_id
    GROUP BY n.tenant_id, i.park_id;
  END IF;
  IF TG_OP IN ('UPDATE', 'DELETE') THEN
    INSERT INTO public.growth_fcr_rollup_dirty (tenant_id, park_id, from_day)
    SELECT o.tenant_id, i.park_id, min(i.feed_day)
    FROM old_rows o
    JOIN public.feed_direction_issues i
      ON i.tenant_id = o.tenant_id AND i.feed_direction_issue_id = o.feed_direction_issue_id
    GROUP BY o.tenant_id, i.park_id;
  END IF;
  RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.growth_fcr_mark_dirty_from_purchases() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  -- A load re-prices every later feed day of its park. A park-less purchase prices nothing
  -- (the FCR price joins on the feed row's own park), so it marks nothing.
  IF TG_OP IN ('INSERT', 'UPDATE') THEN
    INSERT INTO public.growth_fcr_rollup_dirty (tenant_id, park_id, from_day)
    SELECT n.tenant_id, n.park_id, min(n.purchase_date) FROM new_rows n
    WHERE n.park_id IS NOT NULL GROUP BY n.tenant_id, n.park_id;
  END IF;
  IF TG_OP IN ('UPDATE', 'DELETE') THEN
    INSERT INTO public.growth_fcr_rollup_dirty (tenant_id, park_id, from_day)
    SELECT o.tenant_id, o.park_id, min(o.purchase_date) FROM old_rows o
    WHERE o.park_id IS NOT NULL GROUP BY o.tenant_id, o.park_id;
  END IF;
  RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- Statement-level triggers with transition tables: one INSERT per statement regardless of how
-- many rows it touched (a sheet issue writes hundreds of rows in one statement).
DROP TRIGGER IF EXISTS growth_fcr_dirty_issues_ins ON public.feed_direction_issues;
CREATE TRIGGER growth_fcr_dirty_issues_ins AFTER INSERT ON public.feed_direction_issues
  REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.growth_fcr_mark_dirty_from_issues();
DROP TRIGGER IF EXISTS growth_fcr_dirty_issues_upd ON public.feed_direction_issues;
CREATE TRIGGER growth_fcr_dirty_issues_upd AFTER UPDATE ON public.feed_direction_issues
  REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.growth_fcr_mark_dirty_from_issues();
DROP TRIGGER IF EXISTS growth_fcr_dirty_issues_del ON public.feed_direction_issues;
CREATE TRIGGER growth_fcr_dirty_issues_del AFTER DELETE ON public.feed_direction_issues
  REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION public.growth_fcr_mark_dirty_from_issues();

DROP TRIGGER IF EXISTS growth_fcr_dirty_issue_rows_ins ON public.feed_direction_issue_rows;
CREATE TRIGGER growth_fcr_dirty_issue_rows_ins AFTER INSERT ON public.feed_direction_issue_rows
  REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.growth_fcr_mark_dirty_from_issue_rows();
DROP TRIGGER IF EXISTS growth_fcr_dirty_issue_rows_upd ON public.feed_direction_issue_rows;
CREATE TRIGGER growth_fcr_dirty_issue_rows_upd AFTER UPDATE ON public.feed_direction_issue_rows
  REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.growth_fcr_mark_dirty_from_issue_rows();
DROP TRIGGER IF EXISTS growth_fcr_dirty_issue_rows_del ON public.feed_direction_issue_rows;
CREATE TRIGGER growth_fcr_dirty_issue_rows_del AFTER DELETE ON public.feed_direction_issue_rows
  REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION public.growth_fcr_mark_dirty_from_issue_rows();

DROP TRIGGER IF EXISTS growth_fcr_dirty_purchases_ins ON public.feed_purchases;
CREATE TRIGGER growth_fcr_dirty_purchases_ins AFTER INSERT ON public.feed_purchases
  REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.growth_fcr_mark_dirty_from_purchases();
DROP TRIGGER IF EXISTS growth_fcr_dirty_purchases_upd ON public.feed_purchases;
CREATE TRIGGER growth_fcr_dirty_purchases_upd AFTER UPDATE ON public.feed_purchases
  REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.growth_fcr_mark_dirty_from_purchases();
DROP TRIGGER IF EXISTS growth_fcr_dirty_purchases_del ON public.feed_purchases;
CREATE TRIGGER growth_fcr_dirty_purchases_del AFTER DELETE ON public.feed_purchases
  REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION public.growth_fcr_mark_dirty_from_purchases();

-- Backfill marker only (see header): build on first refresh, outside this migration.
INSERT INTO public.growth_fcr_rollup_dirty (tenant_id, park_id, from_day)
SELECT i.tenant_id, i.park_id, min(i.feed_day)
FROM public.feed_direction_issues i
GROUP BY i.tenant_id, i.park_id;

-- +goose Down
DROP TRIGGER IF EXISTS growth_fcr_dirty_purchases_del ON public.feed_purchases;
DROP TRIGGER IF EXISTS growth_fcr_dirty_purchases_upd ON public.feed_purchases;
DROP TRIGGER IF EXISTS growth_fcr_dirty_purchases_ins ON public.feed_purchases;
DROP TRIGGER IF EXISTS growth_fcr_dirty_issue_rows_del ON public.feed_direction_issue_rows;
DROP TRIGGER IF EXISTS growth_fcr_dirty_issue_rows_upd ON public.feed_direction_issue_rows;
DROP TRIGGER IF EXISTS growth_fcr_dirty_issue_rows_ins ON public.feed_direction_issue_rows;
DROP TRIGGER IF EXISTS growth_fcr_dirty_issues_del ON public.feed_direction_issues;
DROP TRIGGER IF EXISTS growth_fcr_dirty_issues_upd ON public.feed_direction_issues;
DROP TRIGGER IF EXISTS growth_fcr_dirty_issues_ins ON public.feed_direction_issues;
DROP FUNCTION IF EXISTS public.growth_fcr_mark_dirty_from_purchases();
DROP FUNCTION IF EXISTS public.growth_fcr_mark_dirty_from_issue_rows();
DROP FUNCTION IF EXISTS public.growth_fcr_mark_dirty_from_issues();
DROP TABLE IF EXISTS public.growth_fcr_rollup_reconcile;
DROP TABLE IF EXISTS public.growth_fcr_rollup_dirty;
DROP TABLE IF EXISTS public.growth_fcr_pen_feed_days;
