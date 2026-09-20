package postgres

import (
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

func TestFeedLifecycleEventsBeforeWorkflowOpen(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const purchaseID = "9a11a11a-0000-4000-8000-00000000d002"
	seedFeedLoad(t, pool, ctx, purchaseID, "Maize", "Vendor", "CBE", 42)
	svc := tasksapp.NewService(repo, nil)
	payload, _ := json.Marshal(map[string]string{"feed_purchase_id": purchaseID})
	event := eventbus.Event{TenantID: wfTenant, Payload: payload, OccurredAt: wfEventAt}
	if err := tasksapp.NewFeedPurchaseReachedWorkflowHandler(svc).HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := tasksapp.NewToxinTestAcceptedWorkflowHandler(svc).HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	// Purchase was originally recorded in transit. Later events may overtake its opener.
	if err := tasksapp.NewFeedPurchaseRecordedWorkflowHandler(svc).HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	workflowID, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, domain.TemplateKeyFeedPurchaseIntake, purchaseID)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"mark_reached", "toxin_test"} {
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM workflow_actions WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid AND action_key=$3`, wfTenant, workflowID, key).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != domain.ActionStatusCompleted {
			t.Errorf("%s status=%s, want completed", key, status)
		}
	}
	// The opener is replayable without duplicate workflows or completion regression.
	if err := tasksapp.NewFeedPurchaseRecordedWorkflowHandler(svc).HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
}
