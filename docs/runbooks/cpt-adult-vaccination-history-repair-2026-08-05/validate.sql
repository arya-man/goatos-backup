\set ON_ERROR_STOP on

-- Read-only validation for the CPT adult vaccination history repair.
-- Expected target DB after repair:
--   * CPT true-adult ET+TT booster split: 114 / 163 / 47 on 2026-07-24/25/26.
--   * CPT true-adult ET+TT first dose: 85 / 239 on 2026-06-30/2026-07-01.
--   * Video proof task accepted, proof refs preserved.
--   * Future CPT adult obligations and weighing tables unchanged versus the
--     comparison hashes captured in the 2026-08-05 runbook.

BEGIN READ ONLY;

WITH adult_w2 AS (
  SELECT
    vc.administered_at::date AS administered_date,
    oi.completed_at::date AS completed_date,
    count(*) AS rows
  FROM vaccination_completions vc
  JOIN obligation_instances oi
    ON oi.tenant_id = vc.tenant_id
   AND oi.obligation_id = vc.obligation_id
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
  JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
  JOIN locations park
    ON park.tenant_id = g.tenant_id
   AND park.location_id = g.park_id
  WHERE park.location_code = 'CPT'
    AND pr.dose_code = 'et_tt_adult_w2'
    AND vc.status = 'accepted'
    AND coalesce(g.management_stage, g.age_band, 'UNKNOWN') IN ('Non-Pregnant', 'Buck')
  GROUP BY 1, 2
),
adult_w2_ok AS (
  SELECT
    count(*) = 3
    AND bool_or(administered_date = DATE '2026-07-24' AND completed_date = DATE '2026-07-24' AND rows = 114)
    AND bool_or(administered_date = DATE '2026-07-25' AND completed_date = DATE '2026-07-25' AND rows = 163)
    AND bool_or(administered_date = DATE '2026-07-26' AND completed_date = DATE '2026-07-26' AND rows = 47)
      AS ok
  FROM adult_w2
)
SELECT 'adult_w2_split' AS check_name, ok, jsonb_agg(to_jsonb(adult_w2) ORDER BY administered_date, completed_date) AS detail
FROM adult_w2_ok, adult_w2
GROUP BY ok;

WITH adult_w1 AS (
  SELECT
    vc.administered_at::date AS administered_date,
    count(*) AS rows
  FROM vaccination_completions vc
  JOIN obligation_instances oi
    ON oi.tenant_id = vc.tenant_id
   AND oi.obligation_id = vc.obligation_id
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
  JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
  JOIN locations park
    ON park.tenant_id = g.tenant_id
   AND park.location_id = g.park_id
  WHERE park.location_code = 'CPT'
    AND pr.dose_code = 'et_tt_adult_w1'
    AND vc.status = 'accepted'
    AND coalesce(g.management_stage, g.age_band, 'UNKNOWN') IN ('Non-Pregnant', 'Buck')
  GROUP BY 1
),
adult_w1_ok AS (
  SELECT
    count(*) = 2
    AND bool_or(administered_date = DATE '2026-06-30' AND rows = 85)
    AND bool_or(administered_date = DATE '2026-07-01' AND rows = 239)
      AS ok
  FROM adult_w1
)
SELECT 'adult_w1_restored' AS check_name, ok, jsonb_agg(to_jsonb(adult_w1) ORDER BY administered_date) AS detail
FROM adult_w1_ok, adult_w1
GROUP BY ok;

SELECT
  'pre_only_goats_restored' AS check_name,
  count(*) = 3 AS ok,
  jsonb_agg(jsonb_build_object(
    'display_id', g.display_id,
    'dose_code', pr.dose_code,
    'administered_date', vc.administered_at::date
  ) ORDER BY g.display_id, pr.dose_code) AS detail
FROM vaccination_completions vc
JOIN obligation_instances oi
  ON oi.tenant_id = vc.tenant_id
 AND oi.obligation_id = vc.obligation_id
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
JOIN goats g
  ON g.tenant_id = vc.tenant_id
 AND g.goat_id = vc.goat_id
WHERE g.display_id IN ('G-000106', 'G-000254', 'G-000255')
  AND pr.dose_code = 'et_tt_adult_w1'
  AND vc.status = 'accepted';

SELECT
  'video_task_accepted' AS check_name,
  t.state = 'accepted'
    AND t.verified_by = '2050cd6e-e02b-5681-be7d-4a78a508102e'::uuid
    AND t.verified_at = TIMESTAMPTZ '2026-07-27 18:30:00+05:30'
    AND t.context->>'closed_by' = 'f94de67d-c8b0-527a-a285-857946dc4c95'
    AND t.context->>'closed_by_display_name' = 'Chandrakant'
      AS ok,
  jsonb_build_object(
    'state', t.state,
    'verified_by', t.verified_by,
    'verified_at', t.verified_at,
    'closed_by', t.context->>'closed_by',
    'closed_at', t.context->>'closed_at'
  ) AS detail
