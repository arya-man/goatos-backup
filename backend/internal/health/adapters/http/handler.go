package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

const maxBodyBytes = 1 << 20

type HealthService interface {
	OpenCase(context.Context, domain.OpenCaseInput) (domain.OpenCaseResult, error)
	ListWorkItems(context.Context, domain.ListFilter) (domain.WorkItemPage, error)
	GetWorkItem(context.Context, string, string) (domain.WorkItemDetail, error)
	CompleteWorkItem(context.Context, domain.CompleteInput) (domain.CompleteResult, error)
	CloseCase(context.Context, domain.CloseCaseInput) (domain.CloseCaseResult, error)
}
type Handler struct {
	svc HealthService
	log *slog.Logger
}

func NewHandler(svc HealthService, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /app/health/cases", h.OpenCase)
	mux.HandleFunc("GET /app/health/work-items", h.ListWorkItems)
	mux.HandleFunc("GET /app/health/work-items/{health_session_id}", h.GetWorkItem)
	mux.HandleFunc("POST /app/health/work-items/{health_session_id}/complete", h.CompleteWorkItem)
	mux.HandleFunc("POST /app/health/cases/{health_case_id}/close", h.CloseCase)
}

type openCaseRequest struct {
	GoatID     string `json:"goat_id"`
	DiseaseKey string `json:"disease_key"`
	AgeBand    string `json:"age_band"`
	StartDate  string `json:"start_date"`
}
type completeRequest struct {
	ProofRef string `json:"proof_ref"`
}
type closeCaseRequest struct {
	Outcome string `json:"outcome"`
	Note    string `json:"note,omitempty"`
}
type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	TraceID   string `json:"trace_id"`
	Retryable bool   `json:"retryable"`
}

func (h *Handler) OpenCase(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeStrict[openCaseRequest](w, r, h)
	if !ok {
		return
	}
	start, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_date", "start_date must be YYYY-MM-DD", err)
		return
	}
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" {
		h.writeError(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key is required", nil)
		return
	}
	res, err := h.svc.OpenCase(r.Context(), domain.OpenCaseInput{TenantID: httpmiddleware.TenantIDFromContext(r.Context()), ActorID: httpmiddleware.ActorIDFromContext(r.Context()), GoatID: req.GoatID, DiseaseKey: req.DiseaseKey, AgeBand: req.AgeBand, StartDate: start, IdempotencyKey: idem, RequestFingerprint: fingerprint(body), TraceID: httpmiddleware.TraceIDFromContext(r.Context())})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	status := http.StatusCreated
	if res.IdempotentReplay {
		status = http.StatusOK
	}
	httpresponse.WriteJSON(w, status, res)
}
func (h *Handler) ListWorkItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := domain.MaxPageSize
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_limit", "limit must be positive", err)
			return
		}
		limit = n
	}
	date := strings.TrimSpace(q.Get("date"))
	if date == "" {
		loc, _ := time.LoadLocation("Asia/Kolkata")
		date = time.Now().In(loc).Format("2006-01-02")
	}
	page, err := h.svc.ListWorkItems(r.Context(), domain.ListFilter{TenantID: httpmiddleware.TenantIDFromContext(r.Context()), AgeBand: q.Get("age_band"), Date: date, Status: q.Get("status"), DiseaseKey: q.Get("disease_key"), ParkID: q.Get("park_id"), ShedID: q.Get("shed_id"), Session: q.Get("session"), Cursor: q.Get("cursor"), Limit: limit})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}
func (h *Handler) GetWorkItem(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.GetWorkItem(r.Context(), httpmiddleware.TenantIDFromContext(r.Context()), r.PathValue("health_session_id"))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	// Caller capabilities for the client's action gating. Derived from the caller's own grants —
	// the same RoleHasPermission the route authorizer applies — never from a role string. A
	// service/internal context with no grants reads false/false, which is the safe direction for
	// a display hint (the route permission remains the enforcement).
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	for _, g := range grants {
		if permissions.RoleHasPermission(g.Role, permissions.HealthExecute) {
			d.CanComplete = true
		}
		if permissions.RoleHasPermission(g.Role, permissions.HealthDiagnose) {
			d.CanCloseCase = true
		}
	}
	httpresponse.WriteJSON(w, http.StatusOK, d)
}
func (h *Handler) CompleteWorkItem(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeStrict[completeRequest](w, r, h)
	if !ok {
		return
	}
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" {
		h.writeError(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key is required", nil)
		return
	}
	res, err := h.svc.CompleteWorkItem(r.Context(), domain.CompleteInput{TenantID: httpmiddleware.TenantIDFromContext(r.Context()), ActorID: httpmiddleware.ActorIDFromContext(r.Context()), SessionID: r.PathValue("health_session_id"), ProofRef: req.ProofRef, IdempotencyKey: idem, RequestFingerprint: fingerprint(body), TraceID: httpmiddleware.TraceIDFromContext(r.Context())})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, res)
}
func (h *Handler) CloseCase(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeStrict[closeCaseRequest](w, r, h)
	if !ok {
		return
	}
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" {
		h.writeError(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key is required", nil)
		return
	}
	res, err := h.svc.CloseCase(r.Context(), domain.CloseCaseInput{
		TenantID: httpmiddleware.TenantIDFromContext(r.Context()), ActorID: httpmiddleware.ActorIDFromContext(r.Context()),
		CaseID: r.PathValue("health_case_id"), Outcome: req.Outcome, Note: req.Note,
		IdempotencyKey: idem, RequestFingerprint: fingerprint(body), TraceID: httpmiddleware.TraceIDFromContext(r.Context())})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, res)
}
func decodeStrict[T any](w http.ResponseWriter, r *http.Request, h *Handler) ([]byte, T, bool) {
	var zero T
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_body", "invalid request body", err)
		return nil, zero, false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var v T
	if err := dec.Decode(&v); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_body", "invalid request body", err)
		return nil, zero, false
	}
	return body, v, true
}
func fingerprint(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
func (h *Handler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, healthapp.ErrInvalidInput), errors.Is(err, healthapp.ErrInvalidDate):
		h.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), err)
	case errors.Is(err, ports.ErrNotFound):
		h.writeError(w, r, http.StatusNotFound, "not_found", err.Error(), err)
	case errors.Is(err, ports.ErrProtocolNotPublished):
		h.writeError(w, r, http.StatusUnprocessableEntity, "protocol_not_published", err.Error(), err)
	case errors.Is(err, ports.ErrAgeBandMismatch):
		h.writeError(w, r, http.StatusUnprocessableEntity, "age_band_mismatch", err.Error(), err)
	case errors.Is(err, ports.ErrGoatNotAlive):
		h.writeError(w, r, http.StatusConflict, "goat_not_alive", err.Error(), err)
	case errors.Is(err, ports.ErrCaseNotOpen):
		h.writeError(w, r, http.StatusConflict, "case_not_open", err.Error(), err)
	case errors.Is(err, ports.ErrConflict):
		h.writeError(w, r, http.StatusConflict, "idempotency_conflict", err.Error(), err)
	default:
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal error", err)
	}
}
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	httpresponse.WriteError(w, r, h.log, status, errorResponse{Code: code, Message: message, TraceID: httpmiddleware.TraceIDFromContext(r.Context()), Retryable: status >= 500}, cause)
}
