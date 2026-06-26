// Package http exposes procurement/source-entry backend APIs.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

type Service interface {
	ListLoads(ctx context.Context, q domain.LoadQuery) (domain.LoadListResult, error)
	CreateLoad(ctx context.Context, in ports.CreateLoad) (domain.Load, error)
	GetLoadDetail(ctx context.Context, tenantID, loadID string) (domain.LoadDetail, error)
	AddGoatToLoad(ctx context.Context, in ports.AddGoatToLoad) (domain.LoadGoat, error)
	RecordHFVaccinationEvidence(ctx context.Context, in ports.HFVaccinationEvidence) (domain.HFVaccinationEvidence, error)
	ReviewHFVaccinationEvidence(ctx context.Context, in ports.ReviewHFVaccinationEvidence) (domain.HFVaccinationEvidence, error)
	RecordSourceHealth(ctx context.Context, in ports.SourceHealth) (domain.SourceHealthCheck, error)
	PreDispatchDecision(ctx context.Context, in ports.Decision) (domain.Decision, error)
	DispatchLoad(ctx context.Context, in ports.DispatchLoad) (domain.TransitHandoff, error)
	RecordArrivalReview(ctx context.Context, in ports.ArrivalReview) (domain.ArrivalReview, error)
	AcceptIntake(ctx context.Context, in ports.AcceptIntake) ([]domain.PHCHandoff, error)
	ActionCenter(ctx context.Context, q domain.WorkQuery) (domain.ActionCenterResponse, error)
	ProtocolAdherence(ctx context.Context, q domain.WorkQuery) (domain.ProtocolAdherenceResponse, error)
	ControlTower(ctx context.Context, q domain.WorkQuery) (domain.ControlTowerResponse, error)
	WorkflowDrilldown(ctx context.Context, q domain.WorkQuery, rowID string) (domain.WorkflowDrilldownResponse, bool, error)
}

type Handler struct {
	service Service
	log     *slog.Logger
}

