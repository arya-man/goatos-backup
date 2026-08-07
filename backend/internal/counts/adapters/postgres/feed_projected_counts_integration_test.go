package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Adversarial coverage for the LIVE-HERD feed projection (ProjectedShedCountsForFeed).
//
// Every test here targets a way this aggregate can report a confidently wrong head count rather
// than fail loudly: double counting an already-executed movement, fanning a multi-impact movement
// out across the live census, dropping an overdue movement, or rendering an impossible negative
// projection as a plausible small positive one. A wrong number here mis-feeds a shed.
//
// The fixtures insert only INPUT facts (tenant, locations, goats, shifting events + impacts).
// Every projected number under assertion is produced by the production query itself.

const (
	feedProjPark  = countsPark
	feedProjShedA = countsShedA
	feedProjShedB = countsShedB
)

// feedProjDay builds a business-day start in the Goat OS calendar, the shape a feed target date
// takes. Tests never build a target date in UTC: that is the bug class the projection guards.
func feedProjDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, biztime.DefaultLocation())
}

// feedProjApproval builds an approval instant inside an India business day.
func feedProjApproval(y int, m time.Month, d, hour int) time.Time {
	return time.Date(y, m, d, hour, 0, 0, 0, biztime.DefaultLocation())
}

func newFeedProjRepo(t *testing.T, ctx context.Context) (*Repository, *pgxpool.Pool) {
	t.Helper()
	pool := setupCountsDB(t, ctx)
	return NewRepository(pool, 10*time.Second), pool
}

// insertFeedProjGoat seeds one live animal at a shed grain.
func insertFeedProjGoat(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	n int,
	breed, sex, stage string,
	shedID string,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status,
  age_band, custodian_party_id, park_id, shed_id, management_stage
) VALUES (
  $1::uuid, $2::uuid, $3, 'goat', $4, $5, 'alive', 'adult',
  '00000000-0000-4000-8000-000000001001'::uuid, $6::uuid, $7::uuid, $8
)`,
		feedProjGoatUUID(n), countsTenant, feedProjDisplayID(n), breed, sex,
		feedProjPark, shedID, stage); err != nil {
		t.Fatalf("seed feed projection goat %d: %v", n, err)
	}
}

func feedProjGoatUUID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-0000000%05d", 70000+n)
}

// feedProjDisplayID satisfies goats_display_id_format_check (^G-[0-9]{6,}$).
func feedProjDisplayID(n int) string { return fmt.Sprintf("G-7%05d", n) }

// feedProjImpact is one cohort line on a movement.
type feedProjImpact struct {
	breedLabel string
	stageTag   string
	sex        string
	headCount  int
}

// insertFeedProjShifting seeds one shifting event plus its impacts as raw input facts.
//
// sourceShed may be empty for an intake movement with no source. eventStatus is passed explicitly
// so a test can seed the 'applied' (already-executed) case that must NOT be counted again.
func insertFeedProjShifting(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	key string,
	priority string,
	approvedAt time.Time,
	sourceShed, destShed string,
	authState, eventStatus string,
	impacts []feedProjImpact,
) string {
	t.Helper()

	var sourcePark, sourceShedArg any
	if sourceShed != "" {
		sourcePark = feedProjPark
		sourceShedArg = sourceShed
	}

	// The applied-shape CHECK requires applied_at exactly when event_status='applied'.
	var appliedAt, completedAt, completedBy any
	if eventStatus == "applied" {
		appliedAt = approvedAt.Add(24 * time.Hour)
	}

	// Migration 000031 requires a non-blank proof_ref for a 'pending_verification' row (the operator's
	// mandatory video). An 'applied' row carries the same proof through approval; other states have none.
	var proofRef any
	if eventStatus == "pending_verification" || eventStatus == "applied" {
		proofRef = key + ":proof"
		completedAt = approvedAt.Add(12 * time.Hour)
		completedBy = countsOperator
	}

	var eventID string
	if err := pool.QueryRow(ctx, `
INSERT INTO shifting_events (
  tenant_id, logical_shifting_event_key, priority, category,
  source_park_id, source_shed_id, destination_park_id, destination_shed_id,
  raised_at, effective_at, authorized_at, authorization_state, event_status, applied_at, proof_ref,
  completed_at, completed_by,
  source_system, source_ref, payload_hash, idempotency_key, request_fingerprint
) VALUES (
  $1::uuid, $2, $3, 'growth',
  $4::uuid, $5::uuid, $6::uuid, $7::uuid,
  $8, $8, $8, $9, $10, $11, $12, $13, $14::uuid,
  'manual_review', $2, $2, $2, $2
)
RETURNING shifting_event_id`,
		countsTenant, key, priority,
		sourcePark, sourceShedArg, feedProjPark, destShed,
		approvedAt, authState, eventStatus, appliedAt, proofRef, completedAt, completedBy,
	).Scan(&eventID); err != nil {
		t.Fatalf("seed shifting event %s: %v", key, err)
	}

	for i, im := range impacts {
		if _, err := pool.Exec(ctx, `
