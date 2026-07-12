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
	CreateTasksForBatches(ctx context.Context, tenantID, sopVersionID, actorID string, tasks []sopdomain.BatchTaskRequest) (map[string]string, error)
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
var _ oblapp.BatchTaskCreator = (*Bridge)(nil)

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

// CreateTasksForBatches exposes a page-level task creation boundary to the sweeper using
// a single batch SQL operation, avoiding N+1. SOP task creation remains individually idempotent
// because each task carries its source batch id.
func (b *Bridge) CreateTasksForBatches(ctx context.Context, tenantID string, batches []oblapp.BatchTaskCreate) (map[string]string, error) {
	if len(batches) == 0 {
		return map[string]string{}, nil
	}

	// Convert obligation batches to SOP batch task requests for bulk create
	tasks := make([]sopdomain.BatchTaskRequest, len(batches))
	for i, batch := range batches {
		tasks[i] = sopdomain.BatchTaskRequest{
			BatchID:   batch.BatchID,
			TaskType:  batch.TaskType,
			Title:     batch.Title,
			ScopeType: batch.ScopeType,
			ScopeID:   batch.ScopeID,
		}
	}

	// Use bulk create for all tasks in one operation
	result, err := b.sop.CreateTasksForBatches(ctx, tenantID, batches[0].SOPVersionID, b.actorID, tasks)
	if err != nil {
		return nil, err
	}
	return result, nil
}
