package sopbridge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

var ErrNoVaccinationCompletions = errors.New("vaccination fanout materialized no completions")
var ErrMissingGoatProof = errors.New("vaccination submission is missing a completed proof for a scanned goat")

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
	mediaRefs := make([]string, 0)
	goatProofs := make(map[string]struct{})
	byGoat := make(map[string]vaccinationdomain.SubmissionCompletion)
	var earliest time.Time
	var shedID *string
	var parkID *string
	shedLabels := make(map[string]string)
	for _, completion := range completions {
		if completion.GoatID == "" {
			continue
		}
		if len(completion.ProofRefIDs) > 0 {
			mediaRefs = append(mediaRefs, completion.ProofRefIDs...)
			goatProofs[completion.GoatID] = struct{}{}
		}
		if existing, ok := byGoat[completion.GoatID]; !ok || completion.AdministeredAt.Before(existing.AdministeredAt) {
			byGoat[completion.GoatID] = completion
		}
		if earliest.IsZero() || completion.AdministeredAt.Before(earliest) {
			earliest = completion.AdministeredAt
		}
		if shedID == nil && completion.ShedID != "" {
			shedID = stringPtr(completion.ShedID)
		}
		if completion.ShedID != "" {
			label := strings.TrimSpace(completion.ShedLabel)
			if label == "" || strings.HasPrefix(label, "-") {
				label = completion.ShedID
			}
			shedLabels[completion.ShedID] = label
		}
		if parkID == nil && completion.ParkID != "" {
			parkID = stringPtr(completion.ParkID)
		}
	}
	if len(byGoat) == 0 {
		return ErrNoVaccinationCompletions
	}
	// P0 shed leak: the Android client posts a CUMULATIVE payload -- by the Nth shed submission of a
	// shared parent task, submission.ProofRefs re-carries every earlier shed's goat proofs. The
	// server already narrows sop_submission_items to goats whose goats.shed_id matches the
	// submission's shed subject (sop/adapters/postgres.filterSubmissionItemsToProofSheds); the same
	// membership rule MUST be applied here, or verification_items.media_refs shows a verifier three
	// other sheds' animals as the evidence for this shed. `completions` IS the server-filtered
	// membership, so scope client refs to it: goat-subject refs must name a goat in this
	// submission's completions, shed-subject refs must name a shed those completions belong to.
	// Refs without a resolvable subject stay (task-level group proof).
	hasGroupProof := false
	for _, ref := range submission.ProofRefs {
		if ref.ProofID == "" {
			continue
		}
		subjectID := ""
		if ref.SubjectID != nil {
			subjectID = strings.TrimSpace(*ref.SubjectID)
		}
		isGoatRef := strings.EqualFold(ref.SubjectType, "goat")
		if isGoatRef {
			// A goat proof with no subject cannot be attributed to this shed -- drop it rather than
			// let an unattributable clip stand in as another shed's evidence.
			if subjectID == "" {
				continue
			}
			if _, ok := byGoat[subjectID]; !ok {
				continue
			}
			mediaRefs = append(mediaRefs, ref.ProofID)
			goatProofs[subjectID] = struct{}{}
			continue
		}
		if strings.EqualFold(ref.SubjectType, "shed") && subjectID != "" && len(shedLabels) > 0 {
			if _, ok := shedLabels[subjectID]; !ok {
				continue
			}
		}
		mediaRefs = append(mediaRefs, ref.ProofID)
		hasGroupProof = true
	}
	mediaRefs = uniqueStrings(mediaRefs)
	if len(mediaRefs) == 0 {
		return fmt.Errorf("%w: submission_id=%s", ErrMissingGoatProof, submission.SubmissionID)
	}
	if !hasGroupProof {
		for goatID := range byGoat {
			if _, ok := goatProofs[goatID]; !ok {
				return fmt.Errorf("%w: submission_id=%s goat_id=%s", ErrMissingGoatProof, submission.SubmissionID, goatID)
			}
		}
	}
	taskID := task.TaskID
	submissionID := submission.SubmissionID
	var operatorID *string
	if submission.SubmittedBy != "" {
		by := submission.SubmittedBy
		operatorID = &by
	}
	subjectLabel := vaccinationSubjectLabel(len(byGoat), shedLabels)
	_, err := b.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     tenantID,
		Vertical:     "preventive_care",
		Module:       "vaccination",
		Category:     VaccinationVerificationCategory,
		SubjectLabel: &subjectLabel,
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
		ParkID:         parkID,
		CapturedAt:     earliest,
		IdempotencyKey: "vaccination:submission:" + submissionID,
	})
	if err != nil {
		return fmt.Errorf("create submission verification item %s: %w", submissionID, err)
	}
	return nil
}

func vaccinationSubjectLabel(goatCount int, shedLabels map[string]string) string {
	animalSummary := fmt.Sprintf("%d goats", goatCount)
	if len(shedLabels) == 0 {
		return animalSummary
	}
	if len(shedLabels) == 1 {
		for _, label := range shedLabels {
			label = strings.TrimSpace(label)
			if label == "" {
				return animalSummary
			}
			return label + " · " + animalSummary
		}
	}
	labels := make([]string, 0, len(shedLabels))
	for _, label := range shedLabels {
		if label = strings.TrimSpace(label); label != "" {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)
	if len(labels) == 2 {
		return strings.Join(labels, " + ") + " · " + animalSummary
	}
	return fmt.Sprintf("%d sheds · %s", len(shedLabels), animalSummary)
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
