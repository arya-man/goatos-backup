-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: feed transport operational-location rollout repair only; no seed contract change
--
-- Forward repair for 000143. Some databases may already have applied 000143, so do not rewrite
-- that migration. Rows touched by 000143's legacy whole-shed cleanup are identifiable because
-- runtime completion always sets completed_at, while the migration only set status='completed'.
-- Reconstruct the actionable state from immutable attempt history instead of inventing proof.

SET lock_timeout = '5s';

ALTER TABLE public.feed_transport_tasks
  DROP CONSTRAINT IF EXISTS feed_transport_tasks_status_check;

ALTER TABLE public.feed_transport_tasks
  ADD CONSTRAINT feed_transport_tasks_status_check
  CHECK (status IN ('due','verification_due','rework','completed','retired')) NOT VALID;

RESET lock_timeout;

WITH partitioned_sheds AS (
  SELECT DISTINCT sp.tenant_id, sp.shed_id
  FROM public.shed_partitions sp
  WHERE sp.status = 'active'
    AND COALESCE(NULLIF(BTRIM(sp.partition_label), ''), 'whole') <> 'whole'
),
recovered AS (
  SELECT
    task.tenant_id,
    task.task_id,
    CASE
      WHEN attempt.status = 'verification_due' THEN 'verification_due'
      WHEN attempt.status = 'rejected' THEN 'rework'
      WHEN task.current_attempt_id IS NULL THEN 'due'
      ELSE 'rework'
    END AS recovered_status
  FROM public.feed_transport_tasks task
  JOIN partitioned_sheds ps
    ON ps.tenant_id = task.tenant_id
   AND ps.shed_id = task.shed_id
  LEFT JOIN public.feed_transport_attempts attempt
    ON attempt.tenant_id = task.tenant_id
   AND attempt.attempt_id = task.current_attempt_id
  WHERE task.status = 'completed'
    AND task.completed_at IS NULL
    AND COALESCE(NULLIF(BTRIM(task.partition_label), ''), 'whole') = 'whole'
)
UPDATE public.feed_transport_tasks task
SET status = recovered.recovered_status,
    updated_at = now(),
    row_version = row_version + 1
FROM recovered
WHERE task.tenant_id = recovered.tenant_id
  AND task.task_id = recovered.task_id
  AND task.status IS DISTINCT FROM recovered.recovered_status;

-- +goose Down
-- +goose NO TRANSACTION
-- Forward data repair only.
