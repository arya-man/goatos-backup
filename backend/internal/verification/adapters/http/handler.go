// Package http exposes the Verification module's queue read + verdict write over REST/JSON.
package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	nethttp "net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

type Handler struct {
	service *app.Service
	log     *slog.Logger
}

func NewHandler(service *app.Service, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

func Register(mux *nethttp.ServeMux, h *Handler) {
	mux.HandleFunc("GET /verification/queue", h.ListQueue)
	mux.HandleFunc("POST /verification/items/{item_id}/verdict", h.RecordVerdict)
}

type queueItemResponse struct {
	ItemID        string             `json:"item_id"`
	Vertical      string             `json:"vertical"`
	Module        string             `json:"module"`
	Category      string             `json:"category"`
	Status        string             `json:"status"`
	VerdictReason *string            `json:"verdict_reason,omitempty"`
	OperatorID    *string            `json:"operator_id,omitempty"`
	OperatorName  *string            `json:"operator_name,omitempty"` // backend-owned display label
	ShedID        *string            `json:"shed_id,omitempty"`
	ShedLabel     *string            `json:"shed_label,omitempty"` // backend-owned display label
	ParkID        *string            `json:"park_id,omitempty"`
	ParkLabel     *string            `json:"park_label,omitempty"` // backend-owned display label
	CapturedAt    string             `json:"captured_at"`
	VerifiedBy    *string            `json:"verified_by,omitempty"`
	VerifiedAt    *string            `json:"verified_at,omitempty"`
	RowVersion    int                `json:"row_version"`
	Media         []domain.MediaItem `json:"media"`
	Source        sourceResponse     `json:"source"`
}

type sourceResponse struct {
	Module       string  `json:"module"`
	TaskID       *string `json:"task_id,omitempty"`
	SubmissionID *string `json:"submission_id,omitempty"`
	RefType      string  `json:"ref_type"`
	RefID        string  `json:"ref_id"`
}

type queueListResponse struct {
	Items      []queueItemResponse `json:"items"`
	NextCursor *string             `json:"next_cursor"`
	TraceID    string              `json:"trace_id"`
}

func toQueueItemResponse(row domain.QueueRow) queueItemResponse {
	var verifiedAt *string
	if row.Item.VerifiedAt != nil {
		s := row.Item.VerifiedAt.Format(rfc3339Nano)
		verifiedAt = &s
	}
	media := row.Media
	if media == nil {
		media = []domain.MediaItem{}
	}
	return queueItemResponse{
		ItemID:        row.Item.ItemID,
		Vertical:      row.Item.Vertical,
		Module:        row.Item.Module,
		Category:      row.Item.Category,
		Status:        row.Item.Status,
		VerdictReason: row.Item.VerdictReason,
		OperatorID:    row.Item.OperatorID,
		OperatorName:  row.Item.OperatorName,
		ShedID:        row.Item.ShedID,
		ShedLabel:     row.Item.ShedLabel,
		ParkID:        row.Item.ParkID,
		ParkLabel:     row.Item.ParkLabel,
		CapturedAt:    row.Item.CapturedAt.Format(rfc3339Nano),
		VerifiedBy:    row.Item.VerifiedBy,
		VerifiedAt:    verifiedAt,
		RowVersion:    row.Item.RowVersion,
		Media:         media,
		Source: sourceResponse{
			Module:       row.Item.Source.Module,
			TaskID:       row.Item.Source.TaskID,
			SubmissionID: row.Item.Source.SubmissionID,
			RefType:      row.Item.Source.RefType,
			RefID:        row.Item.Source.RefID,
		},
	}
}

const rfc3339Nano = "2006-01-02T15:04:05.999999999Z07:00"

func (h *Handler) ListQueue(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	limit, ok := parsePositiveLimit(q.Get("limit"))
	if !ok {
		h.respondError(w, r, app.BadRequest("invalid_limit", "limit must be a positive integer"))
		return
	}
	var cursor *domain.Cursor
	if raw := strings.TrimSpace(q.Get("cursor")); raw != "" {
		decoded, err := domain.DecodeCursor(raw)
		if err != nil {
			h.respondError(w, r, app.BadRequest("invalid_cursor", "cursor must be a valid verification queue cursor"))
			return
		}
		cursor = &decoded
	}
	result, err := h.service.ListQueue(r.Context(), ports.ListQueueParams{
		TenantID: tenantID(r),
		Category: q.Get("category"),
		Vertical: q.Get("vertical"),
		Module:   q.Get("module"),
		Status:   q.Get("status"),
		Cursor:   cursor,
		Limit:    limit,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	items := make([]queueItemResponse, len(result.Items))
	for i, row := range result.Items {
		items[i] = toQueueItemResponse(row)
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, queueListResponse{Items: items, NextCursor: result.NextCursor, TraceID: traceID(r)})
}

type verdictRequest struct {
	Decision   string `json:"decision"`
	Reason     string `json:"reason"`
	RowVersion int    `json:"row_version"`
}

type verdictResponse struct {
	Item    queueItemResponse `json:"item"`
	TraceID string            `json:"trace_id"`
}

func (h *Handler) RecordVerdict(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body verdictRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	item, err := h.service.RecordVerdict(r.Context(), domain.Verdict{
		TenantID:   tenantID(r),
		ItemID:     r.PathValue("item_id"),
		Decision:   body.Decision,
		Reason:     body.Reason,
		VerifierID: actorID(r),
		RowVersion: body.RowVersion,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, verdictResponse{
		Item:    toQueueItemResponse(domain.QueueRow{Item: item}),
		TraceID: traceID(r),
	})
}

func (h *Handler) respondError(w nethttp.ResponseWriter, r *nethttp.Request, err error) {
	status := nethttp.StatusInternalServerError
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
}

func decodeJSON(w nethttp.ResponseWriter, r *nethttp.Request, dst any) bool {
	body, err := io.ReadAll(nethttp.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeBadJSON(w, r, "request body is too large or unreadable")
		return false
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeBadJSON(w, r, "request body must be valid JSON")
		return false
	}
	return true
}

func writeBadJSON(w nethttp.ResponseWriter, r *nethttp.Request, message string) {
	httpresponse.WriteJSON(w, nethttp.StatusBadRequest, domain.ErrorEnvelope{
		Code:        "invalid_json",
		Message:     message,
		FieldErrors: []domain.FieldError{},
		TraceID:     traceID(r),
	})
}

func parsePositiveLimit(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, false
	}
	return limit, true
}

func tenantID(r *nethttp.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *nethttp.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

func traceID(r *nethttp.Request) string {
	tid := httpmiddleware.TraceIDFromContext(r.Context())
	if tid == "" {
		return "missing-trace"
	}
	return tid
}
