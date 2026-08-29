package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// CENSUS SNAPSHOT E2E (maintainer decision 2026-08-24). These drive the REAL
// production write path — Repository.RecordShedObservation against a real
// Postgres — and pin the four halves of the rule:
//
//  1. the client-typed count is IGNORED: the stored count is the herd
//     register's live resident count for the bucket's (shed, pen), and the
//     stored average derives from it;
//  2. the snapshot is FROZEN: a later herd move changes neither the stored
//     row nor an idempotent replay's readback;
//  3. a register-empty shed REFUSES the submit (shed_count_unavailable), with
//     nothing written;
//  4. a pen-scoped bucket counts only its own pen's residents, under the same
//     "Part 2" == "2" normalization the counts module uses — and the verifier's
//     weight correction recomputes the average against the frozen count.

const (
	censusPenBucket = "00000000-0000-4000-8000-000000009601"
	censusPenGoatA  = "00000000-0000-4000-8000-000000009611"
	censusPenGoatB  = "00000000-0000-4000-8000-000000009612"
	censusEmptyShed = "1f0a51a2-1f6a-49e0-9a3e-1b4c0d6a7e01"
	censusEmptyBkt  = "00000000-0000-4000-8000-000000009602"
	censusEmptyPrf  = "00000000-0000-4000-8000-000000009621"
)

func TestLumpSumSubmitSnapshotsCensusIgnoresClientCountAndFreezesIt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// The operator's typed count (999) and average must both be ignored: the
	// fixture houses exactly FOUR live residents in the per-shed bucket's shed.
	obs, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 100, AnimalCount: 999, AverageWeightKg: 0.1001,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:census-snapshot", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record lump sum: %v", err)
	}
	if obs.AnimalCount != 4 || obs.AverageWeightKg != 25 {
		t.Fatalf("stored (count, average) = (%d, %v), want the register snapshot (4, 25)", obs.AnimalCount, obs.AverageWeightKg)
	}
	var storedCount int
	var storedAvg float64
	if err := pool.QueryRow(ctx, `SELECT animal_count, average_weight_kg::float8 FROM weighing_shed_observations WHERE shed_observation_id=$1::uuid`, obs.ObservationID).Scan(&storedCount, &storedAvg); err != nil {
		t.Fatalf("read back observation: %v", err)
	}
	if storedCount != 4 || storedAvg != 25 {
		t.Fatalf("DB round-trip (count, average) = (%d, %v), want (4, 25)", storedCount, storedAvg)
	}

	// FROZEN: two residents leave the shed after submit. The stored row must not
	// move, and the exact idempotent replay must return the original snapshot,
	// never a recomputed census.
	execWeighingTestSQL(t, ctx, pool, `UPDATE goats SET shed_id=$1::uuid, current_location_id=$1::uuid WHERE goat_id IN ($2::uuid, $3::uuid)`,
		repoExpectedShed, "00000000-0000-4000-8000-000000009211", "00000000-0000-4000-8000-000000009212")
	replay, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 100, AnimalCount: 999, AverageWeightKg: 0.1001,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:census-snapshot", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if replay.AnimalCount != 4 {
		t.Fatalf("replay count = %d, want the frozen snapshot 4 (census changed to 2 underneath)", replay.AnimalCount)
	}
	if err := pool.QueryRow(ctx, `SELECT animal_count FROM weighing_shed_observations WHERE shed_observation_id=$1::uuid`, obs.ObservationID).Scan(&storedCount); err != nil {
		t.Fatalf("re-read observation: %v", err)
	}
	if storedCount != 4 {
		t.Fatalf("stored count moved to %d after a herd move; the snapshot must be frozen at 4", storedCount)
	}

	// The verifier's weight-only correction recomputes the average against the
	// SAME frozen count — and a correction naming a count is refused by the
	// domain validator before this store is ever reached (pinned in
	// weighing/domain and weighing/app tests).
	corrected, err := repo.CorrectObservationWeight(ctx, domain.WeightCorrectionCommand{
		TenantID: repoTenant, ObservationID: obs.ObservationID, RefType: domain.VerificationRefTypeShed,
		WeightKg: 200, CorrectedBy: repoOperator, IdempotencyKey: "correction:census-frozen",
	})
	if err != nil {
		t.Fatalf("weight correction: %v", err)
	}
	if corrected.AnimalCount != 4 || corrected.AverageWeightKg != 50 {
		t.Fatalf("corrected (count, average) = (%d, %v), want the frozen count carried (4, 50)", corrected.AnimalCount, corrected.AverageWeightKg)
	}
}

func TestLumpSumSubmitCountsOnlyTheBucketsOwnPen(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// A pen-scoped bucket on the SAME shed as the fixture's whole-shed bucket.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, partition_label, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Q1 - Part 2', 'Part 2', 'per_shed_partition', $5::uuid, 0)
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		censusPenBucket, repoCampaign, repoTenant, repoPerShed, repoOperator)
	// Two residents of pen 2, one labelled "Part 2" and one bare "2": the
	// normalization must count both. The fixture's four pen-less residents
	// ('whole') must NOT count toward this pen's bucket.
	for _, goat := range []struct{ id, display, pen string }{
		{censusPenGoatA, "G-993101", "Part 2"},
		{censusPenGoatB, "G-993102", "2"},
	} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'female', 'adult', 'alive', 'adult', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO NOTHING`,
			goat.id, repoTenant, goat.display, repoParty, repoPerShed, repoPark)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'Q1')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id=EXCLUDED.shed_id, partition_label=EXCLUDED.partition_label`,
			repoTenant, goat.id, repoPerShed, goat.pen)
	}

	obs, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: censusPenBucket,
		WeightKg: 90, AnimalCount: 999,
		ProofArtifactID: repoShedProofTwo, IdempotencyKey: "shed:census-pen", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record pen lump sum: %v", err)
	}
	if obs.AnimalCount != 2 || obs.AverageWeightKg != 45 {
		t.Fatalf("pen bucket (count, average) = (%d, %v), want only the pen's residents (2, 45)", obs.AnimalCount, obs.AverageWeightKg)
	}
}

func TestLumpSumSubmitRefusesARegisterEmptyShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', 'Empty Q9', 'active', 990)
ON CONFLICT (location_id) DO NOTHING`,
		censusEmptyShed, repoTenant)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Empty Q9', 'per_shed_partition', $5::uuid, 0)
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		censusEmptyBkt, repoCampaign, repoTenant, censusEmptyShed, repoOperator)
	insertProof(t, ctx, pool, censusEmptyPrf, "video", "completed", "shed", censusEmptyShed, "shed", censusEmptyShed)

	_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: censusEmptyBkt,
		WeightKg: 50, AnimalCount: 12,
		ProofArtifactID: censusEmptyPrf, IdempotencyKey: "shed:census-empty", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrShedCountUnavailable) {
		t.Fatalf("empty-register submit err=%v, want ErrShedCountUnavailable", err)
	}
	assertNoShedObservationRows(t, ctx, pool, censusEmptyBkt)
}

func assertNoShedObservationRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignShedID string) {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM weighing_shed_observations WHERE campaign_shed_id=$1::uuid`, campaignShedID).Scan(&n); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	if n != 0 {
		t.Fatalf("refused submit still wrote %d observation rows; must write nothing", n)
	}
}