func NewHandler(service Service, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /procurement/source-entry/loads", h.ListLoads)
	mux.HandleFunc("POST /procurement/source-entry/loads", h.CreateLoad)
	mux.HandleFunc("GET /procurement/source-entry/loads/{load_id}", h.GetLoad)
	mux.HandleFunc("POST /procurement/source-entry/loads/{load_id}/goats", h.AddGoat)
	mux.HandleFunc("POST /procurement/source-entry/goats/{goat_id}/hf-vaccination-evidence", h.RecordHFVaccinationEvidence)
	mux.HandleFunc("POST /procurement/source-entry/hf-vaccination-evidence/{evidence_id}/review", h.ReviewHFVaccinationEvidence)
	mux.HandleFunc("POST /procurement/source-entry/goats/{goat_id}/source-health", h.RecordSourceHealth)
	mux.HandleFunc("POST /procurement/source-entry/goats/{goat_id}/pre-dispatch-decision", h.PreDispatchDecision)
	mux.HandleFunc("POST /procurement/source-entry/loads/{load_id}/dispatch", h.DispatchLoad)
	mux.HandleFunc("POST /procurement/source-entry/loads/{load_id}/arrival-review", h.ArrivalReview)
	mux.HandleFunc("POST /procurement/source-entry/loads/{load_id}/accept-intake", h.AcceptIntake)
	// Command lenses are TOP-LEVEL for every vertical. Procurement's Action Center / Protocol Adherence /
	// Control Tower / Workflow data must be served by the top-level command screens via ?domain=procurement
	// (or a generic process-integrity path), NOT by nested /procurement/source-entry/* command routes.
	// Those nested routes are intentionally NOT registered. The handler/service projection methods below
	// (ActionCenter/ProtocolAdherence/ControlTower/WorkflowDrilldown) are PARKED for that future top-level
	// wiring; they are unexposed today. TODO(procurement-lens): mount under the top-level command contract.
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

type listLoadsResponse struct {
	Items      []domain.Load `json:"items"`
	NextCursor *string       `json:"next_cursor,omitempty"`
	TraceID    string        `json:"trace_id"`
}

type loadResponse struct {
	Load    domain.Load `json:"load"`
	TraceID string      `json:"trace_id"`
}

type loadDetailResponse struct {
	Detail  domain.LoadDetail `json:"detail"`
	TraceID string            `json:"trace_id"`
}

type loadGoatResponse struct {
	Goat    domain.LoadGoat `json:"goat"`
	TraceID string          `json:"trace_id"`
}

type hfVaccinationEvidenceResponse struct {
	Evidence domain.HFVaccinationEvidence `json:"evidence"`
	TraceID  string                       `json:"trace_id"`
}

type healthResponse struct {
	HealthCheck domain.SourceHealthCheck `json:"health_check"`
	TraceID     string                   `json:"trace_id"`
}

type decisionResponse struct {
	Decision domain.Decision `json:"decision"`
	TraceID  string          `json:"trace_id"`
}

type dispatchResponse struct {
	Handoff domain.TransitHandoff `json:"handoff"`
	TraceID string                `json:"trace_id"`
}

type arrivalResponse struct {
	Review  domain.ArrivalReview `json:"review"`
	TraceID string               `json:"trace_id"`
}

type intakeResponse struct {
	Handoffs []domain.PHCHandoff `json:"handoffs"`
	TraceID  string              `json:"trace_id"`
}

func (h *Handler) ListLoads(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r, 100)
	if !ok {
		return
	}
	var cursor *domain.LoadCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := domain.DecodeLoadCursor(raw)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor must be a valid procurement load cursor")
			return
		}
		cursor = &decoded
	}
	result, err := h.service.ListLoads(r.Context(), domain.LoadQuery{
		TenantID: tenantID(r),
		Status:   r.URL.Query().Get("status"),
		Limit:    limit,
		Cursor:   cursor,
	})
	h.respond(w, r, listLoadsResponse{Items: result.Items, NextCursor: result.NextCursor, TraceID: traceID(r)}, err)
}

func (h *Handler) ActionCenter(w http.ResponseWriter, r *http.Request) {
	q, ok := h.workQuery(w, r, 100)
	if !ok {
		return
	}
	resp, err := h.service.ActionCenter(r.Context(), q)
	h.respond(w, r, resp, err)
}

func (h *Handler) ProtocolAdherence(w http.ResponseWriter, r *http.Request) {
	q, ok := h.workQuery(w, r, 100)
	if !ok {
		return
	}
	resp, err := h.service.ProtocolAdherence(r.Context(), q)
	h.respond(w, r, resp, err)
}

func (h *Handler) ControlTower(w http.ResponseWriter, r *http.Request) {
	q, ok := h.workQuery(w, r, 50)
	if !ok {
		return
	}
	resp, err := h.service.ControlTower(r.Context(), q)
	h.respond(w, r, resp, err)
}

func (h *Handler) WorkflowDrilldown(w http.ResponseWriter, r *http.Request) {
	q, ok := h.workQuery(w, r, 1)
	if !ok {
		return
	}
	resp, found, err := h.service.WorkflowDrilldown(r.Context(), q, r.PathValue("row_id"))
	if err != nil {
		h.respond(w, r, nil, err)
		return
	}
	if !found {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found_or_not_allowed", Message: "procurement workflow row was not found", TraceID: traceID(r)}, nil)
		return
	}
	h.respond(w, r, resp, nil)
}

type createLoadRequest struct {
	SourcePartyID     string          `json:"source_party_id"`
	SourceLocationID  *string         `json:"source_location_id"`
	ExpectedCount     int             `json:"expected_count"`
	PurchaseDate      *string         `json:"purchase_date"`
	PlannedDispatchAt *time.Time      `json:"planned_dispatch_at"`
	Notes             string          `json:"notes"`
	Context           json.RawMessage `json:"context"`
}

