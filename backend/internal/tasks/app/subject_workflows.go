package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// SOP-DRIVEN SUBJECT WORKFLOWS (maintainer decision 2026-09-13,
// docs/decisions/sop-driven-herd-operations.md).
//
// Reconcile and shifting completion are questionnaires authored on the web, executed as ONE
// workflow instance per subject (the reconcile card, the shifting event). The owning module keeps
// its canonical row and its verification/approval semantics; the tasks engine only runs the
// operator's steps and reports back ONCE when the last step is done, through a per-template
// completion hook the owning module registers at composition time -- the same outward-only,
// receipt-backed shape the death verification bridge uses. There is deliberately no second
// apply path: the module's existing completion service receives the aggregated proofs.

// WorkflowCompletionHook is what a module registers to be told a workflow finished.
type WorkflowCompletionHook interface {
	// OnWorkflowCompleted receives the finished workflow and its steps. It must be idempotent:
	// the engine calls it from the request that completed the last step, and an exact replay of
	// that request calls it again.
	OnWorkflowCompleted(ctx context.Context, workflow domain.WorkflowInstance, actions []domain.WorkflowAction) error
}

// WithCompletionHook registers the hook for one template key.
func (s *Service) WithCompletionHook(templateKey string, hook WorkflowCompletionHook) *Service {
	if s.completionHooks == nil {
		s.completionHooks = map[string]WorkflowCompletionHook{}
	}
	s.completionHooks[templateKey] = hook
	return s
}

// OpenSubjectWorkflowInput opens (or finds) the workflow keyed on a non-goat subject.
type OpenSubjectWorkflowInput struct {
	TenantID      string
	TemplateKey   string
	SubjectRefID  string
	SubjectGoatID string
	EventAt       time.Time
	ParkID        string
	ShedID        string
}

// OpenSubjectWorkflow returns the workflow id for the subject, opening it from the published SOP
// on first call. Idempotent on (template_key, subject_ref_id).
func (s *Service) OpenSubjectWorkflow(ctx context.Context, in OpenSubjectWorkflowInput) (string, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.TemplateKey) == "" ||
		strings.TrimSpace(in.SubjectRefID) == "" || strings.TrimSpace(in.SubjectGoatID) == "" {
		return "", domain.ErrMissingRequiredField
	}
	if id, err := s.repo.WorkflowIDBySubjectRef(ctx, in.TenantID, in.TemplateKey, in.SubjectRefID); err == nil {
		return id, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return "", err
	}
	eventAt := in.EventAt
	if eventAt.IsZero() {
		eventAt = s.now()
	}
	ref := in.SubjectRefID
	if _, err := s.repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
		TenantID:      in.TenantID,
		TemplateKey:   in.TemplateKey,
		SubjectGoatID: in.SubjectGoatID,
		EventAt:       eventAt,
		ParkID:        optionalUUID(in.ParkID),
		ShedID:        optionalUUID(in.ShedID),
		SubjectRefID:  &ref,
	}); err != nil {
		return "", err
	}
	return s.repo.WorkflowIDBySubjectRef(ctx, in.TenantID, in.TemplateKey, in.SubjectRefID)
}

// ReopenForRework sends the workflow's proof steps back after a verifier rejection.
func (s *Service) ReopenForRework(ctx context.Context, tenantID, workflowID, reason string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(workflowID) == "" {
		return domain.ErrMissingRequiredField
	}
	return s.repo.ReopenProofStepsForRework(ctx, tenantID, workflowID, reason)
}

// notifyCompletion runs the registered hook when a write left the workflow completed.
func (s *Service) notifyCompletion(ctx context.Context, tenantID string, result domain.ActionWriteResult) error {
	if result.Workflow.State != domain.WorkflowStateCompleted || s.completionHooks == nil {
		return nil
	}
	hook, ok := s.completionHooks[result.Workflow.TemplateKey]
	if !ok {
		return nil
	}
	detail, err := s.repo.GetWorkflow(ctx, tenantID, result.Workflow.WorkflowID, s.now())
	if err != nil {
		return err
	}
	return hook.OnWorkflowCompleted(ctx, result.Workflow, detail.Actions)
}

