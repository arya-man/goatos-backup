package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// LeaveService is the leave-request workflow (docs/features/leave-requests/
// plan.md, maintainer decisions 2026-09-10): the operator raises from the
// phone Clock screen, the Park Head AND HR both approve, either rejection ends
// it, the operator may withdraw while pending. It owns every label both
// surfaces render -- the date window, the day count, the status line, the slot
// names -- so no client composes a sentence of its own.
type LeaveService struct {
	repo   ports.LeaveRepository
	member interface {
		GetMemberForActor(ctx context.Context, tenantID, actorID string) (domain.OperatorProfile, error)
	}
	now func() time.Time
}

func NewLeaveService(repo ports.LeaveRepository, member interface {
	GetMemberForActor(ctx context.Context, tenantID, actorID string) (domain.OperatorProfile, error)
}) *LeaveService {
	return &LeaveService{repo: repo, member: member, now: time.Now}
}

// LeaveApprover is the caller's approving identity, derived by the HTTP layer
// from their ACTIVE grants and never from the body: a park head decides the
// park_head slot for the parks they head, an `hr` holder decides the hr slot,
// and the CEO floor may decide either.
type LeaveApprover struct {
	ParkHeadParks []string
	// ParkHeadOfHomePark marks a park_head grant that is NOT park-scoped (the live roster
	// seats park heads on tenant-scoped grants); the service resolves the park from the
	// caller's own workforce profile before any queue read or decision.
	ParkHeadOfHomePark bool
	HR                 bool
	Any                bool
}

func (a LeaveApprover) mayApprove() bool {
	return a.Any || a.HR || len(a.ParkHeadParks) > 0 || a.ParkHeadOfHomePark
}

// resolveApprover turns a home-park park head into an explicit park list, so every read and
// decision below compares park ids and never a grant shape.
func (s *LeaveService) resolveApprover(ctx context.Context, tenantID, actorID string, approver LeaveApprover) (LeaveApprover, error) {
	if !approver.ParkHeadOfHomePark {
		return approver, nil
	}
	member, err := s.member.GetMemberForActor(ctx, tenantID, actorID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			approver.ParkHeadOfHomePark = false
			return approver, nil
		}
		return approver, err
	}
	if member.PrimaryLocationID != nil && *member.PrimaryLocationID != "" {
		approver.ParkHeadParks = append(approver.ParkHeadParks, *member.PrimaryLocationID)
	}
	approver.ParkHeadOfHomePark = false
	return approver, nil
}

func (a LeaveApprover) headsPark(parkID string) bool {
	for _, p := range a.ParkHeadParks {
		if p != "" && p == parkID {
			return true
		}
	}
	return false
}

