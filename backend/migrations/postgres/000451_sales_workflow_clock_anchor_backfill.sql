-- +goose Up
-- seed-fixture-guard:ignore: one-time data repair of open sales_deal workflow clocks; no schema,
-- seed contract, source fixture or read-model shape change.
--
-- ANCHOR THE PLANNED SALES OPENED BEFORE 000449 (2026-09-26, docs/decisions/sales-sop.md -> "A
-- planned sale's work is due on the sale day" -> "Existing data"). 000449 added
-- workflow_instances.clock_anchor_at and the opener now anchors a sale dated after its recording
-- day at 00:00 Asia/Kolkata of that day (tasks/domain.SaleClockAnchor). Every sale workflow opened
-- earlier was anchored on the RECORDING instant and left NULL, so an open sale planned for a later
-- day still reads Overdue from the moment it was saved.
--
-- For every OPEN sales_deal workflow with a NULL anchor whose deal's sale_date is AFTER the
-- workflow's recording business day (event_date = the Asia/Kolkata business date of event_at, the
-- exact test SaleClockAnchor makes), this sets the anchor to the start of that business day and
-- recomputes every UNFINISHED clock-timed step exactly as the opener would have from that anchor,
-- by the same rule tasks/adapters/postgres.shiftUnfinishedSaleStepsSQL applies on a close:
--
--   immediately / after_event  due = anchor + offset, so the step moves by (anchor - event_at)
--   at_fixed_time              due = anchor's business day + N days at HH:MM, so the step moves by
--                              whole business days (anchor day - recording day)
--
-- The schedule kind is read from the SOP version the workflow was pinned to (a workflow on the
-- seeded document has only "immediately" steps). A dependency-timed step (after_action_key) and a
-- finished step are left alone. The card's next_due_at is then re-read from its next step, as
-- tasks/domain.RecomputeCard defines it (next_due_at IS the next step's due_at).
--
-- Idempotent: the anchor is stored in the same transaction, and only NULL anchors are candidates,
-- so a second run changes nothing. A sale dated on (or before) its recording day keeps its NULL
-- anchor and its due times. A later close then re-anchors from the stored anchor (or from event_at
-- for a NULL one) exactly once.
-- seed-migration-guard:ignore owner=manohark issue=sales-workflow-clock-anchor reason=one-time-backfill-of-open-planned-sale-workflows expiry=2026-12-31

-- 1. Recompute the unfinished clock-timed steps from the sale-day anchor.
-- projection-review: membership=workflow_actions of OPEN sales_deal workflow_instances with a NULL clock_anchor_at and sales_deals.sale_date > event_date; group_key=(tenant_id, workflow_id); join_cardinality=workflow_instances 1:1 sales_deals (primary key via subject_ref_id), 1:N workflow_actions updated per row, schedule kind read by EXISTS (no fan-out); pagination=none, one bounded repair; scope=tenant + workflow
WITH planned AS (
  SELECT wi.tenant_id, wi.workflow_id, wi.event_at,
         (d.sale_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS anchor
  FROM public.workflow_instances wi
  JOIN public.sales_deals d ON d.tenant_id = wi.tenant_id AND d.id = wi.subject_ref_id
  WHERE wi.template_key = 'sales_deal' AND wi.state = 'open'
    AND wi.clock_anchor_at IS NULL
    AND d.sale_date > wi.event_date
)
UPDATE public.workflow_actions a
SET due_at = a.due_at + CASE
      WHEN EXISTS (
        SELECT 1
        FROM public.workflow_instances wi
        JOIN public.sop_versions sv ON sv.tenant_id = wi.tenant_id AND sv.sop_version_id = wi.sop_version_id
        CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(sv.form_dsl->'follow_up'->'tracks') = 'array'
                                                     THEN sv.form_dsl->'follow_up'->'tracks' ELSE '[]'::jsonb END) tr
        CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(tr->'steps') = 'array'
                                                     THEN tr->'steps' ELSE '[]'::jsonb END) st
        WHERE wi.tenant_id = a.tenant_id AND wi.workflow_id = a.workflow_id
          AND tr->>'key' = 'sales_deal' AND st->>'key' = a.action_key
          AND st->'schedule'->>'kind' = 'at_fixed_time')
      THEN date_trunc('day', p.anchor AT TIME ZONE 'Asia/Kolkata')
         - date_trunc('day', p.event_at AT TIME ZONE 'Asia/Kolkata')
      ELSE p.anchor - p.event_at
    END,
    row_version = a.row_version + 1, updated_at = now()
FROM planned p
WHERE a.tenant_id = p.tenant_id AND a.workflow_id = p.workflow_id
  AND a.status IN ('pending', 'in_review', 'rework')
  AND a.due_at IS NOT NULL AND coalesce(a.after_action_key, '') = '';

-- 2. Store the anchor and re-read the card's next due from its next step.
UPDATE public.workflow_instances wi
SET clock_anchor_at = (d.sale_date::timestamp AT TIME ZONE 'Asia/Kolkata'),
    next_due_at = COALESCE((SELECT a.due_at FROM public.workflow_actions a
                             WHERE a.tenant_id = wi.tenant_id AND a.workflow_id = wi.workflow_id
                               AND a.action_key = wi.next_action_key), wi.next_due_at),
    row_version = wi.row_version + 1, updated_at = now()
FROM public.sales_deals d
WHERE d.tenant_id = wi.tenant_id AND d.id = wi.subject_ref_id
  AND wi.template_key = 'sales_deal' AND wi.state = 'open'
  AND wi.clock_anchor_at IS NULL
  AND d.sale_date > wi.event_date;

-- +goose Down
-- A data repair: the steps now due on the sale day were never owed earlier. Not reversed.
SELECT 1;
