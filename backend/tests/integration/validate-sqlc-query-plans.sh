#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
source "$repo_root/tools/postgres-ci.sh"

container_name="goatos-sqlc-plan-validation-$$"
image="${GOATOS_SQLC_POSTGRES_IMAGE:-${GOATOS_POSTGRES_IMAGE:-postgres:16.9-alpine}}"
db_name="goatos"
db_user="postgres"

cleanup() {
  docker rm -f "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

run_psql() {
  postgres_ci_psql "$container_name" "$db_user" "$db_name" "$@"
}

apply_goose_up() {
  local migration="$1"
  awk '
    /^-- \+goose Up/ { in_up = 1; next }
    /^-- \+goose Down/ { in_up = 0 }
    in_up { print }
  ' "$migration" | run_psql
}

explain_must_use_index() {
  local label="$1"
  local forbidden="$2"
  local sql="$3"
  local extra_forbidden="${4:-}"
  local plan

  plan="$(
    {
      printf '%s\n' 'SET enable_seqscan = off;'
      printf '%s\n' "$sql"
    } | run_psql
  )"

  if grep -E "$forbidden" <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Unexpected sequential scan in $label" >&2
    exit 1
  fi
  if ! grep -E '(Index Scan|Index Only Scan|Bitmap Index Scan)' <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Expected indexed plan in $label" >&2
    exit 1
  fi
  if [ -n "$extra_forbidden" ] && grep -E "$extra_forbidden" <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Unexpected plan node in $label matching: $extra_forbidden" >&2
    exit 1
  fi
  echo "Indexed plan observed: $label"
}

validate_identity_lookup_plans() {
  explain_must_use_index "GoatByID" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND goat_id = '10000000-0000-4000-8000-000000000001'::uuid;"

  explain_must_use_index "GoatSearchDisplay" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND identity_state <> 'merged'
ORDER BY display_id ASC
LIMIT 50;" '(Sort|Incremental Sort)'

  explain_must_use_index "IdentifierResolution" 'Seq Scan on goat_identifiers' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM goat_identifiers
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND identifier_type = 'old_tag'
  AND normalized_value = '1900'
  AND status = 'active'
LIMIT 10;"

  explain_must_use_index "GoatTimeline" 'Seq Scan on goat_identity_events' "EXPLAIN (COSTS OFF)
SELECT identity_event_id, occurred_at
FROM goat_identity_events
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND goat_id = '10000000-0000-4000-8000-000000000001'::uuid
ORDER BY occurred_at DESC, identity_event_id DESC
LIMIT 50;"
}

validate_outbox_claim_plan() {
  explain_must_use_index "OutboxClaimPending" 'Seq Scan on outbox_messages' "EXPLAIN (COSTS OFF)
SELECT outbox_id
FROM outbox_messages
WHERE status = 'pending'
  AND (next_attempt_at IS NULL OR next_attempt_at <= '2026-06-09T12:00:00Z'::timestamptz)
ORDER BY created_at, outbox_id
LIMIT 10
FOR UPDATE SKIP LOCKED;"
}

validate_auth_grant_lookup_plan() {
  explain_must_use_index "ActiveTenantRoles" 'Seq Scan on user_scope_grants' "EXPLAIN (COSTS OFF)
SELECT role
FROM user_scope_grants
WHERE user_id = '90000000-0000-4000-8000-000000000001'::uuid
  AND tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND scope_type = 'tenant'
  AND scope_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status = 'active'
  AND valid_from <= now()
  AND (valid_to IS NULL OR valid_to > now())
ORDER BY role;"
}

validate_obligation_due_window_plan() {
  explain_must_use_index "ObligationDueWindow" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT obligation_id, due_at
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status = 'scheduled'
  AND due_at <= TIMESTAMPTZ '2026-12-31 00:00:00+00'
ORDER BY due_at ASC, obligation_id ASC
LIMIT 100;"
}

validate_obligation_scope_count_plan() {
  explain_must_use_index "ObligationCountByScope" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT count(*)
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND scope_type = 'park'
  AND scope_id = '00000000-0000-4000-8000-000000003001'::uuid
  AND status = 'scheduled';"
}

validate_obligation_open_by_goat_plan() {
  explain_must_use_index "ObligationOpenByGoat" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT obligation_id, due_at, status
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND target_type = 'goat'
  AND target_id = '00000000-0000-4000-8000-0000000000aa'::uuid
  AND status IN ('scheduled', 'due', 'in_progress')
ORDER BY due_at ASC, obligation_id ASC
LIMIT 200;"
}

