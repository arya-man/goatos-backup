-- +goose Up
-- Historical environments may have already applied 000110 before the legacy repeat cleanup
-- was added there. Keep the cleanup as its own forward migration so those rows are normalized too.
UPDATE protocol_rules
SET repeat_until_after_age = COALESCE(NULLIF(repeat_until_after_age, ''), repeat),
    eligibility_json = COALESCE(eligibility_json, '{}'::jsonb) ||
      jsonb_build_object('_legacy_unsupported_repeat', repeat),
    repeat = 'none'
WHERE repeat IN ('until_age', 'after_age');

CREATE OR REPLACE VIEW vw_procurement_vaccination_excluded_goats AS
SELECT DISTINCT
  g.tenant_id,
  g.goat_id,
  CASE
    WHEN g.lifecycle_status IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive') THEN g.lifecycle_status
    WHEN g.identity_state IN ('disputed', 'merged', 'inactive') THEN 'identity_conflict'
    WHEN plg.selection_state IN ('rejected', 'arrival_rejected', 'dead', 'sold', 'lost') THEN plg.selection_state
    WHEN plg.current_state IN ('source_rejected', 'pre_dispatch_rejected', 'arrival_rejected', 'dead', 'sold', 'lost', 'canceled') THEN plg.current_state
    WHEN plg.ownership_state = 'not_owned' THEN 'ownership_not_owned'
    ELSE 'not_excluded'
  END AS exclusion_reason
FROM goats g
LEFT JOIN procurement_load_goats plg
  ON plg.tenant_id = g.tenant_id
 AND plg.goat_id = g.goat_id
WHERE g.lifecycle_status IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive')
   OR g.identity_state IN ('disputed', 'merged', 'inactive')
   OR (
     plg.goat_id IS NOT NULL
     AND (
       plg.selection_state IN ('rejected', 'arrival_rejected', 'dead', 'sold', 'lost')
       OR plg.current_state IN ('source_rejected', 'pre_dispatch_rejected', 'arrival_rejected', 'dead', 'sold', 'lost', 'canceled')
       OR plg.ownership_state = 'not_owned'
     )
   );

-- Pre-canonical next_cycle rows may carry an older idempotency key. Collapse duplicate open
-- obligations for the same tenant/version/rule/target/sequence/due_at before enforcing the guard.
WITH ranked AS (
  SELECT
    obligation_id,
    row_number() OVER (
      PARTITION BY tenant_id, protocol_version_id, rule_id, target_type, target_id, "sequence", due_at
      ORDER BY created_at ASC, obligation_id ASC
    ) AS rn
  FROM obligation_instances
  WHERE status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed')
)
UPDATE obligation_instances oi
SET status = 'superseded',
    row_version = row_version + 1,
    updated_at = now()
FROM ranked r
WHERE oi.obligation_id = r.obligation_id
  AND r.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS obligation_instances_open_logical_due_idx
  ON obligation_instances (
    tenant_id,
    protocol_version_id,
    rule_id,
    target_type,
    target_id,
    "sequence",
    due_at
  )
  WHERE status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed');

-- +goose Down
DROP INDEX IF EXISTS obligation_instances_open_logical_due_idx;

CREATE OR REPLACE VIEW vw_procurement_vaccination_excluded_goats AS
SELECT DISTINCT
  g.tenant_id,
  g.goat_id,
  CASE
    WHEN g.lifecycle_status IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive') THEN g.lifecycle_status
    WHEN g.identity_state IN ('disputed', 'merged', 'inactive') THEN 'identity_conflict'
    WHEN plg.identity_review_state <> 'clean' THEN 'identity_' || plg.identity_review_state
    WHEN plg.ownership_state NOT IN ('mesha_owned', 'settled') THEN 'ownership_' || plg.ownership_state
    WHEN plg.health_state <> 'passed' THEN 'health_' || plg.health_state
    WHEN plg.current_state <> 'accepted_herd_intake' THEN plg.current_state
    ELSE 'not_excluded'
  END AS exclusion_reason
FROM goats g
LEFT JOIN procurement_load_goats plg
  ON plg.tenant_id = g.tenant_id
 AND plg.goat_id = g.goat_id
WHERE g.lifecycle_status IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive')
   OR g.identity_state IN ('disputed', 'merged', 'inactive')
   OR (
     plg.goat_id IS NOT NULL
     AND (
       plg.current_state <> 'accepted_herd_intake'
       OR plg.selection_state IN ('source_only', 'candidate', 'rejected', 'deferred', 'blocked', 'arrival_rejected', 'dead', 'sold', 'lost')
       OR plg.identity_review_state <> 'clean'
       OR plg.ownership_state NOT IN ('mesha_owned', 'settled')
       OR plg.health_state <> 'passed'
     )
   );
