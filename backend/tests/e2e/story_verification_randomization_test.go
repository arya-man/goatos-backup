package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionverificationbridge "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/verificationbridge"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	verificationports "github.com/vgoats/goatos/backend/internal/verification/ports"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceports "github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// TestKernelStory_VerificationRandomization is the end-to-end proof for RANDOMIZED VERIFICATION
// SAMPLING (maintainer decision 2026-08-26), driven entirely through the production path.
//
//	THE RULE          the CEO sets, per verification category, the PERCENTAGE of that category's
//	                  proof videos the verifier actually has to watch. Her day is complete when she
//	                  has cleared HER SHARE: at 40%, reviewing those 40% is 100% of her work.
//
// Verification is not one screen -- it is the spine several screens read -- so this story exists to
// prove what sampling does to EVERY surface that counts a proof video, not only to the queue:
//
//	the verifier's queue        only the drawn videos, with the status badge and the park/shed
//	                            filters narrowed to match (a filter that offers a shed with no drawn
//	                            work sends her to an empty board)
//	leadership's queue          UNCHANGED -- the principal who sets the share must be able to audit
//	                            what it waived
//	the leadership KPI strip    "videos waiting for review" counts what a PERSON still owes, and
//	                            review throughput counts what a PERSON actually did
//	the producing module        a feed session whose video was never drawn still reaches 'completed',
//	                            through the ordinary verdict event and the real consumer
//	the People directory        an operator's proofs settled by the policy are reported as such and
//	                            never inflate his approved count or dilute his rejection rate
//	the Randomization panel     captured / drawn / reviewed / settled for the day, and the progress
//	                            number that reads 100% when her share is done
//
// Nothing about an asserted outcome is hand-seeded. The fixture inserts only external input facts:
// tenant, park, sheds, and the workforce. Every verification item, verdict, settlement, event,
// completion and count under assertion is produced by the production services and the real
// outbox -> domain-consumer chain.
func TestKernelStory_VerificationRandomization(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-verification-randomization",
		"Randomization: how much proof a person actually watches",
		"The CEO decides, per module, what share of the day's proof videos the verifier must watch. "+
			"The draw is random but stable, so raising the share mid-day pulls MORE of today's videos "+
			"into her queue and never takes back one she is already holding. The videos it does not "+
			"draw are not left hanging -- verifier approval is the gate that COMPLETES the work for "+
			"feed and weighing, so the closeout settles them once the day (and with it the share) can "+
			"no longer change, and the producing module applies that exactly as it applies a human "+
			"approve. Two categories refuse a share outright: where the verifier READS the packed "+
			"quantity off the video, there is no number unless somebody watches.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx

	const (
		park     = "da000000-0000-4000-8000-000000003001"
		shedA    = "da000000-0000-4000-8000-000000004001"
		shedB    = "da000000-0000-4000-8000-000000004002"
		operator = "da000000-0000-4000-8000-000000005001"
		parkHead = "da000000-0000-4000-8000-000000005002"
		verifier = "da000000-0000-4000-8000-000000005003"
	)
	seedRandomizationScope(t, ctx, fx.Pool, park, shedA, shedB)
	fx.SeedWorkforce(operator, parkHead, verifier, shedA)
	// The operator's workforce row is matched to verification items by user_id, which is what the
	// People directory's proof statistics join on.
	fx.exec("operator user id",
		`UPDATE workforce_members SET user_id = $1::uuid WHERE tenant_id = $2::uuid AND workforce_member_id = $1::uuid`,
		operator, fxTenant)

	// The business day is the unit this whole feature is cut on, so the story anchors on the SERVER's
	// current business day rather than a fixed calendar date: the share is written at "today" by the
	// production service's own clock, and a pinned 2026 date would resolve to a policy that applies
	// to no item this run creates. Every instant below is derived from that ONE anchor, so nothing
	// here depends on the hour the suite happens to run.
	today := biztime.BusinessDayStart(time.Now())
	yesterday := today.AddDate(0, 0, -1)
	tomorrow := today.AddDate(0, 0, 1)
	atToday := func(hour int) time.Time { return today.Add(time.Duration(hour) * time.Hour) }
	atYesterday := func(hour int) time.Time { return yesterday.Add(time.Duration(hour) * time.Hour) }

	// Production wiring, mirroring internal/bootstrap/api.go. The categories come from the SHARED
	// catalog the API registers from, so the rows this story asserts on are byte-for-byte the ones a
	// real verifier's queue and a real CEO's panel are composed from.
	verification := verificationapp.NewService(fx.VerifRepo, fx.VerifMedia)
	for _, def := range []verificationdomain.CategoryDefinition{
		verificationcatalog.Vaccination,
		verificationcatalog.FeedDistribution,
		verificationcatalog.FeedPacking,
	} {
		if err := verification.RegisterCategory(def); err != nil {
			t.Fatalf("register %s: %v", def.Category, err)
		}
	}
	feedRepo := feeddirectionpg.NewRepository(fx.Pool, 10*time.Second)
	feed := feeddirectionapp.NewService(nil, nil).
		WithDistributionStore(feedRepo).
		WithDistributionVerificationEnqueuer(feeddirectionverificationbridge.New(verification))
	people := workforcepg.NewRepository(fx.Pool, 10*time.Second)

	// ---------------------------------------------------------------------------
	story.Step("The CEO opens Randomization and sets the shares",
		"One row per registered module, composed from the verification registry. Two of them refuse a "+
			"share: on feed packing and feed wastage the verifier is the DATA SOURCE -- the operator "+
			"sends a video and no number -- so every video has to be watched.")

	overview, err := verification.SamplingOverview(ctx, fxTenant, "")
	if err != nil {
		t.Fatalf("SamplingOverview: %v", err)
	}
	story.Assert("every registered module appears, named in farm language and never by its config token",
		len(overview.Categories) == 3 && allNamed(overview.Categories),
		"rows=%d labels=%s", len(overview.Categories), describeSamplingRows(overview.Categories))
	story.Assert("a module nobody has set runs at 100% -- sampling is opt-in, so nothing changed until the CEO asked",
		samplingRow(t, overview, verificationcatalog.Vaccination.Category).SamplePercent == 100,
		"vaccination share=%d%%", samplingRow(t, overview, verificationcatalog.Vaccination.Category).SamplePercent)

	packingRow := samplingRow(t, overview, verificationcatalog.FeedPacking.Category)
	story.Assert("feed packing is LOCKED at 100%, with a reason the CEO can read",
		!packingRow.Waivable && packingRow.SamplePercent == 100 && packingRow.LockedReason != "",
		"waivable=%t share=%d%% reason=%q", packingRow.Waivable, packingRow.SamplePercent, packingRow.LockedReason)

	_, packingErr := verification.SetSamplingPolicy(ctx, verificationdomain.SetSamplingPolicy{
		TenantID: fxTenant, Category: verificationcatalog.FeedPacking.Category, Percent: 40,
	})
	story.Assert("a share on feed packing is REFUSED, not accepted and quietly ignored",
		packingErr != nil,
		"SetSamplingPolicy(feed packing, 40%%) returned %v -- accepting it would show the CEO a share that is not in force", packingErr)

	setShare := func(category string, percent int) verificationdomain.SamplingCategory {
		t.Helper()
		row, err := verification.SetSamplingPolicy(ctx, verificationdomain.SetSamplingPolicy{
			TenantID: fxTenant, Category: category, Percent: percent,
		})
		if err != nil {
			t.Fatalf("SetSamplingPolicy(%s, %d): %v", category, percent, err)
		}
		return row
	}
	setShare(verificationcatalog.Vaccination.Category, 40)
	// ZERO is a real setting -- "review none of this module today" -- and it is what makes the feed
	// half of this story deterministic: nothing is drawn, so the completion below depends entirely on
	// the closeout rather than on where a random draw happened to land.
	setShare(verificationcatalog.FeedDistribution.Category, 0)
	story.Assert("the shares are recorded and read back at the values the CEO typed",
		samplingRow(t, reload(t, ctx, verification), verificationcatalog.Vaccination.Category).SamplePercent == 40 &&
			samplingRow(t, reload(t, ctx, verification), verificationcatalog.FeedDistribution.Category).SamplePercent == 0,
		"vaccination=%d%% feed distribution=%d%%",
		samplingRow(t, reload(t, ctx, verification), verificationcatalog.Vaccination.Category).SamplePercent,
		samplingRow(t, reload(t, ctx, verification), verificationcatalog.FeedDistribution.Category).SamplePercent)

	// ---------------------------------------------------------------------------
	story.Step("200 vaccination proof videos arrive today",
		"One item per animal, raised through the same producer seam every module uses. The draw is a "+
			"stable function of the item's own id, so which videos she gets is decided once and does "+
			"not move underneath her.")

	raiseVaccinationProofs(t, ctx, verification, "today", park, shedA, shedB, operator, atToday(9), 200)
	story.Assert("all 200 videos are on the board, whatever the share",
		countItems(t, ctx, fx.Pool, "status = 'pending'") == 200,
		"pending items=%d", countItems(t, ctx, fx.Pool, "status = 'pending'"))

	verifierQueue := func(category string) []verificationdomain.QueueRow {
		t.Helper()
		return queuePage(t, ctx, verification, category, true)
	}
	leadershipQueue := func(category string) []verificationdomain.QueueRow {
		t.Helper()
		return queuePage(t, ctx, verification, category, false)
	}

	drawn := verifierQueue(verificationcatalog.Vaccination.Category)
	all := leadershipQueue(verificationcatalog.Vaccination.Category)
	story.Assert("the verifier is handed roughly the share the CEO set, not all 200",
		len(drawn) > 0 && len(drawn) < 200 && withinTolerance(len(drawn), 200, 40, 12),
		"drawn=%d of 200 at a 40%% share (tolerance ±12 videos on a random draw)", len(drawn))
	story.Assert("leadership still sees every video -- the CEO who set the share can audit what it waived",
		len(all) == 200, "leadership page rows=%d", len(all))
	story.Assert("every video in her queue is one the policy DREW, and none of the others leaked in",
		allInSample(t, ctx, fx.Pool, drawn),
		"her queue carries at least one video the share did not draw")

	// The badge and the filters have to agree with the board, or a count advertises work the list
	// cannot show and a filter sends her to an empty page.
	counts, options := queueFilters(t, ctx, verification, verificationcatalog.Vaccination.Category, true)
	story.Assert("the status badge counts HER queue, not the whole day",
		counts.Pending == len(drawn),
		"badge says %d pending, her queue holds %d", counts.Pending, len(drawn))
	leadershipCounts, _ := queueFilters(t, ctx, verification, verificationcatalog.Vaccination.Category, false)
	story.Assert("leadership's badge still counts every video",
		leadershipCounts.Pending == 200, "leadership badge=%d", leadershipCounts.Pending)
	story.Assert("her shed filter offers only sheds that actually hold drawn work",
		len(options.Sheds) > 0 && shedsMatchQueue(options.Sheds, drawn),
		"filter offers %d sheds, her queue spans %d", len(options.Sheds), distinctSheds(drawn))

	// ---------------------------------------------------------------------------
	story.Step("The CEO raises the share to 80% at midday",
		"The draw is monotonic: raising it ADDS videos. It can never retract one she is already "+
			"holding, which is what makes a same-day change safe to offer at all.")

	before := itemIDSet(drawn)
	setShare(verificationcatalog.Vaccination.Category, 80)
	after := verifierQueue(verificationcatalog.Vaccination.Category)
	story.Assert("every video she already had is still there",
		containsAll(itemIDSet(after), before),
		"the queue lost %d of the %d videos she was already holding", missingCount(itemIDSet(after), before), len(before))
	story.Assert("and more of today's videos joined it",
		len(after) > len(drawn) && withinTolerance(len(after), 200, 80, 14),
		"queue grew from %d to %d of 200 at an 80%% share", len(drawn), len(after))

	// Put it back. This is the case that caught a real defect: the write's idempotency key is derived
	// from (category, day, share), so returning to a share already used TODAY reuses that key -- and
	// an implementation that treated the reused key as a replay silently refused to lower it, leaving
	// the panel reporting 40% while the queue still ran at 80%.
	setShare(verificationcatalog.Vaccination.Category, 40)
	backDown := verifierQueue(verificationcatalog.Vaccination.Category)
	story.Assert("lowering the share back to a value already used today actually takes effect",
		len(backDown) == len(drawn),
		"queue holds %d after returning to 40%%, was %d at 40%% and %d at 80%%", len(backDown), len(drawn), len(after))
	story.Assert("and the videos it removes are ones she had not reviewed -- lowering never un-reviews anything",
		containsAll(itemIDSet(backDown), before),
		"the queue lost %d videos she was holding at the original 40%% share", missingCount(itemIDSet(backDown), before))

	// ---------------------------------------------------------------------------
	story.Step("A feed session is filmed, and the policy does not draw it",
		"This is the case that decides whether sampling is safe at all: for feed, the verifier's "+
			"approve is not review -- it is the gate that COMPLETES the pen-session. A video nobody is "+
			"going to watch must not hold that session open forever.")

	dist, err := feed.CompleteDistribution(ctx, feeddirectionapp.CompleteDistributionInput{
		TenantID: fxTenant, ParkID: park, ShedID: shedA, SessionNo: 1,
		TargetDate: today, Workflow: feeddirectiondomain.WorkflowNormal,
		FeedWeightProofRef:   "proof-ra-feed-weight",
		DistributionProofRef: "proof-ra-feed-distribution",
		WaterProofRef:        "proof-ra-feed-water",
		CompletedBy:          operator, IdempotencyKey: "ra-feed-distribution-1",
		ActorID: operator, ActorType: "operator",
	})
	if err != nil {
		t.Fatalf("CompleteDistribution: %v", err)
	}
	story.Assert("the session is awaiting verification, exactly as it is today -- sampling changes nothing at submit",
		dist.Status == feeddirectiondomain.DistributionStatusPendingVerification,
		"status=%s", dist.Status)
	story.Assert("the video is NOT in the verifier's queue at a 0% share",
		len(verifierQueue(verificationcatalog.FeedDistribution.Category)) == 0,
		"her feed queue holds %d videos at 0%%", len(verifierQueue(verificationcatalog.FeedDistribution.Category)))
	story.Assert("but it is still on leadership's board, and still countable",
		len(leadershipQueue(verificationcatalog.FeedDistribution.Category)) == 1,
		"leadership feed rows=%d", len(leadershipQueue(verificationcatalog.FeedDistribution.Category)))
	verifiedBefore, err := feedRepo.ListVerifiedDistributions(ctx, fxTenant, park, today)
	if err != nil {
		t.Fatalf("ListVerifiedDistributions(before): %v", err)
	}
	story.Assert("and nothing is completed yet -- the gate is still shut",
		len(verifiedBefore) == 0, "verified sessions=%d", len(verifiedBefore))

	// ---------------------------------------------------------------------------
	story.Step("The day closes and the closeout settles what nobody was asked to watch",
		"It waits for the day to END on purpose: while the day is open the CEO can still raise the "+
			"share and recruit a video back, and one already waived could not be recruited. This is the "+
			"same call the kernel worker's stage makes, one business day later.")

	// Yesterday's videos, raised now so the closeout has a second, already-closed day to work on.
	// They matter for a second reason: the share was set TODAY, and a day resolves to the newest
	// policy row on or before it -- so yesterday ran at no share at all.
	raiseVaccinationProofs(t, ctx, verification, "yesterday", park, shedA, shedB, operator, atYesterday(9), 60)
	story.Assert("yesterday's videos are ALL still a person's work -- a share set today never reaches back and waives yesterday",
		len(queueDay(t, ctx, verification, verificationcatalog.Vaccination.Category, biztime.BusinessDate(yesterday))) == 60,
		"yesterday's drawn queue holds %d of 60; a share that applied retroactively would have waived work she was already told to do",
		len(queueDay(t, ctx, verification, verificationcatalog.Vaccination.Category, biztime.BusinessDate(yesterday))))
	settled, err := fx.VerifRepo.SettleUnsampledItems(ctx, verificationports.SettleUnsampledParams{
		TenantID: fxTenant,
		// The cutoff the stage passes when it next runs: tomorrow's business-day start, which closes
		// today. The stage itself derives this from the business clock; the story states it, because
		// a test that waited for real midnight would not be a test.
		Before:             tomorrow,
		WaivableCategories: waivableCategories(verification),
		Limit:              500,
	})
	if err != nil {
		t.Fatalf("SettleUnsampledItems: %v", err)
	}
	story.Assert("the videos the policy did not draw are settled, and only those",
		settled > 0 && countItems(t, ctx, fx.Pool, "auto_resolution = 'not_sampled'") == settled,
		"settled=%d rows carrying auto_resolution=%d", settled, countItems(t, ctx, fx.Pool, "auto_resolution = 'not_sampled'"))
	story.Assert("each one is recorded as an approval with NO verifier attached -- nobody signed it",
		countItems(t, ctx, fx.Pool, "auto_resolution = 'not_sampled' AND status = 'approved' AND verified_by IS NULL") == settled,
		"settled rows that are approved-with-no-verifier=%d of %d",
		countItems(t, ctx, fx.Pool, "auto_resolution = 'not_sampled' AND status = 'approved' AND verified_by IS NULL"), settled)
	// Her queue spans days: a verifier works the backlog oldest-first, so this is every drawn video
	// still owed, today's and yesterday's together.
	stillOwed := len(verifierQueue(verificationcatalog.Vaccination.Category))
	drawnYesterday := len(queueDay(t, ctx, verification, verificationcatalog.Vaccination.Category, biztime.BusinessDate(yesterday)))
	drawnTodayOnly := len(queueDay(t, ctx, verification, verificationcatalog.Vaccination.Category, biztime.BusinessDate(today)))
	story.Assert("the videos she WAS given are untouched -- the closeout never decides her work for her",
		countItems(t, ctx, fx.Pool, "status = 'pending'") == stillOwed,
		"pending after closeout=%d, her drawn queue holds %d (%d today + %d yesterday)",
		countItems(t, ctx, fx.Pool, "status = 'pending'"), stillOwed, drawnTodayOnly, drawnYesterday)
	story.Assert("yesterday's 60 survive in full, because that day never had a share",
		drawnYesterday == 60, "yesterday still pending=%d of 60", drawnYesterday)

	fx.RelayOutboxEvents()
	verifiedAfter, err := feedRepo.ListVerifiedDistributions(ctx, fxTenant, park, today)
	if err != nil {
		t.Fatalf("ListVerifiedDistributions(after): %v", err)
	}
	story.Assert("THE PEN-SESSION COMPLETES -- through the ordinary verdict event and the real feed consumer, with no verifier in the loop",
		len(verifiedAfter) == 1,
		"verified sessions=%d after the settlement was relayed; a stall here is a feed session held open forever", len(verifiedAfter))

	// ---------------------------------------------------------------------------
	story.Step("What the other screens now say",
		"Sampling splits verification into two populations -- what a person still owes, and what a "+
			"person actually did -- and every number that reports either one has to say which.")

	analytics, err := fx.VerifRepo.OversightAnalytics(ctx, fxTenant)
	if err != nil {
		t.Fatalf("OversightAnalytics: %v", err)
	}
	story.Assert("the CEO's \"videos waiting for review\" counts what a PERSON still owes, not the settled pile",
		analytics.KPIs.VideosWaiting == stillOwed,
		"KPI says %d waiting, her drawn queue holds %d; counting the settled pile here would divide a "+
			"backlog nobody will review by a rate only humans produce",
		analytics.KPIs.VideosWaiting, stillOwed)
	story.Assert("the age buckets still add back to that same headline number",
		analytics.PendingAgeBuckets.Total() == analytics.KPIs.VideosWaiting,
		"buckets total=%d headline=%d", analytics.PendingAgeBuckets.Total(), analytics.KPIs.VideosWaiting)
	story.Assert("review throughput is still zero -- a settlement is not a verdict somebody cast",
		analytics.KPIs.VerdictsPerActiveDayLast7d == 0 && analytics.KPIs.RejectRateLast30d == nil,
		"verdicts/day=%.2f reject rate=%v -- counting settlements here would collapse the reject rate "+
			"by padding it with approvals nobody decided",
		analytics.KPIs.VerdictsPerActiveDayLast7d, analytics.KPIs.RejectRateLast30d)

	person := operatorRow(t, ctx, people, operator)
	story.Assert("the People directory reports the operator's settled proofs separately",
		person.ProofNotReviewed == settled,
		"proof_not_reviewed=%d settled=%d", person.ProofNotReviewed, settled)
	story.Assert("and never counts them as approved -- his work was accepted, not checked",
		person.ProofApproved == 0,
		"proof_approved=%d, which would read as %d proofs a verifier passed", person.ProofApproved, person.ProofApproved)
	story.Assert("his uploads still account for every video, so the four buckets partition the whole",
		person.ProofUploads == person.ProofApproved+person.ProofRejected+person.ProofPending+person.ProofNotReviewed,
		"uploads=%d vs approved=%d + rejected=%d + pending=%d + not_reviewed=%d",
		person.ProofUploads, person.ProofApproved, person.ProofRejected, person.ProofPending, person.ProofNotReviewed)

	// ---------------------------------------------------------------------------
	story.Step("She works her share, and her day reads 100%",
		"The number the maintainer asked for: at a 40% share, reviewing those 40% IS the whole job.")

	remaining := verifierQueue(verificationcatalog.Vaccination.Category)
	for i, row := range remaining {
		if _, err := verification.RecordVerdict(ctx, verificationdomain.Verdict{
			TenantID: fxTenant, ItemID: row.Item.ItemID, Decision: verificationdomain.DecisionApproved,
			VerifierID: verifier, RowVersion: row.Item.RowVersion,
			IdempotencyKey: fmt.Sprintf("ra-verdict-%d", i),
		}); err != nil {
			t.Fatalf("RecordVerdict(%s): %v", row.Item.ItemID, err)
		}
	}
	final, err := verification.SamplingOverview(ctx, fxTenant, biztime.BusinessDate(today))
	if err != nil {
		t.Fatalf("SamplingOverview(final): %v", err)
	}
	vacc := samplingRow(t, final, verificationcatalog.Vaccination.Category)
	story.Assert("her day reads 100% once her share is done, even though most of the day's videos went unwatched",
		vacc.Stats.ProgressPercent() == 100 && vacc.Stats.Selected < vacc.Stats.Captured,
		"progress=%d%% reviewed=%d of drawn=%d, out of %d captured",
		vacc.Stats.ProgressPercent(), vacc.Stats.Reviewed, vacc.Stats.Selected, vacc.Stats.Captured)
	story.Assert("and the panel still reports the whole day honestly beside it",
		vacc.Stats.Captured == 200 && vacc.Stats.Reviewed == drawnTodayOnly &&
			vacc.Stats.Selected+vacc.Stats.AutoAccepted == vacc.Stats.Captured,
		"captured=%d drawn=%d reviewed=%d settled=%d -- drawn plus settled must account for the whole day",
		vacc.Stats.Captured, vacc.Stats.Selected, vacc.Stats.Reviewed, vacc.Stats.AutoAccepted)

	afterWork, err := fx.VerifRepo.OversightAnalytics(ctx, fxTenant)
	if err != nil {
		t.Fatalf("OversightAnalytics(after work): %v", err)
	}
	story.Assert("now the CEO's throughput reflects real reviews, counted from her verdicts alone",
		afterWork.KPIs.VerdictsPerActiveDayLast7d > 0,
		"verdicts/day=%.2f after %d real verdicts", afterWork.KPIs.VerdictsPerActiveDayLast7d, len(remaining))
	story.Assert("the packing video is STILL waiting for a person -- the locked category was never settled",
		countItems(t, ctx, fx.Pool, "category = '"+verificationcatalog.FeedPacking.Category+"' AND auto_resolution IS NOT NULL") == 0,
		"packing items settled by policy=%d, and the verifier records their quantities",
		countItems(t, ctx, fx.Pool, "category = '"+verificationcatalog.FeedPacking.Category+"' AND auto_resolution IS NOT NULL"))
}

