package postgres

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Adversarial coverage for the Growth Director read. Every test seeds real rows
// and asserts the numbers the screen renders, because each of these is a way
// the queries can be quietly wrong while still returning plausible data.

const (
	// Baseline-seeded rows (migration 000001): tenant, the CBE park and the
	// Gandhi shed already exist in every migrated test database.
	gdTenant   = "00000000-0000-4000-8000-000000000001"
	gdParty    = "00000000-0000-4000-8000-000000001001"
	gdPark     = "00000000-0000-4000-8000-000000003001"
	gdShedG    = "f1b1bad0-47ab-4248-95dc-8fa1472d4fec" // Gandhi 1 Part 1 (baseline)
	gdOperator = "00000000-0000-4000-8000-000000000301"

	// Fixture-created rows.
	gdShedQ      = "11111111-0000-4000-8000-000000000101" // second shed, for cross-shed cohorts
	gdShedLump   = "11111111-0000-4000-8000-000000000102" // lump-sum shed
	gdCampaignW1 = "11111111-0000-4000-8000-000000000201"
	gdCampaignW2 = "11111111-0000-4000-8000-000000000202"
	gdBucketW1G  = "11111111-0000-4000-8000-000000000301"
	gdBucketW2G  = "11111111-0000-4000-8000-000000000302"
	gdBucketW1Q  = "11111111-0000-4000-8000-000000000303"
	gdBucketW2Q  = "11111111-0000-4000-8000-000000000304"
	gdBucketW2L  = "11111111-0000-4000-8000-000000000305"
	gdProof      = "11111111-0000-4000-8000-000000000401"
	gdFeedIssue1 = "11111111-0000-4000-8000-000000000501"
	gdFeedIssue2 = "11111111-0000-4000-8000-000000000502"
	gdFeedIssueX = "11111111-0000-4000-8000-000000000503"
)

func gdWindow() (time.Time, time.Time) {
	// Half-open [Jul 1, Aug 1): wide enough that window membership is never the
	// thing under test.
	return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
}

func execGD(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed sql failed: %v\n%s", err, sql)
	}
}

// seedGrowthDirectorFixture lays the shared stage: two extra sheds, an operator
// scope grant, two week-grain campaigns (W1 completed, W2 published) and their
// individual buckets, plus one completed proof artifact every observation
// reuses.
func seedGrowthDirectorFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, loc := range [][2]string{{gdShedQ, "Q2"}, {gdShedLump, "Lump 1"}} {
		execGD(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', $3, $4::uuid, 'active', 500)
ON CONFLICT (location_id) DO NOTHING`, loc[0], gdTenant, loc[1], gdPark)
	}
	execGD(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, gdTenant, gdOperator, gdPark)
	// Week-grain campaigns: W1 Jul 6-12 (completed), W2 Jul 13-19 (published).
	execGD(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES
  ($1::uuid, $3::uuid, $4::uuid, '2026-07-06', '2026-07-12', '2026-07-06', 'completed', 100, $5::uuid, $5::uuid),
  ($2::uuid, $3::uuid, $4::uuid, '2026-07-13', '2026-07-19', '2026-07-13', 'published', 100, $5::uuid, $5::uuid)`,
		gdCampaignW1, gdCampaignW2, gdTenant, gdPark, gdOperator)
	execGD(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES
  ($1::uuid, $6::uuid, $8::uuid, $9::uuid,  'shed', 'Gandhi 1 Part 1', 'individual_animal', $11::uuid, 0),
  ($2::uuid, $7::uuid, $8::uuid, $9::uuid,  'shed', 'Gandhi 1 Part 1', 'individual_animal', $11::uuid, 0),
  ($3::uuid, $6::uuid, $8::uuid, $10::uuid, 'shed', 'Q2',                'individual_animal', $11::uuid, 0),
  ($4::uuid, $7::uuid, $8::uuid, $10::uuid, 'shed', 'Q2',                'individual_animal', $11::uuid, 0),
  ($5::uuid, $7::uuid, $8::uuid, $12::uuid, 'shed', 'Lump 1',            'per_shed_partition', $11::uuid, 0)`,
		gdBucketW1G, gdBucketW2G, gdBucketW1Q, gdBucketW2Q, gdBucketW2L,
		gdCampaignW1, gdCampaignW2, gdTenant, gdShedG, gdShedQ, gdOperator, gdShedLump)
	execGD(t, ctx, pool, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at)
VALUES ($1::uuid, $2::uuid, 'local', 'growthdirector-test/' || $1, 'video/mp4', 'completed', 'shed', $3::uuid, 'shed', $3::uuid, 'video', $4::uuid, now())`,
		gdProof, gdTenant, gdShedG, gdOperator)
}

