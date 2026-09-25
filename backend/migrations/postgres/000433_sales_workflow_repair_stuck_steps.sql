-- +goose Up
-- seed-fixture-guard:ignore: one-time data repair of open sales_deal workflow rows; no schema,
-- seed contract, source fixture or read-model shape change.
--
-- REPAIR THE SALE WORKFLOWS ALREADY STUCK (maintainer decision 2026-09-25, docs/decisions/
-- sales-sop.md -> "A sale without animals" / "A failed sale"). 000432 and the code stop NEW sales
-- from opening an unfinishable tag step, but every sale recorded since 000369 already carries one.
-- Two repairs, each idempotent (a second run finds nothing to change) and each confined to OPEN
-- sales_deal workflows:
--
--   1. A sale with NO live animals -- no animal-kind line whose head counts add to one whole
--      animal, the same test as sales/domain.DealWrite.HasLiveAnimals and the tagging confirm's own
--      gate -- has its still-unfinished tag_animals / loading_video / dispatch_note steps SKIPPED
--      (off the path, never owed), exactly what the opener now leaves out. A step already done
--      keeps its record.
--   2. A sale marked Deal Failed has its workflow CANCELLED: unfinished steps cancelled, the card
--      closed -- what tasks/app.SaleStatusChangedWorkflowHandler does for every failure from now on.
--
-- The card fields (actions_total / actions_done / next_* / state) are recomputed from the steps in
-- the same statement, mirroring tasks/domain.RecomputeCard for a bundle-reviewed workflow: skipped
-- steps are neither owed nor counted, canceled steps count toward the total but never as next, and
-- a workflow whose every remaining main step is done is completed.

-- 1. Sales with no live animals: skip the three animal steps still owed.
-- seed-migration-guard:ignore owner=manohark issue=sales-workflow-conditions reason=one-time-repair-of-open-sale-workflows expiry=2026-12-31
WITH no_animals AS (
  SELECT wi.tenant_id, wi.workflow_id
  FROM public.workflow_instances wi
  WHERE wi.template_key = 'sales_deal'
    AND wi.state = 'open'
    AND wi.subject_ref_id IS NOT NULL
    AND floor(COALESCE((
          SELECT sum(l.animal_count)
          FROM public.sales_deal_lines l
          WHERE l.tenant_id = wi.tenant_id AND l.deal_id = wi.subject_ref_id
            AND l.product_kind = 'animal' AND l.animal_count > 0), 0)) < 1
)
UPDATE public.workflow_actions a
SET status = 'skipped', row_version = a.row_version + 1, updated_at = now()
FROM no_animals n
WHERE a.tenant_id = n.tenant_id AND a.workflow_id = n.workflow_id
  AND a.action_key IN ('tag_animals', 'loading_video', 'dispatch_note')
  AND a.status IN ('pending', 'rework');

-- 2. Failed deals: cancel every unfinished step.
UPDATE public.workflow_actions a
SET status = 'canceled', row_version = a.row_version + 1, updated_at = now()
FROM public.workflow_instances wi
JOIN public.sales_deals d ON d.tenant_id = wi.tenant_id AND d.id = wi.subject_ref_id
WHERE wi.template_key = 'sales_deal' AND wi.state = 'open'
  AND d.status = 'Deal Failed'
  AND a.tenant_id = wi.tenant_id AND a.workflow_id = wi.workflow_id
  AND a.status IN ('pending', 'in_review', 'rework');

UPDATE public.workflow_instances wi
SET state = 'canceled', next_action_key = NULL, next_action_title = NULL, next_due_at = NULL,
    awaiting_verification = false, row_version = wi.row_version + 1, updated_at = now()
FROM public.sales_deals d
WHERE d.tenant_id = wi.tenant_id AND d.id = wi.subject_ref_id
  AND wi.template_key = 'sales_deal' AND wi.state = 'open'
  AND d.status = 'Deal Failed';

-- 3. Recompute the card of every still-open sale workflow from its steps (RecomputeCard).
WITH counts AS (
  SELECT wi.tenant_id, wi.workflow_id,
         count(*) FILTER (WHERE a.status <> 'skipped' AND a.action_type <> 'approval') AS operator_total,
         count(*) FILTER (WHERE a.status IN ('completed', 'in_review') AND a.action_type <> 'approval') AS operator_done,
         count(*) FILTER (WHERE a.status <> 'skipped' AND a.section = 'main') AS main_total,
         count(*) FILTER (WHERE a.status = 'completed' AND a.section = 'main') AS main_done
  FROM public.workflow_instances wi
  JOIN public.workflow_actions a ON a.tenant_id = wi.tenant_id AND a.workflow_id = wi.workflow_id
  WHERE wi.template_key = 'sales_deal' AND wi.state = 'open'
  GROUP BY wi.tenant_id, wi.workflow_id
),
next_step AS (
  SELECT DISTINCT ON (a.tenant_id, a.workflow_id) a.tenant_id, a.workflow_id, a.action_key, a.title, a.due_at
  FROM public.workflow_actions a
  JOIN public.workflow_instances wi ON wi.tenant_id = a.tenant_id AND wi.workflow_id = a.workflow_id
  WHERE wi.template_key = 'sales_deal' AND wi.state = 'open'
    AND a.action_type <> 'approval'
    AND a.status NOT IN ('completed', 'in_review', 'canceled', 'skipped')
  ORDER BY a.tenant_id, a.workflow_id, a.seq
)
UPDATE public.workflow_instances wi
SET actions_total = c.operator_total,
    actions_done = c.operator_done,
    next_action_key = n.action_key,
    next_action_title = n.title,
    next_due_at = n.due_at,
    state = CASE WHEN c.main_total > 0 AND c.main_done = c.main_total THEN 'completed' ELSE wi.state END,
    row_version = wi.row_version + 1,
    updated_at = now()
FROM counts c
LEFT JOIN next_step n ON n.tenant_id = c.tenant_id AND n.workflow_id = c.workflow_id
WHERE wi.tenant_id = c.tenant_id AND wi.workflow_id = c.workflow_id
  AND (wi.actions_total IS DISTINCT FROM c.operator_total
       OR wi.actions_done IS DISTINCT FROM c.operator_done
       OR wi.next_action_key IS DISTINCT FROM n.action_key
       OR (c.main_total > 0 AND c.main_done = c.main_total));

-- +goose Down
-- A data repair: the skipped / cancelled steps were work nobody could or would do. Not reversed.
SELECT 1;
