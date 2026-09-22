package postgres

import (
	"context"
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}

	// Both page modes must execute real SQL; text-only CASE guards missed unmatched parentheses.
	withoutGrids, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "composition,dimensions,origin,shed_type,weight_bands,gain_thresholds", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics without weekly grids: %v", err)
	}
	if len(withoutGrids.ShedComposition) != len(out.ShedComposition) {
		t.Fatal("weekly-grid switch changed the main composition")
	}
	if len(withoutGrids.GainByBreedWeek)+len(withoutGrids.GainByPenWeek)+len(withoutGrids.GainByLoadWeek) != 0 {
		t.Fatal("disabled weekly grids returned data")
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

func TestWeightDemographicsSectionedDimensionsOnly(t *testing.T) {
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
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'chip-dim-section', 'chip-dim-section', 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id=EXCLUDED.goat_id, identifier_value=EXCLUDED.identifier_value, status='active'`,
		repoTenant, repoAnimal)
	seedShedWeightScan(t, ctx, pool, "chip-dim-section", 24.0, time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC))

	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark},
		time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC), "", "", "", "dimensions", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(dimensions): %v", err)
	}
	if len(out.ByBreed) == 0 || len(out.BySex) == 0 || len(out.ByStage) == 0 {
		t.Fatalf("sectioned dimensions must return weight dimensions, got breed=%#v sex=%#v stage=%#v", out.ByBreed, out.BySex, out.ByStage)
	}
	if len(out.ShedComposition) != 0 || len(out.GainByBreedOrigin) != 0 || len(out.GainByBreedShedType) != 0 || len(out.ByWeightBand) != 0 || len(out.GainByBreedWeek) != 0 || len(out.GainThresholdsByBreed) != 0 {
		t.Fatalf("dimensions-only read returned unrelated sections: %#v", out)
	}
}

func TestWeightDemographicsSectionQueriesBindEveryArgument(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	from := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

	for _, sections := range []string{"dimensions", "origin", "shed_type", "weight_bands", "weekly_gain"} {
		t.Run(sections, func(t *testing.T) {
			_, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark},
				from, to, "", "", "", sections, []float64{15, 20, 25, 30, 35}, domain.TimeScope{})
			if err != nil {
				t.Fatalf("GetWeightDemographics(%s) must execute its pruned SQL with all bound arguments: %v", sections, err)
			}
		})
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
  ($1::uuid, $4::uuid, 'G-870101', 'Anantapur Sheep', 'male', 'kid', 'alive', 'F2-Male', $5::uuid, $6::uuid, $8::uuid, $6::uuid),
  ($2::uuid, $4::uuid, 'G-870102', 'Beetal', 'female', 'kid', 'alive', 'F2-Female', $5::uuid, $6::uuid, $8::uuid, $6::uuid),
  ($3::uuid, $4::uuid, 'G-870103', 'Anantapur Sheep', 'male', 'kid', 'alive', 'F2-Male', $5::uuid, $7::uuid, $8::uuid, $7::uuid),
  ($9::uuid, $4::uuid, 'G-870104', 'Beetal', 'female', 'kid', 'alive', 'F2-Female', $5::uuid, $7::uuid, $8::uuid, $7::uuid),
  ($10::uuid, $4::uuid, 'G-870105', 'Sirohi', 'male', 'kid', 'alive', 'K3-Male', $5::uuid, $7::uuid, $8::uuid, $7::uuid),
  ($11::uuid, $4::uuid, 'G-870106', 'Malai', 'female', 'kid', 'alive', 'F2-Female', $5::uuid, $12::uuid, $8::uuid, $12::uuid),
  ($13::uuid, $4::uuid, 'G-870107', 'Sojat', 'male', 'kid', 'alive', 'F2-Male', $5::uuid, $12::uuid, $8::uuid, $12::uuid),
  ($14::uuid, $4::uuid, 'G-870108', 'Osmanabadi', 'female', 'kid', 'alive', 'K3-Female', $5::uuid, $12::uuid, $8::uuid, $12::uuid),
  ($15::uuid, $4::uuid, 'G-870109', 'Malai', 'male', 'kid', 'alive', 'F2-Male', $5::uuid, $16::uuid, $8::uuid, $16::uuid),
  ($17::uuid, $4::uuid, 'G-870110', 'Sojat', 'female', 'kid', 'alive', 'F2-Female', $5::uuid, $16::uuid, $8::uuid, $16::uuid),
  ($18::uuid, $4::uuid, 'G-870111', 'Osmanabadi', 'male', 'kid', 'alive', 'K3-Male', $5::uuid, $16::uuid, $8::uuid, $16::uuid),
  ($19::uuid, $4::uuid, 'G-870112', 'Plain Breed', 'female', 'kid', 'alive', 'Plain-Stage', $5::uuid, $20::uuid, $8::uuid, $20::uuid)
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
		time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC), "", "", "", "", nil, domain.TimeScope{})
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
VALUES ($1::uuid, $2::uuid, 'G-870113', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET breed=EXCLUDED.breed, sex=EXCLUDED.sex, management_stage=EXCLUDED.management_stage,
    current_location_id=EXCLUDED.current_location_id, park_id=EXCLUDED.park_id, shed_id=EXCLUDED.shed_id`,
		weightDemoGoat, repoTenant, repoParty, weightDemoPartitionShed, repoPark)

	// Each explicit partition must have its own resident cohort. A whole-shed
	// resident without a partition is not evidence for either Part A or Part B.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-870114', 'Partition Breed', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)`,
		weightDemoGoatTwo, repoTenant, repoParty, weightDemoPartitionShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'Part A', 'Partition Demo Shed'),
       ($1::uuid, $3::uuid, $4::uuid, 'Part B', 'Partition Demo Shed')`,
		repoTenant, weightDemoGoat, weightDemoGoatTwo, weightDemoPartitionShed)
	for _, proofID := range []string{repoShedProof, repoShedProofTwo, repoShedProofThree, repoShedProofFour} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", weightDemoPartitionShed, "shed", weightDemoPartitionShed)
	}

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
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), "", "", "", "", nil, domain.TimeScope{})
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
		time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC), "", "", "", "", nil, domain.TimeScope{})
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
		time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC), "", "", "", "", nil, domain.TimeScope{})
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
	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, windowFrom, windowTo, "", "", "", "", nil, domain.TimeScope{})
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
	otherPark, err := repo.GetWeightDemographics(ctx, repoTenant, []string{weightDemoGodelShed}, windowFrom, windowTo, "", "", "", "", nil, domain.TimeScope{})
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
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), "", "", "", "", nil, domain.TimeScope{})
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

	demo, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "", "", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}
	growth, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "", "", domain.TimeScope{})
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

	growth, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "", "", domain.TimeScope{})
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

	demo, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(weekly_gain section): %v", err)
	}
	if len(demo.GainByBreedWeek) != 1 {
		t.Fatalf("weekly_gain section must return the one whole-shed week, got %#v", demo.GainByBreedWeek)
	}
	if demo.GainByBreedWeek[0].WeekStart != "2026-07-13" {
		t.Fatalf("weekly_gain section bucket week = %q, want 2026-07-13", demo.GainByBreedWeek[0].WeekStart)
	}
	if len(demo.ByBreed) != 0 || len(demo.ByWeightBand) != 0 || len(demo.GainByBreedOrigin) != 0 || len(demo.GainByBreedShedType) != 0 || len(demo.ShedComposition) != 0 {
		t.Fatalf("weekly_gain-only read returned unrelated sections: %#v", demo)
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

	base, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "", "", domain.TimeScope{})
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
	withWithdrawn, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "", "", domain.TimeScope{})
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
	scoped, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{otherPark}, from, to, "female", "", "", "", domain.TimeScope{})
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
	table, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "female", "", "", 0, 0, 0)
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
		out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", origin, "", "", nil, domain.TimeScope{})
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

	sectioned, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "origin", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(origin section): %v", err)
	}
	var originsSeen int
	for _, bucket := range sectioned.GainByBreedOrigin {
		if bucket.Label == "Partition Breed" {
			originsSeen++
		}
	}
	if originsSeen != 2 {
		t.Fatalf("origin section must return purchased and farm-born buckets for Partition Breed, got %#v", sectioned.GainByBreedOrigin)
	}
	if len(sectioned.ByBreed) != 0 || len(sectioned.ByWeightBand) != 0 || len(sectioned.GainByBreedShedType) != 0 || len(sectioned.GainByBreedWeek) != 0 || len(sectioned.ShedComposition) != 0 {
		t.Fatalf("origin-only read returned unrelated sections: %#v", sectioned)
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
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), "", "", "", "", nil, domain.TimeScope{})
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
		if member.Label == "Shed Type Breed" && member.ShedType == "non_elevated" {
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
	sectioned, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark, secondPark},
		time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), "", "", "", "shed_type", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(shed_type section): %v", err)
	}
	if len(sectioned.GainByBreedShedType) == 0 || len(sectioned.ShedTypeMembers) == 0 {
		t.Fatalf("shed_type section must return bars and members, got bars=%#v members=%#v",
			sectioned.GainByBreedShedType, sectioned.ShedTypeMembers)
	}
	if len(sectioned.ByBreed) != 0 || len(sectioned.ByWeightBand) != 0 || len(sectioned.GainByBreedOrigin) != 0 || len(sectioned.GainByBreedWeek) != 0 || len(sectioned.ShedComposition) != 0 {
		t.Fatalf("shed_type-only read returned unrelated sections: %#v", sectioned)
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
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), "", "", "", "", nil, domain.TimeScope{})
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

// THE PEN x WEEK TABLE SPEAKS FOR THE SAME KIDS AS THE WEEKLY LINE ABOVE IT.
//
// The Time-wise tab's per-pen table (maintainer request 2026-09-08) is the weekly headline cut one
// pen at a time, so on a fixture where every movement is one week the pen rows must (a) land on
// the same Monday the weekly point lands on, (b) add up, animal for animal, to that point's
// denominator, and (c) weight back to the same gain. Each pen's own figure is pinned too: Part A
// moved 20 -> 27 kg over 7 days (1000 g/day, 10 head) and Part B 30 -> 31 kg (142.9 g/day, 10
// head). The label is asserted as the OUTPUT STRING -- "Partition Demo Shed - Part A" -- not as
// field presence, and the Sex filter is driven both ways: the pens hold females, so a female page
// keeps both rows and a male page keeps none.
// seedPenWeekFixture is the two-pen, one-week fixture the Time-wise per-pen and per-load tests
// share: Partition Demo Shed's Part A moves 20 -> 27 kg over 7 days (1000 g/day, 10 head) and
// Part B 30 -> 31 kg (142.9 g/day, 10 head), both whole-shed, both female.
func seedPenWeekFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartB, "2026-07-17")
	for _, proofID := range []string{repoShedProofTwo, repoShedProofThree, repoShedProofFour} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
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

}

func TestPenWeekGainRowsAddUpToTheWeeklyPoint(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedPenWeekFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	growth, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if len(growth.WeeklyGain) != 1 {
		t.Fatalf("expected exactly one weekly point, got %#v", growth.WeeklyGain)
	}
	week := growth.WeeklyGain[0]

	demo, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}
	light, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "dimensions", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(light): %v", err)
	}
	if len(light.GainByBreedWeek) != 0 || len(light.GainByPenWeek) != 0 || len(light.GainByLoadWeek) != 0 {
		t.Fatalf("light demographics read must not carry Time-wise week grids: breed=%#v pens=%#v loads=%#v", light.GainByBreedWeek, light.GainByPenWeek, light.GainByLoadWeek)
	}
	if len(demo.GainByPenWeek) != 2 {
		t.Fatalf("two pens moved in one week, so two pen-week rows are expected: %#v", demo.GainByPenWeek)
	}
	byPen := map[string]domain.WeightGainPenWeekBucket{}
	animals := 0
	weighted := 0.0
	for _, row := range demo.GainByPenWeek {
		if row.WeekStart != week.WeekStart {
			t.Fatalf("a pen row must land on the same Monday as the weekly point (%s): %#v", week.WeekStart, row)
		}
		if row.LocationID != weightDemoPartitionShed || row.ParkID != repoPark || row.ShedName != "Partition Demo Shed" {
			t.Fatalf("pen row must carry its location, park and shed name: %#v", row)
		}
		if row.ParkName == "" {
			t.Fatalf("pen row must carry a park name for the table's park column: %#v", row)
		}
		byPen[row.OperationalLocationDisplay] = row
		animals += row.Animals
		weighted += float64(row.Animals) * row.AverageGainGPerDay
	}
	partA, ok := byPen["Partition Demo Shed - Part A"]
	if !ok {
		t.Fatalf("the pen label must be the composed OUTPUT STRING, got keys %v", keysOf(byPen))
	}
	partB := byPen["Partition Demo Shed - Part B"]
	if partA.Animals != 10 || math.Abs(partA.AverageGainGPerDay-1000) > 0.01 {
		t.Fatalf("Part A moved 20 -> 27 kg over 7 days for 10 head: %#v", partA)
	}
	if partB.Animals != 10 || math.Abs(partB.AverageGainGPerDay-1000.0/7.0) > 0.01 {
		t.Fatalf("Part B moved 30 -> 31 kg over 7 days for 10 head: %#v", partB)
	}
	if animals != week.Animals {
		t.Fatalf("the pen rows must add up to the weekly point's denominator: pens=%d week=%d", animals, week.Animals)
	}
	if got, want := weighted/float64(animals), week.AverageADGGPerDay; math.Abs(got-want) > 0.5 {
		t.Fatalf("the pen rows must weight back to the weekly point: pens=%.2f week=%.2f", got, want)
	}

	// The Sex filter claims a whole-shed pen only through its live cohort, both ways.
	female, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "female", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(female): %v", err)
	}
	if len(female.GainByPenWeek) != 2 {
		t.Fatalf("both pens hold females, so the female page keeps both rows: %#v", female.GainByPenWeek)
	}
	male, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "male", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(male): %v", err)
	}
	if len(male.GainByPenWeek) != 0 {
		t.Fatalf("neither pen holds a male, so the male page must claim no pen row: %#v", male.GainByPenWeek)
	}
}

// The per-load table (maintainer request 2026-09-14, "Time-wise ADG for each shed/load") is the
// pen rows one grain up, attributed through weighing_shed_load_tags exactly as the Load-wise tab's
// by-load read attributes. On the shared fixture the one tagged shed's two pens must collapse to
// ONE load-week row on the same Monday, whose denominator is both pens' head counts and whose
// gain is their animal-weighted mean (10 x 1000 + 10 x 142.9) / 20 = 571.4 g/day. Driven three
// more ways: the owner name rides on the row as served; a male page claims no row because the
// pens hold females; and a shed tagged to TWO loads is claimed by neither, while its pen rows
// stay listed under their own names.
func TestLoadWeekGainRowsAreThePenRowsAttributedByLoadTag(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedPenWeekFixture(t, ctx, pool)
	seedLoadTag(t, ctx, pool, weightDemoPartitionShed, "L-42", "Demo Supplier")
	repo := NewRepository(pool, 5*time.Second)

	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	demo, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}
	if len(demo.GainByPenWeek) != 2 {
		t.Fatalf("the pen rows are unchanged by a load tag: %#v", demo.GainByPenWeek)
	}
	if len(demo.GainByLoadWeek) != 1 {
		t.Fatalf("one tagged shed with two pens moving in one week is ONE load-week row: %#v", demo.GainByLoadWeek)
	}
	row := demo.GainByLoadWeek[0]
	if row.LoadRef != "L-42" || row.OwnerName != "Demo Supplier" {
		t.Fatalf("the load row carries the tag's load ref and owner verbatim: %#v", row)
	}
	if row.WeekStart != demo.GainByPenWeek[0].WeekStart {
		t.Fatalf("the load row lands on the same Monday as its pen rows: %#v vs %#v", row, demo.GainByPenWeek[0])
	}
	if row.Animals != 20 {
		t.Fatalf("the load's denominator is both pens' head counts: %#v", row)
	}
	if want := (10*1000.0 + 10*1000.0/7.0) / 20; math.Abs(row.AverageGainGPerDay-want) > 0.01 {
		t.Fatalf("the load's gain is the animal-weighted mean of its pens (%.2f): %#v", want, row)
	}

	male, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "male", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(male): %v", err)
	}
	if len(male.GainByLoadWeek) != 0 {
		t.Fatalf("the pens hold no male, so the male page claims no load row: %#v", male.GainByLoadWeek)
	}

	// A second load on the same shed makes it a two-load shed: claimed by neither load. The tag
	// is seeded behind the repository's back, so its read cache is dropped the way a real write
	// through the repository drops it; otherwise the second read replays the one-load answer.
	seedLoadTag(t, ctx, pool, weightDemoPartitionShed, "L-43", "Other Supplier")
	repo.invalidateReadCache()
	twoLoads, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(two loads): %v", err)
	}
	if len(twoLoads.GainByLoadWeek) != 0 {
		t.Fatalf("a shed tagged to two loads is claimed by neither: %#v", twoLoads.GainByLoadWeek)
	}
	if len(twoLoads.GainByPenWeek) != 2 {
		t.Fatalf("its pen rows are still listed under their own names: %#v", twoLoads.GainByPenWeek)
	}
}

func keysOf(m map[string]domain.WeightGainPenWeekBucket) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The two week grids, driven adversarially the way every other demographics arm is:
//
//	one-to-many  a load tag on a shed with TWO measured pens folds to ONE load-week row (the
//	             pen rows stay two), and never multiplies them;
//	park scope   another park sees no pen row and no load row -- the grids are scoped through
//	             weighing_campaigns.park_id, never by shed name;
//	status       a WITHDRAWN whole-pen weigh drops out of both grids, so the pen it belonged to
//	             loses its pair and the load's denominator shrinks with it;
//	boundary     a period that ends before the later weigh has no pair inside it and both grids
//	             are EMPTY rather than a row built from a weigh outside the window.
func TestPenAndLoadWeekGainOneToManyPageBoundaryParkScopeStatusBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedPenWeekFixture(t, ctx, pool)
	seedLoadTag(t, ctx, pool, weightDemoPartitionShed, "L-42", "Demo Supplier")
	repo := NewRepository(pool, 5*time.Second)

	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	// One-to-many: two pens under one tag are one load row and still two pen rows.
	whole, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}
	if len(whole.GainByPenWeek) != 2 || len(whole.GainByLoadWeek) != 1 || whole.GainByLoadWeek[0].Animals != 20 {
		t.Fatalf("two tagged pens are two pen rows and ONE load row of 20 animals: pens=%#v loads=%#v", whole.GainByPenWeek, whole.GainByLoadWeek)
	}

	// Park scope: the same tenant, another park -- nothing.
	otherPark := "00000000-0000-4000-8000-0000000041ff"
	scoped, err := repo.GetWeightDemographics(ctx, repoTenant, []string{otherPark}, from, to, "", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(other park): %v", err)
	}
	if len(scoped.GainByPenWeek) != 0 || len(scoped.GainByLoadWeek) != 0 {
		t.Fatalf("another park must see neither grid: pens=%#v loads=%#v", scoped.GainByPenWeek, scoped.GainByLoadWeek)
	}

	// Window boundary: a period closed before the second weigh holds no pair.
	early, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC), "", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(early window): %v", err)
	}
	if len(early.GainByPenWeek) != 0 || len(early.GainByLoadWeek) != 0 {
		t.Fatalf("a window with one weigh per pen has no gain to grid: pens=%#v loads=%#v", early.GainByPenWeek, early.GainByLoadWeek)
	}

	// Status bucket: withdraw Part B's later weigh; Part B loses its pair and the load shrinks to
	// Part A's 10 head at 1000 g/day. Seeded behind the repository's back, so its read cache is
	// dropped as a repository write would drop it.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_shed_observations SET withdrawn_at = now()
WHERE tenant_id = $1::uuid AND campaign_shed_id = $2::uuid`, repoTenant, loadPartBNew)
	repo.invalidateReadCache()
	withdrawn, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(withdrawn): %v", err)
	}
	if len(withdrawn.GainByPenWeek) != 1 || withdrawn.GainByPenWeek[0].OperationalLocationDisplay != "Partition Demo Shed - Part A" {
		t.Fatalf("a withdrawn weigh leaves only Part A with a pair: %#v", withdrawn.GainByPenWeek)
	}
	if len(withdrawn.GainByLoadWeek) != 1 || withdrawn.GainByLoadWeek[0].Animals != 10 ||
		math.Abs(withdrawn.GainByLoadWeek[0].AverageGainGPerDay-1000) > 0.01 {
		t.Fatalf("the load row is now Part A alone, 10 head at 1000 g/day: %#v", withdrawn.GainByLoadWeek)
	}
}

