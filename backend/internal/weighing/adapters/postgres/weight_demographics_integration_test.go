package postgres

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

const (
	weightDemoPartitionShed = "00000000-0000-4000-8000-00000000c001"
	weightDemoGoat          = "00000000-0000-4000-8000-00000000c101"
	weightDemoGoatTwo       = "00000000-0000-4000-8000-00000000c103"
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
	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "")
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
		time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC), "", "", "")
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
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), "", "", "")
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
		time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC), "", "", "")
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
		time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC), "", "", "")
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
	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, windowFrom, windowTo, "", "", "")
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
	otherPark, err := repo.GetWeightDemographics(ctx, repoTenant, []string{weightDemoGodelShed}, windowFrom, windowTo, "", "", "")
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

func findGainThresholdRow(rows []domain.WeightGainThresholdBucket, label string) (domain.WeightGainThresholdBucket, bool) {
	for _, row := range rows {
		if row.Label == label {
			return row, true
		}
	}
	return domain.WeightGainThresholdBucket{}, false
}

// A PEN WEIGHED TWICE INSIDE THE WINDOW IS ONE PEN, NOT TWO.
//
// The defect this pins, found on real STG data (2026-08-26): the lump CTE joined every live
// weighing_shed_observations row, so a pen weighed on both the 17th and the 24th contributed its
// whole head count ONCE PER WEIGH. This is not an edge case -- the page's default window IS "the
// last two whole-shed weigh dates", so it fired on every landing. Castro 1 carried four live
// observations and its 63 kids were counted 252 times over; STG's 276 whole-shed kids reached the
// charts as 678, and "Average weight by sex" reported 908 kids on a page whose own headline said
// 791 had been weighed. The gain charts were already correct (lump_span keeps rn=1 per pen), which
// is exactly why the two halves of the page disagreed with each other.
//
// Part A and Part B are each weighed twice in the window, ten animals apiece: the honest lump
// population is 20, and the pre-fix query returned 40.
func TestWeightDemographicsCountsAPenWeighedTwiceOnlyOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartB, "2026-07-17")
	for _, proofID := range []string{repoShedProofTwo, repoShedProofThree, repoShedProofFour} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'Partition Demo Shed', $3::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`,
		weightDemoPartitionShed, repoTenant, repoPark)
	// One resident per pen, both the same breed and sex, so each pen is a homogeneous cohort the
	// whole-shed attribution rule will actually claim -- without a goat_shed_partitions row naming
	// the pen, shed_cohort resolves to nothing and neither chart would show the pen at all.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990913', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid),
       ($3::uuid, $2::uuid, 'G-990914', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET breed=EXCLUDED.breed, sex=EXCLUDED.sex, management_stage=EXCLUDED.management_stage,
    current_location_id=EXCLUDED.current_location_id, park_id=EXCLUDED.park_id, shed_id=EXCLUDED.shed_id`,
		weightDemoGoat, repoTenant, weightDemoGoatTwo, repoParty, weightDemoPartitionShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'Part A', 'Partition Demo Shed'),
       ($1::uuid, $3::uuid, $4::uuid, 'Part B', 'Partition Demo Shed')
ON CONFLICT (tenant_id, goat_id) DO UPDATE
SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		repoTenant, weightDemoGoat, weightDemoGoatTwo, weightDemoPartitionShed)

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
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), "", "", "")
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}

	// The coverage counter is the plainest statement of the bug: it is the page's own
	// "kids in whole-shed weighs" figure, and it read 40 for twenty animals.
	if out.LumpSumAnimals != 20 {
		t.Fatalf("two pens of ten weighed twice each are 20 whole-shed kids, got %d", out.LumpSumAnimals)
	}

	// And the dimension charts, which is where a reader actually sees it. The weight charts must
	// now range over the SAME pen set the gain charts already did -- that agreement is the point.
	weightAnimals := lookupDemographicAnimals(t, out.ByBreed, "Partition Breed")
	gainAnimals := lookupGainAnimals(t, out.GainByBreed, "Partition Breed")
	if weightAnimals != gainAnimals {
		t.Fatalf("weight and gain charts must count the same pens: by_breed=%d, gain_by_breed=%d", weightAnimals, gainAnimals)
	}
	if weightAnimals != 20 {
		t.Fatalf("by_breed must count each pen once: want 20, got %d", weightAnimals)
	}
}

