package postgres

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// TestReturningToAnEarlierShareTakesEffect is the regression for a defect the randomization E2E
// found on the real path.
//
// The write's idempotency key is derived from (category, business day, share), so setting 40%, then
// 80%, then BACK to 40% on the same day REUSES the first key. The first implementation treated a
// reused key as a replay and returned early without writing — which is not replaying a previous
// outcome, it is silently refusing to return to a share the CEO just asked for. The panel then
// reported 40% while the verifier's queue still ran at 80%.
//
// Run against the old code this fails on the third assertion; the reservation is now the conflict
// detector only, and the value-idempotent upsert always runs.
func TestReturningToAnEarlierShareTakesEffect(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	today := biztime.BusinessDate(time.Now())

	set := func(percent int) {
		t.Helper()
		if err := repo.UpsertSamplingPolicy(ctx, domain.SetSamplingPolicy{
			TenantID: tenantID, Category: "vaccination_proof", Percent: percent,
			EffectiveBusinessDate: today,
			// The SAME derivation the service applies when no client key arrives.
			IdempotencyKey: "verification-sampling-vaccination_proof-" + today + "-" + strconv.Itoa(percent),
		}); err != nil {
			t.Fatalf("UpsertSamplingPolicy(%d): %v", percent, err)
		}
	}
	inForce := func() int {
		t.Helper()
		rows, err := repo.ListSamplingPolicies(ctx, tenantID, today)
		if err != nil {
			t.Fatalf("ListSamplingPolicies: %v", err)
		}
		for _, row := range rows {
			if row.Category == "vaccination_proof" {
				return row.Percent
			}
		}
		t.Fatal("no policy row in force")
		return 0
	}

	set(40)
	if got := inForce(); got != 40 {
		t.Fatalf("after the first write the share is %d%%, want 40%%", got)
	}
	set(80)
	if got := inForce(); got != 80 {
		t.Fatalf("after raising, the share is %d%%, want 80%%", got)
	}
	set(40)
	if got := inForce(); got != 40 {
		t.Fatalf("after returning to a share already used today, the share is still %d%% — the reused "+
			"idempotency key suppressed the write, so the panel would report 40%% while the queue ran at %d%%", got, got)
	}

	// And a genuine same-key DIFFERENT-payload replay is still refused rather than letting the last
	// writer win: that is the case the reservation is actually there for.
	err := repo.UpsertSamplingPolicy(ctx, domain.SetSamplingPolicy{
		TenantID: tenantID, Category: "vaccination_proof", Percent: 15,
		EffectiveBusinessDate: today,
		IdempotencyKey:        "verification-sampling-vaccination_proof-" + today + "-40",
	})
	if err == nil {
		t.Fatal("a same-key different-share write was accepted; two shares racing on one day are two decisions and the loser must be told")
	}
	if got := inForce(); got != 40 {
		t.Fatalf("the refused write still changed the share to %d%%", got)
	}
}

