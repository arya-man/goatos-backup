package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

// TestSM5cCompletionFlow drives the SM-5 verification + completion + consume path end-to-end:
// generate -> sweep+reserve -> accept (completes obligation + consumes a dose) -> double-submit
// no-op -> reject (no completion, no consume). Stock is conserved throughout.
func TestSM5cCompletionFlow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	// Stock: 10 doses on hand, none reserved.
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-C', 'Completion test', 'vaccine', 'dose')`, impItem, impTenant); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', DATE '2026-12-31')`, impLot, impTenant, impItem, impCbe); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	// Published version: K1, one birth_age rule.
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.complete", Name: "Complete", Category: "vaccination", Status: "draft",
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

	const g1 = "30000000-0000-4000-8000-0000000000c1"
	const g2 = "30000000-0000-4000-8000-0000000000c2"
	seedGenGoat(t, ctx, pool, g1, "alive")
	seedGenGoat(t, ctx, pool, g2, "alive")

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	if res, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf); err != nil || res.Generated != 2 {
		t.Fatalf("generate: res=%+v err=%v", res, err)
	}

	// Sweep into a drive batch + reserve 2 doses (one per goat, same park scope).
	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(obl, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: impItem, DosesPerGoat: 1}
	dueBefore := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if sres, err := sweep.SweepVersion(ctx, impTenant, versionID, cfg, dueBefore); err != nil || sres.Batches != 1 {
		t.Fatalf("sweep: res=%+v err=%v", sres, err)
	}
	if got := reservedQty(t, ctx, pool); got != "2" {
		t.Fatalf("after reserve: want reserved 2, got %q", got)
	}

	batchID := scanText(t, ctx, pool, `SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 LIMIT 1`, impTenant)
	ob1 := scanText(t, ctx, pool, `SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, g1)
	ob2 := scanText(t, ctx, pool, `SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, g2)

	completion := vaccapp.NewCompletionService(vaccapp.NewService(vacc), obl, reserver)
	doses := int32(1)
	mk := func(ob, goat, key string) vaccdomain.NewCompletion {
		batch, lot := batchID, impLot
		return vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: ob, GoatID: goat, BatchID: &batch,
			VaccineInventoryLotID: &lot, Doses: &doses, RouteSite: "SC", AdministeredAt: asOf,
			IdempotencyKey: key,
		}
	}

	// Accept g1: completion accepted + obligation completed + 1 dose consumed.
	ar, err := completion.Accept(ctx, vaccapp.AcceptInput{
		Completion: mk(ob1, g1, "rec-g1"), ItemID: impItem, LocationID: impCbe,
	})
	if err != nil || !ar.Applied || !ar.Completed {
		t.Fatalf("accept g1: %+v err=%v", ar, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, ob1); got != "completed" {
		t.Fatalf("ob1 status: want completed, got %s", got)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND goat_id=$2 AND status='accepted'`, impTenant, g1); got != 1 {
		t.Fatalf("want 1 accepted completion for g1, got %d", got)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='consume'`, impTenant); got != 1 {
		t.Fatalf("want 1 consume movement, got %d", got)
	}
	if in, res := onHand(t, ctx, pool); in != "9" || res != "1" {
		t.Fatalf("after consume: want in_stock 9 / reserved 1, got %s / %s", in, res)
	}

	// Double-submit g1 (same idempotency key): no-op — no second completion/consume, balances stable.
	ar2, err := completion.Accept(ctx, vaccapp.AcceptInput{
		Completion: mk(ob1, g1, "rec-g1"), ItemID: impItem, LocationID: impCbe,
	})
	if err != nil || ar2.Applied {
		t.Fatalf("double-submit should be a no-op: %+v err=%v", ar2, err)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='consume'`, impTenant); got != 1 {
		t.Fatalf("double-submit added a consume movement, count=%d", got)
	}
	if in, res := onHand(t, ctx, pool); in != "9" || res != "1" {
		t.Fatalf("double-submit changed balances: in %s / reserved %s", in, res)
	}

	// Reject g2: no completion accepted, obligation stays open, no consume.
	rr, err := completion.Reject(ctx, vaccapp.RejectInput{
		Completion: mk(ob2, g2, "rec-g2"), Reason: "blurry proof",
	})
	if err != nil || !rr.Applied {
		t.Fatalf("reject g2: %+v err=%v", rr, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, ob2); got == "completed" {
		t.Fatalf("ob2 must not be completed after reject")
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='consume'`, impTenant); got != 1 {
		t.Fatalf("reject must not consume, consume count=%d", got)
	}
	if in, res := onHand(t, ctx, pool); in != "9" || res != "1" {
		t.Fatalf("reject changed balances: in %s / reserved %s", in, res)
	}
}

func scanText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx, sql, args...).Scan(&s); err != nil {
		t.Fatalf("scan %q: %v", sql, err)
	}
	return s
}

func reservedQty(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	return scanText(t, ctx, pool, `SELECT quantity_reserved::text FROM inventory_stock WHERE stock_id=$1`, impLot)
}

func onHand(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (inStock, reserved string) {
	t.Helper()
	if err := pool.QueryRow(ctx,
		`SELECT quantity_in_stock::text, quantity_reserved::text FROM inventory_stock WHERE stock_id=$1`, impLot).
		Scan(&inStock, &reserved); err != nil {
		t.Fatalf("onHand: %v", err)
	}
	return inStock, reserved
}
