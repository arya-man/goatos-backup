package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	feeddirectioncounts "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/counts"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionverificationbridge "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/verificationbridge"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// TestKernelStory_FeedAfternoonCorrection is the end-to-end proof for the two 2026-08-10 feed
// decisions, driven entirely through the production path:
//
//	PEN-DAY PACKING GRAIN     a packer packs a pen's whole day in one go and films it ONCE, so the
//	                          worklist serves ONE card per pen carrying Morning and Evening as a
//	                          breakdown, backed by ONE completion and ONE verification item.
//
//	AFTERNOON FEED CORRECTION a movement RAISED but not yet approved counts toward the feed sheet,
//	                          and the 14:00 correction reopens any pen already packed against the
//	                          old head count.
//
// The chain it walks is the real one, hour by hour on one business day:
//
//	07:00  feeddirection.IssueDirection            freezes tomorrow's normal sheet
//	09:00  feeddirection.CompletePacking           operator's ONE video -> pending_verification
//	                                               -> verificationbridge.NewPacking -> a real item
//	09:30  verification.RecordVerdict(approved)    -> outbox -> FeedPackingVerificationHandler
//	                                               -> ApplyVerifiedPacking -> 'completed'
//	10:00  counts: a LOW-PRIORITY shifting is RAISED into that pen and NOBODY APPROVES IT
//	14:00  feeddirection.AmendDirection            recomputes including the unapproved movement,
//	                                               reopens the pen, withdraws its verification item
//	14:01  feeddirection.PackingWorklist           serves the NEW quantities + the reason
//	14:30  feeddirection.CompletePacking           the re-shoot -> a FRESH verification item
//
// Nothing about an asserted outcome is hand-seeded. The fixture inserts only external input facts:
// tenant, park, sheds, the pen catalog, live goats, the authored feed config and dispatch clock, and
// the raised movement. Every quantity, status, verification item and reason under assertion is
// produced by the production services above.
func TestKernelStory_FeedAfternoonCorrection(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-feed-afternoon-correction",
		"Feed: one video per pen per day, and the afternoon correction that takes it back",
		"A packer packs a pen's whole day in ONE go and films it ONCE, so the worklist serves one card "+
			"per pen with Morning and Evening as a breakdown inside it. But a low-priority movement raised "+
			"at 10:00 is due TOMORROW, and tomorrow's sheet was issued at 07:00 and was already packed and "+
			"verified by 09:30 -- so the animals arriving tomorrow would have had no feed. The 14:00 "+
			"correction now counts that movement BEFORE a park head approves it, recomputes the pen, takes "+
			"the already-approved video back, withdraws the verifier's closed item, and hands the packer a "+
			"card carrying the NEW amounts and a sentence saying why. Experiment pens, whose rations are "+
			"authored in absolute kg, are left alone.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx

	const (
		park       = "fe000000-0000-4000-8000-000000003001"
		shedCastro = "fe000000-0000-4000-8000-000000004001"
		shedTrial  = "fe000000-0000-4000-8000-000000004002"
		operator   = "fe000000-0000-4000-8000-000000005001"
		verifier   = "fe000000-0000-4000-8000-000000005002"
	)

	// Castro is subdivided: pens 1 and 2 hold different animals. The pen is part of the completion's
	// identity, so a video shot in Castro - 1 must never close out Castro - 2 (migration 000137), and
	// a correction that re-counts one pen must leave its sibling alone.
	seedFeedCorrectionScope(t, ctx, fx.Pool, park, shedCastro, shedTrial)
	seedFeedCorrectionGoats(t, ctx, fx.Pool, shedCastro, "1", 40)
	seedFeedCorrectionGoats(t, ctx, fx.Pool, shedCastro, "2", 30)

	// The production service wiring, mirroring internal/bootstrap/api.go: one repository owns the
	// config reads, the frozen issue tables, the dispatch clock and the packing completions, and the
	// counts census is reached through the same adapter production uses rather than a fake.
	feedRepo := feeddirectionpg.NewRepository(fx.Pool, 10*time.Second)
	countsRepo := countspg.NewRepository(fx.Pool, 10*time.Second)
	verification := verificationapp.NewService(fx.VerifRepo, fx.VerifMedia)
	// The SAME category definition bootstrap registers, so the item this story asserts on is the one
	// the verifier's real queue would list. CreateItem rejects an unregistered category outright.
	if err := verification.RegisterCategory(verificationdomain.CategoryDefinition{
		Vertical: feeddirectiondomain.VerificationVerticalFeed, Module: feeddirectiondomain.VerificationModuleFeed,
		Category:         feeddirectiondomain.VerificationCategoryPacking,
		ExpectedMedia:    []string{"video"},
		MediaLabels:      []string{"Feed packing video"},
		NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
		PageKey: "feed_packing", PageLabel: "Feed Packing", PageOrder: 2,
	}); err != nil {
		t.Fatalf("register feed packing verification category: %v", err)
	}
	feed := feeddirectionapp.NewService(feedRepo, feeddirectioncounts.NewReader(countsRepo)).
		WithIssueStore(feedRepo).
		WithScheduleReader(feedRepo).
		WithPackingStore(feedRepo).
		WithGeneratedBy("goatos-e2e")
	// The serve path has no per-request clock, so business time is advanced by moving the service's
	// own clock -- the same seam production leaves at time.Now. Every instant is a FIXED India wall
	// clock: a now-relative fixture would pass or fail depending on the hour the suite happened to
	// run, which is the exact defect class the business-day rules exist to prevent.
	clock := &e2eClock{}
	feed.WithClock(clock.Now)
	feed.WithPackingVerificationEnqueuer(feeddirectionverificationbridge.NewPacking(verification))

	// One fixed business day. Every instant below is an India wall clock: a UTC-derived hour would
	// put the 13:30 cutoff on the wrong side and the whole rule would read backwards.
	at := func(hour, minute int) time.Time {
		return time.Date(2026, time.July, 29, hour, minute, 0, 0, biztime.DefaultLocation())
	}
	const feedDay = "2026-07-30" // feed for day D is packed on D-1

	issueReq := func(workflow string, asOf time.Time) feeddirectionapp.IssueRequest {
		// The lifecycle stamps from AsOf, but the generation path's past-date guard reads the service
		// clock, so both must agree or a legitimately dated sheet is refused as a regeneration.
		clock.Set(asOf)
		return feeddirectionapp.IssueRequest{
			TenantID: fxTenant, ParkID: park, Workflow: workflow, AsOf: asOf,
		}
	}

	// -----------------------------------------------------------------------
	story.Step("07:00 — the sheet is issued and frozen",
		"The normal workflow's direction_time is 07:00, so tomorrow's sheet is generated once and "+
			"frozen. From here the packer is working to a fixed document.")

	issued, err := feed.IssueDirection(ctx, issueReq(feeddirectiondomain.WorkflowNormal, at(7, 0)))
	if err != nil {
		t.Fatalf("IssueDirection: %v", err)
	}
	story.Assert("the normal sheet for tomorrow is issued",
		issued.FeedDay == feedDay && issued.Header.State == feeddirectiondomain.IssueStateIssued,
		"feed_day=%s state=%s", issued.FeedDay, issued.Header.State)

	worklist := func(asOf time.Time) feeddirectiondomain.PackingPage {
		t.Helper()
		clock.Set(asOf)
		page, err := feed.PackingWorklist(ctx, feeddirectiondomain.PackingQuery{
			TenantID: fxTenant, ParkID: park, TargetDate: feedDayTime(t), Limit: 50,
		})
		if err != nil {
			t.Fatalf("PackingWorklist: %v", err)
		}
		return page
	}
	penRow := func(page feeddirectiondomain.PackingPage, partition string) feeddirectiondomain.PackingRow {
		t.Helper()
		for _, row := range page.Items {
			if row.ShedID == shedCastro &&
				feeddirectiondomain.PartitionMatchKey(row.PartitionLabel) == feeddirectiondomain.PartitionMatchKey(partition) &&
				row.Workflow == feeddirectiondomain.WorkflowNormal {
				return row
			}
		}
		t.Fatalf("no packing row for Castro pen %q in %d rows", partition, len(page.Items))
		return feeddirectiondomain.PackingRow{}
	}

	before := worklist(at(8, 0))
	pen1 := penRow(before, "1")

	// PEN-DAY GRAIN. Before 2026-08-10 this pen appeared TWICE -- once for Morning, once for Evening --
	// and the packer was asked to film the same work twice.
	story.Assert("the pen appears ONCE, carrying both sessions as a breakdown",
		len(pen1.Sessions) == 2 && countCastroRows(before, shedCastro, "1") == 1,
		"rows for Castro - 1 = %d, sessions on the row = %d (%s)",
		countCastroRows(before, shedCastro, "1"), len(pen1.Sessions), sessionSummary(pen1))
	story.Assert("the pen's head count is counted once for the DAY, never per session",
		pen1.HeadCount == 40, "head_count=%d, want the pen's 40 animals (not 80)", pen1.HeadCount)
	story.Assert("the sibling pen is a separate card with its own animals",
		penRow(before, "2").HeadCount == 30, "Castro - 2 head_count=%d", penRow(before, "2").HeadCount)
	story.Assert("the backend composes the operational location, shed and pen together",
		pen1.OperationalLocationDisplay == "Castro - 1",
		"operational_location_display=%q", pen1.OperationalLocationDisplay)

	originalTotal := pen1.TotalKg

	// -----------------------------------------------------------------------
	story.Step("09:00 — the operator packs Castro - 1 and films it ONCE",
		"One mandatory video covers the pen's whole day. It becomes exactly one item in the verifier's "+
			"queue, subjected on the PEN with no session prefix.")

	complete := func(partition, proof, key string, asOf time.Time) feeddirectionports.CompletePackingResult {
		t.Helper()
		clock.Set(asOf)
		res, err := feed.CompletePacking(ctx, feeddirectionapp.CompletePackingInput{
			TenantID: fxTenant, ParkID: park, ShedID: shedCastro, PartitionLabel: partition,
			TargetDate: feedDayTime(t), Workflow: feeddirectiondomain.WorkflowNormal,
			PackingProofRef: proof, CompletedBy: operator, IdempotencyKey: key,
			ActorID: operator, ActorType: "operator",
		})
		if err != nil {
			t.Fatalf("CompletePacking(%s): %v", partition, err)
		}
		return res
	}

	first := complete("1", "proof-castro-1-morning-and-evening", "feed-pack-castro-1-first", at(9, 0))
	story.Assert("the completion is awaiting verification, not completed",
		first.Status == feeddirectiondomain.PackingStatusPendingVerification,
		"status=%s completion_id=%s", first.Status, first.CompletionID)

	items := listPackingItems(t, ctx, fx.Pool, first.CompletionID)
	story.Assert("exactly ONE verification item was queued for the pen-day",
		len(items) == 1, "verification items for this completion = %d", len(items))
	if len(items) == 1 {
		story.Assert("the verifier's item is subjected on the PEN, with no session prefix",
			items[0].subject == "Castro - 1",
			"subject_label=%q — a 'Session 1 · ' prefix would name half the work the verifier is judging whole",
			items[0].subject)
	}

	// -----------------------------------------------------------------------
	story.Step("09:30 — the verifier approves it",
		"The verdict travels the real outbox -> domain-consumer -> FeedPackingVerificationHandler path, "+
			"which is what actually flips the pen to completed and emits feed.packing.completed.")

	if len(items) == 1 {
		// RowVersion is the optimistic-concurrency fence the real verdict route also carries: a verdict
		// cast against a stale view of the item is refused rather than silently overwriting a newer one.
		if _, err := verification.RecordVerdict(ctx, verificationdomain.Verdict{
			TenantID: fxTenant, ItemID: items[0].id, Decision: verificationdomain.DecisionApproved,
			VerifierID: verifier, Reason: "bags match the sheet",
			RowVersion:     verificationItemRowVersion(t, ctx, fx.Pool, items[0].id),
			IdempotencyKey: "fe-verdict-castro-1",
		}); err != nil {
			t.Fatalf("RecordVerdict(approved): %v", err)
		}
	}
	fx.RelayOutboxEvents()

	afterApproval := worklist(at(9, 45))
	story.Assert("the pen reads completed once a verifier has approved the video",
		penRow(afterApproval, "1").Completed,
		"lifecycle_status=%s completed=%t",
		penRow(afterApproval, "1").LifecycleStatus, penRow(afterApproval, "1").Completed)

	// -----------------------------------------------------------------------
	story.Step("10:00 — a low-priority movement is RAISED, and nobody approves it",
		"This is the situation the whole change exists for. The movement is due TOMORROW, which is the "+
			"feed day that was issued at 07:00 and packed at 09:00. Under the previous rule the projection "+
			"waited for a park head, so the arriving animals had no feed at all.")

	raiseShiftingInto(t, ctx, fx.Pool, park, shedCastro, "1", at(10, 0), 10)
	story.Assert("the movement sits unapproved in the park head's queue",
		shiftingAuthState(t, ctx, fx.Pool) == "pending",
		"authorization_state=%s", shiftingAuthState(t, ctx, fx.Pool))

	// -----------------------------------------------------------------------
	story.Step("14:00 — the afternoon correction recomputes and takes the video back",
		"correction_time is 14:00 for both workflows and needs no new clock. The transport LOCK is "+
			"15:30, AFTER this, so nothing had to be unlocked -- amending a locked sheet is still refused, "+
			"because past the transport cutoff the feed has physically left the store.")

	amended, err := feed.AmendDirection(ctx, issueReq(feeddirectiondomain.WorkflowNormal, at(14, 0)))
	if err != nil {
		t.Fatalf("AmendDirection: %v", err)
	}
	story.Assert("the correction reopened exactly ONE pen — the one whose animal count moved",
		len(amended.ReopenedPackingCompletionIDs) == 1 &&
			amended.ReopenedPackingCompletionIDs[0] == first.CompletionID,
		"reopened=%v, want only Castro - 1 (%s); Castro - 2 did not move and its packer must not refilm",
		amended.ReopenedPackingCompletionIDs, first.CompletionID)

	corrected := worklist(at(14, 1))
	pen1After := penRow(corrected, "1")

	story.Assert("the pen is back with the operator",
		pen1After.LifecycleStatus == feeddirectiondomain.SessionStatusPending && !pen1After.Completed,
		"lifecycle_status=%s completed=%t", pen1After.LifecycleStatus, pen1After.Completed)
	story.Assert("its head count now includes the animals the unapproved movement is bringing",
		pen1After.HeadCount == 50, "head_count=%d, want 40 + the 10 being moved in", pen1After.HeadCount)
	story.Assert("the quantities on the card actually changed",
		pen1After.TotalKg != originalTotal,
		"total_kg %s -> %s", originalTotal, pen1After.TotalKg)
	// THE POINT OF THE REASON. 'rework' has no client bucket of its own -- it normalizes to
	// "pending", the same state as a pen nobody has packed -- so the chip cannot say why this card
	// is back, and only this sentence can.
	story.Assert("the packer is TOLD why the card came back, in farm language",
		pen1After.ReworkReason != "" && !containsAny(pen1After.ReworkReason,
			"amend", "correction", "shifting_events", "projection", "row_version", "workflow"),
		"rework_reason=%q", pen1After.ReworkReason)
	story.Assert("the sibling pen is untouched — same status, same quantities",
		penRow(corrected, "2").LifecycleStatus == feeddirectiondomain.SessionStatusPending &&
			penRow(corrected, "2").HeadCount == 30 && penRow(corrected, "2").ReworkReason == "",
		"Castro - 2 head_count=%d reason=%q",
		penRow(corrected, "2").HeadCount, penRow(corrected, "2").ReworkReason)

	// The verifier's item must LEAVE the pending queue. Left behind, it points at a video of the old
	// quantity; approving it would flip the pen straight back to completed behind the operator who is
	// at that moment repacking it.
	if len(items) == 1 {
		story.Assert("the superseded verification item is withdrawn from the queue",
			verificationItemStatus(t, ctx, fx.Pool, items[0].id) == "withdrawn",
			"item status=%s", verificationItemStatus(t, ctx, fx.Pool, items[0].id))
	}
	story.Assert("the earlier approval is stripped, so no verifier is credited with the old quantity",
		!packingRowHasVerifier(t, ctx, fx.Pool, first.CompletionID),
		"verified_by/verified_at must be cleared on a reopened row")

	// -----------------------------------------------------------------------
	story.Step("14:00 — the experiment pen is deliberately left alone",
		"Experiment rations are authored as an ABSOLUTE kg total per pen, so a head-count change moves "+
			"no quantity there. Reopening one would discard a perfectly good video for a sheet that did "+
			"not change.")

	trialAmend, err := feed.AmendDirection(ctx, issueReq(feeddirectiondomain.WorkflowExperiment, at(14, 0)))
	if err != nil && err != feeddirectionports.ErrIssueNotFound {
		t.Fatalf("AmendDirection(experiment): %v", err)
	}
	story.Assert("an experiment correction reopens nothing",
		len(trialAmend.ReopenedPackingCompletionIDs) == 0,
		"experiment reopened=%v", trialAmend.ReopenedPackingCompletionIDs)

	// -----------------------------------------------------------------------
	story.Step("14:30 — the operator repacks to the new amounts and films it again",
		"The re-shoot is a fresh pending transition, so it queues a NEW verification item rather than "+
			"colliding with the withdrawn one, and the reason is cleared so nobody is told twice.")

	second := complete("1", "proof-castro-1-repacked", "feed-pack-castro-1-repack", at(14, 30))
	story.Assert("the re-submission is awaiting verification again",
		second.Status == feeddirectiondomain.PackingStatusPendingVerification && second.NewlyPending,
		"status=%s newly_pending=%t", second.Status, second.NewlyPending)
	story.Assert("it is the SAME pen-day row, re-opened and re-submitted, not a second row",
		second.CompletionID == first.CompletionID,
		"completion_id %s -> %s", first.CompletionID, second.CompletionID)

	reItems := listPackingItems(t, ctx, fx.Pool, second.CompletionID)
	story.Assert("a FRESH verification item reaches the verifier for the new video",
		len(reItems) == 2, "items for this pen-day = %d (one withdrawn, one pending)", len(reItems))
	story.Assert("exactly one of them is pending",
		countPending(reItems) == 1, "pending items = %d", countPending(reItems))

	final := worklist(at(14, 45))
	story.Assert("the reason is gone once the pen is re-submitted",
		penRow(final, "1").ReworkReason == "",
		"rework_reason=%q on a re-submitted pen — a stale sentence would tell a packer to redo what they just did",
		penRow(final, "1").ReworkReason)
}

