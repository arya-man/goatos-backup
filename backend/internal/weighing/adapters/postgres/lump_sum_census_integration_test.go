package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	censusPenBucket       = "00000000-0000-4000-8000-000000009601"
	censusPenGoatA        = "00000000-0000-4000-8000-000000009611"
	censusPenGoatB        = "00000000-0000-4000-8000-000000009612"
	censusEmptyShed       = "1f0a51a2-1f6a-49e0-9a3e-1b4c0d6a7e01"
	censusEmptyBkt        = "00000000-0000-4000-8000-000000009602"
	censusEmptyPrf        = "00000000-0000-4000-8000-000000009621"
	censusAliasShed       = "00000000-0000-4000-8000-000000009631"
	censusAliasBkt        = "00000000-0000-4000-8000-000000009632"
	censusAliasPrf        = "00000000-0000-4000-8000-000000009633"
	censusAliasGoatA      = "00000000-0000-4000-8000-000000009634"
	censusAliasGoatB      = "00000000-0000-4000-8000-000000009635"
	censusRealQ12         = "00000000-0000-4000-8000-000000009636"
	censusRealQ12Bkt      = "00000000-0000-4000-8000-000000009637"
	censusRealQ12Prf      = "00000000-0000-4000-8000-000000009638"
	censusLiveShed        = "00000000-0000-4000-8000-000000009641"
	censusLiveBkt         = "00000000-0000-4000-8000-000000009642"
	censusLivePrf         = "00000000-0000-4000-8000-000000009643"
	censusLiveGoatA       = "00000000-0000-4000-8000-000000009644"
	censusLiveGoatB       = "00000000-0000-4000-8000-000000009645"
	censusLiveGoatC       = "00000000-0000-4000-8000-000000009646"
	censusStandaloneShed  = "00000000-0000-4000-8000-000000009661"
	censusStandaloneBkt   = "00000000-0000-4000-8000-000000009662"
	censusStandalonePrf   = "00000000-0000-4000-8000-000000009663"
	censusStandaloneGoatA = "00000000-0000-4000-8000-000000009664"
	censusStandaloneGoatB = "00000000-0000-4000-8000-000000009665"
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

func TestLumpSumSubmitCountsParentPartitionWhenBucketIsNumberedAlias(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', 'Q1 2', $3::uuid, 'active', 991)
ON CONFLICT (location_id) DO NOTHING`,
		censusAliasShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order, alias_location_id)
VALUES ($1::uuid, $2::uuid, '2', '2', 'location_alias', 2, $3::uuid)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET partition_label=EXCLUDED.partition_label, status='active', source=EXCLUDED.source, display_order=EXCLUDED.display_order, alias_location_id=EXCLUDED.alias_location_id`,
		repoTenant, repoPerShed, censusAliasShed)
	for _, goat := range []struct{ id, display string }{
		{censusAliasGoatA, "G-993201"},
		{censusAliasGoatB, "G-993202"},
	} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'female', 'adult', 'alive', 'adult', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO NOTHING`,
			goat.id, repoTenant, goat.display, repoParty, repoPerShed, repoPark)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2', 'Q1')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id=EXCLUDED.shed_id, partition_label=EXCLUDED.partition_label`,
			repoTenant, goat.id, repoPerShed)
	}
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Q1 2', 'per_shed_partition', $5::uuid, 0)
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		censusAliasBkt, repoCampaign, repoTenant, censusAliasShed, repoOperator)
	insertProof(t, ctx, pool, censusAliasPrf, "video", "completed", "shed", censusAliasShed, "shed", censusAliasShed)

	obs, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: censusAliasBkt,
		WeightKg:        80,
		ProofArtifactID: censusAliasPrf, IdempotencyKey: "shed:census-alias", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record alias lump sum: %v", err)
	}
	if obs.AnimalCount != 2 || obs.AverageWeightKg != 40 {
		t.Fatalf("alias bucket (count, average) = (%d, %v), want parent partition residents (2, 40)", obs.AnimalCount, obs.AverageWeightKg)
	}
}

