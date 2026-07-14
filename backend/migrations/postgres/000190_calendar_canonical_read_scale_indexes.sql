-- Migration 000190: Scale-certified indexes for canonical calendar reads (5k-50k envelope).
--
-- The calendarCanonicalListSQL query at scale (100k-500k obligations) performs seq-scans on
-- obligation_instances due to a non-sargable 3-way OR condition:
--   (oi.due_at >= $2 AND oi.due_at < $3)            -- window-bound path (most common)
--   OR oi.status IN ('missed','in_progress','deferred')  -- exception catch-up (out-of-window)
--   OR (oi.status IN ('scheduled','due') AND oi.due_at < now()) -- overdue-by-due_at
--
-- A single index cannot optimize all three branches together. This migration creates partial
-- indexes to make each branch indexable. The planner will use a BitmapOr of these indexes
-- instead of a seq scan.
--
-- See: docs/decisions/operational-kernel-5k-50k-scale-envelope.md (scale gate: calendar read index-bound).

-- +goose Up
-- +goose NO TRANSACTION

-- Index for window-bound path (obligated rows within the requested date window).
-- Most production calendar calls are 1-week views, making this the primary path.
-- Serves BOTH base CTEs' window branches:
--   * obligation_events window branch (adds an extra `status <> 'completed'` filter on top), and
--   * obligation_drive_membership window branch (which MUST include 'completed' rows so the
--     drive_summary completed_count / animal-coverage aggregates stay exact).
-- Therefore this partial index excludes ONLY the terminal-drop states (waived/canceled/superseded)
-- and keeps 'completed'; obligation_events layers its own completed-exclusion as a cheap filter.
CREATE INDEX CONCURRENTLY idx_obligation_instances_calendar_window
  ON obligation_instances (tenant_id, due_at)
  WHERE batch_id IS NULL
    AND status NOT IN ('waived', 'canceled', 'superseded');

-- Index for exception catch-up path (missed/in_progress/deferred rows visible outside window).
-- These rows bypass the date-window filter to remain visible in the calendar.
CREATE INDEX CONCURRENTLY idx_obligation_instances_calendar_exceptions
  ON obligation_instances (tenant_id, status)
  WHERE batch_id IS NULL
    AND status IN ('missed', 'in_progress', 'deferred');

-- Index for overdue-by-due_at path (scheduled/due rows that are now overdue).
-- Combines status filter and due_at comparison for rows past their due date.
CREATE INDEX CONCURRENTLY idx_obligation_instances_calendar_overdue
  ON obligation_instances (tenant_id, status, due_at)
  WHERE batch_id IS NULL
    AND status IN ('scheduled', 'due');

-- Index for the sop_events CTE join (sop_tasks -> obligation_instances ON oi.sop_task_id = st.task_id).
-- Without it that join plans a full seq scan of obligation_instances as the inner side of the nested
-- loop. sop_task_id is NULL for most obligations, so a partial index (NOT NULL) is small and selective.
CREATE INDEX CONCURRENTLY idx_obligation_instances_sop_task
  ON obligation_instances (tenant_id, sop_task_id)
  WHERE sop_task_id IS NOT NULL;

-- NOTE on obligation_batches: the batched-drive window path (batch_events CTE + the batched branch of
-- obligation_drive_membership) filters on COALESCE(window_start, planned_date::timestamptz, window_end).
-- That expression is NOT IMMUTABLE (date->timestamptz depends on session TimeZone) so it cannot back an
-- index. obligation_batches is intentionally low-cardinality (one row per park-day aggregate drive, not
-- per animal), so a seq scan there is cheap and correct; the 5k-50k row volume that mattered lives in
-- obligation_instances, which the three indexes above make fully index-bound.

-- +goose Down
-- +goose NO TRANSACTION

DROP INDEX CONCURRENTLY IF EXISTS idx_obligation_instances_calendar_window;
DROP INDEX CONCURRENTLY IF EXISTS idx_obligation_instances_calendar_exceptions;
DROP INDEX CONCURRENTLY IF EXISTS idx_obligation_instances_calendar_overdue;
DROP INDEX CONCURRENTLY IF EXISTS idx_obligation_instances_sop_task;