FROM sop_tasks t
WHERE t.task_id = '02e44a4f-0cd3-4f1e-8d5e-395f43dd2505';

SELECT
  'video_proof_refs_preserved' AS check_name,
  count(*) = 4 AND sum(jsonb_array_length(proof_refs)) = 6 AS ok,
  jsonb_build_object(
    'submissions', count(*),
    'proof_refs', sum(jsonb_array_length(proof_refs)),
    'submission_states', jsonb_object_agg(state, state_count)
  ) AS detail
FROM (
  SELECT s.*, count(*) OVER (PARTITION BY state) AS state_count
  FROM sop_submissions s
  WHERE s.task_id = '02e44a4f-0cd3-4f1e-8d5e-395f43dd2505'
) s;

SELECT
  'sop_submission_items_accepted' AS check_name,
  count(*) = 209 AND bool_and(i.state = 'accepted') AS ok,
  jsonb_object_agg(i.state, state_count) AS detail
FROM (
  SELECT i.*, count(*) OVER (PARTITION BY i.state) AS state_count
  FROM sop_submission_items i
  WHERE i.task_id = '02e44a4f-0cd3-4f1e-8d5e-395f43dd2505'
) i;

SELECT
  'future_cpt_adult_obligation_hash' AS check_name,
  encode(digest(string_agg(row_text, E'\n' ORDER BY row_text), 'sha256'), 'hex')
    = 'fa72d16d1e281e4e3c775bf0b97a3ff71c276144207261f2ceb6c53f984e8efa' AS ok,
  encode(digest(string_agg(row_text, E'\n' ORDER BY row_text), 'sha256'), 'hex') AS detail
FROM (
  SELECT concat_ws('|',
    oi.obligation_id,
    oi.rule_id,
    oi.target_id,
    oi.scope_id,
    oi.due_at,
    oi.status,
    oi.completed_at,
    coalesce(oi.batch_id::text, '')
  ) AS row_text
  FROM obligation_instances oi
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
  JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
  JOIN locations park
    ON park.tenant_id = g.tenant_id
   AND park.location_id = g.park_id
  WHERE park.location_code = 'CPT'
    AND oi.target_type = 'goat'
    AND coalesce(g.management_stage, g.age_band, 'UNKNOWN') IN ('Non-Pregnant', 'Buck')
    AND oi.due_at >= TIMESTAMPTZ '2026-08-05 00:00:00+05:30'
) future_rows;

SELECT
  'weighing_observations_hash' AS check_name,
  count(*) = 317
    AND encode(digest(string_agg(to_jsonb(w)::text, E'\n' ORDER BY w.observation_id::text), 'sha256'), 'hex')
      = '80e580ce91f37985c30a953c31eddba663f31dbfc524b7b0da20cabd78c2294b' AS ok,
  jsonb_build_object('rows', count(*), 'hash', encode(digest(string_agg(to_jsonb(w)::text, E'\n' ORDER BY w.observation_id::text), 'sha256'), 'hex')) AS detail
FROM weighing_observations w;

SELECT
  'weighing_shed_observations_hash' AS check_name,
  count(*) = 11
    AND encode(digest(string_agg(to_jsonb(w)::text, E'\n' ORDER BY w.shed_observation_id::text), 'sha256'), 'hex')
      = 'af11cbac627c116db1d52d67f61eed2e34e7483539d47ae1f10bd10c52c4a894' AS ok,
  jsonb_build_object('rows', count(*), 'hash', encode(digest(string_agg(to_jsonb(w)::text, E'\n' ORDER BY w.shed_observation_id::text), 'sha256'), 'hex')) AS detail
FROM weighing_shed_observations w;

SELECT
  'fk_integrity' AS check_name,
  NOT EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    LEFT JOIN obligation_instances oi
      ON oi.tenant_id = vc.tenant_id
     AND oi.obligation_id = vc.obligation_id
    WHERE oi.obligation_id IS NULL
  )
  AND NOT EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    LEFT JOIN obligation_batches ob
      ON ob.tenant_id = vc.tenant_id
     AND ob.batch_id = vc.batch_id
    WHERE vc.batch_id IS NOT NULL
      AND ob.batch_id IS NULL
  )
  AND NOT EXISTS (
    SELECT 1
    FROM obligation_instances oi
    LEFT JOIN protocol_rules pr
      ON pr.tenant_id = oi.tenant_id
     AND pr.rule_id = oi.rule_id
    WHERE pr.rule_id IS NULL
  ) AS ok,
  '{}'::jsonb AS detail;

ROLLBACK;