// Request raises a leave request for the caller's own profile.
func (s *LeaveService) Request(ctx context.Context, tenantID, actorID string, body domain.LeaveRequestCreate, localeTag, traceID string) (*domain.LeaveRequestResponse, error) {
	if strings.TrimSpace(body.IdempotencyKey) == "" {
		return nil, BadRequest("missing_idempotency_key", "idempotency_key is required")
	}
	copyMap := leaveCopyFor(localeTag)
	starts, err := time.Parse("2006-01-02", strings.TrimSpace(body.StartsOn))
	if err != nil {
		return nil, BadRequest("invalid_starts_on", "starts_on must be YYYY-MM-DD: "+err.Error())
	}
	ends, err := time.Parse("2006-01-02", strings.TrimSpace(body.EndsOn))
	if err != nil {
		return nil, BadRequest("invalid_ends_on", "ends_on must be YYYY-MM-DD: "+err.Error())
	}
	if ends.Before(starts) {
		return nil, &Error{Code: "invalid_window", Message: copyMap["error.window"], HTTPStatus: 422}
	}
	today := biztime.BusinessDate(s.now())
	if body.StartsOn < today {
		return nil, &Error{Code: "starts_in_past", Message: copyMap["error.past"], HTTPStatus: 422}
	}
	if leaveDays(body.StartsOn, body.EndsOn) > domain.MaxLeaveDays {
		return nil, &Error{Code: "window_too_long", Message: fmt.Sprintf(copyMap["error.too_long"], domain.MaxLeaveDays), HTTPStatus: 422}
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		return nil, &Error{Code: "reason_required", Message: copyMap["error.reason"], HTTPStatus: 422}
	}
	if len([]rune(reason)) > domain.MaxLeaveReasonLength {
		return nil, BadRequest("reason_too_long", "reason must be at most 2000 characters")
	}
	member, err := s.member.GetMemberForActor(ctx, tenantID, actorID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil, Forbidden("operator_profile_missing", "no active workforce profile for this login")
		}
		return nil, err
	}
	config, err := s.repo.GetLeaveApprovalConfig(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	parkID := ""
	if member.PrimaryLocationID != nil {
		parkID = *member.PrimaryLocationID
	}
	// A person with no park cannot be routed to a park head; that slot drops
	// away and HR alone decides. Nobody to route to at all is a config gap the
	// CEO must fix, never a request silently approved.
	parkHeadRequired := config.ParkHeadRequired && parkID != ""
	hrRequired := config.HRRequired
	if !parkHeadRequired && !hrRequired {
		return nil, &Error{Code: "leave_routing_unavailable", Message: copyMap["error.routing"], HTTPStatus: 422}
	}
	write, err := s.repo.CreateLeaveRequest(ctx, ports.CreateLeaveRequestCommand{
		TenantID:          tenantID,
		WorkforceMemberID: member.OperatorID,
		ActorUserID:       actorID,
		ParkID:            parkID,
		StartsOn:          body.StartsOn,
		EndsOn:            body.EndsOn,
		Reason:            reason,
		IdempotencyKey:    body.IdempotencyKey,
		ParkHeadRequired:  parkHeadRequired,
		HRRequired:        hrRequired,
		BusinessDate:      today,
	})
	if err != nil {
		return nil, mapLeaveError(err, copyMap)
	}
	return &domain.LeaveRequestResponse{
		Request:          s.composeLeave(write.Row, member.OperatorID, LeaveApprover{}, copyMap),
		IdempotentReplay: write.Replayed,
		TraceID:          traceID,
	}, nil
}

// Withdraw cancels the caller's own pending request.
func (s *LeaveService) Withdraw(ctx context.Context, tenantID, actorID, leaveRequestID, idempotencyKey, localeTag, traceID string) (*domain.LeaveRequestResponse, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, BadRequest("missing_idempotency_key", "Idempotency-Key is required")
	}
	if _, err := uuid.Parse(leaveRequestID); err != nil {
		return nil, BadRequest("invalid_leave_request_id", "leave_request_id must be a UUID")
	}
	copyMap := leaveCopyFor(localeTag)
	member, err := s.member.GetMemberForActor(ctx, tenantID, actorID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil, Forbidden("operator_profile_missing", "no active workforce profile for this login")
		}
		return nil, err
	}
	write, err := s.repo.WithdrawLeaveRequest(ctx, ports.WithdrawLeaveRequestCommand{
		TenantID:          tenantID,
		LeaveRequestID:    leaveRequestID,
		WorkforceMemberID: member.OperatorID,
		ActorUserID:       actorID,
		IdempotencyKey:    idempotencyKey,
		BusinessDate:      biztime.BusinessDate(s.now()),
	})
	if err != nil {
		return nil, mapLeaveError(err, copyMap)
	}
	return &domain.LeaveRequestResponse{
		Request:          s.composeLeave(write.Row, member.OperatorID, LeaveApprover{}, copyMap),
		IdempotentReplay: write.Replayed,
		TraceID:          traceID,
	}, nil
}

