package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// DiagnosisService is the port this handler drives.
type DiagnosisService interface {
	SubmitObservation(context.Context, domain.SubmitObservationInput) (domain.SubmitObservationResult, error)
	ConfirmDiagnosis(context.Context, domain.ConfirmDiagnosisInput) (domain.ConfirmDiagnosisResult, error)
	GetDiagnosisRun(context.Context, string, string) (domain.DiagnosisRun, error)
	ListDiagnosisRuns(context.Context, domain.DiagnosisQueueFilter) (domain.DiagnosisQueuePage, error)
}

type DiagnosisHandler struct {
	svc DiagnosisService
	log *slog.Logger
}

func NewDiagnosisHandler(svc DiagnosisService, log *slog.Logger) *DiagnosisHandler {
	if log == nil {
		log = slog.Default()
	}
	return &DiagnosisHandler{svc: svc, log: log}
}

// RegisterDiagnosis wires the three-step loop.
//
// The split across two routes IS the advisory boundary. Submitting an
// observation proposes; it opens nothing. Only the confirm route opens a course,
// and it carries a different permission so the two cannot be collapsed by a
// client calling one endpoint.
func RegisterDiagnosis(mux *http.ServeMux, h *DiagnosisHandler) {
	mux.HandleFunc("POST /app/health/observations", h.SubmitObservation)
	mux.HandleFunc("GET /app/health/observations", h.ListDiagnosisRuns)
	mux.HandleFunc("GET /app/health/observations/{health_diagnosis_run_id}", h.GetDiagnosisRun)
	mux.HandleFunc("POST /app/health/observations/{health_diagnosis_run_id}/confirm", h.ConfirmDiagnosis)
}

// submitObservationRequest is the completed form.
//
// The animal is named by id ONLY. Its species, sex, age band and status are read
// from GoatOS server-side, because the engine gates whole diagnoses on them and a
// client able to assert them could steer the result.
type submitObservationRequest struct {
	GoatID   string                    `json:"goat_id"`
	Findings diagnosis.Findings        `json:"findings"`
	Context  observationContextRequest `json:"context"`
}

// observationContextRequest is the follow-up state a CLIENT may supply.
//
// It deliberately mirrors diagnosis.Context minus `open`. The animal's open
// problems are resolved server-side from its active courses, and a client able
// to assert them could suppress a reconcile -- making a follow-up form read as a
// fresh diagnosis and duplicating every course under way.
//
// This exists as a separate type rather than reusing diagnosis.Context because
// the engine's type MUST keep `open` decodable: the acceptance catalog supplies
// it directly in 22 of its 180 stories. Sharing one type would force a choice
// between a testable engine and a safe wire contract.
type observationContextRequest struct {
	ClosedRecent []string `json:"closed_recent"`
	Day          *int     `json:"day"`

	CMTNegStreak   int  `json:"cmt_neg_streak"`
	NADPrior7d     int  `json:"nad_prior_7d"`
	ShiftedOutDays int  `json:"shifted_out_days"`
	ShedSimilar    *int `json:"shed_similar"`
	DownFollowups  int  `json:"down_followups"`
	Hour           *int `json:"hour"`

	PriorImproved    bool `json:"prior_improved"`
	ProblemImproving bool `json:"problem_improving"`
	AnimalWorsening  bool `json:"animal_worsening"`
	HeatConfirmed    bool `json:"heat_confirmed"`
	Died             bool `json:"died"`
	OffRegister      bool `json:"off_register"`
}

// toDomain converts the client-supplied context. Open is left zero on purpose;
// the repository fills it from the animal's active courses.
func (c observationContextRequest) toDomain() diagnosis.Context {
	return diagnosis.Context{
		ClosedRecent:     c.ClosedRecent,
		Day:              c.Day,
		CMTNegStreak:     c.CMTNegStreak,
		NADPrior7d:       c.NADPrior7d,
		ShiftedOutDays:   c.ShiftedOutDays,
		ShedSimilar:      c.ShedSimilar,
		DownFollowups:    c.DownFollowups,
		Hour:             c.Hour,
		PriorImproved:    c.PriorImproved,
		ProblemImproving: c.ProblemImproving,
		AnimalWorsening:  c.AnimalWorsening,
		HeatConfirmed:    c.HeatConfirmed,
		Died:             c.Died,
		OffRegister:      c.OffRegister,
	}
}

type confirmDiagnosisRequest struct {
	// ConfirmedProblems is a subset of what the engine proposed. An empty list
	// is a legitimate override: the Director rejected the whole proposal.
	ConfirmedProblems []string `json:"confirmed_problems"`
}