func (h *Handler) CreateLoad(w http.ResponseWriter, r *http.Request) {
	var req createLoadRequest
	if !h.decode(w, r, &req) {
		return
	}
	purchaseDate, ok := parseOptionalDate(w, r, req.PurchaseDate, "purchase_date")
	if !ok {
		return
	}
	load, err := h.service.CreateLoad(r.Context(), ports.CreateLoad{
		TenantID:         tenantID(r),
		SourcePartyID:    req.SourcePartyID,
		SourceLocationID: req.SourceLocationID,
		ExpectedCount:    req.ExpectedCount,
		PurchaseDate:     purchaseDate,
		PlannedDispatch:  req.PlannedDispatchAt,
		Notes:            req.Notes,
		Context:          req.Context,
		IdempotencyKey:   idempotencyKey(r),
		ActorID:          actorPtr(r),
	})
	h.respond(w, r, loadResponse{Load: load, TraceID: traceID(r)}, err)
}

func (h *Handler) GetLoad(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.GetLoadDetail(r.Context(), tenantID(r), r.PathValue("load_id"))
	h.respond(w, r, loadDetailResponse{Detail: detail, TraceID: traceID(r)}, err)
}

type addGoatRequest struct {
	GoatID            *string         `json:"goat_id"`
	SourceTag         *string         `json:"source_tag"`
	SourceRFID        *string         `json:"source_rfid"`
	TemporaryID       *string         `json:"temporary_id"`
	SelectionState    string          `json:"selection_state"`
	SelectionReason   string          `json:"selection_reason"`
	Purpose           string          `json:"purpose"`
	CurrentState      string          `json:"current_state"`
	IdentityState     string          `json:"identity_review_state"`
	IdentityReviewRef *string         `json:"identity_review_ref"`
	OwnershipState    string          `json:"ownership_state"`
	HealthState       string          `json:"health_state"`
	WarmupStartedAt   *time.Time      `json:"warmup_started_at"`
	WarmupEndedAt     *time.Time      `json:"warmup_ended_at"`
	WarmupDays        *int            `json:"warmup_days"`
	HoldingLocationID *string         `json:"holding_location_id"`
	ProofRefs         json.RawMessage `json:"proof_refs"`
	Metadata          json.RawMessage `json:"metadata"`
}

func (h *Handler) AddGoat(w http.ResponseWriter, r *http.Request) {
	var req addGoatRequest
	if !h.decode(w, r, &req) {
		return
	}
	goat, err := h.service.AddGoatToLoad(r.Context(), ports.AddGoatToLoad{
		TenantID:          tenantID(r),
		LoadID:            r.PathValue("load_id"),
		GoatID:            req.GoatID,
		SourceTag:         req.SourceTag,
		SourceRFID:        req.SourceRFID,
		TemporaryID:       req.TemporaryID,
		SelectionState:    req.SelectionState,
		SelectionReason:   req.SelectionReason,
		Purpose:           req.Purpose,
		CurrentState:      req.CurrentState,
		IdentityState:     req.IdentityState,
		IdentityReviewRef: req.IdentityReviewRef,
		OwnershipState:    req.OwnershipState,
		HealthState:       req.HealthState,
		WarmupStartedAt:   req.WarmupStartedAt,
		WarmupEndedAt:     req.WarmupEndedAt,
		WarmupDays:        req.WarmupDays,
		HoldingLocationID: req.HoldingLocationID,
		ProofRefs:         req.ProofRefs,
		Metadata:          req.Metadata,
		ActorID:           actorPtr(r),
		IdempotencyKey:    idempotencyKey(r),
	})
	h.respond(w, r, loadGoatResponse{Goat: goat, TraceID: traceID(r)}, err)
}

