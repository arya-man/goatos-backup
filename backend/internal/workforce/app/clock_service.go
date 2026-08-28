package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// ClockService is the Clock In / Clock Out module (docs/features/clock-in-out/
// plan.md). It owns every label both surfaces render: punch time labels, the
// hours string, flag chips, presence buckets, and the not-clocked-in banner.
// Clients render these verbatim (backend-owns-copy rule); the phone composes
// NOTHING beyond the live elapsed tick of an open entry.
type ClockService struct {
	repo   ports.ClockRepository
	people ports.PeopleRepository
	member interface {
		GetMemberForActor(ctx context.Context, tenantID, actorID string) (domain.OperatorProfile, error)
	}
}

func NewClockService(repo ports.ClockRepository, people ports.PeopleRepository, member interface {
	GetMemberForActor(ctx context.Context, tenantID, actorID string) (domain.OperatorProfile, error)
}) *ClockService {
	return &ClockService{repo: repo, people: people, member: member}
}

const (
	clockEventIn  = "clock_in"
	clockEventOut = "clock_out"
)

// Punch records a clock-in or clock-out. The integrity gate runs HERE, before
// any write: a payload admitting a mock-provided fix or an installed
// mock-location app is refused 422 regardless of what the client decided.
func (s *ClockService) Punch(ctx context.Context, tenantID, actorID, eventType string, req domain.ClockPunchRequest, client httpmiddleware.ClientInfo, localeTag, traceID string) (*domain.ClockPunchResponse, error) {
	if eventType != clockEventIn && eventType != clockEventOut {
		return nil, BadRequest("invalid_event_type", "unknown clock event type")
	}
	if req.Integrity.MockLocation || len(req.Integrity.MockProviderPackages) > 0 {
		return nil, &Error{Code: "mock_location_detected", Message: clockCopyFor(localeTag)["refusal.mock"], HTTPStatus: 422}
	}
	switch req.Location.Status {
	case "captured", "permission_missing", "unavailable":
	case "":
		req.Location.Status = "unavailable"
	default:
		return nil, BadRequest("invalid_location_status", "unknown location status")
	}
	if req.NetworkKind != "" && req.NetworkKind != "wifi" && req.NetworkKind != "cellular" {
		req.NetworkKind = ""
	}

	member, err := s.member.GetMemberForActor(ctx, tenantID, actorID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil, Forbidden("operator_profile_missing", "no active workforce profile for this login")
		}
		return nil, err
	}

	now := time.Now()
	capturedAt := now
	if strings.TrimSpace(req.CapturedAt) != "" {
		parsed, parseErr := time.Parse(time.RFC3339, req.CapturedAt)
		if parseErr != nil {
			return nil, BadRequest("invalid_captured_at", "captured_at must be RFC3339")
		}
		capturedAt = parsed
	}

	// Effective instant (maintainer decision D1): server now for an online
	// punch; the device tap time for an offline-queued one, where arrival can
	// be hours late and stamping arrival would move the day.
	effective := now
	networkType := "online"
	if req.Offline {
		effective = capturedAt
		networkType = "offline_queued"
	}
	skewMs := now.Sub(capturedAt).Milliseconds()

	record, err := s.repo.RecordClockPunch(ctx, ports.ClockPunchCommand{
		TenantID:          tenantID,
		WorkforceMemberID: member.OperatorID,
		UserID:            actorID,
		EventType:         eventType,
		IdempotencyKey:    req.IdempotencyKey,
		BusinessDate:      biztime.BusinessDate(effective),
		EffectiveAt:       effective,
		CapturedAt:        capturedAt,
		ClockSkewMs:       skewMs,
		Location:          req.Location,
		Integrity:         req.Integrity,
		DeviceID:          client.DeviceID,
		AppVersion:        client.AppVersion,
		AppVersionCode:    client.AppVersionCode,
		BuildType:         client.BuildType,
		OSVersion:         client.OSVersion,
		SDKVersion:        client.SDKVersion,
		DeviceModel:       client.DeviceModel,
		NetworkType:       networkType,
		BatteryPct:        req.BatteryPct,
	})
	if err != nil {
		return nil, mapClockError(err, localeTag)
	}
	copyMap := clockCopyFor(localeTag)
	entry := s.composeEntry(record.Entry, member.DisplayName, "", "", nil, nil, copyMap)
	return &domain.ClockPunchResponse{Entry: entry, TraceID: traceID}, nil
}