INSERT INTO shifting_event_impacts (
  tenant_id, shifting_event_id, grain_key, breed_key, breed_label,
  stage_tag, sex, head_count
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8
)`,
			countsTenant, eventID,
			fmt.Sprintf("%s:%s:%d", destShed, im.breedLabel, i),
			countAliasNorm(im.breedLabel), im.breedLabel,
			im.stageTag, im.sex, im.headCount); err != nil {
			t.Fatalf("seed shifting impact %s/%d: %v", key, i, err)
		}
	}
	return eventID
}

// findFeedProjRow locates the grain row for a shed, or fails.
func findFeedProjRow(t *testing.T, got domain.FeedProjectedCounts, shedID, breed, stage, sex string) domain.FeedProjectedCountRow {
	t.Helper()
	for _, row := range got.Items {
		if row.ShedID != nil && *row.ShedID == shedID &&
			row.Breed == breed && row.ManagementStage == stage && row.Sex == sex {
			return row
		}
	}
	t.Fatalf("no grain row for shed=%s breed=%s stage=%s sex=%s in %+v", shedID, breed, stage, sex, got.Items)
	return domain.FeedProjectedCountRow{}
}

func feedProjQuery(target time.Time) domain.FeedProjectedCountQuery {
	return domain.FeedProjectedCountQuery{TenantID: countsTenant, TargetDate: target, Limit: 100}
}

// TestFeedProjectionDateShiftTimingRule proves the zero-lead timing rule at the SQL layer (maintainer
// decision 2026-07-27): a movement counts from its AUTHORIZATION day onward regardless of priority,
// and is flagged OVERDUE only once it has been standing open since before the packing day
// (feed day - 1). It also proves the <= comparison: an overdue movement keeps counting instead of
// silently dropping out and de-feeding a shed whose animals are still expected.
func TestFeedProjectionDateShiftTimingRule(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name     string
		priority string
		// offsets from the approval day to the feed day under test.
		targetOffset int
		wantDelta    int64
		wantOverdue  bool
	}{
		// No lead, no priority branch: high and low behave identically. A movement counts on its
		// authorization day and every day after.
		{name: "high counts on the authorization day", priority: "high", targetOffset: 0, wantDelta: 4, wantOverdue: false},
		{name: "low counts on the authorization day", priority: "low", targetOffset: 0, wantDelta: 4, wantOverdue: false},
		// Authorized on the packing day (feed day - 1) is on time, not overdue.
		{name: "high the day after authorization is not overdue", priority: "high", targetOffset: 1, wantDelta: 4, wantOverdue: false},
		{name: "low the day after authorization is not overdue", priority: "low", targetOffset: 1, wantDelta: 4, wantOverdue: false},
		// Authorized before the packing day is overdue but keeps counting (the <= half of the rule).
		{name: "high two days on is overdue and still counts", priority: "high", targetOffset: 2, wantDelta: 4, wantOverdue: true},
		{name: "low two days on is overdue and still counts", priority: "low", targetOffset: 2, wantDelta: 4, wantOverdue: true},
		{name: "an overdue movement still counts five days on", priority: "low", targetOffset: 5, wantDelta: 4, wantOverdue: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := newFeedProjRepo(t, ctx)

			approvedAt := feedProjApproval(2026, time.July, 10, 9)
			approvalDay := feedProjDay(2026, time.July, 10)

			// Six live animals sit in the DESTINATION shed already.
			for i := 0; i < 6; i++ {
				insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
			}
			insertFeedProjShifting(t, ctx, pool, "timing-"+tc.priority, tc.priority, approvedAt,
				feedProjShedA, feedProjShedB, "authorized", "authorized",
				[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 4}})

			target := approvalDay.AddDate(0, 0, tc.targetOffset)
			got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(target))
			if err != nil {
				t.Fatalf("ProjectedShedCountsForFeed: %v", err)
			}

			row := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K1", "female")
			if row.CurrentHeadCount != 6 {
				t.Fatalf("current_head_count=%d, want 6 (the live herd never changes with the target date)", row.CurrentHeadCount)
			}
			if row.PendingDelta != tc.wantDelta {
				t.Errorf("pending_delta=%d, want %d", row.PendingDelta, tc.wantDelta)
			}
			if want := 6 + tc.wantDelta; row.ProjectedHeadCount != want {
				t.Errorf("projected_head_count=%d, want %d", row.ProjectedHeadCount, want)
			}
			if row.OverduePending != tc.wantOverdue {
				t.Errorf("overdue_pending=%t, want %t", row.OverduePending, tc.wantOverdue)
			}
			if tc.wantOverdue && len(row.OverdueShiftingEventIDs) != 1 {
				t.Errorf("overdue_shifting_event_ids=%v, want exactly one id so the UI can link to the late movement", row.OverdueShiftingEventIDs)
			}
			if !tc.wantOverdue && len(row.OverdueShiftingEventIDs) != 0 {
				t.Errorf("overdue_shifting_event_ids=%v, want empty", row.OverdueShiftingEventIDs)
			}
		})
	}
}

// TestFeedProjectionStatusMatrixCountsPendingVerificationExcludesApplied walks the full
// event_status matrix and is the double-count / under-count regression.
//
// An APPLIED shifting has already relocated its animals in goats, so it is ALREADY inside
// current_head_count. Adding its delta again would over-feed the destination shed by exactly the
// size of the movement — a wrong number that looks entirely plausible on screen. Conversely a
// PENDING_VERIFICATION shifting (operator completed with proof, verifier has not approved) has NOT
// relocated its animals yet, so it MUST still contribute or the shed is under-fed until approval.
// This asserts every status bucket resolves to the right side of that line.
func TestFeedProjectionStatusMatrixCountsPendingVerificationExcludesApplied(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name        string
		authState   string
		eventStatus string
		wantDelta   int64
	}{
		{
			name:        "an authorized but unexecuted movement contributes its delta",
			authState:   "authorized",
			eventStatus: "authorized",
			wantDelta:   5,
		},
		{
			// Compatibility shape from the superseded verifier gate: authorization still makes it a
			// feed input until the lazy compatibility apply runs.
			name:        "an authorized legacy pending_verification movement still contributes",
			authState:   "authorized",
			eventStatus: "pending_verification",
			wantDelta:   5,
		},
		{
			// The whole point of this test.
			name:        "an applied movement is already in the live herd and must not be added again",
			authState:   "authorized",
			eventStatus: "applied",
			wantDelta:   0,
		},
		{
			// Authorization is what makes a movement a pending feed input. An unapproved movement may
			// never happen at all, so feeding for it would waste ration on animals that are not coming.
			name:        "a pending unapproved movement does not contribute",
			authState:   "pending",
			eventStatus: "pending",
			wantDelta:   0,
		},
		{
			name:        "a rejected movement does not contribute",
			authState:   "rejected",
			eventStatus: "rejected",
			wantDelta:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := newFeedProjRepo(t, ctx)

			approvedAt := feedProjApproval(2026, time.July, 10, 9)
			// Well after the authorization day, so timing can never be the reason a delta is absent.
			target := feedProjDay(2026, time.July, 20)

			for i := 0; i < 10; i++ {
				insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
			}
			insertFeedProjShifting(t, ctx, pool, "applied-"+tc.eventStatus, "low", approvedAt,
				feedProjShedA, feedProjShedB, tc.authState, tc.eventStatus,
				[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 5}})

			got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(target))
			if err != nil {
				t.Fatalf("ProjectedShedCountsForFeed: %v", err)
			}

			row := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K1", "female")
			if row.CurrentHeadCount != 10 {
				t.Fatalf("current_head_count=%d, want 10", row.CurrentHeadCount)
			}
			if row.PendingDelta != tc.wantDelta {
				t.Errorf("pending_delta=%d, want %d", row.PendingDelta, tc.wantDelta)
			}
			if want := 10 + tc.wantDelta; row.ProjectedHeadCount != want {
				t.Errorf("projected_head_count=%d, want %d", row.ProjectedHeadCount, want)
			}
		})
	}
}

// TestFeedProjectionClampsSourceGrainAtZeroAndSurfacesIt proves a source grain never renders a
// negative projection, and — just as importantly — that the clamp is VISIBLE.
//
// Silently flooring a negative to zero would present an impossible movement (removing more
// animals than the shed holds) as a tidy empty shed, hiding a real data problem. The raw delta is
// still reported so the full movement pressure stays legible.
func TestFeedProjectionClampsSourceGrainAtZeroAndSurfacesIt(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name          string
		liveHead      int
		moveHead      int
		wantDelta     int64
		wantProjected int64
		wantClamped   bool
	}{
		{
			name:     "a movement smaller than the source grain leaves a positive remainder",
			liveHead: 8, moveHead: 3, wantDelta: -3, wantProjected: 5, wantClamped: false,
		},
		{
			// Exactly emptying the shed is legitimate, not a clamp. Flagging it would train
			// operators to ignore the flag.
			name:     "a movement that exactly empties the source grain is zero but not clamped",
			liveHead: 4, moveHead: 4, wantDelta: -4, wantProjected: 0, wantClamped: false,
		},
		{
			name:     "a movement larger than the source grain clamps at zero and is flagged",
			liveHead: 3, moveHead: 7, wantDelta: -7, wantProjected: 0, wantClamped: true,
		},
		{
			// The extreme: a movement out of a grain that holds nobody at all.
			name:     "a movement out of an empty source grain clamps at zero and is flagged",
			liveHead: 0, moveHead: 5, wantDelta: -5, wantProjected: 0, wantClamped: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := newFeedProjRepo(t, ctx)

			approvedAt := feedProjApproval(2026, time.July, 10, 9)
			target := feedProjDay(2026, time.July, 20)

			for i := 0; i < tc.liveHead; i++ {
				insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedA)
			}
			insertFeedProjShifting(t, ctx, pool, "clamp", "low", approvedAt,
				feedProjShedA, feedProjShedB, "authorized", "authorized",
				[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: tc.moveHead}})

			got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(target))
			if err != nil {
				t.Fatalf("ProjectedShedCountsForFeed: %v", err)
			}

			row := findFeedProjRow(t, got, feedProjShedA, "Beetal", "K1", "female")
			if row.CurrentHeadCount != int64(tc.liveHead) {
				t.Fatalf("current_head_count=%d, want %d", row.CurrentHeadCount, tc.liveHead)
			}
			if row.PendingDelta != tc.wantDelta {
				t.Errorf("pending_delta=%d, want %d (the RAW delta must survive the clamp)", row.PendingDelta, tc.wantDelta)
			}
			if row.ProjectedHeadCount != tc.wantProjected {
				t.Errorf("projected_head_count=%d, want %d", row.ProjectedHeadCount, tc.wantProjected)
			}
			if row.ProjectedHeadCount < 0 {
				t.Errorf("projected_head_count=%d is negative — the clamp did not apply", row.ProjectedHeadCount)
			}
			if row.Clamped != tc.wantClamped {
				t.Errorf("clamped=%t, want %t", row.Clamped, tc.wantClamped)
			}
		})
	}
}

// TestFeedProjectionOneToManyMultiImpactMovementDoesNotFanOutLiveCount is the join-cardinality regression.
//
// shifting_event_impacts is 1:N per event. If the movement legs were joined to the live census
// BEFORE being aggregated, a three-impact movement would multiply every live grain row it touched
// by three. The delta CTE pre-aggregates to one row per grain precisely to make that impossible.
func TestFeedProjectionOneToManyMultiImpactMovementDoesNotFanOutLiveCount(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	approvedAt := feedProjApproval(2026, time.July, 10, 9)
	target := feedProjDay(2026, time.July, 20)

	// Nine live animals across three distinct grains in the destination shed.
	for i := 0; i < 3; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
	}
	for i := 3; i < 6; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Malai", "male", "K2", feedProjShedB)
	}
	for i := 6; i < 9; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Sojat", "female", "F2", feedProjShedB)
	}

	// ONE movement carrying THREE impact lines, one per grain.
	insertFeedProjShifting(t, ctx, pool, "fanout", "low", approvedAt,
		feedProjShedA, feedProjShedB, "authorized", "authorized",
		[]feedProjImpact{
			{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 1},
			{breedLabel: "Malai", stageTag: "K2", sex: "male", headCount: 2},
			{breedLabel: "Sojat", stageTag: "F2", sex: "female", headCount: 3},
		})

	got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(target))
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}

	for _, want := range []struct {
		breed, stage, sex string
		current, delta    int64
	}{
		{"Beetal", "K1", "female", 3, 1},
		{"Malai", "K2", "male", 3, 2},
		{"Sojat", "F2", "female", 3, 3},
	} {
		row := findFeedProjRow(t, got, feedProjShedB, want.breed, want.stage, want.sex)
		if row.CurrentHeadCount != want.current {
			t.Errorf("%s/%s current_head_count=%d, want %d — a fan-out on the impacts join would inflate this",
				want.breed, want.stage, row.CurrentHeadCount, want.current)
		}
		if row.PendingDelta != want.delta {
			t.Errorf("%s/%s pending_delta=%d, want %d", want.breed, want.stage, row.PendingDelta, want.delta)
		}
		if row.ProjectedHeadCount != want.current+want.delta {
			t.Errorf("%s/%s projected_head_count=%d, want %d",
				want.breed, want.stage, row.ProjectedHeadCount, want.current+want.delta)
		}
	}
}

// TestFeedProjectionIncomingGrainWithNoLiveAnimalsStillAppears proves the FULL OUTER JOIN.
//
// A shed that holds none of an incoming grain today has no live census row at all. Under a LEFT
// JOIN from the live herd those animals would vanish from the projection entirely — and that is
// exactly the shed the feed team most needs to see, because it has no ration prepared for them.
func TestFeedProjectionIncomingGrainWithNoLiveAnimalsStillAppears(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	approvedAt := feedProjApproval(2026, time.July, 10, 9)
	target := feedProjDay(2026, time.July, 20)

	// The destination shed holds a DIFFERENT grain (Beetal/female vs the incoming Malai/male), so it
	// exists in the census but not for Malai. Its residents are K2 -- the SAME stage the incoming
	// animals adopt on arrival -- so this test stays focused on the FULL OUTER JOIN and the incoming
	// grain's stage is not what is under test here (cohort adoption has its own test,
	// TestFeedProjectionDestinationLegAdoptsDestinationCohort).
	for i := 0; i < 2; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K2", feedProjShedB)
	}
	insertFeedProjShifting(t, ctx, pool, "incoming", "low", approvedAt,
		feedProjShedA, feedProjShedB, "authorized", "authorized",
		[]feedProjImpact{{breedLabel: "Malai", stageTag: "K2", sex: "male", headCount: 6}})

	got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(target))
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}

	row := findFeedProjRow(t, got, feedProjShedB, "Malai", "K2", "male")
	if row.CurrentHeadCount != 0 {
		t.Errorf("current_head_count=%d, want 0", row.CurrentHeadCount)
	}
	// A delta-only row's partition label can ONLY come from the delta side: there is no live row to
	// COALESCE from. Before the delta CTE selected partition_label_raw this whole query failed with
	// "column d.partition_label_raw does not exist", so this asserts the column exists and resolves
	// (empty here because this fixture's movement carries no destination partition).
	if row.PartitionLabel != "" {
		t.Errorf("partition_label=%q, want empty for an unpartitioned destination", row.PartitionLabel)
	}
	if row.PendingDelta != 6 || row.ProjectedHeadCount != 6 {
		t.Errorf("pending_delta=%d projected=%d, want 6/6", row.PendingDelta, row.ProjectedHeadCount)
	}
	if row.Clamped {
		t.Errorf("clamped=true on a purely incoming grain")
	}
	if row.ShedLabel == "" {
		t.Errorf("shed_label is empty — the locations join did not resolve for a delta-only row")
	}
}

// TestFeedProjectionMatchesGrainAcrossLabelFormatting proves the normalized grain join.
//
// The live herd carries free-text goats.breed while a movement carries an already-normalized
// breed_key alongside its label. Raw equality would fail to match "Beetal" to "beetal" — and a
// failed match on a FULL OUTER JOIN does not error, it SPLITS one grain into two rows: an
// unchanged current count beside a free-floating delta. Nothing about that looks wrong on screen.
func TestFeedProjectionMatchesGrainAcrossLabelFormatting(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	approvedAt := feedProjApproval(2026, time.July, 10, 9)
	target := feedProjDay(2026, time.July, 20)

	for i := 0; i < 5; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
	}
	// Same grain, differently cased/spaced on the movement side.
	insertFeedProjShifting(t, ctx, pool, "casing", "low", approvedAt,
		feedProjShedA, feedProjShedB, "authorized", "authorized",
		[]feedProjImpact{{breedLabel: "  beetal  ", stageTag: "k1", sex: "FEMALE", headCount: 2}})

	got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(target))
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}

	// The grain must be ONE row, not two.
	var shedBRows int
	for _, row := range got.Items {
		if row.ShedID != nil && *row.ShedID == feedProjShedB {
			shedBRows++
		}
	}
	if shedBRows != 1 {
		t.Fatalf("destination shed produced %d rows, want 1 — the grain join split on label formatting: %+v", shedBRows, got.Items)
	}

	row := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K1", "female")
	if row.CurrentHeadCount != 5 || row.PendingDelta != 2 || row.ProjectedHeadCount != 7 {
		t.Errorf("current=%d delta=%d projected=%d, want 5/2/7",
			row.CurrentHeadCount, row.PendingDelta, row.ProjectedHeadCount)
	}
}

// TestFeedProjectionApprovalDateUsesIndiaBusinessCalendar proves the timezone half of the timing
// rule at the SQL layer.
//
// An approval stamped 20:00 UTC is already the NEXT day in India. With the zero-lead rule the
// movement is feed-effective on its India authorization day; deriving that day in UTC would put it
// one day early and feed the destination shed a day too soon.
func TestFeedProjectionApprovalDateUsesIndiaBusinessCalendar(t *testing.T) {
	ctx := context.Background()

	// 2026-07-10T20:00:00Z == 2026-07-11T01:30 IST, so the authorization business day is the 11th.
	// The movement is feed-effective from the 11th in the India calendar, and NOT from the 10th.
	approvedAt := time.Date(2026, time.July, 10, 20, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		target    time.Time
		wantDelta int64
	}{
		{
			name:      "the UTC-derived authorization day does not count",
			target:    feedProjDay(2026, time.July, 10),
			wantDelta: 0,
		},
		{
			name:      "the India-derived authorization day counts",
			target:    feedProjDay(2026, time.July, 11),
			wantDelta: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := newFeedProjRepo(t, ctx)

			for i := 0; i < 2; i++ {
				insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
			}
			insertFeedProjShifting(t, ctx, pool, "tz", "high", approvedAt,
				feedProjShedA, feedProjShedB, "authorized", "authorized",
				[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 3}})

			got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(tc.target))
			if err != nil {
				t.Fatalf("ProjectedShedCountsForFeed: %v", err)
			}
			row := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K1", "female")
			if row.PendingDelta != tc.wantDelta {
				t.Errorf("pending_delta=%d, want %d", row.PendingDelta, tc.wantDelta)
			}
		})
	}
}

// TestFeedProjectionShedFilterScopesBothLiveAndDeltaSides guards the scope matrix.
//
// The park/shed filter must reach the movement legs as well as the live census. If it only
// narrowed the live side, filtering to one shed would still show deltas from movements into other
// sheds — a count that disagrees with the filter the operator selected.
func TestFeedProjectionShedFilterScopesBothLiveAndDeltaSides(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	approvedAt := feedProjApproval(2026, time.July, 10, 9)
	target := feedProjDay(2026, time.July, 20)

	for i := 0; i < 4; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedA)
	}
	for i := 4; i < 9; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
	}
	insertFeedProjShifting(t, ctx, pool, "scope", "low", approvedAt,
		feedProjShedA, feedProjShedB, "authorized", "authorized",
		[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 2}})

	shedB := feedProjShedB
	q := feedProjQuery(target)
	q.ShedID = &shedB
	got, err := repo.ProjectedShedCountsForFeed(ctx, q)
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}

	for _, row := range got.Items {
		if row.ShedID == nil || *row.ShedID != feedProjShedB {
			t.Fatalf("row outside the requested shed leaked through: %+v", row)
		}
	}
	if got.TotalRows != 1 {
		t.Fatalf("total_rows=%d, want 1", got.TotalRows)
	}
	row := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K1", "female")
	if row.CurrentHeadCount != 5 || row.PendingDelta != 2 || row.ProjectedHeadCount != 7 {
		t.Errorf("current=%d delta=%d projected=%d, want 5/2/7",
			row.CurrentHeadCount, row.PendingDelta, row.ProjectedHeadCount)
	}
}

// TestFeedProjectionBreedFilterScopesBothLiveAndDeltaSides proves the breed filter added to close
// the P1 gap: before this fix ProjectedShedCountsForFeed() filtered park + shed only, so
// requesting breed=Beetal silently returned every breed (Boer included). The filter must apply on
// the live side AND the pending/delta side, using the SAME normalization the grouping key
// already applies (feedGrainNormSQL / countAliasNorm), so a raw label and its normalized twin
// still match the same grain.
func TestFeedProjectionBreedFilterScopesBothLiveAndDeltaSides(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	approvedAt := feedProjApproval(2026, time.July, 10, 9)
	target := feedProjDay(2026, time.July, 20)

	// Two breeds standing in the SAME shed, so a park/shed-only filter cannot
	// distinguish them -- only a breed predicate can.
	for i := 0; i < 3; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedA)
	}
	for i := 3; i < 8; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Boer", "female", "K1", feedProjShedA)
	}
	// A pending movement for EACH breed into the same shed, so the delta side
	// must also be breed-scoped, not just the live side.
	insertFeedProjShifting(t, ctx, pool, "breed-beetal", "low", approvedAt,
		feedProjShedB, feedProjShedA, "authorized", "authorized",
		[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 2}})
	insertFeedProjShifting(t, ctx, pool, "breed-boer", "low", approvedAt,
		feedProjShedB, feedProjShedA, "authorized", "authorized",
		[]feedProjImpact{{breedLabel: "Boer", stageTag: "K1", sex: "female", headCount: 4}})

	beetal := "Beetal"
	q := feedProjQuery(target)
	q.Breed = &beetal
	got, err := repo.ProjectedShedCountsForFeed(ctx, q)
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}

	for _, row := range got.Items {
		if row.Breed != "Beetal" {
			t.Fatalf("breed filter leaked a non-Beetal row through: %+v", row)
		}
	}
	// Two Beetal grains survive: shed A's destination leg (live 3 + pending +2)
	// and shed B's source leg (the movement's Beetal decrement, clamped). BOTH
	// are Beetal -- the breed predicate held on the live side AND the delta side.
	// No Boer grain appears on either leg (asserted by the leak loop above),
	// which is the actual point of this test.
	if got.TotalRows != 2 {
		t.Fatalf("total_rows=%d, want 2 (both Beetal legs; no Boer grain on either side)", got.TotalRows)
	}
	row := findFeedProjRow(t, got, feedProjShedA, "Beetal", "K1", "female")
	if row.CurrentHeadCount != 3 || row.PendingDelta != 2 || row.ProjectedHeadCount != 5 {
		t.Errorf("current=%d delta=%d projected=%d, want 3/2/5",
			row.CurrentHeadCount, row.PendingDelta, row.ProjectedHeadCount)
	}
}

// TestFeedProjectionDestinationLegAdoptsDestinationCohort is the cross-profile projection
// regression (PR #12 review, 2026-07-20).
//
// The pending projection used ONE shared stage_tag for both legs of a movement, so an
// approved-but-unexecuted K1 -> K2 move projected +N of the SOURCE cohort (K1) into the DESTINATION
// (K2) shed. Feed Direction then fed those arriving animals the WRONG ration -- K1 food in the K2
// shed -- before the goat rows were ever updated. The two legs are distinct: the source shed loses
// N under the source tag, and the destination shed gains N under the tag its own residents carry
// (the tag the animals ADOPT on arrival, matching identity.resolveDestinationTag at completion).
func TestFeedProjectionDestinationLegAdoptsDestinationCohort(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	approvedAt := feedProjApproval(2026, time.July, 10, 9)
	target := feedProjDay(2026, time.July, 20) // well after authorization, so timing is never the reason.

	// Source shed A is a homogeneous K1 shed; destination shed B is a homogeneous K2 shed.
	for i := 0; i < 8; i++ {
		insertFeedProjGoat(t, ctx, pool, 10+i, "Beetal", "female", "K1", feedProjShedA)
	}
	for i := 0; i < 5; i++ {
		insertFeedProjGoat(t, ctx, pool, 30+i, "Beetal", "female", "K2", feedProjShedB)
	}

	// Move 4 K1 animals A -> B. The impact carries the SOURCE stage (K1), exactly as it is recorded.
	insertFeedProjShifting(t, ctx, pool, "cross-profile-k1-to-k2", "low", approvedAt,
		feedProjShedA, feedProjShedB, "authorized", "authorized",
		[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 4}})

	got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(target))
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}

	// Source shed A, K1: loses the 4 animals under the source tag.
	src := findFeedProjRow(t, got, feedProjShedA, "Beetal", "K1", "female")
	if src.CurrentHeadCount != 8 || src.PendingDelta != -4 || src.ProjectedHeadCount != 4 {
		t.Errorf("source K1: current=%d delta=%d projected=%d, want 8/-4/4",
			src.CurrentHeadCount, src.PendingDelta, src.ProjectedHeadCount)
	}

	// Destination shed B, K2: gains the 4 animals under the DESTINATION cohort (K2), not K1.
	dst := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K2", "female")
	if dst.CurrentHeadCount != 5 || dst.PendingDelta != 4 || dst.ProjectedHeadCount != 9 {
		t.Errorf("destination K2: current=%d delta=%d projected=%d, want 5/4/9",
			dst.CurrentHeadCount, dst.PendingDelta, dst.ProjectedHeadCount)
	}

	// The bug: NO K1 grain may appear in the destination (K2) shed. Before the fix this was a
	// phantom +4 K1 row in shed B that fed K1 ration to K2 animals.
	for _, row := range got.Items {
		if row.ShedID != nil && *row.ShedID == feedProjShedB &&
			row.ManagementStage == "K1" && row.PendingDelta != 0 {
			t.Fatalf("destination shed B has a K1 grain with delta %d; the destination leg leaked the SOURCE tag: %+v",
				row.PendingDelta, row)
		}
	}
}

// TestFeedProjectionPageBoundaryTotalRowsInvariantToPaging is the pagination adversarial
// regression: a projected count total must never move with the page window.
//
// TotalRows is a window function over the whole combined grain set, so it must report the same
// full count on every page regardless of Limit/Offset, and a caller draining the projection under
// StableOrder must see each grain exactly once — never dropped at a page boundary, never duplicated
// across two pages. This is the read-model twin of the "totals independent of UI page size" rule.
func TestFeedProjectionPageBoundaryTotalRowsInvariantToPaging(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	// Five distinct grains in one shed (one live animal per breed), so the projection returns five
	// grain rows that a small page size must split.
	breeds := []string{"Beetal", "Sirohi", "Osmanabadi", "Jamunapari", "Barbari"}
	for i, breed := range breeds {
		insertFeedProjGoat(t, ctx, pool, i, breed, "female", "K1", feedProjShedB)
	}

	target := feedProjDay(2026, time.July, 20)

	// Walk the projection two rows at a time under the consistent-snapshot order.
	seen := map[string]bool{}
	const pageSize = 2
	for offset := int32(0); ; offset += pageSize {
		got, err := repo.ProjectedShedCountsForFeed(ctx, domain.FeedProjectedCountQuery{
			TenantID:    countsTenant,
			TargetDate:  target,
			Limit:       pageSize,
			Offset:      offset,
			StableOrder: true,
		})
		if err != nil {
			t.Fatalf("ProjectedShedCountsForFeed offset=%d: %v", offset, err)
		}
		// partition_key is part of the GROUP BY, so it participates in the grain the window function
		// counts. A page must therefore carry the label for every row it returns -- a page whose rows
		// lost it would mean the delta/live COALESCE resolved differently across the page boundary.
		for _, row := range got.Items {
			if row.ShedID == nil {
				t.Errorf("offset=%d returned a row with no shed", offset)
			}
		}
		// TotalRows is the FULL grain count on every page, never the page's own length.
		if got.TotalRows != int64(len(breeds)) {
			t.Fatalf("offset=%d total_rows=%d, want %d (a window-function total must not move with the page)",
				offset, got.TotalRows, len(breeds))
		}
		if len(got.Items) > pageSize {
			t.Fatalf("offset=%d returned %d rows, want at most page size %d", offset, len(got.Items), pageSize)
		}
		for _, row := range got.Items {
			key := row.Breed
			if seen[key] {
				t.Fatalf("grain breed=%q appeared on a second page — a page boundary duplicated it", key)
			}
			seen[key] = true
		}
		if len(got.Items) < pageSize {
			break
		}
	}

	if len(seen) != len(breeds) {
		t.Fatalf("drained %d distinct grains across pages, want %d — a page boundary dropped one", len(seen), len(breeds))
	}
}

// TestFeedProjectionParkScopeCarriesPartitionLabelFromBothSides is the adversarial scope test for
// the partition label, and the regression for the defect that 500'd every feed sheet.
//
// `combined` selects COALESCE(lv.partition_label_raw, d.partition_label_raw, NULL). The LIVE side
// always produced that alias; the DELTA side did not, so the whole query failed with
// "column d.partition_label_raw does not exist (SQLSTATE 42703)" -- and because feed's
// ProjectedGrainsForSheds is READ 3 inside generate(), every in-horizon feed preview returned 500.
//
// Both sides are exercised under one park scope: a live partitioned grain (label from lv) and a
// delta-only incoming grain into a partitioned destination holding none of that grain (label from
// d, the side that was missing). A park-scoped read must return each with its OWN partition, not
// one shed's label smeared across both.
func TestFeedProjectionParkScopeCarriesPartitionLabelFromBothSides(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	approvedAt := feedProjApproval(2026, time.July, 10, 9)
	target := feedProjDay(2026, time.July, 20)

	// LIVE side: two Beetal females in shed A, pinned to partition "Part 1".
	for i := 0; i < 2; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K2", feedProjShedA)
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, source_shed_name, partition_label)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Shed A - Part 1', 'Part 1')`,
			countsTenant, feedProjGoatUUID(i), feedProjShedA); err != nil {
			t.Fatalf("seed partition row %d: %v", i, err)
		}
	}

	// DELTA side: a movement into shed B, which holds NO animals of this grain at all, landing in
	// partition "Part 3". Its label has no live row to come from.
	insertFeedProjShifting(t, ctx, pool, "into-part-3", "low", approvedAt,
		feedProjShedA, feedProjShedB, "authorized", "authorized",
		[]feedProjImpact{{breedLabel: "Malai", stageTag: "K2", sex: "male", headCount: 6}})
	if _, err := pool.Exec(ctx, `
UPDATE shifting_events SET destination_partition_label = 'Part 3'
WHERE tenant_id = $1::uuid AND logical_shifting_event_key = 'into-part-3'`, countsTenant); err != nil {
		t.Fatalf("set destination partition: %v", err)
	}

	got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(target))
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}

	live := findFeedProjRow(t, got, feedProjShedA, "Beetal", "K2", "female")
	if live.PartitionLabel != "Part 1" {
		t.Errorf("live partition_label=%q, want \"Part 1\"", live.PartitionLabel)
	}

	incoming := findFeedProjRow(t, got, feedProjShedB, "Malai", "K2", "male")
	if incoming.CurrentHeadCount != 0 || incoming.ProjectedHeadCount != 6 {
		t.Errorf("incoming current=%d projected=%d, want 0/6",
			incoming.CurrentHeadCount, incoming.ProjectedHeadCount)
	}
	// THE REGRESSION. This label exists only because the delta CTE now aggregates
	// min(l.partition_label_raw); without it the query does not run at all.
	if incoming.PartitionLabel != "Part 3" {
		t.Errorf("delta-only partition_label=%q, want \"Part 3\" -- the delta side must carry its own label",
			incoming.PartitionLabel)
	}

	// Every returned row must stay inside the park the query scoped to.
	for _, row := range got.Items {
		if row.ParkID != nil && *row.ParkID != feedProjPark {
			t.Errorf("row escaped the park scope: park_id=%v", *row.ParkID)
		}
	}
}

