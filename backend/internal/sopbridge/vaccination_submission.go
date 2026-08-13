package sopbridge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
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

// ObligationCompleter is the slice of the obligation module this bridge needs to close an
// obligation the moment its completion is recorded (maintainer state-model: the obligation is the
// operator's work list and closes on RECORD, not on verification -- tying it to verification left a
// vaccinated animal reading "due" until someone reviewed the video, so the operator could scan and
// dose it a second time). Idempotent: MarkCompleted no-ops on an obligation already closed.
type ObligationCompleter interface {
	MarkCompleted(ctx context.Context, tenantID, obligationID string) (bool, error)
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
	obligation   ObligationCompleter
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

// WithObligationCompleter wires the obligation-close-on-record seam. Production composition always
// supplies it; recorder-only instances (fan-out unit tests that don't care about obligation state)
// are unaffected -- a nil obligation completer is a no-op below.
func (b *VaccinationSubmissionBridge) WithObligationCompleter(o ObligationCompleter) *VaccinationSubmissionBridge {
	b.obligation = o
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
	completions, err := b.recorder.SubmissionCompletions(ctx, tenantID, submission.SubmissionID)
	if err != nil {
		return err
	}
	if err := b.closeObligationsForCompletions(ctx, tenantID, completions); err != nil {
		return err
	}
	if b.verification == nil {
		return nil
	}
	return b.emitVerificationItems(ctx, tenantID, task, submission, completions)
}

// closeObligationsForCompletions closes the obligation axis the moment its completion is recorded
// (see ObligationCompleter's doc comment for the "why"). Runs for every completion materialized by
// this submission, not just the ones recorded THIS call: on a replayed/partial submission that is
// safe and cheap, because MarkCompleted no-ops on an obligation that is already closed. A failure
// here is returned (not swallowed) so the caller's outbox/verification fan-out never proceeds on top
// of a work list that silently failed to close.
func (b *VaccinationSubmissionBridge) closeObligationsForCompletions(
	ctx context.Context,
	tenantID string,
	completions []vaccinationdomain.SubmissionCompletion,
) error {
	if b.obligation == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(completions))
	for _, completion := range completions {
		if completion.ObligationID == "" {
			continue
		}
		if _, ok := seen[completion.ObligationID]; ok {
			continue
		}
		seen[completion.ObligationID] = struct{}{}
		if _, err := b.obligation.MarkCompleted(ctx, tenantID, completion.ObligationID); err != nil {
			return fmt.Errorf("close obligation %s on record: %w", completion.ObligationID, err)
		}
	}
	return nil
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
	// Proof is captured PER ANIMAL, so the verdict has to land per animal too: one verification
	// item per goat, carrying only that goat's clips. goatMedia keeps them separated all the way
	// to CreateItem. The flat mediaRefs slice is still built alongside it because the group-proof
	// and missing-proof gates below reason over the submission as a whole.
	goatMedia := make(map[string][]string)
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
			goatMedia[completion.GoatID] = append(goatMedia[completion.GoatID], completion.ProofRefIDs...)
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
			// An unresolvable shed is OMITTED, never substituted with its id. This used to fall
			// back to `label = completion.ShedID`, which put a raw UUID straight into the
			// verifier's subject line the moment a shed name failed to resolve -- exactly what
			// the locked spec forbids ("Never render a UUID as a label"), and doubly so now that
			// the label LEADS with the shed. A leading "-" means only the partition suffix
			// survived (" - Part 3"), which is an id-shaped fragment for the same reason.
			// Callers treat a missing entry as "no shed to show" and drop the segment.
			label := strings.TrimSpace(completion.ShedLabel)
			if label != "" && !strings.HasPrefix(label, "-") {
				shedLabels[completion.ShedID] = label
			}
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
			// Same membership rule as the flat list: this ref survived the shed-leak scoping
			// above, so it belongs to THIS goat and to no other item.
			goatMedia[subjectID] = append(goatMedia[subjectID], ref.ProofID)
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
	// PER-ANIMAL FAN-OUT. Proof is captured one clip per goat, so the verdict is issued one goat
	// at a time: a verifier who sees a single bad clip rejects THAT animal, and the rest of the
	// shed keeps its accepted work instead of being re-scanned and re-filmed.
	//
	// This emits the ref type the applier has always understood -- vaccination/app
	// verification_handler.go routes ref_type "vaccination_goat" to ApplyGoatVerification, which
	// accepts or rejects that goat's own completion and obligation. Nothing downstream is new:
	// verdict_reason, verified_by, row_version, the approve evidence gate, verifier != operator
	// and idempotency were already per-ROW, so they become per-animal for free. No new domain
	// event type is introduced (a type absent from the envelope enum would be written, fail
	// validation, and never be delivered).
	//
	// source_submission_id stays set on every item: it is the grouping key the verifier queue
	// uses to show one shed card holding N animals.
	goatIDs := make([]string, 0, len(byGoat))
	for goatID := range byGoat {
		goatIDs = append(goatIDs, goatID)
	}
	sort.Strings(goatIDs) // deterministic emission order; map iteration is not stable

	for _, goatID := range goatIDs {
		completion := byGoat[goatID]
		animalMedia := uniqueStrings(goatMedia[goatID])
		if len(animalMedia) == 0 {
			// Only reachable when a task-level group clip is standing in for per-animal proof
			// (the !hasGroupProof gate above already rejects a submission with a goat that has
			// neither). That animal's evidence is the group clip, which is emitted as its own
			// shed-grain item below, so there is nothing to judge per-animal here.
			continue
		}
		animalLabel := vaccinationAnimalSubjectLabel(completion, shedLabels)
		partitionLabel := completion.PartitionLabel
		if partitionLabel == "" || partitionLabel == "whole" {
			partitionLabel = "" // never display 'whole' sentinel
		}
		if _, err := b.verification.CreateItem(ctx, verificationdomain.CreateItem{
			TenantID:     tenantID,
			Vertical:     "preventive_care",
			Module:       "vaccination",
			Category:     VaccinationVerificationCategory,
			SubjectLabel: &animalLabel,
			Source: verificationdomain.SourceRef{
				Module:       "vaccination",
				TaskID:       &taskID,
				SubmissionID: &submissionID,
				RefType:      "vaccination_goat",
				RefID:        goatID,
			},
			MediaRefs:      animalMedia,
			OperatorID:     operatorID,
			ShedID:         stringPtr(completion.ShedID), // per-animal shed, not submission-wide
			PartitionLabel: stringPtr(partitionLabel),
			ParkID:         parkID,
			// This animal's own capture time, not the submission-wide earliest: the queue sorts
			// on it, and a shared timestamp would collapse the ordering of a shed's animals.
			CapturedAt:     completion.AdministeredAt,
			IdempotencyKey: "vaccination:submission:" + submissionID + ":goat:" + goatID,
		}); err != nil {
			return fmt.Errorf("create goat verification item %s/%s: %w", submissionID, goatID, err)
		}
	}

	if hasGroupProof {
		// A task-level clip covers the shed rather than any one animal, so it keeps the
		// shed-grain shape and the applier keeps routing it through ApplySubmissionVerification.
		groupMedia := make([]string, 0, len(mediaRefs))
		claimed := make(map[string]struct{}, len(mediaRefs))
		for _, refs := range goatMedia {
			for _, ref := range refs {
				claimed[ref] = struct{}{}
			}
		}
		for _, ref := range mediaRefs {
			if _, ok := claimed[ref]; !ok {
				groupMedia = append(groupMedia, ref)
			}
		}
		if len(groupMedia) > 0 {
			// Agree-or-go-bare: shed-grain proof is valid only if all animals in the submission
			// are in the SAME partition. Multiple partitions → ambiguous at shed grain → NULL.
			partitionLabel := ""
			if len(byGoat) > 0 {
				// Get the first completion's partition as a reference
				firstPartition := ""
				for _, completion := range byGoat {
					if completion.PartitionLabel != "" && completion.PartitionLabel != "whole" {
						firstPartition = completion.PartitionLabel
					}
					break
				}
				// Verify all completions have the same partition
				allSame := true
				for _, completion := range byGoat {
					currPartition := ""
					if completion.PartitionLabel != "" && completion.PartitionLabel != "whole" {
						currPartition = completion.PartitionLabel
					}
					if currPartition != firstPartition {
						allSame = false
						break
					}
				}
				if allSame && firstPartition != "" {
					partitionLabel = firstPartition
				}
			}
			subjectLabel := vaccinationSubjectLabel(len(byGoat), shedLabels, vaccinationVaccineSummary(byGoat))
			if _, err := b.verification.CreateItem(ctx, verificationdomain.CreateItem{
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
				MediaRefs:      groupMedia,
				OperatorID:     operatorID,
				ShedID:         shedID,
				PartitionLabel: stringPtr(partitionLabel),
				ParkID:         parkID,
				CapturedAt:     earliest,
				IdempotencyKey: "vaccination:submission:" + submissionID + ":group",
			}); err != nil {
				return fmt.Errorf("create submission verification item %s: %w", submissionID, err)
			}
		}
	}
	return nil
}

