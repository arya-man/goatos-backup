package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	defaultListLimit    = 100
	maxListLimit        = 200
	defaultHistoryLimit = 50
	maxHistoryLimit     = 100
	maxDateRange        = 45 * 24 * time.Hour
	defaultDateRange    = 30 * 24 * time.Hour
	defaultSLA1After    = 0
	defaultSLA2After    = 4 * time.Hour
	defaultSLA3After    = 24 * time.Hour
	defaultSLA4After    = 48 * time.Hour
	minIdempotencyLen   = 8
	maxNudgeMessageLen  = 2000
	maxActionReasonLen  = 1000
)

type Service struct {
	repo ports.Repository
	now  func() time.Time
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().In(biztime.DefaultLocation()) }}
}

func (s *Service) ListEvents(ctx context.Context, q domain.Query) (domain.CalendarEventListResponse, error) {
	q.TenantID = strings.TrimSpace(q.TenantID)
	if !uuidutil.IsUUIDString(q.TenantID) {
		return domain.CalendarEventListResponse{}, BadRequest("invalid_tenant", "tenant id is required")
	}
	if q.ParkID != nil && !uuidutil.IsUUIDString(*q.ParkID) {
		return domain.CalendarEventListResponse{}, BadRequest("invalid_park_id", "park_id must be a UUID")
	}
	if q.ShedID != nil && !uuidutil.IsUUIDString(*q.ShedID) {
		return domain.CalendarEventListResponse{}, BadRequest("invalid_shed_id", "shed_id must be a UUID")
	}
	q.OwnerKey = normalizeOwnerKey(q.OwnerKey)
	if !allowedOwnerKey(q.OwnerKey) {
		return domain.CalendarEventListResponse{}, BadRequest("invalid_owner_key", "owner_key must be all, pc, inventory, or admin_data_ops")
	}
	if q.Status != nil {
		status := strings.TrimSpace(*q.Status)
		if !allowedStatus(status) {
			return domain.CalendarEventListResponse{}, BadRequest("invalid_status", "status must be a Calendar status")
		}
		q.Status = &status
	}
	if q.DateFrom.IsZero() {
		loc := mustCalendarLocation()
		today := s.now().In(loc)
		q.DateFrom = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	}
	if q.DateTo.IsZero() {
		q.DateTo = q.DateFrom.Add(defaultDateRange)
	}
	if q.DateTo.Before(q.DateFrom) {
		return domain.CalendarEventListResponse{}, BadRequest("invalid_date_range", "date_to must be on or after date_from")
	}
	if q.DateTo.Sub(q.DateFrom) > maxDateRange {
		return domain.CalendarEventListResponse{}, BadRequest("invalid_date_range", "date range may not exceed 45 days")
	}
	if q.Limit <= 0 {
		q.Limit = defaultListLimit
	}
	if q.Limit > maxListLimit {
		q.Limit = maxListLimit
	}
	resp, err := s.repo.ListEvents(ctx, q)
	if err != nil {
		return domain.CalendarEventListResponse{}, mapRepoError(err)
	}
	resp.Presentation = domain.CalendarPresentationForQuery(q.OwnerKey)
	// DRV-005: reminder_rail.empty_message reuses the same backend-owned copy the week view already
	// renders (CalendarPresentation.Week.ReminderEmptyMessage) rather than a second hardcoded literal
	// in the repository layer.
	if resp.ReminderRail != nil {
		resp.ReminderRail.EmptyMessage = resp.Presentation.Week.ReminderEmptyMessage
	}
	return resp, nil
}

func (s *Service) GetEventDetail(ctx context.Context, q domain.EventQuery) (domain.CalendarEventDetail, error) {
	if !uuidutil.IsUUIDString(q.TenantID) {
		return domain.CalendarEventDetail{}, BadRequest("invalid_tenant", "tenant id is required")
	}
	if err := domain.ValidateEventID(q.EventID); err != nil {
		return domain.CalendarEventDetail{}, BadRequest("invalid_event_id", "event_id is invalid")
	}
	detail, err := s.repo.GetEventDetail(ctx, q)
	if err != nil {
		return domain.CalendarEventDetail{}, mapRepoError(err)
	}
	return detail, nil
}

func (s *Service) ListDriveTargets(ctx context.Context, q domain.DriveTargetQuery) (domain.CalendarDriveTargetListResponse, error) {
	if !uuidutil.IsUUIDString(q.TenantID) {
		return domain.CalendarDriveTargetListResponse{}, BadRequest("invalid_tenant", "tenant id is required")
	}
	if err := domain.ValidateEventID(q.EventID); err != nil {
		return domain.CalendarDriveTargetListResponse{}, BadRequest("invalid_event_id", "event_id is invalid")
	}
	if _, err := domain.ParseDriveEventID(q.EventID); err != nil {
		return domain.CalendarDriveTargetListResponse{}, BadRequest("invalid_event_id", "event_id is not a vaccination drive")
	}
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Limit > 50 {
		q.Limit = 50
	}
	resp, err := s.repo.ListDriveTargets(ctx, q)
	if err != nil {
		return domain.CalendarDriveTargetListResponse{}, mapRepoError(err)
	}
	return resp, nil
}

