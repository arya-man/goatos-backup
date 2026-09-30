package workforcehttp

import (
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TimetableHandler serves People / HRMS > Timetable (maintainer request 2026-09-30). The read is
// workforce.timetable.read; both writes are workforce.timetable.write (HR and the CEO/CXO),
// enforced by the route table in permissions/routes.go.
type TimetableHandler struct {
	service *app.TimetableService
	log     *slog.Logger
}

func NewTimetableHandler(service *app.TimetableService, log *slog.Logger) *TimetableHandler {
	if log == nil {
		log = slog.Default()
	}
	return &TimetableHandler{service: service, log: log}
}

func RegisterTimetable(mux *http.ServeMux, h *TimetableHandler) {
	mux.HandleFunc("GET /admin/workforce/timetable", h.Timetable)
	mux.HandleFunc("PUT /admin/workforce/timetable/parks/{park_id}/shifts/{shift_code}", h.SetShiftTiming)
	mux.HandleFunc("PUT /admin/workforce/timetable/people/{person_id}/shift", h.SetMemberShift)
	// Weekly offs and holidays (2026-09-30): the days the clock-in check never checks.
	mux.HandleFunc("PUT /admin/workforce/timetable/people/{person_id}/week-offs", h.SetWeekOffs)
	mux.HandleFunc("POST /admin/workforce/holidays", h.AddHoliday)
	mux.HandleFunc("POST /admin/workforce/holidays/{holiday_id}/remove", h.RemoveHoliday)
}

func (h *TimetableHandler) Timetable(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.Timetable(r.Context(), tenantID(r), q.Get("park_id"), q.Get("shift"), q.Get("cursor"),
		parseLimit(q.Get("limit")), traceID(r))
	writeServiceResponse(w, r, h.log, result, err)
}

func (h *TimetableHandler) SetShiftTiming(w http.ResponseWriter, r *http.Request) {
	var body domain.ShiftTimingUpdate
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.SetShiftTiming(r.Context(), tenantID(r), actorID(r), r.PathValue("park_id"), r.PathValue("shift_code"), body, traceID(r))
	if err != nil {
		h.log.WarnContext(r.Context(), "timetable_shift_timing_failed",
			slog.String("trace_id", traceID(r)), slog.String("actor_id", actorID(r)),
			slog.String("park_id", r.PathValue("park_id")), slog.String("shift_code", r.PathValue("shift_code")),
			slog.String("error", err.Error()))
	}
	writeServiceResponse(w, r, h.log, result, err)
}

func (h *TimetableHandler) SetWeekOffs(w http.ResponseWriter, r *http.Request) {
	var body domain.WeekOffsUpdate
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.SetWeekOffs(r.Context(), tenantID(r), actorID(r), r.PathValue("person_id"), body, traceID(r))
	if err != nil {
		h.log.WarnContext(r.Context(), "timetable_week_offs_failed", slog.String("trace_id", traceID(r)), slog.String("error", err.Error()))
	}
	writeServiceResponse(w, r, h.log, result, err)
}

func (h *TimetableHandler) AddHoliday(w http.ResponseWriter, r *http.Request) {
	var body domain.AddHolidayRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.AddHoliday(r.Context(), tenantID(r), actorID(r), body, traceID(r))
	if err != nil {
		h.log.WarnContext(r.Context(), "timetable_holiday_add_failed", slog.String("trace_id", traceID(r)), slog.String("error", err.Error()))
	}
	writeServiceResponse(w, r, h.log, result, err)
}

func (h *TimetableHandler) RemoveHoliday(w http.ResponseWriter, r *http.Request) {
	err := h.service.RemoveHoliday(r.Context(), tenantID(r), actorID(r), r.PathValue("holiday_id"))
	if err != nil {
		h.log.WarnContext(r.Context(), "timetable_holiday_remove_failed", slog.String("trace_id", traceID(r)), slog.String("error", err.Error()))
		writeServiceResponse(w, r, h.log, nil, err)
		return
	}
	writeServiceResponse(w, r, h.log, map[string]string{"trace_id": traceID(r)}, nil)
}

func (h *TimetableHandler) SetMemberShift(w http.ResponseWriter, r *http.Request) {
	var body domain.MemberShiftUpdate
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.SetMemberShift(r.Context(), tenantID(r), actorID(r), r.PathValue("person_id"), body, traceID(r))
	if err != nil {
		h.log.WarnContext(r.Context(), "timetable_member_shift_failed",
			slog.String("trace_id", traceID(r)), slog.String("actor_id", actorID(r)),
			slog.String("person_id", r.PathValue("person_id")), slog.String("error", err.Error()))
	}
	writeServiceResponse(w, r, h.log, result, err)
}