// TestSettleUnsampledLeavesTodayAndTheLockedCategoriesAlone pins the closeout's two narrowings on
// real rows: it never touches a day that is still open (the share can still change), and it never
// settles a category whose approve must carry a measurement.
func TestSettleUnsampledLeavesTodayAndTheLockedCategoriesAlone(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	todayStart := biztime.BusinessDayStart(time.Now())
	yesterday := todayStart.AddDate(0, 0, -1).Add(9 * time.Hour)

	// 0% on both categories: nothing is drawn, so every item is a settlement candidate and the only
	// thing deciding the outcome is the closeout's own narrowing.
	for _, category := range []string{"vaccination_proof", "feed_packing"} {
		if err := repo.UpsertSamplingPolicy(ctx, domain.SetSamplingPolicy{
			TenantID: tenantID, Category: category, Percent: 0,
			EffectiveBusinessDate: biztime.BusinessDate(yesterday),
			IdempotencyKey:        "sampling-" + category,
		}); err != nil {
			t.Fatalf("UpsertSamplingPolicy(%s): %v", category, err)
		}
	}

	raise := func(category string, capturedAt time.Time, key string) string {
		t.Helper()
		label := key
		res, err := repo.CreateItem(ctx, domain.CreateItem{
			TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: category,
			Source:       domain.SourceRef{Module: "vaccination", RefType: "vaccination_goat", RefID: newUUID(t, ctx, pool)},
			SubjectLabel: &label, MediaRefs: []string{"proof-" + key},
			CapturedAt: capturedAt, IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("CreateItem(%s): %v", category, err)
		}
		return res.Item.ItemID
	}
	closedDay := raise("vaccination_proof", yesterday, "sampling-closed-day")
	openDay := raise("vaccination_proof", todayStart.Add(9*time.Hour), "sampling-open-day")
	locked := raise("feed_packing", yesterday, "sampling-locked-category")

	settled, err := repo.SettleUnsampledItems(ctx, ports.SettleUnsampledParams{
		TenantID: tenantID, Before: todayStart,
		// The registry-derived allowlist: feed packing is absent because its approve must carry the
		// packed quantities.
		WaivableCategories: []string{"vaccination_proof"},
		Limit:              100,
	})
	if err != nil {
		t.Fatalf("SettleUnsampledItems: %v", err)
	}
	if settled != 1 {
		t.Fatalf("settled %d items, want exactly the one on the closed day", settled)
	}
	assertStatus(t, ctx, pool, closedDay, "approved", true)
	// TODAY is still open: the CEO can raise the share this afternoon and recruit this video back,
	// and one already waived could not be recruited.
	assertStatus(t, ctx, pool, openDay, "pending", false)
	// The locked category waits for a person however long that takes -- waiving it would complete a
	// pen-session with no packed quantity recorded at all.
	assertStatus(t, ctx, pool, locked, "pending", false)
}

func assertStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID, wantStatus string, wantSettled bool) {
	t.Helper()
	var status string
	var autoResolution *string
	var verifiedBy *string
	if err := pool.QueryRow(ctx, `
SELECT status, auto_resolution, verified_by::text
FROM verification_items WHERE item_id = $1::uuid`, itemID).Scan(&status, &autoResolution, &verifiedBy); err != nil {
		t.Fatalf("read item %s: %v", itemID, err)
	}
	if status != wantStatus {
		t.Fatalf("item %s status = %q, want %q", itemID, status, wantStatus)
	}
	settled := autoResolution != nil
	if settled != wantSettled {
		t.Fatalf("item %s auto_resolution = %v, want settled=%t", itemID, autoResolution, wantSettled)
	}
	if settled {
		if *autoResolution != domain.AutoResolutionNotSampled {
			t.Fatalf("item %s auto_resolution = %q", itemID, *autoResolution)
		}
		// Nobody signed it, which is what keeps a settlement out of every per-verifier aggregate.
		if verifiedBy != nil {
			t.Fatalf("item %s was settled by the policy but carries verified_by=%q", itemID, *verifiedBy)
		}
	}
}

func newUUID(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatalf("gen_random_uuid: %v", err)
	}
	return id
}

// --- Adversarial coverage for the Randomization panel's day aggregate -------------------------
//
// ListSamplingDayStats is the one aggregate this feature adds, and the CEO reads a completion
// percentage off it. These five exercise the ways an aggregate lies: a join that multiplies rows, a
// count silently capped by a page, a day boundary read in the wrong timezone, another tenant's rows
// leaking in, and a status landing in the wrong bucket.

// samplingStatsFixture seeds one tenant with items at a chosen instant and returns the tenant id.
func samplingStatsFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	return newTenant(t, ctx, pool)
}

func seedSamplingItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, tenantID, category string, capturedAt time.Time, key string) string {
	t.Helper()
	label := key
	res, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: category,
		Source:       domain.SourceRef{Module: "vaccination", RefType: "vaccination_goat", RefID: newUUID(t, ctx, pool)},
		SubjectLabel: &label, MediaRefs: []string{"proof-" + key},
		CapturedAt: capturedAt, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("CreateItem(%s): %v", key, err)
	}
	return res.Item.ItemID
}

func dayStats(t *testing.T, ctx context.Context, repo *Repository, tenantID, businessDate string) domain.SamplingDayStats {
	t.Helper()
	stats, err := repo.ListSamplingDayStats(ctx, tenantID, businessDate)
	if err != nil {
		t.Fatalf("ListSamplingDayStats(%s): %v", businessDate, err)
	}
	return stats["vaccination_proof"]
}

