package identityhttp

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type Handler struct {
	service *app.Service
	log     *slog.Logger
}

// NewHandler creates a Handler. If log is nil, slog.Default() is used.
func NewHandler(service *app.Service, log ...*slog.Logger) *Handler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Handler{service: service, log: l}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /goats/search", h.SearchGoats)
	mux.HandleFunc("GET /goats/{goat_id}", h.GetGoatPassport)
	mux.HandleFunc("GET /goats/{goat_id}/timeline", h.GetGoatTimeline)
	mux.HandleFunc("GET /identifiers/{type}/{value}/resolve", h.ResolveIdentifier)
	mux.HandleFunc("GET /identity/correction-requests", h.ListCorrectionRequests)
	mux.HandleFunc("POST /identity/correction-requests", h.CreateCorrectionRequest)

	mux.HandleFunc("GET /admin/identity/review-summary", h.ReviewSummary)
	mux.HandleFunc("GET /admin/identity/conflicts", h.ListConflicts)
	mux.HandleFunc("GET /admin/identity/conflicts/{conflict_id}", h.GetConflict)
	mux.HandleFunc("POST /admin/identity/conflicts/{conflict_id}/resolve", h.ResolveConflict)
	mux.HandleFunc("GET /admin/import-runs", h.ListImportRuns)
	mux.HandleFunc("GET /admin/import-runs/{import_run_id}", h.GetImportRun)
	mux.HandleFunc("GET /admin/import-runs/{import_run_id}/rows", h.ListImportRunRows)
	mux.HandleFunc("GET /admin/import-runs/{import_run_id}/rows.csv", h.ExportImportRunRowsCSV)
	mux.HandleFunc("POST /admin/import-runs", h.NotImplemented("legacy_import_deferred"))
	mux.HandleFunc("GET /admin/identity/candidates", h.ListCandidates)
	mux.HandleFunc("POST /admin/identity/candidates/{candidate_id}/approve", h.ApproveCandidate)
	mux.HandleFunc("POST /admin/identity/candidates/{candidate_id}/reject", h.RejectCandidate)
	mux.HandleFunc("POST /admin/goats", h.NotImplemented("admin_goat_writes_deferred"))
	mux.HandleFunc("PATCH /admin/goats/{goat_id}", h.NotImplemented("admin_goat_writes_deferred"))
	mux.HandleFunc("POST /admin/goats/{goat_id}/identifiers", h.AddGoatIdentifier)
	mux.HandleFunc("POST /admin/goats/{goat_id}/identifiers/{identifier_id}/retire", h.RetireGoatIdentifier)
	mux.HandleFunc("GET /admin/identity/correction-requests", h.AdminListCorrectionRequests)
	mux.HandleFunc("POST /admin/identity/correction-requests/{correction_request_id}/resolve", h.ResolveCorrectionRequest)
}