// THE PEN'S OWN LATEST WEIGH, not each animal's (maintainer decision 2026-09-21).
//
// An animal weighed in one pen and then, later in the same window, in another used to file its
// composition chip under the SECOND pen only -- so the first pen carried weighs on its row and no
// cohort at all, and the Breed column beside it read as absent data on a pen that was
// demonstrably weighed. Both pens now report the animals of their OWN most recent weighing day,
// which is the population the shed-weights row beside each one counts.
//
// Reproduced on the real read: seeded against the pre-change query this fails with "missing
// composition for the first pen", which is exactly the blank cell the maintainer reported.
func TestShedCompositionOneToManyPageBoundaryParkScopeReportsEachPensOwnLatestWeigh(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		secondShed   = "00000000-0000-4000-8000-0000000091f1"
		secondBucket = "00000000-0000-4000-9000-0000000091f1"
	)

	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats SET breed='Sojat', sex='male'
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, repoTenant, repoAnimal)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990003', 'Beetal', 'male', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET breed=EXCLUDED.breed, sex=EXCLUDED.sex, current_location_id=EXCLUDED.current_location_id, shed_id=EXCLUDED.shed_id`,
		repoAnimalTwo, repoTenant, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES
  ($1::uuid, $2::uuid, 'animal_identifier_1', 'chip-moved-one', 'chip-moved-one', 'global', true, 'active', now(), 'test'),
  ($1::uuid, $3::uuid, 'animal_identifier_1', 'chip-moved-two', 'chip-moved-two', 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id=EXCLUDED.goat_id, identifier_value=EXCLUDED.identifier_value, status='active'`,
		repoTenant, repoAnimal, repoAnimalTwo)

	// A second pen, weighed the day after the first.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'Second Pen', $3::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`, secondShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (
  campaign_shed_id, campaign_id, tenant_id, location_id, location_type,
  display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Second Pen', 'individual_animal', $5::uuid, 0)
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		secondBucket, repoCampaign, repoTenant, secondShed, repoOperator)

	firstDay := time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC)
	secondDay := firstDay.AddDate(0, 0, 1)
	seedShedWeightScan(t, ctx, pool, "chip-moved-one", 24.0, firstDay)
	seedShedWeightScan(t, ctx, pool, "chip-moved-two", 25.0, firstDay)
	for _, tag := range []string{"chip-moved-one", "chip-moved-two"} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 26.0, $5::uuid, $6::uuid, $7, $8::timestamptz, $8::timestamptz)`,
			repoTenant, repoCampaign, secondBucket, tag, repoAnimalProof, repoOperator,
			"secondpen:"+tag, secondDay)
	}

	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark},
		time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC), "", "", "", "composition", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(composition): %v", err)
	}

	byLocation := map[string][]string{}
	totals := map[string]int{}
	for _, shed := range out.ShedComposition {
		for _, chip := range shed.Chips {
			byLocation[shed.LocationID] = append(byLocation[shed.LocationID], chip.Breed)
		}
		totals[shed.LocationID] = shed.TotalAnimals
	}

	first, ok := byLocation[repoExpectedShed]
	if !ok {
		t.Fatalf("missing composition for the first pen %s: a pen whose animals were weighed again elsewhere still has a cohort of its own, got %#v",
			repoExpectedShed, out.ShedComposition)
	}
	sort.Strings(first)
	if len(first) != 2 || first[0] != "Beetal" || first[1] != "Sojat" {
		t.Fatalf("first pen chips = %#v, want Beetal + Sojat from its own weighing day", first)
	}
	if totals[repoExpectedShed] != 2 {
		t.Fatalf("first pen total = %d, want the 2 animals it weighed", totals[repoExpectedShed])
	}
	second, ok := byLocation[secondShed]
	if !ok {
		t.Fatalf("missing composition for the second pen %s in %#v", secondShed, out.ShedComposition)
	}
	if len(second) != 2 || totals[secondShed] != 2 {
		t.Fatalf("second pen chips = %#v total = %d, want its own 2 animals", second, totals[secondShed])
	}
}

// ---------------------------------------------------------------------------------------------
// The Time-wise bucket and pen scope (maintainer request 2026-09-21). These four exercise the
// grain the bucket change moved: a pen now contributes ONE row per bucket over ALL its movement
// in it, and a selected pen narrows every arm of the read rather than being filtered afterwards.

const (
	penBucketCampaignThird = "00000000-0000-4000-8000-00000000a004"
	penBucketPartAThird    = "00000000-0000-4000-8000-00000000a113"
)

// A THIRD Part A weigh, three weeks after the second, so one 30-day bucket holds TWO legs for that
// pen and only one for Part B. Seeded on its own campaign because a bucket holds one live
// observation per campaign_shed.
func seedPenBucketThirdWeigh(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedShedWeightsCampaign(t, ctx, pool, penBucketCampaignThird, "2026-07-24")
	insertProof(t, ctx, pool, repoShedProofFive, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	seedLoadBucketPartition(t, ctx, pool, penBucketPartAThird, penBucketCampaignThird, weightDemoPartitionShed, "Part A", "per_shed_partition")
	// 27.0 -> 34.0 kg over the 7 days from 17 Jul to 24 Jul: 1000 g/day on its own.
	seedLoadLumpWeigh(t, ctx, pool, penBucketPartAThird, penBucketCampaignThird, repoShedProofFive, 34.0, 10,
		time.Date(2026, 7, 24, 6, 0, 0, 0, time.UTC))
}

// CARDINALITY. A pen weighed three times inside ONE bucket produces two legs, and counting both
// would put its head count in that bucket's denominator twice -- the defect this grain exists to
// prevent. It contributes ONE row, its head count ONCE, and its figure is the grams it gained
// across the WHOLE bucket over the days those legs cover: reporting only the last leg would put one
// week's gain under a heading that says thirty days.
func TestPenBucketGainCountsAPenOnceWhenOneToManyWeighsLandInOneBucket(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedPenWeekFixture(t, ctx, pool)
	seedPenBucketThirdWeigh(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	from := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	month := domain.TimeScope{Bucket: domain.GainBucketMonth}

	demo, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, month)
	if err != nil {
		t.Fatalf("GetWeightDemographics(month): %v", err)
	}
	byPen := map[string][]domain.WeightGainPenWeekBucket{}
	for _, row := range demo.GainByPenWeek {
		byPen[row.OperationalLocationDisplay] = append(byPen[row.OperationalLocationDisplay], row)
	}
	partA := byPen["Partition Demo Shed - Part A"]
	if len(partA) != 1 {
		t.Fatalf("Part A moved twice inside one 30-day block and must contribute ONE row, got %#v", partA)
	}
	if partA[0].Animals != 10 {
		t.Fatalf("the pen's head count must be counted once, not once per leg: %#v", partA[0])
	}
	// 20 -> 27 kg over 7 days, then 27 -> 34 kg over 7 days: 14,000 g over 14 days = 1000 g/day.
	// The last leg alone is also 1000, so the fixture makes them differ below via Part B.
	if math.Abs(partA[0].AverageGainGPerDay-1000) > 0.01 {
		t.Fatalf("Part A gained 14 kg across 14 days: %#v", partA[0])
	}
	partB := byPen["Partition Demo Shed - Part B"]
	if len(partB) != 1 || partB[0].Animals != 10 {
		t.Fatalf("Part B moved once and must contribute one row for ten head: %#v", partB)
	}
	if math.Abs(partB[0].AverageGainGPerDay-1000.0/7.0) > 0.01 {
		t.Fatalf("Part B moved 30 -> 31 kg over 7 days: %#v", partB[0])
	}

	// The headline series over the same window sees the same two pens ONCE each: 20 animals, not 30.
	growth, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "", month)
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG(month): %v", err)
	}
	if len(growth.WeeklyGain) != 1 {
		t.Fatalf("one 30-day block covers this window: %#v", growth.WeeklyGain)
	}
	if growth.WeeklyGain[0].Animals != 20 {
		t.Fatalf("two pens of ten, each counted once: %#v", growth.WeeklyGain[0])
	}
}

// PAGINATION. These series are NOT paginated and must not become so: the tab draws a column per
// bucket, so a page boundary inside the window would silently drop columns from a chart that reads
// as complete. Every bucket in the window is carried, and the pen rows sit on exactly the bucket
// starts the headline series carries.
func TestTimeWiseBucketsCarryTheWholeWindowWithNoPaginationBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedPenWeekFixture(t, ctx, pool)
	seedPenBucketThirdWeigh(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	week := domain.TimeScope{Bucket: domain.GainBucketWeek}

	growth, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "", week)
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	demo, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, week)
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}
	headlineWeeks := map[string]bool{}
	for _, point := range growth.WeeklyGain {
		headlineWeeks[point.WeekStart] = true
	}
	// 17 Jul and 24 Jul both fall in the window and both carry movement.
	if len(headlineWeeks) != 2 {
		t.Fatalf("both weeks of movement must be carried, got %v", headlineWeeks)
	}
	penWeeks := map[string]bool{}
	for _, row := range demo.GainByPenWeek {
		penWeeks[row.WeekStart] = true
		if !headlineWeeks[row.WeekStart] {
			t.Fatalf("a pen row landed on a bucket the headline does not carry: %#v", row)
		}
	}
	if len(penWeeks) != len(headlineWeeks) {
		t.Fatalf("the grid and the chart must span the same buckets: pens=%v headline=%v", penWeeks, headlineWeeks)
	}
}

// SCOPE. It reads UNSCOPED FIRST ON PURPOSE: these analytics reads memoise their answer per
// (tenant, parks, window, sex, origin, mode), and the first browser run of this feature showed the
// page narrowing nothing because the pen was missing from that key -- the unfiltered answer was
// already cached. Asking wide and then narrow, in that order, is the defect's own path.
//
// The selected pen narrows the WHOLE read inside the park scope -- its own growth, its breed
// row and the load it sits in -- because three of the tab's four sections are grouped aggregates
// that carry no pen to filter on afterwards. A pen is (location, partition): the partition half is
// load-bearing, and dropping it would answer for the whole shed.
func TestPenScopeNarrowsEveryArmInsideTheSameParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedPenWeekFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	everyPen := domain.TimeScope{}
	partA := domain.TimeScope{PenLocationID: weightDemoPartitionShed, PenPartitionLabel: "Part A"}

	wide, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, everyPen)
	if err != nil {
		t.Fatalf("GetWeightDemographics(every pen): %v", err)
	}
	if len(wide.GainByPenWeek) != 2 {
		t.Fatalf("the unscoped read must still carry both pens: %#v", wide.GainByPenWeek)
	}

	scoped, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, partA)
	if err != nil {
		t.Fatalf("GetWeightDemographics(Part A): %v", err)
	}
	if len(scoped.GainByPenWeek) != 1 || scoped.GainByPenWeek[0].PartitionLabel != "Part A" {
		t.Fatalf("the pen grid must hold Part A alone: %#v", scoped.GainByPenWeek)
	}
	if len(scoped.GainByLoadWeek) > len(wide.GainByLoadWeek) {
		t.Fatalf("a pen cannot widen the load rows: scoped=%#v wide=%#v", scoped.GainByLoadWeek, wide.GainByLoadWeek)
	}

	growth, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "", partA)
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG(Part A): %v", err)
	}
	if len(growth.WeeklyGain) != 1 || growth.WeeklyGain[0].Animals != 10 {
		t.Fatalf("the headline series must answer for Part A's ten head alone: %#v", growth.WeeklyGain)
	}
	if math.Abs(growth.WeeklyGain[0].AverageADGGPerDay-1000) > 0.01 {
		t.Fatalf("Part A moved 20 -> 27 kg over 7 days: %#v", growth.WeeklyGain[0])
	}

	// The park scope still binds: a pen id from this park read under another park's scope returns
	// nothing rather than reaching across the scope.
	const penScopeOtherPark = "00000000-0000-4000-8000-0000000030fd"
	otherPark, err := repo.GetWeightDemographics(ctx, repoTenant, []string{penScopeOtherPark}, from, to, "", "", "", "weekly_gain", nil, partA)
	if err != nil {
		t.Fatalf("GetWeightDemographics(other park): %v", err)
	}
	if len(otherPark.GainByPenWeek) != 0 {
		t.Fatalf("a pen scope must not reach outside the park scope: %#v", otherPark.GainByPenWeek)
	}
}

// STATUS. A withdrawn or rejected pen weigh is not evidence, so it must leave every bucket arm --
// and taking it away must move the figure, not merely the row count, because the pen's remaining
// legs then span different days.
func TestBucketArmsDropWithdrawnAndRejectedStatusBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedPenWeekFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	week := domain.TimeScope{Bucket: domain.GainBucketWeek}

	before, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, week)
	if err != nil {
		t.Fatalf("GetWeightDemographics(before): %v", err)
	}
	if len(before.GainByPenWeek) != 2 {
		t.Fatalf("both pens move in this week: %#v", before.GainByPenWeek)
	}

	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_shed_observations SET verification_status = 'rejected'
WHERE tenant_id = $1::uuid AND campaign_shed_id = $2::uuid`, repoTenant, loadPartANew)

	after, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "weekly_gain", nil, week)
	if err != nil {
		t.Fatalf("GetWeightDemographics(after): %v", err)
	}
	for _, row := range after.GainByPenWeek {
		if row.PartitionLabel == "Part A" {
			t.Fatalf("a rejected weigh must leave the bucket arms entirely: %#v", row)
		}
	}
	if len(after.GainByPenWeek) != 1 {
		t.Fatalf("Part B alone remains: %#v", after.GainByPenWeek)
	}
}

