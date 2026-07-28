-- +goose Up
-- Every kid task requires one video; Tag the kid is manual; ORS round 2 is due 50 minutes after round 1.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

UPDATE public.workflow_actions wa
SET requires_video = true,
    detail = CASE WHEN wa.action_key = 'tag_the_kid'
      THEN 'Scan or enter the permanent RFID, then record one tagging video. The same goat record is retained and its temporary identifier is retired.'
      ELSE wa.detail END,
    status = CASE WHEN wa.status = 'completed' AND NULLIF(wa.proof_ref, '') IS NULL THEN 'pending' ELSE wa.status END,
    answer_value = CASE WHEN wa.status = 'completed' AND NULLIF(wa.proof_ref, '') IS NULL THEN NULL ELSE wa.answer_value END,
    completed_by = CASE WHEN wa.status = 'completed' AND NULLIF(wa.proof_ref, '') IS NULL THEN NULL ELSE wa.completed_by END,
    completed_at = CASE WHEN wa.status = 'completed' AND NULLIF(wa.proof_ref, '') IS NULL THEN NULL ELSE wa.completed_at END,
    idempotency_key = CASE WHEN wa.status = 'completed' AND NULLIF(wa.proof_ref, '') IS NULL THEN NULL ELSE wa.idempotency_key END,
    request_fingerprint = CASE WHEN wa.status = 'completed' AND NULLIF(wa.proof_ref, '') IS NULL THEN NULL ELSE wa.request_fingerprint END,
    row_version = wa.row_version + 1, updated_at = now()
FROM public.workflow_instances wi
WHERE wi.workflow_id = wa.workflow_id AND wi.tenant_id = wa.tenant_id
  AND wi.template_key = 'birth_kid' AND wa.action_type <> 'approval';

UPDATE public.workflow_actions ors2
SET due_at = ors1.completed_at + interval '50 minutes',
    detail = 'Give the mother a second round of ORS water exactly 50 minutes after the first round was given.',
    row_version = ors2.row_version + 1, updated_at = now()
FROM public.workflow_instances wi
LEFT JOIN public.workflow_actions ors1 ON ors1.tenant_id = wi.tenant_id
 AND ors1.workflow_id = wi.workflow_id AND ors1.action_key = 'ors_water_1'
WHERE wi.tenant_id = ors2.tenant_id AND wi.workflow_id = ors2.workflow_id
  AND wi.template_key = 'birth_mother' AND ors2.action_key = 'ors_water_2';

UPDATE public.workflow_instances
SET awaiting_verification = false, row_version = row_version + 1, updated_at = now()
WHERE template_key IN ('birth_kid', 'birth_mother') AND awaiting_verification;

-- projection-review: actions are unique(workflow_id, action_key), pre-aggregated by workflow_id;
-- numerator and denominator use the identical main/non-approval key set before the 1:1 update.
WITH rollup AS (
  SELECT wa.workflow_id,
    count(*) FILTER (WHERE wa.section = 'main' AND wa.action_type <> 'approval')::integer AS total,
    count(*) FILTER (WHERE wa.section = 'main' AND wa.action_type <> 'approval' AND wa.status = 'completed')::integer AS done
  FROM public.workflow_actions wa
  JOIN public.workflow_instances wi ON wi.tenant_id = wa.tenant_id AND wi.workflow_id = wa.workflow_id
  WHERE wi.template_key IN ('birth_kid', 'birth_mother') GROUP BY wa.workflow_id
), next_action AS (
  SELECT DISTINCT ON (wa.workflow_id) wa.workflow_id, wa.action_key, wa.title, wa.due_at
  FROM public.workflow_actions wa
  JOIN public.workflow_instances wi ON wi.tenant_id = wa.tenant_id AND wi.workflow_id = wa.workflow_id
  WHERE wi.template_key IN ('birth_kid', 'birth_mother') AND wa.section = 'main'
    AND wa.action_type <> 'approval' AND wa.status NOT IN ('completed', 'canceled')
  ORDER BY wa.workflow_id, wa.seq
)
UPDATE public.workflow_instances wi
SET actions_total = r.total, actions_done = r.done,
    state = CASE WHEN wi.state = 'canceled' THEN 'canceled' WHEN r.total = r.done THEN 'completed' ELSE 'open' END,
    next_action_key = n.action_key, next_action_title = n.title, next_due_at = n.due_at,
    row_version = wi.row_version + 1, updated_at = now()
FROM rollup r LEFT JOIN next_action n ON n.workflow_id = r.workflow_id
WHERE wi.workflow_id = r.workflow_id;

-- +goose Down
-- Missing evidence cannot be reconstructed; rollback restores only former template flags/timing.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
UPDATE public.workflow_actions wa
SET requires_video = (wa.action_key = 'first_colostrum'),
    detail = CASE WHEN wa.action_key = 'tag_the_kid'
      THEN 'Assign the permanent RFID from the Awaiting RFID list. This step completes automatically when the permanent tag is assigned.' ELSE wa.detail END,
    row_version = wa.row_version + 1, updated_at = now()
FROM public.workflow_instances wi
WHERE wi.tenant_id = wa.tenant_id AND wi.workflow_id = wa.workflow_id AND wi.template_key = 'birth_kid';
UPDATE public.workflow_actions wa
SET due_at = wi.event_at + interval '6 hours', detail = 'Give the mother a second round of ORS water.',
    row_version = wa.row_version + 1, updated_at = now()
FROM public.workflow_instances wi
WHERE wi.tenant_id = wa.tenant_id AND wi.workflow_id = wa.workflow_id
  AND wi.template_key = 'birth_mother' AND wa.action_key = 'ors_water_2';