type hfVaccinationEvidenceRequest struct {
	LoadID            string          `json:"load_id"`
	ProtocolVersionID string          `json:"protocol_version_id"`
	RuleID            string          `json:"rule_id"`
	DoseCode          string          `json:"dose_code"`
	AdministeredAt    time.Time       `json:"administered_at"`
	VaccineName       string          `json:"vaccine_name"`
	LotNumber         string          `json:"lot_number"`
	ProofRefID        *string         `json:"proof_ref_id"`
	SourceRef         string          `json:"source_ref"`
	Metadata          json.RawMessage `json:"metadata"`
}

func (h *Handler) RecordHFVaccinationEvidence(w http.ResponseWriter, r *http.Request) {
	var req hfVaccinationEvidenceRequest
	if !h.decode(w, r, &req) {
		return
	}
	evidence, err := h.service.RecordHFVaccinationEvidence(r.Context(), ports.HFVaccinationEvidence{
		TenantID:          tenantID(r),
		LoadID:            req.LoadID,
		GoatID:            r.PathValue("goat_id"),
		ProtocolVersionID: req.ProtocolVersionID,
		RuleID:            req.RuleID,
		DoseCode:          req.DoseCode,
		AdministeredAt:    req.AdministeredAt,
		VaccineName:       req.VaccineName,
		LotNumber:         req.LotNumber,
		ProofRefID:        req.ProofRefID,
		SourceRef:         req.SourceRef,
		Metadata:          req.Metadata,
		ImportedBy:        actorPtr(r),
		IdempotencyKey:    idempotencyKey(r),
	})
	h.respond(w, r, hfVaccinationEvidenceResponse{Evidence: evidence, TraceID: traceID(r)}, err)
}

type hfVaccinationEvidenceReviewRequest struct {
	ExpectedRowVersion int        `json:"expected_row_version"`
	ReviewStatus       string     `json:"review_status"`
	ReviewReason       string     `json:"review_reason"`
	ReviewedAt         *time.Time `json:"reviewed_at"`
}

func (h *Handler) ReviewHFVaccinationEvidence(w http.ResponseWriter, r *http.Request) {
	var req hfVaccinationEvidenceReviewRequest
	if !h.decode(w, r, &req) {
		return
	}
	reviewedAt := time.Time{}
	if req.ReviewedAt != nil {
		reviewedAt = *req.ReviewedAt
	}
	evidence, err := h.service.ReviewHFVaccinationEvidence(r.Context(), ports.ReviewHFVaccinationEvidence{
		TenantID:           tenantID(r),
		EvidenceID:         r.PathValue("evidence_id"),
		ExpectedRowVersion: req.ExpectedRowVersion,
		ReviewStatus:       req.ReviewStatus,
		ReviewReason:       req.ReviewReason,
		ReviewedBy:         actorPtr(r),
		ReviewedAt:         reviewedAt,
		ReviewedAtSet:      req.ReviewedAt != nil,
		IdempotencyKey:     idempotencyKey(r),
	})
	h.respond(w, r, hfVaccinationEvidenceResponse{Evidence: evidence, TraceID: traceID(r)}, err)
}

type sourceHealthRequest struct {
	LoadID      string     `json:"load_id"`
	HealthState string     `json:"health_state"`
	Reason      string     `json:"reason"`
	CheckedAt   *time.Time `json:"checked_at"`
	ProofRefID  *string    `json:"proof_ref_id"`
	SOPTaskID   *string    `json:"sop_task_id"`
}

func (h *Handler) RecordSourceHealth(w http.ResponseWriter, r *http.Request) {
	var req sourceHealthRequest
	if !h.decode(w, r, &req) {
		return
	}
	checkedAt := time.Time{}
	if req.CheckedAt != nil {
		checkedAt = *req.CheckedAt
	}
	check, err := h.service.RecordSourceHealth(r.Context(), ports.SourceHealth{
		TenantID:       tenantID(r),
		GoatID:         r.PathValue("goat_id"),
		LoadID:         req.LoadID,
		HealthState:    req.HealthState,
		Reason:         req.Reason,
		CheckedBy:      actorPtr(r),
		CheckedAt:      checkedAt,
		CheckedAtSet:   req.CheckedAt != nil,
		ProofRefID:     req.ProofRefID,
		SOPTaskID:      req.SOPTaskID,
		IdempotencyKey: idempotencyKey(r),
	})
	h.respond(w, r, healthResponse{HealthCheck: check, TraceID: traceID(r)}, err)
}

