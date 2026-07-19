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
	var appliedAt any
	if eventStatus == "applied" {
		appliedAt = approvedAt.Add(24 * time.Hour)
	}

	var eventID string
	if err := pool.QueryRow(ctx, `
INSERT INTO shifting_events (
  tenant_id, logical_shifting_event_key, priority, category,
  source_park_id, source_shed_id, destination_park_id, destination_shed_id,
  raised_at, effective_at, authorized_at, authorization_state, event_status, applied_at,
  source_system, source_ref, payload_hash, idempotency_key, request_fingerprint
) VALUES (
  $1::uuid, $2, $3, 'routine',
  $4::uuid, $5::uuid, $6::uuid, $7::uuid,
  $8, $8, $8, $9, $10, $11,
  'manual_review', $2, $2, $2, $2
)
RETURNING shifting_event_id`,
		countsTenant, key, priority,
		sourcePark, sourceShedArg, feedProjPark, destShed,
		approvedAt, authState, eventStatus, appliedAt,
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

// TestFeedProjectionTimingRule proves the approval-date + lead-days rule at the SQL layer, and in
// particular the <= comparison: an overdue movement keeps counting instead of silently dropping
// out of the projection.
func TestFeedProjectionTimingRule(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name     string
		priority string
		// offsets from the approval day to the feed day under test.
		targetOffset int
		wantDelta    int64
		wantOverdue  bool
	}{
		// Emergency: approved day X, feed-effective X+1.
		{name: "emergency does not count on the approval day", priority: "emergency", targetOffset: 0, wantDelta: 0},
		{name: "emergency counts on the day after approval", priority: "emergency", targetOffset: 1, wantDelta: 4},
		// Normal: approved day X, feed-effective X+2.
		{name: "normal does not count on the approval day", priority: "normal", targetOffset: 0, wantDelta: 0},
		{name: "normal does not count one day after approval", priority: "normal", targetOffset: 1, wantDelta: 0},
		{name: "normal counts two days after approval", priority: "normal", targetOffset: 2, wantDelta: 4},
		// High shares the normal lead: it is a queue-order signal, not an execution-speed one.
		{name: "high does not count one day after approval", priority: "high", targetOffset: 1, wantDelta: 0},
		{name: "high counts two days after approval", priority: "high", targetOffset: 2, wantDelta: 4},
		// The overdue half of the rule. Under an == comparison these would be 0, and the
		// destination shed would quietly stop being fed for animals still expected.
		{name: "an overdue emergency still counts five days on", priority: "emergency", targetOffset: 5, wantDelta: 4, wantOverdue: true},
		{name: "an overdue normal still counts five days on", priority: "normal", targetOffset: 5, wantDelta: 4, wantOverdue: true},
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

// TestFeedProjectionExcludesAppliedMovements is the double-count regression.
//
// A COMPLETED shifting has already relocated its animals in goats, so it is ALREADY inside
// current_head_count. Adding its delta again would over-feed the destination shed by exactly the
// size of the movement — a wrong number that looks entirely plausible on screen.
func TestFeedProjectionExcludesAppliedMovements(t *testing.T) {
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
			// The whole point of this test.
			name:        "an applied movement is already in the live herd and must not be added again",
			authState:   "authorized",
			eventStatus: "applied",
			wantDelta:   0,
		},
		{
			// Approval is what starts the feed lead. An unapproved movement may never happen at
			// all, so feeding for it would waste ration on animals that are not coming.
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
			// Well past the two-day lead, so timing can never be the reason a delta is absent.
			target := feedProjDay(2026, time.July, 20)

			for i := 0; i < 10; i++ {
				insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
			}
			insertFeedProjShifting(t, ctx, pool, "applied-"+tc.eventStatus, "normal", approvedAt,
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
			insertFeedProjShifting(t, ctx, pool, "clamp", "normal", approvedAt,
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

// TestFeedProjectionMultiImpactMovementDoesNotFanOutLiveCount is the join-cardinality regression.
//
// shifting_event_impacts is 1:N per event. If the movement legs were joined to the live census
// BEFORE being aggregated, a three-impact movement would multiply every live grain row it touched
// by three. The delta CTE pre-aggregates to one row per grain precisely to make that impossible.
func TestFeedProjectionMultiImpactMovementDoesNotFanOutLiveCount(t *testing.T) {
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
	insertFeedProjShifting(t, ctx, pool, "fanout", "normal", approvedAt,
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

	// The destination shed holds a DIFFERENT grain, so it exists in the census but not for Malai.
	for i := 0; i < 2; i++ {
		insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
	}
	insertFeedProjShifting(t, ctx, pool, "incoming", "normal", approvedAt,
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
	insertFeedProjShifting(t, ctx, pool, "casing", "normal", approvedAt,
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
// An approval stamped 20:00 UTC is already the NEXT day in India. Deriving the approval business
// date in UTC would start the lead a day early and feed the destination shed a day late.
func TestFeedProjectionApprovalDateUsesIndiaBusinessCalendar(t *testing.T) {
	ctx := context.Background()

	// 2026-07-10T20:00:00Z == 2026-07-11T01:30 IST. With the one-day emergency lead the movement
	// is feed-effective on the 12th in the India calendar, and NOT on the 11th.
	approvedAt := time.Date(2026, time.July, 10, 20, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		target    time.Time
		wantDelta int64
	}{
		{
			name:      "the UTC-derived effective day does not count",
			target:    feedProjDay(2026, time.July, 11),
			wantDelta: 0,
		},
		{
			name:      "the India-derived effective day counts",
			target:    feedProjDay(2026, time.July, 12),
			wantDelta: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := newFeedProjRepo(t, ctx)

			for i := 0; i < 2; i++ {
				insertFeedProjGoat(t, ctx, pool, i, "Beetal", "female", "K1", feedProjShedB)
			}
			insertFeedProjShifting(t, ctx, pool, "tz", "emergency", approvedAt,
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
	insertFeedProjShifting(t, ctx, pool, "scope", "normal", approvedAt,
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
	insertFeedProjShifting(t, ctx, pool, "breed-beetal", "normal", approvedAt,
		feedProjShedB, feedProjShedA, "authorized", "authorized",
		[]feedProjImpact{{breedLabel: "Beetal", stageTag: "K1", sex: "female", headCount: 2}})
	insertFeedProjShifting(t, ctx, pool, "breed-boer", "normal", approvedAt,
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
	if got.TotalRows != 1 {
		t.Fatalf("total_rows=%d, want 1 (Boer grain must not appear)", got.TotalRows)
	}
	row := findFeedProjRow(t, got, feedProjShedA, "Beetal", "K1", "female")
	if row.CurrentHeadCount != 3 || row.PendingDelta != 2 || row.ProjectedHeadCount != 5 {
		t.Errorf("current=%d delta=%d projected=%d, want 3/2/5",
			row.CurrentHeadCount, row.PendingDelta, row.ProjectedHeadCount)
	}
}