// TestSamplingDayStatsOneToManyDoesNotFanOut: the policy side is a HISTORY -- one row per change --
// so a category with several policy rows would multiply every item it joins if the CTE were not
// DISTINCT ON (category). Ten items and three policy rows must still count ten.
func TestSamplingDayStatsOneToManyDoesNotFanOut(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	tenantID := samplingStatsFixture(t, ctx, pool)

	day := biztime.BusinessDayStart(time.Now())
	for i := 0; i < 10; i++ {
		seedSamplingItem(t, ctx, pool, repo, tenantID, "vaccination_proof", day.Add(9*time.Hour), fmt.Sprintf("fanout-%02d", i))
	}
	// Three shares in force on three different days, all on or before the day being read.
	for i, percent := range []int{100, 60, 40} {
		if err := repo.UpsertSamplingPolicy(ctx, domain.SetSamplingPolicy{
			TenantID: tenantID, Category: "vaccination_proof", Percent: percent,
			EffectiveBusinessDate: biztime.BusinessDate(day.AddDate(0, 0, -(3 - i))),
			IdempotencyKey:        fmt.Sprintf("fanout-policy-%d", i),
		}); err != nil {
			t.Fatalf("UpsertSamplingPolicy: %v", err)
		}
	}
	got := dayStats(t, ctx, repo, tenantID, biztime.BusinessDate(day))
	if got.Captured != 10 {
		t.Fatalf("captured = %d, want 10 -- three policy rows multiplied the item rows", got.Captured)
	}
	if got.Selected > got.Captured {
		t.Fatalf("selected = %d exceeds captured = %d", got.Selected, got.Captured)
	}
}

// TestSamplingDayStatsPageBoundary: the panel's numbers are WHOLE-DAY aggregates and must never be
// derived from a page. 250 items is past every page size in this module (20 default, 100 max), so a
// count that came from a page would stop at one of those.
func TestSamplingDayStatsPageBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 10*time.Second)
	tenantID := samplingStatsFixture(t, ctx, pool)

	day := biztime.BusinessDayStart(time.Now())
	for i := 0; i < 250; i++ {
		seedSamplingItem(t, ctx, pool, repo, tenantID, "vaccination_proof", day.Add(9*time.Hour), fmt.Sprintf("page-%03d", i))
	}
	got := dayStats(t, ctx, repo, tenantID, biztime.BusinessDate(day))
	if got.Captured != 250 {
		t.Fatalf("captured = %d, want 250 -- a page-derived count would cap at 20 or 100", got.Captured)
	}
}

// TestSamplingDayStatsDateShift: the day is an Asia/Kolkata business day, not a UTC one. An item
// captured at 23:30 IST belongs to that IST day even though it is already the NEXT day in UTC, and
// one captured at 00:30 IST belongs to the new day even though UTC still says yesterday.
func TestSamplingDayStatsDateShift(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	tenantID := samplingStatsFixture(t, ctx, pool)

	day := biztime.BusinessDayStart(time.Now()).AddDate(0, 0, -1)
	lateOnDay := day.Add(23*time.Hour + 30*time.Minute)    // 23:30 IST = 18:00 UTC the same day
	earlyNextDay := day.Add(24*time.Hour + 30*time.Minute) // 00:30 IST next day = 19:00 UTC the day before
	seedSamplingItem(t, ctx, pool, repo, tenantID, "vaccination_proof", lateOnDay, "shift-late")
	seedSamplingItem(t, ctx, pool, repo, tenantID, "vaccination_proof", earlyNextDay, "shift-early")

	if got := dayStats(t, ctx, repo, tenantID, biztime.BusinessDate(day)); got.Captured != 1 {
		t.Fatalf("day %s captured = %d, want exactly the 23:30 IST item", biztime.BusinessDate(day), got.Captured)
	}
	if got := dayStats(t, ctx, repo, tenantID, biztime.BusinessDate(day.AddDate(0, 0, 1))); got.Captured != 1 {
		t.Fatalf("next day captured = %d, want exactly the 00:30 IST item", got.Captured)
	}
}

