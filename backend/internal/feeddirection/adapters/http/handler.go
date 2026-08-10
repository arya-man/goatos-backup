// Package http exposes the feed-direction generation read API plus the VERIFIER-GATED write paths the
// module owns: distribution completion, packing completion, and transport submission. Each of those
// flips a session to pending_verification and is completed only by a verifier's approval; the
// pre-gate instant route POST /feed-direction/complete is no longer registered.
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
	// CompleteDistribution is the verifier-gated feed DISTRIBUTION completion, entirely separate from
	// CompleteSession (the old instant path). It requires two mandatory proofs and flips the session to
	// pending_verification (maintainer decision, 2026-07-26).
	CompleteDistribution(ctx context.Context, in app.CompleteDistributionInput) (ports.CompleteDistributionResult, error)
	// CompletePacking is the verifier-gated feed PACKING completion (maintainer decision, 2026-07-26,
	// SUPERSEDING the "packing stays instant" rule). It requires ONE mandatory packing video and flips the
	// session to pending_verification. Separate from CompleteSession (the old instant path, now inert) and
	// from CompleteDistribution.
	CompletePacking(ctx context.Context, in app.CompletePackingInput) (ports.CompletePackingResult, error)
	ListTransportTasks(ctx context.Context, in app.ListTransportTasksInput) (ports.FeedTransportTaskPage, error)
	SubmitTransport(ctx context.Context, in app.SubmitTransportInput) (ports.SubmitTransportResult, error)
	// ListAlerts serves the feed module's own lifecycle alerts feed, the twin of
	// GET /app/weighing/alerts and GET /app/vaccination/alerts. See app/alerts.go.
	ListAlerts(ctx context.Context, tenantID, memberOrUserID string, tenantWide bool, parkIDs []string, cursor string, limit int) (domain.AlertPage, error)
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
	// POST /feed-direction/complete (the pre-gate INSTANT completion) is deliberately NOT registered.
	// It wrote 'completed' at operator submit, which walks around the ratified verification gate
	// (submit -> pending_verification -> verifier approve -> completed). Its service dependency is
	// unwired too, so the app path fails closed with ports.ErrCompletionUnavailable even if a caller
	// reaches it another way. See docs/decisions/feed-distribution-verification.md.
	mux.HandleFunc("POST /feed-direction/distribution/complete", h.PostCompleteDistribution)
	// The verifier-gated feed PACKING completion (maintainer decision, 2026-07-26).
	mux.HandleFunc("POST /feed-direction/packing/complete", h.PostCompletePacking)
	mux.HandleFunc("GET /feed-transport/tasks", h.GetTransportTasks)
	mux.HandleFunc("POST /feed-transport/tasks/{task_id}/submit", h.PostTransportSubmit)
	mux.HandleFunc("GET /app/feed/alerts", h.ListAlerts)
}