validate_inventory_fefo_plan() {
  explain_must_use_index "InventoryFEFOPick" 'Seq Scan on inventory_stock' "EXPLAIN (COSTS OFF)
SELECT stock_id, expiry_date
FROM inventory_stock
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND location_id = '00000000-0000-4000-8000-000000003001'::uuid
  AND item_id = '00000000-0000-4000-8000-0000000000bb'::uuid
  AND quantity_in_stock > 0
  AND quantity_in_stock > quantity_reserved
ORDER BY expiry_date ASC NULLS LAST, stock_id ASC
LIMIT 1;"
}

validate_inventory_movements_ledger_plan() {
  explain_must_use_index "InventoryMovementsByLot" 'Seq Scan on inventory_stock_movements' "EXPLAIN (COSTS OFF)
SELECT movement_id, occurred_at
FROM inventory_stock_movements
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND lot_id = '00000000-0000-4000-8000-0000000000cc'::uuid
ORDER BY occurred_at;"
}

validate_inventory_batch_reconcile_plan() {
  explain_must_use_index "InventoryBatchStockReconcileCandidates" 'Seq Scan on obligation_batches' "EXPLAIN (COSTS OFF)
SELECT batch_id,
       row_version::text AS repair_token,
       (
         CASE WHEN context #>> '{defer_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{defer_repair,release_qty}', '')::numeric, 0)
              ELSE 0 END
         + CASE WHEN context #>> '{shift_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{shift_repair,release_qty}', '')::numeric, 0)
              ELSE 0 END
         + CASE WHEN context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{cancel_repair,release_qty}', '')::numeric, 0)
              ELSE 0 END
       )::numeric AS release_qty
FROM obligation_batches
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND (
    context #>> '{defer_repair,state}' = 'stock_reconcile_required'
    OR context #>> '{shift_repair,state}' = 'stock_reconcile_required'
    OR context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
  )
ORDER BY updated_at ASC, batch_id ASC
LIMIT 100
FOR UPDATE SKIP LOCKED;"
}

validate_vaccination_generation_scan_plan() {
  explain_must_use_index "VaccinationGenerationKeyset" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id, dob
FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND management_stage = 'K1'
  AND goat_id > '00000000-0000-0000-0000-000000000000'::uuid
ORDER BY goat_id
LIMIT 500;"
}

validate_vaccination_review_queue_plan() {
  explain_must_use_index "VaccinationReviewQueue" 'Seq Scan on vaccination_completions' "EXPLAIN (COSTS OFF)
SELECT completion_id
FROM vaccination_completions
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status = 'recorded'
ORDER BY administered_at ASC, completion_id ASC
LIMIT 100;"
}

validate_vaccination_fanout_plan() {
  explain_must_use_index "VaccinationFanoutByTask" 'Seq Scan on vaccination_completions' "EXPLAIN (COSTS OFF)
SELECT c.completion_id
FROM vaccination_completions c
JOIN sop_submission_items i ON i.tenant_id = c.tenant_id AND i.item_id = c.sop_submission_item_id
JOIN sop_submissions s ON s.tenant_id = i.tenant_id AND s.submission_id = i.submission_id
WHERE c.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND s.task_id = '00000000-0000-4000-8000-0000000000dd'::uuid
  AND c.status = 'recorded'
  AND c.sop_submission_item_id IS NOT NULL
ORDER BY c.completion_id;"
}

validate_sop_failed_submission_fanouts_plan() {
  explain_must_use_index "SOPAgedFailedSubmissionFanouts" 'Seq Scan on sop_task_submission_fanouts|Seq Scan on sop_submission_items|Seq Scan on vaccination_completions' "EXPLAIN (COSTS OFF)
SELECT f.submission_fanout_id,
       count(si.item_id)::int AS eligible_items,
       count(DISTINCT vc.sop_submission_item_id)::int AS materialized_completions
FROM sop_task_submission_fanouts f
JOIN sop_tasks st
  ON st.tenant_id = f.tenant_id
 AND st.task_id = f.task_id
JOIN sop_definitions sd
  ON sd.tenant_id = st.tenant_id
 AND sd.sop_id = st.sop_id
JOIN sop_submissions ss
  ON ss.tenant_id = f.tenant_id
 AND ss.submission_id = f.submission_id
LEFT JOIN sop_submission_items si
  ON si.tenant_id = f.tenant_id
 AND si.task_id = f.task_id
 AND si.submission_id = f.submission_id
 AND si.goat_id IS NOT NULL
 AND si.state IN ('accepted', 'needs_review')
LEFT JOIN vaccination_completions vc
  ON vc.tenant_id = si.tenant_id
 AND vc.sop_submission_item_id = si.item_id
 AND vc.status <> 'reversed'
WHERE f.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND f.status = 'failed'
  AND f.updated_at <= TIMESTAMPTZ '2026-06-24 11:45:00+00'
  AND (
    sd.code IN ('vaccination.drive', 'vaccination.session')
    OR st.task_type IN ('vaccination', 'vaccination_drive', 'vaccination_session')
  )
GROUP BY f.submission_fanout_id, f.updated_at
ORDER BY f.updated_at ASC, f.submission_fanout_id ASC
LIMIT 50;"
}

