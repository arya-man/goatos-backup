package sopbridge

import (
	"context"
	"errors"
	"log/slog"
	"time"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

var ErrNoVaccinationCompletions = errors.New("vaccination fanout materialized no completions")

// VaccinationVerificationCategory is the type-registry category vaccination proof submissions
// register under (verification-module-design.md §2.3). Registered at composition time via
// verification/app.Service.RegisterCategory.
const VaccinationVerificationCategory = "vaccination_proof"

type VaccinationSubmissionRecorder interface {
	RecordCompletionsFromSubmission(ctx context.Context, tenantID, taskID, submissionID, recordedBy string) (int, error)
}

// VerificationProducer is the minimal slice of the generic Verification module's app.Service a
// producer needs: enqueue one item. sopbridge composes vaccination + verification without either
// module reaching into the other's tables (backend/AGENTS.md: "do not write another module's tables
// directly").
type VerificationProducer interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

type VaccinationSubmissionBridge struct {
	recorder     VaccinationSubmissionRecorder
	verification VerificationProducer
	log          *slog.Logger
}

func NewVaccinationSubmissionBridge(recorder VaccinationSubmissionRecorder) *VaccinationSubmissionBridge {
	return &VaccinationSubmissionBridge{recorder: recorder, log: slog.Default()}
}

// WithVerificationProducer wires the additive verification-item hook (build-handover-20260713.md §1
// P0 1a). Optional — a bridge without it behaves exactly as before (recorder-only).
func (b *VaccinationSubmissionBridge) WithVerificationProducer(p VerificationProducer) *VaccinationSubmissionBridge {
	b.verification = p
	return b
}

func (b *VaccinationSubmissionBridge) WithLogger(log *slog.Logger) *VaccinationSubmissionBridge {
	if log != nil {
		b.log = log
	}
	return b
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
	// Additive verification hook: enqueue the daily independent double-verification item for this
	// drive/session's proof media. Best-effort and NEVER returned as an error — the sop submission
	// fanout above is wired fail-closed (SubmitTask itself fails if OnTaskSubmitted errors), so a
	// verification-side hiccup must not block a real vaccination proof submission. This means
	// delivery here is at-least-once on the HAPPY path only; a stricter durable retry (its own
	// outbox-backed queue) is a follow-up, not built in this pass.
	b.emitVerificationItem(ctx, tenantID, task, submission)
	return nil
}

func (b *VaccinationSubmissionBridge) emitVerificationItem(ctx context.Context, tenantID string, task sopdomain.TaskSummary, submission sopdomain.SubmissionSummary) {
	if b == nil || b.verification == nil {
		return
	}
	mediaRefs := make([]string, 0, len(submission.ProofRefs))
	for _, ref := range submission.ProofRefs {
		if ref.ProofID != "" {
			mediaRefs = append(mediaRefs, ref.ProofID)
		}
	}
	if len(mediaRefs) == 0 {
		// Nothing captured to independently verify.
		return
	}
	capturedAt, parseErr := time.Parse(time.RFC3339Nano, submission.SubmittedAt)
	if parseErr != nil {
		capturedAt = time.Now().UTC()
	}
	taskID := task.TaskID
	submissionID := submission.SubmissionID
	var shedID *string
	if task.ScopeType == "shed" && task.ScopeID != "" {
		scope := task.ScopeID
		shedID = &scope
	}
	var operatorID *string
	if submission.SubmittedBy != "" {
		by := submission.SubmittedBy
		operatorID = &by
	}
	_, err := b.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID: tenantID,
		Vertical: "preventive_care",
		Module:   "vaccination",
		Category: VaccinationVerificationCategory,
		Source: verificationdomain.SourceRef{
			Module:       "vaccination",
			TaskID:       &taskID,
			SubmissionID: &submissionID,
			RefType:      "sop_submission",
			RefID:        submissionID,
		},
		MediaRefs:      mediaRefs,
		OperatorID:     operatorID,
		ShedID:         shedID,
		CapturedAt:     capturedAt,
		IdempotencyKey: "vaccination:submission:" + submissionID,
	})
	if err != nil && b.log != nil {
		b.log.Error("verification_item_emit_failed",
			slog.String("tenant_id", tenantID),
			slog.String("task_id", taskID),
			slog.String("submission_id", submissionID),
			slog.String("error", err.Error()),
		)
	}
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
