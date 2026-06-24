// Package http exposes vaccination execution read APIs. Park/shed is the physical execution context of
// PHC Vaccination work; Parks does not own these routes.
package http

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// Reader is the vaccination execution read slice required by this handler.
type Reader interface {
	VaccinationExecution(ctx context.Context, q domain.ExecutionQuery) ([]domain.ExecutionRow, error)
	ShedDrilldown(ctx context.Context, q domain.ExecutionQuery) (domain.ShedDrilldown, bool, error)
	VaccinationOperations(ctx context.Context, q domain.OperationsQuery) (domain.OperationsResponse, error)
}

// Handler serves vaccination execution endpoints (park/shed execution context for PHC Vaccination).
type Handler struct {
	reader Reader
	log    *slog.Logger
}

// NewHandler constructs the vaccination execution handler.
func NewHandler(reader Reader, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{reader: reader, log: l}
}

// Register mounts the vaccination execution routes (owned by PHC Vaccination, park/shed scope).
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /vaccination/execution", h.ListVaccinationExecution)
	mux.HandleFunc("GET /vaccination/execution/sheds/{shed_id}", h.GetShedDrilldown)
	mux.HandleFunc("GET /vaccination/operations", h.VaccinationOperations)
}

// VaccinationOperations serves the source-backed cohort × protocol matrix + per-cohort detail for the
// /vaccination screen. Park scope (park_id) + as_of + due_before come from the top bar.
func (h *Handler) VaccinationOperations(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	asOf := time.Now().UTC()
	q := domain.OperationsQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Limit:     defaultDrilldownLimit,
	}
	if parkID := query.Get("park_id"); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
			return
		}
		q.ParkID = &parkID
	}
	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := time.Parse(time.RFC3339, asOfRaw)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return
		}
		q.AsOf = parsed.UTC()
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
	}
	if dueBefore := query.Get("due_before"); dueBefore != "" {
		parsed, err := time.Parse(time.RFC3339, dueBefore)
		if err != nil {
			h.badRequest(w, r, "invalid_due_before", "due_before must be RFC3339")
			return
		}
		q.DueBefore = parsed.UTC()
	}
	if limit := query.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > maxExecutionLimit {
			n = maxExecutionLimit
		}
		q.Limit = n
	}
	resp, err := h.reader.VaccinationOperations(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

const (
	defaultExecutionLimit       = 200
	defaultDrilldownLimit       = 500
	maxExecutionLimit           = 500
	defaultExecutionHorizonDays = 30
)

var allowedWorkStates = map[domain.WorkState]bool{
	domain.WorkStateDue:                 true,
	domain.WorkStateOverdue:             true,
	domain.WorkStateScheduled:           true,
	domain.WorkStateInProgress:          true,
	domain.WorkStateProofPending:        true,
	domain.WorkStateVerificationPending: true,
	domain.WorkStateRejected:            true,
	domain.WorkStateDeferred:            true,
	domain.WorkStateBlocked:             true,
	domain.WorkStateOwnerMissing:        true,
	domain.WorkStateCompleted:           true,
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

// ListVaccinationExecution lists bounded vaccination execution rows grouped by park/shed context.
func (h *Handler) ListVaccinationExecution(w http.ResponseWriter, r *http.Request) {
	q, ok := h.executionQuery(w, r, defaultExecutionLimit)
	if !ok {
		return
	}
	rows, err := h.reader.VaccinationExecution(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, domain.ExecutionResponse{Source: domain.SourceAPI, Rows: rows})
}

// GetShedDrilldown returns the vaccination execution context for a single shed.
func (h *Handler) GetShedDrilldown(w http.ResponseWriter, r *http.Request) {
	shedID := r.PathValue("shed_id")
	if !uuidutil.IsUUIDString(shedID) {
		h.badRequest(w, r, "invalid_shed_id", "shed_id must be a UUID")
		return
	}
	q, ok := h.executionQuery(w, r, defaultDrilldownLimit)
	if !ok {
		return
	}
	q.ShedID = &shedID
	detail, found, err := h.reader.ShedDrilldown(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	if !found {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "shed vaccination execution was not found", TraceID: traceID(r)}, nil)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, detail)
}

func (h *Handler) executionQuery(w http.ResponseWriter, r *http.Request, defaultLimit int) (domain.ExecutionQuery, bool) {
	query := r.URL.Query()
	asOf := time.Now().UTC()
	q := domain.ExecutionQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Limit:     defaultLimit,
	}

	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := time.Parse(time.RFC3339, asOfRaw)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return domain.ExecutionQuery{}, false
		}
		q.AsOf = parsed.UTC()
		// Re-anchor the default horizon to as_of; an explicit due_before below still wins.
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
	}
	if parkID := query.Get("park_id"); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
			return domain.ExecutionQuery{}, false
		}
		q.ParkID = &parkID
	}
	if state := query.Get("work_state"); state != "" {
		workState := domain.WorkState(state)
		if !allowedWorkStates[workState] {
			h.badRequest(w, r, "invalid_work_state", "work_state must be a vaccination execution work state")
			return domain.ExecutionQuery{}, false
		}
		q.WorkState = &workState
	}
	if dueBefore := query.Get("due_before"); dueBefore != "" {
		parsed, err := time.Parse(time.RFC3339, dueBefore)
		if err != nil {
			h.badRequest(w, r, "invalid_due_before", "due_before must be RFC3339")
			return domain.ExecutionQuery{}, false
		}
		q.DueBefore = parsed.UTC()
	}
	if limit := query.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return domain.ExecutionQuery{}, false
		}
		if n > maxExecutionLimit {
			n = maxExecutionLimit
		}
		q.Limit = n
	}
	return q, true
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
		errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, msg string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		errorEnvelope{Code: code, Message: msg, TraceID: traceID(r)}, nil)
}
