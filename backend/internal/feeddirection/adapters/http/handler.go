// Package http exposes the feed-direction generation read API plus the ONE write path the module now
// owns: recording that a shed-session's feed direction was carried out (POST /feed-direction/complete).
// The two GET routes remain pure reads; the completion route is idempotent (Idempotency-Key header)
// and is the client-facing edge of the feed.direction.completed producer.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/app"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Service is the generation + completion boundary this handler renders.
type Service interface {
	Preview(ctx context.Context, q domain.PreviewQuery) (domain.PreviewPage, error)
	PackingWorklist(ctx context.Context, q domain.PackingQuery) (domain.PackingPage, error)
	CompleteSession(ctx context.Context, in app.CompleteSessionInput) (ports.CompleteSessionResult, error)
}

type Handler struct {
	service Service
	log     *slog.Logger
}

func NewHandler(service Service, log *slog.Logger) *Handler {
	return &Handler{service: service, log: log}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /feed-direction/preview", h.GetPreview)
	mux.HandleFunc("GET /feed-packing/worklist", h.GetPackingWorklist)
	mux.HandleFunc("POST /feed-direction/complete", h.PostComplete)
}

// completeSessionRequest is the completion body: which shed-session, on which feed day and workflow,
// plus optional video proof references (each carrying a server-minted proof_id). The Idempotency-Key
// header, not the body, carries the replay key.
type completeSessionRequest struct {
	ParkID     string        `json:"park_id"`
	ShedID     string        `json:"shed_id"`
	SessionNo  int32         `json:"session_no"`
	TargetDate string        `json:"target_date"`
	Workflow   string        `json:"workflow"`
	ProofRefs  []proofRefDTO `json:"proof_refs"`
}