// seedScan inserts one accepted individual weigh. submitted_at is stamped
// (finished buckets are the terminal state of real weighing work), which is
// also what makes repeat scans of one tag legal under the one-open-tag index.
func seedScan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, tag string, weightKg float64, at time.Time, verification string) {
	t.Helper()
	campaign := gdCampaignW1
	if bucketID == gdBucketW2G || bucketID == gdBucketW2Q {
		campaign = gdCampaignW2
	}
	seedScanInCampaign(t, ctx, pool, campaign, bucketID, tag, weightKg, at, verification)
}

// seedScanInCampaign is seedScan with an EXPLICIT campaign id, for fixtures
// that seed buckets seedScan's bucketID heuristic does not know about.
func seedScanInCampaign(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaign, bucketID, tag string, weightKg float64, at time.Time, verification string) {
	t.Helper()
	execGD(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at, verification_status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz, $9::timestamptz, $10)`,
		gdTenant, campaign, bucketID, tag, weightKg, gdProof, gdOperator,
		fmt.Sprintf("gd:%s:%s:%d", bucketID, tag, at.UnixNano()), at, verification)
}

// seedGoatWithTag creates a herd animal and its lifetime-unique tag mapping.
// normalized_value is UPPER (the identity module's normalizer), which is what
// the read joins upper(tag_key) against.
func seedGoatWithTag(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, displaySeq, tag, breed, sex string) {
	t.Helper()
	execGD(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, 'kid', 'alive', 'kid', $6::uuid, $7::uuid, $8::uuid, $7::uuid)`,
		goatID, gdTenant, "G-99"+displaySeq, breed, sex, gdParty, gdShedG, gdPark)
	execGD(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, upper(btrim($3)), 'tenant', true, 'active', now(), 'test')`,
		gdTenant, goatID, tag)
}

func day(d int, hour int) time.Time {
	return time.Date(2026, 7, d, hour, 0, 0, 0, time.UTC)
}

// BAND COUNTS, MOVEMENT AND TRUST. The dedupe rule (latest capture of the
// latest round wins), the rework exclusion, the matched/unmatched split and the
// movement pair rule are each a way this widget can lie while looking fine.
func TestGrowthDirectorRoadToSaleBandsMovementAndTrust(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedGrowthDirectorFixture(t, ctx, pool)

	// TAG-A: 14.0 in W1; scanned TWICE in W2 (19.0 then 21.0 — the later
	// capture wins). Latest band 20-25, previous band <15: moved up.
	seedScan(t, ctx, pool, gdBucketW1G, "TAG-A", 14.0, day(8, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-A", 19.0, day(15, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-A", 21.0, day(15, 8), "pending")
	// TAG-B: 16.0 -> 16.4, same band: held.
	seedScan(t, ctx, pool, gdBucketW1G, "TAG-B", 16.0, day(8, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-B", 16.4, day(15, 6), "verified")
	// TAG-C: 25.0 -> 19.0: moved down.
	seedScan(t, ctx, pool, gdBucketW1G, "TAG-C", 25.0, day(8, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-C", 19.0, day(15, 6), "pending")
	// TAG-D: one round only, 36.0: banded, never movement-eligible.
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-D", 36.0, day(15, 6), "pending")
	// TAG-E: rework only. Bounced proof = untrusted weight: absent from every
	// band, present in the trust panel's raw stream.
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-E", 31.0, day(15, 6), "rework")

	// A and B resolve in the herd register; C, D, E do not.
	seedGoatWithTag(t, ctx, pool, "11111111-0000-4000-8000-000000000601", "0001", "TAG-A", "Sojat", "male")
	seedGoatWithTag(t, ctx, pool, "11111111-0000-4000-8000-000000000602", "0002", "TAG-B", "Sojat", "male")

	// Lump-sum: one live, one withdrawn. Only the live row may count.
	execGD(t, ctx, pool, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at, withdrawn_at)
VALUES
  ($1::uuid, $2::uuid, $3::uuid, 500.0, 20.0, 25, $4::uuid, $5::uuid, 'gd:lump:withdrawn', $6::timestamptz, $7::timestamptz),
  ($1::uuid, $2::uuid, $3::uuid, 520.0, 20.8, 25, $4::uuid, $5::uuid, 'gd:lump:live', $7::timestamptz, NULL)`,
		gdTenant, gdCampaignW2, gdBucketW2L, gdProof, gdOperator, day(14, 6), day(15, 6))

	from, to := gdWindow()
	repo := NewRepository(pool, 5*time.Second)
	out, err := repo.GetGrowthDirectorWeights(ctx, gdTenant, []string{gdPark}, from, to)
	if err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}

	road := out.RoadToSale
	if road.TotalIdentities != 4 {
		t.Fatalf("total identities: want 4 (rework-only TAG-E excluded), got %d", road.TotalIdentities)
	}
	if road.MatchedIdentities != 2 || road.UnmatchedIdentities != 2 {
		t.Fatalf("matched/unmatched: want 2/2, got %d/%d", road.MatchedIdentities, road.UnmatchedIdentities)
	}
	wantBands := map[string]int{"<15": 0, "15-20": 2, "20-25": 1, "25-30": 0, "30-35": 0, "35+": 1}
	if len(road.Bands) != 6 {
		t.Fatalf("bands must always list all six, got %d", len(road.Bands))
	}
	for _, band := range road.Bands {
		if band.IdentityCount != wantBands[band.Band] {
			t.Errorf("band %s: want %d, got %d", band.Band, wantBands[band.Band], band.IdentityCount)
		}
	}
	if road.Movement.PairIdentities != 3 {
		t.Fatalf("movement pairs: want 3 (two rounds each; TAG-D has one), got %d", road.Movement.PairIdentities)
	}
	if road.Movement.MovedUp != 1 || road.Movement.Held != 1 || road.Movement.MovedDown != 1 {
		t.Fatalf("movement up/held/down: want 1/1/1, got %d/%d/%d",
			road.Movement.MovedUp, road.Movement.Held, road.Movement.MovedDown)
	}

	trust := out.Trust
	if trust.ScansTotal != 9 || trust.ScansMatched != 5 || trust.ScansUnmatched != 4 {
		t.Fatalf("scans total/matched/unmatched: want 9/5/4, got %d/%d/%d", trust.ScansTotal, trust.ScansMatched, trust.ScansUnmatched)
	}
	if trust.IdentitiesTotal != 5 {
		t.Fatalf("trust identities: want 5 (rework TAG-E IS the raw stream), got %d", trust.IdentitiesTotal)
	}
	if trust.IdentitiesWithPair != 3 || trust.IdentitiesOnceOnly != 2 {
		t.Fatalf("identities with pair/once-only: want 3/2, got %d/%d", trust.IdentitiesWithPair, trust.IdentitiesOnceOnly)
	}
	if trust.ScansRework != 1 {
		t.Fatalf("rework scans: want 1, got %d", trust.ScansRework)
	}
	if trust.ScansPendingVerification != 7 {
		t.Fatalf("pending scans: want 7 (9 minus 1 verified minus 1 rework), got %d", trust.ScansPendingVerification)
	}
	if trust.WholeShedObservations != 1 {
		t.Fatalf("whole-shed observations must count live rows only (withdrawn_at IS NULL): want 1, got %d", trust.WholeShedObservations)
	}

	if len(out.Parks) != 1 || out.Parks[0].ParkID != gdPark {
		t.Fatalf("park vocabulary must carry exactly the scoped park, got %+v", out.Parks)
	}
	if out.Period.Resolution != domain.PeriodResolutionCampaignWeek {
		t.Fatalf("period resolution must be disclosed, got %q", out.Period.Resolution)
	}
}

