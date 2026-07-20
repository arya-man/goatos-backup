package sopbridge

import (
	"context"
	"errors"
	"fmt"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

var ErrNoVaccinationCompletions = errors.New("vaccination fanout materialized no completions")
var ErrMissingGoatProof = errors.New("vaccination submission is missing a completed camera proof for a scanned goat")

// VaccinationVerificationCategory is the type-registry category vaccination proof submissions
// register under (verification-module-design.md §2.3). Registered at composition time via
// verification/app.Service.RegisterCategory.
const VaccinationVerificationCategory = "vaccination_proof"

type VaccinationSubmissionRecorder interface {
	RecordCompletionsFromSubmission(ctx context.Context, tenantID, taskID, submissionID, recordedBy string) (int, error)
	SubmissionCompletions(ctx context.Context, tenantID, submissionID string) ([]vaccinationdomain.SubmissionCompletion, error)
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
}

func NewVaccinationSubmissionBridge(recorder VaccinationSubmissionRecorder) *VaccinationSubmissionBridge {
	return &VaccinationSubmissionBridge{recorder: recorder}
}

// WithVerificationProducer wires the generic verification producer. Production composition always
// supplies it; recorder-only instances remain useful for focused vaccination fan-out tests.
func (b *VaccinationSubmissionBridge) WithVerificationProducer(p VerificationProducer) *VaccinationSubmissionBridge {
	b.verification = p
	return b
}

// OnTaskSubmitted no longer marks any obligation in_progress at submit time (PEND-1 REDESIGN): an
// earlier submit-time obligation.MarkInProgress writer was the wrong shape for this offline-first
// submit flow (no separate server-side "start" event exists here -- RecordCompletionsFromSubmission
// only ever materializes a 'recorded' vaccination_completions row; the obligation itself completes
// later via the separate verify/accept step). The reachable in_progress trigger now lives entirely
// in obligation.Repository.MarkCompleted (see that function's doc comment): the first real
// completion in a multi-obligation drive flips its still-open siblings to in_progress in the same
// transaction. sopbridge's only remaining job here is the verification-item fan-out.
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
	if b.verification == nil {
		return nil
	}
	completions, err := b.recorder.SubmissionCompletions(ctx, tenantID, submission.SubmissionID)
	if err != nil {
		return err
	}
	return b.emitVerificationItems(ctx, tenantID, task, submission, completions)
}

func (b *VaccinationSubmissionBridge) emitVerificationItems(
	ctx context.Context,
	tenantID string,
	task sopdomain.TaskSummary,
	submission sopdomain.SubmissionSummary,
	completions []vaccinationdomain.SubmissionCompletion,
) error {
	proofsByGoat := make(map[string][]string)
	for _, ref := range submission.ProofRefs {
		if ref.ProofID == "" || ref.SubjectType != "goat" || ref.SubjectID == nil || *ref.SubjectID == "" {
			continue
		}
		proofsByGoat[*ref.SubjectID] = append(proofsByGoat[*ref.SubjectID], ref.ProofID)
	}
	byGoat := make(map[string]vaccinationdomain.SubmissionCompletion)
	for _, completion := range completions {
		if completion.GoatID == "" {
			continue
		}
		if len(completion.ProofRefIDs) > 0 {
			proofsByGoat[completion.GoatID] = append(proofsByGoat[completion.GoatID], completion.ProofRefIDs...)
		}
		if existing, ok := byGoat[completion.GoatID]; !ok || completion.AdministeredAt.Before(existing.AdministeredAt) {
			byGoat[completion.GoatID] = completion
		}
	}
	if len(byGoat) == 0 {
		return ErrNoVaccinationCompletions
	}
	taskID := task.TaskID
	submissionID := submission.SubmissionID
	var operatorID *string
	if submission.SubmittedBy != "" {
		by := submission.SubmittedBy
		operatorID = &by
	}
	for goatID, completion := range byGoat {
		mediaRefs := proofsByGoat[goatID]
		mediaRefs = uniqueStrings(mediaRefs)
		if len(mediaRefs) == 0 {
			return fmt.Errorf("%w: goat_id=%s", ErrMissingGoatProof, goatID)
		}
		shedID := stringPtr(completion.ShedID)
		parkID := stringPtr(completion.ParkID)
		_, err := b.verification.CreateItem(ctx, verificationdomain.CreateItem{
			TenantID:     tenantID,
			Vertical:     "preventive_care",
			Module:       "vaccination",
			Category:     VaccinationVerificationCategory,
			SubjectLabel: stringPtr(completion.GoatLabel),
			Source: verificationdomain.SourceRef{
				Module:       "vaccination",
				TaskID:       &taskID,
				SubmissionID: &submissionID,
				RefType:      "vaccination_goat",
				RefID:        goatID,
			},
			MediaRefs:      mediaRefs,
			OperatorID:     operatorID,
			ShedID:         shedID,
			ParkID:         parkID,
			CapturedAt:     completion.AdministeredAt,
			IdempotencyKey: "vaccination:submission:" + submissionID + ":goat:" + goatID,
		})
		if err != nil {
			return fmt.Errorf("create goat verification item %s: %w", goatID, err)
		}
	}
	return nil
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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
