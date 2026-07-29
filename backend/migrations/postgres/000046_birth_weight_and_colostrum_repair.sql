-- +goose Up
-- Repair the legacy birth-kid template that instantiated five same-day colostrum rows regardless
-- of birth time and omitted every next-day row. The source contract is five IST clock sessions
-- (07:00, 11:00, 15:00, 18:30, 22:00): on birth day include a slot only when birth is strictly
-- before its 15-minute cutoff; on the next day include all five. Also replace kid-weight bands with
-- one exact numeric kilograms answer. The repair is idempotent and skips any workflow whose legacy
-- colostrum evidence has already been submitted, preserving historical proof.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

UPDATE public.workflow_actions wa
SET action_type = 'question',
    detail = 'Weigh the kid and enter the exact weight in kilograms (kg).',
    options = NULL,
    row_version = wa.row_version + 1,
    updated_at = now()
FROM public.workflow_instances wi
WHERE wi.tenant_id = wa.tenant_id
  AND wi.workflow_id = wa.workflow_id
  AND wi.template_key = 'birth_kid'
  AND wa.action_key = 'take_weight'
  AND (wa.action_type <> 'question' OR wa.options IS NOT NULL OR
       wa.detail IS DISTINCT FROM 'Weigh the kid and enter the exact weight in kilograms (kg).');

-- A completed legacy band is not an exact weight. Reopen only work that has not entered verifier
-- review, so the operator can enter kilograms and capture replacement evidence. Accepted or
-- pending-verification history is never rewritten.
CREATE TEMP TABLE birth_weight_reopen_candidates AS
SELECT wa.tenant_id, wa.workflow_id, wa.action_id
FROM public.workflow_actions wa
JOIN public.workflow_instances wi
  ON wi.tenant_id = wa.tenant_id
 AND wi.workflow_id = wa.workflow_id
WHERE wi.template_key = 'birth_kid'
  AND NOT wi.awaiting_verification
  AND wa.action_key = 'take_weight'
  AND wa.status = 'completed'
  AND wa.verification_item_id IS NULL
  AND CASE
    WHEN coalesce(wa.answer_value, '') ~ '^[0-9]+([.][0-9]+)?$'
      THEN wa.answer_value::numeric <= 0
    ELSE true
  END;

UPDATE public.workflow_actions wa
SET status = 'pending',
    answer_value = NULL,
    proof_ref = NULL,
    completed_by = NULL,
    completed_at = NULL,
    idempotency_key = NULL,
    request_fingerprint = NULL,
    row_version = wa.row_version + 1,
    updated_at = now()
FROM birth_weight_reopen_candidates candidate
WHERE candidate.tenant_id = wa.tenant_id
  AND candidate.workflow_id = wa.workflow_id
  AND candidate.action_id = wa.action_id;

-- projection-review: membership=birth_kid workflows with invalid completed birth weight actions; group_key=tenant_id and workflow_id; join_cardinality=affected is distinct and actions aggregate to one row per workflow before update; pagination=one migration repair set independent of page size; scope=template_key birth_kid and the identical main non-approval action set
-- Producer unique = workflow_actions(workflow_id, action_key); consumer match =
-- workflow_instances(tenant_id, workflow_id). affected is distinct at that consumer key, each
-- action side is pre-aggregated to one row per workflow, and actions_done/actions_total range over
-- the identical section=main, non-approval key set.
WITH affected AS (
  SELECT DISTINCT tenant_id, workflow_id
  FROM birth_weight_reopen_candidates
), action_rollup AS (
  SELECT
    wa.tenant_id,
    wa.workflow_id,
    count(*) FILTER (
      WHERE wa.section = 'main' AND wa.action_type <> 'approval'
    )::integer AS actions_total,
    count(*) FILTER (
      WHERE wa.section = 'main' AND wa.action_type <> 'approval' AND wa.status = 'completed'
    )::integer AS actions_done
  FROM public.workflow_actions wa
  JOIN affected
    ON affected.tenant_id = wa.tenant_id
   AND affected.workflow_id = wa.workflow_id
  GROUP BY wa.tenant_id, wa.workflow_id
)
UPDATE public.workflow_instances wi
SET actions_total = rollup.actions_total,
    actions_done = rollup.actions_done,
    state = 'open',
    next_action_key = weight.action_key,
    next_action_title = weight.title,
    next_due_at = weight.due_at,
    awaiting_verification = false,
    row_version = wi.row_version + 1,
    updated_at = now()
FROM action_rollup rollup
JOIN public.workflow_actions weight
  ON weight.tenant_id = rollup.tenant_id
 AND weight.workflow_id = rollup.workflow_id
 AND weight.action_key = 'take_weight'
WHERE wi.tenant_id = rollup.tenant_id
  AND wi.workflow_id = rollup.workflow_id;

DROP TABLE birth_weight_reopen_candidates;

CREATE TEMP TABLE birth_colostrum_repair_candidates AS
SELECT wi.tenant_id, wi.workflow_id, wi.event_at
FROM public.workflow_instances wi
WHERE wi.template_key = 'birth_kid'
  AND EXISTS (
    SELECT 1
    FROM public.workflow_actions legacy
    WHERE legacy.tenant_id = wi.tenant_id
      AND legacy.workflow_id = wi.workflow_id
      AND legacy.action_key LIKE 'colostrum_session_%'
  )
  AND NOT EXISTS (
    SELECT 1
    FROM public.workflow_actions protected
    WHERE protected.tenant_id = wi.tenant_id
      AND protected.workflow_id = wi.workflow_id
      AND protected.action_key LIKE 'colostrum_session_%'
      AND (protected.status <> 'pending' OR protected.proof_ref IS NOT NULL OR
           protected.completed_at IS NOT NULL OR protected.verification_item_id IS NOT NULL)
  );

