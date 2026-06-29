// Package postgres implements Calendar read/action persistence.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

const defaultQueryTimeout = 3 * time.Second

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

func (r *Repository) ListEvents(ctx context.Context, q domain.Query) (domain.CalendarEventListResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	fetchLimit := limit + 1
	ownerKey := q.OwnerKey
	if ownerKey == domain.OwnerAll {
		ownerKey = ""
	}
	status := ""
	if q.Status != nil {
		status = *q.Status
	}
	parkID := ""
	if q.ParkID != nil {
		parkID = *q.ParkID
	}
	shedID := ""
	if q.ShedID != nil {
		shedID = *q.ShedID
	}
	var cursorDue any
	cursorEventID := ""
	if q.Cursor != nil {
		cursorDue = q.Cursor.DueAt
		cursorEventID = q.Cursor.EventID
	}
	tenantWide, parkIDs, shedIDs := scopeArgs(q.Scope)
	rows, err := r.pool.Query(ctx, calendarListSQL,
		q.TenantID, ownerKey, status, parkID, shedID, q.DateFrom, q.DateTo.Add(24*time.Hour), cursorDue, cursorEventID, fetchLimit,
		tenantWide, parkIDs, shedIDs)
	if err != nil {
		return domain.CalendarEventListResponse{}, fmt.Errorf("calendar: list events: %w", err)
	}
	defer rows.Close()
	items := []domain.CalendarEvent{}
	for rows.Next() {
		event, err := scanCalendarEvent(rows)
		if err != nil {
			return domain.CalendarEventListResponse{}, err
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return domain.CalendarEventListResponse{}, err
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		cursor, err := domain.EncodeCalendarCursor(domain.CalendarCursor{DueAt: last.DueAt, EventID: last.EventID})
		if err != nil {
			return domain.CalendarEventListResponse{}, err
		}
		next = &cursor
	}
	return domain.CalendarEventListResponse{Source: domain.SourceAPI, Items: items, NextCursor: next}, nil
}

func (r *Repository) GetEventDetail(ctx context.Context, q domain.EventQuery) (domain.CalendarEventDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var detailRaw, linksRaw []byte
	tenantWide, parkIDs, shedIDs := scopeArgs(q.Scope)
	event, err := scanCalendarEventWithDetail(r.pool.QueryRow(ctx, calendarDetailSQL, q.TenantID, q.EventID, tenantWide, parkIDs, shedIDs), &detailRaw, &linksRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CalendarEventDetail{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.CalendarEventDetail{}, fmt.Errorf("calendar: get detail: %w", err)
	}
	blocks := decodeDetailBlocks(detailRaw)
	recent, err := r.History(ctx, domain.HistoryQuery{TenantID: q.TenantID, EventID: q.EventID, Limit: 10, Scope: q.Scope})
	if err != nil {
		return domain.CalendarEventDetail{}, err
	}
	links := linksRaw
	if detailLinks, ok := blocks["links"]; ok && len(detailLinks) > 0 && string(detailLinks) != "null" {
		links = detailLinks
	}
	return domain.CalendarEventDetail{
		Event:                event,
		Summary:              blocks.raw("summary"),
		SourceAndRule:        blocks.raw("source_and_rule"),
		Execution:            blocks.raw("execution"),
		Stock:                blocks.raw("stock"),
		Proof:                blocks.raw("proof"),
		Verification:         blocks.raw("verification"),
		NotificationChannels: blocks.stringList("notification_channels"),
		NotificationPolicy:   blocks.raw("notification_policy"),
		Links:                rawOrObject(links, nil),
		RecentActions:        recent.Items,
	}, nil
}

func (r *Repository) History(ctx context.Context, q domain.HistoryQuery) (domain.CalendarHistoryResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := r.eventExists(ctx, q.TenantID, q.EventID, q.Scope); err != nil {
		return domain.CalendarHistoryResponse{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	fetchLimit := limit + 1
	var cursorAt any
	cursorID := ""
	if q.Cursor != nil {
		cursorAt = q.Cursor.OccurredAt
		cursorID = q.Cursor.HistoryID
	}
	rows, err := r.pool.Query(ctx, calendarHistorySQL, q.TenantID, q.EventID, cursorAt, cursorID, fetchLimit)
	if err != nil {
		return domain.CalendarHistoryResponse{}, fmt.Errorf("calendar: history: %w", err)
	}
	defer rows.Close()
	items := []domain.CalendarHistoryItem{}
	for rows.Next() {
		item, err := scanHistoryItem(rows)
		if err != nil {
			return domain.CalendarHistoryResponse{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.CalendarHistoryResponse{}, err
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		cursor, err := domain.EncodeHistoryCursor(domain.HistoryCursor{OccurredAt: last.OccurredAt, HistoryID: last.HistoryID})
		if err != nil {
			return domain.CalendarHistoryResponse{}, err
		}
		next = &cursor
	}
	return domain.CalendarHistoryResponse{Source: domain.SourceAPI, EventID: q.EventID, Items: items, NextCursor: next}, nil
}

func (r *Repository) SendNudge(ctx context.Context, in ports.SendNudge) (domain.CalendarActionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "calendar.nudge"
	target, err := loadActionTarget(ctx, tx, in.TenantID, in.EventID, in.Scope)
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	fingerprint := requestFingerprint(in.TenantID, in.EventID, in.ActorID, in.Channel, in.Message, in.Reason)
	res, err := reserveIdempotency(ctx, tx, in.TenantID, scope, in.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if !res.proceed {
		_ = tx.Rollback(ctx)
		action, err := r.notificationActionByID(ctx, in.TenantID, in.EventID, res.resultID)
		action.IdempotentReplay = true
		return action, err
	}
	channel := normalizeChannel(in.Channel, target.PrimaryChannel)
	body := in.Message
	if strings.TrimSpace(body) == "" {
		body = "Calendar nudge for " + target.Title
	}
	scopedKey := idemScopedKey(in.TenantID, scope, in.IdempotencyKey)
	var requestID string
	contextJSON, _ := json.Marshal(map[string]any{
		"calendar_event_id":  in.EventID,
		"reason":             in.Reason,
		"source_target_type": target.TargetType,
		"source_target_id":   target.TargetID,
	})
	if err := tx.QueryRow(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  recipient_ref, title, body, status, requested_by, idempotency_key, request_fingerprint,
  context, trace_id
) VALUES (
  $1::uuid, $2, $3, nullif($4::text, '')::uuid, 'nudge', $5,
  nullif($6, ''), $7, $8, 'queued', $9::uuid, $10, $11,
  $12::jsonb, nullif($13, '')
)
RETURNING notification_request_id::text`,
		in.TenantID, in.EventID, target.TargetType, target.TargetID, channel,
		target.RecipientRef, "Nudge: "+target.Title, body, in.ActorID, scopedKey, fingerprint, contextJSON, in.TraceID).Scan(&requestID); err != nil {
		return domain.CalendarActionResponse{}, fmt.Errorf("calendar: insert nudge notification: %w", err)
	}
	response := domain.CalendarActionResponse{
		ActionID:   requestID,
		EventID:    in.EventID,
		ActionType: "nudge",
		Status:     "queued",
		Channel:    &channel,
	}
	if err := insertOutbox(ctx, tx, in.TenantID, "calendar.nudge.requested", "calendar.notification.v1",
		"calendar_notification", requestID, "calendar.notifications", scopedKey, in.TraceID, "human", in.ActorID, response, in.EventID); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      in.ActorID,
		ActorType:    "human",
		Action:       "calendar.nudge.sent",
		ResourceType: "calendar_notification",
		ResourceID:   requestID,
		ScopeType:    "notification_request",
		ScopeID:      requestID,
		AfterState:   response,
		TraceID:      in.TraceID,
		Metadata: map[string]any{
			"domain":            "calendar",
			"module":            "vaccination",
			"category":          "nudge",
			"calendar_event_id": in.EventID,
			"idempotency_key":   in.IdempotencyKey,
			"status":            "queued",
			"result":            "queued",
			"channel":           channel,
		},
	}); err != nil {
		return domain.CalendarActionResponse{}, fmt.Errorf("calendar: audit nudge: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE calendar_event_projections
SET reminder_state = 'nudged',
    primary_notification_channel = $3,
    updated_at = now()
WHERE tenant_id = $1::uuid AND event_id = $2`, in.TenantID, in.EventID, channel); err != nil {
		return domain.CalendarActionResponse{}, fmt.Errorf("calendar: update nudge projection: %w", err)
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, scope, in.IdempotencyKey, "notification_request", requestID); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	return response, nil
}

func (r *Repository) Snooze(ctx context.Context, in ports.Snooze) (domain.CalendarActionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "calendar.snooze"
	target, err := loadActionTarget(ctx, tx, in.TenantID, in.EventID, in.Scope)
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	fingerprint := requestFingerprint(in.TenantID, in.EventID, in.ActorID, fpTime(in.SnoozeUntil), in.Reason, fmt.Sprintf("%t", in.ReplaceExisting))
	res, err := reserveIdempotency(ctx, tx, in.TenantID, scope, in.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if !res.proceed {
		_ = tx.Rollback(ctx)
		action, err := r.snoozeActionByID(ctx, in.TenantID, in.EventID, res.resultID)
		action.IdempotentReplay = true
		return action, err
	}
	activeIDs, err := activeSnoozeIDs(ctx, tx, in.TenantID, in.EventID)
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if len(activeIDs) > 0 && !in.ReplaceExisting {
		return domain.CalendarActionResponse{}, ports.ErrActiveSnoozeExists
	}
	scopedKey := idemScopedKey(in.TenantID, scope, in.IdempotencyKey)
	var snoozeID string
	contextJSON, _ := json.Marshal(map[string]any{
		"calendar_event_id":  in.EventID,
		"source_target_type": target.TargetType,
		"source_target_id":   target.TargetID,
	})
	if err := tx.QueryRow(ctx, `
INSERT INTO calendar_snoozes (
  tenant_id, calendar_event_id, target_type, target_id, snooze_until, reason,
  status, created_by, idempotency_key, request_fingerprint, context, trace_id
) VALUES (
  $1::uuid, $2, $3, nullif($4::text, '')::uuid, $5::timestamptz, $6,
  'active', $7::uuid, $8, $9, $10::jsonb, nullif($11, '')
)
RETURNING snooze_id::text`,
		in.TenantID, in.EventID, target.TargetType, target.TargetID, in.SnoozeUntil.UTC(),
		in.Reason, in.ActorID, scopedKey, fingerprint, contextJSON, in.TraceID).Scan(&snoozeID); err != nil {
		return domain.CalendarActionResponse{}, fmt.Errorf("calendar: insert snooze: %w", err)
	}
	if len(activeIDs) > 0 {
		if _, err := tx.Exec(ctx, `
UPDATE calendar_snoozes
SET status = 'replaced', replaced_by_snooze_id = $3::uuid
WHERE tenant_id = $1::uuid AND calendar_event_id = $2 AND status = 'active' AND snooze_id <> $3::uuid`,
			in.TenantID, in.EventID, snoozeID); err != nil {
			return domain.CalendarActionResponse{}, fmt.Errorf("calendar: replace snooze: %w", err)
		}
	}
	response := domain.CalendarActionResponse{
		ActionID:    snoozeID,
		EventID:     in.EventID,
		ActionType:  "snooze",
		Status:      "active",
		SnoozeUntil: &in.SnoozeUntil,
	}
	if err := insertOutbox(ctx, tx, in.TenantID, "calendar.snooze.recorded", "calendar.snooze.v1",
		"calendar_snooze", snoozeID, "calendar.notifications", scopedKey, in.TraceID, "human", in.ActorID, response, in.EventID); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      in.ActorID,
		ActorType:    "human",
		Action:       "calendar.snooze.recorded",
		ResourceType: "calendar_snooze",
		ResourceID:   snoozeID,
		ScopeType:    "calendar_snooze",
		ScopeID:      snoozeID,
		AfterState:   response,
		TraceID:      in.TraceID,
		Metadata: map[string]any{
			"domain":            "calendar",
			"module":            "vaccination",
			"category":          "snooze",
			"calendar_event_id": in.EventID,
			"idempotency_key":   in.IdempotencyKey,
			"status":            "active",
			"result":            "active",
			"snooze_until":      in.SnoozeUntil.UTC().Format(time.RFC3339Nano),
			"replace_existing":  in.ReplaceExisting,
		},
	}); err != nil {
		return domain.CalendarActionResponse{}, fmt.Errorf("calendar: audit snooze: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE calendar_event_projections
SET reminder_state = 'snoozed',
    updated_at = now()
WHERE tenant_id = $1::uuid AND event_id = $2`, in.TenantID, in.EventID); err != nil {
		return domain.CalendarActionResponse{}, fmt.Errorf("calendar: update snooze projection: %w", err)
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, scope, in.IdempotencyKey, "calendar_snooze", snoozeID); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	return response, nil
}

func (r *Repository) AcknowledgeEscalation(ctx context.Context, in ports.AcknowledgeEscalation) (domain.CalendarActionResponse, error) {
	return r.applyEscalationAction(ctx, escalationActionInput{
		TenantID:        in.TenantID,
		EventID:         in.EventID,
		ActorID:         in.ActorID,
		TraceID:         in.TraceID,
		IdempotencyKey:  in.IdempotencyKey,
		Reason:          in.Reason,
		Scope:           in.Scope,
		ActionType:      "acknowledge_escalation",
		ScopeName:       "calendar.escalation.acknowledge",
		EventType:       "calendar.escalation.acknowledged",
		StatusEventType: "escalation_acknowledged",
		ResultStatus:    "acknowledged",
		ActorGrants:     in.ActorGrants,
	})
}

func (r *Repository) ResolveEscalation(ctx context.Context, in ports.ResolveEscalation) (domain.CalendarActionResponse, error) {
	return r.applyEscalationAction(ctx, escalationActionInput{
		TenantID:        in.TenantID,
		EventID:         in.EventID,
		ActorID:         in.ActorID,
		TraceID:         in.TraceID,
		IdempotencyKey:  in.IdempotencyKey,
		Reason:          in.Reason,
		Scope:           in.Scope,
		ActionType:      "resolve_escalation",
		ScopeName:       "calendar.escalation.resolve",
		EventType:       "calendar.escalation.resolved",
		StatusEventType: "escalation_resolved",
		ResultStatus:    "resolved",
		ActorGrants:     in.ActorGrants,
	})
}

type escalationActionInput struct {
	TenantID        string
	EventID         string
	ActorID         string
	TraceID         string
	IdempotencyKey  string
	Reason          string
	Scope           domain.ScopeFilter
	ActionType      string
	ScopeName       string
	EventType       string
	StatusEventType string
	ResultStatus    string
	ActorGrants     []ports.ActorGrant
}

type lockedEscalation struct {
	EscalationID string
	ObligationID string
	Level        int
	Role         string
	Status       string
}

type escalationClosure struct {
	IDs    []string
	Levels []int
}

func (r *Repository) applyEscalationAction(ctx context.Context, in escalationActionInput) (domain.CalendarActionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	target, err := loadActionTarget(ctx, tx, in.TenantID, in.EventID, in.Scope)
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if target.TargetType != "obligation" || strings.TrimSpace(target.TargetID) == "" {
		return domain.CalendarActionResponse{}, ports.ErrEventNotActionable
	}
	fingerprint := requestFingerprint(in.TenantID, in.EventID, in.ActorID, in.ActionType, in.Reason)
	res, err := reserveIdempotency(ctx, tx, in.TenantID, in.ScopeName, in.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if !res.proceed {
		_ = tx.Rollback(ctx)
		action, err := r.escalationActionByID(ctx, in.TenantID, in.EventID, res.resultID, in.ActionType)
		action.IdempotentReplay = true
		return action, err
	}
	esc, err := lockLatestEscalation(ctx, tx, in.TenantID, target.TargetID)
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if !actorCanActionEscalation(in.ActorGrants, in.TenantID, target, esc.Role) {
		return domain.CalendarActionResponse{}, ports.ErrForbidden
	}
	changed, resultStatus, err := updateEscalationState(ctx, tx, in, esc)
	if err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if !changed && in.ActionType == "resolve_escalation" {
		return domain.CalendarActionResponse{}, ports.ErrEventNotActionable
	}
	closure := escalationClosure{}
	if in.ActionType == "resolve_escalation" && changed {
		closure, err = resolveActiveEscalations(ctx, tx, in, esc)
		if err != nil {
			return domain.CalendarActionResponse{}, err
		}
	}
	response := domain.CalendarActionResponse{
		ActionID:   esc.EscalationID,
		EventID:    in.EventID,
		ActionType: in.ActionType,
		Status:     resultStatus,
	}
	if changed {
		scopedKey := idemScopedKey(in.TenantID, in.ScopeName, in.IdempotencyKey)
		if err := recordEscalationActionSideEffects(ctx, tx, in, target, esc, closure, response, scopedKey); err != nil {
			return domain.CalendarActionResponse{}, err
		}
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, in.ScopeName, in.IdempotencyKey, "obligation_escalation", esc.EscalationID); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	return response, nil
}

func lockLatestEscalation(ctx context.Context, tx pgx.Tx, tenantID, obligationID string) (lockedEscalation, error) {
	var esc lockedEscalation
	if err := tx.QueryRow(ctx, `
SELECT escalation_id::text, obligation_id::text, level, COALESCE(escalated_to_role, ''), status
FROM obligation_escalations
WHERE tenant_id = $1::uuid
  AND obligation_id = $2::uuid
  AND status IN ('open', 'acknowledged')
ORDER BY level DESC, opened_at DESC, escalation_id DESC
LIMIT 1
FOR UPDATE`, tenantID, obligationID).Scan(&esc.EscalationID, &esc.ObligationID, &esc.Level, &esc.Role, &esc.Status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lockedEscalation{}, ports.ErrEventNotActionable
		}
		return lockedEscalation{}, err
	}
	return esc, nil
}

func updateEscalationState(ctx context.Context, tx pgx.Tx, in escalationActionInput, esc lockedEscalation) (bool, string, error) {
	switch in.ActionType {
	case "acknowledge_escalation":
		if esc.Status == "acknowledged" {
			return false, "acknowledged", nil
		}
		tag, err := tx.Exec(ctx, `
UPDATE obligation_escalations
SET status = 'acknowledged',
    acknowledged_at = $4::timestamptz,
    acknowledged_by = $3::uuid,
    acknowledgement_note = $5
WHERE tenant_id = $1::uuid
  AND escalation_id = $2::uuid
  AND status = 'open'`, in.TenantID, esc.EscalationID, in.ActorID, time.Now().UTC(), in.Reason)
		return tag.RowsAffected() > 0, "acknowledged", err
	case "resolve_escalation":
		tag, err := tx.Exec(ctx, `
UPDATE obligation_escalations
SET status = 'resolved',
    resolved_at = $4::timestamptz,
    resolved_by = $3::uuid,
    resolution_note = $5
WHERE tenant_id = $1::uuid
  AND escalation_id = $2::uuid
  AND status IN ('open', 'acknowledged')`, in.TenantID, esc.EscalationID, in.ActorID, time.Now().UTC(), in.Reason)
		return tag.RowsAffected() > 0, "resolved", err
	default:
		return false, "", ports.ErrEventNotActionable
	}
}

func resolveActiveEscalations(ctx context.Context, tx pgx.Tx, in escalationActionInput, esc lockedEscalation) (escalationClosure, error) {
	closure := escalationClosure{
		IDs:    []string{esc.EscalationID},
		Levels: []int{esc.Level},
	}
	rows, err := tx.Query(ctx, `
UPDATE obligation_escalations
SET status = 'resolved',
    resolved_at = COALESCE(resolved_at, $4::timestamptz),
    resolved_by = COALESCE(resolved_by, $3::uuid),
    resolution_note = CASE WHEN resolution_note = '' THEN $5 ELSE resolution_note END
WHERE tenant_id = $1::uuid
  AND obligation_id = $2::uuid
  AND status IN ('open', 'acknowledged')
RETURNING escalation_id::text, level`, in.TenantID, esc.ObligationID, in.ActorID, time.Now().UTC(), in.Reason)
	if err != nil {
		return escalationClosure{}, fmt.Errorf("calendar: resolve active escalation ladder: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var level int
		if err := rows.Scan(&id, &level); err != nil {
			return escalationClosure{}, err
		}
		closure.IDs = append(closure.IDs, id)
		closure.Levels = append(closure.Levels, level)
	}
	if err := rows.Err(); err != nil {
		return escalationClosure{}, err
	}
	return closure, nil
}

func recordEscalationActionSideEffects(ctx context.Context, tx pgx.Tx, in escalationActionInput, target actionTarget, esc lockedEscalation, closure escalationClosure, response domain.CalendarActionResponse, scopedKey string) error {
	now := time.Now().UTC()
	projectionState := "resolved"
	if in.ActionType == "acknowledge_escalation" {
		projectionState = fmt.Sprintf("level_%d_acknowledged", esc.Level)
	}
	if _, err := tx.Exec(ctx, `
UPDATE notification_requests
SET status = CASE WHEN status IN ('queued', 'sending', 'sent', 'failed') THEN 'read' ELSE status END,
    read_at = COALESCE(read_at, $4::timestamptz),
    updated_at = $4::timestamptz
WHERE tenant_id = $1::uuid
  AND calendar_event_id = $2
  AND notification_type = 'escalation'
  AND context->>'obligation_escalation_id' = ANY($3::text[])`, in.TenantID, in.EventID, escalationNotificationIDs(esc, closure), now); err != nil {
		return fmt.Errorf("calendar: mark escalation notification read: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO obligation_status_events (
  tenant_id, obligation_id, event_type, occurred_at, actor_id, payload, idempotency_key
)
SELECT $1::uuid, $2::uuid, $3, $7::timestamptz, $4::uuid, $5::jsonb, $6
WHERE NOT EXISTS (
  SELECT 1
  FROM obligation_status_events
  WHERE tenant_id = $1::uuid
    AND obligation_id = $2::uuid
    AND idempotency_key = $6
)`,
		in.TenantID, esc.ObligationID, in.StatusEventType, in.ActorID,
		mustJSON(escalationActionDetails(in, target, esc, closure)),
		scopedKey+":obligation_status", now); err != nil {
		return fmt.Errorf("calendar: insert escalation action event: %w", err)
	}
	if err := insertOutbox(ctx, tx, in.TenantID, in.EventType, "calendar.escalation.v1",
		"obligation_escalation", esc.EscalationID, "calendar.notifications", scopedKey, in.TraceID, "human", in.ActorID, response, in.EventID); err != nil {
		return err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      in.ActorID,
		ActorType:    "human",
		Action:       in.EventType,
		ResourceType: "obligation_escalation",
		ResourceID:   esc.EscalationID,
		ScopeType:    "obligation_escalation",
		ScopeID:      esc.EscalationID,
		AfterState:   response,
		TraceID:      in.TraceID,
		Metadata:     escalationActionAuditMetadata(in, esc, closure, response),
	}); err != nil {
		return fmt.Errorf("calendar: audit escalation action: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE calendar_event_projections
SET escalation_state = $3,
    updated_at = $4::timestamptz
WHERE tenant_id = $1::uuid AND event_id = $2`, in.TenantID, in.EventID, projectionState, now); err != nil {
		return fmt.Errorf("calendar: update escalation action projection: %w", err)
	}
	return nil
}

func escalationActionDetails(in escalationActionInput, target actionTarget, esc lockedEscalation, closure escalationClosure) map[string]any {
	details := map[string]any{
		"level":              esc.Level,
		"role":               esc.Role,
		"calendar_event_id":  in.EventID,
		"escalation_id":      esc.EscalationID,
		"action":             in.ActionType,
		"reason":             in.Reason,
		"source_target_type": target.TargetType,
		"source_target_id":   target.TargetID,
	}
	if in.ActionType == "resolve_escalation" {
		details["resolved_escalation_ids"] = closure.IDs
		details["resolved_escalation_levels"] = closure.Levels
	}
	return details
}

func escalationActionAuditMetadata(in escalationActionInput, esc lockedEscalation, closure escalationClosure, response domain.CalendarActionResponse) map[string]any {
	metadata := map[string]any{
		"domain":            "calendar",
		"module":            "vaccination",
		"category":          "escalation",
		"calendar_event_id": in.EventID,
		"idempotency_key":   in.IdempotencyKey,
		"status":            response.Status,
		"result":            response.Status,
		"level":             esc.Level,
		"role":              esc.Role,
		"reason":            in.Reason,
	}
	if in.ActionType == "resolve_escalation" {
		metadata["resolved_escalation_ids"] = closure.IDs
		metadata["resolved_escalation_levels"] = closure.Levels
	}
	return metadata
}

func escalationNotificationIDs(esc lockedEscalation, closure escalationClosure) []string {
	if len(closure.IDs) > 0 {
		return closure.IDs
	}
	return []string{esc.EscalationID}
}

func (r *Repository) SweepDueReminders(ctx context.Context, tenantID string, limit int) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if limit <= 0 {
		limit = 100
	}
	events, err := r.selectDueReminderEvents(ctx, tenantID, limit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range events {
		persisted, err := r.queueDueReminder(ctx, tenantID, e.EventID)
		if err != nil {
			return count, err
		}
		if persisted {
			count++
		}
	}
	return count, nil
}

type dueReminderEvent struct {
	EventID        string
	Title          string
	TargetType     string
	TargetID       string
	PrimaryChannel string
	Timezone       string
}

func (r *Repository) selectDueReminderEvents(ctx context.Context, tenantID string, limit int) ([]dueReminderEvent, error) {
	rows, err := r.pool.Query(ctx, `
SELECT event_id, title, target_type, COALESCE(source_target_id::text, ''), primary_notification_channel,
       COALESCE(NULLIF(timezone, ''), 'Asia/Kolkata')
	FROM calendar_event_projections
	WHERE tenant_id = $1::uuid
	  AND slice_key = 'vaccination'
	  AND system = false
	  AND due_at <= now() + interval '1 hour'
	  AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due', 'deferred', 'blocked')
	  AND NOT EXISTS (
	    SELECT 1
	    FROM calendar_snoozes cs
	    WHERE cs.tenant_id = $1::uuid
	      AND cs.calendar_event_id = calendar_event_projections.event_id
	      AND cs.status = 'active'
	      AND cs.snooze_until > now()
	  )
	  AND NOT EXISTS (
	    SELECT 1
	    FROM notification_requests nr
	    WHERE nr.tenant_id = $1::uuid
	      AND nr.idempotency_key = $1 || ':calendar.reminder:' || calendar_event_projections.event_id || ':' || to_char((now() AT TIME ZONE COALESCE(NULLIF(calendar_event_projections.timezone, ''), 'Asia/Kolkata'))::date, 'YYYY-MM-DD')
	  )
	ORDER BY due_at ASC, event_id ASC
	LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("calendar: select reminder sweep: %w", err)
	}
	defer rows.Close()
	events := []dueReminderEvent{}
	for rows.Next() {
		var e dueReminderEvent
		if err := rows.Scan(&e.EventID, &e.Title, &e.TargetType, &e.TargetID, &e.PrimaryChannel, &e.Timezone); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *Repository) queueDueReminder(ctx context.Context, tenantID, eventID string) (bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var e dueReminderEvent
	if err := tx.QueryRow(ctx, `
SELECT event_id, title, target_type, COALESCE(source_target_id::text, ''), primary_notification_channel,
       COALESCE(NULLIF(timezone, ''), 'Asia/Kolkata')
FROM calendar_event_projections
	WHERE tenant_id = $1::uuid
	  AND event_id = $2
	  AND slice_key = 'vaccination'
	  AND system = false
	  AND due_at <= now() + interval '1 hour'
	  AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due', 'deferred', 'blocked')
	  AND NOT EXISTS (
	    SELECT 1
	    FROM calendar_snoozes cs
	    WHERE cs.tenant_id = $1::uuid
	      AND cs.calendar_event_id = calendar_event_projections.event_id
	      AND cs.status = 'active'
	      AND cs.snooze_until > now()
	  )
	  AND NOT EXISTS (
	    SELECT 1
	    FROM notification_requests nr
	    WHERE nr.tenant_id = $1::uuid
	      AND nr.idempotency_key = $1 || ':calendar.reminder:' || calendar_event_projections.event_id || ':' || to_char((now() AT TIME ZONE COALESCE(NULLIF(calendar_event_projections.timezone, ''), 'Asia/Kolkata'))::date, 'YYYY-MM-DD')
	  )
	ORDER BY due_at ASC, event_id ASC
	LIMIT 1
	FOR UPDATE SKIP LOCKED`, tenantID, eventID).Scan(&e.EventID, &e.Title, &e.TargetType, &e.TargetID, &e.PrimaryChannel, &e.Timezone); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("calendar: lock reminder event: %w", err)
	}
	channel := normalizeChannel("", e.PrimaryChannel)
	key := tenantID + ":calendar.reminder:" + e.EventID + ":" + calendarBusinessDateIn(time.Now().UTC(), e.Timezone)
	var requestID string
	err = tx.QueryRow(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context
) VALUES (
  $1::uuid, $2, $3, nullif($4::text, '')::uuid, 'reminder', $5,
  $6, $7, 'queued', $8, $9, $10::jsonb
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING notification_request_id::text`,
		tenantID, e.EventID, e.TargetType, e.TargetID, channel,
		"Reminder: "+e.Title, "Calendar reminder for "+e.Title, key,
		requestFingerprint(tenantID, e.EventID, "reminder", key),
		mustJSON(map[string]any{"calendar_event_id": e.EventID, "sweeper": "calendar-reminder-sweeper"})).Scan(&requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("calendar: insert reminder: %w", err)
	}
	resp := domain.CalendarActionResponse{ActionID: requestID, EventID: e.EventID, ActionType: "reminder", Status: "queued", Channel: &channel}
	if err := insertOutbox(ctx, tx, tenantID, "calendar.reminder.queued", "calendar.notification.v1",
		"calendar_notification", requestID, "calendar.notifications", key, "", "system_rule", "", resp, e.EventID); err != nil {
		return false, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorType:    "system",
		Action:       "calendar.reminder.queued",
		ResourceType: "calendar_notification",
		ResourceID:   requestID,
		ScopeType:    "notification_request",
		ScopeID:      requestID,
		AfterState:   resp,
		Metadata: map[string]any{
			"domain":            "calendar",
			"module":            "vaccination",
			"category":          "reminder",
			"calendar_event_id": e.EventID,
			"idempotency_key":   key,
			"status":            "queued",
			"result":            "queued",
			"channel":           channel,
		},
	}); err != nil {
		return false, fmt.Errorf("calendar: audit reminder: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE calendar_event_projections
SET reminder_state = 'queued',
    primary_notification_channel = $3,
    updated_at = now()
WHERE tenant_id = $1::uuid AND event_id = $2`, tenantID, e.EventID, channel); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) SweepEscalations(ctx context.Context, in ports.SweepEscalations) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if in.Limit <= 0 {
		in.Limit = 100
	}
	events, err := r.selectEscalationEvents(ctx, in)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, event := range events {
		persisted, err := r.queueEscalation(ctx, in.TenantID, event.EventID, event.Level, in.Now)
		if err != nil {
			return count, err
		}
		if persisted {
			count++
		}
	}
	return count, nil
}

type escalationEvent struct {
	EventID string
	Level   int
}

func (r *Repository) selectEscalationEvents(ctx context.Context, in ports.SweepEscalations) ([]escalationEvent, error) {
	level1Cutoff := in.Now.Add(-in.Level1After)
	level2Cutoff := in.Now.Add(-in.Level2After)
	level3Cutoff := in.Now.Add(-in.Level3After)
	level4Cutoff := in.Now.Add(-in.Level4After)
	rows, err := r.pool.Query(ctx, `
WITH candidates AS (
  SELECT
    event_id,
    due_at,
    CASE
      WHEN due_at <= $6::timestamptz THEN 4
      WHEN due_at <= $5::timestamptz THEN 3
      WHEN due_at <= $4::timestamptz THEN 2
      WHEN due_at <= $3::timestamptz THEN 1
      ELSE 0
    END AS level
  FROM calendar_event_projections
  WHERE tenant_id = $1::uuid
    AND slice_key = 'vaccination'
    AND system = false
	    AND due_at IS NOT NULL
	    AND due_at <= $2::timestamptz
	    AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due', 'deferred', 'blocked')
	)
SELECT event_id, level
FROM candidates c
WHERE level > 0
  AND NOT EXISTS (
    SELECT 1
    FROM notification_requests nr
    WHERE nr.tenant_id = $1::uuid
      AND nr.idempotency_key = $1 || ':calendar.escalation:' || c.event_id || ':level:' || c.level::text
  )
ORDER BY due_at ASC, event_id ASC
LIMIT $7`, in.TenantID, in.Now, level1Cutoff, level2Cutoff, level3Cutoff, level4Cutoff, in.Limit)
	if err != nil {
		return nil, fmt.Errorf("calendar: select escalation sweep: %w", err)
	}
	defer rows.Close()
	events := []escalationEvent{}
	for rows.Next() {
		var event escalationEvent
		if err := rows.Scan(&event.EventID, &event.Level); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

type escalationTarget struct {
	EventID          string
	Title            string
	Status           string
	TargetType       string
	TargetID         string
	PrimaryChannel   string
	SourceTargetType string
	SourceTargetID   string
	ExecutorRole     string
	VerifierLabel    string
	DueAt            time.Time
}

func (r *Repository) queueEscalation(ctx context.Context, tenantID, eventID string, level int, now time.Time) (bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var target escalationTarget
	if err := tx.QueryRow(ctx, `
SELECT event_id, title, status, source_target_type, COALESCE(source_target_id::text, ''),
       primary_notification_channel, source_target_type, COALESCE(source_target_id::text, ''),
       COALESCE(executor_role, ''), COALESCE(verifier_label, ''), due_at
FROM calendar_event_projections
WHERE tenant_id = $1::uuid
  AND event_id = $2
  AND slice_key = 'vaccination'
	  AND system = false
	  AND due_at <= $3::timestamptz
	  AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due', 'deferred', 'blocked')
	FOR UPDATE SKIP LOCKED`, tenantID, eventID, now).Scan(
		&target.EventID,
		&target.Title,
		&target.Status,
		&target.TargetType,
		&target.TargetID,
		&target.PrimaryChannel,
		&target.SourceTargetType,
		&target.SourceTargetID,
		&target.ExecutorRole,
		&target.VerifierLabel,
		&target.DueAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("calendar: lock escalation event: %w", err)
	}
	role := escalationRole(level, target)
	key := tenantID + ":calendar.escalation:" + target.EventID + ":level:" + fmt.Sprint(level)
	fingerprint := requestFingerprint(tenantID, target.EventID, "escalation", fmt.Sprint(level), role)
	var escalationID string
	if target.SourceTargetType == "obligation" && strings.TrimSpace(target.SourceTargetID) != "" {
		err := tx.QueryRow(ctx, `
INSERT INTO obligation_escalations (
  tenant_id, obligation_id, level, escalated_to_role, reason, status, opened_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, 'open', $6::timestamptz
)
ON CONFLICT DO NOTHING
RETURNING escalation_id::text`,
			tenantID, target.SourceTargetID, level, role, escalationReason(target, level), now).Scan(&escalationID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("calendar: insert obligation escalation: %w", err)
		}
		if escalationID != "" {
			if _, err := tx.Exec(ctx, `
INSERT INTO obligation_status_events (
  tenant_id, obligation_id, event_type, occurred_at, actor_id, payload, idempotency_key
)
SELECT $1::uuid, $2::uuid, 'escalated', $5::timestamptz, NULL, $3::jsonb, $4
WHERE NOT EXISTS (
  SELECT 1
  FROM obligation_status_events
  WHERE tenant_id = $1::uuid
    AND obligation_id = $2::uuid
    AND idempotency_key = $4
)`,
				tenantID, target.SourceTargetID,
				mustJSON(map[string]any{"level": level, "role": role, "calendar_event_id": target.EventID, "escalation_id": escalationID}),
				key, now); err != nil {
				return false, fmt.Errorf("calendar: insert obligation escalation event: %w", err)
			}
		}
	}
	channel := normalizeChannel("", target.PrimaryChannel)
	if level >= 3 {
		channel = "incident"
	} else if channel == "local-stub" && level >= 2 {
		channel = "slack"
	}
	contextJSON := mustJSON(map[string]any{
		"calendar_event_id":        target.EventID,
		"source_target_type":       target.SourceTargetType,
		"source_target_id":         target.SourceTargetID,
		"escalation_level":         level,
		"escalated_to_role":        role,
		"obligation_escalation_id": escalationID,
		"due_at":                   target.DueAt.UTC().Format(time.RFC3339Nano),
		"sweeper":                  "calendar-escalation-sweeper",
	})
	var requestID string
	err = tx.QueryRow(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  recipient_ref, title, body, status, idempotency_key, request_fingerprint, context, trace_id
) VALUES (
  $1::uuid, $2, $3, nullif($4::text, '')::uuid, 'escalation', $5,
  $6, $7, $8, 'queued', $9, $10, $11::jsonb, $12
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING notification_request_id::text`,
		tenantID, target.EventID, target.TargetType, target.TargetID, channel, role,
		fmt.Sprintf("Escalation L%d: %s", level, target.Title),
		fmt.Sprintf("%s is overdue. Escalated to %s.", target.Title, role),
		key, fingerprint, contextJSON, "calendar-escalation-sweeper:"+target.EventID).Scan(&requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("calendar: insert escalation notification: %w", err)
	}
	resp := domain.CalendarActionResponse{ActionID: requestID, EventID: target.EventID, ActionType: "escalation", Status: "queued", Channel: &channel}
	if err := insertOutbox(ctx, tx, tenantID, "calendar.escalation.queued", "calendar.notification.v1",
		"calendar_notification", requestID, "calendar.notifications", key, "calendar-escalation-sweeper:"+target.EventID, "system_rule", "", resp, target.EventID); err != nil {
		return false, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorType:    "system",
		Action:       "calendar.escalation.queued",
		ResourceType: "calendar_notification",
		ResourceID:   requestID,
		ScopeType:    "notification_request",
		ScopeID:      requestID,
		AfterState:   resp,
		TraceID:      "calendar-escalation-sweeper:" + target.EventID,
		Metadata: map[string]any{
			"domain":            "calendar",
			"module":            "vaccination",
			"category":          "escalation",
			"calendar_event_id": target.EventID,
			"idempotency_key":   key,
			"status":            "queued",
			"result":            "queued",
			"channel":           channel,
			"level":             level,
			"role":              role,
		},
	}); err != nil {
		return false, fmt.Errorf("calendar: audit escalation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE calendar_event_projections
SET status = CASE WHEN status IN ('scheduled', 'due') THEN 'overdue' ELSE status END,
    severity = 'critical',
    reminder_state = 'escalated',
    primary_notification_channel = $3,
    escalation_state = $4,
    updated_at = $5::timestamptz
WHERE tenant_id = $1::uuid AND event_id = $2`, tenantID, target.EventID, channel, fmt.Sprintf("level_%d_open", level), now); err != nil {
		return false, fmt.Errorf("calendar: update escalation projection: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func escalationRole(level int, target escalationTarget) string {
	switch {
	case level >= 4:
		return permissions.RoleCEOInternal
	case level == 3:
		return permissions.RolePHCDirector
	case level == 2:
		return permissions.RoleParkHead
	case target.Status == "verification_pending" || strings.TrimSpace(target.VerifierLabel) != "":
		return permissions.RoleVerifier
	default:
		return permissions.RoleOperator
	}
}

func actorCanActionEscalation(grants []ports.ActorGrant, tenantID string, target actionTarget, targetRole string) bool {
	targetRank, ok := escalationRoleRank(targetRole)
	if !ok {
		return false
	}
	for _, grant := range grants {
		actorRank, ok := escalationRoleRank(grant.Role)
		if !ok || actorRank < targetRank {
			continue
		}
		switch grant.ScopeType {
		case "tenant":
			if strings.EqualFold(grant.ScopeID, tenantID) {
				return true
			}
		case "park":
			if target.ParkID != "" && grant.ScopeID == target.ParkID {
				return true
			}
		case "shed":
			if target.ShedID != "" && grant.ScopeID == target.ShedID {
				return true
			}
		}
	}
	return false
}

func escalationRoleRank(role string) (int, bool) {
	switch strings.TrimSpace(role) {
	case permissions.RoleOperator, permissions.RoleVerifier:
		return 10, true
	case permissions.RoleParkHead:
		return 20, true
	case permissions.RolePHCDirector:
		return 30, true
	case permissions.RoleCEOInternal, permissions.RoleAdmin:
		return 40, true
	default:
		return 0, false
	}
}

func calendarBusinessDate(t time.Time) string {
	return calendarBusinessDateIn(t, domain.DefaultTimezone)
}

func calendarBusinessDateIn(t time.Time, timezone string) string {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = domain.DefaultTimezone
	}
	loc, err := time.LoadLocation(domain.DefaultTimezone)
	if timezone != domain.DefaultTimezone {
		loc, err = time.LoadLocation(timezone)
	}
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	return t.In(loc).Format("2006-01-02")
}

func escalationReason(target escalationTarget, level int) string {
	return fmt.Sprintf("calendar event %s overdue at level %d; status=%s due_at=%s", target.EventID, level, target.Status, target.DueAt.UTC().Format(time.RFC3339))
}

func (r *Repository) RefreshVaccinationProjection(ctx context.Context, in ports.RefreshVaccinationProjection) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	total := 0
	var cursor *domain.CalendarCursor
	for {
		page, err := r.refreshVaccinationProjectionPage(ctx, in, cursor)
		if err != nil {
			return total, err
		}
		total += page.upserted
		if page.nextCursor == nil {
			break
		}
		cursor = page.nextCursor
	}
	tombstoned, err := r.tombstoneVaccinationProjection(ctx, in)
	if err != nil {
		return total, err
	}
	return total + tombstoned, nil
}

type refreshProjectionPage struct {
	upserted   int
	nextCursor *domain.CalendarCursor
}

func (r *Repository) refreshVaccinationProjectionPage(ctx context.Context, in ports.RefreshVaccinationProjection, cursor *domain.CalendarCursor) (refreshProjectionPage, error) {
	var cursorDue any
	cursorEventID := ""
	if cursor != nil {
		cursorDue = cursor.DueAt
		cursorEventID = cursor.EventID
	}
	var count int
	var lastDue pgtype.Timestamptz
	var lastEventID string
	if err := r.pool.QueryRow(ctx, calendarVaccinationProjectionRefreshSQL,
		in.TenantID, in.DateFrom, in.DateTo, in.Limit, cursorDue, cursorEventID).Scan(&count, &lastDue, &lastEventID); err != nil {
		return refreshProjectionPage{}, fmt.Errorf("calendar: refresh vaccination projection: %w", err)
	}
	page := refreshProjectionPage{upserted: count}
	if count == in.Limit && lastDue.Valid && lastEventID != "" {
		page.nextCursor = &domain.CalendarCursor{DueAt: lastDue.Time, EventID: lastEventID}
	}
	return page, nil
}

func (r *Repository) tombstoneVaccinationProjection(ctx context.Context, in ports.RefreshVaccinationProjection) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, calendarVaccinationProjectionTombstoneSQL,
		in.TenantID, in.DateFrom, in.DateTo).Scan(&count); err != nil {
		return 0, fmt.Errorf("calendar: tombstone vaccination projection: %w", err)
	}
	return count, nil
}

func (r *Repository) PruneClosedVaccinationProjection(ctx context.Context, tenantID string, cutoff time.Time, limit int) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if cutoff.IsZero() {
		cutoff = time.Now().UTC().Add(-90 * 24 * time.Hour)
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var count int
	if err := r.pool.QueryRow(ctx, calendarPruneClosedVaccinationProjectionSQL, tenantID, cutoff, limit).Scan(&count); err != nil {
		return 0, fmt.Errorf("calendar: prune closed vaccination projection: %w", err)
	}
	return count, nil
}

func (r *Repository) eventExists(ctx context.Context, tenantID, eventID string, scope domain.ScopeFilter) error {
	var exists bool
	tenantWide, parkIDs, shedIDs := scopeArgs(scope)
	if err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM calendar_event_projections
  WHERE tenant_id = $1::uuid AND event_id = $2 AND slice_key = 'vaccination'
    AND system = false
    AND ($3::bool OR park_id::text = ANY($4::text[]) OR shed_id::text = ANY($5::text[]))
)`, tenantID, eventID, tenantWide, parkIDs, shedIDs).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ports.ErrNotFound
	}
	return nil
}

func (r *Repository) notificationActionByID(ctx context.Context, tenantID, eventID, requestID string) (domain.CalendarActionResponse, error) {
	var action domain.CalendarActionResponse
	var channel pgtype.Text
	if err := r.pool.QueryRow(ctx, `
SELECT notification_request_id::text, calendar_event_id, notification_type, status, channel
FROM notification_requests
WHERE tenant_id = $1::uuid AND calendar_event_id = $2 AND notification_request_id = $3::uuid`,
		tenantID, eventID, requestID).Scan(&action.ActionID, &action.EventID, &action.ActionType, &action.Status, &channel); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CalendarActionResponse{}, ports.ErrNotFound
		}
		return domain.CalendarActionResponse{}, err
	}
	action.Channel = textPtr(channel)
	return action, nil
}

func (r *Repository) snoozeActionByID(ctx context.Context, tenantID, eventID, snoozeID string) (domain.CalendarActionResponse, error) {
	var action domain.CalendarActionResponse
	var snoozeUntil pgtype.Timestamptz
	if err := r.pool.QueryRow(ctx, `
SELECT snooze_id::text, calendar_event_id, status, snooze_until
FROM calendar_snoozes
WHERE tenant_id = $1::uuid AND calendar_event_id = $2 AND snooze_id = $3::uuid`,
		tenantID, eventID, snoozeID).Scan(&action.ActionID, &action.EventID, &action.Status, &snoozeUntil); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CalendarActionResponse{}, ports.ErrNotFound
		}
		return domain.CalendarActionResponse{}, err
	}
	action.ActionType = "snooze"
	action.SnoozeUntil = timePtr(snoozeUntil)
	return action, nil
}

func (r *Repository) escalationActionByID(ctx context.Context, tenantID, eventID, escalationID, actionType string) (domain.CalendarActionResponse, error) {
	var action domain.CalendarActionResponse
	if err := r.pool.QueryRow(ctx, `
SELECT oe.escalation_id::text, cep.event_id, oe.status
FROM obligation_escalations oe
JOIN calendar_event_projections cep
  ON cep.tenant_id = oe.tenant_id
 AND cep.source_target_type = 'obligation'
 AND cep.source_target_id = oe.obligation_id
WHERE oe.tenant_id = $1::uuid
  AND cep.event_id = $2
  AND oe.escalation_id = $3::uuid`,
		tenantID, eventID, escalationID).Scan(&action.ActionID, &action.EventID, &action.Status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CalendarActionResponse{}, ports.ErrNotFound
		}
		return domain.CalendarActionResponse{}, err
	}
	action.ActionType = actionType
	return action, nil
}

type actionTarget struct {
	EventID        string
	EventType      string
	Title          string
	TargetType     string
	TargetID       string
	ParkID         string
	ShedID         string
	PrimaryChannel string
	RecipientRef   string
}

func scopeArgs(scope domain.ScopeFilter) (bool, []string, []string) {
	parkIDs := scope.ParkIDs
	if parkIDs == nil {
		parkIDs = []string{}
	}
	shedIDs := scope.ShedIDs
	if shedIDs == nil {
		shedIDs = []string{}
	}
	return scope.TenantWide, parkIDs, shedIDs
}

func loadActionTarget(ctx context.Context, tx pgx.Tx, tenantID, eventID string, scope domain.ScopeFilter) (actionTarget, error) {
	var out actionTarget
	var targetID, assignee, executor, verifier pgtype.Text
	var sourceBacked, system bool
	tenantWide, parkIDs, shedIDs := scopeArgs(scope)
	err := tx.QueryRow(ctx, `
SELECT event_id, event_type, title, source_target_type, COALESCE(source_target_id::text, ''),
       primary_notification_channel, COALESCE(assignee_label, ''), COALESCE(executor_role, ''),
       COALESCE(verifier_label, ''), COALESCE(park_id::text, ''), COALESCE(shed_id::text, ''),
       source_backed, system
FROM calendar_event_projections
WHERE tenant_id = $1::uuid AND event_id = $2 AND slice_key = 'vaccination'
  AND system = false
  AND ($3::bool OR park_id::text = ANY($4::text[]) OR shed_id::text = ANY($5::text[]))
FOR UPDATE`, tenantID, eventID, tenantWide, parkIDs, shedIDs).Scan(
		&out.EventID, &out.EventType, &out.Title, &out.TargetType, &targetID, &out.PrimaryChannel,
		&assignee, &executor, &verifier, &out.ParkID, &out.ShedID, &sourceBacked, &system,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return actionTarget{}, ports.ErrNotFound
	}
	if err != nil {
		return actionTarget{}, err
	}
	out.TargetID = targetID.String
	switch {
	case assignee.String != "":
		out.RecipientRef = assignee.String
	case executor.String != "":
		out.RecipientRef = executor.String
	case verifier.String != "":
		out.RecipientRef = verifier.String
	}
	if system || (!sourceBacked && out.EventType != domain.EventVaccinationConfigSourceApproval) || out.RecipientRef == "" {
		return actionTarget{}, ports.ErrEventNotActionable
	}
	return out, nil
}

func activeSnoozeIDs(ctx context.Context, tx pgx.Tx, tenantID, eventID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT snooze_id::text
FROM calendar_snoozes
WHERE tenant_id = $1::uuid AND calendar_event_id = $2 AND status = 'active' AND snooze_until > now()
FOR UPDATE`, tenantID, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

const calendarOutboxEnvelopeVersion = "1.0.0"

func insertOutbox(ctx context.Context, tx pgx.Tx, tenantID, eventType, schemaRef, aggregateType, aggregateID, topic, idempotencyKey, traceID, actorType, actorID string, payload any, calendarEventID string) error {
	var eventID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&eventID); err != nil {
		return fmt.Errorf("calendar: generate outbox event id: %w", err)
	}
	payloadBytes, err := calendarOutboxEnvelope(tenantID, eventID, eventType, schemaRef, aggregateType, aggregateID, idempotencyKey, traceID, actorType, actorID, payload, calendarEventID)
	if err != nil {
		return err
	}
	headersBytes, _ := json.Marshal(map[string]any{
		"calendar_event_id": calendarEventID,
		"schema_version":    calendarOutboxEnvelopeVersion,
	})
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, nullif($11, ''), 'pending', now()
)`, tenantID, eventID, eventType, calendarOutboxEnvelopeVersion, aggregateType, aggregateID, topic, payloadBytes, headersBytes, idempotencyKey, traceID)
	if err != nil {
		return fmt.Errorf("calendar: insert outbox: %w", err)
	}
	return nil
}

func calendarOutboxEnvelope(tenantID, eventID, eventType, schemaRef, aggregateType, aggregateID, idempotencyKey, traceID, actorType, actorID string, payload any, calendarEventID string) ([]byte, error) {
	if strings.TrimSpace(traceID) == "" {
		traceID = "calendar-outbox:" + eventType + ":" + aggregateID
	}
	actorIDValue := any(nil)
	if strings.TrimSpace(actorID) != "" {
		actorIDValue = actorID
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": calendarOutboxEnvelopeVersion,
		"schema_ref":     schemaRef,
		"aggregate_type": aggregateType,
		"aggregate_id":   aggregateID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "calendar_vaccination",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": actorType,
			"actor_id":   actorIDValue,
			"actor_ref":  nil,
		},
		"subject_type": "calendar_event",
		"subject_id":   calendarEventID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "event",
			"evidence_id":   calendarEventID,
		}},
		"payload":  payload,
		"trace_id": traceID,
	})
}

const calendarListSQL = `
SELECT event_id, event_type, owner_key, title, subtitle, status, severity, due_at, window_start,
       window_end, timezone, timezone_source, park_id::text, park_code, shed_id::text, shed_name,
       cohort_id::text, cohort_name, target_type, target_count, protocol_id::text,
       protocol_version_id::text, rule_id::text, vaccine_name, dose_code, source_backed,
       source_label, assignee_label, executor_role, verifier_label, reminder_state,
       primary_notification_channel, escalation_state, system, cross_cutting, links
FROM calendar_event_projections
WHERE tenant_id = $1::uuid
  AND slice_key = 'vaccination'
  AND system = false
  AND due_at >= $6::timestamptz
  AND due_at < $7::timestamptz
  AND ($2::text = '' OR owner_key = $2::text)
  AND ($3::text = '' OR status = $3::text)
  AND ($3::text <> '' OR status NOT IN ('completed', 'canceled'))
  AND ($4::text = '' OR park_id = nullif($4::text, '')::uuid)
  AND ($5::text = '' OR shed_id = nullif($5::text, '')::uuid)
  AND ($8::timestamptz IS NULL OR (due_at, event_id) > ($8::timestamptz, $9::text))
  AND ($11::bool OR park_id::text = ANY($12::text[]) OR shed_id::text = ANY($13::text[]))
ORDER BY due_at ASC, event_id ASC
LIMIT $10`

const calendarDetailSQL = `
SELECT event_id, event_type, owner_key, title, subtitle, status, severity, due_at, window_start,
       window_end, timezone, timezone_source, park_id::text, park_code, shed_id::text, shed_name,
       cohort_id::text, cohort_name, target_type, target_count, protocol_id::text,
       protocol_version_id::text, rule_id::text, vaccine_name, dose_code, source_backed,
       source_label, assignee_label, executor_role, verifier_label, reminder_state,
       primary_notification_channel, escalation_state, system, cross_cutting, links, detail
FROM calendar_event_projections
WHERE tenant_id = $1::uuid AND event_id = $2 AND slice_key = 'vaccination'
  AND system = false
  AND ($3::bool OR park_id::text = ANY($4::text[]) OR shed_id::text = ANY($5::text[]))`

const calendarHistorySQL = `
WITH history AS (
  SELECT
    'notification:' || notification_request_id::text AS history_id,
    'calendar_notification_' || notification_type AS event_type,
    status,
    title,
    requested_by::text AS actor_label,
    requested_at AS occurred_at,
    channel,
    NULLIF(context->>'reason', '') AS reason,
    trace_id,
    'notification_requests' AS source_table,
    jsonb_build_object(
      'notification_request_id', notification_request_id,
      'notification_type', notification_type,
      'channel', channel,
      'body', body,
      'failure_reason', failure_reason
    ) AS details
  FROM notification_requests
  WHERE tenant_id = $1::uuid AND calendar_event_id = $2

  UNION ALL

  SELECT
    'snooze:' || snooze_id::text AS history_id,
    'calendar_snooze' AS event_type,
    status,
    'Snoozed until ' || to_char(snooze_until AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI') AS title,
    created_by::text AS actor_label,
    created_at AS occurred_at,
    NULL::text AS channel,
    reason,
    trace_id,
    'calendar_snoozes' AS source_table,
    jsonb_build_object(
      'snooze_id', snooze_id,
      'snooze_until', snooze_until,
      'replaced_by_snooze_id', replaced_by_snooze_id
    ) AS details
  FROM calendar_snoozes
  WHERE tenant_id = $1::uuid AND calendar_event_id = $2

  UNION ALL

  SELECT
    'escalation:' || oe.escalation_id::text AS history_id,
    'calendar_escalation_' || oe.status AS event_type,
    oe.status,
    'Escalation L' || oe.level::text || ' ' || oe.status AS title,
    COALESCE(NULLIF(to_jsonb(oe)->>'resolved_by', ''), NULLIF(to_jsonb(oe)->>'acknowledged_by', ''), oe.escalated_to_user_id::text) AS actor_label,
    COALESCE(oe.resolved_at, oe.acknowledged_at, oe.opened_at) AS occurred_at,
    NULL::text AS channel,
    NULLIF(COALESCE(NULLIF(to_jsonb(oe)->>'resolution_note', ''), NULLIF(to_jsonb(oe)->>'acknowledgement_note', ''), oe.reason), '') AS reason,
    NULL::text AS trace_id,
    'obligation_escalations' AS source_table,
    jsonb_build_object(
      'escalation_id', oe.escalation_id,
      'obligation_id', oe.obligation_id,
      'level', oe.level,
      'escalated_to_role', oe.escalated_to_role,
      'acknowledged_by', to_jsonb(oe)->>'acknowledged_by',
      'resolved_by', to_jsonb(oe)->>'resolved_by',
      'opened_at', oe.opened_at,
      'acknowledged_at', oe.acknowledged_at,
      'resolved_at', oe.resolved_at
    ) AS details
  FROM obligation_escalations oe
  JOIN calendar_event_projections cep
    ON cep.tenant_id = oe.tenant_id
   AND cep.source_target_type = 'obligation'
   AND cep.source_target_id = oe.obligation_id
  WHERE oe.tenant_id = $1::uuid AND cep.event_id = $2

  UNION ALL

  SELECT
    'audit:' || audit_id::text AS history_id,
    action AS event_type,
    COALESCE(metadata->>'status', metadata->>'result', 'recorded') AS status,
    action AS title,
    actor_id::text AS actor_label,
    recorded_at AS occurred_at,
    metadata->>'channel' AS channel,
    COALESCE(metadata->>'reason', metadata->>'review_reason') AS reason,
    trace_id,
    'audit_log' AS source_table,
    jsonb_build_object(
      'audit_id', audit_id,
      'resource_type', resource_type,
      'resource_id', resource_id,
      'metadata', metadata,
      'after_state', after_state
    ) AS details
  FROM audit_log
  WHERE tenant_id = $1::uuid AND metadata->>'calendar_event_id' = $2
)
SELECT history_id, event_type, status, title, actor_label, occurred_at, channel, reason, trace_id, source_table, details
FROM history
WHERE ($3::timestamptz IS NULL OR (occurred_at, history_id) < ($3::timestamptz, $4::text))
ORDER BY occurred_at DESC, history_id DESC
LIMIT $5`

const calendarVaccinationProjectionRefreshSQL = `
WITH obligation_events AS (
  SELECT
    'obligation:' || oi.obligation_id::text AS event_id,
    'vaccination_dose_due'::text AS event_type,
    'phc'::text AS owner_key,
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
    COALESCE(scope_loc.timezone, 'Asia/Kolkata') AS timezone,
    CASE WHEN scope_loc.timezone IS NULL THEN 'fallback' ELSE 'location' END AS timezone_source,
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
    COALESCE(NULLIF(pv.rule_dsl -> 'source' ->> 'source_ref', ''), pd.name) AS source_label,
    'obligation'::text AS source_target_type,
    oi.obligation_id AS source_target_id,
    'PHC vaccinator'::text AS assignee_label,
    'phc_vaccinator'::text AS executor_role,
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
      'summary', jsonb_build_object('owner', 'PHC', 'target_count', 1),
      'source_and_rule', jsonb_build_object(
        'source_backed', true,
        'source_ref', pv.rule_dsl -> 'source' ->> 'source_ref',
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
    AND oi.due_at >= $2::timestamptz
    AND oi.due_at < $3::timestamptz
    AND pd.category = 'vaccination'
    AND pv.status = 'published'
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'review_status', '') = 'approved'
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'source_ref', '') <> ''
    AND oi.status NOT IN ('waived', 'canceled', 'superseded')
    AND (oi.status <> 'completed' OR oi.due_at >= now() - interval '90 days')
),
batch_events AS (
  SELECT DISTINCT ON (ob.batch_id, pr.rule_id)
    'batch:' || ob.batch_id::text || ':rule:' || pr.rule_id::text || ':shed:' || ob.scope_id::text AS event_id,
    'vaccination_drive'::text AS event_type,
    'phc'::text AS owner_key,
    pd.name || ' drive' AS title,
    COALESCE(scope_loc.name, 'Vaccination drive') AS subtitle,
    CASE ob.status
      WHEN 'planned' THEN 'scheduled'
      WHEN 'superseded' THEN 'canceled'
      ELSE ob.status
    END AS status,
    CASE
      WHEN COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) < now() AND ob.status <> 'completed' THEN 'warning'
      ELSE 'info'
    END AS severity,
    COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) AS due_at,
    COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) AS window_start,
    COALESCE(ob.window_end, ob.window_start + interval '8 hours', ob.planned_date::timestamptz + interval '8 hours') AS window_end,
    COALESCE(scope_loc.timezone, 'Asia/Kolkata') AS timezone,
    CASE WHEN scope_loc.timezone IS NULL THEN 'fallback' ELSE 'location' END AS timezone_source,
    scope_parent.location_id AS park_id,
    scope_parent.location_code AS park_code,
    ob.scope_id AS shed_id,
    scope_loc.name AS shed_name,
    NULL::uuid AS cohort_id,
    NULL::text AS cohort_name,
    'shed'::text AS target_type,
    GREATEST(ob.estimated_targets, 1)::int AS target_count,
    pd.protocol_id,
    pv.protocol_version_id,
    pr.rule_id,
    pd.name AS vaccine_name,
    pr.dose_code,
    true AS source_backed,
    COALESCE(NULLIF(pv.rule_dsl -> 'source' ->> 'source_ref', ''), pd.name) AS source_label,
    'batch'::text AS source_target_type,
    ob.batch_id AS source_target_id,
    'PHC drive team'::text AS assignee_label,
    'phc_vaccinator'::text AS executor_role,
    'PHC verifier'::text AS verifier_label,
    'not_scheduled'::text AS reminder_state,
    'local-stub'::text AS primary_notification_channel,
    'none'::text AS escalation_state,
    false AS system,
    false AS cross_cutting,
    jsonb_build_object(
      'vaccination', '/vaccination/operations',
      'drive', '/vaccination/execution/sheds/' || ob.scope_id::text
    ) AS links,
    jsonb_build_object(
      'summary', jsonb_build_object('owner', 'PHC', 'target_count', GREATEST(ob.estimated_targets, 1)),
      'source_and_rule', jsonb_build_object(
        'source_backed', true,
        'protocol_version_id', pv.protocol_version_id,
        'rule_id', pr.rule_id
      ),
      'execution', jsonb_build_object('batch_id', ob.batch_id, 'sop_task_id', ob.sop_task_id, 'work_state', ob.status),
      'stock', jsonb_build_object('reserved_qty', ob.reserved_quantity, 'planned_qty', ob.planned_quantity),
      'proof', jsonb_build_object(),
      'verification', jsonb_build_object('verifier', 'PHC verifier'),
      'notification_channels', jsonb_build_array('local-stub'),
      'notification_policy', jsonb_build_object('nudge_allowed', true),
      'links', jsonb_build_object()
    ) AS detail
  FROM obligation_batches ob
  JOIN obligation_instances oi
    ON oi.tenant_id = ob.tenant_id AND oi.batch_id = ob.batch_id
  JOIN protocol_versions pv
    ON pv.tenant_id = ob.tenant_id AND pv.protocol_version_id = ob.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
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
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'review_status', '') = 'approved'
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'source_ref', '') <> ''
    AND ob.status NOT IN ('superseded', 'canceled')
    AND (ob.status <> 'completed' OR COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) >= now() - interval '90 days')
  ORDER BY ob.batch_id, pr.rule_id, COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end)
),
sop_events AS (
  SELECT DISTINCT ON (st.task_id)
    'calendar:' || st.task_id::text AS event_id,
    CASE
      WHEN st.state IN ('rework_requested', 'rejected') THEN 'vaccination_rework_due'
      ELSE 'vaccination_proof_verification'
    END AS event_type,
    'phc'::text AS owner_key,
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
    COALESCE(scope_loc.timezone, 'Asia/Kolkata') AS timezone,
    CASE WHEN scope_loc.timezone IS NULL THEN 'fallback' ELSE 'location' END AS timezone_source,
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
    COALESCE(NULLIF(pv.rule_dsl -> 'source' ->> 'source_ref', ''), pd.name) AS source_label,
    'sop_task'::text AS source_target_type,
    st.task_id AS source_target_id,
    COALESCE(st.assigned_to::text, 'PHC verifier') AS assignee_label,
    CASE WHEN st.state IN ('submitted', 'needs_review') THEN NULL ELSE 'phc_vaccinator' END AS executor_role,
    CASE WHEN st.state IN ('submitted', 'needs_review') THEN 'PHC verifier' ELSE NULL END AS verifier_label,
    'not_scheduled'::text AS reminder_state,
    'local-stub'::text AS primary_notification_channel,
    'none'::text AS escalation_state,
    false AS system,
    false AS cross_cutting,
    jsonb_build_object('workflow', '/vaccination/workflows/' || ('calendar:' || st.task_id::text)) AS links,
    jsonb_build_object(
      'summary', jsonb_build_object('owner', 'PHC', 'task_type', st.task_type),
      'source_and_rule', jsonb_build_object(
        'source_backed', true,
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
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'review_status', '') = 'approved'
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'source_ref', '') <> ''
    AND st.state IN ('assigned', 'in_progress', 'submitted', 'needs_review', 'rework_requested', 'rejected')
  ORDER BY st.task_id, st.due_at
),
config_due AS (
  SELECT
    pv.*,
    pd.name AS protocol_name,
    NULLIF(pv.rule_dsl -> 'source' ->> 'review_due_at', '') AS raw_due_at
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
    'vaccination_config_source_approval'::text AS event_type,
    'admin_data_ops'::text AS owner_key,
    'Approve ' || protocol_name || ' source review' AS title,
    'Protocol source review due'::text AS subtitle,
    'due'::text AS status,
    CASE WHEN raw_due_at::timestamptz < now() THEN 'critical' ELSE 'warning' END AS severity,
    raw_due_at::timestamptz AS due_at,
    raw_due_at::timestamptz AS window_start,
    raw_due_at::timestamptz + interval '1 day' AS window_end,
    'Asia/Kolkata'::text AS timezone,
    'fallback'::text AS timezone_source,
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
    false AS source_backed,
    COALESCE(NULLIF(rule_dsl -> 'source' ->> 'source_ref', ''), protocol_name) AS source_label,
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
      'source_and_rule', jsonb_build_object('protocol_version_id', protocol_version_id, 'review_state', 'approval_due'),
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
  SELECT * FROM batch_events
  UNION ALL
  SELECT * FROM sop_events
  UNION ALL
  SELECT * FROM config_events
),
limited AS (
  SELECT *
  FROM source_events
  WHERE due_at IS NOT NULL
    AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending',
                   'verification_pending', 'rejected', 'rework_due', 'deferred',
                   'blocked', 'completed')
    AND ($5::timestamptz IS NULL OR (due_at, event_id) > ($5::timestamptz, $6::text))
  ORDER BY due_at ASC, event_id ASC
  LIMIT $4
),
upserted AS (
  INSERT INTO calendar_event_projections (
    tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle, status, severity,
    due_at, window_start, window_end, timezone, timezone_source, park_id, park_code, shed_id, shed_name,
    cohort_id, cohort_name, target_type, target_count, protocol_id, protocol_version_id, rule_id,
    vaccine_name, dose_code, source_backed, source_label, source_target_type, source_target_id,
    assignee_label, executor_role, verifier_label, reminder_state, primary_notification_channel,
    escalation_state, system, cross_cutting, links, detail
  )
  SELECT
    $1::uuid, event_id, 'vaccination', event_type, owner_key, title, subtitle, status, severity,
    due_at, window_start, window_end, timezone, timezone_source, park_id, park_code, shed_id, shed_name,
    cohort_id, cohort_name, target_type, target_count, protocol_id, protocol_version_id, rule_id,
    vaccine_name, dose_code, source_backed, source_label, source_target_type, source_target_id,
    assignee_label, executor_role, verifier_label, reminder_state, primary_notification_channel,
    escalation_state, system, cross_cutting, links, detail
  FROM limited
  ON CONFLICT (tenant_id, event_id) DO UPDATE
  SET event_type = EXCLUDED.event_type,
      owner_key = EXCLUDED.owner_key,
      title = EXCLUDED.title,
      subtitle = EXCLUDED.subtitle,
      status = EXCLUDED.status,
      severity = EXCLUDED.severity,
      due_at = EXCLUDED.due_at,
      window_start = EXCLUDED.window_start,
      window_end = EXCLUDED.window_end,
      timezone = EXCLUDED.timezone,
      timezone_source = EXCLUDED.timezone_source,
      park_id = EXCLUDED.park_id,
      park_code = EXCLUDED.park_code,
      shed_id = EXCLUDED.shed_id,
      shed_name = EXCLUDED.shed_name,
      cohort_id = EXCLUDED.cohort_id,
      cohort_name = EXCLUDED.cohort_name,
      target_type = EXCLUDED.target_type,
      target_count = EXCLUDED.target_count,
      protocol_id = EXCLUDED.protocol_id,
      protocol_version_id = EXCLUDED.protocol_version_id,
      rule_id = EXCLUDED.rule_id,
      vaccine_name = EXCLUDED.vaccine_name,
      dose_code = EXCLUDED.dose_code,
      source_backed = EXCLUDED.source_backed,
      source_label = EXCLUDED.source_label,
      source_target_type = EXCLUDED.source_target_type,
      source_target_id = EXCLUDED.source_target_id,
      assignee_label = EXCLUDED.assignee_label,
      executor_role = EXCLUDED.executor_role,
      verifier_label = EXCLUDED.verifier_label,
      reminder_state = CASE
        WHEN calendar_event_projections.reminder_state <> 'not_scheduled' THEN calendar_event_projections.reminder_state
        ELSE EXCLUDED.reminder_state
      END,
      primary_notification_channel = CASE
        WHEN calendar_event_projections.reminder_state <> 'not_scheduled'
          OR calendar_event_projections.escalation_state <> 'none' THEN calendar_event_projections.primary_notification_channel
        ELSE EXCLUDED.primary_notification_channel
      END,
      escalation_state = CASE
        WHEN calendar_event_projections.escalation_state <> 'none' THEN calendar_event_projections.escalation_state
        ELSE EXCLUDED.escalation_state
      END,
      system = EXCLUDED.system,
      cross_cutting = EXCLUDED.cross_cutting,
      links = EXCLUDED.links,
      detail = EXCLUDED.detail,
      updated_at = now()
  RETURNING event_id, due_at
)
SELECT
  count(*)::int AS upserted_count,
  (array_agg(due_at ORDER BY due_at DESC, event_id DESC))[1] AS last_due_at,
  COALESCE((array_agg(event_id ORDER BY due_at DESC, event_id DESC))[1], '') AS last_event_id