# Broad Parks base-join guard for the shell hot-path sweep. The exact production
# query is covered by TestParksVaccinationExecutionProductionQueryPlanUsesIndexes.
validate_parks_vaccination_base_join_plan() {
  explain_must_use_index "ParksVaccinationExecutionBaseJoins" 'Seq Scan on obligation_instances|Seq Scan on goats|Seq Scan on vaccination_completions|Seq Scan on locations|Seq Scan on workforce_members|Seq Scan on shed_profiles|Seq Scan on location_operational_attributes' "EXPLAIN (COSTS OFF)
WITH raw AS (
  SELECT
    oi.obligation_id,
    oi.rule_id,
    oi.batch_id,
    oi.due_at,
    oi.status AS obligation_status,
    pr.dose_code,
    pd.name AS protocol_name,
    ob.status AS batch_status,
    ob.conducted_by,
    st.state AS task_state,
    st.assigned_to,
    g.lifecycle_status AS goat_lifecycle_status,
    g.health_status AS goat_health_status,
    g.management_stage AS goat_stage,
    vc.status AS completion_status,
    CASE
      WHEN g.shed_id IS NOT NULL THEN g.shed_id
      WHEN oi.target_type = 'shed' THEN oi.target_id
      WHEN oi.scope_type = 'shed' THEN oi.scope_id
      ELSE NULL
    END AS shed_uuid,
    CASE
      WHEN g.park_id IS NOT NULL THEN g.park_id
      WHEN oi.target_type = 'park' THEN oi.target_id
      WHEN oi.scope_type = 'park' THEN oi.scope_id
      ELSE NULL
    END AS direct_park_uuid
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
  LEFT JOIN goats g
    ON oi.target_type = 'goat'
   AND g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.identity_state <> 'merged'
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN sop_tasks st
    ON st.tenant_id = oi.tenant_id
   AND st.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
  LEFT JOIN vaccination_completions vc
    ON vc.tenant_id = oi.tenant_id
   AND vc.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'completed', 'missed', 'waived')
    AND oi.due_at <= TIMESTAMPTZ '2026-12-31 00:00:00+00'
    AND (
      oi.status IN ('scheduled', 'due', 'in_progress')
      OR oi.due_at >= TIMESTAMPTZ '2026-06-10 00:00:00+00'
    )
),
located AS (
  SELECT raw.*, COALESCE(raw.direct_park_uuid, shed_loc.parent_location_id) AS park_uuid
  FROM raw
  LEFT JOIN locations shed_loc
    ON shed_loc.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND shed_loc.location_id = raw.shed_uuid
   AND shed_loc.location_type = 'shed'
  WHERE raw.shed_uuid IS NOT NULL
),
grouped AS (
  SELECT
    located.park_uuid,
    located.shed_uuid,
    located.batch_id,
    located.rule_id,
    MIN(located.due_at) AS due_at,
    COUNT(*)::bigint AS obligation_count,
    COUNT(*) FILTER (WHERE located.obligation_status = 'due')::bigint AS due_count,
    COUNT(*) FILTER (WHERE located.obligation_status = 'in_progress')::bigint AS in_progress_count,
    COUNT(*) FILTER (WHERE located.obligation_status = 'completed')::bigint AS completed_count,
    COUNT(*) FILTER (WHERE located.obligation_status = 'missed')::bigint AS missed_count,
    COUNT(*) FILTER (WHERE located.obligation_status = 'waived')::bigint AS deferred_count,
    COUNT(*) FILTER (WHERE located.completion_status = 'recorded')::bigint AS completion_recorded,
    COUNT(*) FILTER (WHERE located.completion_status = 'rejected')::bigint AS completion_rejected,
    (ARRAY_AGG(located.batch_status ORDER BY located.due_at DESC NULLS LAST) FILTER (WHERE located.batch_status IS NOT NULL))[1] AS batch_status,
    (ARRAY_AGG(located.task_state ORDER BY located.due_at DESC NULLS LAST) FILTER (WHERE located.task_state IS NOT NULL))[1] AS task_state,
    (ARRAY_AGG(operator.display_name ORDER BY located.due_at DESC NULLS LAST, operator.updated_at DESC NULLS LAST) FILTER (WHERE operator.display_name IS NOT NULL))[1] AS operator_name,
    MAX(sp.capacity) AS shed_capacity,
    COUNT(*) FILTER (
      WHERE located.goat_lifecycle_status IN ('sick', 'under_treatment', 'quarantine', 'icu')
         OR COALESCE(located.goat_health_status, '') IN ('sick', 'under_treatment', 'quarantine', 'icu')
    )::bigint AS health_deferred_count
  FROM located
  JOIN locations shed
    ON shed.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND shed.location_id = located.shed_uuid
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
  JOIN locations park
    ON park.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND park.location_id = located.park_uuid
   AND park.location_type = 'park'
   AND park.status = 'active'
  LEFT JOIN shed_profiles sp
    ON sp.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND sp.location_id = located.shed_uuid
  LEFT JOIN workforce_members operator
    ON operator.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND operator.workforce_member_id = COALESCE(located.conducted_by, located.assigned_to)
   AND operator.status = 'active'
  WHERE located.park_uuid IS NOT NULL
  GROUP BY located.park_uuid, located.shed_uuid, located.batch_id, located.rule_id
),
enriched AS (
  SELECT
    grouped.*,
    COALESCE(loa.usable_for_vaccination, true) AS usable_for_vaccination,
    COALESCE(loa.is_quarantine, false) AS is_quarantine,
    COALESCE(loa.is_icu, false) AS is_icu
  FROM grouped
  LEFT JOIN location_operational_attributes loa
    ON loa.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND loa.location_id = grouped.shed_uuid
),
stateful AS (
  SELECT
    enriched.*,
    CASE
      WHEN enriched.obligation_count > 0
       AND enriched.completed_count = enriched.obligation_count
       AND enriched.completion_rejected = 0
       AND enriched.completion_recorded = 0 THEN 'completed'
      WHEN enriched.completion_rejected > 0 THEN 'rejected'
      WHEN NOT enriched.usable_for_vaccination THEN 'blocked'
      WHEN enriched.deferred_count > 0
        OR enriched.health_deferred_count > 0
        OR enriched.is_quarantine
        OR enriched.is_icu THEN 'deferred'
      WHEN enriched.missed_count > 0 THEN 'blocked'
      WHEN enriched.operator_name IS NULL
       AND enriched.completed_count < enriched.obligation_count THEN 'owner_missing'
      WHEN enriched.task_state IN ('rework_requested', 'rejected') THEN 'rejected'
      WHEN enriched.completion_recorded > 0
        OR enriched.task_state IN ('submitted', 'needs_review') THEN 'verification_pending'
      WHEN enriched.in_progress_count > 0
        OR enriched.batch_status = 'in_progress'
        OR enriched.task_state = 'in_progress' THEN 'in_progress'
      WHEN enriched.due_at < TIMESTAMPTZ '2026-06-24 12:00:00+00' THEN 'overdue'
      WHEN enriched.due_count > 0 THEN 'due'
      ELSE 'scheduled'
    END AS work_state
  FROM enriched
)
SELECT grouped.shed_uuid, grouped.work_state, grouped.shed_capacity, park_head.display_name AS park_head_name
FROM stateful grouped
JOIN locations park
  ON park.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
 AND park.location_id = grouped.park_uuid