// ---------------------------------------------------------------------------
// Fixture helpers — external INPUT facts only
// ---------------------------------------------------------------------------

// e2eClock is the pinned business clock the story advances hour by hour.
type e2eClock struct{ at time.Time }

func (c *e2eClock) Set(t time.Time) { c.at = t }
func (c *e2eClock) Now() time.Time {
	if c.at.IsZero() {
		return time.Date(2026, time.July, 29, 7, 0, 0, 0, biztime.DefaultLocation())
	}
	return c.at
}

func feedDayTime(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2026, time.July, 30, 0, 0, 0, 0, biztime.DefaultLocation())
}

func seedFeedCorrectionScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool, park, shedCastro, shedTrial string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed feed correction scope: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'FE-CPT', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, fxTenant, park)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($3::uuid, $1::uuid, 'shed', 'FE-CASTRO', 'Castro', 'active', $2::uuid, 1),
       ($4::uuid, $1::uuid, 'shed', 'FE-TRIAL',  'Trial',  'active', $2::uuid, 2)
ON CONFLICT (location_id) DO NOTHING`, fxTenant, park, shedCastro, shedTrial)

	// The pen catalog. Castro is subdivided; Trial is not.
	for _, label := range []string{"1", "2"} {
		exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active', 'manual')
ON CONFLICT (tenant_id, shed_id, normalized_label) DO NOTHING`,
			fxTenant, shedCastro, label, feeddirectiondomain.PartitionMatchKey(label))
	}

	exec(`INSERT INTO feed_shed_tags (tenant_id, shed_tag_label, applies_to, display_order, status)
VALUES ($1::uuid, 'Non-Pregnant', 'adult', 1, 'active')`, fxTenant)
	exec(`INSERT INTO feed_ration_groups (tenant_id, breed_label, ration_group_label)
VALUES ($1::uuid, 'Beetal', 'Beetal/Sirohi')`, fxTenant)
	exec(`INSERT INTO feed_item_catalog (tenant_id, feed_item_label, display_order, status)
VALUES ($1::uuid, 'Concentrate', 1, 'active')`, fxTenant)

	// Morning/Evening at half the day each — the split the pen-day card renders as a breakdown.
	exec(`INSERT INTO feed_session_templates (tenant_id, park_id, session_no, session_label, split_fraction, display_order, status)
VALUES ($1::uuid, $2::uuid, 1, 'Morning', 0.5, 1, 'active'),
       ($1::uuid, $2::uuid, 2, 'Evening', 0.5, 2, 'active')`, fxTenant, park)
	exec(`INSERT INTO feed_session_template_items (tenant_id, park_id, session_no, slot_no, feed_item_label, status)
VALUES ($1::uuid, $2::uuid, 1, 1, 'Concentrate', 'active'),
       ($1::uuid, $2::uuid, 2, 1, 'Concentrate', 'active')`, fxTenant, park)

	// A PER-HEAD rate, which is exactly why a head-count change moves the quantity here and why an
	// absolute-kg experiment pen is exempt from the reopen.
	exec(`INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label, grams_per_head, valid_from)
VALUES ($1::uuid, $2::uuid, 'Beetal/Sirohi', 'Non-Pregnant', 'Concentrate', 200.000, DATE '2026-01-01')`, fxTenant, park)

	// The dispatch clock: normal issues at 07:00, both workflows correct at 14:00, transport locks at
	// 15:30 — AFTER the correction, which is why no lock has to be lifted for it.
	exec(`INSERT INTO feed_schedule_config (tenant_id, park_id, workflow, direction_time, correction_time, transport_time, valid_from)
VALUES ($1::uuid, $2::uuid, 'normal',     TIME '07:00', TIME '14:00', TIME '15:30', DATE '2026-01-01'),
       ($1::uuid, $2::uuid, 'experiment', TIME '14:00', TIME '14:00', TIME '15:30', DATE '2026-01-01')`,
		fxTenant, park)
}

