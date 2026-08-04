package sopbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

type captureVaccinationRecorder struct {
	calls       int
	task        string
	sub         string
	by          string
	count       int
	completions []vaccinationdomain.SubmissionCompletion
}

func (r *captureVaccinationRecorder) RecordCompletionsFromSubmission(_ context.Context, _ string, taskID, submissionID, recordedBy string) (int, error) {
	r.calls++
	r.task = taskID
	r.sub = submissionID
	r.by = recordedBy
	return r.count, nil
}

func (r *captureVaccinationRecorder) SubmissionCompletions(_ context.Context, _ string, _ string) ([]vaccinationdomain.SubmissionCompletion, error) {
	return append([]vaccinationdomain.SubmissionCompletion(nil), r.completions...), nil
}

func (r *captureVaccinationRecorder) CompletedProofRefsByTask(_ context.Context, _ string, _ string) (map[string][]string, error) {
	out := make(map[string][]string)
	for _, completion := range r.completions {
		if completion.GoatID == "" || len(completion.ProofRefIDs) == 0 {
			continue
		}
		out[completion.GoatID] = append(out[completion.GoatID], completion.ProofRefIDs...)
	}
	return out, nil
}

func TestVaccinationSubmissionBridgeOnlyRecordsVaccinationTasks(t *testing.T) {
	rec := &captureVaccinationRecorder{count: 2}
	bridge := NewVaccinationSubmissionBridge(rec)
	submission := sopdomain.SubmissionSummary{SubmissionID: "sub-1", SubmittedBy: "operator-1"}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	if rec.calls != 1 || rec.task != "task-1" || rec.sub != "sub-1" || rec.by != "operator-1" {
		t.Fatalf("recorder = %+v", rec)
	}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-2", SOPCode: "shifting"}, submission); err != nil {
		t.Fatalf("shifting submit: %v", err)
	}
	if rec.calls != 1 {
		t.Fatalf("non-vaccination task should not record, calls=%d", rec.calls)
	}
}

func TestVaccinationSubmissionBridgeFailsZeroMaterializedCompletions(t *testing.T) {
	rec := &captureVaccinationRecorder{}
	bridge := NewVaccinationSubmissionBridge(rec)
	submission := sopdomain.SubmissionSummary{SubmissionID: "sub-1", SubmittedBy: "operator-1"}
	err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.session"}, submission)
	if !errors.Is(err, ErrNoVaccinationCompletions) {
		t.Fatalf("err = %v, want ErrNoVaccinationCompletions", err)
	}
}

type captureVerificationProducer struct {
	calls int
	last  verificationdomain.CreateItem
	err   error
}

func (p *captureVerificationProducer) CreateItem(_ context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error) {
	p.calls++
	p.last = in
	if p.err != nil {
		return verificationdomain.CreateItemResult{}, p.err
	}
	return verificationdomain.CreateItemResult{Item: verificationdomain.Item{ItemID: "item-1", TenantID: in.TenantID}, Created: true}, nil
}

func TestVaccinationSubmissionBridgeEmitsOneVerificationItemPerShedSubmissionWithAllClips(t *testing.T) {
	administeredAt := time.Date(2026, 7, 13, 7, 55, 0, 0, time.UTC)
	rec := &captureVaccinationRecorder{
		count: 2,
		completions: []vaccinationdomain.SubmissionCompletion{
			{CompletionID: "completion-1", SubmissionID: "sub-1", GoatID: "goat-1", ShedID: "shed-1", ParkID: "park-1", AdministeredAt: administeredAt},
			// Two vaccines administered during the same handling still produce ONE goat proof item.
			{CompletionID: "completion-2", SubmissionID: "sub-1", GoatID: "goat-1", ShedID: "shed-1", ParkID: "park-1", AdministeredAt: administeredAt.Add(time.Minute)},
		},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	goatID := "goat-1"
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		SubmittedAt:  "2026-07-13T08:00:00Z",
		ProofRefs: []sopdomain.ProofReference{
			{ProofID: "proof-1", SubjectType: "goat", SubjectID: &goatID},
			{ProofID: "proof-2", SubjectType: "goat", SubjectID: &goatID},
		},
	}
	task := sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive", ScopeType: "shed", ScopeID: "shed-1"}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", task, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	if producer.calls != 1 {
		t.Fatalf("verification producer calls = %d, want 1", producer.calls)
	}
	if got := producer.last; got.TenantID != "tenant-1" || got.Module != "vaccination" || got.Category != VaccinationVerificationCategory {
		t.Fatalf("verification create item = %+v", got)
	}
	if len(producer.last.MediaRefs) != 2 {
		t.Fatalf("media refs = %v, want 2 entries", producer.last.MediaRefs)
	}
	if producer.last.ShedID == nil || *producer.last.ShedID != "shed-1" {
		t.Fatalf("shed id = %v, want shed-1", producer.last.ShedID)
	}
	if producer.last.ParkID == nil || *producer.last.ParkID != "park-1" {
		t.Fatalf("park id = %v, want park-1", producer.last.ParkID)
	}
	if producer.last.Source.RefType != "sop_submission" || producer.last.Source.RefID != "sub-1" {
		t.Fatalf("source = %+v, want sop_submission/sub-1", producer.last.Source)
	}
	if !producer.last.CapturedAt.Equal(administeredAt) {
		t.Fatalf("captured at = %s, want operator administered_at %s", producer.last.CapturedAt, administeredAt)
	}
	if producer.last.IdempotencyKey != "vaccination:submission:sub-1" {
		t.Fatalf("idempotency key = %q", producer.last.IdempotencyKey)
	}
}

