package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TestKernelStory_HighPriorityShiftingIntoEmptyPenPricesFeedPerAnimalStage is the production-path
// proof for the 2026-08-12 maintainer decision: when a movement carries no target management stage,
// the embedded high-priority feed ration is priced against EACH ANIMAL's own current stage.
//
// The defect it closes: since 2026-08-03 the raiser does not choose a stage — the backend adopts the
// destination pen's cohort at raise time, and ResolveShiftingDestinationStage deliberately returns
// BLANK for an empty pen, meaning "each animal keeps its own stage". The high-priority feed pricer
// keyed its ration grid off that blank field, so every high-priority movement into an empty pen was
// refused with "selected destination management stage is missing" — naming a choice the phone does
// not offer. The operator had no way forward.
//
//	RAISE     counts.ShiftingDestinations -> domain.ResolveShiftingDestinationStage  (blank, correct)
//	APPROVE   counts.CreateApprovalRequest -> DecideApprovalRequest                  (park head)
//	READ      counts.ListShiftingEventsPendingExecution                              (the operator's
//	          card, carrying the backend-owned feed_requirement and its fingerprint)
//	COMPLETE  counts.CompleteShiftingEvent -> identity.RelocateGoatsToShedInTx
//
// The movement deliberately carries TWO cohorts, so a per-event stage cannot pass by accident: the
// quantity can only come out right if each animal is priced on its own grid.
func TestKernelStory_HighPriorityShiftingIntoEmptyPenPricesFeedPerAnimalStage(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-shifting-empty-pen-feed-price",
		"High-priority shifting into an empty pen prices feed per animal stage",
		"A movement into an EMPTY pen adopts no cohort: each animal keeps the stage it already has. "+
			"The feed a high-priority movement must carry is therefore priced against each animal's own "+
			"stage, so a mixed Adult + Grower movement carries the sum of both rations — and the movement "+
			"is no longer refused for a management stage the operator was never asked to choose.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx

	const (
		sourceShed  = "5f000000-0000-4000-8000-0000000f1201"
		emptyPen    = "5f000000-0000-4000-8000-0000000f1202" // the destination: no residents at all
		sharedStage = "5f000000-0000-4000-8000-0000000f12a1"
		adultGoat   = "5f000000-0000-4000-8000-0000000f1301"
		growerGoat  = "5f000000-0000-4000-8000-0000000f1302"
	)

	story.Step("Two cohorts in one shed, and an empty destination pen",
		"The mover set is deliberately mixed: one Adult and one Grower. The destination pen holds no "+
			"animals, which is exactly the case that used to block.")
	fx.SeedShed(sourceShed, "E2E-SF-SRC", sharedStage)
	fx.SeedShed(emptyPen, "E2E-SF-EMPTY", sharedStage)
	dob := time.Date(2024, 1, 1, 0, 0, 0, 0, biztime.DefaultLocation())
	fx.SeedGoat(GoatSpec{GoatID: adultGoat, ShedID: sourceShed, Stage: "Adult", DOB: &dob, AgeBand: "adult", Breed: "Beetal"})
	fx.SeedGoat(GoatSpec{GoatID: growerGoat, ShedID: sourceShed, Stage: "Grower", DOB: &dob, AgeBand: "adult", Breed: "Beetal"})

	story.Step("The park's active Feed Config prices Adult and Grower differently",
		"250 g/head of Concentrate for an Adult, 150 g/head for a Grower. Two different numbers is what "+
			"makes the assertion below meaningful — a single-stage fallback could not produce their sum.")
	seedEmptyPenFeedConfig(fx)

	shiftRepo := countspg.NewRepository(fx.Pool, 10*time.Second).
		WithIdentityTxWriter(identitypg.NewRepository(fx.Pool, 10*time.Second))

	story.Step("The raise resolves NO destination stage, because the pen is empty",
		"This is the production resolver reading the production destination catalog. Blank is the "+
			"correct answer and means keep-current; it is not a missing input, and the phone never asked "+
			"the raiser for a stage.")
	catalog, err := shiftRepo.ShiftingDestinationCatalog(ctx, fxTenant)
	if err != nil {
		t.Fatalf("read shifting destinations: %v", err)
	}
	var destinationStages []string
	for _, park := range catalog.Parks {
		for _, shed := range park.Sheds {
			if shed.ShedID == emptyPen {
				destinationStages = shed.ManagementStages
			}
		}
	}
	resolved := countsdomain.ResolveShiftingDestinationStage(destinationStages, catalog.ManagementStages)
	story.Assert("the empty pen offers no cohort to adopt", len(destinationStages) == 0,
		"management_stages=%v", destinationStages)
	story.Assert("so the raise records no target stage", resolved == "", "resolved=%q", resolved)

	at := time.Date(2026, 8, 12, 9, 0, 0, 0, biztime.DefaultLocation())
	eventID := raiseAndApproveHighPriorityShifting(t, ctx, shiftRepo, "sf-empty-pen",
		[]string{adultGoat, growerGoat}, sourceShed, emptyPen, resolved, at)

	storedTarget := fx.scanText(
		`SELECT COALESCE(target_management_stage,'') FROM shifting_events WHERE tenant_id=$1 AND shifting_event_id=$2`,
		fxTenant, eventID)
	story.Assert("the movement carries a blank target stage", storedTarget == "", "target_management_stage=%q", storedTarget)

	story.Step("The operator's card now shows a priced ration instead of a dead end",
		"ListShiftingEventsPendingExecution is the read the phone renders. Its feed_requirement is "+
			"backend-owned: status, the exact feed lines, and the semantic config fingerprint the "+
			"completion is checked against. This is where the operator used to see 'selected destination "+
			"management stage is missing' with nothing they could do about it.")
	from := biztime.BusinessDayStart(at).UTC()
	to := from.AddDate(0, 0, 1)
	page, err := shiftRepo.ListShiftingEventsPendingExecution(ctx, countsdomain.ShiftingExecutionQuery{
		TenantID: fxTenant, RaisedFrom: &from, RaisedBefore: &to, Status: "authorized",
		Now: at, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("list shifting actions: %v", err)
	}
	var card *countsdomain.ShiftingExecutionRow
	for i := range page.Items {
		if page.Items[i].ShiftingEventID == eventID {
			card = &page.Items[i]
		}
	}
	if card == nil {
		t.Fatalf("movement %s is absent from the operator's Actions list", eventID)
	}
	if !story.Assert("the card carries a feed requirement", card.FeedRequirement != nil, "feed_requirement=nil") {
		return
	}
	story.Assert("it is READY, not blocked", card.FeedRequirement.Status == "ready",
		"status=%s blocked_reason=%q", card.FeedRequirement.Status, card.FeedRequirement.BlockedReason)
	if !story.Assert("it names one feed line", len(card.FeedRequirement.Items) == 1,
		"items=%+v", card.FeedRequirement.Items) {
		return
	}
	story.Assert("priced as Adult 250 g + Grower 150 g, each on its OWN stage's grid",
		card.FeedRequirement.Items[0].QuantityGrams == "400.0000",
		"quantity=%s (a single-stage fallback would read 500 or 300)", card.FeedRequirement.Items[0].QuantityGrams)

	story.Step("The operator completes with the three mandatory videos and the movement applies",
		"Completion re-resolves the ration under the shifting row lock and compares the fingerprint the "+
			"card carried, so a config edit between reading and filming is caught rather than guessed.")
	result, _, err := shiftRepo.CompleteShiftingEvent(ctx, countsdomain.ShiftingCompletionCommand{
		TenantID: fxTenant, ShiftingEventID: eventID,
		CompletedByUserID: fxParty, CompletedAt: at, TraceID: "trace-sf-empty-pen",
		ProofRef:              "proof-shifting-sf-empty-pen",
		FeedPackingProofRef:   "proof-packing-sf-empty-pen",
		FeedGivenProofRef:     "proof-feeding-sf-empty-pen",
		FeedConfigFingerprint: card.FeedRequirement.Fingerprint,
		IdempotencyKey:        "complete-sf-empty-pen", RequestFingerprint: "complete-fp-sf-empty-pen",
	})
	story.Assert("completion succeeded", err == nil, "err=%v", err)
	story.Assert("the movement applied", result.EventStatus == countsdomain.ShiftingEventStatusApplied,
		"event_status=%s", result.EventStatus)

	story.Step("The animals moved, and each kept the cohort it already had",
		"Keep-current is the whole point of the blank target: an empty pen has no cohort to impose, so "+
			"the Adult stays an Adult and the Grower stays a Grower.")
	adultShedNow := fx.scanText(`SELECT shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, adultGoat)
	growerShedNow := fx.scanText(`SELECT shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, growerGoat)
	story.Assert("the Adult is in the destination pen", adultShedNow == emptyPen, "shed_id=%s", adultShedNow)
	story.Assert("the Grower is too", growerShedNow == emptyPen, "shed_id=%s", growerShedNow)

	adultStage := fx.scanText(`SELECT COALESCE(management_stage,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, adultGoat)
	growerStage := fx.scanText(`SELECT COALESCE(management_stage,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, growerGoat)
	story.Assert("the Adult kept its cohort", adultStage == "Adult", "management_stage=%s", adultStage)
	story.Assert("the Grower kept its cohort", growerStage == "Grower", "management_stage=%s", growerStage)

	story.Step("The ration that was actually carried is recorded as immutable evidence",
		"The completion snapshots the resolved requirement onto the shifting row, so a later Feed Config "+
			"edit cannot rewrite what the operator was told to load and filmed themselves loading.")
	snapshot := fx.scanText(
		`SELECT COALESCE(feed_requirement_snapshot::text,'') FROM shifting_events WHERE tenant_id=$1 AND shifting_event_id=$2`,
		fxTenant, eventID)
	story.Assert("a feed snapshot was stored", snapshot != "", "snapshot=%q", snapshot)
	var stored countsdomain.ShiftingFeedRequirement
	if snapshot != "" {
		if err := json.Unmarshal([]byte(snapshot), &stored); err != nil {
			t.Fatalf("decode feed snapshot: %v", err)
		}
	}
	story.Assert("and it records the same 400 g the operator was shown",
		len(stored.Items) == 1 && stored.Items[0].QuantityGrams == "400.0000", "snapshot items=%+v", stored.Items)
}

// seedEmptyPenFeedConfig authors the park's active ration grid: one feed item, two stages, two rates.
// External/input config only -- every requirement asserted above is resolved by the production pricer.
func seedEmptyPenFeedConfig(f *Fixture) {
	f.T.Helper()
	f.exec("feed shed tag adult",
		`INSERT INTO feed_shed_tags (tenant_id, shed_tag_label, applies_to, status) VALUES ($1, 'Adult', 'adult', 'active')`, fxTenant)
	f.exec("feed shed tag grower",
		`INSERT INTO feed_shed_tags (tenant_id, shed_tag_label, applies_to, status) VALUES ($1, 'Grower', 'adult', 'active')`, fxTenant)
	f.exec("feed ration group",
		`INSERT INTO feed_ration_groups (tenant_id, breed_label, ration_group_label) VALUES ($1, 'Beetal', 'Beetal/Sirohi')`, fxTenant)
	f.exec("feed session template",
		`INSERT INTO feed_session_templates (tenant_id, park_id, session_no, session_label, split_fraction, status)
		 VALUES ($1, $2, 1, 'Morning', 1.0, 'active')`, fxTenant, fxPark)
	f.exec("feed session template item",
		`INSERT INTO feed_session_template_items (tenant_id, park_id, session_no, slot_no, feed_item_label, status, valid_from)
		 VALUES ($1, $2, 1, 1, 'Concentrate', 'active', DATE '2026-01-01')`, fxTenant, fxPark)
	f.exec("feed ration rate adult",
		`INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label, grams_per_head, valid_from)
		 VALUES ($1, $2, 'Beetal/Sirohi', 'Adult', 'Concentrate', 250, DATE '2026-01-01')`, fxTenant, fxPark)
	f.exec("feed ration rate grower",
		`INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label, grams_per_head, valid_from)
		 VALUES ($1, $2, 'Beetal/Sirohi', 'Grower', 'Concentrate', 150, DATE '2026-01-01')`, fxTenant, fxPark)
}

// raiseAndApproveHighPriorityShifting drives the real raise + park-head approval path, carrying the
// stage the production resolver produced (blank for an empty pen).
func raiseAndApproveHighPriorityShifting(
	t *testing.T, ctx context.Context, repo *countspg.Repository, key string, goatIDs []string,
	sourceShed, destShed, targetStage string, at time.Time,
) string {
	t.Helper()
	src := sourceShed
	mode := "keep_current"
	if targetStage != "" {
		mode = "destination_stage"
	}
	eventID, _, err := repo.RecordShiftingEvent(ctx, countsdomain.ShiftingEvent{
		TenantID: fxTenant, LogicalShiftingEventKey: key, Priority: "high", Category: "growth",
		SourceParkID: shParkPtr(), SourceShedID: &src,
		DestinationParkID: fxPark, DestinationShedID: destShed,
		ManagementStageMode: mode, TargetManagementStage: targetStage,
		RaisedAt: at, EffectiveAt: at,
		AuthorizationState: "pending", VerificationState: "unverified", EventStatus: "pending",
		SourceSystem: "goatos_canonical", SourceRef: "e2e:" + key,
		PayloadHash: "hash-" + key, IdempotencyKey: "idem-" + key, RequestFingerprint: "fp-" + key,
		Impacts: []countsdomain.ShiftingEventImpact{{
			GrainKey: destShed + ":beetal", BreedKey: "beetal", BreedLabel: "Beetal",
			HeadCount: int32(len(goatIDs)), RiskFlagsJSON: []byte("{}"),
		}},
	})
	if err != nil {
		t.Fatalf("record high-priority shifting %s: %v", key, err)
	}
	payload, err := json.Marshal(map[string]any{
		"shifting_event_id":   eventID,
		"destination_park_id": fxPark,
		"destination_shed_id": destShed,
		"goat_ids":            goatIDs,
	})
	if err != nil {
		t.Fatalf("marshal approval payload %s: %v", key, err)
	}
	req, _, err := repo.CreateApprovalRequest(ctx, countsdomain.ApprovalRequestSubmission{
		TenantID: fxTenant, RequestType: countsdomain.ApprovalRequestTypeShifting,
		Payload: payload, ShiftingEventID: &eventID,
		RaisedByUserID: fxParty, RaisedAt: at,
		IdempotencyKey: "submit-" + key, RequestFingerprint: "submit-fp-" + key,
	})
	if err != nil {
		t.Fatalf("submit approval %s: %v", key, err)
	}
	if _, _, err := repo.DecideApprovalRequest(ctx, countsdomain.ApprovalDecision{
		TenantID: fxTenant, ApprovalRequestID: req.ApprovalRequestID,
		Status: countsdomain.ApprovalStatusApproved, DecidedByUserID: fxParty, DecidedAt: at,
		IdempotencyKey: "decide-" + key, RequestFingerprint: "decide-fp-" + key,
		Effect: &countsdomain.ApprovalEffect{Shifting: &countsdomain.ShiftingApprovalEffect{
			ShiftingEventID: eventID, DestinationParkID: fxPark, DestinationShedID: destShed, GoatIDs: goatIDs,
		}},
	}); err != nil {
		t.Fatalf("approve shifting %s: %v", key, err)
	}
	return eventID
}