var feedCorrectionGoatSeq int

func seedFeedCorrectionGoats(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, partition string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		feedCorrectionGoatSeq++
		seq := feedCorrectionGoatSeq
		var goatID string
		if err := pool.QueryRow(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status,
  age_band, custodian_party_id, park_id, shed_id, management_stage
) VALUES (
  gen_random_uuid(), $1::uuid, $2, 'goat', 'Beetal', 'female', 'alive', 'adult',
  '00000000-0000-4000-8000-000000001001'::uuid, $3::uuid, $4::uuid, 'Non-Pregnant'
) RETURNING goat_id::text`,
			fxTenant, feedCorrectionDisplayID(seq), feedCorrectionParkOf(t, ctx, pool, shedID), shedID).Scan(&goatID); err != nil {
			t.Fatalf("seed feed correction goat %d: %v", seq, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, source_shed_name, partition_label)
VALUES ($1::uuid, $2::uuid, $3::uuid, $5, $4)
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET
  shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
			fxTenant, goatID, shedID, partition, "Castro - "+partition); err != nil {
			t.Fatalf("seed feed correction goat partition %d: %v", seq, err)
		}
	}
}

func feedCorrectionDisplayID(n int) string {
	return "G-9" + padFive(n)
}

func padFive(n int) string {
	s := ""
	for _, d := range []int{10000, 1000, 100, 10, 1} {
		s += string(rune('0' + (n/d)%10))
	}
	return s
}

func feedCorrectionParkOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID string) string {
	t.Helper()
	var park string
	if err := pool.QueryRow(ctx, `
SELECT parent_location_id::text FROM locations WHERE tenant_id = $1::uuid AND location_id = $2::uuid`,
		fxTenant, shedID).Scan(&park); err != nil {
		t.Fatalf("resolve park for shed %s: %v", shedID, err)
	}
	return park
}

// raiseShiftingInto seeds a RAISED, NOT-YET-APPROVED low-priority movement bringing animals into a
// pen. authorized_at stays NULL: a movement nobody has approved has no approval instant, and a
// fixture carrying one would let the projection pass by reading the wrong column.
func raiseShiftingInto(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	park, destShed, destPartition string, raisedAt time.Time, headCount int,
) {
	t.Helper()
	var eventID string
	if err := pool.QueryRow(ctx, `
INSERT INTO shifting_events (
  tenant_id, logical_shifting_event_key, priority, category,
  destination_park_id, destination_shed_id, destination_partition_label,
  raised_at, effective_at, authorized_at, authorization_state, event_status,
  source_system, source_ref, payload_hash, idempotency_key, request_fingerprint
) VALUES (
  $1::uuid, 'fe-raise-1', 'low', 'growth',
  $2::uuid, $3::uuid, $4,
  $5, $5, NULL, 'pending', 'pending',
  'manual_review', 'fe-raise-1', 'fe-raise-1', 'fe-raise-1', 'fe-raise-1'
) RETURNING shifting_event_id::text`,
		fxTenant, park, destShed, destPartition, raisedAt).Scan(&eventID); err != nil {
		t.Fatalf("raise shifting: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shifting_event_impacts (
  tenant_id, shifting_event_id, grain_key, breed_key, breed_label, stage_tag, sex, head_count
) VALUES ($1::uuid, $2::uuid, 'fe-raise-1:beetal', 'beetal', 'Beetal', 'Non-Pregnant', 'female', $3)`,
		fxTenant, eventID, headCount); err != nil {
		t.Fatalf("seed shifting impact: %v", err)
	}
}