// PAIR LOGIC ACROSS SHEDS. Fair fight must group by herd-register breed/sex
// only, require >=3 pairs per shed and >=2 sheds per cohort, and keep
// unmatched tags out. Slow growth statuses judge the same pairs against the
// disclosed target.
func TestGrowthDirectorFairFightAndSlowGrowthPairLogic(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedGrowthDirectorFixture(t, ctx, pool)

	// Three Sojat males per shed, each weighed in both weeks, 7 days apart.
	// Gandhi gains 1.4 kg (200 g/day); Q2 gains 0.7 kg (100 g/day).
	for i, tag := range []string{"SJ-G1", "SJ-G2", "SJ-G3"} {
		goatID := fmt.Sprintf("11111111-0000-4000-8000-0000000007%02d", i)
		seedGoatWithTag(t, ctx, pool, goatID, fmt.Sprintf("10%02d", i), tag, "Sojat", "male")
		seedScan(t, ctx, pool, gdBucketW1G, tag, 14.0, day(8, 6), "pending")
		seedScan(t, ctx, pool, gdBucketW2G, tag, 15.4, day(15, 6), "pending")
	}
	for i, tag := range []string{"SJ-Q1", "SJ-Q2", "SJ-Q3"} {
		goatID := fmt.Sprintf("11111111-0000-4000-8000-0000000008%02d", i)
		seedGoatWithTag(t, ctx, pool, goatID, fmt.Sprintf("11%02d", i), tag, "Sojat", "male")
		seedScan(t, ctx, pool, gdBucketW1Q, tag, 14.0, day(8, 6), "pending")
		seedScan(t, ctx, pool, gdBucketW2Q, tag, 14.7, day(15, 6), "pending")
	}
	// An UNMATCHED pair identity: grows fastest of all, but resolves to no goat
	// so it can never enter a breed/sex cohort.
	seedScan(t, ctx, pool, gdBucketW1G, "NO-MATCH", 14.0, day(8, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "NO-MATCH", 18.0, day(15, 6), "pending")

	from, to := gdWindow()
	repo := NewRepository(pool, 5*time.Second)
	out, err := repo.GetGrowthDirectorWeights(ctx, gdTenant, []string{gdPark}, from, to)
	if err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}

	if len(out.FairFight.Cohorts) != 1 {
		t.Fatalf("want exactly one (Sojat, male) cohort, got %+v", out.FairFight.Cohorts)
	}
	cohort := out.FairFight.Cohorts[0]
	if cohort.Breed != "Sojat" || cohort.Sex != "male" {
		t.Fatalf("cohort key must come from the herd register: got (%s, %s)", cohort.Breed, cohort.Sex)
	}
	if len(cohort.Sheds) != 2 {
		t.Fatalf("cohort must field both sheds, got %+v", cohort.Sheds)
	}
	// Strongest first.
	if cohort.Sheds[0].LocationID != gdShedG || math.Abs(cohort.Sheds[0].MedianADGGPerDay-200) > 0.01 {
		t.Fatalf("first shed: want Gandhi at 200 g/day, got %+v", cohort.Sheds[0])
	}
	if cohort.Sheds[1].LocationID != gdShedQ || math.Abs(cohort.Sheds[1].MedianADGGPerDay-100) > 0.01 {
		t.Fatalf("second shed: want Q2 at 100 g/day, got %+v", cohort.Sheds[1])
	}
	if cohort.Sheds[0].PairIdentities != 3 || cohort.Sheds[1].PairIdentities != 3 {
		t.Fatalf("pair identities: want 3/3, got %d/%d", cohort.Sheds[0].PairIdentities, cohort.Sheds[1].PairIdentities)
	}

	groups := out.SlowGrowth.Groups
	if len(groups) != 2 {
		t.Fatalf("slow growth: want the two (shed, Sojat, male) groups, got %+v", groups)
	}
	if out.SlowGrowth.TargetGPerDay != 200 {
		t.Fatalf("target must be disclosed as 200, got %v", out.SlowGrowth.TargetGPerDay)
	}
	// Slowest first: Q2 at 100 g/day is below target; Gandhi at exactly 200 is on track.
	if groups[0].LocationID != gdShedQ || groups[0].Status != domain.SlowGrowthStatusBelowTarget {
		t.Fatalf("first group: want Q2 below_target, got %+v", groups[0])
	}
	if groups[1].LocationID != gdShedG || groups[1].Status != domain.SlowGrowthStatusOnTrack {
		t.Fatalf("second group: want Gandhi on_track, got %+v", groups[1])
	}
	// One week of consecutive pairs = no trend yet.
	if groups[0].WeekOverWeekDeltaG != nil || groups[1].WeekOverWeekDeltaG != nil {
		t.Fatalf("week-over-week needs pairs in two distinct weeks, got %+v / %+v", groups[0].WeekOverWeekDeltaG, groups[1].WeekOverWeekDeltaG)
	}
}

