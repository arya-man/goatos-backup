package postgres

import (
	"context"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

func TestSM4SweeperBatchesByScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.sweep", Name: "Sweep", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "phc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	const cpt = "00000000-0000-4000-8000-000000003002" // park
	ins := func(target string, day int, key string) {
		_, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "park", TargetID: target, ScopeType: "park", ScopeID: target,
			DueAt: time.Date(2026, 8, day, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert %s: applied=%v err=%v", key, applied, err)
		}
	}
	ins(cbePark, 1, "o1")
	ins(cbePark, 2, "o2")
	ins(cpt, 1, "o3")

	sweep := oblapp.NewSweeperService(repo)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.Batches != 2 { // cbe + cpt
		t.Fatalf("batches: want 2, got %d", res.Batches)
	}
	if res.Obligations != 3 {
		t.Fatalf("obligations attached: want 3, got %d", res.Obligations)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE protocol_version_id=$1 AND batch_id IS NOT NULL`, versionID); got != 3 {
		t.Fatalf("expected 3 batched obligations, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE protocol_version_id=$1`, versionID); got != 2 {
		t.Fatalf("expected 2 batches, got %d", got)
	}

	res2, err := sweep.SweepVersion(ctx, tenantID, versionID, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("re-sweep: %v", err)
	}
	if res2.Batches != 0 {
		t.Fatalf("re-sweep should create 0 batches, got %d", res2.Batches)
	}
}