// TestLumpSumSubmitCountsAliasStillMarkedActive is the STG production shape that
// stopped every lump-sum submit between 2026-08-24 and 2026-08-31: the pen-alias
// location is still status='active', so 000112 seed 2 -- which only ever matched an
// INACTIVE alias -- never recorded it, and re-running seed 2 could not fix it either
// because the pen already carries a seed-1 'goat_attested' row on the same unique key
// (tenant_id, shed_id, normalized_label). Migration 000239 records the relationship on
// its own column instead, and the census resolves the bucket by that id.
func TestLumpSumSubmitCountsAliasStillMarkedActive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// ACTIVE alias location -- exactly why the old catalog signal was silent.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', 'Q1 3', $3::uuid, 'active', 993)
ON CONFLICT (location_id) DO NOTHING`,
		censusLiveShed, repoTenant, repoPark)
	// The pen row is 'goat_attested' (seed 1), as it is in production; the alias is
	// carried on its own column, not by overloading source.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order, alias_location_id)
VALUES ($1::uuid, $2::uuid, '3', '3', 'goat_attested', 3, $3::uuid)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET status='active', alias_location_id=EXCLUDED.alias_location_id`,
		repoTenant, repoPerShed, censusLiveShed)
	for _, goat := range []struct{ id, display string }{
		{censusLiveGoatA, "G-994301"},
		{censusLiveGoatB, "G-994302"},
		{censusLiveGoatC, "G-994303"},
	} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'female', 'adult', 'alive', 'adult', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO NOTHING`,
			goat.id, repoTenant, goat.display, repoParty, repoPerShed, repoPark)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '3', 'Q1 3')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id=EXCLUDED.shed_id, partition_label=EXCLUDED.partition_label, source_shed_name=EXCLUDED.source_shed_name`,
			repoTenant, goat.id, repoPerShed)
	}
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Q1 3', 'per_shed_partition', $5::uuid, 0)
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		censusLiveBkt, repoCampaign, repoTenant, censusLiveShed, repoOperator)
	insertProof(t, ctx, pool, censusLivePrf, "video", "completed", "shed", censusLiveShed, "shed", censusLiveShed)

	obs, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: censusLiveBkt,
		WeightKg:        90,
		ProofArtifactID: censusLivePrf, IdempotencyKey: "shed:census-live-alias", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record active-alias lump sum: %v", err)
	}
	if obs.AnimalCount != 3 || obs.AverageWeightKg != 30 {
		t.Fatalf("active alias (count, average) = (%d, %v), want (3, 30)", obs.AnimalCount, obs.AverageWeightKg)
	}
}

// TestLumpSumSubmitRefusesStandaloneSuffixShedDespiteResidentAttestation pins the
// defect found in review of the first attempt at this fix. That attempt resolved the
// alias from goat_shed_partitions.source_shed_name, which looks like an origin fact
// and is not: goat_relocate.go and admin_goat_create.go both write it as
// oploc.Display(destination), so a goat living in canonical 'Q1' pen '4' carries the
// string 'Q1 4' -- identical to what a genuinely standalone shed named 'Q1 4' carries.
// Under that rule an EMPTY standalone shed reported its parent pen's animals as
// weighed (count=2 on a shed nobody weighed). Here the attesting goats are alive and
// currently resident, so requiring liveness would NOT have saved it; only refusing to
// infer does. No alias_location_id is set, so the bucket must be refused.
func TestLumpSumSubmitRefusesStandaloneSuffixShedDespiteResidentAttestation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', 'Q1 4', $3::uuid, 'active', 994)
ON CONFLICT (location_id) DO NOTHING`,
		censusStandaloneShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
VALUES ($1::uuid, $2::uuid, '4', '4', 'goat_attested', 4)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE SET status='active'`,
		repoTenant, repoPerShed)
	// A DIFFERENT pen on the same shed IS a real alias, of a DIFFERENT location. This
	// is what makes the test able to fail: an alias lookup that forgets to match the
	// bucket's own location id finds this row instead and reports pen 2's animals for
	// a bucket standing on pen 4's shed. Mutation-tested by relaxing the census
	// predicate to `alias_location_id IS NOT NULL`, which turns this test red.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', 'Q1 2', $3::uuid, 'active', 995)