// THE HEADLINE AND THE GAIN CHART ARE ONE NUMBER (maintainer decision 2026-08-26).
//
// The defect this pins: the Weights page reported the farm's daily gain twice, from two different
// calculations, and under the Sex filter they became statements about the identical population and
// disagreed out loud -- 133 g/day in the headline above 200 g/day in the by-sex chart. The headline
// was the MEDIAN of individually-scanned PAIRS; the chart was the animal-weighted MEAN of scanned
// ANIMALS plus whole-shed pens. Three separate mismatches (statistic, grain, and whether whole-shed
// pens counted at all), each individually defensible, adding up to a page with no true number on it.
//
// Filtering to ONE sex is what makes the assertion exact: every animal in scope is then that sex, so
// gain_by_sex holds exactly one bucket and it must be the headline, animal for animal. That is also
// precisely the screen state the reader was looking at when they reported it.
func TestGrowthHeadlineEqualsTheGainChartForTheSameSex(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartB, "2026-07-17")
	for _, proofID := range []string{repoShedProofTwo, repoShedProofThree, repoShedProofFour} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'Partition Demo Shed', $3::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`,
		weightDemoPartitionShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990915', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid),
       ($3::uuid, $2::uuid, 'G-990916', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE SET sex = EXCLUDED.sex, shed_id = EXCLUDED.shed_id`,
		weightDemoGoat, repoTenant, weightDemoGoatTwo, repoParty, weightDemoPartitionShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'Part A', 'Partition Demo Shed'),
       ($1::uuid, $3::uuid, $4::uuid, 'Part B', 'Partition Demo Shed')
ON CONFLICT (tenant_id, goat_id) DO UPDATE
SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		repoTenant, weightDemoGoat, weightDemoGoatTwo, weightDemoPartitionShed)

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

	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	demo, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "")
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}
	growth, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}

	if len(demo.GainBySex) != 1 {
		t.Fatalf("a sex-filtered page must produce exactly one gain bucket, got %#v", demo.GainBySex)
	}
	chart := demo.GainBySex[0]
	if growth.Headline.AverageADGGPerDay == nil {
		t.Fatalf("the headline must report a gain when whole-shed pens moved: %#v", growth.Headline)
	}
	// Whole-shed pens are the ENTIRE population here, so a headline that still counted scanned pairs
	// only would be nil and this line alone would catch the regression that started all of this.
	if got, want := *growth.Headline.AverageADGGPerDay, chart.MedianGainGPerDay; math.Abs(got-want) > 0.5 {
		t.Fatalf("headline and gain chart must be the same number: headline=%.2f g/day, chart=%.2f g/day", got, want)
	}
	if growth.Headline.HeadlineAnimals != chart.Animals {
		t.Fatalf("headline and gain chart must speak for the same kids: headline=%d, chart=%d",
			growth.Headline.HeadlineAnimals, chart.Animals)
	}
}

// TestWeeklyGainEqualsTheHeadlineWhenAllMovementIsOneWeek pins the Weights analytics page's
// Time-wise tab to the SAME statistic every other figure on that page reports.
//
// This is the weekly half of the 2026-08-26 one-number decision. `trend` beside `weekly_gain` on
// the same response is the MEDIAN over SCANNED PAIRS ONLY; the headline is the animal-weighted
// mean over those pairs PLUS whole-shed pens. Most of this farm's kids are weighed by the whole
// shed, so a weekly chart built on `trend` would sit under a headline computed from a different
// population and quietly disagree with it -- which is exactly the defect (133 g/day above
// 200 g/day) that decision was written to stop, one axis over.
//
// The fixture seeds NO scanned observations, so the two pens ARE the whole population, and both
// of their pairs (10 Jul -> 17 Jul 2026, both Fridays) are bucketed by their LATER weigh into the
// single ISO week beginning Monday 13 Jul. One week means the week's own average and denominator
// must equal the headline's exactly, animal for animal -- there is no other week for a difference
// to hide in.
//
// MUTATION TEST when this was written: pointing the assertion at growth.Trend (the pair median)
// makes it fail with an empty series, because no kid here was ever scanned. Dropping the
// whole-shed arm from growthWeeklyGain does the same.
func TestWeeklyGainEqualsTheHeadlineWhenAllMovementIsOneWeek(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartB, "2026-07-17")
	for _, proofID := range []string{repoShedProofTwo, repoShedProofThree, repoShedProofFour} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'Partition Demo Shed', $3::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`,
		weightDemoPartitionShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990915', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid),
       ($3::uuid, $2::uuid, 'G-990916', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE SET sex = EXCLUDED.sex, shed_id = EXCLUDED.shed_id`,
		weightDemoGoat, repoTenant, weightDemoGoatTwo, repoParty, weightDemoPartitionShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'Part A', 'Partition Demo Shed'),
       ($1::uuid, $3::uuid, $4::uuid, 'Part B', 'Partition Demo Shed')
