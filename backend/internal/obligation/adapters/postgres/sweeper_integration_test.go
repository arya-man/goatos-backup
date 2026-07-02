package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// rawTaskCreator stands in for the production sopbridge: it inserts a real sop_task (so the
// obligation_batches.sop_task_id composite FK holds) using the seeded vaccination SOP skeleton.
type rawTaskCreator struct {
	pool *pgxpool.Pool
	n    int
}

const skeletonSOPID = "b0000000-0000-4000-8000-000000000001"

func (c *rawTaskCreator) CreateTaskForBatch(ctx context.Context, tenantID, batchID, sopVersionID, taskType, title, scopeType, scopeID string) (string, error) {
	var existing string
	err := c.pool.QueryRow(ctx, `
SELECT task_id::text
FROM sop_tasks
WHERE tenant_id = $1::uuid
  AND context ->> 'obligation_batch_id' = $2
LIMIT 1`, tenantID, batchID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	c.n++
	var id string
	err = c.pool.QueryRow(ctx,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, priority, context)
		 VALUES ($1, $2, $3, $4, $5, 'queued', $6, $7, 'normal', jsonb_build_object('created_by', 'obligation-sweeper', 'obligation_batch_id', $8::text)) RETURNING task_id::text`,
		tenantID, skeletonSOPID, sopVersionID, taskType, title, scopeType, scopeID, batchID).Scan(&id)
	return id, err
}

func TestSM4bSpawnsSopTaskPerBatch(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.sweepb", Name: "SweepB", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	skeletonVersion := "b0000000-0000-4000-8000-000000000002"
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
		SopVersionID: &skeletonVersion,
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

	const cpt = "00000000-0000-4000-8000-000000003002"
	mkObl := func(target, key string) {
		_, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "park", TargetID: target, ScopeType: "park", ScopeID: target,
			DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert %s: %v", key, err)
		}
	}
	mkObl(cbePark, "b1")
	mkObl(cpt, "b2")

	v, err := proto.GetVersion(ctx, tenantID, versionID)
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	creator := &rawTaskCreator{pool: pool}
	sweep := oblapp.NewSweeperService(repo, creator, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, oblapp.SweepConfig{SOPVersionID: v.SopVersionID}, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.Batches != 2 {
		t.Fatalf("batches: want 2, got %d", res.Batches)
	}
	if creator.n != 2 {
		t.Fatalf("expected 2 sop tasks spawned, got %d", creator.n)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE protocol_version_id=$1 AND sop_task_id IS NOT NULL`, versionID); got != 2 {
		t.Fatalf("expected 2 batches with sop_task_id, got %d", got)
	}

	// Idempotent re-sweep: no new batches, no new tasks.
	res2, err := sweep.SweepVersion(ctx, tenantID, versionID, oblapp.SweepConfig{SOPVersionID: v.SopVersionID}, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("re-sweep: %v", err)
	}
	if res2.Batches != 0 || creator.n != 2 {
		t.Fatalf("re-sweep should spawn nothing: batches=%d tasks=%d", res2.Batches, creator.n)
	}
}

func TestSM4bFinalizesPlannedBatchMissingSOPTask(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.sweep.repair", Name: "SweepRepair", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	skeletonVersion := "b0000000-0000-4000-8000-000000000002"
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
		SopVersionID: &skeletonVersion,
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

	obligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "park", TargetID: cbePark, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "repair-b1", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation: applied=%v err=%v", applied, err)
	}
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Status: "planned", EstimatedTargets: 1,
	}, []string{obligationID})
	if err != nil {
		t.Fatalf("create attached batch: %v", err)
	}
	if attached != 1 {
		t.Fatalf("attached=%d, want 1", attached)
	}

	creator := &rawTaskCreator{pool: pool}
	sweep := oblapp.NewSweeperService(repo, creator, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, oblapp.SweepConfig{SOPVersionID: skeletonVersion}, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("repair sweep: %v", err)
	}
	if res.Batches != 0 || res.Obligations != 0 {
		t.Fatalf("repair should not create new batches: %#v", res)
	}
	if creator.n != 1 {
		t.Fatalf("expected one repair task, got %d", creator.n)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2 AND sop_task_id IS NOT NULL`, tenantID, batchID); got != 1 {
		t.Fatalf("expected repaired batch to have sop_task_id, got %d", got)
	}

	res2, err := sweep.SweepVersion(ctx, tenantID, versionID, oblapp.SweepConfig{SOPVersionID: skeletonVersion}, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("repair re-sweep: %v", err)
	}
	if res2.Batches != 0 || creator.n != 1 {
		t.Fatalf("repair re-sweep should be idempotent: batches=%d tasks=%d", res2.Batches, creator.n)
	}
}

func TestSM4SweeperBatchesByScopeRuleAndDueDate(t *testing.T) {
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

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, oblapp.SweepConfig{}, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.Batches != 3 { // cbe day 1 + cbe day 2 + cpt day 1
		t.Fatalf("batches: want 3, got %d", res.Batches)
	}
	if res.Obligations != 3 {
		t.Fatalf("obligations attached: want 3, got %d", res.Obligations)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE protocol_version_id=$1 AND batch_id IS NOT NULL`, versionID); got != 3 {
		t.Fatalf("expected 3 batched obligations, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE protocol_version_id=$1`, versionID); got != 3 {
		t.Fatalf("expected 3 batches, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE protocol_version_id=$1 AND scope_id=$2 AND planned_date IN ('2026-08-01', '2026-08-02')`, versionID, cbePark); got != 2 {
		t.Fatalf("expected cbe obligations on different due dates to split into 2 batches, got %d", got)
	}

	res2, err := sweep.SweepVersion(ctx, tenantID, versionID, oblapp.SweepConfig{}, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("re-sweep: %v", err)
	}
	if res2.Batches != 0 {
		t.Fatalf("re-sweep should create 0 batches, got %d", res2.Batches)
	}
}

func TestSM4SweeperKeepsOneScopeInOneBatchAcrossPages(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.sweep.pages", Name: "SweepPages", Category: "vaccination", Status: "draft",
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

	if _, err := pool.Exec(ctx, `
WITH seeded_goats AS (
  SELECT ('40000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid AS goat_id
  FROM generate_series(0, 1000) AS i
), inserted_goats AS (
	  INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id, sex, current_location_id, park_id)
	  SELECT goat_id, $1::uuid, 'alive', 'clean', $2::uuid, 'female', $3::uuid, $3::uuid
  FROM seeded_goats
  ON CONFLICT (goat_id) DO NOTHING
  RETURNING goat_id
)
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, "sequence"
)
SELECT
  $1::uuid, $4::uuid, $5::uuid, 'goat', goat_id,
  'park', $3::uuid, '2026-08-01 00:00:00+00'::timestamptz, 'scheduled',
  'page-split-obligation-' || goat_id::text, 1
FROM inserted_goats`, tenantID, meshaParty, cbePark, versionID, ruleID); err != nil {
		t.Fatalf("seed page-split obligations: %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE protocol_version_id=$1 AND batch_id IS NULL`, versionID); got != 1001 {
		t.Fatalf("seeded obligations = %d, want 1001", got)
	}

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, oblapp.SweepConfig{}, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.Batches != 1 || res.Obligations != 1001 {
		t.Fatalf("result = %#v, want one scope batch and 1001 obligations", res)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE protocol_version_id=$1`, versionID); got != 1 {
		t.Fatalf("batch count = %d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE protocol_version_id=$1 AND batch_id IS NOT NULL`, versionID); got != 1001 {
		t.Fatalf("batched obligations = %d, want 1001", got)
	}
}
