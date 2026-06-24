package sopbridge

import (
	"context"
	"errors"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

var ErrNoVaccinationCompletions = errors.New("vaccination fanout materialized no completions")

type VaccinationSubmissionRecorder interface {
	RecordCompletionsFromSubmission(ctx context.Context, tenantID, taskID, submissionID, recordedBy string) (int, error)
}

type VaccinationSubmissionBridge struct {
	recorder VaccinationSubmissionRecorder
}

func NewVaccinationSubmissionBridge(recorder VaccinationSubmissionRecorder) *VaccinationSubmissionBridge {
	return &VaccinationSubmissionBridge{recorder: recorder}
}

func (b *VaccinationSubmissionBridge) OnTaskSubmitted(ctx context.Context, tenantID string, task sopdomain.TaskSummary, submission sopdomain.SubmissionSummary) error {
	if b == nil || b.recorder == nil || !isVaccinationTask(task) {
		return nil
	}
	count, err := b.recorder.RecordCompletionsFromSubmission(ctx, tenantID, task.TaskID, submission.SubmissionID, submission.SubmittedBy)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNoVaccinationCompletions
	}
	return nil
}

func isVaccinationTask(task sopdomain.TaskSummary) bool {
	switch task.SOPCode {
	case "vaccination.drive", "vaccination.session":
		return true
	}
	switch task.TaskType {
	case "vaccination", "vaccination_drive", "vaccination_session":
		return true
	default:
		return false
	}
}