ON CONFLICT (tenant_id, goat_id) DO UPDATE
SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		repoTenant, weightDemoGoat, weightDemoGoatTwo, weightDemoPartitionShed)

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

	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	growth, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if growth.Headline.AverageADGGPerDay == nil {
		t.Fatalf("the headline must report a gain when whole-shed pens moved: %#v", growth.Headline)
	}
	if len(growth.WeeklyGain) != 1 {
		t.Fatalf("both pen pairs land in the week of 13 Jul, so exactly one weekly point is expected, got %#v", growth.WeeklyGain)
	}
	week := growth.WeeklyGain[0]
	// The bucket is the LATER weigh's week, not the earlier one: the movement was observed on
	// 17 Jul. Bucketing on the first weigh would file it under 6 Jul and misdate every bar.
	if week.WeekStart != "2026-07-13" {
		t.Fatalf("a pair is bucketed by its later weigh, so the week must begin Monday 13 Jul: got %q", week.WeekStart)
	}
	if got, want := week.AverageADGGPerDay, *growth.Headline.AverageADGGPerDay; math.Abs(got-want) > 0.5 {
		t.Fatalf("the only week and the headline must be the same number: week=%.2f g/day, headline=%.2f g/day", got, want)
	}
	if week.Animals != growth.Headline.HeadlineAnimals {
		t.Fatalf("the only week and the headline must speak for the same kids: week=%d, headline=%d",
			week.Animals, growth.Headline.HeadlineAnimals)
	}
	// A whole-shed pen carries no tag, so the SCANNED-pair trend beside it is empty here. This is
	// the line that would go red if the Time-wise tab were ever repointed at `trend`.
	if len(growth.Trend) != 0 {
		t.Fatalf("no kid was scanned in this fixture, so the pair trend must be empty: %#v", growth.Trend)
	}
}

func lookupDemographicAnimals(t *testing.T, buckets []domain.WeightDemographicBucket, label string) int {
	t.Helper()
	for _, bucket := range buckets {
		if bucket.Label == label {
			return bucket.Animals
		}
	}
	t.Fatalf("missing %q bucket in %#v", label, buckets)
	return 0
}

func lookupGainAnimals(t *testing.T, buckets []domain.WeightGainBucket, label string) int {
	t.Helper()
	for _, bucket := range buckets {
		if bucket.Label == label {
			return bucket.Animals
		}
	}
	t.Fatalf("missing %q gain bucket in %#v", label, buckets)
	return 0
}

// THE DAILY-GAIN AGGREGATE UNDER ADVERSARIAL SHAPES: fan-out, page boundary, park scope, status.
//
// The headline gain and the gain charts are animal-weighted aggregates, and every defect this file
// has recorded came from one of four places -- a row counted once per weigh instead of once per pen,
// a summary recomputed from a visible page slice, one park's pens leaking into another's number, and
// a rejected weigh treated as a measurement. This test drives all four at once on one fixture, so a
// change that gets three of them right still goes red on the fourth.
func TestGrowthGainAggregateOneToManyPageBoundaryParkScopeStatusBuckets(t *testing.T) {
	t.Log("OneToMany PageBoundary ParkScope StatusBuckets: growth rejected counts cover individual and whole-shed observations without widening the page scope")
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartB, "2026-07-17")
	for _, proofID := range []string{repoShedProofTwo, repoShedProofThree, repoShedProofFour} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'Partition Demo Shed', $3::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`,
		weightDemoPartitionShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990917', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid),
       ($3::uuid, $2::uuid, 'G-990918', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE SET sex = EXCLUDED.sex, shed_id = EXCLUDED.shed_id`,
		weightDemoGoat, repoTenant, weightDemoGoatTwo, repoParty, weightDemoPartitionShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'Part A', 'Partition Demo Shed'),
       ($1::uuid, $3::uuid, $4::uuid, 'Part B', 'Partition Demo Shed')
