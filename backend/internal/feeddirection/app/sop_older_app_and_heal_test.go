package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// E2E 2026-09-17 on the QA clone. Program decision 7: an OLDER APP (one that sends only the legacy
// fixed proof fields) is NEVER forced to update. Its request is mapped onto the card, what it sent
// is judged for placement and kind, and every compulsory capture / required question it could not
// have sent is recorded as "Not captured (older app)" on the verifier item -- instead of the 422
// feed_proof_slot_invalid / feed_answer_invalid the live run returned for every stage once the farm
// authored one extra capture or question.

func olderAppRulesSource(stage string, rules domain.Rules) ports.StaticSOPRules {
	rules.Version, rules.Stage = metaCardVersion, stage
	return ports.StaticSOPRules{ByStage: map[string]domain.Rules{stage: rules}}
}

func stricterPackingCard() domain.Rules {
	lo, hi := 1.0, 3.0
	return domain.Rules{StageRules: domain.StageRules{
		Proofs: []authored.ProofSlot{
			{Key: domain.SlotPackingVideo, Title: "Packing proof video", Kind: authored.KindVideo, Required: true},
			{Key: "bag_label_photo", Title: "Bag label photo", Kind: authored.KindPhoto, Required: true},
		},
		Questions: []authored.Question{{ID: "bags", Kind: authored.QuestionNumber, Title: "Bags packed", Required: true, Min: &lo, Max: &hi}},
	}}
}

func hasRow(rows []authored.AnswerRow, title, value string) bool {
	for _, r := range rows {
		if r.Title == title && r.Value == value {
			return true
		}
	}
	return false
}

func packingSvc(t *testing.T, store *metaPackingStore, enq *recordingPackingEnqueuer, rules ports.StaticSOPRules) *Service {
	t.Helper()
	return NewService(nil, nil).
		WithIssueStore(metaIssueStore(t, domain.WorkflowNormal)).
		WithSOPRules(rules).
		WithPackingStore(store).
		WithPackingVerificationEnqueuer(enq).
		WithProofValidator(&fakeProofValidator{}).
		WithNowFunc(func() time.Time { return targetDate() })
}

func packingIn() CompletePackingInput {
	return CompletePackingInput{
		TenantID: testTenant, ParkID: testPark, ShedID: shedA, PartitionLabel: "1", SessionNo: 1, TargetDate: targetDate(),
		Workflow: domain.WorkflowNormal, CompletedBy: "00000000-0000-4000-8000-000000000111", IdempotencyKey: "feed-packing-older-1",
	}
}

func TestOlderAppPackingIsAcceptedAgainstAStricterCardAndTheGapIsShown(t *testing.T) {
	store := &metaPackingStore{result: ports.CompletePackingResult{
		CompletionID: "pk-1", Status: domain.PackingStatusPendingVerification, RowVersion: 1, NewlyPending: true,
		SOPProofs: authored.ProofRefs{domain.SlotPackingVideo: "p-video"},
	}}
	enq := &recordingPackingEnqueuer{}
	in := packingIn()
	in.PackingProofRef = "p-video"
	if _, err := packingSvc(t, store, enq, olderAppRulesSource(domain.StagePacking, stricterPackingCard())).CompletePacking(context.Background(), in); err != nil {
		t.Fatalf("older app packing refused: %v", err)
	}
	if got := store.completeCalls[0].SOPProofs; len(got) != 1 || got[domain.SlotPackingVideo] != "p-video" {
		t.Fatalf("stored proofs = %v, want only the video the older app sent", got)
	}
	rows := enq.calls[0].ContextRows
	if !hasRow(rows, "Bag label photo", authored.MissingNoteOlderApp) || !hasRow(rows, "Bags packed", authored.MissingNoteOlderApp) {
		t.Fatalf("verifier rows = %+v, want both authored items marked not captured (older app)", rows)
	}
}

func TestNewAppPackingIsStillJudgedStrictly(t *testing.T) {
	store := &metaPackingStore{}
	in := packingIn()
	in.Proofs = authored.ProofRefs{domain.SlotPackingVideo: "p-video"}
	_, err := packingSvc(t, store, &recordingPackingEnqueuer{}, olderAppRulesSource(domain.StagePacking, stricterPackingCard())).CompletePacking(context.Background(), in)
	if !errors.Is(err, ports.ErrSOPProofSlotInvalid) {
		t.Fatalf("new app missing a compulsory slot: err = %v, want feed_proof_slot_invalid", err)
	}
}

