package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	weightDemoPartitionShed = "00000000-0000-4000-8000-00000000c001"
	weightDemoGoat          = "00000000-0000-4000-8000-00000000c101"
)

func TestWeightDemographicsReturnsRealShedBreedSexCompositionChips(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats
SET breed='Anantapur Sheep', sex='female'
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		repoTenant, repoAnimal)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990002', 'F2', 'male', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET breed=EXCLUDED.breed, sex=EXCLUDED.sex, current_location_id=EXCLUDED.current_location_id, shed_id=EXCLUDED.shed_id`,
		repoAnimalTwo, repoTenant, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES
  ($1::uuid, $2::uuid, 'animal_identifier_1', 'chip-female', 'chip-female', 'global', true, 'active', now(), 'test'),
  ($1::uuid, $3::uuid, 'animal_identifier_1', 'chip-male', 'chip-male', 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id=EXCLUDED.goat_id, identifier_value=EXCLUDED.identifier_value, status='active'`,
		repoTenant, repoAnimal, repoAnimalTwo)
	seedShedWeightScan(t, ctx, pool, "chip-female", 24.0, time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC))
	seedShedWeightScan(t, ctx, pool, "chip-male", 25.0, time.Date(2026, 7, 29, 6, 5, 0, 0, time.UTC))

	from := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to)
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}

	var found bool
	for _, shed := range out.ShedComposition {
		if shed.LocationID != repoExpectedShed {
			continue
		}
		found = true
		if shed.Source != "scanned_tags" {
			t.Fatalf("composition source=%q, want scanned_tags", shed.Source)
		}
		if shed.TotalAnimals != 2 {
			t.Fatalf("composition total=%d, want 2", shed.TotalAnimals)
		}
		assertChip := func(breed, sex string) {
			t.Helper()
			for _, chip := range shed.Chips {
				if chip.Breed == breed && chip.Sex == sex && chip.Animals == 1 {
					return
				}
			}
			t.Fatalf("missing composition chip %q/%q in %#v", breed, sex, shed.Chips)
		}
		assertChip("Anantapur Sheep", "female")
		assertChip("F2", "male")
	}
	if !found {
		t.Fatalf("missing composition for shed %s in %#v", repoExpectedShed, out.ShedComposition)
	}
}

func TestWeightDemographicsLumpGainPartitionOneToManyPageBoundaryParkScopeStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartB, "2026-07-17")
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'Partition Demo Shed', $3::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`,
		weightDemoPartitionShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'WG-PART-1', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET breed=EXCLUDED.breed, sex=EXCLUDED.sex, management_stage=EXCLUDED.management_stage,
    current_location_id=EXCLUDED.current_location_id, park_id=EXCLUDED.park_id, shed_id=EXCLUDED.shed_id`,
		weightDemoGoat, repoTenant, repoParty, weightDemoPartitionShed, repoPark)

	seedLoadBucketPartition(t, ctx, pool, loadPartAOld, loadCampaignPartA, weightDemoPartitionShed, "Part A", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, loadPartANew, loadCampaignPartB, weightDemoPartitionShed, "Part A", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, loadPartBOld, loadCampaignPartA, weightDemoPartitionShed, "Part B", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, loadPartBNew, loadCampaignPartB, weightDemoPartitionShed, "Part B", "per_shed_partition")
	seedLoadLumpWeigh(t, ctx, pool, loadPartAOld, loadCampaignPartA, repoShedProof, 20.0, 10,
		time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, loadPartANew, loadCampaignPartB, repoShedProofTwo, 27.0, 10,
		time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, loadPartBOld, loadCampaignPartA, repoShedProofThree, 30.0, 10,
		time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, loadPartBNew, loadCampaignPartB, repoShedProofFour, 31.0, 10,
		time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC))

	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark},
		time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}

	for _, bucket := range out.GainByBreed {
		if bucket.Label != "Partition Breed" {
			continue
		}
		if bucket.Animals != 20 {
			t.Fatalf("partition breed gain must count both measured partitions: want 20, got %d", bucket.Animals)
		}
		if got := fmt.Sprintf("%.1f", bucket.MedianGainGPerDay); got != "571.4" {
			t.Fatalf("partition breed gain must blend Part A and Part B independently, got %s", got)
		}
		return
	}
	t.Fatalf("missing Partition Breed gain bucket in %#v", out.GainByBreed)
}