-- Lock only the bounded candidate workflow rows while their action set is replaced.
SELECT wi.workflow_id
FROM public.workflow_instances wi
JOIN birth_colostrum_repair_candidates candidate
  ON candidate.tenant_id = wi.tenant_id
 AND candidate.workflow_id = wi.workflow_id
FOR UPDATE;

DELETE FROM public.workflow_actions wa
USING birth_colostrum_repair_candidates candidate
WHERE candidate.tenant_id = wa.tenant_id
  AND candidate.workflow_id = wa.workflow_id
  AND wa.action_key LIKE 'colostrum_session_%';

-- projection-review: membership=birth_kid workflows selected for colostrum schedule repair; group_key=tenant_id workflow_id and numbered session; join_cardinality=candidates are unique and slots expand to one row per retained due time before keyed insert; pagination=one migration repair set independent of page size; scope=template_key birth_kid and the candidate birth-day window
-- Producer unique = workflow_actions(workflow_id, action_key); consumer match =
-- candidate (tenant_id, workflow_id), exactly one row per workflow. slots is exactly ten rows per
-- candidate before the birth-day predicate and numbered is exactly one row per retained due time.
-- No ratio is computed; inserted and tag-sequence key sets both range over the identical numbered
-- session set for each candidate workflow.
WITH slots(day_offset, slot_order, slot_time, slot_label) AS (
  VALUES
    (0, 1, time '07:00', '07:00'),
    (0, 2, time '11:00', '11:00'),
    (0, 3, time '15:00', '15:00'),
    (0, 4, time '18:30', '18:30'),
    (0, 5, time '22:00', '22:00'),
    (1, 1, time '07:00', '07:00'),
    (1, 2, time '11:00', '11:00'),
    (1, 3, time '15:00', '15:00'),
    (1, 4, time '18:30', '18:30'),
    (1, 5, time '22:00', '22:00')
), eligible AS (
  SELECT
    candidate.tenant_id,
    candidate.workflow_id,
    slots.day_offset,
    slots.slot_order,
    slots.slot_label,
    (((candidate.event_at AT TIME ZONE 'Asia/Kolkata')::date + slots.day_offset) + slots.slot_time)
      AT TIME ZONE 'Asia/Kolkata' AS due_at
  FROM birth_colostrum_repair_candidates candidate
  CROSS JOIN slots
  WHERE slots.day_offset = 1
     OR candidate.event_at <
       (((candidate.event_at AT TIME ZONE 'Asia/Kolkata')::date + slots.slot_time)
         AT TIME ZONE 'Asia/Kolkata') - interval '15 minutes'
), numbered AS (
  SELECT eligible.*,
         row_number() OVER (PARTITION BY tenant_id, workflow_id ORDER BY due_at)::integer AS session_index
  FROM eligible
)
INSERT INTO public.workflow_actions (
  tenant_id, workflow_id, action_key, seq, section, action_type, title, detail,
  requires_video, options, due_at, status
)
SELECT
  tenant_id,
  workflow_id,
  'colostrum_day_' || (day_offset + 1)::text || '_' || replace(slot_label, ':', ''),
  7 + session_index,
  'colostrum_session',
  'action',
  CASE session_index + 1
    WHEN 2 THEN '2nd Colostrum'
    WHEN 3 THEN '3rd Colostrum'
    ELSE (session_index + 1)::text || 'th Colostrum'
  END,
  'Feed colostrum at the ' || slot_label || ' session on the ' ||
    CASE WHEN day_offset = 0 THEN 'birth day.' ELSE 'day after birth.' END,
  true,
  NULL,
  due_at,
  'pending'
FROM numbered
ON CONFLICT (workflow_id, action_key) DO NOTHING;

UPDATE public.workflow_actions tag
SET seq = 8 + session_count.total,
    row_version = tag.row_version + 1,
    updated_at = now()
FROM (
  SELECT wa.tenant_id, wa.workflow_id, count(*)::integer AS total
  FROM public.workflow_actions wa
  JOIN birth_colostrum_repair_candidates candidate
    ON candidate.tenant_id = wa.tenant_id
   AND candidate.workflow_id = wa.workflow_id
  WHERE wa.section = 'colostrum_session'
  GROUP BY wa.tenant_id, wa.workflow_id
) session_count
WHERE tag.tenant_id = session_count.tenant_id
  AND tag.workflow_id = session_count.workflow_id
  AND tag.action_key = 'tag_the_kid'
  AND tag.seq IS DISTINCT FROM 8 + session_count.total;

DROP TABLE birth_colostrum_repair_candidates;

-- +goose Down
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Corrected session times and newly added next-day work are retained: recreating known-wrong past
-- sessions would corrupt medical history. Restore only the old weight-input shape for binary
-- compatibility if this migration is rolled back.
UPDATE public.workflow_actions wa
SET action_type = 'question_select',
    detail = 'Weigh the kid and select the weight band.',
    options = '["Below 2.0 kg","2.0 – 2.5 kg","2.5 – 3.0 kg","3.0 – 3.5 kg","Above 3.5 kg"]'::jsonb,
    row_version = wa.row_version + 1,
    updated_at = now()
FROM public.workflow_instances wi
WHERE wi.tenant_id = wa.tenant_id
  AND wi.workflow_id = wa.workflow_id
  AND wi.template_key = 'birth_kid'
  AND wa.action_key = 'take_weight';
