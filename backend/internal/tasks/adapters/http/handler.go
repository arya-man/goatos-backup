// Package http is the tasks module's app-tier transport: the operator's birth/death per-goat
// follow-up work list (GET /app/workflows), the per-goat detail, and the answer/complete action
// writes (docs/decisions/birth-death-workflows.md).
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
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

const (
	appWorkflowsRoute              = "/app/workflows"
	appWorkflowDetailRoute         = "/app/workflows/{workflow_id}"
	appWorkflowStartRoute          = "/app/workflows/start"
	appGeneralSOPsRoute            = "/app/sops/general"
	appWorkflowActionAnswerRoute   = "/app/workflows/{workflow_id}/actions/{action_id}/answer"
	appWorkflowActionCompleteRoute = "/app/workflows/{workflow_id}/actions/{action_id}/complete"

	appWorkflowAnswerCommand   = "tasks.app.workflow_action_answer"
	appWorkflowCompleteCommand = "tasks.app.workflow_action_complete"

	maxBodyBytes = 1 << 20
)

// WorkflowService is the slice of tasks/app.Service this handler drives.
type WorkflowService interface {
	ListWorkflows(ctx context.Context, in tasksapp.ListWorkflowsInput) (domain.WorkflowListPage, error)
	ListColostrumDay(ctx context.Context, in tasksapp.ListColostrumDayInput) (domain.WorkflowListPage, error)
	GetWorkflow(ctx context.Context, tenantID, workflowID string) (domain.WorkflowDetail, error)
	GetColostrumDay(ctx context.Context, tenantID, workflowID, date string) (tasksapp.ColostrumDetail, error)
	AnswerAction(ctx context.Context, in tasksapp.AnswerActionInput) (domain.ActionWriteResult, error)
	CompleteAction(ctx context.Context, in tasksapp.CompleteActionInput) (domain.ActionWriteResult, error)
	ListGeneralSOPs(ctx context.Context, tenantID string) ([]ports.GeneralSOP, error)
	StartGeneralWorkflow(ctx context.Context, in tasksapp.StartGeneralWorkflowInput) (string, error)
}

// Handler serves the four workflow routes.
type Handler struct {
	svc WorkflowService
	log *slog.Logger
}

func NewHandler(svc WorkflowService, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// Register mounts the workflow routes on the protected mux.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET "+appWorkflowsRoute, h.ListWorkflows)
	mux.HandleFunc("GET "+appWorkflowDetailRoute, h.GetWorkflow)
	mux.HandleFunc("POST "+appWorkflowActionAnswerRoute, h.AnswerAction)
	mux.HandleFunc("POST "+appWorkflowActionCompleteRoute, h.CompleteAction)
	// General SOPs (maintainer decision 2026-09-18): the startable work instructions and the
	// start itself. Patterns are byte-identical to permissions/routes.go.
	mux.HandleFunc("GET "+appGeneralSOPsRoute, h.ListGeneralSOPs)
	mux.HandleFunc("POST "+appWorkflowStartRoute, h.StartGeneralWorkflow)
}

type generalSOPsResponse struct {
	SOPs []ports.GeneralSOP `json:"sops"`
}

type startWorkflowRequest struct {
	SOPCode string `json:"sop_code"`
	ParkID  string `json:"park_id,omitempty"`
}

type startWorkflowResponse struct {
	WorkflowID string `json:"workflow_id"`
}

// ListGeneralSOPs serves GET /app/sops/general.
func (h *Handler) ListGeneralSOPs(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	sops, err := h.svc.ListGeneralSOPs(r.Context(), tenantID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, generalSOPsResponse{SOPs: sops})
}