// A card that no longer has the seeded water slot: an older phone still sends all three fixed refs.
// The water video has no slot to land on, so it is left out rather than refusing the pen forever.
func TestOlderAppDistributionOntoACardWithoutTheWaterSlot(t *testing.T) {
	card := domain.Rules{StageRules: domain.StageRules{Proofs: []authored.ProofSlot{
		{Key: domain.SlotFeedWeightPhoto, Title: "Feed weight photo", Kind: authored.KindPhoto, Required: true},
		{Key: domain.SlotFeedVideo, Title: "Feed distribution video", Kind: authored.KindVideo, Required: true},
	}}}
	store := &fakeDistributionStore{completeResult: ports.CompleteDistributionResult{
		CompletionID: "c-1", Status: domain.DistributionStatusPendingVerification, RowVersion: 1, NewlyPending: true,
	}}
	enq := &fakeDistributionEnqueuer{}
	svc := NewService(nil, nil).
		WithIssueStore(metaIssueStore(t, domain.WorkflowNormal)).
		WithSOPRules(olderAppRulesSource(domain.StageDistribution, card)).
		WithDistributionStore(store).
		WithDistributionVerificationEnqueuer(enq).
		WithProofValidator(&fakeProofValidator{}).
		WithNowFunc(func() time.Time { return targetDate().Add(12 * time.Hour) })
	in := validCompleteDistributionInput()
	if _, err := svc.CompleteDistribution(context.Background(), in); err != nil {
		t.Fatalf("older app distribution refused against a card without the water slot: %v", err)
	}
}

// A card that turned the seeded wastage VIDEO slot into a PHOTO and added a required question: the
// older phone's video fits no slot, and a submit with NO capture at all cannot create a verifier
// item (decision 6), so that one case is refused naming the slot. The question alone never blocks.
func TestOlderAppWastageVideoWithNoSlotLeftIsRefusedNamingTheSlot(t *testing.T) {
	card := domain.Rules{StageRules: domain.StageRules{
		Proofs:    []authored.ProofSlot{{Key: domain.SlotWastageVideo, Title: "Leftover feed photo", Kind: authored.KindPhoto, Required: true}},
		Questions: []authored.Question{{ID: "left", Kind: authored.QuestionText, Title: "What was left", Required: true}},
	}}
	store := &fakeWastageStore{result: ports.CompleteWastageResult{CompletionID: "w-1", Status: domain.WastageStatusPendingVerification, RowVersion: 1, NewlyPending: true}}
	svc, _ := newWastageService(store, &recordingWastageEnqueuer{})
	rules := olderAppRulesSource(domain.StageWastage, card)
	// The wastage card is pinned through the sheet's DIRECTION version, so the direction document
	// must be published at the same version for the authored wastage card to be the one in force.
	direction := domain.SeededRules(domain.StageDistribution)
	direction.Version = metaCardVersion
	rules.ByStage[domain.StageDistribution] = direction
	svc = svc.WithSOPRules(rules).WithProofValidator(&fakeProofValidator{})
	_, err := svc.CompleteWastage(context.Background(), wastageInput(shedA))
	if key, _, ok := SOPProofSlotError(err); !ok || key != domain.SlotWastageVideo {
		t.Fatalf("err = %v, want feed_proof_slot_invalid naming %s", err, domain.SlotWastageVideo)
	}
}

