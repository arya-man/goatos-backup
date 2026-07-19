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

# explain_must_use_named_index asserts a SPECIFIC index is chosen (not just "some index"). Needed
# where "no Seq Scan" is too weak -- e.g. a tenant-scoped claim could ride the global claim index
# and residual-Filter the tenant, which is exactly the inefficiency we are gating against.
explain_must_use_named_index() {
  local label="$1"
  local required_index="$2"
  local sql="$3"
  local plan
  plan="$(printf '%s\n' "$sql" | run_psql)"
  if ! grep -E "$required_index" <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Expected $label to use index $required_index (tenant-leading), got the plan above" >&2
    exit 1
  fi
  if grep -E 'Seq Scan on vaccination_projection_dirty_scopes' <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Unexpected sequential scan in $label" >&2
    exit 1
  fi
  echo "Named-index plan observed: $label -> $required_index"
}

# Natural-plan proof for a hot update: unlike explain_must_use_index, this does
# not disable sequential scans. The fixture below gives PostgreSQL enough rows
# and fresh statistics to choose the real primary-key access path on its own.
explain_natural_index() {
  local label="$1"
  local forbidden="$2"
  local sql="$3"
  local plan
  plan="$(printf '%s\n' "$sql" | run_psql)"
  if grep -E "$forbidden" <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Unexpected sequential scan in $label" >&2
    exit 1
  fi
  if ! grep -E '(Index Scan|Index Only Scan|Bitmap Index Scan)' <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Expected natural indexed plan in $label" >&2
    exit 1
  fi
  echo "Natural indexed plan observed: $label"
}

