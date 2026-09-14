package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// RECONCILE IS A SOP QUESTIONNAIRE (maintainer decision 2026-09-13,
// docs/decisions/sop-driven-herd-operations.md; migration 000311).
//
// The card is still the unit of work and the verifier still judges ONE item per card. What the
// operator does to close it is now the `reconcile` track of the published counts.reconcile SOP,
// executed as one tasks-engine workflow per card. Two seams, both outward-only:
//
//   - EnsureWorkflow: the phone asks for the card's questionnaire; the tasks engine opens it
//     from the SOP (idempotent on the card id) and the card remembers the workflow id.
//   - OnWorkflowCompleted: the tasks engine reports the last step done; this service runs the
//     SAME Complete path the legacy one-video route uses, with every proof the steps captured,
//     so verification enqueue, idempotency and recovery are one implementation.
//
// A verifier's rework reopens the workflow's proof steps from the TASKS side
// (tasksapp.SubjectWorkflowVerdictHandler, keyed on the card id as subject_ref_id), so every bus
// process reopens alike; a legacy one-video card has no workflow and keeps behaving as before.

// PenReconciliationWorkflowEngine is the tasks-engine seam this service talks to.
type PenReconciliationWorkflowEngine interface {
	OpenSubjectWorkflow(ctx context.Context, tenantID, templateKey, subjectRefID, subjectGoatID string, eventAt time.Time, parkID, shedID string) (string, error)
}

// WithWorkflowEngine wires the tasks engine.
func (s *PenReconciliationService) WithWorkflowEngine(engine PenReconciliationWorkflowEngine) *PenReconciliationService {
	s.engine = engine
	return s
}

// ErrPenReconciliationWorkflowEngineNotWired is a composition bug surfaced loudly.
var ErrPenReconciliationWorkflowEngineNotWired = errors.New("counts: pen reconciliation workflow engine is not wired")

// EnsureWorkflow returns the card's questionnaire workflow id, opening it on first call.
func (s *PenReconciliationService) EnsureWorkflow(ctx context.Context, tenantID, cardID string) (string, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(cardID) == "" {
		return "", ErrMissingRequiredField
	}
	if s.engine == nil {
		return "", ErrPenReconciliationWorkflowEngineNotWired
	}
	facts, err := s.repo.PenReconciliationCardForWorkflow(ctx, tenantID, cardID)
	if err != nil {
		return "", err
	}
	if facts.WorkflowID != "" {
		return facts.WorkflowID, nil
	}
	if facts.Status != domain.PenReconciliationStatusOpen && facts.Status != domain.PenReconciliationStatusRework {
		return "", ports.ErrPenReconciliationNotActionable
	}
	workflowID, err := s.engine.OpenSubjectWorkflow(ctx, tenantID, tasksdomain.TemplateKeyReconcile, cardID, facts.GoatID, facts.RaisedAt, facts.ParkID, facts.RegisteredShedID)
	if err != nil {
		return "", err
	}
	if err := s.repo.SetPenReconciliationWorkflow(ctx, tenantID, cardID, workflowID); err != nil {
		return "", err
	}
	return workflowID, nil
}

// OnWorkflowCompleted implements tasks/app.WorkflowCompletionHook for the reconcile template.
func (s *PenReconciliationService) OnWorkflowCompleted(ctx context.Context, workflow tasksdomain.WorkflowInstance, actions []tasksdomain.WorkflowAction) error {
	cardID, err := s.repo.PenReconciliationCardIDByWorkflow(ctx, workflow.TenantID, workflow.WorkflowID)
	if err != nil {
		return err
	}
	var proofs []string
	kinds := map[string]string{}
	operator := ""
	for _, a := range actions {
		if a.ActionType == tasksdomain.ActionTypeApproval || a.Status != tasksdomain.ActionStatusCompleted {
			continue
		}
		for _, item := range a.ProofRefs {
			kinds[item.Ref] = item.Kind
		}
		proofs = append(proofs, a.AllProofRefs()...)
		if a.CompletedBy != nil && *a.CompletedBy != "" {
			operator = *a.CompletedBy
		}
	}
	if len(proofs) == 0 {
		return ports.ErrPenReconciliationProofRequired
	}
	sum := sha256.Sum256([]byte(strings.Join(proofs, "|")))
	fingerprint := hex.EncodeToString(sum[:])
	_, _, err = s.Complete(ctx, CompletePenReconciliationInput{
		TenantID:          workflow.TenantID,
		CardID:            cardID,
		CompletedByUserID: operator,
		ProofRef:          proofs[0],
		ProofRefs:         proofs,
		ProofKinds:        kinds,
		// Keyed on the workflow + the exact proof set: an exact replay of the completing step
		// collapses; a re-shoot after rework mints a new completion.
		IdempotencyKey:     "counts-pen-reconciliation-workflow:" + workflow.WorkflowID + ":" + fingerprint[:16],
		RequestFingerprint: fingerprint,
	})
	return err
}