// vaccinationAnimalSubjectLabel names ONE animal for the verifier's card. The verifier is
// deciding a single goat's clip, so the label leads with the animal and carries its shed for
// context -- "Gandhi 1 - G-006004". Farm words only; no ids the operator would not recognise.
func vaccinationAnimalSubjectLabel(completion vaccinationdomain.SubmissionCompletion, shedLabels map[string]string) string {
	animal := strings.TrimSpace(completion.GoatLabel)
	if animal == "" {
		animal = "1 goat"
	}
	parts := make([]string, 0, 3)
	shed := strings.TrimSpace(shedLabels[completion.ShedID])
	// A leading "-" means the shed name itself did not resolve and only the partition suffix
	// survived (" - Part 3"), which is an id-shaped fragment, not a name -- drop it rather than
	// show the verifier a dangling separator.
	if shed != "" && !strings.HasPrefix(shed, "-") {
		parts = append(parts, shed)
	}
	parts = append(parts, animal)
	// The vaccine is the whole point of the review: the verifier is deciding whether the clip
	// shows THIS dose being given. Without it she is judging a video of an animal against nothing.
	if vaccine := strings.TrimSpace(completion.VaccineLabel); vaccine != "" {
		parts = append(parts, vaccine)
	}
	return strings.Join(parts, " · ")
}