func (h *DiagnosisHandler) SubmitObservation(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeStrict[submitObservationRequest](w, r, h.base())
	if !ok {
		return
	}
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" {
		h.writeError(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key is required", nil)
		return
	}

	res, err := h.svc.SubmitObservation(r.Context(), domain.SubmitObservationInput{
		TenantID:           httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:            httpmiddleware.ActorIDFromContext(r.Context()),
		GoatID:             req.GoatID,
		Findings:           req.Findings,
		Context:            req.Context.toDomain(),
		IdempotencyKey:     idem,
		RequestFingerprint: fingerprint(body),
		TraceID:            httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeDiagnosisError(w, r, err)
		return
	}
	res.MayConfirm = callerMayConfirm(r)
	status := http.StatusCreated
	if res.IdempotentReplay {
		status = http.StatusOK
	}
	httpresponse.WriteJSON(w, status, res)
}

func (h *DiagnosisHandler) ConfirmDiagnosis(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeStrict[confirmDiagnosisRequest](w, r, h.base())
	if !ok {
		return
	}
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" {
		h.writeError(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key is required", nil)
		return
	}

	res, err := h.svc.ConfirmDiagnosis(r.Context(), domain.ConfirmDiagnosisInput{
		TenantID:           httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:            httpmiddleware.ActorIDFromContext(r.Context()),
		DiagnosisRunID:     r.PathValue("health_diagnosis_run_id"),
		ConfirmedProblems:  req.ConfirmedProblems,
		IdempotencyKey:     idem,
		RequestFingerprint: fingerprint(body),
		TraceID:            httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeDiagnosisError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, res)
}

func (h *DiagnosisHandler) GetDiagnosisRun(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.GetDiagnosisRun(r.Context(),
		httpmiddleware.TenantIDFromContext(r.Context()), r.PathValue("health_diagnosis_run_id"))
	if err != nil {
		h.writeDiagnosisError(w, r, err)
		return
	}
	// Resolved from the caller's own grants, not from the run: the manager who
	// recorded the observation gets the same confirmable list and must not be
	// offered the decision on it.
	res.MayConfirm = callerMayConfirm(r)
	httpresponse.WriteJSON(w, http.StatusOK, res)
}

// callerMayConfirm reports whether this request's principal holds the confirm
// authority. Advisory for the UI only -- POST .../confirm is gated at the route,
// which is the actual enforcement.
func callerMayConfirm(r *http.Request) bool {
	for _, grant := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		if permissions.RoleHasPermission(grant.Role, permissions.HealthDiagnose) {
			return true
		}
	}
	return false
}

// writeDiagnosisError maps each failure onto a status the client can act on.
//
// The one worth reading twice is ErrSOPNotAuthored -> 422. It is not a client
// mistake and not a server fault: the Director confirmed a real diagnosis whose
// treatment card nobody has written yet. 422 with a named code lets the app say
// which card is missing instead of showing a generic failure.
func (h *DiagnosisHandler) writeDiagnosisError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, healthapp.ErrInvalidInput):
		h.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), err)
	case errors.Is(err, domain.ErrGoatNotDiagnosable):
		h.writeError(w, r, http.StatusUnprocessableEntity, "goat_not_diagnosable", err.Error(), err)
	case errors.Is(err, ports.ErrNotFound):
		h.writeError(w, r, http.StatusNotFound, "not_found", err.Error(), err)
	case errors.Is(err, ports.ErrGoatNotAlive):
		h.writeError(w, r, http.StatusConflict, "goat_not_alive", err.Error(), err)
	case errors.Is(err, ports.ErrSOPNotAuthored):
		h.writeError(w, r, http.StatusUnprocessableEntity, "treatment_plan_missing", err.Error(), err)
	case errors.Is(err, ports.ErrDiagnosisNotProposed):
		h.writeError(w, r, http.StatusUnprocessableEntity, "diagnosis_not_proposed", err.Error(), err)
	case errors.Is(err, ports.ErrDiagnosisNotConfirmable):
		h.writeError(w, r, http.StatusUnprocessableEntity, "diagnosis_not_confirmable", err.Error(), err)
	case errors.Is(err, ports.ErrDiagnosisAlreadyDecided):
		h.writeError(w, r, http.StatusConflict, "diagnosis_already_decided", err.Error(), err)
	case errors.Is(err, ports.ErrConflict):
		h.writeError(w, r, http.StatusConflict, "idempotency_conflict", err.Error(), err)
	default:
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal error", err)
	}
}

// base adapts this handler to the shared strict decoder, which reports its own
// errors through a *Handler.
func (h *DiagnosisHandler) base() *Handler { return &Handler{log: h.log} }

func (h *DiagnosisHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	httpresponse.WriteError(w, r, h.log, status, errorResponse{
		Code: code, Message: message,
		TraceID: httpmiddleware.TraceIDFromContext(r.Context()), Retryable: status >= 500,
	}, cause)
}

// ListDiagnosisRuns serves the Director's queue.
//
// Gated on health.read, NOT health.diagnose, and the split is deliberate: a
// health manager may see that what they recorded is still waiting, which is a
// real thing to want to know. Only the per-page MayConfirm says who can act, and
// only the confirm ROUTE enforces it.
func (h *DiagnosisHandler) ListDiagnosisRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_request", "limit must be a number", nil)
			return
		}
		limit = parsed
	}

	page, err := h.svc.ListDiagnosisRuns(r.Context(), domain.DiagnosisQueueFilter{
		TenantID: httpmiddleware.TenantIDFromContext(r.Context()),
		Status:   q.Get("status"),
		GoatID:   strings.TrimSpace(q.Get("goat_id")),
		Cursor:   strings.TrimSpace(q.Get("cursor")),
		Limit:    limit,
	})
	if err != nil {
		h.writeDiagnosisError(w, r, err)
		return
	}
	page.MayConfirm = callerMayConfirm(r)
	httpresponse.WriteJSON(w, http.StatusOK, page)
}
