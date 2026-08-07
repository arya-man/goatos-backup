// Package postgres — canonical read-through for the Calendar API.
//
// U4a (ADR operational-kernel-5k-50k-scale-envelope): at the 5k-to-50k envelope the Calendar API
// serves from canonical tables through bounded, indexed SQL instead of gating reads on the
// calendar_event_projections freshness watermark. calendarCanonicalListSQL reconstructs the same
// calendar event rows the projector materializes (obligation_instances / obligation_batches /
// sop_tasks / protocol_* / locations / goats) as a keyset page, reusing the projector's own
// source_events CTE chain verbatim so the row/summary/drive semantics never diverge. This is
// ADDITIVE read-through: calendar_event_projections is unchanged (its retirement is a later unit) and
// remains the primary, mutable-state (reminder/escalation/snooze) serving path; ListEvents falls
// through to this canonical read only when the projection would otherwise fail closed
// (never-synced / requested window outside projected coverage).
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
)

// calendarCanonicalEventsCTE is the shared canonical event-reconstruction CTE chain ("source_events")
// reused by every canonical Calendar read: the list page (calendarCanonicalListSQL), the single-event
// detail lookup (calendarCanonicalDetailSQL), the reminder rail, and the reminder/escalation
// operational sweeps in repository.go/reminder_cadence.go. Keeping ONE copy of this chain means the
// row/summary/drive semantics can never diverge between the list, detail, and sweep paths. It is
// parameterized by $1 (tenant), $2 (window dateFrom), $3 (window dateToExclusive) -- callers that have
// no natural list window (single-event lookups, operational sweeps) pass a generously wide window via
// canonicalUnboundedWindow so the OR-bypassed missed/in_progress/deferred/overdue rows are still
// unaffected (those branches do not depend on the window) while genuinely future-scheduled rows stay
// reachable. NOTE: unlike canonical_selected below, this chain does NOT exclude event_type =
// 'vaccination_dose_due' (individual obligation rows) -- callers that must see those (single-event
// detail/action-target resolution, the obligation-specific escalation lookup) query source_events
// directly; callers that must NOT (the list, the general reminder/escalation sweeps) add their own
// `event_type <> 'vaccination_dose_due'` predicate, exactly mirroring the pre-cutover projector's own
// distinction between its general sweep and its single-obligation lookup.
//
// SCALE-OPTIMIZATION (migration 000190): The original OR predicate combining three date/status branches
// caused seq-scans at scale (100k+ obligations). Refactored as UNION ALL of three indexed branches:
// - Branch 1: window-bound (index: idx_obligation_instances_calendar_window on (tenant_id, due_at))
// - Branch 2: bounded exception catch-up (index: idx_obligation_instances_calendar_exceptions_due on (tenant_id, status, due_at))
// - Branch 3: overdue-by-due_at (index: idx_obligation_instances_calendar_overdue on (tenant_id, status, due_at))
// This ensures the planner uses index scans for each branch and stays sub-second at 500k obligations.
//
// calendarTodayInRequestedWindow is true only when the CURRENT Asia/Kolkata business day falls inside
// the requested [$2, $3) window. It gates the P1 drive-rollover lookback below.
//
// The rollover re-dates an open past drive onto TODAY. That rolled date is meaningful ONLY to a query
// whose window actually contains today: a window for tomorrow (or any later day) must not be handed a
// card dated today. Without this guard the widened 45-day lower bound admitted the batch into EVERY
// future window and the rollover then stamped it as today's date, so tapping the 2nd, the 3rd and
// the 4th on the phone all showed the same cards. Anchored to the business-day start
// (biztime.BusinessDayStart's SQL twin, the same expression rolled_due_at uses), never now()±N hours.
const calendarTodayInRequestedWindow = `(
          (now() AT TIME ZONE 'Asia/Kolkata')::date::timestamp AT TIME ZONE 'Asia/Kolkata' >= $2::timestamptz
      AND (now() AT TIME ZONE 'Asia/Kolkata')::date::timestamp AT TIME ZONE 'Asia/Kolkata' <  $3::timestamptz
        )`

// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarCanonicalEventsCTE = `obligation_events AS (
  WITH obligation_events_rows AS (
    -- Index-bound decomposition of the former 3-way OR (see migration 000190). Each branch is a
    -- guaranteed index scan on a partial index; the branches select FULL obligation_instances rows so
    -- there is no join back to the base table (a candidate-id join let the planner hash-join against a
    -- full seq scan). UNION (distinct over the whole row) dedups an obligation matching two branches so
    -- it is emitted exactly once. Semantically identical to the original OR over the same tenant/
    -- unbatched/active-status set.
    -- Branch 1: window-bound (index: idx_obligation_instances_calendar_window)
    SELECT * FROM obligation_instances
    WHERE tenant_id = $1::uuid AND batch_id IS NULL
      AND status NOT IN ('waived', 'canceled', 'superseded', 'completed')
      AND due_at >= $2::timestamptz AND due_at < $3::timestamptz
    UNION
    -- Branch 2: bounded exception catch-up (index: idx_obligation_instances_calendar_exceptions_due)
    SELECT * FROM obligation_instances
    WHERE tenant_id = $1::uuid AND batch_id IS NULL
      AND status IN ('missed', 'in_progress', 'deferred')
      AND due_at >= $2::timestamptz - interval '45 days'
      AND due_at < $3::timestamptz
    UNION
    -- Branch 3: overdue-by-due_at (index: idx_obligation_instances_calendar_overdue)
    SELECT * FROM obligation_instances
    WHERE tenant_id = $1::uuid AND batch_id IS NULL
      AND status IN ('scheduled', 'due') AND due_at < now()
      AND due_at >= $2::timestamptz - interval '45 days'
      AND due_at < $3::timestamptz
  )
  SELECT
    'obligation:' || oi.obligation_id::text AS event_id,
    'vaccination_dose_due'::text AS event_type,
    'pc'::text AS owner_key,
    pd.name || ' ' || pr.dose_code || ' due' AS title,
    COALESCE(loc.shed_name, loc.park_code, 'Vaccination obligation') AS subtitle,
    CASE oi.status
      WHEN 'scheduled' THEN CASE WHEN oi.due_at < now() THEN 'overdue' ELSE 'scheduled' END
      WHEN 'due' THEN CASE WHEN oi.due_at < now() THEN 'overdue' ELSE 'due' END
      WHEN 'missed' THEN 'missed'
      WHEN 'waived' THEN 'deferred'
      WHEN 'superseded' THEN 'canceled'
      ELSE oi.status
    END AS status,
    CASE
      WHEN oi.status = 'completed' THEN 'info'
      WHEN oi.status = 'missed' OR oi.due_at < now() THEN 'critical'
      WHEN oi.due_at <= now() + interval '24 hours' THEN 'warning'
      ELSE 'info'
    END AS severity,
    oi.due_at,
    COALESCE(oi.window_start, oi.due_at) AS window_start,
    COALESCE(oi.window_end, oi.due_at + make_interval(days => pr.due_window_days)) AS window_end,
    'Asia/Kolkata'::text AS timezone,
    'india_only'::text AS timezone_source,
    loc.park_id,
    loc.park_code,
    loc.shed_id,
    loc.shed_name,
    CASE WHEN oi.scope_type = 'cohort' THEN oi.scope_id END AS cohort_id,
    CASE WHEN oi.scope_type = 'cohort' THEN scope_loc.name END AS cohort_name,
    oi.target_type,
    1::int AS target_count,
    pd.protocol_id,
    pv.protocol_version_id,
    pr.rule_id,
    pd.name AS vaccine_name,
    pr.dose_code,
    true AS source_backed,
    pd.name AS source_label,
    'obligation'::text AS source_target_type,
    oi.obligation_id AS source_target_id,
    'PC vaccinator'::text AS assignee_label,
    'pc_vaccinator'::text AS executor_role,
    NULL::text AS verifier_label,
    'not_scheduled'::text AS reminder_state,
    'local-stub'::text AS primary_notification_channel,
    'none'::text AS escalation_state,
    false AS system,
    false AS cross_cutting,
    jsonb_build_object(
      'vaccination', '/vaccination/operations',
      'action_center', '/vaccination/action-center',
      'workflow', '/vaccination/workflows/' || ('obligation:' || oi.obligation_id::text)
    ) AS links,
    jsonb_build_object(
      'summary', jsonb_build_object('owner', 'PC', 'target_count', 1),
      'source_and_rule', jsonb_build_object(
        'protocol_version_id', pv.protocol_version_id,
        'rule_id', pr.rule_id
      ),
      'execution', jsonb_build_object('obligation_id', oi.obligation_id, 'work_state', oi.status),
      'stock', jsonb_build_object(),
      'proof', jsonb_build_object('sop_task_id', oi.sop_task_id),
      'verification', jsonb_build_object(),
      'notification_channels', jsonb_build_array('local-stub'),
      'notification_policy', jsonb_build_object('reminder', 'due_minus_1h'),
      'links', jsonb_build_object()
    ) AS detail
  FROM obligation_events_rows oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
  LEFT JOIN locations scope_loc
    ON scope_loc.tenant_id = oi.tenant_id
   AND scope_loc.location_id = oi.scope_id
   AND oi.scope_type IN ('park', 'shed', 'cohort')
  LEFT JOIN locations scope_parent
    ON scope_parent.tenant_id = oi.tenant_id
   AND scope_parent.location_id = scope_loc.parent_location_id
  LEFT JOIN locations scope_grand
    ON scope_grand.tenant_id = oi.tenant_id
   AND scope_grand.location_id = scope_parent.parent_location_id
  LEFT JOIN LATERAL (
    SELECT
      CASE
        WHEN oi.scope_type = 'park' THEN scope_loc.location_id
        WHEN oi.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id
        WHEN oi.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_id
      END AS park_id,
      CASE
        WHEN oi.scope_type = 'park' THEN scope_loc.location_code
        WHEN oi.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_code
        WHEN oi.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_code
      END AS park_code,
      CASE
        WHEN oi.scope_type = 'shed' THEN scope_loc.location_id
        WHEN oi.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.location_id
      END AS shed_id,
      CASE
        WHEN oi.scope_type = 'shed' THEN scope_loc.name
        WHEN oi.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.name
      END AS shed_name
  ) loc ON true
  WHERE pd.category = 'vaccination'
    AND pv.status = 'published'
    AND oi.status NOT IN ('waived', 'canceled', 'superseded', 'completed')
),
catchup_drive_events AS (
  SELECT
    CASE
      WHEN grouped.park_id IS NOT NULL THEN
        'catchup:park:' || grouped.park_id::text || ':due:' || grouped.due_day
      ELSE
        'catchup:tenant:' || $1::text || ':due:' || grouped.due_day
    END AS event_id,
    'vaccination_drive'::text AS event_type,
    'pc'::text AS owner_key,
    CASE
      WHEN grouped.park_id IS NOT NULL THEN 'Park vaccination drive'
      ELSE 'Vaccination drive'
    END AS title,
    'Catch-up drive'::text AS subtitle,
    grouped.status,
    grouped.severity,
    grouped.due_at,
    grouped.window_start,
    grouped.window_end,
    grouped.timezone,
    grouped.timezone_source,
    grouped.park_id,
    grouped.park_code,
    CASE WHEN grouped.shed_count = 1 THEN grouped.primary_shed_id ELSE NULL::uuid END AS shed_id,
    CASE WHEN grouped.shed_count = 1 THEN grouped.primary_shed_name ELSE NULL::text END AS shed_name,
    NULL::uuid AS cohort_id,
    NULL::text AS cohort_name,
    CASE
      WHEN grouped.shed_count = 1 THEN 'shed'::text
      WHEN grouped.park_id IS NOT NULL THEN 'park'::text
      ELSE 'tenant'::text
    END AS target_type,
    grouped.target_count,
    NULL::uuid AS protocol_id,
    NULL::uuid AS protocol_version_id,
    NULL::uuid AS rule_id,
    queue_meta.queue_summary AS vaccine_name,
    queue_meta.queue_preview AS dose_code,
    true AS source_backed,
    queue_meta.queue_summary AS source_label,
    'catchup'::text AS source_target_type,
    COALESCE(grouped.park_id, CASE WHEN grouped.shed_count = 1 THEN grouped.primary_shed_id ELSE NULL::uuid END, $1::uuid) AS source_target_id,
    'PC drive team'::text AS assignee_label,
    'pc_vaccinator'::text AS executor_role,
    'PC verifier'::text AS verifier_label,
    'not_scheduled'::text AS reminder_state,
    'local-stub'::text AS primary_notification_channel,
    'none'::text AS escalation_state,
    false AS system,
    false AS cross_cutting,
    jsonb_build_object(
      'vaccination', '/vaccination/operations',
      'drive', CASE WHEN grouped.shed_count = 1 AND grouped.primary_shed_id IS NOT NULL THEN '/vaccination/execution/sheds/' || grouped.primary_shed_id::text ELSE NULL END
    ) AS links,
    jsonb_build_object(
      'summary', jsonb_build_object(
        'owner', 'PC',
        'target_count', grouped.target_count,
        'catchup', true,
        'queue_count', grouped.queue_count,
        'queue_preview', queue_meta.queue_preview,
        'shed_count', grouped.shed_count,
        'shed_labels', to_jsonb(grouped.shed_labels),
        'vaccine_labels', to_jsonb(grouped.vaccine_labels)
      ),
      'source_and_rule', jsonb_build_object(
        'due_day', grouped.due_day,
        'queue_count', grouped.queue_count,
        'queue_preview', queue_meta.queue_preview
      ),
      'execution', jsonb_build_object(
        'catchup', true,
        'work_state', grouped.status,
        'shed_count', grouped.shed_count
      ),
      'stock', jsonb_build_object(),
      'proof', jsonb_build_object(),
      'verification', jsonb_build_object(),
      'notification_channels', jsonb_build_array('local-stub'),
      'notification_policy', jsonb_build_object('nudge_allowed', false, 'read_only', true),
      'links', jsonb_build_object()
    ) AS detail
  FROM (
    WITH catchup_rows AS (
      -- Same index-bound UNION decomposition as obligation_events_rows (migration 000190):
      -- window / exception / overdue branches selecting FULL rows (no join-back), deduped so a row
      -- matching two branches is counted once.
      SELECT * FROM obligation_instances
      WHERE tenant_id = $1::uuid AND batch_id IS NULL
        AND status NOT IN ('waived', 'canceled', 'superseded', 'completed')
        AND due_at >= $2::timestamptz AND due_at < $3::timestamptz
      UNION
      SELECT * FROM obligation_instances
      WHERE tenant_id = $1::uuid AND batch_id IS NULL
        AND status IN ('missed', 'in_progress', 'deferred')
      UNION
      SELECT * FROM obligation_instances
      WHERE tenant_id = $1::uuid AND batch_id IS NULL
        AND status IN ('scheduled', 'due') AND due_at < now()
    )
    SELECT
      loc.park_id,
      loc.park_code,
      count(DISTINCT loc.shed_id)::int AS shed_count,
      NULLIF(min(loc.shed_id::text), '')::uuid AS primary_shed_id,
      min(loc.shed_name) FILTER (WHERE loc.shed_name IS NOT NULL) AS primary_shed_name,
      'Asia/Kolkata'::text AS timezone,
      'india_only'::text AS timezone_source,
      to_char((oi.due_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD') AS due_day,
      count(DISTINCT oi.target_id)::int AS target_count,
      count(DISTINCT pr.rule_id)::int AS queue_count,
      array_agg(
        DISTINCT COALESCE(NULLIF(pr.dose_code, ''), pd.name) || '|' || pr.rule_id::text
        ORDER BY COALESCE(NULLIF(pr.dose_code, ''), pd.name) || '|' || pr.rule_id::text
      ) AS queue_labels,
      array_agg(DISTINCT loc.shed_name ORDER BY loc.shed_name) FILTER (WHERE loc.shed_name IS NOT NULL) AS shed_labels,
      array_agg(
        DISTINCT COALESCE(NULLIF(prd.vaccine_json->>'name', ''), pd.name)
        ORDER BY COALESCE(NULLIF(prd.vaccine_json->>'name', ''), pd.name)
      ) AS vaccine_labels,
      min(oi.due_at) AS due_at,
      min(COALESCE(oi.window_start, oi.due_at)) AS window_start,
      max(COALESCE(oi.window_end, oi.due_at + make_interval(days => pr.due_window_days))) AS window_end,
      CASE
        WHEN bool_or(oi.status = 'missed') THEN 'missed'
        WHEN bool_or(
          oi.status IN ('scheduled', 'due')
          AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < (now() AT TIME ZONE 'Asia/Kolkata')::date
        ) THEN 'overdue'
        WHEN bool_or(oi.status = 'in_progress') THEN 'in_progress'
        WHEN bool_or(oi.status = 'deferred') THEN 'deferred'
        WHEN bool_or(oi.status = 'due') THEN 'due'
        ELSE 'scheduled'
      END AS status,
      CASE
        WHEN bool_or(oi.status = 'missed')
          OR bool_or((oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < (now() AT TIME ZONE 'Asia/Kolkata')::date) THEN 'critical'
        WHEN min(oi.due_at) <= now() + interval '24 hours' THEN 'warning'
        ELSE 'info'
      END AS severity
    FROM catchup_rows oi
    JOIN protocol_versions pv
      ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
    JOIN protocol_rules pr
      ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
    LEFT JOIN protocol_rule_dimensions prd
      ON prd.tenant_id = pr.tenant_id AND prd.rule_id = pr.rule_id
    LEFT JOIN locations scope_loc
      ON scope_loc.tenant_id = oi.tenant_id
     AND scope_loc.location_id = oi.scope_id
     AND oi.scope_type IN ('park', 'shed', 'cohort')
    LEFT JOIN locations scope_parent
      ON scope_parent.tenant_id = oi.tenant_id
     AND scope_parent.location_id = scope_loc.parent_location_id
    LEFT JOIN locations scope_grand
      ON scope_grand.tenant_id = oi.tenant_id
     AND scope_grand.location_id = scope_parent.parent_location_id
    LEFT JOIN LATERAL (
      SELECT
        CASE
          WHEN oi.scope_type = 'park' THEN scope_loc.location_id
          WHEN oi.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id
          WHEN oi.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_id
        END AS park_id,
        CASE
          WHEN oi.scope_type = 'park' THEN scope_loc.location_code
          WHEN oi.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_code
          WHEN oi.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_code
        END AS park_code,
        CASE
          WHEN oi.scope_type = 'shed' THEN scope_loc.location_id
          WHEN oi.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.location_id
        END AS shed_id,
        CASE
          WHEN oi.scope_type = 'shed' THEN scope_loc.name
          WHEN oi.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.name
        END AS shed_name
    ) loc ON true
    WHERE pd.category = 'vaccination'
      AND pv.status = 'published'
      AND oi.status NOT IN ('waived', 'canceled', 'superseded', 'completed')
    GROUP BY
      loc.park_id, loc.park_code,
      'Asia/Kolkata'::text,
      'india_only'::text,
      due_day
  ) grouped
  CROSS JOIN LATERAL (
    SELECT
      CASE
        WHEN grouped.queue_count <= 0 THEN 'Vaccination queue'
        WHEN grouped.queue_count = 1 THEN '1 vaccine queue'
        ELSE grouped.queue_count::text || ' vaccine queues'
      END AS queue_summary,
      CASE
        WHEN grouped.queue_count <= 0 THEN 'Vaccination queue'
        WHEN grouped.queue_count = 1 THEN split_part(grouped.queue_labels[1], '|', 1)
        WHEN grouped.queue_count = 2 THEN split_part(grouped.queue_labels[1], '|', 1) || ', ' || split_part(grouped.queue_labels[2], '|', 1)
        WHEN grouped.queue_count = 3 THEN split_part(grouped.queue_labels[1], '|', 1) || ', ' || split_part(grouped.queue_labels[2], '|', 1) || ', ' || split_part(grouped.queue_labels[3], '|', 1)
        ELSE split_part(grouped.queue_labels[1], '|', 1) || ', ' || split_part(grouped.queue_labels[2], '|', 1) || ', ' || split_part(grouped.queue_labels[3], '|', 1) || ' +' || (grouped.queue_count - 3)::text || ' more'
      END AS queue_preview
  ) queue_meta
),
batch_events AS (
  SELECT
    'batch:' || grouped.batch_id::text AS event_id,
    'vaccination_drive'::text AS event_type,
    'pc'::text AS owner_key,
    CASE
      WHEN grouped.park_id IS NOT NULL THEN 'Park vaccination drive'
      ELSE 'Vaccination drive'
    END AS title,
    'Scheduled drive'::text AS subtitle,
    CASE grouped.batch_status
      WHEN 'planned' THEN 'scheduled'
      WHEN 'superseded' THEN 'canceled'
      ELSE grouped.batch_status
    END AS status,
    CASE
      WHEN grouped.batch_status <> 'completed'
        AND (grouped.due_at AT TIME ZONE 'Asia/Kolkata')::date < (now() AT TIME ZONE 'Asia/Kolkata')::date THEN 'warning'
      ELSE 'info'
    END AS severity,
    rollover.rolled_due_at AS due_at,
    rollover.rolled_due_at AS window_start,
    grouped.window_end + (rollover.rolled_due_at - grouped.due_at) AS window_end,
    'Asia/Kolkata'::text AS timezone,
    'india_only'::text AS timezone_source,
    grouped.park_id,
    grouped.park_code,
    CASE WHEN grouped.shed_count = 1 THEN grouped.shed_id ELSE NULL::uuid END AS shed_id,
    CASE WHEN grouped.shed_count = 1 THEN grouped.shed_name ELSE NULL::text END AS shed_name,
    NULL::uuid AS cohort_id,
    NULL::text AS cohort_name,
    CASE
      WHEN grouped.park_id IS NULL THEN 'tenant'::text
      WHEN grouped.batch_scope_type = 'park' OR grouped.shed_count <> 1 THEN 'park'::text
      ELSE 'shed'::text
    END AS target_type,
    grouped.target_count,
    grouped.protocol_id,
    grouped.protocol_version_id,
    NULL::uuid AS rule_id,
    queue_meta.queue_summary AS vaccine_name,
    queue_meta.queue_preview AS dose_code,
    true AS source_backed,
    queue_meta.queue_summary AS source_label,
    'batch'::text AS source_target_type,
    grouped.batch_id AS source_target_id,
    COALESCE(NULLIF(grouped.operator_names, ''), 'PC drive team')::text AS assignee_label,
    'pc_vaccinator'::text AS executor_role,
    'PC verifier'::text AS verifier_label,
    'not_scheduled'::text AS reminder_state,
    'local-stub'::text AS primary_notification_channel,
    'none'::text AS escalation_state,
    false AS system,
    false AS cross_cutting,
    jsonb_build_object(
      'vaccination', '/vaccination/operations',
      'drive', CASE WHEN grouped.shed_count = 1 AND grouped.shed_id IS NOT NULL THEN '/vaccination/execution/sheds/' || grouped.shed_id::text ELSE NULL END
    ) AS links,
    jsonb_build_object(
      'summary', jsonb_build_object(
        'owner', 'PC',
        'target_count', grouped.target_count,
        'queue_count', grouped.queue_count,
        'queue_preview', queue_meta.queue_preview,
        'shed_count', grouped.shed_count,
        'shed_labels', to_jsonb(grouped.shed_labels),
        'vaccine_labels', to_jsonb(grouped.vaccine_labels)
      ),
      'source_and_rule', jsonb_build_object(
        'protocol_version_id', grouped.protocol_version_id,
        'queue_count', grouped.queue_count,
        'queue_preview', queue_meta.queue_preview
      ),
      'execution', jsonb_build_object('batch_id', grouped.batch_id, 'sop_task_id', grouped.sop_task_id, 'work_state', grouped.batch_status),
      'stock', jsonb_build_object('reserved_qty', grouped.reserved_quantity, 'planned_qty', grouped.planned_quantity),
      'proof', jsonb_build_object(),
      'verification', jsonb_build_object('verifier', 'PC verifier'),
      'notification_channels', jsonb_build_array('local-stub'),
      'notification_policy', jsonb_build_object('nudge_allowed', true),
      'links', jsonb_build_object()
    ) AS detail
  FROM (
    SELECT
      ob.batch_id,
      ob.status AS batch_status,
      ob.scope_type AS batch_scope_type,
      COALESCE((assignment_scope.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), ob.window_start, ob.window_end) AS due_at,
      COALESCE((assignment_scope.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), ob.window_start, ob.window_end) AS window_start,
      CASE
        WHEN assignment_scope.planned_date IS NOT NULL THEN (assignment_scope.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') + interval '8 hours'
        WHEN ob.planned_date IS NOT NULL THEN (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') + interval '8 hours'
        ELSE COALESCE(ob.window_end, ob.window_start + interval '8 hours')
      END AS window_end,
      COALESCE(
        goat_park.location_id,
        CASE WHEN scope_loc.location_type = 'park' THEN scope_loc.location_id END,
        CASE WHEN scope_loc.location_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id END
      ) AS park_id,
      COALESCE(
        goat_park.location_code,
        CASE WHEN scope_loc.location_type = 'park' THEN scope_loc.location_code END,
        CASE WHEN scope_loc.location_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_code END
      ) AS park_code,
      NULLIF(
        min(COALESCE(
          goat_shed.location_id::text,
          CASE WHEN scope_loc.location_type = 'shed' THEN scope_loc.location_id::text END
        )) FILTER (
          WHERE COALESCE(
            goat_shed.location_id::text,
            CASE WHEN scope_loc.location_type = 'shed' THEN scope_loc.location_id::text END
          ) IS NOT NULL
        ),
        ''
      )::uuid AS shed_id,
      min(COALESCE(
        goat_shed.name,
        CASE WHEN scope_loc.location_type = 'shed' THEN scope_loc.name END
      )) FILTER (
        WHERE COALESCE(
          goat_shed.name,
          CASE WHEN scope_loc.location_type = 'shed' THEN scope_loc.name END
        ) IS NOT NULL
      ) AS shed_name,
      count(DISTINCT COALESCE(
        goat_shed.location_id,
        CASE WHEN scope_loc.location_type = 'shed' THEN scope_loc.location_id END
      ))::int AS shed_count,
      -- shed_labels now includes partition information per shed (when all animals in the shed share
      -- the same real partition, the partition is included; otherwise bare shed name only).
      -- This uses the same partition-detection logic as obligation_drive_shed_animals (see the
      -- "DEFECT-1 fix" CTE around line 1187-1244): count distinct partitions per shed to determine
      -- if a single partition can be attributed to the whole shed. The subquery groups by shed_id
      -- to resolve the partition per shed before aggregating back to the batch level.
      (SELECT coalesce(jsonb_agg(jsonb_build_object(
        'shed_name', per_shed.shed_name,
        'partition_label', per_shed.single_partition_label
      ) ORDER BY per_shed.shed_name), '[]'::jsonb)
      FROM (
        SELECT
          COALESCE(goat_shed2.location_id, (CASE WHEN scope_loc2.location_type = 'shed' THEN scope_loc2.location_id END))::uuid AS shed_id,
          COALESCE(goat_shed2.name, CASE WHEN scope_loc2.location_type = 'shed' THEN scope_loc2.name END) AS shed_name,
          CASE
            WHEN count(DISTINCT gsp2.partition_label) FILTER (WHERE gsp2.partition_label IS NOT NULL AND gsp2.partition_label <> 'whole') = 1
              THEN min(gsp2.partition_label) FILTER (WHERE gsp2.partition_label IS NOT NULL AND gsp2.partition_label <> 'whole')
            ELSE NULL
          END AS single_partition_label
        FROM obligation_instances oi2
        LEFT JOIN goats g2
          ON g2.tenant_id = oi2.tenant_id
         AND oi2.target_type = 'goat'
         AND g2.goat_id = oi2.target_id
         AND g2.merged_into_goat_id IS NULL
        LEFT JOIN locations goat_shed2
          ON goat_shed2.tenant_id = oi2.tenant_id
         AND goat_shed2.location_id = g2.shed_id
         AND goat_shed2.location_type = 'shed'
        LEFT JOIN goat_shed_partitions gsp2
          ON gsp2.tenant_id = oi2.tenant_id
         AND gsp2.goat_id = oi2.target_id
         AND gsp2.shed_id = g2.shed_id
        LEFT JOIN locations scope_loc2
          ON scope_loc2.tenant_id = ob.tenant_id AND scope_loc2.location_id = ob.scope_id
        WHERE oi2.tenant_id = ob.tenant_id AND oi2.batch_id = ob.batch_id
        GROUP BY
          COALESCE(goat_shed2.location_id, (CASE WHEN scope_loc2.location_type = 'shed' THEN scope_loc2.location_id END)),
          COALESCE(goat_shed2.name, CASE WHEN scope_loc2.location_type = 'shed' THEN scope_loc2.name END)
      ) per_shed
      WHERE per_shed.shed_name IS NOT NULL
      ) AS shed_labels,
      -- Cross-surface parity: count DISTINCT animals (like the single-shed drive path at
      -- count(DISTINCT oi.target_id) above and the operator drive schedule), NOT
      -- ob.estimated_targets. estimated_targets is an obligation-ROW estimate (e.g. 200 animals x
      -- 2 dose rows = 400), so rendering it as the drive's "doses" over-counted the calendar vs the
      -- operator schedule (see docs/decisions/scale-anti-patterns.md -> cross-surface count parity).
      GREATEST(count(DISTINCT oi.target_id), 1)::int AS target_count,
      pd.protocol_id,
      pv.protocol_version_id,
      ob.sop_task_id,
      ob.reserved_quantity,
      ob.planned_quantity,
      max(assignment_scope.operator_names) AS operator_names,
      count(DISTINCT pr.rule_id)::int AS queue_count,
      array_agg(
        DISTINCT COALESCE(NULLIF(pr.dose_code, ''), pd.name) || '|' || pr.rule_id::text
        ORDER BY COALESCE(NULLIF(pr.dose_code, ''), pd.name) || '|' || pr.rule_id::text
      ) AS queue_labels,
      array_agg(
        DISTINCT COALESCE(NULLIF(prd.vaccine_json->>'name', ''), pd.name)
        ORDER BY COALESCE(NULLIF(prd.vaccine_json->>'name', ''), pd.name)
      ) AS vaccine_labels
    FROM obligation_batches ob
    JOIN obligation_instances oi
      ON oi.tenant_id = ob.tenant_id AND oi.batch_id = ob.batch_id
    JOIN protocol_versions pv
      ON pv.tenant_id = ob.tenant_id AND pv.protocol_version_id = ob.protocol_version_id
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
    JOIN protocol_rules pr
      ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
    LEFT JOIN goats g
      ON g.tenant_id = oi.tenant_id
     AND oi.target_type = 'goat'
     AND g.goat_id = oi.target_id
     AND g.merged_into_goat_id IS NULL
    LEFT JOIN locations goat_shed
      ON goat_shed.tenant_id = oi.tenant_id
     AND goat_shed.location_id = g.shed_id
     AND goat_shed.location_type = 'shed'
    LEFT JOIN locations goat_park
      ON goat_park.tenant_id = oi.tenant_id
     AND goat_park.location_id = COALESCE(g.park_id, goat_shed.parent_location_id)
     AND goat_park.location_type = 'park'
    LEFT JOIN protocol_rule_dimensions prd
      ON prd.tenant_id = pr.tenant_id AND prd.rule_id = pr.rule_id
    LEFT JOIN locations scope_loc
      ON scope_loc.tenant_id = ob.tenant_id AND scope_loc.location_id = ob.scope_id
    LEFT JOIN locations scope_parent
      ON scope_parent.tenant_id = ob.tenant_id AND scope_parent.location_id = scope_loc.parent_location_id
    LEFT JOIN LATERAL (
      SELECT count(*) > 0 AS has_any_assignment
      FROM vaccination_drive_assignments vda
      WHERE vda.tenant_id = ob.tenant_id
        AND vda.batch_id = ob.batch_id
    ) assignment_presence ON true
    LEFT JOIN LATERAL (
      SELECT
        vda.planned_date,
        array_remove(array_agg(DISTINCT assignment_rule.rule_id), NULL)::uuid[] AS rule_ids,
        string_agg(DISTINCT NULLIF(wm.display_name, ''), ', ') AS operator_names
      FROM vaccination_drive_assignments vda
      LEFT JOIN LATERAL unnest(vda.vaccine_rule_ids) AS assignment_rule(rule_id) ON true
      LEFT JOIN workforce_members wm
        ON wm.workforce_member_id = vda.operator_id
      WHERE vda.tenant_id = ob.tenant_id
        AND vda.batch_id = ob.batch_id
        AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') < $3::timestamptz
        -- P1 drive rollover: widen inclusion to a 45-day lookback ONLY for 'in_progress' batches
        -- (gated identically to the display-date rollover a few hundred lines below) -- these are
        -- the "keeps showing on the CURRENT date until CLOSED" drives. A merely 'planned' batch is
        -- NOT widened here: it already surfaces via the pre-existing catch-up/overdue path on its
        -- own date, and widening it too would return it (still labeled with its ORIGINAL due_at,
        -- since it never gets the display rollover) into unrelated future query windows --
        -- "returned but not surfaced on D+1", the exact defect this fix targets. The lookback is
        -- ADDITIONALLY gated on calendarTodayInRequestedWindow: the roll only produces a card dated
        -- TODAY, so a window that does not contain today has no business being handed one. Without
        -- that gate every future window inherited the rolled card (tomorrow showed today's drives).
        AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') + interval '1 day' > $2::timestamptz - CASE WHEN ob.status = 'in_progress' AND ` + calendarTodayInRequestedWindow + ` THEN interval '45 days' ELSE interval '0 days' END
      GROUP BY vda.planned_date
    ) assignment_scope ON true
    WHERE ob.tenant_id = $1::uuid
      AND ob.scope_type IN ('tenant', 'shed', 'park')
      AND (
        assignment_scope.planned_date IS NOT NULL
        OR (
          NOT COALESCE(assignment_presence.has_any_assignment, false)
          AND
          ob.planned_date IS NOT NULL
          AND (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') < $3::timestamptz
          AND (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') + interval '1 day' > $2::timestamptz - CASE WHEN ob.status = 'in_progress' AND ` + calendarTodayInRequestedWindow + ` THEN interval '45 days' ELSE interval '0 days' END
        )
        OR (
          NOT COALESCE(assignment_presence.has_any_assignment, false)
          AND
          ob.planned_date IS NULL
          AND COALESCE(ob.window_start, ob.window_end) >= $2::timestamptz
          AND COALESCE(ob.window_start, ob.window_end) < $3::timestamptz
        )
      )
      AND pd.category = 'vaccination'
      AND pv.status = 'published'
      AND ob.status NOT IN ('superseded', 'canceled')
      AND (
        assignment_scope.planned_date IS NULL
        OR cardinality(assignment_scope.rule_ids) = 0
        OR oi.rule_id = ANY(assignment_scope.rule_ids)
      )
    GROUP BY
      ob.batch_id,
      ob.status,
      ob.scope_type,
      COALESCE((assignment_scope.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), ob.window_start, ob.window_end),
      CASE
        WHEN assignment_scope.planned_date IS NOT NULL THEN (assignment_scope.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') + interval '8 hours'
        WHEN ob.planned_date IS NOT NULL THEN (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') + interval '8 hours'
        ELSE COALESCE(ob.window_end, ob.window_start + interval '8 hours')
      END,
      CASE WHEN scope_loc.location_type = 'park' THEN scope_loc.location_id END,
      CASE WHEN scope_loc.location_type = 'park' THEN scope_loc.location_code END,
      CASE WHEN scope_loc.location_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id END,
      CASE WHEN scope_loc.location_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_code END,
      pd.protocol_id,
      pv.protocol_version_id,
      ob.sop_task_id,
      ob.reserved_quantity,
      ob.planned_quantity,
      COALESCE(
        goat_park.location_id,
        CASE WHEN scope_loc.location_type = 'park' THEN scope_loc.location_id END,
        CASE WHEN scope_loc.location_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id END
      ),
      COALESCE(
        goat_park.location_code,
        CASE WHEN scope_loc.location_type = 'park' THEN scope_loc.location_code END,
        CASE WHEN scope_loc.location_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_code END
      )
  ) grouped
  -- P1 drive rollover: maintainer contract is "a vaccination drive keeps showing on the CURRENT
  -- date until it is CLOSED" (status reaches 'completed'/'canceled'). The scheduled planned_date
  -- never moves in the data; only the CARD's display date (due_at/window_start/window_end, the
  -- columns the API and frontend group calendar days by) rolls forward daily to the current
  -- Asia/Kolkata business date while the drive is still open. severity/status above intentionally
  -- keep reading grouped.due_at (the ORIGINAL scheduled date) so a rolled-forward-but-still-open
  -- drive still reads 'warning' for being late, instead of laundering itself back to 'info' by
  -- rolling onto today.
  CROSS JOIN LATERAL (
    SELECT
      CASE
        -- Only roll drives where WORK HAS ACTUALLY STARTED: batch status = 'in_progress' means
        -- the operator has begun/submitted the drive but the verifier/director has not yet
        -- closed it. This is a SINGLE stored column, checked identically here and in
        -- obligation_drive_membership below, so batch_events (one row per batch) and the
        -- obligation-grain membership CTE can never disagree about which date a batch's
        -- obligations belong under -- avoiding a split-brain where some of a batch's obligations
        -- roll forward and others are left orphaned on the original date. A merely 'planned'
        -- drive that nobody has touched (or one still 'planned' despite an individual obligation
        -- reading 'missed') stays on its own scheduled date -- it already surfaces as
        -- 'overdue'/'missed' via the pre-existing catch-up path, and MANY fixtures across this
        -- package seed a bare untouched/never-'in_progress' batch on a fixed historical date and
        -- assert it is found there; rolling those forward too would silently vanish them from
        -- their seeded date on every test run after that date passes.
        WHEN grouped.batch_status = 'in_progress'
         AND (grouped.due_at AT TIME ZONE 'Asia/Kolkata')::date < (now() AT TIME ZONE 'Asia/Kolkata')::date
        THEN (now() AT TIME ZONE 'Asia/Kolkata')::date::timestamp AT TIME ZONE 'Asia/Kolkata'
        ELSE grouped.due_at
      END AS rolled_due_at
  ) rollover
  CROSS JOIN LATERAL (
    SELECT
      CASE
        WHEN grouped.queue_count <= 0 THEN 'Vaccination queue'
        WHEN grouped.queue_count = 1 THEN '1 vaccine queue'
        ELSE grouped.queue_count::text || ' vaccine queues'
      END AS queue_summary,
      CASE
        WHEN grouped.queue_count <= 0 THEN 'Vaccination queue'
        WHEN grouped.queue_count = 1 THEN split_part(grouped.queue_labels[1], '|', 1)
        WHEN grouped.queue_count = 2 THEN split_part(grouped.queue_labels[1], '|', 1) || ', ' || split_part(grouped.queue_labels[2], '|', 1)
        WHEN grouped.queue_count = 3 THEN split_part(grouped.queue_labels[1], '|', 1) || ', ' || split_part(grouped.queue_labels[2], '|', 1) || ', ' || split_part(grouped.queue_labels[3], '|', 1)
        ELSE split_part(grouped.queue_labels[1], '|', 1) || ', ' || split_part(grouped.queue_labels[2], '|', 1) || ', ' || split_part(grouped.queue_labels[3], '|', 1) || ' +' || (grouped.queue_count - 3)::text || ' more'
      END AS queue_preview
  ) queue_meta
),
drive_sources AS (
  SELECT * FROM batch_events
  UNION ALL
  SELECT * FROM catchup_drive_events
),
-- projection-review: membership=drive_sources (batch_events plus catchup_drive_events, one source event row per stable event_id); group_key=(park_id, due_day) where due_day is derived from the source event due_at in Asia/Kolkata and batched sources already prefer planned_date before window_start; join_cardinality=source rows are UNION ALL event facts, grouped once by park/day with count(DISTINCT shed_id) only for shed metadata and scheduled_count filtered to active scheduled/review batch statuses so completed/canceled batches cannot create scheduled work; pagination=calendar park-drive grouping is computed inside the bounded canonical list request before the event page is emitted, while month markers use their own whole-month aggregate; scope=park_id from the event source, shed only remains a display dimension and never narrows park-drive membership
park_drive_groups AS (
  SELECT
    park_id,
    max(park_code) AS park_code,
    to_char((due_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD') AS due_day,
    min(due_at) AS first_due_at,
    min(window_start) AS window_start,
    max(window_end) AS window_end,
    sum(target_count)::int AS target_count,
    count(*)::int AS drive_count,
    sum(target_count) FILTER (WHERE source_target_type = 'catchup')::int AS catch_up_count,
    sum(target_count) FILTER (
      WHERE source_target_type = 'batch'
        AND status IN ('scheduled', 'due', 'overdue', 'in_progress', 'proof_pending', 'verification_pending', 'rejected', 'rework_due')
    )::int AS scheduled_count,
    sum(COALESCE((detail->'summary'->>'queue_count')::int, 0))::int AS queue_count,
    bool_or(source_target_type = 'catchup') AS has_catch_up,
    sum(target_count) FILTER (WHERE status = 'deferred')::int AS deferred_count,
    min(source_target_type) AS single_source_target_type,
    min(source_target_id::text)::uuid AS single_source_target_id,
    bool_or(status = 'completed') AND bool_and(status IN ('completed', 'canceled')) AS all_completed,
    bool_or(status = 'missed') AS has_missed,
    bool_or(status = 'in_progress') AS has_in_progress,
    bool_or(status = 'deferred') AS has_deferred,
    bool_or(status IN ('proof_pending', 'verification_pending', 'rejected', 'rework_due')) AS has_review,
    bool_or(
      status IN ('scheduled', 'due', 'overdue')
      AND (due_at AT TIME ZONE 'Asia/Kolkata')::date < (now() AT TIME ZONE 'Asia/Kolkata')::date
    ) AS has_overdue,
    jsonb_agg(event_id ORDER BY event_id) AS source_event_ids,
    string_agg(DISTINCT NULLIF(assignee_label, 'PC drive team'), ', ') AS operator_names,
    count(DISTINCT shed_id) FILTER (WHERE shed_id IS NOT NULL)::int AS shed_count,
    NULLIF(min(shed_id::text) FILTER (WHERE shed_id IS NOT NULL), '')::uuid AS primary_shed_id,
    min(shed_name) FILTER (WHERE shed_name IS NOT NULL) AS primary_shed_name
  FROM drive_sources
  GROUP BY park_id, to_char((due_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD')
),
obligation_drive_membership AS (
  WITH obligation_membership_rows AS (
    -- Index-bound decomposition of the former batched-OR-unbatched-3-way-OR membership predicate
    -- (migration 000190). Four index-scannable branches selecting FULL obligation_instances rows (no
    -- join-back to the base table), deduped by UNION so an obligation matching two branches (e.g.
    -- in-window AND overdue) is counted EXACTLY ONCE -- preserving the exact total_count /
    -- completed_count / total_animals / completed_animals drive-summary invariants.
    -- Unbatched window branch keeps 'completed' (membership needs it for the completed aggregates),
    -- hence idx_obligation_instances_calendar_window excludes only waived/canceled/superseded.
    -- Unbatched window (index: idx_obligation_instances_calendar_window)
    SELECT oi0.*, NULL::timestamptz AS membership_at_override
    FROM obligation_instances oi0
    WHERE tenant_id = $1::uuid AND batch_id IS NULL
      AND status NOT IN ('superseded', 'canceled', 'waived')
      AND due_at >= $2::timestamptz AND due_at < $3::timestamptz
    UNION
    -- Unbatched bounded exception catch-up (index: idx_obligation_instances_calendar_exceptions_due)
    SELECT oi0.*, NULL::timestamptz AS membership_at_override
    FROM obligation_instances oi0
    WHERE tenant_id = $1::uuid AND batch_id IS NULL
      AND status IN ('missed', 'in_progress', 'deferred')
      AND due_at >= $2::timestamptz - interval '45 days'
      AND due_at < $3::timestamptz
    UNION
    -- Unbatched overdue-by-due_at (index: idx_obligation_instances_calendar_overdue)
    SELECT oi0.*, NULL::timestamptz AS membership_at_override
    FROM obligation_instances oi0
    WHERE tenant_id = $1::uuid AND batch_id IS NULL
      AND status IN ('scheduled', 'due') AND due_at < now()
      AND due_at >= $2::timestamptz - interval '45 days'
      AND due_at < $3::timestamptz
    UNION
    -- Operator-planned vaccination drives. Exact assignment membership is the source of truth for
    -- the execution day after operator-cap planning or admin date moves; do not join every
    -- obligation in a split batch to every assignment date for the same vaccine rule.
    SELECT oi2.*, (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS membership_at_override
    FROM obligation_instances oi2
    JOIN obligation_batches ob2
      ON ob2.tenant_id = oi2.tenant_id AND ob2.batch_id = oi2.batch_id
    JOIN vaccination_drive_assignment_members vdam
      ON vdam.tenant_id = oi2.tenant_id
     AND vdam.obligation_id = oi2.obligation_id
    JOIN vaccination_drive_assignments vda
      ON vda.tenant_id = vdam.tenant_id
     AND vda.assignment_id = vdam.assignment_id
     AND vda.batch_id = oi2.batch_id
     AND (
       cardinality(vda.vaccine_rule_ids) = 0
       OR oi2.rule_id = ANY(vda.vaccine_rule_ids)
     )
    WHERE oi2.tenant_id = $1::uuid AND oi2.batch_id IS NOT NULL
      AND oi2.status NOT IN ('superseded', 'canceled', 'waived')
      AND ob2.status NOT IN ('superseded', 'canceled')
      AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') < $3::timestamptz
      AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') + interval '1 day' > $2::timestamptz - CASE WHEN ob2.status = 'in_progress' AND ` + calendarTodayInRequestedWindow + ` THEN interval '45 days' ELSE interval '0 days' END
    UNION
    -- Assignment-era compatibility for completed or otherwise still-unbound batch obligations. Some
    -- historical split batches have assignment rows for the day-level cards but no member rows for
    -- already-completed animals. Keep those rows on their own obligation business day instead of
    -- fanning them out across every assignment date in the batch.
    SELECT oi2.*, oi2.due_at AS membership_at_override
    FROM obligation_instances oi2
    JOIN obligation_batches ob2
      ON ob2.tenant_id = oi2.tenant_id AND ob2.batch_id = oi2.batch_id
    WHERE oi2.tenant_id = $1::uuid AND oi2.batch_id IS NOT NULL
      AND oi2.status NOT IN ('superseded', 'canceled', 'waived')
      AND ob2.status NOT IN ('superseded', 'canceled')
      AND oi2.due_at >= $2::timestamptz
      AND oi2.due_at < $3::timestamptz
      AND EXISTS (
        SELECT 1
        FROM vaccination_drive_assignments vda
        WHERE vda.tenant_id = oi2.tenant_id
          AND vda.batch_id = oi2.batch_id
      )
      AND NOT EXISTS (
        SELECT 1
        FROM vaccination_drive_assignment_members vdam
        WHERE vdam.tenant_id = oi2.tenant_id
          AND vdam.obligation_id = oi2.obligation_id
      )
    UNION
    -- Batched drives in-window (obligation_batches window joined to obligation_instances via
    -- obligation_instances_batch_idx). obligation_batches stays a cheap scan (low cardinality).
    SELECT oi2.*, NULL::timestamptz AS membership_at_override FROM obligation_instances oi2
    JOIN obligation_batches ob2
      ON ob2.tenant_id = oi2.tenant_id AND ob2.batch_id = oi2.batch_id
    LEFT JOIN LATERAL (
      SELECT count(*) > 0 AS has_assignment
      FROM vaccination_drive_assignments vda
      WHERE vda.tenant_id = oi2.tenant_id
        AND vda.batch_id = oi2.batch_id
    ) assignment_presence ON true
    WHERE oi2.tenant_id = $1::uuid AND oi2.batch_id IS NOT NULL
      AND oi2.status NOT IN ('superseded', 'canceled', 'waived')
      AND ob2.status NOT IN ('superseded', 'canceled')
      AND NOT COALESCE(assignment_presence.has_assignment, false)
      AND (
        (
          ob2.planned_date IS NOT NULL
          AND (ob2.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') < $3::timestamptz
          AND (ob2.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') + interval '1 day' > $2::timestamptz - CASE WHEN ob2.status = 'in_progress' AND ` + calendarTodayInRequestedWindow + ` THEN interval '45 days' ELSE interval '0 days' END -- scale-guard:ignore: 5k-50k-envelope; concatenating calendarTodayInRequestedWindow splits calendarCanonicalEventsCTE into several Go literals, so god-cte/non-sargable-like re-anchor from the const's annotated declaration to this fragment's first line. Same query, same accepted debt; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
        )
        OR (
          ob2.planned_date IS NULL
          AND COALESCE(ob2.window_start, ob2.window_end) >= $2::timestamptz
          AND COALESCE(ob2.window_start, ob2.window_end) < $3::timestamptz
        )
      )
  )
  SELECT
    oi.obligation_id,
    oi.status,
    oi.rule_id,
    oi.batch_id,
    oi.protocol_version_id,
    COALESCE(
      (ob.window_start AT TIME ZONE 'Asia/Kolkata')::date,
      (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date
    ) AS logical_window_start,
    COALESCE(
      (ob.window_end AT TIME ZONE 'Asia/Kolkata')::date,
      (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date
    ) AS logical_window_end,
    COALESCE(
      NULLIF(pr.eligibility_json->'vaccine'->>'display_name', ''),
      NULLIF(pr.eligibility_json->'vaccine'->>'name', ''),
      NULLIF(pr.eligibility_json->'vaccine'->>'code', ''),
      NULLIF(pr.dose_code, ''),
      pd.name
    ) AS logical_vaccine_label,
    (lower(pr.dose_code) LIKE '%adult%' OR lower(pr.dose_code) LIKE '%revac%') AS adult_drive,
    pd.name AS protocol_name,
    loc.park_id,
    loc.park_code,
    loc.shed_id,
    (member.membership_at AT TIME ZONE 'Asia/Kolkata')::date AS due_date,
    -- The ORIGINAL scheduled business date, BEFORE the "keeps showing on the current date until
    -- CLOSED" rollover below rewrites membership_at to today. Grouping and display must use the
    -- rolled due_date, but LATENESS must not: comparing a rolled date against today is always
    -- false, which laundered a drive with zero completions from 'overdue' back to 'in_progress'.
    -- genuine_overdue/genuine_missed read THIS column, so a rolled-forward drive still reports
    -- that it is late.
    (member_raw.membership_at_raw AT TIME ZONE 'Asia/Kolkata')::date AS original_due_date,
    CASE WHEN oi.target_type = 'goat' THEN oi.target_id END AS animal_id,
    EXISTS (
      SELECT 1
      FROM vaccination_completions vc
      WHERE vc.tenant_id = oi.tenant_id
        AND vc.obligation_id = oi.obligation_id
        AND vc.status = 'recorded'
    ) AS submitted_for_verification,
    -- CURRENTLY rejected, not EVER rejected. A verifier's rejection moves the completion out of
    -- vaccination_completions into the archive table (migration 000093); if the animal is then
    -- rescanned and accepted, a NEW live vaccination_completions row is written for the SAME
    -- obligation_id while the old archive row is left in place as history. Reading the archive
    -- alone would keep counting that obligation as rejected forever, exactly the reviewCount bug
    -- already fixed for the scan roster (vaccinationexecution/adapters/postgres/repository.go
    -- scanRosterSQL "vc" lateral): a live recorded/accepted completion for this obligation
    -- outranks an older rejection archive row, so the flag clears itself the moment the animal is
    -- redone and accepted. Collapsed through the same live_rank/updated_at/completion_id
    -- tie-break as scanRosterSQL so the two reads can never disagree about which verdict is
    -- current for a given obligation.
    COALESCE((
      SELECT verdicts.completion_status = 'rejected'
      FROM (
        SELECT vcc.status AS completion_status, vcc.updated_at, vcc.completion_id,
               CASE WHEN vcc.status IN ('recorded', 'accepted') THEN 0 ELSE 1 END AS live_rank
        FROM vaccination_completions vcc
        WHERE vcc.tenant_id = oi.tenant_id
          AND vcc.obligation_id = oi.obligation_id
        UNION ALL
        SELECT 'rejected', vcr.rejected_at, vcr.completion_id, 1
        FROM vaccination_completion_rejections vcr
        WHERE vcr.tenant_id = oi.tenant_id
          AND vcr.obligation_id = oi.obligation_id
      ) verdicts
      ORDER BY verdicts.live_rank ASC, verdicts.updated_at DESC, verdicts.completion_id DESC
      FETCH FIRST 1 ROW ONLY
    ), false) AS currently_rejected
    -- Stock/execution "blocked" visibility is deliberately out of scope -- stock is not a built
    -- product feature yet (owner decision 2026-07-14); revisit when the stock module ships.
  FROM obligation_membership_rows oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id AND ob.batch_id = oi.batch_id
  LEFT JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND oi.target_type = 'goat'
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
  CROSS JOIN LATERAL (
    SELECT
      CASE
        WHEN oi.membership_at_override IS NOT NULL THEN oi.membership_at_override
        WHEN oi.batch_id IS NOT NULL THEN COALESCE((ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), ob.window_start, ob.window_end)
        ELSE oi.due_at
      END AS membership_at_raw
  ) member_raw
  -- P1 drive rollover: keep this due_date in lockstep with batch_events.due_at (the SAME "keeps
  -- showing on the CURRENT date until CLOSED" rollover) so obligation_drive_effective_state and
  -- obl_summary, which join on (park_id, due_date) below, land on the SAME rolled-forward date
  -- park_drive_groups now groups the drive event under -- otherwise the headline/status/count
  -- join would miss and silently fall back to the COALESCE(..., false) zero-obligation defaults.
  -- Only batch-backed rows (oi.batch_id IS NOT NULL) roll; standalone (non-batch) obligations keep
  -- their own due_at/override semantics unchanged.
  CROSS JOIN LATERAL (
    SELECT
      CASE WHEN oi.batch_id IS NOT NULL THEN ob.scope_type ELSE oi.scope_type END AS scope_type,
      CASE WHEN oi.batch_id IS NOT NULL THEN ob.scope_id ELSE oi.scope_id END AS scope_id,
      CASE
        -- Same 'in_progress'-only gate as batch_events.rolled_due_at above, keyed off the SAME
        -- ob.status column (not a per-obligation signal) so this CTE's due_date can never diverge
        -- from the rolled date park_drive_groups groups the event under -- every obligation in
        -- an 'in_progress' batch rolls together, so none is left orphaned on the original date.
        WHEN oi.batch_id IS NOT NULL
         AND ob.status = 'in_progress'
         AND (member_raw.membership_at_raw AT TIME ZONE 'Asia/Kolkata')::date < (now() AT TIME ZONE 'Asia/Kolkata')::date
        THEN (now() AT TIME ZONE 'Asia/Kolkata')::date::timestamp AT TIME ZONE 'Asia/Kolkata'
        ELSE member_raw.membership_at_raw
      END AS membership_at
  ) member
  LEFT JOIN locations scope_loc
    ON scope_loc.tenant_id = oi.tenant_id
   AND scope_loc.location_id = member.scope_id
   AND member.scope_type IN ('park', 'shed', 'cohort')
  LEFT JOIN locations scope_parent
    ON scope_parent.tenant_id = oi.tenant_id
   AND scope_parent.location_id = scope_loc.parent_location_id
  LEFT JOIN locations scope_grand
    ON scope_grand.tenant_id = oi.tenant_id
   AND scope_grand.location_id = scope_parent.parent_location_id
  LEFT JOIN locations goat_shed
    ON goat_shed.tenant_id = oi.tenant_id
   AND goat_shed.location_id = g.shed_id
   AND goat_shed.location_type = 'shed'
  LEFT JOIN locations goat_park
    ON goat_park.tenant_id = oi.tenant_id
   AND goat_park.location_id = COALESCE(g.park_id, goat_shed.parent_location_id)
   AND goat_park.location_type = 'park'
  LEFT JOIN LATERAL (
    SELECT
      COALESCE(
        goat_park.location_id,
        CASE
        WHEN member.scope_type = 'park' THEN scope_loc.location_id
        WHEN member.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id
        WHEN member.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_id
        END
      ) AS park_id,
      COALESCE(
        goat_park.location_code,
        CASE
        WHEN member.scope_type = 'park' THEN scope_loc.location_code
        WHEN member.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_code
        WHEN member.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_code
        END
      ) AS park_code,
      COALESCE(
        goat_shed.location_id,
        CASE
        WHEN member.scope_type = 'shed' THEN scope_loc.location_id
        WHEN member.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.location_id
        END
      ) AS shed_id
  ) loc ON true
  WHERE pd.category = 'vaccination'
    AND pv.status = 'published'
    AND oi.status NOT IN ('superseded', 'canceled', 'waived')
),
-- A logical vaccination drive may span multiple operator-days. Calendar rows stay at the
-- executable park/day grain, while this bounded seed-and-expand rollup gives each row the same
-- backend-owned drive name and DISTINCT-animal total. The key is the persisted batch window,
-- protocol and vaccine identity; verifier/director workflow timestamps never participate.
obligation_logical_drive_keys AS (
  SELECT DISTINCT
    m.park_id,
    m.park_code,
    m.due_date AS execution_date,
    m.protocol_version_id,
    m.logical_window_start,
    m.logical_window_end,
    m.logical_vaccine_label,
    m.adult_drive
  FROM obligation_drive_membership m
  WHERE m.batch_id IS NOT NULL
    AND m.park_id IS NOT NULL
    AND m.logical_vaccine_label IS NOT NULL
),
obligation_logical_drive_full_membership AS (
  SELECT
    k.park_id,
    k.park_code,
    k.execution_date,
    k.logical_window_start,
    k.logical_vaccine_label,
    k.adult_drive,
    CASE WHEN all_oi.target_type = 'goat' THEN all_oi.target_id END AS animal_id
  FROM obligation_logical_drive_keys k
  JOIN obligation_batches all_ob
    ON all_ob.tenant_id = $1::uuid
   AND all_ob.protocol_version_id = k.protocol_version_id
   AND COALESCE(
         (all_ob.window_start AT TIME ZONE 'Asia/Kolkata')::date,
         all_ob.planned_date
       ) = k.logical_window_start
   AND COALESCE(
         (all_ob.window_end AT TIME ZONE 'Asia/Kolkata')::date,
         all_ob.planned_date
       ) = k.logical_window_end
   AND all_ob.status NOT IN ('superseded', 'canceled')
  JOIN obligation_instances all_oi
    ON all_oi.tenant_id = all_ob.tenant_id
   AND all_oi.batch_id = all_ob.batch_id
   AND all_oi.status NOT IN ('superseded', 'canceled', 'waived')
  JOIN protocol_rules all_pr
    ON all_pr.tenant_id = all_oi.tenant_id
   AND all_pr.rule_id = all_oi.rule_id
  LEFT JOIN goats all_goat
    ON all_goat.tenant_id = all_oi.tenant_id
   AND all_oi.target_type = 'goat'
   AND all_goat.goat_id = all_oi.target_id
   AND all_goat.merged_into_goat_id IS NULL
  LEFT JOIN locations all_shed
    ON all_shed.tenant_id = all_goat.tenant_id
   AND all_shed.location_id = all_goat.shed_id
   AND all_shed.location_type = 'shed'
  WHERE COALESCE(
          NULLIF(all_pr.eligibility_json->'vaccine'->>'display_name', ''),
          NULLIF(all_pr.eligibility_json->'vaccine'->>'name', ''),
          NULLIF(all_pr.eligibility_json->'vaccine'->>'code', ''),
          NULLIF(all_pr.dose_code, '')
        ) = k.logical_vaccine_label
    AND COALESCE(all_goat.park_id, all_shed.parent_location_id) = k.park_id
),
-- projection-review: membership=obligation_logical_drive_full_membership starts from the bounded executable-day keys, then expands through persisted batch membership to every animal in the same protocol+park+batch-window+vaccine cohort, including operator days outside the requested Calendar page/window; group_key=(park_id, protocol_version_id, logical_window_start, logical_window_end, logical_vaccine_label) identifies one persisted multi-day drive cohort, while execution_date keeps the emitted row at park/day grain; join_cardinality=protocol_rules is one row per rule and drive_total collapses batch members with COUNT(DISTINCT animal_id), with no protocol_rule_dimensions fan-out; pagination=the complete matching cohort is aggregated before the bounded Calendar event page is emitted, so Limit or a single-day request cannot change drive_total; scope=park is resolved explicitly from goat.park_id or its physical-shed parent and matched to the executable key
obligation_logical_drive_rollup AS (
  SELECT
    m.park_id,
    m.execution_date,
    COALESCE(NULLIF(max(m.park_code), ''), 'Park') ||
      CASE WHEN bool_and(m.adult_drive) THEN ' Adult ' ELSE ' ' END ||
      string_agg(DISTINCT m.logical_vaccine_label, ' + ' ORDER BY m.logical_vaccine_label) ||
      ' – ' || to_char(min(m.logical_window_start), 'Mon YYYY') AS drive_name,
    count(DISTINCT m.animal_id)::int AS drive_total
  FROM obligation_logical_drive_full_membership m
  GROUP BY m.park_id, m.execution_date
),
obligation_drive_vaccine_labels AS (
  SELECT
    m.park_id,
    m.due_date,
    array_remove(array_agg(DISTINCT COALESCE(NULLIF(prd.vaccine_json->>'name', ''), m.protocol_name)), NULL)::text[] AS vaccine_labels
  FROM obligation_drive_membership m
  LEFT JOIN protocol_rule_dimensions prd
    ON prd.tenant_id = $1::uuid AND prd.rule_id = m.rule_id
  GROUP BY m.park_id, m.due_date
),
obligation_drive_shed_complete AS (
  SELECT park_id, due_date, count(*)::int AS sheds_completed
  FROM (
    SELECT park_id, due_date, shed_id
    FROM obligation_drive_membership
    WHERE shed_id IS NOT NULL
    GROUP BY park_id, due_date, shed_id
    -- A shed is DONE when the operator has finished every animal in it -- completed OR submitted
    -- for verification. Requiring status='completed' alone meant a shed whose every animal was
    -- vaccinated and whose proof was submitted still read "0 of 4 sheds done" until a verifier
    -- cleared it, which is the same "done means verified" redefinition corrected in
    -- progress_completed below. The outstanding review is carried by the verification-pending
    -- status, not by under-reporting the operator's field work.
    HAVING count(DISTINCT obligation_id) = count(DISTINCT obligation_id) FILTER (
      WHERE status = 'completed' OR submitted_for_verification
    )
  ) done_sheds
  GROUP BY park_id, due_date
),
obligation_drive_animal_coverage AS (
  SELECT park_id, due_date,
         count(*)::int AS total_animals,
         count(*) FILTER (WHERE fully_completed)::int AS completed_animals,
         count(*) FILTER (WHERE submitted_for_verification)::int AS submitted_animals
  FROM (
    -- BOTH flags are ALL-DOSE (bool_and), never any-dose. A goat due two vaccines on
    -- the same drive day is ONE animal that is done only when EVERY one of its drive
    -- obligations is done -- that is the whole point of the distinct-ANIMAL progress
    -- basis below. bool_or(submitted_for_verification) broke exactly that: a goat with
    -- PPR submitted and ET still open counted as a finished animal, so a one-goat drive
    -- reported 1/1, 100% while medicine was still owed. The animal-grain numerator is
    -- completed_animals + submitted_animals, so an any-dose flag there is a silent
    -- over-count of field work, not a display nicety.
    --
    -- The two flags stay DISJOINT (the numerator adds them): fully_completed is the
    -- strict all-'completed' case, and submitted_for_verification is the all-dose
    -- finished case that is NOT fully completed, i.e. at least one dose is sitting in
    -- verification. An animal with any dose neither completed nor submitted falls into
    -- neither bucket and correctly holds the ring below 100%.
    SELECT park_id, due_date, animal_id,
           bool_and(status = 'completed') AS fully_completed,
           bool_and(status = 'completed' OR submitted_for_verification)
             AND NOT bool_and(status = 'completed') AS submitted_for_verification
    FROM obligation_drive_membership
    WHERE animal_id IS NOT NULL
    GROUP BY park_id, due_date, animal_id
  ) per_animal
  GROUP BY park_id, due_date
),
obligation_drive_shed_animals AS (
  -- DEFECT-1 fix (calendar shed-partition awareness, see AGENTS.md operational-location
  -- convention): a drive's per-shed row previously carried only shed_id/shed_name, so a shed
  -- that is actually one partition of a larger physical building rendered as the bare parent
  -- shed name ("Mandela 2") with no way to tell an operator which partition the drive is in.
  --
  -- Rule applied here (per the DEFECT 1 judgement call): partition truth for a goat comes from
  -- goat_shed_partitions (per-animal), never a stored snapshot column. Per shed *within this
  -- drive's animal membership*:
  --   - every animal in the shed shares the SAME real partition   -> compose "Shed - Partition"
  --   - the shed's animals span MORE THAN ONE partition            -> render the bare shed name
  --     (a drive spanning every partition of a shed has no single partition to show; that is
  --     correct, not a bug -- do not invent one)
  --   - no animal in the shed carries a real partition (all 'whole')-> render the bare shed name
  -- Composition uses the canonical oploc.Display() rule in Go (see below); this CTE only
  -- resolves the single-or-none partition label per shed so Go never re-derives it from a raw
  -- per-goat join.
  SELECT
    per_shed.park_id,
    per_shed.due_date,
    jsonb_agg(
      jsonb_build_object(
        'shed_id', per_shed.shed_id::text,
        'shed_name', per_shed.shed_name,
        'total_animals', per_shed.total_animals,
        'partition_label', per_shed.single_partition_label
      )
      ORDER BY per_shed.shed_name, per_shed.shed_id::text
    ) AS sheds
  FROM (
    SELECT
      m.park_id,
      m.due_date,
      m.shed_id,
      COALESCE(NULLIF(l.name, ''), l.location_code, m.shed_id::text) AS shed_name,
      count(DISTINCT m.animal_id) FILTER (WHERE m.animal_id IS NOT NULL)::int AS total_animals,
      -- single_partition_label is non-NULL ONLY when every animal in this shed (within the
      -- drive's membership) resolves to the SAME real (non-'whole') partition. count(DISTINCT
      -- gsp.partition_label) over just the partitioned animals tells us how many distinct real
      -- partitions are represented; > 1 means the shed spans multiple partitions and must stay
      -- bare per the rule above.
      CASE
        WHEN count(DISTINCT gsp.partition_label) FILTER (WHERE gsp.partition_label IS NOT NULL AND gsp.partition_label <> 'whole') = 1
          THEN min(gsp.partition_label) FILTER (WHERE gsp.partition_label IS NOT NULL AND gsp.partition_label <> 'whole')
        ELSE NULL
      END AS single_partition_label
    FROM obligation_drive_membership m
    LEFT JOIN locations l
      ON l.tenant_id = $1::uuid
     AND l.location_id = m.shed_id
    LEFT JOIN goat_shed_partitions gsp
      ON gsp.tenant_id = $1::uuid
     AND gsp.goat_id = m.animal_id
     AND gsp.shed_id = m.shed_id
    WHERE m.shed_id IS NOT NULL
    GROUP BY m.park_id, m.due_date, m.shed_id, COALESCE(NULLIF(l.name, ''), l.location_code, m.shed_id::text)
  ) per_shed
  GROUP BY per_shed.park_id, per_shed.due_date
),
-- projection-review: membership=obligation_drive_membership (one row per obligation_id); group_key=(park_id, due_date) from that same membership row; join_cardinality=count(DISTINCT obligation_id) FILTER per mutually-exclusive bucket (completed>submitted>overdue>due>deferred) so total_count=completed+submitted+due+overdue+deferred with no double-counting, PLUS new animal_grain aggregation (count DISTINCT target_id where target_type='goat') rolled up per animal's bool_and(status='completed') per (park_id, due_date), with a separate animal_coverage subquery (separate animal subquery 1:1 LEFT JOIN on (park_id, due_date) produces animal_total_count and animal_completed_count, no fan-out); pagination=computed inline per ListEvents request as a bounded, keyset-paginated canonical read (5k-50k envelope, no projector, no materialized temp table); scope=(park_id, due_date) identical to membership's scope matrix, no re-derivation (unchanged by animal coverage); date/status: all existing dimension semantics preserved (animal counts are independent new grain, do not affect obligation buckets or date-window/status-bucket logic)
-- projection-review: membership=obligation_drive_membership (one row per obligation_id, the obligation grain); group_key=(park_id, due_date); join_cardinality=count(DISTINCT obligation_id) FILTER per bucket over that single membership row-set, no join fan-out (sc/ac/vl/sa are each exactly 1 row per (park_id, due_date) and are attached 1:1 AFTER grouping). Grain proof for the FIVE-bucket disjointness fix: the bucket key is the pair (status, submitted_for_verification), both columns of the SAME membership row, so bucketing is a pure per-row partition -- no cross-row/cross-grain dependency. Partition is now total and disjoint: completed = status='completed'; submitted = status<>'completed' AND submitted; deferred = status='deferred' AND NOT submitted; overdue = status IN ('overdue','missed') AND NOT submitted AND NOT deferred; due = the remaining open statuses AND NOT submitted. Every status in total_count's list appears in exactly one branch for each value of submitted_for_verification, hence total_count = completed+submitted+due+overdue+deferred exactly (previously an overdue-or-deferred row that was ALSO submitted was counted twice, in submitted_count and again in overdue_count/deferred_count). progress_* is derived at the SAME group grain from already-grouped scalars (animal grain when total_animals>0, else obligation grain) and adds no rows, no joins, and no new scan. pagination=unchanged (computed inline per ListEvents request, bounded keyset canonical read, 5k-50k envelope, no projector, no materialized table); scope=(park_id, due_date), unchanged, no re-derivation; date/status window semantics unchanged -- only the mutually-exclusive bucket predicates and the new derived progress scalars changed, no index or scan shape impact.
-- projection-review evidence (AGENTS.md "Grain Predicates and Executable Gates", clauses a/b/c):
--   (a) PRODUCER unique column list: obligation_drive_membership is unique on (obligation_id) -- the
--       obligation_membership_rows UNION dedups a row matched by several branches, so one obligation
--       appears exactly once. CONSUMER match/group column list: GROUP BY (m.park_id, m.due_date).
--       total_count/completed_count/submitted_count/due_count/overdue_count/deferred_count and the
--       progress_* scalars all range over that one grouped row-set; no bucket introduces an extra
--       WHERE dimension that the others lack, and every bucket now carries the SAME explicit status
--       whitelist (no bucket falls back to the membership CTE's hand-maintained deny-list).
--   (b) Row multiplicity of every joined side: sc (shed_complete), ac (animal_coverage), vl
--       (vaccine_labels) and sa (sheds) are each pre-aggregated to EXACTLY ONE row per
--       (park_id, due_date) and LEFT JOINed 1:1 AFTER the GROUP BY, so no join fans out the
--       obligation grain. ac itself pre-aggregates goats to one row per animal
--       (bool_and(status='completed')) before counting, so an animal due several vaccines the same
--       day contributes 1, not N.
--   (c) Ratio key sets, shown identical: progress_pct's numerator and denominator range over the
--       SAME key set in both branches -- animals branch = DISTINCT target_id (target_type='goat')
--       within (park_id, due_date) for BOTH completed_animals and total_animals; doses branch =
--       DISTINCT obligation_id within (park_id, due_date) for BOTH completed_count and total_count.
--       The branch predicate (total_animals > 0) is evaluated once and selects the key set for
--       numerator, denominator and basis label together, so the percentage can never mix an animal
--       numerator with a dose denominator. progress_completed is the COMPLETED count in both
--       branches -- never a submitted/pending count -- so progress <= 100 by construction.
obligation_drive_summary AS (
  -- Bucket precedence is mutually exclusive and total_count-complete. Invariant:
  --   total_count = completed_count + submitted_count + due_count + overdue_count + deferred_count
  -- Stock/execution "blocked" visibility is deliberately out of scope -- stock is not a built
  -- product feature yet (owner decision 2026-07-14); an obligation on a stock-blocked batch is
  -- bucketed purely by its own status. Revisit when the stock module ships.
  -- Precedence (each obligation counted in EXACTLY ONE of the FIVE buckets). submitted_for_verification
  -- is a SECOND dimension on top of status (work recorded on mobile, not yet verified), so it must be
  -- subtracted from EVERY non-completed status bucket -- not just due_count. Before this fix a shed that
  -- was submitted-but-late landed in BOTH submitted_count and overdue_count (and submitted-while-deferred
  -- in both submitted_count and deferred_count), so the "5 disjoint buckets" invariant documented on
  -- DriveSummary in contracts/openapi/app-api.yaml was false and total_count < sum(buckets):
  --   completed = status='completed'                       (verification already done)
  --   submitted = NOT completed AND submitted_for_verification (any open status, awaiting verification)
  --   deferred  = NOT submitted AND status='deferred'      (a clinical/anchor hold stays deferred)
  --   overdue   = NOT submitted AND NOT deferred AND status IN ('overdue','missed')
  --   due       = everything else open, not already bucketed
  -- SCALE: aggregate membership to the (park_id, due_date) GROUP grain FIRST, then attach the three
  -- 1:1 per-group sub-metrics (shed_complete / animal_coverage / vaccine_labels). Previously the
  -- membership rows were LEFT JOINed to those sub-aggregates at ROW grain and grouped afterwards; with
  -- CTE rows=1 estimates + IS-NOT-DISTINCT-FROM join keys the planner re-executed each sub-aggregate
  -- once per membership row (O(n^2): loops = #membership rows). Grouping first collapses the outer side
  -- to one row per park-day, so the sub-aggregate joins are group-count x group-count. Result set is
  -- byte-for-byte identical (sc/ac/vl are exactly one row per (park_id, due_date), so no fan-out and
  -- the former max()/COALESCE picked that single value).
  SELECT
    g.park_id,
    g.due_date,
    g.total_count,
    g.completed_count,
    g.submitted_count,
    g.due_count,
    g.overdue_count,
    g.deferred_count,
    g.rejected_count,
    g.shed_count,
    COALESCE(sc.sheds_completed, 0)::int AS sheds_completed,
    COALESCE(ac.total_animals, 0)::int AS total_animals,
    COALESCE(ac.completed_animals, 0)::int AS completed_animals,
    COALESCE(ac.submitted_animals, 0)::int AS submitted_animals,
    COALESCE(vl.vaccine_labels, ARRAY[]::text[]) AS vaccine_labels,
    COALESCE(sa.sheds, '[]'::jsonb) AS sheds,
    COALESCE(ld.drive_name, '') AS drive_name,
    COALESCE(ld.drive_total, ac.total_animals, 0)::int AS drive_total,
    g.park_code
  FROM (
    SELECT
      m.park_id,
      m.due_date,
      count(DISTINCT m.obligation_id) FILTER (WHERE m.status IN (
        'completed', 'scheduled', 'due', 'in_progress', 'proof_pending',
        'verification_pending', 'rejected', 'rework_due', 'overdue', 'missed',
        'deferred'))::int AS total_count,
      count(DISTINCT m.obligation_id) FILTER (WHERE m.status = 'completed')::int AS completed_count,
      -- Carries the SAME explicit status whitelist as total_count above. It must not lean on the
      -- membership CTE's NOT IN ('superseded','canceled','waived') pre-filter (canonical_read.go
      -- obligation_membership_rows): that list is hand-maintained in a different CTE, so a newly
      -- added terminal status would be excluded from total_count (explicit allow-list) while still
      -- falling into submitted_count (implicit deny-list), silently breaking
      -- total_count = completed + submitted + due + overdue + deferred. Every bucket now ranges over
      -- one identical status key set.
      count(DISTINCT m.obligation_id) FILTER (
        WHERE m.status <> 'completed'
          AND m.submitted_for_verification
          AND m.status IN ('scheduled', 'due', 'in_progress', 'proof_pending',
            'verification_pending', 'rejected', 'rework_due', 'overdue', 'missed',
            'deferred')
      )::int AS submitted_count,
      -- due vs overdue is READ-TIME on the ORIGINAL scheduled business date, matching the
      -- headline's genuine_overdue. obligation_instances never carries a literal 'overdue'
      -- status (the baseline CHECK does not permit it), so keying this bucket off
      -- status IN ('overdue','missed') made overdue_count structurally ~0 while the card
      -- headline said "overdue" -- one card, two answers. original_due_date (not due_date) is
      -- used because the rollover rewrites due_date to today, and a rolled date is never
      -- < today.
      count(DISTINCT m.obligation_id) FILTER (
        WHERE m.status <> 'completed'
          AND NOT m.submitted_for_verification
          AND m.status <> 'deferred'
          AND m.status IN ('scheduled', 'due', 'in_progress', 'proof_pending', 'verification_pending', 'rejected', 'rework_due')
          AND m.original_due_date >= (now() AT TIME ZONE 'Asia/Kolkata')::date
      )::int AS due_count,
      count(DISTINCT m.obligation_id) FILTER (
        WHERE m.status <> 'completed'
          AND NOT m.submitted_for_verification
          AND m.status <> 'deferred'
          AND (
            m.status = 'missed'
            OR (
              m.status IN ('scheduled', 'due', 'in_progress', 'proof_pending', 'verification_pending', 'rejected', 'rework_due')
              AND m.original_due_date < (now() AT TIME ZONE 'Asia/Kolkata')::date
            )
          )
      )::int AS overdue_count,
      count(DISTINCT m.obligation_id) FILTER (
        WHERE m.status = 'deferred'
          AND NOT m.submitted_for_verification
      )::int AS deferred_count,
      -- rejected_count is INFORMATIONAL ONLY -- a subset already counted inside due_count/
      -- overdue_count above, never an additional partition. obligation_instances.status has NO
      -- 'rejected' value (the baseline CHECK does not permit it, and
      -- vaccinationexecution/adapters/postgres/repository.go RecordVerdict-adjacent read maps a
      -- rejected verdict back to effective status 'due' -- the obligation genuinely reopens as
      -- due/overdue work, matching m.status here). "Rejected" is therefore a SEPARATE dimension
      -- from status, exactly like submitted_for_verification above: m.currently_rejected (see the
      -- obligation_drive_membership CTE) reads the live vaccination_completions row for this
      -- obligation over the vaccination_completion_rejections archive row, so an animal that was
      -- rejected and then rescanned-and-accepted stops counting the moment the new completion is
      -- accepted -- current state, not lifetime history. It exists so the card can explain WHY the
      -- completed/progress numerator dropped after a verifier rejects proof, instead of the drop
      -- reading as an unexplained mystery. Adding it does NOT change the five-bucket disjoint
      -- total invariant above: total_count still equals completed+submitted+due+overdue+deferred
      -- exactly -- currently_rejected obligations already carry status IN ('due','overdue') (or
      -- occasionally 'scheduled'/'in_progress' before the rollover catches up) and were already
      -- counted in exactly one of those buckets before this column existed.
      count(DISTINCT m.obligation_id) FILTER (
        WHERE m.currently_rejected
          AND m.status <> 'completed'
      )::int AS rejected_count,
      count(DISTINCT m.shed_id) FILTER (WHERE m.shed_id IS NOT NULL)::int AS shed_count,
      max(m.park_code) AS park_code
    FROM obligation_drive_membership m
    WHERE current_setting('goatos.include_drive_summary', true) = 'true'
    GROUP BY m.park_id, m.due_date
  ) g
  LEFT JOIN obligation_drive_shed_complete sc
    ON sc.park_id IS NOT DISTINCT FROM g.park_id AND sc.due_date = g.due_date
  LEFT JOIN obligation_drive_animal_coverage ac
    ON ac.park_id IS NOT DISTINCT FROM g.park_id AND ac.due_date = g.due_date
  LEFT JOIN obligation_drive_vaccine_labels vl
    ON vl.park_id IS NOT DISTINCT FROM g.park_id AND vl.due_date = g.due_date
  LEFT JOIN obligation_drive_shed_animals sa
    ON sa.park_id IS NOT DISTINCT FROM g.park_id AND sa.due_date = g.due_date
  LEFT JOIN obligation_logical_drive_rollup ld
    ON ld.park_id IS NOT DISTINCT FROM g.park_id AND ld.execution_date = g.due_date
),
-- projection-review: membership=obligation_drive_membership (exactly one row per obligation_id, the obligation grain -- the SAME membership CTE the five-bucket obl_summary groups over, so headline and counts can never diverge on membership); group_key=(park_id, due_date) taken from that same membership row, never re-derived from a joined table; join_cardinality=NO JOIN -- this CTE reads the single membership row-set and collapses it with bool_or over three predicates, so it cannot fan out; each flag is a pure per-row predicate on columns of the SAME row (status, submitted_for_verification, due_date), making the grouping a total partition of the row-set; the result is attached to park_drive_events 1:1 on (park_id, due_date), the identical key, so it adds no rows; pagination=computed inline per ListEvents request inside the bounded keyset canonical read (5k-50k envelope, no projector, no materialized table); the flags are whole-filter aggregates over the group, NOT page-local, so Limit changes rows only and never the headline; scope=(park_id, due_date), identical to membership's scope matrix, shed remains a display dimension only and never narrows drive membership; date=due_date is already IST-normalized at membership build time ((membership_at AT TIME ZONE 'Asia/Kolkata')::date), so genuine_overdue compares IST date to IST date with no second conversion; status=genuine_missed/genuine_overdue are submission-aware (status AND submitted_for_verification together, never status alone), and genuine_overdue is READ-TIME over the open-status allow-list -- it never references a literal 'overdue' obligation status, which the baseline CHECK does not permit.
obligation_drive_effective_state AS (
  -- Always-on effective status flags computed at obligation grain, independent of optional
  -- drive_summary. These account for submission status to ensure headline/severity don't render
  -- FALSE CRITICAL when all work is submitted. Unlike obl_summary (conditional on
  -- include_drive_summary=true), these are ALWAYS available for headline logic.
  -- grain proof: membership=obligation_drive_membership (one obligation per row);
  -- group_key=(park_id, due_date) from that same membership row; bucket dimensions are
  -- submission-aware (submitted_for_verification AND status together, not status alone).
  -- Read-time semantics: genuine_overdue = open obligations (status NOT IN completed/deferred/missed)
  -- with IST business due_date in the past, AND NOT submitted (matching grouped.has_overdue logic
  -- which uses '(due_at AT TIME ZONE 'Asia/Kolkata')::date < (now())::date' on same membership).
  SELECT
    m.park_id,
    m.due_date,
    bool_or(m.submitted_for_verification) AS has_submitted,
    bool_or(m.status = 'missed' AND NOT m.submitted_for_verification) AS genuine_missed,
    bool_or(
      m.status NOT IN ('completed', 'deferred', 'missed')
      AND m.status IN ('scheduled', 'due', 'in_progress', 'proof_pending', 'verification_pending', 'rejected', 'rework_due')
      -- original_due_date, NOT due_date: due_date may have been rolled forward to today by the
      -- "keeps showing until CLOSED" rollover, and a rolled date is never < today, so using it
      -- here reported an untouched past-due drive as merely in_progress.
      AND m.original_due_date < (now() AT TIME ZONE 'Asia/Kolkata')::date
      AND NOT m.submitted_for_verification
    ) AS genuine_overdue,
    -- Bucket COUNTS at obligation grain, carrying the SAME disjoint predicates obl_summary
    -- already proves total = completed + submitted + due + overdue + deferred. They live here,
    -- not in obl_summary, because the calendar CARD renders them unconditionally while
    -- obl_summary is gated on goatos.include_drive_summary='true'. Before this, the card's
    -- scheduled_count/review_count were derived from the BATCH status in park_drive_groups —
    -- a different grain and a different source of truth from the headline status, which reads
    -- eff_state.has_submitted. That split is what made a fully-submitted drive render
    -- status='verification_pending' with review_count=0 and scheduled_count=20: batch status
    -- 'in_progress' is not in the review list (-> has_review false -> review_count 0) yet IS in
    -- the scheduled list (-> the same 20 submitted animals counted as still scheduled). Both
    -- numbers now come from the same membership rows the headline does, so card counts and card
    -- status can no longer disagree, and submitted work is in exactly one bucket.
    count(DISTINCT m.obligation_id) FILTER (
      WHERE m.status <> 'completed'
        AND m.submitted_for_verification
        AND m.status IN ('scheduled', 'due', 'in_progress', 'proof_pending',
          'verification_pending', 'rejected', 'rework_due', 'overdue', 'missed', 'deferred')
    )::int AS submitted_count,
    count(DISTINCT m.obligation_id) FILTER (
      WHERE m.status <> 'completed'
        AND NOT m.submitted_for_verification
        AND m.status <> 'deferred'
        AND m.status IN ('scheduled', 'due', 'in_progress', 'proof_pending',
          'verification_pending', 'rejected', 'rework_due')
    )::int AS due_count,
    count(DISTINCT m.obligation_id) FILTER (
      WHERE m.status = 'deferred' AND NOT m.submitted_for_verification
    )::int AS deferred_count
  FROM obligation_drive_membership m
  GROUP BY m.park_id, m.due_date
),
park_drive_events AS (
  -- CR-002/CR-003 (calendar-canonical-5k50k review): the event_id is the STABLE park+business-date
  -- identity from the moment a drive exists -- NEVER the underlying single source's own batch:/
  -- catchup: id, even when drive_count = 1. Two related bugs this fixes together:
  --   CR-002 (aggregated ids were unparseable): a drive_count > 1 row already emitted
  --     'parkdrive:park:...'/'parkdrive:tenant:...', but domain.ParseDriveEventID only accepted
  --     batch:/catchup: -- so GetEventDetail/ListDriveTargets 400'd on every real multi-source drive.
  --     Fixed on the domain side (event_id.go now parses this form too); this CTE's shape was already
  --     the target the fix aligns to.
  --   CR-003 (identity instability across membership change): the OLD CASE below collapsed
  --     drive_count = 1 to grouped.single_event_id (the lone source's own batch:<id>/catchup:... id),
  --     so a solo drive's event_id MUTATED to parkdrive:... the moment a second same-day source
  --     joined the same park+day. calendar_snoozes/notification_requests key on the exact
  --     calendar_event_id text (see repository.go's loadActionTarget/activeSnoozeIDs/
  --     calendarReminderRailSQL, which all join by plain string equality against source_events.
  --     event_id -- no FK, no ID-shape awareness since migration 000189) -- so that mutation silently
  --     detached any existing snooze/reminder/escalation state and could let a reminder rearm/dup.
  --     Removing the drive_count = 1 branch means a solo drive is ALREADY parkdrive:-identified from
  --     day one: adding a second source never changes event_id, so state keyed on it never detaches.
  --     batch:/catchup: ids remain valid, independently resolvable MEMBERSHIP identities (still parsed
  --     by domain.ParseDriveEventID, still usable for e.g. legacy/internal lookups) -- they are simply
  --     no longer the identity ANY drive is first assigned or ever mutates through; grouped.
  --     single_source_target_type/single_source_target_id (below) still carry that membership detail
  --     into source_target_type/source_target_id for a drive_count = 1 row.
  SELECT
    CASE
      WHEN grouped.park_id IS NOT NULL THEN 'parkdrive:park:' || grouped.park_id::text || ':date:' || grouped.due_day
      ELSE 'parkdrive:tenant:' || $1::text || ':date:' || grouped.due_day
    END AS event_id,
    'vaccination_drive'::text AS event_type,
    'pc'::text AS owner_key,
    CASE WHEN grouped.park_id IS NOT NULL THEN 'Park vaccination drive' ELSE 'Vaccination drive' END AS title,
    cardinality(shed_meta.labels)::text ||
      CASE WHEN cardinality(shed_meta.labels) = 1 THEN ' shed · ' ELSE ' sheds · ' END ||
      cardinality(vaccine_meta.labels)::text ||
      CASE WHEN cardinality(vaccine_meta.labels) = 1 THEN ' vaccine' ELSE ' vaccines' END AS subtitle,
    CASE
      -- projection-review: membership=obligation_drive_membership via obligation_drive_effective_state, one row per obligation collapsed to one row per (park_id, due_date); group_key=(park_id, due_date), the SAME key grouped.* already carries, joined with IS NOT DISTINCT FROM on park_id so a NULL-park tenant drive still matches instead of dropping out; join_cardinality=strictly 1:1, because eff_state is UNIQUE on (park_id, due_date) by construction (it is GROUP BY on exactly those two columns), so this LEFT JOIN adds no rows and cannot fan out the drive -- the COALESCE(..., false) wrappers cover ONLY the zero-obligation drive, where every flag is correctly false; pagination=headline and severity come from whole-filter group aggregates, never from the emitted page, so Limit changes which events appear but never what an event says about itself (asserted by the MultiPage/PageBoundary case); scope=(park_id, due_date) only, shed stays a display dimension and never narrows the headline, date=eff_state.due_date is IST-normalized at membership build time and compared against grouped.due_day::date so both sides are IST dates with no second conversion, status=precedence is a total ordering placing the three submission-aware flags first (genuine_missed, then genuine_overdue, then has_submitted/has_review) ahead of the legacy in_progress/completed/scheduled/deferred branches, so a mixed drive still reads missed/critical and past-due open work can never fall through to the false-green scheduled.
      -- grain proof: headline depends on OBLIGATION-grain effective state flags, ALWAYS computed
      -- from obligation_drive_membership (which accounts for submission status), independent of
      -- the optional drive_summary. This ensures consistent headline regardless of whether
      -- include_drive_summary is requested. eff_state columns (genuine_missed, genuine_overdue,
      -- has_submitted) are grouped at (park_id, due_date), same scope as headline decision.
      -- Headline precedence (each drive status assigned exactly once):
      --   genuine_missed (status='missed' AND NOT submitted) → missed/critical (don't hide real misses)
      --   genuine_overdue (past-due open, NOT submitted) → overdue/critical (don't hide real overdue work)
      --   has_submitted OR has_review → verification_pending/warning (work is recorded)
      --   in_progress / completed / scheduled / deferred as before
      -- C13 fix: genuine_overdue MUST outrank has_submitted/has_review in the headline, exactly
      -- like genuine_missed already outranks both. genuine_overdue is defined as
      -- (open status AND past-due AND NOT submitted), so it is already mutually exclusive with
      -- "every obligation in this group is submitted" -- a fully-submitted drive always has
      -- genuine_overdue = false regardless of this branch's position, so this reorder cannot
      -- regress the f4cdd29b8 fix (fully-submitted drives still read verification_pending). What
      -- it does fix: a MIXED drive (>=1 submitted, >=1 genuinely overdue+unsubmitted) previously
      -- matched has_submitted first and reported 'verification_pending', hiding the real overdue
      -- work; severity (below) independently derives from genuine_overdue and disagreed
      -- ('critical'), so headline and severity could contradict each other on the exact same row.
      -- See TestCanonicalRead_MixedDriveOverdueOutranksSubmitted.
      WHEN COALESCE(eff_state.genuine_missed, false) THEN 'missed'
      WHEN COALESCE(eff_state.genuine_overdue, false) THEN 'overdue'
      WHEN grouped.has_review OR COALESCE(eff_state.has_submitted, false) THEN 'verification_pending'
      WHEN grouped.has_in_progress THEN 'in_progress'
      WHEN grouped.all_completed THEN 'completed'
      WHEN COALESCE(grouped.scheduled_count, 0) > 0 THEN 'scheduled'
      WHEN grouped.has_deferred THEN 'deferred'
      ELSE 'scheduled'
    END AS status,
    CASE
      -- severity precedence: critical only for genuine (unsubmitted) missed/overdue;
      -- warning if submitted/review pending or due within 24h; info otherwise.
      WHEN COALESCE(eff_state.genuine_missed, false) OR COALESCE(eff_state.genuine_overdue, false) THEN 'critical'
      WHEN grouped.first_due_at <= now() + interval '24 hours' THEN 'warning'
      ELSE 'info'
    END AS severity,
    grouped.first_due_at AS due_at,
    grouped.window_start,
    grouped.window_end,
    'Asia/Kolkata'::text AS timezone,
    'india_only'::text AS timezone_source,
    grouped.park_id,
    grouped.park_code,
    -- Mirrors the catchup_drive_events single-shed collapse (shed_count = 1): a park-day drive that
    -- is genuinely backed by exactly one shed's obligations/batches must still resolve to that shed
    -- so shed-scoped scope authorization (canonical_selected / calendarCanonicalDetailSQL's
    -- park_id = ANY / shed_id = ANY predicate) can match it. Multi-shed park-day drives keep shed_id
    -- NULL (there is no single shed to attribute the aggregate to).
    CASE WHEN grouped.shed_count = 1 THEN grouped.primary_shed_id ELSE NULL::uuid END AS shed_id,
    CASE WHEN grouped.shed_count = 1 THEN grouped.primary_shed_name ELSE NULL::text END AS shed_name,
    NULL::uuid AS cohort_id,
    NULL::text AS cohort_name,
    CASE WHEN grouped.park_id IS NULL THEN 'tenant'::text ELSE 'park'::text END AS target_type,
    grouped.target_count,
    NULL::uuid AS protocol_id,
    NULL::uuid AS protocol_version_id,
    NULL::uuid AS rule_id,
    CASE
      WHEN cardinality(vaccine_meta.labels) = 1 THEN vaccine_meta.labels[1]
      ELSE cardinality(vaccine_meta.labels)::text || ' vaccines'
    END AS vaccine_name,
    CASE
      WHEN cardinality(vaccine_meta.labels) = 0 THEN NULL::text
      WHEN cardinality(vaccine_meta.labels) = 1 THEN vaccine_meta.labels[1]
      WHEN cardinality(vaccine_meta.labels) = 2 THEN vaccine_meta.labels[1] || ', ' || vaccine_meta.labels[2]
      ELSE vaccine_meta.labels[1] || ', ' || vaccine_meta.labels[2] || ' +' || (cardinality(vaccine_meta.labels) - 2)::text || ' more'
    END AS dose_code,
    true AS source_backed,
    'Park/day vaccination drive projection'::text AS source_label,
    CASE WHEN grouped.drive_count = 1 THEN grouped.single_source_target_type ELSE 'park_drive'::text END AS source_target_type,
    CASE WHEN grouped.drive_count = 1 THEN grouped.single_source_target_id ELSE COALESCE(grouped.park_id, $1::uuid) END AS source_target_id,
    COALESCE(NULLIF(grouped.operator_names, ''), 'PC drive team')::text AS assignee_label,
    'pc_vaccinator'::text AS executor_role,
    'PC verifier'::text AS verifier_label,
    'not_scheduled'::text AS reminder_state,
    'local-stub'::text AS primary_notification_channel,
    'none'::text AS escalation_state,
    false AS system,
    false AS cross_cutting,
    jsonb_build_object('vaccination', '/vaccination') AS links,
    jsonb_build_object(
      'summary', jsonb_build_object(
        'owner', 'PC',
        'target_count', grouped.target_count,
        'summary_primary', COALESCE(eff_state.due_count, grouped.scheduled_count, 0)::text || CASE WHEN COALESCE(eff_state.due_count, grouped.scheduled_count, 0) = 1 THEN ' scheduled dose' ELSE ' scheduled doses' END,
        'summary_secondary', cardinality(shed_meta.labels)::text ||
          CASE WHEN cardinality(shed_meta.labels) = 1 THEN ' shed · ' ELSE ' sheds · ' END ||
          cardinality(vaccine_meta.labels)::text ||
          CASE WHEN cardinality(vaccine_meta.labels) = 1 THEN ' vaccine' ELSE ' vaccines' END,
        'summary_tertiary', CASE
          WHEN cardinality(vaccine_meta.labels) = 0 THEN ''
          WHEN cardinality(vaccine_meta.labels) = 1 THEN vaccine_meta.labels[1]
          WHEN cardinality(vaccine_meta.labels) = 2 THEN vaccine_meta.labels[1] || ', ' || vaccine_meta.labels[2]
          ELSE vaccine_meta.labels[1] || ', ' || vaccine_meta.labels[2] || ' +' || (cardinality(vaccine_meta.labels) - 2)::text || ' more'
        END,
        'shed_count', cardinality(shed_meta.labels),
        'vaccine_count', cardinality(vaccine_meta.labels),
        'drive_count', grouped.drive_count,
        'catch_up_count', COALESCE(grouped.catch_up_count, 0),
        'scheduled_count', COALESCE(eff_state.due_count, grouped.scheduled_count, 0),
        'queue_count', grouped.queue_count,
        'deferred_count', COALESCE(eff_state.deferred_count, grouped.deferred_count, 0),
        -- review_count is an OBLIGATION COUNT, not a boolean flag. It previously rendered
        -- CASE WHEN has_review THEN 1 ELSE 0 END -- grain-incompatible with its siblings
        -- target_count/scheduled_count/deferred_count, which are work counts, and sourced from
        -- the batch status rather than the submission truth the card's own status uses.
        'review_count', COALESCE(eff_state.submitted_count, CASE WHEN grouped.has_review THEN grouped.target_count ELSE 0 END, 0),
        'shed_labels', to_jsonb(shed_meta.labels),
        'vaccine_labels', to_jsonb(vaccine_meta.labels)
      ),
      'source_and_rule', jsonb_build_object('business_date', grouped.due_day, 'source_event_ids', grouped.source_event_ids),
      'execution', jsonb_build_object('work_state', CASE WHEN grouped.all_completed THEN 'completed' ELSE 'open' END, 'source_event_ids', grouped.source_event_ids),
      'stock', jsonb_build_object(),
      'proof', jsonb_build_object(),
      'verification', jsonb_build_object('verifier', 'PC verifier'),
      'notification_channels', jsonb_build_array('local-stub'),
      'notification_policy', jsonb_build_object(
        'nudge_allowed', NOT grouped.has_catch_up,
        'read_only', grouped.has_catch_up
      ),
      'links', jsonb_build_object('vaccination', '/vaccination'),
      'drive_summary', CASE WHEN current_setting('goatos.include_drive_summary', true) = 'true' AND obl_summary.total_count > 0 THEN jsonb_build_object(
        'park_name', COALESCE(obl_summary.park_code, grouped.park_code, 'Vaccination drive'),
        'drive_name', obl_summary.drive_name,
        'drive_total', obl_summary.drive_total,
        'due_date', grouped.due_day,
        'shed_count', obl_summary.shed_count,
        'sheds_completed', obl_summary.sheds_completed,
        'sheds', obl_summary.sheds,
        'vaccine_labels', to_jsonb(obl_summary.vaccine_labels),
        'total_count', obl_summary.total_count,
        'completed_count', obl_summary.completed_count,
        'submitted_count', obl_summary.submitted_count,
        -- remaining_count = WORK STILL OWED BY THE OPERATOR, summed from the three open buckets so
        -- it is the SAME arithmetic the card already ships beside it. It was
        -- total_count - completed_count, a numerator that excludes submitted work, while
        -- progress_completed below is FIELD WORK DONE = completed + submitted. On a fully submitted
        -- drive the one object then carried two contradictory answers to "how much is left"
        -- (progress_pct 100 next to remaining_count 20), and a client picking remaining_count for an
        -- "N left" label disagreed with the ring beside it -- the cross-surface failure mode that
        -- produced the 200-vs-400 incident. Under the OLD "progress = verified only" rule the two
        -- agreed; after the 2026-08-03 progress-semantics decision they cannot, so remaining_count
        -- follows the progress numerator. The outstanding verifier review is carried by
        -- submitted_count and the verification_pending status, never by inflating "remaining".
        -- Identical to total_count - completed_count - submitted_count, because the five buckets are
        -- a disjoint, total partition (invariant asserted directly above in obligation_drive_summary).
        'remaining_count', obl_summary.due_count + obl_summary.overdue_count + obl_summary.deferred_count,
        'due_count', obl_summary.due_count,
        'overdue_count', obl_summary.overdue_count,
        'deferred_count', obl_summary.deferred_count,
        -- Informational subset of due_count/overdue_count (see obligation_drive_summary CTE) so
        -- the card can name why the progress numerator dropped, instead of leaving a silent gap.
        'rejected_count', obl_summary.rejected_count,
        'total_animals', obl_summary.total_animals,
        'completed_animals', obl_summary.completed_animals,
        'submitted_animals', obl_summary.submitted_animals,
        -- SINGLE cross-surface progress definition. Android and admin-web previously each derived
        -- their own ring numerator from different fields (Android took max(submitted, completed),
        -- web took completed only), so the SAME drive showed two different numbers and two different
        -- ring percentages. The backend now owns the numerator AND its basis; both clients render
        -- these verbatim. Basis is the distinct-ANIMAL grain whenever the drive has animals (a goat
        -- due several vaccines the same day is ONE animal, complete only when ALL its drive
        -- obligations are), else the obligation/dose grain. Numerator is FIELD WORK DONE =
        -- completed + submitted (maintainer contract): the operator vaccinated the animal, so the
        -- drive reads 100% and the outstanding video review is carried by the
        -- verification-pending status/chip, NOT by holding the ring at 0%. An earlier change made
        -- this COMPLETED-only to settle a web-vs-mobile parity disagreement; that silently
        -- redefined "done" as "verified" and showed an operator who had vaccinated every animal a
        -- 0% ring. Parity is preserved here instead -- backend owns the single number and both
        -- clients render it verbatim.
        -- The two buckets are disjoint by the precedence above (submitted_for_verification wins
        -- over completed), so completed + submitted <= total and progress can never exceed 100.
        'progress_basis', CASE WHEN obl_summary.total_animals > 0 THEN 'animals' ELSE 'doses' END,
        'progress_completed', CASE WHEN obl_summary.total_animals > 0 THEN obl_summary.completed_animals + obl_summary.submitted_animals ELSE obl_summary.completed_count + obl_summary.submitted_count END,
        'progress_total', CASE WHEN obl_summary.total_animals > 0 THEN obl_summary.total_animals ELSE obl_summary.total_count END,
        'progress_pct', CASE
          WHEN obl_summary.total_animals > 0 THEN round((obl_summary.completed_animals + obl_summary.submitted_animals) * 100.0 / obl_summary.total_animals)::int
          WHEN obl_summary.total_count > 0 THEN round((obl_summary.completed_count + obl_summary.submitted_count) * 100.0 / obl_summary.total_count)::int
          ELSE 0
        END,
        'owner_label', COALESCE(NULLIF(grouped.operator_names, ''), 'PC')
      ) ELSE NULL END
    ) AS detail
  FROM park_drive_groups grouped
  LEFT JOIN obligation_drive_effective_state eff_state
    ON eff_state.park_id IS NOT DISTINCT FROM grouped.park_id
    AND eff_state.due_date = grouped.due_day::date
  LEFT JOIN obligation_drive_summary obl_summary
    ON obl_summary.park_id IS NOT DISTINCT FROM grouped.park_id
    AND obl_summary.due_date = grouped.due_day::date
  CROSS JOIN LATERAL (
    SELECT COALESCE(array_agg(DISTINCT label ORDER BY label), ARRAY[]::text[]) AS labels
    FROM drive_sources source
    CROSS JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(source.detail->'summary'->'shed_labels') = 'array' THEN source.detail->'summary'->'shed_labels' ELSE '[]'::jsonb END) AS shed(label)
    WHERE source.park_id IS NOT DISTINCT FROM grouped.park_id
      AND (source.due_at AT TIME ZONE 'Asia/Kolkata')::date = grouped.due_day::date
  ) shed_meta
  CROSS JOIN LATERAL (
    SELECT COALESCE(array_agg(DISTINCT label ORDER BY label), ARRAY[]::text[]) AS labels
    FROM drive_sources source
    CROSS JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(source.detail->'summary'->'vaccine_labels') = 'array' THEN source.detail->'summary'->'vaccine_labels' ELSE '[]'::jsonb END) AS vaccine(label)
    WHERE source.park_id IS NOT DISTINCT FROM grouped.park_id
      AND (source.due_at AT TIME ZONE 'Asia/Kolkata')::date = grouped.due_day::date
  ) vaccine_meta
),
vaccination_history_events AS (
  -- Accepted vaccination administration history: completed doses from vaccination_completions table.
  -- These represent past accepted administrations that should appear as completed history in Calendar reads.
  -- Scoped by the completed obligation's own park/shed (goat's current location, falling back to the
  -- obligation/batch scope) to match the same owner/park/shed filters as open work above.
  SELECT
    'completion:' || vc.completion_id::text AS event_id,
    'vaccination_history'::text AS event_type,
    'pc'::text AS owner_key,
    pd.name || ' ' || pr.dose_code || ' completed' AS title,
    COALESCE(loc.shed_name, loc.park_code, 'Vaccination history') AS subtitle,
    'completed'::text AS status,
    'info'::text AS severity,
    COALESCE(vc.administered_at, vc.created_at) AS due_at,
    COALESCE(vc.administered_at, vc.created_at) AS window_start,
    COALESCE(vc.administered_at, vc.created_at) + interval '1 day' AS window_end,
    'Asia/Kolkata'::text AS timezone,
    'india_only'::text AS timezone_source,
    loc.park_id,
    loc.park_code,
    loc.shed_id,
    loc.shed_name,
    NULL::uuid AS cohort_id,
    NULL::text AS cohort_name,
    oi.target_type,
    1::int AS target_count,
    pd.protocol_id,
    pv.protocol_version_id,
    pr.rule_id,
    pd.name AS vaccine_name,
    pr.dose_code,
    true AS source_backed,
    pd.name AS source_label,
    'vaccination_completion'::text AS source_target_type,
    vc.completion_id AS source_target_id,
    'PC vaccinator'::text AS assignee_label,
    NULL::text AS executor_role,
    'PC verifier'::text AS verifier_label,
    'not_scheduled'::text AS reminder_state,
    'local-stub'::text AS primary_notification_channel,
    'none'::text AS escalation_state,
    false AS system,
    false AS cross_cutting,
    jsonb_build_object(
      'vaccination', '/vaccination/operations',
      'action_center', '/vaccination/action-center'
    ) AS links,
    jsonb_build_object(
      'summary', jsonb_build_object('owner', 'PC', 'target_count', 1),
      'source_and_rule', jsonb_build_object(
        'protocol_version_id', pv.protocol_version_id,
        'rule_id', pr.rule_id,
        'administered_at', COALESCE(vc.administered_at, vc.created_at)
      ),
      'execution', jsonb_build_object('completion_id', vc.completion_id, 'work_state', 'completed'),
      'stock', jsonb_build_object(),
      'proof', jsonb_build_object(),
      'verification', jsonb_build_object(),
      'notification_channels', jsonb_build_array('local-stub'),
      'notification_policy', jsonb_build_object(),
      'links', jsonb_build_object()
    ) AS detail
  FROM vaccination_completions vc
  JOIN obligation_instances oi
    ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
  LEFT JOIN goats g
    ON oi.target_type = 'goat' AND g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id AND ob.batch_id = oi.batch_id
  LEFT JOIN locations scope_loc
    ON scope_loc.tenant_id = oi.tenant_id
   AND scope_loc.location_id = COALESCE(g.park_id, ob.scope_id, oi.scope_id)
   AND scope_loc.location_type IN ('park', 'shed')
  LEFT JOIN locations scope_parent
    ON scope_parent.tenant_id = oi.tenant_id
   AND scope_parent.location_id = scope_loc.parent_location_id
  LEFT JOIN LATERAL (
    SELECT
      CASE
        WHEN scope_loc.location_type = 'park' THEN scope_loc.location_id
        WHEN scope_loc.location_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id
      END AS park_id,
      CASE
        WHEN scope_loc.location_type = 'park' THEN scope_loc.location_code
        WHEN scope_loc.location_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_code
      END AS park_code,
      CASE
        WHEN scope_loc.location_type = 'shed' THEN scope_loc.location_id
      END AS shed_id,
      CASE
        WHEN scope_loc.location_type = 'shed' THEN scope_loc.name
      END AS shed_name
  ) loc ON true
  WHERE vc.tenant_id = $1::uuid
    AND vc.status = 'accepted'
    AND pd.category = 'vaccination'
    AND pv.status = 'published'
    AND (COALESCE(vc.administered_at, vc.created_at) AT TIME ZONE 'Asia/Kolkata')::date >= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
    AND (COALESCE(vc.administered_at, vc.created_at) AT TIME ZONE 'Asia/Kolkata')::date < ($3::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
),
sop_events AS (
  SELECT DISTINCT ON (st.task_id)
    'calendar:' || st.task_id::text AS event_id,
    CASE
      WHEN st.state IN ('rework_requested', 'rejected') THEN 'vaccination_rework_due'
      ELSE 'vaccination_proof_verification'
    END AS event_type,
    'pc'::text AS owner_key,
    st.title,
    COALESCE(scope_loc.name, 'Vaccination SOP task') AS subtitle,
    CASE st.state
      WHEN 'submitted' THEN 'verification_pending'
      WHEN 'needs_review' THEN 'verification_pending'
      WHEN 'rework_requested' THEN 'rework_due'
      WHEN 'rejected' THEN 'rework_due'
      WHEN 'canceled' THEN 'canceled'
      ELSE 'proof_pending'
    END AS status,
    CASE
      WHEN st.priority IN ('high', 'urgent') OR st.due_at < now() THEN 'critical'
      WHEN st.due_at <= now() + interval '24 hours' THEN 'warning'
      ELSE 'info'
    END AS severity,
    st.due_at,
    st.due_at AS window_start,
    st.due_at + interval '1 day' AS window_end,
    'Asia/Kolkata'::text AS timezone,
    'india_only'::text AS timezone_source,
    loc.park_id,
    loc.park_code,
    loc.shed_id,
    loc.shed_name,
    CASE WHEN st.scope_type = 'cohort' THEN st.scope_id END AS cohort_id,
    CASE WHEN st.scope_type = 'cohort' THEN scope_loc.name END AS cohort_name,
    st.scope_type AS target_type,
    1::int AS target_count,
    pd.protocol_id,
    pv.protocol_version_id,
    pr.rule_id,
    pd.name AS vaccine_name,
    pr.dose_code,
    true AS source_backed,
    pd.name AS source_label,
    'sop_task'::text AS source_target_type,
    st.task_id AS source_target_id,
    COALESCE(st.assigned_to::text, 'PC verifier') AS assignee_label,
    CASE WHEN st.state IN ('submitted', 'needs_review') THEN NULL ELSE 'pc_vaccinator' END AS executor_role,
    CASE WHEN st.state IN ('submitted', 'needs_review') THEN 'PC verifier' ELSE NULL END AS verifier_label,
    'not_scheduled'::text AS reminder_state,
    'local-stub'::text AS primary_notification_channel,
    'none'::text AS escalation_state,
    false AS system,
    false AS cross_cutting,
    jsonb_build_object('workflow', '/vaccination/workflows/' || ('calendar:' || st.task_id::text)) AS links,
    jsonb_build_object(
      'summary', jsonb_build_object('owner', 'PC', 'task_type', st.task_type),
      'source_and_rule', jsonb_build_object(
        'protocol_version_id', pv.protocol_version_id,
        'rule_id', pr.rule_id
      ),
      'execution', jsonb_build_object('sop_task_id', st.task_id, 'work_state', st.state),
      'stock', jsonb_build_object(),
      'proof', jsonb_build_object('state', st.state),
      'verification', jsonb_build_object('state', st.state),
      'notification_channels', jsonb_build_array('local-stub'),
      'notification_policy', jsonb_build_object('nudge_allowed', true),
      'links', jsonb_build_object()
    ) AS detail
  FROM sop_tasks st
  JOIN obligation_instances oi
    ON oi.tenant_id = st.tenant_id AND oi.sop_task_id = st.task_id
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
  LEFT JOIN locations scope_loc
    ON scope_loc.tenant_id = st.tenant_id
   AND scope_loc.location_id = st.scope_id
   AND st.scope_type IN ('park', 'shed', 'cohort')
  LEFT JOIN locations scope_parent
    ON scope_parent.tenant_id = st.tenant_id
   AND scope_parent.location_id = scope_loc.parent_location_id
  LEFT JOIN locations scope_grand
    ON scope_grand.tenant_id = st.tenant_id
   AND scope_grand.location_id = scope_parent.parent_location_id
  LEFT JOIN LATERAL (
    SELECT
      CASE
        WHEN st.scope_type = 'park' THEN scope_loc.location_id
        WHEN st.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id
        WHEN st.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_id
      END AS park_id,
      CASE
        WHEN st.scope_type = 'park' THEN scope_loc.location_code
        WHEN st.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_code
        WHEN st.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_code
      END AS park_code,
      CASE
        WHEN st.scope_type = 'shed' THEN scope_loc.location_id
        WHEN st.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.location_id
      END AS shed_id,
      CASE
        WHEN st.scope_type = 'shed' THEN scope_loc.name
        WHEN st.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.name
      END AS shed_name
  ) loc ON true
  WHERE st.tenant_id = $1::uuid
    AND st.due_at >= $2::timestamptz
    AND st.due_at < $3::timestamptz
    AND pd.category = 'vaccination'
    AND pv.status = 'published'
    AND st.state IN ('assigned', 'in_progress', 'submitted', 'needs_review', 'rework_requested', 'rejected')
    AND NOT EXISTS (
      SELECT 1
      FROM obligation_batches ob
      WHERE ob.tenant_id = st.tenant_id
        AND ob.sop_task_id = st.task_id
        AND ob.status NOT IN ('superseded', 'canceled')
    )
  ORDER BY st.task_id, st.due_at
),
config_due AS (
  SELECT
    pv.*,
    pd.name AS protocol_name,
    NULLIF(pv.rule_dsl ->> 'activation_due_at', '') AS raw_due_at
  FROM protocol_versions pv
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  WHERE pv.tenant_id = $1::uuid
    AND pd.category = 'vaccination'
    AND pv.status = 'draft'
),
config_events AS (
  SELECT
    'calendar:' || protocol_version_id::text AS event_id,
    'vaccination_config_activation_review'::text AS event_type,
    'admin_data_ops'::text AS owner_key,
    'Review ' || protocol_name || ' activation' AS title,
    'Protocol activation review due'::text AS subtitle,
    'due'::text AS status,
    CASE WHEN raw_due_at::timestamptz < now() THEN 'critical' ELSE 'warning' END AS severity,
    raw_due_at::timestamptz AS due_at,
    raw_due_at::timestamptz AS window_start,
    raw_due_at::timestamptz + interval '1 day' AS window_end,
    'Asia/Kolkata'::text AS timezone,
    'india_only'::text AS timezone_source,
    NULL::uuid AS park_id,
    NULL::text AS park_code,
    NULL::uuid AS shed_id,
    NULL::text AS shed_name,
    NULL::uuid AS cohort_id,
    NULL::text AS cohort_name,
    'protocol_version'::text AS target_type,
    1::int AS target_count,
    protocol_id,
    protocol_version_id,
    NULL::uuid AS rule_id,
    protocol_name AS vaccine_name,
    NULL::text AS dose_code,
    true AS source_backed,
    protocol_name AS source_label,
    'protocol_version'::text AS source_target_type,
    protocol_version_id AS source_target_id,
    'Admin Data Ops reviewer'::text AS assignee_label,
    'admin_data_ops_reviewer'::text AS executor_role,
    NULL::text AS verifier_label,
    'not_scheduled'::text AS reminder_state,
    'local-stub'::text AS primary_notification_channel,
    'none'::text AS escalation_state,
    false AS system,
    false AS cross_cutting,
    jsonb_build_object('protocol', '/protocols/versions/' || protocol_version_id::text) AS links,
    jsonb_build_object(
      'summary', jsonb_build_object('owner', 'Admin / Data Ops'),
      'source_and_rule', jsonb_build_object('protocol_version_id', protocol_version_id, 'review_state', 'activation_due'),
      'execution', jsonb_build_object(),
      'stock', jsonb_build_object(),
      'proof', jsonb_build_object(),
      'verification', jsonb_build_object(),
      'notification_channels', jsonb_build_array('local-stub'),
      'notification_policy', jsonb_build_object(),
      'links', jsonb_build_object()
    ) AS detail
  FROM config_due
  WHERE raw_due_at ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}'
    AND raw_due_at::timestamptz >= $2::timestamptz
    AND raw_due_at::timestamptz < $3::timestamptz
),
source_events AS (
  SELECT * FROM obligation_events
  UNION ALL
  SELECT * FROM vaccination_history_events
  UNION ALL
  SELECT * FROM park_drive_events
  UNION ALL
  SELECT * FROM sop_events
  UNION ALL
  SELECT * FROM config_events
)`

// canonicalUnboundedWindow returns a generously wide [from, to) window for canonical point-lookups that
// have no natural calendar-list window: single-event resolution (detail, action-target, existence)
// and the reminder/escalation operational sweeps. It deliberately is NOT -infinity/infinity (that would
// defeat the shared CTE's tenant+due-window pruning) -- +/-2 years is wide enough that no realistically
// due obligation/batch/SOP task is ever missed, while still bounding the scan.
func canonicalUnboundedWindow(now time.Time) (time.Time, time.Time) {
	return now.AddDate(-2, 0, 0), now.AddDate(2, 0, 0)
}

// calendarCanonicalListSQL is the compute-on-read canonical reconstruction of the Calendar list. It
// is deliberately a large multi-CTE query: at the 5k-to-50k scale envelope this is the accepted
// alternative to a derived read model (ADR operational-kernel-5k-50k-scale-envelope). The scale-guard
// god-cte detector is suppressed on calendarCanonicalEventsCTE above via `scale-guard:ignore`.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarCanonicalListSQL = "WITH " + calendarCanonicalEventsCTE + `,
canonical_selected AS (
  -- scale-guard:ignore: 5k-50k-envelope; see operational-kernel-5k-50k-scale-envelope.md
  -- Canonical read-through: the same summary/label/drive_summary extraction calendarListSQL applies
  -- over calendar_event_projections, applied here directly over the canonical source_events CTE chain
  -- so the Calendar API serves without a calendar_projection_state freshness gate (ADR:
  -- operational-kernel-5k-50k-scale-envelope). Keyset on (due_at, event_id); the scale-critical scan is
  -- the tenant+due-window index scan of obligation_instances inside source_events.
  SELECT event_id, event_type, owner_key, title, subtitle, status, severity, due_at, window_start,
         window_end, timezone, timezone_source, park_id::text, park_code, shed_id::text, shed_name,
         cohort_id::text, cohort_name, target_type, target_count, protocol_id::text,
         protocol_version_id::text, rule_id::text, vaccine_name, dose_code, source_backed,
         source_label, assignee_label, executor_role, verifier_label, reminder_state,
         primary_notification_channel, escalation_state, system, cross_cutting, links,
         event_type = 'vaccination_drive' AS aggregated,
         event_type = 'vaccination_drive' AS all_day,
         COALESCE(detail->'summary'->>'summary_primary', '') AS summary_primary,
         COALESCE(detail->'summary'->>'summary_secondary', '') AS summary_secondary,
         COALESCE(detail->'summary'->>'summary_tertiary', '') AS summary_tertiary,
         COALESCE((detail->'summary'->>'shed_count')::int, CASE WHEN shed_id IS NULL THEN 0 ELSE 1 END) AS shed_count,
         COALESCE((detail->'summary'->>'vaccine_count')::int, CASE WHEN vaccine_name IS NULL THEN 0 ELSE 1 END) AS vaccine_count,
         COALESCE((detail->'summary'->>'drive_count')::int, CASE WHEN event_type = 'vaccination_drive' THEN 1 ELSE 0 END) AS drive_count,
         COALESCE((detail->'summary'->>'catch_up_count')::int, 0) AS catch_up_count,
         COALESCE((detail->'summary'->>'scheduled_count')::int, 0) AS scheduled_count,
         COALESCE((detail->'summary'->>'deferred_count')::int, 0) AS deferred_count,
         COALESCE((detail->'summary'->>'review_count')::int, 0) AS review_count,
         ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(detail->'summary'->'shed_labels') = 'array' THEN detail->'summary'->'shed_labels' ELSE '[]'::jsonb END)) AS shed_labels,
         ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(detail->'summary'->'vaccine_labels') = 'array' THEN detail->'summary'->'vaccine_labels' ELSE '[]'::jsonb END)) AS vaccine_labels,
         CASE WHEN current_setting('goatos.include_drive_summary', true) = 'true' AND jsonb_typeof(detail->'drive_summary') = 'object' THEN detail->'drive_summary' ELSE NULL END AS drive_summary
  FROM source_events
  WHERE due_at IS NOT NULL
    AND event_type <> 'vaccination_dose_due'
    AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending',
                   'verification_pending', 'rejected', 'rework_due', 'deferred', 'blocked', 'completed')
    -- Calendar catch-up is intentionally bounded to the requested operating window plus the same
    -- 45-day lookback used by obligation_events. The old unbounded bypass made every month/mobile
    -- click inspect historical tenant work before paging the requested window.
    AND (
      (due_at >= $2::timestamptz AND due_at < $3::timestamptz)
      OR (
        event_type = 'vaccination_drive'
        AND status IN ('missed', 'in_progress', 'verification_pending', 'proof_pending', 'rejected', 'rework_due', 'deferred', 'overdue')
        AND due_at >= $2::timestamptz - interval '45 days'
        AND due_at < $3::timestamptz
      )
    )
    AND ($4::text = '' OR owner_key = $4::text)
    AND ($5::text = '' OR status = $5::text)
    AND ($5::text <> '' OR status NOT IN ('completed', 'canceled', 'deferred'))
    AND ($6::text = '' OR park_id::text = nullif($6::text, ''))
    AND ($7::text = '' OR shed_id::text = nullif($7::text, ''))
    AND (
      $14::text = ''
      OR vaccine_name = $14::text
      OR (
        jsonb_typeof(detail->'summary'->'vaccine_labels') = 'array'
        AND (detail->'summary'->'vaccine_labels') ? $14::text
      )
    )
    AND ($8::timestamptz IS NULL OR (due_at, event_id) > ($8::timestamptz, $9::text))
    AND ($11::bool OR park_id = ANY($12::uuid[]) OR shed_id = ANY($13::uuid[]))
  ORDER BY due_at ASC, event_id ASC
  LIMIT $10
)
SELECT event_id, event_type, owner_key, title, subtitle, status, severity, due_at, window_start,
       window_end, timezone, timezone_source, park_id, park_code, shed_id, shed_name,
       cohort_id, cohort_name, target_type, target_count, protocol_id,
       protocol_version_id, rule_id, vaccine_name, dose_code, source_backed,
       source_label, assignee_label, executor_role, verifier_label, reminder_state,
       primary_notification_channel, escalation_state, system, cross_cutting, links,
       aggregated, all_day, summary_primary, summary_secondary, summary_tertiary,
       shed_count, vaccine_count, drive_count, catch_up_count, scheduled_count,
       deferred_count, review_count, shed_labels, vaccine_labels, drive_summary
FROM canonical_selected
ORDER BY due_at ASC, event_id ASC
LIMIT $10`

// calendarCanonicalDetailSQL resolves ONE calendar event by event_id straight from the canonical
// source_events reconstruction (not canonical_selected -- a single-event lookup must still resolve an
// individual 'obligation:<id>' (vaccination_dose_due) row, which canonical_selected's list-only
// `event_type <> 'vaccination_dose_due'` filter deliberately excludes). Column order matches
// scanCalendarEventWithDetail exactly (37 CalendarEvent columns + the raw detail jsonb blob).
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarCanonicalDetailSQL = "WITH " + calendarCanonicalEventsCTE + `
SELECT event_id, event_type, owner_key, title, subtitle, status, severity, due_at, window_start,
       window_end, timezone, timezone_source, park_id::text, park_code, shed_id::text, shed_name,
       cohort_id::text, cohort_name, target_type, target_count, protocol_id::text,
       protocol_version_id::text, rule_id::text, vaccine_name, dose_code, source_backed,
       source_label, assignee_label, executor_role, verifier_label, reminder_state,
       primary_notification_channel, escalation_state, system, cross_cutting, links,
       event_type = 'vaccination_drive' AS aggregated,
       event_type = 'vaccination_drive' AS all_day,
       COALESCE(detail->'summary'->>'summary_primary', '') AS summary_primary,
       COALESCE(detail->'summary'->>'summary_secondary', '') AS summary_secondary,
       COALESCE(detail->'summary'->>'summary_tertiary', '') AS summary_tertiary,
       COALESCE((detail->'summary'->>'shed_count')::int, CASE WHEN shed_id IS NULL THEN 0 ELSE 1 END) AS shed_count,
       COALESCE((detail->'summary'->>'vaccine_count')::int, CASE WHEN vaccine_name IS NULL THEN 0 ELSE 1 END) AS vaccine_count,
       COALESCE((detail->'summary'->>'drive_count')::int, CASE WHEN event_type = 'vaccination_drive' THEN 1 ELSE 0 END) AS drive_count,
       COALESCE((detail->'summary'->>'catch_up_count')::int, 0) AS catch_up_count,
       COALESCE((detail->'summary'->>'scheduled_count')::int, 0) AS scheduled_count,
       COALESCE((detail->'summary'->>'deferred_count')::int, 0) AS deferred_count,
       COALESCE((detail->'summary'->>'review_count')::int, 0) AS review_count,
       ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(detail->'summary'->'shed_labels') = 'array' THEN detail->'summary'->'shed_labels' ELSE '[]'::jsonb END)) AS shed_labels,
       ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(detail->'summary'->'vaccine_labels') = 'array' THEN detail->'summary'->'vaccine_labels' ELSE '[]'::jsonb END)) AS vaccine_labels,
       CASE WHEN jsonb_typeof(detail->'drive_summary') = 'object' THEN detail->'drive_summary' ELSE NULL END AS drive_summary,
       detail
FROM source_events
WHERE event_id = $4
  AND ($5::bool OR park_id = ANY($6::uuid[]) OR shed_id = ANY($7::uuid[]))
LIMIT 1`

// listEventsCanonical runs the canonical read-through page for ListEvents. Params mirror the projector
// window ($1 tenant, $2 dateFrom, $3 dateToExclusive) plus the list filters ($4 owner, $5 status,
// $6 park, $7 shed, $8/$9 keyset cursor, $10 fetch limit, $11 tenantWide,
// $12/$13 park/shed scope, $14 vaccine). includeDriveSummary is carried as a
// transaction-local Postgres setting so the shared canonical CTE remains reusable by detail,
// markers, reminder, and escalation queries without growing every caller's parameter list.
func (r *Repository) listEventsCanonical(
	ctx context.Context,
	tenantID string,
	dateFrom, dateToExclusive time.Time,
	ownerKey, status, parkID, shedID, vaccine string,
	cursorDue any, cursorEventID string,
	fetchLimit int,
	tenantWide bool, parkIDs, shedIDs []string,
	includeDriveSummary bool,
) ([]domain.CalendarEvent, error) {
	type queryer interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}
	run := func(q queryer) (pgx.Rows, error) {
		return q.Query(ctx, calendarCanonicalListSQL,
			tenantID, dateFrom, dateToExclusive, ownerKey, status, parkID, shedID,
			cursorDue, cursorEventID, fetchLimit, tenantWide, parkIDs, shedIDs, vaccine)
	}
	var rows pgx.Rows
	var err error
	if includeDriveSummary {
		tx, txErr := r.pool.Begin(ctx)
		if txErr != nil {
			return nil, fmt.Errorf("calendar: begin rich canonical list: %w", txErr)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, "SET LOCAL goatos.include_drive_summary = 'true'"); err != nil {
			return nil, fmt.Errorf("calendar: enable drive summary: %w", err)
		}
		rows, err = run(tx)
	} else {
		rows, err = run(r.pool)
	}
	if err != nil {
		return nil, fmt.Errorf("calendar: canonical list events: %w", err)
	}
	defer rows.Close()
	items := []domain.CalendarEvent{}
	for rows.Next() {
		event, err := scanCalendarEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
