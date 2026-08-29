package workforcehttp

import (
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// PeopleHandler serves the People/HRMS directory: the cross-park member list
// and the create-person onboarding write.
type PeopleHandler struct {
	service *app.PeopleService
	log     *slog.Logger
}

func NewPeopleHandler(service *app.PeopleService, log ...*slog.Logger) *PeopleHandler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &PeopleHandler{service: service, log: l}
}

func RegisterPeople(mux *http.ServeMux, h *PeopleHandler) {
	mux.HandleFunc("GET /admin/workforce/people", h.ListPeople)
	mux.HandleFunc("POST /admin/workforce/people", h.CreatePerson)
}

func (h *PeopleHandler) ListPeople(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.ListPeople(r.Context(), ports.ListPeopleParams{
		TenantID:     tenantID(r),
		ParkID:       q.Get("park_id"),
		DepartmentID: q.Get("department_id"),
		Status:       q.Get("status"),
		Search:       q.Get("q"),
		Limit:        parseLimit(q.Get("limit")),
		Cursor:       q.Get("cursor"),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *PeopleHandler) CreatePerson(w http.ResponseWriter, r *http.Request) {
	var body domain.CreatePersonRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.CreatePerson(r.Context(), tenantID(r), actorID(r), idempotencyKeyHeader(r), body, traceID(r))
	if err != nil {
		h.log.WarnContext(r.Context(), "admin_create_person_failed",
			slog.String("trace_id", traceID(r)),
			slog.String("actor_id", actorID(r)),
			slog.String("tenant_id", tenantID(r)),
			slog.String("error", err.Error()),
		)
	} else if result != nil {
		h.log.InfoContext(r.Context(), "admin_create_person_succeeded",
			slog.String("trace_id", traceID(r)),
			slog.String("actor_id", actorID(r)),
			slog.String("tenant_id", tenantID(r)),
			slog.String("person_id", result.Person.PersonID),
			slog.String("account_status", result.Login.AccountStatus),
		)
	}
	h.respond(w, r, result, err)
}

// respond shares the workforce success/error envelope contract.
func (h *PeopleHandler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
	writeServiceResponse(w, r, h.log, payload, err)
}
