-- +goose Up
-- Maintainer decision 2026-07-28: every birth_mother operator step carries exactly one video.
-- Mother's Medicine remains ONE action; its detail lists the complete four-medicine regimen and
-- its single proof_ref covers that whole action. Any legacy completion without proof is reopened
-- because it does not satisfy the new evidence contract.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

UPDATE public.workflow_actions wa
SET requires_video = true,
    detail = CASE wa.action_key
      WHEN 'mothers_medicine' THEN E'Chocolate Injection at 1.5 ml SQ\nMeloxicam Paracetamol at 4 ml IM\nExapar at 20 ml\nGlucoboost at 100 ml mix with 150gms Concentrate'
      ELSE wa.detail
    END,
    status = CASE
      WHEN wa.status = 'completed' AND wa.proof_ref IS NULL THEN 'pending'
      ELSE wa.status
    END,
    answer_value = CASE
      WHEN wa.status = 'completed' AND wa.proof_ref IS NULL THEN NULL
      ELSE wa.answer_value
    END,
    completed_by = CASE
      WHEN wa.status = 'completed' AND wa.proof_ref IS NULL THEN NULL
      ELSE wa.completed_by
    END,
    completed_at = CASE
      WHEN wa.status = 'completed' AND wa.proof_ref IS NULL THEN NULL
      ELSE wa.completed_at
    END,
    verification_item_id = CASE
      WHEN wa.status = 'completed' AND wa.proof_ref IS NULL THEN NULL
      ELSE wa.verification_item_id
    END,
    idempotency_key = CASE
      WHEN wa.status = 'completed' AND wa.proof_ref IS NULL THEN NULL
      ELSE wa.idempotency_key
    END,
    request_fingerprint = CASE
      WHEN wa.status = 'completed' AND wa.proof_ref IS NULL THEN NULL
      ELSE wa.request_fingerprint
    END,
    row_version = wa.row_version + 1,
    updated_at = now()
FROM public.workflow_instances wi
WHERE wi.workflow_id = wa.workflow_id
  AND wi.tenant_id = wa.tenant_id
  AND wi.template_key = 'birth_mother';

-- projection-review: producer unique = workflow_actions(workflow_id, action_key); consumer match =
-- workflow_instances(workflow_id). Every action side is pre-aggregated to one row per workflow;
-- next_action is a one-row LATERAL over the same workflow_id. Numerator and denominator both range
-- over section='main' AND action_type<>'approval' for this exact workflow.
WITH action_rollup AS (
  SELECT
    wa.workflow_id,
    count(*) FILTER (WHERE wa.section = 'main' AND wa.action_type <> 'approval')::integer AS actions_total,
    count(*) FILTER (
      WHERE wa.section = 'main' AND wa.action_type <> 'approval' AND wa.status = 'completed'
    )::integer AS actions_done,
    count(*) FILTER (WHERE wa.section = 'main')::integer AS all_main_total,
    count(*) FILTER (WHERE wa.section = 'main' AND wa.status = 'completed')::integer AS all_main_done
  FROM public.workflow_actions wa
  JOIN public.workflow_instances wi
    ON wi.tenant_id = wa.tenant_id AND wi.workflow_id = wa.workflow_id
  WHERE wi.template_key = 'birth_mother'
  GROUP BY wa.workflow_id
), next_action AS (
  SELECT DISTINCT ON (wa.workflow_id)
    wa.workflow_id, wa.action_key, wa.title, wa.due_at
  FROM public.workflow_actions wa
  JOIN public.workflow_instances wi
    ON wi.tenant_id = wa.tenant_id AND wi.workflow_id = wa.workflow_id
  WHERE wi.template_key = 'birth_mother'
    AND wa.section = 'main'
    AND wa.action_type <> 'approval'
    AND wa.status NOT IN ('completed', 'canceled')
  ORDER BY wa.workflow_id, wa.seq
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
    row_version = wi.row_version + 1,
    updated_at = now()
FROM action_rollup ar
LEFT JOIN next_action na ON na.workflow_id = ar.workflow_id
WHERE wi.workflow_id = ar.workflow_id
  AND wi.template_key = 'birth_mother';

-- +goose Down
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

UPDATE public.workflow_actions wa
SET requires_video = false,
    detail = CASE wa.action_key
      WHEN 'mothers_medicine' THEN 'Administer the prescribed post-delivery medicine course to the mother.'
      ELSE wa.detail
    END,
    row_version = wa.row_version + 1,
    updated_at = now()
FROM public.workflow_instances wi
WHERE wi.workflow_id = wa.workflow_id
  AND wi.tenant_id = wa.tenant_id
  AND wi.template_key = 'birth_mother';

-- Answers reopened by the Up migration cannot be reconstructed safely: the old rows had no video
-- and therefore remain pending after rollback rather than fabricating evidence.
