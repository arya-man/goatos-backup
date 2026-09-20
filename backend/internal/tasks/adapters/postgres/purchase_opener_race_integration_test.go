package postgres

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

func TestPurchaseWorkflowOpenerRacesBusinessFact(t *testing.T) {
	for _, animal := range []bool{true, false} {
		name := "feed"
		if animal {
			name = "animal"
		}
		t.Run(name, func(t *testing.T) {
			repo, pool, ctx := newWorkflowRepo(t)
			svc := tasksapp.NewService(repo, nil)
			for i := 0; i < 5; i++ {
				subject := fmt.Sprintf("9a11a11a-0000-4000-8000-%012d", 7000+i)
				template, hook := domain.TemplateKeyFeedPurchaseIntake, domain.EngineHookFeedPurchaseReached
				if animal {
					template, hook = domain.TemplateKeyAnimalPurchaseIntake, domain.EngineHookAnimalPurchaseDecision
					seedAnimalLoad(t, pool, ctx, subject, fmt.Sprintf("RACE-%d", i), "Vendor", "CBE")
				} else {
					seedFeedLoad(t, pool, ctx, subject, "Maize", "Vendor", "CBE", 60+i)
				}
				body, _ := json.Marshal(map[string]any{"load_id": subject, "feed_purchase_id": subject, "load_pending": 0, "load_accepted": 1})
				event := eventbus.Event{TenantID: wfTenant, Payload: body, OccurredAt: wfEventAt}
				start := make(chan struct{})
				results := make(chan error, 2)
				go func() {
					<-start
					if animal {
						results <- tasksapp.NewAnimalPurchaseLoadRecordedWorkflowHandler(svc).HandleEvent(ctx, event)
					} else {
						results <- tasksapp.NewFeedPurchaseRecordedWorkflowHandler(svc).HandleEvent(ctx, event)
					}
				}()
				go func() {
					<-start
					if animal {
						results <- tasksapp.NewAnimalPurchaseDecidedWorkflowHandler(svc).HandleEvent(ctx, event)
					} else {
						results <- tasksapp.NewFeedPurchaseReachedWorkflowHandler(svc).HandleEvent(ctx, event)
					}
				}()
				close(start)
				for j := 0; j < 2; j++ {
					if err := <-results; err != nil {
						t.Fatal(err)
					}
				}
				var count int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_actions a JOIN workflow_instances w USING(tenant_id,workflow_id) WHERE w.tenant_id=$1::uuid AND w.template_key=$2 AND w.subject_ref_id=$3::uuid AND a.engine_hook=$4 AND a.status='completed'`, wfTenant, template, subject, hook).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 1 {
					t.Fatalf("race %d completed=%d want 1", i, count)
				}
			}
		})
	}
}
