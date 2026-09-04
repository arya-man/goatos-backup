package postgres

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// The per-park daily-gain cards ("CBE — daily gain" beside "All parks — daily gain") are cut from
// the SAME growth request the page already makes. They used to be built by calling that endpoint
// once per park from the page; those calls were removed for page speed and the cards silently
// emptied, because the markup survived with nothing to render.
//
// Everything here is adversarial about the ways a per-park cut can be plausible and wrong: an
// animal weighed three times counted twice, a park's figure quietly including the other park's
// animals, a rejected weigh counted, or the cut arriving truncated.

const (
	pgCampaignCPT = "00000000-0000-4000-8000-00000000b001"
	pgBucketCPT   = "00000000-0000-4000-8000-00000000b002"
	pgShedCPT     = "00000000-0000-4000-8000-00000000b003"
	pgCampCPTTwo  = "00000000-0000-4000-8000-00000000b006"
	pgBucketCPT2  = "00000000-0000-4000-8000-00000000b007"
)

// seedParkGainSecondPark gives CPT a park, a shed, a campaign and a whole-pen bucket, so the herd
// spans two parks the way the real farm does.
func seedParkGainSecondPark(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	// CPT is a REAL park in the baseline (a database trigger refuses to invent a CBE/CPT/HF row
	// outside an approved migration), so this seeds only the work that happens inside it.
	lsInsertShed(t, ctx, pool, pgShedCPT, lsParkCPT, "CPT Pen 1", 1)
	// Two proof artifacts for the pen's two weighs: a whole-pen weigh carries a video, and the
	// foreign key is what says so.
	for _, proofID := range []string{repoShedProofFive, repoShedProofSix} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	// An operator belongs to ONE park, and the database enforces it on the bucket: the CPT work
	// below needs a CPT-scoped grant before any bucket in that park will insert.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOperator, lsParkCPT)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-06', '2026-08-31', '2026-07-08', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, pgCampaignCPT, repoTenant, lsParkCPT, repoOperator)
	seedLoadBucket(t, ctx, pool, pgBucketCPT, pgCampaignCPT, pgShedCPT, string(domain.CategoryPerShedPartition))
	// A pen holds at most ONE open whole-pen weigh per bucket, so its history necessarily lives
	// across buckets -- which is why every gain CTE keys on location, not on bucket. The second
	// campaign is what lets this pen be weighed twice at all.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-08-10', '2026-08-31', '2026-08-14', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, pgCampCPTTwo, repoTenant, lsParkCPT, repoOperator)
	seedLoadBucket(t, ctx, pool, pgBucketCPT2, pgCampCPTTwo, pgShedCPT, string(domain.CategoryPerShedPartition))
}

