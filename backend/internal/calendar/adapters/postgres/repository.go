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
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	defaultQueryTimeout    = 3 * time.Second
	defaultProjectionFresh = 7 * time.Minute // > 5m refresh schedule; serve LKG through a late cycle instead of 503 (handoff P0-B)
)

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
	completedOnly := q.Status != nil && *q.Status == domain.StatusCompleted
	projection, err := r.servingProjection(ctx, q.TenantID, q.DateFrom, q.DateTo)
	// serveCanonical: U4a (ADR operational-kernel-5k-50k-scale-envelope) drops the
	// calendar_projection_state freshness gate as a fail-closed. A never-synced projection or a
	// requested window outside projected coverage no longer 503s; the non-completed (upcoming) slice
	// reads through to canonical tables via listEventsCanonical instead. A completed/accepted-history
	// list is unaffected: it is served by calendarListSQL's completed_history CTE
	// (vaccination_completions) under its own history-projection gate, independent of the upcoming
	// projection. Any error other than the two freshness signals still propagates.
	serveCanonical := false
	if err != nil {
		if !errors.Is(err, ports.ErrProjectionStale) && !errors.Is(err, ports.ErrProjectionUnavailable) {
			return domain.CalendarEventListResponse{}, err
		}
		if !completedOnly {
			serveCanonical = true
			projection = domain.ProjectionMetadata{
				Stale:           true,
				ServingState:    "canonical_read_through",
				FreshnessStatus: "canonical",
			}
		}
		// servingProjection returns best-effort metadata alongside ErrProjectionStale and a zero
		// value for ErrProjectionUnavailable; either is fine to surface for a canonical read.
	}
	// C5-002: the completed_history CTE branch of calendarListSQL only ever contributes rows when the
	// status filter is exactly "completed" (completedOnly); calendarDateMarkersSQL's history branch is
	// broader (it also contributes when no status narrows the request at all). Gate the HISTORY
	// projection's own freshness independently of the UPCOMING gate above -- this is what was missing
	// before: a never-synced history projection used to silently return zero rows instead of failing
	// closed.
	requestedToExclusive := q.DateTo.Add(24 * time.Hour)
	historyMarkersActive := q.IncludeDateMarkers && (q.Status == nil || strings.TrimSpace(*q.Status) == "" || *q.Status == domain.StatusCompleted)
	var historyProjection *domain.ProjectionMetadata
	if completedOnly || historyMarkersActive {
		meta, herr := r.historyProjectionFreshness(ctx, q.TenantID, true, q.DateFrom, requestedToExclusive)
		if herr != nil {
			return domain.CalendarEventListResponse{}, herr
		}
		historyProjection = &meta
	}
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
	var items []domain.CalendarEvent
	if serveCanonical {
		// Read-through: the upcoming projection is unavailable/out-of-window, so serve the
		// non-completed slice directly from canonical tables rather than failing closed.
		items, err = r.listEventsCanonical(ctx, q.TenantID, q.DateFrom, requestedToExclusive,
			ownerKey, status, parkID, shedID, cursorDue, cursorEventID, fetchLimit, tenantWide, parkIDs, shedIDs)
		if err != nil {
			return domain.CalendarEventListResponse{}, err
		}
	} else {
		rows, err := r.pool.Query(ctx, calendarListSQL,
			q.TenantID, ownerKey, status, parkID, shedID, q.DateFrom, requestedToExclusive, cursorDue, cursorEventID, fetchLimit,
			tenantWide, parkIDs, shedIDs)
		if err != nil {
			return domain.CalendarEventListResponse{}, fmt.Errorf("calendar: list events: %w", err)
		}
		defer rows.Close()
		items = []domain.CalendarEvent{}
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
		rows.Close()
	}
	dateMarkers := []domain.CalendarDateMarker{}
	if q.IncludeDateMarkers {
		markerRows, err := r.pool.Query(ctx, calendarDateMarkersSQL,
			q.TenantID, ownerKey, status, parkID, shedID, q.DateFrom, requestedToExclusive,
			tenantWide, parkIDs, shedIDs)
		if err != nil {
			return domain.CalendarEventListResponse{}, fmt.Errorf("calendar: list date markers: %w", err)
		}
		defer markerRows.Close()
		for markerRows.Next() {
			var marker domain.CalendarDateMarker
			if err := markerRows.Scan(&marker.Date, &marker.EventCount, &marker.CompletedCount, &marker.OpenCount, &marker.DriveCount); err != nil {
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
		rail, err := r.reminderRail(ctx, q.TenantID, ownerKey, status, parkID, shedID, q.DateFrom, requestedToExclusive, tenantWide, parkIDs, shedIDs)
		if err != nil {
			return domain.CalendarEventListResponse{}, err
		}
		reminderRail = &rail
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
	return domain.CalendarEventListResponse{Source: domain.SourceAPI, Items: items, DateMarkers: dateMarkers, NextCursor: next, Projection: projection, HistoryProjection: historyProjection, ReminderRail: reminderRail}, nil
}

// reminderRailLimit bounds the reminder rail preview (task calls for "~20 items"). The whole-result
// Count field is independent of this bound -- it comes from the same query's count(*) OVER() window.
const reminderRailLimit = 20

// reminderRail runs calendarReminderRailSQL: a single bounded, indexed scan of calendar_event_projections
// for ACTIVE reminder/escalation events (same semantics as admin-web's hasReminderOrEscalation) within the
// requested week window, scoped identically to the main list/date-marker queries. EmptyMessage is left
// blank here -- the app-layer service fills it from the same CalendarPresentation.Week.ReminderEmptyMessage
// copy the week view already renders, so there is exactly one backend-owned literal, not a duplicate.
func (r *Repository) reminderRail(ctx context.Context, tenantID, ownerKey, status, parkID, shedID string, dateFrom, dateToExclusive time.Time, tenantWide bool, parkIDs, shedIDs []string) (domain.CalendarReminderRail, error) {
	rows, err := r.pool.Query(ctx, calendarReminderRailSQL,
		tenantID, ownerKey, status, parkID, shedID, dateFrom, dateToExclusive, tenantWide, parkIDs, shedIDs, reminderRailLimit)
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

func (r *Repository) servingProjection(ctx context.Context, tenantID string, dateFrom, dateTo time.Time) (domain.ProjectionMetadata, error) {
	var meta domain.ProjectionMetadata
	var projectedFrom, projectedTo time.Time
	err := r.pool.QueryRow(ctx, `
SELECT projection_version, projected_at, freshness_status, serving_state, date_from, date_to
FROM calendar_projection_state
WHERE tenant_id = $1::uuid AND slice_key = 'vaccination'`, tenantID).Scan(
		&meta.ProjectionVersion, &meta.ProjectedAt, &meta.FreshnessStatus, &meta.ServingState, &projectedFrom, &projectedTo,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectionMetadata{}, ports.ErrProjectionUnavailable
	}
	if err != nil {
		return domain.ProjectionMetadata{}, fmt.Errorf("calendar: read projection state: %w", err)
	}
	if meta.ProjectionVersion <= 0 || meta.ServingState == "never_synced" {
		meta.Stale = true
		return meta, ports.ErrProjectionUnavailable
	}
	now := time.Now()
	meta.Stale = meta.ServingState != "fresh" || meta.FreshnessStatus != "green" ||
		now.Sub(meta.ProjectedAt) > defaultProjectionFresh || meta.ProjectedAt.After(now.Add(time.Minute))
	requestedToExclusive := dateTo.Add(24 * time.Hour)
	// Handler/business-date normalization can move equivalent boundaries by a
	// few milliseconds. Keep strict coverage while tolerating clock-call skew.
	if dateFrom.Before(projectedFrom.Add(-time.Minute)) || requestedToExclusive.After(projectedTo.Add(time.Minute)) {
		meta.Stale = true
		return meta, ports.ErrProjectionStale
	}
	return meta, nil
}

// historyProjectionFreshness gates a completed/history read against calendar_history_projection_state
// (C5-002). Unlike servingProjection (the fast-moving UPCOMING vaccination-execution/shed gate), a
// stale or partial-coverage history projection is NOT fail-closed: history is append-mostly, so serving
// last-known-good rows with the staleness/partial-coverage surfaced in the returned metadata is safer
// than a hard 503. The ONE fail-closed case is a projection that has NEVER completed a build (no
// serving_projection_version at all) -- an empty completed_history read is then indistinguishable from
// "genuinely no history", so this returns ports.ErrProjectionUnavailable instead of a misleadingly
// empty success, letting the caller tell "not built yet" apart from "no history".
//
// checkCoverage/dateFrom/dateToExclusive are only meaningful for a date-windowed read (ListEvents,
// calendarDateMarkersSQL); GetEventDetail's single-event lookup has no request-level date window to
// compare against and passes checkCoverage=false.
func (r *Repository) historyProjectionFreshness(ctx context.Context, tenantID string, checkCoverage bool, dateFrom, dateToExclusive time.Time) (domain.ProjectionMetadata, error) {
	var meta domain.ProjectionMetadata
	var servingVersion *int64
	var projectedFrom, projectedTo *time.Time
	err := r.pool.QueryRow(ctx, `
SELECT serving_projection_version, projected_at, freshness_status, serving_state, date_from, date_to
FROM calendar_history_projection_state
WHERE tenant_id = $1::uuid`, tenantID).Scan(
		&servingVersion, &meta.ProjectedAt, &meta.FreshnessStatus, &meta.ServingState, &projectedFrom, &projectedTo,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// No row at all: RecomputeVaccinationHistoryProjection has never run for this tenant.
		return domain.ProjectionMetadata{}, ports.ErrProjectionUnavailable
	}
	if err != nil {
		return domain.ProjectionMetadata{}, fmt.Errorf("calendar: read history projection state: %w", err)
	}
	if servingVersion == nil {
		// A row exists (a build was attempted) but has never completed successfully -- still
		// never-synced from the reader's point of view.
		return domain.ProjectionMetadata{}, ports.ErrProjectionUnavailable
	}
	meta.ProjectionVersion = *servingVersion
	meta.Stale = meta.ServingState != "fresh" || meta.FreshnessStatus != "green" ||
		time.Since(meta.ProjectedAt) > defaultHistoryProjectionFresh
	if checkCoverage && projectedFrom != nil && projectedTo != nil {
		// Same +/-1 minute clock-call-skew tolerance as servingProjection's coverage check.
		if dateFrom.Before(projectedFrom.Add(-time.Minute)) || dateToExclusive.After(projectedTo.Add(time.Minute)) {
			meta.PartialCoverage = true
		}
	}
	return meta, nil
}

func (r *Repository) GetEventDetail(ctx context.Context, q domain.EventQuery) (domain.CalendarEventDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var detailRaw, linksRaw []byte
	tenantWide, parkIDs, shedIDs := scopeArgs(q.Scope)
	var historyProjection *domain.ProjectionMetadata
	event, err := scanCalendarEventWithDetail(r.pool.QueryRow(ctx, calendarDetailSQL, q.TenantID, q.EventID, tenantWide, parkIDs, shedIDs), &detailRaw, &linksRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, ok := completedHistoryEventKey(q.EventID); ok {
			// C5-002: a single-event history lookup has no request-level date window, so
			// checkCoverage=false -- but a NEVER-synced history projection must still fail closed
			// (ErrProjectionUnavailable) rather than silently falling through to ErrNotFound, which
			// would misreport "not built yet" as "this event does not exist".
			meta, herr := r.historyProjectionFreshness(ctx, q.TenantID, false, time.Time{}, time.Time{})
			if herr != nil {
				return domain.CalendarEventDetail{}, herr
			}
			historyProjection = &meta
			// History detail now serves from the calendar_history_projection_rows projector output
			// (migration 000178 + history_projection.go) keyed directly by event_id, replacing the
			// canonical vaccination_completions/obligation_instances/protocol_* join that used to run
			// on every request.
			event, err = scanCalendarEventWithDetail(
				r.pool.QueryRow(ctx, calendarHistoryProjectionDetailSQL, q.TenantID, q.EventID, tenantWide, parkIDs, shedIDs),
				&detailRaw,
				&linksRaw,
			)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CalendarEventDetail{}, ports.ErrNotFound
		}
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
		HistoryProjection:    historyProjection,
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
       'Asia/Kolkata'
	FROM calendar_event_projections
	WHERE tenant_id = $1::uuid
	  AND slice_key = 'vaccination'
	  AND system = false
	  AND event_type <> 'vaccination_dose_due'
	  AND due_at <= now() + interval '1 hour'
	  AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due')
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
	      AND nr.idempotency_key = $1 || ':calendar.reminder:' || calendar_event_projections.event_id || ':' || to_char((now() AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD')
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
       'Asia/Kolkata'
FROM calendar_event_projections
	WHERE tenant_id = $1::uuid
	  AND event_id = $2
	  AND slice_key = 'vaccination'
	  AND system = false
	  AND event_type <> 'vaccination_dose_due'
	  AND due_at <= now() + interval '1 hour'
	  AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due')
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
	      AND nr.idempotency_key = $1 || ':calendar.reminder:' || calendar_event_projections.event_id || ':' || to_char((now() AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD')
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
	obligationID := strings.TrimSpace(in.ObligationID)
	if obligationID != "" {
		return r.selectEscalationEventForObligation(ctx, in, obligationID, level1Cutoff, level2Cutoff, level3Cutoff, level4Cutoff)
	}
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
    AND event_type <> 'vaccination_dose_due'
    AND due_at IS NOT NULL
    AND due_at <= $2::timestamptz
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

func (r *Repository) selectEscalationEventForObligation(ctx context.Context, in ports.SweepEscalations, obligationID string, level1Cutoff, level2Cutoff, level3Cutoff, level4Cutoff time.Time) ([]escalationEvent, error) {
	var event escalationEvent
	err := r.pool.QueryRow(ctx, `
WITH candidate AS (
  SELECT
    event_id,
    CASE
      WHEN due_at <= $6::timestamptz THEN 4
      WHEN due_at <= $5::timestamptz THEN 3
      WHEN due_at <= $4::timestamptz THEN 2
      WHEN due_at <= $3::timestamptz THEN 1
      ELSE 0
    END AS level
  FROM calendar_event_projections
  WHERE tenant_id = $1::uuid
    AND (
      event_id = 'obligation:' || $7::text
      OR (source_target_type = 'obligation' AND source_target_id = $7::uuid)
    )
    AND slice_key = 'vaccination'
    AND system = false
    AND due_at IS NOT NULL
    AND due_at <= $2::timestamptz
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
  )`, in.TenantID, in.Now, level1Cutoff, level2Cutoff, level3Cutoff, level4Cutoff, obligationID).Scan(&event.EventID, &event.Level)
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
	  AND status IN ('scheduled', 'due', 'overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due')
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

func (r *Repository) RefreshVaccinationProjection(ctx context.Context, in ports.RefreshVaccinationProjection) (result int, retErr error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return 0, fmt.Errorf("calendar: begin projection refresh: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
		if !committed && retErr != nil {
			r.markProjectionFailed(in.TenantID, retErr)
		}
	}()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 86171))`, in.TenantID); err != nil {
		return 0, fmt.Errorf("calendar: lock projection refresh: %w", err)
	}
	var projectionVersion int64
	var projectedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT (extract(epoch FROM clock_timestamp()) * 1000000)::bigint, clock_timestamp()`).Scan(&projectionVersion, &projectedAt); err != nil {
		return 0, fmt.Errorf("calendar: projection stamp: %w", err)
	}
	// DRV-004: materialize the drive_summary aggregate EXACTLY ONCE for this refresh call, before the
	// keyset page loop below runs it (potentially many times) via calendarVaccinationProjectionRefreshSQL.
	// The DROP is defensive (ON COMMIT DROP already clears the previous transaction's copy; transactional
	// DDL means a rolled-back refresh leaves no residue either) -- cheap and makes reuse of a pooled
	// connection across refresh calls safe regardless of how the prior transaction ended.
	if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS calendar_drive_summary_tmp`); err != nil {
		return 0, fmt.Errorf("calendar: reset drive summary materialization: %w", err)
	}
	if _, err := tx.Exec(ctx, calendarVaccinationDriveSummaryMaterializeSQL, in.TenantID, in.DateFrom, in.DateTo); err != nil {
		return 0, fmt.Errorf("calendar: materialize drive summary: %w", err)
	}
	if _, err := tx.Exec(ctx, `CREATE INDEX ON calendar_drive_summary_tmp (park_id, due_date)`); err != nil {
		return 0, fmt.Errorf("calendar: index drive summary: %w", err)
	}
	total := 0
	var cursor *domain.CalendarCursor
	for {
		page, err := r.refreshVaccinationProjectionPage(ctx, tx, in, cursor)
		if err != nil {
			return total, err
		}
		total += page.upserted
		if page.nextCursor == nil {
			break
		}
		cursor = page.nextCursor
	}
	tombstoned, err := r.tombstoneVaccinationProjection(ctx, tx, in)
	if err != nil {
		return total, err
	}
	total += tombstoned
	if _, err := tx.Exec(ctx, `
INSERT INTO calendar_projection_state (
  tenant_id,slice_key,projection_version,projected_at,date_from,date_to,
  freshness_status,serving_state,last_error,updated_at
) VALUES (
  $1::uuid,'vaccination',$2::bigint,$3::timestamptz,$4::timestamptz,$5::timestamptz,
  'green','fresh',NULL,now()
)
ON CONFLICT (tenant_id,slice_key) DO UPDATE SET
  projection_version=EXCLUDED.projection_version,
  projected_at=EXCLUDED.projected_at,
  date_from=EXCLUDED.date_from,
  date_to=EXCLUDED.date_to,
  freshness_status='green',
  serving_state='fresh',
  last_error=NULL,
  updated_at=now()`,
		in.TenantID, projectionVersion, projectedAt, in.DateFrom, in.DateTo); err != nil {
		return total, fmt.Errorf("calendar: publish projection state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return total, fmt.Errorf("calendar: commit projection refresh: %w", err)
	}
	committed = true
	return total, nil
}

func (r *Repository) markProjectionRebuilding(ctx context.Context, in ports.RefreshVaccinationProjection, version int64, projectedAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO calendar_projection_state (
  tenant_id, slice_key, projection_version, projected_at, date_from, date_to,
  freshness_status, serving_state, last_error, updated_at
) VALUES ($1::uuid, 'vaccination', $2::bigint, $3::timestamptz, $4::timestamptz, $5::timestamptz,
  'unknown', 'rebuilding', NULL, now())
ON CONFLICT (tenant_id, slice_key) DO UPDATE SET
  -- Existing rows continue serving while the replacement is built in one
  -- transaction. Do not turn every scheduled refresh into a read outage.
  last_error = NULL, updated_at = now()`,
		in.TenantID, version, projectedAt, in.DateFrom, in.DateTo)
	if err != nil {
		return fmt.Errorf("calendar: mark projection rebuilding: %w", err)
	}
	return nil
}

func (r *Repository) markProjectionFailed(tenantID string, cause error) {
	if cause == nil {
		return
	}
	stateCtx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	message := cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, _ = r.pool.Exec(stateCtx, `
UPDATE calendar_projection_state
SET freshness_status = CASE WHEN projection_version <= 0 THEN 'red' ELSE freshness_status END,
    serving_state = CASE WHEN projection_version <= 0 THEN 'failed' ELSE serving_state END,
    last_error = $2::text, updated_at = now()
WHERE tenant_id = $1::uuid AND slice_key = 'vaccination'`, tenantID, message)
}

type refreshProjectionPage struct {
	upserted   int
	nextCursor *domain.CalendarCursor
}

func (r *Repository) refreshVaccinationProjectionPage(ctx context.Context, tx pgx.Tx, in ports.RefreshVaccinationProjection, cursor *domain.CalendarCursor) (refreshProjectionPage, error) {
	var cursorDue any
	cursorEventID := ""
	if cursor != nil {
		cursorDue = cursor.DueAt
		cursorEventID = cursor.EventID
	}
	var count int
	var lastDue pgtype.Timestamptz
	var lastEventID string
	if err := tx.QueryRow(ctx, calendarVaccinationProjectionRefreshSQL,
		in.TenantID, in.DateFrom, in.DateTo, in.Limit, cursorDue, cursorEventID).Scan(&count, &lastDue, &lastEventID); err != nil {
		return refreshProjectionPage{}, fmt.Errorf("calendar: refresh vaccination projection: %w", err)
	}
	page := refreshProjectionPage{upserted: count}
	if count == in.Limit && lastDue.Valid && lastEventID != "" {
		page.nextCursor = &domain.CalendarCursor{DueAt: lastDue.Time, EventID: lastEventID}
	}
	return page, nil
}

func (r *Repository) tombstoneVaccinationProjection(ctx context.Context, tx pgx.Tx, in ports.RefreshVaccinationProjection) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, calendarVaccinationProjectionTombstoneSQL,
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

func (r *Repository) eventExists(ctx context.Context, tenantID, eventID string, scope domain.ScopeFilter) error {
	var exists bool
	tenantWide, parkIDs, shedIDs := scopeArgs(scope)
	// The history branch now checks the calendar_history_projection_rows projector output directly by
	// event_id + serving version, replacing the canonical obligation_instances/vaccination_completions
	// join that used to run on every request.
	if err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM calendar_event_projections
  WHERE tenant_id = $1::uuid AND event_id = $2 AND slice_key = 'vaccination'
    AND system = false
    AND ($3::bool OR park_id::text = ANY($4::text[]) OR shed_id::text = ANY($5::text[]))
  UNION ALL
  SELECT 1
  FROM calendar_history_projection_rows r
  JOIN calendar_history_projection_state s
    ON s.tenant_id = r.tenant_id
   AND s.serving_projection_version = r.projection_version
  WHERE r.tenant_id = $1::uuid AND r.event_id = $2
    AND ($3::bool OR r.park_id::text = ANY($4::text[]) OR r.shed_id::text = ANY($5::text[]))
)`, tenantID, eventID, tenantWide, parkIDs, shedIDs).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ports.ErrNotFound
	}
	return nil
}

func completedHistoryEventKey(eventID string) (domain.ParsedHistoryEvent, bool) {
	parsed, err := domain.ParseHistoryEventID(strings.TrimSpace(eventID))
	if err != nil {
		return domain.ParsedHistoryEvent{}, false
	}
	if !uuidutil.IsUUIDString(parsed.RuleID) {
		return domain.ParsedHistoryEvent{}, false
	}
	return parsed, true
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

const calendarListSQL = `
WITH history_serving AS (
  SELECT serving_projection_version
  FROM calendar_history_projection_state
  WHERE tenant_id = $1::uuid
    AND serving_projection_version IS NOT NULL
),
completed_history AS (
  -- Bounded, indexed read of the calendar_history_projection_rows projector output (migration
  -- 000178 + history_projection.go). This used to be a live join over
  -- vaccination_completions/obligation_instances/protocol_* on every request; that canonical replay
  -- is now the projector's job (RecomputeVaccinationHistoryProjection), off the request path.
  -- Missing/never-synced projection state (history_serving empty) yields zero rows here, not an
  -- error -- history bootstraps empty rather than 500ing.
  SELECT
    r.event_id,
    'vaccination_history'::text AS event_type,
    'pc'::text AS owner_key,
    r.title,
    r.subtitle,
    'completed'::text AS status,
    'info'::text AS severity,
    r.window_start AS due_at,
    r.window_start,
    r.window_end,
    'Asia/Kolkata'::text AS timezone,
    'india_only'::text AS timezone_source,
    r.park_id::text AS park_id,
    r.park_code,
    r.shed_id::text AS shed_id,
    r.shed_name,
    NULL::text AS cohort_id,
    NULL::text AS cohort_name,
    'goat'::text AS target_type,
    r.target_count,
    r.protocol_id::text AS protocol_id,
    r.protocol_version_id::text AS protocol_version_id,
    r.rule_id::text AS rule_id,
    r.vaccine_name,
    r.dose_code,
    true AS source_backed,
    'Accepted vaccination administration history'::text AS source_label,
    'Completed history'::text AS assignee_label,
    'pc_vaccinator'::text AS executor_role,
    'Accepted at source cutover'::text AS verifier_label,
    'not_scheduled'::text AS reminder_state,
    ''::text AS primary_notification_channel,
    'none'::text AS escalation_state,
    false AS system,
    false AS cross_cutting,
    jsonb_build_object('vaccination', '/vaccination') AS links
  FROM calendar_history_projection_rows r
  JOIN history_serving hs ON hs.serving_projection_version = r.projection_version
  WHERE r.tenant_id = $1::uuid
    AND r.business_date >= ($6::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
    AND r.business_date < ($7::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
),
candidates AS (
  (
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
    FROM calendar_event_projections
    WHERE tenant_id = $1::uuid
      AND slice_key = 'vaccination'
      AND system = false
      AND due_at >= $6::timestamptz AND due_at < $7::timestamptz
      AND event_type <> 'vaccination_dose_due'
      AND ($2::text = '' OR owner_key = $2::text)
      AND ($3::text = '' OR status = $3::text)
      AND ($3::text <> '' OR status NOT IN ('completed', 'canceled'))
      AND ($4::text = '' OR park_id = nullif($4::text, '')::uuid)
      AND ($5::text = '' OR shed_id = nullif($5::text, '')::uuid)
      AND ($8::timestamptz IS NULL OR (due_at, event_id) > ($8::timestamptz, $9::text))
      AND ($11::bool OR park_id::text = ANY($12::text[]) OR shed_id::text = ANY($13::text[]))
    ORDER BY due_at ASC, event_id ASC
    LIMIT $10
  )

  UNION

  (
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
    FROM calendar_event_projections
    WHERE tenant_id = $1::uuid
      AND slice_key = 'vaccination'
      AND system = false
      AND due_at IS NOT NULL
      AND event_type <> 'vaccination_dose_due'
      AND status IN ('overdue', 'missed', 'in_progress', 'proof_pending', 'verification_pending', 'rejected', 'rework_due', 'deferred', 'blocked')
      AND ($2::text = '' OR owner_key = $2::text)
      AND ($3::text = '' OR status = $3::text)
      AND ($4::text = '' OR park_id = nullif($4::text, '')::uuid)
      AND ($5::text = '' OR shed_id = nullif($5::text, '')::uuid)
      AND ($8::timestamptz IS NULL OR (due_at, event_id) > ($8::timestamptz, $9::text))
      AND ($11::bool OR park_id::text = ANY($12::text[]) OR shed_id::text = ANY($13::text[]))
    ORDER BY due_at ASC, event_id ASC
    LIMIT $10
  )

  UNION

  (
    SELECT event_id, event_type, owner_key, title, subtitle, status, severity, due_at, window_start,
           window_end, timezone, timezone_source, park_id, park_code, shed_id, shed_name,
           cohort_id, cohort_name, target_type, target_count, protocol_id,
           protocol_version_id, rule_id, vaccine_name, dose_code, source_backed,
           source_label, assignee_label, executor_role, verifier_label, reminder_state,
           primary_notification_channel, escalation_state, system, cross_cutting, links,
           false AS aggregated, false AS all_day,
           ''::text AS summary_primary, ''::text AS summary_secondary, ''::text AS summary_tertiary,
           CASE WHEN shed_id IS NULL THEN 0 ELSE 1 END AS shed_count,
           CASE WHEN vaccine_name IS NULL THEN 0 ELSE 1 END AS vaccine_count,
           0::int AS drive_count, 0::int AS catch_up_count, 0::int AS scheduled_count,
           0::int AS deferred_count, 0::int AS review_count,
           CASE WHEN shed_name IS NULL THEN ARRAY[]::text[] ELSE ARRAY[shed_name] END AS shed_labels,
           CASE WHEN vaccine_name IS NULL THEN ARRAY[]::text[] ELSE ARRAY[vaccine_name] END AS vaccine_labels,
           NULL::jsonb AS drive_summary
    FROM completed_history
    WHERE ($2::text = '' OR owner_key = $2::text)
      AND $3::text = 'completed'
      AND ($4::text = '' OR park_id = $4::text)
      AND ($5::text = '' OR shed_id = $5::text)
      AND ($8::timestamptz IS NULL OR (due_at, event_id) > ($8::timestamptz, $9::text))
      AND ($11::bool OR park_id = ANY($12::text[]) OR shed_id = ANY($13::text[]))
    ORDER BY due_at ASC, event_id ASC
    LIMIT $10
  )
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
FROM candidates
ORDER BY due_at ASC, event_id ASC
LIMIT $10`

// projection-review: membership=calendar_event_projections rows in the requested [$6,$7) window matching the same owner/status/park/shed/scope filters as the list query, UNION ALL with the materialized calendar_history_date_markers projector output gated by calendar_history_projection_state.serving_projection_version; group_key=marker_date (Asia/Kolkata calendar day), the live branch from due_at and the history branch from the already-IST-derived business_date, both collapsed by the outer GROUP BY marker_date so a day never double-counts; join_cardinality=plain per-row GROUP BY over calendar_event_projections with no dimension join (no fan-out), the history branch's JOIN to history_serving is a semijoin against calendar_history_projection_state's tenant_id PRIMARY KEY (at most one row); pagination=whole-result, one query per ListEvents(IncludeDateMarkers) call bounded to the requested date range, never recomputed per event row or UI list page; scope=park_id/shed_id read directly from calendar_event_projections' precomputed columns (already resolved at refresh time), no ad hoc parent/self coalescing
const calendarDateMarkersSQL = `
WITH history_serving AS (
  SELECT serving_projection_version
  FROM calendar_history_projection_state
  WHERE tenant_id = $1::uuid
    AND serving_projection_version IS NOT NULL
),
marker_rows AS (
  SELECT
    ((due_at AT TIME ZONE 'Asia/Kolkata')::date)::text AS marker_date,
    count(*)::bigint AS event_count,
    count(*) FILTER (WHERE status = 'completed')::bigint AS completed_count,
    count(*) FILTER (WHERE status NOT IN ('completed', 'canceled'))::bigint AS open_count,
    count(*) FILTER (WHERE event_type = 'vaccination_drive')::bigint AS drive_count
  FROM calendar_event_projections
  WHERE tenant_id = $1::uuid
    AND slice_key = 'vaccination'
    AND system = false
    AND event_type <> 'vaccination_dose_due'
    AND ($2::text = '' OR owner_key = $2::text)
    AND ($3::text = '' OR status = $3::text)
    AND ($3::text <> '' OR status NOT IN ('completed', 'canceled'))
    AND ($4::text = '' OR park_id = nullif($4::text, '')::uuid)
    AND ($5::text = '' OR shed_id = nullif($5::text, '')::uuid)
    AND due_at >= $6::timestamptz
    AND due_at < $7::timestamptz
    AND ($8::bool OR park_id::text = ANY($9::text[]) OR shed_id::text = ANY($10::text[]))
  GROUP BY (due_at AT TIME ZONE 'Asia/Kolkata')::date

  UNION ALL

  -- Bounded, indexed read of the calendar_history_date_markers projector output (migration 000178 +
  -- history_projection.go), replacing the vaccination_completions aggregate that used to run on every
  -- request. Missing/never-synced projection state yields zero marker rows here, not an error.
  SELECT
    (m.business_date)::text AS marker_date,
    SUM(m.completion_count)::bigint AS event_count,
    SUM(m.completion_count)::bigint AS completed_count,
    0::bigint AS open_count,
    0::bigint AS drive_count
  FROM calendar_history_date_markers m
  JOIN history_serving hs ON hs.serving_projection_version = m.projection_version
  WHERE m.tenant_id = $1::uuid
    AND ($2::text = '' OR $2::text = 'pc')
    AND ($3::text = '' OR $3::text = 'completed')
    AND ($4::text = '' OR m.park_id = nullif($4::text, '')::uuid)
    AND ($5::text = '' OR m.shed_id = nullif($5::text, '')::uuid)
    AND m.business_date >= ($6::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
    AND m.business_date < ($7::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
    AND ($8::bool OR m.park_id::text = ANY($9::text[]) OR m.shed_id::text = ANY($10::text[]))
  GROUP BY m.business_date
)
SELECT marker_date,
       sum(event_count)::int,
       sum(completed_count)::int,
       sum(open_count)::int,
       sum(drive_count)::int
FROM marker_rows
GROUP BY marker_date
ORDER BY marker_date`

// projection-review: membership=calendar_event_projections rows in the requested [$6,$7) window matching the same owner/status/park/shed/scope filters as the list/date-marker queries (system=false, slice_key='vaccination', event_type <> 'vaccination_dose_due'), LEFT JOINed to at most one row each from calendar_snoozes (active_snooze), notification_requests (latest_reminder/latest_escalation, one row per calendar_event_id via DISTINCT ON), and obligation_escalations (obligation_escalation_active, one row per obligation_id via DISTINCT ON) -- every join side is itself grouped to at most one row per join key before the join, so none of these LEFT JOINs can fan out a candidate row; group_key=event_id (one row per already-materialized projection row plus its at-most-one derived state row, no grouping/aggregation beyond the window count(*) OVER() total); join_cardinality=candidates 1:{0,1} against each derived CTE (all four are pre-deduplicated to their join key), so no dimension fan-out to dedupe; pagination=whole-result total (count(*) OVER() over the FULL filtered week window, independent of the ~20-row LIMIT below) AND bounded-execution (one indexed query per ListEvents(IncludeReminderRail) call: the notification_requests/calendar_snoozes/obligation_escalations CTEs are each restricted via JOIN candidates to the same bounded date-window event set, using notification_requests_event_idx/calendar_snoozes_event_idx/(tenant_id, obligation_id) rather than a tenant-wide scan; LIMIT $11 keeps the returned preview small, never recomputed by filtering whatever page of Items the frontend currently holds); scope=park_id/shed_id read directly from calendar_event_projections' precomputed columns (already resolved at refresh time), same tenant-wide/park/shed scope predicate as calendarListSQL/calendarDateMarkersSQL
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
const calendarReminderRailSQL = `
WITH candidates AS (
  SELECT event_id, title, subtitle, source_target_type, source_target_id, primary_notification_channel, due_at
  FROM calendar_event_projections
  WHERE tenant_id = $1::uuid
    AND slice_key = 'vaccination'
    AND system = false
    AND event_type <> 'vaccination_dose_due'
    AND ($2::text = '' OR owner_key = $2::text)
    AND ($3::text = '' OR status = $3::text)
    AND ($4::text = '' OR park_id = nullif($4::text, '')::uuid)
    AND ($5::text = '' OR shed_id = nullif($5::text, '')::uuid)
    AND due_at >= $6::timestamptz
    AND due_at < $7::timestamptz
    AND ($8::bool OR park_id::text = ANY($9::text[]) OR shed_id::text = ANY($10::text[]))
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

const calendarDetailSQL = `
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
FROM calendar_event_projections
WHERE tenant_id = $1::uuid AND event_id = $2 AND slice_key = 'vaccination'
  AND system = false
  AND ($3::bool OR park_id::text = ANY($4::text[]) OR shed_id::text = ANY($5::text[]))`

const calendarHistoryProjectionDetailSQL = `
WITH history_serving AS (
  SELECT serving_projection_version
  FROM calendar_history_projection_state
  WHERE tenant_id = $1::uuid
    AND serving_projection_version IS NOT NULL
)
SELECT
  r.event_id, 'vaccination_history'::text AS event_type, 'pc'::text AS owner_key,
  r.title, r.subtitle, 'completed'::text AS status, 'info'::text AS severity,
  r.window_start AS due_at, r.window_start, r.window_end,
  'Asia/Kolkata'::text AS timezone, 'india_only'::text AS timezone_source,
  r.park_id::text AS park_id, r.park_code, r.shed_id::text AS shed_id, r.shed_name,
  NULL::text AS cohort_id, NULL::text AS cohort_name,
  'goat'::text AS target_type, r.target_count,
  r.protocol_id::text AS protocol_id, r.protocol_version_id::text AS protocol_version_id, r.rule_id::text AS rule_id,
  r.vaccine_name, r.dose_code,
  true AS source_backed,
  'Accepted vaccination administration history'::text AS source_label,
  'Completed history'::text AS assignee_label,
  'pc_vaccinator'::text AS executor_role,
  'Accepted at source cutover'::text AS verifier_label,
  'not_scheduled'::text AS reminder_state,
  ''::text AS primary_notification_channel,
  'none'::text AS escalation_state,
  false AS system, false AS cross_cutting,
  jsonb_build_object('vaccination', '/vaccination') AS links,
  false AS aggregated, false AS all_day,
  ''::text AS summary_primary, ''::text AS summary_secondary, ''::text AS summary_tertiary,
  CASE WHEN r.shed_id IS NULL THEN 0 ELSE 1 END AS shed_count,
  CASE WHEN r.vaccine_name IS NULL THEN 0 ELSE 1 END AS vaccine_count,
  0::int AS drive_count, 0::int AS catch_up_count, 0::int AS scheduled_count,
  0::int AS deferred_count, 0::int AS review_count,
  CASE WHEN r.shed_name IS NULL THEN ARRAY[]::text[] ELSE ARRAY[r.shed_name] END AS shed_labels,
  CASE WHEN r.vaccine_name IS NULL THEN ARRAY[]::text[] ELSE ARRAY[r.vaccine_name] END AS vaccine_labels,
  NULL::jsonb AS drive_summary,
  r.detail
FROM calendar_history_projection_rows r
JOIN history_serving hs ON hs.serving_projection_version = r.projection_version
WHERE r.tenant_id = $1::uuid AND r.event_id = $2
  AND ($3::bool OR r.park_id::text = ANY($4::text[]) OR r.shed_id::text = ANY($5::text[]))`

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

// DRV-004 bounded execution: this materializes the drive_summary aggregate (obligation_drive_membership
// -> obligation_drive_vaccine_labels -> obligation_drive_shed_complete -> obligation_drive_summary) EXACTLY
// ONCE per RefreshVaccinationProjection call, into the transaction-scoped temp table
// calendar_drive_summary_tmp (ON COMMIT DROP -- transactional DDL means a rolled-back refresh leaves no
// residue either). RefreshVaccinationProjection executes this before entering its keyset page loop; every
// call to refreshVaccinationProjectionPage then reads the small, indexed temp table instead of re-scanning
// obligation_instances/obligation_batches/protocol_* for every page. Same tenant/date-window/status/scope
// filters as the CTE chain this replaces -- copied byte-for-byte from the original inline CTEs so the
// aggregation semantics (membership source, group key, join cardinality, scope matrix) are unchanged; see
// the projection-review marker below calendarVaccinationProjectionRefreshSQL's obligation_drive_summary stub.
const calendarVaccinationDriveSummaryMaterializeSQL = `
CREATE TEMP TABLE calendar_drive_summary_tmp ON COMMIT DROP AS
-- projection-review: membership=oi.batch_id (batched -> obligation_batches window/scope/context, unbatched -> obligation's own due_at/scope); group_key=(park_id, due_date) via member.membership_at in Asia/Kolkata; join_cardinality=one row per obligation_id; pagination=materialized EXACTLY ONCE per RefreshVaccinationProjection call before the keyset page loop; scope=explicit park/shed/cohort location matrix (scope_loc/scope_parent/scope_grand), batch scope for batched obligations and obligation scope for unbatched
WITH obligation_drive_membership AS (
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
)
SELECT * FROM obligation_drive_summary`

const calendarVaccinationProjectionRefreshSQL = `
WITH obligation_events AS (
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
-- drive_summary aggregation. Emits EXACTLY ONE row per (park_id, due_date) so the LEFT JOIN
-- into park_drive_events below is 1:1 and never fans a single park-drive event_id into multiple
-- rows (which would break the ON CONFLICT (tenant_id, event_id) upsert in the refresh).
--
-- DRV-004 bounded execution: obligation_drive_membership -> obligation_drive_vaccine_labels ->
-- obligation_drive_shed_complete -> obligation_drive_summary is NOT computed inline here anymore.
-- RefreshVaccinationProjection materializes it EXACTLY ONCE per refresh call, before the keyset
-- page loop begins, via calendarVaccinationDriveSummaryMaterializeSQL into the transaction-scoped
-- temp table calendar_drive_summary_tmp (ON COMMIT DROP), indexed on (park_id, due_date). Every
-- keyset page of THIS query then does a single indexed lookup into that temp table instead of
-- re-scanning obligation_instances/obligation_batches/protocol_* per page. Membership, group key,
-- join cardinality, and scope matrix are byte-identical to the CTE chain this replaces (see
-- calendarVaccinationDriveSummaryMaterializeSQL above) -- only WHERE this work executes changed,
-- never the aggregation semantics. Proof: TestRefreshVaccinationProjectionMaterializesDriveSummaryOnce
-- (query-count + EXPLAIN evidence) in repository_integration_test.go.
--
-- projection-review: membership=oi.batch_id (batched -> obligation_batches window/scope, unbatched -> obligation's own due_at/scope, mirroring catchup_drive_events); group_key=(park_id, due_date) derived from that same membership source, matching park_drive_groups' (park_id, due_day) key in Asia/Kolkata; join_cardinality=count(DISTINCT obligation_id) FILTER per bucket over a one-row-per-obligation membership CTE with mutually-exclusive precedence completed > deferred > overdue > due so total_count = completed+due+overdue+deferred with no double-counting, dimension fan-out isolated to a separate DISTINCT array_agg for vaccine_labels, one summary row per (park_id, due_date) LEFT JOINed 1:1 into park_drive_events; pagination=whole-result AND bounded-execution -- obligation_drive_summary is materialized EXACTLY ONCE per RefreshVaccinationProjection call (calendarVaccinationDriveSummaryMaterializeSQL, executed before the keyset page loop) into the indexed temp table calendar_drive_summary_tmp(park_id, due_date), never re-derived per UI list page (reads are indexed lookups over the materialized calendar_event_projections.detail) nor re-scanned per keyset refresh page (each page reads the small indexed temp table, not obligation_instances/obligation_batches again); scope=explicit park/shed/cohort location matrix (scope_loc/scope_parent/scope_grand), batch scope for batched obligations and obligation scope for unbatched, never COALESCE(parent_id, self_id)
-- Stock/execution "blocked" visibility is deliberately out of scope -- stock is not a built product
-- feature yet (owner decision 2026-07-14); revisit when the stock module ships.
obligation_drive_summary AS (
  SELECT * FROM calendar_drive_summary_tmp
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
    due_at, window_start,
    CASE
      WHEN window_start IS NOT NULL AND window_end IS NOT NULL AND window_end < window_start THEN window_start
      ELSE window_end
    END AS window_end,
    timezone, timezone_source, park_id, park_code, shed_id, shed_name,
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

  UNION ALL

  SELECT 'batch:' || ob.batch_id::text AS event_id
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
    AND ob.status NOT IN ('superseded', 'canceled')
    AND (ob.status <> 'completed' OR COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) >= now() - interval '90 days')

  UNION ALL

  SELECT
    CASE
      WHEN loc.park_id IS NOT NULL THEN
        'catchup:park:' || loc.park_id::text || ':due:' ||
        to_char((oi.due_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD')
      ELSE
        'catchup:tenant:' || oi.tenant_id::text || ':due:' ||
        to_char((oi.due_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD')
    END AS event_id
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
        WHEN oi.scope_type = 'shed' THEN scope_loc.location_id
        WHEN oi.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.location_id
      END AS shed_id
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
  GROUP BY event_id

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
    AND st.state IN ('assigned', 'in_progress', 'submitted', 'needs_review', 'rework_requested', 'rejected')

  UNION ALL

  SELECT 'calendar:' || protocol_version_id::text AS event_id
  FROM (
    SELECT
      pv.protocol_version_id,
      NULLIF(pv.rule_dsl ->> 'activation_due_at', '') AS raw_due_at
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
    AND cep.source_target_type IN ('obligation', 'batch', 'catchup', 'park_drive', 'sop_task', 'protocol_version')
    AND cep.status NOT IN ('completed', 'canceled')
    AND (
      (
        cep.source_target_type IN ('batch', 'catchup')
        AND EXISTS (
          SELECT 1
          FROM calendar_event_projections grouped_drive
          CROSS JOIN LATERAL jsonb_array_elements_text(
            COALESCE(grouped_drive.detail->'source_and_rule'->'source_event_ids', '[]'::jsonb)
          ) AS child(event_id)
          WHERE grouped_drive.tenant_id = cep.tenant_id
            AND grouped_drive.slice_key = 'vaccination'
            AND grouped_drive.source_target_type = 'park_drive'
            AND grouped_drive.status NOT IN ('completed', 'canceled')
            AND child.event_id = cep.event_id
        )
      )
      OR (
        cep.source_target_type = 'park_drive'
        AND NOT EXISTS (
          SELECT 1
          FROM jsonb_array_elements_text(
            COALESCE(cep.detail->'source_and_rule'->'source_event_ids', '[]'::jsonb)
          ) AS child(event_id)
          JOIN source_event_ids source ON source.event_id = child.event_id
        )
      )
      OR (
        cep.source_target_type NOT IN ('park_drive')
        AND NOT EXISTS (
          SELECT 1 FROM source_event_ids source
          WHERE source.event_id = cep.event_id
        )
      )
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
