// Package http exposes feed-direction generation reads plus verifier-gated feed proof submissions.
// The old instant-complete route is deliberately not registered; completion happens only after
// proof review.
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
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Service is the generation + completion boundary this handler renders.
type Service interface {
	Preview(ctx context.Context, q domain.PreviewQuery) (domain.PreviewPage, error)
	PackingWorklist(ctx context.Context, q domain.PackingQuery) (domain.PackingPage, error)
	CompleteSession(ctx context.Context, in app.CompleteSessionInput) (ports.CompleteSessionResult, error)
	// CompleteDistribution is the verifier-gated feed DISTRIBUTION completion, entirely separate from
	// CompleteSession (the old instant path). It requires three mandatory proofs and flips the session to
	// pending_verification (maintainer decision, 2026-07-26).
	CompleteDistribution(ctx context.Context, in app.CompleteDistributionInput) (ports.CompleteDistributionResult, error)
	// CompletePacking is the verifier-gated feed PACKING completion (maintainer decision, 2026-07-26,
	// SUPERSEDING the "packing stays instant" rule). It requires ONE mandatory packing video and flips the
	// session to pending_verification. Separate from CompleteSession (the old instant path, now inert) and
	// from CompleteDistribution.
	CompletePacking(ctx context.Context, in app.CompletePackingInput) (ports.CompletePackingResult, error)
	// WastageWorklist serves the per-pen Feed Wastage view (EXPERIMENT pens only, one row per pen
	// per feed day — maintainer decision 2026-08-18).
	WastageWorklist(ctx context.Context, q domain.WastageQuery) (domain.WastagePage, error)
	// CompleteWastage is the verifier-gated feed WASTAGE completion: ONE mandatory wastage video
	// flips the pen-day to pending_verification.
	CompleteWastage(ctx context.Context, in app.CompleteWastageInput) (ports.CompleteWastageResult, error)
	ListTransportTasks(ctx context.Context, in app.ListTransportTasksInput) (ports.FeedTransportTaskPage, error)
	SubmitTransport(ctx context.Context, in app.SubmitTransportInput) (ports.SubmitTransportResult, error)
	// ListPenSessionCaptures reports which of a pen-session's proof slots are ALREADY recorded, by
	// any operator, with the server proof id of each. Read-only; it gates nothing.
	ListPenSessionCaptures(ctx context.Context, in app.PenSessionCapturesInput) (app.PenSessionCapturesResult, error)
	// DirectedAnalytics is the Feed Analytics windowed rollup of the frozen sheet
	// (directed kg, head-days, per-head grams) — read-only, normal workflow only.
	DirectedAnalytics(ctx context.Context, in app.DirectedAnalyticsInput) (domain.DirectedAnalytics, error)
	ExecutionAnalytics(ctx context.Context, in app.DirectedAnalyticsInput) (domain.ExecutionAnalytics, error)
	ExperimentAnalytics(ctx context.Context, in app.DirectedAnalyticsInput) (domain.ExperimentAnalytics, error)
	StockAnalytics(ctx context.Context, in app.DirectedAnalyticsInput) (domain.StockAnalytics, error)
	ShedFeedAnalytics(ctx context.Context, in app.DirectedAnalyticsInput) (domain.ShedFeedAnalytics, error)
}

type Handler struct {
	service Service
	log     *slog.Logger
	// wastageMeasurer is the OPTIONAL verifier's measurement write (maintainer decision
	// 2026-08-18). Deliberately NOT part of Service: recording the measured leftover is the
	// verifier's act, and the route that serves it must not be able to reach the operator writes.
	// A deployment that has not wired it answers 404 on the route.
	wastageMeasurer WastageMeasurer
}