func mapClockError(err error, localeTag string) error {
	copyMap := clockCopyFor(localeTag)
	switch {
	case errors.Is(err, ports.ErrAlreadyClockedIn):
		return Conflict("already_clocked_in", copyMap["refusal.already_in"])
	case errors.Is(err, ports.ErrNotClockedIn):
		return Conflict("not_clocked_in", copyMap["refusal.not_in"])
	case errors.Is(err, ports.ErrAlreadyClockedOut):
		return Conflict("already_clocked_out", copyMap["refusal.already_out"])
	case errors.Is(err, ports.ErrMockLocationDetected):
		return &Error{Code: "mock_location_detected", Message: copyMap["refusal.mock"], HTTPStatus: 422}
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "this punch key was already used with a different payload")
	default:
		return err
	}
}

// Status serves the My Clock screen and the shell banner in one read.
func (s *ClockService) Status(ctx context.Context, tenantID, actorID, localeTag, traceID string) (*domain.ClockStatusResponse, error) {
	member, err := s.member.GetMemberForActor(ctx, tenantID, actorID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil, Forbidden("operator_profile_missing", "no active workforce profile for this login")
		}
		return nil, err
	}
	today := biztime.BusinessDate(time.Now())
	day, recent, err := s.repo.ClockDayForMember(ctx, ports.ClockStatusParams{
		TenantID:          tenantID,
		WorkforceMemberID: member.OperatorID,
		BusinessDate:      today,
		RecentLimit:       14,
	})
	if err != nil {
		return nil, err
	}
	copyMap := clockCopyFor(localeTag)
	resp := &domain.ClockStatusResponse{
		BusinessDate:     today,
		State:            "not_clocked_in",
		BannerText:       copyMap["banner.not_clocked_in"],
		PunchRefusedCopy: copyMap["refusal.mock_named"],
		Copy:             copyMap,
		RecentEntries:    []domain.ClockEntry{},
		TraceID:          traceID,
	}
	if day != nil {
		entry := s.composeEntry(*day, member.DisplayName, "", "", nil, nil, copyMap)
		resp.Entry = &entry
		resp.BannerText = ""
		if day.Status == "open" {
			resp.State = "clocked_in"
		} else {
			resp.State = "clocked_out"
		}
	}
	for _, row := range recent {
		resp.RecentEntries = append(resp.RecentEntries, s.composeEntry(row, member.DisplayName, "", "", nil, nil, copyMap))
	}
	return resp, nil
}

// Presence serves the leadership Team page. The admin-web clock tab reads the
// SAME repository method through AdminEntries below — cross-surface parity by
// construction, not by test alone.
func (s *ClockService) Presence(ctx context.Context, tenantID string, params ports.ClockPresenceParams, localeTag, traceID string) (*domain.ClockPresenceResponse, error) {
	if strings.TrimSpace(params.BusinessDate) == "" {
		params.BusinessDate = biztime.BusinessDate(time.Now())
	} else if _, err := time.Parse("2006-01-02", params.BusinessDate); err != nil {
		return nil, BadRequest("invalid_date", "date must be YYYY-MM-DD")
	}
	params.TenantID = tenantID
	page, err := s.repo.ListClockPresence(ctx, params)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidFilter) {
			return nil, BadRequest("invalid_cursor", "cursor is not valid")
		}
		return nil, err
	}
	copyMap := clockCopyFor(localeTag)
	today := biztime.BusinessDate(time.Now())
	resp := &domain.ClockPresenceResponse{
		BusinessDate: params.BusinessDate,
		IsToday:      params.BusinessDate == today,
		Summary:      page.Summary,
		Rows:         []domain.ClockPresenceRow{},
		NextCursor:   page.NextCursor,
		Designations: clockDesignationOptions(),
		Copy:         copyMap,
		TraceID:      traceID,
	}
	catalog, err := s.people.PeopleCatalog(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	resp.Parks = catalog.Parks
	for _, raw := range page.Rows {
		resp.Rows = append(resp.Rows, s.composePresenceRow(raw, params.BusinessDate, today, copyMap))
	}
	return resp, nil
}

// PersonDay is the presence drill-down: one person's day in full.
func (s *ClockService) PersonDay(ctx context.Context, tenantID, workforceMemberID, businessDate, localeTag, traceID string) (*domain.ClockPersonDayResponse, error) {
	if strings.TrimSpace(businessDate) == "" {
		businessDate = biztime.BusinessDate(time.Now())
	} else if _, err := time.Parse("2006-01-02", businessDate); err != nil {
		return nil, BadRequest("invalid_date", "date must be YYYY-MM-DD")
	}
	detail, err := s.repo.ClockPersonDayDetail(ctx, tenantID, workforceMemberID, businessDate)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil, NotFound("person not found")
		}
		return nil, err
	}
	return s.composePersonDay(detail, businessDate, localeTag, traceID), nil
}

