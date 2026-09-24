package postgres

import (
	"testing"
	"time"

	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/readcache/readcachetest"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// Completing the birth workflow's Record shed step places the kid (goats + goat_shed_partitions,
// read by the shared analytics cache) inside the action's own transaction. The eviction rides
// that transaction, so every instance -- the writer included, through its listener -- reads the
// placement within a second.
func TestRecordShedPlacementEvictsTheSharedReadCacheOnBothInstances(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const (
		park = "aaaaaaa1-0000-0000-0000-0000000000a1"
		shed = "aaaaaaa1-0000-0000-0000-0000000000a2"
	)
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($2::uuid, $1::uuid, 'park', 'Ryw Park', NULL, 'active'),
       ($3::uuid, $1::uuid, 'shed', 'Ryw Shed', $2::uuid, 'active')`, wfTenant, park, shed); err != nil {
		t.Fatalf("seed locations: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')`, wfTenant, shed); err != nil {
		t.Fatalf("seed pen: %v", err)
	}
	// Born in the park, not yet in a pen: exactly the case Record shed exists for.
	if _, err := pool.Exec(ctx, `UPDATE goats SET park_id = $2::uuid, current_location_id = $2::uuid WHERE goat_id = $1::uuid`, wfKid, park); err != nil {
		t.Fatalf("place kid in park: %v", err)
	}
	pair := readcachetest.NewPair(t, ctx, pool)
	repo = repo.WithIdentityTxWriter(identitypg.NewRepository(pool, 30*time.Second).WithReadCacheInvalidator(pair.Writer))

	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// The kid track is ordered: every step before Record shed is fixture, marked done directly.
	var seq int
	if err := pool.QueryRow(ctx, `SELECT seq FROM workflow_actions WHERE workflow_id = $1::uuid AND action_key = $2`, workflowID, domain.ActionKeyRecordShed).Scan(&seq); err != nil {
		t.Fatalf("record shed sequence: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_actions SET status = 'completed', completed_at = now(), proof_ref = 'seeded'
WHERE workflow_id = $1::uuid AND seq < $2`, workflowID, seq); err != nil {
		t.Fatalf("complete earlier steps: %v", err)
	}
	penLabel := "Part 1"
	pair.Check(t, ctx, "tasks Record shed (newborn placement)", wfTenant, []string{park}, false, func(t *testing.T) {
		if _, err := repo.AnswerAction(ctx, domain.AnswerActionCommand{
			TenantID: wfTenant, WorkflowID: workflowID, ActionID: actionIDByKey(t, detail, domain.ActionKeyRecordShed),
			AnswerValue: domain.FormatRecordedPenAnswer(shed, &penLabel), AnsweredAt: wfEventAt.UTC(),
			IdempotencyKey: "ryw-record-shed", RequestFingerprint: "ryw-record-shed", AnsweredBy: "aaaaaaa1-0000-0000-0000-0000000000a3",
		}); err != nil {
			t.Fatalf("AnswerAction record_shed: %v", err)
		}
	})
}
