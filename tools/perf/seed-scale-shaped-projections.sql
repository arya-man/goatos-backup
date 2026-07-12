\set ON_ERROR_STOP on

BEGIN;
SET LOCAL synchronous_commit = off;
SET LOCAL maintenance_work_mem = '1GB';

-- The million-row CI load is a disposable fresh-database fixture. Building
-- every production secondary index in bulk is materially faster than updating
-- fourteen indexes row-by-row. All indexes are restored before COMMIT, so the
-- plans are still measured against the exact serving schema.
\if :bulk_load
DROP INDEX IF EXISTS process_integrity_projection_rows_hot_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_scope_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_work_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_severity_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_protocol_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_owner_gin_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_due_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_version_row_uidx;
DROP INDEX IF EXISTS process_integrity_projection_rows_serving_hot_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_serving_work_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_serving_scope_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_serving_severity_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_serving_protocol_idx;
DROP INDEX IF EXISTS process_integrity_projection_rows_serving_due_idx;
\endif

-- Canonical non-empty fixture for vaccination execution/operations/shed APIs.
-- This is the local-laptop shape (1,300 animals). The separate projection
-- inserts below establish the million-cardinality command-lens shape.
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES (
  'f4000000-0000-4000-8000-000000000001'::uuid,
  :'tenant_id'::uuid,
  'shed',
  'SCALE-CI-SHED',
  'Scale CI Shed',
  '00000000-0000-4000-8000-000000003001'::uuid,
  'active'
)
ON CONFLICT (location_id) DO NOTHING;

INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES (
  'f3000000-0000-4000-8000-000000000001'::uuid,
  :'tenant_id'::uuid,
  'vaccination.scale_ci',
  'Scale CI Vaccination',
  'vaccination',
  'draft'
)
ON CONFLICT (protocol_id) DO NOTHING;

INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, version, status,
  effective_from, rule_dsl, proof_policy
) VALUES (
  'f3000000-0000-4000-8000-000000000002'::uuid,
  :'tenant_id'::uuid,
  'f3000000-0000-4000-8000-000000000001'::uuid,
  'tenant',
  1,
  'draft',
  current_date,
  '{}'::jsonb,
  '{}'::jsonb
)
ON CONFLICT (protocol_version_id) DO NOTHING;

INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  eligibility_json, proof_policy
) VALUES (
  'f3000000-0000-4000-8000-000000000003'::uuid,
  :'tenant_id'::uuid,
  'f3000000-0000-4000-8000-000000000002'::uuid,
  'SCALE_CI_D1',
  1,
  'birth_age',
  '{}'::jsonb,
  '{}'::jsonb
)
ON CONFLICT (rule_id) DO NOTHING;

DELETE FROM obligation_instances
WHERE tenant_id = :'tenant_id'::uuid
  AND idempotency_key LIKE 'scale-ci-obligation:%';

DELETE FROM goats
WHERE tenant_id = :'tenant_id'::uuid
  AND goat_id::text LIKE 'f1000000-0000-4000-8000-%';

