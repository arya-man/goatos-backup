package http

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/operationsaudit/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

type Reader interface {
	List(ctx context.Context, q domain.Query, traceID string) (domain.ListResponse, error)
	Summary(ctx context.Context, q domain.Query, traceID string) (domain.SummaryResponse, error)
}

type Handler struct {
	reader Reader
	log    *slog.Logger
}

func NewHandler(reader Reader, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{reader: reader, log: l}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /operations/audit", h.List)
	mux.HandleFunc("GET /operations/audit/summary", h.Summary)
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q, ok := h.query(w, r)
	if !ok {
		return
	}
	resp, err := h.reader.List(r.Context(), q, traceID(r))
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	q, ok := h.query(w, r)
	if !ok {
		return
	}
	resp, err := h.reader.Summary(r.Context(), q, traceID(r))
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) query(w http.ResponseWriter, r *http.Request) (domain.Query, bool) {
	values := r.URL.Query()
	q := domain.Query{TenantID: tenantID(r), Limit: 100}
	if limit := strings.TrimSpace(values.Get("limit")); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 1 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return domain.Query{}, false
		}
		if n > 500 {
			n = 500
		}
		q.Limit = n
	}
	if cursor := strings.TrimSpace(values.Get("cursor")); cursor != "" {
		decoded, err := domain.DecodeCursor(cursor)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor must be a valid operations audit cursor")
			return domain.Query{}, false
		}
		q.Cursor = &decoded
	}
	if from := strings.TrimSpace(values.Get("from")); from != "" {
		parsed, err := time.Parse(time.RFC3339, from)
		if err != nil {
			h.badRequest(w, r, "invalid_from", "from must be RFC3339")
			return domain.Query{}, false
		}
		parsed = parsed.In(biztime.DefaultLocation())
		q.From = &parsed
	}
	if to := strings.TrimSpace(values.Get("to")); to != "" {
		parsed, err := time.Parse(time.RFC3339, to)
		if err != nil {
			h.badRequest(w, r, "invalid_to", "to must be RFC3339")
			return domain.Query{}, false
		}
		parsed = parsed.In(biztime.DefaultLocation())
		q.To = &parsed
	}
	if !h.uuidQuery(w, r, values.Get("actor_id"), "actor_id", &q.ActorID) {
		return domain.Query{}, false
	}
	if !h.uuidQuery(w, r, values.Get("resource_id"), "resource_id", &q.ResourceID) {
		return domain.Query{}, false
	}
	if !h.uuidQuery(w, r, values.Get("scope_id"), "scope_id", &q.ScopeID) {
		return domain.Query{}, false
	}
	q.ActorType = optional(values.Get("actor_type"))
	q.Action = optional(values.Get("action"))
	q.ResourceType = optional(values.Get("resource_type"))
	q.ScopeType = optional(values.Get("scope_type"))
	q.Domain = optional(values.Get("domain"))
	q.Module = optional(values.Get("module"))
	q.Category = optional(values.Get("category"))
	q.Result = optional(values.Get("result"))
	q.Status = optional(values.Get("status"))
	q.Search = optional(values.Get("q"))
	q.AnomaliesOnly = strings.EqualFold(values.Get("anomalies_only"), "true")
	q.ProofGapsOnly = strings.EqualFold(values.Get("proof_gaps"), "true")
	return q, true
}

func (h *Handler) uuidQuery(w http.ResponseWriter, r *http.Request, raw string, field string, dest **string) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return true
	}
	if !uuidutil.IsUUIDString(value) {
		h.badRequest(w, r, "invalid_"+field, field+" must be a UUID")
		return false
	}
	*dest = &value
	return true
}

func optional(raw string) *string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	return &value
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

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, message string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		errorEnvelope{Code: code, Message: message, TraceID: traceID(r)}, nil)
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
		errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
}
