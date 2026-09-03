// Package http exposes the herd register read model API.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type HerdRegisterService interface {
	GetSummary(ctx context.Context, req domain.HerdRegisterSummaryQuery) (domain.HerdRegisterSummary, error)
	GetBreakdown(ctx context.Context, req domain.CountsBreakdownQuery) (domain.CountsBreakdown, error)
	GetHerdAnalytics(ctx context.Context, req domain.HerdAnalyticsQuery) (domain.HerdAnalytics, error)
	GetMilkPreparation(ctx context.Context, req domain.MilkPreparationQuery) (domain.MilkPreparationPage, error)
	SubmitMilkPreparation(ctx context.Context, req domain.MilkPreparationSubmission) (domain.MilkPreparationSubmissionResult, error)
	ListMilkFeedingTasks(ctx context.Context, req domain.MilkFeedingQuery) (domain.MilkFeedingPage, error)
	SubmitMilkFeeding(ctx context.Context, req domain.MilkFeedingSubmission) (domain.MilkFeedingSubmissionResult, error)
	// ListAlerts serves the counts module's own lifecycle alerts feed, the twin of
	// GET /app/weighing/alerts and GET /app/vaccination/alerts. See app/alerts.go.
	ListAlerts(ctx context.Context, tenantID, memberOrUserID string, tenantWide bool, parkIDs []string, cursor string, limit int) (domain.AlertPage, error)
}

type Handler struct {
	service HerdRegisterService
	log     *slog.Logger
}

func NewHandler(service HerdRegisterService, log *slog.Logger) *Handler {
	return &Handler{service: service, log: log}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /herd-register/summary", h.GetSummary)
	mux.HandleFunc("GET /counts/breakdown", h.GetBreakdown)
	mux.HandleFunc("GET /counts/herd-analytics", h.GetHerdAnalytics)
	mux.HandleFunc("GET /counts/milk-preparation", h.GetMilkPreparation)
	mux.HandleFunc("GET /app/counts/milk-preparation", h.GetMilkPreparation)
	mux.HandleFunc("POST /app/counts/milk-preparation/submit", h.SubmitMilkPreparation)
	mux.HandleFunc("GET /app/counts/milk-feeding/tasks", h.ListMilkFeedingTasks)
	mux.HandleFunc("POST /app/counts/milk-feeding/tasks/{task_id}/submit", h.SubmitMilkFeeding)
	mux.HandleFunc("GET /app/counts/alerts", h.ListAlerts)
}

// ListAlerts serves GET /app/counts/alerts -- the counts module's OWN alerts feed, the twin of
// GET /app/weighing/alerts and GET /app/vaccination/alerts.
//
// WHY IT EXISTS: backend/internal/notificationbridge/verification_notify_consumer.go has been
// queuing counts.proof.* / counts.record.closed notifications for the verifier, health_director
// (COUNTS' documented owner), park_head and CEO since the shifting verification gate shipped, and
// there was no route to read them back.
//
// SCOPE: the query filters on context->>'member_id' = the caller, so the feed is already "my own
// alerts" and cannot leak another person's row. Park scope is therefore passed WIDE here (tenantWide
// = true, parkIDs = nil) rather than re-deriving a capability park list -- see the identical note on
// feeddirection's ListAlerts handler for the "recipient vs reader" defect this avoids. It matters
// MORE here than anywhere else: health_director deliberately holds NEITHER counts.read NOR
// counts.write (COUNTS IS AN OFF FEATURE, AGENTS.md), so any capability-derived park scope would
// leave health_director with tenantWide=false and an empty park list -- exactly the same shape as
// the verifier defect that inspired this rule -- and their Alerts tab would 200 with zero rows while
// counts proofs sat addressed to them.
func (h *Handler) ListAlerts(w http.ResponseWriter, r *http.Request) {
	limit := domain.AlertPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_limit", "message": "limit must be a positive integer"}, nil)
			return
		}
		limit = parsed
	}
	if limit > domain.MaxAlertPageSize {
		limit = domain.MaxAlertPageSize
	}

	tenant := httpmiddleware.TenantIDFromContext(r.Context())
	actor := httpmiddleware.ActorIDFromContext(r.Context())
	if tenant == "" || actor == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, map[string]string{"code": "actor_required", "message": "counts alerts require an authenticated caller"}, nil)
		return
	}

	page, err := h.service.ListAlerts(
		r.Context(), tenant, actor,
		true, nil,
		strings.TrimSpace(r.URL.Query().Get("cursor")), limit,
	)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidArgument) {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_cursor", "message": "the paging cursor is not valid"}, nil)
			return
		}
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, map[string]string{"code": "internal_error", "message": "internal server error"}, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