INSERT INTO goats (
  goat_id, tenant_id, display_id, species, sex, lifecycle_status,
  custodian_party_id, current_location_id, park_id, shed_id, management_stage, health_status
)
SELECT
  ('f1000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
  :'tenant_id'::uuid,
  'G-' || lpad((900000 + n)::text, 6, '0'),
  'goat',
  CASE WHEN n % 2 = 0 THEN 'female' ELSE 'male' END,
  'alive',
  '00000000-0000-4000-8000-000000001001'::uuid,
  'f4000000-0000-4000-8000-000000000001'::uuid,
  '00000000-0000-4000-8000-000000003001'::uuid,
  'f4000000-0000-4000-8000-000000000001'::uuid,
  'K1',
  'healthy'
FROM generate_series(1, 1300) AS generated(n);

INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence
)
SELECT
  ('f2000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
  :'tenant_id'::uuid,
  'f3000000-0000-4000-8000-000000000002'::uuid,
  'f3000000-0000-4000-8000-000000000003'::uuid,
  'goat',
  ('f1000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
  'shed',
  'f4000000-0000-4000-8000-000000000001'::uuid,
  now() + ((n % 21) - 10) * interval '1 day',
  CASE WHEN n % 5 = 0 THEN 'scheduled' ELSE 'due' END,
  'scale-ci-obligation:' || n,
  1
FROM generate_series(1, 1300) AS generated(n);

DELETE FROM process_integrity_projection_rows
WHERE tenant_id = :'tenant_id'::uuid
  AND row_id LIKE 'scale:%';

INSERT INTO process_integrity_projection_rows (
  tenant_id, category, row_id, sort_priority, process_key, obligation_id,
  due_at, expected_count, obligation_status, work_state, gap_type, severity,
  owner_state, next_action, process_intact, owner_refs, projection_version, projected_at
)
SELECT
  :'tenant_id'::uuid,
  'vaccination',
  'scale:' || n,
  (n % 11)::int,
  'scale-process:' || n,
  'scale-obligation:' || n,
  date_trunc('day', now()) + ((n % 365) - 182) * interval '1 day' + (n % 86400) * interval '1 second',
  1,
  CASE WHEN n % 7 = 0 THEN 'completed' ELSE 'due' END,
  CASE
    WHEN n % 13 = 0 THEN 'blocked'
    WHEN n % 11 = 0 THEN 'overdue'
    WHEN n % 7 = 0 THEN 'completed'
    ELSE 'due'
  END,
  CASE WHEN n % 13 = 0 THEN 'owner_missing' ELSE '' END,
  CASE WHEN n % 13 = 0 THEN 'broken' WHEN n % 11 = 0 THEN 'at_risk' ELSE 'watch' END,
  CASE WHEN n % 13 = 0 THEN 'missing' ELSE 'assigned' END,
  'Open vaccination work',
  n % 7 = 0,
  ARRAY['scale-owner-' || (n % 1000)],
  9001,
  now()
FROM generate_series(1, (:scale_rows)::int) AS generated(n);

INSERT INTO process_integrity_projection_state (
  tenant_id, projection_version, serving_projection_version, projected_at, as_of,
  row_count, freshness_status, serving_state, updated_at
) VALUES (
  :'tenant_id'::uuid, 9001, 9001, now(), now(), (:scale_rows)::bigint,
  'green', 'fresh', now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
  projection_version = EXCLUDED.projection_version,
  serving_projection_version = EXCLUDED.serving_projection_version,
  projected_at = EXCLUDED.projected_at,
  as_of = EXCLUDED.as_of,
  row_count = EXCLUDED.row_count,
  freshness_status = EXCLUDED.freshness_status,
  serving_state = EXCLUDED.serving_state,
  updated_at = EXCLUDED.updated_at;

DELETE FROM process_integrity_projection_summaries
WHERE tenant_id = :'tenant_id'::uuid
  AND projection_version = 9001;

-- The adversarial row projection has one million distinct timestamps. The
-- request-path summary must collapse those timestamps to India business dates,
-- keeping the widest-date summary grain bounded.
INSERT INTO process_integrity_projection_summaries (
  tenant_id, projection_version, category, park_id, shed_id, owner_id,
  protocol_version_id, due_business_date, work_state, severity, process_intact,
  row_count, expected_count, completed_count, deferred_count, projected_at
)
SELECT
  tenant_id,
  projection_version,
  category,
  park_id,
  shed_id,
  ''::text,
  protocol_version_id,
  (due_at AT TIME ZONE 'Asia/Kolkata')::date,
  work_state,
  severity,
  process_intact,
  count(*)::bigint,
  sum(expected_count)::bigint,
  sum(completed_count)::bigint,
  sum(CASE WHEN work_state = 'deferred' THEN GREATEST(deferred_count, 1) ELSE 0 END)::bigint,
  max(projected_at)
FROM process_integrity_projection_rows
WHERE tenant_id = :'tenant_id'::uuid
  AND projection_version = 9001
  AND row_id LIKE 'scale:%'
GROUP BY
  tenant_id, projection_version, category, park_id, shed_id,
  protocol_version_id, (due_at AT TIME ZONE 'Asia/Kolkata')::date,
  work_state, severity, process_intact;

\if :bulk_load
CREATE INDEX process_integrity_projection_rows_hot_idx
  ON process_integrity_projection_rows (tenant_id, category, sort_priority, due_at, row_id);
CREATE INDEX process_integrity_projection_rows_scope_idx
  ON process_integrity_projection_rows (tenant_id, category, park_id, shed_id, due_at, row_id);
CREATE INDEX process_integrity_projection_rows_work_idx
  ON process_integrity_projection_rows (tenant_id, category, work_state, sort_priority, due_at, row_id);
CREATE INDEX process_integrity_projection_rows_severity_idx
  ON process_integrity_projection_rows (tenant_id, category, severity, sort_priority, due_at, row_id);
CREATE INDEX process_integrity_projection_rows_protocol_idx
  ON process_integrity_projection_rows (tenant_id, category, protocol_version_id, due_at, row_id);
CREATE INDEX process_integrity_projection_rows_owner_gin_idx
  ON process_integrity_projection_rows USING gin (owner_refs);
CREATE INDEX process_integrity_projection_rows_due_idx
  ON process_integrity_projection_rows (tenant_id, category, due_at, row_id);
CREATE UNIQUE INDEX process_integrity_projection_rows_version_row_uidx
  ON process_integrity_projection_rows (tenant_id, projection_version, row_id);
CREATE INDEX process_integrity_projection_rows_serving_hot_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, sort_priority, due_at, row_id);
CREATE INDEX process_integrity_projection_rows_serving_work_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, work_state, sort_priority, due_at, row_id);
CREATE INDEX process_integrity_projection_rows_serving_scope_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, park_id, shed_id, due_at, row_id);
CREATE INDEX process_integrity_projection_rows_serving_severity_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, severity, sort_priority, due_at, row_id);
CREATE INDEX process_integrity_projection_rows_serving_protocol_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, protocol_version_id, due_at, row_id);
CREATE INDEX process_integrity_projection_rows_serving_due_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, due_at, row_id);
\endif