func NewHandler(service Service, log *slog.Logger) *Handler {
	return &Handler{service: service, log: log}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /feed-direction/preview", h.GetPreview)
	mux.HandleFunc("GET /feed-analytics/directed", h.GetDirectedAnalytics)
	mux.HandleFunc("GET /feed-analytics/execution", h.GetExecutionAnalytics)
	mux.HandleFunc("GET /feed-analytics/experiment", h.GetExperimentAnalytics)
	mux.HandleFunc("GET /feed-analytics/stock", h.GetStockAnalytics)
	mux.HandleFunc("GET /feed-analytics/shed-feed", h.GetShedFeedAnalytics)
	mux.HandleFunc("GET /feed-packing/worklist", h.GetPackingWorklist)
	// Which of a pen-session's proof slots are already recorded, by ANY operator. Read-only; it is
	// what lets three people split one pen-session's three proofs.
	mux.HandleFunc("GET /feed-direction/distribution/captures", h.GetDistributionCaptures)
	// The verifier-gated feed DISTRIBUTION completion. Separate route from POST /feed-direction/complete
	// (the old instant path).
	mux.HandleFunc("POST /feed-direction/distribution/complete", h.PostCompleteDistribution)
	// The verifier-gated feed PACKING completion (maintainer decision, 2026-07-26). Separate route from
	// both POST /feed-direction/complete (old instant path) and the distribution route.
	mux.HandleFunc("POST /feed-direction/packing/complete", h.PostCompletePacking)
	mux.HandleFunc("GET /feed-transport/tasks", h.GetTransportTasks)
	mux.HandleFunc("POST /feed-transport/tasks/{task_id}/submit", h.PostTransportSubmit)
	// Feed WASTAGE (maintainer decision, 2026-08-18): the per-pen experiment worklist, the
	// verifier-gated completion, and the verifier's measurement write.
	mux.HandleFunc("GET /feed-wastage/worklist", h.GetWastageWorklist)
	mux.HandleFunc("POST /feed-direction/wastage/complete", h.PostCompleteWastage)
	mux.HandleFunc("POST /feed-direction/wastage/{completion_id}/measurement", h.PostWastageMeasurement)
}

type transportTaskDTO struct {
	TaskID       string    `json:"task_id"`
	ParkID       string    `json:"park_id"`
	ParkLabel    string    `json:"park_label"`
	ShedID       string    `json:"shed_id"`
	ShedLabel    string    `json:"shed_label"`
	BusinessDate string    `json:"business_date"`
	Status       string    `json:"status"`
	OperatorID   string    `json:"operator_id,omitempty"`
	ReworkReason string    `json:"rework_reason,omitempty"`
	ScheduledAt  time.Time `json:"scheduled_at"`
}
type transportListResponse struct {
	Items      []transportTaskDTO  `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty"`
	Filters    transportFiltersDTO `json:"filters"`
}

type transportFilterOptionDTO struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type transportFiltersDTO struct {
	Parks []transportFilterOptionDTO `json:"parks"`
	Sheds []transportFilterOptionDTO `json:"sheds"`
}

func (h *Handler) GetTransportTasks(w http.ResponseWriter, r *http.Request) {
	tenant := httpmiddleware.TenantIDFromContext(r.Context())
	actor := httpmiddleware.ActorIDFromContext(r.Context())
	if tenant == "" || actor == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing actor context", nil)
		return
	}
	limit, err := boundedIntParam(r.URL.Query(), "limit", 20, 1, 100)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	scope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(r.Context(), tenant, strings.TrimSpace(r.URL.Query().Get("park_id")), permissions.FeedTransportRead)
	if !scope.Allowed {
		httpresponse.WriteError(w, r, h.log, scope.Status, codedError{Code: scope.Code, Message: scope.Message}, nil)
		return
	}
	actorFilter := actor
	if httpmiddleware.HasTenantWideCapability(httpmiddleware.AuthGrantsFromContext(r.Context()), tenant, permissions.FeedTransportRead) {
		actorFilter = ""
	}
	page, err := h.service.ListTransportTasks(r.Context(), app.ListTransportTasksInput{
		TenantID:          tenant,
		ActorID:           actorFilter,
		Date:              r.URL.Query().Get("business_date"),
		ParkID:            scope.ParkID,
		ShedID:            r.URL.Query().Get("shed_id"),
		Status:            r.URL.Query().Get("status"),
		Cursor:            r.URL.Query().Get("cursor"),
		Limit:             int(limit),
		AuthorizedParkIDs: scope.ParkIDs,
	})
	if err != nil {
		h.writeServiceError(w, r, "list feed transport tasks", err)
		return
	}
	out := make([]transportTaskDTO, 0, len(page.Items))
	for _, x := range page.Items {
		out = append(out, transportTaskDTO{TaskID: x.TaskID, ParkID: x.ParkID, ParkLabel: x.ParkLabel, ShedID: x.ShedID, ShedLabel: x.ShedLabel, BusinessDate: x.BusinessDate, Status: x.Status, OperatorID: x.OperatorID, ReworkReason: x.ReworkReason, ScheduledAt: x.ScheduledAt})
	}
	httpresponse.WriteJSON(w, http.StatusOK, transportListResponse{Items: out, NextCursor: page.NextCursor, Filters: transportFiltersFromPort(page.Filters)})
}