// AdminEntries is the admin-web People/HRMS Clock In / Out tab list. It reads
// the SAME repository page as the phone presence board, so the two surfaces
// cannot disagree on a number (cross-surface parity by construction).
func (s *ClockService) AdminEntries(ctx context.Context, tenantID string, params ports.ClockPresenceParams, localeTag, traceID string) (*domain.ClockEntriesListResponse, error) {
	if strings.TrimSpace(params.BusinessDate) == "" {
		params.BusinessDate = biztime.BusinessDate(time.Now())
	} else if _, err := time.Parse("2006-01-02", params.BusinessDate); err != nil {
		return nil, BadRequest("invalid_date", "date must be YYYY-MM-DD")
	}
	params.TenantID = tenantID
	page, err := s.repo.ListClockPresence(ctx, params)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidFilter) {
			return nil, BadRequest("invalid_cursor", "cursor is not valid")
		}
		return nil, err
	}
	copyMap := clockCopyFor(localeTag)
	items := make([]domain.ClockEntry, 0, len(page.Rows))
	for _, raw := range page.Rows {
		designation := designationLabel(raw.RoleHint, raw.DesignationGrade, copyMap)
		var parkLabel *string
		if raw.ParkLabel != "" {
			pl := raw.ParkLabel
			parkLabel = &pl
		}
		if raw.Entry != nil {
			entry := s.composeEntry(*raw.Entry, raw.PersonName, designation, raw.RoleHint, parkLabel, optionalString(raw.DepartmentLabel), copyMap)
			items = append(items, entry)
			continue
		}
		// Not clocked in: an honest roster row with no punch facts.
		items = append(items, domain.ClockEntry{
			WorkforceMemberID: raw.WorkforceMemberID,
			PersonName:        raw.PersonName,
			RoleHint:          raw.RoleHint,
			Designation:       designation,
			ParkLabel:         parkLabel,
			DepartmentLabel:   optionalString(raw.DepartmentLabel),
			BusinessDate:      params.BusinessDate,
			Flags:             []domain.ClockFlag{},
		})
	}
	catalog, err := s.people.PeopleCatalog(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &domain.ClockEntriesListResponse{
		Summary:      page.Summary,
		Items:        items,
		NextCursor:   page.NextCursor,
		Parks:        catalog.Parks,
		Designations: clockDesignationOptions(),
		TraceID:      traceID,
	}, nil
}

// EntryDetail is the admin drawer: the paired entry plus every punch capture.
func (s *ClockService) EntryDetail(ctx context.Context, tenantID, clockEntryID, localeTag, traceID string) (*domain.ClockEntryDetailResponse, error) {
	detail, err := s.repo.ClockEntryDetail(ctx, tenantID, clockEntryID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil, NotFound("clock entry not found")
		}
		return nil, err
	}
	day := s.composePersonDay(detail, "", localeTag, traceID)
	if day.Entry == nil {
		return nil, NotFound("clock entry not found")
	}
	return &domain.ClockEntryDetailResponse{Entry: *day.Entry, Events: day.Events, TraceID: traceID}, nil
}

func (s *ClockService) composePersonDay(detail ports.ClockPersonDay, businessDate, localeTag, traceID string) *domain.ClockPersonDayResponse {
	copyMap := clockCopyFor(localeTag)
	if businessDate == "" && detail.Entry != nil {
		businessDate = detail.Entry.BusinessDate
	}
	resp := &domain.ClockPersonDayResponse{
		PersonName:   detail.Person.PersonName,
		Designation:  designationLabel(detail.Person.RoleHint, detail.Person.DesignationGrade, copyMap),
		BusinessDate: businessDate,
		Events:       []domain.ClockEventDetail{},
		RecentDays:   []domain.ClockEntry{},
		Copy:         copyMap,
		TraceID:      traceID,
	}
	if detail.Person.ParkLabel != "" {
		park := detail.Person.ParkLabel
		resp.ParkLabel = &park
	}
	if detail.Entry != nil {
		entry := s.composeEntry(*detail.Entry, detail.Person.PersonName,
			designationLabel(detail.Person.RoleHint, detail.Person.DesignationGrade, copyMap),
			detail.Person.RoleHint, resp.ParkLabel, optionalString(detail.Person.DepartmentLabel), copyMap)
		resp.Entry = &entry
	}
	for _, ev := range detail.Events {
		resp.Events = append(resp.Events, composeEventDetail(ev))
	}
	for _, row := range detail.RecentDays {
		resp.RecentDays = append(resp.RecentDays, s.composeEntry(row, detail.Person.PersonName, "", "", nil, nil, copyMap))
	}
	return resp
}

