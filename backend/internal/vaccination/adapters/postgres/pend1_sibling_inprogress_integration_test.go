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

// TestAcceptCompletionAtomicFlipsOpenSiblingToInProgress proves the PEND-1 sibling in_progress
// trigger through the REAL production completion path: CompletionService.Accept ->
// RecordAndAcceptCompletionAtomic -> acceptCompletionInTx -> recomputeObligationBatchStatusOnComplete
// (this file's mirror of obligation.Repository.MarkCompleted's sibling UPDATE). This is the path
// every real Postgres-backed vaccination-drive completion actually takes in production --
// obligation.Repository.MarkCompleted's own copy of this same logic is NEVER reached for vaccination
// completions once the atomic adapter methods are implemented (see completion.go's Accept/
// AcceptExisting: the s.obl.MarkCompleted fallback only runs for non-Postgres fakes). Without this
// mirror, PEND-1 in_progress would remain unreachable for the dominant vaccination-drive flow even
// with obligation.Repository.MarkCompleted fixed.
func TestAcceptCompletionAtomicFlipsOpenSiblingToInProgress(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-PEND1-SIB', 'PEND1 sibling test', 'vaccine', 'dose')`, impItem, impTenant); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', DATE '2026-12-31')`, impLot, impTenant, impItem, impCbe); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.pend1sibling", Name: "PEND1 sibling", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"stage":"K1"},` +
		`"source":{"source_system":"pc","source_ref":"PC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	const g1 = "30000000-0000-4000-8000-0000000000d1"
	const g2 = "30000000-0000-4000-8000-0000000000d2"
	seedGenGoat(t, ctx, pool, g1, "alive")
	seedGenGoat(t, ctx, pool, g2, "alive")

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	if res, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf); err != nil || res.Generated != 2 {
		t.Fatalf("generate: res=%+v err=%v", res, err)
	}

	// Sweep both obligations into ONE drive batch + reserve 2 doses.
	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(obl, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: impItem, DosesPerGoat: 1}
	dueBefore := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if sres, err := sweep.SweepVersion(ctx, impTenant, versionID, cfg, dueBefore); err != nil || sres.Batches != 1 {
		t.Fatalf("sweep: res=%+v err=%v", sres, err)
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
			ColdChainVerified: true, IdempotencyKey: key,
		}
	}

	// --- Accept g1 through the REAL atomic production path. g2's obligation (ob2) is still
	// scheduled/due, so it must flip to in_progress in the SAME transaction as g1's completion.
	ar, err := completion.Accept(ctx, vaccapp.AcceptInput{Completion: mk(ob1, g1, "pend1-sib-g1")})
	if err != nil || !ar.Applied || !ar.Completed {
		t.Fatalf("accept g1: %+v err=%v", ar, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, ob1); got != "completed" {
		t.Fatalf("ob1 status = %s, want completed", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, impTenant, batchID); got != "in_progress" {
		t.Fatalf("batch status = %s, want in_progress (ob2 still open)", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, ob2); got != "in_progress" {
		t.Fatalf("ob2 (sibling) status = %s, want in_progress -- PEND-1 sibling trigger must fire through the real atomic accept path", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='in_progress'`,
		impTenant, ob2); got != 1 {
		t.Fatalf("ob2 in_progress event count = %d, want 1", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='obligation.in_progress'`,
		impTenant, ob2); got != 1 {
		t.Fatalf("ob2 in_progress outbox count = %d, want 1", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.in_progress'`,
		impTenant, ob2); got != 1 {
		t.Fatalf("ob2 in_progress audit count = %d, want 1", got)
	}
	if got := scanText(t, ctx, pool,
		`SELECT metadata->>'domain' FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.in_progress'`,
		impTenant, ob2); got != "vaccination" {
		t.Fatalf("ob2 in_progress audit domain = %q, want vaccination", got)
	}
	if got := scanText(t, ctx, pool,
		`SELECT metadata->>'status' FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.in_progress'`,
		impTenant, ob2); got != "in_progress" {
		t.Fatalf("ob2 in_progress audit status = %q, want in_progress", got)
	}
	if got := scanText(t, ctx, pool,
		`SELECT metadata->>'domain' FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='vaccination.completed'`,
		impTenant, ob1); got != "vaccination" {
		t.Fatalf("ob1 completed audit domain = %q, want vaccination", got)
	}
	if got := scanText(t, ctx, pool,
		`SELECT metadata->>'status' FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='vaccination.completed'`,
		impTenant, ob1); got != "completed" {
		t.Fatalf("ob1 completed audit status = %q, want completed", got)
	}

	// --- Accept g2: the last open obligation on the batch closes it out to completed.
	ar2, err := completion.Accept(ctx, vaccapp.AcceptInput{Completion: mk(ob2, g2, "pend1-sib-g2")})
	if err != nil || !ar2.Applied || !ar2.Completed {
		t.Fatalf("accept g2: %+v err=%v", ar2, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, impTenant, batchID); got != "completed" {
		t.Fatalf("batch status after last complete = %s, want completed", got)
	}
	// No duplicate sibling in_progress event from the second (closing) completion.
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='in_progress'`,
		impTenant, ob2); got != 1 {
		t.Fatalf("final ob2 in_progress event count = %d, want 1 (no duplicate)", got)
	}
}
