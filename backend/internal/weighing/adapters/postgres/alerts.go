package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// alertCursor is the keyset over the feed's own order: newest first, broken by
// the request id so two alerts stamped in the same instant still page stably.
type alertCursor struct {
	OccurredAt time.Time `json:"occurred_at"`
	AlertID    string    `json:"alert_id"`
}

func encodeAlertCursor(cursor alertCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeAlertCursor(value string) (alertCursor, error) {
	if strings.TrimSpace(value) == "" {
		return alertCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return alertCursor{}, ports.ErrInvalidArgument
	}
	var cursor alertCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return alertCursor{}, ports.ErrInvalidArgument
	}
	if cursor.AlertID == "" || cursor.OccurredAt.IsZero() {
		return alertCursor{}, ports.ErrInvalidArgument
	}
	return cursor, nil
}

// listAlertsSQL reads the weighing lifecycle notifications already routed to one
// person.
//
// MODULE DISCRIMINATOR: context->>'message_key' LIKE 'weighing.%'. Every weighing
// producer stamps it -- the lifecycle consumer's own keys
// ("weighing.assigned_operator", "weighing.shed_submitted",
// "weighing.shed_reopened", "weighing.verdict.rework", "weighing.shed_closed",
// "weighing.campaign_closed", "weighing.work_item.*") and the shared verification
// consumer's weighing PROFILE, whose messageKeyPrefix is "weighing"
// ("weighing.proof.pending.leadership", "weighing.proof.rework", ...). It is a
// single predicate over one jsonb field, and it is the reason this feed needs no
// vaccination/obligation table at all: the vaccination rows carry
// "vaccination.*" and are simply not selected.
//
// IDENTITY: context->>'member_id' is the canonical workforce_member_id the
// consumer resolved before writing, so filtering on it IS the audience check.
// An operator can never see another operator's bucket here because they were
// never a recipient of it.
//
// DEDUPE: QueueRoleNotifications writes one row per recipient DEVICE, so a
// two-phone operator has two rows for one transition. Only the newest row per
// producer event_key is kept (see the mine CTE), collapsing them back to one alert.
//
// BOUNDED: the rolling AlertRetentionDays window plus the per-member equality
// predicate keep this off a full-history scan; both are served by
// notification_requests_weighing_alerts_idx (migration 000083).
//
// scale-guard:ignore: workforce-scale read -- one person's own weighing notifications inside a fixed 30-day window, keyset-paginated. Never herd-scale: notification_requests rows are produced per work-state transition per recipient device, not per animal.
// scale-guard:plan-proof-exempt: PENDING at-scale plan test (docs/progress/plan-proof-backlog.md); measured <500ms on STG-size data, not yet proven at 500k rows.
const listAlertsSQL = `
WITH me AS (
  SELECT COALESCE(
    (SELECT wm.workforce_member_id
       FROM workforce_members wm
      WHERE wm.tenant_id = $1::uuid
        AND wm.workforce_member_id = $2::uuid
        AND wm.status = 'active'),
    (SELECT wm.workforce_member_id
       FROM workforce_members wm
      WHERE wm.tenant_id = $1::uuid
        AND wm.user_id = $2::uuid
        AND wm.status = 'active')
  ) AS workforce_member_id
),
mine AS (
  -- DEDUPE without sorting the whole window: a row survives when no NEWER candidate row (same
  -- predicates, compared on (requested_at, notification_request_id) -- the DISTINCT ON tie-break
  -- this replaced) carries its event_key, which is exactly "the newest row of its event_key
  -- group" (NULL keys form one group, as DISTINCT ON grouped them). The outer keyset walk then
  -- stops after the page instead of deduping 30 days of one leader's per-device rows first
  -- (16k rows on STG: 300-900 ms -> ~15-40 ms, full result sets and cursor pages identical).
  -- The probe spells the module predicate with starts_with (identical to LIKE 'weighing.%': '.'
  -- is literal in LIKE) so it is served by notification_requests_member_dedupe_idx, whose key
  -- carries the event_key, instead of re-walking every newer row of the partial feed index.
  SELECT nr.notification_request_id, nr.requested_at, nr.title, nr.body, nr.context
  FROM me, notification_requests nr
  WHERE nr.tenant_id = $1::uuid
    AND me.workforce_member_id IS NOT NULL
    AND nr.context->>'member_id' = me.workforce_member_id::text
    AND nr.context->>'message_key' LIKE 'weighing.%'
    AND nr.requested_at >= now() - ($3::int * INTERVAL '1 day')
    AND ($4::bool OR nr.context->>'park_id' = ANY($5::text[]))
    AND CASE WHEN COALESCE(nr.context->>'event_key', '') <> '' THEN NOT EXISTS (
          SELECT 1 FROM notification_requests n2
          WHERE COALESCE(NULLIF(n2.context->>'event_key', ''), n2.notification_request_id::text)
                  = nr.context->>'event_key'
          AND n2.context->>'event_key' = nr.context->>'event_key'
          AND n2.tenant_id = $1::uuid
          AND n2.context->>'member_id' = me.workforce_member_id::text
          AND starts_with(n2.context->>'message_key', 'weighing.')
          AND n2.requested_at >= now() - ($3::int * INTERVAL '1 day')
          AND ($4::bool OR n2.context->>'park_id' = ANY($5::text[]))
          AND n2.requested_at >= nr.requested_at
          AND (n2.requested_at, n2.notification_request_id) > (nr.requested_at, nr.notification_request_id))
        ELSE NOT EXISTS (
          SELECT 1 FROM notification_requests n2
          WHERE n2.context->>'event_key' IS NOT DISTINCT FROM nr.context->>'event_key'
          AND n2.tenant_id = $1::uuid
          AND n2.context->>'member_id' = me.workforce_member_id::text
          AND starts_with(n2.context->>'message_key', 'weighing.')
          AND n2.requested_at >= now() - ($3::int * INTERVAL '1 day')
          AND ($4::bool OR n2.context->>'park_id' = ANY($5::text[]))
          AND n2.requested_at >= nr.requested_at
          AND (n2.requested_at, n2.notification_request_id) > (nr.requested_at, nr.notification_request_id))
        END
)
SELECT
  mine.notification_request_id::text,
  mine.requested_at,
  mine.title,
  mine.body,
  COALESCE(mine.context->>'type', ''),
  COALESCE(mine.context->>'role', ''),
  COALESCE(mine.context->>'priority', ''),
  COALESCE(mine.context->>'target', ''),
  COALESCE(mine.context->>'shed_label', mine.context->>'shed_labels', mine.context->>'shed_list', ''),
  COALESCE(mine.context->>'campaign_id', ''),
  COALESCE(mine.context->>'park_id', '')
FROM mine
WHERE $6::timestamptz IS NULL
   OR (mine.requested_at, mine.notification_request_id)
      < ($6::timestamptz, $7::uuid)
ORDER BY mine.requested_at DESC, mine.notification_request_id DESC
LIMIT $8`