// SubjectWorkflowVerdictHandler reopens a SOP questionnaire's proof steps when the verifier
// sends the owning module's evidence back. It keys on the item's source ref (the card / event id
// IS the workflow's subject_ref_id), so it needs no module dependency, and it lives in the ONE
// shared registration (eventwiring.RegisterWorkflowConsumers) so every bus process behaves alike.
// Approvals are the owning module's business (the card completes); this handler ignores them.
type SubjectWorkflowVerdictHandler struct {
	svc *Service
}

// Subject-ref verdict routing: verification source (module, ref_type) -> workflow template key.
var subjectWorkflowVerdictRoutes = map[[2]string]string{
	// counts/domain.VerificationModulePenReconciliation / VerificationRefTypePenReconciliation.
	{"counts", "pen_reconciliation_card"}: domain.TemplateKeyReconcile,
}

// NewSubjectWorkflowVerdictHandler constructs the handler.
func NewSubjectWorkflowVerdictHandler(svc *Service) *SubjectWorkflowVerdictHandler {
	return &SubjectWorkflowVerdictHandler{svc: svc}
}

// Register subscribes the rework verdict.
func (h *SubjectWorkflowVerdictHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVerificationVerdictRework, h)
}

// HandleEvent reopens the matching workflow's proof steps.
func (h *SubjectWorkflowVerdictHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != EventVerificationVerdictRework {
		return nil
	}
	var p deathVerdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	templateKey, ok := subjectWorkflowVerdictRoutes[[2]string{p.Source.Module, p.Source.RefType}]
	if !ok {
		return nil
	}
	refID := strings.TrimSpace(p.Source.RefID)
	if refID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	workflowID, err := h.svc.repo.WorkflowIDBySubjectRef(ctx, e.TenantID, templateKey, refID)
	if errors.Is(err, domain.ErrNotFound) {
		// A legacy one-video card has no questionnaire; nothing to reopen.
		return nil
	}
	if err != nil {
		return err
	}
	return h.svc.ReopenForRework(ctx, e.TenantID, workflowID, strings.TrimSpace(p.Reason))
}

// CaptureReshootListener is told a birth report's rejected proof was re-shot (counts replaces it
// in the approval row's snapshot and sends the report back to Verify). Death needs no listener:
// its snapshot lives on the workflow and is replaced in the recording's own transaction.
type CaptureReshootListener interface {
	OnBirthCaptureReshot(ctx context.Context, tenantID, birthEventID string, index int, proof domain.ProofItem) error
}

// WithCaptureReshootListener wires the counts side of a birth report re-shoot.
func (s *Service) WithCaptureReshootListener(l CaptureReshootListener) *Service {
	s.reshootListener = l
	return s
}

// OpenBirthCaptureReshoot appends the re-shoot steps for a rejected birth report (decision 5).
// Idempotent per verdict recording key.
func (s *Service) OpenBirthCaptureReshoot(ctx context.Context, tenantID, birthEventID string, capture authored.Evidence, recordingKey, reason string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(birthEventID) == "" {
		return domain.ErrMissingRequiredField
	}
	if len(capture.Media) == 0 {
		return nil
	}
	workflowID, err := s.repo.BirthWorkflowIDForEvent(ctx, tenantID, birthEventID)
	if errors.Is(err, domain.ErrNotFound) {
		s.log.Warn("tasks_birth_capture_reshoot_unroutable", "tenant_id", tenantID, "birth_event_id", birthEventID)
		return nil
	}
	if err != nil {
		return err
	}
	return s.repo.AppendCaptureReshootSteps(ctx, tenantID, workflowID, capture, recordingKey, reason)
}

// notifyCaptureReshoot tells counts a birth report proof was re-shot. Derived from state, so an
// exact replay re-notifies and the listener's idempotent write heals a failed first attempt.
func (s *Service) notifyCaptureReshoot(ctx context.Context, tenantID string, result domain.ActionWriteResult) error {
	a := result.Action
	if !a.HasHook(domain.EngineHookReshootReport) || a.Status != domain.ActionStatusCompleted || !domain.ReviewedPerStep(result.Workflow.TemplateKey) {
		return nil
	}
	if result.Workflow.BirthEventID == nil || s.reshootListener == nil {
		return nil
	}
	idx, ok := domain.ReshootMediaIndex(a)
	if !ok || len(a.ProofRefs) == 0 {
		return nil
	}
	return s.reshootListener.OnBirthCaptureReshot(ctx, tenantID, *result.Workflow.BirthEventID, idx, a.ProofRefs[0])
}
