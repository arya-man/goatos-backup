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
		"calendar_notification", requestID, "calendar.notifications", scopedKey, in.TraceID, response, in.EventID); err != nil {
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
		"calendar_snooze", snoozeID, "calendar.notifications", scopedKey, in.TraceID, response, in.EventID); err != nil {
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
}

func (r *Repository) selectDueReminderEvents(ctx context.Context, tenantID string, limit int) ([]dueReminderEvent, error) {
	rows, err := r.pool.Query(ctx, `
SELECT event_id, title, target_type, COALESCE(source_target_id::text, ''), primary_notification_channel
FROM calendar_event_projections
WHERE tenant_id = $1::uuid
  AND slice_key = 'vaccination'
  AND system = false
  AND due_at <= now() + interval '1 hour'
  AND status IN ('scheduled', 'due', 'overdue', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due', 'deferred', 'blocked')
  AND reminder_state IN ('not_scheduled', 'scheduled')
ORDER BY due_at ASC, event_id ASC
LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("calendar: select reminder sweep: %w", err)
	}
	defer rows.Close()
	events := []dueReminderEvent{}
	for rows.Next() {
		var e dueReminderEvent
		if err := rows.Scan(&e.EventID, &e.Title, &e.TargetType, &e.TargetID, &e.PrimaryChannel); err != nil {
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
SELECT event_id, title, target_type, COALESCE(source_target_id::text, ''), primary_notification_channel
FROM calendar_event_projections
WHERE tenant_id = $1::uuid
  AND event_id = $2
  AND slice_key = 'vaccination'
  AND system = false
  AND due_at <= now() + interval '1 hour'
  AND status IN ('scheduled', 'due', 'overdue', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due', 'deferred', 'blocked')
  AND reminder_state IN ('not_scheduled', 'scheduled')
ORDER BY due_at ASC, event_id ASC
LIMIT 1
FOR UPDATE SKIP LOCKED`, tenantID, eventID).Scan(&e.EventID, &e.Title, &e.TargetType, &e.TargetID, &e.PrimaryChannel); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("calendar: lock reminder event: %w", err)
	}
	channel := normalizeChannel("", e.PrimaryChannel)
	key := tenantID + ":calendar.reminder:" + e.EventID + ":" + time.Now().UTC().Format("2006-01-02")
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
		"calendar_notification", requestID, "calendar.notifications", key, "", resp, e.EventID); err != nil {
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

func (r *Repository) RefreshVaccinationProjection(ctx context.Context, in ports.RefreshVaccinationProjection) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var count int
	if err := r.pool.QueryRow(ctx, calendarVaccinationProjectionRefreshSQL,
		in.TenantID, in.DateFrom, in.DateTo, in.Limit).Scan(&count); err != nil {
		return 0, fmt.Errorf("calendar: refresh vaccination projection: %w", err)
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

type actionTarget struct {
	EventID        string
	Title          string
	TargetType     string
	TargetID       string
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
SELECT event_id, title, source_target_type, COALESCE(source_target_id::text, ''),
       primary_notification_channel, COALESCE(assignee_label, ''), COALESCE(executor_role, ''),
       COALESCE(verifier_label, ''), source_backed, system
FROM calendar_event_projections
WHERE tenant_id = $1::uuid AND event_id = $2 AND slice_key = 'vaccination'
  AND system = false
  AND ($3::bool OR park_id::text = ANY($4::text[]) OR shed_id::text = ANY($5::text[]))
FOR UPDATE`, tenantID, eventID, tenantWide, parkIDs, shedIDs).Scan(
		&out.EventID, &out.Title, &out.TargetType, &targetID, &out.PrimaryChannel,
		&assignee, &executor, &verifier, &sourceBacked, &system,
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
	if system || !sourceBacked || out.RecipientRef == "" {
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

func insertOutbox(ctx context.Context, tx pgx.Tx, tenantID, eventType, schemaVersion, aggregateType, aggregateID, topic, idempotencyKey, traceID string, payload any, calendarEventID string) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	headersBytes, _ := json.Marshal(map[string]any{
		"calendar_event_id": calendarEventID,
		"schema_version":    schemaVersion,
	})
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, gen_random_uuid(), $2, $3, $4, $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, nullif($10, ''), 'pending', now()
)`, tenantID, eventType, schemaVersion, aggregateType, aggregateID, topic, payloadBytes, headersBytes, idempotencyKey, traceID)
	if err != nil {
		return fmt.Errorf("calendar: insert outbox: %w", err)
	}
	return nil
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
      WHEN 'missed' THEN 'overdue'
      WHEN 'waived' THEN 'deferred'
      WHEN 'superseded' THEN 'canceled'
      ELSE oi.status
    END AS status,
    CASE
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
    AND status IN ('scheduled', 'due', 'overdue', 'in_progress', 'proof_pending',
                   'verification_pending', 'rejected', 'rework_due', 'deferred',
                   'blocked', 'completed', 'canceled')
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
      primary_notification_channel = EXCLUDED.primary_notification_channel,
      escalation_state = EXCLUDED.escalation_state,
      system = EXCLUDED.system,
      cross_cutting = EXCLUDED.cross_cutting,
      links = EXCLUDED.links,
      detail = EXCLUDED.detail,
      updated_at = now()
  RETURNING 1
)
SELECT count(*) FROM upserted`

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
	case "local-stub", "push_fcm", "slack", "email", "webhook":
		return requested
	}
	switch primary {
	case "local-stub", "push_fcm", "slack", "email", "webhook":
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
