package sopbridge

import (
	"context"
	"testing"
	"time"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// A shed's proof is captured one clip per animal, so the VERDICT has to be issuable one animal at
// a time. Before the fan-out, five animals produced ONE verification item holding five clips and a
// single Approve/Reject: a verifier who saw one bad clip could only reject the whole shed, sending
// four correctly-vaccinated animals back to be re-scanned and re-filmed.
func TestVaccinationSubmissionBridgeEmitsOneVerificationItemPerAnimal(t *testing.T) {
	administeredAt := time.Date(2026, 8, 4, 3, 24, 0, 0, time.UTC)
	completions := make([]vaccinationdomain.SubmissionCompletion, 0, 5)
	proofRefs := make([]sopdomain.ProofReference, 0, 5)
	goatIDs := []string{"goat-1", "goat-2", "goat-3", "goat-4", "goat-5"}
	for i, goatID := range goatIDs {
		id := goatID
		completions = append(completions, vaccinationdomain.SubmissionCompletion{
			CompletionID:   "completion-" + id,
			SubmissionID:   "sub-1",
			GoatID:         id,
			GoatLabel:      "G-00600" + string(rune('1'+i)),
			ShedID:         "shed-1",
			ShedLabel:      "Gandhi 1",
			ParkID:         "park-1",
			AdministeredAt: administeredAt.Add(time.Duration(i) * time.Minute),
		})
		proofRefs = append(proofRefs, sopdomain.ProofReference{
			ProofID:     "proof-" + id,
			SubjectType: "goat",
			SubjectID:   &id,
		})
	}

	rec := &captureVaccinationRecorder{count: len(completions), completions: completions}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		SubmittedAt:  "2026-08-04T03:30:00Z",
		ProofRefs:    proofRefs,
	}
	task := sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive", ScopeType: "shed", ScopeID: "shed-1"}

	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", task, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}

	if producer.calls != len(goatIDs) {
		t.Fatalf("verification items = %d, want one per animal (%d) -- a single shed-grain item means one bad clip rejects the whole shed", producer.calls, len(goatIDs))
	}

	seen := make(map[string]int, len(goatIDs))
	for _, item := range producer.items {
		if item.Source.RefType != "vaccination_goat" {
			t.Fatalf("ref type = %q, want vaccination_goat (the ref type the per-goat applier routes to ApplyGoatVerification)", item.Source.RefType)
		}
		if len(item.MediaRefs) != 1 {
			t.Fatalf("goat %s media refs = %v, want exactly its own single clip", item.Source.RefID, item.MediaRefs)
		}
		if item.MediaRefs[0] != "proof-"+item.Source.RefID {
			t.Fatalf("goat %s carries clip %q -- an item must never hold another animal's evidence", item.Source.RefID, item.MediaRefs[0])
		}
		wantKey := "vaccination:submission:sub-1:goat:" + item.Source.RefID
		if item.IdempotencyKey != wantKey {
			t.Fatalf("idempotency key = %q, want %q", item.IdempotencyKey, wantKey)
		}
		if item.Source.SubmissionID == nil || *item.Source.SubmissionID != "sub-1" {
			t.Fatalf("submission id = %v, want sub-1 (the grouping key the queue shows as one shed card)", item.Source.SubmissionID)
		}
		if item.SubjectLabel == nil || *item.SubjectLabel == "" {
			t.Fatalf("goat %s has no subject label -- the verifier must be told which animal she is judging", item.Source.RefID)
		}
		seen[item.Source.RefID]++
	}
	for _, goatID := range goatIDs {
		if seen[goatID] != 1 {
			t.Fatalf("goat %s produced %d items, want exactly 1", goatID, seen[goatID])
		}
	}
}

// Each animal's card sorts on ITS OWN capture time. Stamping every item with the submission-wide
// earliest would collapse a shed's animals onto one timestamp and scramble the queue's order.
func TestVaccinationPerAnimalItemsCarryTheirOwnCaptureTime(t *testing.T) {
	first := time.Date(2026, 8, 4, 3, 24, 0, 0, time.UTC)
	second := first.Add(11 * time.Second)
	goatA, goatB := "goat-a", "goat-b"
	rec := &captureVaccinationRecorder{
		count: 2,
		completions: []vaccinationdomain.SubmissionCompletion{
			{CompletionID: "c-a", SubmissionID: "sub-2", GoatID: goatA, GoatLabel: "G-1", ShedID: "shed-1", ShedLabel: "Gandhi 1", ParkID: "park-1", AdministeredAt: first},
			{CompletionID: "c-b", SubmissionID: "sub-2", GoatID: goatB, GoatLabel: "G-2", ShedID: "shed-1", ShedLabel: "Gandhi 1", ParkID: "park-1", AdministeredAt: second},
		},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-2",
		SubmittedBy:  "operator-1",
		ProofRefs: []sopdomain.ProofReference{
			{ProofID: "proof-a", SubjectType: "goat", SubjectID: &goatA},
			{ProofID: "proof-b", SubjectType: "goat", SubjectID: &goatB},
		},
	}
	task := sopdomain.TaskSummary{TaskID: "task-2", SOPCode: "vaccination.drive", ScopeType: "shed", ScopeID: "shed-1"}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", task, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	byGoat := map[string]time.Time{}
	for _, item := range producer.items {
		byGoat[item.Source.RefID] = item.CapturedAt
	}
	if !byGoat[goatA].Equal(first) {
		t.Fatalf("goat-a captured_at = %s, want its own %s", byGoat[goatA], first)
	}
	if !byGoat[goatB].Equal(second) {
		t.Fatalf("goat-b captured_at = %s, want its own %s", byGoat[goatB], second)
	}
}

