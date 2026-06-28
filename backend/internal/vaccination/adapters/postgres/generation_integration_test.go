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
	if err := vacc.FinishGenerationRun(ctx, impTenant, run.RunID, vaccdomain.GenerateResult{Generated: 1, Reopened: 2}, "", "forced partial failure", firstAt.Add(time.Minute)); err != nil {
		t.Fatalf("finish failed run: %v", err)
	}
	var reopenedCount int
	if err := pool.QueryRow(ctx,
		`SELECT reopened_count FROM vaccination_generation_runs WHERE tenant_id = $1::uuid AND run_id = $2::uuid`,
		impTenant, run.RunID).Scan(&reopenedCount); err != nil {
		t.Fatalf("read reopened count: %v", err)
	}
	if reopenedCount != 2 {
		t.Fatalf("persisted reopened count: want 2, got %d", reopenedCount)
	}

	retry, restarted, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "catchup@second",
		StartedAt: secondAt, IdempotencyKey: "manual-campaign-retry-key", RequestHash: "request-hash",
	})
	if err != nil || !restarted {
		t.Fatalf("restart generation run restarted=%v err=%v run=%#v", restarted, err, retry)
	}
	if !retry.StartedAt.Equal(firstAt) || retry.Status != "running" || retry.CompletedAt != nil || retry.Generated != 0 || retry.Reopened != 0 || retry.RequestHash != "request-hash" {
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
	run, started, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "stale@first",
		StartedAt: firstAt, IdempotencyKey: "manual-campaign-stale-key", RequestHash: "request-hash",
	})
	if err != nil || !started {
		t.Fatalf("start generation run started=%v err=%v run=%#v", started, err, run)
	}

	// Model a DEAD run: no heartbeat for >15 REAL minutes. Staleness is wall-clock now(), so updated_at
	// must be aged relative to now(), NOT to the business asOf (the bug this guards: an asOf-based
	// window can never elapse on replay).
	if _, err := pool.Exec(ctx, `
UPDATE vaccination_generation_runs SET updated_at = now() - interval '16 minutes'
WHERE tenant_id = $1::uuid AND idempotency_key = 'manual-campaign-stale-key'`, impTenant); err != nil {
		t.Fatalf("age dead generation run: %v", err)
	}

	// Publish REDELIVERY: the same event carries the same OccurredAt, so asOf == the stuck run's
	// started_at. A wall-clock staleness window still reclaims the dead run; an asOf-based window
	// (started_at < asOf-15m) never could, wedging the cohort forever.
	retry, restarted, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "stale@first",
		StartedAt: firstAt, IdempotencyKey: "manual-campaign-stale-key", RequestHash: "request-hash",
	})
	if err != nil || !restarted {
		t.Fatalf("restart stale generation run restarted=%v err=%v run=%#v", restarted, err, retry)
	}
	if retry.Status != "running" || retry.RequestHash != "request-hash" {
		t.Fatalf("stale retry run = %#v, want restarted running row", retry)
	}
}