// BLOCKED VS ZERO. A blocked cell (quantity NULL + reason) is a problem; an
// authored zero is an instruction. The two must never collapse, per-head math
// must skip experiment/informational rows, and head counts must not multiply
// across items or sessions.
func TestGrowthDirectorFeedBlockedVsZero(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedGrowthDirectorFixture(t, ctx, pool)

	issue := func(id, feedDay, workflow string) {
		execGD(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow, state, issued_at, generation_input_fingerprint, idempotency_key, request_fingerprint, source_contract, source_contract_version, generated_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $5, 'issued', now(), 'fp:'||$1, 'idem:'||$1, 'fp:'||$1, 'growthdirector-test', '1', 'test')`,
			id, gdTenant, gdPark, feedDay, workflow)
	}
	row := func(issueID string, sessionNo int, item string, quantity any, reasonCode any, headCount int, informational bool, workflow string) {
		execGD(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label, shed_id, shed_label, shed_tag, breed, session_no, head_count, head_count_informational, workflow, feed_item_label, quantity_kg, blocked_reason_code, blocked_reason_detail, session_total_kg, overdue_pending, row_seq, item_seq)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Gandhi 1 Part 1', '', '', $5, $6, $7, $8, $9, $10, $11, $11, 0, false, 0, $5)`,
			gdTenant, issueID, gdPark, gdShedG, sessionNo, headCount, informational, workflow, item, quantity, reasonCode)
	}

	// Day 1 (normal sheet): one blocked cell, one authored zero, one real ration.
	issue(gdFeedIssue1, "2026-07-15", "normal")
	row(gdFeedIssue1, 1, "Mineral Mix", nil, "no_ration_rate", 10, false, "normal")
	row(gdFeedIssue1, 1, "Milk", 0.0, nil, 10, false, "normal")
	row(gdFeedIssue1, 2, "Maize Crush", 3.5, nil, 10, false, "normal")
	// Day 1 experiment sheet: absolute kg, informational head count. Its kg
	// must never enter per-head math; its presence flags the shed.
	issue(gdFeedIssueX, "2026-07-15", "experiment")
	row(gdFeedIssueX, 1, "Trial Ration", 50.0, nil, 99, true, "experiment")
	// Day 2 (the latest day): the same Mineral Mix cell is still blocked.
	issue(gdFeedIssue2, "2026-07-16", "normal")
	row(gdFeedIssue2, 1, "Mineral Mix", nil, "no_ration_rate", 10, false, "normal")

	// GRAIN PROOF (the STG bug this widget shipped with): TAG-F is WEIGHED in
	// the Q bucket — a different location from the fed shed, exactly like the
	// synthetic per-partition weighing locations on STG — but its herd-register
	// home (goats.shed_id) is the fed shed. Its gain must land on the fed
	// shed's row: joining the weighing bucket location to feed_direction rows
	// would find nothing.
	seedScan(t, ctx, pool, gdBucketW1Q, "TAG-F", 14.0, day(8, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2Q, "TAG-F", 21.0, day(15, 6), "pending")
	seedGoatWithTag(t, ctx, pool, "11111111-0000-4000-8000-000000000606", "0006", "TAG-F", "Sojat", "male")

	from, to := gdWindow()
	repo := NewRepository(pool, 5*time.Second)
	out, err := repo.GetGrowthDirectorWeights(ctx, gdTenant, []string{gdPark}, from, to)
	if err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}

	problems := out.FeedProblems
	if problems.BlockedRowsHistory != 2 || problems.BlockedRowsLatestDay != 1 {
		t.Fatalf("blocked history/latest: want 2/1, got %d/%d", problems.BlockedRowsHistory, problems.BlockedRowsLatestDay)
	}
	if problems.AuthoredZeroHistory != 1 || problems.AuthoredZeroLatestDay != 0 {
		t.Fatalf("authored-zero history/latest: want 1/0 (the zero is on day 1, latest day is day 2), got %d/%d",
			problems.AuthoredZeroHistory, problems.AuthoredZeroLatestDay)
	}
	if len(problems.Items) != 1 {
		t.Fatalf("problem items: the authored zero must NEVER appear as a problem; want 1 item, got %+v", problems.Items)
	}
	item := problems.Items[0]
	if item.FeedItemLabel != "Mineral Mix" || item.BlockedDays != 2 || item.LatestReasonCode != "no_ration_rate" {
		t.Fatalf("problem item: want Mineral Mix blocked 2 days with no_ration_rate, got %+v", item)
	}

	if len(out.FeedVsGrowth.Sheds) != 1 {
		t.Fatalf("feed-vs-growth: want the one fed shed, got %+v", out.FeedVsGrowth.Sheds)
	}
	shed := out.FeedVsGrowth.Sheds[0]
	if !out.FeedVsGrowth.Estimate {
		t.Fatal("feed-vs-growth must always be labelled an estimate")
	}
	if !shed.IsExperiment {
		t.Fatal("a shed carrying an experiment sheet must be flagged")
	}
	// Fed kg = 0 (authored zero stays IN) + 3.5, never the blocked NULL, never
	// the 50 kg experiment total. Head-days = 10 heads x 2 sheet days — the
	// head count must not multiply across items or sessions.
	if shed.FeedGPerHeadPerDay == nil || math.Abs(*shed.FeedGPerHeadPerDay-175) > 0.01 {
		t.Fatalf("feed per head-day: want 175 g (3.5 kg over 20 head-days), got %+v", shed.FeedGPerHeadPerDay)
	}
	// TAG-F was weighed in the Q bucket but lives (and is fed) in the fed shed:
	// its pair must be attributed to the fed shed via goats.shed_id. 14 -> 21 kg
	// over 7 days = 1000 g/day; with a single pair the basis stays whole_shed
	// (average movement), and the ratio is 0.175 kg feed-day per kg gained.
	if shed.PairIdentities != 1 {
		t.Fatalf("grain proof: the Q-bucket pair must re-grain to the fed shed via the herd register; want 1 pair, got %d", shed.PairIdentities)
	}
	if shed.ADGGPerDay == nil || math.Abs(*shed.ADGGPerDay-1000) > 0.01 {
		t.Fatalf("grain proof: want 1000 g/day from the re-grained pair, got %+v", shed.ADGGPerDay)
	}
	if shed.KgFeedPerKgGain == nil || math.Abs(*shed.KgFeedPerKgGain-0.175) > 0.001 {
		t.Fatalf("grain proof: want 0.175 kg feed per kg gained, got %+v", shed.KgFeedPerKgGain)
	}
	if shed.Basis != domain.FeedGrowthBasisWholeShed {
		t.Fatalf("single-pair shed: basis must stay whole_shed, got %s", shed.Basis)
	}
}