func shiftingAuthState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `
SELECT authorization_state FROM shifting_events WHERE tenant_id = $1::uuid AND logical_shifting_event_key = 'fe-raise-1'`,
		fxTenant).Scan(&state); err != nil {
		t.Fatalf("read shifting authorization state: %v", err)
	}
	return state
}

type packingItem struct {
	id      string
	subject string
	status  string
}

func listPackingItems(t *testing.T, ctx context.Context, pool *pgxpool.Pool, completionID string) []packingItem {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT item_id::text, coalesce(subject_label, ''), status
FROM verification_items
WHERE tenant_id = $1::uuid
  AND source_module = 'feed'
  AND source_ref_type = 'feed_packing_completion'
  AND source_ref_id = $2
ORDER BY created_at`, fxTenant, completionID)
	if err != nil {
		t.Fatalf("list packing verification items: %v", err)
	}
	defer rows.Close()
	out := []packingItem{}
	for rows.Next() {
		var it packingItem
		if err := rows.Scan(&it.id, &it.subject, &it.status); err != nil {
			t.Fatalf("scan packing verification item: %v", err)
		}
		out = append(out, it)
	}
	return out
}

func countPending(items []packingItem) int {
	n := 0
	for _, it := range items {
		if it.status == "pending" {
			n++
		}
	}
	return n
}

func verificationItemStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid`,
		fxTenant, itemID).Scan(&status); err != nil {
		t.Fatalf("read verification item status: %v", err)
	}
	return status
}