func (s *Service) History(ctx context.Context, q domain.HistoryQuery) (domain.CalendarHistoryResponse, error) {
	if !uuidutil.IsUUIDString(q.TenantID) {
		return domain.CalendarHistoryResponse{}, BadRequest("invalid_tenant", "tenant id is required")
	}
	if err := domain.ValidateEventID(q.EventID); err != nil {
		return domain.CalendarHistoryResponse{}, BadRequest("invalid_event_id", "event_id is invalid")
	}
	if q.Limit <= 0 {
		q.Limit = defaultHistoryLimit
	}
	if q.Limit > maxHistoryLimit {
		q.Limit = maxHistoryLimit
	}
	resp, err := s.repo.History(ctx, q)
	if err != nil {
		return domain.CalendarHistoryResponse{}, mapRepoError(err)
	}
	return resp, nil
}

func (s *Service) SendNudge(ctx context.Context, in ports.SendNudge) (domain.CalendarActionResponse, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.EventID = strings.TrimSpace(in.EventID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.Channel = strings.TrimSpace(in.Channel)
	in.Message = strings.TrimSpace(in.Message)
	in.Reason = strings.TrimSpace(in.Reason)
	if err := validateActionEnvelope(in.TenantID, in.ActorID, in.EventID, in.IdempotencyKey); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if in.Channel != "" && !allowedNotificationChannel(in.Channel) {
		return domain.CalendarActionResponse{}, BadRequest("invalid_channel", "channel must be local-stub, push_fcm, slack, email, webhook, incident, opsgenie, or pagerduty")
	}
	if len(in.Message) > maxNudgeMessageLen {
		return domain.CalendarActionResponse{}, BadRequest("invalid_message", "message may not exceed 2000 characters")
	}
	if len(in.Reason) > maxActionReasonLen {
		return domain.CalendarActionResponse{}, BadRequest("invalid_reason", "reason may not exceed 1000 characters")
	}
	resp, err := s.repo.SendNudge(ctx, in)
	if err != nil {
		return domain.CalendarActionResponse{}, mapRepoError(err)
	}
	return resp, nil
}

func (s *Service) Snooze(ctx context.Context, in ports.Snooze) (domain.CalendarActionResponse, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.EventID = strings.TrimSpace(in.EventID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.Reason = strings.TrimSpace(in.Reason)
	if err := validateActionEnvelope(in.TenantID, in.ActorID, in.EventID, in.IdempotencyKey); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if in.SnoozeUntil.IsZero() || !in.SnoozeUntil.After(s.now()) {
		return domain.CalendarActionResponse{}, BadRequest("invalid_snooze_until", "snooze_until must be in the future")
	}
	if in.Reason == "" {
		return domain.CalendarActionResponse{}, BadRequest("invalid_reason", "reason is required")
	}
	if len(in.Reason) > maxActionReasonLen {
		return domain.CalendarActionResponse{}, BadRequest("invalid_reason", "reason may not exceed 1000 characters")
	}
	resp, err := s.repo.Snooze(ctx, in)
	if err != nil {
		return domain.CalendarActionResponse{}, mapRepoError(err)
	}
	return resp, nil
}

func (s *Service) AcknowledgeEscalation(ctx context.Context, in ports.AcknowledgeEscalation) (domain.CalendarActionResponse, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.EventID = strings.TrimSpace(in.EventID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.Reason = strings.TrimSpace(in.Reason)
	if err := validateActionEnvelope(in.TenantID, in.ActorID, in.EventID, in.IdempotencyKey); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if len(in.Reason) > maxActionReasonLen {
		return domain.CalendarActionResponse{}, BadRequest("invalid_reason", "reason may not exceed 1000 characters")
	}
	resp, err := s.repo.AcknowledgeEscalation(ctx, in)
	if err != nil {
		return domain.CalendarActionResponse{}, mapRepoError(err)
	}
	return resp, nil
}

func (s *Service) ResolveEscalation(ctx context.Context, in ports.ResolveEscalation) (domain.CalendarActionResponse, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.EventID = strings.TrimSpace(in.EventID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.Reason = strings.TrimSpace(in.Reason)
	if err := validateActionEnvelope(in.TenantID, in.ActorID, in.EventID, in.IdempotencyKey); err != nil {
		return domain.CalendarActionResponse{}, err
	}
	if in.Reason == "" {
		return domain.CalendarActionResponse{}, BadRequest("invalid_reason", "reason is required")
	}
	if len(in.Reason) > maxActionReasonLen {
		return domain.CalendarActionResponse{}, BadRequest("invalid_reason", "reason may not exceed 1000 characters")
	}
	resp, err := s.repo.ResolveEscalation(ctx, in)
	if err != nil {
		return domain.CalendarActionResponse{}, mapRepoError(err)
	}
	return resp, nil
}

func (s *Service) SweepDueReminders(ctx context.Context, tenantID string, limit int) (int, error) {
	if !uuidutil.IsUUIDString(tenantID) {
		return 0, BadRequest("invalid_tenant", "tenant id is required")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	n, err := s.repo.SweepDueReminders(ctx, tenantID, limit)
	if err != nil {
		return 0, mapRepoError(err)
	}
	return n, nil
}

func (s *Service) SweepEscalations(ctx context.Context, in ports.SweepEscalations) (int, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	if !uuidutil.IsUUIDString(in.TenantID) {
		return 0, BadRequest("invalid_tenant", "tenant id is required")
	}
	in.ObligationID = strings.TrimSpace(in.ObligationID)
	if in.ObligationID != "" && !uuidutil.IsUUIDString(in.ObligationID) {
		return 0, BadRequest("invalid_obligation_id", "obligation_id must be a UUID")
	}
	if in.Limit <= 0 {
		in.Limit = 100
	}
	if in.Limit > 500 {
		in.Limit = 500
	}
	if in.Now.IsZero() {
		in.Now = s.now()
	}
	in.Now = in.Now.UTC()
	if in.Level2After <= 0 {
		in.Level2After = defaultSLA2After
	}
	if in.Level3After <= 0 {
		in.Level3After = defaultSLA3After
	}
	if in.Level4After <= 0 {
		in.Level4After = defaultSLA4After
	}
	if in.Level1After < 0 || in.Level2After < in.Level1After || in.Level3After < in.Level2After || in.Level4After < in.Level3After {
		return 0, BadRequest("invalid_sla_thresholds", "SLA thresholds must be non-negative and increasing")
	}
	n, err := s.repo.SweepEscalations(ctx, in)
	if err != nil {
		return 0, mapRepoError(err)
	}
	return n, nil
}


// QueueRoleNotifications validates the envelope and passes an already-resolved recipient list
// straight through to the repository's set-based insert. No recipients is a legitimate no-op (e.g. a
// verification_pending event where no position currently holds the verify duty for that park) --
// never an error, so the caller (the verification notification producer) doesn't have to special-case
// it.
func (s *Service) QueueRoleNotifications(ctx context.Context, in ports.QueueRoleNotifications) (int, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	if !uuidutil.IsUUIDString(in.TenantID) {
		return 0, BadRequest("invalid_tenant", "tenant id is required")
	}
	in.CalendarEventID = strings.TrimSpace(in.CalendarEventID)
	if in.CalendarEventID == "" {
		return 0, BadRequest("invalid_calendar_event", "calendar_event_id is required")
	}
	in.NotificationType = strings.TrimSpace(in.NotificationType)
	if in.NotificationType == "" {
		return 0, BadRequest("invalid_notification_type", "notification_type is required")
	}
	in.Channel = strings.TrimSpace(in.Channel)
	if in.Channel == "" {
		in.Channel = "push_fcm"
	}
	in.EventKey = strings.TrimSpace(in.EventKey)
	if in.EventKey == "" {
		return 0, BadRequest("invalid_event_key", "event_key is required for idempotency")
	}
	if len(in.Recipients) == 0 {
		return 0, nil
	}
	n, err := s.repo.QueueRoleNotifications(ctx, in)
	if err != nil {
		return 0, mapRepoError(err)
	}
	return n, nil
}

// SweepReminderCadence validates and defaults the cadence sweep envelope and returns the fires due to
// be queued as of in.Now (see ports.ReminderCadenceQuery / vaccination-notification-rules.md §3). The
// caller (cmd/calendar-reminder-sweeper) resolves each fire's audience and calls
// QueueReminderCadenceBatch to actually write the notification rows.
func (s *Service) SweepReminderCadence(ctx context.Context, in ports.ReminderCadenceQuery) ([]ports.ReminderCadenceFire, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	if !uuidutil.IsUUIDString(in.TenantID) {
		return nil, BadRequest("invalid_tenant", "tenant id is required")
	}
	if in.Now.IsZero() {
		in.Now = s.now()
	}
	if in.Limit <= 0 {
		in.Limit = 200
	}
	if in.Limit > 2000 {
		in.Limit = 2000
	}
	fires, err := s.repo.SweepReminderCadence(ctx, in)
	if err != nil {
		return nil, mapRepoError(err)
	}
	return fires, nil
}

// QueueReminderCadenceBatch validates the envelope and passes already-collapsed, already-audienced
// cadence fires straight through to the repository's set-based batch insert. No fires is a legitimate
// no-op (e.g. a tick with nothing currently due, or everything deferred by quiet hours).
func (s *Service) QueueReminderCadenceBatch(ctx context.Context, in ports.QueueReminderCadenceBatch) (int, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	if !uuidutil.IsUUIDString(in.TenantID) {
		return 0, BadRequest("invalid_tenant", "tenant id is required")
	}
	in.Channel = strings.TrimSpace(in.Channel)
	if in.Channel == "" {
		in.Channel = "push_fcm"
	}
	if len(in.Fires) == 0 {
		return 0, nil
	}
	n, err := s.repo.QueueReminderCadenceBatch(ctx, in)
	if err != nil {
		return 0, mapRepoError(err)
	}
	return n, nil
}

func validateActionEnvelope(tenantID, actorID, eventID, idempotencyKey string) error {
	if !uuidutil.IsUUIDString(tenantID) {
		return BadRequest("invalid_tenant", "tenant id is required")
	}
	if !uuidutil.IsUUIDString(actorID) {
		return BadRequest("invalid_actor", "actor id is required")
	}
	if err := domain.ValidateEventID(eventID); err != nil {
		return BadRequest("invalid_event_id", "event_id is invalid")
	}
	if len(idempotencyKey) < minIdempotencyLen || len(idempotencyKey) > 200 {
		return BadRequest("invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
	}
	return nil
}

func normalizeOwnerKey(ownerKey string) string {
	ownerKey = strings.TrimSpace(ownerKey)
	if ownerKey == "" {
		return domain.OwnerAll
	}
	return ownerKey
}

func allowedOwnerKey(ownerKey string) bool {
	switch ownerKey {
	case domain.OwnerAll, domain.OwnerPC, domain.OwnerInventory, domain.OwnerAdminDataOps:
		return true
	default:
		return false
	}
}

func allowedStatus(status string) bool {
	switch status {
	case domain.StatusScheduled, domain.StatusDue, domain.StatusOverdue, domain.StatusMissed, domain.StatusInProgress,
		domain.StatusProofPending, domain.StatusVerificationPending, domain.StatusRejected,
		domain.StatusReworkDue, domain.StatusDeferred, domain.StatusBlocked, domain.StatusCompleted,
		domain.StatusCanceled:
		return true
	default:
		return false
	}
}

func allowedNotificationChannel(channel string) bool {
	switch channel {
	case "local-stub", "push_fcm", "slack", "email", "webhook", "incident", "opsgenie", "pagerduty":
		return true
	default:
		return false
	}
}

func mapRepoError(err error) error {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return NotFound("calendar event was not found")
	case errors.Is(err, ports.ErrForbidden):
		return Forbidden("permission_denied", "actor is not allowed to action this escalation level")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "same Idempotency-Key was replayed with a different payload")
	case errors.Is(err, ports.ErrIdempotencyInProgress):
		return Conflict("idempotency_in_progress", "same Idempotency-Key is still being processed")
	case errors.Is(err, ports.ErrActiveSnoozeExists):
		return Conflict("active_snooze_exists", "an active snooze already exists; set replace_existing to create a replacement")
	case errors.Is(err, ports.ErrEventNotActionable):
		return Conflict("event_not_actionable", "calendar event is not actionable")
	case errors.Is(err, ports.ErrInvalidReference):
		return BadRequest("invalid_reference", "calendar event references invalid source state")
	case errors.Is(err, ports.ErrProjectionUnavailable):
		return Unavailable("projection_unavailable", "calendar projection is temporarily unavailable")
	case errors.Is(err, ports.ErrProjectionStale):
		return Unavailable("projection_stale", "calendar projection is stale; retry after refresh")
	default:
		return Internal("calendar request failed")
	}
}

// ResolveVaccinationCompletionContext delegates to the repository to resolve a vaccination completion_id
// to its obligation context. Used by the notification layer to decouple from importing internal/vaccination.
func (s *Service) ResolveVaccinationCompletionContext(ctx context.Context, tenantID, completionID string) (ports.VaccinationCompletionContext, error) {
	return s.repo.ResolveVaccinationCompletionContext(ctx, tenantID, completionID)
}

func mustCalendarLocation() *time.Location {
	loc, err := time.LoadLocation(domain.DefaultTimezone)
	if err != nil {
		return time.FixedZone("IST", 5*60*60+30*60)
	}
	return loc
}