// TestStartGenerationRunHeartbeatPreventsReclaim proves a long but LIVE run (recent wall-clock
// heartbeat) is not reclaimed/duplicated by the stale-run detector — even when the reclaiming caller
// supplies a FUTURE business asOf (staleness is measured against now(), not asOf).
func TestStartGenerationRunHeartbeatPreventsReclaim(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.manual.heartbeat", Name: "Manual heartbeat", Category: "vaccination", Status: "draft",
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
	run, started, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "live@first",
		StartedAt: firstAt, IdempotencyKey: "manual-campaign-live-key", RequestHash: "request-hash",
	})
	if err != nil || !started {
		t.Fatalf("start generation run started=%v err=%v run=%#v", started, err, run)
	}

	// Recent heartbeat: updated_at 5 REAL minutes ago (well inside the 15m wall-clock window).
	if _, err := pool.Exec(ctx, `
UPDATE vaccination_generation_runs SET updated_at = now() - interval '5 minutes'
WHERE tenant_id = $1::uuid AND idempotency_key = 'manual-campaign-live-key'`, impTenant); err != nil {
		t.Fatalf("heartbeat generation run: %v", err)
	}

	// FUTURE business asOf must NOT steal the live run: an asOf-based window (asOf-15m) would exceed
	// now() and wrongly reclaim, but staleness is wall-clock so the recent heartbeat wins.
	futureAsOf := firstAt.Add(72 * time.Hour)
	retry, restarted, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "live@retry",
		StartedAt: futureAsOf, IdempotencyKey: "manual-campaign-live-key", RequestHash: "request-hash",
	})
	if err != nil {
		t.Fatalf("retry live run: %v", err)
	}
	if restarted {
		t.Fatalf("live (recently heartbeated) run must NOT be reclaimed even with a future asOf; got restarted=true run=%#v", retry)
	}
	if retry.RunID != run.RunID || retry.Status != "running" {
		t.Fatalf("retry should return the original live run, got %#v", retry)
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

// TestGoatCreatedAcceptedCompletionSuppressesMatchingObligation proves that an ACCEPTED+VERIFIED
// Goat OS administration for the same protocol+dose suppresses re-generation of that dose, while a
// merely RECORDED or accepted-without-verified_at completion does NOT — so version changes / catch-up
// cannot double-issue a dose the goat already received, without over-suppressing untrusted work.
func TestGoatCreatedAcceptedCompletionSuppressesMatchingObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.goatos.suppress", Name: "GoatOS Suppress", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"animal_stage":"K1"}}`)
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

	acceptedGoat := "30000000-0000-4000-8000-0000000000e1"
	recordedGoat := "30000000-0000-4000-8000-0000000000e2"
	unverifiedGoat := "30000000-0000-4000-8000-0000000000e3"
	entryDate := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	for _, g := range []string{acceptedGoat, recordedGoat, unverifiedGoat} {
		seedGenGoat(t, ctx, pool, g, "alive")
		if _, err := pool.Exec(ctx, `
UPDATE goats SET entry_date=$3::date, origin_type='procured', shed_id=$4::uuid, current_location_id=$4::uuid
WHERE tenant_id=$1 AND goat_id=$2`, impTenant, g, entryDate, impCbe); err != nil {
			t.Fatalf("set entry/shed: %v", err)
		}
	}

	administered := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	seedGoatOSCompletion(t, ctx, pool, obl, versionID, ruleID, acceptedGoat, "accepted", administered)
	seedGoatOSCompletion(t, ctx, pool, obl, versionID, ruleID, recordedGoat, "recorded", administered)
	seedGoatOSCompletion(t, ctx, pool, obl, versionID, ruleID, unverifiedGoat, "accepted_unverified", administered)

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	acceptedRes, err := gen.GenerateForGoat(ctx, impTenant, acceptedGoat, asOf)
	if err != nil {
		t.Fatalf("generate accepted goat: %v", err)
	}
	if acceptedRes.Generated != 0 || acceptedRes.SuppressedByTrustedHistory != 1 {
		t.Fatalf("accepted result = %#v, want generated 0 suppressed 1", acceptedRes)
	}

	recordedRes, err := gen.GenerateForGoat(ctx, impTenant, recordedGoat, asOf)
	if err != nil {
		t.Fatalf("generate recorded goat: %v", err)
	}
	if recordedRes.Generated != 1 || recordedRes.SuppressedByTrustedHistory != 0 {
		t.Fatalf("recorded result = %#v, want generated 1 suppressed 0 (only accepted history suppresses)", recordedRes)
	}

	unverifiedRes, err := gen.GenerateForGoat(ctx, impTenant, unverifiedGoat, asOf)
	if err != nil {
		t.Fatalf("generate unverified goat: %v", err)
	}
	if unverifiedRes.Generated != 1 || unverifiedRes.SuppressedByTrustedHistory != 0 {
		t.Fatalf("unverified result = %#v, want generated 1 suppressed 0 (accepted requires verified_at)", unverifiedRes)
	}
}

// TestGenerateScopesObligationToShedLocation proves a goat assigned to a real shed location gets a
// shed-scoped obligation, so the SM-4 sweeper batches one vaccination drive per shed.
func TestGenerateScopesObligationToShedLocation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.shed.scope", Name: "Shed Scope", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"eligibility":{"animal_stage":"K1"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "phc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	shedID := "00000000-0000-4000-8000-00000000f001"
	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'shed', 'SHEDT1', 'Test Shed 1', 'active')`, shedID, impTenant); err != nil {
		t.Fatalf("shed location: %v", err)
	}
	goatID := "30000000-0000-4000-8000-0000000000f1"
	seedGenGoat(t, ctx, pool, goatID, "alive")
	if _, err := pool.Exec(ctx,
		`UPDATE goats SET entry_date=DATE '2026-06-10', shed_id=$3::uuid, current_location_id=$3::uuid
		 WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID, shedID); err != nil {
		t.Fatalf("set shed: %v", err)
	}

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	res, err := gen.GenerateForGoat(ctx, impTenant, goatID, time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 1 {
		t.Fatalf("want 1 generated, got %#v", res)
	}

	var scopeType, scopeID string
	if err := pool.QueryRow(ctx,
		`SELECT scope_type, scope_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`,
		impTenant, goatID).Scan(&scopeType, &scopeID); err != nil {
		t.Fatalf("scan scope: %v", err)
	}
	if scopeType != "shed" || scopeID != shedID {
		t.Fatalf("obligation scope = %s/%s, want shed/%s (one drive per shed)", scopeType, scopeID, shedID)
	}
}

// seedGoatOSCompletion creates a prior obligation (tenant-scoped, scope irrelevant to suppression)
// and a vaccination_completions row for it in the given status, simulating a goat's Goat OS
// administration history.
func seedGoatOSCompletion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obl *oblpg.Repository, versionID, ruleID, goatID, status string, administered time.Time) {
	t.Helper()
	dbStatus := status
	var verifiedAt any
	if status == "accepted" {
		verifiedAt = administered.Add(2 * time.Hour)
	}
	if status == "accepted_unverified" {
		dbStatus = "accepted"
	}
	obID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
		DueAt: administered.Add(48 * time.Hour), Status: "completed", IdempotencyKey: "prior:" + goatID, Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("seed prior obligation: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (tenant_id, obligation_id, goat_id, doses, administered_at, status, verified_at, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1, $4::timestamptz, $5, $6::timestamptz, $7)`,
		impTenant, obID, goatID, administered, dbStatus, verifiedAt, "compl:"+goatID); err != nil {
		t.Fatalf("seed completion: %v", err)
	}
}