type decisionRequest struct {
	LoadID          string          `json:"load_id"`
	DecisionType    string          `json:"decision_type"`
	Reason          string          `json:"reason"`
	DecidedAt       *time.Time      `json:"decided_at"`
	ProofRefID      *string         `json:"proof_ref_id"`
	SOPTaskID       *string         `json:"sop_task_id"`
	OwnerID         *string         `json:"owner_id"`
	ResumeCondition *string         `json:"resume_condition"`
	Metadata        json.RawMessage `json:"metadata"`
}

func (h *Handler) PreDispatchDecision(w http.ResponseWriter, r *http.Request) {
	var req decisionRequest
	if !h.decode(w, r, &req) {
		return
	}
	decidedAt := time.Time{}
	if req.DecidedAt != nil {
		decidedAt = *req.DecidedAt
	}
	decision, err := h.service.PreDispatchDecision(r.Context(), ports.Decision{
		TenantID:        tenantID(r),
		GoatID:          r.PathValue("goat_id"),
		LoadID:          req.LoadID,
		DecisionType:    req.DecisionType,
		Reason:          req.Reason,
		DecidedBy:       actorPtr(r),
		DecidedAt:       decidedAt,
		DecidedAtSet:    req.DecidedAt != nil,
		ProofRefID:      req.ProofRefID,
		SOPTaskID:       req.SOPTaskID,
		OwnerID:         req.OwnerID,
		ResumeCondition: req.ResumeCondition,
		Metadata:        req.Metadata,
		IdempotencyKey:  idempotencyKey(r),
	})
	h.respond(w, r, decisionResponse{Decision: decision, TraceID: traceID(r)}, err)
}

type dispatchRequest struct {
	FromLocationID *string    `json:"from_location_id"`
	ToLocationID   string     `json:"to_location_id"`
	GoatIDs        []string   `json:"goat_ids"`
	DispatchedAt   *time.Time `json:"dispatched_at"`
	ArrivedAt      *time.Time `json:"arrived_at"`
	ProofRefID     *string    `json:"proof_ref_id"`
}

func (h *Handler) DispatchLoad(w http.ResponseWriter, r *http.Request) {
	var req dispatchRequest
	if !h.decode(w, r, &req) {
		return
	}
	dispatchedAt := time.Time{}
	if req.DispatchedAt != nil {
		dispatchedAt = *req.DispatchedAt
	}
	handoff, err := h.service.DispatchLoad(r.Context(), ports.DispatchLoad{
		TenantID:        tenantID(r),
		LoadID:          r.PathValue("load_id"),
		FromLocationID:  req.FromLocationID,
		ToLocationID:    req.ToLocationID,
		GoatIDs:         req.GoatIDs,
		DispatchedAt:    dispatchedAt,
		DispatchedAtSet: req.DispatchedAt != nil,
		ArrivedAt:       req.ArrivedAt,
		ProofRefID:      req.ProofRefID,
		IdempotencyKey:  idempotencyKey(r),
		ActorID:         actorPtr(r),
	})
	h.respond(w, r, dispatchResponse{Handoff: handoff, TraceID: traceID(r)}, err)
}

type arrivalReviewRequest struct {
	ParkLocationID string               `json:"park_location_id"`
	ExpectedCount  int                  `json:"expected_count"`
	LoadedCount    int                  `json:"loaded_count"`
	ArrivedCount   int                  `json:"arrived_count"`
	MatchedCount   int                  `json:"matched_count"`
	MissingCount   int                  `json:"missing_count"`
	ExtraCount     int                  `json:"extra_count"`
	RejectedCount  int                  `json:"rejected_count"`
	HealthFlags    json.RawMessage      `json:"health_flags"`
	WeightFlags    json.RawMessage      `json:"weight_flags"`
	MediaProofID   *string              `json:"media_proof_id"`
	Status         string               `json:"status"`
	ReviewedAt     *time.Time           `json:"reviewed_at"`
	Goats          []arrivalGoatRequest `json:"goats"`
}