ON CONFLICT (location_id) DO NOTHING`,
		censusAliasShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order, alias_location_id)
VALUES ($1::uuid, $2::uuid, '2', '2', 'goat_attested', 2, $3::uuid)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET status='active', alias_location_id=EXCLUDED.alias_location_id`,
		repoTenant, repoPerShed, censusAliasShed)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-995499', 'female', 'adult', 'alive', 'adult', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO NOTHING`,
		censusAliasGoatA, repoTenant, repoParty, repoPerShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2', 'Q1 2')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id=EXCLUDED.shed_id, partition_label=EXCLUDED.partition_label`,
		repoTenant, censusAliasGoatA, repoPerShed)
	for _, goat := range []struct{ id, display string }{
		{censusStandaloneGoatA, "G-995401"},
		{censusStandaloneGoatB, "G-995402"},
	} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'female', 'adult', 'alive', 'adult', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO NOTHING`,
			goat.id, repoTenant, goat.display, repoParty, repoPerShed, repoPark)
		// oploc.Display("Q1","4") == "Q1 4" -- what production actually writes.
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '4', 'Q1 4')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id=EXCLUDED.shed_id, partition_label=EXCLUDED.partition_label, source_shed_name=EXCLUDED.source_shed_name`,
			repoTenant, goat.id, repoPerShed)
	}
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Q1 4', 'per_shed_partition', $5::uuid, 0)
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		censusStandaloneBkt, repoCampaign, repoTenant, censusStandaloneShed, repoOperator)
	insertProof(t, ctx, pool, censusStandalonePrf, "video", "completed", "shed", censusStandaloneShed, "shed", censusStandaloneShed)

	_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: censusStandaloneBkt,
		WeightKg:        80,
		ProofArtifactID: censusStandalonePrf, IdempotencyKey: "shed:standalone-q1-4", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrShedCountUnavailable) {
		t.Fatalf("standalone suffix-named shed err=%v, want ErrShedCountUnavailable", err)
	}
	assertNoShedObservationRows(t, ctx, pool, censusStandaloneBkt)
}

func TestLumpSumSubmitDoesNotTreatEmptySuffixNamedShedAsAliasWithoutCatalogSignal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', 'Q1 2', $3::uuid, 'active', 992)
ON CONFLICT (location_id) DO NOTHING`,
		censusRealQ12, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
VALUES ($1::uuid, $2::uuid, '2', '2', 'goat_attested', 2)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET partition_label=EXCLUDED.partition_label, status='active', source=EXCLUDED.source, display_order=EXCLUDED.display_order`,
		repoTenant, repoPerShed)
	for _, goat := range []struct{ id, display string }{
		{censusAliasGoatA, "G-993201"},
		{censusAliasGoatB, "G-993202"},
	} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'female', 'adult', 'alive', 'adult', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO NOTHING`,
			goat.id, repoTenant, goat.display, repoParty, repoPerShed, repoPark)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2', 'Q1')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id=EXCLUDED.shed_id, partition_label=EXCLUDED.partition_label`,
			repoTenant, goat.id, repoPerShed)
	}
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Q1 2', 'per_shed_partition', $5::uuid, 0)
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		censusRealQ12Bkt, repoCampaign, repoTenant, censusRealQ12, repoOperator)
	insertProof(t, ctx, pool, censusRealQ12Prf, "video", "completed", "shed", censusRealQ12, "shed", censusRealQ12)

	_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: censusRealQ12Bkt,
		WeightKg:        80,
		ProofArtifactID: censusRealQ12Prf, IdempotencyKey: "shed:census-real-q1-2", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrShedCountUnavailable) {
		t.Fatalf("empty suffix-named shed err=%v, want ErrShedCountUnavailable", err)
	}
	assertNoShedObservationRows(t, ctx, pool, censusRealQ12Bkt)
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

// TestAliasBackfillBindsOnASeparatorBoundary pins the prefix-collision defect found in
// review. Without a separator the backfill's LIKE matches 'Yashoda 10' against the
// sibling 'Yashoda 1' and derives the pen '0'; because the candidate picks the LONGEST
// matching parent, that wrong sibling wins. So the bare form both risks a false
// permanent mapping AND denies 'Yashoda 10' its correct one, leaving its bucket refused.
// Three such names are live on STG (Yashoda 10, Mandela 1 - Part 10, Mandela 2 - Part 10).
//
// It re-runs the shipped migration file rather than restating its SQL, so the assertion
// cannot drift from what actually ships. The migration is idempotent (ALTER/CREATE ...
// IF NOT EXISTS, and the UPDATE only touches alias_location_id IS NULL).
func TestAliasBackfillBindsOnASeparatorBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)

	const canonical = "00000000-0000-4000-8000-000000009671"
	const siblingOne = "00000000-0000-4000-8000-000000009672"
	const collider = "00000000-0000-4000-8000-000000009673"

	for _, l := range []struct{ id, name string }{
		{canonical, "Zeta"},    // the real shed, carrying pens '1' and '10'
		{siblingOne, "Zeta 1"}, // alias of pen '1'
		{collider, "Zeta 10"},  // alias of pen '10' -- and a bare-prefix match for 'Zeta 1'
	} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', $3, $4::uuid, 'active', 970)
ON CONFLICT (location_id) DO NOTHING`, l.id, repoTenant, l.name, repoPark)
	}
	for _, pen := range []string{"1", "10"} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
VALUES ($1::uuid, $2::uuid, $3, $3, 'goat_attested', 970)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE SET status='active'`,
			repoTenant, canonical, pen)
	}
	// A pen '0' on the sibling is what a bare-prefix match would latch onto.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