ON CONFLICT (tenant_id, goat_id) DO UPDATE
SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		repoTenant, weightDemoGoat, weightDemoGoatTwo, weightDemoPartitionShed)

	seedLoadBucketPartition(t, ctx, pool, loadPartAOld, loadCampaignPartA, weightDemoPartitionShed, "Part A", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, loadPartANew, loadCampaignPartB, weightDemoPartitionShed, "Part A", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, loadPartBOld, loadCampaignPartA, weightDemoPartitionShed, "Part B", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, loadPartBNew, loadCampaignPartB, weightDemoPartitionShed, "Part B", "per_shed_partition")

	// ONE-TO-MANY: each pen carries TWO live whole-shed observations inside the window. The pen must
	// contribute its ten kids ONCE, not once per weigh -- the 2026-08-26 defect, where four Castro 1
	// observations turned 63 kids into 252 and the page reported more kids than it had weighed.
	seedLoadLumpWeigh(t, ctx, pool, loadPartAOld, loadCampaignPartA, repoShedProof, 20.0, 10,
		time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, loadPartANew, loadCampaignPartB, repoShedProofTwo, 27.0, 10,
		time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, loadPartBOld, loadCampaignPartA, repoShedProofThree, 30.0, 10,
		time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, loadPartBNew, loadCampaignPartB, repoShedProofFour, 31.0, 10,
		time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC))

	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	base, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if base.Headline.HeadlineAnimals != 20 {
		t.Fatalf("two pens of ten weighed twice each are 20 kids, not %d -- the pen is counted per weigh again",
			base.Headline.HeadlineAnimals)
	}

	// STATUS BUCKETS: a WITHDRAWN whole-shed weigh is not a measurement and must leave the aggregate
	// exactly as it was. `withdrawn_at IS NULL` is the live rule here -- migration 000058 narrowed
	// verification_status to pending/verified/rework, so the `<> 'rejected'` predicates these queries
	// still carry can no longer exclude anything, and withdrawal is what actually retires a weigh. It
	// is also the reason the uniqueness on this table is PARTIAL: a reopened bucket legitimately holds
	// several rows, only one of them live, and an aggregate that joined them all would fan the pen out
	// past its own grain. Pending deliberately still counts -- an unverified weight is a real one.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_observations (
  tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count,
  proof_artifact_id, recorded_by, idempotency_key, accepted_at, verification_status, withdrawn_at
) VALUES ($1::uuid, $2::uuid, $3::uuid, 499950, 999.9, 500,
  $4::uuid, $5::uuid, 'gain-aggregate-withdrawn', $6::timestamptz, 'rework', $6::timestamptz)`,
		repoTenant, loadCampaignPartB, loadPartANew, repoShedProofTwo, repoOperator,
		time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC))
	withWithdrawn, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG after a withdrawn weigh: %v", err)
	}
	if withWithdrawn.Headline.HeadlineAnimals != base.Headline.HeadlineAnimals {
		t.Fatalf("a withdrawn weigh must not enter the gain population: %d became %d",
			base.Headline.HeadlineAnimals, withWithdrawn.Headline.HeadlineAnimals)
	}
	if got, want := *withWithdrawn.Headline.AverageADGGPerDay, *base.Headline.AverageADGGPerDay; math.Abs(got-want) > 0.01 {
		t.Fatalf("a withdrawn weigh must not move the gain: %.2f became %.2f", want, got)
	}
	if withWithdrawn.Headline.RejectedObservationCount != 1 {
		t.Fatalf("a verifier-bounced whole-shed weigh must be counted in rejected observations, got %d",
			withWithdrawn.Headline.RejectedObservationCount)
	}

	// PARK SCOPE: these pens hang off repoPark. Asking about a park that owns none of them must
	// return nothing rather than the tenant's rows -- the scope predicate carrying, not the caller.
	otherPark := "00000000-0000-4000-8000-0000000030ff"
	scoped, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{otherPark}, from, to, "female", "", "")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG for another park: %v", err)
	}
	if scoped.Headline.HeadlineAnimals != 0 || scoped.Headline.AverageADGGPerDay != nil {
		t.Fatalf("another park's gain must be empty, got %d kids / %v",
			scoped.Headline.HeadlineAnimals, scoped.Headline.AverageADGGPerDay)
	}

	// PAGE BOUNDARY: the headline is a WHOLE-FILTER aggregate. The shed table paginates; this number
	// must not. Asking for a single-row page of the table must leave the gain untouched -- recomputing
	// a summary from the visible slice is the capped read-time rollup this repo bans outright.
	table, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "female", "", "")
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	var overAllRows int
	for _, row := range table.Rows {
		overAllRows += row.AnimalsWeighed
	}
	if table.Summary.AnimalsWeighed != overAllRows {
		t.Fatalf("the summary must be the whole-filter total: summary=%d, rows=%d",
			table.Summary.AnimalsWeighed, overAllRows)
	}
	// The admin-web table slices these rows for display. A summary derived from the visible slice
	// instead of the whole filter is the capped read-time rollup this repo bans outright, so the
	// first page must NOT reproduce the total whenever there is more than one row to show.
	if len(table.Rows) > 1 {
		if firstPage := table.Rows[0].AnimalsWeighed; firstPage == table.Summary.AnimalsWeighed {
			t.Fatalf("summary %d equals the first row alone -- it is being derived from a page, not the filter",
				table.Summary.AnimalsWeighed)
		}
	}
	if base.Headline.HeadlineAnimals > table.Summary.AnimalsWeighed {
		t.Fatalf("the gain cannot speak for more kids than were weighed: gain=%d, weighed=%d",
			base.Headline.HeadlineAnimals, table.Summary.AnimalsWeighed)
	}
}

// THE ORIGIN FILTER SPLITS THIS AGGREGATE WITHOUT CHANGING ITS GRAIN.
//
// The adversarial shape, and why each dimension is here: ONE shed of ONE breed holding TWO
// partitions (cardinality — the one-to-many the aggregate must not fan out on), each weighed on
// TWO dates across TWO campaign weeks (page boundary and date shift), scoped to one park (park
// scope), with only accepted weighs counted (status buckets). It is the same fixture the
// unfiltered sibling test above asserts on, so the two are directly comparable.
//
// Part A is tagged to a purchase load through its ALIAS location; Part B is not. So the filter has
// to divide two partitions of the SAME shed, the same breed and the same window between the two
// cohorts — the case where a rule keyed on the shed rather than the pen puts both halves on one
// side, and a rule that fans out reports each partition's kids twice.
//
// The arithmetic is chosen so a blend cannot masquerade as a split:
//
//	Part A  20.0 -> 27.0 kg over 7 days = 1000.0 g/day, 10 kids   (purchased)
//	Part B  30.0 -> 31.0 kg over 7 days =  142.9 g/day, 10 kids   (farm born)
//	blended (the unfiltered page)        =  571.4 g/day, 20 kids
//
// A filter that leaked the other partition in would report 571.4 and 20 kids on both sides.
func TestWeightDemographicsOriginOneToManyPageBoundaryParkScopeStatusBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartB, "2026-07-17")
	for _, proofID := range []string{repoShedProofTwo, repoShedProofThree, repoShedProofFour} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	repo := NewRepository(pool, 5*time.Second)

	const (
		partALoad = "00000000-0000-4000-8000-0000000094a1"
		partBGoat = "00000000-0000-4000-8000-0000000094a2"
	)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'Partition Demo Shed', $3::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`,
		weightDemoPartitionShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-940001', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET breed=EXCLUDED.breed, sex=EXCLUDED.sex, management_stage=EXCLUDED.management_stage,
    current_location_id=EXCLUDED.current_location_id, park_id=EXCLUDED.park_id, shed_id=EXCLUDED.shed_id`,
		weightDemoGoat, repoTenant, repoParty, weightDemoPartitionShed, repoPark)
	// One resident per PEN, so each partition resolves to its own single-breed cohort. A lump-sum
	// weigh has no tags and is attributed through the pen's residents; without a row here the pen
	// resolves to no breed and drops out of the breed aggregate entirely.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-940002', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id`,
		partBGoat, repoTenant, repoParty, weightDemoPartitionShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'Part A', 'Partition Demo Shed - Part A'),
       ($1::uuid, $3::uuid, $4::uuid, 'Part B', 'Partition Demo Shed - Part B')
