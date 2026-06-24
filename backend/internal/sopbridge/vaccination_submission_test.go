package sopbridge

import (
	"context"
	"errors"
	"testing"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
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
