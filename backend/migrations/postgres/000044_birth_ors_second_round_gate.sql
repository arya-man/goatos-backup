-- +goose Up
-- Maintainer decision 2026-07-28: ORS round two is a hard not-before action. Its due_at is the
-- persisted first-round completed_at + 50 minutes. Reopen only unverified legacy completions that
-- were recorded before that canonical deadline; accepted verification history is never rewritten.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- projection-review: producer unique = workflow_actions(workflow_id, action_key); consumer match =
-- workflow_instances(tenant_id, workflow_id). The reopened side returns at most one ORS-2 row per
-- workflow and every joined action side is pre-aggregated to one row per workflow. Numerator and
-- denominator both range over section='main' AND action_type<>'approval' for the same workflow_id.
WITH invalid_completion AS (
  SELECT wa.tenant_id, wa.workflow_id, wa.action_id
  FROM public.workflow_actions wa
  JOIN public.workflow_instances wi
    ON wi.tenant_id = wa.tenant_id
   AND wi.workflow_id = wa.workflow_id
  WHERE wi.template_key = 'birth_mother'
    AND NOT wi.awaiting_verification
    AND wa.action_key = 'ors_water_2'
    AND wa.status = 'completed'
    AND wa.completed_at IS NOT NULL
    AND wa.due_at IS NOT NULL
    AND wa.completed_at < wa.due_at
    AND wa.verification_item_id IS NULL
), reopened AS (
  UPDATE public.workflow_actions wa
  SET status = 'pending',
      proof_ref = NULL,
      completed_by = NULL,
      completed_at = NULL,
      verification_item_id = NULL,
      idempotency_key = NULL,
      request_fingerprint = NULL,
      row_version = wa.row_version + 1,
      updated_at = now()
  FROM invalid_completion invalid
  WHERE wa.tenant_id = invalid.tenant_id
    AND wa.workflow_id = invalid.workflow_id
    AND wa.action_id = invalid.action_id
  RETURNING wa.tenant_id, wa.workflow_id
), affected AS (
  SELECT DISTINCT tenant_id, workflow_id
  FROM reopened
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
  JOIN affected a
    ON a.tenant_id = wa.tenant_id
   AND a.workflow_id = wa.workflow_id
  GROUP BY wa.tenant_id, wa.workflow_id
), next_action AS (
  SELECT DISTINCT ON (wa.tenant_id, wa.workflow_id)
    wa.tenant_id, wa.workflow_id, wa.action_key, wa.title, wa.due_at
  FROM public.workflow_actions wa
  JOIN affected a
    ON a.tenant_id = wa.tenant_id
   AND a.workflow_id = wa.workflow_id
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

-- Premature evidence removed by Up cannot be restored as a valid medical action. Rolling back the
-- binary relaxes the future gate but deliberately does not fabricate the discarded completion.
SELECT 1;