ON CONFLICT (tenant_id, goat_id) DO UPDATE
SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		repoTenant, weightDemoGoat, partBGoat, weightDemoPartitionShed)

	// Part A's resident came off a purchase load; Part B's did not. Origin is resolved PER ANIMAL
	// (maintainer correction 2026-09-01), and a whole-shed pen is claimed only when every live
	// resident agrees — so this is what makes Part A a purchased pen and Part B a farm-born one.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'weightdemo:load:940')
ON CONFLICT (load_id) DO NOTHING`, partALoad, repoTenant, repoParty)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_load_goats (load_goat_id, tenant_id, load_id, goat_id)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid)
ON CONFLICT DO NOTHING`, repoTenant, partALoad, weightDemoGoat)

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

	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	demoFor := func(t *testing.T, origin string) domain.WeightDemographics {
		t.Helper()
		out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", origin, "")
		if err != nil {
			t.Fatalf("GetWeightDemographics(origin=%q): %v", origin, err)
		}
		return out
	}
	gainFor := func(t *testing.T, origin string) (float64, int) {
		t.Helper()
		out := demoFor(t, origin)
		for _, bucket := range out.GainByBreed {
			if bucket.Label == "Partition Breed" {
				return bucket.MedianGainGPerDay, bucket.Animals
			}
		}
		t.Fatalf("missing Partition Breed gain bucket for origin=%q in %#v", origin, out.GainByBreed)
		return 0, 0
	}

	// Unfiltered: both partitions, blended, each kid counted ONCE.
	if gain, animals := gainFor(t, ""); animals != 20 || fmt.Sprintf("%.1f", gain) != "571.4" {
		t.Fatalf("unfiltered must blend both partitions over 20 kids, got %.1f over %d", gain, animals)
	}
	if out := demoFor(t, ""); out.LumpSumAnimals != 20 || out.LumpSumUnattributedAnimals != 0 {
		t.Fatalf("unfiltered counters must describe both whole-shed pens, got lump=%d unattributed=%d",
			out.LumpSumAnimals, out.LumpSumUnattributedAnimals)
	}

	// Purchased: Part A alone. 20 kids here would mean the untagged partition leaked in; 571.4
	// would mean the filter selected nothing and the page fell back to the blend.
	if gain, animals := gainFor(t, OriginPurchased); animals != 10 || fmt.Sprintf("%.1f", gain) != "1000.0" {
		t.Fatalf("purchased must report Part A alone: want 1000.0 over 10, got %.1f over %d", gain, animals)
	}
	if out := demoFor(t, OriginPurchased); out.LumpSumAnimals != 10 || out.LumpSumUnattributedAnimals != 0 {
		t.Fatalf("purchased counters must exclude the farm-born pen, got lump=%d unattributed=%d",
			out.LumpSumAnimals, out.LumpSumUnattributedAnimals)
	}

	// Farm born: Part B alone, from the SAME shed, breed and window.
	if gain, animals := gainFor(t, OriginFarmBorn); animals != 10 || fmt.Sprintf("%.1f", gain) != "142.9" {
		t.Fatalf("farm born must report Part B alone: want 142.9 over 10, got %.1f over %d", gain, animals)
	}
	if out := demoFor(t, OriginFarmBorn); out.LumpSumAnimals != 10 || out.LumpSumUnattributedAnimals != 0 {
		t.Fatalf("farm-born counters must exclude the purchased pen, got lump=%d unattributed=%d",
			out.LumpSumAnimals, out.LumpSumUnattributedAnimals)
	}
}

