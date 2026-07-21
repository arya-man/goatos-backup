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
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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
	// 5k-50k envelope (docs/decisions/operational-kernel-5k-50k-scale-envelope.md, U7): canonical is
	// now the ONLY serving path -- calendar_event_projections and its freshness watermark
	// (calendar_projection_state) are gone, so there is no stale/never-synced/partial-coverage case to
	// gate on or fail closed for. A canonical read cannot be stale relative to the canonical write.
	// completed/history rows are served by the SAME canonical_selected predicate as everything else
	// (status IN (...,'completed') within its 90-day retention window) -- there is no longer a
	// separate history projection or freshness gate to consult.
	projection := domain.ProjectionMetadata{ServingState: "canonical", FreshnessStatus: "green", Stale: false}
	requestedToExclusive := q.DateTo.Add(24 * time.Hour)
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
	vaccine := ""
	if q.Vaccine != nil {
		vaccine = *q.Vaccine
	}
	var cursorDue any
	cursorEventID := ""
	if q.Cursor != nil {
		cursorDue = q.Cursor.DueAt
		cursorEventID = q.Cursor.EventID
	}
	tenantWide, parkIDs, shedIDs := scopeArgs(q.Scope)
	// Canonical is the only path now: no freshness gate, no read-through fallback branch.
	items := []domain.CalendarEvent{}
	if !q.MarkersOnly {
		var err error
		items, err = r.listEventsCanonical(ctx, q.TenantID, q.DateFrom, requestedToExclusive,
			ownerKey, status, parkID, shedID, vaccine, cursorDue, cursorEventID, fetchLimit, tenantWide, parkIDs, shedIDs, q.IncludeDriveSummary)
		if err != nil {
			return domain.CalendarEventListResponse{}, err
		}
	}
	dateMarkers := []domain.CalendarDateMarker{}
	if q.IncludeDateMarkers {
		markerRows, err := r.pool.Query(ctx, calendarDateMarkersSQL,
			q.TenantID, q.DateFrom, requestedToExclusive, ownerKey, status, parkID, shedID,
			tenantWide, parkIDs, shedIDs, vaccine)
		if err != nil {
			return domain.CalendarEventListResponse{}, fmt.Errorf("calendar: list date markers: %w", err)
		}
		defer markerRows.Close()
		for markerRows.Next() {
			var marker domain.CalendarDateMarker
			if err := markerRows.Scan(
				&marker.Date,
				&marker.EventCount,
				&marker.CompletedCount,
				&marker.OpenCount,
				&marker.DriveCount,
				&marker.DueCount,
				&marker.OverdueCount,
				&marker.DeferredCount,
			); err != nil {
				return domain.CalendarEventListResponse{}, fmt.Errorf("calendar: scan date marker: %w", err)
			}
			dateMarkers = append(dateMarkers, marker)
		}
		if err := markerRows.Err(); err != nil {
			return domain.CalendarEventListResponse{}, err
		}
	}
	// DRV-005: reminder_rail is a backend-computed, whole-filtered-week summary -- never the
	// frontend filtering whatever page of Items it happens to hold (a reminder on list page 2 would
	// otherwise vanish from the rail). Same trigger (q.IncludeDateMarkers) and same owner/park/shed/
	// date scope as the date-marker query above; the total Count comes from a single bounded, indexed
	// query's count(*) OVER() window, independent of the reminderRailLimit-sized Items preview.
	var reminderRail *domain.CalendarReminderRail
	if q.IncludeReminderRail {
		rail, err := r.reminderRail(ctx, q.TenantID, q.DateFrom, requestedToExclusive, ownerKey, status, parkID, shedID, vaccine, tenantWide, parkIDs, shedIDs)
		if err != nil {
			return domain.CalendarEventListResponse{}, err
		}
		reminderRail = &rail
	}
	var filterOptions *domain.CalendarFilterOptions
	if q.IncludeFilterOptions {
		options, err := r.listFilterOptions(ctx, q)
		if err != nil {
			return domain.CalendarEventListResponse{}, err
		}
		filterOptions = &options
	}
	var next *string
	if !q.MarkersOnly && len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		cursor, err := domain.EncodeCalendarCursor(domain.CalendarCursor{DueAt: last.DueAt, EventID: last.EventID})
		if err != nil {
			return domain.CalendarEventListResponse{}, err
		}
		next = &cursor
	}
	// HistoryProjection is always nil now: completed/history rows are served by the same canonical
	// predicate as everything else, so there is no separate history-projection freshness to report.
	return domain.CalendarEventListResponse{Source: domain.SourceAPI, Items: items, DateMarkers: dateMarkers, FilterOptions: filterOptions, NextCursor: next, Projection: projection, HistoryProjection: nil, ReminderRail: reminderRail}, nil
}

// listFilterOptions returns the location hierarchy and vaccines available to
// the current principal. One set-based query resolves all three collections;
// no per-park or per-shed lookups are performed.
func (r *Repository) listFilterOptions(ctx context.Context, q domain.Query) (domain.CalendarFilterOptions, error) {
	tenantWide, parkIDs, shedIDs := scopeArgs(q.Scope)
	rows, err := r.pool.Query(ctx, calendarFilterOptionsSQL,
		q.TenantID, tenantWide, parkIDs, shedIDs)
	if err != nil {
		return domain.CalendarFilterOptions{}, fmt.Errorf("calendar: list filter options: %w", err)
	}
	defer rows.Close()
	options := domain.CalendarFilterOptions{
		Parks:    []domain.CalendarFilterOption{},
		Sheds:    []domain.CalendarFilterOption{},
		Vaccines: []domain.CalendarFilterOption{},
	}
	for rows.Next() {
		var kind string
		var option domain.CalendarFilterOption
		if err := rows.Scan(&kind, &option.Value, &option.Label, &option.ParentValue); err != nil {
			return domain.CalendarFilterOptions{}, fmt.Errorf("calendar: scan filter option: %w", err)
		}
		switch kind {
		case "park":
			options.Parks = append(options.Parks, option)
		case "shed":
			options.Sheds = append(options.Sheds, option)
		case "vaccine":
			options.Vaccines = append(options.Vaccines, option)
		}
	}
	if err := rows.Err(); err != nil {
		return domain.CalendarFilterOptions{}, fmt.Errorf("calendar: iterate filter options: %w", err)
	}
	return options, nil
}

// reminderRailLimit bounds the reminder rail preview (task calls for "~20 items"). The whole-result
// Count field is independent of this bound -- it comes from the same query's count(*) OVER() window.
const reminderRailLimit = 20