type proofRefDTO struct {
	ProofID     string `json:"proof_id"`
	ProofType   string `json:"proof_type"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	UploadState string `json:"upload_state"`
}

type completeSessionResponse struct {
	CompletionID string `json:"completion_id"`
	Status       string `json:"status"`
	// Applied is false on an idempotent replay or when the shed-session was already completed.
	Applied bool `json:"applied"`
}

// PostComplete records that one shed-session's feed direction was carried out. Idempotent: the same
// Idempotency-Key returns the original result and runs no new side effects.
func (h *Handler) PostComplete(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	actorID := httpmiddleware.ActorIDFromContext(r.Context())
	if actorID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing actor context", nil)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "Idempotency-Key header is required", nil)
		return
	}
	if len(key) < 8 || len(key) > 200 {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "Idempotency-Key must be between 8 and 200 characters", nil)
		return
	}

	var body completeSessionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	targetDate, err := businessDateFromString(body.TargetDate)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	proofRefs := make([]domain.ProofRef, 0, len(body.ProofRefs))
	for _, ref := range body.ProofRefs {
		proofRefs = append(proofRefs, domain.ProofRef{
			ProofID:     strings.TrimSpace(ref.ProofID),
			ProofType:   strings.TrimSpace(ref.ProofType),
			SubjectType: strings.TrimSpace(ref.SubjectType),
			SubjectID:   strings.TrimSpace(ref.SubjectID),
			UploadState: strings.TrimSpace(ref.UploadState),
		})
	}

	res, err := h.service.CompleteSession(r.Context(), app.CompleteSessionInput{
		TenantID:       tenantID,
		ParkID:         strings.TrimSpace(body.ParkID),
		ShedID:         strings.TrimSpace(body.ShedID),
		SessionNo:      body.SessionNo,
		TargetDate:     targetDate,
		Workflow:       strings.TrimSpace(body.Workflow),
		ProofRefs:      proofRefs,
		CompletedBy:    actorID,
		IdempotencyKey: key,
		ActorID:        actorID,
		ActorType:      "operator",
		TraceID:        httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "feed direction complete", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, completeSessionResponse{
		CompletionID: res.CompletionID,
		Status:       res.Status,
		Applied:      res.Applied,
	})
}

// businessDateFromString parses a required YYYY-MM-DD feed day in Asia/Kolkata, same contract as the
// read routes' target_date. An instant is rejected rather than truncated.
func businessDateFromString(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("target_date is required (YYYY-MM-DD)")
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, biztime.DefaultLocation())
	if err != nil {
		return time.Time{}, fmt.Errorf("target_date must be a business date in YYYY-MM-DD form")
	}
	return biztime.BusinessDayStart(parsed), nil
}

// GetPreview serves the generated feed direction for one park and one feed day.
func (h *Handler) GetPreview(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	query := r.URL.Query()

	targetDate, err := requiredBusinessDate(query, "target_date")
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	limit, err := boundedIntParam(query, "limit", app.DefaultShedPageLimit, 1, app.MaxShedPageLimit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	offset, err := boundedIntParam(query, "offset", 0, 0, app.MaxShedPageOffset)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	// Session 0 means "every session". A present-but-invalid session is rejected rather than
	// widened to all sessions, which would silently hand back three times the requested sheet.
	sessionNo, err := boundedIntParam(query, "session", 0, 0, 99)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	page, err := h.service.Preview(r.Context(), domain.PreviewQuery{
		TenantID:   tenantID,
		ParkID:     strings.TrimSpace(query.Get("park_id")),
		TargetDate: targetDate,
		ShedID:     strings.TrimSpace(query.Get("shed_id")),
		SessionNo:  sessionNo,
		Workflow:   strings.TrimSpace(query.Get("workflow")),
		Draft:      parseDraft(query),
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		h.writeServiceError(w, r, "feed direction preview", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

// GetPackingWorklist serves the per-shed packing view for one park and one feed day.
func (h *Handler) GetPackingWorklist(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	query := r.URL.Query()

	targetDate, err := requiredBusinessDate(query, "target_date")
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	limit, err := boundedIntParam(query, "limit", app.DefaultShedPageLimit, 1, app.MaxShedPageLimit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	offset, err := boundedIntParam(query, "offset", 0, 0, app.MaxShedPageOffset)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	// Session 0 means "every session"; a present-but-invalid session is rejected, not widened --
	// same contract as the preview.
	sessionNo, err := boundedIntParam(query, "session", 0, 0, 99)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	page, err := h.service.PackingWorklist(r.Context(), domain.PackingQuery{
		TenantID:   tenantID,
		ParkID:     strings.TrimSpace(query.Get("park_id")),
		TargetDate: targetDate,
		SessionNo:  sessionNo,
		Workflow:   strings.TrimSpace(query.Get("workflow")),
		Draft:      parseDraft(query),
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		h.writeServiceError(w, r, "feed packing worklist", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

// writeServiceError maps the module's sentinel errors onto status codes. A caller error (bad park,
// bad paging) is a 400/404 rather than a 500, so a mistyped park id is not reported as an outage.
func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, op string, err error) {
	switch {
	case errors.Is(err, ports.ErrParkNotFound),
		errors.Is(err, ports.ErrShedNotInPark):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, ports.ErrParkRequired),
		errors.Is(err, ports.ErrInvalidTargetDate),
		errors.Is(err, ports.ErrInvalidWorkflow),
		errors.Is(err, ports.ErrInvalidPaging),
		errors.Is(err, ports.ErrShedRequired),
		errors.Is(err, ports.ErrInvalidSession),
		errors.Is(err, ports.ErrWorkflowRequired),
		errors.Is(err, ports.ErrIdempotencyRequired),
		errors.Is(err, ports.ErrInvalidProof):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, ports.ErrIdempotencyConflict):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, err.Error(), nil)
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, op, err)
	}
}

// requiredBusinessDate parses the mandatory feed day.
//
// It accepts YYYY-MM-DD ONLY, and resolves it in Asia/Kolkata. An instant is rejected rather than
// truncated: accepting one would reintroduce exactly the UTC-vs-IST day-boundary bug the date form
// exists to prevent, and a feed sheet generated for the wrong day is a shed fed the wrong ration.
func requiredBusinessDate(query url.Values, name string) (time.Time, error) {
	raw := strings.TrimSpace(query.Get(name))
	if raw == "" {
		return time.Time{}, fmt.Errorf("%s is required (YYYY-MM-DD)", name)
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, biztime.DefaultLocation())
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be a business date in YYYY-MM-DD form", name)
	}
	return biztime.BusinessDayStart(parsed), nil
}

// parseDraft reads the deliberate live-compute escape hatch. draft=true (or 1) live-computes a
// what-if sheet WITHOUT reading or writing any issue; anything else serves the frozen issued sheet.
// It is the only param that switches on live computation, and the response is stamped draft so a
// what-if can never be mistaken for an issued document.
func parseDraft(query url.Values) bool {
	raw := strings.ToLower(strings.TrimSpace(query.Get("draft")))
	return raw == "true" || raw == "1"
}

// boundedIntParam parses an optional integer query param. Absent or empty means the declared
// default; present but non-numeric or out of range is a 400, never a coerced value.
func boundedIntParam(query url.Values, name string, fallback, minValue, maxValue int32) (int32, error) {
	raw := strings.TrimSpace(query.Get(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if parsed < int64(minValue) || parsed > int64(maxValue) {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minValue, maxValue)
	}
	return int32(parsed), nil
}
