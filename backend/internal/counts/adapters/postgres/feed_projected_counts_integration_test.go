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
			// SUPERSEDED 2026-08-10. This case used to assert 0: authorization was what made a movement
			// a feed input, on the reasoning that an unapproved movement may never happen and feeding
			// for it wastes ration.
			//
			// The farm proved the opposite risk is worse. A low-priority movement raised at 09:00 is
			// due TOMORROW, and tomorrow's normal sheet was issued at 07:00 that same morning and is
			// already being packed — so waiting for approval meant ten animals arriving in a pen packed
			// for one had NO FEED AT ALL. Over-packing for a movement that is later turned down costs a
			// bag; under-feeding animals that really arrive costs the animals. Only a REJECTION now
			// stops the feed clock (the next case), never the mere absence of an approval.
			//
			// This target date is 10 days after the raise, so the ACTIONS lead time is long past and
			// the delta is present for the reason under test rather than by timing accident; the lead
			// itself is pinned by TestFeedProjectionScheduledDateRaisedMovementUsesTheActionsLeadTime.
			name:        "a raised movement contributes before a park head approves it",
			authState:   "pending",
			eventStatus: "pending",
			wantDelta:   5,
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

// TestFeedProjectionParkScopeIgnoresStalePartitionLabels proves the exact-shed cutover rule for
// feed: old goat_shed_partitions/shifting partition fields may help resolve an old parent request,
// but once the shed id is exact they must not be returned as another live display/grain axis.
func TestFeedProjectionParkScopeIgnoresStalePartitionLabels(t *testing.T) {
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
	if live.PartitionLabel != "" {
		t.Errorf("live partition_label=%q, want empty exact-shed label", live.PartitionLabel)
	}

	incoming := findFeedProjRow(t, got, feedProjShedB, "Malai", "K2", "male")
	if incoming.CurrentHeadCount != 0 || incoming.ProjectedHeadCount != 6 {
		t.Errorf("incoming current=%d projected=%d, want 0/6",
			incoming.CurrentHeadCount, incoming.ProjectedHeadCount)
	}
	if incoming.PartitionLabel != "" {
		t.Errorf("delta-only partition_label=%q, want empty exact-shed label", incoming.PartitionLabel)
	}

	// Every returned row must stay inside the park the query scoped to.
	for _, row := range got.Items {
		if row.ParkID != nil && *row.ParkID != feedProjPark {
			t.Errorf("row escaped the park scope: park_id=%v", *row.ParkID)
		}
	}
}

// TestFeedProjectionOneToManyLegacyPartitionsCollapseToExactShedGrain is the cardinality
// adversarial test for the new exact-shed rule. Same breed/stage/sex with old partition rows must
// be one shed grain, not one row per stale label.
func TestFeedProjectionOneToManyLegacyPartitionsCollapseToExactShedGrain(t *testing.T) {
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

	rowsForGrain := 0
	var headCount int64
	for _, row := range got.Items {
		if row.ShedID == nil || *row.ShedID != feedProjShedA || row.Breed != "Beetal" {
			continue
		}
		rowsForGrain++
		if row.PartitionLabel != "" {
			t.Fatalf("partition_label=%q, want empty exact-shed label", row.PartitionLabel)
		}
		headCount += row.CurrentHeadCount
	}
	if rowsForGrain != 1 {
		t.Fatalf("rows for the Beetal/K2/female grain = %d, want exactly 1 exact-shed row", rowsForGrain)
	}
	if headCount != 5 {
		t.Errorf("exact-shed head count = %d, want 5", headCount)
	}
}

// TestFeedProjectionMultiPageLegacyPartitionRowsDoNotDuplicateExactShed is the pagination
// adversarial test for exact shed grain: stale partition labels must collapse before pagination.
func TestFeedProjectionMultiPageLegacyPartitionRowsDoNotDuplicateExactShed(t *testing.T) {
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

	got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(target))
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}
	row := findFeedProjRow(t, got, feedProjShedA, "Beetal", "K2", "female")
	if row.PartitionLabel != "" {
		t.Fatalf("partition_label=%q, want empty exact-shed label", row.PartitionLabel)
	}
	if row.CurrentHeadCount != int64(len(labels)) {
		t.Fatalf("current_head_count=%d, want %d", row.CurrentHeadCount, len(labels))
	}
}