// MyRequests is the caller's own history (bounded, newest window first).
func (s *LeaveService) MyRequests(ctx context.Context, tenantID, actorID, localeTag, traceID string) (*domain.LeaveRequestListResponse, error) {
	copyMap := leaveCopyFor(localeTag)
	member, err := s.member.GetMemberForActor(ctx, tenantID, actorID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil, Forbidden("operator_profile_missing", "no active workforce profile for this login")
		}
		return nil, err
	}
	rows, err := s.MyRequestRows(ctx, tenantID, member.OperatorID)
	if err != nil {
		return nil, err
	}
	resp := &domain.LeaveRequestListResponse{Items: make([]domain.LeaveRequest, 0, len(rows)), Copy: copyMap, TraceID: traceID}
	for _, row := range rows {
		resp.Items = append(resp.Items, s.composeLeave(row, member.OperatorID, LeaveApprover{}, copyMap))
	}
	return resp, nil
}

// MyRequestRows is the raw own-history read the Clock status read shares.
func (s *LeaveService) MyRequestRows(ctx context.Context, tenantID, memberID string) ([]ports.LeaveRequestRow, error) {
	return s.repo.ListLeaveRequestsForMember(ctx, tenantID, memberID, biztime.BusinessDate(s.now()), domain.MaxLeavePageSize)
}

// ComposeForMember renders own-history rows for the Clock status read.
func (s *LeaveService) ComposeForMember(rows []ports.LeaveRequestRow, memberID, localeTag string) []domain.LeaveRequest {
	copyMap := leaveCopyFor(localeTag)
	out := make([]domain.LeaveRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.composeLeave(row, memberID, LeaveApprover{}, copyMap))
	}
	return out
}

// TodayForMember reports whether an approved request covers the business date.
func (s *LeaveService) TodayForMember(ctx context.Context, tenantID, memberID, businessDate, localeTag string) (domain.LeaveTodaySummary, error) {
	row, err := s.repo.LeaveTodayForMember(ctx, tenantID, memberID, businessDate)
	if err != nil || row == nil {
		return domain.LeaveTodaySummary{}, err
	}
	copyMap := leaveCopyFor(localeTag)
	return domain.LeaveTodaySummary{OnLeave: true, Label: fmt.Sprintf(copyMap["today.on_leave"], datesLabel(row.StartsOn, row.EndsOn, copyMap))}, nil
}

// Queue is the approver's open queue.
func (s *LeaveService) Queue(ctx context.Context, tenantID, actorID string, approver LeaveApprover, limit int, cursor, localeTag, traceID string) (*domain.LeaveRequestListResponse, error) {
	copyMap := leaveCopyFor(localeTag)
	if !approver.mayApprove() {
		return nil, Forbidden("permission_denied", copyMap["error.not_approver"])
	}
	approver, err := s.resolveApprover(ctx, tenantID, actorID, approver)
	if err != nil {
		return nil, err
	}
	page, err := s.repo.ListLeaveQueue(ctx, ports.LeaveQueueParams{
		TenantID:      tenantID,
		ParkHeadParks: approver.ParkHeadParks,
		HR:            approver.HR,
		Any:           approver.Any,
		Limit:         limit,
		Cursor:        cursor,
	})
	if err != nil {
		return nil, mapLeaveError(err, copyMap)
	}
	resp := &domain.LeaveRequestListResponse{Items: make([]domain.LeaveRequest, 0, len(page.Rows)), NextCursor: page.NextCursor, Copy: copyMap, TraceID: traceID}
	for _, row := range page.Rows {
		resp.Items = append(resp.Items, s.composeLeave(row, "", approver, copyMap))
	}
	return resp, nil
}

