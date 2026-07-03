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

const (
	parkShedA = "00000000-0000-4000-8000-00000000d001"
	parkShedB = "00000000-0000-4000-8000-00000000d002"
	parkShedC = "00000000-0000-4000-8000-00000000d003"
)

func defaultParkSweepConfig() oblapp.SweepConfig {
	return oblapp.SweepConfig{ParkConsolidation: domain.DefaultParkConsolidationSettings()}
}

func seedParkConsolidationShed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, code string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
VALUES ($1, $2, 'shed', $3, $4, 'active', $5)
ON CONFLICT (location_id) DO NOTHING`, shedID, tenantID, code, code, cbePark); err != nil {
		t.Fatalf("seed shed %s: %v", code, err)
	}
}

func parkConsolidationProtocol(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code string) (versionID, ruleID string) {
	t.Helper()
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: code, Name: code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err = proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		DueWindowDays: 7, EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	return versionID, ruleID
}

func insertShedObligation(t *testing.T, ctx context.Context, repo *Repository, versionID, ruleID, goatID, shedID, key string, due time.Time) {
	t.Helper()
	windowEnd := due.Add(7 * 24 * time.Hour)
	if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
		DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
	}); err != nil || !applied {
		t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
	}
}

// TestSM4ParkConsolidationShedDriveBatchesMultipleGoatsInOneShed proves layer 1 still creates a
// shed drive when two goats in the same shed share rule + due day.
func TestSM4ParkConsolidationShedDriveBatchesMultipleGoatsInOneShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedParkConsolidationShed(t, ctx, pool, parkShedA, "PARK-SHED-A")
	repo := NewRepository(pool, 5*time.Second)
	versionID, ruleID := parkConsolidationProtocol(t, ctx, pool, "vaccination.park.shedbatch")

	const goat1 = "10000000-0000-4000-8000-00000000d101"
	const goat2 = "10000000-0000-4000-8000-00000000d102"
	seedReserveGoats(t, ctx, pool, parkShedA, cbePark, goat1, goat2)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goat1, parkShedA, "park-shed-a-1", due)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goat2, parkShedA, "park-shed-a-2", due)

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, defaultParkSweepConfig(), time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.ParkBatches != 0 {
		t.Fatalf("park batches = %d, want 0 (shed drive should absorb both goats)", res.ParkBatches)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_batches
WHERE protocol_version_id=$1 AND scope_type='shed' AND scope_id=$2`, versionID, parkShedA); got != 1 {
		t.Fatalf("shed batches = %d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances
WHERE protocol_version_id=$1 AND batch_id IS NOT NULL`, versionID); got != 2 {
		t.Fatalf("batched obligations = %d, want 2", got)
	}
}

// TestSM4ParkConsolidationMergesSingletonsAcrossSheds proves layer 2 creates one park drive when
// each shed only has one leftover goat with overlapping medical windows.
func TestSM4ParkConsolidationMergesSingletonsAcrossSheds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedParkConsolidationShed(t, ctx, pool, parkShedA, "PARK-SHED-A")
	seedParkConsolidationShed(t, ctx, pool, parkShedB, "PARK-SHED-B")
	repo := NewRepository(pool, 5*time.Second)
	versionID, ruleID := parkConsolidationProtocol(t, ctx, pool, "vaccination.park.merge")

	const goatA = "10000000-0000-4000-8000-00000000d201"
	const goatB = "10000000-0000-4000-8000-00000000d202"
	seedReserveGoats(t, ctx, pool, parkShedA, cbePark, goatA)
	seedReserveGoats(t, ctx, pool, parkShedB, cbePark, goatB)

	dueA := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	dueB := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatA, parkShedA, "park-merge-a", dueA)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatB, parkShedB, "park-merge-b", dueB)

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, defaultParkSweepConfig(), time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_batches
WHERE protocol_version_id=$1 AND scope_type='shed'`, versionID); got != 0 {
		t.Fatalf("shed batches = %d, want 0 (singletons deferred to park pass)", got)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 2 {
		t.Fatalf("result = %#v, want one park batch with two obligations", res)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_batches
WHERE protocol_version_id=$1 AND scope_type='park' AND scope_id=$2
  AND session LIKE 'park-consolidation:%'`, versionID, cbePark); got != 1 {
		t.Fatalf("park consolidation batches = %d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances o
JOIN obligation_batches b ON b.batch_id = o.batch_id
WHERE o.protocol_version_id=$1 AND b.scope_type='park'`, versionID); got != 2 {
		t.Fatalf("obligations on park batch = %d, want 2", got)
	}
}

// TestSM4ParkConsolidationShedFirstThenParkLeftovers models shed A running a full drive while
// singleton leftovers in sheds B and C merge into one park drive.
func TestSM4ParkConsolidationShedFirstThenParkLeftovers(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	for _, row := range []struct {
		id, code string
	}{
		{parkShedA, "PARK-SHED-A"},
		{parkShedB, "PARK-SHED-B"},
		{parkShedC, "PARK-SHED-C"},
	} {
		seedParkConsolidationShed(t, ctx, pool, row.id, row.code)
	}

	repo := NewRepository(pool, 5*time.Second)
	versionID, ruleID := parkConsolidationProtocol(t, ctx, pool, "vaccination.park.mixed")

	const goatA1 = "10000000-0000-4000-8000-00000000d301"
	const goatA2 = "10000000-0000-4000-8000-00000000d302"
	const goatB = "10000000-0000-4000-8000-00000000d303"
	const goatC = "10000000-0000-4000-8000-00000000d304"
	seedReserveGoats(t, ctx, pool, parkShedA, cbePark, goatA1, goatA2)
	seedReserveGoats(t, ctx, pool, parkShedB, cbePark, goatB)
	seedReserveGoats(t, ctx, pool, parkShedC, cbePark, goatC)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatA1, parkShedA, "park-mixed-a1", due)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatA2, parkShedA, "park-mixed-a2", due)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatB, parkShedB, "park-mixed-b", due.Add(24*time.Hour))
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatC, parkShedC, "park-mixed-c", due.Add(48*time.Hour))

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, defaultParkSweepConfig(), time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_batches
WHERE protocol_version_id=$1 AND scope_type='shed' AND scope_id=$2`, versionID, parkShedA); got != 1 {
		t.Fatalf("shed A batches = %d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances o
JOIN obligation_batches b ON b.batch_id = o.batch_id
WHERE o.protocol_version_id=$1 AND b.scope_type='shed' AND b.scope_id=$2`, versionID, parkShedA); got != 2 {
		t.Fatalf("goats on shed A batch = %d, want 2", got)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 2 {
		t.Fatalf("result = %#v, want one park batch for B+C leftovers", res)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances
WHERE protocol_version_id=$1 AND batch_id IS NULL`, versionID); got != 0 {
		t.Fatalf("unbatched obligations = %d, want 0 after park consolidation + fallback", got)
	}
}

// TestSM4ParkConsolidationFallbackCreatesSingletonShedDrive proves a lone leftover goat still
// receives a shed drive when no cross-shed park merge is possible.
func TestSM4ParkConsolidationFallbackCreatesSingletonShedDrive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedParkConsolidationShed(t, ctx, pool, parkShedA, "PARK-SHED-A")
	repo := NewRepository(pool, 5*time.Second)
	versionID, ruleID := parkConsolidationProtocol(t, ctx, pool, "vaccination.park.orphan")

	const goatA = "10000000-0000-4000-8000-00000000d501"
	seedReserveGoats(t, ctx, pool, parkShedA, cbePark, goatA)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatA, parkShedA, "park-orphan-a", due)

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, defaultParkSweepConfig(), time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.ParkBatches != 0 {
		t.Fatalf("park batches = %d, want 0 for single-shed orphan", res.ParkBatches)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_batches
WHERE protocol_version_id=$1 AND scope_type='shed' AND scope_id=$2`, versionID, parkShedA); got != 1 {
		t.Fatalf("fallback shed batches = %d, want 1 singleton drive", got)
	}
}