func optionalString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func composeEventDetail(ev ports.ClockEventRow) domain.ClockEventDetail {
	return domain.ClockEventDetail{
		ClockEventID:            ev.ClockEventID,
		EventType:               ev.EventType,
		BusinessDate:            ev.BusinessDate,
		CapturedAt:              ev.CapturedAt.UTC().Format(time.RFC3339),
		RecordedAt:              ev.RecordedAt.UTC().Format(time.RFC3339),
		ClockSkewMs:             ev.ClockSkewMs,
		LocationStatus:          ev.LocationStatus,
		Latitude:                ev.Latitude,
		Longitude:               ev.Longitude,
		GpsAccuracyM:            ev.GpsAccuracyM,
		Address:                 ev.Address,
		MockLocation:            ev.MockLocation,
		DeveloperOptionsEnabled: ev.DeveloperOptionsEnabled,
		DeviceModel:             ev.DeviceModel,
		AppVersion:              ev.AppVersion,
		OSVersion:               ev.OSVersion,
		NetworkType:             ev.NetworkType,
		BatteryPct:              ev.BatteryPct,
	}
}

// composeEntry turns a raw pairing row into the rendered contract row.
func (s *ClockService) composeEntry(row ports.ClockEntryRow, personName, designation, roleHint string, parkLabel, departmentLabel *string, copyMap map[string]string) domain.ClockEntry {
	today := biztime.BusinessDate(time.Now())
	entry := domain.ClockEntry{
		ClockEntryID:      row.ClockEntryID,
		WorkforceMemberID: row.WorkforceMemberID,
		PersonName:        personName,
		RoleHint:          roleHint,
		Designation:       designation,
		ParkLabel:         parkLabel,
		DepartmentLabel:   departmentLabel,
		BusinessDate:      row.BusinessDate,
		Status:            row.Status,
		ClockInAt:         row.ClockInAt.UTC().Format(time.RFC3339),
		ClockInLabel:      istClock(row.ClockInAt),
		WorkedMinutes:     row.WorkedMinutes,
		Flags:             []domain.ClockFlag{},
	}
	if row.ClockOutAt != nil {
		out := row.ClockOutAt.UTC().Format(time.RFC3339)
		outLabel := istClock(*row.ClockOutAt)
		entry.ClockOutAt = &out
		entry.ClockOutLabel = &outLabel
	}
	switch {
	case row.WorkedMinutes != nil:
		entry.HoursLabel = hoursLabel(*row.WorkedMinutes)
	case row.Status == "open" && row.BusinessDate == today:
		elapsed := int(time.Since(row.ClockInAt) / time.Minute)
		if elapsed < 0 {
			elapsed = 0
		}
		entry.HoursLabel = fmt.Sprintf(copyMap["hours.so_far"], hoursLabel(elapsed))
	}
	entry.LocationLabel = row.Address
	if row.DeviceModel != "" && row.AppVersion != "" {
		entry.DeviceLabel = row.DeviceModel + " · " + row.AppVersion
	} else if row.DeviceModel != "" {
		entry.DeviceLabel = row.DeviceModel
	} else {
		entry.DeviceLabel = row.AppVersion
	}
	if row.OfflinePunch {
		entry.Flags = append(entry.Flags, domain.ClockFlag{Key: "offline", Label: copyMap["flag.offline"]})
	}
	if row.LocationMissing {
		entry.Flags = append(entry.Flags, domain.ClockFlag{Key: "no_location", Label: copyMap["flag.no_location"]})
	}
	if row.Status == "auto_closed" || (row.Status == "open" && row.BusinessDate < today) {
		entry.Flags = append(entry.Flags, domain.ClockFlag{Key: "not_clocked_out", Label: copyMap["flag.not_clocked_out"]})
	}
	return entry
}