func transportFiltersFromPort(in ports.FeedTransportFilterOptions) transportFiltersDTO {
	out := transportFiltersDTO{
		Parks: make([]transportFilterOptionDTO, 0, len(in.Parks)),
		Sheds: make([]transportFilterOptionDTO, 0, len(in.Sheds)),
	}
	for _, option := range in.Parks {
		out.Parks = append(out.Parks, transportFilterOptionDTO{ID: option.ID, Label: option.Label})
	}
	for _, option := range in.Sheds {
		out.Sheds = append(out.Sheds, transportFilterOptionDTO{ID: option.ID, Label: option.Label})
	}
	return out
}

type transportSubmitRequest struct {
	ProofRef string `json:"proof_ref"`
}

type transportSubmitResponse struct {
	AttemptID   string `json:"attempt_id"`
	Status      string `json:"status"`
	AttemptNo   int32  `json:"attempt_no"`
	NewlyQueued bool   `json:"newly_pending"`
}

func (h *Handler) PostTransportSubmit(w http.ResponseWriter, r *http.Request) {
	tenant := httpmiddleware.TenantIDFromContext(r.Context())
	actor := httpmiddleware.ActorIDFromContext(r.Context())
	if tenant == "" || actor == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing actor context", nil)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 8 || len(key) > 200 {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "Idempotency-Key must be between 8 and 200 characters", nil)
		return
	}
	var body transportSubmitRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	if strings.TrimSpace(body.ProofRef) == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "proof_required", Message: "a live feed-transport video proof (proof_ref) is required"}, nil)
		return
	}
	scope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(r.Context(), tenant, "", permissions.FeedDirectionComplete)
	if !scope.Allowed {
		httpresponse.WriteError(w, r, h.log, scope.Status, codedError{Code: scope.Code, Message: scope.Message}, nil)
		return
	}
	res, err := h.service.SubmitTransport(r.Context(), app.SubmitTransportInput{TenantID: tenant, TaskID: r.PathValue("task_id"), ProofRef: body.ProofRef, OperatorID: actor, IdempotencyKey: key, ActorID: actor, ActorType: "operator", TraceID: httpmiddleware.TraceIDFromContext(r.Context()), AuthorizedParkIDs: scope.ParkIDs})
	if err != nil {
		h.writeServiceError(w, r, "submit feed transport", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, transportSubmitResponse{AttemptID: res.AttemptID, Status: res.Status, AttemptNo: res.AttemptNo, NewlyQueued: res.NewlyPending})
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

// completeDistributionRequest is the verifier-gated distribution completion body: which shed-session,
// on which feed day and workflow, plus the three mandatory proof references (feed weight photo,
// feed-distribution video, and water-distribution video). The Idempotency-Key header, not the body,
// carries the replay key.
type completeDistributionRequest struct {
	ParkID string `json:"park_id"`
	ShedID string `json:"shed_id"`
	// PartitionLabel is part of the completion's IDENTITY, not decoration: a partitioned shed has one
	// completion PER PEN. It was DECLARED in OpenAPI and MISSING here, so encoding/json dropped the
	// pen the phone sent, the write path resolved it to the 'whole' shed, and the catalog check
	// rejected every partitioned shed with ErrInvalidPartition -- a 400 that made feed distribution
	// unsubmittable for any pen (reported 2026-08-13, Castro - 1 session 2). The packing sibling
	// carried the field all along; only this route lacked it.
	PartitionLabel       string `json:"partition_label"`
	SessionNo            int32  `json:"session_no"`
	TargetDate           string `json:"target_date"`
	Workflow             string `json:"workflow"`
	FeedWeightProofRef   string `json:"feed_weight_proof_ref"`
	DistributionProofRef string `json:"distribution_proof_ref"`
	WaterProofRef        string `json:"water_proof_ref"`
}

type completeDistributionResponse struct {
	CompletionID string `json:"completion_id"`
	// Status is 'pending_verification' on a fresh submit or a rework re-submit, or 'completed' when the
	// shed-session was already verifier-approved.
	Status string `json:"status"`
	// NewlyPending is true when this call flipped the session into pending_verification (a verification
	// item was enqueued). False on an idempotent replay or an already-pending/already-completed no-op.
	NewlyPending bool `json:"newly_pending"`
}

// codedError is the small {code,message} envelope the distribution route uses for its actionable input
// errors (422 proof_required), so a client sees a stable machine code.
type codedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PostCompleteDistribution records a shed-session's three mandatory proofs and flips it to
// pending_verification (maintainer decision, 2026-07-26). Nothing is completed here: the session is
// completed only when a verifier approves the video. Idempotent: the same Idempotency-Key returns the
// original result and runs no new side effects. Entirely separate from PostComplete (packing).
func (h *Handler) PostCompleteDistribution(w http.ResponseWriter, r *http.Request) {
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

	var body completeDistributionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	targetDate, err := businessDateFromString(body.TargetDate)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// All proofs are mandatory. Reject a blank one with 422 proof_required BEFORE calling the service,
	// mirroring the shifting complete route, so a proofless request never reaches the write path.
	if strings.TrimSpace(body.FeedWeightProofRef) == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: "a feed weight photo proof (feed_weight_proof_ref) is required"}, nil)
		return
	}
	if strings.TrimSpace(body.DistributionProofRef) == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: "a feed-distribution video proof (distribution_proof_ref) is required"}, nil)
		return
	}
	if strings.TrimSpace(body.WaterProofRef) == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: "a water-distribution video proof (water_proof_ref) is required"}, nil)
		return
	}

	res, err := h.service.CompleteDistribution(r.Context(), app.CompleteDistributionInput{
		TenantID:             tenantID,
		ParkID:               strings.TrimSpace(body.ParkID),
		ShedID:               strings.TrimSpace(body.ShedID),
		PartitionLabel:       strings.TrimSpace(body.PartitionLabel),
		SessionNo:            body.SessionNo,
		TargetDate:           targetDate,
		Workflow:             strings.TrimSpace(body.Workflow),
		FeedWeightProofRef:   strings.TrimSpace(body.FeedWeightProofRef),
		DistributionProofRef: strings.TrimSpace(body.DistributionProofRef),
		WaterProofRef:        strings.TrimSpace(body.WaterProofRef),
		CompletedBy:          actorID,
		IdempotencyKey:       key,
		ActorID:              actorID,
		ActorType:            "operator",
		TraceID:              httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "feed distribution complete", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, completeDistributionResponse{
		CompletionID: res.CompletionID,
		Status:       res.Status,
		NewlyPending: res.NewlyPending,
	})
}