JOIN locations shed
  ON shed.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
 AND shed.location_id = grouped.shed_uuid
LEFT JOIN LATERAL (
  SELECT wm.display_name
  FROM workforce_members wm
  WHERE wm.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND wm.status = 'active'
    AND wm.primary_role_hint = 'park_head'
    AND wm.primary_location_id IN (grouped.shed_uuid, grouped.park_uuid)
  ORDER BY CASE WHEN wm.primary_location_id = grouped.shed_uuid THEN 0 ELSE 1 END, wm.updated_at DESC, wm.workforce_member_id DESC
  LIMIT 1
) park_head ON true
WHERE grouped.work_state = 'blocked'
ORDER BY
  CASE grouped.work_state
    WHEN 'rejected' THEN 0
    WHEN 'blocked' THEN 1
    ELSE 11
  END,
  grouped.due_at ASC NULLS LAST
LIMIT 200;"
}

# Broad process-integrity base-join guard for the shell hot-path sweep. The exact production
# query is covered by TestProcessIntegrityProductionQueryPlanUsesIndexes.
validate_vaccination_process_integrity_base_join_plan() {
  explain_must_use_index "VaccinationProcessIntegrityBaseJoins" 'Seq Scan on obligation_instances|Seq Scan on protocol_versions|Seq Scan on protocol_definitions|Seq Scan on protocol_rules|Seq Scan on goats|Seq Scan on obligation_batches|Seq Scan on sop_tasks|Seq Scan on sop_submissions|Seq Scan on vaccination_completions|Seq Scan on locations|Seq Scan on workforce_members|Seq Scan on shed_profiles|Seq Scan on animal_stage_lookup|Seq Scan on location_operational_attributes' "EXPLAIN (COSTS OFF)
WITH raw AS (
  SELECT
    oi.obligation_id,
    oi.rule_id,
    oi.batch_id,
    oi.due_at,
    oi.status AS obligation_status,
    pv.protocol_id,
    pr.dose_code,
    ob.status AS batch_status,
    ob.conducted_by,
    st.state AS task_state,
    st.assigned_to,
    ss.submission_id,
    ss.proof_refs,
    g.goat_id,
    g.lifecycle_status AS goat_lifecycle_status,
    g.health_status AS goat_health_status,
    g.management_stage AS goat_stage,
    vc.status AS completion_status,
    vc.verified_by,
    vc.verified_at,
    CASE
      WHEN g.shed_id IS NOT NULL THEN g.shed_id
      WHEN oi.target_type = 'shed' THEN oi.target_id
      WHEN oi.scope_type = 'shed' THEN oi.scope_id
      ELSE NULL
    END AS shed_uuid,
    CASE
      WHEN g.park_id IS NOT NULL THEN g.park_id
      WHEN oi.target_type = 'park' THEN oi.target_id
      WHEN oi.scope_type = 'park' THEN oi.scope_id
      ELSE NULL
    END AS direct_park_uuid
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
  LEFT JOIN goats g
    ON oi.target_type = 'goat'
   AND g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.identity_state <> 'merged'
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN sop_tasks st
    ON st.tenant_id = oi.tenant_id
   AND st.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
  LEFT JOIN LATERAL (
    SELECT submission_id, proof_refs, submitted_at
    FROM sop_submissions sub
    WHERE sub.tenant_id = oi.tenant_id
      AND sub.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
    ORDER BY sub.submitted_at DESC, sub.submission_id DESC
    LIMIT 1
  ) ss ON true
  LEFT JOIN vaccination_completions vc
    ON vc.tenant_id = oi.tenant_id
   AND vc.obligation_id = oi.obligation_id
   AND vc.status <> 'reversed'
  WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'completed', 'missed', 'waived')
    AND oi.due_at <= TIMESTAMPTZ '2026-12-31 00:00:00+00'
    AND (
      oi.status IN ('scheduled', 'due', 'in_progress', 'missed', 'waived')
      OR oi.due_at >= TIMESTAMPTZ '2026-06-10 00:00:00+00'
    )
),
located AS (
  SELECT raw.*, COALESCE(raw.direct_park_uuid, shed_loc.parent_location_id) AS park_uuid
  FROM raw
  LEFT JOIN locations shed_loc
    ON shed_loc.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND shed_loc.location_id = raw.shed_uuid
   AND shed_loc.location_type = 'shed'
  WHERE raw.shed_uuid IS NOT NULL
),
grouped AS (
  SELECT
    located.park_uuid,
    located.shed_uuid,
    located.batch_id,
    located.rule_id,
    MIN(located.due_at) AS due_at,
    COUNT(*)::int AS expected_count,
    COUNT(*) FILTER (WHERE located.obligation_status = 'due')::int AS due_count,
    COUNT(*) FILTER (WHERE located.obligation_status = 'in_progress')::int AS in_progress_count,
    COUNT(*) FILTER (WHERE located.obligation_status = 'completed')::int AS completed_count,
    COUNT(*) FILTER (WHERE located.obligation_status = 'missed')::int AS missed_count,
    COUNT(*) FILTER (WHERE located.obligation_status = 'waived')::int AS deferred_count,
    COUNT(*) FILTER (WHERE located.completion_status = 'recorded')::int AS completion_recorded,
    COUNT(*) FILTER (WHERE located.completion_status = 'accepted')::int AS completion_accepted,
    COUNT(*) FILTER (WHERE located.completion_status = 'rejected')::int AS completion_rejected,
    MAX(jsonb_array_length(COALESCE(located.proof_refs, '[]'::jsonb)))::int AS proof_count,
    (ARRAY_AGG(located.batch_status ORDER BY located.due_at DESC NULLS LAST) FILTER (WHERE located.batch_status IS NOT NULL))[1] AS batch_status,
    (ARRAY_AGG(located.task_state ORDER BY located.due_at DESC NULLS LAST) FILTER (WHERE located.task_state IS NOT NULL))[1] AS task_state,
    (ARRAY_AGG(located.conducted_by::text ORDER BY located.due_at DESC NULLS LAST) FILTER (WHERE located.conducted_by IS NOT NULL))[1] AS conducted_by,
    (ARRAY_AGG(located.assigned_to::text ORDER BY located.due_at DESC NULLS LAST) FILTER (WHERE located.assigned_to IS NOT NULL))[1] AS assigned_to,
    (ARRAY_AGG(located.verified_by::text ORDER BY located.verified_at DESC NULLS LAST) FILTER (WHERE located.verified_by IS NOT NULL))[1] AS verified_by,
    COALESCE(MAX(stage.stage_code), MAX(stage.name), MAX(located.goat_stage), 'Unknown') AS animal_stage,
    COUNT(*) FILTER (
      WHERE located.goat_lifecycle_status IN ('sick', 'under_treatment', 'quarantine', 'icu')
         OR COALESCE(located.goat_health_status, '') IN ('sick', 'under_treatment', 'quarantine', 'icu')
    )::int AS health_deferred_count
  FROM located
  JOIN locations shed
    ON shed.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND shed.location_id = located.shed_uuid
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
  JOIN locations park
    ON park.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND park.location_id = located.park_uuid
   AND park.location_type = 'park'
   AND park.status = 'active'
  LEFT JOIN shed_profiles sp
    ON sp.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND sp.location_id = located.shed_uuid
  LEFT JOIN animal_stage_lookup stage
    ON stage.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND stage.animal_stage_id = sp.animal_stage_id
  WHERE located.park_uuid IS NOT NULL
  GROUP BY located.park_uuid, located.shed_uuid, located.batch_id, located.rule_id
),
enriched AS (
  SELECT
    grouped.*,
    COALESCE(loa.usable_for_vaccination, true) AS usable_for_vaccination,
    COALESCE(loa.is_quarantine, false) AS is_quarantine,
    COALESCE(loa.is_icu, false) AS is_icu
  FROM grouped
  LEFT JOIN location_operational_attributes loa
    ON loa.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND loa.location_id = grouped.shed_uuid
),
stateful AS (
  SELECT
    enriched.*,
    CASE
      WHEN enriched.expected_count > 0
       AND enriched.completed_count = enriched.expected_count
       AND enriched.completion_rejected = 0
       AND enriched.completion_recorded = 0 THEN 'completed'
      WHEN enriched.completion_rejected > 0 THEN 'rejected'
      WHEN NOT enriched.usable_for_vaccination THEN 'blocked'
      WHEN enriched.deferred_count > 0
        OR enriched.health_deferred_count > 0
        OR enriched.is_quarantine
        OR enriched.is_icu THEN 'deferred'
      WHEN enriched.missed_count > 0 THEN 'blocked'
      WHEN enriched.conducted_by IS NULL
       AND enriched.assigned_to IS NULL
       AND enriched.completed_count < enriched.expected_count THEN 'owner_missing'
      WHEN enriched.task_state IN ('rework_requested', 'rejected') THEN 'rejected'
      WHEN enriched.completion_recorded > 0
        OR enriched.task_state IN ('submitted', 'needs_review') THEN 'verification_pending'
      WHEN enriched.in_progress_count > 0
        OR enriched.batch_status = 'in_progress'
        OR enriched.task_state = 'in_progress' THEN 'in_progress'
      WHEN enriched.due_at < TIMESTAMPTZ '2026-06-24 12:00:00+00' THEN 'overdue'
      WHEN enriched.due_count > 0 THEN 'due'
      ELSE 'scheduled'
    END AS work_state
  FROM enriched
),
with_locations AS (
  SELECT stateful.*, park.name AS park_name, shed.name AS shed_name
  FROM stateful
  JOIN locations park
    ON park.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND park.location_id = stateful.park_uuid
  JOIN locations shed
    ON shed.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND shed.location_id = stateful.shed_uuid
),
with_owners AS (
  SELECT
    with_locations.*,
    operator.display_name AS operator_name,
    park_head.display_name AS park_head_name,
    verifier.display_name AS verifier_name
  FROM with_locations
  LEFT JOIN workforce_members operator
    ON operator.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
   AND operator.workforce_member_id = COALESCE(with_locations.conducted_by::uuid, with_locations.assigned_to::uuid)
   AND operator.status = 'active'
  LEFT JOIN LATERAL (
    SELECT wm.display_name
    FROM workforce_members wm
    WHERE wm.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
      AND wm.status = 'active'
      AND wm.primary_role_hint = 'park_head'
      AND wm.primary_location_id IN (with_locations.shed_uuid, with_locations.park_uuid)
    ORDER BY CASE WHEN wm.primary_location_id = with_locations.shed_uuid THEN 0 ELSE 1 END, wm.updated_at DESC, wm.workforce_member_id DESC
    LIMIT 1
  ) park_head ON true
  LEFT JOIN LATERAL (
    SELECT wm.display_name
    FROM workforce_members wm
    WHERE wm.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
      AND wm.status = 'active'
      AND wm.primary_role_hint = 'verifier'
      AND (wm.primary_location_id IS NULL OR wm.primary_location_id IN (with_locations.shed_uuid, with_locations.park_uuid))
    ORDER BY CASE WHEN wm.primary_location_id = with_locations.shed_uuid THEN 0 WHEN wm.primary_location_id = with_locations.park_uuid THEN 1 ELSE 2 END,
             wm.updated_at DESC, wm.workforce_member_id DESC
    LIMIT 1
  ) verifier ON true
)
SELECT
  shed_uuid,
  work_state,
  animal_stage,
  operator_name,
  park_head_name,
  verifier_name