func (s *ClockService) composePresenceRow(raw ports.ClockPresenceRawRow, businessDate, today string, copyMap map[string]string) domain.ClockPresenceRow {
	row := domain.ClockPresenceRow{
		WorkforceMemberID: raw.WorkforceMemberID,
		PersonName:        raw.PersonName,
		Designation:       designationLabel(raw.RoleHint, raw.DesignationGrade, copyMap),
		Bucket:            "not_clocked_in",
		Flags:             []domain.ClockFlag{},
	}
	if raw.ParkLabel != "" {
		park := raw.ParkLabel
		row.ParkLabel = &park
	}
	entryRow := raw.Entry
	if entryRow == nil {
		return row
	}
	entry := s.composeEntry(*entryRow, raw.PersonName, row.Designation, raw.RoleHint, row.ParkLabel, nil, copyMap)
	row.ClockEntryID = entry.ClockEntryID
	row.Flags = entry.Flags
	row.ClockInAt = &entry.ClockInAt
	row.ClockOutAt = entry.ClockOutAt
	row.WorkedMinutes = entry.WorkedMinutes
	if entryRow.Status == "open" {
		row.Bucket = "working"
		if businessDate == today {
			row.TimeLabel = fmt.Sprintf(copyMap["row.working"], entry.ClockInLabel, entry.HoursLabel)
		} else {
			row.TimeLabel = fmt.Sprintf(copyMap["row.open_past"], entry.ClockInLabel)
		}
	} else {
		row.Bucket = "clocked_out"
		if entry.ClockOutLabel != nil && entry.HoursLabel != "" {
			row.TimeLabel = fmt.Sprintf(copyMap["row.closed"], entry.ClockInLabel, *entry.ClockOutLabel, entry.HoursLabel)
		} else {
			row.TimeLabel = fmt.Sprintf(copyMap["row.open_past"], entry.ClockInLabel)
		}
	}
	return row
}

func istClock(t time.Time) string {
	return t.In(biztime.Location("")).Format("15:04")
}

func hoursLabel(minutes int) string {
	if minutes < 0 {
		minutes = 0
	}
	return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
}

// designationLabel prefers the HR grade when set (leadership) and otherwise
// names the job (role hint), so the presence board reads "Director" for a CXO
// and "Operator" for field staff.
func designationLabel(roleHint, grade string, copyMap map[string]string) string {
	if grade != "" {
		if label, ok := copyMap["grade."+grade]; ok {
			return label
		}
		return grade
	}
	if label, ok := copyMap["role."+roleHint]; ok {
		return label
	}
	return roleHint
}

// designationOptions is the designation filter vocabulary: the role-hint CHECK
// set on workforce_members. IDs are filter values; labels are English catalog
// copy (the admin surface language), matching the people board's tabs.
func clockDesignationOptions() []domain.PeopleCatalogOption {
	en := clockCopyFor("en")
	hints := []string{"operator", "park_head", "pc_director", "verifier", "supervisor", "admin", "other"}
	out := make([]domain.PeopleCatalogOption, 0, len(hints))
	for _, h := range hints {
		out = append(out, domain.PeopleCatalogOption{ID: h, Code: h, Label: en["role."+h]})
	}
	return out
}

// clockCopyFor is the backend-owned copy catalog for the clock module, per
// locale. Farm language only (copy-firewall rule) — no words like "mock" or
// "GPS provider" reach an operator; the refusal names what to do.
func clockCopyFor(localeTag string) map[string]string {
	switch localization.Normalize(localeTag) {
	case "hi":
		return clockCopyHI
	case "kn":
		return clockCopyKN
	case "te":
		return clockCopyTE
	default:
		return clockCopyEN
	}
}

var clockCopyEN = map[string]string{
	"module.title":          "Clock In / Out",
	"banner.not_clocked_in": "You haven't clocked in today — tap to clock in",
	"action.clock_in":       "Clock In",
	"action.clock_out":      "Clock Out",
	"state.not_clocked_in":  "Not clocked in yet",
	"state.clocked_in":      "Clocked in at %s",
	"state.clocked_out":     "Day complete",
	"section.working":       "Working now",
	"section.worked":        "Worked",
	"section.clocked_out":   "Clocked out",
	"section.not_clocked_in": "Not clocked in",
	"summary.working":       "Working now",
	"summary.worked":        "Worked",
	"summary.clocked_out":   "Clocked out",
	"summary.not_clocked_in": "Not clocked in",
	"summary.flagged":       "Flagged",
	"row.working":           "In %s · %s",
	"row.closed":            "%s – %s · %s",
	"row.open_past":         "In %s · not clocked out",
	"hours.so_far":          "%s so far",
	"flag.offline":          "Recorded offline",
	"flag.no_location":      "No location",
	"flag.not_clocked_out":  "Not clocked out",
	"refusal.mock":          "This phone has an app that fakes its location. Remove it, then clock in.",
	"refusal.mock_named":    "Remove %s to clock in — it changes this phone's location.",
	"refusal.already_in":    "You have already clocked in today.",
	"refusal.not_in":        "Clock in first — there is no clock-in for today.",
	"refusal.already_out":   "You have already clocked out today.",
	"search.placeholder":    "Search people",
	"filter.park":           "Park",
	"filter.designation":    "Designation",
	"filter.all":            "All",
	"empty.presence":        "Nobody matches this filter.",
	"empty.recent":          "No days recorded yet.",
	"recent.title":          "Recent days",
	"team.title":            "Team",
	"check_again":           "Check again",
	"role.operator":         "Operator",
	"role.park_head":        "Park Head",
	"role.pc_director":      "Director",
	"role.verifier":         "Verifier",
	"role.supervisor":       "Supervisor",
	"role.admin":            "Admin",
	"role.other":            "Staff",
	"grade.cxo":             "CXO",
	"grade.director":        "Director",
	"grade.manager":         "Manager",
	"grade.assistant_manager": "Assistant Manager",
}

