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
	rows, err := r.pool.Query(ctx, calendarListSQL,
		q.TenantID, ownerKey, status, parkID, shedID, q.DateFrom, q.DateTo.Add(24*time.Hour), cursorDue, cursorEventID, fetchLimit)
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

func (r *Repository) GetEventDetail(ctx context.Context, tenantID, eventID string) (domain.CalendarEventDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var detailRaw, linksRaw []byte
	event, err := scanCalendarEventWithDetail(r.pool.QueryRow(ctx, calendarDetailSQL, tenantID, eventID), &detailRaw, &linksRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CalendarEventDetail{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.CalendarEventDetail{}, fmt.Errorf("calendar: get detail: %w", err)
	}
	blocks := decodeDetailBlocks(detailRaw)
	recent, err := r.History(ctx, domain.HistoryQuery{TenantID: tenantID, EventID: eventID, Limit: 10})
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
	if err := r.eventExists(ctx, q.TenantID, q.EventID); err != nil {
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

	target, err := loadActionTarget(ctx, tx, in.TenantID, in.EventID)
	if err != nil {
		return domain.CalendarActionResponse{}, err
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
	target, err := loadActionTarget(ctx, tx, in.TenantID, in.EventID)
	if err != nil {
		return domain.CalendarActionResponse{}, err
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
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
SELECT event_id, title, target_type, COALESCE(source_target_id::text, ''), primary_notification_channel
FROM calendar_event_projections
WHERE tenant_id = $1::uuid
  AND slice_key = 'vaccination'
  AND system = false
  AND due_at <= now() + interval '1 hour'
  AND status IN ('scheduled', 'due', 'overdue', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due', 'deferred', 'blocked')
  AND reminder_state IN ('not_scheduled', 'scheduled')
ORDER BY due_at ASC, event_id ASC
LIMIT $2
FOR UPDATE SKIP LOCKED`, tenantID, limit)
	if err != nil {
		return 0, fmt.Errorf("calendar: select reminder sweep: %w", err)
	}
	defer rows.Close()
	type dueEvent struct {
		EventID        string
		Title          string
		TargetType     string
		TargetID       string
		PrimaryChannel string
	}
	events := []dueEvent{}
	for rows.Next() {
		var e dueEvent
		if err := rows.Scan(&e.EventID, &e.Title, &e.TargetType, &e.TargetID, &e.PrimaryChannel); err != nil {
			return 0, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	count := 0
	for _, e := range events {
		channel := normalizeChannel("", e.PrimaryChannel)
		key := tenantID + ":calendar.reminder:" + e.EventID + ":" + time.Now().UTC().Format("2006-01-02")
		var requestID string
		err := tx.QueryRow(ctx, `
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
			continue
		}
		if err != nil {
			return count, fmt.Errorf("calendar: insert reminder: %w", err)
		}
		resp := domain.CalendarActionResponse{ActionID: requestID, EventID: e.EventID, ActionType: "reminder", Status: "queued", Channel: &channel}
		if err := insertOutbox(ctx, tx, tenantID, "calendar.reminder.queued", "calendar.notification.v1",
			"calendar_notification", requestID, "calendar.notifications", key, "", resp, e.EventID); err != nil {
			return count, err
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
			return count, fmt.Errorf("calendar: audit reminder: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE calendar_event_projections
SET reminder_state = 'queued',
    primary_notification_channel = $3,
    updated_at = now()
WHERE tenant_id = $1::uuid AND event_id = $2`, tenantID, e.EventID, channel); err != nil {
			return count, err
		}
		count++
	}
	if err := tx.Commit(ctx); err != nil {
		return count, err
	}
	return count, nil
}

func (r *Repository) eventExists(ctx context.Context, tenantID, eventID string) error {
	var exists bool
	if err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM calendar_event_projections
  WHERE tenant_id = $1::uuid AND event_id = $2 AND slice_key = 'vaccination'
)`, tenantID, eventID).Scan(&exists); err != nil {
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

func loadActionTarget(ctx context.Context, tx pgx.Tx, tenantID, eventID string) (actionTarget, error) {
	var out actionTarget
	var targetID, assignee, executor, verifier pgtype.Text
	var sourceBacked, system bool
	err := tx.QueryRow(ctx, `
SELECT event_id, title, source_target_type, COALESCE(source_target_id::text, ''),
       primary_notification_channel, COALESCE(assignee_label, ''), COALESCE(executor_role, ''),
       COALESCE(verifier_label, ''), source_backed, system
FROM calendar_event_projections
WHERE tenant_id = $1::uuid AND event_id = $2 AND slice_key = 'vaccination'
FOR UPDATE`, tenantID, eventID).Scan(
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
WHERE tenant_id = $1::uuid AND event_id = $2 AND slice_key = 'vaccination'`

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
