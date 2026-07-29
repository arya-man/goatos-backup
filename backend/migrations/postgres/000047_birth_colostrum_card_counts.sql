-- +goose Up
-- Retire the legacy card rule that excluded scheduled colostrum. Every operator-visible action
-- now contributes to actions_total/actions_done and next_*, matching the Birth detail list.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- projection-review: membership=birth_kid workflow actions across main and scheduled colostrum sections; group_key=tenant_id and workflow_id; join_cardinality=rollup is one row and next action is at most one row per workflow; pagination=one migration repair set independent of page size; scope=template_key birth_kid and the identical non-approval operator-action set
-- Producer unique = workflow_actions(workflow_id, action_key); consumer unique =
-- workflow_instances(tenant_id, workflow_id). rollup is exactly one row per Birth-kid workflow and
-- next_action is reduced to at most one row at that same key. total, done, and next range over the
-- identical non-approval operator-action key set across both main and colostrum_session sections.
WITH action_rollup AS (
  SELECT
    wi.tenant_id,
    wi.workflow_id,
    count(*) FILTER (WHERE wa.action_type <> 'approval')::integer AS actions_total,
    count(*) FILTER (
      WHERE wa.action_type <> 'approval' AND wa.status = 'completed'
    )::integer AS actions_done
  FROM public.workflow_instances wi
  JOIN public.workflow_actions wa
    ON wa.tenant_id = wi.tenant_id
   AND wa.workflow_id = wi.workflow_id
  WHERE wi.template_key = 'birth_kid'
  GROUP BY wi.tenant_id, wi.workflow_id
), ranked_next AS (
  SELECT
    wa.tenant_id,
    wa.workflow_id,
    wa.action_key,
    wa.title,
    wa.due_at,
    row_number() OVER (
      PARTITION BY wa.tenant_id, wa.workflow_id
      ORDER BY wa.seq, wa.action_id
    ) AS position
  FROM public.workflow_actions wa
  JOIN public.workflow_instances wi
    ON wi.tenant_id = wa.tenant_id
   AND wi.workflow_id = wa.workflow_id
  WHERE wi.template_key = 'birth_kid'
    AND wa.action_type <> 'approval'
    AND wa.status NOT IN ('completed', 'canceled')
), recomputed AS (
  SELECT
    rollup.tenant_id,
    rollup.workflow_id,
    rollup.actions_total,
    rollup.actions_done,
    next_action.action_key,
    next_action.title,
    next_action.due_at
  FROM action_rollup rollup
  LEFT JOIN ranked_next next_action
    ON next_action.tenant_id = rollup.tenant_id
   AND next_action.workflow_id = rollup.workflow_id
   AND next_action.position = 1
)
UPDATE public.workflow_instances wi
SET actions_total = recomputed.actions_total,
    actions_done = recomputed.actions_done,
    next_action_key = recomputed.action_key,
    next_action_title = recomputed.title,
    next_due_at = recomputed.due_at,
    row_version = wi.row_version + 1,
    updated_at = now()
FROM recomputed
WHERE wi.tenant_id = recomputed.tenant_id
  AND wi.workflow_id = recomputed.workflow_id
  AND (wi.actions_total, wi.actions_done, wi.next_action_key, wi.next_action_title, wi.next_due_at)
      IS DISTINCT FROM
      (recomputed.actions_total, recomputed.actions_done, recomputed.action_key,
       recomputed.title, recomputed.due_at);

-- +goose Down
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Restore the previous binary's main-only card semantics without deleting any operator task.
WITH action_rollup AS (
  SELECT
    wi.tenant_id,
    wi.workflow_id,
    count(*) FILTER (
      WHERE wa.section = 'main' AND wa.action_type <> 'approval'
    )::integer AS actions_total,
    count(*) FILTER (
      WHERE wa.section = 'main' AND wa.action_type <> 'approval' AND wa.status = 'completed'
    )::integer AS actions_done
  FROM public.workflow_instances wi
  JOIN public.workflow_actions wa
    ON wa.tenant_id = wi.tenant_id
   AND wa.workflow_id = wi.workflow_id
  WHERE wi.template_key = 'birth_kid'
  GROUP BY wi.tenant_id, wi.workflow_id
), ranked_next AS (
  SELECT
    wa.tenant_id,
    wa.workflow_id,
    wa.action_key,
    wa.title,
    wa.due_at,
    row_number() OVER (
      PARTITION BY wa.tenant_id, wa.workflow_id
      ORDER BY wa.seq, wa.action_id
    ) AS position
  FROM public.workflow_actions wa
  JOIN public.workflow_instances wi
    ON wi.tenant_id = wa.tenant_id
   AND wi.workflow_id = wa.workflow_id
  WHERE wi.template_key = 'birth_kid'
    AND wa.section = 'main'
    AND wa.action_type <> 'approval'
    AND wa.status NOT IN ('completed', 'canceled')
), recomputed AS (
  SELECT
    rollup.tenant_id,
    rollup.workflow_id,
    rollup.actions_total,
    rollup.actions_done,
    next_action.action_key,
    next_action.title,
    next_action.due_at
  FROM action_rollup rollup
  LEFT JOIN ranked_next next_action
    ON next_action.tenant_id = rollup.tenant_id
   AND next_action.workflow_id = rollup.workflow_id
   AND next_action.position = 1
)
UPDATE public.workflow_instances wi
SET actions_total = recomputed.actions_total,
    actions_done = recomputed.actions_done,
    next_action_key = recomputed.action_key,
    next_action_title = recomputed.title,
    next_due_at = recomputed.due_at,
    row_version = wi.row_version + 1,
    updated_at = now()
FROM recomputed
WHERE wi.tenant_id = recomputed.tenant_id
  AND wi.workflow_id = recomputed.workflow_id
  AND (wi.actions_total, wi.actions_done, wi.next_action_key, wi.next_action_title, wi.next_due_at)
      IS DISTINCT FROM
      (recomputed.actions_total, recomputed.actions_done, recomputed.action_key,
       recomputed.title, recomputed.due_at);