// ListAlerts serves GET /app/feed/alerts -- the feed module's OWN alerts feed, the twin of
// GET /app/weighing/alerts and GET /app/vaccination/alerts.
//
// WHY IT EXISTS: backend/internal/notificationbridge/verification_notify_consumer.go has been
// queuing feed.proof.* / feed.record.closed notifications for the verifier, feed_director,
// park_head and CEO since the feed verification gates shipped (2026-07-26), and there was no route
// to read them back.
//
// SCOPE: the query filters on context->>'member_id' = the caller, so the feed is already "my own
// alerts" and cannot leak another person's row. Park scope is therefore passed WIDE here (tenantWide
// = true, parkIDs = nil) rather than re-deriving a capability park list: a narrower park filter
// could only ever HIDE alerts that were addressed to this caller on purpose -- it can never widen
// what is visible, because the audience predicate above already pins the caller's identity. This
// mirrors vaccinationexecution's handler and is the fix for the "recipient vs reader" defect class:
// deriving tenantWide/parkIDs from the caller's FEED capabilities (as the route-level permission
// check does) would leave a verifier -- who holds VerificationReview but no feed_direction.*
// capability -- with tenantWide=false and an EMPTY park list, so the SQL predicate
// ($4::bool OR context->>'park_id' = ANY($5)) would silently exclude every row addressed to them.
func (h *Handler) ListAlerts(w http.ResponseWriter, r *http.Request) {
	limit := domain.AlertPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "limit must be a positive integer", nil)
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
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing actor context", nil)
		return
	}

	page, err := h.service.ListAlerts(
		r.Context(), tenant, actor,
		true, nil,
		strings.TrimSpace(r.URL.Query().Get("cursor")), limit,
	)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidArgument) {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "the paging cursor is not valid", nil)
			return
		}
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "list feed alerts", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
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
	// Park scope is CLAMPED to the caller's own grant before anything is read or written. This is
	// the precondition for admitting a park-scoped grant on this route in
	// httpmiddleware.routeAllowsScopedGrants: without it a CPT operator could name a CBE park_id.
	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenant, strings.TrimSpace(r.URL.Query().Get("park_id")), permissions.FeedTransportRead,
	)
	if !parkScope.Allowed {
		httpresponse.WriteError(w, r, h.log, parkScope.Status, map[string]string{
			"code": parkScope.Code, "message": parkScope.Message,
		}, nil)
		return
	}

	limit, err := boundedIntParam(r.URL.Query(), "limit", 20, 1, 100)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	actorFilter := actor
	if httpmiddleware.HasTenantWideCapability(httpmiddleware.AuthGrantsFromContext(r.Context()), tenant, permissions.FeedTransportRead) {
		actorFilter = ""
	}
	page, err := h.service.ListTransportTasks(r.Context(), app.ListTransportTasksInput{
		TenantID: tenant,
		ActorID:  actorFilter,
		Date:     r.URL.Query().Get("business_date"),
		ParkID:   parkScope.ParkID,
		ShedID:   r.URL.Query().Get("shed_id"),
		Status:   r.URL.Query().Get("status"),
		Cursor:   r.URL.Query().Get("cursor"),
		Limit:    int(limit),
	})
	if err != nil {
		h.writeServiceError(w, r, "list feed transport tasks", err)
		return
	}
	out := make([]transportTaskDTO, 0, len(page.Items))
	for _, x := range page.Items {
		out = append(out, transportTaskDTO{TaskID: x.TaskID, ParkID: x.ParkID, ParkLabel: x.ParkLabel, ShedID: x.ShedID, ShedLabel: x.ShedLabel, BusinessDate: x.BusinessDate, Status: x.Status, OperatorID: x.OperatorID, ReworkReason: x.ReworkReason, ScheduledAt: x.ScheduledAt})
	}
	parks := make([]transportFilterOptionDTO, 0, len(page.Filters.Parks))
	for _, option := range page.Filters.Parks {
		parks = append(parks, transportFilterOptionDTO{ID: option.ID, Label: option.Label})
	}
	sheds := make([]transportFilterOptionDTO, 0, len(page.Filters.Sheds))
	for _, option := range page.Filters.Sheds {
		sheds = append(sheds, transportFilterOptionDTO{ID: option.ID, Label: option.Label})
	}
	httpresponse.WriteJSON(w, http.StatusOK, transportListResponse{
		Items:      out,
		NextCursor: page.NextCursor,
		Filters:    transportFiltersDTO{Parks: parks, Sheds: sheds},
	})
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
	// This route names no park -- the task id in the path is the only input -- so the caller's OWN
	// scope is resolved here and checked against the TASK's park in the service. That check is the
	// precondition for httpmiddleware.routeAllowsScopedGrants admitting a park-scoped grant on this
	// route; without it a CBE operator holding a CPT task id could submit CPT work.
	//
	// A blank requested park asks the resolver for the caller's own authorized set rather than
	// validating a named one, so a tenant-wide principal comes back unrestricted (empty ParkIDs) and
	// a park-scoped operator comes back with exactly his park.
	transportScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenant, "", permissions.FeedDirectionComplete,
	)
	if !transportScope.Allowed {
		httpresponse.WriteError(w, r, h.log, transportScope.Status, map[string]string{
			"code": transportScope.Code, "message": transportScope.Message,
		}, nil)
		return
	}
	res, err := h.service.SubmitTransport(r.Context(), app.SubmitTransportInput{TenantID: tenant, TaskID: r.PathValue("task_id"), ProofRef: body.ProofRef, OperatorID: actor, IdempotencyKey: key, ActorID: actor, ActorType: "operator", TraceID: httpmiddleware.TraceIDFromContext(r.Context()), AuthorizedParkIDs: transportScope.ParkIDs})
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