// seedParkGainWeigh writes one accepted, SUBMITTED weigh. Submitted matters: a bucket holds at
// most one OPEN row per tag, so a kid weighed repeatedly over a period is necessarily a kid whose
// earlier weighs were submitted -- which is exactly the history a gain is read out of.
func seedParkGainWeigh(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, campaignID, tag string, weightKg float64, at time.Time) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz, $9::timestamptz)`,
		repoTenant, campaignID, bucketID, tag, weightKg, repoAnimalProof, repoOperator,
		fmt.Sprintf("parkgain:%s:%d", tag, at.UnixNano()), at)
}

func parkGain(t *testing.T, adg domain.GrowthADG, parkID string) domain.GrowthParkGain {
	t.Helper()
	for _, park := range adg.ByPark {
		if park.ParkID == parkID {
			return park
		}
	}
	t.Fatalf("park %s missing from by_park (%d rows)", parkID, len(adg.ByPark))
	return domain.GrowthParkGain{}
}

// CARDINALITY. inperiod holds PAIRS: a kid weighed three times contributes two of them. The park
// cut groups by (park, animal) first, so that kid weighs ONCE in its park's denominator -- the
// same rule the herd headline follows. Counting pairs instead would let the most-handled kids
// speak twice for their park.
func TestParkGainsAreOneToManyPairsCollapsedToOneAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedParkGainSecondPark(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	// ONE kid, weighed twice: one pair, one animal.
	day := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-thrice", 20.0, day)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-thrice", 22.0, day.AddDate(0, 0, 10))
	// A second park with work in it, so the cut has two rows to get wrong.
	seedLoadLumpWeigh(t, ctx, pool, pgBucketCPT, pgCampaignCPT, repoShedProofFive, 20.0, 10, day)
	seedLoadLumpWeigh(t, ctx, pool, pgBucketCPT2, pgCampCPTTwo, repoShedProofSix, 23.0, 10, day.AddDate(0, 0, 10))

	before, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark, lsParkCPT}, start, end, "", "", "")
	if err != nil {
		t.Fatalf("baseline growth: %v", err)
	}
	cbeBefore := parkGain(t, before, repoPark)

	// A THIRD weigh of the SAME kid: a second pair, still one animal.
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-thrice", 24.0, day.AddDate(0, 0, 20))

	after, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark, lsParkCPT}, start, end, "", "", "")
	if err != nil {
		t.Fatalf("growth after the third weigh: %v", err)
	}
	cbeAfter := parkGain(t, after, repoPark)
	if cbeAfter.HeadlineAnimals != cbeBefore.HeadlineAnimals {
		t.Fatalf("a third weigh of ONE kid moved the park denominator %d -> %d; pairs are being counted as animals",
			cbeBefore.HeadlineAnimals, cbeAfter.HeadlineAnimals)
	}
	if delta := after.Headline.PairCount - before.Headline.PairCount; delta != 1 {
		t.Fatalf("the third weigh added %d pair(s), want 1 -- the fixture is not what this test thinks it is", delta)
	}
}

// SCOPE. Each park's card must describe that park and nothing else. The strongest available check
// is the read's own single-park answer: asking for CBE alone must produce the figure the two-park
// response prints on the CBE card, animal for animal.
func TestParkGainsParkScopeMatchesTheSingleParkRead(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedParkGainSecondPark(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	day := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-cbe", 20.0, day)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-cbe", 23.0, day.AddDate(0, 0, 10))
	// CPT is weighed as a WHOLE PEN: no tag at all, ten animals, counted at the pen's own movement.
	seedLoadLumpWeigh(t, ctx, pool, pgBucketCPT, pgCampaignCPT, repoShedProofFive, 20.0, 10, day)
	seedLoadLumpWeigh(t, ctx, pool, pgBucketCPT2, pgCampCPTTwo, repoShedProofSix, 23.0, 10, day.AddDate(0, 0, 10))

	both, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark, lsParkCPT}, start, end, "", "", "")
	if err != nil {
		t.Fatalf("two-park growth: %v", err)
	}
	for _, parkID := range []string{repoPark, lsParkCPT} {
		alone, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{parkID}, start, end, "", "", "")
		if err != nil {
			t.Fatalf("single-park growth for %s: %v", parkID, err)
		}
		card := parkGain(t, both, parkID)
		if card.HeadlineAnimals != alone.Headline.HeadlineAnimals {
			t.Fatalf("park %s card counts %d animals, its own read counts %d", parkID, card.HeadlineAnimals, alone.Headline.HeadlineAnimals)
		}
		if (card.AverageADGGPerDay == nil) != (alone.Headline.AverageADGGPerDay == nil) {
			t.Fatalf("park %s card gain nil=%v, its own read nil=%v", parkID, card.AverageADGGPerDay == nil, alone.Headline.AverageADGGPerDay == nil)
		}
		if card.AverageADGGPerDay != nil && math.Abs(*card.AverageADGGPerDay-*alone.Headline.AverageADGGPerDay) > 0.001 {
			t.Fatalf("park %s card gain %.3f, its own read %.3f", parkID, *card.AverageADGGPerDay, *alone.Headline.AverageADGGPerDay)
		}
	}
	// The parks partition the herd: no animal is claimed twice, and none is dropped.
	total := 0
	for _, park := range both.ByPark {
		total += park.HeadlineAnimals
	}
	if total != both.Headline.HeadlineAnimals {
		t.Fatalf("park cards account for %d animals, the herd headline for %d", total, both.Headline.HeadlineAnimals)
	}
	// The CPT figure is whole-pen movement -- ten animals from a pen carrying no tag at all.
	if cpt := parkGain(t, both, lsParkCPT); cpt.HeadlineAnimals != 10 {
		t.Fatalf("CPT counts %d animals; a pen of ten weighed twice must count ten", cpt.HeadlineAnimals)
	}
}

// PAGINATION. The cut is ONE ROW PER PARK IN SCOPE and is never paged -- a truncated list would
// silently delete a park's card while the page still rendered the others. And a reader who has
// already narrowed to one park gets NO cards: the headline above is then that park's own figure,
// and a card restating it is one number printed twice.
func TestParkGainsPaginationIsOneRowPerParkAndAbsentForASinglePark(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedParkGainSecondPark(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	day := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-page-cbe", 20.0, day)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-page-cbe", 23.0, day.AddDate(0, 0, 10))
	seedLoadLumpWeigh(t, ctx, pool, pgBucketCPT, pgCampaignCPT, repoShedProofFive, 20.0, 10, day)
	seedLoadLumpWeigh(t, ctx, pool, pgBucketCPT2, pgCampCPTTwo, repoShedProofSix, 23.0, 10, day.AddDate(0, 0, 10))

	both, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark, lsParkCPT}, start, end, "", "", "")
	if err != nil {
		t.Fatalf("two-park growth: %v", err)
	}
	if len(both.ByPark) != 2 {
		t.Fatalf("by_park has %d rows for two weighed parks, want 2", len(both.ByPark))
	}
	seen := map[string]int{}
	for _, park := range both.ByPark {
		seen[park.ParkID]++
		if park.ParkName == "" {
			t.Fatalf("park %s has no name; a card labelled with a uuid is unreadable", park.ParkID)
		}
	}
	for parkID, count := range seen {
		if count != 1 {
			t.Fatalf("park %s appears %d times in by_park, want exactly one row", parkID, count)
		}
	}
	// The short code, the same label the shed rows and load placements use.
	if name := parkGain(t, both, lsParkCPT).ParkName; name != "CPT" {
		t.Fatalf("CPT card is labelled %q, want the park's short code", name)
	}

	alone, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "")
	if err != nil {
		t.Fatalf("single-park growth: %v", err)
	}
	if alone.ByPark == nil {
		t.Fatalf("by_park is nil for a single-park request; the wire contract is [] or populated, never null")
	}
	if len(alone.ByPark) != 0 {
		t.Fatalf("by_park has %d rows for a single-park request, want none", len(alone.ByPark))
	}
}

func TestParkGainsIncludeScopedParksWithNoQualifyingGain(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedParkGainSecondPark(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	day := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-only-cbe", 20.0, day)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-only-cbe", 23.0, day.AddDate(0, 0, 10))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark, lsParkCPT}, start, end, "", "", "")
	if err != nil {
		t.Fatalf("two-park growth: %v", err)
	}
	if len(adg.ByPark) != 2 {
		t.Fatalf("by_park has %d rows for two scoped parks, want 2 even when one has no gain", len(adg.ByPark))
	}
	cpt := parkGain(t, adg, lsParkCPT)
	if cpt.AverageADGGPerDay != nil {
		t.Fatalf("CPT gain = %.3f, want nil for a scoped park with nothing weighed twice", *cpt.AverageADGGPerDay)
	}
	if cpt.HeadlineAnimals != 0 {
		t.Fatalf("CPT headline animals = %d, want 0 for a scoped park with nothing weighed twice", cpt.HeadlineAnimals)
	}
}

// STATUS. Two halves of one rule, and they pull in opposite directions on purpose. A WITHDRAWN pen
// weigh is not a weigh and must leave the park exactly where it was. A weigh still awaiting
// verification IS a real measurement and must be counted -- hiding pending weights from leadership
// is the defect the sibling weight-history read already settled, and a park card that waited for a
// verdict would read as a herd that stopped growing every time the verifier was a day behind.
//
// ('rejected' is deliberately not exercised: neither weighing table can hold that value -- the
// check constraint allows pending/verified/rework only -- so the SQL's `<> 'rejected'` guard is
// vocabulary carried forward, not a state a row can actually reach.)
func TestParkGainsStatusBucketsExcludeWithdrawnAndKeepPendingWeighs(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedParkGainSecondPark(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	day := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-status", 20.0, day)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-status", 23.0, day.AddDate(0, 0, 10))
	seedLoadLumpWeigh(t, ctx, pool, pgBucketCPT, pgCampaignCPT, repoShedProofFive, 20.0, 10, day)
	seedLoadLumpWeigh(t, ctx, pool, pgBucketCPT2, pgCampCPTTwo, repoShedProofSix, 23.0, 10, day.AddDate(0, 0, 10))

	before, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark, lsParkCPT}, start, end, "", "", "")
	if err != nil {
		t.Fatalf("growth before the withdrawn weigh: %v", err)
	}
	cbeBefore := parkGain(t, before, repoPark)
	cptBefore := parkGain(t, before, lsParkCPT)
	if cptBefore.AverageADGGPerDay == nil {
		t.Fatalf("CPT has no gain to protect; the pen fixture did not take")
	}

	// A WITHDRAWN pen weigh at an absurd 90 kg: counted, it would triple CPT's gain.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at, withdrawn_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 900, 90, 10, $4::uuid, $5::uuid, 'park-gain-withdrawn', $6::timestamptz, now())`,
		repoTenant, pgCampCPTTwo, pgBucketCPT2, repoShedProofSix, repoOperator, day.AddDate(0, 0, 20))

	after, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark, lsParkCPT}, start, end, "", "", "")
	if err != nil {
		t.Fatalf("growth after the withdrawn weigh: %v", err)
	}
	cptAfter := parkGain(t, after, lsParkCPT)
	if cptAfter.HeadlineAnimals != cptBefore.HeadlineAnimals {
		t.Fatalf("a WITHDRAWN pen weigh moved the CPT denominator %d -> %d", cptBefore.HeadlineAnimals, cptAfter.HeadlineAnimals)
	}
	if cptAfter.AverageADGGPerDay == nil {
		t.Fatalf("CPT lost its gain figure to a withdrawn weigh")
	}
	if math.Abs(*cptAfter.AverageADGGPerDay-*cptBefore.AverageADGGPerDay) > 0.001 {
		t.Fatalf("a WITHDRAWN 90 kg pen weigh moved CPT's gain %.3f -> %.3f", *cptBefore.AverageADGGPerDay, *cptAfter.AverageADGGPerDay)
	}

	// A kid weighed twice while still PENDING verification: counted, not held back.
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-pending", 21.0, day)
	seedParkGainWeigh(t, ctx, pool, repoAnimalScope, repoCampaign, "park-gain-pending", 24.0, day.AddDate(0, 0, 10))
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_observations SET verification_status = 'pending'
WHERE tenant_id = $1::uuid AND scanned_identifier = 'park-gain-pending'`, repoTenant)

	withPending, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark, lsParkCPT}, start, end, "", "", "")
	if err != nil {
		t.Fatalf("growth with a pending kid: %v", err)
	}
	if got := parkGain(t, withPending, repoPark); got.HeadlineAnimals != cbeBefore.HeadlineAnimals+1 {
		t.Fatalf("a kid awaiting verification moved the CBE denominator %d -> %d, want +1: a pending weight is a real measurement",
			cbeBefore.HeadlineAnimals, got.HeadlineAnimals)
	}
}