var clockCopyHI = map[string]string{
	"module.title":          "हाज़िरी (क्लॉक इन/आउट)",
	"banner.not_clocked_in": "आज क्लॉक इन नहीं हुआ है — क्लॉक इन करने के लिए दबाएँ",
	"action.clock_in":       "क्लॉक इन",
	"action.clock_out":      "क्लॉक आउट",
	"state.not_clocked_in":  "अभी क्लॉक इन नहीं हुआ",
	"state.clocked_in":      "%s बजे क्लॉक इन",
	"state.clocked_out":     "आज का दिन पूरा",
	"section.working":       "अभी काम पर",
	"section.worked":        "काम किया",
	"section.clocked_out":   "क्लॉक आउट",
	"section.not_clocked_in": "क्लॉक इन नहीं",
	"summary.working":       "अभी काम पर",
	"summary.worked":        "काम किया",
	"summary.clocked_out":   "क्लॉक आउट",
	"summary.not_clocked_in": "क्लॉक इन नहीं",
	"summary.flagged":       "ध्यान दें",
	"row.working":           "इन %s · %s",
	"row.closed":            "%s – %s · %s",
	"row.open_past":         "इन %s · क्लॉक आउट नहीं",
	"hours.so_far":          "अब तक %s",
	"flag.offline":          "ऑफ़लाइन दर्ज",
	"flag.no_location":      "लोकेशन नहीं",
	"flag.not_clocked_out":  "क्लॉक आउट नहीं",
	"refusal.mock":          "इस फ़ोन में लोकेशन बदलने वाला ऐप है। उसे हटाएँ, फिर क्लॉक इन करें।",
	"refusal.mock_named":    "क्लॉक इन के लिए %s हटाएँ — यह फ़ोन की लोकेशन बदलता है।",
	"refusal.already_in":    "आज आप पहले ही क्लॉक इन कर चुके हैं।",
	"refusal.not_in":        "पहले क्लॉक इन करें — आज का क्लॉक इन नहीं है।",
	"refusal.already_out":   "आज आप पहले ही क्लॉक आउट कर चुके हैं।",
	"search.placeholder":    "लोग खोजें",
	"filter.park":           "पार्क",
	"filter.designation":    "पद",
	"filter.all":            "सभी",
	"empty.presence":        "इस फ़िल्टर में कोई नहीं मिला।",
	"empty.recent":          "अभी कोई दिन दर्ज नहीं।",
	"recent.title":          "पिछले दिन",
	"team.title":            "टीम",
	"check_again":           "फिर जाँचें",
	"role.operator":         "ऑपरेटर",
	"role.park_head":        "पार्क प्रमुख",
	"role.pc_director":      "निदेशक",
	"role.verifier":         "सत्यापक",
	"role.supervisor":       "सुपरवाइज़र",
	"role.admin":            "एडमिन",
	"role.other":            "स्टाफ़",
	"grade.cxo":             "सीएक्सओ",
	"grade.director":        "निदेशक",
	"grade.manager":         "मैनेजर",
	"grade.assistant_manager": "सहायक मैनेजर",
}