// Decide records the caller's slot decision.
func (s *LeaveService) Decide(ctx context.Context, tenantID, actorID, leaveRequestID string, approve bool, body domain.LeaveDecisionRequest, approver LeaveApprover, localeTag, traceID string) (*domain.LeaveRequestResponse, error) {
	copyMap := leaveCopyFor(localeTag)
	if strings.TrimSpace(body.IdempotencyKey) == "" {
		return nil, BadRequest("missing_idempotency_key", "Idempotency-Key is required")
	}
	if _, err := uuid.Parse(leaveRequestID); err != nil {
		return nil, BadRequest("invalid_leave_request_id", "leave_request_id must be a UUID")
	}
	if !approver.mayApprove() {
		return nil, Forbidden("permission_denied", copyMap["error.not_approver"])
	}
	note := strings.TrimSpace(body.Reason)
	if !approve && note == "" {
		return nil, &Error{Code: "reason_required", Message: copyMap["error.reject_reason"], HTTPStatus: 422}
	}
	if len([]rune(note)) > domain.MaxLeaveReasonLength {
		return nil, BadRequest("reason_too_long", "reason must be at most 2000 characters")
	}
	approver, err := s.resolveApprover(ctx, tenantID, actorID, approver)
	if err != nil {
		return nil, err
	}
	row, err := s.repo.GetLeaveRequest(ctx, tenantID, leaveRequestID)
	if err != nil {
		return nil, mapLeaveError(err, copyMap)
	}
	slot, err := resolveLeaveSlot(row, approver, strings.TrimSpace(body.Slot), copyMap)
	if err != nil {
		return nil, err
	}
	decision := domain.LeaveDecisionApproved
	if !approve {
		decision = domain.LeaveDecisionRejected
	}
	write, err := s.repo.DecideLeaveRequest(ctx, ports.DecideLeaveRequestCommand{
		TenantID:       tenantID,
		LeaveRequestID: leaveRequestID,
		Slot:           slot,
		Decision:       decision,
		Note:           note,
		ActorUserID:    actorID,
		IdempotencyKey: body.IdempotencyKey,
		BusinessDate:   biztime.BusinessDate(s.now()),
	})
	if err != nil {
		return nil, mapLeaveError(err, copyMap)
	}
	return &domain.LeaveRequestResponse{
		Request:          s.composeLeave(write.Row, "", approver, copyMap),
		IdempotentReplay: write.Replayed,
		TraceID:          traceID,
	}, nil
}

// resolveLeaveSlot picks the slot the caller decides on this request. The
// slot is DERIVED from authority: a body slot is honoured only when the caller
// may decide it, so a park head cannot sign the HR line by naming it.
func resolveLeaveSlot(row ports.LeaveRequestRow, approver LeaveApprover, requested string, copyMap map[string]string) (string, error) {
	mayParkHead := row.ParkHeadRequired && (approver.Any || approver.headsPark(row.ParkID))
	mayHR := row.HRRequired && (approver.Any || approver.HR)
	openParkHead := mayParkHead && row.ParkHeadDecision == ""
	openHR := mayHR && row.HRDecision == ""
	switch requested {
	case domain.LeaveSlotParkHead:
		if !mayParkHead {
			return "", Forbidden("permission_denied", copyMap["error.not_this_slot"])
		}
		return domain.LeaveSlotParkHead, nil
	case domain.LeaveSlotHR:
		if !mayHR {
			return "", Forbidden("permission_denied", copyMap["error.not_this_slot"])
		}
		return domain.LeaveSlotHR, nil
	case "":
	default:
		return "", BadRequest("invalid_slot", "slot must be park_head or hr")
	}
	switch {
	case openParkHead:
		return domain.LeaveSlotParkHead, nil
	case openHR:
		return domain.LeaveSlotHR, nil
	case mayParkHead:
		return domain.LeaveSlotParkHead, nil
	case mayHR:
		return domain.LeaveSlotHR, nil
	}
	return "", Forbidden("permission_denied", copyMap["error.not_this_slot"])
}

// AdminList is the People / HRMS list.
func (s *LeaveService) AdminList(ctx context.Context, tenantID, status, parkID string, limit int, cursor, localeTag, traceID string) (*domain.LeaveRequestListResponse, error) {
	copyMap := leaveCopyFor(localeTag)
	switch status {
	case "", domain.LeaveRequestStatusPending, domain.LeaveRequestStatusApproved, domain.LeaveRequestStatusRejected, domain.LeaveRequestStatusWithdrawn:
	default:
		return nil, BadRequest("invalid_status", "status must be one of pending, approved, rejected, withdrawn")
	}
	if parkID != "" {
		if _, err := uuid.Parse(parkID); err != nil {
			return nil, BadRequest("invalid_park", "park_id must be a UUID")
		}
	}
	page, err := s.repo.ListLeaveRequestsAdmin(ctx, ports.LeaveAdminListParams{TenantID: tenantID, Status: status, ParkID: parkID, Limit: limit, Cursor: cursor})
	if err != nil {
		return nil, mapLeaveError(err, copyMap)
	}
	resp := &domain.LeaveRequestListResponse{Items: make([]domain.LeaveRequest, 0, len(page.Rows)), NextCursor: page.NextCursor, Copy: copyMap, TraceID: traceID}
	for _, row := range page.Rows {
		resp.Items = append(resp.Items, s.composeLeave(row, "", LeaveApprover{}, copyMap))
	}
	return resp, nil
}