// completeDistributionRequest is the verifier-gated distribution completion body: which shed-session,
// on which feed day and workflow, plus the TWO mandatory proof references (a feed-distribution video
// and a water proof). The Idempotency-Key header, not the body, carries the replay key.
type completeDistributionRequest struct {
	ParkID string `json:"park_id"`
	ShedID string `json:"shed_id"`
	// PartitionLabel names the PEN the operator worked ("2", "Part 3"); omit or send "" for an
	// undivided shed. It is part of the completion's identity: without it one pen's video closed
	// out every pen of the shed (STG 2026-08-08). See migration 000137.
	PartitionLabel       string `json:"partition_label"`
	SessionNo            int32  `json:"session_no"`
	TargetDate           string `json:"target_date"`
	Workflow             string `json:"workflow"`
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

// PostCompleteDistribution records a shed-session's two mandatory proofs and flips it to
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

	// Both proofs are mandatory. Reject a blank one with 422 proof_required BEFORE calling the service,
	// mirroring the shifting complete route, so a proofless request never reaches the write path.
	if strings.TrimSpace(body.DistributionProofRef) == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: "a feed-distribution video proof (distribution_proof_ref) is required"}, nil)
		return
	}
	if strings.TrimSpace(body.WaterProofRef) == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: "a water-distribution proof (water_proof_ref) is required"}, nil)
		return
	}

	// Park scope is CLAMPED to the caller's own grant before the write. Precondition for admitting a
	// park-scoped grant on this route (httpmiddleware.routeAllowsScopedGrants): a CPT operator must
	// not be able to record CBE work by naming another park in the body.
	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenantID, strings.TrimSpace(body.ParkID), permissions.FeedDirectionComplete,
	)
	if !parkScope.Allowed {
		httpresponse.WriteError(w, r, h.log, parkScope.Status, map[string]string{
			"code": parkScope.Code, "message": parkScope.Message,
		}, nil)
		return
	}

	res, err := h.service.CompleteDistribution(r.Context(), app.CompleteDistributionInput{
		TenantID:             tenantID,
		ParkID:               parkScope.ParkID,
		ShedID:               strings.TrimSpace(body.ShedID),
		PartitionLabel:       strings.TrimSpace(body.PartitionLabel),
		SessionNo:            body.SessionNo,
		TargetDate:           targetDate,
		Workflow:             strings.TrimSpace(body.Workflow),
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
	ParkID string `json:"park_id"`
	ShedID string `json:"shed_id"`
	// PartitionLabel names the PEN the operator worked; see completeDistributionRequest.
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

	// Park scope is CLAMPED to the caller's own grant before the write. Precondition for admitting a
	// park-scoped grant on this route (httpmiddleware.routeAllowsScopedGrants): a CPT operator must
	// not be able to record CBE work by naming another park in the body.
	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenantID, strings.TrimSpace(body.ParkID), permissions.FeedDirectionComplete,
	)
	if !parkScope.Allowed {
		httpresponse.WriteError(w, r, h.log, parkScope.Status, map[string]string{
			"code": parkScope.Code, "message": parkScope.Message,
		}, nil)
		return
	}

	res, err := h.service.CompletePacking(r.Context(), app.CompletePackingInput{
		TenantID:        tenantID,
		ParkID:          parkScope.ParkID,
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
	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenantID, strings.TrimSpace(query.Get("park_id")), permissions.FeedDirectionRead,
	)
	if !parkScope.Allowed {
		httpresponse.WriteError(w, r, h.log, parkScope.Status, map[string]string{
			"code": parkScope.Code, "message": parkScope.Message,
		}, nil)
		return
	}

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

	page, err := h.service.Preview(r.Context(), domain.PreviewQuery{
		TenantID:   tenantID,
		ParkID:     parkScope.ParkID,
		TargetDate: targetDate,
		ShedID:     strings.TrimSpace(query.Get("shed_id")),
		SessionNo:  sessionNo,
		Workflow:   strings.TrimSpace(query.Get("workflow")),
		Status:     status,
		Draft:      parseDraft(query),
		Limit:      limit,
		Offset:     offset,
		// The caller's own park set, so the FILTER VOCABULARY is scoped the same way the read is.
		// The clamp above already refuses a park outside this set; passing it down stops the response
		// from advertising those parks as choices in the first place. Nil for a tenant-wide principal.
		AuthorizedParkIDs: parkScope.ParkIDs,
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
	// Park scope is CLAMPED to the caller's own grant before anything is read or written. This is
	// the precondition for admitting a park-scoped grant on this route in
	// httpmiddleware.routeAllowsScopedGrants: without it a CPT operator could name a CBE park_id.
	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenantID, strings.TrimSpace(query.Get("park_id")), permissions.FeedPackingRead,
	)
	if !parkScope.Allowed {
		httpresponse.WriteError(w, r, h.log, parkScope.Status, map[string]string{
			"code": parkScope.Code, "message": parkScope.Message,
		}, nil)
		return
	}

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

	page, err := h.service.PackingWorklist(r.Context(), domain.PackingQuery{
		TenantID:   tenantID,
		ParkID:     parkScope.ParkID,
		TargetDate: targetDate,
		SessionNo:  sessionNo,
		Workflow:   strings.TrimSpace(query.Get("workflow")),
		Status:     status,
		Draft:      parseDraft(query),
		Limit:      limit,
		Offset:     offset,
		// Same scoping contract as the preview above: the packing farm dropdown offers only the parks
		// this caller may open. Nil for a tenant-wide principal.
		AuthorizedParkIDs: parkScope.ParkIDs,
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
		errors.Is(err, ports.ErrInvalidTransportStatus),
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
	case errors.Is(err, ports.ErrDistributionProofRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: err.Error()}, nil)
	case errors.Is(err, ports.ErrWaterProofRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: err.Error()}, nil)
	case errors.Is(err, ports.ErrPackingProofRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			codedError{Code: "proof_required", Message: err.Error()}, nil)
	case errors.Is(err, ports.ErrTransportProofRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "proof_required", Message: err.Error()}, nil)
	case errors.Is(err, ports.ErrTransportParkForbidden):
		// 403, not 409: the task is fine, the CALLER is out of scope. Coded so the client can tell
		// this apart from a task-state conflict and show the operator something true.
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden,
			codedError{Code: "park_scope_forbidden", Message: err.Error()}, nil)
	case errors.Is(err, ports.ErrTransportAssignedToAnotherOperator), errors.Is(err, ports.ErrTransportTaskNotActionable):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, ports.ErrDistributionStoreUnavailable),
		errors.Is(err, app.ErrDistributionEnqueuerNotWired),
		errors.Is(err, ports.ErrPackingStoreUnavailable),
		errors.Is(err, app.ErrPackingEnqueuerNotWired),
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