func TestVaccinationSubmissionBridgeUsesCompletionProofRefsForShedAck(t *testing.T) {
	administeredAt := time.Date(2026, 7, 13, 7, 55, 0, 0, time.UTC)
	rec := &captureVaccinationRecorder{
		count: 1,
		completions: []vaccinationdomain.SubmissionCompletion{{
			CompletionID:   "completion-1",
			SubmissionID:   "sub-1",
			GoatID:         "goat-1",
			ShedID:         "shed-1",
			ParkID:         "park-1",
			ProofRefIDs:    []string{"proof-from-backend-artifact"},
			AdministeredAt: administeredAt,
		}},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		// Shed completion is an acknowledgement. The operator does not fill or see a proof_refs
		// form field; the bridge must use the completed proof artifacts already attached to the
		// scanned goat rows.
		ProofRefs: nil,
	}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	if producer.calls != 1 {
		t.Fatalf("verification producer calls = %d, want 1", producer.calls)
	}
	if got := producer.last.MediaRefs; len(got) != 1 || got[0] != "proof-from-backend-artifact" {
		t.Fatalf("media refs = %v, want backend proof artifact", got)
	}
}

func TestVaccinationSubmissionBridgeUsesShedLevelVideoForOneShedVerificationItem(t *testing.T) {
	administeredAt := time.Date(2026, 7, 13, 7, 55, 0, 0, time.UTC)
	rec := &captureVaccinationRecorder{
		count: 2,
		completions: []vaccinationdomain.SubmissionCompletion{
			{CompletionID: "completion-1", SubmissionID: "sub-1", GoatID: "goat-1", ShedID: "shed-1", ShedLabel: "Godel 2 - Part 4", ParkID: "park-1", AdministeredAt: administeredAt},
			{CompletionID: "completion-2", SubmissionID: "sub-1", GoatID: "goat-2", ShedID: "shed-1", ShedLabel: "Godel 2 - Part 4", ParkID: "park-1", AdministeredAt: administeredAt.Add(time.Minute)},
		},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	shedID := "shed-1"
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		ProofRefs: []sopdomain.ProofReference{
			{ProofID: "shed-video-1", SubjectType: "shed", SubjectID: &shedID},
		},
	}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	if producer.calls != 1 {
		t.Fatalf("verification producer calls = %d, want 1", producer.calls)
	}
	if got := producer.last.MediaRefs; len(got) != 1 || got[0] != "shed-video-1" {
		t.Fatalf("media refs = %v, want shed-level video proof", got)
	}
	if producer.last.Source.RefType != "sop_submission" || producer.last.Source.RefID != "sub-1" {
		t.Fatalf("source = %+v, want one shed submission verification item", producer.last.Source)
	}
	if producer.last.SubjectLabel == nil || *producer.last.SubjectLabel != "Godel 2 - Part 4 · 2 goats" {
		t.Fatalf("subject label = %v, want shed/partition context", producer.last.SubjectLabel)
	}
}

func TestVaccinationSubmissionBridgeLabelsGroupedShedSubmissionHonestly(t *testing.T) {
	administeredAt := time.Date(2026, 7, 13, 7, 55, 0, 0, time.UTC)
	rec := &captureVaccinationRecorder{
		count: 3,
		completions: []vaccinationdomain.SubmissionCompletion{
			{CompletionID: "completion-1", SubmissionID: "sub-1", GoatID: "goat-1", ShedID: "shed-1", ShedLabel: "Godel 1", ParkID: "park-1", AdministeredAt: administeredAt},
			{CompletionID: "completion-2", SubmissionID: "sub-1", GoatID: "goat-2", ShedID: "shed-2", ShedLabel: "Godel 2 - Part 4", ParkID: "park-1", AdministeredAt: administeredAt.Add(time.Minute)},
			{CompletionID: "completion-3", SubmissionID: "sub-1", GoatID: "goat-3", ShedID: "shed-3", ShedLabel: "Mandela 2", ParkID: "park-1", AdministeredAt: administeredAt.Add(2 * time.Minute)},
		},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		ProofRefs:    []sopdomain.ProofReference{{ProofID: "group-video", SubjectType: "shed"}},
	}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	if producer.last.SubjectLabel == nil || *producer.last.SubjectLabel != "3 sheds · 3 goats" {
		t.Fatalf("subject label = %v, want grouped shed context", producer.last.SubjectLabel)
	}
}