// TestSM4ParkConsolidationIncludesMissedObligations proves missed shed obligations are swept
// into drives instead of being left behind.
func TestSM4ParkConsolidationIncludesMissedObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedParkConsolidationShed(t, ctx, pool, parkShedA, "PARK-SHED-A")
	seedParkConsolidationShed(t, ctx, pool, parkShedB, "PARK-SHED-B")
	repo := NewRepository(pool, 5*time.Second)
	versionID, ruleID := parkConsolidationProtocol(t, ctx, pool, "vaccination.park.missed")

	const goatA = "10000000-0000-4000-8000-00000000d601"
	const goatB = "10000000-0000-4000-8000-00000000d602"
	seedReserveGoats(t, ctx, pool, parkShedA, cbePark, goatA)
	seedReserveGoats(t, ctx, pool, parkShedB, cbePark, goatB)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatA, parkShedA, "park-missed-a", due)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatB, parkShedB, "park-missed-b", due)

	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status='missed'
WHERE protocol_version_id=$1`, versionID); err != nil {
		t.Fatalf("mark missed: %v", err)
	}

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, defaultParkSweepConfig(), time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.Obligations != 2 {
		t.Fatalf("result = %#v, want two missed goats batched into drives", res)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances
WHERE protocol_version_id=$1 AND batch_id IS NULL`, versionID); got != 0 {
		t.Fatalf("unbatched missed obligations = %d, want 0", got)
	}
}

