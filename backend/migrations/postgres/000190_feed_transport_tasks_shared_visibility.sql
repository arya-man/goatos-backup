-- Feed transport is shared park work, like feed distribution: one shed task can be
-- seen by every operator in the park. The submitter belongs on
-- feed_transport_attempts.operator_id, not on the task row.
SET lock_timeout = '5s';
SET statement_timeout = '30s';

UPDATE public.feed_transport_tasks
SET operator_id = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE operator_id IS NOT NULL;
