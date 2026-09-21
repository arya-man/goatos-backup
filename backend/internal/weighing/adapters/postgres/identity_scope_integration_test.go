package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// TWO RFIDs ON ONE ANIMAL ARE ONE ANIMAL, for every number the Weights and Growth screens report.
//
// The defect these pin (maintainer report 2026-09-07): an animal here can carry two RFIDs, and the
// operator scans whichever tag they can read. Weighing keyed an animal by the RAW SCANNED STRING,
// so an animal weighed on its primary tag one week and its secondary the next was TWO animals with
// ONE weigh each. It produced no pair, and therefore NO ADG AT ALL -- a silently MISSING number,
// which is worse than a visibly wrong one -- while counting twice in the denominators the page's
// averages divide. Reverting animal_key to lower(btrim(scanned_identifier)) turns these red.

// seedDoubleTaggedGoat writes one goat carrying two identifiers, and is where these tests differ
// from every other growth fixture: the two tags are REAL rows in the herd register, which is the
// only place the fact that they are one animal exists at all.
func seedDoubleTaggedGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, displayID, primaryTag, secondaryTag, secondaryStatus string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'male', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE SET shed_id=EXCLUDED.shed_id`,
		goatID, repoTenant, displayID, repoParty, repoExpectedShed, repoPark)
	seedGoatIdentifier(t, ctx, pool, goatID, "animal_identifier_1", primaryTag, "active")
	if secondaryTag != "" {
		seedGoatIdentifier(t, ctx, pool, goatID, "animal_identifier_2", secondaryTag, secondaryStatus)
	}
}

func seedGoatIdentifier(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, kind, value, status string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (
  tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key,
  is_primary_for_goat, status, valid_from, normalizer_version
) VALUES ($1::uuid, $2::uuid, $3, $4, upper($4), 'tenant', $5, $6, now(), 'v1')`,
		repoTenant, goatID, kind, value, kind == "animal_identifier_1", status)
}

