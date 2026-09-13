package countsbridge

import (
	"context"
	"time"

	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
)

// ReconcileWorkflowEngine adapts the tasks engine to counts/app.PenReconciliationWorkflowEngine
// (SOP-driven reconcile, docs/decisions/sop-driven-herd-operations.md). Composition-time glue only.
type ReconcileWorkflowEngine struct {
	tasks *tasksapp.Service
}

// NewReconcileWorkflowEngine wraps the tasks service.
func NewReconcileWorkflowEngine(tasks *tasksapp.Service) *ReconcileWorkflowEngine {
	return &ReconcileWorkflowEngine{tasks: tasks}
}

// OpenSubjectWorkflow opens (or finds) the card's questionnaire workflow.
func (e *ReconcileWorkflowEngine) OpenSubjectWorkflow(ctx context.Context, tenantID, templateKey, subjectRefID, subjectGoatID string, eventAt time.Time, parkID, shedID string) (string, error) {
	return e.tasks.OpenSubjectWorkflow(ctx, tasksapp.OpenSubjectWorkflowInput{
		TenantID:      tenantID,
		TemplateKey:   templateKey,
		SubjectRefID:  subjectRefID,
		SubjectGoatID: subjectGoatID,
		EventAt:       eventAt,
		ParkID:        parkID,
		ShedID:        shedID,
	})
}