// StartGeneralWorkflow serves POST /app/workflows/start (Idempotency-Key mandatory: the key IS the
// run, so a retried tap opens one workflow, not two).
func (h *Handler) StartGeneralWorkflow(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	clientKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(clientKey) < 8 || len(clientKey) > 200 {
		h.writeError(w, r, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header (8-200 characters) is required", nil)
		return
	}
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	var req startWorkflowRequest
	if err := decodeStrictJSON(body, &req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must match StartWorkflowRequest", err)
		return
	}
	workflowID, err := h.svc.StartGeneralWorkflow(r.Context(), tasksapp.StartGeneralWorkflowInput{
		TenantID: tenantID,
		SOPCode:  req.SOPCode,
		ParkID:   req.ParkID,
		ActorID:  httpmiddleware.ActorIDFromContext(r.Context()),
		RunID:    runIDFromKey(tenantID, clientKey),
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, startWorkflowResponse{WorkflowID: workflowID})
}

// runIDFromKey derives the run's subject_ref_id (a uuid) from the client's idempotency key, so
// the same key always names the same run.
func runIDFromKey(tenantID, clientKey string) string {
	sum := sha256.Sum256([]byte(tenantID + "\x1f" + clientKey))
	sum[6] = (sum[6] & 0x0f) | 0x40
	sum[8] = (sum[8] & 0x3f) | 0x80
	h := hex.EncodeToString(sum[:16])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// ---------------------------------------------------------------------------
// DTOs (snake_case JSON; backend-owned display copy)
// ---------------------------------------------------------------------------

type workflowCardDTO struct {
	WorkflowID  string `json:"workflow_id"`
	Module      string `json:"module"`
	TemplateKey string `json:"template_key"`
	// TemplateLabel is the operator-facing kind ("Birth", "Death", "Pen return", "Pen move").
	TemplateLabel        string                     `json:"template_label"`
	Subject              domain.WorkflowSubject     `json:"subject"`
	EventAt              time.Time                  `json:"event_at"`
	EventDate            string                     `json:"event_date"`
	ParkLabel            string                     `json:"park_label"`
	ShedLabel            string                     `json:"shed_label"`
	ActionsDone          int                        `json:"actions_done"`
	ActionsTotal         int                        `json:"actions_total"`
	NextAction           *domain.WorkflowNextAction `json:"next_action"`
	AwaitingVerification bool                       `json:"awaiting_verification"`
	State                string                     `json:"state"`
}

type workflowListResponse struct {
	Items        []workflowCardDTO            `json:"items"`
	Chips        domain.WorkflowChips         `json:"chips"`
	OverdueDates []domain.WorkflowOverdueDate `json:"overdue_dates"`
	NextCursor   *string                      `json:"next_cursor"`
}

type workflowActionDTO struct {
	ActionID      string     `json:"action_id"`
	ActionKey     string     `json:"action_key"`
	Seq           int        `json:"seq"`
	Section       string     `json:"section"`
	ActionType    string     `json:"action_type"`
	Title         string     `json:"title"`
	Detail        string     `json:"detail"`
	RequiresVideo bool       `json:"requires_video"`
	Options       []string   `json:"options,omitempty"`
	DueAt         *time.Time `json:"due_at"`
	Status        string     `json:"status"`
	Blocked       bool       `json:"blocked"`
	BlockedReason string     `json:"blocked_reason,omitempty"`
	AnswerValue   *string    `json:"answer_value"`
	ProofRef      *string    `json:"proof_ref"`
	// SOP-driven step attributes (docs/decisions/sop-driven-herd-operations.md). The phone renders
	// answer_type / proof_min_* verbatim: a step can ask for a number, text, several choices, and
	// several videos and photos. proof_refs lists every captured proof; proof_ref stays the first video.
	TaskType       string         `json:"task_type"`
	AnswerType     string         `json:"answer_type"`
	ProofMinVideos int            `json:"proof_min_videos"`
	ProofMinPhotos int            `json:"proof_min_photos"`
	ProofRefs      []proofItemDTO `json:"proof_refs"`
	// ReworkReason is the verifier's words when the step was sent back; render verbatim.
	ReworkReason string `json:"rework_reason"`
	// BranchNote is the backend-composed sentence for a step on an answer-driven branch ("Only
	// if 'Is the animal ready?' is No"); blank on an unconditional step. Render verbatim.
	BranchNote         string     `json:"branch_note,omitempty"`
	CompletedByLabel   string     `json:"completed_by_label"`
	CompletedAt        *time.Time `json:"completed_at"`
	VerificationStatus string     `json:"verification_status"`
}

type workflowDetailResponse struct {
	workflowCardDTO
	Facts   []domain.WorkflowFact `json:"facts"`
	Actions []workflowActionDTO   `json:"actions"`
}

type workflowActionWriteResponse struct {
	WorkflowID           string     `json:"workflow_id"`
	ActionID             string     `json:"action_id"`
	Status               string     `json:"status"`
	WorkflowState        string     `json:"workflow_state"`
	ActionsDone          int        `json:"actions_done"`
	ActionsTotal         int        `json:"actions_total"`
	AwaitingVerification bool       `json:"awaiting_verification"`
	CompletedAt          *time.Time `json:"completed_at"`
	IdempotentReplay     bool       `json:"idempotent_replay"`
}

type errorEnvelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	TraceID   string `json:"trace_id"`
	Retryable bool   `json:"retryable"`
}

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

// ListWorkflows serves one keyset page of cards + chips for one module and business date.
func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	query := r.URL.Query()
	// `colostrum` is a LENS keyword, not a workflow_instances.module value (that stays 'birth').
	// It selects the same card DTO built from a different grain: the kids with colostrum feeds due
	// on ONE date, counted over that date's feeds (docs/decisions/colostrum-milk-module.md).
	module := strings.ToLower(strings.TrimSpace(query.Get("module")))
	if module != domain.ModuleBirth && module != domain.ModuleDeath && module != domain.ModuleColostrum && module != domain.ModuleGeneral {
		h.writeError(w, r, http.StatusBadRequest, "invalid_module", "module must be birth, death, colostrum, or general", nil)
		return
	}
	if !hasModulePermission(r.Context(), module) {
		h.writeError(w, r, http.StatusForbidden, "permission_denied", "permission denied", nil)
		return
	}
	date := strings.TrimSpace(query.Get("date"))
	if date != "" {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_date", "date must be YYYY-MM-DD", nil)
			return
		}
	}
	filter := strings.ToLower(strings.TrimSpace(query.Get("filter")))
	if module == domain.ModuleColostrum {
		// The colostrum lens has no awaiting_video bucket: verification is enqueued once per WHOLE
		// kid workflow, so a single day's feeds can never occupy it. Rejecting the filter keeps a
		// client from rendering a chip that would always read zero.
		if !domain.ColostrumFilterAllowed(filter) {
			h.writeError(w, r, http.StatusBadRequest, "invalid_filter",
				"filter must be all, overdue, due, or completed", nil)
			return
		}
	} else {
		switch filter {
		case "", domain.FilterAll, domain.FilterOverdue, domain.FilterDue, domain.FilterCompleted, domain.FilterAwaitingVideo:
		default:
			h.writeError(w, r, http.StatusBadRequest, "invalid_filter",
				"filter must be all, overdue, due, completed, or awaiting_video", nil)
			return
		}
	}
	pageSize := domain.MaxWorkflowPageSize
	if raw := strings.TrimSpace(query.Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_page_size", "page_size must be a positive integer", nil)
			return
		}
		if parsed < pageSize {
			pageSize = parsed
		}
	}

	var (
		page domain.WorkflowListPage
		err  error
	)
	if module == domain.ModuleColostrum {
		page, err = h.svc.ListColostrumDay(r.Context(), tasksapp.ListColostrumDayInput{
			TenantID: tenantID,
			Date:     date,
			Filter:   filter,
			PageSize: pageSize,
			Cursor:   strings.TrimSpace(query.Get("cursor")),
		})
	} else {
		page, err = h.svc.ListWorkflows(r.Context(), tasksapp.ListWorkflowsInput{
			TenantID: tenantID,
			Module:   module,
			Date:     date,
			Filter:   filter,
			PageSize: pageSize,
			Cursor:   strings.TrimSpace(query.Get("cursor")),
		})
	}
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	items := make([]workflowCardDTO, 0, len(page.Items))
	for _, card := range page.Items {
		items = append(items, cardDTO(card))
	}
	overdueDates := page.OverdueDates
	if overdueDates == nil {
		overdueDates = make([]domain.WorkflowOverdueDate, 0)
	}
	httpresponse.WriteJSON(w, http.StatusOK, workflowListResponse{
		Items:        items,
		Chips:        page.Chips,
		OverdueDates: overdueDates,
		NextCursor:   page.NextCursor,
	})
}