# Natural-plan proof for an order-sensitive keyset read. It requires the named covering index and
# rejects bitmap access or an explicit sort: either would materialize/filter a growing candidate
# tail instead of walking the B-tree in cursor order. Sequential scans remain enabled.
explain_natural_named_index_no_sort() {
  local label="$1"
  local required_index="$2"
  local sql="$3"
  local plan
  plan="$(printf '%s\n' "$sql" | run_psql)"
  if ! grep -E "(Index Scan|Index Only Scan) using $required_index" <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Expected natural ordered plan in $label to use $required_index" >&2
    exit 1
  fi
  if grep -E '(Seq Scan on obligation_instances|Bitmap (Heap|Index) Scan|(^|[[:space:]])(Sort|Incremental Sort)([[:space:]]|$))' <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Unexpected scan/sort node in ordered keyset plan $label" >&2
    exit 1
  fi
  echo "Natural ordered named-index plan observed: $label -> $required_index"
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
  AND merged_into_goat_id IS NULL
ORDER BY display_id ASC
LIMIT 50;" '(Sort|Incremental Sort)'

  explain_must_use_index "IdentifierResolution" 'Seq Scan on goat_identifiers' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM goat_identifiers
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND identifier_type = 'animal_identifier_1'
  AND normalized_value = '1900'
  AND status = 'active'
LIMIT 10;"

  printf '%s\n' "
INSERT INTO tenants (tenant_id, name, status)
VALUES ('00000000-0000-4000-8000-000000000001', 'sqlc-plan-tenant', 'active')
ON CONFLICT (tenant_id) DO NOTHING;
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ('00000000-0000-4000-8000-000000001001', 'system', 'sqlc-plan-system', 'active')
ON CONFLICT (party_id) DO NOTHING;
INSERT INTO goats (goat_id, tenant_id, display_id, sex, lifecycle_status, custodian_party_id)
VALUES ('10000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'G-990101', 'female', 'alive', '00000000-0000-4000-8000-000000001001')
ON CONFLICT (goat_id) DO NOTHING;
INSERT INTO goat_identity_events (
  identity_event_id, tenant_id, goat_id, event_type, event_version,
  occurred_at, recorded_at, payload, idempotency_key
)
SELECT
  ('21000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
  '00000000-0000-4000-8000-000000000001'::uuid,
  '10000000-0000-4000-8000-000000000001'::uuid,
  'goat.timeline_plan', 1,
  TIMESTAMPTZ '2026-07-01 00:00:00+00' + i * INTERVAL '1 second',
  TIMESTAMPTZ '2026-07-01 00:00:00+00' + i * INTERVAL '1 second',
  '{}'::jsonb, 'sqlc-plan-goat-timeline-' || i::text
FROM generate_series(1, 10000) AS s(i)
ON CONFLICT DO NOTHING;
ANALYZE goat_identity_events;
" | run_psql

  explain_natural_named_index_no_sort "GoatTimeline" 'goat_identity_events_tenant_goat_timeline_keyset_idx' "EXPLAIN (COSTS OFF)
SELECT identity_event_id, occurred_at
FROM goat_identity_events
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND goat_id = '10000000-0000-4000-8000-000000000001'::uuid
ORDER BY occurred_at DESC, identity_event_id DESC
LIMIT 50;"

  # location_display is composed from the animal's OWN park/shed (COALESCE(shed.name, park.name,
  # 'Unknown location')), never from the vestigial goats.current_location_id. Both joins must stay
  # indexed lookups on locations_tenant_location_unique: this is the hot /goats/search page read,
  # and a Seq Scan on locations here would be per-page work against every location row.
  printf '%s\n' "
INSERT INTO locations (location_id, tenant_id, location_type, name, status)
VALUES
  ('30000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'park', 'sqlc-plan-park', 'active'),
  ('30000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001', 'shed', 'sqlc-plan-shed', 'active')
ON CONFLICT (location_id) DO NOTHING;
UPDATE goats
   SET park_id = '30000000-0000-4000-8000-000000000001'::uuid,
       shed_id = '30000000-0000-4000-8000-000000000002'::uuid
 WHERE goat_id = '10000000-0000-4000-8000-000000000001'::uuid;
ANALYZE locations;
" | run_psql

  explain_must_use_index "GoatSearchLocationDisplayJoins" 'Seq Scan on locations' "EXPLAIN (COSTS OFF)
SELECT g.goat_id, COALESCE(shed.name, park.name, 'Unknown location') AS location_display
FROM goats g
LEFT JOIN locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
LEFT JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
ORDER BY g.display_id ASC
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

# KERN-FIX-01: ReleasePublishingByIDs must keep the UUID column bare so the
# outbox primary-key index remains usable. Seed a realistic hot-table shape and
# assert the natural planner choice; a forced-index EXPLAIN alone is too weak.
validate_outbox_release_plan() {
  printf '%s\n' "
INSERT INTO tenants (tenant_id, name, status)
VALUES ('00000000-0000-4000-8000-000000000001', 'sqlc-plan-tenant', 'active')
ON CONFLICT (tenant_id) DO NOTHING;
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ('00000000-0000-4000-8000-000000001001', 'system', 'sqlc-plan-system', 'active')
ON CONFLICT (party_id) DO NOTHING;
INSERT INTO goats (goat_id, tenant_id, display_id, sex, lifecycle_status, custodian_party_id)
VALUES ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'G-990001', 'female', 'alive', '00000000-0000-4000-8000-000000001001')
ON CONFLICT (goat_id) DO NOTHING;
INSERT INTO goat_identity_events (
  identity_event_id, tenant_id, goat_id, event_type, event_version,
  occurred_at, recorded_at, payload, idempotency_key
)
SELECT
  ('20000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
  '00000000-0000-4000-8000-000000000001'::uuid,
  '00000000-0000-4000-8000-000000000001'::uuid,
  'goat.created', 1,
  TIMESTAMPTZ '2026-07-01 00:00:00+00' + i * INTERVAL '1 second',
  TIMESTAMPTZ '2026-07-01 00:00:00+00' + i * INTERVAL '1 second',
  '{}'::jsonb, 'sqlc-plan-event-' || i::text
FROM generate_series(1, 10000) AS s(i)
ON CONFLICT DO NOTHING;
INSERT INTO outbox_messages (
  outbox_id, tenant_id, event_id, event_type, schema_version,
  aggregate_type, aggregate_id, topic, payload, headers,
  idempotency_key, status
)
SELECT
  ('30000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
  '00000000-0000-4000-8000-000000000001'::uuid,
  ('20000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
  'goat.created', 'v1', 'goat_identity_event',
  '00000000-0000-4000-8000-000000000001'::uuid,
  'sqlc-plan-topic', '{}'::jsonb, '{}'::jsonb,
  'sqlc-plan-outbox-' || i::text, 'publishing'
FROM generate_series(1, 10000) AS s(i)
ON CONFLICT DO NOTHING;
ANALYZE outbox_messages;
" | run_psql

  explain_natural_index "OutboxReleasePublishingByIDs" 'Seq Scan on outbox_messages' "EXPLAIN (COSTS OFF)
UPDATE outbox_messages
SET status = 'pending', next_attempt_at = NULL, updated_at = TIMESTAMPTZ '2026-07-15 12:00:00+00'
WHERE outbox_id = ANY(ARRAY[
  '30000000-0000-4000-8000-000000000001'::uuid,
  '30000000-0000-4000-8000-000000000002'::uuid
]::uuid[])
  AND status = 'publishing';"
}

# KERN-REV-06B: the notification dispatcher claim (ClaimDue / ClaimDueSQL) selects
# DUE rows with status IN ('queued','failed') AND COALESCE(next_attempt_at, requested_at)
# <= now, ORDER BY COALESCE(...). This is the candidate-selection scan of the real
# writable CTE; it must ride notification_requests_due_order_idx and never Seq Scan
# the queued/failed partition (which under future-retry skew would degrade the hot
# path). Predicate is kept IDENTICAL to ClaimDueSQL's candidates block.
validate_notification_claim_plan() {
  explain_must_use_index "NotificationClaimDue" 'Seq Scan on notification_requests' "EXPLAIN (COSTS OFF)
SELECT notification_request_id
FROM notification_requests
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status IN ('queued', 'failed')
  AND COALESCE(next_attempt_at, requested_at) <= '2026-06-09T12:00:00Z'::timestamptz
  AND delivery_attempts < 5
ORDER BY COALESCE(next_attempt_at, requested_at), notification_request_id
LIMIT 100
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

validate_obligation_planned_batch_finalization_plan() {
  explain_must_use_index "PlannedBatchFinalizationKeyset" 'Seq Scan on obligation_batches' "EXPLAIN (COSTS OFF)
SELECT ob.batch_id::text,
       COALESCE(MIN(oi.rule_id::text), '')::text AS rule_id,
       ob.scope_type,
       ob.scope_id::text,
       ob.created_at,
       COUNT(oi.obligation_id)::bigint AS attached_obligations
FROM obligation_batches ob
JOIN obligation_instances oi
  ON oi.tenant_id = ob.tenant_id
 AND oi.batch_id = ob.batch_id
 AND oi.status IN ('scheduled', 'due', 'in_progress')
WHERE ob.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND ob.protocol_version_id = '10000000-0000-4000-8000-000000000001'::uuid
  AND ob.status = 'planned'
  AND (
    '2026-06-29T12:00:00+00'::timestamptz IS NULL
    OR ob.created_at > '2026-06-29T12:00:00+00'::timestamptz
    OR (ob.created_at = '2026-06-29T12:00:00+00'::timestamptz AND ob.batch_id > '10000000-0000-4000-8000-000000000001'::uuid)
  )
GROUP BY ob.tenant_id, ob.batch_id, ob.scope_type, ob.scope_id, ob.planned_date, ob.estimated_targets, ob.sop_task_id, ob.context, ob.created_at
HAVING COUNT(oi.obligation_id) > 0
ORDER BY ob.created_at ASC, ob.batch_id ASC
LIMIT 100;"
}

validate_combo_align_keyset_plan() {
  # R2-06b: ListPlannedComboBatchesKeyset (AlignComboDrives, runs every sweep) keyset-pages planned
  # combo batches ORDER BY (scope_type, scope_id, session, batch_id) filtered to status='planned',
  # sop_task_id IS NULL, session LIKE 'combo:%'. It must ride the partial composite index
  # obligation_batches_combo_align_keyset_idx (migration 000209), never a tenant-wide sequential scan
  # + sort. The extra_forbidden Sort assertion proves the index also supplies the ordering.
  explain_must_use_index "ComboAlignKeyset" 'Seq Scan on obligation_batches' "EXPLAIN (COSTS OFF)
SELECT b.batch_id
FROM obligation_batches b
WHERE b.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND b.status = 'planned'
  AND b.session LIKE 'combo:%'
  AND b.sop_task_id IS NULL
  AND NOT (b.context ? 'stock_reservation')
  AND (b.planned_date IS NULL OR b.planned_date <= '2026-08-15'::date)
ORDER BY b.scope_type, b.scope_id, b.session, b.batch_id
LIMIT 1000;" '(Sort|Incremental Sort)'
}

validate_kernel_sweeper_hot_path_plans() {
  explain_must_use_index "ObligationListUnbatchedDueForVersion" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT oi.obligation_id::text AS obligation_id,
       oi.rule_id::text AS rule_id,
       oi.scope_type,
       COALESCE(oi.scope_id::text, '')::text AS scope_id,
       CASE
         WHEN oi.target_type = 'goat' THEN COALESCE(g.species, 'goat')::text
         ELSE ''
       END AS target_species,
       oi.due_at,
       oi.window_start,
       oi.window_end
FROM obligation_instances oi
LEFT JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
 AND oi.target_type = 'goat'
WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND oi.protocol_version_id = '10000000-0000-4000-8000-000000000001'::uuid
  AND oi.status IN ('scheduled', 'due', 'missed')
  AND oi.batch_id IS NULL
  AND oi.due_at <= TIMESTAMPTZ '2026-06-29 12:00:00+00'
ORDER BY oi.scope_type, oi.scope_id, oi.rule_id, target_species, oi.due_at, oi.obligation_id
LIMIT 1000;"

  # CalendarDueReminderSweep / CalendarEscalationSweep (formerly direct calendar_event_projections
  # reads) are retired along with that table (5k-50k envelope, migration 000189,
  # docs/decisions/operational-kernel-5k-50k-scale-envelope.md). Both sweeps now read the same
  # canonical source_events reconstruction as the list (calendarCanonicalEventsCTE,
  # backend/internal/calendar/adapters/postgres/canonical_read.go), whose driving scan
  # (tenant+due-window keyset over obligation_instances) is exactly what
  # validate_calendar_canonical_read_plan's CalendarCanonicalReadKeysetDriver proves is index-backed;
  # the full assembled query shape is proven by the Go-level canonical_read_plan_test.go.
  #
  # NOTE: the write-free preflight's OWN full-scan sibling of the query above --
  # ListUnbatchedDueForVersionKeyset / ListUnbatchedDueForVersionHWM's unbatchedDueKeysetSelect CTE
  # (internal/obligation/adapters/postgres/repository.go) -- is a SEPARATE raw-SQL query this
  # ObligationListUnbatchedDueForVersion check does NOT cover (RV-06). It is validated below by
  # validate_obligation_unbatched_due_keyset_plan with realistic seeded row counts, since that query
  # keyset-pages across MANY pages while the caller holds the per-tenant sweep advisory lock -- a
  # scale hazard the empty-table COSTS-OFF style check above cannot surface.

  explain_must_use_index "ObligationMarkMissedBefore" 'Seq Scan on obligation_instances|Seq Scan on obligation_batches' "EXPLAIN (COSTS OFF)
WITH candidate AS (
  SELECT oi.obligation_id
  FROM obligation_instances oi
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND oi.status IN ('scheduled', 'due', 'in_progress')
    AND COALESCE(oi.window_end, oi.due_at) < TIMESTAMPTZ '2026-06-29 12:00:00+00'
    AND NOT (oi.status = 'in_progress' AND COALESCE(ob.status, '') = 'in_progress')
    AND NOT EXISTS (
      SELECT 1
      FROM protocol_versions pv
      JOIN protocol_definitions pd
        ON pd.tenant_id = pv.tenant_id
       AND pd.protocol_id = pv.protocol_id
      JOIN vw_procurement_vaccination_excluded_goats ex
        ON ex.tenant_id = oi.tenant_id
       AND ex.goat_id = oi.target_id
      WHERE oi.target_type = 'goat'
        AND pv.tenant_id = oi.tenant_id
        AND pv.protocol_version_id = oi.protocol_version_id
        AND pd.category = 'vaccination'
    )
  ORDER BY COALESCE(oi.window_end, oi.due_at) ASC, oi.obligation_id ASC
  LIMIT 100
  FOR UPDATE OF oi SKIP LOCKED
)
SELECT obligation_id
FROM candidate;"
}

# RV-06: exercise the exact production query (joins, health filters, HWM, snapshot predicate,
# projection, raw UUID cursor, and ORDER BY) at the accepted 50k ceiling. Sequential scans are not
# disabled. Both the first and late page must naturally walk the named partial index in order with
# no Sort/Bitmap fallback.
validate_obligation_unbatched_due_keyset_plan() {
  printf '%s\n' "
INSERT INTO tenants (tenant_id, name, status)
VALUES ('00000000-0000-4000-8000-000000000001', 'sqlc-plan-tenant', 'active')
ON CONFLICT (tenant_id) DO NOTHING;
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES ('40000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'vaccination.rv06.plan', 'RV-06 plan gate', 'vaccination', 'active')
ON CONFLICT (protocol_id) DO NOTHING;
INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy)
VALUES ('10000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', '40000000-0000-4000-8000-000000000001', 'tenant', 1, 'draft', DATE '2026-01-01', '{}'::jsonb, '{}'::jsonb)
ON CONFLICT (protocol_version_id) DO NOTHING;
INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, catch_up, eligibility_json, proof_policy)
VALUES ('50000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', '10000000-0000-4000-8000-000000000001', 'primary', 1, 'birth_age', 'pc_approval', '{}'::jsonb, '{}'::jsonb)
ON CONFLICT (rule_id) DO NOTHING;
-- target_type='tenant' (not 'goat') deliberately bypasses the goat-existence/health JOIN filters
-- entirely (oi.target_type <> 'goat' short-circuits true), so this fixture needs no goats table
-- rows to exercise the driving scan + ORDER BY at realistic scale.
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence
)
SELECT
  ('60000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
  '00000000-0000-4000-8000-000000000001'::uuid,
  '10000000-0000-4000-8000-000000000001'::uuid,
  '50000000-0000-4000-8000-000000000001'::uuid,
  'tenant',
  '00000000-0000-4000-8000-000000000001'::uuid,
  'tenant',
  '00000000-0000-4000-8000-000000000001'::uuid,
  -- Every row must get a DISTINCT due_at: obligation_instances_dup_guard is UNIQUE NULLS NOT
  -- DISTINCT on (tenant_id, protocol_version_id, rule_id, target_type, target_id, due_at), and every
  -- row here shares the same tenant/version/rule/target -- a repeating due_at (e.g. i modulo N) would
  -- silently drop all but the first row per bucket via ON CONFLICT DO NOTHING.
  TIMESTAMPTZ '2026-01-01 00:00:00+00' + i * INTERVAL '1 minute',
  'scheduled',
  'rv06-plan-gate-' || i::text,
  1
FROM generate_series(1, 50000) AS s(i)
ON CONFLICT DO NOTHING;
ANALYZE obligation_instances;
" | run_psql

  local query_prefix="WITH candidates AS (
  SELECT oi.obligation_id AS obligation_id_key,
         oi.rule_id AS rule_id_key,
         oi.scope_type,
         oi.scope_id AS scope_id_key,
         oi.target_id AS target_id_key,
         CASE WHEN oi.target_type = 'goat' THEN COALESCE(g.species, 'goat')::text ELSE '' END AS target_species,
         CASE WHEN oi.target_type = 'goat' THEN COALESCE(asl.stage_code, g.management_stage, '')::text ELSE '' END AS target_animal_stage,
         CASE WHEN oi.target_type = 'goat' THEN COALESCE(g.reproductive_status, '')::text ELSE '' END AS target_reproductive_status,
         oi.due_at,
         oi.window_start,
         oi.window_end,
         COALESCE(oi.batching_hold_count, 0)::int AS batching_hold_count,
         oi.first_batching_hold_until
  FROM obligation_instances oi
  LEFT JOIN goats g
    ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id AND oi.target_type = 'goat'
  LEFT JOIN location_operational_attributes loa
    ON loa.tenant_id = g.tenant_id AND loa.location_id = g.current_location_id
  LEFT JOIN shed_profiles sp
    ON sp.tenant_id = g.tenant_id AND sp.location_id = COALESCE(g.shed_id, CASE WHEN oi.scope_type = 'shed' THEN oi.scope_id END)
  LEFT JOIN animal_stage_lookup asl
    ON asl.tenant_id = sp.tenant_id AND asl.animal_stage_id = sp.animal_stage_id AND asl.status = 'active'
  WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND oi.protocol_version_id = '10000000-0000-4000-8000-000000000001'::uuid
    AND oi.status IN ('scheduled', 'due', 'missed')
    AND oi.batch_id IS NULL
    AND oi.due_at <= TIMESTAMPTZ '2027-12-31 00:00:00+00'
    AND (TIMESTAMPTZ '2027-12-31 00:00:00+00' IS NULL OR oi.created_at <= TIMESTAMPTZ '2027-12-31 00:00:00+00')
    AND (NULL::uuid[] IS NULL OR oi.obligation_id = ANY(NULL::uuid[]))
    AND (
      oi.target_type <> 'goat'
      OR (
        g.goat_id IS NOT NULL
        AND g.lifecycle_status = 'alive'
        AND COALESCE(g.health_status, '') NOT IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
        AND COALESCE(loa.usable_for_vaccination, true)
        AND NOT COALESCE(loa.is_quarantine, false)
        AND NOT COALESCE(loa.is_icu, false)
      )
    )
)
SELECT obligation_id_key::text AS obligation_id,
       rule_id_key::text AS rule_id,
       scope_type,
       scope_id_key::text AS scope_id,
       target_id_key::text AS target_id,
       target_species, target_animal_stage, target_reproductive_status, due_at, window_start,
       window_end, batching_hold_count, first_batching_hold_until
FROM candidates"

  explain_natural_named_index_no_sort "ObligationUnbatchedDueKeysetFirstPage" 'obligation_instances_unbatched_due_version_idx' "EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)
${query_prefix}
ORDER BY scope_type, scope_id_key, rule_id_key, due_at, obligation_id_key
LIMIT 1000;"

  # Cursor after 49k rows: the final page must seek into the B-tree rather than re-read/filter/sort
  # the preceding 49k rows.
  explain_natural_named_index_no_sort "ObligationUnbatchedDueKeysetLatePage" 'obligation_instances_unbatched_due_version_idx' "EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)
${query_prefix}
WHERE (scope_type, scope_id_key, rule_id_key, due_at, obligation_id_key) > (
  'tenant',
  '00000000-0000-4000-8000-000000000001'::uuid,
  '50000000-0000-4000-8000-000000000001'::uuid,
  TIMESTAMPTZ '2026-02-04 00:40:00+00',
  '60000000-0000-4000-8000-000000049000'::uuid
)
ORDER BY scope_type, scope_id_key, rule_id_key, due_at, obligation_id_key
LIMIT 1000;"
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
         + CASE WHEN context #>> '{missed_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{missed_repair,release_qty}', '')::numeric, 0)
              ELSE 0 END
       )::numeric AS release_qty
FROM obligation_batches
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND (
    context #>> '{defer_repair,state}' = 'stock_reconcile_required'
    OR context #>> '{shift_repair,state}' = 'stock_reconcile_required'
    OR context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
    OR context #>> '{missed_repair,state}' = 'stock_reconcile_required'
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

validate_vaccination_gaps_plan() {
  explain_must_use_index "VaccinationGapsKeyset" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT
  g.goat_id::text,
  g.display_id,
  g.park_id::text,
  park.name,
  g.shed_id::text,
  shed.name,
  CASE WHEN g.dob IS NULL THEN 'no_date_of_birth' ELSE 'no_breed_on_record' END
FROM goats g
JOIN locations park
  ON park.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
 AND park.location_id = g.park_id
 AND park.location_type = 'park'
LEFT JOIN locations shed
  ON shed.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
 AND shed.location_id = g.shed_id
 AND shed.location_type = 'shed'
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND g.lifecycle_status = 'alive'
  AND g.merged_into_goat_id IS NULL
  AND g.park_id IS NOT NULL
  AND (''::text = '' OR g.park_id = nullif(''::text, '')::uuid)
  AND (g.dob IS NULL OR (g.breed IS NULL AND g.breed_id IS NULL))
  AND g.goat_id > '00000000-0000-0000-0000-000000000000'::uuid
ORDER BY g.goat_id ASC
LIMIT 200;"
}

validate_vaccination_impact_count_plans() {
  local impact_predicates="
FROM goats g
CROSS JOIN (
  SELECT 7::int AS warmup_no_vaccination_days,
         TIMESTAMPTZ '2026-06-29 12:00:00+00' AS as_of
) ip
LEFT JOIN LATERAL (
  SELECT COALESCE(plg.warmup_started_at, plg.intake_accepted_at, g.entry_date::timestamptz) AS warming_entry_at
  FROM procurement_load_goats plg
  WHERE plg.tenant_id = g.tenant_id
    AND plg.goat_id = g.goat_id
  ORDER BY COALESCE(plg.warmup_started_at, plg.intake_accepted_at, plg.created_at) DESC NULLS LAST
  LIMIT 1
) proc ON true
LEFT JOIN location_operational_attributes loa
  ON loa.tenant_id = g.tenant_id
 AND loa.location_id = COALESCE(g.current_location_id, g.shed_id)
LEFT JOIN shed_profiles sp
  ON sp.tenant_id = g.tenant_id
 AND sp.location_id = g.shed_id
LEFT JOIN animal_stage_lookup asl
  ON asl.tenant_id = sp.tenant_id
 AND asl.animal_stage_id = sp.animal_stage_id
 AND asl.status = 'active'
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND g.lifecycle_status = 'alive'
  AND COALESCE(g.health_status, '') NOT IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
  AND COALESCE(loa.usable_for_vaccination, true)
  AND NOT COALESCE(loa.is_quarantine, false)
  AND NOT COALESCE(loa.is_icu, false)
  AND (
    ip.warmup_no_vaccination_days <= 0
    OR COALESCE(
      proc.warming_entry_at + (ip.warmup_no_vaccination_days * INTERVAL '1 day') <= ip.as_of,
      g.entry_date IS NULL OR g.entry_date::timestamptz + (ip.warmup_no_vaccination_days * INTERVAL '1 day') <= ip.as_of
    )
  )
  AND ('goat'::text = '' OR g.species = 'goat'::text)
  AND ('K1'::text = '' OR COALESCE(asl.stage_code, g.management_stage, '') = 'K1'::text)
  AND ('female'::text = '' OR g.sex = 'female'::text)
  AND ('beetal'::text = '' OR g.breed = 'beetal'::text)
  AND ('healthy'::text = '' OR COALESCE(g.health_status, '') = 'healthy'::text)"

  explain_must_use_index "VaccinationImpactCountEligibleGoats" 'Seq Scan on goats|Seq Scan on procurement_load_goats|Seq Scan on location_operational_attributes|SubPlan' "EXPLAIN (COSTS OFF)
SELECT count(*)::bigint AS total
$impact_predicates;"

  explain_must_use_index "VaccinationImpactCountCatchupGoats" 'Seq Scan on goats|Seq Scan on procurement_load_goats|Seq Scan on vaccination_completions|Seq Scan on location_operational_attributes|SubPlan' "EXPLAIN (COSTS OFF)
SELECT count(DISTINCT g.goat_id)::bigint AS total
FROM goats g
CROSS JOIN (
  SELECT 7::int AS warmup_no_vaccination_days,
         TIMESTAMPTZ '2026-06-29 12:00:00+00' AS as_of
) ip
JOIN vaccination_completions vc
  ON vc.tenant_id = g.tenant_id
 AND vc.goat_id = g.goat_id
 AND vc.status = 'accepted'
LEFT JOIN LATERAL (
  SELECT COALESCE(plg.warmup_started_at, plg.intake_accepted_at, g.entry_date::timestamptz) AS warming_entry_at
  FROM procurement_load_goats plg
  WHERE plg.tenant_id = g.tenant_id
    AND plg.goat_id = g.goat_id
  ORDER BY COALESCE(plg.warmup_started_at, plg.intake_accepted_at, plg.created_at) DESC NULLS LAST
  LIMIT 1
) proc ON true
LEFT JOIN location_operational_attributes loa
  ON loa.tenant_id = g.tenant_id
 AND loa.location_id = COALESCE(g.current_location_id, g.shed_id)
LEFT JOIN shed_profiles sp
  ON sp.tenant_id = g.tenant_id
 AND sp.location_id = g.shed_id
LEFT JOIN animal_stage_lookup asl
  ON asl.tenant_id = sp.tenant_id
 AND asl.animal_stage_id = sp.animal_stage_id
 AND asl.status = 'active'
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND g.lifecycle_status = 'alive'
  AND COALESCE(g.health_status, '') NOT IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
  AND COALESCE(loa.usable_for_vaccination, true)
  AND NOT COALESCE(loa.is_quarantine, false)
  AND NOT COALESCE(loa.is_icu, false)
  AND (
    ip.warmup_no_vaccination_days <= 0
    OR COALESCE(
      proc.warming_entry_at + (ip.warmup_no_vaccination_days * INTERVAL '1 day') <= ip.as_of,
      g.entry_date IS NULL OR g.entry_date::timestamptz + (ip.warmup_no_vaccination_days * INTERVAL '1 day') <= ip.as_of
    )
  )
  AND ('goat'::text = '' OR g.species = 'goat'::text)
  AND ('K1'::text = '' OR COALESCE(asl.stage_code, g.management_stage, '') = 'K1'::text)
  AND ('female'::text = '' OR g.sex = 'female'::text)
  AND ('beetal'::text = '' OR g.breed = 'beetal'::text)
  AND ('healthy'::text = '' OR COALESCE(g.health_status, '') = 'healthy'::text);"

  explain_must_use_index "VaccinationImpactCountEligibleShedScopes" 'Seq Scan on goats|Seq Scan on procurement_load_goats|Seq Scan on location_operational_attributes|SubPlan' "EXPLAIN (COSTS OFF)
SELECT count(DISTINCT g.shed_id)::bigint AS total
$impact_predicates
  AND g.shed_id IS NOT NULL;"

  explain_must_use_index "VaccinationRecoverableDeferredRepairCandidates" 'Seq Scan on obligation_instances|Seq Scan on goats|Seq Scan on procurement_load_goats|Seq Scan on location_operational_attributes' "EXPLAIN (COSTS OFF)
WITH earliest_by_goat AS (
  SELECT DISTINCT ON (oi.target_id)
         oi.target_id::text AS goat_id,
         oi.due_at,
         oi.obligation_id
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
  JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
  LEFT JOIN location_operational_attributes loa
    ON loa.tenant_id = g.tenant_id
   AND loa.location_id = COALESCE(g.current_location_id, g.shed_id)
  WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND oi.target_type = 'goat'
    AND oi.status = 'deferred'
    AND oi.due_at <= TIMESTAMPTZ '2026-06-22 12:00:00+00'
    AND pd.category = 'vaccination'
    AND g.lifecycle_status = 'alive'
    AND COALESCE(g.health_status, '') NOT IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
    AND COALESCE(loa.usable_for_vaccination, true)
    AND NOT COALESCE(loa.is_quarantine, false)
    AND NOT COALESCE(loa.is_icu, false)
    AND NOT EXISTS (
      SELECT 1
      FROM vw_procurement_vaccination_excluded_goats ex
      WHERE ex.tenant_id = oi.tenant_id
        AND ex.goat_id = oi.target_id
    )
  ORDER BY oi.target_id, oi.due_at ASC, oi.obligation_id ASC
)
SELECT goat_id
FROM earliest_by_goat
ORDER BY due_at ASC, obligation_id ASC
LIMIT 1000;"
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
   AND g.merged_into_goat_id IS NULL
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
         OR COALESCE(located.goat_health_status, '') IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
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
       AND enriched.completed_count < enriched.obligation_count THEN 'blocked'
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
   AND g.merged_into_goat_id IS NULL
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
         OR COALESCE(located.goat_health_status, '') IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
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
       AND enriched.completed_count < enriched.expected_count THEN 'blocked'
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
WHERE work_state IN ('rejected', 'blocked', 'blocked', 'overdue', 'proof_pending', 'verification_pending')
ORDER BY
  CASE work_state
    WHEN 'rejected' THEN 0
    WHEN 'blocked' THEN 1
    WHEN 'blocked' THEN 2
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
  AND source_entry_state = 'accepted'
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

validate_calendar_canonical_read_plan() {
  # U4a (ADR operational-kernel-5k-50k-scale-envelope): the Calendar API reads through to canonical
  # tables (calendarCanonicalListSQL) when the calendar_event_projections freshness gate would
  # otherwise fail closed. That read is a compute-on-read multi-CTE reconstruction whose scale-
  # critical scan is the tenant+due-window keyset page over obligation_instances. Prove that driving
  # scan is index-backed, never a sequential scan. The Go sibling
  # TestCalendarCanonicalListKeysetPlanUsesIndex additionally EXPLAINs the full assembled read.
  explain_must_use_index "CalendarCanonicalReadKeysetDriver" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT obligation_id
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND batch_id IS NULL
  AND due_at >= TIMESTAMPTZ '2026-06-27 00:00:00+00'
  AND due_at < TIMESTAMPTZ '2026-08-12 00:00:00+00'
ORDER BY due_at ASC, obligation_id ASC
LIMIT 21;"
}

validate_calendar_vaccination_plans() {
  # CalendarVaccinationWidestList (formerly a direct calendar_event_projections read) is retired
  # along with that table (5k-50k envelope, migration 000189,
  # docs/decisions/operational-kernel-5k-50k-scale-envelope.md). The widest-list scan shape is now
  # exactly calendarCanonicalListSQL's canonical_selected read over source_events, whose driving
  # scan is proven by validate_calendar_canonical_read_plan's CalendarCanonicalReadKeysetDriver and
  # by the Go-level canonical_read_plan_test.go.
  :
}

validate_herd_register_summary_plan() {
  # C35-005: Herd Register KPIs read the bounded summary projection via the scope
  # index (tenant_id, lifecycle_status, park_id, breed, sex, ...), never an
  # aggregate over the goats table. Prove the tenant+status KPI path is indexed.
  explain_must_use_index "HerdRegisterSummary" 'Seq Scan on herd_register_summary_projection' "EXPLAIN (COSTS OFF)
SELECT park_id, farm_id, current_location_id, breed, sex, lifecycle_status,
       active_count, adult_count, kid_count, untagged_kid_count, projected_at
FROM herd_register_summary_projection
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND lifecycle_status = COALESCE(NULLIF('alive', ''), lifecycle_status)
  AND (''::text = '' OR park_id = NULLIF(''::text, '')::uuid)
  AND (''::text = '' OR breed = ''::text)
  AND (''::text = '' OR sex = ''::text)
ORDER BY park_id, farm_id, current_location_id, breed, sex, lifecycle_status;"
}

validate_verification_queue_plan() {
  # Generic Verification vertical (context/architecture/verification-module-design.md): the
  # Verifier's queue read (GET /verification/queue) keysets by (captured_at, item_id) filtered by
  # tenant + status + category. Proves the request path stays on verification_items_queue_idx
  # (tenant_id, status, category, captured_at, item_id), never a sequential scan.
  explain_must_use_index "VerificationQueueKeyset" 'Seq Scan on verification_items' "EXPLAIN (COSTS OFF)
SELECT item_id
FROM verification_items
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status = 'pending'
  AND category = 'vaccination_proof'
ORDER BY captured_at ASC, item_id ASC
LIMIT 20;"
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
validate_outbox_release_plan
validate_notification_claim_plan
validate_auth_grant_lookup_plan
validate_obligation_due_window_plan
validate_obligation_scope_count_plan
validate_obligation_open_by_goat_plan
validate_obligation_planned_batch_finalization_plan
validate_combo_align_keyset_plan
validate_kernel_sweeper_hot_path_plans
validate_obligation_unbatched_due_keyset_plan
validate_inventory_fefo_plan
validate_inventory_movements_ledger_plan
validate_inventory_batch_reconcile_plan
validate_vaccination_generation_scan_plan
validate_vaccination_gaps_plan
validate_vaccination_impact_count_plans
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
validate_calendar_canonical_read_plan
validate_herd_register_summary_plan
validate_verification_queue_plan

echo "Validated current hot-path query plans"