\if :bulk_load
DROP INDEX IF EXISTS calendar_event_projections_hot_list_idx;
DROP INDEX IF EXISTS calendar_event_projections_owner_window_idx;
DROP INDEX IF EXISTS calendar_event_projections_status_window_idx;
DROP INDEX IF EXISTS calendar_event_projections_scope_window_idx;
DROP INDEX IF EXISTS calendar_event_projections_due_reminder_idx;
DROP INDEX IF EXISTS calendar_event_projections_closed_prune_idx;
\endif

DELETE FROM calendar_event_projections
WHERE tenant_id = :'tenant_id'::uuid
  AND event_id LIKE 'scale:%';

INSERT INTO calendar_event_projections (
  tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle,
  status, severity, due_at, target_type, target_count, assignee_label,
  executor_role, source_backed, source_label, links, detail
)
SELECT
  :'tenant_id'::uuid,
  'scale:' || n,
  'vaccination',
  'vaccination_drive',
  'pc',
  'Scale-shaped park vaccination drive ' || n,
  'CI projection-cardinality fixture',
  CASE WHEN n % 11 = 0 THEN 'overdue' ELSE 'scheduled' END,
  CASE WHEN n % 11 = 0 THEN 'warning' ELSE 'info' END,
  date_trunc('day', now()) + ((n % 365) - 182) * interval '1 day' + (n % 86400) * interval '1 second',
  'park',
  1,
  'Scale CI owner',
  'pc_vaccinator',
  false,
  'Generated scale-shaped projection fixture; not business truth',
  '{}'::jsonb,
  jsonb_build_object('summary', jsonb_build_object('shed_count', 1, 'vaccine_count', 1, 'drive_count', 1))