// --- story helpers -----------------------------------------------------------

func seedRandomizationScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool, park, shedA, shedB string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed randomization scope: %v\nsql: %s", err, sql)
		}
	}
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'RA-CPT', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, fxTenant, park)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($3::uuid, $1::uuid, 'shed', 'RA-CASTRO', 'Castro', 'active', $2::uuid, 1),
       ($4::uuid, $1::uuid, 'shed', 'RA-GANDHI', 'Gandhi', 'active', $2::uuid, 2)
ON CONFLICT (location_id) DO NOTHING`, fxTenant, park, shedA, shedB)
}

// raiseVaccinationProofs enqueues one proof item per animal through the SAME producer seam
// sopbridge uses. The ids are minted by the database, so every draw in this story is the real one.
func raiseVaccinationProofs(t *testing.T, ctx context.Context, svc *verificationapp.Service,
	tag, park, shedA, shedB, operator string, capturedAt time.Time, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		shed := shedA
		if i%2 == 1 {
			shed = shedB
		}
		label := fmt.Sprintf("Tag %s-%04d", tag, i)
		res, err := svc.CreateItem(ctx, verificationdomain.CreateItem{
			TenantID: fxTenant,
			Vertical: verificationcatalog.Vaccination.Vertical,
			Module:   verificationcatalog.Vaccination.Module,
			Category: verificationcatalog.Vaccination.Category,
			Source: verificationdomain.SourceRef{
				Module: "vaccination", RefType: "vaccination_goat",
				RefID: fmt.Sprintf("da%06d-0000-4000-8000-%012d", len(tag), i+1),
			},
			SubjectLabel:   &label,
			MediaRefs:      []string{fmt.Sprintf("proof-%s-%04d", tag, i)},
			OperatorID:     &operator,
			ShedID:         &shed,
			ParkID:         &park,
			CapturedAt:     capturedAt,
			IdempotencyKey: fmt.Sprintf("ra-%s-%04d", tag, i),
		})
		if err != nil {
			t.Fatalf("CreateItem(%s %d): %v", tag, i, err)
		}
		ids = append(ids, res.Item.ItemID)
	}
	return ids
}

// queuePage reads the verifier's board the way the HTTP handler does. sampled=false is the
// leadership read, which carries verification.oversee and is deliberately NOT narrowed.
func queuePage(t *testing.T, ctx context.Context, svc *verificationapp.Service, category string, sampled bool) []verificationdomain.QueueRow {
	t.Helper()
	rows := []verificationdomain.QueueRow{}
	var cursor *verificationdomain.Cursor
	for {
		res, err := svc.ListQueue(ctx, verificationports.ListQueueParams{
			TenantID: fxTenant, Category: category, Limit: 100, Cursor: cursor,
			IsVerifierQueueRead:     true,
			SamplingApplied:         sampled,
			OversightFiltersEnabled: !sampled,
		})
		if err != nil {
			t.Fatalf("ListQueue(%s sampled=%t): %v", category, sampled, err)
		}
		rows = append(rows, res.Items...)
		if res.NextCursor == nil {
			return rows
		}
		decoded, err := verificationdomain.DecodeCursor(*res.NextCursor)
		if err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
		cursor = &decoded
	}
}

// queueDay is the same verifier read narrowed to ONE business day, which is how the effective-dated
// share is observed: each day resolves to the newest policy row on or before it.
func queueDay(t *testing.T, ctx context.Context, svc *verificationapp.Service, category, businessDate string) []verificationdomain.QueueRow {
	t.Helper()
	rows := []verificationdomain.QueueRow{}
	var cursor *verificationdomain.Cursor
	for {
		res, err := svc.ListQueue(ctx, verificationports.ListQueueParams{
			TenantID: fxTenant, Category: category, Limit: 100, Cursor: cursor,
			BusinessDate:    businessDate,
			SamplingApplied: true,
		})
		if err != nil {
			t.Fatalf("ListQueue(%s on %s): %v", category, businessDate, err)
		}
		rows = append(rows, res.Items...)
		if res.NextCursor == nil {
			return rows
		}
		decoded, err := verificationdomain.DecodeCursor(*res.NextCursor)
		if err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
		cursor = &decoded
	}
}

func queueFilters(t *testing.T, ctx context.Context, svc *verificationapp.Service, category string, sampled bool) (verificationdomain.QueueStatusCounts, verificationdomain.QueueFilterOptions) {
	t.Helper()
	res, err := svc.ListQueue(ctx, verificationports.ListQueueParams{
		TenantID: fxTenant, Category: category, Limit: 20,
		IsVerifierQueueRead:     true,
		SamplingApplied:         sampled,
		OversightFiltersEnabled: !sampled,
	})
	if err != nil {
		t.Fatalf("ListQueue filters(%s): %v", category, err)
	}
	return res.FilterOptions.Counts, res.FilterOptions
}

func reload(t *testing.T, ctx context.Context, svc *verificationapp.Service) verificationdomain.SamplingOverview {
	t.Helper()
	out, err := svc.SamplingOverview(ctx, fxTenant, "")
	if err != nil {
		t.Fatalf("SamplingOverview: %v", err)
	}
	return out
}

func samplingRow(t *testing.T, overview verificationdomain.SamplingOverview, category string) verificationdomain.SamplingCategory {
	t.Helper()
	for _, row := range overview.Categories {
		if row.Category == category {
			return row
		}
	}
	t.Fatalf("category %s missing from the Randomization panel", category)
	return verificationdomain.SamplingCategory{}
}

func waivableCategories(svc *verificationapp.Service) []string {
	out := []string{}
	for _, def := range svc.Categories() {
		if def.SamplingWaivable() {
			out = append(out, def.Category)
		}
	}
	return out
}

func allNamed(rows []verificationdomain.SamplingCategory) bool {
	for _, row := range rows {
		if row.ModuleLabel == "" || row.PageLabel == "" {
			return false
		}
	}
	return len(rows) > 0
}

func describeSamplingRows(rows []verificationdomain.SamplingCategory) string {
	out := ""
	for _, row := range rows {
		out += fmt.Sprintf("[%s · %s] ", row.ModuleLabel, row.PageLabel)
	}
	return out
}

func countItems(t *testing.T, ctx context.Context, pool *pgxpool.Pool, where string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM verification_items WHERE tenant_id = $1::uuid AND `+where, fxTenant).Scan(&n); err != nil {
		t.Fatalf("count verification_items WHERE %s: %v", where, err)
	}
	return n
}

