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
	weightDemoSlowGoat      = "00000000-0000-4000-8000-00000000c102"
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

// The daily-gain bands are DISJOINT (maintainer, 2026-08-24, superseding the cumulative
// marks of the same day): a kid at 300 g/day is counted in >250 ONLY. This pins that
// partition and the STRICT comparison at each boundary — a kid at exactly 200 g/day sits
// in the 180-200 band, not the 200-250 band, which is the case a `>=` typo would silently
// flip and no client could detect. It also pins the slowest band, whose whole purpose is
// to hold the kids no cumulative mark ever showed.
//
// All three kids are one breed on purpose: the partition is only observable inside a
// single row.
func TestWeightGainBandsByBreedAreDisjointAndStrictlyGreater(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// The slow kid: a third animal, because the slowest band cannot be observed with two.
	const repoAnimalSlow = "00000000-0000-4000-8000-000000009203"

	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats SET breed='Anantapur Sheep', sex='female'
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, repoTenant, repoAnimal)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES
  ($1::uuid, $2::uuid, 'G-990002', 'Anantapur Sheep', 'male', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid),
  ($6::uuid, $2::uuid, 'G-990003', 'Anantapur Sheep', 'male', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET breed=EXCLUDED.breed, sex=EXCLUDED.sex`,
		repoAnimalTwo, repoTenant, repoParty, repoExpectedShed, repoPark, repoAnimalSlow)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES
  ($1::uuid, $2::uuid, 'animal_identifier_1', 'chip-female', 'chip-female', 'global', true, 'active', now(), 'test'),
  ($1::uuid, $3::uuid, 'animal_identifier_1', 'chip-male', 'chip-male', 'global', true, 'active', now(), 'test'),
  ($1::uuid, $4::uuid, 'animal_identifier_1', 'chip-slow', 'chip-slow', 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id=EXCLUDED.goat_id, identifier_value=EXCLUDED.identifier_value, status='active'`,
		repoTenant, repoAnimal, repoAnimalTwo, repoAnimalSlow)

	// 10 days apart: 3.0 kg -> 300 g/day, exactly 2.0 kg -> 200 g/day, 1.5 kg -> 150 g/day.
	seedShedWeightScan(t, ctx, pool, "chip-female", 20.0, time.Date(2026, 7, 19, 6, 0, 0, 0, time.UTC))
	seedShedWeightScan(t, ctx, pool, "chip-female", 23.0, time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC))
	seedShedWeightScan(t, ctx, pool, "chip-male", 20.0, time.Date(2026, 7, 19, 6, 5, 0, 0, time.UTC))
	seedShedWeightScan(t, ctx, pool, "chip-male", 22.0, time.Date(2026, 7, 29, 6, 5, 0, 0, time.UTC))
	seedShedWeightScan(t, ctx, pool, "chip-slow", 20.0, time.Date(2026, 7, 19, 6, 10, 0, 0, time.UTC))
	seedShedWeightScan(t, ctx, pool, "chip-slow", 21.5, time.Date(2026, 7, 29, 6, 10, 0, 0, time.UTC))

	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark},
		time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}

	row, found := findGainThresholdRow(out.GainThresholdsByBreed, "Anantapur Sheep")
	if !found {
		t.Fatalf("no Anantapur Sheep gain-band row in %#v", out.GainThresholdsByBreed)
	}
	if row.Animals != 3 {
		t.Fatalf("animals=%d, want 3 (all three kids have a second weigh)", row.Animals)
	}
	if row.Above250 != 1 {
		t.Fatalf("above 250=%d, want 1 (only the 300 g/day kid)", row.Above250)
	}
	// The 300 g/day kid must NOT appear here too — that is the cumulative behaviour this
	// change removed, and it is the one regression a reader of the numbers cannot see.
	if row.Band200To250 != 0 {
		t.Fatalf("200-250=%d, want 0 — the 300 g/day kid belongs to the top band only", row.Band200To250)
	}
	// Strictly greater at the top: exactly 200 g/day is NOT above 200.
	if row.Band180To200 != 1 {
		t.Fatalf("180-200=%d, want 1 — the 200 g/day kid sits at the top of this band", row.Band180To200)
	}
	if row.AtOrBelow180 != 1 {
		t.Fatalf("at or below 180=%d, want 1 (the 150 g/day kid)", row.AtOrBelow180)
	}

	// THE SAME BANDS, PER SEX (maintainer, 2026-08-25). The fixture splits cleanly: the one female
	// kid is the 300 g/day one, and both males are the 200 and the 150. So a per-sex row that
	// quietly returned the whole breed — the failure a same-label lookup would hide — cannot pass
	// here, because the female row would then carry three kids and two bands that are not hers.
	female, found := findGainThresholdSexRow(out.GainThresholdsByBreed, "Anantapur Sheep", "female")
	if !found {
		t.Fatalf("no female Anantapur Sheep gain-band row in %#v", out.GainThresholdsByBreed)
	}
	if female.Animals != 1 || female.Above250 != 1 || female.Band200To250 != 0 || female.Band180To200 != 0 || female.AtOrBelow180 != 0 {
		t.Fatalf("the female kid is the 300 g/day one and belongs to the top band alone, got %#v", female)
	}
	male, found := findGainThresholdSexRow(out.GainThresholdsByBreed, "Anantapur Sheep", "male")
	if !found {
		t.Fatalf("no male Anantapur Sheep gain-band row in %#v", out.GainThresholdsByBreed)
	}
	if male.Animals != 2 || male.Above250 != 0 || male.Band200To250 != 0 || male.Band180To200 != 1 || male.AtOrBelow180 != 1 {
		t.Fatalf("the two male kids are the 200 and the 150 g/day ones, got %#v", male)
	}
	// The two grains agree about who was counted: a breed's per-sex rows add up to its combined
	// row, band for band. They OVERLAP by construction, which is exactly why a client renders one
	// grain at a time — this is the arithmetic that makes summing them a double count.
	if male.Animals+female.Animals != row.Animals {
		t.Fatalf("per-sex animals %d+%d do not add up to the combined %d", male.Animals, female.Animals, row.Animals)
	}
	if male.Above250+female.Above250 != row.Above250 ||
		male.Band200To250+female.Band200To250 != row.Band200To250 ||
		male.Band180To200+female.Band180To200 != row.Band180To200 ||
		male.AtOrBelow180+female.AtOrBelow180 != row.AtOrBelow180 {
		t.Fatalf("per-sex bands do not add up to the combined row: male=%#v female=%#v combined=%#v", male, female, row)
	}
	// Each per-sex row is its own denominator: the four bands partition ITS animals, so a client
	// may take a share row-locally without reaching for the combined total.
	for _, sexRow := range []domain.WeightGainThresholdBucket{male, female} {
		if sum := sexRow.AtOrBelow180 + sexRow.Band180To200 + sexRow.Band200To250 + sexRow.Above250; sum != sexRow.Animals {
			t.Fatalf("bands sum to %d but animals=%d on the %s row: %#v", sum, sexRow.Animals, sexRow.Sex, sexRow)
		}
	}
	// The partition, stated as the invariant a client relies on to render the bands as a
	// distribution: every kid with a gain lands in exactly one band.
	if sum := row.AtOrBelow180 + row.Band180To200 + row.Band200To250 + row.Above250; sum != row.Animals {
		t.Fatalf("bands sum to %d but animals=%d — not a partition: %#v", sum, row.Animals, row)
	}

	// Same population as the median gain chart beside it, so the two cannot disagree about
	// how many kids of a breed were weighed twice.
	for _, gain := range out.GainByBreed {
		if gain.Label == "Anantapur Sheep" && gain.Animals != row.Animals {
			t.Fatalf("gain_by_breed animals=%d but band animals=%d — different populations", gain.Animals, row.Animals)
		}
	}
}