// THE TWO GRAINS RACE ON DATE (maintainer report 2026-09-22, live STG).
//
// A pen weighed animal by animal for weeks and then weighed WHOLE keeps both histories inside one
// window. The row on screen reports the pen's LATEST weigh, so the cohort beside it must come from
// that same weigh. Reported on CBE Godel 2 - Part 1: ten resident Anantapur Sheep weighed as one
// total on 22/09, labelled "Beetal" from a single scanned kid on 08/09 -- a breed the pen does not
// hold, next to a count it does not match (1 against the row's 10).
//
// Both directions are asserted here on purpose: whichever grain was weighed LAST wins, so this
// cannot be satisfied by simply preferring the whole-pen side.
//
// ONE-TO-MANY: a pen holds MANY weighs of each kind -- an animal scanned on several days, a pen
// weighed whole more than once -- and the cohort is a count of ANIMALS, so repeats must not fan it
// out. PAGE BOUNDARY and PARK SCOPE: the pens table pages client-side over a whole-result read, so
// the cohort a pen carries must be identical whatever page of shed rows the caller asked for and
// whether the caller selected one park or left the park filter open. STATUS MATRIX: only a weigh
// that still counts can win the race -- a REJECTED scan is not a weigh however recent it is, and
// withdrawing the whole-pen weighs hands the pen back to the scans.
func TestPenCohortOneToManyPageBoundaryParkScopeAndStatusMatrixFollowTheGrainWeighedLast(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)

	const (
		individualBucketOnLumpPen = "00000000-0000-4000-9000-0000000092f1"
		// A SECOND campaign: one campaign may hold only one bucket per (location, partition)
		// (weighing_campaign_sheds_campaign_location_partition_uidx), and a pen weighed both ways
		// really is two pieces of work.
		raceCampaign = "00000000-0000-4000-8000-0000000092f2"
		// A third campaign for the pen's SECOND whole-pen weigh: one bucket may hold only one LIVE
		// shed observation (weighing_shed_observations_one_open_scope_uidx), so a pen weighed whole
		// twice really is two buckets.
		raceCampaignTwo  = "00000000-0000-4000-8000-0000000092f3"
		secondLumpBucket = "00000000-0000-4000-9000-0000000092f4"
	)

	// The pen's OWN residents: four Anantapur Sheep, the cohort a whole-pen weigh covers.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats SET breed='Anantapur Sheep', sex='male'