type arrivalGoatRequest struct {
	GoatID       *string `json:"goat_id"`
	TemporaryID  *string `json:"temporary_id"`
	SourceTag    *string `json:"source_tag"`
	ArrivalState string  `json:"arrival_state"`
	HealthFlag   *string `json:"health_flag"`
	WeightFlag   *string `json:"weight_flag"`
	ProofRefID   *string `json:"proof_ref_id"`
	Notes        string  `json:"notes"`
}

func (h *Handler) ArrivalReview(w http.ResponseWriter, r *http.Request) {
	var req arrivalReviewRequest
	if !h.decode(w, r, &req) {
		return
	}
	reviewedAt := time.Time{}
	if req.ReviewedAt != nil {
		reviewedAt = *req.ReviewedAt
	}
	items := make([]ports.ArrivalGoat, 0, len(req.Goats))
	for _, goat := range req.Goats {
		items = append(items, ports.ArrivalGoat{
			GoatID:       goat.GoatID,
			TemporaryID:  goat.TemporaryID,
			SourceTag:    goat.SourceTag,
			ArrivalState: goat.ArrivalState,
			HealthFlag:   goat.HealthFlag,
			WeightFlag:   goat.WeightFlag,
			ProofRefID:   goat.ProofRefID,
			Notes:        goat.Notes,
		})
	}
	review, err := h.service.RecordArrivalReview(r.Context(), ports.ArrivalReview{
		TenantID:       tenantID(r),
		LoadID:         r.PathValue("load_id"),
		ParkLocationID: req.ParkLocationID,
		ExpectedCount:  req.ExpectedCount,
		LoadedCount:    req.LoadedCount,
		ArrivedCount:   req.ArrivedCount,
		MatchedCount:   req.MatchedCount,
		MissingCount:   req.MissingCount,
		ExtraCount:     req.ExtraCount,
		RejectedCount:  req.RejectedCount,
		HealthFlags:    req.HealthFlags,
		WeightFlags:    req.WeightFlags,
		MediaProofID:   req.MediaProofID,
		Status:         req.Status,
		ReviewedBy:     actorPtr(r),
		ReviewedAt:     reviewedAt,
		ReviewedAtSet:  req.ReviewedAt != nil,
		IdempotencyKey: idempotencyKey(r),
		Goats:          items,
	})
	h.respond(w, r, arrivalResponse{Review: review, TraceID: traceID(r)}, err)
}

type acceptIntakeRequest struct {
	GoatIDs                   []string        `json:"goat_ids"`
	ParkLocationID            string          `json:"park_location_id"`
	ShedLocationID            string          `json:"shed_location_id"`
	AcceptedAt                *time.Time      `json:"accepted_at"`
	EntryDate                 *string         `json:"entry_date"`
	TrustedVaccinationHistory json.RawMessage `json:"trusted_vaccination_history"`
	IntakeHealthSignal        *string         `json:"intake_health_signal"`
}

