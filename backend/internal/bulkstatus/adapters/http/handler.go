// Package http exposes the bulk status-update preview + commit APIs.
package http

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	bulkapp "github.com/vgoats/goatos/backend/internal/bulkstatus/app"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// maxBulkStatusBody bounds the request body for preview/commit. It is generous
// enough for a large in-request row set while still capping one HTTP round trip.
const maxBulkStatusBody = 16 << 20

type errorEnvelope struct {
	Code        string        `json:"code"`
	Message     string        `json:"message"`
	FieldErrors []interface{} `json:"field_errors"`
	TraceID     string        `json:"trace_id"`
	Retryable   bool          `json:"retryable"`
}

type Handler struct {
	service *bulkapp.Service
	log     *slog.Logger
}

func NewHandler(service *bulkapp.Service, log ...*slog.Logger) *Handler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Handler{service: service, log: l}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /admin/goats/bulk-status/preview", h.Preview)
	mux.HandleFunc("POST /admin/goats/bulk-status/commit", h.Commit)
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.Preview(r.Context(), bulkapp.PreviewInput{
		TenantID: tenantID(r),
		TraceID:  traceID(r),
		RawBody:  body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) Commit(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	result, err := h.service.Commit(r.Context(), bulkapp.CommitInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBulkStatusBody))
	if err != nil {
		httpresponse.WriteJSON(w, http.StatusBadRequest, errorEnvelope{
			Code:        "invalid_json",
			Message:     "request body is too large or unreadable",
			FieldErrors: []interface{}{},
			TraceID:     traceID(r),
		})
		return nil, false
	}
	return body, true
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		envelope := errorEnvelope{
			Code:        "internal_error",
			Message:     "internal server error",
			FieldErrors: []interface{}{},
			TraceID:     traceID(r),
			Retryable:   true,
		}
		var appErr *bulkapp.Error
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

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *http.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	id := httpmiddleware.TraceIDFromContext(r.Context())
	if id == "" {
		return "missing-trace"
	}
	return id
}
