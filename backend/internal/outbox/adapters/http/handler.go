package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	outboxdomain "github.com/vgoats/goatos/backend/internal/outbox/domain"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	defaultDeadLetterLimit = 100
	maxDeadLetterLimit     = 500
	maxActionIDs           = 100
	maxActionReasonBytes   = 500
)

type Repository interface {
	ListDeadLetters(ctx context.Context, q ports.DeadLetterQuery) ([]outboxdomain.DeadLetterMessage, error)
	Health(ctx context.Context, tenantID string, now time.Time) (outboxdomain.Health, error)
	ReplayDeadLetters(ctx context.Context, params ports.ReplayDeadLettersParams) (ports.DLQActionResult, error)
	DiscardDeadLetters(ctx context.Context, params ports.DiscardDeadLettersParams) (ports.DLQActionResult, error)
	RecordDLQActionAudit(ctx context.Context, params ports.RecordDLQActionAuditParams) error
}

type Handler struct {
	repo Repository
	log  *slog.Logger
	now  func() time.Time
}

func NewHandler(repo Repository, _ any, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{repo: repo, log: l, now: func() time.Time { return time.Now().UTC() }}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /operations/kernel-health", h.Health)
	mux.HandleFunc("GET /operations/dlq", h.List)
	mux.HandleFunc("POST /operations/dlq/replay", h.Replay)
	mux.HandleFunc("POST /operations/dlq/discard", h.Discard)
}

type listResponse struct {
	Items   []outboxdomain.DeadLetterMessage `json:"items"`
	TraceID string                           `json:"trace_id"`
}

type healthResponse struct {
	Outbox  outboxdomain.Health `json:"outbox"`
	TraceID string              `json:"trace_id"`
}

type actionRequest struct {
	OutboxIDs []string `json:"outbox_ids"`
	Reason    string   `json:"reason"`
}

type actionResponse struct {
	Action    string   `json:"action"`
	Updated   int64    `json:"updated"`
	OutboxIDs []string `json:"outbox_ids"`
	TraceID   string   `json:"trace_id"`
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q, ok := h.deadLetterQuery(w, r)
	if !ok {
		return
	}
	items, err := h.repo.ListDeadLetters(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, listResponse{Items: items, TraceID: traceID(r)})
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	health, err := h.repo.Health(r.Context(), tenantID(r), h.now())
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, healthResponse{Outbox: health, TraceID: traceID(r)})
}

func (h *Handler) Replay(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "replay")
}

func (h *Handler) Discard(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "discard")
}

func (h *Handler) action(w http.ResponseWriter, r *http.Request, action string) {
	req, ok := h.actionRequest(w, r)
	if !ok {
		return
	}
	idempotencyKey, ok := h.actionIdempotencyKey(w, r)
	if !ok {
		return
	}
	now := h.now()
	requestHash := dlqActionRequestHash(action, req)
	var result ports.DLQActionResult
	var err error
	switch action {
	case "replay":
		result, err = h.repo.ReplayDeadLetters(r.Context(), ports.ReplayDeadLettersParams{
			TenantID:       tenantID(r),
			OutboxIDs:      req.OutboxIDs,
			Reason:         req.Reason,
			Now:            now,
			IdempotencyKey: idempotencyKey,
			RequestHash:    requestHash,
		})
	case "discard":
		result, err = h.repo.DiscardDeadLetters(r.Context(), ports.DiscardDeadLettersParams{
			TenantID:       tenantID(r),
			OutboxIDs:      req.OutboxIDs,
			Reason:         req.Reason,
			Now:            now,
			IdempotencyKey: idempotencyKey,
			RequestHash:    requestHash,
		})
	default:
		err = errors.New("outbox: unsupported action")
	}
	if err != nil {
		if errors.Is(err, ports.ErrDLQActionConflict) {
			h.conflict(w, r, "idempotency_conflict", "Idempotency-Key was reused with a different DLQ repair request")
			return
		}
		if errors.Is(err, ports.ErrDLQActionPending) {
			h.conflict(w, r, "idempotency_pending", "Idempotency-Key is already processing")
			return
		}
		h.internal(w, r, err)
		return
	}
	if err := h.repo.RecordDLQActionAudit(r.Context(), ports.RecordDLQActionAuditParams{
		TenantID:       tenantID(r),
		Action:         action,
		OutboxIDs:      req.OutboxIDs,
		Reason:         req.Reason,
		Updated:        result.Updated,
		ActorID:        actorID(r),
		TraceID:        traceID(r),
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
	}); err != nil {
		if errors.Is(err, ports.ErrDLQActionConflict) {
			h.conflict(w, r, "idempotency_conflict", "Idempotency-Key was reused with a different DLQ repair request")
			return
		}
		if errors.Is(err, ports.ErrDLQActionPending) {
			h.conflict(w, r, "idempotency_pending", "Idempotency-Key is already processing")
			return
		}
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, actionResponse{
		Action:    action,
		Updated:   result.Updated,
		OutboxIDs: req.OutboxIDs,
		TraceID:   traceID(r),
	})
}