func (h *Handler) AcceptIntake(w http.ResponseWriter, r *http.Request) {
	var req acceptIntakeRequest
	if !h.decode(w, r, &req) {
		return
	}
	entryDate, ok := parseOptionalDate(w, r, req.EntryDate, "entry_date")
	if !ok {
		return
	}
	acceptedAt := time.Time{}
	if req.AcceptedAt != nil {
		acceptedAt = *req.AcceptedAt
	}
	handoffs, err := h.service.AcceptIntake(r.Context(), ports.AcceptIntake{
		TenantID:                  tenantID(r),
		LoadID:                    r.PathValue("load_id"),
		GoatIDs:                   req.GoatIDs,
		ParkLocationID:            req.ParkLocationID,
		ShedLocationID:            req.ShedLocationID,
		AcceptedAt:                acceptedAt,
		AcceptedAtSet:             req.AcceptedAt != nil,
		EntryDate:                 derefTime(entryDate),
		TrustedVaccinationHistory: req.TrustedVaccinationHistory,
		IntakeHealthSignal:        req.IntakeHealthSignal,
		IdempotencyKey:            idempotencyKey(r),
		ActorID:                   actorPtr(r),
	})
	h.respond(w, r, intakeResponse{Handoffs: handoffs, TraceID: traceID(r)}, err)
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, out any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.badRequest(w, r, "invalid_body", "request body could not be read")
		return false
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	if err := json.Unmarshal(body, out); err != nil {
		h.badRequest(w, r, "invalid_json", "request body is not valid JSON")
		return false
	}
	return true
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
	if err == nil {
		httpresponse.WriteJSON(w, http.StatusOK, payload)
		return
	}
	var appErr *app.Error
	if errors.As(err, &appErr) {
		httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus,
			errorEnvelope{Code: appErr.Code, Message: appErr.Message, TraceID: traceID(r)}, err)
		return
	}
	if errors.Is(err, ports.ErrIdempotencyConflict) {
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "idempotency_conflict", Message: "Idempotency-Key was reused with a different request payload", TraceID: traceID(r)}, err)
		return
	}
	if errors.Is(err, ports.ErrStaleWrite) {
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "write_conflict", Message: "record changed; reload before retrying", TraceID: traceID(r)}, err)
		return
	}
	if errors.Is(err, ports.ErrNotFound) {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found_or_not_allowed", Message: "procurement record was not found", TraceID: traceID(r)}, err)
		return
	}
	httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
		errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, message string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		errorEnvelope{Code: code, Message: message, TraceID: traceID(r)}, nil)
}

func parseLimit(w http.ResponseWriter, r *http.Request, fallback int) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		httpresponse.WriteJSON(w, http.StatusBadRequest, errorEnvelope{Code: "invalid_limit", Message: "limit must be a positive integer", TraceID: traceID(r)})
		return 0, false
	}
	if n > 500 {
		n = 500
	}
	return n, true
}

func (h *Handler) workQuery(w http.ResponseWriter, r *http.Request, fallbackLimit int) (domain.WorkQuery, bool) {
	limit, ok := parseLimit(w, r, fallbackLimit)
	if !ok {
		return domain.WorkQuery{}, false
	}
	q := domain.WorkQuery{TenantID: tenantID(r), Limit: limit}
	values := r.URL.Query()
	if state := values.Get("work_state"); state != "" {
		q.WorkState = &state
	}
	if state := values.Get("status"); state != "" && q.WorkState == nil {
		q.WorkState = &state
	}
	if severity := values.Get("severity"); severity != "" {
		q.Severity = &severity
	}
	if raw := values.Get("cursor"); raw != "" {
		cursor, err := domain.DecodeWorkCursor(raw)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor must be a valid procurement work cursor")
			return domain.WorkQuery{}, false
		}
		q.Cursor = &cursor
	}
	return q, true
}

func parseOptionalDate(w http.ResponseWriter, r *http.Request, raw *string, field string) (*time.Time, bool) {
	if raw == nil || *raw == "" {
		return nil, true
	}
	t, err := time.Parse("2006-01-02", *raw)
	if err != nil {
		httpresponse.WriteJSON(w, http.StatusBadRequest, errorEnvelope{Code: "invalid_" + field, Message: field + " must be YYYY-MM-DD", TraceID: traceID(r)})
		return nil, false
	}
	return &t, true
}

func derefTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorPtr(r *http.Request) *string {
	if actor := httpmiddleware.ActorIDFromContext(r.Context()); actor != "" {
		return &actor
	}
	return nil
}

func idempotencyKey(r *http.Request) string {
	return r.Header.Get("Idempotency-Key")
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}
