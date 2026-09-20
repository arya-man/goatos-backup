package postgres

import (
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

func TestPurchaseHookFollowsAnswerBranchAndReanswer(t *testing.T) {
	for _, animal := range []bool{true, false} {
		name := "feed"
		if animal {
			name = "animal"
		}
		t.Run(name, func(t *testing.T) {
			repo, pool, ctx := newWorkflowRepo(t)
			const subject = "9a11a11a-0000-4000-8000-00000000d004"
			template, hook, question := domain.TemplateKeyFeedPurchaseIntake, domain.EngineHookFeedPurchaseReached, "weighbridge_slip"
			if animal {
				template, hook, question = domain.TemplateKeyAnimalPurchaseIntake, domain.EngineHookAnimalPurchaseDecision, "record_animals"
				seedAnimalLoad(t, pool, ctx, subject, "BRANCH-HOOK", "Vendor", "CBE")
			} else {
				seedFeedLoad(t, pool, ctx, subject, "Maize", "Vendor", "CBE", 44)
			}
			workflowID := openPurchase(t, repo, ctx, template, subject, wfEventAt, nil)
			var questionID string
			if err := pool.QueryRow(ctx, `UPDATE workflow_actions SET action_type='question',task_type='record_yes_no',answer_type='yes_no',requires_video=false,proof_min_photos=0,proof_min_videos=0,options='["yes","no"]'::jsonb WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid AND action_key=$3 RETURNING action_id::text`, wfTenant, workflowID, question).Scan(&questionID); err != nil {
				t.Fatal(err)
			}
			gate, _ := json.Marshal(map[string]any{"step": question, "op": "eq", "value": []string{"yes"}})
			if _, err := pool.Exec(ctx, `UPDATE workflow_actions SET answer_gate=$4::jsonb WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid AND engine_hook=$3`, wfTenant, workflowID, hook, gate); err != nil {
				t.Fatal(err)
			}
			svc := tasksapp.NewService(repo, nil)
			payload, _ := json.Marshal(map[string]any{"load_id": subject, "feed_purchase_id": subject, "load_pending": 0, "load_accepted": 1})
			event := eventbus.Event{TenantID: wfTenant, Payload: payload, OccurredAt: wfEventAt}
			if animal {
				if err := tasksapp.NewAnimalPurchaseDecidedWorkflowHandler(svc).HandleEvent(ctx, event); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := tasksapp.NewFeedPurchaseReachedWorkflowHandler(svc).HandleEvent(ctx, event); err != nil {
					t.Fatal(err)
				}
			}
			check := func(want string) {
				t.Helper()
				var status string
				if err := pool.QueryRow(ctx, `SELECT status FROM workflow_actions WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid AND engine_hook=$3`, wfTenant, workflowID, hook).Scan(&status); err != nil {
					t.Fatal(err)
				}
				if status != want {
					t.Fatalf("status=%s want %s", status, want)
				}
			}
			check(domain.ActionStatusPending)
			answer := func(value, key string) {
				t.Helper()
				if _, err := repo.AnswerAction(ctx, domain.AnswerActionCommand{TenantID: wfTenant, WorkflowID: workflowID, ActionID: questionID, AnswerValue: value, AnsweredAt: wfEventAt, IdempotencyKey: key, RequestFingerprint: key}); err != nil {
					t.Fatal(err)
				}
			}
			rework := func() {
				t.Helper()
				if _, err := pool.Exec(ctx, `UPDATE workflow_actions SET status='rework',idempotency_key=NULL,request_fingerprint=NULL,row_version=row_version+1 WHERE tenant_id=$1::uuid AND action_id=$2::uuid`, wfTenant, questionID); err != nil {
					t.Fatal(err)
				}
			}
			answer("no", "branch-no")
			check(domain.ActionStatusSkipped)
			rework()
			answer("yes", "branch-yes")
			check(domain.ActionStatusCompleted)
			// A recorded answer still drives the branch while its proof is reviewed
			// or sent for rework, matching the shared branch engine's semantics.
			for _, status := range []string{domain.ActionStatusInReview, domain.ActionStatusRework} {
				if _, err := pool.Exec(ctx, `UPDATE workflow_actions SET status=$3 WHERE tenant_id=$1::uuid AND action_id=$2::uuid`, wfTenant, questionID, status); err != nil {
					t.Fatal(err)
				}
				var err error
				if animal {
					err = tasksapp.NewAnimalPurchaseDecidedWorkflowHandler(svc).HandleEvent(ctx, event)
				} else {
					err = tasksapp.NewFeedPurchaseReachedWorkflowHandler(svc).HandleEvent(ctx, event)
				}
				if err != nil {
					t.Fatal(err)
				}
				check(domain.ActionStatusCompleted)
			}
			rework()
			answer("no", "branch-no-again")
			check(domain.ActionStatusSkipped)
		})
	}
}
