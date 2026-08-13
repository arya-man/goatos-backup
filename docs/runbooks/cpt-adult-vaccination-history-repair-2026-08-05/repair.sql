\set ON_ERROR_STOP on

-- Guarded repair for CPT adult vaccination history collapse.
--
-- This script is intentionally not self-contained. Before running it on any DB,
-- load the pre-collapse extracts into these staging tables in schema
-- forensic_repair:
--
--   forensic_repair.pre_needed_obligation_batches
--   forensic_repair.pre_needed_obligation_instances
--   forensic_repair.pre_needed_vaccination_completions
--
-- The extracts used for local validation were produced from Cloud SQL backup
-- 1785879718314 and live at:
--   outputs/stg-history-audit/local-forensics-20260805T170257/
--
-- Example local load:
--   CREATE SCHEMA IF NOT EXISTS forensic_repair;
--   CREATE TABLE forensic_repair.pre_needed_obligation_batches
--     (LIKE obligation_batches INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
--   CREATE TABLE forensic_repair.pre_needed_obligation_instances
--     (LIKE obligation_instances INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
--   CREATE TABLE forensic_repair.pre_needed_vaccination_completions
--     (LIKE vaccination_completions INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
--   \copy forensic_repair.pre_needed_obligation_batches FROM 'outputs/.../pre_needed_obligation_batches.csv' CSV HEADER
--   \copy forensic_repair.pre_needed_obligation_instances FROM 'outputs/.../pre_needed_obligation_instances.csv' CSV HEADER
--   \copy forensic_repair.pre_needed_vaccination_completions FROM 'outputs/.../pre_needed_vaccination_completions.csv' CSV HEADER
--
-- Run only with:
--   psql "$DATABASE_URL" -v apply_repair=yes -f docs/runbooks/cpt-adult-vaccination-history-repair-2026-08-05/repair.sql

\if :{?apply_repair}
\else
  \echo 'refusing to run: pass -v apply_repair=yes'
  SELECT 1/0;
\endif

\if :apply_repair
\else
  \echo 'refusing to run: apply_repair must be exactly yes'
  SELECT 1/0;
\endif

BEGIN;

DO $$
DECLARE
  missing_batches int;
  missing_obligations int;
  missing_completions int;
BEGIN
  SELECT count(*) INTO missing_batches
  FROM information_schema.tables
  WHERE table_schema = 'forensic_repair'
    AND table_name = 'pre_needed_obligation_batches';

  SELECT count(*) INTO missing_obligations
  FROM information_schema.tables
  WHERE table_schema = 'forensic_repair'
    AND table_name = 'pre_needed_obligation_instances';

  SELECT count(*) INTO missing_completions
  FROM information_schema.tables
  WHERE table_schema = 'forensic_repair'
    AND table_name = 'pre_needed_vaccination_completions';

  IF missing_batches <> 1 OR missing_obligations <> 1 OR missing_completions <> 1 THEN
    RAISE EXCEPTION 'missing forensic_repair pre-collapse staging tables';
  END IF;
END $$;

INSERT INTO obligation_batches (
  batch_id, tenant_id, protocol_version_id, scope_type, scope_id, session,
  planned_date, window_start, window_end, status, estimated_targets,
  planned_quantity, reserved_quantity, used_quantity, quantity_unit,
  primary_inventory_lot_id, sop_task_id, conducted_by, proof_ref, context,
  created_at, updated_at, row_version
)
SELECT
  batch_id, tenant_id, protocol_version_id, scope_type, scope_id, session,
  planned_date, window_start, window_end, status, estimated_targets,
  planned_quantity, reserved_quantity, used_quantity, quantity_unit,
  primary_inventory_lot_id, sop_task_id, conducted_by, proof_ref, context,
  created_at, updated_at, row_version
FROM forensic_repair.pre_needed_obligation_batches
ON CONFLICT (tenant_id, batch_id) DO UPDATE SET
  protocol_version_id = EXCLUDED.protocol_version_id,
  scope_type = EXCLUDED.scope_type,
  scope_id = EXCLUDED.scope_id,
  session = EXCLUDED.session,
  planned_date = EXCLUDED.planned_date,
  window_start = EXCLUDED.window_start,
  window_end = EXCLUDED.window_end,
  status = EXCLUDED.status,
  estimated_targets = EXCLUDED.estimated_targets,
  planned_quantity = EXCLUDED.planned_quantity,
  reserved_quantity = EXCLUDED.reserved_quantity,
  used_quantity = EXCLUDED.used_quantity,
  quantity_unit = EXCLUDED.quantity_unit,
  primary_inventory_lot_id = EXCLUDED.primary_inventory_lot_id,
  sop_task_id = EXCLUDED.sop_task_id,
  conducted_by = EXCLUDED.conducted_by,
  proof_ref = EXCLUDED.proof_ref,
  context = EXCLUDED.context,
  updated_at = now(),
  row_version = obligation_batches.row_version + 1;

INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
  target_type, target_id, scope_type, scope_id, due_at, window_start,
  window_end, status, sop_task_id, idempotency_key, generated_by_trigger_id,
  sequence, completed_at, created_at, updated_at, row_version,
  batching_hold_count, first_batching_hold_until
)
SELECT
  obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
  target_type, target_id, scope_type, scope_id, due_at, window_start,
  window_end, status, sop_task_id, idempotency_key, generated_by_trigger_id,
  sequence, completed_at, created_at, updated_at, row_version,
  batching_hold_count, first_batching_hold_until
FROM forensic_repair.pre_needed_obligation_instances
ON CONFLICT (tenant_id, obligation_id) DO NOTHING;

INSERT INTO vaccination_completions (
  completion_id, tenant_id, obligation_id, batch_id, goat_id,
  sop_submission_item_id, vaccine_inventory_lot_id, doses, dose_ml_given,
  route_site, adverse_reaction, adverse_reaction_problem_id,
  cold_chain_verified, administered_at, status, verified_by, verified_at,
  rejection_reason, withdrawal_until_date, recorded_by, idempotency_key,
  row_version, created_at, updated_at
)
SELECT
  completion_id, tenant_id, obligation_id, batch_id, goat_id,
  sop_submission_item_id, vaccine_inventory_lot_id, doses, dose_ml_given,
  route_site, adverse_reaction, adverse_reaction_problem_id,
  cold_chain_verified, administered_at, status, verified_by, verified_at,
  rejection_reason, withdrawal_until_date, recorded_by, idempotency_key,
  row_version, created_at, updated_at
FROM forensic_repair.pre_needed_vaccination_completions
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING;

UPDATE obligation_instances oi
SET batch_id = pre.batch_id,
    updated_at = now(),
    row_version = oi.row_version + 1
FROM forensic_repair.pre_needed_obligation_instances pre
WHERE oi.tenant_id = pre.tenant_id
  AND oi.obligation_id = pre.obligation_id
  AND oi.batch_id IS DISTINCT FROM pre.batch_id;

UPDATE vaccination_completions vc
SET batch_id = pre.batch_id,
    updated_at = now(),
    row_version = vc.row_version + 1
FROM forensic_repair.pre_needed_vaccination_completions pre
WHERE vc.tenant_id = pre.tenant_id
  AND vc.completion_id = pre.completion_id
  AND vc.batch_id IS DISTINCT FROM pre.batch_id;

CREATE TEMP TABLE cpt_adult_w2_repair_split ON COMMIT DROP AS
WITH base AS (
  SELECT
    vc.tenant_id,
    vc.completion_id,
    vc.obligation_id,
    g.display_id,
    vc.administered_at,
    oi.completed_at,
    CASE
      WHEN vc.administered_at::date = DATE '2026-07-24' THEN DATE '2026-07-24'
      WHEN row_number() OVER (
        PARTITION BY CASE WHEN vc.administered_at::date <> DATE '2026-07-24' THEN 1 ELSE 0 END
        ORDER BY g.display_id, vc.completion_id
      ) <= 163 THEN DATE '2026-07-25'
      ELSE DATE '2026-07-26'
    END AS repaired_date
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
  JOIN locations park
    ON park.tenant_id = g.tenant_id
   AND park.location_id = g.park_id
  WHERE park.location_code = 'CPT'
    AND pr.dose_code = 'et_tt_adult_w2'
    AND vc.status = 'accepted'
    AND coalesce(g.management_stage, g.age_band, 'UNKNOWN') IN ('Non-Pregnant', 'Buck')
)
SELECT *
FROM base;

DO $$
DECLARE
  rows_24 int;
  rows_25 int;
  rows_26 int;
BEGIN
  SELECT count(*) FILTER (WHERE repaired_date = DATE '2026-07-24'),
         count(*) FILTER (WHERE repaired_date = DATE '2026-07-25'),
         count(*) FILTER (WHERE repaired_date = DATE '2026-07-26')
    INTO rows_24, rows_25, rows_26
  FROM cpt_adult_w2_repair_split;

  IF rows_24 <> 114 OR rows_25 <> 163 OR rows_26 <> 47 THEN
    RAISE EXCEPTION 'unexpected CPT adult W2 split before repair: 24=%, 25=%, 26=%',
      rows_24, rows_25, rows_26;
  END IF;
END $$;

UPDATE vaccination_completions vc
SET administered_at = split.repaired_date + (vc.administered_at - vc.administered_at::date),
    verified_by = '2050cd6e-e02b-5681-be7d-4a78a508102e'::uuid,
    verified_at = TIMESTAMPTZ '2026-07-27 18:30:00+05:30',
    updated_at = now(),
    row_version = vc.row_version + 1
FROM cpt_adult_w2_repair_split split
WHERE vc.tenant_id = split.tenant_id
  AND vc.completion_id = split.completion_id;

UPDATE obligation_instances oi
SET completed_at = split.repaired_date + (coalesce(oi.completed_at, split.administered_at) - coalesce(oi.completed_at, split.administered_at)::date),
    updated_at = now(),
    row_version = oi.row_version + 1
FROM cpt_adult_w2_repair_split split
WHERE oi.tenant_id = split.tenant_id
  AND oi.obligation_id = split.obligation_id;

UPDATE sop_submission_items
SET state = 'accepted'
WHERE task_id = '02e44a4f-0cd3-4f1e-8d5e-395f43dd2505'
  AND state IS DISTINCT FROM 'accepted';

UPDATE sop_submissions
SET state = 'accepted',
    accepted_at = TIMESTAMPTZ '2026-07-27 18:30:00+05:30',
    row_version = row_version + 1
WHERE task_id = '02e44a4f-0cd3-4f1e-8d5e-395f43dd2505'
  AND (state IS DISTINCT FROM 'accepted'
       OR accepted_at IS DISTINCT FROM TIMESTAMPTZ '2026-07-27 18:30:00+05:30');

UPDATE sop_tasks
SET state = 'accepted',
    verified_by = '2050cd6e-e02b-5681-be7d-4a78a508102e'::uuid,
    verified_at = TIMESTAMPTZ '2026-07-27 18:30:00+05:30',
    context = context
      || jsonb_build_object(
        'closed_by', 'f94de67d-c8b0-527a-a285-857946dc4c95',
        'closed_at', '2026-07-27T18:30:00+05:30',
        'closed_by_display_name', 'Chandrakant',
        'verified_by_display_name', 'Jyothi',
        'forensic_repair', 'cpt_adult_et_tt_24_25_26_clubbed_pre_post'
      ),
    updated_at = now(),
    row_version = row_version + 1
WHERE task_id = '02e44a4f-0cd3-4f1e-8d5e-395f43dd2505';

DO $$
DECLARE
  rows_24 int;
  rows_25 int;
  rows_26 int;
BEGIN
  SELECT count(*) FILTER (WHERE administered_at::date = DATE '2026-07-24' AND completed_at::date = DATE '2026-07-24'),
         count(*) FILTER (WHERE administered_at::date = DATE '2026-07-25' AND completed_at::date = DATE '2026-07-25'),
         count(*) FILTER (WHERE administered_at::date = DATE '2026-07-26' AND completed_at::date = DATE '2026-07-26')
    INTO rows_24, rows_25, rows_26
  FROM cpt_adult_w2_repair_split split
  JOIN vaccination_completions vc
    ON vc.tenant_id = split.tenant_id
   AND vc.completion_id = split.completion_id
  JOIN obligation_instances oi
    ON oi.tenant_id = split.tenant_id
   AND oi.obligation_id = split.obligation_id;

  IF rows_24 <> 114 OR rows_25 <> 163 OR rows_26 <> 47 THEN
    RAISE EXCEPTION 'unexpected CPT adult W2 split after repair: 24=%, 25=%, 26=%',
      rows_24, rows_25, rows_26;
  END IF;
END $$;

COMMIT;
