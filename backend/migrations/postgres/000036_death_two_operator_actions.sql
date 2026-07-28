-- +goose Up
-- Death has exactly two persisted operator actions. Admin approval and verifier review are held on
-- workflow_instances.awaiting_verification, not represented as a third workflow_action.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

WITH death_cards AS (
  SELECT
    wi.workflow_id,
    wi.state,
    wi.awaiting_verification OR bool_or(
      wa.action_key = 'park_head_signoff' AND wa.status = 'in_review'
    ) AS awaiting_verification,
    count(*) FILTER (
      WHERE wa.action_key IN ('death_video', 'post_mortem_video')
        AND wa.status = 'completed'
    )::integer AS actions_done
  FROM public.workflow_instances wi
  JOIN public.workflow_actions wa ON wa.workflow_id = wi.workflow_id
  WHERE wi.template_key = 'death'
  GROUP BY wi.workflow_id, wi.state, wi.awaiting_verification
)
UPDATE public.workflow_instances wi
SET actions_total = 2,
    actions_done = dc.actions_done,
    awaiting_verification = dc.awaiting_verification,
    state = CASE
      WHEN wi.state = 'canceled' THEN 'canceled'
      WHEN dc.actions_done = 2 THEN 'completed'
      ELSE 'open'
    END,
    next_action_key = CASE
      WHEN wi.state = 'canceled' THEN NULL
      WHEN dc.actions_done = 0 THEN 'death_video'
      WHEN dc.actions_done = 1 THEN 'post_mortem_video'
      ELSE NULL
    END,
    next_action_title = CASE
      WHEN wi.state = 'canceled' THEN NULL
      WHEN dc.actions_done = 0 THEN 'Record death video'
      WHEN dc.actions_done = 1 THEN 'Record post-mortem video'
      ELSE NULL
    END,
    next_due_at = CASE
      WHEN wi.state <> 'canceled' AND dc.actions_done < 2 THEN (
        SELECT due_at
        FROM public.workflow_actions next_action
        WHERE next_action.workflow_id = wi.workflow_id
          AND next_action.action_key = CASE dc.actions_done
            WHEN 0 THEN 'death_video'
            ELSE 'post_mortem_video'
          END
      )
      ELSE NULL
    END,
    row_version = wi.row_version + 1,
    updated_at = now()
FROM death_cards dc
WHERE wi.workflow_id = dc.workflow_id;

DELETE FROM public.workflow_actions wa
USING public.workflow_instances wi
WHERE wa.workflow_id = wi.workflow_id
  AND wi.template_key = 'death'
  AND wa.action_key = 'park_head_signoff';

-- +goose Down
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

INSERT INTO public.workflow_actions (
  tenant_id, workflow_id, action_key, seq, section, action_type, title, detail,
  requires_video, status, row_version
)
SELECT
  wi.tenant_id, wi.workflow_id, 'park_head_signoff', 3, 'main', 'approval',
  'Verifier review',
  'After admin approval, an authorized verifier reviews both videos together.',
  false,
  CASE WHEN wi.awaiting_verification THEN 'in_review' ELSE 'pending' END,
  wi.row_version
FROM public.workflow_instances wi
WHERE wi.template_key = 'death'
ON CONFLICT (workflow_id, action_key) DO NOTHING;