// reminderRail runs calendarReminderRailSQL: a single bounded, indexed scan of calendar_event_projections
// for ACTIVE reminder/escalation events (same semantics as admin-web's hasReminderOrEscalation) within the
// requested week window, scoped identically to the main list/date-marker queries. EmptyMessage is left
// blank here -- the app-layer service fills it from the same CalendarPresentation.Week.ReminderEmptyMessage
// copy the week view already renders, so there is exactly one backend-owned literal, not a duplicate.
func (r *Repository) reminderRail(ctx context.Context, tenantID string, dateFrom, dateToExclusive time.Time, ownerKey, status, parkID, shedID, vaccine string, tenantWide bool, parkIDs, shedIDs []string) (domain.CalendarReminderRail, error) {
	rows, err := r.pool.Query(ctx, calendarReminderRailSQL,
		tenantID, dateFrom, dateToExclusive, ownerKey, status, parkID, shedID, tenantWide, parkIDs, shedIDs, reminderRailLimit, vaccine)
	if err != nil {
		return domain.CalendarReminderRail{}, fmt.Errorf("calendar: list reminder rail: %w", err)
	}
	defer rows.Close()
	rail := domain.CalendarReminderRail{Items: []domain.CalendarReminderRailItem{}}
	for rows.Next() {
		var eventID, title, subtitle, reminderState, escalationState, channel string
		var totalCount int
		if err := rows.Scan(&eventID, &title, &subtitle, &reminderState, &escalationState, &channel, &totalCount); err != nil {
			return domain.CalendarReminderRail{}, fmt.Errorf("calendar: scan reminder rail row: %w", err)
		}
		rail.Count = totalCount
		item := domain.CalendarReminderRailItem{
			EventID:         eventID,
			Title:           title,
			Subtitle:        subtitle,
			ReminderLabel:   reminderStateLabel(reminderState),
			EscalationLabel: escalationStateLabel(escalationState),
			Channels:        []string{},
		}
		if strings.TrimSpace(channel) != "" {
			item.Channels = []string{channel}
		}
		rail.Items = append(rail.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.CalendarReminderRail{}, err
	}
	return rail, nil
}

// reminderStateLabel/escalationStateLabel humanize the durable reminder_state/escalation_state kernel
// values for the reminder rail badge (mirrors admin-web's activeReminderState/activeEscalationState
// active-set semantics in calendar-contract.ts -- not_scheduled/none are never surfaced here because
// calendarReminderRailSQL's WHERE clause already excludes them).
func reminderStateLabel(state string) string {
	switch state {
	case "scheduled":
		return "Reminder scheduled"
	case "queued":
		return "Reminder queued"
	case "nudged", "sent":
		return "Reminder sent"
	case "snoozed":
		return "Reminder snoozed"
	case "escalated":
		return "Reminder escalated"
	default:
		return ""
	}
}

func escalationStateLabel(state string) string {
	switch state {
	case "pending":
		return "Escalation pending"
	case "queued":
		return "Escalation queued"
	case "escalated":
		return "Escalated"
	case "acknowledged":
		return "Escalation acknowledged"
	case "level_1_open":
		return "Escalated · L1"
	case "level_2_open":
		return "Escalated · L2"
	case "level_3_open":
		return "Escalated · L3"
	case "level_4_open":
		return "Escalated · L4"
	case "level_1_acknowledged":
		return "Escalated · L1 acknowledged"
	case "level_2_acknowledged":
		return "Escalated · L2 acknowledged"
	case "level_3_acknowledged":
		return "Escalated · L3 acknowledged"
	case "level_4_acknowledged":
		return "Escalated · L4 acknowledged"
	default:
		return ""
	}
}

func (r *Repository) GetEventDetail(ctx context.Context, q domain.EventQuery) (domain.CalendarEventDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var detailRaw, linksRaw []byte
	tenantWide, parkIDs, shedIDs := scopeArgs(q.Scope)
	// 5k-50k envelope: single-event detail is served directly from the canonical source_events
	// reconstruction (calendarCanonicalDetailSQL) -- calendar_event_projections and the separate
	// completed-history projection (calendarHistoryProjectionDetailSQL) are both gone. A completed
	// event stays resolvable under its live event_id within canonical's 90-day retention window; an
	// event older than that (previously reachable only via the separate unbounded-lookback history
	// projection) is no longer resolvable by detail lookup -- an accepted simplification for this
	// envelope (see docs/decisions/operational-kernel-5k-50k-scale-envelope.md).
	from, to := canonicalUnboundedWindow(time.Now())
	event, err := scanCalendarEventWithDetail(
		r.pool.QueryRow(ctx, calendarCanonicalDetailSQL, q.TenantID, from, to, q.EventID, tenantWide, parkIDs, shedIDs),
		&detailRaw,
		&linksRaw,
	)
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
		HistoryProjection:    nil,
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
	if directNotificationTargetReadOnly(target) {
		return domain.CalendarActionResponse{}, ports.ErrEventNotActionable
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
	// 5k-50k envelope: reminder_state is no longer a persisted/mutable column -- it is read-derived
	// from notification_requests/calendar_snoozes at read time (calendarReminderRailSQL), so there is
	// no projection row left to update here.
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
	if directNotificationTargetReadOnly(target) {
		return domain.CalendarActionResponse{}, ports.ErrEventNotActionable
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
	// 5k-50k envelope: reminder_state is read-derived (an active calendar_snoozes row is what
	// calendarReminderRailSQL checks), so there is no projection row left to update here.
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
  AND status = 'open'`, in.TenantID, esc.EscalationID, in.ActorID,
			// india-date-guard:ignore: owner=ravi issue=GH-india-date scope=escalation-ack-absolute-instant-storage expiry=2026-12-31
			time.Now().UTC(), in.Reason)
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
  AND status IN ('open', 'acknowledged')`, in.TenantID, esc.EscalationID, in.ActorID,
			// india-date-guard:ignore: owner=ravi issue=GH-india-date scope=escalation-resolve-absolute-instant-storage expiry=2026-12-31
			time.Now().UTC(), in.Reason)
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
RETURNING escalation_id::text, level`, in.TenantID, esc.ObligationID, in.ActorID,
		// india-date-guard:ignore: owner=ravi issue=GH-india-date scope=escalation-auto-resolve-absolute-instant-storage expiry=2026-12-31
		time.Now().UTC(), in.Reason)
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
	escalationActionDetailsJSON, err := marshalJSON("escalation action details", escalationActionDetails(in, target, esc, closure))
	if err != nil {
		return err
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
		escalationActionDetailsJSON,
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
	// 5k-50k envelope: escalation_state is read-derived from obligation_escalations at read time
	// (calendarReminderRailSQL), so there is no projection row left to update here.
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

// calendarReminderCandidatesSQL is the general (not-single-obligation) due-reminder sweep candidate
// set: the same predicate the pre-cutover projector-backed sweep used, now read directly off the
// canonical source_events reconstruction. Deliberately excludes event_type = 'vaccination_dose_due'
// (individual per-obligation rows never reminder independently of their batch/drive/SOP grouping) --
// see calendarCanonicalEventsCTE's doc comment. dateFrom is a wide lower bound (canonicalUnboundedWindow)
// so every open/overdue candidate is reachable regardless of how far in the past it fell due; dateTo is
// now()+1h, which both bounds the CTE's window AND matches the historical "due within the next hour"
// cutoff.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarReminderCandidatesSQL = "WITH " + calendarCanonicalEventsCTE + `
SELECT event_id, title, target_type, COALESCE(source_target_id::text, ''), primary_notification_channel,
       'Asia/Kolkata'
FROM source_events
WHERE system = false
  AND event_type <> 'vaccination_dose_due'
  AND due_at <= $3::timestamptz
  AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due')
  AND NOT EXISTS (
    SELECT 1
    FROM calendar_snoozes cs
    WHERE cs.tenant_id = $1::uuid
      AND cs.calendar_event_id = source_events.event_id
      AND cs.status = 'active'
      AND cs.snooze_until > now()
  )
  AND NOT EXISTS (
    SELECT 1
    FROM notification_requests nr
    WHERE nr.tenant_id = $1::uuid
      AND nr.idempotency_key = $1 || ':calendar.reminder:' || source_events.event_id || ':' || to_char((now() AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD')
  )
ORDER BY due_at ASC, event_id ASC
LIMIT $4`

// calendarReminderCandidateBySeq is the single-event variant of calendarReminderCandidatesSQL used by
// queueDueReminder. It cannot FOR UPDATE SKIP LOCKED a CTE-derived row (unlike the old direct
// calendar_event_projections read) -- that lock was only a contention courtesy under concurrent
// sweepers; the real idempotency guard is the INSERT ... ON CONFLICT (tenant_id, idempotency_key) DO
// NOTHING below, which still makes a concurrent double-queue a safe no-op.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarReminderCandidateBySeq = "WITH " + calendarCanonicalEventsCTE + `
SELECT event_id, title, target_type, COALESCE(source_target_id::text, ''), primary_notification_channel,
       'Asia/Kolkata'
FROM source_events
WHERE event_id = $4
  AND system = false
  AND event_type <> 'vaccination_dose_due'
  AND due_at <= $3::timestamptz
  AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due')
  AND NOT EXISTS (
    SELECT 1
    FROM calendar_snoozes cs
    WHERE cs.tenant_id = $1::uuid
      AND cs.calendar_event_id = source_events.event_id
      AND cs.status = 'active'
      AND cs.snooze_until > now()
  )
  AND NOT EXISTS (
    SELECT 1
    FROM notification_requests nr
    WHERE nr.tenant_id = $1::uuid
      AND nr.idempotency_key = $1 || ':calendar.reminder:' || source_events.event_id || ':' || to_char((now() AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD')
  )
LIMIT 1`

func (r *Repository) selectDueReminderEvents(ctx context.Context, tenantID string, limit int) ([]dueReminderEvent, error) {
	now := time.Now()
	from, _ := canonicalUnboundedWindow(now)
	dueBefore := now.Add(time.Hour)
	rows, err := r.pool.Query(ctx, calendarReminderCandidatesSQL, tenantID, from, dueBefore, limit)
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
	now := time.Now()
	from, _ := canonicalUnboundedWindow(now)
	dueBefore := now.Add(time.Hour)
	if err := tx.QueryRow(ctx, calendarReminderCandidateBySeq, tenantID, from, dueBefore, eventID).Scan(
		&e.EventID, &e.Title, &e.TargetType, &e.TargetID, &e.PrimaryChannel, &e.Timezone); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("calendar: lock reminder event: %w", err)
	}
	channel := normalizeChannel("", e.PrimaryChannel)
	key := tenantID + ":calendar.reminder:" + e.EventID + ":" + calendarBusinessDateIn(time.Now(), e.Timezone)
	contextJSON, err := marshalJSON("reminder context", map[string]any{"calendar_event_id": e.EventID, "sweeper": "calendar-reminder-sweeper"})
	if err != nil {
		return false, err
	}
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
		contextJSON).Scan(&requestID)
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
	// 5k-50k envelope: reminder_state is read-derived (calendarReminderRailSQL), so there is no
	// projection row left to update here.
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

// calendarEscalationCandidatesSQL is the general (not-single-obligation) escalation sweep candidate
// set, read directly off the canonical source_events reconstruction instead of
// calendar_event_projections. Excludes event_type = 'vaccination_dose_due' -- matches the pre-cutover
// projector-backed general sweep (individual per-obligation rows never escalate independently of
// their batch/drive/SOP grouping via this path; see selectEscalationEventForObligation for the one
// that does). dateFrom is a wide lower bound (canonicalUnboundedWindow); dateTo/$3 doubles as the
// shared CTE's window upper bound AND the "due at or before now" cutoff the original had.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarEscalationCandidatesSQL = "WITH " + calendarCanonicalEventsCTE + `,
candidates AS (
  SELECT
    event_id,
    due_at,
    CASE
      WHEN due_at <= $7::timestamptz THEN 4
      WHEN due_at <= $6::timestamptz THEN 3
      WHEN due_at <= $5::timestamptz THEN 2
      WHEN due_at <= $4::timestamptz THEN 1
      ELSE 0
    END AS level
  FROM source_events
  WHERE system = false
    AND event_type <> 'vaccination_dose_due'
    AND due_at IS NOT NULL
    AND due_at <= $3::timestamptz
    AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due')
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
LIMIT $8`

// calendarEscalationCandidateForObligationSQL is the single-obligation escalation lookup used by
// missed_handler.go (a durable obligation.missed event escalates immediately). Unlike
// calendarEscalationCandidatesSQL it deliberately does NOT exclude event_type = 'vaccination_dose_due'
// -- an individual obligation's own calendar event IS exactly that event_type (see
// calendarCanonicalEventsCTE's doc comment), and this is the one path that targets it directly by
// event_id/source_target_id rather than by the general due-window sweep.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarEscalationCandidateForObligationSQL = "WITH " + calendarCanonicalEventsCTE + `,
candidate AS (
  SELECT
    event_id,
    CASE
      WHEN due_at <= $7::timestamptz THEN 4
      WHEN due_at <= $6::timestamptz THEN 3
      WHEN due_at <= $5::timestamptz THEN 2
      WHEN due_at <= $4::timestamptz THEN 1
      ELSE 0
    END AS level
  FROM source_events
  WHERE (
      event_id = 'obligation:' || $8::text
      OR (source_target_type = 'obligation' AND source_target_id = $8::uuid)
    )
    AND system = false
    AND due_at IS NOT NULL
    AND due_at <= $3::timestamptz
    AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due')
)
SELECT event_id, level
FROM candidate c
WHERE level > 0
  AND NOT EXISTS (
    SELECT 1
    FROM notification_requests nr
    WHERE nr.tenant_id = $1::uuid
      AND nr.idempotency_key = $1 || ':calendar.escalation:' || c.event_id || ':level:' || c.level::text
  )`

func (r *Repository) selectEscalationEvents(ctx context.Context, in ports.SweepEscalations) ([]escalationEvent, error) {
	level1Cutoff := in.Now.Add(-in.Level1After)
	level2Cutoff := in.Now.Add(-in.Level2After)
	level3Cutoff := in.Now.Add(-in.Level3After)
	level4Cutoff := in.Now.Add(-in.Level4After)
	obligationID := strings.TrimSpace(in.ObligationID)
	if obligationID != "" {
		return r.selectEscalationEventForObligation(ctx, in, obligationID, level1Cutoff, level2Cutoff, level3Cutoff, level4Cutoff)
	}
	from, _ := canonicalUnboundedWindow(in.Now)
	rows, err := r.pool.Query(ctx, calendarEscalationCandidatesSQL,
		in.TenantID, from, in.Now, level1Cutoff, level2Cutoff, level3Cutoff, level4Cutoff, in.Limit)
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

func (r *Repository) selectEscalationEventForObligation(ctx context.Context, in ports.SweepEscalations, obligationID string, level1Cutoff, level2Cutoff, level3Cutoff, level4Cutoff time.Time) ([]escalationEvent, error) {
	var event escalationEvent
	from, _ := canonicalUnboundedWindow(in.Now)
	err := r.pool.QueryRow(ctx, calendarEscalationCandidateForObligationSQL,
		in.TenantID, from, in.Now, level1Cutoff, level2Cutoff, level3Cutoff, level4Cutoff, obligationID).Scan(&event.EventID, &event.Level)
	if errors.Is(err, pgx.ErrNoRows) {
		return []escalationEvent{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("calendar: select obligation escalation sweep: %w", err)
	}
	return []escalationEvent{event}, nil
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

// calendarEscalationTargetSQL resolves the escalation target for one calendar event by ID, straight
// from the canonical source_events reconstruction. No FOR UPDATE (a CTE-derived row cannot be locked
// like the old direct calendar_event_projections read could) -- that lock was a contention courtesy
// under concurrent sweepers, not the correctness guard; the INSERT ... ON CONFLICT DO NOTHING below
// (both for obligation_escalations and notification_requests) is what makes a concurrent double-queue
// safe. Deliberately no event_type filter, matching the pre-cutover target-by-ID lookup: it must
// resolve an individual 'obligation:<id>' (vaccination_dose_due) row when that is the actual target.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarEscalationTargetSQL = "WITH " + calendarCanonicalEventsCTE + `
SELECT event_id, title, status, source_target_type, COALESCE(source_target_id::text, ''),
       primary_notification_channel, source_target_type, COALESCE(source_target_id::text, ''),
       COALESCE(executor_role, ''), COALESCE(verifier_label, ''), due_at
FROM source_events
WHERE event_id = $4
  AND system = false
  AND due_at <= $3::timestamptz
  AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due')
LIMIT 1`

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
	from, _ := canonicalUnboundedWindow(now)
	if err := tx.QueryRow(ctx, calendarEscalationTargetSQL, tenantID, from, now, eventID).Scan(
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
			escalationStatusJSON, err := marshalJSON("obligation escalation status event", map[string]any{"level": level, "role": role, "calendar_event_id": target.EventID, "escalation_id": escalationID})
			if err != nil {
				return false, err
			}
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
				escalationStatusJSON,
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
	contextJSON, err := marshalJSON("escalation context", map[string]any{
		"calendar_event_id":        target.EventID,
		"source_target_type":       target.SourceTargetType,
		"source_target_id":         target.SourceTargetID,
		"escalation_level":         level,
		"escalated_to_role":        role,
		"obligation_escalation_id": escalationID,
		"due_at":                   target.DueAt.UTC().Format(time.RFC3339Nano),
		"sweeper":                  "calendar-escalation-sweeper",
	})
	if err != nil {
		return false, err
	}
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
	// 5k-50k envelope: status/severity/reminder_state/escalation_state are all read-derived now --
	// status/severity from due_at vs now() (canonical_read.go's CASE expressions; an escalated event
	// is already overdue by construction, since escalation only fires once a level cutoff has passed),
	// reminder_state/escalation_state from notification_requests/obligation_escalations
	// (calendarReminderRailSQL). There is no projection row left to update here.
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
		return permissions.RolePCDirector
	case level == 2:
		return permissions.RoleParkHead
	case target.Status == "verification_pending":
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
	case permissions.RolePCDirector:
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
	return biztime.BusinessDateIn(t, timezone)
}

func escalationReason(target escalationTarget, level int) string {
	return fmt.Sprintf("calendar event %s overdue at level %d; status=%s due_at=%s", target.EventID, level, target.Status, target.DueAt.UTC().Format(time.RFC3339))
}

// QueueRoleNotifications writes one notification_requests row per recipient device via a single
// set-based INSERT ... SELECT FROM unnest(...) -- no per-recipient round trip, no N+1, regardless of
// how many recipients are resolved (bounded by workforce headcount, never herd-scale). Idempotency
// key/fingerprint are computed here (device-scoped, keyed by in.EventKey) so an exact replay of the
// SAME triggering event is a guaranteed no-op per recipient (ON CONFLICT DO NOTHING), while a
// different event for the same completion (e.g. a later resubmission's fresh verification_pending)
// gets its own rows.
func (r *Repository) QueueRoleNotifications(ctx context.Context, in ports.QueueRoleNotifications) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	deviceIDs := make([]string, len(in.Recipients))
	fcmTokens := make([]string, len(in.Recipients))
	idempotencyKeys := make([]string, len(in.Recipients))
	fingerprints := make([]string, len(in.Recipients))
	contexts := make([]string, len(in.Recipients))
	for i, recipient := range in.Recipients {
		idempotencyKeys[i] = in.TenantID + ":" + in.EventKey + ":device:" + recipient.DeviceID
		fingerprints[i] = requestFingerprint(in.TenantID, in.EventKey, in.NotificationType, recipient.DeviceID)
		deviceIDs[i] = recipient.DeviceID
		fcmTokens[i] = recipient.FCMToken
		contextData := map[string]any{
			"priority":  in.Priority,
			"role":      recipient.RoleLabel,
			"member_id": recipient.MemberID,
			"event_key": in.EventKey,
			"channel":   in.Channel,
			"source":    "notificationbridge.verification",
		}
		// Merge any optional context fields from the caller (e.g., type, obligation_id, park_id for FCM).
		if in.Context != nil {
			for key, value := range in.Context {
				contextData[key] = value
			}
		}
		contextJSON, err := json.Marshal(contextData)
		if err != nil {
			return 0, fmt.Errorf("calendar: encode queue-role-notification context: %w", err)
		}
		contexts[i] = string(contextJSON)
	}
	var targetID any
	if strings.TrimSpace(in.TargetID) != "" {
		targetID = in.TargetID
	}
	rows, err := r.pool.Query(ctx, `
WITH input AS (
  SELECT device_id, fcm_token, idempotency_key, request_fingerprint, context_text::jsonb AS context
  FROM unnest($3::text[], $4::text[], $5::text[], $6::text[], $7::text[])
    AS t(device_id, fcm_token, idempotency_key, request_fingerprint, context_text)
)
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  recipient_ref, title, body, status, idempotency_key, request_fingerprint, context, trace_id
)
SELECT
  $1::uuid, $2, $8, $9::uuid, $10, $11,
  input.fcm_token, $12, $13, 'queued',
  input.idempotency_key, input.request_fingerprint, input.context, $14
FROM input
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING notification_request_id`,
		in.TenantID, in.CalendarEventID, deviceIDs, fcmTokens, idempotencyKeys, fingerprints, contexts,
		in.TargetType, targetID, in.NotificationType, in.Channel, in.Title, in.Body, in.TraceID,
	)
	if err != nil {
		return 0, fmt.Errorf("calendar: queue role notifications: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("calendar: queue role notifications rows: %w", err)
	}
	return count, nil
}

// calendarCanonicalExistsSQL checks event existence straight off the canonical source_events
// reconstruction. The separate calendar_history_projection_rows branch is gone along with the rest of
// the history projection (see canonical_read.go's doc comment on GetEventDetail's residual: an event
// older than canonical's 90-day retention window is no longer resolvable by ID).
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarCanonicalExistsSQL = "WITH " + calendarCanonicalEventsCTE + `
SELECT EXISTS (
  SELECT 1 FROM source_events
  WHERE event_id = $4
    AND ($5::bool OR park_id = ANY($6::uuid[]) OR shed_id = ANY($7::uuid[]))
)`

func (r *Repository) eventExists(ctx context.Context, tenantID, eventID string, scope domain.ScopeFilter) error {
	var exists bool
	tenantWide, parkIDs, shedIDs := scopeArgs(scope)
	from, to := canonicalUnboundedWindow(time.Now())
	if err := r.pool.QueryRow(ctx, calendarCanonicalExistsSQL, tenantID, from, to, eventID, tenantWide, parkIDs, shedIDs).Scan(&exists); err != nil {
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
	// obligation-sourced calendar events are always keyed 'obligation:' || obligation_id (see
	// calendarCanonicalEventsCTE's obligation_events CTE) -- match by that naming convention directly
	// instead of joining through the now-retired calendar_event_projections table.
	if err := r.pool.QueryRow(ctx, `
SELECT oe.escalation_id::text, 'obligation:' || oe.obligation_id::text, oe.status
FROM obligation_escalations oe
WHERE oe.tenant_id = $1::uuid
  AND 'obligation:' || oe.obligation_id::text = $2
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

// calendarActionTargetSQL resolves the action target (nudge/snooze/escalation) for one calendar event
// by ID, straight from the canonical source_events reconstruction. No FOR UPDATE: the row it used to
// lock (calendar_event_projections) no longer has any mutable reminder/escalation state for this
// transaction to protect -- that state is entirely read-derived now (calendarReminderRailSQL) -- and a
// CTE-derived row cannot be locked anyway. The transaction's real serialization is reserveIdempotency's
// idempotency_keys row, unaffected by this change.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarActionTargetSQL = "WITH " + calendarCanonicalEventsCTE + `
SELECT event_id, event_type, title, source_target_type, COALESCE(source_target_id::text, ''),
       primary_notification_channel, COALESCE(assignee_label, ''), COALESCE(executor_role, ''),
       COALESCE(verifier_label, ''), COALESCE(park_id::text, ''), COALESCE(shed_id::text, ''),
       source_backed, system
FROM source_events
WHERE event_id = $4
  AND ($5::bool OR park_id = ANY($6::uuid[]) OR shed_id = ANY($7::uuid[]))
LIMIT 1`

func loadActionTarget(ctx context.Context, tx pgx.Tx, tenantID, eventID string, scope domain.ScopeFilter) (actionTarget, error) {
	var out actionTarget
	var targetID, assignee, executor, verifier pgtype.Text
	var sourceBacked, system bool
	tenantWide, parkIDs, shedIDs := scopeArgs(scope)
	from, to := canonicalUnboundedWindow(time.Now())
	err := tx.QueryRow(ctx, calendarActionTargetSQL, tenantID, from, to, eventID, tenantWide, parkIDs, shedIDs).Scan(
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
	if system || out.RecipientRef == "" {
		return actionTarget{}, ports.ErrEventNotActionable
	}
	return out, nil
}

func directNotificationTargetReadOnly(target actionTarget) bool {
	return target.TargetType == "catchup" || target.EventType == "vaccination_dose_due"
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

// calendarDateMarkersSQL is the bounded month-grid summary. The LIVE branch reads the same canonical
// source_events reconstruction as the list (no separate due-window filter needed -- source_events is
// already bounded by $2/$3 inside calendarCanonicalEventsCTE). The HISTORY branch replaces the retired
// calendar_history_date_markers projector with a direct, live aggregate over vaccination_completions
// (ACCEPTED doses only) -- the same canonical source the projector itself replayed from, now read
// straight on the request path since there is no longer a separate history projection to gate on.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
// projection-review: membership=non-system canonical live vaccination events plus accepted vaccination completions inside the requested business-date window after owner/status/vaccine/authorization scope predicates; group_key=Asia/Kolkata marker_date with one final row per date; join_cardinality=live source_events are already canonical one-row events and each history completion joins one obligation/version/definition while vaccine dimensions are tested by correlated EXISTS so one-to-many rule dimensions cannot fan out counts; pagination=the bounded month marker set is aggregated in full before the separately paged event list and contains no LIMIT; scope=tenant plus explicit park/shed filters and the same tenant-wide or authorized park/shed grant arrays on both live and history branches
const calendarDateMarkersSQL = "WITH " + calendarCanonicalEventsCTE + `,
marker_rows AS (
  SELECT
    ((due_at AT TIME ZONE 'Asia/Kolkata')::date)::text AS marker_date,
    count(*)::bigint AS event_count,
    count(*) FILTER (WHERE status = 'completed')::bigint AS completed_count,
    count(*) FILTER (WHERE status NOT IN ('completed', 'canceled'))::bigint AS open_count,
    count(*) FILTER (WHERE event_type = 'vaccination_drive')::bigint AS drive_count,
    count(*) FILTER (WHERE status = 'due')::bigint AS due_count,
    count(*) FILTER (WHERE status = 'overdue')::bigint AS overdue_count,
    count(*) FILTER (WHERE status = 'deferred')::bigint AS deferred_count
  FROM source_events
  WHERE system = false
    AND due_at >= $2::timestamptz
    AND due_at < $3::timestamptz
    AND event_type <> 'vaccination_dose_due'
    AND event_type <> 'vaccination_history'
    AND ($4::text = '' OR owner_key = $4::text)
    AND ($5::text = '' OR status = $5::text)
    AND ($5::text <> '' OR status NOT IN ('completed', 'canceled'))
    AND ($6::text = '' OR park_id::text = nullif($6::text, ''))
    AND ($7::text = '' OR shed_id::text = nullif($7::text, ''))
    AND (
      $11::text = ''
      OR vaccine_name = $11::text
      OR (
        jsonb_typeof(detail->'summary'->'vaccine_labels') = 'array'
        AND (detail->'summary'->'vaccine_labels') ? $11::text
      )
    )
    AND ($8::bool OR park_id = ANY($9::uuid[]) OR shed_id = ANY($10::uuid[]))
  GROUP BY (due_at AT TIME ZONE 'Asia/Kolkata')::date

  UNION ALL

  -- Accepted vaccination administration history, live off vaccination_completions/obligation_instances/
  -- protocol_* -- the canonical join the retired history projector used to replay off the request path.
  -- Scoped by the completed obligation's own park/shed (goat's current location, falling back to the
  -- obligation/batch scope) so it matches the same owner/park/shed filters as the live branch above.
  SELECT
    ((COALESCE(vc.administered_at, vc.created_at) AT TIME ZONE 'Asia/Kolkata')::date)::text AS marker_date,
    count(*)::bigint AS event_count,
    count(*)::bigint AS completed_count,
    0::bigint AS open_count,
    0::bigint AS drive_count,
    0::bigint AS due_count,
    0::bigint AS overdue_count,
    0::bigint AS deferred_count
  FROM vaccination_completions vc
  JOIN obligation_instances oi
    ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id AND pd.category = 'vaccination'
  LEFT JOIN goats g
    ON oi.target_type = 'goat' AND g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id AND ob.batch_id = oi.batch_id
  WHERE vc.tenant_id = $1::uuid
    AND vc.status = 'accepted'
    AND ($4::text = '' OR $4::text = 'pc')
    AND ($5::text = '' OR $5::text = 'completed')
    AND ($6::text = '' OR COALESCE(g.park_id, ob.scope_id, oi.scope_id)::text = nullif($6::text, ''))
    AND ($7::text = '' OR COALESCE(g.shed_id, ob.scope_id, oi.scope_id)::text = nullif($7::text, ''))
    AND (
      $11::text = ''
      OR EXISTS (
        SELECT 1
        FROM protocol_rule_dimensions prd
        WHERE prd.tenant_id = oi.tenant_id
          AND prd.rule_id = oi.rule_id
          AND COALESCE(NULLIF(prd.vaccine_json->>'name', ''), pd.name) = $11::text
      )
      OR (
        NOT EXISTS (
          SELECT 1
          FROM protocol_rule_dimensions prd
          WHERE prd.tenant_id = oi.tenant_id
            AND prd.rule_id = oi.rule_id
        )
        AND pd.name = $11::text
      )
    )
    AND (COALESCE(vc.administered_at, vc.created_at) AT TIME ZONE 'Asia/Kolkata')::date >= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
    AND (COALESCE(vc.administered_at, vc.created_at) AT TIME ZONE 'Asia/Kolkata')::date < ($3::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
    AND ($8::bool OR COALESCE(g.park_id, ob.scope_id, oi.scope_id) = ANY($9::uuid[]) OR COALESCE(g.shed_id, ob.scope_id, oi.scope_id) = ANY($10::uuid[]))
  GROUP BY (COALESCE(vc.administered_at, vc.created_at) AT TIME ZONE 'Asia/Kolkata')::date
)
SELECT marker_date,
       sum(event_count)::int,
       sum(completed_count)::int,
       sum(open_count)::int,
       sum(drive_count)::int,
       sum(due_count)::int,
       sum(overdue_count)::int,
       sum(deferred_count)::int
FROM marker_rows
GROUP BY marker_date
ORDER BY marker_date`

// calendarFilterOptionsSQL resolves every filter dimension with one bounded,
// set-based read. Location visibility is enforced with the same tenant/park/shed
// grant arrays used by Calendar events. Vaccines are tenant-scoped published
// protocol metadata, so option discovery does not reconstruct the large
// canonical event CTE or issue one lookup per location.
const calendarFilterOptionsSQL = `
WITH scoped_parks AS (
  SELECT p.location_id, COALESCE(NULLIF(p.location_code, ''), p.name) AS label
  FROM locations p
  WHERE p.tenant_id = $1::uuid
    AND p.location_type = 'park'
    AND p.status = 'active'
    AND (
      $2::bool
      OR p.location_id = ANY($3::uuid[])
      OR EXISTS (
        SELECT 1
        FROM locations granted_shed
        WHERE granted_shed.tenant_id = p.tenant_id
          AND granted_shed.parent_location_id = p.location_id
          AND granted_shed.location_id = ANY($4::uuid[])
      )
    )
),
scoped_sheds AS (
  SELECT s.location_id, s.name AS label, s.parent_location_id
  FROM locations s
  WHERE s.tenant_id = $1::uuid
    AND s.location_type = 'shed'
    AND s.status = 'active'
    AND (
      $2::bool
      OR s.parent_location_id = ANY($3::uuid[])
      OR s.location_id = ANY($4::uuid[])
    )
),
published_vaccines AS (
  SELECT DISTINCT COALESCE(
    NULLIF(prd.vaccine_json->>'name', ''),
    NULLIF(pr.eligibility_json->'vaccine'->>'display_name', ''),
    NULLIF(pr.eligibility_json->'vaccine'->>'name', ''),
    NULLIF(pr.eligibility_json->'vaccine'->>'code', ''),
    NULLIF(pd.name, '')
  ) AS label
  FROM protocol_rules pr
  JOIN protocol_versions pv
    ON pv.tenant_id = pr.tenant_id
   AND pv.protocol_version_id = pr.protocol_version_id
   AND pv.status = 'published'
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  LEFT JOIN protocol_rule_dimensions prd
    ON prd.tenant_id = pr.tenant_id
   AND prd.rule_id = pr.rule_id
  WHERE pr.tenant_id = $1::uuid
)
SELECT 'park'::text, location_id::text, label, NULL::text
FROM scoped_parks
UNION ALL
SELECT 'shed'::text, location_id::text, label, parent_location_id::text
FROM scoped_sheds
UNION ALL
SELECT 'vaccine'::text, label, label, NULL::text
FROM published_vaccines
WHERE label IS NOT NULL
ORDER BY 1, 3, 2`

// projection-review: membership=calendar_event_projections rows in the requested [$6,$7) window matching the same owner/status/park/shed/scope filters as the list/date-marker queries (system=false, slice_key='vaccination', event_type <> 'vaccination_dose_due'), LEFT JOINed to at most one row each from calendar_snoozes (active_snooze), notification_requests (latest_reminder/latest_escalation, one row per calendar_event_id via DISTINCT ON), and obligation_escalations (obligation_escalation_active, one row per obligation_id via DISTINCT ON) -- every join side is itself grouped to at most one row per join key before the join, so none of these LEFT JOINs can fan out a candidate row; group_key=event_id (one row per already-materialized projection row plus its at-most-one derived state row, no grouping/aggregation beyond the window count(*) OVER() total); join_cardinality=candidates 1:{0,1} against each derived CTE (all four are pre-deduplicated to their join key), so no dimension fan-out to dedupe; pagination=whole-result total (count(*) OVER() over the FULL filtered week window, independent of the ~20-row LIMIT below) AND bounded-execution (one indexed query per ListEvents(IncludeReminderRail) call: the notification_requests/calendar_snoozes/obligation_escalations CTEs are each restricted via JOIN candidates to the same bounded date-window event set, using notification_requests_event_idx/calendar_snoozes_event_idx/(tenant_id, obligation_id) rather than a tenant-wide scan, LIMIT $11 keeps the returned preview small, never recomputed by filtering whatever page of Items the frontend currently holds); scope=park_id/shed_id read directly from calendar_event_projections' precomputed columns (already resolved at refresh time), same tenant-wide/park/shed scope predicate as calendarListSQL/calendarDateMarkersSQL
//
// U5 (operational-kernel-5k-50k-scale-envelope ADR step 5): this query used to read the persisted
// reminder_state/escalation_state columns on calendar_event_projections. Those columns are still
// written (for now -- U7 removes the whole table) but are no longer READ here, so the rail's
// contents no longer depend on calendar_event_projections' state columns and stay correct even if a
// sweeper crashed before writing them. Reminder/nudge/escalation activity is derived at read time
// from the same durable/canonical tables the sweepers already use for idempotency:
// notification_requests (the durable record of every reminder/nudge/escalation actually queued --
// see queueDueReminder/SendNudge/queueEscalation) and obligation_escalations (the canonical
// escalation ladder for obligation-sourced events -- see lockLatestEscalation/updateEscalationState).
// Priority mirrors the single mutable reminder_state/escalation_state field the old code kept:
// an active snooze always wins display-wise; otherwise, once ANY escalation notification has ever
// fired for the event (obligation-sourced or not) the reminder label is pinned to "escalated" and
// never reverts (acknowledge/resolve only ever changed escalation_state, never reminder_state, in
// the old code); otherwise the most recently requested reminder/nudge notification decides
// "queued" vs "nudged". escalation_state mirrors obligation_escalations while it has an open/
// acknowledged row (matching AcknowledgeEscalation/ResolveEscalation, which only ever act on
// obligation-sourced events -- see applyEscalationAction's target.TargetType != "obligation" guard);
// for composite/drive events (catchup/park_drive/batch/sop_task), which can never be acknowledged
// or resolved, it stays at the highest escalation level ever queued, exactly like the old
// escalation_state column did for those event types.
const calendarReminderRailSQL = "WITH " + calendarCanonicalEventsCTE + `,
candidates AS (
  SELECT event_id, title, subtitle, source_target_type, source_target_id, primary_notification_channel, due_at
  FROM source_events
  WHERE system = false
    AND event_type <> 'vaccination_dose_due'
    AND ($4::text = '' OR owner_key = $4::text)
    AND ($5::text = '' OR status = $5::text)
    AND ($6::text = '' OR park_id::text = nullif($6::text, ''))
    AND ($7::text = '' OR shed_id::text = nullif($7::text, ''))
    AND (
      $12::text = ''
      OR vaccine_name = $12::text
      OR (
        jsonb_typeof(detail->'summary'->'vaccine_labels') = 'array'
        AND (detail->'summary'->'vaccine_labels') ? $12::text
      )
    )
    AND ($8::bool OR park_id = ANY($9::uuid[]) OR shed_id = ANY($10::uuid[]))
),
active_snooze AS (
  SELECT DISTINCT cs.calendar_event_id
  FROM calendar_snoozes cs
  JOIN candidates c ON c.event_id = cs.calendar_event_id
  WHERE cs.tenant_id = $1::uuid AND cs.status = 'active' AND cs.snooze_until > now()
),
latest_reminder AS (
  SELECT DISTINCT ON (nr.calendar_event_id) nr.calendar_event_id, nr.notification_type
  FROM notification_requests nr
  JOIN candidates c ON c.event_id = nr.calendar_event_id
  WHERE nr.tenant_id = $1::uuid AND nr.notification_type IN ('reminder', 'nudge')
  ORDER BY nr.calendar_event_id, nr.requested_at DESC, nr.notification_request_id DESC
),
escalation_notification_level AS (
  SELECT nr.calendar_event_id, max(NULLIF(nr.context->>'escalation_level', '')::int) AS max_level
  FROM notification_requests nr
  JOIN candidates c ON c.event_id = nr.calendar_event_id
  WHERE nr.tenant_id = $1::uuid AND nr.notification_type = 'escalation'
  GROUP BY nr.calendar_event_id
),
obligation_escalation_active AS (
  SELECT DISTINCT ON (oe.obligation_id) oe.obligation_id, oe.level, oe.status
  FROM obligation_escalations oe
  JOIN candidates c ON c.source_target_type = 'obligation' AND c.source_target_id = oe.obligation_id
  WHERE oe.tenant_id = $1::uuid AND oe.status IN ('open', 'acknowledged')
  ORDER BY oe.obligation_id, oe.level DESC, oe.opened_at DESC
),
derived AS (
  SELECT
    c.event_id, c.title, c.subtitle, c.primary_notification_channel, c.due_at,
    CASE
      WHEN asn.calendar_event_id IS NOT NULL THEN 'snoozed'
      WHEN oea.obligation_id IS NOT NULL OR enl.calendar_event_id IS NOT NULL THEN 'escalated'
      WHEN lr.notification_type = 'nudge' THEN 'nudged'
      WHEN lr.notification_type = 'reminder' THEN 'queued'
      ELSE 'not_scheduled'
    END AS reminder_state,
    CASE
      WHEN oea.obligation_id IS NOT NULL AND oea.status = 'acknowledged' THEN 'level_' || oea.level::text || '_acknowledged'
      WHEN oea.obligation_id IS NOT NULL THEN 'level_' || oea.level::text || '_open'
      WHEN enl.calendar_event_id IS NOT NULL THEN 'level_' || COALESCE(enl.max_level, 1)::text || '_open'
      ELSE 'none'
    END AS escalation_state
  FROM candidates c
  LEFT JOIN active_snooze asn ON asn.calendar_event_id = c.event_id
  LEFT JOIN latest_reminder lr ON lr.calendar_event_id = c.event_id
  LEFT JOIN escalation_notification_level enl ON enl.calendar_event_id = c.event_id
  LEFT JOIN obligation_escalation_active oea ON c.source_target_type = 'obligation' AND oea.obligation_id = c.source_target_id
)
SELECT event_id, title, subtitle, reminder_state, escalation_state, primary_notification_channel,
       count(*) OVER ()::int AS total_count
FROM derived
WHERE
  reminder_state IN ('scheduled', 'queued', 'nudged', 'snoozed', 'sent', 'escalated')
  OR escalation_state IN (
    'pending', 'queued', 'escalated', 'acknowledged',
    'level_1_open', 'level_2_open', 'level_3_open', 'level_4_open',
    'level_1_acknowledged', 'level_2_acknowledged', 'level_3_acknowledged', 'level_4_acknowledged'
  )
ORDER BY due_at ASC, event_id ASC
LIMIT $11`

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
    'Snoozed until ' || to_char(snooze_until AT TIME ZONE 'Asia/Kolkata', 'YYYY-MM-DD HH24:MI') AS title,
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
  -- obligation-sourced calendar events are always keyed 'obligation:' || obligation_id (see
  -- calendarCanonicalEventsCTE's obligation_events CTE) -- match by that naming convention directly
  -- instead of joining through the now-retired calendar_event_projections table.
  WHERE oe.tenant_id = $1::uuid AND 'obligation:' || oe.obligation_id::text = $2

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
	var driveSummaryRaw []byte
	dest := []any{
		&event.EventID, &event.EventType, &event.OwnerKey, &event.Title, &event.Subtitle,
		&event.Status, &event.Severity, &event.DueAt, &windowStart, &windowEnd,
		&event.Timezone, &event.TimezoneSource, &parkID, &parkCode, &shedID, &shedName,
		&cohortID, &cohortName, &event.TargetType, &event.TargetCount, &protocolID,
		&versionID, &ruleID, &vaccineName, &doseCode, &event.SourceBacked,
		&event.SourceLabel, &assignee, &executor, &verifier, &event.ReminderState,
		&event.PrimaryNotificationChannel, &event.EscalationState, &event.System,
		&event.CrossCutting, &links, &event.Aggregated, &event.AllDay,
		&event.SummaryPrimary, &event.SummarySecondary, &event.SummaryTertiary,
		&event.ShedCount, &event.VaccineCount, &event.DriveCount, &event.CatchUpCount,
		&event.ScheduledCount, &event.DeferredCount, &event.ReviewCount,
		&event.ShedLabels, &event.VaccineLabels, &driveSummaryRaw,
	}
	if detail != nil {
		dest = append(dest, detail)
	}
	if err := rows.Scan(dest...); err != nil {
		return domain.CalendarEvent{}, err
	}
	// drive_summary is populated only for aggregated park-level vaccination_drive events; it is
	// NULL (nil) for every other event type and left as a nil *DriveSummary.
	if len(driveSummaryRaw) > 0 && string(driveSummaryRaw) != "null" {
		var ds domain.DriveSummary
		if err := json.Unmarshal(driveSummaryRaw, &ds); err != nil {
			return domain.CalendarEvent{}, fmt.Errorf("calendar: decode drive_summary: %w", err)
		}
		event.DriveSummary = &ds
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

// ResolveVaccinationCompletionContext resolves a vaccination completion_id to its obligation context
// (completion_id, obligation_id, park_id, sop_task_id, executor). Couples at the DB level only, never importing
// internal/vaccination or internal/sopbridge packages.
func (r *Repository) ResolveVaccinationCompletionContext(ctx context.Context, tenantID, completionID string) (ports.VaccinationCompletionContext, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var result ports.VaccinationCompletionContext
	err := r.pool.QueryRow(ctx, `
SELECT vc.completion_id::text, vc.obligation_id::text, oi.scope_id::text, oi.scope_type,
       COALESCE(oi.sop_task_id::text, ''), COALESCE(vc.recorded_by::text, '')
FROM vaccination_completions vc
JOIN obligation_instances oi ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
WHERE vc.tenant_id = $1::uuid AND vc.completion_id = $2::uuid
LIMIT 1`,
		tenantID, completionID).Scan(&result.CompletionID, &result.ObligationID, &result.ParkID, &result.ScopeType, &result.SOPTaskID, &result.ExecutedBy)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.VaccinationCompletionContext{}, ports.ErrNotFound
		}
		return ports.VaccinationCompletionContext{}, fmt.Errorf("calendar: resolve vaccination completion: %w", err)
	}

	return result, nil
}

func marshalJSON(label string, value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("calendar: marshal %s: %w", label, err)
	}
	return raw, nil
}