// TestSamplingDayStatsParkScope: the aggregate is TENANT-scoped. A second tenant's proof on the same
// day must never reach this tenant's panel -- a missing tenant predicate would show a CEO another
// farm's work as his own.
func TestSamplingDayStatsParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	mine := samplingStatsFixture(t, ctx, pool)
	theirs := samplingStatsFixture(t, ctx, pool)

	day := biztime.BusinessDayStart(time.Now())
	seedSamplingItem(t, ctx, pool, repo, mine, "vaccination_proof", day.Add(9*time.Hour), "scope-mine")
	for i := 0; i < 5; i++ {
		seedSamplingItem(t, ctx, pool, repo, theirs, "vaccination_proof", day.Add(9*time.Hour), fmt.Sprintf("scope-theirs-%d", i))
	}
	if got := dayStats(t, ctx, repo, mine, biztime.BusinessDate(day)); got.Captured != 1 {
		t.Fatalf("captured = %d, want 1 -- another tenant's proof leaked into this panel", got.Captured)
	}
	// The other tenant's own share must also not be read from this tenant's policy rows.
	if err := repo.UpsertSamplingPolicy(ctx, domain.SetSamplingPolicy{
		TenantID: mine, Category: "vaccination_proof", Percent: 0,
		EffectiveBusinessDate: biztime.BusinessDate(day), IdempotencyKey: "scope-policy",
	}); err != nil {
		t.Fatalf("UpsertSamplingPolicy: %v", err)
	}
	if got := dayStats(t, ctx, repo, theirs, biztime.BusinessDate(day)); got.Selected != 5 {
		t.Fatalf("the other tenant's selected = %d, want 5 -- this tenant's 0%% share reached across the boundary", got.Selected)
	}
}

// TestSamplingDayStatsStatusMatrix: every status lands in exactly the bucket it belongs to, and the
// two exclusions hold -- a WITHDRAWN item counts nowhere (its source record was superseded, so it is
// nobody's work), and a POLICY-SETTLED item counts as settled and never as reviewed.
func TestSamplingDayStatsStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	tenantID := samplingStatsFixture(t, ctx, pool)

	day := biztime.BusinessDayStart(time.Now())
	// 100% so every item is drawn: this test is about the STATUS axis, not the draw.
	if err := repo.UpsertSamplingPolicy(ctx, domain.SetSamplingPolicy{
		TenantID: tenantID, Category: "vaccination_proof", Percent: 100,
		EffectiveBusinessDate: biztime.BusinessDate(day), IdempotencyKey: "status-policy",
	}); err != nil {
		t.Fatalf("UpsertSamplingPolicy: %v", err)
	}
	ids := map[string]string{}
	for _, key := range []string{"pending", "approved", "rejected", "withdrawn", "settled"} {
		ids[key] = seedSamplingItem(t, ctx, pool, repo, tenantID, "vaccination_proof", day.Add(9*time.Hour), "status-"+key)
	}
	verifier := newUUID(t, ctx, pool)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed status: %v", err)
		}
	}
	exec(`UPDATE verification_items SET status='approved', verified_by=$2::uuid, verified_at=now() WHERE item_id=$1::uuid`, ids["approved"], verifier)
	exec(`UPDATE verification_items SET status='rejected', verdict_reason='re-shoot', verified_by=$2::uuid, verified_at=now() WHERE item_id=$1::uuid`, ids["rejected"], verifier)
	exec(`UPDATE verification_items SET status='withdrawn' WHERE item_id=$1::uuid`, ids["withdrawn"])
	exec(`UPDATE verification_items SET status='approved', auto_resolution='not_sampled', verified_at=now() WHERE item_id=$1::uuid`, ids["settled"])

	got := dayStats(t, ctx, repo, tenantID, biztime.BusinessDate(day))
	if got.Captured != 4 {
		t.Fatalf("captured = %d, want 4 -- withdrawn work must count nowhere", got.Captured)
	}
	if got.Selected != 4 {
		t.Fatalf("selected = %d, want 4 at a 100%% share", got.Selected)
	}
	if got.Reviewed != 2 {
		t.Fatalf("reviewed = %d, want 2 (one approved + one rejected); a settled item is not a review", got.Reviewed)
	}
	if got.AutoAccepted != 1 {
		t.Fatalf("auto_accepted = %d, want 1", got.AutoAccepted)
	}
	if got.ProgressPercent() != 50 {
		t.Fatalf("progress = %d%%, want 50%% (2 of 4 drawn reviewed)", got.ProgressPercent())
	}
}

