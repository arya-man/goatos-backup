-- name: GetObligationByIdempotencyKey :one
SELECT obligation_id::text AS obligation_id, status, due_at, row_version
FROM obligation_instances
WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key;

-- name: GetOpenObligationByLogicalKey :one
-- Convergent no-op lookup for InsertObligationInstance's "WHERE NOT EXISTS" dedup guard (mirrors that
-- exact predicate): when InsertObligationInstance affects 0 rows because an equivalent open obligation
-- already exists for the same logical target (protocol_version_id, rule_id, target, sequence, due_at),
-- callers use this to fetch that existing row and return it as an idempotent success instead of
-- surfacing an internal error. See Repository.insertReworkObligationForMissed.
SELECT obligation_id::text AS obligation_id, status, due_at, row_version
FROM obligation_instances
WHERE tenant_id = @tenant_id
  AND protocol_version_id = @protocol_version_id
  AND rule_id = @rule_id
  AND target_type = @target_type
  AND target_id = @target_id
  AND "sequence" = @sequence
  AND due_at = @due_at
  AND status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed')
ORDER BY obligation_id
LIMIT 1;

-- name: ListOpenObligationsByGoat :many
-- Goat Passport next-due: a goat's still-actionable obligations, earliest due first. Vaccination
-- drive rows must emit the live assignment planned date or batch planned date, not the original
-- obligation due_at.
SELECT oi.obligation_id::text AS obligation_id, oi.protocol_version_id::text AS protocol_version_id,
       oi.rule_id::text AS rule_id, oi.scope_type, COALESCE(oi.scope_id::text, '')::text AS scope_id,
       COALESCE(oi.batch_id::text, '')::text AS batch_id,
       COALESCE(vda_member.assignment_planned_at, vda_guess.assignment_planned_at, (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), oi.due_at)::timestamptz AS due_at,
       oi.due_at AS clinical_due_at,
       COALESCE(vda_member.assignment_planned_at, vda_guess.assignment_planned_at, (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'))::timestamptz AS scheduled_for,
       COALESCE(pr.dose_code, '')::text AS dose_code,
       COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'display_name', ''), NULLIF(prd.vaccine_code, ''), NULLIF(pr.dose_code, ''), '')::text AS vaccine_label,
       oi.status, oi."sequence"
FROM obligation_instances oi
LEFT JOIN obligation_batches ob
  ON ob.tenant_id = oi.tenant_id
 AND ob.batch_id = oi.batch_id
LEFT JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
LEFT JOIN LATERAL (
  SELECT vaccine_code
  FROM protocol_rule_dimensions dim
  WHERE dim.tenant_id = pr.tenant_id
    AND dim.rule_id = pr.rule_id
  ORDER BY NULLIF(dim.vaccine_code, '') NULLS LAST, dim.protocol_rule_dimension_id
  LIMIT 1
) prd ON true
LEFT JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
 AND g.merged_into_goat_id IS NULL
-- projection-review: membership=this obligation's exact vaccination_drive_assignment_members row
-- (tenant_id, obligation_id) UNIQUE. That member row binds the goat/obligation to one assignment
-- row, so scheduled_for is the goat's own operator drive date. The lateral guess below is legacy
-- fallback only when old rows have no member binding.
LEFT JOIN vaccination_drive_assignment_members m
  ON m.tenant_id = oi.tenant_id
 AND m.obligation_id = oi.obligation_id
 AND m.goat_id = oi.target_id
LEFT JOIN vaccination_drive_assignments assignment
  ON assignment.tenant_id = m.tenant_id
 AND assignment.assignment_id = m.assignment_id