// ListAlerts returns one keyset page of the caller's own weighing alerts.
func (r *Repository) ListAlerts(
	ctx context.Context,
	tenantID, memberOrUserID string,
	tenantWide bool,
	parkIDs []string,
	cursor string,
	limit int,
) (domain.AlertPage, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	decoded, err := decodeAlertCursor(cursor)
	if err != nil {
		return domain.AlertPage{}, err
	}
	var cursorAt, cursorID any
	if decoded.AlertID != "" {
		cursorAt = decoded.OccurredAt
		cursorID = decoded.AlertID
	}
	if parkIDs == nil {
		parkIDs = []string{}
	}

	// One extra row decides whether a next page exists without a second count query.
	rows, err := r.pool.Query(ctx, listAlertsSQL,
		tenantID, memberOrUserID, domain.AlertRetentionDays,
		tenantWide, parkIDs, cursorAt, cursorID, limit+1,
	)
	if err != nil {
		return domain.AlertPage{}, fmt.Errorf("weighing: list alerts: %w", err)
	}
	defer rows.Close()

	page := domain.AlertPage{
		Items:        make([]domain.Alert, 0, limit),
		Title:        domain.AlertFeedTitle,
		EmptyMessage: domain.AlertFeedEmptyMessage,
	}
	for rows.Next() {
		var (
			alert    domain.Alert
			role     string
			priority string
		)
		if err := rows.Scan(
			&alert.AlertID, &alert.OccurredAt, &alert.Title, &alert.Body,
			&alert.Kind, &role, &priority, &alert.Target,
			&alert.ShedLabel, &alert.CampaignID, &alert.ParkID,
		); err != nil {
			return domain.AlertPage{}, fmt.Errorf("weighing: scan alert: %w", err)
		}
		alert.Direction = alertDirectionForRole(role)
		alert.Severity = alertSeverityForPriority(priority)
		if strings.TrimSpace(alert.Target) == "" {
			alert.Target = "/weighing"
		}
		page.Items = append(page.Items, alert)
	}
	if err := rows.Err(); err != nil {
		return domain.AlertPage{}, fmt.Errorf("weighing: list alerts rows: %w", err)
	}

	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = encodeAlertCursor(alertCursor{OccurredAt: last.OccurredAt, AlertID: last.AlertID})
	}
	return page, nil
}

// alertDirectionForRole reads the direction off the RECIPIENT ROLE the producing
// consumer stamped, not off the event type. The same transition can travel both
// ways at once (a rework goes down to the operator and up to the director), and
// each recipient's own row must say which way it went for THEM.
func alertDirectionForRole(role string) string {
	if strings.TrimSpace(role) == "operator" {
		return domain.AlertDirectionDownstream
	}
	return domain.AlertDirectionUpstream
}

func alertSeverityForPriority(priority string) string {
	if strings.EqualFold(strings.TrimSpace(priority), "high") {
		return domain.AlertSeverityHigh
	}
	return domain.AlertSeverityNormal
}
