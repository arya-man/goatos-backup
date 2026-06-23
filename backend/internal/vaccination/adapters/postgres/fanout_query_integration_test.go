package postgres

import (
	"context"
	"testing"
	"time"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

const (
	skeletonSOPID        = "b0000000-0000-4000-8000-000000000001" // seeded by migration 000075
	skeletonSOPVersionID = "b0000000-0000-4000-8000-000000000002"
)

// TestListRecordedCompletionsByTask checks the SOP verify fan-out source: it returns only the
// still-recorded completions captured under a task's submissions (accepted ones are excluded).
func TestListRecordedCompletionsByTask(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)
	svc := vaccapp.NewService(vacc)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.fanout", Name: "Fanout", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "phc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	const ga = "30000000-0000-4000-8000-0000000000a8"
	const gb = "30000000-0000-4000-8000-0000000000b8"
	seedGenGoat(t, ctx, pool, ga, "alive")
	seedGenGoat(t, ctx, pool, gb, "alive")

	mkObl := func(goat, key string) string {
		id, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goat, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("obligation %s: applied=%v err=%v", goat, applied, err)
		}
		return id
	}
	obA := mkObl(ga, "fo-a")
	obB := mkObl(gb, "fo-b")

	// SOP task + submission + one item per goat (reuses the seeded vaccination SOP skeleton).
	taskID := scanText(t, ctx, pool,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id)
		 VALUES ($1, $2, $3, 'vaccination_drive', 'Drive', 'park', $4) RETURNING task_id::text`,
		impTenant, skeletonSOPID, skeletonSOPVersionID, impCbe)
	subID := scanText(t, ctx, pool,
		`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers)
		 VALUES ($1, $2, $3, $4, 'fo-sub', '{}'::jsonb) RETURNING submission_id::text`,
		impTenant, taskID, skeletonSOPVersionID, impParty)
	itemA := scanText(t, ctx, pool,
		`INSERT INTO sop_submission_items (tenant_id, submission_id, task_id, goat_id, item_key, state)
		 VALUES ($1, $2, $3, $4, 'dose', 'needs_review') RETURNING item_id::text`,
		impTenant, subID, taskID, ga)
	itemB := scanText(t, ctx, pool,
		`INSERT INTO sop_submission_items (tenant_id, submission_id, task_id, goat_id, item_key, state)
		 VALUES ($1, $2, $3, $4, 'dose', 'accepted') RETURNING item_id::text`,
		impTenant, subID, taskID, gb)

	doses := int32(1)
	record := func(ob, goat, item, key string) string {
		it := item
		cid, applied, err := svc.RecordCompletion(ctx, vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: ob, GoatID: goat, SopSubmissionItemID: &it,
			Doses: &doses, RouteSite: "SC", AdministeredAt: time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
			Status: "recorded", IdempotencyKey: key,
		})
		if err != nil || !applied {
			t.Fatalf("record %s: applied=%v err=%v", goat, applied, err)
		}
		return cid
	}
	cidA := record(obA, ga, itemA, "fo-c-a")
	cidB := record(obB, gb, itemB, "fo-c-b")

	// Accept B so only A remains 'recorded'.
	if _, applied, err := vacc.AcceptCompletion(ctx, impTenant, cidB, nil, nil); err != nil || !applied {
		t.Fatalf("accept B: applied=%v err=%v", applied, err)
	}

	ids, err := vacc.ListRecordedCompletionsByTask(ctx, impTenant, taskID)
	if err != nil {
		t.Fatalf("list by task: %v", err)
	}
	if len(ids) != 1 || ids[0] != cidA {
		t.Fatalf("want only recorded completion %s, got %v", cidA, ids)
	}
}