// ---------------------------------------------------------------------------
// The RAISED-but-unapproved branch (maintainer decision 2026-08-10)
// ---------------------------------------------------------------------------
//
// A low-priority movement raised at 09:00 is due tomorrow, but tomorrow's normal sheet was issued at
// 07:00 that same morning and is already being packed. Waiting for a park head's approval meant the
// destination pen was packed for the head count it had at breakfast and the arriving animals had no
// feed. These prove the second pending_event branch at the SQL layer.

// insertFeedProjRaised seeds a RAISED, NOT-YET-APPROVED movement.
//
// authorized_at is left NULL deliberately. The shared helper above stamps raised_at, effective_at
// and authorized_at from one instant, which is honest for an approved row and a lie for this one: a
// movement nobody has approved has no approval instant, and a fixture carrying one would let the
// query pass by reading the wrong column.
func insertFeedProjRaised(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	key string,
	priority string,
	raisedAt time.Time,
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

	// shifting_events_canceled_shape_check: a canceled row must carry its cancellation stamps, and a
	// NON-canceled row must carry none. Both halves are enforced, so these cannot simply be set
	// unconditionally.
	var canceledAt, canceledBy, cancelReason any
	if eventStatus == "canceled" {
		canceledAt = raisedAt.Add(time.Hour)
		canceledBy = countsOperator
		cancelReason = "raised in error"
	}

	var eventID string
	if err := pool.QueryRow(ctx, `
INSERT INTO shifting_events (
  tenant_id, logical_shifting_event_key, priority, category,
  source_park_id, source_shed_id, destination_park_id, destination_shed_id,
  raised_at, effective_at, authorized_at, authorization_state, event_status,
  canceled_at, canceled_by, cancel_reason,
  source_system, source_ref, payload_hash, idempotency_key, request_fingerprint
) VALUES (
  $1::uuid, $2, $3, 'growth',
  $4::uuid, $5::uuid, $6::uuid, $7::uuid,
  $8, $8, NULL, $9, $10,
  $11, $12::uuid, $13,
  'manual_review', $2, $2, $2, $2
)
RETURNING shifting_event_id`,
		countsTenant, key, priority,
		sourcePark, sourceShedArg, feedProjPark, destShed,
		raisedAt, authState, eventStatus,
		canceledAt, canceledBy, cancelReason,
	).Scan(&eventID); err != nil {
		t.Fatalf("seed raised shifting event %s: %v", key, err)
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
			t.Fatalf("seed raised shifting impact %s/%d: %v", key, i, err)
		}
	}
	return eventID
}