FROM upserted`

const calendarVaccinationProjectionTombstoneSQL = `
WITH source_event_ids AS (
  SELECT 'obligation:' || oi.obligation_id::text AS event_id
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.due_at >= $2::timestamptz
    AND oi.due_at < $3::timestamptz
    AND pd.category = 'vaccination'
    AND pv.status = 'published'
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'review_status', '') = 'approved'
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'source_ref', '') <> ''
    AND oi.status NOT IN ('waived', 'canceled', 'superseded')
    AND (oi.status <> 'completed' OR oi.due_at >= now() - interval '90 days')

  UNION ALL

  SELECT 'batch:' || ob.batch_id::text || ':rule:' || oi.rule_id::text || ':shed:' || ob.scope_id::text AS event_id
  FROM obligation_batches ob
  JOIN obligation_instances oi
    ON oi.tenant_id = ob.tenant_id AND oi.batch_id = ob.batch_id
  JOIN protocol_versions pv
    ON pv.tenant_id = ob.tenant_id AND pv.protocol_version_id = ob.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  WHERE ob.tenant_id = $1::uuid
    AND ob.scope_type = 'shed'
    AND COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) >= $2::timestamptz
    AND COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) < $3::timestamptz
    AND pd.category = 'vaccination'
    AND pv.status = 'published'
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'review_status', '') = 'approved'
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'source_ref', '') <> ''
    AND ob.status NOT IN ('superseded', 'canceled')
    AND (ob.status <> 'completed' OR COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) >= now() - interval '90 days')

  UNION ALL

  SELECT 'calendar:' || st.task_id::text AS event_id
  FROM sop_tasks st
  JOIN obligation_instances oi
    ON oi.tenant_id = st.tenant_id AND oi.sop_task_id = st.task_id
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  WHERE st.tenant_id = $1::uuid
    AND st.due_at >= $2::timestamptz
    AND st.due_at < $3::timestamptz
    AND pd.category = 'vaccination'
    AND pv.status = 'published'
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'review_status', '') = 'approved'
    AND COALESCE(pv.rule_dsl -> 'source' ->> 'source_ref', '') <> ''
    AND st.state IN ('assigned', 'in_progress', 'submitted', 'needs_review', 'rework_requested', 'rejected')

  UNION ALL

  SELECT 'calendar:' || protocol_version_id::text AS event_id
  FROM (
    SELECT
      pv.protocol_version_id,
      NULLIF(pv.rule_dsl -> 'source' ->> 'review_due_at', '') AS raw_due_at
    FROM protocol_versions pv
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
    WHERE pv.tenant_id = $1::uuid
      AND pd.category = 'vaccination'
      AND pv.status = 'draft'
  ) config_due
  WHERE raw_due_at ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}'
    AND raw_due_at::timestamptz >= $2::timestamptz
    AND raw_due_at::timestamptz < $3::timestamptz
),
tombstoned AS (
  UPDATE calendar_event_projections cep
  SET status = 'canceled',
      escalation_state = 'none',
      detail = jsonb_set(
        COALESCE(cep.detail, '{}'::jsonb),
        '{tombstone}',
        jsonb_build_object('reason', 'source_no_longer_qualifies', 'refreshed_at', now()),
        true
      ),
      updated_at = now()
  WHERE cep.tenant_id = $1::uuid
    AND cep.slice_key = 'vaccination'
    AND cep.system = false
    AND cep.due_at >= $2::timestamptz
    AND cep.due_at < $3::timestamptz
    AND cep.source_target_type IN ('obligation', 'batch', 'sop_task', 'protocol_version')
    AND cep.status NOT IN ('completed', 'canceled')
    AND NOT EXISTS (
      SELECT 1 FROM source_event_ids source
      WHERE source.event_id = cep.event_id
    )
  RETURNING 1
)
SELECT count(*)::int FROM tombstoned`

const calendarPruneClosedVaccinationProjectionSQL = `
WITH doomed AS (
  SELECT ctid
  FROM calendar_event_projections
  WHERE tenant_id = $1::uuid
    AND slice_key = 'vaccination'
    AND system = false
    AND status IN ('completed', 'canceled')
    AND due_at < $2::timestamptz
  ORDER BY due_at ASC, event_id ASC
  LIMIT $3
),
deleted AS (
  DELETE FROM calendar_event_projections cep
  USING doomed
  WHERE cep.ctid = doomed.ctid
  RETURNING 1
)
SELECT count(*)::int FROM deleted`

type eventScanner interface {
	Scan(dest ...any) error
}

func scanCalendarEvent(rows eventScanner) (domain.CalendarEvent, error) {
	return scanCalendarEventWithDetail(rows, nil, nil)
}

func scanCalendarEventWithDetail(rows eventScanner, detail *[]byte, linksOut *[]byte) (domain.CalendarEvent, error) {
	var event domain.CalendarEvent
	var windowStart, windowEnd pgtype.Timestamptz
	var parkID, parkCode, shedID, shedName, cohortID, cohortName pgtype.Text
	var protocolID, versionID, ruleID, vaccineName, doseCode pgtype.Text
	var assignee, executor, verifier pgtype.Text
	var links []byte
	dest := []any{
		&event.EventID, &event.EventType, &event.OwnerKey, &event.Title, &event.Subtitle,
		&event.Status, &event.Severity, &event.DueAt, &windowStart, &windowEnd,
		&event.Timezone, &event.TimezoneSource, &parkID, &parkCode, &shedID, &shedName,
		&cohortID, &cohortName, &event.TargetType, &event.TargetCount, &protocolID,
		&versionID, &ruleID, &vaccineName, &doseCode, &event.SourceBacked,
		&event.SourceLabel, &assignee, &executor, &verifier, &event.ReminderState,
		&event.PrimaryNotificationChannel, &event.EscalationState, &event.System,
		&event.CrossCutting, &links,
	}
	if detail != nil {
		dest = append(dest, detail)
	}
	if err := rows.Scan(dest...); err != nil {
		return domain.CalendarEvent{}, err
	}
	event.WindowStart = timePtr(windowStart)
	event.WindowEnd = timePtr(windowEnd)
	event.ParkID = textPtr(parkID)
	event.ParkCode = textPtr(parkCode)
	event.ShedID = textPtr(shedID)
	event.ShedName = textPtr(shedName)
	event.CohortID = textPtr(cohortID)
	event.CohortName = textPtr(cohortName)
	event.ProtocolID = textPtr(protocolID)
	event.ProtocolVersionID = textPtr(versionID)
	event.RuleID = textPtr(ruleID)
	event.VaccineName = textPtr(vaccineName)
	event.DoseCode = textPtr(doseCode)
	event.AssigneeLabel = textPtr(assignee)
	event.ExecutorRole = textPtr(executor)
	event.VerifierLabel = textPtr(verifier)
	event.Links = rawOrObject(links, nil)
	if linksOut != nil {
		*linksOut = links
	}
	return event, nil
}

func scanHistoryItem(rows pgx.Rows) (domain.CalendarHistoryItem, error) {
	var item domain.CalendarHistoryItem
	var actor, channel, reason, trace pgtype.Text
	var details []byte
	if err := rows.Scan(
		&item.HistoryID, &item.EventType, &item.Status, &item.Title, &actor,
		&item.OccurredAt, &channel, &reason, &trace, &item.SourceTable, &details,
	); err != nil {
		return domain.CalendarHistoryItem{}, err
	}
	item.ActorLabel = textPtr(actor)
	item.Channel = textPtr(channel)
	item.Reason = textPtr(reason)
	item.TraceID = textPtr(trace)
	item.Details = rawOrObject(details, nil)
	return item, nil
}

type detailBlocks map[string]json.RawMessage

func decodeDetailBlocks(raw []byte) detailBlocks {
	if len(raw) == 0 {
		return detailBlocks{}
	}
	var blocks detailBlocks
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return detailBlocks{}
	}
	return blocks
}

func (d detailBlocks) raw(key string) json.RawMessage {
	if raw, ok := d[key]; ok && len(raw) > 0 && string(raw) != "null" {
		return raw
	}
	return json.RawMessage(`{}`)
}

func (d detailBlocks) stringList(key string) []string {
	raw, ok := d[key]
	if !ok || len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func rawOrObject(primary []byte, fallback []byte) json.RawMessage {
	if len(primary) > 0 && string(primary) != "null" {
		return json.RawMessage(primary)
	}
	if len(fallback) > 0 && string(fallback) != "null" {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(`{}`)
}

func normalizeChannel(requested, primary string) string {
	switch requested {
	case "local-stub", "push_fcm", "slack", "email", "webhook", "incident", "opsgenie", "pagerduty":
		return requested
	}
	switch primary {
	case "local-stub", "push_fcm", "slack", "email", "webhook", "incident", "opsgenie", "pagerduty":
		return primary
	case "push - FCM":
		return "push_fcm"
	case "Slack":
		return "slack"
	default:
		return "local-stub"
	}
}

func textPtr(v pgtype.Text) *string {
	if !v.Valid || v.String == "" {
		return nil
	}
	s := v.String
	return &s
}

func timePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}
