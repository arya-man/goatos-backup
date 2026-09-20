package postgres

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// The source-state receipt is not a workflow authority. The shared workflow
// mutation still owns action state and card rollups, including reopening a card.
func TestAnimalPurchaseDecisionIncrementalSyncAndEventOrder(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprintf("decision_before_opener_%v", before), func(t *testing.T) {
			repo, pool, ctx := newWorkflowRepo(t)
			const loadID = "9a11a11a-0000-4000-8000-00000000d001"
			seedAnimalLoad(t, pool, ctx, loadID, "SYNC-DECISION", "Vendor", "CBE")
			svc := tasksapp.NewService(repo, nil)
			handler := tasksapp.NewAnimalPurchaseDecidedWorkflowHandler(svc)
			deliver := func(pending, decided int) {
				t.Helper()
				body, _ := json.Marshal(map[string]any{"load_id": loadID, "load_pending": pending, "load_accepted": decided})
				if err := handler.HandleEvent(ctx, eventbus.Event{Type: tasksapp.EventAnimalPurchaseDecided, TenantID: wfTenant, Payload: body, OccurredAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			// Last decision can be delivered before the workflow opener.
			if before {
				deliver(0, 1)
			}
			body, _ := json.Marshal(map[string]any{"load_id": loadID})
			if err := tasksapp.NewAnimalPurchaseLoadRecordedWorkflowHandler(svc).HandleEvent(ctx, eventbus.Event{Type: tasksapp.EventAnimalPurchaseLoadRecorded, TenantID: wfTenant, Payload: body, OccurredAt: wfEventAt}); err != nil {
				t.Fatal(err)
			}
			workflowID, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, domain.TemplateKeyAnimalPurchaseIntake, loadID)
			if err != nil {
				t.Fatal(err)
			}
			check := func(want string, done int) {
				t.Helper()
				var status string
				var actualDone int
				if err := pool.QueryRow(ctx, `SELECT a.status,w.actions_done FROM workflow_actions a JOIN workflow_instances w USING(tenant_id,workflow_id) WHERE a.tenant_id=$1::uuid AND a.workflow_id=$2::uuid AND a.action_key='decision'`, wfTenant, workflowID).Scan(&status, &actualDone); err != nil {
					t.Fatal(err)
				}
				if status != want || actualDone != done {
					t.Fatalf("decision status=%s done=%d, want %s/%d", status, actualDone, want, done)
				}
			}
			if !before {
				deliver(0, 1)
			}
			check(domain.ActionStatusCompleted, 1)
			// A second offline animal syncs later. Reopen both action and card rollup.
			deliver(1, 1)
			check(domain.ActionStatusPending, 0)
			// Old final-decision and load-open replays must not re-complete it.
			deliver(0, 1)
			check(domain.ActionStatusPending, 0)
			deliver(1, 0)
			check(domain.ActionStatusPending, 0)
			deliver(0, 2)
			check(domain.ActionStatusCompleted, 1)
			// Candidate event delivered after the decision is stale; leave it completed.
			deliver(1, 1)
			check(domain.ActionStatusCompleted, 1)

			// Concurrent workers process stale and current events. All paths lock the
			// workflow before reading the latest receipt, so the largest revision wins.
			states := [][2]int{{0, 1}, {1, 1}, {0, 2}, {1, 2}, {0, 3}, {1, 3}}
			errors := make(chan error, len(states))
			for _, state := range states {
				go func(pending, decided int) {
					body, _ := json.Marshal(map[string]any{"load_id": loadID, "load_pending": pending, "load_accepted": decided})
					errors <- handler.HandleEvent(ctx, eventbus.Event{Type: tasksapp.EventAnimalPurchaseDecided, TenantID: wfTenant, Payload: body, OccurredAt: time.Now()})
				}(state[0], state[1])
			}
			for range states {
				if err := <-errors; err != nil {
					t.Fatal(err)
				}
			}
			check(domain.ActionStatusPending, 0)

		})
	}
}