VALUES ($1::uuid, $2::uuid, '0', '0', 'goat_attested', 970)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE SET status='active'`,
		repoTenant, siblingOne)

	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations", "postgres", "000239_shed_partitions_active_location_alias.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	execWeighingTestSQL(t, ctx, pool, stripGooseDown(string(migration)))

	var shedName, penLabel string
	if err := pool.QueryRow(ctx, `
SELECT s.name, sp.partition_label
FROM shed_partitions sp JOIN locations s ON s.location_id = sp.shed_id
WHERE sp.tenant_id = $1::uuid AND sp.alias_location_id = $2::uuid`, repoTenant, collider).Scan(&shedName, &penLabel); err != nil {
		t.Fatalf("'Zeta 10' was not mapped at all (the bare-prefix form denies it its own pen): %v", err)
	}
	if shedName != "Zeta" || penLabel != "10" {
		t.Fatalf("'Zeta 10' mapped to %s pen %q, want Zeta pen \"10\"", shedName, penLabel)
	}
	// And the sibling's pen '0' must never have been claimed.
	var wrong int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM shed_partitions
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid AND normalized_label = '0' AND alias_location_id IS NOT NULL`,
		repoTenant, siblingOne).Scan(&wrong); err != nil {
		t.Fatalf("count sibling pen 0: %v", err)
	}
	if wrong != 0 {
		t.Fatalf("bare-prefix match claimed the sibling's pen '0' (%d rows)", wrong)
	}
}

// stripGooseDown keeps only the Up half so re-running the file in a test cannot drop
// the column it just verified.
func stripGooseDown(sql string) string {
	if i := strings.Index(sql, "-- +goose Down"); i >= 0 {
		return sql[:i]
	}
	return sql
}

// TestAliasMappingBeatsAStaleBucketPartitionLabel pins the second review finding. An
// alias location IS exactly one pen, so a partition_label carried on the bucket beside
// it is redundant at best and contradictory at worst. Letting the label win made a
// 'Castro 1' bucket count 'Castro' pen 2. The explicit mapping is the source of truth.
//
// The two pens hold DIFFERENT head counts, so the assertion distinguishes them: reading
// the label gives 5, reading the mapping gives 2.
func TestAliasMappingBeatsAStaleBucketPartitionLabel(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const aliasLoc = "00000000-0000-4000-8000-000000009681"
	const bucket = "00000000-0000-4000-8000-000000009682"
	const proof = "00000000-0000-4000-8000-000000009683"

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', 'Q1 7', $3::uuid, 'active', 980)
ON CONFLICT (location_id) DO NOTHING`, aliasLoc, repoTenant, repoPark)
	// Pen '7' is what the alias maps to; pen '8' is the stale label's target.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order, alias_location_id)
VALUES ($1::uuid, $2::uuid, '7', '7', 'goat_attested', 7, $3::uuid)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET status='active', alias_location_id=EXCLUDED.alias_location_id`, repoTenant, repoPerShed, aliasLoc)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
VALUES ($1::uuid, $2::uuid, '8', '8', 'goat_attested', 8)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE SET status='active'`, repoTenant, repoPerShed)

	// TWO goats in the mapped pen '7', FIVE in the stale label's pen '8'.
	place := func(idx int, pen string) {
		goatID := fmt.Sprintf("00000000-0000-4000-8000-0000000097%02d", idx)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'female', 'adult', 'alive', 'adult', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO NOTHING`,
			goatID, repoTenant, fmt.Sprintf("G-9970%02d", idx), repoParty, repoPerShed, repoPark)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'Q1')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id=EXCLUDED.shed_id, partition_label=EXCLUDED.partition_label`,
			repoTenant, goatID, repoPerShed, pen)
	}
	for i := 1; i <= 2; i++ {
		place(i, "7")
	}
	for i := 11; i <= 15; i++ {
		place(i, "8")
	}

	// The bucket carries a partition_label that CONTRADICTS the alias mapping.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count, partition_label)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Q1 7', 'per_shed_partition', $5::uuid, 0, '8')
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		bucket, repoCampaign, repoTenant, aliasLoc, repoOperator)
	insertProof(t, ctx, pool, proof, "video", "completed", "shed", aliasLoc, "shed", aliasLoc)

	obs, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: bucket,
		WeightKg: 60, ProofArtifactID: proof, IdempotencyKey: "shed:alias-vs-stale-label", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record alias lump sum: %v", err)
	}
	if obs.AnimalCount != 2 {
		t.Fatalf("count=%d -- the stale bucket label won over the alias mapping (pen 8 holds 5, mapped pen 7 holds 2)", obs.AnimalCount)
	}
}