func TestVaccinationSubmissionBridgeDoesNotEmitBarePartitionLabel(t *testing.T) {
	administeredAt := time.Date(2026, 7, 13, 7, 55, 0, 0, time.UTC)
	rec := &captureVaccinationRecorder{
		count: 1,
		completions: []vaccinationdomain.SubmissionCompletion{{
			CompletionID:   "completion-1",
			SubmissionID:   "sub-1",
			GoatID:         "goat-1",
			ShedID:         "shed-1",
			ShedLabel:      " - Part 4",
			ParkID:         "park-1",
			AdministeredAt: administeredAt,
		}},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		ProofRefs:    []sopdomain.ProofReference{{ProofID: "shed-video", SubjectType: "shed"}},
	}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	if producer.last.SubjectLabel == nil || *producer.last.SubjectLabel != "shed-1 · 1 goats" {
		t.Fatalf("subject label = %v, want shed id fallback instead of bare partition", producer.last.SubjectLabel)
	}
}

func TestVaccinationSubmissionBridgeFailsWhenScannedGoatHasNoCameraProof(t *testing.T) {
	rec := &captureVaccinationRecorder{
		count: 1,
		completions: []vaccinationdomain.SubmissionCompletion{{
			CompletionID:   "completion-1",
			SubmissionID:   "sub-1",
			GoatID:         "goat-1",
			AdministeredAt: time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC),
		}},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{SubmissionID: "sub-1", SubmittedBy: "operator-1"}
	err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission)
	if !errors.Is(err, ErrMissingGoatProof) {
		t.Fatalf("err = %v, want ErrMissingGoatProof", err)
	}
	if producer.calls != 0 {
		t.Fatalf("verification producer calls = %d, want 0", producer.calls)
	}
}

func TestVaccinationSubmissionBridgeFailsWhenAnyScannedGoatLacksCameraProof(t *testing.T) {
	rec := &captureVaccinationRecorder{
		count: 2,
		completions: []vaccinationdomain.SubmissionCompletion{
			{
				CompletionID:   "completion-1",
				SubmissionID:   "sub-1",
				GoatID:         "goat-1",
				AdministeredAt: time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC),
			},
			{
				CompletionID:   "completion-2",
				SubmissionID:   "sub-1",
				GoatID:         "goat-2",
				AdministeredAt: time.Date(2026, 7, 13, 8, 1, 0, 0, time.UTC),
			},
		},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	goatID := "goat-1"
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		ProofRefs:    []sopdomain.ProofReference{{ProofID: "proof-goat-1", SubjectType: "goat", SubjectID: &goatID}},
	}
	err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission)
	if !errors.Is(err, ErrMissingGoatProof) {
		t.Fatalf("err = %v, want ErrMissingGoatProof", err)
	}
	if producer.calls != 0 {
		t.Fatalf("verification producer calls = %d, want 0", producer.calls)
	}
}

func TestVaccinationSubmissionBridgeFailsClosedOnVerificationError(t *testing.T) {
	administeredAt := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	rec := &captureVaccinationRecorder{
		count: 1,
		completions: []vaccinationdomain.SubmissionCompletion{{
			CompletionID:   "completion-1",
			SubmissionID:   "sub-1",
			GoatID:         "goat-1",
			AdministeredAt: administeredAt,
		}},
	}
	producer := &captureVerificationProducer{err: errors.New("boom")}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	goatID := "goat-1"
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		ProofRefs:    []sopdomain.ProofReference{{ProofID: "proof-1", SubjectType: "goat", SubjectID: &goatID}},
	}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission); err == nil {
		t.Fatal("verification producer failure must fail the submission fanout")
	}
	if producer.calls != 1 {
		t.Fatalf("verification producer calls = %d, want 1", producer.calls)
	}
}

// Submit-time obligation in_progress marking (a since-removed ObligationInProgressMarker /
// WithObligationInProgressMarker wiring) was removed as part of the PEND-1 redesign: it was
// unreachable in the intended shape for this offline-first submit flow. The reachable in_progress
// trigger now lives entirely in obligation.Repository.MarkCompleted's sibling-transition logic; see
// backend/internal/obligation/adapters/postgres/inprogress_integration_test.go for its proof.

