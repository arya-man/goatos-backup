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

func TestVaccinationSubmissionBridgeEmitsOneVerificationItemPerGoatWithAllClips(t *testing.T) {
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
	if producer.last.Source.RefType != "vaccination_goat" || producer.last.Source.RefID != goatID {
		t.Fatalf("source = %+v, want vaccination_goat/%s", producer.last.Source, goatID)
	}
	if !producer.last.CapturedAt.Equal(administeredAt) {
		t.Fatalf("captured at = %s, want operator administered_at %s", producer.last.CapturedAt, administeredAt)
	}
	if producer.last.IdempotencyKey != "vaccination:submission:sub-1:goat:goat-1" {
		t.Fatalf("idempotency key = %q", producer.last.IdempotencyKey)
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