type submitMilkFeedingRequest struct {
	ParkID      string                    `json:"park_id"`
	FeedingDate string                    `json:"feeding_date"`
	SessionNo   int                       `json:"session_no"`
	Answers     domain.MilkFeedingAnswers `json:"answers"`
	Proofs      domain.MilkFeedingProofs  `json:"proofs"`
}

func (h *Handler) ListMilkFeedingTasks(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	q := r.URL.Query()
	day := time.Now().In(biztime.DefaultLocation())
	if raw := strings.TrimSpace(q.Get("feeding_date")); raw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", raw, biztime.DefaultLocation())
		if err != nil {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_date", "message": "feeding_date must be YYYY-MM-DD"}, nil)
			return
		}
		day = parsed
	}
	limit, err := boundedIntParam(q, "limit", 20, 1, 20)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	offset, err := boundedIntParam(q, "offset", 0, 0, milkPreparationMaxOffset)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	var sessionNo *int
	if raw := strings.TrimSpace(q.Get("session_no")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > 4 {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_session", "message": "session_no must be between 1 and 4"}, nil)
			return
		}
		sessionNo = &parsed
	}
	page, err := h.service.ListMilkFeedingTasks(r.Context(), domain.MilkFeedingQuery{TenantID: tenantID, ParkID: nullableString(q.Get("park_id")), FeedingDate: day, SessionNo: sessionNo, Limit: limit, Offset: offset})
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "failed to load milk feeding actions", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) SubmitMilkFeeding(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	actorID := httpmiddleware.ActorIDFromContext(r.Context())
	if tenantID == "" || actorID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, map[string]string{"code": "missing_context", "message": "tenant and actor context are required"}, nil)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "idempotency_key_required", "message": "Idempotency-Key header is required"}, nil)
		return
	}
	var body submitMilkFeedingRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_json", "message": "invalid request body"}, err)
		return
	}
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(body.FeedingDate), biztime.DefaultLocation())
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_date", "message": "feeding_date must be YYYY-MM-DD"}, nil)
		return
	}
	submittedAt := time.Now().UTC() // india-date-guard:ignore: owner=goatos issue=GH-india-date scope=submission-absolute-instant-storage expiry=2026-12-31
	result, err := h.service.SubmitMilkFeeding(r.Context(), domain.MilkFeedingSubmission{TenantID: tenantID, TaskID: strings.TrimSpace(r.PathValue("task_id")), ParkID: strings.TrimSpace(body.ParkID), FeedingDate: day, SessionNo: body.SessionNo, Answers: body.Answers, Proofs: body.Proofs, SubmittedBy: actorID, SubmittedAt: submittedAt, IdempotencyKey: key, TraceID: httpmiddleware.TraceIDFromContext(r.Context())})
	if err != nil {
		status := http.StatusUnprocessableEntity
		code := "invalid_milk_feeding"
		if errors.Is(err, ports.ErrMilkFeedingNotFound) {
			status = http.StatusNotFound
			code = "task_not_found"
		} else if errors.Is(err, ports.ErrIdempotencyConflict) {
			status = http.StatusConflict
			code = "idempotency_conflict"
		} else if errors.Is(err, ports.ErrMilkFeedingPending) || errors.Is(err, ports.ErrMilkFeedingCompleted) {
			status = http.StatusConflict
			code = "task_not_actionable"
		} else if errors.Is(err, ports.ErrMilkFeedingNotYetAvailable) {
			status = http.StatusConflict
			code = "task_not_yet_available"
		}
		httpresponse.WriteError(w, r, h.log, status, map[string]string{"code": code, "message": err.Error()}, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusAccepted, result)
}

type submitMilkPreparationRequest struct {
	ParkID          string                         `json:"park_id"`
	PreparationDate string                         `json:"preparation_date"`
	GoatMilkUsed    bool                           `json:"goat_milk_used"`
	Answers         *domain.MilkPreparationAnswers `json:"answers"`
	Proofs          domain.MilkPreparationProofs   `json:"proofs"`
}