// completePackingRequest is the verifier-gated packing completion body: which shed-session, on which
// feed day and workflow, plus the ONE mandatory packing video reference. The Idempotency-Key header,
// not the body, carries the replay key.
type completePackingRequest struct {
	ParkID          string `json:"park_id"`
	ShedID          string `json:"shed_id"`
	PartitionLabel  string `json:"partition_label"`
	SessionNo       int32  `json:"session_no"`
	TargetDate      string `json:"target_date"`
	Workflow        string `json:"workflow"`
	PackingProofRef string `json:"packing_proof_ref"`
}

type completePackingResponse struct {
	CompletionID string `json:"completion_id"`
	// Status is 'pending_verification' on a fresh submit or a rework re-submit, or 'completed' when the
	// shed-session was already verifier-approved.
	Status string `json:"status"`
	// NewlyPending is true when this call flipped the session into pending_verification (a verification
	// item was enqueued). False on an idempotent replay or an already-pending/already-completed no-op.
	NewlyPending bool `json:"newly_pending"`
}

// PostCompletePacking records a shed-session's ONE mandatory packing video and flips it to
// pending_verification (maintainer decision, 2026-07-26, SUPERSEDING the "packing stays instant" rule).
// Nothing is completed here: the packing session is completed only when a verifier approves the video.
// Idempotent: the same Idempotency-Key returns the original result and runs no new side effects. Entirely
// separate from PostComplete (the old instant path) and PostCompleteDistribution.
func (h *Handler) PostCompletePacking(w http.ResponseWriter, r *http.Request) {
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

	var body completePackingRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	targetDate, err := businessDateFromString(body.TargetDate)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// The packing video is mandatory. Reject a blank one with 422 proof_required BEFORE calling the
	// service, mirroring the distribution route, so a proofless request never reaches the write path.
	if strings.TrimSpace(body.PackingProofRef) == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: "a packing video proof (packing_proof_ref) is required"}, nil)
		return
	}

	res, err := h.service.CompletePacking(r.Context(), app.CompletePackingInput{
		TenantID:        tenantID,
		ParkID:          strings.TrimSpace(body.ParkID),
		ShedID:          strings.TrimSpace(body.ShedID),
		PartitionLabel:  strings.TrimSpace(body.PartitionLabel),
		SessionNo:       body.SessionNo,
		TargetDate:      targetDate,
		Workflow:        strings.TrimSpace(body.Workflow),
		PackingProofRef: strings.TrimSpace(body.PackingProofRef),
		CompletedBy:     actorID,
		IdempotencyKey:  key,
		ActorID:         actorID,
		ActorType:       "operator",
		TraceID:         httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "feed packing complete", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, completePackingResponse{
		CompletionID: res.CompletionID,
		Status:       res.Status,
		NewlyPending: res.NewlyPending,
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
	// status narrows to one verification-lifecycle bucket (pending | pending_verification | completed);
	// empty means every status. A present-but-unknown value is rejected rather than widened to all.
	status := strings.TrimSpace(query.Get("status"))
	if !domain.IsValidSessionStatusFilter(status) {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "status must be one of pending, pending_verification, completed", nil)
		return
	}
	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(),
		tenantID,
		strings.TrimSpace(query.Get("park_id")),
		permissions.FeedDirectionRead,
	)
	if !parkScope.Allowed {
		httpresponse.WriteError(w, r, h.log, parkScope.Status, parkScope.Message, nil)
		return
	}

	page, err := h.service.Preview(r.Context(), domain.PreviewQuery{
		TenantID:          tenantID,
		ParkID:            parkScope.ParkID,
		AuthorizedParkIDs: parkScope.ParkIDs,
		TargetDate:        targetDate,
		ShedID:            strings.TrimSpace(query.Get("shed_id")),
		PartitionLabel:    strings.TrimSpace(query.Get("partition_label")),
		SessionNo:         sessionNo,
		Workflow:          strings.TrimSpace(query.Get("workflow")),
		Status:            status,
		Draft:             parseDraft(query),
		Limit:             limit,
		Offset:            offset,
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
	// Same status contract as the preview: one bucket or empty for all; unknown values are rejected.
	status := strings.TrimSpace(query.Get("status"))
	if !domain.IsValidSessionStatusFilter(status) {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "status must be one of pending, pending_verification, completed", nil)
		return
	}
	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(),
		tenantID,
		strings.TrimSpace(query.Get("park_id")),
		permissions.FeedPackingRead,
	)
	if !parkScope.Allowed {
		httpresponse.WriteError(w, r, h.log, parkScope.Status, parkScope.Message, nil)
		return
	}

	page, err := h.service.PackingWorklist(r.Context(), domain.PackingQuery{
		TenantID:          tenantID,
		ParkID:            parkScope.ParkID,
		AuthorizedParkIDs: parkScope.ParkIDs,
		TargetDate:        targetDate,
		ShedID:            strings.TrimSpace(query.Get("shed_id")),
		PartitionLabel:    strings.TrimSpace(query.Get("partition_label")),
		SessionNo:         sessionNo,
		Workflow:          strings.TrimSpace(query.Get("workflow")),
		Status:            status,
		Draft:             parseDraft(query),
		Limit:             limit,
		Offset:            offset,
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
		errors.Is(err, ports.ErrInvalidPartition),
		errors.Is(err, ports.ErrInvalidProof):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, ports.ErrIdempotencyConflict),
		errors.Is(err, ports.ErrDistributionAlreadyRecorded),
		errors.Is(err, ports.ErrPackingAlreadyRecorded),
		errors.Is(err, ports.ErrWastageAlreadyRecorded):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, err.Error(), nil)
	// Every mandatory distribution capture answers the same way, listed together so a fourth proof
	// cannot be added to the service and silently fall through to the 500 default -- which is what
	// happened to the feed-weight photo, telling an operator who had not taken it yet that the
	// server was broken. The message names which capture is missing; the code stays one value so a
	// client can branch on "you still owe a capture" without parsing prose.
	case errors.Is(err, ports.ErrFeedWeightProofRequired),
		errors.Is(err, ports.ErrDistributionProofRequired),
		errors.Is(err, ports.ErrWaterProofRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: err.Error()}, nil)
	case errors.Is(err, ports.ErrPackingProofRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: err.Error()}, nil)
	case errors.Is(err, ports.ErrWastageProofRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: err.Error()}, nil)
	case errors.Is(err, ports.ErrWastageNotExperimentPen):
		// A caller error, not an outage: the pen is not on that day's experiment sheet, so no
		// wastage task exists for it. The code lets a client render the business sentence.
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "not_experiment_pen", Message: err.Error()}, nil)
	case errors.Is(err, ports.ErrTransportProofRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "proof_required", Message: err.Error()}, nil)
	case errors.Is(err, ports.ErrTransportAssignedToAnotherOperator), errors.Is(err, ports.ErrTransportTaskNotActionable):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, ports.ErrDistributionStoreUnavailable),
		errors.Is(err, app.ErrDistributionEnqueuerNotWired),
		errors.Is(err, ports.ErrPackingStoreUnavailable),
		errors.Is(err, app.ErrPackingEnqueuerNotWired),
		errors.Is(err, ports.ErrWastageStoreUnavailable),
		errors.Is(err, app.ErrWastageEnqueuerNotWired),
		errors.Is(err, app.ErrTransportEnqueuerNotWired):
		// A wiring/deployment fault, not a client error: 500.
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, op, err)
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