// The P0 shed-leak fix must survive the fan-out. The Android client posts a CUMULATIVE payload, so
// an earlier shed's goat clips re-arrive on this submission; they belong to no animal here and
// must not become this shed's evidence -- per-animal items make that leak worse, not better, since
// a stray ref would fabricate an item for an animal that is not in this shed at all.
func TestVaccinationPerAnimalFanOutKeepsShedMembershipScoping(t *testing.T) {
	administeredAt := time.Date(2026, 8, 4, 3, 24, 0, 0, time.UTC)
	mine, foreign := "goat-mine", "goat-other-shed"
	rec := &captureVaccinationRecorder{
		count: 1,
		completions: []vaccinationdomain.SubmissionCompletion{
			{CompletionID: "c-1", SubmissionID: "sub-3", GoatID: mine, GoatLabel: "G-9", ShedID: "shed-1", ShedLabel: "Gandhi 1", ParkID: "park-1", AdministeredAt: administeredAt},
		},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-3",
		SubmittedBy:  "operator-1",
		ProofRefs: []sopdomain.ProofReference{
			{ProofID: "proof-mine", SubjectType: "goat", SubjectID: &mine},
			// Cumulative payload residue from an earlier shed of the same parent task.
			{ProofID: "proof-foreign", SubjectType: "goat", SubjectID: &foreign},
		},
	}
	task := sopdomain.TaskSummary{TaskID: "task-3", SOPCode: "vaccination.drive", ScopeType: "shed", ScopeID: "shed-1"}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", task, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	if producer.calls != 1 {
		t.Fatalf("items = %d, want 1 -- only this shed's animal may be raised", producer.calls)
	}
	for _, item := range producer.items {
		if item.Source.RefID == foreign {
			t.Fatalf("raised an item for %s, which is not a member of this submission", foreign)
		}
		for _, ref := range item.MediaRefs {
			if ref == "proof-foreign" {
				t.Fatalf("another shed's clip %q became this shed's evidence", ref)
			}
		}
	}
}

// REGRESSION (found on a real submit against the QA stack, 2026-08-07): a submission
// spanning two sheds stamped EVERY per-animal item with the FIRST shed seen, because the
// bridge computed one submission-wide shedID and reused it. Two Castro 2 goats were filed
// under Mandela 2.
//
// This is worse than a mislabel. PartitionLabel is already per-goat, so pairing it with a
// different animal's shed composes a location that does not exist -- and the leadership
// shed filter then hides the evidence under a shed the animal was never in.
func TestVaccinationPerAnimalItemsCarryTheirOwnShedAcrossAMultiShedSubmission(t *testing.T) {
	administeredAt := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	castroGoat, mandelaGoat := "goat-castro", "goat-mandela"
	rec := &captureVaccinationRecorder{
		count: 2,
		completions: []vaccinationdomain.SubmissionCompletion{
			// Castro first, so the old code's "first shed wins" would stamp BOTH as Castro;
			// asserting both directions below means neither ordering can pass by luck.
			{CompletionID: "c-castro", SubmissionID: "sub-multi", GoatID: castroGoat, GoatLabel: "C-1", ShedID: "shed-castro-2", ShedLabel: "Castro 2", ParkID: "park-1", AdministeredAt: administeredAt},
			{CompletionID: "c-mandela", SubmissionID: "sub-multi", GoatID: mandelaGoat, GoatLabel: "M-1", ShedID: "shed-mandela-2", ShedLabel: "Mandela 2", ParkID: "park-1", AdministeredAt: administeredAt},
		},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-multi",
		SubmittedBy:  "operator-1",
		ProofRefs: []sopdomain.ProofReference{
			{ProofID: "proof-castro", SubjectType: "goat", SubjectID: &castroGoat},
			{ProofID: "proof-mandela", SubjectType: "goat", SubjectID: &mandelaGoat},
		},
	}
	task := sopdomain.TaskSummary{TaskID: "task-multi", SOPCode: "vaccination.drive", ScopeType: "park", ScopeID: "park-1"}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", task, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}

	shedByGoat := map[string]string{}
	for _, item := range producer.items {
		if item.ShedID != nil {
			shedByGoat[item.Source.RefID] = *item.ShedID
		}
	}
	if got := shedByGoat[castroGoat]; got != "shed-castro-2" {
		t.Fatalf("castro goat filed under shed %q, want its own shed-castro-2", got)
	}
	if got := shedByGoat[mandelaGoat]; got != "shed-mandela-2" {
		t.Fatalf("mandela goat filed under shed %q, want its own shed-mandela-2 (the submission-wide shed would have been shed-castro-2)", got)
	}
}