// SubmitMilkPreparation accepts one farm-day attempt. It stores nothing as completed: success is
// 202 pending_verification until the generic verifier approves the complete step-video package.
func (h *Handler) SubmitMilkPreparation(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	actorID := httpmiddleware.ActorIDFromContext(r.Context())
	if tenantID == "" || actorID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, map[string]string{"code": "missing_context", "message": "tenant and actor context are required"}, nil)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 8 || len(key) > 200 {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_idempotency_key", "message": "Idempotency-Key must be between 8 and 200 characters"}, nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body submitMilkPreparationRequest
	if err := decoder.Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_json", "message": "request body must match the milk preparation submission schema"}, nil)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_json", "message": "request body must contain one JSON object"}, nil)
		return
	}
	preparationDate, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(body.PreparationDate), biztime.DefaultLocation())
	if err != nil || strings.TrimSpace(body.ParkID) == "" || body.Answers == nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_scope", "message": "park_id and preparation_date (YYYY-MM-DD) are required"}, nil)
		return
	}
	submittedAt := time.Now().UTC() // india-date-guard:ignore: owner=goatos issue=GH-india-date scope=submission-absolute-instant-storage expiry=2026-12-31
	result, err := h.service.SubmitMilkPreparation(r.Context(), domain.MilkPreparationSubmission{
		TenantID: tenantID, ParkID: strings.TrimSpace(body.ParkID), PreparationDate: preparationDate,
		GoatMilkUsed: body.GoatMilkUsed, Answers: *body.Answers, Proofs: body.Proofs, SubmittedBy: actorID,
		SubmittedAt: submittedAt, IdempotencyKey: key,
		TraceID: httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		switch {
		case errors.Is(err, ports.ErrMilkPreparationProofs):
			httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, map[string]string{"code": "proof_required", "message": err.Error()}, nil)
		case errors.Is(err, ports.ErrMilkPreparationInvalidProof):
			httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, map[string]string{"code": "invalid_proof", "message": err.Error()}, nil)
		case errors.Is(err, ports.ErrMilkPreparationPending), errors.Is(err, ports.ErrMilkPreparationCompleted), errors.Is(err, ports.ErrIdempotencyConflict):
			httpresponse.WriteError(w, r, h.log, http.StatusConflict, map[string]string{"code": "state_conflict", "message": err.Error()}, nil)
		default:
			httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, map[string]string{"code": "milk_preparation_submit_failed", "message": "milk preparation submission failed"}, err)
		}
		return
	}
	httpresponse.WriteJSON(w, http.StatusAccepted, result)
}

// GetMilkPreparation serves the live-herd K1/K2/K3 milk preparation direction for one
// business day. preparation_date (YYYY-MM-DD, IST) selects the day; absent means today.
// A past day replays that day's completion/verification overlay and K3 weaning window
// against the CURRENT herd placement (head counts come from the live goats table), so it
// answers "was this day's preparation submitted?" rather than reconstructing that day's
// exact head counts — the mobile date bar renders past days read-only for this reason.
func (h *Handler) GetMilkPreparation(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	query := r.URL.Query()
	asOf := time.Now()
	if raw := strings.TrimSpace(query.Get("preparation_date")); raw != "" {
		parsed, parseErr := time.ParseInLocation("2006-01-02", raw, biztime.DefaultLocation())
		if parseErr != nil {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_date", "message": "preparation_date must be YYYY-MM-DD"}, nil)
			return
		}
		asOf = parsed
	}
	limit, err := boundedIntParam(query, "limit", milkPreparationDefaultLimit, 1, milkPreparationMaxLimit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	offset, err := boundedIntParam(query, "offset", 0, 0, milkPreparationMaxOffset)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	page, err := h.service.GetMilkPreparation(r.Context(), domain.MilkPreparationQuery{
		TenantID: tenantID,
		ParkID:   nullableString(query.Get("park_id")),
		Limit:    limit,
		Offset:   offset,
		AsOf:     asOf,
	})
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "milk preparation direction", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

// GetSummary serves exact summary counts from the herd register summary projection.
func (h *Handler) GetSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}

	// Parse query parameters.
	lifecycle := r.URL.Query().Get("lifecycle_status")
	park := r.URL.Query().Get("park_id")
	breed := r.URL.Query().Get("breed")
	sex := r.URL.Query().Get("sex")

	req := domain.HerdRegisterSummaryQuery{
		TenantID:        tenantID,
		LifecycleStatus: nullableString(lifecycle),
		ParkID:          nullableString(park),
		Breed:           nullableString(breed),
		Sex:             nullableString(sex),
	}

	summary, err := h.service.GetSummary(r.Context(), req)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "herd register summary", err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, map[string]any{
		"items": summary.Items,
	})
}