FROM with_owners
WHERE work_state IN ('rejected', 'blocked', 'owner_missing', 'overdue', 'proof_pending', 'verification_pending')
ORDER BY
  CASE work_state
    WHEN 'rejected' THEN 0
    WHEN 'blocked' THEN 1
    WHEN 'owner_missing' THEN 2
    WHEN 'overdue' THEN 3
    WHEN 'proof_pending' THEN 4
    WHEN 'verification_pending' THEN 5
    ELSE 11
  END,
  due_at ASC NULLS LAST,
  shed_uuid ASC
LIMIT 200;"
}

validate_feed_review_queue_plan() {
  explain_must_use_index "FeedReviewQueue" 'Seq Scan on feed_direction_completions' "EXPLAIN (COSTS OFF)
SELECT completion_id
FROM feed_direction_completions
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status = 'recorded'
ORDER BY fed_at ASC, completion_id ASC
LIMIT 100;"
}

validate_feed_shed_history_plan() {
  explain_must_use_index "FeedShedHistory" 'Seq Scan on feed_direction_completions' "EXPLAIN (COSTS OFF)
SELECT completion_id, fed_at, status
FROM feed_direction_completions
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND shed_id = '55000000-0000-4000-8000-0000000000f1'::uuid
ORDER BY fed_at DESC, completion_id DESC
LIMIT 100;"
}

