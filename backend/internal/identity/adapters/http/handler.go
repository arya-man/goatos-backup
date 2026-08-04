package identityhttp

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
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

	mux.HandleFunc("POST /admin/goats", h.CreateAdminGoat)
	mux.HandleFunc("POST /admin/goats/bulk-preview", h.PreviewAdminGoatBulkImport)
	mux.HandleFunc("POST /admin/goats/bulk-commit", h.CommitAdminGoatBulkImport)
	mux.HandleFunc("POST /admin/goats/{goat_id}/move", h.MoveGoat)
	mux.HandleFunc("POST /admin/goats/{goat_id}/exit", h.ExitGoat)
	mux.HandleFunc("POST /admin/goats/{goat_id}/critical-death-exit", h.CriticalDeathExit)
	mux.HandleFunc("POST /admin/goats/{goat_id}/stage", h.StageGoat)
	mux.HandleFunc("POST /admin/goats/{goat_id}/health", h.HealthGoat)
	mux.HandleFunc("POST /admin/goats/{goat_id}/reproductive", h.ReproductiveGoat)
	mux.HandleFunc("POST /admin/goats/{goat_id}/identity", h.IdentityGoat)
	mux.HandleFunc("POST /admin/goats/{goat_id}/identifiers", h.AddGoatIdentifier)
	mux.HandleFunc("POST /admin/goats/{goat_id}/identifiers/{identifier_id}/retire", h.RetireGoatIdentifier)
}

func (h *Handler) GetGoatPassport(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetGoatPassport(r.Context(), tenantID(r), r.PathValue("goat_id"), traceID(r))
	h.respond(w, r, result, err)
}

// searchGoatsAllowedParams is the exact query-parameter vocabulary of GET /goats/search, and it
// must stay in sync with both the params read below and contracts/openapi/app-api.yaml.
//
// Anything outside this set is REJECTED rather than ignored. Silently dropping an unknown parameter
// is the dangerous failure here: a caller that sends ?query=... instead of ?q=... (or keeps sending
// a filter that was later renamed) gets an unfiltered page of the herd returned to them as if it
// were a search result. Failing loudly turns a silent wrong-data answer into an obvious 400.
var searchGoatsAllowedParams = map[string]bool{
	"limit": true, "cursor": true, "q": true, "goat_id": true,
	"identifier_type": true, "scope_key": true, "breed": true, "sex": true,
	"farm_id": true, "park_id": true, "location_id": true, "status": true,
}

func (h *Handler) SearchGoats(w http.ResponseWriter, r *http.Request) {
	// Checked BEFORE parseLimit so a request carrying a typo'd filter reports that typo, rather
	// than a missing_limit error that sends the caller looking in the wrong place.
	if !rejectUnknownQueryParams(w, r, searchGoatsAllowedParams) {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenantID(r), strings.TrimSpace(q.Get("park_id")), permissions.GoatRead,
	)
	if !parkScope.Allowed {
		writeHandlerError(w, r, h.log, parkScope.Status, domain.ErrorEnvelope{
			Code:        parkScope.Code,
			Message:     parkScope.Message,
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		}, nil)
		return
	}
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
		ParkID:         optionalQuery(parkScope.ParkID),
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

func (h *Handler) CreateAdminGoat(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	result, err := h.service.CreateAdminGoat(r.Context(), app.CreateAdminGoatInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) PreviewAdminGoatBulkImport(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 5<<20)
	if !ok {
		return
	}
	result, err := h.service.PreviewAdminGoatBulkImport(r.Context(), app.PreviewAdminGoatBulkInput{
		TenantID: tenantID(r),
		TraceID:  traceID(r),
		RawBody:  body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) CommitAdminGoatBulkImport(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 5<<20)
	if !ok {
		return
	}
	result, err := h.service.CommitAdminGoatBulkImport(r.Context(), app.CommitAdminGoatBulkInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) MoveGoat(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	result, err := h.service.MoveGoat(r.Context(), app.MoveGoatInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		GoatID:         r.PathValue("goat_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) ExitGoat(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	result, err := h.service.ExitGoat(r.Context(), app.ExitGoatInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		GoatID:         r.PathValue("goat_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) CriticalDeathExit(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	result, err := h.service.CriticalDeathExit(r.Context(), app.ExitGoatInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		GoatID:         r.PathValue("goat_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) StageGoat(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	result, err := h.service.StageGoat(r.Context(), app.StageGoatInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		GoatID:         r.PathValue("goat_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) HealthGoat(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	result, err := h.service.HealthGoat(r.Context(), app.HealthGoatInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		GoatID:         r.PathValue("goat_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) ReproductiveGoat(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	result, err := h.service.ReproductiveGoat(r.Context(), app.ReproductiveGoatInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		GoatID:         r.PathValue("goat_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) IdentityGoat(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	result, err := h.service.IdentityGoat(r.Context(), app.IdentityGoatInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		GoatID:         r.PathValue("goat_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) AddGoatIdentifier(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
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
	body, ok := readBody(w, r, 1<<20)
	if !ok {
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

func readBody(w http.ResponseWriter, r *http.Request, maxBytes int64) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_json",
			Message:     "request body is too large or unreadable",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return nil, false
	}
	return body, true
}

// rejectUnknownQueryParams fails the request with 400 unknown_query_parameter when the URL carries
// any query parameter outside allowed. It reports false when it has already written the response.
//
// Unknown names are reported sorted so the error is deterministic for a caller sending several, and
// every offending name is listed as its own field error rather than only the first: a client fixing
// a hand-built query string should need one round trip, not one per typo.
func rejectUnknownQueryParams(w http.ResponseWriter, r *http.Request, allowed map[string]bool) bool {
	var unknown []string
	for name := range r.URL.Query() {
		if !allowed[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return true
	}
	sort.Strings(unknown)

	fieldErrors := make([]domain.FieldError, 0, len(unknown))
	for _, name := range unknown {
		fieldErrors = append(fieldErrors, domain.FieldError{
			Field:   name,
			Code:    "unknown",
			Message: "unknown query parameter " + name,
		})
	}
	writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
		Code:        "unknown_query_parameter",
		Message:     "unknown query parameter(s): " + strings.Join(unknown, ", "),
		FieldErrors: fieldErrors,
		TraceID:     traceID(r),
		Retryable:   false,
	})
	return false
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