// GetBreakdown serves the Counts Breakdown census grouped by farm, stage, breed, sex and shed.
func (h *Handler) GetBreakdown(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}

	query := r.URL.Query()

	// A present-but-invalid paging value is rejected, never silently rewritten to a default the
	// caller never asked for. Absent values fall back to the declared defaults.
	limit, err := boundedIntParam(query, "limit", countsBreakdownDefaultLimit, 1, countsBreakdownMaxLimit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	offset, err := boundedIntParam(query, "offset", 0, 0, countsBreakdownMaxOffset)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// Every filter dimension is REPEATABLE: each occurrence adds a value to the match set
	// (OR within a dimension, AND across dimensions), and a single occurrence behaves exactly as
	// the old single-valued form — installed mobile clients keep working unchanged.
	//
	// Pens arrive two ways, merged into ONE set:
	//   - repeatable `pen` values in the facet-key convention, "<shed_uuid>" for a whole shed or
	//     "<shed_uuid>#<partition>" for one pen (the same oploc.Key shape facets.sheds emits);
	//   - the legacy single `shed_id` + `partition_label` pair, kept for installed clients.
	pens := make([]domain.CountsBreakdownPen, 0, len(query["pen"])+1)
	for _, raw := range query["pen"] {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		shedID, partition, _ := strings.Cut(raw, "#")
		if shedID == "" {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "pen must be <shed_id> or <shed_id>#<partition>", nil)
			return
		}
		pens = append(pens, domain.CountsBreakdownPen{ShedID: shedID, PartitionLabel: partition})
	}
	if legacyShed := strings.TrimSpace(query.Get("shed_id")); legacyShed != "" {
		pens = append(pens, domain.CountsBreakdownPen{
			ShedID:         legacyShed,
			PartitionLabel: strings.TrimSpace(query.Get("partition_label")),
		})
	}

	req := domain.CountsBreakdownQuery{
		TenantID:         tenantID,
		LifecycleStatus:  nullableString(query.Get("lifecycle_status")),
		ParkIDs:          multiParam(query, "park_id"),
		Pens:             pens,
		ManagementStages: multiParam(query, "management_stage"),
		Breeds:           multiParam(query, "breed"),
		Sexes:            multiParam(query, "sex"),
		Limit:            limit,
		Offset:           offset,
	}

	breakdown, err := h.service.GetBreakdown(r.Context(), req)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "counts breakdown", err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, breakdown)
}

// GetHerdAnalytics serves the Counts -> Herd Analytics read: the live census
// composition beside month-by-month births, exits and applied pen movements.
//
// A present-but-invalid `months` is REJECTED rather than silently rewritten to
// the default: a leader who asked for a specific history length must never be
// shown a different one under the same label.
func (h *Handler) GetHerdAnalytics(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}

	fromDate, toDate, err := domain.ResolveHerdAnalyticsWindow(r.URL.Query().Get("from"), r.URL.Query().Get("to"), time.Now())
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	analytics, err := h.service.GetHerdAnalytics(r.Context(), domain.HerdAnalyticsQuery{
		TenantID: tenantID,
		ParkID:   nullableString(strings.TrimSpace(r.URL.Query().Get("park_id"))),
		FromDate: fromDate,
		ToDate:   toDate,
	})
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "counts herd analytics", err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, analytics)
}

const (
	countsBreakdownDefaultLimit = 10
	countsBreakdownMaxLimit     = 100
	countsBreakdownMaxOffset    = 5000
	milkPreparationDefaultLimit = 10
	milkPreparationMaxLimit     = 50
	milkPreparationMaxOffset    = 5000
)

// boundedIntParam parses an optional integer query param. Absent or empty means the declared
// default; present but non-numeric or out of range is a 400, not a coerced value.
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

// multiParam collects every occurrence of a repeatable query parameter, trimmed, with empty
// values dropped — so `?breed=` filters nothing rather than matching a breed named "".
func multiParam(query url.Values, name string) []string {
	values := query[name]
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
