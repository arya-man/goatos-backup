-- +goose Up
-- Migration 000044 reopens premature ORS-2 actions. PostgreSQL data-modifying CTE subqueries read
-- the statement-start snapshot, so its same-statement rollup can retain the pre-reopen 6/6 card.
-- Repair only that exact inconsistent read-model shape in a fresh statement.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- projection-review: producer unique = workflow_actions(workflow_id, action_key); consumer match =
-- workflow_instances(tenant_id, workflow_id). candidate_workflows is unique at that consumer key;
-- every action side is pre-aggregated to one row per workflow. Numerator and denominator both range
-- over section='main' AND action_type<>'approval' for the identical candidate workflow key set.
WITH candidate_workflows AS (
  SELECT wi.tenant_id, wi.workflow_id
  FROM public.workflow_instances wi
  JOIN public.workflow_actions ors2
    ON ors2.tenant_id = wi.tenant_id
   AND ors2.workflow_id = wi.workflow_id
   AND ors2.action_key = 'ors_water_2'
  WHERE wi.template_key = 'birth_mother'
    AND wi.actions_done = wi.actions_total
    AND ors2.status = 'pending'
    AND ors2.completed_at IS NULL
    AND ors2.due_at IS NOT NULL
), action_rollup AS (
  SELECT
    wa.tenant_id,
    wa.workflow_id,
    count(*) FILTER (
      WHERE wa.section = 'main' AND wa.action_type <> 'approval'
    )::integer AS actions_total,
    count(*) FILTER (
      WHERE wa.section = 'main' AND wa.action_type <> 'approval' AND wa.status = 'completed'
    )::integer AS actions_done,
    count(*) FILTER (WHERE wa.section = 'main')::integer AS all_main_total,
    count(*) FILTER (
      WHERE wa.section = 'main' AND wa.status = 'completed'
    )::integer AS all_main_done
  FROM public.workflow_actions wa
  JOIN candidate_workflows candidate
    ON candidate.tenant_id = wa.tenant_id
   AND candidate.workflow_id = wa.workflow_id
  GROUP BY wa.tenant_id, wa.workflow_id
), next_action AS (
  SELECT DISTINCT ON (wa.tenant_id, wa.workflow_id)
    wa.tenant_id, wa.workflow_id, wa.action_key, wa.title, wa.due_at
  FROM public.workflow_actions wa
  JOIN candidate_workflows candidate
    ON candidate.tenant_id = wa.tenant_id
   AND candidate.workflow_id = wa.workflow_id
  WHERE wa.section = 'main'
    AND wa.action_type <> 'approval'
    AND wa.status NOT IN ('completed', 'canceled')
  ORDER BY wa.tenant_id, wa.workflow_id, wa.seq
)
UPDATE public.workflow_instances wi
SET actions_total = ar.actions_total,
    actions_done = ar.actions_done,
    state = CASE
      WHEN wi.state = 'canceled' THEN 'canceled'
      WHEN ar.all_main_total > 0 AND ar.all_main_done = ar.all_main_total THEN 'completed'
      ELSE 'open'
    END,
    next_action_key = na.action_key,
    next_action_title = na.title,
    next_due_at = na.due_at,
    awaiting_verification = false,
    row_version = wi.row_version + 1,
    updated_at = now()
FROM action_rollup ar
LEFT JOIN next_action na
  ON na.tenant_id = ar.tenant_id
 AND na.workflow_id = ar.workflow_id
WHERE wi.tenant_id = ar.tenant_id
  AND wi.workflow_id = ar.workflow_id;

-- +goose Down
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- This migration only restores card parity with canonical action rows. Re-introducing stale card
-- counters on rollback would knowingly violate that invariant, so Down is intentionally a no-op.
SELECT 1;