func packingRowHasVerifier(t *testing.T, ctx context.Context, pool *pgxpool.Pool, completionID string) bool {
	t.Helper()
	var has bool
	if err := pool.QueryRow(ctx, `
SELECT verified_by IS NOT NULL OR verified_at IS NOT NULL
FROM feed_packing_completions WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`,
		fxTenant, completionID).Scan(&has); err != nil {
		t.Fatalf("read packing completion verifier: %v", err)
	}
	return has
}

func countCastroRows(page feeddirectiondomain.PackingPage, shedID, partition string) int {
	n := 0
	for _, row := range page.Items {
		if row.ShedID == shedID &&
			feeddirectiondomain.PartitionMatchKey(row.PartitionLabel) == feeddirectiondomain.PartitionMatchKey(partition) {
			n++
		}
	}
	return n
}

func sessionSummary(row feeddirectiondomain.PackingRow) string {
	out := ""
	for i, s := range row.Sessions {
		if i > 0 {
			out += " · "
		}
		out += s.SessionLabel + " " + s.TotalKg + " kg"
	}
	return out
}

func containsAny(s string, needles ...string) bool {
	lower := ""
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		lower += string(r)
	}
	for _, n := range needles {
		if len(n) == 0 {
			continue
		}
		for i := 0; i+len(n) <= len(lower); i++ {
			if lower[i:i+len(n)] == n {
				return true
			}
		}
	}
	return false
}

func verificationItemRowVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) int {
	t.Helper()
	var version int
	if err := pool.QueryRow(ctx, `
SELECT row_version FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid`,
		fxTenant, itemID).Scan(&version); err != nil {
		t.Fatalf("read verification item row_version: %v", err)
	}
	return version
}
