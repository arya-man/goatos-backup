package postgres

import (
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

func TestPurchaseRepeatedHooksReconcileEveryAction(t *testing.T) {
	for _, animal := range []bool{true, false} {
		name := "feed"
		if animal {
			name = "animal"
		}
		t.Run(name, func(t *testing.T) {
			repo, pool, ctx := newWorkflowRepo(t)
			const subject = "9a11a11a-0000-4000-8000-00000000d003"
			template, hook := domain.TemplateKeyFeedPurchaseIntake, domain.EngineHookFeedPurchaseReached
			if animal {
				template, hook = domain.TemplateKeyAnimalPurchaseIntake, domain.EngineHookAnimalPurchaseDecision
				seedAnimalLoad(t, pool, ctx, subject, "REPEAT-HOOK", "Vendor", "CBE")
			} else {
				seedFeedLoad(t, pool, ctx, subject, "Maize", "Vendor", "CBE", 43)
			}
			workflowID := openPurchase(t, repo, ctx, template, subject, wfEventAt, nil)
			// The author can use the same task type at multiple positions. Copy the
			// compiled type/proof/role contract exactly; keys and sequence are distinct.
			if _, err := pool.Exec(ctx, `INSERT INTO workflow_actions (
 tenant_id,workflow_id,action_key,seq,section,action_type,title,detail,requires_video,options,due_at,
 task_type,answer_type,engine_hook,proof_min_videos,proof_min_photos,hard_time_gate,wait_for_all,
 requires_keys,after_action_key,after_offset_seconds,answer_gate,owner_role)
 SELECT tenant_id,workflow_id,'repeated_hook',99,section,action_type,title,detail,requires_video,options,due_at,
 task_type,answer_type,engine_hook,proof_min_videos,proof_min_photos,hard_time_gate,wait_for_all,
 requires_keys,after_action_key,after_offset_seconds,answer_gate,owner_role
 FROM workflow_actions WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid AND engine_hook=$3`, wfTenant, workflowID, hook); err != nil {
				t.Fatal(err)
			}
			svc := tasksapp.NewService(repo, nil)
			deliver := func(pending, decided int) {
				t.Helper()
				body, _ := json.Marshal(map[string]any{"load_id": subject, "feed_purchase_id": subject, "load_pending": pending, "load_accepted": decided})
				event := eventbus.Event{TenantID: wfTenant, Payload: body, OccurredAt: wfEventAt}
				var err error
				if animal {
					err = tasksapp.NewAnimalPurchaseDecidedWorkflowHandler(svc).HandleEvent(ctx, event)
				} else {
					err = tasksapp.NewFeedPurchaseReachedWorkflowHandler(svc).HandleEvent(ctx, event)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			check := func(want int) {
				t.Helper()
				var completed int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_actions WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid AND engine_hook=$3 AND status='completed'`, wfTenant, workflowID, hook).Scan(&completed); err != nil {
					t.Fatal(err)
				}
				if completed != want {
					t.Fatalf("completed=%d want %d", completed, want)
				}
			}
			deliver(0, 1)
			check(2)
			deliver(0, 1)
			check(2)
			if animal {
				deliver(1, 1)
				check(0)
				deliver(0, 2)
				check(2)
			}
		})
	}
}