// TestFeedProjectionScheduledDateRaisedMovementUsesTheActionsLeadTime is the timing half of the new
// branch, and the 13:45 case is the one that makes the rule earn its complexity.
//
// A raised movement is feed-effective from the day its animals are expected to WALK, not the day it
// was asked for. Anchoring on the raise day would feed a destination a full day before a post-cutoff
// raise's animals move — the same over-feeding defect this change exists to fix, one day earlier.
func TestFeedProjectionScheduledDateRaisedMovementUsesTheActionsLeadTime(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		// raisedHour/raisedMinute are the India wall clock the 13:30 cutoff compares against.
		raisedHour, raisedMinute int
		priority                 string
		targetOffset             int // days from the raise day to the feed day under test
		wantDelta                int64
	}{
		{name: "raised before the cutoff does not count on the raise day itself", raisedHour: 9, priority: "low", targetOffset: 0, wantDelta: 0},
		{name: "raised before the cutoff counts tomorrow", raisedHour: 9, priority: "low", targetOffset: 1, wantDelta: 4},
		{name: "raised one minute before the cutoff still counts tomorrow", raisedHour: 13, raisedMinute: 29, priority: "low", targetOffset: 1, wantDelta: 4},
		// THE CASE THE LEAD TIME EXISTS FOR: these animals do not walk until the day after tomorrow.
		{name: "raised after the cutoff does NOT reach tomorrow's sheet", raisedHour: 13, raisedMinute: 45, priority: "low", targetOffset: 1, wantDelta: 0},
		{name: "raised after the cutoff counts the day after", raisedHour: 13, raisedMinute: 45, priority: "low", targetOffset: 2, wantDelta: 4},
		// High priority is executed same-day, so its animals eat at the destination today.
		{name: "high priority counts on the raise day", raisedHour: 16, priority: "high", targetOffset: 0, wantDelta: 4},
		// The <= half: once due, an unapproved movement keeps counting rather than silently
		// de-feeding a destination whose animals are still expected.
		{name: "a long-unapproved movement keeps counting", raisedHour: 9, priority: "low", targetOffset: 6, wantDelta: 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := newFeedProjRepo(t, ctx)

			raisedAt := time.Date(2026, time.July, 10, tc.raisedHour, tc.raisedMinute, 0, 0, biztime.DefaultLocation())
			raiseDay := feedProjDay(2026, time.July, 10)

			for i := 0; i < 6; i++ {
				insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
			}
			insertFeedProjRaised(t, ctx, pool, "raised-lead", tc.priority, raisedAt,
				feedProjShedA, feedProjShedB, "pending", "pending",
				[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 4}})

			got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(raiseDay.AddDate(0, 0, tc.targetOffset)))
			if err != nil {
				t.Fatalf("ProjectedShedCountsForFeed: %v", err)
			}

			row := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K1", "female")
			if row.PendingDelta != tc.wantDelta {
				t.Errorf("pending_delta=%d, want %d", row.PendingDelta, tc.wantDelta)
			}
			if want := 6 + tc.wantDelta; row.ProjectedHeadCount != want {
				t.Errorf("projected_head_count=%d, want %d", row.ProjectedHeadCount, want)
			}
		})
	}
}

// TestFeedProjectionStatusMatrixRaisedBranchExcludesRejectedAndCanceled walks the authorization
// matrix for the new branch. Approval no longer starts the feed clock; only REJECTION stops it, so a
// movement the park head turns down must stop feeding the shed immediately.
//
// It also pins DISJOINTNESS. The two pending_event branches split on authorization_state, so a
// movement contributes exactly ONCE as it travels from raised to approved. If they overlapped, the
// delta would double at the moment of approval — a destination fed for eight animals when four are
// coming, with nothing on screen to suggest anything is wrong.
func TestFeedProjectionStatusMatrixRaisedBranchExcludesRejectedAndCanceled(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name        string
		authState   string
		eventStatus string
		wantDelta   int64
	}{
		{name: "raised and awaiting approval counts", authState: "pending", eventStatus: "pending", wantDelta: 4},
		{name: "rejected by the park head stops feeding the shed", authState: "rejected", eventStatus: "rejected", wantDelta: 0},
		{name: "canceled stops feeding the shed", authState: "pending", eventStatus: "canceled", wantDelta: 0},
		{name: "unresolved does not feed the shed", authState: "pending", eventStatus: "unresolved", wantDelta: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := newFeedProjRepo(t, ctx)

			raisedAt := time.Date(2026, time.July, 10, 9, 0, 0, 0, biztime.DefaultLocation())
			for i := 0; i < 6; i++ {
				insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
			}
			insertFeedProjRaised(t, ctx, pool, "raised-matrix", "low", raisedAt,
				feedProjShedA, feedProjShedB, tc.authState, tc.eventStatus,
				[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 4}})

			got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(feedProjDay(2026, time.July, 11)))
			if err != nil {
				t.Fatalf("ProjectedShedCountsForFeed: %v", err)
			}
			if row := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K1", "female"); row.PendingDelta != tc.wantDelta {
				t.Errorf("pending_delta=%d, want %d", row.PendingDelta, tc.wantDelta)
			}
		})
	}
}

// TestFeedProjectionStatusMatrixApprovedMovementIsNotCountedTwice is the disjointness regression the
// test above describes. One movement, approved: it must satisfy the AUTHORIZED branch and NOT also
// the raised one.
func TestFeedProjectionStatusMatrixApprovedMovementIsNotCountedTwice(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	approvedAt := feedProjApproval(2026, time.July, 10, 9)
	for i := 0; i < 6; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
	}
	insertFeedProjShifting(t, ctx, pool, "raised-then-approved", "low", approvedAt,
		feedProjShedA, feedProjShedB, "authorized", "authorized",
		[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 4}})

	got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(feedProjDay(2026, time.July, 11)))
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}
	row := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K1", "female")
	if row.PendingDelta != 4 {
		t.Fatalf("pending_delta=%d, want 4 — an approved movement must be counted by exactly ONE branch, not both", row.PendingDelta)
	}
}