func (h *Handler) GetGoatPassport(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetGoatPassport(r.Context(), tenantID(r), r.PathValue("goat_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) SearchGoats(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	params := ports.SearchGoatsParams{
		TenantID:       tenantID(r),
		Limit:          limit,
		Cursor:         optionalQuery(q.Get("cursor")),
		Query:          optionalQuery(q.Get("q")),
		GoatID:         optionalQuery(q.Get("goat_id")),
		IdentifierType: optionalQuery(q.Get("identifier_type")),
		ScopeKey:       optionalQuery(q.Get("scope_key")),
		Breed:          optionalQuery(q.Get("breed")),
		Sex:            optionalQuery(q.Get("sex")),
		FarmID:         optionalQuery(q.Get("farm_id")),
		ParkID:         optionalQuery(q.Get("park_id")),
		LocationID:     optionalQuery(q.Get("location_id")),
		Status:         optionalQuery(q.Get("status")),
	}
	result, err := h.service.SearchGoats(r.Context(), params, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ResolveIdentifier(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := ports.ResolveIdentifierParams{
		TenantID:        tenantID(r),
		IdentifierType:  r.PathValue("type"),
		NormalizedValue: r.PathValue("value"),
		ScopeKey:        optionalQuery(q.Get("scope_key")),
		FarmID:          optionalQuery(q.Get("farm_id")),
		ParkID:          optionalQuery(q.Get("park_id")),
		LocationID:      optionalQuery(q.Get("location_id")),
	}
	result, err := h.service.ResolveIdentifier(r.Context(), params, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) GetGoatTimeline(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	result, err := h.service.GetGoatTimeline(r.Context(), ports.GetGoatTimelineParams{
		TenantID: tenantID(r),
		GoatID:   r.PathValue("goat_id"),
		Limit:    limit,
		Cursor:   optionalQuery(q.Get("cursor")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ReviewSummary(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ReviewSummary(r.Context(), tenantID(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ListConflicts(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	params := ports.ListConflictsParams{
		TenantID:     tenantID(r),
		Limit:        limit,
		Cursor:       optionalQuery(q.Get("cursor")),
		State:        optionalQuery(q.Get("state")),
		ConflictType: optionalQuery(q.Get("conflict_type")),
	}
	result, err := h.service.ListConflicts(r.Context(), params, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) GetConflict(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetConflict(r.Context(), tenantID(r), r.PathValue("conflict_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ListCandidates(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	result, err := h.service.ListCandidates(r.Context(), ports.ListCandidatesParams{
		TenantID: tenantID(r),
		Limit:    limit,
		Cursor:   optionalQuery(q.Get("cursor")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) GetImportRun(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetImportRun(r.Context(), tenantID(r), r.PathValue("import_run_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ListImportRuns(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimitWithMax(w, r, 50)
	if !ok {
		return
	}
	result, err := h.service.ListImportRuns(r.Context(), ports.ListImportRunsParams{
		TenantID: tenantID(r),
		Limit:    limit,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ListImportRunRows(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimitWithMax(w, r, 500)
	if !ok {
		return
	}
	q := r.URL.Query()
	result, err := h.service.ListImportRunRows(r.Context(), ports.ListImportRunRowsParams{
		TenantID:        tenantID(r),
		ImportRunID:     r.PathValue("import_run_id"),
		Limit:           limit,
		Cursor:          optionalQuery(q.Get("cursor")),
		ProcessingState: optionalQuery(q.Get("processing_state")),
		ReasonCode:      optionalQuery(q.Get("reason_code")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ListCorrectionRequests(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	actor := actorID(r)
	q := r.URL.Query()
	result, err := h.service.ListCorrectionRequests(r.Context(), ports.ListCorrectionRequestsParams{
		TenantID:  tenantID(r),
		Limit:     limit,
		Cursor:    optionalQuery(q.Get("cursor")),
		CreatedBy: &actor,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) AdminListCorrectionRequests(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	result, err := h.service.ListCorrectionRequests(r.Context(), ports.ListCorrectionRequestsParams{
		TenantID: tenantID(r),
		Limit:    limit,
		Cursor:   optionalQuery(q.Get("cursor")),
		State:    optionalQuery(q.Get("state")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ResolveConflict(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_json",
			Message:     "request body is too large or unreadable",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	result, err := h.service.ResolveConflict(r.Context(), app.ResolveConflictInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		ConflictID:     r.PathValue("conflict_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) ApproveCandidate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_json",
			Message:     "request body is too large or unreadable",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	result, err := h.service.ApproveCandidate(r.Context(), app.ReviewCandidateInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		CandidateID:    r.PathValue("candidate_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) RejectCandidate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_json",
			Message:     "request body is too large or unreadable",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	result, err := h.service.RejectCandidate(r.Context(), app.ReviewCandidateInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		CandidateID:    r.PathValue("candidate_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) CreateCorrectionRequest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_json",
			Message:     "request body is too large or unreadable",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	result, err := h.service.CreateCorrectionRequest(r.Context(), app.CreateCorrectionRequestInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		RawBody:        body,
	})
	if err != nil {
		h.respond(w, r, nil, err)
		return
	}
	status := http.StatusCreated
	if result.Idempotency.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (h *Handler) ResolveCorrectionRequest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_json",
			Message:     "request body is too large or unreadable",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	result, err := h.service.ResolveCorrectionRequest(r.Context(), app.ResolveCorrectionRequestInput{
		TenantID:            tenantID(r),
		ActorID:             actorID(r),
		IdempotencyKey:      r.Header.Get("Idempotency-Key"),
		TraceID:             traceID(r),
		CorrectionRequestID: r.PathValue("correction_request_id"),
		RawBody:             body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) AddGoatIdentifier(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_json",
			Message:     "request body is too large or unreadable",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	result, err := h.service.AddGoatIdentifier(r.Context(), app.AddGoatIdentifierInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		GoatID:         r.PathValue("goat_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) RetireGoatIdentifier(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_json",
			Message:     "request body is too large or unreadable",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	result, err := h.service.RetireGoatIdentifier(r.Context(), app.RetireGoatIdentifierInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		GoatID:         r.PathValue("goat_id"),
		IdentifierID:   r.PathValue("identifier_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) NotImplemented(code string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, domain.ErrorEnvelope{
			Code:        code,
			Message:     "Endpoint is intentionally deferred in this Phase 1 backend foundation slice.",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
	}
}

func parseLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	return parseLimitWithMax(w, r, 100)
}

func parseLimitWithMax(w http.ResponseWriter, r *http.Request, maxLimit int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "missing_limit",
			Message:     "limit query parameter is required",
			FieldErrors: []domain.FieldError{{Field: "limit", Code: "required", Message: "limit is required"}},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxLimit {
		message := "limit must be between 1 and " + strconv.Itoa(maxLimit)
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_limit",
			Message:     "limit must be an integer between 1 and " + strconv.Itoa(maxLimit),
			FieldErrors: []domain.FieldError{{Field: "limit", Code: "invalid", Message: message}},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return 0, false
	}
	return limit, true
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		envelope := domain.ErrorEnvelope{
			Code:        "internal_error",
			Message:     "internal server error",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   true,
		}
		var appErr *app.Error
		if errors.As(err, &appErr) {
			status = appErr.HTTPStatus
			envelope.Code = appErr.Code
			envelope.Message = appErr.Message
			envelope.Retryable = appErr.Retryable
		}
		writeHandlerError(w, r, h.log, status, envelope, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func writeHandlerError(w http.ResponseWriter, r *http.Request, log *slog.Logger, status int, envelope domain.ErrorEnvelope, cause error) {
	if envelope.FieldErrors == nil {
		envelope.FieldErrors = []domain.FieldError{}
	}
	httpresponse.WriteError(w, r, log, status, envelope, cause)
}

func writeError(w http.ResponseWriter, status int, envelope domain.ErrorEnvelope) {
	if envelope.FieldErrors == nil {
		envelope.FieldErrors = []domain.FieldError{}
	}
	writeJSON(w, status, envelope)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	httpresponse.WriteJSON(w, status, payload)
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *http.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	traceID := httpmiddleware.TraceIDFromContext(r.Context())
	if traceID == "" {
		return "missing-trace"
	}
	return traceID
}

func optionalQuery(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