validate_procurement_source_entry_plans() {
  explain_must_use_index "ProcurementSourceEntryBoard" 'Seq Scan on procurement_loads' "EXPLAIN (COSTS OFF)
SELECT load_id, status, updated_at
FROM procurement_loads
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status = 'source_warmup'
ORDER BY updated_at DESC, load_id DESC
LIMIT 100;"

  explain_must_use_index "ProcurementSourceEntryPagination" 'Seq Scan on procurement_loads' "EXPLAIN (COSTS OFF)
SELECT load_id, status, updated_at
FROM procurement_loads
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND (
    TIMESTAMPTZ '2026-06-24 00:00:00+00' IS NULL
    OR (updated_at, load_id) < (TIMESTAMPTZ '2026-06-24 00:00:00+00', 'ac000000-0000-4000-8000-000000000001'::uuid)
  )
ORDER BY updated_at DESC, load_id DESC
LIMIT 101;"

  explain_must_use_index "ProcurementLoadDetailGoats" 'Seq Scan on procurement_load_goats' "EXPLAIN (COSTS OFF)
SELECT goat_id, current_state
FROM procurement_load_goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND load_id = 'ac000000-0000-4000-8000-000000000001'::uuid
ORDER BY created_at ASC, goat_id ASC
LIMIT 500;"

  explain_must_use_index "ProcurementVaccinationExclusionLookup" 'Seq Scan on procurement_load_goats|Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM vw_procurement_vaccination_excluded_goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND goat_id = '10000000-0000-4000-8000-000000000001'::uuid
LIMIT 1;"

  explain_must_use_index "ProcurementAcceptedIntakeEligibility" 'Seq Scan on procurement_load_goats' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM procurement_load_goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND load_id = 'ac000000-0000-4000-8000-000000000001'::uuid
  AND current_state = 'arrival_accepted'
  AND loaded_at IS NOT NULL
  AND arrived_at IS NOT NULL
  AND health_state = 'passed'
  AND identity_review_state = 'clean'
  AND ownership_state IN ('mesha_owned', 'settled')
  AND EXISTS (
    SELECT 1
    FROM transit_handoffs th
    WHERE th.tenant_id = procurement_load_goats.tenant_id
      AND th.load_id = procurement_load_goats.load_id
      AND th.proof_ref_id IS NOT NULL
      AND th.status IN ('in_transit', 'arrived')
  )
ORDER BY goat_id
LIMIT 500;"

  explain_must_use_index "ProcurementActionCenterRows" 'Seq Scan on procurement_load_goats|Seq Scan on procurement_loads' "EXPLAIN (COSTS OFF)
SELECT 'load_goat:' || plg.load_goat_id::text AS row_id, pl.load_id, plg.current_state, plg.updated_at
FROM procurement_load_goats plg
JOIN procurement_loads pl
  ON pl.tenant_id = plg.tenant_id
  AND pl.load_id = plg.load_id
WHERE plg.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
ORDER BY plg.updated_at DESC, plg.load_goat_id DESC
LIMIT 101;"
}

