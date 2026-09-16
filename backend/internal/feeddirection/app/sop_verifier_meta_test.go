package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// SOP VERIFIER PARITY (2026-09-16): every feed stage hands the verifier each capture under its
// card title with the kind it really is, and the crew's answers as the item's context rows --
// read from the ROW the store holds, so a repair retry can never queue what the request said.

const metaCardVersion = 3

// metaCard is a two-slot card: a photo and an `either` slot, plus one required pick-one.
func metaCard(stage string) domain.Rules {
	return domain.Rules{Version: metaCardVersion, Stage: stage, StageRules: domain.StageRules{
		Proofs: []authored.ProofSlot{
			{Key: "bag", Title: "Bag on the scale", Kind: authored.KindPhoto, Required: true},
			{Key: "clip", Title: "Crew clip", Kind: authored.KindEither, Required: true},
		},
		Questions: []authored.Question{{
			ID: "clean", Kind: authored.QuestionChoice, Title: "Trough clean?", Required: true,
			Options: []authored.Option{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}},
		}},
	}}
}

func metaAnswers(v string) authored.Answers {
	raw, _ := json.Marshal(v)
	return authored.Answers{"clean": raw}
}

func metaProofs() authored.ProofRefs {
	return authored.ProofRefs{"clip": "p-clip", "bag": "p-bag"}
}

// describingValidator is a proof validator that also reports the register's kind per proof.
type describingValidator struct {
	*fakeProofValidator
	kinds map[string]ports.MediaKind
}

func (d describingValidator) DescribeFeedProofMedia(context.Context, string, []string) (map[string]ports.MediaKind, error) {
	return d.kinds, nil
}

func clipIsVideo() describingValidator {
	return describingValidator{fakeProofValidator: &fakeProofValidator{}, kinds: map[string]ports.MediaKind{"p-clip": "video", "p-bag": "photo"}}
}

func metaIssueStore(t *testing.T, workflow string) *fakeIssueStore {
	t.Helper()
	issues := newFakeIssueStore()
	if _, err := issues.PersistIssue(context.Background(), ports.PersistIssueCommand{
		TenantID: testTenant, ParkID: testPark, FeedDay: biztime.BusinessDate(targetDate()), Workflow: workflow,
		IssuedAt: targetDate(), Fingerprint: "fp-meta", SOPVersion: metaCardVersion, PackingSOPVersion: metaCardVersion,
	}); err != nil {
		t.Fatal(err)
	}
	return issues
}

func metaRulesSource() ports.StaticSOPRules {
	return ports.StaticSOPRules{ByStage: map[string]domain.Rules{
		domain.StageDistribution: metaCard(domain.StageDistribution),
		domain.StagePacking:      metaCard(domain.StagePacking),
		domain.StageWastage:      metaCard(domain.StageWastage),
		domain.StageTransport:    metaCard(domain.StageTransport),
	}}
}

func assertCardMeta(t *testing.T, refs []string, meta []ports.ProofMeta, rows []authored.AnswerRow, wantAnswer string) {
	t.Helper()
	if len(refs) != 2 || refs[0] != "p-bag" || refs[1] != "p-clip" {
		t.Fatalf("media refs = %v, want card slot order [p-bag p-clip]", refs)
	}
	if len(meta) != 2 || meta[0] != (ports.ProofMeta{Label: "Bag on the scale", Kind: "photo"}) || meta[1] != (ports.ProofMeta{Label: "Crew clip", Kind: "video"}) {
		t.Fatalf("media meta = %+v, want slot titles with the register's kinds", meta)
	}
	if len(rows) != 1 || rows[0].Title != "Trough clean?" || rows[0].Value != wantAnswer {
		t.Fatalf("answer rows = %+v, want Trough clean? = %s", rows, wantAnswer)
	}
}

func metaDistributionService(t *testing.T, store *fakeDistributionStore, enq *fakeDistributionEnqueuer) *Service {
	t.Helper()
	return NewService(nil, nil).
		WithIssueStore(metaIssueStore(t, domain.WorkflowNormal)).
		WithSOPRules(metaRulesSource()).
		WithDistributionStore(store).
		WithDistributionVerificationEnqueuer(enq).
		WithProofValidator(clipIsVideo()).
		WithNowFunc(func() time.Time { return targetDate().Add(12 * time.Hour) })
}

func metaDistributionInput(answer string) CompleteDistributionInput {
	in := validCompleteDistributionInput()
	in.FeedWeightProofRef, in.DistributionProofRef, in.WaterProofRef = "", "", ""
	in.Proofs = metaProofs()
	in.Answers = metaAnswers(answer)
	return in
}