// TestFeedProjectionOneToManyRaisedMultiImpactMovementDoesNotFanOutLiveCount is the fan-out
// regression for the new branch.
//
// A movement has MANY impact rows (one per cohort). Joining the events to the live grains directly
// would multiply the live COUNT by the impact-row count. The delta CTE pre-aggregates the legs to
// one row per grain first — this proves the new branch feeds that same pre-aggregation rather than
// bypassing it.
func TestFeedProjectionOneToManyRaisedMultiImpactMovementDoesNotFanOutLiveCount(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	raisedAt := time.Date(2026, time.July, 10, 9, 0, 0, 0, biztime.DefaultLocation())

	// Ten live Beetal females in the destination. If the join fanned out, this 10 would be
	// multiplied by the number of impact rows and read as 30.
	for i := 0; i < 10; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
	}
	insertFeedProjRaised(t, ctx, pool, "raised-fanout", "low", raisedAt,
		feedProjShedA, feedProjShedB, "pending", "pending",
		[]feedProjImpact{
			{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 3},
			// Two more legs on the SAME movement, at grains that must not touch the row above.
			{breedLabel: "Sirohi", stageTag: "K1", sex: "female", headCount: 5},
			{breedLabel: "Beetal", stageTag: "K1", sex: "male", headCount: 2},
		})

	got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(feedProjDay(2026, time.July, 11)))
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}

	row := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K1", "female")
	if row.CurrentHeadCount != 10 {
		t.Fatalf("current_head_count=%d, want 10 — a multi-impact raised movement fanned the live census out", row.CurrentHeadCount)
	}
	if row.PendingDelta != 3 {
		t.Fatalf("pending_delta=%d, want only this grain's own leg (3)", row.PendingDelta)
	}
	if row.ProjectedHeadCount != 13 {
		t.Fatalf("projected_head_count=%d, want 13", row.ProjectedHeadCount)
	}
}

// TestFeedProjectionPageBoundaryRaisedDeltaKeepsTotalRowsInvariant: total_rows is a window function
// over the WHOLE combined set, so it must not move with the page — including for a grain that exists
// ONLY because a raised movement is bringing animals to a shed that holds none of it today.
//
// A page-scoped total here would tell the feed team the park has fewer grains than it does, and the
// grain most likely to be dropped is exactly the incoming one they most need to see.
func TestFeedProjectionPageBoundaryRaisedDeltaKeepsTotalRowsInvariant(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	raisedAt := time.Date(2026, time.July, 10, 9, 0, 0, 0, biztime.DefaultLocation())

	insertFeedProjGoat(t, ctx, pool, 0, "Beetal", "female", "K1", feedProjShedA)
	insertFeedProjGoat(t, ctx, pool, 1, "Sirohi", "female", "K1", feedProjShedA)
	insertFeedProjGoat(t, ctx, pool, 2, "Beetal", "male", "K2", feedProjShedB)
	// An incoming grain the destination holds NONE of today: delta-only, no live row at all.
	insertFeedProjRaised(t, ctx, pool, "raised-paging", "low", raisedAt,
		feedProjShedA, feedProjShedB, "pending", "pending",
		[]feedProjImpact{{breedLabel: "Jamnapari", stageTag: "K2", sex: "female", headCount: 7}})

	target := feedProjDay(2026, time.July, 11)
	full, err := repo.ProjectedShedCountsForFeed(ctx, domain.FeedProjectedCountQuery{
		TenantID: countsTenant, TargetDate: target, Limit: 100, StableOrder: true,
	})
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed(full): %v", err)
	}
	if full.TotalRows < 4 {
		t.Fatalf("total_rows=%d, want at least the 3 live grains plus the incoming one", full.TotalRows)
	}

	// Walk it one row at a time. Every page must report the SAME whole-result total, and the union
	// of the pages must equal the unpaged read exactly — no grain seen twice, none skipped.
	seen := map[string]bool{}
	for offset := int32(0); offset < int32(full.TotalRows); offset++ {
		page, err := repo.ProjectedShedCountsForFeed(ctx, domain.FeedProjectedCountQuery{
			TenantID: countsTenant, TargetDate: target, Limit: 1, Offset: offset, StableOrder: true,
		})
		if err != nil {
			t.Fatalf("ProjectedShedCountsForFeed(offset=%d): %v", offset, err)
		}
		if page.TotalRows != full.TotalRows {
			t.Fatalf("total_rows=%d at offset %d, want the page-invariant %d", page.TotalRows, offset, full.TotalRows)
		}
		if len(page.Items) != 1 {
			t.Fatalf("page at offset %d returned %d rows, want 1", offset, len(page.Items))
		}
		key := fmt.Sprintf("%v|%s|%s|%s", page.Items[0].ShedID, page.Items[0].Breed, page.Items[0].ManagementStage, page.Items[0].Sex)
		if seen[key] {
			t.Fatalf("grain %s appeared on two pages — the paged walk is duplicating rows", key)
		}
		seen[key] = true
	}
	if int64(len(seen)) != full.TotalRows {
		t.Fatalf("paged walk saw %d distinct grains, want %d — a grain was skipped between pages", len(seen), full.TotalRows)
	}
}