// GetWorkflow serves the per-goat detail (card header + facts + <= 13 action rows).
func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	workflowID := strings.TrimSpace(r.PathValue("workflow_id"))
	if workflowID == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_workflow_id", "workflow_id is required", nil)
		return
	}
	query := r.URL.Query()
	lens := strings.ToLower(strings.TrimSpace(query.Get("lens")))
	if lens != "" && lens != domain.ModuleColostrum {
		h.writeError(w, r, http.StatusBadRequest, "invalid_lens", "lens must be colostrum when present", nil)
		return
	}
	date := strings.TrimSpace(query.Get("date"))
	if date != "" {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_date", "date must be YYYY-MM-DD", nil)
			return
		}
	}

	now := time.Now().UTC()
	if lens == domain.ModuleColostrum {
		if !hasWorkflowPermission(r.Context(), permissions.CountsWrite) {
			h.writeError(w, r, http.StatusForbidden, "permission_denied", "permission denied", nil)
			return
		}
		colostrum, err := h.svc.GetColostrumDay(r.Context(), tenantID, workflowID, date)
		if err != nil {
			h.writeDomainError(w, r, err)
			return
		}
		// Rendered rows are the day's feeds; the SIBLING set stays the kid's complete action list.
		// actionDTO derives blocked/blocked_reason from siblings, and `1st Colostrum` is gated by
		// four earlier main-section steps that are not colostrum. Passing the filtered list would
		// report a gated feed as ready and turn an honest "previous_action" into a bare 409 on tap.
		actions := make([]workflowActionDTO, 0, len(colostrum.Visible))
		for _, a := range colostrum.Visible {
			actions = append(actions, actionDTO(colostrum.Detail.Card.TemplateKey, a, colostrum.Detail.Actions, now))
		}
		httpresponse.WriteJSON(w, http.StatusOK, workflowDetailResponse{
			workflowCardDTO: cardDTO(colostrum.Detail.Card),
			Facts:           colostrum.Detail.Facts,
			Actions:         actions,
		})
		return
	}

	detail, err := h.svc.GetWorkflow(r.Context(), tenantID, workflowID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	if !hasModulePermission(r.Context(), detail.Card.Module) {
		h.writeError(w, r, http.StatusForbidden, "permission_denied", "permission denied", nil)
		return
	}
	actions := make([]workflowActionDTO, 0, len(detail.Actions))
	for _, a := range detail.Actions {
		// A skipped step is the branch the answer did not take: never shown, never owed.
		if a.ActionType == domain.ActionTypeApproval || a.Status == domain.ActionStatusSkipped {
			continue
		}
		actions = append(actions, actionDTO(detail.Card.TemplateKey, a, detail.Actions, now))
	}
	httpresponse.WriteJSON(w, http.StatusOK, workflowDetailResponse{
		workflowCardDTO: cardDTOWithActions(detail.Card, detail.Actions),
		Facts:           detail.Facts,
		Actions:         actions,
	})
}