WHERE tenant_id=$1::uuid AND shed_id=$2::uuid`, repoTenant, repoPerShed)
	// The animals whose TAGS were scanned in that pen are different animals of different breeds,
	// and the register does not place them there -- exactly the STG shape.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats SET breed='Sojat', sex='male' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		repoTenant, repoAnimal)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990004', 'Beetal', 'male', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET breed=EXCLUDED.breed, sex=EXCLUDED.sex, shed_id=EXCLUDED.shed_id`,
		repoAnimalTwo, repoTenant, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES
  ($1::uuid, $2::uuid, 'animal_identifier_1', 'chip-race-one', 'chip-race-one', 'global', true, 'active', now(), 'test'),
  ($1::uuid, $3::uuid, 'animal_identifier_1', 'chip-race-two', 'chip-race-two', 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id=EXCLUDED.goat_id, identifier_value=EXCLUDED.identifier_value, status='active'`,
		repoTenant, repoAnimal, repoAnimalTwo)

	// An individual bucket on the SAME pen as the whole-pen bucket, in its own campaign.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-27', '2026-08-02', '2026-07-29', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`,
		raceCampaign, repoTenant, repoPark, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (
  campaign_shed_id, campaign_id, tenant_id, location_id, location_type,
  display_name, weighing_category, operator_user_id, expected_animal_count, status)
-- 'completed': only ONE bucket per park/date/pen may be OPEN
-- (uq_weighing_open_shed_partition_per_park_date), and the whole-pen bucket already holds that
-- slot. A finished round of scanning is what this fixture means anyway.
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Lump Pen', 'individual_animal', $5::uuid, 0, 'completed')
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		individualBucketOnLumpPen, raceCampaign, repoTenant, repoPerShed, repoOperator)

	// ONE-TO-MANY on the scanned side: each animal is scanned THREE times, twice on the pen's
	// latest day. Two animals were weighed, however many captures they left behind.
	scanDay := time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC)
	for _, tag := range []string{"chip-race-one", "chip-race-two"} {
		for i, at := range []time.Time{scanDay.AddDate(0, 0, -1), scanDay, scanDay.Add(3 * time.Hour)} {
			execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 24.0, $5::uuid, $6::uuid, $7, $8::timestamptz, $8::timestamptz)`,
				repoTenant, raceCampaign, individualBucketOnLumpPen, tag, repoAnimalProof, repoOperator,
				fmt.Sprintf("race:%s:%d", tag, i), at)
		}
	}

	window := func() (time.Time, time.Time) {
		return time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	}
	cohortFor := func(t *testing.T) (string, []string, int) {
		t.Helper()
		from, to := window()
		// A FRESH repository per read: this path memoises by (tenant, parks, window, filters,
		// sections), so asking the same question twice inside one instance answers from the cache
		// and the second half of this test would assert against the first half's answer.
		repo := NewRepository(pool, 5*time.Second)
		out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "composition", nil, domain.TimeScope{})
		if err != nil {
			t.Fatalf("GetWeightDemographics(composition): %v", err)
		}
		for _, shed := range out.ShedComposition {
			if shed.LocationID != repoPerShed {
				continue
			}
			breeds := make([]string, 0, len(shed.Chips))
			for _, chip := range shed.Chips {
				breeds = append(breeds, chip.Breed)
			}
			sort.Strings(breeds)
			return shed.Source, breeds, shed.TotalAnimals
		}
		t.Fatalf("no composition for the pen in %#v", out.ShedComposition)
		return "", nil, 0
	}

	// SCANS ONLY so far: the pen is described by the animals actually scanned in it.
	source, breeds, total := cohortFor(t)
	if source != "scanned_tags" || total != 2 || len(breeds) != 2 || breeds[0] != "Beetal" || breeds[1] != "Sojat" {
		t.Fatalf("with scans as the pen's only weigh the cohort is the scanned one, got source=%q breeds=%#v total=%d",
			source, breeds, total)
	}

	// A WHOLE-PEN weigh LATER in the same window: the row now reports that weigh, so the cohort
	// must follow it -- the pen's own four residents, not one scanned kid of another breed.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_observations (
  tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count,
  proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 96.0, 24.0, 4, $4::uuid, $5::uuid, 'race:lump', $6::timestamptz)`,
		repoTenant, repoCampaign, repoShedScope, repoShedProofTwo, repoOperator,
		scanDay.AddDate(0, 0, 1))

	// ONE-TO-MANY on the whole-pen side too: a SECOND live whole-pen weigh, later again. The pen
	// holds four animals however many times it was put on the scale.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-27', '2026-08-02', '2026-07-31', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`,
		raceCampaignTwo, repoTenant, repoPark, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (
  campaign_shed_id, campaign_id, tenant_id, location_id, location_type,
  display_name, weighing_category, operator_user_id, expected_animal_count, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Lump Pen', 'per_shed_partition', $5::uuid, 0, 'completed')
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		secondLumpBucket, raceCampaignTwo, repoTenant, repoPerShed, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_observations (
  tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count,
  proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 100.0, 25.0, 4, $4::uuid, $5::uuid, 'race:lump-two', $6::timestamptz)`,
		repoTenant, raceCampaignTwo, secondLumpBucket, repoShedProofThree, repoOperator,
		scanDay.AddDate(0, 0, 2))

	source, breeds, total = cohortFor(t)
	if source != "live_shed_cohort" || total != 4 {
		t.Fatalf("a whole-pen weigh AFTER the last scan owns the cohort, got source=%q total=%d (want live_shed_cohort/4)",
			source, total)
	}
	for _, breed := range breeds {
		if breed != "Anantapur Sheep" {
			t.Fatalf("the whole-pen cohort is the pen's own residents, got %#v", breeds)
		}
	}

	// PAGE BOUNDARY: the cohort is a whole-result fact about the pen, so it cannot move with the
	// page of shed rows a caller asked for. Both reads are taken over the same window and filters.
	from, to := window()
	for _, page := range []struct {
		name           string
		selectedParkID string
	}{{"all parks", ""}, {"one park", repoPark}} {
		rows, err := NewRepository(pool, 5*time.Second).GetShedWeights(ctx, repoTenant, []string{repoPark},
			page.selectedParkID, from, to, "", "", "", 0, 30, 35)
		if err != nil {
			t.Fatalf("GetShedWeights(%s): %v", page.name, err)
		}
		var found bool
		for _, row := range rows.Rows {
			if row.LocationID != repoPerShed {
				continue
			}
			found = true
			if row.AnimalsWeighed != 4 {
				t.Fatalf("%s: the pen's row counts the animals it weighed once, got %d captures' worth",
					page.name, row.AnimalsWeighed)
			}
		}
		if !found {
			t.Fatalf("%s: the pen is missing from the shed rows the cohort labels", page.name)
		}
		if _, gotBreeds, gotTotal := cohortFor(t); gotTotal != total || len(gotBreeds) != len(breeds) {
			t.Fatalf("%s: the cohort moved with the caller's page: %d/%#v became %d/%#v",
				page.name, total, breeds, gotTotal, gotBreeds)
		}
	}

	// STATUS: WITHDRAWN is how a weigh is retired here -- migration 000058 narrowed
	// verification_status to pending/verified/rework, so `<> 'rejected'` can no longer exclude
	// anything and withdrawal is the real lever. Retire the LATER whole-pen weigh and the earlier
	// one still stands, so the pen stays with its residents.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_shed_observations SET withdrawn_at = now()
WHERE tenant_id = $1::uuid AND campaign_shed_id = $2::uuid`, repoTenant, secondLumpBucket)
	if source, _, total := cohortFor(t); source != "live_shed_cohort" || total != 4 {
		t.Fatalf("one retired whole-pen weigh does not hand the pen back while another still stands, got source=%q total=%d",
			source, total)
	}

	// STATUS, the other way: retire the LAST standing whole-pen weigh and the scans are the pen's
	// most recent weigh again, so the cohort returns to them. This is what proves the race reads
	// the live rows rather than merely preferring one grain.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_shed_observations SET withdrawn_at = now()