// TestSM4ParkConsolidationMergesDifferentVaccinesAcrossSheds proves one park drive can carry
// multiple vaccine rules when their medical windows overlap.
func TestSM4ParkConsolidationMergesDifferentVaccinesAcrossSheds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedParkConsolidationShed(t, ctx, pool, parkShedA, "PARK-SHED-A")
	seedParkConsolidationShed(t, ctx, pool, parkShedB, "PARK-SHED-B")
	repo := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.park.multivax", Name: "MultiVax", Category: "vaccination", Status: "draft",
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
	ruleET, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		DueWindowDays: 7, EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule et: %v", err)
	}
	ruleTT, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "booster_1", Sequence: 2,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		DueWindowDays: 7, EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule tt: %v", err)
	}

	const goatET = "10000000-0000-4000-8000-00000000d701"
	const goatTT = "10000000-0000-4000-8000-00000000d702"
	seedReserveGoats(t, ctx, pool, parkShedA, cbePark, goatET)
	seedReserveGoats(t, ctx, pool, parkShedB, cbePark, goatTT)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	insertShedObligation(t, ctx, repo, versionID, ruleET, goatET, parkShedA, "park-multivax-et", due)
	insertShedObligation(t, ctx, repo, versionID, ruleTT, goatTT, parkShedB, "park-multivax-tt", due.Add(24*time.Hour))

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, defaultParkSweepConfig(), time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 2 {
		t.Fatalf("result = %#v, want one multi-vaccine park batch", res)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(DISTINCT oi.rule_id)
FROM obligation_instances oi
JOIN obligation_batches b ON b.batch_id = oi.batch_id
WHERE oi.protocol_version_id=$1 AND b.scope_type='park'`, versionID); got != 2 {
		t.Fatalf("distinct rules on park batch = %d, want 2 vaccines", got)
	}
}

// TestSM4ParkConsolidationDisabledKeepsSingletonShedDrives proves disabling park consolidation
// restores legacy per-shed singleton batching.
func TestSM4ParkConsolidationDisabledKeepsSingletonShedDrives(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedParkConsolidationShed(t, ctx, pool, parkShedA, "PARK-SHED-A")
	seedParkConsolidationShed(t, ctx, pool, parkShedB, "PARK-SHED-B")
	repo := NewRepository(pool, 5*time.Second)
	versionID, ruleID := parkConsolidationProtocol(t, ctx, pool, "vaccination.park.disabled")

	const goatA = "10000000-0000-4000-8000-00000000d401"
	const goatB = "10000000-0000-4000-8000-00000000d402"
	seedReserveGoats(t, ctx, pool, parkShedA, cbePark, goatA)
	seedReserveGoats(t, ctx, pool, parkShedB, cbePark, goatB)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatA, parkShedA, "park-off-a", due)
	insertShedObligation(t, ctx, repo, versionID, ruleID, goatB, parkShedB, "park-off-b", due)

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	res, err := sweep.SweepVersion(ctx, tenantID, versionID, oblapp.SweepConfig{}, time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.ParkBatches != 0 {
		t.Fatalf("park batches = %d, want 0 when park consolidation disabled", res.ParkBatches)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_batches
WHERE protocol_version_id=$1 AND scope_type='shed'`, versionID); got != 2 {
		t.Fatalf("shed batches = %d, want 2 singleton shed drives", got)
	}
}