FROM generate_series(1, (:scale_rows)::int) AS generated(n);

INSERT INTO calendar_projection_state (
  tenant_id, slice_key, projection_version, projected_at, date_from, date_to,
  freshness_status, serving_state, last_error, updated_at
) VALUES (
  :'tenant_id'::uuid, 'vaccination', 9001, now(),
  now() - interval '183 days', now() + interval '183 days',
  'green', 'fresh', NULL, now()
)
ON CONFLICT (tenant_id, slice_key) DO UPDATE SET
  projection_version = EXCLUDED.projection_version,
  projected_at = EXCLUDED.projected_at,
  date_from = EXCLUDED.date_from,
  date_to = EXCLUDED.date_to,
  freshness_status = EXCLUDED.freshness_status,
  serving_state = EXCLUDED.serving_state,
  last_error = NULL,
  updated_at = EXCLUDED.updated_at;

\if :bulk_load
CREATE INDEX calendar_event_projections_hot_list_idx
  ON calendar_event_projections (tenant_id, slice_key, system, due_at, event_id)
  INCLUDE (owner_key, status, severity, park_id, shed_id)
  WHERE system = false AND due_at IS NOT NULL;
CREATE INDEX calendar_event_projections_owner_window_idx
  ON calendar_event_projections (tenant_id, slice_key, owner_key, due_at, event_id)
  WHERE system = false AND due_at IS NOT NULL;
CREATE INDEX calendar_event_projections_status_window_idx
  ON calendar_event_projections (tenant_id, slice_key, status, due_at, event_id)
  WHERE system = false AND due_at IS NOT NULL;
CREATE INDEX calendar_event_projections_scope_window_idx
  ON calendar_event_projections (tenant_id, slice_key, park_id, shed_id, due_at, event_id)
  WHERE system = false AND due_at IS NOT NULL;
CREATE INDEX calendar_event_projections_due_reminder_idx
  ON calendar_event_projections (tenant_id, slice_key, reminder_state, due_at, event_id)
  WHERE system = false
    AND due_at IS NOT NULL
    AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending',
                   'verification_pending', 'rework_due', 'deferred', 'blocked');
CREATE INDEX calendar_event_projections_closed_prune_idx
  ON calendar_event_projections (tenant_id, slice_key, due_at, event_id)
  WHERE system = false
    AND status IN ('completed', 'canceled')
    AND due_at IS NOT NULL;
\endif

-- A million-animal-equivalent shed projection uses 1,000 shed rows with
-- 1,000 animals each. The row cardinality matches the bounded shed list while
-- SUM(animals) proves the scale shape without manufacturing one million sheds.
DELETE FROM vaccination_shed_projection_rows
WHERE tenant_id = :'tenant_id'::uuid
  AND projection_version = 9001;

INSERT INTO vaccination_shed_projection_rows (
  tenant_id, park_id, park_name, shed_id, shed_name, animals, due_animals,
  open_cells, sessions, capacity_status, shed_status, last_done, next_due,
  projection_version, projected_at
)
SELECT
  :'tenant_id'::uuid,
  'e4000000-0000-4000-8000-' || lpad(((n - 1) / 100 + 1)::text, 12, '0'),
  'Scale Park ' || lpad(((n - 1) / 100 + 1)::text, 4, '0'),
  'e5000000-0000-4000-8000-' || lpad(n::text, 12, '0'),
  'Scale Shed ' || lpad(n::text, 6, '0'),
  1000,
  CASE WHEN n % 7 = 0 THEN 100 ELSE 20 END,
  CASE WHEN n % 7 = 0 THEN 100 ELSE 20 END,
  CASE WHEN n % 7 = 0 THEN 4 ELSE 1 END,
  CASE WHEN n % 7 = 0 THEN 'over_cap' ELSE 'within_cap' END,
  CASE WHEN n % 11 = 0 THEN 'overdue' ELSE 'scheduled' END,
  now() - (n % 30) * interval '1 day',
  now() + (n % 30) * interval '1 day',
  9001,
  now()