// The Shed-wise card names the pens behind its two bars, and this pins the three things that
// membership list can get wrong. It is a MEMBERSHIP list, not an aggregate, so there is no count
// or ratio to prove -- what has to hold is WHICH pens appear.
//
// ONE-TO-MANY: this farm's register carries legacy ALIAS rows, one physical pen spelled as two
// active locations -- "Godel 2" carrying label "Part 1", and a separate location literally named
// "Godel 2 - Part 1". Both are weighed, both classify as elevated, and both compose to the SAME
// display. The SQL can only dedupe on (location_id, partition_label), so it returns the pen twice
// and the panel named one shed as two. Caught in Chrome on the real register before this test
// existed.
//
// PAGE BOUNDARY: the list is a WHOLE-FILTER answer, never a page of one. The shed table beside it
// pages, and a membership list built from the visible page would name only the pens that happened
// to be on screen. Here the ground pen is deliberately the only one of its class while five
// elevated pens crowd the other, so a page-shaped answer loses it.
//
// And a pen that did NOT contribute is absent for the same reason it is absent from the bars: a
// pen weighed ONCE has no gain, and an unclassified pen was never claimed by either side.
func TestShedTypeMembersPerBreedPerParkOneToManyAliasRowsPageBoundaryParkScopeAndContributionOnly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartB, "2026-07-17")
	for _, proofID := range []string{repoShedProofTwo, repoShedProofThree, repoShedProofFour} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	repo := NewRepository(pool, 5*time.Second)

	const (
		aliasShed     = "00000000-0000-4000-8000-0000000095a1"
		aliasGoat     = "00000000-0000-4000-8000-0000000095a2"
		godelGoat     = "00000000-0000-4000-8000-0000000095a3"
		castroGoat    = "00000000-0000-4000-8000-0000000095a4"
		onceGoat      = "00000000-0000-4000-8000-0000000095a5"
		plainGoat     = "00000000-0000-4000-8000-0000000095a6"
		bucketGodelA  = "00000000-0000-4000-8000-0000000095b1"
		bucketGodelB  = "00000000-0000-4000-8000-0000000095b2"
		bucketAliasA  = "00000000-0000-4000-8000-0000000095b3"
		bucketAliasB  = "00000000-0000-4000-8000-0000000095b4"
		bucketCastroA = "00000000-0000-4000-8000-0000000095b5"
		bucketCastroB = "00000000-0000-4000-8000-0000000095b6"
		bucketOnce    = "00000000-0000-4000-8000-0000000095b7"
		bucketPlainA  = "00000000-0000-4000-8000-0000000095b8"
		bucketPlainB  = "00000000-0000-4000-8000-0000000095b9"
		otherShed     = "00000000-0000-4000-8000-0000000095ba"
		otherGoat     = "00000000-0000-4000-8000-0000000095bb"
		bucketOtherA  = "00000000-0000-4000-8000-0000000095bc"
		bucketOtherB  = "00000000-0000-4000-8000-0000000095bd"
		secondPark    = "00000000-0000-4000-8000-0000000095c0"
		twinShed      = "00000000-0000-4000-8000-0000000095c2"
		twinGoat      = "00000000-0000-4000-8000-0000000095c3"
		bucketTwinA   = "00000000-0000-4000-8000-0000000095c4"
		bucketTwinB   = "00000000-0000-4000-8000-0000000095c5"
		twinCampaignA = "00000000-0000-4000-8000-0000000095c6"
		twinCampaignB = "00000000-0000-4000-8000-0000000095c7"
	)

	// A SECOND PARK with its own "Castro 1". The farm really has one in each park, and Gandhi and
	// Yashoda repeat the same way, so a list keyed on the shed NAME merges two real pens into one
	// line -- the OL-1 name-keying defect one grain down. Both must be named, under their parks.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'Shed Type Second Park', 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`, secondPark, repoTenant)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $3::uuid, $4::uuid, '2026-07-10', '2026-07-16', '2026-07-10', 'published', 100, $5::uuid, $5::uuid),
       ($2::uuid, $3::uuid, $4::uuid, '2026-07-17', '2026-07-23', '2026-07-17', 'published', 100, $5::uuid, $5::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, twinCampaignA, twinCampaignB, repoTenant, secondPark, repoOperator)

	// "Godel 2" holds Part 1; `aliasShed` is the SAME pen spelled as its own location row. Castro
	// and Castro 2 are ground; "Plain 1" carries no classification at all.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES
  ($1::uuid, $6::uuid, 'shed', 'Godel 2', $7::uuid, 'active'),
  ($2::uuid, $6::uuid, 'shed', 'Godel 2 - Part 1', $7::uuid, 'active'),
  ($3::uuid, $6::uuid, 'shed', 'Castro 1', $7::uuid, 'active'),
  ($4::uuid, $6::uuid, 'shed', 'Castro 2', $7::uuid, 'active'),
  ($5::uuid, $6::uuid, 'shed', 'Plain 1', $7::uuid, 'active'),
  ($8::uuid, $6::uuid, 'shed', 'Godel 9', $7::uuid, 'active'),
  ($9::uuid, $6::uuid, 'shed', 'Castro 1', $10::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO UPDATE SET name=EXCLUDED.name, parent_location_id=EXCLUDED.parent_location_id`,
		weightDemoGodelShed, aliasShed, weightDemoCastroOne, weightDemoCastroTwo, weightDemoPlainOneShed,
		repoTenant, repoPark, otherShed, twinShed, secondPark)

	// One single-breed resident per pen: a lump-sum weigh has no tags, so a pen with no resident
	// resolves to no breed and never reaches the bars -- or this list.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES
  ($1::uuid, $7::uuid, 'WG-ST-GODEL', 'Shed Type Breed', 'male', 'kid', 'alive', 'kid', $8::uuid, $2::uuid, $9::uuid, $2::uuid),
  ($3::uuid, $7::uuid, 'WG-ST-ALIAS', 'Shed Type Breed', 'male', 'kid', 'alive', 'kid', $8::uuid, $4::uuid, $9::uuid, $4::uuid),
  ($5::uuid, $7::uuid, 'WG-ST-CASTRO', 'Shed Type Breed', 'male', 'kid', 'alive', 'kid', $8::uuid, $6::uuid, $9::uuid, $6::uuid),
  ($10::uuid, $7::uuid, 'WG-ST-ONCE', 'Shed Type Breed', 'male', 'kid', 'alive', 'kid', $8::uuid, $11::uuid, $9::uuid, $11::uuid),
  ($12::uuid, $7::uuid, 'WG-ST-PLAIN', 'Shed Type Breed', 'male', 'kid', 'alive', 'kid', $8::uuid, $13::uuid, $9::uuid, $13::uuid),
  ($14::uuid, $7::uuid, 'WG-ST-OTHER', 'Other Shed Type Breed', 'male', 'kid', 'alive', 'kid', $8::uuid, $15::uuid, $9::uuid, $15::uuid),
  ($16::uuid, $7::uuid, 'WG-ST-TWIN', 'Shed Type Breed', 'male', 'kid', 'alive', 'kid', $8::uuid, $17::uuid, $18::uuid, $17::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET breed=EXCLUDED.breed, sex=EXCLUDED.sex, current_location_id=EXCLUDED.current_location_id,
    park_id=EXCLUDED.park_id, shed_id=EXCLUDED.shed_id`,
		godelGoat, weightDemoGodelShed, aliasGoat, aliasShed, castroGoat, weightDemoCastroOne,
		repoTenant, repoParty, repoPark, onceGoat, weightDemoCastroTwo, plainGoat, weightDemoPlainOneShed,
		otherGoat, otherShed, twinGoat, twinShed, secondPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Part 1', 'Godel 2 - Part 1')
ON CONFLICT (tenant_id, goat_id) DO UPDATE
SET shed_id=EXCLUDED.shed_id, partition_label=EXCLUDED.partition_label`,
		repoTenant, godelGoat, weightDemoGodelShed)

	// Weighed TWICE: the two spellings of one pen, and the ground pen.
	seedLoadBucketPartition(t, ctx, pool, bucketGodelA, loadCampaignPartA, weightDemoGodelShed, "Part 1", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, bucketGodelB, loadCampaignPartB, weightDemoGodelShed, "Part 1", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, bucketAliasA, loadCampaignPartA, aliasShed, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, bucketAliasB, loadCampaignPartB, aliasShed, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, bucketCastroA, loadCampaignPartA, weightDemoCastroOne, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, bucketCastroB, loadCampaignPartB, weightDemoCastroOne, "", "per_shed_partition")
	// Weighed ONCE, and classified: no gain, so it is in neither the bars nor the list.
	seedLoadBucketPartition(t, ctx, pool, bucketOnce, loadCampaignPartA, weightDemoCastroTwo, "", "per_shed_partition")
	// Another BREED in its own elevated pen, weighed twice. Elevated by class, and behind a
	// DIFFERENT bar -- so it must not be named under the first breed's elevated list.
	seedLoadBucketPartition(t, ctx, pool, bucketOtherA, loadCampaignPartA, otherShed, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, bucketOtherB, loadCampaignPartB, otherShed, "", "per_shed_partition")
	// The second park's own "Castro 1", same breed and same class as the first park's.
	seedLoadBucketPartition(t, ctx, pool, bucketTwinA, twinCampaignA, twinShed, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, bucketTwinB, twinCampaignB, twinShed, "", "per_shed_partition")
	// Weighed twice but UNCLASSIFIED: neither side may claim it.
	seedLoadBucketPartition(t, ctx, pool, bucketPlainA, loadCampaignPartA, weightDemoPlainOneShed, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, bucketPlainB, loadCampaignPartB, weightDemoPlainOneShed, "", "per_shed_partition")

	first := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)
	second := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	seedLoadLumpWeigh(t, ctx, pool, bucketGodelA, loadCampaignPartA, repoShedProof, 20.0, 4, first)
	seedLoadLumpWeigh(t, ctx, pool, bucketGodelB, loadCampaignPartB, repoShedProofTwo, 23.0, 4, second)
	seedLoadLumpWeigh(t, ctx, pool, bucketAliasA, loadCampaignPartA, repoShedProof, 21.0, 4, first)
	seedLoadLumpWeigh(t, ctx, pool, bucketAliasB, loadCampaignPartB, repoShedProofTwo, 24.0, 4, second)
	seedLoadLumpWeigh(t, ctx, pool, bucketCastroA, loadCampaignPartA, repoShedProofThree, 18.0, 4, first)
	seedLoadLumpWeigh(t, ctx, pool, bucketCastroB, loadCampaignPartB, repoShedProofFour, 19.0, 4, second)
	seedLoadLumpWeigh(t, ctx, pool, bucketOnce, loadCampaignPartA, repoShedProofThree, 17.0, 4, first)
	seedLoadLumpWeigh(t, ctx, pool, bucketOtherA, loadCampaignPartA, repoShedProof, 22.0, 4, first)
	seedLoadLumpWeigh(t, ctx, pool, bucketOtherB, loadCampaignPartB, repoShedProofTwo, 26.0, 4, second)
	seedLoadLumpWeigh(t, ctx, pool, bucketTwinA, twinCampaignA, repoShedProofThree, 15.0, 4, first)
	seedLoadLumpWeigh(t, ctx, pool, bucketTwinB, twinCampaignB, repoShedProofFour, 17.0, 4, second)
	seedLoadLumpWeigh(t, ctx, pool, bucketPlainA, loadCampaignPartA, repoShedProofThree, 16.0, 4, first)
	seedLoadLumpWeigh(t, ctx, pool, bucketPlainB, loadCampaignPartB, repoShedProofFour, 18.0, 4, second)

	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark, secondPark},
		time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), "", "", "")
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}

	// PER BAR: keyed by breed AND class, which is the grain one bar is drawn at.
	named := map[string][]string{}
	for _, member := range out.ShedTypeMembers {
		key := member.Label + "/" + member.ShedType
		named[key] = append(named[key], member.OperationalLocationDisplay)
	}
	for key, names := range named {
		seen := map[string]int{}
		for _, name := range names {
			seen[name]++
			if seen[name] > 1 {
				t.Fatalf("%s names %q twice — two alias rows for one pen must collapse to one line: %v", key, name, names)
			}
		}
	}
	if got := named["Shed Type Breed/elevated"]; len(got) != 1 || got[0] != "Godel 2 - Part 1" {
		t.Fatalf("this breed's elevated bar must list exactly its own pen, both of whose spellings were weighed twice: got %v", got)
	}
	// TWO PARKS, TWO PENS, ONE NAME. Both "Castro 1"s must be named, each under its own park --
	// deduping on the name alone reported two real pens as one.
	if got := named["Shed Type Breed/ground"]; len(got) != 2 || got[0] != "Castro 1" || got[1] != "Castro 1" {
		t.Fatalf("both parks' Castro 1 must be named, and NOT the pen weighed once: got %v", got)
	}
	parksOfGroundCastro := map[string]int{}
	for _, member := range out.ShedTypeMembers {
		if member.Label == "Shed Type Breed" && member.ShedType == "ground" {
			parksOfGroundCastro[member.ParkName]++
		}
	}
	if len(parksOfGroundCastro) != 2 {
		t.Fatalf("the two Castro 1 pens must carry DIFFERENT park names, got %#v", parksOfGroundCastro)
	}
	// The other breed's elevated pen is elevated, and belongs to the OTHER bar only. Listing it
	// under this breed is the defect the per-class grain had.
	if got := named["Other Shed Type Breed/elevated"]; len(got) != 1 || got[0] != "Godel 9" {
		t.Fatalf("the second breed's elevated bar must list its own pen: got %v", got)
	}
	for _, name := range named["Shed Type Breed/elevated"] {
		if name == "Godel 9" {
			t.Fatalf("a pen holding another breed must not be named under this breed's bar: %v", named["Shed Type Breed/elevated"])
		}
	}
	// PARK SCOPE: the list inherits the page's park filter, so another park's pens are not named
	// under this park's bars. Every pen above lives in repoPark, so a different park must return
	// an EMPTY list rather than the same one -- the failure a missing park predicate produces.
	otherPark := "00000000-0000-4000-8000-0000000095c1" // a park holding nothing at all
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'Shed Type Other Park', 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`, otherPark, repoTenant)
	elsewhere, err := repo.GetWeightDemographics(ctx, repoTenant, []string{otherPark},
		time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), "", "", "")
	if err != nil {
		t.Fatalf("GetWeightDemographics(other park): %v", err)
	}
	if len(elsewhere.ShedTypeMembers) != 0 {
		t.Fatalf("another park must name none of this park's pens, got %#v", elsewhere.ShedTypeMembers)
	}

	for _, member := range out.ShedTypeMembers {
		if member.OperationalLocationDisplay == "Plain 1" {
			t.Fatalf("an unclassified pen must be claimed by neither side, got %#v", member)
		}
		if member.OperationalLocationDisplay == "Castro 2" {
			t.Fatalf("a pen weighed once has no gain and must not be named beside the bars, got %#v", member)
		}
	}
}
