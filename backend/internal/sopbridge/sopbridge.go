// Package sopbridge wires the obligation sweeper's TaskCreator to the SOP module's CreateTask,
// without coupling the obligation module to SOP types. It is the composition-layer adapter used at
// service wiring (cmd) time. SOP task creation itself is covered by the SOP module's own tests.
package sopbridge

import (
	"context"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
)

// SOPTaskCreator is the slice of the SOP repo/service this bridge needs (satisfied by both).
type SOPTaskCreator interface {
	CreateTask(ctx context.Context, cmd sopports.CreateTaskCommand) (sopdomain.TaskSummary, error)
}

// Bridge adapts SOP CreateTask to the obligation sweeper's TaskCreator.
type Bridge struct {
	sop     SOPTaskCreator
	actorID string
}

// New constructs the bridge with the system actor id used for task audit.
func New(sop SOPTaskCreator, actorID string) *Bridge {
	return &Bridge{sop: sop, actorID: actorID}
}

var _ oblapp.TaskCreator = (*Bridge)(nil)

// CreateTaskForBatch spawns one SOP task for a batch from the given published sop_version.
func (b *Bridge) CreateTaskForBatch(ctx context.Context, tenantID, batchID, sopVersionID, taskType, title, scopeType, scopeID string) (string, error) {
	versionID := sopVersionID
	task, err := b.sop.CreateTask(ctx, sopports.CreateTaskCommand{
		TenantID: tenantID,
		ActorID:  b.actorID,
		Body: sopdomain.CreateTaskRequest{
			SOPVersionID: &versionID,
			TaskType:     taskType,
			Title:        title,
			ScopeType:    scopeType,
			ScopeID:      scopeID,
			Priority:     "normal",
			Context:      map[string]any{"created_by": "obligation-sweeper", "obligation_batch_id": batchID},
		},
	})
	if err != nil {
		return "", err
	}
	return task.TaskID, nil
}