FROM generate_series(1, 1000) AS generated(n);

INSERT INTO vaccination_shed_projection_state (
  tenant_id, projection_version, serving_projection_version, projected_at,
  as_of, due_before, row_count, freshness_status, serving_state, updated_at
) VALUES (
  :'tenant_id'::uuid, 9001, 9001, now(), now(), now() + interval '30 days', 1000,
  'green', 'fresh', now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
  projection_version = EXCLUDED.projection_version,
  serving_projection_version = EXCLUDED.serving_projection_version,
  projected_at = EXCLUDED.projected_at,
  as_of = EXCLUDED.as_of,
  due_before = EXCLUDED.due_before,
  row_count = EXCLUDED.row_count,
  freshness_status = EXCLUDED.freshness_status,
  serving_state = EXCLUDED.serving_state,
  last_error = NULL,
  updated_at = EXCLUDED.updated_at;

DELETE FROM vaccination_execution_projection_rows
WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001;

INSERT INTO vaccination_execution_projection_rows (
  tenant_id, projection_version, park_id, park_name, shed_id, shed_name,
  animal_stage, batch_id, protocol_name, dose_code, due_at, obligation_count,
  scheduled_count, due_count, in_progress_count, completed_count, missed_count,
  deferred_count, canceled_count, completion_recorded, completion_accepted,
  completion_rejected, completion_reversed, batch_status, task_state,
  operator_name, park_head_name, verifier_name, usable_for_vaccination,
  is_quarantine, is_icu, health_deferred_count, obligation_id, sop_task_id,
  sop_version_id, sop_task_row_version, completion_id, work_state, severity,
  sort_rank, sort_due_micros, sort_row_key, projected_at
) VALUES (
  :'tenant_id'::uuid, 9001,
  '00000000-0000-4000-8000-000000003001', 'Scale CI Park',
  'f4000000-0000-4000-8000-000000000001', 'Scale CI Shed',
  'K1', NULL, 'Scale CI Vaccination', 'SCALE_CI_D1', now(),
  1300, 260, 1040, 0, 0, 0, 0, 0, 0, 0, 0, 0,
  NULL, NULL, 'Scale CI owner', NULL, NULL, true, false, false, 0,
  'f2000000-0000-4000-8000-000000000001', NULL, NULL, NULL, NULL,
  'due', 'watch', 20, (extract(epoch FROM now()) * 1000000)::bigint,
  'scale-ci-execution-row', now()
);

INSERT INTO vaccination_execution_projection_state (
  tenant_id, projection_version, serving_projection_version, projected_at,
  as_of, due_before, closed_after, row_count, freshness_status, serving_state,
  last_error, updated_at
) VALUES (
  :'tenant_id'::uuid, 9001, 9001, now(), now(),
  now() + interval '30 days', now() - interval '14 days', 1,
  'green', 'fresh', NULL, now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
  projection_version = EXCLUDED.projection_version,
  serving_projection_version = EXCLUDED.serving_projection_version,
  projected_at = EXCLUDED.projected_at,
  as_of = EXCLUDED.as_of,
  due_before = EXCLUDED.due_before,
  closed_after = EXCLUDED.closed_after,
  row_count = EXCLUDED.row_count,
  freshness_status = EXCLUDED.freshness_status,
  serving_state = EXCLUDED.serving_state,
  last_error = NULL,
  updated_at = EXCLUDED.updated_at;

DELETE FROM vaccination_operations_projection_rows
WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001;

INSERT INTO vaccination_operations_projection_rows (
  tenant_id, projection_version, park_id, park_name, shed_id, shed_name,
  stage, age_band, protocol_id, protocol_name, animals, next_due, last_dose,
  overdue_count, due_count, in_progress_count, scheduled_count, missed_count,
  deferred_count, accepted_count, proof_pending_count, rejected_count,
  total_count, projected_at
) VALUES (
  :'tenant_id'::uuid, 9001,
  '00000000-0000-4000-8000-000000003001', 'Scale CI Park',
  'f4000000-0000-4000-8000-000000000001', 'Scale CI Shed',
  'K1', 'scale-ci', 'f3000000-0000-4000-8000-000000000001',
  'Scale CI Vaccination', 1300, now(), NULL,
  0, 1040, 0, 260, 0, 0, 0, 0, 0, 1, now()
);

INSERT INTO vaccination_operations_projection_state (
  tenant_id, projection_version, serving_projection_version, projected_at,
  as_of, due_before, row_count, freshness_status, serving_state, last_error,
  updated_at
) VALUES (
  :'tenant_id'::uuid, 9001, 9001, now(), now(),
  now() + interval '30 days', 1, 'green', 'fresh', NULL, now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
  projection_version = EXCLUDED.projection_version,
  serving_projection_version = EXCLUDED.serving_projection_version,
  projected_at = EXCLUDED.projected_at,
  as_of = EXCLUDED.as_of,
  due_before = EXCLUDED.due_before,
  row_count = EXCLUDED.row_count,
  freshness_status = EXCLUDED.freshness_status,
  serving_state = EXCLUDED.serving_state,
  last_error = NULL,
  updated_at = EXCLUDED.updated_at;

-- Freshness starts when the disposable projection build and index restoration
-- finish, not when the transaction began (now() is transaction-stable).
UPDATE process_integrity_projection_state
SET projected_at = clock_timestamp(),
    as_of = clock_timestamp(),
    freshness_status = 'green',
    serving_state = 'fresh',
    updated_at = clock_timestamp()
WHERE tenant_id = :'tenant_id'::uuid;

UPDATE process_integrity_projection_summaries
SET projected_at = clock_timestamp()
WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001;

UPDATE calendar_projection_state
SET projected_at = clock_timestamp(),
    freshness_status = 'green',
    serving_state = 'fresh',
    updated_at = clock_timestamp()
WHERE tenant_id = :'tenant_id'::uuid AND slice_key = 'vaccination';

UPDATE vaccination_shed_projection_state
SET projected_at = clock_timestamp(),
    as_of = clock_timestamp(),
    due_before = clock_timestamp() + interval '30 days',
    freshness_status = 'green',
    serving_state = 'fresh',
    updated_at = clock_timestamp()
WHERE tenant_id = :'tenant_id'::uuid;

UPDATE vaccination_execution_projection_state
SET projected_at = clock_timestamp(),
    as_of = clock_timestamp(),
    due_before = clock_timestamp() + interval '30 days',
    closed_after = clock_timestamp() - interval '14 days',
    freshness_status = 'green',
    serving_state = 'fresh',
    updated_at = clock_timestamp()
WHERE tenant_id = :'tenant_id'::uuid;

UPDATE vaccination_operations_projection_state
SET projected_at = clock_timestamp(),
    as_of = clock_timestamp(),
    due_before = clock_timestamp() + interval '30 days',
    freshness_status = 'green',
    serving_state = 'fresh',
    updated_at = clock_timestamp()
WHERE tenant_id = :'tenant_id'::uuid;

COMMIT;

ANALYZE process_integrity_projection_rows;
ANALYZE process_integrity_projection_state;
ANALYZE process_integrity_projection_summaries;
ANALYZE calendar_event_projections;
ANALYZE calendar_projection_state;
ANALYZE vaccination_shed_projection_rows;
ANALYZE vaccination_shed_projection_state;
ANALYZE vaccination_execution_projection_rows;
ANALYZE vaccination_execution_projection_state;
ANALYZE vaccination_operations_projection_rows;
ANALYZE vaccination_operations_projection_state;
ANALYZE goats;
ANALYZE obligation_instances;
