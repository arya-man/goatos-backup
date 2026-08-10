-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: feed transport operational-location rollout repair only; no seed contract change
--
-- Forward repair for databases that applied the earlier unsafe 000143 body. Rows touched by that
-- legacy whole-shed cleanup are identifiable because runtime completion always sets completed_at,
-- while the unsafe migration only set status='completed'. Fresh databases converge in 000143;
-- this migration keeps upgraded databases at the same final state.
-- Reconstruct submitted work from immutable attempt history instead of inventing proof. Unstarted
-- whole-shed work is retired because it has no truthful partition identity; the partition-aware
-- materializer creates its replacement rows.

SET lock_timeout = '5s';

ALTER TABLE public.feed_transport_tasks
  DROP CONSTRAINT IF EXISTS feed_transport_tasks_status_check;

ALTER TABLE public.feed_transport_tasks
  ADD CONSTRAINT feed_transport_tasks_status_check
  CHECK (status IN ('due','verification_due','rework','completed','retired')) NOT VALID;

ALTER TABLE public.feed_transport_tasks
  VALIDATE CONSTRAINT feed_transport_tasks_status_check;

RESET lock_timeout;

WITH partitioned_sheds AS (
  SELECT DISTINCT sp.tenant_id, sp.shed_id
  FROM public.shed_partitions sp
  WHERE sp.status = 'active'
    AND COALESCE(NULLIF(BTRIM(sp.partition_label), ''), 'whole') <> 'whole'
),
desired AS (
  SELECT
    task.tenant_id,
    task.task_id,
    CASE
      WHEN attempt.status = 'verification_due' THEN 'verification_due'
      WHEN attempt.status = 'rejected' THEN 'rework'
      WHEN task.current_attempt_id IS NULL THEN 'retired'
      ELSE 'rework'
    END AS desired_status
  FROM public.feed_transport_tasks task
  JOIN partitioned_sheds ps
    ON ps.tenant_id = task.tenant_id
   AND ps.shed_id = task.shed_id
  LEFT JOIN public.feed_transport_attempts attempt
    ON attempt.tenant_id = task.tenant_id
   AND attempt.attempt_id = task.current_attempt_id
  WHERE COALESCE(NULLIF(BTRIM(task.partition_label), ''), 'whole') = 'whole'
    AND task.completed_at IS NULL
    AND (
      task.status = 'completed'
      OR (task.status = 'due' AND task.current_attempt_id IS NULL)
    )
)
UPDATE public.feed_transport_tasks task
SET status = desired.desired_status,
    updated_at = now(),
    row_version = task.row_version + 1
FROM desired
WHERE task.tenant_id = desired.tenant_id
  AND task.task_id = desired.task_id
  AND task.status IS DISTINCT FROM desired.desired_status;

-- +goose Down
-- +goose NO TRANSACTION
-- Forward data repair only.