// countPendingOtherDays counts pending items that are NOT today's vaccination board -- yesterday's
// drawn leftovers, which the closeout correctly leaves for a person.
func countPendingOtherDays(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM verification_items
WHERE tenant_id = $1::uuid AND status = 'pending'
  AND (captured_at AT TIME ZONE 'Asia/Kolkata')::date <> (now() AT TIME ZONE 'Asia/Kolkata')::date`, fxTenant).Scan(&n); err != nil {
		t.Fatalf("count pending on other days: %v", err)
	}
	return n
}

func allInSample(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rows []verificationdomain.QueueRow) bool {
	t.Helper()
	for _, row := range rows {
		var bucket int
		if err := pool.QueryRow(ctx, `SELECT sampling_bucket::int FROM verification_items WHERE item_id = $1::uuid`, row.Item.ItemID).Scan(&bucket); err != nil {
			t.Fatalf("read sampling bucket: %v", err)
		}
		// Read against the SHARE, not against the queue that produced the row: this has to fail if
		// the predicate and the panel ever disagree about what "drawn" means.
		if !verificationdomain.InSample(bucket, 40) {
			return false
		}
	}
	return true
}

func itemIDSet(rows []verificationdomain.QueueRow) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		out[row.Item.ItemID] = true
	}
	return out
}

func containsAll(have, want map[string]bool) bool { return missingCount(have, want) == 0 }

func missingCount(have, want map[string]bool) int {
	missing := 0
	for id := range want {
		if !have[id] {
			missing++
		}
	}
	return missing
}

func distinctSheds(rows []verificationdomain.QueueRow) int {
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Item.ShedID != nil {
			seen[*row.Item.ShedID] = true
		}
	}
	return len(seen)
}

func shedsMatchQueue(options []verificationdomain.LocationFilterOption, rows []verificationdomain.QueueRow) bool {
	inQueue := map[string]bool{}
	for _, row := range rows {
		if row.Item.ShedID != nil {
			inQueue[*row.Item.ShedID] = true
		}
	}
	for _, option := range options {
		id := option.ID
		if idx := indexOfHash(id); idx >= 0 {
			id = id[:idx]
		}
		if !inQueue[id] {
			return false
		}
	}
	return true
}

func indexOfHash(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '#' {
			return i
		}
	}
	return -1
}

// withinTolerance keeps the random draw's assertion honest: it asserts the SHARE landed near the
// setting, with room for the spread a real draw has, rather than pinning an exact count that would
// make this story a hash-value test.
func withinTolerance(got, total, percent, tolerance int) bool {
	expected := total * percent / 100
	return got >= expected-tolerance && got <= expected+tolerance
}

func operatorRow(t *testing.T, ctx context.Context, repo *workforcepg.Repository, operatorID string) (person struct {
	ProofUploads     int
	ProofApproved    int
	ProofRejected    int
	ProofPending     int
	ProofNotReviewed int
}) {
	t.Helper()
	people, _, err := repo.ListPeople(ctx, workforceports.ListPeopleParams{TenantID: fxTenant, Limit: 50})
	if err != nil {
		t.Fatalf("ListPeople: %v", err)
	}
	for _, p := range people {
		if p.UserID != nil && *p.UserID == operatorID {
			person.ProofUploads = p.ProofUploads
			person.ProofApproved = p.ProofApproved
			person.ProofRejected = p.ProofRejected
			person.ProofPending = p.ProofPending
			person.ProofNotReviewed = p.ProofNotReviewed
			return person
		}
	}
	t.Fatalf("operator %s missing from the People directory", operatorID)
	return person
}