func TestOlderAppTransportIsAcceptedAgainstACardWithARequiredQuestion(t *testing.T) {
	lo, hi := 1.0, 4.0
	card := domain.Rules{StageRules: domain.StageRules{
		Proofs:    []authored.ProofSlot{{Key: domain.SlotTransportVideo, Title: "Transport video", Kind: authored.KindEither, Required: true}},
		Questions: []authored.Question{{ID: "trips", Kind: authored.QuestionNumber, Title: "Trips", Required: true, Min: &lo, Max: &hi}},
	}}
	store := &transportServiceStore{
		task:   ports.FeedTransportTask{TaskID: "task-1", ParkID: "park-1", ShedID: "shed-1", SOPVersion: metaCardVersion},
		result: ports.SubmitTransportResult{AttemptID: "a-1", Status: "verification_due", AttemptNo: 1, NewlyPending: true, SOPProofs: authored.ProofRefs{domain.SlotTransportVideo: "p-v"}},
	}
	enq := &recordingTransportEnqueuer{}
	svc := NewService(nil, nil).WithSOPRules(olderAppRulesSource(domain.StageTransport, card)).WithTransportStore(store).WithProofValidator(&transportProofValidator{}).WithTransportVerificationEnqueuer(enq)
	if _, err := svc.SubmitTransport(context.Background(), SubmitTransportInput{TenantID: "tenant-1", TaskID: "task-1", OperatorID: "op", IdempotencyKey: "transport-older-1", ProofRef: "p-v"}); err != nil {
		t.Fatalf("older app transport refused: %v", err)
	}
	if !hasRow(enq.calls[0].AnswerRows, "Trips", authored.MissingNoteOlderApp) {
		t.Fatalf("rows = %+v, want Trips not captured (older app)", enq.calls[0].AnswerRows)
	}
}

// HEAL. The store commits the pending row, then the verifier enqueue fails; the phone retries the
// SAME request. The replay is not "newly pending", and packing/wastage enqueued only on a newly
// pending transition -- so the row stayed pending_verification with NO verifier item, forever.
// Distribution and transport already re-enqueued on every pending replay; packing and wastage must
// too, and from the ROW's stored proofs and answers.
func TestPackingReplayAfterAFailedEnqueueHealsFromTheRow(t *testing.T) {
	store := &metaPackingStore{result: ports.CompletePackingResult{
		CompletionID: "pk-1", Status: domain.PackingStatusPendingVerification, RowVersion: 1, NewlyPending: false,
		SOPProofs: metaProofs(), SOPAnswers: metaAnswers("no"),
	}}
	enq := &recordingPackingEnqueuer{}
	svc := packingSvc(t, store, enq, metaRulesSource()).WithProofValidator(clipIsVideo())
	in := packingIn()
	in.Proofs, in.Answers = metaProofs(), metaAnswers("yes")
	if _, err := svc.CompletePacking(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 1 {
		t.Fatalf("packing replay enqueue calls = %d, want 1 (a pending row with no item never heals otherwise)", len(enq.calls))
	}
	if enq.calls[0].IdempotencyKey != "feed-packing-verification:pk-1:1" {
		t.Fatalf("heal key = %q", enq.calls[0].IdempotencyKey)
	}
	assertCardMeta(t, enq.calls[0].MediaRefs, enq.calls[0].MediaMeta, enq.calls[0].ContextRows, "No")
}

func TestWastageReplayAfterAFailedEnqueueHealsFromTheRow(t *testing.T) {
	store := &fakeWastageStore{result: ports.CompleteWastageResult{
		CompletionID: "w-1", Status: domain.WastageStatusPendingVerification, RowVersion: 2, NewlyPending: false,
		SOPProofs: metaProofs(), SOPAnswers: metaAnswers("no"),
	}}
	enq := &recordingWastageEnqueuer{}
	svc, _ := newWastageService(store, enq)
	svc = svc.WithSOPRules(metaRulesSource()).WithProofValidator(clipIsVideo())
	in := wastageInput(shedA)
	in.WastageProofRef, in.Proofs, in.Answers = "", metaProofs(), metaAnswers("yes")
	if _, err := svc.CompleteWastage(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 1 {
		t.Fatalf("wastage replay enqueue calls = %d, want 1", len(enq.calls))
	}
	assertCardMeta(t, enq.calls[0].MediaRefs, enq.calls[0].MediaMeta, enq.calls[0].AnswerRows, "No")
}

// A completed row is left alone: nothing to heal.
func TestPackingReplayOfACompletedBagEnqueuesNothing(t *testing.T) {
	store := &metaPackingStore{result: ports.CompletePackingResult{
		CompletionID: "pk-1", Status: domain.PackingStatusCompleted, RowVersion: 2, SOPProofs: metaProofs(),
	}}
	enq := &recordingPackingEnqueuer{}
	svc := packingSvc(t, store, enq, metaRulesSource()).WithProofValidator(clipIsVideo())
	in := packingIn()
	in.Proofs, in.Answers = metaProofs(), metaAnswers("yes")
	if _, err := svc.CompletePacking(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 0 {
		t.Fatalf("enqueue calls = %d, want 0 for a completed bag", len(enq.calls))
	}
}