var clockCopyKN = map[string]string{
	"module.title":          "ಹಾಜರಾತಿ (ಕ್ಲಾಕ್ ಇನ್/ಔಟ್)",
	"banner.not_clocked_in": "ಇಂದು ಕ್ಲಾಕ್ ಇನ್ ಆಗಿಲ್ಲ — ಕ್ಲಾಕ್ ಇನ್ ಮಾಡಲು ಒತ್ತಿರಿ",
	"action.clock_in":       "ಕ್ಲಾಕ್ ಇನ್",
	"action.clock_out":      "ಕ್ಲಾಕ್ ಔಟ್",
	"state.not_clocked_in":  "ಇನ್ನೂ ಕ್ಲಾಕ್ ಇನ್ ಆಗಿಲ್ಲ",
	"state.clocked_in":      "%s ಕ್ಕೆ ಕ್ಲಾಕ್ ಇನ್",
	"state.clocked_out":     "ಇಂದಿನ ದಿನ ಮುಗಿದಿದೆ",
	"section.working":       "ಈಗ ಕೆಲಸದಲ್ಲಿ",
	"section.worked":        "ಕೆಲಸ ಮಾಡಿದರು",
	"section.clocked_out":   "ಕ್ಲಾಕ್ ಔಟ್",
	"section.not_clocked_in": "ಕ್ಲಾಕ್ ಇನ್ ಇಲ್ಲ",
	"summary.working":       "ಈಗ ಕೆಲಸದಲ್ಲಿ",
	"summary.worked":        "ಕೆಲಸ ಮಾಡಿದರು",
	"summary.clocked_out":   "ಕ್ಲಾಕ್ ಔಟ್",
	"summary.not_clocked_in": "ಕ್ಲಾಕ್ ಇನ್ ಇಲ್ಲ",
	"summary.flagged":       "ಗಮನಿಸಿ",
	"row.working":           "ಇನ್ %s · %s",
	"row.closed":            "%s – %s · %s",
	"row.open_past":         "ಇನ್ %s · ಕ್ಲಾಕ್ ಔಟ್ ಇಲ್ಲ",
	"hours.so_far":          "ಈವರೆಗೆ %s",
	"flag.offline":          "ಆಫ್‌ಲೈನ್ ದಾಖಲೆ",
	"flag.no_location":      "ಸ್ಥಳ ಇಲ್ಲ",
	"flag.not_clocked_out":  "ಕ್ಲಾಕ್ ಔಟ್ ಇಲ್ಲ",
	"refusal.mock":          "ಈ ಫೋನ್‌ನಲ್ಲಿ ಸ್ಥಳ ಬದಲಿಸುವ ಆ್ಯಪ್ ಇದೆ. ಅದನ್ನು ತೆಗೆದುಹಾಕಿ, ನಂತರ ಕ್ಲಾಕ್ ಇನ್ ಮಾಡಿ.",
	"refusal.mock_named":    "ಕ್ಲಾಕ್ ಇನ್ ಮಾಡಲು %s ತೆಗೆದುಹಾಕಿ — ಅದು ಫೋನ್‌ನ ಸ್ಥಳ ಬದಲಿಸುತ್ತದೆ.",
	"refusal.already_in":    "ಇಂದು ನೀವು ಈಗಾಗಲೇ ಕ್ಲಾಕ್ ಇನ್ ಮಾಡಿದ್ದೀರಿ.",
	"refusal.not_in":        "ಮೊದಲು ಕ್ಲಾಕ್ ಇನ್ ಮಾಡಿ — ಇಂದಿನ ಕ್ಲಾಕ್ ಇನ್ ಇಲ್ಲ.",
	"refusal.already_out":   "ಇಂದು ನೀವು ಈಗಾಗಲೇ ಕ್ಲಾಕ್ ಔಟ್ ಮಾಡಿದ್ದೀರಿ.",
	"search.placeholder":    "ಜನರನ್ನು ಹುಡುಕಿ",
	"filter.park":           "ಪಾರ್ಕ್",
	"filter.designation":    "ಹುದ್ದೆ",
	"filter.all":            "ಎಲ್ಲಾ",
	"empty.presence":        "ಈ ಫಿಲ್ಟರ್‌ಗೆ ಯಾರೂ ಸಿಗಲಿಲ್ಲ.",
	"empty.recent":          "ಇನ್ನೂ ಯಾವ ದಿನವೂ ದಾಖಲಾಗಿಲ್ಲ.",
	"recent.title":          "ಇತ್ತೀಚಿನ ದಿನಗಳು",
	"team.title":            "ತಂಡ",
	"check_again":           "ಮತ್ತೆ ಪರಿಶೀಲಿಸಿ",
	"role.operator":         "ಆಪರೇಟರ್",
	"role.park_head":        "ಪಾರ್ಕ್ ಮುಖ್ಯಸ್ಥ",
	"role.pc_director":      "ನಿರ್ದೇಶಕ",
	"role.verifier":         "ಪರಿಶೀಲಕ",
	"role.supervisor":       "ಮೇಲ್ವಿಚಾರಕ",
	"role.admin":            "ಆಡ್ಮಿನ್",
	"role.other":            "ಸಿಬ್ಬಂದಿ",
	"grade.cxo":             "ಸಿಎಕ್ಸ್‌ಒ",
	"grade.director":        "ನಿರ್ದೇಶಕ",
	"grade.manager":         "ಮ್ಯಾನೇಜರ್",
	"grade.assistant_manager": "ಸಹಾಯಕ ಮ್ಯಾನೇಜರ್",
}

