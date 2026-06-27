package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func seedGenGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, lifecycle string) {
	t.Helper()
	seedGenGoatWithStage(t, ctx, pool, id, lifecycle, "K1")
}

func seedGenGoatWithStage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, lifecycle, stage string) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id,
		   current_location_id, park_id, management_stage, dob)
		 VALUES ($1, $2, $3, 'clean', $4, $5, $5, $6, DATE '2026-05-01')`,
		id, impTenant, lifecycle, impParty, impCbe, stage)
	if err != nil {
		t.Fatalf("seed gen goat %s: %v", id, err)
	}
}

func TestSM1GenerationIdempotentAndDeferVisible(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	// Published vaccination version: K1 eligibility, defer ICU/quarantine/sick, one birth_age rule.
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.gen", Name: "Gen", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["ICU","quarantine","sick"]},` +
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

	// 2 alive + 1 quarantine (defer) K1 goats, plus one non-K1 goat that Config-authored
	// animal_stage eligibility must exclude.
	seedGenGoat(t, ctx, pool, "30000000-0000-4000-8000-0000000000a1", "alive")
	seedGenGoat(t, ctx, pool, "30000000-0000-4000-8000-0000000000a2", "alive")
	seedGenGoat(t, ctx, pool, "30000000-0000-4000-8000-0000000000d1", "quarantine")
	seedGenGoatWithStage(t, ctx, pool, "30000000-0000-4000-8000-0000000000e1", "alive", "adult")

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)

	res, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 3 {
		t.Fatalf("generated: want 3, got %d", res.Generated)
	}
	if res.Deferred != 1 {
		t.Fatalf("deferred: want 1 (quarantine goat), got %d", res.Deferred)
	}

	// Idempotent re-run: no new obligations.
	res2, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if res2.Generated != 0 {
		t.Fatalf("re-gen should be a no-op, generated %d", res2.Generated)
	}

	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=$2`, impTenant, versionID); got != 3 {
		t.Fatalf("expected 3 obligations, got %d", got)
	}
	// Defer is canonical: the quarantine goat's obligation is held in deferred status and also has
	// a deferred ledger event for audit/as-of views.
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`,
		impTenant, "30000000-0000-4000-8000-0000000000d1"); got != 1 {
		t.Fatalf("expected 1 deferred obligation for quarantine goat, got %d", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events e
		 JOIN obligation_instances o ON o.tenant_id=e.tenant_id AND o.obligation_id=e.obligation_id
		 WHERE e.tenant_id=$1 AND e.event_type='deferred' AND o.target_id=$2`,
		impTenant, "30000000-0000-4000-8000-0000000000d1"); got != 1 {
		t.Fatalf("expected 1 deferred event for quarantine goat, got %d", got)
	}
}

func TestStartGenerationRunFailedReplayPreservesStartedAt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.manual.retry", Name: "Manual retry", Category: "vaccination", Status: "draft",
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

	firstAt := time.Date(2026, 6, 27, 8, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(5 * time.Minute)
	run, started, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "catchup@first",
		StartedAt: firstAt, IdempotencyKey: "manual-campaign-retry-key", RequestHash: "request-hash",
	})
	if err != nil || !started {
		t.Fatalf("start generation run started=%v err=%v run=%#v", started, err, run)
	}
	if err := vacc.FinishGenerationRun(ctx, impTenant, run.RunID, vaccdomain.GenerateResult{Generated: 1}, "", "forced partial failure", firstAt.Add(time.Minute)); err != nil {
		t.Fatalf("finish failed run: %v", err)
	}

	retry, restarted, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "catchup@second",
		StartedAt: secondAt, IdempotencyKey: "manual-campaign-retry-key", RequestHash: "request-hash",
	})
	if err != nil || !restarted {
		t.Fatalf("restart generation run restarted=%v err=%v run=%#v", restarted, err, retry)
	}
	if !retry.StartedAt.Equal(firstAt) || retry.Status != "running" || retry.CompletedAt != nil || retry.Generated != 0 || retry.RequestHash != "request-hash" {
		t.Fatalf("retry run = %#v, want original started_at, reset counts, running", retry)
	}
}

func TestStartGenerationRunStaleRunningReplayRestarts(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.manual.stale_retry", Name: "Manual stale retry", Category: "vaccination", Status: "draft",
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

	firstAt := time.Date(2026, 6, 27, 8, 0, 0, 0, time.UTC)
	staleRetryAt := firstAt.Add(16 * time.Minute)
	run, started, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "stale@first",
		StartedAt: firstAt, IdempotencyKey: "manual-campaign-stale-key", RequestHash: "request-hash",
	})
	if err != nil || !started {
		t.Fatalf("start generation run started=%v err=%v run=%#v", started, err, run)
	}

	retry, restarted, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "stale@retry",
		StartedAt: staleRetryAt, IdempotencyKey: "manual-campaign-stale-key", RequestHash: "request-hash",
	})
	if err != nil || !restarted {
		t.Fatalf("restart stale generation run restarted=%v err=%v run=%#v", restarted, err, retry)
	}
	if !retry.StartedAt.Equal(staleRetryAt) || retry.Status != "running" || retry.RequestHash != "request-hash" {
		t.Fatalf("stale retry run = %#v, want restarted running row", retry)
	}
}

func TestGoatCreatedHandlerGeneratesViaBus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.bus", Name: "Bus", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["ICU","quarantine","sick"]},` +
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

	goatID := "30000000-0000-4000-8000-0000000000b9"
	seedGenGoat(t, ctx, pool, goatID, "alive")

	bus := eventbus.NewInProcessBus()
	vaccapp.NewGoatCreatedHandler(vaccapp.NewGenerationService(proto, vacc, obl)).Register(bus)

	if err := bus.Publish(ctx, eventbus.Event{
		Type: vaccapp.EventGoatCreated, TenantID: impTenant, Key: goatID,
		OccurredAt: time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.created: %v", err)
	}

	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		impTenant, goatID, versionID); got != 1 {
		t.Fatalf("expected 1 obligation generated via bus, got %d", got)
	}
}

func TestGoatRecheckDefersExistingScheduledObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.recheck.defer", Name: "Recheck Defer", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["sick","quarantine","ICU"]},` +
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

	goatID := "30000000-0000-4000-8000-0000000000f1"
	seedGenGoat(t, ctx, pool, goatID, "alive")
	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	if res, err := gen.GenerateForGoat(ctx, impTenant, goatID, asOf); err != nil || res.Generated != 1 {
		t.Fatalf("initial generate result=%#v err=%v", res, err)
	}
	obligationID := scanText(t, ctx, pool,
		`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		impTenant, goatID, versionID)
	batchID, attached, err := obl.CreateBatchWithObligations(ctx, obldomain.NewBatch{
		TenantID: impTenant, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: impCbe,
		Status: "planned", EstimatedTargets: 1,
	}, []string{obligationID})
	if err != nil {
		t.Fatalf("attach planned batch: %v", err)
	}
	if attached != 1 {
		t.Fatalf("attached=%d, want 1", attached)
	}

	if _, err := pool.Exec(ctx, `UPDATE goats SET health_status='sick' WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID); err != nil {
		t.Fatalf("mark goat sick: %v", err)
	}
	bus := eventbus.NewInProcessBus()
	vaccapp.NewGoatRecheckHandler(gen).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		Type: vaccapp.EventGoatLocationChanged, TenantID: impTenant, Key: goatID,
		OccurredAt: asOf.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("publish goat recheck: %v", err)
	}

	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obligationID); got != "deferred" {
		t.Fatalf("status=%q, want deferred", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2 AND batch_id IS NULL`,
		impTenant, obligationID); got != 1 {
		t.Fatalf("expected deferred obligation detached from planned batch, got %d", got)
	}
	if got := scanText(t, ctx, pool, `SELECT estimated_targets::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, impTenant, batchID); got != "0" {
		t.Fatalf("planned batch estimated_targets=%s, want 0 after defer detach", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`,
		impTenant, obligationID); got != 1 {
		t.Fatalf("deferred events=%d, want 1", got)
	}
}

func TestGoatCreatedTrustedHFEvidenceSuppressesMatchingObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.hf.suppress", Name: "HF Suppress", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"animal_stage":"K1"},"source":{"source_system":"phc","source_ref":"PHC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "phc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	trustedGoat := "30000000-0000-4000-8000-0000000000c1"
	importedGoat := "30000000-0000-4000-8000-0000000000c2"
	futureTrustedGoat := "30000000-0000-4000-8000-0000000000c3"
	entryDate := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	seedGenGoat(t, ctx, pool, trustedGoat, "alive")
	seedGenGoat(t, ctx, pool, importedGoat, "alive")
	seedGenGoat(t, ctx, pool, futureTrustedGoat, "alive")
	for _, goatID := range []string{trustedGoat, importedGoat, futureTrustedGoat} {
		if _, err := pool.Exec(ctx, `
UPDATE goats
SET entry_date = $3::date,
    origin_type = 'procured',
    shed_id = $4::uuid,
    current_location_id = $4::uuid
WHERE tenant_id = $1 AND goat_id = $2`, impTenant, goatID, entryDate, impCbe); err != nil {
			t.Fatalf("set entry date: %v", err)
		}
	}
	pastAdministered := time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC)
	pastReviewed := time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)
	futureAdministered := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	futureReviewed := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	seedGenerationProcurementEvidence(t, ctx, pool, "30000000-0000-4000-8000-00000000d001", trustedGoat, versionID, ruleID, "trusted", pastAdministered, &pastReviewed)
	seedGenerationProcurementEvidence(t, ctx, pool, "30000000-0000-4000-8000-00000000d002", importedGoat, versionID, ruleID, "imported", pastAdministered, nil)
	seedGenerationProcurementEvidence(t, ctx, pool, "30000000-0000-4000-8000-00000000d003", futureTrustedGoat, versionID, ruleID, "trusted", futureAdministered, &futureReviewed)

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	trustedResult, err := gen.GenerateForGoat(ctx, impTenant, trustedGoat, asOf)
	if err != nil {
		t.Fatalf("generate trusted goat: %v", err)
	}
	if trustedResult.Generated != 0 || trustedResult.SuppressedByTrustedHistory != 1 {
		t.Fatalf("trusted result = %#v, want generated 0 suppressed 1", trustedResult)
	}
	importedResult, err := gen.GenerateForGoat(ctx, impTenant, importedGoat, asOf)
	if err != nil {
		t.Fatalf("generate imported goat: %v", err)
	}
	if importedResult.Generated != 1 || importedResult.SuppressedByTrustedHistory != 0 {
		t.Fatalf("imported result = %#v, want generated 1 suppressed 0", importedResult)
	}
	futureTrustedResult, err := gen.GenerateForGoat(ctx, impTenant, futureTrustedGoat, asOf)
	if err != nil {
		t.Fatalf("generate future trusted goat: %v", err)
	}
	if futureTrustedResult.Generated != 1 || futureTrustedResult.SuppressedByTrustedHistory != 0 {
		t.Fatalf("future trusted result = %#v, want generated 1 suppressed 0", futureTrustedResult)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, trustedGoat); got != 0 {
		t.Fatalf("trusted HF evidence goat obligations = %d, want 0", got)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, importedGoat); got != 1 {
		t.Fatalf("imported-only HF evidence goat obligations = %d, want 1", got)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, futureTrustedGoat); got != 1 {
		t.Fatalf("future trusted HF evidence goat obligations = %d, want 1", got)
	}
}

func seedGenerationProcurementEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, loadID, goatID, versionID, ruleID, reviewStatus string, administeredAt time.Time, reviewedAt *time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, expected_count, status, idempotency_key)
VALUES ($1, $2, $3, 1, 'source_warmup', $4)
ON CONFLICT (tenant_id, load_id) DO NOTHING`, loadID, impTenant, impParty, "gen-hf-load:"+loadID); err != nil {
		t.Fatalf("seed procurement load: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (
  tenant_id, load_id, goat_id, purpose, current_state, selection_state,
  identity_review_state, ownership_state, health_state
) VALUES (
  $1, $2, $3, 'breeding', 'accepted_herd_intake', 'accepted_herd_intake',
  'clean', 'mesha_owned', 'passed'
)
ON CONFLICT (tenant_id, load_id, goat_id) DO NOTHING`, impTenant, loadID, goatID); err != nil {
		t.Fatalf("seed procurement load goat: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_hf_vaccination_evidence (
  tenant_id, load_id, goat_id, protocol_version_id, rule_id, dose_code,
  administered_at, vaccine_name, lot_number, source_ref, review_status,
  reviewed_at, idempotency_key
) VALUES (
  $1, $2, $3, $4, $5, 'primary',
  $6::timestamptz, 'HF vaccine', 'HF-LOT', 'supplier:hf',
  $7, $8::timestamptz,
  $9
)`, impTenant, loadID, goatID, versionID, ruleID, administeredAt, reviewStatus, reviewedAt, "gen-hf-evidence:"+goatID); err != nil {
		t.Fatalf("seed HF evidence: %v", err)
	}
}

func countRowsVacc(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