// Config reads the routing flags.
func (s *LeaveService) Config(ctx context.Context, tenantID, localeTag, traceID string) (*domain.LeaveApprovalConfigResponse, error) {
	row, err := s.repo.GetLeaveApprovalConfig(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &domain.LeaveApprovalConfigResponse{Config: composeLeaveConfig(row), Copy: leaveCopyFor(localeTag), TraceID: traceID}, nil
}

// SetConfig writes the routing flags (CEO only, gated at the route).
func (s *LeaveService) SetConfig(ctx context.Context, tenantID, actorID string, update domain.LeaveApprovalConfigUpdate, localeTag, traceID string) (*domain.LeaveApprovalConfigResponse, error) {
	copyMap := leaveCopyFor(localeTag)
	if !update.ParkHeadRequired && !update.HRRequired {
		return nil, &Error{Code: "leave_routing_required", Message: copyMap["error.config_none"], HTTPStatus: 422}
	}
	row, err := s.repo.SetLeaveApprovalConfig(ctx, tenantID, actorID, update)
	if err != nil {
		return nil, mapLeaveError(err, copyMap)
	}
	return &domain.LeaveApprovalConfigResponse{Config: composeLeaveConfig(row), Copy: copyMap, TraceID: traceID}, nil
}

func composeLeaveConfig(row ports.LeaveApprovalConfigRow) domain.LeaveApprovalConfig {
	out := domain.LeaveApprovalConfig{ParkHeadRequired: row.ParkHeadRequired, HRRequired: row.HRRequired, RowVersion: row.RowVersion, UpdatedByName: row.UpdatedByName}
	if row.UpdatedAt != nil {
		out.UpdatedAt = row.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (s *LeaveService) composeLeave(row ports.LeaveRequestRow, viewerMemberID string, approver LeaveApprover, copyMap map[string]string) domain.LeaveRequest {
	out := domain.LeaveRequest{
		LeaveRequestID:    row.LeaveRequestID,
		WorkforceMemberID: row.WorkforceMemberID,
		PersonName:        row.PersonName,
		Designation:       designationLabel(row.DesignationLabel, row.RoleHint, row.DesignationGrade, clockCopyFor("en")),
		StartsOn:          row.StartsOn,
		EndsOn:            row.EndsOn,
		DayCount:          leaveDays(row.StartsOn, row.EndsOn),
		DatesLabel:        datesLabel(row.StartsOn, row.EndsOn, copyMap),
		Reason:            row.Reason,
		Status:            row.Status,
		StatusLabel:       copyMap["status."+row.Status],
		ParkHeadRequired:  row.ParkHeadRequired,
		HRRequired:        row.HRRequired,
		RaisedAt:          row.RaisedAt.UTC().Format(time.RFC3339),
		RaisedAtLabel:     fmt.Sprintf(copyMap["raised_on"], biztime.FarmDate(row.RaisedAt)),
		RowVersion:        row.RowVersion,
	}
	if row.ParkID != "" {
		park := row.ParkID
		out.ParkID = &park
	}
	if row.ParkLabel != "" {
		label := row.ParkLabel
		out.ParkLabel = &label
	}
	if row.ParkHeadDecision != "" && row.ParkHeadDecidedAt != nil {
		out.ParkHead = &domain.LeaveSlotDecision{Slot: domain.LeaveSlotParkHead, Decision: row.ParkHeadDecision, DecidedByName: row.ParkHeadDeciderNm, DecidedAt: row.ParkHeadDecidedAt.UTC().Format(time.RFC3339), Note: row.ParkHeadNote}
	}
	if row.HRDecision != "" && row.HRDecidedAt != nil {
		out.HR = &domain.LeaveSlotDecision{Slot: domain.LeaveSlotHR, Decision: row.HRDecision, DecidedByName: row.HRDeciderName, DecidedAt: row.HRDecidedAt.UTC().Format(time.RFC3339), Note: row.HRNote}
	}
	if row.DecidedAt != nil {
		at := row.DecidedAt.UTC().Format(time.RFC3339)
		out.DecidedAt = &at
	}
	out.StatusLine = leaveStatusLine(row, copyMap)
	out.CanWithdraw = viewerMemberID != "" && viewerMemberID == row.WorkforceMemberID && row.Status == domain.LeaveRequestStatusPending
	if approver.mayApprove() && row.Status == domain.LeaveRequestStatusPending {
		if slot, err := resolveLeaveSlot(row, approver, "", copyMap); err == nil {
			out.MySlot = slot
			out.MySlotLabel = copyMap["slot."+slot]
		}
	}
	return out
}

// leaveStatusLine says where the request stands, in farm words.
func leaveStatusLine(row ports.LeaveRequestRow, copyMap map[string]string) string {
	switch row.Status {
	case domain.LeaveRequestStatusWithdrawn:
		return copyMap["line.withdrawn"]
	case domain.LeaveRequestStatusApproved:
		return copyMap["line.approved"]
	case domain.LeaveRequestStatusRejected:
		by, note := "", ""
		switch {
		case row.ParkHeadDecision == domain.LeaveDecisionRejected:
			by, note = copyMap["slot.park_head"], row.ParkHeadNote
		case row.HRDecision == domain.LeaveDecisionRejected:
			by, note = copyMap["slot.hr"], row.HRNote
		}
		line := fmt.Sprintf(copyMap["line.rejected_by"], by)
		if note != "" {
			line += " · " + note
		}
		return line
	}
	parts := make([]string, 0, 2)
	if row.ParkHeadRequired {
		if row.ParkHeadDecision == domain.LeaveDecisionApproved {
			parts = append(parts, copyMap["line.park_head_approved"])
		} else {
			parts = append(parts, copyMap["line.waiting_park_head"])
		}
	}
	if row.HRRequired {
		if row.HRDecision == domain.LeaveDecisionApproved {
			parts = append(parts, copyMap["line.hr_approved"])
		} else {
			parts = append(parts, copyMap["line.waiting_hr"])
		}
	}
	return strings.Join(parts, " · ")
}

// datesLabel renders "12–14 Sep 2026 · 3 days" or "12 Sep 2026 · 1 day".
func datesLabel(startsOn, endsOn string, copyMap map[string]string) string {
	start, err1 := time.Parse("2006-01-02", startsOn)
	end, err2 := time.Parse("2006-01-02", endsOn)
	if err1 != nil || err2 != nil {
		return startsOn + " – " + endsOn
	}
	days := leaveDays(startsOn, endsOn)
	unit := copyMap["unit.days"]
	if days == 1 {
		unit = copyMap["unit.day"]
	}
	// Every visible date is DD/MM/YYYY (maintainer decision 2026-09-10), so a range renders
	// both ends in full rather than eliding the shared month or year. The elision saved a few
	// characters and cost the reader a second date shape to learn.
	var window string
	if start.Equal(end) {
		window = biztime.FarmDate(start)
	} else {
		window = biztime.FarmDate(start) + " – " + biztime.FarmDate(end)
	}
	return fmt.Sprintf("%s · %d %s", window, days, unit)
}

// leaveDays is the inclusive day span; both bounds are business DATES.
func leaveDays(startsOn, endsOn string) int {
	start, err1 := time.Parse("2006-01-02", startsOn)
	end, err2 := time.Parse("2006-01-02", endsOn)
	if err1 != nil || err2 != nil || end.Before(start) {
		return 0
	}
	return int(end.Sub(start).Hours()/24) + 1
}

func mapLeaveError(err error, copyMap map[string]string) error {
	switch {
	case errors.Is(err, ports.ErrLeaveOverlap):
		return Conflict("leave_overlap", copyMap["error.overlap"])
	case errors.Is(err, ports.ErrLeaveNotPending):
		return Conflict("leave_not_pending", copyMap["error.not_pending"])
	case errors.Is(err, ports.ErrLeaveSlotDecided):
		return Conflict("leave_slot_decided", copyMap["error.slot_decided"])
	case errors.Is(err, ports.ErrLeaveSlotNotRequired):
		return BadRequest("leave_slot_not_required", "this approver slot is not required for the request")
	case errors.Is(err, ports.ErrLeaveConfigConflict):
		return Conflict("leave_config_conflict", copyMap["error.config_conflict"])
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "this key was already used with a different payload")
	case errors.Is(err, ports.ErrIdempotencyInFlight):
		return Conflict("idempotency_in_flight", "this request is still being processed")
	case errors.Is(err, ports.ErrNotFound):
		return NotFound("leave request not found")
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return err
	}
	if strings.Contains(err.Error(), "invalid cursor") {
		return BadRequest("invalid_cursor", "cursor is not valid")
	}
	return err
}

// leaveCopyFor returns the backend-owned leave copy. Hindi, Kannada and
// Telugu fall back to English until the farm supplies translations; the keys
// are stable so a translation lands without a client change.
func leaveCopyFor(localeTag string) map[string]string {
	_ = localeTag
	return leaveCopyEN
}

var leaveCopyEN = map[string]string{
	"title":                    "Leave",
	"request.title":            "Request leave",
	"request.from":             "From",
	"request.to":               "To",
	"request.reason":           "Reason",
	"request.reason_hint":      "Why do you need leave?",
	"request.submit":           "Submit request",
	"request.submitted":        "Leave request sent for approval",
	"request.withdraw":         "Withdraw request",
	"request.withdraw_confirm": "Withdraw this leave request?",
	"request.resubmit":         "Request again",
	"list.title":               "Your leave",
	"list.empty":               "No leave requests yet",
	"queue.title":              "Leave requests",
	"queue.empty":              "No leave requests waiting for you",
	"approve":                  "Approve",
	"reject":                   "Reject",
	"reject.reason":            "Reason for rejecting",
	"reject.reason_hint":       "Tell the person why",
	"raised_on":                "Asked on %s",
	"today.on_leave":           "On leave · %s",
	"status.pending":           "Waiting for approval",
	"status.approved":          "Approved",
	"status.rejected":          "Rejected",
	"status.withdrawn":         "Withdrawn",
	"slot.park_head":           "Park head",
	"slot.hr":                  "HR",
	"line.waiting_park_head":   "Waiting for park head",
	"line.waiting_hr":          "Waiting for HR",
	"line.park_head_approved":  "Park head approved",
	"line.hr_approved":         "HR approved",
	"line.approved":            "Approved by park head and HR",
	"line.rejected_by":         "Rejected by %s",
	"line.withdrawn":           "Withdrawn by the person",
	"unit.day":                 "day",
	"unit.days":                "days",
	"config.title":             "Who approves leave",
	"config.park_head":         "Park head of the person's park",
	"config.hr":                "HR",
	"config.help":              "Every ticked approver must accept before leave is granted. Any one of them can reject.",
	"config.save":              "Save",
	"error.window":             "The last day cannot be before the first day",
	"error.past":               "Leave cannot start on a day that has passed",
	"error.too_long":           "One request can cover at most %d days",
	"error.reason":             "Please say why you need leave",
	"error.reject_reason":      "Please say why you are rejecting",
	"error.overlap":            "You already have a leave request covering these days",
	"error.not_pending":        "This request has already been decided",
	"error.slot_decided":       "You have already decided this request",
	"error.routing":            "Leave approvals are not set up yet. Ask the office.",
	"error.not_approver":       "You are not an approver for leave",
	"error.not_this_slot":      "This request is not waiting for your approval",
	"error.config_conflict":    "Someone else changed this setting. Reload and try again.",
	"error.config_none":        "At least one approver must be ticked",
}