func TestDistributionEnqueueCarriesCardOrderedMediaMetaAndAnswers(t *testing.T) {
	store := &fakeDistributionStore{completeResult: ports.CompleteDistributionResult{
		CompletionID: "c-1", Status: domain.DistributionStatusPendingVerification, RowVersion: 1, NewlyPending: true,
		SOPProofs: metaProofs(), SOPAnswers: metaAnswers("yes"),
	}}
	enq := &fakeDistributionEnqueuer{}
	if _, err := metaDistributionService(t, store, enq).CompleteDistribution(context.Background(), metaDistributionInput("yes")); err != nil {
		t.Fatal(err)
	}
	assertCardMeta(t, enq.last.MediaRefs, enq.last.MediaMeta, enq.last.ContextRows, "Yes")
}

// A retry against a row already pending re-enqueues from the ROW: the answers the crew stored,
// never the answers this retry happens to carry.
func TestDistributionRepairEnqueueCarriesStoredAnswers(t *testing.T) {
	store := &fakeDistributionStore{completeResult: ports.CompleteDistributionResult{
		CompletionID: "c-1", Status: domain.DistributionStatusPendingVerification, RowVersion: 1, NewlyPending: false,
		SOPProofs: metaProofs(), SOPAnswers: metaAnswers("no"),
	}}
	enq := &fakeDistributionEnqueuer{}
	if _, err := metaDistributionService(t, store, enq).CompleteDistribution(context.Background(), metaDistributionInput("yes")); err != nil {
		t.Fatal(err)
	}
	if enq.calls != 1 {
		t.Fatalf("repair enqueue calls = %d, want 1", enq.calls)
	}
	if len(enq.last.ContextRows) != 1 || enq.last.ContextRows[0].Value != "No" {
		t.Fatalf("repair answers = %+v, want the stored No", enq.last.ContextRows)
	}
}

type metaPackingStore struct {
	fakePackingStore
	result ports.CompletePackingResult
}

func (m *metaPackingStore) CompletePacking(_ context.Context, p ports.CompletePackingParams) (ports.CompletePackingResult, error) {
	m.completeCalls = append(m.completeCalls, p)
	return m.result, nil
}

type recordingPackingEnqueuer struct {
	calls []FeedPackingVerificationEnqueueRequest
}

func (r *recordingPackingEnqueuer) EnqueueFeedPackingVerification(_ context.Context, in FeedPackingVerificationEnqueueRequest) error {
	r.calls = append(r.calls, in)
	return nil
}

