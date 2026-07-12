package sopbridge

import (
	"context"
	"errors"
	"testing"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

type captureVaccinationRecorder struct {
	calls int
	task  string
	sub   string
	by    string
	count int
}

func (r *captureVaccinationRecorder) RecordCompletionsFromSubmission(_ context.Context, _ string, taskID, submissionID, recordedBy string) (int, error) {
	r.calls++
	r.task = taskID
	r.sub = submissionID
	r.by = recordedBy
	return r.count, nil
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

func TestVaccinationSubmissionBridgeEmitsVerificationItemWithMedia(t *testing.T) {
	rec := &captureVaccinationRecorder{count: 1}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		SubmittedAt:  "2026-07-13T08:00:00Z",
		ProofRefs: []sopdomain.ProofReference{
			{ProofID: "proof-1"},
			{ProofID: "proof-2"},
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
	if producer.last.IdempotencyKey != "vaccination:submission:sub-1" {
		t.Fatalf("idempotency key = %q", producer.last.IdempotencyKey)
	}
}

func TestVaccinationSubmissionBridgeSkipsVerificationItemWithoutMedia(t *testing.T) {
	rec := &captureVaccinationRecorder{count: 1}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{SubmissionID: "sub-1", SubmittedBy: "operator-1"}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	if producer.calls != 0 {
		t.Fatalf("verification producer calls = %d, want 0 (no media captured)", producer.calls)
	}
}

func TestVaccinationSubmissionBridgeNeverFailsSubmissionOnVerificationError(t *testing.T) {
	rec := &captureVaccinationRecorder{count: 1}
	producer := &captureVerificationProducer{err: errors.New("boom")}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		ProofRefs:    []sopdomain.ProofReference{{ProofID: "proof-1"}},
	}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission); err != nil {
		t.Fatalf("verification producer failure must not fail the vaccination submission fanout: %v", err)
	}
	if producer.calls != 1 {
		t.Fatalf("verification producer calls = %d, want 1", producer.calls)
	}
}