type proofItemDTO struct {
	Ref  string `json:"ref"`
	Kind string `json:"kind"`
}

func proofItemsFromDTO(in []proofItemDTO) []domain.ProofItem {
	out := make([]domain.ProofItem, 0, len(in))
	for _, p := range in {
		out = append(out, domain.ProofItem{Ref: strings.TrimSpace(p.Ref), Kind: strings.TrimSpace(p.Kind)})
	}
	return out
}

func proofItemsToDTO(in []domain.ProofItem) []proofItemDTO {
	out := make([]proofItemDTO, 0, len(in))
	for _, p := range in {
		out = append(out, proofItemDTO{Ref: p.Ref, Kind: p.Kind})
	}
	return out
}

type answerActionRequest struct {
	AnswerValue string `json:"answer_value"`
	ProofRef    string `json:"proof_ref,omitempty"`
	// Proofs carries every capture for a multi-proof step ({ref, kind: video|photo}). proof_ref
	// alone still works for a one-video step (older APKs).
	Proofs []proofItemDTO `json:"proofs,omitempty"`
}

// AnswerAction answers a question / question_select step, including numeric kg (Idempotency-Key mandatory).
func (h *Handler) AnswerAction(w http.ResponseWriter, r *http.Request) {
	tenantID, workflowID, actionID, clientKey, ok := h.writePreamble(w, r)
	if !ok {
		return
	}
	if !h.authorizeWorkflowWrite(w, r, tenantID, workflowID) {
		return
	}
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	var req answerActionRequest
	if err := decodeStrictJSON(body, &req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must match AnswerWorkflowActionRequest", err)
		return
	}
	canonical, err := canonicalRequestBytes(tenantID, appWorkflowAnswerCommand, workflowID, actionID, req)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	result, err := h.svc.AnswerAction(r.Context(), tasksapp.AnswerActionInput{
		TenantID:           tenantID,
		WorkflowID:         workflowID,
		ActionID:           actionID,
		AnswerValue:        req.AnswerValue,
		ProofRef:           req.ProofRef,
		Proofs:             proofItemsFromDTO(req.Proofs),
		AnsweredBy:         httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey:     "tasks-workflow-answer:" + clientKey,
		RequestFingerprint: stableHash("tasks-workflow-answer", canonical),
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, writeResponse(result))
}

type completeActionRequest struct {
	ProofRef string         `json:"proof_ref,omitempty"`
	Proofs   []proofItemDTO `json:"proofs,omitempty"`
}

// CompleteAction completes an "action" step. A requires_video step without proof_ref is rejected
// 422 proof_required (mirrors the shifting completion contract). The second death video triggers
// the verification enqueue after the durable completion commits.
func (h *Handler) CompleteAction(w http.ResponseWriter, r *http.Request) {
	tenantID, workflowID, actionID, clientKey, ok := h.writePreamble(w, r)
	if !ok {
		return
	}
	if !h.authorizeWorkflowWrite(w, r, tenantID, workflowID) {
		return
	}
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	var req completeActionRequest
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := decodeStrictJSON(body, &req); err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must match CompleteWorkflowActionRequest", err)
			return
		}
	}
	canonical, err := canonicalRequestBytes(tenantID, appWorkflowCompleteCommand, workflowID, actionID, req)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	result, err := h.svc.CompleteAction(r.Context(), tasksapp.CompleteActionInput{
		TenantID:           tenantID,
		WorkflowID:         workflowID,
		ActionID:           actionID,
		ProofRef:           strings.TrimSpace(req.ProofRef),
		Proofs:             proofItemsFromDTO(req.Proofs),
		CompletedBy:        httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey:     "tasks-workflow-complete:" + clientKey,
		RequestFingerprint: stableHash("tasks-workflow-complete", canonical),
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, writeResponse(result))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func cardDTO(card domain.WorkflowCard) workflowCardDTO {
	actionsDone := card.ActionsDone
	actionsTotal := card.ActionsTotal
	nextAction := card.NextAction
	// The internal sign-off row is never the operator's next step. (The old cap of two death
	// steps is gone: death follows the SOP, which may author more than the seeded pair --
	// maintainer decision 1, 2026-09-16. A legacy card that counted its internal approval is
	// normalized from the served steps in cardDTOWithActions.)
	if card.TemplateKey == domain.TemplateKeyDeath && nextAction != nil && nextAction.Key == domain.ActionKeyParkHeadSignoff {
		nextAction = nil
	}
	return workflowCardDTO{
		WorkflowID:           card.WorkflowID,
		Module:               card.Module,
		TemplateKey:          card.TemplateKey,
		TemplateLabel:        templateLabelFor(card),
		Subject:              card.Subject,
		EventAt:              card.EventAt,
		EventDate:            card.EventDate,
		ParkLabel:            card.ParkLabel,
		ShedLabel:            card.ShedLabel,
		ActionsDone:          actionsDone,
		ActionsTotal:         actionsTotal,
		NextAction:           nextAction,
		AwaitingVerification: card.AwaitingVerification,
		State:                card.State,
	}
}

// cardDTOWithActions serves the detail header with the operator counters derived from the
// served steps (approval rows excluded, canceled excluded, recorded = completed or awaiting a
// verdict), so a card written before the internal approval left the operator count reads the
// same as one written after, whatever number of steps the SOP authored.
// templateLabelFor is the card's operator-facing kind: the herd-operations label by template
// key, or the authored SOP name of a general run.
func templateLabelFor(card domain.WorkflowCard) string {
	if card.SOPName != "" {
		return card.SOPName
	}
	return domain.TemplateLabel(card.TemplateKey)
}

func cardDTOWithActions(card domain.WorkflowCard, actions []domain.WorkflowAction) workflowCardDTO {
	out := cardDTO(card)
	if len(actions) == 0 {
		return out
	}
	total, done := 0, 0
	for _, a := range actions {
		if a.ActionType == domain.ActionTypeApproval || a.Status == domain.ActionStatusCanceled || a.Status == domain.ActionStatusSkipped {
			continue
		}
		total++
		if a.Status == domain.ActionStatusCompleted || a.Status == domain.ActionStatusInReview {
			done++
		}
	}
	out.ActionsTotal, out.ActionsDone = total, done
	return out
}

func actionDTO(templateKey string, a domain.WorkflowAction, siblings []domain.WorkflowAction, now time.Time) workflowActionDTO {
	verificationStatus := ""
	switch a.Status {
	case domain.ActionStatusInReview:
		verificationStatus = "pending"
	case domain.ActionStatusRework:
		verificationStatus = "rework"
	case domain.ActionStatusCompleted:
		if a.ActionType == domain.ActionTypeApproval {
			verificationStatus = "approved"
		}
	}
	blockedReason := ""
	switch {
	case domain.ActionTimeBlocked(a, now):
		blockedReason = "not_yet_due"
	case domain.AnswerGateUnresolved(a, siblings):
		// The branch condition is not decided yet: the question it hangs on is unanswered.
		blockedReason = "awaiting_answer"
	case domain.OperatorActionBlocked(templateKey, a, siblings):
		blockedReason = "previous_action"
	case domain.SignoffBlocked(a, siblings):
		blockedReason = "signoff"
	}
	return workflowActionDTO{
		ActionID:           a.ActionID,
		ActionKey:          a.ActionKey,
		Seq:                a.Seq,
		Section:            a.Section,
		ActionType:         a.ActionType,
		Title:              a.Title,
		Detail:             a.Detail,
		RequiresVideo:      a.RequiresVideo,
		Options:            a.Options,
		DueAt:              a.DueAt,
		Status:             a.Status,
		Blocked:            blockedReason != "",
		BlockedReason:      blockedReason,
		BranchNote:         domain.BranchNote(a, siblings),
		AnswerValue:        a.AnswerValue,
		ProofRef:           a.ProofRef,
		TaskType:           a.TaskType,
		AnswerType:         answerTypeForDTO(a),
		ProofMinVideos:     proofMinVideosForDTO(a),
		ProofMinPhotos:     a.ProofMinPhotos,
		ProofRefs:          proofItemsToDTO(a.ProofRefs),
		ReworkReason:       strings.TrimSpace(ptrString(a.ReworkReason)),
		CompletedByLabel:   "", // operator display resolution is a follow-up; the id is not UI copy
		CompletedAt:        a.CompletedAt,
		VerificationStatus: verificationStatus,
	}
}

// answerTypeForDTO reports the step's answer kind, deriving it for rows stamped before the SOP
// attributes existed so the phone never sees an empty kind.
func answerTypeForDTO(a domain.WorkflowAction) string {
	if a.AnswerType != "" && a.AnswerType != domain.AnswerKindNone {
		return a.AnswerType
	}
	switch {
	case a.ActionType == domain.ActionTypeQuestionSelect:
		return domain.AnswerKindSelect
	case a.HasHook(domain.EngineHookWeighKg):
		return domain.AnswerKindNumber
	case a.HasHook(domain.EngineHookRecordPen):
		return domain.AnswerKindText
	case a.ActionType == domain.ActionTypeQuestion:
		return domain.AnswerKindYesNo
	}
	return domain.AnswerKindNone
}

func proofMinVideosForDTO(a domain.WorkflowAction) int {
	if a.ProofMinVideos == 0 && a.RequiresVideo {
		return 1
	}
	return a.ProofMinVideos
}

func writeResponse(result domain.ActionWriteResult) workflowActionWriteResponse {
	return workflowActionWriteResponse{
		WorkflowID:           result.Workflow.WorkflowID,
		ActionID:             result.Action.ActionID,
		Status:               result.Action.Status,
		WorkflowState:        result.Workflow.State,
		ActionsDone:          result.Workflow.ActionsDone,
		ActionsTotal:         result.Workflow.ActionsTotal,
		AwaitingVerification: result.Workflow.AwaitingVerification,
		CompletedAt:          result.Action.CompletedAt,
		IdempotentReplay:     result.Replayed,
	}
}

// authorizeWorkflowWrite re-authorizes a shared /app/workflows write by the workflow's OWN
// module. The route table ORs CountsWrite with WorkInstructionsExecute because the two modules
// share these routes, so the route gate alone proves only that the caller holds ONE of them;
// which one must match the workflow. It always reads the workflow first: a caller must not learn
// from the status code alone whether an id it cannot open exists.
func (h *Handler) authorizeWorkflowWrite(w http.ResponseWriter, r *http.Request, tenantID, workflowID string) bool {
	detail, err := h.svc.GetWorkflow(r.Context(), tenantID, workflowID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return false
	}
	if !hasModulePermission(r.Context(), detail.Card.Module) {
		h.writeError(w, r, http.StatusForbidden, "permission_denied", "permission denied", nil)
		return false
	}
	return true
}

// hasModulePermission is THE per-module gate behind the shared workflow routes. A general
// (work-instruction) run is opened, listed, answered and completed on WorkInstructionsExecute
// alone; every herd-operations module (birth, death, shifting, reconcile, and the colostrum lens
// over birth) on CountsWrite alone. Holding the other module's permission never carries across:
// a counts-only tick must not reach a general run and a work-instructions-only tick must not
// reach a birth (PR #308 review, 2026-09-19). Module is the workflow's stored module, or the
// list route's module keyword.
func hasModulePermission(ctx context.Context, module string) bool {
	if module == domain.ModuleGeneral {
		return hasWorkflowPermission(ctx, permissions.WorkInstructionsExecute)
	}
	return hasWorkflowPermission(ctx, permissions.CountsWrite)
}

func hasWorkflowPermission(ctx context.Context, permission string) bool {
	if perms, ok := httpmiddleware.PersonPermissionsFromContext(ctx); ok {
		for _, p := range perms {
			if p == permission {
				return true
			}
		}
		return false
	}
	for _, grant := range httpmiddleware.AuthGrantsFromContext(ctx) {
		if permissions.RoleHasPermission(grant.Role, permission) {
			return true
		}
	}
	return false
}

func (h *Handler) writePreamble(w http.ResponseWriter, r *http.Request) (tenantID, workflowID, actionID, clientKey string, ok bool) {
	tenantID = httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return "", "", "", "", false
	}
	workflowID = strings.TrimSpace(r.PathValue("workflow_id"))
	actionID = strings.TrimSpace(r.PathValue("action_id"))
	if workflowID == "" || actionID == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_path_params", "workflow_id and action_id are required", nil)
		return "", "", "", "", false
	}
	clientKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if clientKey == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", nil)
		return "", "", "", "", false
	}
	if len(clientKey) < 8 || len(clientKey) > 200 {
		h.writeError(w, r, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters", nil)
		return "", "", "", "", false
	}
	return tenantID, workflowID, actionID, clientKey, true
}