func TestPackingEnqueueCarriesMediaMetaAndAnswers(t *testing.T) {
	store := &metaPackingStore{result: ports.CompletePackingResult{
		CompletionID: "pk-1", Status: domain.DistributionStatusPendingVerification, RowVersion: 1, NewlyPending: true, SOPProofs: metaProofs(),
	}}
	enq := &recordingPackingEnqueuer{}
	svc := NewService(nil, nil).
		WithIssueStore(metaIssueStore(t, domain.WorkflowNormal)).
		WithSOPRules(metaRulesSource()).
		WithPackingStore(store).
		WithPackingVerificationEnqueuer(enq).
		WithProofValidator(clipIsVideo()).
		WithNowFunc(func() time.Time { return targetDate() })
	_, err := svc.CompletePacking(context.Background(), CompletePackingInput{
		TenantID: testTenant, ParkID: testPark, ShedID: shedA, PartitionLabel: "1", SessionNo: 1, TargetDate: targetDate(),
		Workflow: domain.WorkflowNormal, Proofs: metaProofs(), Answers: metaAnswers("yes"),
		CompletedBy: "00000000-0000-4000-8000-000000000111", IdempotencyKey: "feed-packing-meta-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 1 {
		t.Fatalf("packing enqueue calls = %d", len(enq.calls))
	}
	assertCardMeta(t, enq.calls[0].MediaRefs, enq.calls[0].MediaMeta, enq.calls[0].ContextRows, "Yes")
}

func TestWastageEnqueueCarriesMediaMetaAndAnswers(t *testing.T) {
	store := &fakeWastageStore{result: ports.CompleteWastageResult{
		CompletionID: "w-1", Status: domain.WastageStatusPendingVerification, RowVersion: 1, NewlyPending: true, SOPProofs: metaProofs(),
	}}
	enq := &recordingWastageEnqueuer{}
	svc, _ := newWastageService(store, enq)
	svc = svc.WithSOPRules(metaRulesSource()).WithProofValidator(clipIsVideo())
	in := wastageInput(shedA)
	in.WastageProofRef = ""
	in.Proofs = metaProofs()
	in.Answers = metaAnswers("yes")
	if _, err := svc.CompleteWastage(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 1 {
		t.Fatalf("wastage enqueue calls = %d", len(enq.calls))
	}
	assertCardMeta(t, enq.calls[0].MediaRefs, enq.calls[0].MediaMeta, enq.calls[0].AnswerRows, "Yes")
}

type recordingTransportEnqueuer struct {
	calls []FeedTransportVerificationEnqueueRequest
}

func (r *recordingTransportEnqueuer) EnqueueFeedTransportVerification(_ context.Context, in FeedTransportVerificationEnqueueRequest) error {
	r.calls = append(r.calls, in)
	return nil
}

func metaTransportService(store *transportServiceStore, enq *recordingTransportEnqueuer, proofs ports.ProofValidator) *Service {
	return NewService(nil, nil).WithSOPRules(metaRulesSource()).WithTransportStore(store).WithProofValidator(proofs).WithTransportVerificationEnqueuer(enq)
}

func metaTransportStore(answer string, newlyPending bool) *transportServiceStore {
	return &transportServiceStore{
		task: ports.FeedTransportTask{TaskID: "task-1", ParkID: "park-1", ShedID: "shed-1", SOPVersion: metaCardVersion},
		result: ports.SubmitTransportResult{
			AttemptID: "attempt-1", ParkID: "park-1", ShedID: "shed-1", Status: "verification_due", AttemptNo: 1, NewlyPending: newlyPending,
			SOPProofs: metaProofs(), SOPAnswers: metaAnswers(answer),
		},
	}
}

func TestTransportEnqueueCarriesMediaMetaAndAnswers(t *testing.T) {
	enq := &recordingTransportEnqueuer{}
	// The replay carries a DIFFERENT answer; the enqueue must name what the attempt row stored.
	svc := metaTransportService(metaTransportStore("no", false), enq, clipIsVideo())
	if _, err := svc.SubmitTransport(context.Background(), SubmitTransportInput{
		TenantID: "tenant-1", TaskID: "task-1", OperatorID: "operator-1", IdempotencyKey: "transport-meta-1",
		Proofs: metaProofs(), Answers: metaAnswers("yes"),
	}); err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 1 {
		t.Fatalf("transport enqueue calls = %d", len(enq.calls))
	}
	assertCardMeta(t, enq.calls[0].MediaRefs, enq.calls[0].MediaMeta, enq.calls[0].AnswerRows, "No")
}

// With no register answer, an `either` slot is UNKNOWN on the verifier item -- the queue asks the
// proof register at read time -- never a guessed video.
func TestTransportProofMetaLeavesAnUnjudgedEitherSlotUnknown(t *testing.T) {
	enq := &recordingTransportEnqueuer{}
	svc := metaTransportService(metaTransportStore("yes", true), enq, &transportProofValidator{})
	if _, err := svc.SubmitTransport(context.Background(), SubmitTransportInput{
		TenantID: "tenant-1", TaskID: "task-1", OperatorID: "operator-1", IdempotencyKey: "transport-meta-2",
		Proofs: metaProofs(), Answers: metaAnswers("yes"),
	}); err != nil {
		t.Fatal(err)
	}
	meta := enq.calls[0].MediaMeta
	if len(meta) != 2 || meta[0].Kind != "photo" || meta[1].Kind != "" {
		t.Fatalf("transport meta = %+v, want the photo slot typed and the unjudged either slot blank", meta)
	}
}

func TestCanonicalProofMetaLeavesAnUnjudgedEitherSlotUnknown(t *testing.T) {
	rules := metaCard(domain.StageDistribution)
	judged := []judgedProof{
		{Slot: rules.Proofs[0], Ref: "p-bag", Kind: authored.KindPhoto},
		{Slot: rules.Proofs[1], Ref: "p-clip", Kind: authored.KindEither},
	}
	for name, meta := range map[string][]ports.ProofMeta{
		"canonical": canonicalProofMeta(rules, metaProofs(), judged),
		"request":   proofMeta(judged),
		// A stored ref this request never judged falls back to the SLOT's kind -- blank for either.
		"unjudged": canonicalProofMeta(rules, metaProofs(), nil),
	} {
		if len(meta) != 2 || meta[0].Kind != "photo" || meta[1].Kind != "" || meta[1].Label != "Crew clip" {
			t.Fatalf("%s meta = %+v, want the either slot left unknown", name, meta)
		}
	}
}