// TestFeedProjectionOneToManyPartitionsOfOneShedStayDistinctGrains is the cardinality adversarial
// test for partition entering the GROUP BY.
//
// Partition is now part of the grain key, which cuts both ways. Too coarse and two pens of the same
// breed/stage/sex collapse into one row carrying their SUM, so a per-pen feed quantity is computed
// off the whole shed's head count. Too fine and one pen's animals split across rows and the shed is
// fed several times over. Same breed, same stage, same sex, two partitions: exactly two rows,
// carrying their own counts, and summing to the shed.
func TestFeedProjectionOneToManyPartitionsOfOneShedStayDistinctGrains(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)
	target := feedProjDay(2026, time.July, 20)

	// 3 animals in Part 1, 2 in Part 2 -- identical on every other grain dimension.
	partitions := []struct {
		label string
		head  int
	}{{"Part 1", 3}, {"Part 2", 2}}
	n := 0
	for _, part := range partitions {
		for i := 0; i < part.head; i++ {
			insertFeedProjGoat(t, ctx, pool, n, "Beetal", "female", "K2", feedProjShedA)
			if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, source_shed_name, partition_label)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5)`,
				countsTenant, feedProjGoatUUID(n), feedProjShedA, "Shed A - "+part.label, part.label); err != nil {
				t.Fatalf("seed partition row %d: %v", n, err)
			}
			n++
		}
	}

	got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(target))
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}

	byPartition := map[string]int64{}
	rowsForGrain := 0
	for _, row := range got.Items {
		if row.ShedID == nil || *row.ShedID != feedProjShedA || row.Breed != "Beetal" {
			continue
		}
		rowsForGrain++
		byPartition[row.PartitionLabel] += row.CurrentHeadCount
	}
	if rowsForGrain != 2 {
		t.Fatalf("rows for the Beetal/K2/female grain = %d, want exactly 2 (one per partition): %v",
			rowsForGrain, byPartition)
	}
	if byPartition["Part 1"] != 3 || byPartition["Part 2"] != 2 {
		t.Errorf("per-partition head counts = %v, want Part 1=3 Part 2=2", byPartition)
	}
	var total int64
	for _, head := range byPartition {
		total += head
	}
	if total != 5 {
		t.Errorf("partition rows sum to %d, want 5 -- they must reconcile to the shed", total)
	}
}

// TestFeedProjectionMultiPagePartitionGrainsAreNeitherDroppedNorDuplicated is the pagination
// adversarial test for the same change: partitions MULTIPLY the grain count, so a shed that used to
// be one row can now be several and a drain that pages must still see each exactly once.
func TestFeedProjectionMultiPagePartitionGrainsAreNeitherDroppedNorDuplicated(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)
	target := feedProjDay(2026, time.July, 20)

	labels := []string{"Part 1", "Part 2", "Part 3", "Part 4"}
	for i, label := range labels {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K2", feedProjShedA)
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, source_shed_name, partition_label)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5)`,
			countsTenant, feedProjGoatUUID(i), feedProjShedA, "Shed A - "+label, label); err != nil {
			t.Fatalf("seed partition row %d: %v", i, err)
		}
	}

	seen := map[string]int{}
	var reportedTotal int64
	for offset := int32(0); offset < int32(len(labels)); offset += 2 {
		query := feedProjQuery(target)
		query.Limit = 2
		query.Offset = offset
		query.StableOrder = true
		got, err := repo.ProjectedShedCountsForFeed(ctx, query)
		if err != nil {
			t.Fatalf("offset=%d: %v", offset, err)
		}
		if reportedTotal == 0 {
			reportedTotal = got.TotalRows
		}
		// The window count must not move with the page.
		if got.TotalRows != reportedTotal {
			t.Errorf("offset=%d total_rows=%d, want %d on every page", offset, got.TotalRows, reportedTotal)
		}
		for _, row := range got.Items {
			seen[row.PartitionLabel]++
		}
	}
	for _, label := range labels {
		if seen[label] != 1 {
			t.Errorf("partition %q seen %d time(s) across pages, want exactly 1: %v", label, seen[label], seen)
		}
	}
}