// OPERATIONAL LOCATION GRAIN. Two partitions of the SAME physical shed must
// stay as two distinct fair-fight/slow-growth rows with distinct
// operational_keys, never merged by location_id alone. And two parks fielding
// an identically-named shed ("Castro 1" in both) must render with
// park-disambiguated labels, never the bare shed name — see
// docs/decisions/partition-is-operational-shed.md.
func TestGrowthDirectorOperationalLocationGrain(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedGrowthDirectorFixture(t, ctx, pool)

	const (
		gdPark2 = "22222222-0000-4000-8000-000000003002"

		gdShedCastroA = "22222222-0000-4000-8000-000000000111" // "Castro 1" in gdPark
		gdShedCastroB = "22222222-0000-4000-8000-000000000112" // "Castro 1" in gdPark2 -- same name, different park
		gdShedPart    = "22222222-0000-4000-8000-000000000120" // one physical shed, two partitions below

		gdCampaignW1B = "22222222-0000-4000-8000-000000000211"
		gdCampaignW2B = "22222222-0000-4000-8000-000000000212"

		gdBucketW1CA = "22222222-0000-4000-8000-000000000311"
		gdBucketW2CA = "22222222-0000-4000-8000-000000000312"
		gdBucketW1CB = "22222222-0000-4000-8000-000000000313"
		gdBucketW2CB = "22222222-0000-4000-8000-000000000314"

		gdBucketW1P1 = "22222222-0000-4000-8000-000000000321"
		gdBucketW2P1 = "22222222-0000-4000-8000-000000000322"
		gdBucketW1P2 = "22222222-0000-4000-8000-000000000323"
		gdBucketW2P2 = "22222222-0000-4000-8000-000000000324"
	)

	execGD(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'park', 'CPT', NULL, 'active', 501)
ON CONFLICT (location_id) DO NOTHING`, gdPark2, gdTenant)

	for _, loc := range [][3]string{
		{gdShedCastroA, "Castro 1", gdPark},
		{gdShedCastroB, "Castro 1", gdPark2},
		{gdShedPart, "Godel 9", gdPark},
	} {
		execGD(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', $3, $4::uuid, 'active', 502)
ON CONFLICT (location_id) DO NOTHING`, loc[0], gdTenant, loc[1], loc[2])
	}
	execGD(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, gdTenant, gdOperator, gdPark2)
	execGD(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES
  ($1::uuid, $3::uuid, $4::uuid, '2026-07-06', '2026-07-12', '2026-07-06', 'completed', 100, $5::uuid, $5::uuid),
  ($2::uuid, $3::uuid, $4::uuid, '2026-07-13', '2026-07-19', '2026-07-13', 'published', 100, $5::uuid, $5::uuid)`,
		gdCampaignW1B, gdCampaignW2B, gdTenant, gdPark2, gdOperator)

	// Two "Castro 1" buckets, one per park (no partition — the ambiguity is
	// purely the shared shed name across parks). Castro-A rides the ORIGINAL
	// fixture's park-A campaigns; Castro-B rides its own park-B campaigns —
	// each shed's buckets stay inside their own park's campaigns.
	execGD(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES
  ($1::uuid, $5::uuid, $9::uuid,  $11::uuid, 'shed', 'Castro 1', 'individual_animal', $12::uuid, 0),
  ($2::uuid, $6::uuid, $9::uuid,  $11::uuid, 'shed', 'Castro 1', 'individual_animal', $12::uuid, 0),
  ($3::uuid, $7::uuid, $9::uuid,  $10::uuid, 'shed', 'Castro 1', 'individual_animal', $12::uuid, 0),
  ($4::uuid, $8::uuid, $9::uuid,  $10::uuid, 'shed', 'Castro 1', 'individual_animal', $12::uuid, 0)`,
		gdBucketW1CA, gdBucketW2CA, gdBucketW1CB, gdBucketW2CB,
		gdCampaignW1, gdCampaignW2, gdCampaignW1B, gdCampaignW2B,
		gdTenant, gdShedCastroB, gdShedCastroA, gdOperator)

	// One physical shed, two OPERATIONAL locations (partitions), both inside
	// the ORIGINAL fixture's park/campaigns.
	execGD(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, partition_label, weighing_category, operator_user_id, expected_animal_count)
