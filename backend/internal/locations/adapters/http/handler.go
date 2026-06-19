package http

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/locations/app"
	"github.com/vgoats/goatos/backend/internal/locations/domain"
	"github.com/vgoats/goatos/backend/internal/locations/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type Handler struct {
	service *app.Service
	log     *slog.Logger
}

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
	mux.HandleFunc("GET /admin/locations", h.ListLocations)
	mux.HandleFunc("POST /admin/locations", h.CreateLocation)
	mux.HandleFunc("GET /admin/locations/{location_id}", h.GetLocation)
	mux.HandleFunc("PATCH /admin/locations/{location_id}", h.UpdateLocation)
	mux.HandleFunc("DELETE /admin/locations/{location_id}", h.DeleteLocation)
	mux.HandleFunc("POST /admin/locations/{location_id}/retire", h.RetireLocation)
	mux.HandleFunc("GET /admin/locations/{location_id}/children", h.ListChildren)
	mux.HandleFunc("GET /admin/locations/{location_id}/usage", h.Usage)
	mux.HandleFunc("GET /admin/locations/{location_id}/aliases", h.ListAliases)
	mux.HandleFunc("POST /admin/locations/{location_id}/aliases", h.CreateLocationAlias)
	mux.HandleFunc("PATCH /admin/locations/{location_id}/aliases/{alias_id}", h.UpdateLocationAlias)
	mux.HandleFunc("DELETE /admin/locations/{location_id}/aliases/{alias_id}", h.DeleteLocationAlias)
	mux.HandleFunc("POST /admin/locations/{location_id}/aliases/{alias_id}/retire", h.RetireLocationAlias)
	mux.HandleFunc("GET /admin/locations/{location_id}/capacity", h.ListCapacity)
	mux.HandleFunc("POST /admin/locations/{location_id}/capacity", h.CreateLocationCapacity)
	mux.HandleFunc("PATCH /admin/locations/{location_id}/capacity/{capacity_record_id}", h.UpdateLocationCapacity)
	mux.HandleFunc("DELETE /admin/locations/{location_id}/capacity/{capacity_record_id}", h.DeleteLocationCapacity)
	mux.HandleFunc("GET /admin/location-review-items", h.ListReviewItems)
	mux.HandleFunc("POST /admin/location-review-items", h.CreateLocationReviewItem)
	mux.HandleFunc("POST /admin/location-review-items/{review_id}/resolve", h.ResolveLocationReviewItem)
}

func (h *Handler) ListLocations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.ListLocations(r.Context(), ports.ListParams{
		TenantID:         tenantID(r),
		LocationType:     q.Get("type"),
		Status:           q.Get("status"),
		ParentLocationID: q.Get("parent_location_id"),
		Search:           q.Get("search"),
		Alias:            q.Get("alias"),
		Limit:            parseLimit(q.Get("limit")),
		Offset:           parseOffset(q.Get("offset")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) GetLocation(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetLocation(r.Context(), tenantID(r), r.PathValue("location_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) CreateLocation(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.CreateLocation(r.Context(), app.CreateLocationInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) UpdateLocation(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.UpdateLocation(r.Context(), app.UpdateLocationInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		LocationID:     r.PathValue("location_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) DeleteLocation(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.DeleteLocation(r.Context(), app.DeleteLocationInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		LocationID:     r.PathValue("location_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) RetireLocation(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.RetireLocation(r.Context(), app.RetireLocationInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		LocationID:     r.PathValue("location_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) ListChildren(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListChildren(r.Context(), tenantID(r), r.PathValue("location_id"), parseLimit(r.URL.Query().Get("limit")), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Usage(r.Context(), tenantID(r), r.PathValue("location_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ListAliases(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListAliases(r.Context(), tenantID(r), r.PathValue("location_id"), parseLimit(r.URL.Query().Get("limit")), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) CreateLocationAlias(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.CreateLocationAlias(r.Context(), app.CreateLocationAliasInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		LocationID:     r.PathValue("location_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) UpdateLocationAlias(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.UpdateLocationAlias(r.Context(), app.UpdateLocationAliasInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		LocationID:     r.PathValue("location_id"),
		AliasID:        r.PathValue("alias_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) RetireLocationAlias(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.RetireLocationAlias(r.Context(), app.RetireLocationAliasInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		LocationID:     r.PathValue("location_id"),
		AliasID:        r.PathValue("alias_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) DeleteLocationAlias(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.DeleteLocationAlias(r.Context(), app.DeleteLocationAliasInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		LocationID:     r.PathValue("location_id"),
		AliasID:        r.PathValue("alias_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) ListCapacity(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListCapacity(r.Context(), tenantID(r), r.PathValue("location_id"), parseLimit(r.URL.Query().Get("limit")), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) CreateLocationCapacity(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.CreateLocationCapacity(r.Context(), app.CreateLocationCapacityInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		LocationID:     r.PathValue("location_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) UpdateLocationCapacity(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.UpdateLocationCapacity(r.Context(), app.UpdateLocationCapacityInput{
		TenantID:         tenantID(r),
		ActorID:          actorID(r),
		IdempotencyKey:   r.Header.Get("Idempotency-Key"),
		TraceID:          traceID(r),
		LocationID:       r.PathValue("location_id"),
		CapacityRecordID: r.PathValue("capacity_record_id"),
		RawBody:          body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) DeleteLocationCapacity(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.DeleteLocationCapacity(r.Context(), app.DeleteLocationCapacityInput{
		TenantID:         tenantID(r),
		ActorID:          actorID(r),
		IdempotencyKey:   r.Header.Get("Idempotency-Key"),
		TraceID:          traceID(r),
		LocationID:       r.PathValue("location_id"),
		CapacityRecordID: r.PathValue("capacity_record_id"),
		RawBody:          body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) ListReviewItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.ListReviewItems(r.Context(), ports.ReviewParams{
		TenantID:   tenantID(r),
		Status:     q.Get("status"),
		ReviewType: q.Get("review_type"),
		Limit:      parseLimit(q.Get("limit")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) CreateLocationReviewItem(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.CreateLocationReviewItem(r.Context(), app.CreateLocationReviewItemInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) ResolveLocationReviewItem(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.ResolveLocationReviewItem(r.Context(), app.ResolveLocationReviewItemInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		ReviewID:       r.PathValue("review_id"),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		h.respond(w, r, nil, app.BadRequest("invalid_json", "request body is too large or unreadable"))
		return nil, false
	}
	return body, true
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
		httpresponse.WriteError(w, r, h.log, status, envelope, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, payload)
}

func parseLimit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return limit
}

func parseOffset(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
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
