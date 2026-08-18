package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

const (
	weightDemoPartitionShed = "00000000-0000-4000-8000-00000000c001"
	weightDemoGoat          = "00000000-0000-4000-8000-00000000c101"
	weightDemoGodelShed     = "00000000-0000-4000-8000-00000000c201"
	weightDemoCastroShed    = "00000000-0000-4000-8000-00000000c202"
	weightDemoCastroOne     = "00000000-0000-4000-8000-00000000c203"
	weightDemoCastroTwo     = "00000000-0000-4000-8000-00000000c204"
	weightDemoCastroThree   = "00000000-0000-4000-8000-00000000c205"
	weightDemoGandhiShed    = "00000000-0000-4000-8000-00000000c206"
	weightDemoGandhiOne     = "00000000-0000-4000-8000-00000000c207"
	weightDemoGandhiTwo     = "00000000-0000-4000-8000-00000000c208"
	weightDemoGandhiThree   = "00000000-0000-4000-8000-00000000c209"
	weightDemoGandiShed     = "00000000-0000-4000-8000-00000000c20a"
	weightDemoGandiOne      = "00000000-0000-4000-8000-00000000c20b"
	weightDemoGandiTwo      = "00000000-0000-4000-8000-00000000c20c"
	weightDemoGandiThree    = "00000000-0000-4000-8000-00000000c20d"
	weightDemoPlainOneShed  = "00000000-0000-4000-8000-00000000c20e"
	weightDemoGodelPart1    = "00000000-0000-4000-8000-00000000c301"
	weightDemoGodelPart2    = "00000000-0000-4000-8000-00000000c302"
	weightDemoCastroPart1   = "00000000-0000-4000-8000-00000000c303"
	weightDemoCastroPart2   = "00000000-0000-4000-8000-00000000c304"
	weightDemoCastroPart3   = "00000000-0000-4000-8000-00000000c305"
	weightDemoGandhiPart1   = "00000000-0000-4000-8000-00000000c306"
	weightDemoGandhiPart2   = "00000000-0000-4000-8000-00000000c307"
	weightDemoGandhiPart3   = "00000000-0000-4000-8000-00000000c308"
	weightDemoGandiPart1    = "00000000-0000-4000-8000-00000000c309"
	weightDemoGandiPart2    = "00000000-0000-4000-8000-00000000c30a"
	weightDemoGandiPart3    = "00000000-0000-4000-8000-00000000c30b"
	weightDemoPlainOneGoat  = "00000000-0000-4000-8000-00000000c30c"
	weightDemoGodelBucket   = "00000000-0000-4000-8000-00000000c401"
	weightDemoCastroBucket  = "00000000-0000-4000-8000-00000000c402"
	weightDemoCastro2Bucket = "00000000-0000-4000-8000-00000000c403"
	weightDemoCastro3Bucket = "00000000-0000-4000-8000-00000000c404"
	weightDemoGandhi1Bucket = "00000000-0000-4000-8000-00000000c405"
	weightDemoGandhi2Bucket = "00000000-0000-4000-8000-00000000c406"
	weightDemoGandhi3Bucket = "00000000-0000-4000-8000-00000000c407"
	weightDemoGandi1Bucket  = "00000000-0000-4000-8000-00000000c408"
	weightDemoGandi2Bucket  = "00000000-0000-4000-8000-00000000c409"
	weightDemoGandi3Bucket  = "00000000-0000-4000-8000-00000000c40a"
	weightDemoPlainBucket   = "00000000-0000-4000-8000-00000000c40b"
	weightDemoGodelProof    = "00000000-0000-4000-8000-00000000c501"
	weightDemoCastroProof   = "00000000-0000-4000-8000-00000000c502"
	weightDemoPlainProof    = "00000000-0000-4000-8000-00000000c503"
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

func TestWeightDemographicsLumpCompositionResolvesPhysicalShedPartitions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES
  ($1::uuid, $4::uuid, 'shed', 'Godel 2', $5::uuid, 'active'),
  ($2::uuid, $4::uuid, 'shed', 'Castro', $5::uuid, 'active'),
  ($3::uuid, $4::uuid, 'shed', 'Castro 1', $5::uuid, 'active'),
  ($6::uuid, $4::uuid, 'shed', 'Castro 2', $5::uuid, 'active'),
  ($7::uuid, $4::uuid, 'shed', 'Castro 3', $5::uuid, 'active'),
  ($8::uuid, $4::uuid, 'shed', 'Gandhi', $5::uuid, 'active'),
  ($9::uuid, $4::uuid, 'shed', 'Gandhi 1', $5::uuid, 'active'),
  ($10::uuid, $4::uuid, 'shed', 'Gandhi 2', $5::uuid, 'active'),
  ($11::uuid, $4::uuid, 'shed', 'Gandhi 3', $5::uuid, 'active'),
  ($12::uuid, $4::uuid, 'shed', 'Gandi', $5::uuid, 'active'),
  ($13::uuid, $4::uuid, 'shed', 'Gandi 1', $5::uuid, 'active'),
  ($14::uuid, $4::uuid, 'shed', 'Gandi 2', $5::uuid, 'active'),
  ($15::uuid, $4::uuid, 'shed', 'Gandi 3', $5::uuid, 'active'),
  ($16::uuid, $4::uuid, 'shed', 'Plain 1', $5::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO UPDATE SET name=EXCLUDED.name, parent_location_id=EXCLUDED.parent_location_id`,
		weightDemoGodelShed, weightDemoCastroShed, weightDemoCastroOne, repoTenant, repoPark,
		weightDemoCastroTwo, weightDemoCastroThree, weightDemoGandhiShed, weightDemoGandhiOne,
		weightDemoGandhiTwo, weightDemoGandhiThree, weightDemoGandiShed, weightDemoGandiOne,
		weightDemoGandiTwo, weightDemoGandiThree, weightDemoPlainOneShed)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES
  ($1::uuid, $4::uuid, 'WG-GODEL-P1', 'Anantapur Sheep', 'male', 'kid', 'alive', 'F2-Male', $5::uuid, $6::uuid, $8::uuid, $6::uuid),
  ($2::uuid, $4::uuid, 'WG-GODEL-P2', 'Beetal', 'female', 'kid', 'alive', 'F2-Female', $5::uuid, $6::uuid, $8::uuid, $6::uuid),
  ($3::uuid, $4::uuid, 'WG-CASTRO-1', 'Anantapur Sheep', 'male', 'kid', 'alive', 'F2-Male', $5::uuid, $7::uuid, $8::uuid, $7::uuid),
  ($9::uuid, $4::uuid, 'WG-CASTRO-2', 'Beetal', 'female', 'kid', 'alive', 'F2-Female', $5::uuid, $7::uuid, $8::uuid, $7::uuid),
  ($10::uuid, $4::uuid, 'WG-CASTRO-3', 'Sirohi', 'male', 'kid', 'alive', 'K3-Male', $5::uuid, $7::uuid, $8::uuid, $7::uuid),
  ($11::uuid, $4::uuid, 'WG-GANDHI-1', 'Malai', 'female', 'kid', 'alive', 'F2-Female', $5::uuid, $12::uuid, $8::uuid, $12::uuid),
  ($13::uuid, $4::uuid, 'WG-GANDHI-2', 'Sojat', 'male', 'kid', 'alive', 'F2-Male', $5::uuid, $12::uuid, $8::uuid, $12::uuid),
  ($14::uuid, $4::uuid, 'WG-GANDHI-3', 'Osmanabadi', 'female', 'kid', 'alive', 'K3-Female', $5::uuid, $12::uuid, $8::uuid, $12::uuid),
  ($15::uuid, $4::uuid, 'WG-GANDI-1', 'Malai', 'male', 'kid', 'alive', 'F2-Male', $5::uuid, $16::uuid, $8::uuid, $16::uuid),
  ($17::uuid, $4::uuid, 'WG-GANDI-2', 'Sojat', 'female', 'kid', 'alive', 'F2-Female', $5::uuid, $16::uuid, $8::uuid, $16::uuid),
  ($18::uuid, $4::uuid, 'WG-GANDI-3', 'Osmanabadi', 'male', 'kid', 'alive', 'K3-Male', $5::uuid, $16::uuid, $8::uuid, $16::uuid),
  ($19::uuid, $4::uuid, 'WG-PLAIN-1', 'Plain Breed', 'female', 'kid', 'alive', 'Plain-Stage', $5::uuid, $20::uuid, $8::uuid, $20::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET breed=EXCLUDED.breed, sex=EXCLUDED.sex, management_stage=EXCLUDED.management_stage,
    current_location_id=EXCLUDED.current_location_id, park_id=EXCLUDED.park_id, shed_id=EXCLUDED.shed_id`,
		weightDemoGodelPart1, weightDemoGodelPart2, weightDemoCastroPart1,
		repoTenant, repoParty, weightDemoGodelShed, weightDemoCastroShed, repoPark,
		weightDemoCastroPart2, weightDemoCastroPart3,
		weightDemoGandhiPart1, weightDemoGandhiShed, weightDemoGandhiPart2, weightDemoGandhiPart3,
		weightDemoGandiPart1, weightDemoGandiShed, weightDemoGandiPart2, weightDemoGandiPart3,
		weightDemoPlainOneGoat, weightDemoPlainOneShed)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES
  ($1::uuid, $2::uuid, $5::uuid, 'Part 1', 'Godel 2 - Part 1'),
  ($1::uuid, $3::uuid, $5::uuid, 'Part 2', 'Godel 2 - Part 2'),
  ($1::uuid, $4::uuid, $6::uuid, '1', 'Castro 1'),
  ($1::uuid, $7::uuid, $6::uuid, '2', 'Castro 2'),
  ($1::uuid, $8::uuid, $6::uuid, '3', 'Castro 3'),
  ($1::uuid, $9::uuid, $10::uuid, '1', 'Gandhi 1'),
  ($1::uuid, $11::uuid, $10::uuid, '2', 'Gandhi 2'),
  ($1::uuid, $12::uuid, $10::uuid, '3', 'Gandhi 3'),
  ($1::uuid, $13::uuid, $14::uuid, '1', 'Gandi 1'),
  ($1::uuid, $15::uuid, $14::uuid, '2', 'Gandi 2'),
  ($1::uuid, $16::uuid, $14::uuid, '3', 'Gandi 3')
ON CONFLICT (tenant_id, goat_id) DO UPDATE
SET shed_id=EXCLUDED.shed_id, partition_label=EXCLUDED.partition_label, source_shed_name=EXCLUDED.source_shed_name`,
		repoTenant, weightDemoGodelPart1, weightDemoGodelPart2, weightDemoCastroPart1,
		weightDemoGodelShed, weightDemoCastroShed,
		weightDemoCastroPart2, weightDemoCastroPart3,
		weightDemoGandhiPart1, weightDemoGandhiShed, weightDemoGandhiPart2, weightDemoGandhiPart3,
		weightDemoGandiPart1, weightDemoGandiShed, weightDemoGandiPart2, weightDemoGandiPart3)

	insertProof(t, ctx, pool, weightDemoGodelProof, "video", "completed", "shed", weightDemoGodelShed, "shed", weightDemoGodelShed)
	insertProof(t, ctx, pool, weightDemoCastroProof, "video", "completed", "shed", weightDemoCastroOne, "shed", weightDemoCastroOne)
	insertProof(t, ctx, pool, weightDemoPlainProof, "video", "completed", "shed", weightDemoPlainOneShed, "shed", weightDemoPlainOneShed)
	seedLoadBucketPartition(t, ctx, pool, weightDemoGodelBucket, loadCampaignPartA, weightDemoGodelShed, "Part 1", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, weightDemoCastroBucket, loadCampaignPartA, weightDemoCastroOne, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, weightDemoCastro2Bucket, loadCampaignPartA, weightDemoCastroTwo, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, weightDemoCastro3Bucket, loadCampaignPartA, weightDemoCastroThree, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, weightDemoGandhi1Bucket, loadCampaignPartA, weightDemoGandhiOne, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, weightDemoGandhi2Bucket, loadCampaignPartA, weightDemoGandhiTwo, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, weightDemoGandhi3Bucket, loadCampaignPartA, weightDemoGandhiThree, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, weightDemoGandi1Bucket, loadCampaignPartA, weightDemoGandiOne, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, weightDemoGandi2Bucket, loadCampaignPartA, weightDemoGandiTwo, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, weightDemoGandi3Bucket, loadCampaignPartA, weightDemoGandiThree, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, weightDemoPlainBucket, loadCampaignPartA, weightDemoPlainOneShed, "", "per_shed_partition")
	seedLoadLumpWeigh(t, ctx, pool, weightDemoGodelBucket, loadCampaignPartA, weightDemoGodelProof, 25.0, 1,
		time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, weightDemoCastroBucket, loadCampaignPartA, weightDemoCastroProof, 24.0, 1,
		time.Date(2026, 7, 10, 6, 5, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, weightDemoCastro2Bucket, loadCampaignPartA, weightDemoCastroProof, 24.5, 1,
		time.Date(2026, 7, 10, 6, 6, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, weightDemoCastro3Bucket, loadCampaignPartA, weightDemoCastroProof, 24.6, 1,
		time.Date(2026, 7, 10, 6, 7, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, weightDemoGandhi1Bucket, loadCampaignPartA, weightDemoCastroProof, 24.7, 1,
		time.Date(2026, 7, 10, 6, 8, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, weightDemoGandhi2Bucket, loadCampaignPartA, weightDemoCastroProof, 24.8, 1,
		time.Date(2026, 7, 10, 6, 9, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, weightDemoGandhi3Bucket, loadCampaignPartA, weightDemoCastroProof, 24.9, 1,
		time.Date(2026, 7, 10, 6, 10, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, weightDemoGandi1Bucket, loadCampaignPartA, weightDemoCastroProof, 25.1, 1,
		time.Date(2026, 7, 10, 6, 11, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, weightDemoGandi2Bucket, loadCampaignPartA, weightDemoCastroProof, 25.2, 1,
		time.Date(2026, 7, 10, 6, 12, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, weightDemoGandi3Bucket, loadCampaignPartA, weightDemoCastroProof, 25.3, 1,
		time.Date(2026, 7, 10, 6, 13, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, weightDemoPlainBucket, loadCampaignPartA, weightDemoPlainProof, 23.0, 1,
		time.Date(2026, 7, 10, 6, 14, 0, 0, time.UTC))

	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark},
		time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}

	assertCompositionChip(t, out.ShedComposition, weightDemoGodelShed, "Part 1", "live_shed_cohort", "F2-Male", "Anantapur Sheep", "male", 1)
	assertNoCompositionChip(t, out.ShedComposition, weightDemoGodelShed, "Part 1", "F2-Female", "Beetal", "female")
	assertCompositionChip(t, out.ShedComposition, weightDemoCastroOne, "", "live_shed_cohort", "F2-Male", "Anantapur Sheep", "male", 1)
	assertNoCompositionChip(t, out.ShedComposition, weightDemoCastroOne, "", "F2-Female", "Beetal", "female")
	assertNoCompositionChip(t, out.ShedComposition, weightDemoCastroOne, "", "K3-Male", "Sirohi", "male")
	assertNoCompositionChip(t, out.ShedComposition, weightDemoCastroOne, "", "F2-Female", "Malai", "female")
	assertNoCompositionChip(t, out.ShedComposition, weightDemoCastroOne, "", "F2-Male", "Malai", "male")
	assertCompositionChip(t, out.ShedComposition, weightDemoCastroTwo, "", "live_shed_cohort", "F2-Female", "Beetal", "female", 1)
	assertNoCompositionChip(t, out.ShedComposition, weightDemoCastroTwo, "", "F2-Male", "Anantapur Sheep", "male")
	assertCompositionChip(t, out.ShedComposition, weightDemoCastroThree, "", "live_shed_cohort", "K3-Male", "Sirohi", "male", 1)
	assertCompositionChip(t, out.ShedComposition, weightDemoGandhiOne, "", "live_shed_cohort", "F2-Female", "Malai", "female", 1)
	assertCompositionChip(t, out.ShedComposition, weightDemoGandhiTwo, "", "live_shed_cohort", "F2-Male", "Sojat", "male", 1)
	assertCompositionChip(t, out.ShedComposition, weightDemoGandhiThree, "", "live_shed_cohort", "K3-Female", "Osmanabadi", "female", 1)
	assertCompositionChip(t, out.ShedComposition, weightDemoGandiOne, "", "live_shed_cohort", "F2-Male", "Malai", "male", 1)
	assertCompositionChip(t, out.ShedComposition, weightDemoGandiTwo, "", "live_shed_cohort", "F2-Female", "Sojat", "female", 1)
	assertCompositionChip(t, out.ShedComposition, weightDemoGandiThree, "", "live_shed_cohort", "K3-Male", "Osmanabadi", "male", 1)
	assertCompositionChip(t, out.ShedComposition, weightDemoPlainOneShed, "", "live_shed_cohort", "Plain-Stage", "Plain Breed", "female", 1)
}

func TestWeightDemographicsLumpGainPartitionOneToManyPageBoundaryParkScopeStatusBuckets(t *testing.T) {
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

func assertCompositionChip(t *testing.T, compositions []domain.ShedComposition, locationID, partitionLabel, source, stage, breed, sex string, animals int) {
	t.Helper()
	for _, composition := range compositions {
		if composition.LocationID != locationID || composition.PartitionLabel != partitionLabel {
			continue
		}
		if composition.Source != source {
			t.Fatalf("composition %s/%s source=%q, want %q", locationID, partitionLabel, composition.Source, source)
		}
		for _, chip := range composition.Chips {
			if chip.Stage == stage && chip.Breed == breed && chip.Sex == sex && chip.Animals == animals {
				return
			}
		}
		t.Fatalf("missing composition chip %q/%q/%q x%d in %#v", stage, breed, sex, animals, composition.Chips)
	}
	t.Fatalf("missing composition for %s/%s in %#v", locationID, partitionLabel, compositions)
}

func assertNoCompositionChip(t *testing.T, compositions []domain.ShedComposition, locationID, partitionLabel, stage, breed, sex string) {
	t.Helper()
	for _, composition := range compositions {
		if composition.LocationID != locationID || composition.PartitionLabel != partitionLabel {
			continue
		}
		for _, chip := range composition.Chips {
			if chip.Stage == stage && chip.Breed == breed && chip.Sex == sex {
				t.Fatalf("composition %s/%s leaked sibling partition chip %#v", locationID, partitionLabel, chip)
			}
		}
		return
	}
	t.Fatalf("missing composition for %s/%s in %#v", locationID, partitionLabel, compositions)
}
