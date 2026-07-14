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

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
)

// calendarCanonicalListSQL is the compute-on-read canonical reconstruction of the Calendar list. It
// is deliberately a large multi-CTE query: at the 5k-to-50k scale envelope this is the accepted
// alternative to a derived read model (ADR operational-kernel-5k-50k-scale-envelope). The scale-guard
// god-cte detector is suppressed on the driving SELECT below via `scale-guard:ignore`.
// scale-guard:ignore: 5k-50k-envelope; see operational-kernel-5k-50k-scale-envelope.md
const calendarCanonicalListSQL = `WITH obligation_events AS (
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
  FROM obligation_instances oi
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
  WHERE oi.tenant_id = $1::uuid
    AND oi.batch_id IS NULL
    AND (
      (oi.due_at >= $2::timestamptz AND oi.due_at < $3::timestamptz)
      OR oi.status IN ('missed', 'in_progress', 'deferred')
      OR (oi.status IN ('scheduled', 'due') AND oi.due_at < now())
    )
    AND pd.category = 'vaccination'
    AND pv.status = 'published'
    AND oi.status NOT IN ('waived', 'canceled', 'superseded')
    AND (oi.status <> 'completed' OR oi.due_at >= now() - interval '90 days')
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
        WHEN bool_or(oi.status IN ('scheduled', 'due') AND oi.due_at < now()) THEN 'overdue'
        WHEN bool_or(oi.status = 'in_progress') THEN 'in_progress'
        WHEN bool_or(oi.status = 'deferred') THEN 'deferred'
        WHEN bool_or(oi.status = 'due') THEN 'due'
        ELSE 'scheduled'
      END AS status,
      CASE
        WHEN bool_or(oi.status = 'missed') OR bool_or(oi.due_at < now()) THEN 'critical'
        WHEN min(oi.due_at) <= now() + interval '24 hours' THEN 'warning'
        ELSE 'info'
      END AS severity
    FROM obligation_instances oi
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
    WHERE oi.tenant_id = $1::uuid
      AND oi.batch_id IS NULL
      AND (
        (oi.due_at >= $2::timestamptz AND oi.due_at < $3::timestamptz)
        OR oi.status IN ('missed', 'in_progress', 'deferred')
        OR (oi.status IN ('scheduled', 'due') AND oi.due_at < now())
      )
      AND pd.category = 'vaccination'
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
      WHEN grouped.due_at < now() AND grouped.batch_status <> 'completed' THEN 'warning'
      ELSE 'info'
    END AS severity,
    grouped.due_at,
    grouped.window_start,
    grouped.window_end,
    'Asia/Kolkata'::text AS timezone,
    'india_only'::text AS timezone_source,
    grouped.park_id,
    grouped.park_code,
    grouped.shed_id,
    grouped.shed_name,
    NULL::uuid AS cohort_id,
    NULL::text AS cohort_name,
    'shed'::text AS target_type,
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
      'drive', CASE WHEN grouped.shed_id IS NOT NULL THEN '/vaccination/execution/sheds/' || grouped.shed_id::text ELSE NULL END
    ) AS links,
    jsonb_build_object(
      'summary', jsonb_build_object(
        'owner', 'PC',
        'target_count', grouped.target_count,
        'queue_count', grouped.queue_count,
        'queue_preview', queue_meta.queue_preview,
        'shed_count', 1,
        'shed_labels', jsonb_build_array(grouped.shed_name),
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
      COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) AS due_at,
      COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) AS window_start,
      COALESCE(ob.window_end, ob.window_start + interval '8 hours', ob.planned_date::timestamptz + interval '8 hours') AS window_end,
      scope_parent.location_id AS park_id,
      scope_parent.location_code AS park_code,
      ob.scope_id AS shed_id,
      scope_loc.name AS shed_name,
      GREATEST(ob.estimated_targets, 1)::int AS target_count,
      pd.protocol_id,
      pv.protocol_version_id,
      ob.sop_task_id,
      ob.reserved_quantity,
      ob.planned_quantity,
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
    LEFT JOIN protocol_rule_dimensions prd
      ON prd.tenant_id = pr.tenant_id AND prd.rule_id = pr.rule_id
    LEFT JOIN locations scope_loc
      ON scope_loc.tenant_id = ob.tenant_id AND scope_loc.location_id = ob.scope_id
    LEFT JOIN locations scope_parent
      ON scope_parent.tenant_id = ob.tenant_id AND scope_parent.location_id = scope_loc.parent_location_id
    WHERE ob.tenant_id = $1::uuid
      AND ob.scope_type = 'shed'
      AND COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) >= $2::timestamptz
      AND COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) < $3::timestamptz
      AND pd.category = 'vaccination'
      AND pv.status = 'published'
      AND ob.status NOT IN ('superseded', 'canceled')
      AND (ob.status <> 'completed' OR COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) >= now() - interval '90 days')
    GROUP BY
      ob.batch_id,
      ob.status,
      COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end),
      COALESCE(ob.window_end, ob.window_start + interval '8 hours', ob.planned_date::timestamptz + interval '8 hours'),
      scope_parent.location_id,
      scope_parent.location_code,
      ob.scope_id,
      scope_loc.name,
      GREATEST(ob.estimated_targets, 1)::int,
      pd.protocol_id,
      pv.protocol_version_id,
      ob.sop_task_id,
      ob.reserved_quantity,
      ob.planned_quantity
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
drive_sources AS (
  SELECT * FROM batch_events
  UNION ALL
  SELECT * FROM catchup_drive_events
),
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
    sum(target_count) FILTER (WHERE source_target_type = 'batch')::int AS scheduled_count,
    sum(COALESCE((detail->'summary'->>'queue_count')::int, 0))::int AS queue_count,
    bool_or(source_target_type = 'catchup') AS has_catch_up,
    sum(target_count) FILTER (WHERE status = 'deferred')::int AS deferred_count,
    min(event_id) AS single_event_id,
    min(source_target_type) AS single_source_target_type,
    min(source_target_id::text)::uuid AS single_source_target_id,
    bool_or(status = 'completed') AND bool_and(status IN ('completed', 'canceled')) AS all_completed,
    bool_or(status = 'missed') AS has_missed,
    bool_or(status = 'in_progress') AS has_in_progress,
    bool_or(status = 'deferred') AS has_deferred,
    bool_or(status IN ('proof_pending', 'verification_pending', 'rejected', 'rework_due')) AS has_review,
    bool_or(status IN ('scheduled', 'due', 'overdue') AND due_at < now()) AS has_overdue,
    jsonb_agg(event_id ORDER BY event_id) AS source_event_ids
  FROM drive_sources
  GROUP BY park_id, to_char((due_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD')
),
obligation_drive_membership AS (
  SELECT
    oi.obligation_id,
    oi.status,
    oi.rule_id,
    pd.name AS protocol_name,
    loc.park_id,
    loc.park_code,
    loc.shed_id,
    (member.membership_at AT TIME ZONE 'Asia/Kolkata')::date AS due_date,
    CASE WHEN oi.target_type = 'goat' THEN oi.target_id END AS animal_id
    -- Stock/execution "blocked" visibility is deliberately out of scope -- stock is not a built
    -- product feature yet (owner decision 2026-07-14); revisit when the stock module ships.
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id AND ob.batch_id = oi.batch_id
  CROSS JOIN LATERAL (
    SELECT
      CASE WHEN oi.batch_id IS NOT NULL THEN ob.scope_type ELSE oi.scope_type END AS scope_type,
      CASE WHEN oi.batch_id IS NOT NULL THEN ob.scope_id ELSE oi.scope_id END AS scope_id,
      CASE
        WHEN oi.batch_id IS NOT NULL THEN COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end)
        ELSE oi.due_at
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
  LEFT JOIN LATERAL (
    SELECT
      CASE
        WHEN member.scope_type = 'park' THEN scope_loc.location_id
        WHEN member.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id
        WHEN member.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_id
      END AS park_id,
      CASE
        WHEN member.scope_type = 'park' THEN scope_loc.location_code
        WHEN member.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_code
        WHEN member.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_code
      END AS park_code,
      CASE
        WHEN member.scope_type = 'shed' THEN scope_loc.location_id
        WHEN member.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.location_id
      END AS shed_id
  ) loc ON true
  WHERE oi.tenant_id = $1::uuid
    AND pd.category = 'vaccination'
    AND pv.status = 'published'
    AND oi.status NOT IN ('superseded', 'canceled', 'waived')
    AND (
      (oi.batch_id IS NOT NULL
        AND ob.status NOT IN ('superseded', 'canceled')
        AND COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) >= $2::timestamptz
        AND COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) < $3::timestamptz
        AND (ob.status <> 'completed' OR COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) >= now() - interval '90 days'))
      OR
      (oi.batch_id IS NULL
        AND (
          (oi.due_at >= $2::timestamptz AND oi.due_at < $3::timestamptz)
          OR oi.status IN ('missed', 'in_progress', 'deferred')
          OR (oi.status IN ('scheduled', 'due') AND oi.due_at < now())
        ))
    )
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
    HAVING count(DISTINCT obligation_id) = count(DISTINCT obligation_id) FILTER (WHERE status = 'completed')
  ) done_sheds
  GROUP BY park_id, due_date
),
obligation_drive_animal_coverage AS (
  SELECT park_id, due_date,
         count(*)::int AS total_animals,
         count(*) FILTER (WHERE fully_completed)::int AS completed_animals
  FROM (
    SELECT park_id, due_date, animal_id,
           bool_and(status = 'completed') AS fully_completed
    FROM obligation_drive_membership
    WHERE animal_id IS NOT NULL
    GROUP BY park_id, due_date, animal_id
  ) per_animal
  GROUP BY park_id, due_date
),
-- projection-review: membership=obligation_drive_membership (one row per obligation_id); group_key=(park_id, due_date) from that same membership row; join_cardinality=count(DISTINCT obligation_id) FILTER per mutually-exclusive bucket (completed>deferred>overdue>due) so total_count=completed+due+overdue+deferred with no double-counting, PLUS new animal_grain aggregation (count DISTINCT target_id where target_type='goat') rolled up per animal's bool_and(status='completed') per (park_id, due_date); animal_coverage_cardinality=separate animal subquery 1:1 LEFT JOIN on (park_id, due_date) produces animal_total_count and animal_completed_count, no fan-out; pagination=materialized EXACTLY ONCE per RefreshVaccinationProjection call into calendar_drive_summary_tmp before the keyset page loop (unchanged by animal coverage); scope=(park_id, due_date) identical to membership's scope matrix, no re-derivation (unchanged by animal coverage); date/status: all existing dimension semantics preserved (animal counts are independent new grain, do not affect obligation buckets or date-window/status-bucket logic)
obligation_drive_summary AS (
  -- Bucket precedence is mutually exclusive and total_count-complete. Invariant:
  --   total_count = completed_count + due_count + overdue_count + deferred_count
  -- Stock/execution "blocked" visibility is deliberately out of scope -- stock is not a built
  -- product feature yet (owner decision 2026-07-14); an obligation on a stock-blocked batch is
  -- bucketed purely by its own status. Revisit when the stock module ships.
  -- Precedence (each obligation counted in EXACTLY ONE bucket):
  --   completed = status='completed'
  --   deferred  = NOT completed AND status='deferred' (a clinical/anchor hold stays deferred)
  --   overdue   = NOT completed AND NOT deferred AND status IN ('overdue','missed')
  --   due       = everything else open, not already bucketed
  SELECT
    m.park_id,
    m.due_date,
    count(DISTINCT m.obligation_id) FILTER (WHERE m.status IN (
      'completed', 'scheduled', 'due', 'in_progress', 'proof_pending',
      'verification_pending', 'rejected', 'rework_due', 'overdue', 'missed',
      'deferred'))::int AS total_count,
    count(DISTINCT m.obligation_id) FILTER (WHERE m.status = 'completed')::int AS completed_count,
    count(DISTINCT m.obligation_id) FILTER (
      WHERE m.status <> 'completed'
        AND m.status <> 'deferred'
        AND m.status IN ('scheduled', 'due', 'in_progress', 'proof_pending', 'verification_pending', 'rejected', 'rework_due')
    )::int AS due_count,
    count(DISTINCT m.obligation_id) FILTER (
      WHERE m.status <> 'completed'
        AND m.status <> 'deferred'
        AND m.status IN ('overdue', 'missed')
    )::int AS overdue_count,
    count(DISTINCT m.obligation_id) FILTER (WHERE m.status = 'deferred')::int AS deferred_count,
    count(DISTINCT m.shed_id) FILTER (WHERE m.shed_id IS NOT NULL)::int AS shed_count,
    COALESCE(max(sc.sheds_completed), 0)::int AS sheds_completed,
    COALESCE(max(ac.total_animals), 0)::int AS total_animals,
    COALESCE(max(ac.completed_animals), 0)::int AS completed_animals,
    COALESCE(max(vl.vaccine_labels), ARRAY[]::text[]) AS vaccine_labels,
    max(m.park_code) AS park_code
  FROM obligation_drive_membership m
  LEFT JOIN obligation_drive_shed_complete sc
    ON sc.park_id IS NOT DISTINCT FROM m.park_id AND sc.due_date = m.due_date
  LEFT JOIN obligation_drive_animal_coverage ac
    ON ac.park_id IS NOT DISTINCT FROM m.park_id AND ac.due_date = m.due_date
  LEFT JOIN obligation_drive_vaccine_labels vl
    ON vl.park_id IS NOT DISTINCT FROM m.park_id AND vl.due_date = m.due_date
  GROUP BY m.park_id, m.due_date
),
park_drive_events AS (
  SELECT
    CASE
      WHEN grouped.drive_count = 1 THEN grouped.single_event_id
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
      WHEN grouped.has_missed THEN 'missed'
      WHEN grouped.has_review THEN 'verification_pending'
      WHEN grouped.has_overdue THEN 'overdue'
      WHEN grouped.has_in_progress THEN 'in_progress'
      WHEN grouped.has_deferred THEN 'deferred'
      WHEN grouped.all_completed THEN 'completed'
      ELSE 'scheduled'
    END AS status,
    CASE
      WHEN grouped.has_missed OR grouped.has_overdue THEN 'critical'
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
    NULL::uuid AS shed_id,
    NULL::text AS shed_name,
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
    'PC drive team'::text AS assignee_label,
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
        'summary_primary', grouped.target_count::text || CASE WHEN grouped.target_count = 1 THEN ' scheduled dose' ELSE ' scheduled doses' END,
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
        'scheduled_count', COALESCE(grouped.scheduled_count, 0),
        'queue_count', grouped.queue_count,
        'deferred_count', COALESCE(grouped.deferred_count, 0),
        'review_count', CASE WHEN grouped.has_review THEN 1 ELSE 0 END,
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
      'drive_summary', CASE WHEN obl_summary.total_count > 0 THEN jsonb_build_object(
        'park_name', COALESCE(obl_summary.park_code, grouped.park_code, 'Vaccination drive'),
        'due_date', grouped.due_day,
        'shed_count', obl_summary.shed_count,
        'sheds_completed', obl_summary.sheds_completed,
        'vaccine_labels', to_jsonb(obl_summary.vaccine_labels),
        'total_count', obl_summary.total_count,
        'completed_count', obl_summary.completed_count,
        'remaining_count', obl_summary.total_count - obl_summary.completed_count,
        'due_count', obl_summary.due_count,
        'overdue_count', obl_summary.overdue_count,
        'deferred_count', obl_summary.deferred_count,
        'total_animals', obl_summary.total_animals,
        'completed_animals', obl_summary.completed_animals,
        'owner_label', 'PC'
      ) ELSE NULL END
    ) AS detail
  FROM park_drive_groups grouped
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
  SELECT * FROM park_drive_events
  UNION ALL
  SELECT * FROM sop_events
  UNION ALL
  SELECT * FROM config_events
),
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
         CASE WHEN jsonb_typeof(detail->'drive_summary') = 'object' THEN detail->'drive_summary' ELSE NULL END AS drive_summary
  FROM source_events
  WHERE due_at IS NOT NULL
    AND event_type <> 'vaccination_dose_due'
    AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending',
                   'verification_pending', 'rejected', 'rework_due', 'deferred', 'blocked', 'completed')
    AND due_at >= $2::timestamptz AND due_at < $3::timestamptz
    AND ($4::text = '' OR owner_key = $4::text)
    AND ($5::text = '' OR status = $5::text)
    AND ($5::text <> '' OR status NOT IN ('completed', 'canceled'))
    AND ($6::text = '' OR park_id::text = nullif($6::text, ''))
    AND ($7::text = '' OR shed_id::text = nullif($7::text, ''))
    AND ($8::timestamptz IS NULL OR (due_at, event_id) > ($8::timestamptz, $9::text))
    AND ($11::bool OR park_id::text = ANY($12::text[]) OR shed_id::text = ANY($13::text[]))
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

// listEventsCanonical runs the canonical read-through page for ListEvents. Params mirror the projector
// window ($1 tenant, $2 dateFrom, $3 dateToExclusive) plus the list filters ($4 owner, $5 status,
// $6 park, $7 shed, $8/$9 keyset cursor, $10 fetch limit, $11 tenantWide, $12/$13 park/shed scope).
func (r *Repository) listEventsCanonical(
	ctx context.Context,
	tenantID string,
	dateFrom, dateToExclusive time.Time,
	ownerKey, status, parkID, shedID string,
	cursorDue any, cursorEventID string,
	fetchLimit int,
	tenantWide bool, parkIDs, shedIDs []string,
) ([]domain.CalendarEvent, error) {
	rows, err := r.pool.Query(ctx, calendarCanonicalListSQL,
		tenantID, dateFrom, dateToExclusive, ownerKey, status, parkID, shedID,
		cursorDue, cursorEventID, fetchLimit, tenantWide, parkIDs, shedIDs)
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