// TestAVerdictOnAnUndrawnItemIsHersToCast pins the rule raised in review of this feature
// (maintainer decision 2026-08-27): the share is a FLOOR on the review a verifier is REQUIRED to
// do, never a ceiling on the review she is PERMITTED to do.
//
// The reachable case is a drawer still open after the CEO LOWERED the share -- the draw is
// monotonic, so raising it can never drop an item she was holding -- plus an older push or a direct
// call. Failing that write closed would make bad work unreportable (her rejection refused, the work
// proceeds to completed) and would discard a review she has already performed.
//
// What this proves is that allowing it mislabels nothing: the row records a HUMAN verdict, the
// closeout leaves it alone rather than stamping a waiver over it, and the panel's share math is
// untouched because Reviewed and Selected are both share-scoped.
func TestAVerdictOnAnUndrawnItemIsHersToCast(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	day := biztime.BusinessDayStart(time.Now())

	// 0%: nothing is drawn, so every item below is one the policy did not ask her to watch.
	if err := repo.UpsertSamplingPolicy(ctx, domain.SetSamplingPolicy{
		TenantID: tenantID, Category: "vaccination_proof", Percent: 0,
		EffectiveBusinessDate: biztime.BusinessDate(day), IdempotencyKey: "undrawn-share",
	}); err != nil {
		t.Fatalf("UpsertSamplingPolicy: %v", err)
	}
	approved := seedSamplingItem(t, ctx, pool, repo, tenantID, "vaccination_proof", day.Add(9*time.Hour), "undrawn-approve")
	rejected := seedSamplingItem(t, ctx, pool, repo, tenantID, "vaccination_proof", day.Add(9*time.Hour), "undrawn-reject")
	verifier := newUUID(t, ctx, pool)

	if _, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: approved, Decision: domain.DecisionApproved,
		VerifierID: verifier, RowVersion: 1, IdempotencyKey: "undrawn-approve-verdict",
	}); err != nil {
		t.Fatalf("approve on an undrawn item was refused: %v", err)
	}
	// The one that matters most: she watched an undrawn video and the work was WRONG. A refusal here
	// would push bad work through to completed.
	if _, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: rejected, Decision: domain.DecisionRejected, Reason: "wrong pen on camera",
		VerifierID: verifier, RowVersion: 1, IdempotencyKey: "undrawn-reject-verdict",
	}); err != nil {
		t.Fatalf("rejection of bad work on an undrawn item was refused: %v", err)
	}

	// Both are recorded as HUMAN verdicts. auto_resolution = 'not_sampled' is the contract for a
	// video NOBODY reviewed; these were reviewed, so stamping it here would be a lie about who
	// decided them.
	assertStatus(t, ctx, pool, approved, "approved", false)
	assertStatus(t, ctx, pool, rejected, "rejected", false)
	for _, id := range []string{approved, rejected} {
		var verifiedBy *string
		if err := pool.QueryRow(ctx, `SELECT verified_by::text FROM verification_items WHERE item_id=$1::uuid`, id).Scan(&verifiedBy); err != nil {
			t.Fatalf("read verified_by: %v", err)
		}
		if verifiedBy == nil || *verifiedBy != verifier {
			t.Fatalf("item %s verified_by = %v, want the verifier who cast it", id, verifiedBy)
		}
	}

	// The closeout does not stamp a waiver over a decision she already made -- the same precedence
	// its own claim loop encodes.
	settled, err := repo.SettleUnsampledItems(ctx, ports.SettleUnsampledParams{
		TenantID: tenantID, Before: day.AddDate(0, 0, 1),
		WaivableCategories: []string{"vaccination_proof"}, Limit: 100,
	})
	if err != nil {
		t.Fatalf("SettleUnsampledItems: %v", err)
	}
	if settled != 0 {
		t.Fatalf("closeout settled %d already-decided item(s); her verdict must win", settled)
	}
	assertStatus(t, ctx, pool, approved, "approved", false)
	assertStatus(t, ctx, pool, rejected, "rejected", false)

	// And her day is unchanged by the extra work: at a 0% share she owed nothing, so an extra review
	// cannot push the panel past 100% or invent work she was never given.
	got := dayStats(t, ctx, repo, tenantID, biztime.BusinessDate(day))
	if got.Captured != 2 || got.Selected != 0 || got.Reviewed != 0 || got.AutoAccepted != 0 {
		t.Fatalf("panel = captured %d / drawn %d / reviewed %d / settled %d, want 2/0/0/0 -- an undrawn "+
			"review must not enter the share's own arithmetic", got.Captured, got.Selected, got.Reviewed, got.AutoAccepted)
	}
	if got.ProgressPercent() != 100 {
		t.Fatalf("progress = %d%%, want 100%% -- she owed nothing at a 0%% share", got.ProgressPercent())
	}
}