// TestVaccinationSubmissionBridgeDropsForeignShedProofRefsFromCumulativePayload is the adversarial
// two-shed regression for the media_refs shed leak: the Android client posts a CUMULATIVE payload,
// so by the Nth shed submission of a shared parent task it re-sends every earlier shed's goat
// proof_refs. sop_submission_items is already narrowed server-side to goats whose goats.shed_id
// matches the submission's shed subject; verification_items.media_refs was NOT, so a verifier
// reviewing shed B was shown shed A's animals as evidence. media_refs must carry only the proofs of
// goats that actually belong to this submission's shed.
func TestVaccinationSubmissionBridgeDropsForeignShedProofRefsFromCumulativePayload(t *testing.T) {
	administeredAt := time.Date(2026, 7, 13, 7, 55, 0, 0, time.UTC)
	goatA1, goatA2 := "goat-a1", "goat-a2"
	goatB1, goatB2 := "goat-b1", "goat-b2"
	shedA, shedB := "shed-a", "shed-b"

	// Submission 2 of the parent task: server fanout materialized ONLY shed B's completions
	// (the shed filter did its job), but the client payload still carries shed A's proofs.
	rec := &captureVaccinationRecorder{
		count: 2,
		completions: []vaccinationdomain.SubmissionCompletion{
			{CompletionID: "c-b1", SubmissionID: "sub-2", GoatID: goatB1, ShedID: shedB, ShedLabel: "Mandela 2", ParkID: "park-1", AdministeredAt: administeredAt},
			{CompletionID: "c-b2", SubmissionID: "sub-2", GoatID: goatB2, ShedID: shedB, ShedLabel: "Mandela 2", ParkID: "park-1", AdministeredAt: administeredAt.Add(time.Minute)},
		},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-2",
		SubmittedBy:  "operator-1",
		ProofRefs: []sopdomain.ProofReference{
			{ProofID: "proof-a1", SubjectType: "goat", SubjectID: &goatA1}, // shed A - foreign
			{ProofID: "proof-a2", SubjectType: "goat", SubjectID: &goatA2}, // shed A - foreign
			{ProofID: "proof-b1", SubjectType: "goat", SubjectID: &goatB1},
			{ProofID: "proof-b2", SubjectType: "goat", SubjectID: &goatB2},
			{ProofID: "shed-video-a", SubjectType: "shed", SubjectID: &shedA}, // shed A - foreign
			{ProofID: "shed-video-b", SubjectType: "shed", SubjectID: &shedB},
		},
	}
	task := sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", task, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	if producer.calls != 1 {
		t.Fatalf("verification producer calls = %d, want 1", producer.calls)
	}
	got := map[string]bool{}
	for _, ref := range producer.last.MediaRefs {
		got[ref] = true
	}
	for _, foreign := range []string{"proof-a1", "proof-a2", "shed-video-a"} {
		if got[foreign] {
			t.Fatalf("media_refs leaked foreign-shed proof %q: %v", foreign, producer.last.MediaRefs)
		}
	}
	for _, own := range []string{"proof-b1", "proof-b2", "shed-video-b"} {
		if !got[own] {
			t.Fatalf("media_refs dropped own-shed proof %q: %v", own, producer.last.MediaRefs)
		}
	}
	if len(producer.last.MediaRefs) != 3 {
		t.Fatalf("media refs = %v, want exactly shed B's 3 proofs", producer.last.MediaRefs)
	}

	// Mirror image: the shed A submission must carry ONLY shed A's proofs.
	recA := &captureVaccinationRecorder{
		count: 2,
		completions: []vaccinationdomain.SubmissionCompletion{
			{CompletionID: "c-a1", SubmissionID: "sub-1", GoatID: goatA1, ShedID: shedA, ShedLabel: "Castro 1", ParkID: "park-1", AdministeredAt: administeredAt},
			{CompletionID: "c-a2", SubmissionID: "sub-1", GoatID: goatA2, ShedID: shedA, ShedLabel: "Castro 1", ParkID: "park-1", AdministeredAt: administeredAt},
		},
	}
	producerA := &captureVerificationProducer{}
	bridgeA := NewVaccinationSubmissionBridge(recA).WithVerificationProducer(producerA)
	submissionA := submission
	submissionA.SubmissionID = "sub-1"
	if err := bridgeA.OnTaskSubmitted(context.Background(), "tenant-1", task, submissionA); err != nil {
		t.Fatalf("shed A submit: %v", err)
	}
	gotA := map[string]bool{}
	for _, ref := range producerA.last.MediaRefs {
		gotA[ref] = true
	}
	for _, foreign := range []string{"proof-b1", "proof-b2", "shed-video-b"} {
		if gotA[foreign] {
			t.Fatalf("shed A media_refs leaked shed B proof %q: %v", foreign, producerA.last.MediaRefs)
		}
	}
	if len(producerA.last.MediaRefs) != 3 {
		t.Fatalf("shed A media refs = %v, want exactly shed A's 3 proofs", producerA.last.MediaRefs)
	}
}