// vaccinationVaccineSummary names the vaccine(s) a shed-grain submission covers. A drive is a park
// visit that can mix vaccines within one shed, so this does not assume a single dose: it reports
// every distinct human label, and collapses to a count past two so the row stays a row.
func vaccinationVaccineSummary(completions map[string]vaccinationdomain.SubmissionCompletion) string {
	seen := make(map[string]struct{}, 4)
	labels := make([]string, 0, 4)
	for _, completion := range completions {
		label := strings.TrimSpace(completion.VaccineLabel)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	sort.Strings(labels) // map iteration is not stable; the label must not shuffle between reads
	switch len(labels) {
	case 0:
		return ""
	case 1, 2:
		return strings.Join(labels, " + ")
	default:
		return strconv.Itoa(len(labels)) + " vaccines"
	}
}

// vaccinationSubjectLabel composes the shed-grain sentence: WHERE · WHAT · HOW MANY, e.g.
// "Sumathi 1 - Part 3 · ET+TT · 12 goats". The shed carries its partition because that is the pen
// the animals stand in; the vaccine is what the verifier is actually judging the clip against.
// Any part that does not resolve is omitted rather than substituted with an id or a placeholder.
func vaccinationSubjectLabel(goatCount int, shedLabels map[string]string, vaccineSummary string) string {
	parts := make([]string, 0, 3)
	if shed := vaccinationShedSummary(shedLabels); shed != "" {
		parts = append(parts, shed)
	}
	if vaccine := strings.TrimSpace(vaccineSummary); vaccine != "" {
		parts = append(parts, vaccine)
	}
	parts = append(parts, fmt.Sprintf("%d goats", goatCount))
	return strings.Join(parts, " · ")
}

// vaccinationShedSummary names the shed(s) a submission covers, collapsing past two so the row
// stays a row. Returns "" when nothing resolved, so the caller omits the segment entirely.
func vaccinationShedSummary(shedLabels map[string]string) string {
	labels := make([]string, 0, len(shedLabels))
	for _, label := range shedLabels {
		if label = strings.TrimSpace(label); label != "" {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)
	switch len(labels) {
	case 0:
		return ""
	case 1, 2:
		return strings.Join(labels, " + ")
	default:
		return fmt.Sprintf("%d sheds", len(labels))
	}
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
