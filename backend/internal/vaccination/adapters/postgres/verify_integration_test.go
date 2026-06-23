package postgres

import (
	"context"
	"testing"
	"time"

	invpg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	invapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// TestSM5VerifyExistingTwoPhase drives the two-phase operator flow: a dose is RECORDED at submit
// time, then VERIFIED later. AcceptExisting completes the obligation + consumes a dose; RejectExisting
// leaves the obligation open and consumes nothing. Both are idempotent on replay.
func TestSM5VerifyExistingTwoPhase(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-V', 'Verify test', 'vaccine', 'dose')`, impItem, impTenant); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', DATE '2026-12-31')`, impLot, impTenant, impItem, impCbe); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.verify", Name: "Verify", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"stage":"K1"},` +
		`"source":{"source_system":"phc","source_ref":"PHC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "phc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	const g1 = "30000000-0000-4000-8000-0000000000f1"
	const g2 = "30000000-0000-4000-8000-0000000000f2"
	seedGenGoat(t, ctx, pool, g1, "alive")
	seedGenGoat(t, ctx, pool, g2, "alive")

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	if res, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf); err != nil || res.Generated != 2 {
		t.Fatalf("generate: res=%+v err=%v", res, err)
	}

	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(obl, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: impItem, DosesPerGoat: 1}
	if _, err := sweep.SweepVersion(ctx, impTenant, versionID, cfg, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	batchID := scanText(t, ctx, pool, `SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 LIMIT 1`, impTenant)
	ob1 := scanText(t, ctx, pool, `SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, g1)
	ob2 := scanText(t, ctx, pool, `SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, g2)

	svc := vaccapp.NewService(vacc)
	completion := vaccapp.NewCompletionService(svc, obl, reserver)
	doses := int32(1)
	record := func(ob, goat, key string) string {
		batch, lot := batchID, impLot
		cid, applied, err := svc.RecordCompletion(ctx, vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: ob, GoatID: goat, BatchID: &batch,
			VaccineInventoryLotID: &lot, Doses: &doses, RouteSite: "SC", AdministeredAt: asOf,
			Status: "recorded", IdempotencyKey: key,
		})
		if err != nil || !applied || cid == "" {
			t.Fatalf("record %s: cid=%q applied=%v err=%v", goat, cid, applied, err)
		}
		return cid
	}
	cid1 := record(ob1, g1, "sub-g1")
	cid2 := record(ob2, g2, "sub-g2")

	// Verify-accept g1: obligation completed + 1 dose consumed.
	ar, err := completion.AcceptExisting(ctx, vaccapp.AcceptExistingInput{TenantID: impTenant, CompletionID: cid1})
	if err != nil || !ar.Applied || !ar.Completed {
		t.Fatalf("accept-existing g1: %+v err=%v", ar, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, ob1); got != "completed" {
		t.Fatalf("ob1: want completed, got %s", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM vaccination_completions WHERE tenant_id=$1 AND completion_id=$2`, impTenant, cid1); got != "accepted" {
		t.Fatalf("completion g1: want accepted, got %s", got)
	}
	if in, res := onHand(t, ctx, pool); in != "9" || res != "1" {
		t.Fatalf("after accept: want in 9 / reserved 1, got %s / %s", in, res)
	}

	// Replay accept: no-op.
	if ar2, err := completion.AcceptExisting(ctx, vaccapp.AcceptExistingInput{TenantID: impTenant, CompletionID: cid1}); err != nil || ar2.Applied {
		t.Fatalf("accept-existing replay should be a no-op: %+v err=%v", ar2, err)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='consume'`, impTenant); got != 1 {
		t.Fatalf("replay added a consume movement, count=%d", got)
	}

	// Verify-reject g2: obligation stays open, nothing consumed.
	rr, err := completion.RejectExisting(ctx, impTenant, cid2, "blurry proof", nil)
	if err != nil || !rr.Applied {
		t.Fatalf("reject-existing g2: %+v err=%v", rr, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, ob2); got == "completed" {
		t.Fatalf("ob2 must not be completed after reject")
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM vaccination_completions WHERE tenant_id=$1 AND completion_id=$2`, impTenant, cid2); got != "rejected" {
		t.Fatalf("completion g2: want rejected, got %s", got)
	}
	if in, res := onHand(t, ctx, pool); in != "9" || res != "1" {
		t.Fatalf("after reject: balances changed: in %s / reserved %s", in, res)
	}
	// Replay reject: no-op.
	if rr2, err := completion.RejectExisting(ctx, impTenant, cid2, "blurry proof", nil); err != nil || rr2.Applied {
		t.Fatalf("reject-existing replay should be a no-op: %+v err=%v", rr2, err)
	}
}