// TestFeedProjectionParkScopeFiltersTheRaisedLegOnBothSides: the shed filter is applied to the live
// side AND to both legs of a movement, so filtering to one shed cannot leave it showing a delta
// sourced from a shed the filter excluded.
//
// The new branch adds legs, so it needs the same proof the authorized one has: a scope predicate
// that reaches the live CTE but not the raised legs is the classic way a filtered view reports a
// number the unfiltered view cannot explain.
func TestFeedProjectionParkScopeFiltersTheRaisedLegOnBothSides(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	raisedAt := time.Date(2026, time.July, 10, 9, 0, 0, 0, biztime.DefaultLocation())

	for i := 0; i < 6; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedA)
	}
	for i := 10; i < 14; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
	}
	// Shed A loses four, shed B gains them.
	insertFeedProjRaised(t, ctx, pool, "raised-scope", "low", raisedAt,
		feedProjShedA, feedProjShedB, "pending", "pending",
		[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 4}})

	target := feedProjDay(2026, time.July, 11)
	shedA := feedProjShedA
	scoped, err := repo.ProjectedShedCountsForFeed(ctx, domain.FeedProjectedCountQuery{
		TenantID: countsTenant, TargetDate: target, Limit: 100, ShedID: &shedA,
	})
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed(shed A): %v", err)
	}
	for _, row := range scoped.Items {
		if row.ShedID != nil && *row.ShedID != feedProjShedA {
			t.Fatalf("a shed-A-scoped read returned a row for shed %s", *row.ShedID)
		}
	}
	// Shed A is the SOURCE, so its own leg is negative and must survive the filter.
	row := findFeedProjRow(t, scoped, feedProjShedA, "Beetal", "K1", "female")
	if row.PendingDelta != -4 {
		t.Fatalf("shed A pending_delta=%d, want -4 — the source leg of a raised movement must be scoped in, not filtered away", row.PendingDelta)
	}
	if row.ProjectedHeadCount != 2 {
		t.Fatalf("shed A projected_head_count=%d, want 2", row.ProjectedHeadCount)
	}

	shedB := feedProjShedB
	scopedB, err := repo.ProjectedShedCountsForFeed(ctx, domain.FeedProjectedCountQuery{
		TenantID: countsTenant, TargetDate: target, Limit: 100, ShedID: &shedB,
	})
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed(shed B): %v", err)
	}
	rowB := findFeedProjRow(t, scopedB, feedProjShedB, "Beetal", "K1", "female")
	if rowB.PendingDelta != 4 || rowB.ProjectedHeadCount != 8 {
		t.Fatalf("shed B delta=%d projected=%d, want +4 and 8", rowB.PendingDelta, rowB.ProjectedHeadCount)
	}
}