WHERE tenant_id = $1::uuid AND campaign_shed_id = $2::uuid`, repoTenant, repoShedScope)
	if source, breeds, total := cohortFor(t); source != "scanned_tags" || total != 2 || len(breeds) != 2 {
		t.Fatalf("with every whole-pen weigh withdrawn the scans own the pen again, got source=%q breeds=%#v total=%d",
			source, breeds, total)
	}
}

// TestFilteredPenCohortKeepsScansWhenTheLaterWholePenWeighCannotBeClaimed is the regression test
// for the review finding on PR 346: the grain race suppressed a filtered page's scanned cohort
// using a whole-pen weigh that same page refuses to count.
//
// pen_lump_day asked only "was this pen weighed whole, and when", while `lump` -- the CTE every
// whole-pen consumer reads -- claims a pen only when its resident cohort is single-sex and matches
// the selected sex, and when the pen is inside the selected origin scope. A pen of MALE residents
// scanned on 29 Jul and weighed WHOLE on 30 Jul therefore fell between the two on a FEMALE page:
// the scans were suppressed because a later whole-pen weigh existed, and lump_composition never
// produced the pen because `lump` correctly refused to claim it. The pen vanished from the
// composition panel entirely, taking real filtered scanned animals with it.
//
// The race must therefore run against whole-pen weighs THIS page can claim, not against every
// whole-pen weigh in the window.
//
// MUTATION TEST when this was written: dropping either claim predicate from pen_lump_day turns
// this red with the pen missing from ShedComposition, while the unfiltered assertion below still
// passes -- which is exactly why the shipped grain-race test did not catch it.
func TestFilteredPenCohortKeepsScansWhenTheLaterWholePenWeighCannotBeClaimed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)

	const (
		filteredIndividualBucket = "00000000-0000-4000-9000-0000000093f1"
		filteredCampaign         = "00000000-0000-4000-8000-0000000093f2"
	)

	// The pen's OWN residents are MALE, so a female page cannot claim the whole-pen weigh.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats SET breed='Anantapur Sheep', sex='male'
WHERE tenant_id=$1::uuid AND shed_id=$2::uuid`, repoTenant, repoPerShed)
	// The animals whose tags were SCANNED in that pen are FEMALE, so they are exactly what a
	// female page is asking to see.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats SET breed='Sojat', sex='female' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		repoTenant, repoAnimal)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990005', 'Beetal', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET breed=EXCLUDED.breed, sex=EXCLUDED.sex, shed_id=EXCLUDED.shed_id`,
		repoAnimalTwo, repoTenant, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES
  ($1::uuid, $2::uuid, 'animal_identifier_1', 'chip-filter-one', 'chip-filter-one', 'global', true, 'active', now(), 'test'),
  ($1::uuid, $3::uuid, 'animal_identifier_1', 'chip-filter-two', 'chip-filter-two', 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id=EXCLUDED.goat_id, identifier_value=EXCLUDED.identifier_value, status='active'`,
		repoTenant, repoAnimal, repoAnimalTwo)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-27', '2026-08-02', '2026-07-29', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`,
		filteredCampaign, repoTenant, repoPark, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (
  campaign_shed_id, campaign_id, tenant_id, location_id, location_type,
  display_name, weighing_category, operator_user_id, expected_animal_count, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Lump Pen', 'individual_animal', $5::uuid, 0, 'completed')
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		filteredIndividualBucket, filteredCampaign, repoTenant, repoPerShed, repoOperator)

	scanDay := time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC)
	for _, tag := range []string{"chip-filter-one", "chip-filter-two"} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 24.0, $5::uuid, $6::uuid, $7, $8::timestamptz, $8::timestamptz)`,
			repoTenant, filteredCampaign, filteredIndividualBucket, tag, repoAnimalProof, repoOperator,
			fmt.Sprintf("filtered-race:%s", tag), scanDay)
	}
	// The whole-pen weigh, LATER than every scan. A female page must not count it -- the pen holds
	// only male residents -- and must not let it silence the scans either.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_observations (
  tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count,
  proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 96.0, 24.0, 4, $4::uuid, $5::uuid, 'filtered-race:lump', $6::timestamptz)`,
		repoTenant, repoCampaign, repoShedScope, repoShedProofTwo, repoOperator,
		scanDay.AddDate(0, 0, 1))

	from := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	// A FRESH repository per read: this path memoises by (tenant, parks, window, filters, sections).
	cohortFor := func(t *testing.T, sex string) (string, int, bool) {
		t.Helper()
		out, err := NewRepository(pool, 5*time.Second).
			GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, sex, "", "", "composition", nil, domain.TimeScope{})
		if err != nil {
			t.Fatalf("GetWeightDemographics(sex=%q): %v", sex, err)
		}
		for _, shed := range out.ShedComposition {
			if shed.LocationID == repoPerShed {
				return shed.Source, shed.TotalAnimals, true
			}
		}
		return "", 0, false
	}

	// THE FINDING: a female page keeps its scanned animals.
	source, total, found := cohortFor(t, "female")
	if !found {
		t.Fatalf("the pen vanished from a female page: its two scanned female kids were suppressed by " +
			"a whole-pen weigh that same page refuses to count")
	}
	if source != "scanned_tags" || total != 2 {
		t.Fatalf("a female page describes the pen by the animals it can claim, got source=%q total=%d (want scanned_tags/2)",
			source, total)
	}

	// AND THE GRAIN RACE STILL HOLDS unfiltered, where the whole-pen weigh IS claimable: the later
	// weigh owns the cohort, which is the defect the race was added for.
	source, total, found = cohortFor(t, "")
	if !found {
		t.Fatalf("the pen vanished from the unfiltered page")
	}
	if source != "live_shed_cohort" || total != 4 {
		t.Fatalf("unfiltered, the later whole-pen weigh still owns the cohort, got source=%q total=%d (want live_shed_cohort/4)",
			source, total)
	}
}