func (h *Handler) deadLetterQuery(w http.ResponseWriter, r *http.Request) (ports.DeadLetterQuery, bool) {
	values := r.URL.Query()
	status := strings.TrimSpace(values.Get("status"))
	if status == "" {
		status = outboxdomain.StatusDeadLetter
	}
	if !validStatus(status) {
		h.badRequest(w, r, "invalid_status", "status must be dead_letter, failed, or discarded")
		return ports.DeadLetterQuery{}, false
	}
	limit := defaultDeadLetterLimit
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return ports.DeadLetterQuery{}, false
		}
		limit = n
	}
	if limit > maxDeadLetterLimit {
		limit = maxDeadLetterLimit
	}
	return ports.DeadLetterQuery{
		TenantID:  tenantID(r),
		Status:    status,
		EventType: strings.TrimSpace(values.Get("event_type")),
		Topic:     strings.TrimSpace(values.Get("topic")),
		Limit:     limit,
	}, true
}

func (h *Handler) actionRequest(w http.ResponseWriter, r *http.Request) (actionRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	defer r.Body.Close()
	var req actionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.badRequest(w, r, "invalid_json", "request body must be valid JSON")
		return actionRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.badRequest(w, r, "invalid_json", "request body must contain one JSON object")
		return actionRequest{}, false
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		h.badRequest(w, r, "missing_reason", "reason is required for DLQ repair")
		return actionRequest{}, false
	}
	if len(req.Reason) > maxActionReasonBytes {
		h.badRequest(w, r, "reason_too_long", "reason must be 500 bytes or less")
		return actionRequest{}, false
	}
	if len(req.OutboxIDs) == 0 {
		h.badRequest(w, r, "missing_outbox_ids", "outbox_ids must contain at least one ID")
		return actionRequest{}, false
	}
	if len(req.OutboxIDs) > maxActionIDs {
		h.badRequest(w, r, "too_many_outbox_ids", "outbox_ids is limited to 100 IDs per repair action")
		return actionRequest{}, false
	}
	seen := map[string]struct{}{}
	deduped := make([]string, 0, len(req.OutboxIDs))
	for _, raw := range req.OutboxIDs {
		id := strings.TrimSpace(raw)
		if !uuidutil.IsUUIDString(id) {
			h.badRequest(w, r, "invalid_outbox_id", "every outbox_id must be a UUID")
			return actionRequest{}, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	req.OutboxIDs = deduped
	return req, true
}

func (h *Handler) actionIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.badRequest(w, r, "missing_idempotency_key", "Idempotency-Key header is required")
		return "", false
	}
	if len(key) < 8 || len(key) > 200 {
		h.badRequest(w, r, "invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
		return "", false
	}
	return key, true
}

func dlqActionRequestHash(action string, req actionRequest) string {
	ids := append([]string(nil), req.OutboxIDs...)
	sort.Strings(ids)
	raw, _ := json.Marshal(struct {
		Action    string   `json:"action"`
		OutboxIDs []string `json:"outbox_ids"`
		Reason    string   `json:"reason"`
	}{
		Action:    action,
		OutboxIDs: ids,
		Reason:    req.Reason,
	})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validStatus(status string) bool {
	switch status {
	case outboxdomain.StatusDeadLetter, outboxdomain.StatusFailed, outboxdomain.StatusDiscarded:
		return true
	default:
		return false
	}
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *http.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
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

func (h *Handler) conflict(w http.ResponseWriter, r *http.Request, code, message string) {
	httpresponse.WriteError(w, r, h.log, http.StatusConflict,
		errorEnvelope{Code: code, Message: message, TraceID: traceID(r)}, nil)
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
		errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
}