VALUES
  ($1::uuid, $5::uuid, $6::uuid, $7::uuid, 'shed', 'Godel 9', 'Part 1', 'individual_animal', $8::uuid, 0),
  ($2::uuid, $9::uuid, $6::uuid, $7::uuid, 'shed', 'Godel 9', 'Part 1', 'individual_animal', $8::uuid, 0),
  ($3::uuid, $5::uuid, $6::uuid, $7::uuid, 'shed', 'Godel 9', 'Part 2', 'individual_animal', $8::uuid, 0),
  ($4::uuid, $9::uuid, $6::uuid, $7::uuid, 'shed', 'Godel 9', 'Part 2', 'individual_animal', $8::uuid, 0)`,
		gdBucketW1P1, gdBucketW2P1, gdBucketW1P2, gdBucketW2P2,
		gdCampaignW1, gdTenant, gdShedPart, gdOperator, gdCampaignW2)

	seedTrio := func(groupCode int, tagPrefix string, w1Campaign, w1Bucket, w2Campaign, w2Bucket string, w1Kg, w2Kg float64) {
		for i, tag := range []string{tagPrefix + "1", tagPrefix + "2", tagPrefix + "3"} {
			goatID := fmt.Sprintf("22222222-0000-4000-8000-%012d", groupCode*100+i)
			seedGoatWithTag(t, ctx, pool, goatID, fmt.Sprintf("%02d%02d", groupCode, i), tag, "Sojat", "male")
			seedScanInCampaign(t, ctx, pool, w1Campaign, w1Bucket, tag, w1Kg, day(8, 6), "pending")
			seedScanInCampaign(t, ctx, pool, w2Campaign, w2Bucket, tag, w2Kg, day(15, 6), "pending")
		}
	}
	// Castro-A gains 200 g/day, Castro-B gains 100 g/day: a fair fight cohort
	// with two distinctly-parked sheds sharing one name.
	seedTrio(1, "CA-", gdCampaignW1, gdBucketW1CA, gdCampaignW2, gdBucketW2CA, 14.0, 15.4)
	seedTrio(2, "CB-", gdCampaignW1B, gdBucketW1CB, gdCampaignW2B, gdBucketW2CB, 14.0, 14.7)
	// Godel 9 Part 1 gains 200 g/day, Part 2 gains 100 g/day: same physical
	// shed, two operational locations.
	seedTrio(3, "P1-", gdCampaignW1, gdBucketW1P1, gdCampaignW2, gdBucketW2P1, 14.0, 15.4)
	seedTrio(4, "P2-", gdCampaignW1, gdBucketW1P2, gdCampaignW2, gdBucketW2P2, 14.0, 14.7)

	from, to := gdWindow()
	repo := NewRepository(pool, 5*time.Second)
	out, err := repo.GetGrowthDirectorWeights(ctx, gdTenant, []string{gdPark, gdPark2}, from, to)
	if err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}

	// --- Fair fight: find the Sojat/male cohort and assert both Castro-named
	// sheds and both Godel 9 partitions show up as FOUR distinct sheds, keyed
	// by distinct operational_key, with park-prefixed / partition-suffixed
	// labels.
	var cohort *domain.FairFightCohort
	for i := range out.FairFight.Cohorts {
		if out.FairFight.Cohorts[i].Breed == "Sojat" && out.FairFight.Cohorts[i].Sex == "male" {
			cohort = &out.FairFight.Cohorts[i]
		}
	}
	if cohort == nil {
		t.Fatalf("expected a Sojat/male fair-fight cohort, got %+v", out.FairFight.Cohorts)
	}
	if len(cohort.Sheds) != 4 {
		t.Fatalf("want 4 distinct operational-location sheds (2 Castro parks + 2 Godel partitions), got %d: %+v",
			len(cohort.Sheds), cohort.Sheds)
	}
	seenKeys := map[string]bool{}
	seenLabels := map[string]bool{}
	var castroLabels, godelLabels []string
	for _, shed := range cohort.Sheds {
		if seenKeys[shed.OperationalKey] {
			t.Fatalf("duplicate operational_key %q: two operational locations must never merge", shed.OperationalKey)
		}
		seenKeys[shed.OperationalKey] = true
		if seenLabels[shed.ShedDisplayName] {
			t.Fatalf("duplicate shed_display_name %q: distinct operational locations must render distinct labels", shed.ShedDisplayName)
		}
		seenLabels[shed.ShedDisplayName] = true
		if shed.LocationID == gdShedCastroA || shed.LocationID == gdShedCastroB {
			castroLabels = append(castroLabels, shed.ShedDisplayName)
		}
		if shed.LocationID == gdShedPart {
			godelLabels = append(godelLabels, shed.ShedDisplayName)
		}
	}
	if len(castroLabels) != 2 {
		t.Fatalf("want both Castro 1 sheds present, got %v", castroLabels)
	}
	if castroLabels[0] == castroLabels[1] {
		t.Fatalf("two parks fielding the same shed name must render distinct park-prefixed labels, got %v", castroLabels)
	}
	if len(godelLabels) != 2 || godelLabels[0] == godelLabels[1] {
		t.Fatalf("two partitions of one physical shed must render distinct labels, got %v", godelLabels)
	}
	for _, label := range castroLabels {
		if !strings.Contains(label, "Castro 1") {
			t.Fatalf("label must still carry the shed name, got %q", label)
		}
	}

	// --- Slow growth: the same operational-location grain applies. Two Godel
	// 9 partitions must appear as two distinct groups with distinct
	// operational_key, never collapsed into one shed_id-keyed row.
	var godelGroups []domain.SlowGrowthGroup
	for _, g := range out.SlowGrowth.Groups {
		if g.LocationID == gdShedPart {
			godelGroups = append(godelGroups, g)
		}
	}
	if len(godelGroups) != 2 {
		t.Fatalf("want 2 distinct slow-growth groups for the two Godel 9 partitions, got %d: %+v", len(godelGroups), godelGroups)
	}
	if godelGroups[0].OperationalKey == godelGroups[1].OperationalKey {
		t.Fatalf("slow-growth groups for two partitions must carry distinct operational_key, got %+v / %+v", godelGroups[0], godelGroups[1])
	}
}