func (h *Handler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is too large or unreadable", err)
		return nil, false
	}
	return body, true
}

func decodeStrictJSON(raw []byte, dest any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func canonicalRequestBytes(tenantID, command, workflowID, actionID string, body any) ([]byte, error) {
	return json.Marshal(struct {
		TenantID   string `json:"tenant_id"`
		Command    string `json:"command"`
		WorkflowID string `json:"workflow_id"`
		ActionID   string `json:"action_id"`
		Body       any    `json:"body"`
	}{TenantID: tenantID, Command: command, WorkflowID: workflowID, ActionID: actionID, Body: body})
}

func stableHash(domainSeparator string, payload []byte) string {
	sum := sha256.New()
	_, _ = sum.Write([]byte(domainSeparator))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write(payload)
	return hex.EncodeToString(sum.Sum(nil))
}

// writeDomainError maps the tasks domain sentinels onto the HTTP contract. proof_required mirrors
// the shifting completion's 422 shape.
func (h *Handler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.writeError(w, r, http.StatusNotFound, "workflow_not_found",
			"workflow or action not found in this tenant", err)
	case errors.Is(err, domain.ErrProofRequired):
		h.writeError(w, r, http.StatusUnprocessableEntity, "proof_required",
			"this step needs its required proof (video and/or photo) before it can be completed", err)
	case errors.Is(err, domain.ErrPermanentIdentifierRequired):
		h.writeError(w, r, http.StatusUnprocessableEntity, "permanent_identifier_required",
			"scan or enter the permanent RFID before recording the Tag the kid video", err)
	case errors.Is(err, domain.ErrIdempotencyConflict):
		h.writeError(w, r, http.StatusConflict, "idempotency_conflict",
			"Idempotency-Key was reused with a different payload", err)
	case errors.Is(err, domain.ErrActionAlreadyCompleted):
		h.writeError(w, r, http.StatusConflict, "action_already_completed",
			"this action is already completed", err)
	case errors.Is(err, domain.ErrActionInReview):
		h.writeError(w, r, http.StatusConflict, "action_in_review",
			"this action is awaiting verification and cannot be changed", err)
	case errors.Is(err, domain.ErrActionOutOfSequence):
		h.writeError(w, r, http.StatusConflict, "action_out_of_sequence",
			"complete the previous action before starting this one", err)
	case errors.Is(err, domain.ErrActionNotYetDue):
		h.writeError(w, r, http.StatusConflict, "action_not_yet_due",
			"this step is not open yet; it unlocks at its scheduled time", err)
	case errors.Is(err, domain.ErrActionNotAnswerable):
		h.writeError(w, r, http.StatusBadRequest, "action_not_answerable",
			"this action is not a question; use the complete endpoint", err)
	case errors.Is(err, domain.ErrActionNotCompletable):
		h.writeError(w, r, http.StatusBadRequest, "action_not_completable",
			"this action cannot be completed directly", err)
	case errors.Is(err, domain.ErrInvalidAnswer):
		h.writeError(w, r, http.StatusBadRequest, "invalid_answer",
			"answer_value is required and must be one of the allowed options", err)
	case errors.Is(err, domain.ErrInvalidCursor):
		h.writeError(w, r, http.StatusBadRequest, "invalid_cursor", "cursor is malformed", err)
	case errors.Is(err, domain.ErrMissingRequiredField):
		h.writeError(w, r, http.StatusBadRequest, "missing_required_field", "a required field is missing", err)
	default:
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	traceID := httpmiddleware.TraceIDFromContext(r.Context())
	if traceID == "" {
		traceID = "missing-trace"
	}
	httpresponse.WriteError(w, r, h.log, status, errorEnvelope{
		Code:      code,
		Message:   message,
		TraceID:   traceID,
		Retryable: status >= http.StatusInternalServerError,
	}, cause)
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