LEFT JOIN LATERAL (
  SELECT (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
) vda_member ON assignment.assignment_id IS NOT NULL
LEFT JOIN LATERAL (
  SELECT (guess.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
  FROM vaccination_drive_assignments guess
  WHERE m.assignment_id IS NULL
    AND guess.tenant_id = oi.tenant_id
    AND guess.batch_id = oi.batch_id
    AND guess.shed_id = g.shed_id
    AND (
      cardinality(guess.vaccine_rule_ids) = 0
      OR oi.rule_id = ANY(guess.vaccine_rule_ids)
    )
  ORDER BY guess.planned_date ASC,
           guess.partition_label ASC,
           guess.operator_id ASC NULLS LAST,
           guess.assignment_id ASC
  LIMIT 1
) vda_guess ON true
WHERE oi.tenant_id = @tenant_id AND oi.target_type = 'goat' AND oi.target_id = @target_id
  AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed')
ORDER BY COALESCE(vda_member.assignment_planned_at, vda_guess.assignment_planned_at, (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), oi.due_at)::timestamptz ASC, oi.obligation_id ASC
LIMIT @row_limit;

-- name: GetObligationBoosterContext :one
-- SM-7 basis on the verify path: the obligation's version + scope + sequence, looked up by PK
-- (obligation_instances_tenant_id_unique) so the booster can schedule the next dose without the
-- caller threading protocol context through the verification event.
SELECT protocol_version_id::text AS protocol_version_id,
       scope_type, COALESCE(scope_id::text, '')::text AS scope_id, "sequence"
FROM obligation_instances
WHERE tenant_id = @tenant_id AND obligation_id = @obligation_id;

-- name: ListDueObligations :many
-- Due-window scan. Uses obligation_instances_due_window_idx (tenant_id, status, due_at, obligation_id).
SELECT obligation_id::text AS obligation_id, protocol_version_id::text AS protocol_version_id,
       rule_id::text AS rule_id, target_type, target_id::text AS target_id,
       scope_type, COALESCE(scope_id::text, '')::text AS scope_id, due_at, status
FROM obligation_instances
WHERE tenant_id = @tenant_id AND status = @status AND due_at <= @due_before
ORDER BY due_at ASC, obligation_id ASC
LIMIT @row_limit;

-- name: ListUnbatchedDueForVersion :many
-- SM-4 sweeper: unbatched scheduled/due obligations for a version within the window, grouped by
-- scope + rule + due/window downstream. batch_id IS NULL makes re-sweeps idempotent.
-- Uses obligation_instances_unbatched_due_version_idx.
SELECT oi.obligation_id::text AS obligation_id,
       oi.rule_id::text AS rule_id,
       oi.scope_type,
       COALESCE(oi.scope_id::text, '')::text AS scope_id,
       COALESCE(g.park_id::text, '')::text AS park_id,
       COALESCE(shed.name, '')::text AS shed_name,
       COALESCE(gsp.partition_label, '')::text AS partition_label,
       COALESCE(oi.target_id::text, '')::text AS target_id,
       CASE
         WHEN oi.target_type = 'goat' THEN COALESCE(g.species, 'goat')::text
         ELSE ''
       END AS target_species,
       CASE
         WHEN oi.target_type = 'goat' THEN COALESCE(asl.stage_code, g.management_stage, '')::text
         ELSE ''
       END AS target_animal_stage,
       CASE
         WHEN oi.target_type = 'goat' THEN COALESCE(g.reproductive_status, '')::text
         ELSE ''
       END AS target_reproductive_status,
       oi.due_at,
       oi.window_start,
       COALESCE(oi.window_end, oi.due_at + make_interval(days => GREATEST(COALESCE(pr.due_window_days, 0), 0))) AS window_end,
       COALESCE(oi.batching_hold_count, 0)::int AS batching_hold_count,
       oi.first_batching_hold_until
FROM obligation_instances oi
LEFT JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
LEFT JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
 AND oi.target_type = 'goat'
LEFT JOIN locations shed
  ON shed.tenant_id = oi.tenant_id
 AND shed.location_id = COALESCE(g.shed_id, CASE WHEN oi.scope_type = 'shed' THEN oi.scope_id END)
 AND shed.location_type = 'shed'
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id
 AND gsp.goat_id = g.goat_id
 AND gsp.shed_id = shed.location_id
LEFT JOIN location_operational_attributes loa
  ON loa.tenant_id = g.tenant_id
 AND loa.location_id = g.current_location_id
LEFT JOIN shed_profiles sp
  ON sp.tenant_id = g.tenant_id
 AND sp.location_id = COALESCE(g.shed_id, CASE WHEN oi.scope_type = 'shed' THEN oi.scope_id END)
LEFT JOIN animal_stage_lookup asl
  ON asl.tenant_id = sp.tenant_id
 AND asl.animal_stage_id = sp.animal_stage_id
 AND asl.status = 'active'
WHERE oi.tenant_id = @tenant_id
  AND oi.protocol_version_id = @protocol_version_id
  AND oi.status IN ('scheduled', 'due', 'missed')
  AND oi.batch_id IS NULL
  AND oi.due_at <= @due_before
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
ORDER BY oi.scope_type, oi.scope_id, oi.rule_id, target_species, target_animal_stage, oi.due_at, oi.obligation_id
LIMIT @row_limit;

-- name: CountObligationsByScope :one
-- Per-scope rollup. Uses obligation_instances_scope_idx (tenant_id, scope_type, scope_id, status, due_at).
SELECT COUNT(*)::bigint AS total
FROM obligation_instances
WHERE tenant_id = @tenant_id AND scope_type = @scope_type
  AND scope_id = @scope_id AND status = @status;

-- name: FindNearestPlannedBatchDate :one
-- Sick-recovery align: earliest compatible planned drive in shed or park within the align window.
SELECT b.planned_date
FROM obligation_batches b
WHERE b.tenant_id = @tenant_id
  AND b.protocol_version_id = @protocol_version_id
  AND b.session = ANY(@sessions::text[])
  AND b.status = 'planned'
  AND b.planned_date >= @from_date::date
  AND b.planned_date <= @to_date::date
  AND (
    (@shed_id::uuid IS NOT NULL AND b.scope_type = 'shed' AND b.scope_id = @shed_id)
    OR (@park_id::uuid IS NOT NULL AND b.scope_type = 'park' AND b.scope_id = @park_id)
  )
ORDER BY b.planned_date ASC
LIMIT 1;

-- name: IdempotencyKeyStatus :one
SELECT status, COALESCE(result_type, '')::text AS result_type, COALESCE(result_id::text, '')::text AS result_id
FROM idempotency_keys
WHERE idempotency_key = @idempotency_key;