func TestWeightGainBandsUseLatestSameDayObservationBeforePairing(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		goatID = "00000000-0000-4000-8000-0000000ee001"
		breed  = "Same Day Band Breed"
		tag    = "same-day-band"
	)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-991001', $3, 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE SET breed=EXCLUDED.breed`,
		goatID, repoTenant, breed, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, $3, 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE SET goat_id=EXCLUDED.goat_id, status='active'`,
		repoTenant, goatID, tag)

	seedShedWeightScan(t, ctx, pool, tag, 20.0, time.Date(2026, 7, 19, 6, 0, 0, 0, time.UTC))
	// Same business day, different band sides. The later same-day row must win before
	// lag() pairs days, or this animal lands in the wrong band while still looking valid.
	seedShedWeightScan(t, ctx, pool, tag, 22.0, time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC))
	seedShedWeightScan(t, ctx, pool, tag, 23.0, time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC))

	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark},
		time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}
	row, found := findGainThresholdRow(out.GainThresholdsByBreed, breed)
	if !found {
		t.Fatalf("missing %q gain-band row in %#v", breed, out.GainThresholdsByBreed)
	}
	if row.Animals != 1 || row.Above250 != 1 || row.Band200To250 != 0 || row.Band180To200 != 0 || row.AtOrBelow180 != 0 {
		t.Fatalf("same-day latest observation was not used before band pairing: %#v", row)
	}
}

// The adversarial pass over the SAME aggregate: fan-out, page boundary, date shift, park
// scope and status buckets. Each is a way this row can lie while still looking like a
// plausible growth figure, and none of them is visible from the number itself.
//
// OneToMany = many captures per tag collapse to one animal; PageBoundary = the row is a
// whole-window figure, larger than any page size the table declares; DateShift = a pair
// whose latest weigh falls outside the window is not counted; ParkScope = another park's
// kids never reach this park's row; StatusBuckets = every capture status this table allows
// (pending, verified, rework) is counted.
func TestWeightGainThresholdsOneToManyPageBoundaryDateShiftParkScopeStatusBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const breed = "Threshold Breed"
	earlier := time.Date(2026, 7, 19, 6, 0, 0, 0, time.UTC)
	latest := time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC)

	// PAGE BOUNDARY. Twelve kids — more than the smallest page size the table contract
	// declares (10). This row is a whole-window aggregate, so it must count all twelve; a
	// LIMIT slipped into it would report a page of them as the herd.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
SELECT ('00000000-0000-4000-8000-0000000' || lpad(n::text, 5, 'd'))::uuid, $1::uuid,
       'G-91' || lpad(n::text, 4, '0'), $2, 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid
FROM generate_series(1, 12) n
ON CONFLICT (goat_id) DO UPDATE SET breed=EXCLUDED.breed`,
		repoTenant, breed, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