validate_operations_audit_plans() {
  explain_must_use_index "OperationsAuditList" 'Seq Scan on audit_log' "EXPLAIN (COSTS OFF)
SELECT audit_id, recorded_at
FROM audit_log
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND recorded_at >= TIMESTAMPTZ '2026-06-24 00:00:00+00'
  AND recorded_at <= TIMESTAMPTZ '2026-06-25 00:00:00+00'
ORDER BY recorded_at DESC, audit_id DESC
LIMIT 101;"

  explain_must_use_index "OperationsAuditResourceDrilldown" 'Seq Scan on audit_log' "EXPLAIN (COSTS OFF)
SELECT audit_id, recorded_at
FROM audit_log
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND resource_type = 'goat'
  AND resource_id = '10000000-0000-4000-8000-000000000001'::uuid
ORDER BY recorded_at DESC, audit_id DESC
LIMIT 50;"
}

validate_calendar_vaccination_plans() {
  explain_must_use_index "CalendarVaccinationWidestList" 'Seq Scan on calendar_event_projections' "EXPLAIN (COSTS OFF)
SELECT event_id, event_type, owner_key, title, status, severity, due_at
FROM calendar_event_projections
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND slice_key = 'vaccination'
  AND system = false
  AND due_at >= TIMESTAMPTZ '2026-06-27 00:00:00+00'
  AND due_at < TIMESTAMPTZ '2026-08-12 00:00:00+00'
  AND (''::text = '' OR owner_key = ''::text)
  AND (''::text = '' OR status = ''::text)
  AND (''::text = '' OR park_id = nullif(''::text, '')::uuid)
  AND (''::text = '' OR shed_id = nullif(''::text, '')::uuid)
  AND (NULL::timestamptz IS NULL OR (due_at, event_id) > (NULL::timestamptz, ''::text))
ORDER BY due_at ASC, event_id ASC
LIMIT 200;"
}

docker run --rm --name "$container_name" \
  -e POSTGRES_PASSWORD=goatos \
  -e POSTGRES_DB="$db_name" \
  -d "$image" >/dev/null

postgres_ci_wait_ready "$container_name" "$db_user" "$db_name"

while IFS= read -r migration; do
  apply_goose_up "$migration"
done < <(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | sort)

validate_identity_lookup_plans
validate_outbox_claim_plan
validate_auth_grant_lookup_plan
validate_obligation_due_window_plan
validate_obligation_scope_count_plan
validate_obligation_open_by_goat_plan
validate_inventory_fefo_plan
validate_inventory_movements_ledger_plan
validate_inventory_batch_reconcile_plan
validate_vaccination_generation_scan_plan
validate_vaccination_review_queue_plan
validate_vaccination_fanout_plan
validate_sop_failed_submission_fanouts_plan
validate_parks_vaccination_base_join_plan
validate_vaccination_process_integrity_base_join_plan
validate_feed_review_queue_plan
validate_feed_shed_history_plan
validate_procurement_source_entry_plans
validate_operations_audit_plans
validate_calendar_vaccination_plans

echo "Validated current hot-path query plans"