var clockCopyTE = map[string]string{
	"module.title":          "హాజరు (క్లాక్ ఇన్/అవుట్)",
	"banner.not_clocked_in": "ఈరోజు క్లాక్ ఇన్ కాలేదు — క్లాక్ ఇన్ చేయడానికి నొక్కండి",
	"action.clock_in":       "క్లాక్ ఇన్",
	"action.clock_out":      "క్లాక్ అవుట్",
	"state.not_clocked_in":  "ఇంకా క్లాక్ ఇన్ కాలేదు",
	"state.clocked_in":      "%s కి క్లాక్ ఇన్",
	"state.clocked_out":     "ఈరోజు పూర్తయింది",
	"section.working":       "ఇప్పుడు పనిలో",
	"section.worked":        "పని చేశారు",
	"section.clocked_out":   "క్లాక్ అవుట్",
	"section.not_clocked_in": "క్లాక్ ఇన్ లేదు",
	"summary.working":       "ఇప్పుడు పనిలో",
	"summary.worked":        "పని చేశారు",
	"summary.clocked_out":   "క్లాక్ అవుట్",
	"summary.not_clocked_in": "క్లాక్ ఇన్ లేదు",
	"summary.flagged":       "గమనించండి",
	"row.working":           "ఇన్ %s · %s",
	"row.closed":            "%s – %s · %s",
	"row.open_past":         "ఇన్ %s · క్లాక్ అవుట్ లేదు",
	"hours.so_far":          "ఇప్పటివరకు %s",
	"flag.offline":          "ఆఫ్‌లైన్ నమోదు",
	"flag.no_location":      "లొకేషన్ లేదు",
	"flag.not_clocked_out":  "క్లాక్ అవుట్ లేదు",
	"refusal.mock":          "ఈ ఫోన్‌లో లొకేషన్ మార్చే యాప్ ఉంది. దాన్ని తీసివేసి, తర్వాత క్లాక్ ఇన్ చేయండి.",
	"refusal.mock_named":    "క్లాక్ ఇన్ చేయడానికి %s తీసివేయండి — అది ఫోన్ లొకేషన్ మారుస్తుంది.",
	"refusal.already_in":    "ఈరోజు మీరు ఇప్పటికే క్లాక్ ఇన్ చేశారు.",
	"refusal.not_in":        "ముందుగా క్లాక్ ఇన్ చేయండి — ఈరోజు క్లాక్ ఇన్ లేదు.",
	"refusal.already_out":   "ఈరోజు మీరు ఇప్పటికే క్లాక్ అవుట్ చేశారు.",
	"search.placeholder":    "వ్యక్తులను వెతకండి",
	"filter.park":           "పార్క్",
	"filter.designation":    "హోదా",
	"filter.all":            "అన్నీ",
	"empty.presence":        "ఈ ఫిల్టర్‌కు ఎవరూ లేరు.",
	"empty.recent":          "ఇంకా ఏ రోజూ నమోదు కాలేదు.",
	"recent.title":          "ఇటీవలి రోజులు",
	"team.title":            "బృందం",
	"check_again":           "మళ్లీ తనిఖీ చేయండి",
	"role.operator":         "ఆపరేటర్",
	"role.park_head":        "పార్క్ హెడ్",
	"role.pc_director":      "డైరెక్టర్",
	"role.verifier":         "వెరిఫయర్",
	"role.supervisor":       "సూపర్‌వైజర్",
	"role.admin":            "అడ్మిన్",
	"role.other":            "సిబ్బంది",
	"grade.cxo":             "సీఎక్స్ఓ",
	"grade.director":        "డైరెక్టర్",
	"grade.manager":         "మేనేజర్",
	"grade.assistant_manager": "అసిస్టెంట్ మేనేజర్",
}