SELECT $1::uuid, ('00000000-0000-4000-8000-0000000' || lpad(n::text, 5, 'd'))::uuid,
       'animal_identifier_1', 'thresh-' || n, 'thresh-' || n, 'global', true, 'active', now(), 'test'
FROM generate_series(1, 12) n
ON CONFLICT (tenant_id, normalized_value) DO UPDATE SET status='active'`, repoTenant)

	for n := 1; n <= 12; n++ {
		tag := fmt.Sprintf("thresh-%d", n)
		// Every kid gains 3.0 kg over 10 days = 300 g/day, so all twelve land in the top band.
		seedShedWeightScan(t, ctx, pool, tag, 20.0, earlier.Add(time.Duration(n)*time.Minute))
		seedShedWeightScan(t, ctx, pool, tag, 23.0, latest.Add(time.Duration(n)*time.Minute))
	}

	// ONE-TO-MANY. One kid re-scanned twice more on the latest day. weighing_observations
	// keeps superseded rows, so a per-CAPTURE count would report this kid three times and
	// inflate the denominator and every mark above it.
	seedShedWeightScan(t, ctx, pool, "thresh-1", 22.5, latest.Add(2*time.Hour))
	seedShedWeightScan(t, ctx, pool, "thresh-1", 23.0, latest.Add(4*time.Hour))

	// THE SLOWEST BAND, ON THE SAME AXES. A kid gaining 1.5 kg over 10 days = 150 g/day,
	// re-scanned once (OneToMany) and left in a non-default capture status (StatusBuckets).
	// The band added by this change has to survive every adversarial dimension the other
	// three do; asserting it only in the clean two-kid fixture would prove it in the one
	// setting where nothing can go wrong.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-920002', $3, 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE SET breed=EXCLUDED.breed`,
		weightDemoSlowGoat, repoTenant, breed, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'thresh-slow', 'thresh-slow', 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE SET status='active'`, repoTenant, weightDemoSlowGoat)
	seedShedWeightScan(t, ctx, pool, "thresh-slow", 20.0, earlier.Add(30*time.Minute))
	seedShedWeightScan(t, ctx, pool, "thresh-slow", 21.5, latest.Add(30*time.Minute))
	seedShedWeightScan(t, ctx, pool, "thresh-slow", 21.5, latest.Add(3*time.Hour))

	// DATE SHIFT. A further kid weighed twice, both weighs BEFORE the window. Its pair is
	// perfectly computable — it is simply not in the period the reader asked about.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-920001', $3, 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE SET breed=EXCLUDED.breed`,
		weightDemoGoat, repoTenant, breed, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'thresh-old', 'thresh-old', 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE SET status='active'`, repoTenant, weightDemoGoat)
	seedShedWeightScan(t, ctx, pool, "thresh-old", 20.0, time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC))
	seedShedWeightScan(t, ctx, pool, "thresh-old", 26.0, time.Date(2026, 7, 11, 6, 0, 0, 0, time.UTC))

	// STATUS BUCKETS. weighing_observations allows exactly pending / verified / rework —
	// there is no 'rejected' capture state on this table — so the row must count a kid in
	// EVERY one of them. A `verification_status = 'verified'` slipped into the aggregate
	// would look like a tightening and would quietly report one kid where twelve were weighed.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_observations SET verification_status='verified'
WHERE tenant_id=$1::uuid AND lower(btrim(scanned_identifier))='thresh-2'`, repoTenant)
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_observations SET verification_status='rework'
WHERE tenant_id=$1::uuid AND lower(btrim(scanned_identifier))='thresh-3'`, repoTenant)
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_observations SET verification_status='verified'
WHERE tenant_id=$1::uuid AND lower(btrim(scanned_identifier))='thresh-slow'`, repoTenant)

	windowFrom := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	windowTo := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, windowFrom, windowTo)
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}

	row, found := findGainThresholdRow(out.GainThresholdsByBreed, breed)
	if !found {
		t.Fatalf("missing %q gain-band row in %#v", breed, out.GainThresholdsByBreed)
	}
	if row.Animals != 13 {
		t.Fatalf("animals=%d, want 13: twelve fast kids plus the slow one, every capture status counted, each re-scanned kid counted ONCE, the out-of-window kid not at all", row.Animals)
	}
	if row.Above250 != 12 || row.Band200To250 != 0 || row.Band180To200 != 0 || row.AtOrBelow180 != 1 {
		t.Fatalf("twelve kids at 300 g/day belong to the top band and the 150 g/day kid to the slowest, got %#v", row)
	}
	// The partition holds under fan-out, the page boundary, the date shift and the status
	// matrix together — not only in the clean fixture.
	if sum := row.AtOrBelow180 + row.Band180To200 + row.Band200To250 + row.Above250; sum != row.Animals {
		t.Fatalf("bands sum to %d but animals=%d under the adversarial fixture: %#v", sum, row.Animals, row)
	}

	// PARK SCOPE. The same window under a park these kids are not in returns nothing for this
	// breed — the park filter is a real predicate, not a label on an unscoped aggregate.
	otherPark, err := repo.GetWeightDemographics(ctx, repoTenant, []string{weightDemoGodelShed}, windowFrom, windowTo)
	if err != nil {
		t.Fatalf("GetWeightDemographics(other park): %v", err)
	}
	if _, leaked := findGainThresholdRow(otherPark.GainThresholdsByBreed, breed); leaked {
		t.Fatalf("breed %q leaked across the park scope: %#v", breed, otherPark.GainThresholdsByBreed)
	}
	// ParkScope for the band added by this change specifically: the slow kid is one park's
	// fact, and a leak would show up as another park's slowest band gaining a member —
	// a far quieter lie than a whole breed row appearing where it does not belong.
	for _, parkScopeRow := range otherPark.GainThresholdsByBreed {
		if parkScopeRow.AtOrBelow180 != 0 {
			t.Fatalf("the slow kid leaked into another park's slowest band: %#v", parkScopeRow)
		}
	}

	// PageBoundary for the same row: this aggregate is a WHOLE-WINDOW figure, so it must
	// stand above the smallest page size the gain table's contract declares (10). A LIMIT
	// slipped into the aggregate would still return a plausible-looking distribution.
	const pageBoundarySmallestPageSize = 10
	if row.Animals <= pageBoundarySmallestPageSize {
		t.Fatalf("animals=%d does not cross the %d-row page boundary, so this fixture cannot detect a LIMIT in the aggregate", row.Animals, pageBoundarySmallestPageSize)
	}
}

// findGainThresholdRow returns the COMBINED row for a breed — every kid of it, which is the grain
// the card opens on. Each breed is also emitted once per sex (maintainer, 2026-08-25), and those
// rows carry a subset of the same kids, so matching on the label alone would let a caller assert a
// whole breed's distribution against one sex's.
func findGainThresholdRow(rows []domain.WeightGainThresholdBucket, label string) (domain.WeightGainThresholdBucket, bool) {
	return findGainThresholdSexRow(rows, label, "")
}

func findGainThresholdSexRow(rows []domain.WeightGainThresholdBucket, label, sex string) (domain.WeightGainThresholdBucket, bool) {
	for _, row := range rows {
		if row.Label == label && row.Sex == sex {
			return row, true
		}
	}
	return domain.WeightGainThresholdBucket{}, false
}