// markWeighsSubmitted closes every open scan in the fixture. Two weighs of ONE tag in ONE bucket
// cannot both be open (000073), so a test that weighs the same tag twice submits the first round
// first -- the ordinary lifecycle, not a workaround.
func markWeighsSubmitted(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_observations SET submitted_at = accepted_at
WHERE tenant_id = $1::uuid AND submitted_at IS NULL`, repoTenant)
}

const (
	idScopeGoat      = "00000000-0000-4000-8000-0000000097a1"
	idScopeGoatTwo   = "00000000-0000-4000-8000-0000000097a2"
	idScopePrimary   = "rfid-primary-001"
	idScopeSecondary = "rfid-secondary-001"
)

// THE CORE REGRESSION. Week 1 on the primary tag, week 2 on the secondary: one animal, one pair,
// one ADG. Before the fix this produced PairCount 0 and a nil headline while reporting TWO animals
// weighed -- the animal was invisible to every gain statistic on the page and double-counted in
// every coverage counter on it at the same time.
func TestTwoRFIDsOnOneAnimalPairIntoOneADG(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	seedDoubleTaggedGoat(t, ctx, pool, idScopeGoat, "G-970001", idScopePrimary, idScopeSecondary, "active")

	// 20.0 kg scanned on the PRIMARY tag, then 22.1 kg on the SECONDARY seven days later.
	// 2.1 kg over 7 whole days = 300 g/day.
	first := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, idScopePrimary, 20.0, first)
	seedGrowthObservation(t, ctx, pool, idScopeSecondary, 22.1, first.AddDate(0, 0, 7))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 1 {
		t.Fatalf("PairCount=%d, want 1: an animal weighed on its primary RFID then its secondary is ONE animal weighed twice, not two animals weighed once", adg.Headline.PairCount)
	}
	if adg.Headline.HeadlineAnimals != 1 {
		t.Fatalf("HeadlineAnimals=%d, want 1", adg.Headline.HeadlineAnimals)
	}
	if adg.Headline.AverageADGGPerDay == nil {
		t.Fatal("headline ADG is nil: the animal's two weighs did not pair, which is the missing-number defect this test exists for")
	}
	if got := *adg.Headline.AverageADGGPerDay; got < 299.9 || got > 300.1 {
		t.Fatalf("headline ADG=%.3f g/day, want 300 (2.1 kg over 7 days)", got)
	}
	// The coverage counters are the other half of the same defect, and they moved in the OPPOSITE
	// direction: two animals, neither with a pair.
	if adg.Eligibility.TotalAnimalsWeighed != 1 {
		t.Fatalf("TotalAnimalsWeighed=%d, want 1: the same animal under two tags was counted twice", adg.Eligibility.TotalAnimalsWeighed)
	}
	if adg.Eligibility.AnimalsWithTwoPlusWeighs != 1 {
		t.Fatalf("AnimalsWithTwoPlusWeighs=%d, want 1", adg.Eligibility.AnimalsWithTwoPlusWeighs)
	}
}

// THE MERGED HISTORY IS REPORTED UNDER THE PRIMARY TAG, never a goat_id and never whichever tag
// happened to be scanned last. animal_key is rendered verbatim to a reader as ScannedIdentifier in
// the losing-animals list, so a uuid here would put a database key on a farm screen -- which is
// why the canonical key is one of the animal's OWN identifiers.
func TestMergedAnimalIsReportedUnderItsPrimaryRFID(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	seedDoubleTaggedGoat(t, ctx, pool, idScopeGoat, "G-970001", idScopePrimary, idScopeSecondary, "active")

	// A LOSS, so the animal lands in the losing-animals list where the key is rendered. Scanned on
	// the SECONDARY tag last, to prove the report does not simply echo the most recent scan.
	first := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, idScopePrimary, 24.0, first)
	seedGrowthObservation(t, ctx, pool, idScopeSecondary, 22.6, first.AddDate(0, 0, 7))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if len(adg.LosingAnimals) != 1 {
		t.Fatalf("losing animals=%d, want 1", len(adg.LosingAnimals))
	}
	if got := adg.LosingAnimals[0].ScannedIdentifier; got != idScopePrimary {
		t.Fatalf("losing animal reported as %q, want the primary RFID %q: the merged history must report under the tag the farm calls primary, not the tag that happened to be scanned last", got, idScopePrimary)
	}
}

// A SINGLE-TAG ANIMAL IS NEVER REMAPPED, and neither is a tag the register has never heard of.
// This is what bounds the blast radius of the herd exception: a farm with no double-tagged animal
// runs the query it ran before this feature existed, key for key. If the map ever starts rewriting
// keys for animals it was not asked about, this is the test that says so.
func TestSingleTaggedAndUnknownTagsAreUntouched(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	// One animal with ONE registered tag, and one tag belonging to no animal at all -- free-flow
	// capture accepts a scan the register cannot resolve, and it must still be counted and paired.
	seedDoubleTaggedGoat(t, ctx, pool, idScopeGoat, "G-970001", "rfid-single-001", "", "")
	first := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, "rfid-single-001", 20.0, first)
	seedGrowthObservation(t, ctx, pool, "rfid-not-in-register", 30.0, first)
	// The first round is SUBMITTED before the second is scanned, which is both what actually happens
	// on the farm and what weighing_observations_one_open_tag_uidx (000073) requires: one OPEN row
	// per tag per bucket is the "no scanning an animal twice in a bucket" rule, and it is untouched
	// by this change -- it still compares raw strings, deliberately.
	markWeighsSubmitted(t, ctx, pool)
	seedGrowthObservation(t, ctx, pool, "rfid-single-001", 21.4, first.AddDate(0, 0, 7))
	seedGrowthObservation(t, ctx, pool, "rfid-not-in-register", 31.4, first.AddDate(0, 0, 7))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 2 {
		t.Fatalf("PairCount=%d, want 2: a single-tag animal and an unregistered tag each pair with themselves exactly as before", adg.Headline.PairCount)
	}
	if adg.Eligibility.TotalAnimalsWeighed != 2 {
		t.Fatalf("TotalAnimalsWeighed=%d, want 2: these are two distinct animals and must not merge", adg.Eligibility.TotalAnimalsWeighed)
	}
}

// A temporary tag is a birth/provisional identity, not the second RFID slot this resolver exists for.
// A genuine re-tag still splits history: weighing only knew the string scanned that day, and merging a
// temporary-tag weigh into the later permanent RFID would rewrite old capture history into an ADG the
// operator never observed under two permanent identifiers.
func TestTemporaryTagDoesNotMergeWithPermanentRFID(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	seedDoubleTaggedGoat(t, ctx, pool, idScopeGoat, "G-970001", idScopePrimary, "", "")
	seedGoatIdentifier(t, ctx, pool, idScopeGoat, "temporary_tag", "temp-birth-001", "active")

	first := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, "temp-birth-001", 20.0, first)
	seedGrowthObservation(t, ctx, pool, idScopePrimary, 22.1, first.AddDate(0, 0, 7))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 0 {
		t.Fatalf("PairCount=%d, want 0: temporary_tag is not a second RFID and must not merge into the permanent tag's ADG history", adg.Headline.PairCount)
	}
	if adg.Eligibility.TotalAnimalsWeighed != 2 {
		t.Fatalf("TotalAnimalsWeighed=%d, want 2: a temporary-tag scan and a permanent-RFID scan remain raw weighing identities", adg.Eligibility.TotalAnimalsWeighed)
	}
}

// A SECOND IDENTIFIER THE REGISTER DOES NOT TRUST MUST NOT MERGE TWO ANIMALS.
//
// This is the safety narrowing, and it is the whole reason only status='active' rows are read.
// 'duplicate'/'disputed'/'invalid' are the register's own record that an identifier is wrong --
// exactly the rows that, believed, would fuse two animals' weights and report the gap between them
// as growth that never happened. Dropping the status predicate turns this red.
func TestDisputedSecondIdentifierDoesNotMergeAnimals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	seedDoubleTaggedGoat(t, ctx, pool, idScopeGoat, "G-970001", idScopePrimary, idScopeSecondary, "duplicate")

	first := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, idScopePrimary, 20.0, first)
	seedGrowthObservation(t, ctx, pool, idScopeSecondary, 22.1, first.AddDate(0, 0, 7))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 0 {
		t.Fatalf("PairCount=%d, want 0: a 'duplicate' identifier is the register saying do not trust this tag, and merging on it invents a gain between two animals", adg.Headline.PairCount)
	}
	if adg.Eligibility.TotalAnimalsWeighed != 2 {
		t.Fatalf("TotalAnimalsWeighed=%d, want 2", adg.Eligibility.TotalAnimalsWeighed)
	}
}

// THE SHED TABLE MERGES THE SAME ANIMALS THE HEADLINE DOES. Fixing the growth read alone would
// leave one page whose headline counts an animal once and whose shed table counts it twice -- the
// cross-surface disagreement about a business number that is a maintainer question, not an
// implementation detail. This asserts the two reads agree on the fixture above.
func TestShedTableAndHeadlineAgreeOnADoubleTaggedAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	seedDoubleTaggedGoat(t, ctx, pool, idScopeGoat, "G-970001", idScopePrimary, idScopeSecondary, "active")
	first := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, idScopePrimary, 20.0, first)
	seedGrowthObservation(t, ctx, pool, idScopeSecondary, 22.1, first.AddDate(0, 0, 7))

	sheds, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, repoPark, start, end, "", "", "", 0, 0, 0)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	var animals int
	for _, row := range sheds.Rows {
		animals += row.AnimalsWeighed
	}
	if animals != 1 {
		t.Fatalf("shed table counted %d animals, want 1: the headline merges this animal's two RFIDs and the shed table beside it must merge the same one", animals)
	}
}

// ADVERSARIAL GRAIN TEST for the same-animal map: one-to-many fan-out, park scope, the status
// matrix, and the page boundary of the bounded lists, in one fixture.
//
// The map is spliced into every aggregate on the page as `LEFT JOIN unnest($tags, $canonical)`,
// which is the classic place a read model silently doubles: if the map could ever name one tag
// twice, EVERY weigh of that animal would fan out and be counted twice under COUNT/AVG/percentile.
// The uniqueness that prevents it lives in identity_scope.go's DISTINCT ON, one file away from the
// aggregates that depend on it, so it is asserted here rather than assumed.
func TestSameAnimalKeyOneToManyPageBoundaryParkScopeStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	// ONE-TO-MANY: the animal carries two identifiers, so it contributes TWO map rows. That is the
	// fan-out risk -- the map is spliced in as a LEFT JOIN, so the invariant that must hold is one
	// map row per TAG, not per animal. (The schema's goat_identifiers_lifetime_value_unique already
	// forbids re-issuing one tag string, so the DISTINCT ON in identity_scope.go is belt-and-braces
	// over a constraint; what is asserted here is the property the aggregates actually depend on.)
	seedDoubleTaggedGoat(t, ctx, pool, idScopeGoat, "G-970001", idScopePrimary, idScopeSecondary, "active")
	seedDoubleTaggedGoat(t, ctx, pool, idScopeGoatTwo, "G-970002", "rfid-primary-002", "rfid-secondary-002", "active")

	// STATUS MATRIX: weighing_observations carries pending / verified / rework (000058) and the growth
	// reads exclude only 'rejected', which this table cannot hold -- so every status counts. The merge
	// must change WHICH KEY a weigh groups under and nothing about WHICH STATUSES are counted, so the
	// same fixture is read twice, once with an empty map and once with the real one.
	first := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, idScopePrimary, 20.0, first)
	seedGrowthObservation(t, ctx, pool, idScopeSecondary, 22.1, first.AddDate(0, 0, 7))
	markWeighsSubmitted(t, ctx, pool)
	seedGrowthObservation(t, ctx, pool, idScopePrimary, 23.5, first.AddDate(0, 0, 14))
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_observations SET verification_status = CASE
  WHEN weight_kg = 20.0 THEN 'pending'
  WHEN weight_kg = 22.1 THEN 'verified'
  ELSE 'rework' END
WHERE tenant_id=$1::uuid AND weight_kg IN (20.0, 22.1, 23.5)`, repoTenant)

	// Resolved AFTER the weighs exist: the map is bounded by the tags actually weighed in the
	// window, so resolving it against an empty fixture returns an empty map and would assert
	// nothing at all -- the trap this ordering exists to avoid.
	idMap, err := repo.resolveAnimalIdentityMap(ctx, repoTenant, []string{repoPark}, start.AddDate(0, 0, -growthLookbackDays), end)
	if err != nil {
		t.Fatalf("resolve map: %v", err)
	}
	seen := map[string]int{}
	for _, tag := range idMap.Tags {
		seen[tag]++
	}
	for tag, n := range seen {
		if n > 1 {
			t.Fatalf("tag %q appears %d times in the map: the join is a LEFT JOIN onto these arrays, so a duplicate tag fans every weigh of that animal out under every aggregate on the page", tag, n)
		}
	}
	if len(idMap.Tags) != len(idMap.CanonicalTags) {
		t.Fatalf("parallel arrays disagree: %d tags vs %d canonical", len(idMap.Tags), len(idMap.CanonicalTags))
	}

	unmerged, err := repo.growthHeadlineStats(ctx, repoTenant, []string{repoPark}, start.AddDate(0, 0, -growthLookbackDays), start, end, false, SexScope{}, EmptyAnimalIdentityMap(), "")
	if err != nil {
		t.Fatalf("unmerged headline: %v", err)
	}
	merged, err := repo.growthHeadlineStats(ctx, repoTenant, []string{repoPark}, start.AddDate(0, 0, -growthLookbackDays), start, end, false, SexScope{}, idMap, "")
	if err != nil {
		t.Fatalf("merged headline: %v", err)
	}
	// Keyed raw: primary tag has two weighs (one pair), secondary has one (none). Keyed by animal:
	// three weighs in one timeline, so two consecutive pairs -- and the middle one, on the OTHER
	// RFID, is the comparison that was previously thrown away.
	if unmerged.PairCount != 1 {
		t.Fatalf("unmerged PairCount=%d, want 1", unmerged.PairCount)
	}
	if merged.PairCount != 2 {
		t.Fatalf("merged PairCount=%d, want 2: three weighs of one animal across two RFIDs are two consecutive pairs", merged.PairCount)
	}
	// Mixed statuses all counted, on both sides -- the merge did not silently drop or add a status.
	if unmerged.UnverifiedObservationCount != merged.UnverifiedObservationCount {
		t.Fatalf("pending-observation count changed with the merge (%d -> %d); the map keys rows, it must not filter them",
			unmerged.UnverifiedObservationCount, merged.UnverifiedObservationCount)
	}

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Eligibility.TotalAnimalsWeighed != 1 {
		t.Fatalf("TotalAnimalsWeighed=%d, want 1: two map rows for one animal must not fan it out into two", adg.Eligibility.TotalAnimalsWeighed)
	}

	// PARK SCOPE: the map is resolved per authorized park set. Asking about the OTHER park must
	// resolve an empty map and report nothing from this park's animals.
	otherPark := "00000000-0000-4000-8000-0000000039ff"
	otherMap, err := repo.resolveAnimalIdentityMap(ctx, repoTenant, []string{otherPark}, start.AddDate(0, 0, -growthLookbackDays), end)
	if err != nil {
		t.Fatalf("resolve other-park map: %v", err)
	}
	if !otherMap.Empty() {
		t.Fatalf("a park with no weighing resolved %d map rows; the map must never reach outside the caller's authorized park scope", len(otherMap.Tags))
	}

	// PAGE BOUNDARY: the losing-animals list is LIMIT-bounded and one row per ANIMAL, so a merged
	// animal must occupy ONE slot, never one per tag.
	losing, err := repo.growthLosingAnimals(ctx, repoTenant, []string{repoPark}, start.AddDate(0, 0, -growthLookbackDays), start, end, false, SexScope{}, idMap, "")
	if err != nil {
		t.Fatalf("losing animals: %v", err)
	}
	slots := map[string]int{}
	for _, a := range losing {
		slots[a.ScannedIdentifier]++
	}
	for tag, n := range slots {
		if n > 1 {
			t.Fatalf("%q occupies %d rows of the bounded losing-animals list; it is one row per animal", tag, n)
		}
	}
}
