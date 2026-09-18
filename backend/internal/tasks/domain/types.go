package domain

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// Sentinel errors shared by the app service, the postgres adapter, and the in-memory fakes so the
// idempotency/state-machine contract is one implementation exercised everywhere.
var (
	// ErrNotFound is a missing workflow/action in the tenant scope.
	ErrNotFound = errors.New("tasks: not found")
	// ErrIdempotencyConflict is a same-key/different-payload replay (HTTP 409).
	ErrIdempotencyConflict = errors.New("tasks: idempotency key reused with a different payload")
	// ErrActionAlreadyCompleted is a NEW key writing to a terminally-completed action (HTTP 409).
	ErrActionAlreadyCompleted = errors.New("tasks: action is already completed")
	// ErrActionInReview is a write to an action whose verification is pending (HTTP 409).
	ErrActionInReview = errors.New("tasks: action is awaiting verification")
	// ErrActionOutOfSequence is a write to a later operator step before every earlier step in the
	// same workflow section has completed (HTTP 409).
	ErrActionOutOfSequence = errors.New("tasks: complete the previous action first")
	// ErrActionNotYetDue rejects a dependency-timed action before its canonical due_at.
	ErrActionNotYetDue = errors.New("tasks: action is not due yet")
	// ErrProofRequired is a requires_video completion without a proof_ref (HTTP 422 proof_required).
	ErrProofRequired = errors.New("tasks: this step needs its required proof (video and/or photo) before it can be completed")
	// ErrPermanentIdentifierRequired prevents Tag the kid from finishing before the canonical goat
	// has received its operator-scanned permanent RFID.
	ErrPermanentIdentifierRequired = errors.New("tasks: assign the permanent RFID before completing Tag the kid")
	// ErrActionNotAnswerable is answer on a non-question action (HTTP 400).
	ErrActionNotAnswerable = errors.New("tasks: action is not a question and cannot be answered")
	// ErrActionNotCompletable is complete on a question/approval action (HTTP 400).
	ErrActionNotCompletable = errors.New("tasks: action cannot be completed directly")
	// ErrInvalidAnswer is an empty, malformed, or out-of-contract answer (HTTP 400).
	ErrInvalidAnswer = errors.New("tasks: answer_value is invalid for this action")
	// ErrMissingRequiredField covers blank command fields (HTTP 400).
	ErrMissingRequiredField = errors.New("tasks: missing required field")
	// ErrUnknownTemplate is an OpenWorkflow with a template key no code-defined template matches.
	ErrUnknownTemplate = errors.New("tasks: unknown template key")
	// ErrVerificationEnqueuerNotWired surfaces a composition bug: a death-video completion that must
	// enqueue verification but has no enqueue seam. The completion itself is durable; a retry heals.
	ErrVerificationEnqueuerNotWired = errors.New("tasks: death verification enqueuer is not wired")
)

// WorkflowInstance mirrors one workflow_instances row (the card).
type WorkflowInstance struct {
	WorkflowID           string
	TenantID             string
	TemplateKey          string
	Module               string
	SubjectGoatID        string
	DamGoatID            *string
	EventAt              time.Time
	EventDate            string // YYYY-MM-DD, Asia/Kolkata
	ParkID               *string
	ShedID               *string
	State                string
	ActionsTotal         int
	ActionsDone          int
	NextActionKey        *string
	NextActionTitle      *string
	NextDueAt            *time.Time
	AwaitingVerification bool
	RowVersion           int
	// CaptureEvidence is the capture form's snapshot (Add death today), taken when the report was
	// submitted and stamped at open; it leads the verifier bundle. Empty for every workflow
	// opened without one (birth tracks, reconcile, pre-feature rows).
	CaptureEvidence authored.Evidence
	// BirthEventID is the litter a birth track belongs to (workflow_instances.birth_event_id).
	BirthEventID *string
}

// WorkflowAction mirrors one workflow_actions row.
type WorkflowAction struct {
	ActionID      string
	TenantID      string
	WorkflowID    string
	ActionKey     string
	Seq           int
	Section       string
	ActionType    string
	Title         string
	Detail        string
	RequiresVideo bool
	Options       []string
	DueAt         *time.Time
	Status        string
	AnswerValue   *string
	ProofRef      *string
	// SOP-driven attributes stamped at open from the pinned follow_up (sop_followup.go). Rows
	// stamped before migration 000308 were backfilled so the generalized gates below read the
	// same thing the old key-matched code implied.
	TaskType           string
	AnswerType         string
	EngineHook         string
	ProofMinVideos     int
	ProofMinPhotos     int
	ProofRefs          []ProofItem
	HardTimeGate       bool
	WaitForAll         bool
	RequiresKeys       []string
	AfterActionKey     string
	AfterOffsetSeconds int
	// AnswerGate is the branch condition this step runs under (nil = always). Until the question
	// it names is answered the step is blocked; once answered it is either on the path (pending)
	// or skipped.
	AnswerGate *AnswerCondition
	// ReworkReason is the verifier's words when this step was sent back; cleared on completion.
	ReworkReason       *string
	CompletedBy        *string
	CompletedAt        *time.Time
	VerificationItemID *string
	IdempotencyKey     *string
	RequestFingerprint *string
	RowVersion         int
}

// ProofItem is one captured proof on a step: the media reference and whether it is a video or a
// photo. proof_ref (the legacy single column) mirrors the first video.
type ProofItem struct {
	Ref  string `json:"ref"`
	Kind string `json:"kind"`
}

// Proof kinds.
const (
	ProofKindVideo = "video"
	ProofKindPhoto = "photo"
)

// HasHook reports whether the step carries an engine hook. Pre-000304 rows and rows whose
// task type carries no hook fall back to the legacy step-key match, so a behaviour never
// detaches on old data.
func (a WorkflowAction) HasHook(hook string) bool {
	if a.EngineHook != "" {
		return a.EngineHook == hook
	}
	switch hook {
	case EngineHookWeighKg:
		return a.ActionKey == ActionKeyTakeWeight
	case EngineHookTagKid:
		return a.ActionKey == ActionKeyTagTheKid
	case EngineHookRecordPen:
		return a.ActionKey == ActionKeyRecordShed
	case EngineHookColostrum:
		return a.ActionKey == ActionKeyFirstColostrum || a.Section == SectionColostrumSession
	case EngineHookDeathVideo:
		return a.ActionKey == ActionKeyDeathVideo || a.ActionKey == ActionKeyPostMortemVideo
	}
	return false
}

// proofCounts tallies captured proofs by kind, counting the legacy proof_ref as one video when
// proof_refs is empty.
func (a WorkflowAction) proofCounts() (videos, photos int) {
	if len(a.ProofRefs) == 0 {
		if a.ProofRef != nil && strings.TrimSpace(*a.ProofRef) != "" {
			return 1, 0
		}
		return 0, 0
	}
	for _, p := range a.ProofRefs {
		switch p.Kind {
		case ProofKindPhoto:
			photos++
		default:
			videos++
		}
	}
	return videos, photos
}

// ProofSatisfied reports whether the step's captured proofs meet its authored minimums. A
// pre-000304 row has ProofMinVideos backfilled from requires_video, so the legacy one-video rule
// is the same check.
func (a WorkflowAction) ProofSatisfied() bool {
	v, p := a.proofCounts()
	minV := a.ProofMinVideos
	if minV == 0 && a.RequiresVideo && len(a.ProofRefs) == 0 {
		minV = 1
	}
	return v >= minV && p >= a.ProofMinPhotos
}

// mergeProofs applies a write's proofs to the step: an explicit proofs list replaces the step's
// captured set; a bare proof_ref (legacy client) becomes one video. The legacy proof_ref column
// mirrors the first video so older readers keep working.
func mergeProofs(a *WorkflowAction, proofRef string, proofs []ProofItem) {
	items := make([]ProofItem, 0, len(proofs)+1)
	for _, p := range proofs {
		ref := strings.TrimSpace(p.Ref)
		if ref == "" {
			continue
		}
		kind := p.Kind
		if kind != ProofKindPhoto {
			kind = ProofKindVideo
		}
		items = append(items, ProofItem{Ref: ref, Kind: kind})
	}
	if ref := strings.TrimSpace(proofRef); ref != "" {
		dup := false
		for _, it := range items {
			if it.Ref == ref {
				dup = true
				break
			}
		}
		if !dup {
			items = append([]ProofItem{{Ref: ref, Kind: ProofKindVideo}}, items...)
		}
	}
	a.ProofRefs = items
	a.ProofRef = nil
	for _, it := range items {
		if it.Kind == ProofKindVideo {
			r := it.Ref
			a.ProofRef = &r
			break
		}
	}
	if a.ProofRef == nil && len(items) > 0 {
		r := items[0].Ref
		a.ProofRef = &r
	}
}

// AnswerActionCommand answers a question / question_select action. take_weight is a numeric
// kilograms question; its answer value stores the decimal number without a unit suffix.
type AnswerActionCommand struct {
	TenantID           string
	WorkflowID         string
	ActionID           string
	AnswerValue        string
	ProofRef           string
	Proofs             []ProofItem
	AnsweredBy         string
	AnsweredAt         time.Time
	IdempotencyKey     string
	RequestFingerprint string
}

// CompleteActionCommand completes an "action"-type step (optionally with a video proof).
type CompleteActionCommand struct {
	TenantID           string
	WorkflowID         string
	ActionID           string
	ProofRef           string
	Proofs             []ProofItem
	CompletedBy        string
	CompletedAt        time.Time
	IdempotencyKey     string
	RequestFingerprint string
}

// ActionWriteResult reports an answer/complete outcome.
type ActionWriteResult struct {
	Workflow WorkflowInstance
	Action   WorkflowAction
	Replayed bool
	// NeedsVerificationEnqueue is true when the death workflow is awaiting verification
	// after this write (both death videos in). It is computed from STATE, not from "did this call
	// flip it", so an exact replay after a failed enqueue re-reports it and the retry heals.
	NeedsVerificationEnqueue bool
	// DeathProofRefs carries every proof of the death bundle in bundle order (capture form first,
	// then steps in seq order -- for the seeded document: death video, post mortem video) when
	// NeedsVerificationEnqueue is set. DeathEvidence carries the same refs with their titles,
	// kinds and the answer rows.
	DeathProofRefs []string
	DeathEvidence  DeathEvidence
	// DeathReviewRound is the workflow row_version — a monotonic counter of review rounds. It goes
	// into the verification idempotency
	// key so that a re-shoot after a rejection ALWAYS opens a new review item, even in the pathological
	// case where the operator re-submits byte-identical proof refs. A replay does not mutate the
	// sign-off, so the round is stable and genuine retries still de-duplicate.
	DeathReviewRound int
	// NeedsStepReviewEnqueue is true when the written step now sits in_review on a per-step-reviewed
	// workflow (birth): the caller enqueues ONE verification item for exactly this recording
	// (Action carries the proofs; BirthStepReviewKey(Action) is the idempotency key). Derived from
	// STATE, so an exact replay after a failed enqueue re-reports it and the retry heals.
	NeedsStepReviewEnqueue bool
}

// WriteResult derives the enqueue flags every adapter reports after an action write, from state
// alone: the postgres adapter and the fakes both call it so a fake can never report an enqueue the
// real path would not.
func WriteResult(w WorkflowInstance, actions []WorkflowAction, target WorkflowAction, replay bool) ActionWriteResult {
	result := ActionWriteResult{Workflow: w, Action: target, Replayed: replay}
	if w.TemplateKey == TemplateKeyDeath && w.AwaitingVerification && DeathStepsComplete(actions) {
		result.NeedsVerificationEnqueue = true
		result.DeathEvidence = DeathEvidenceBundle(w.CaptureEvidence, actions)
		result.DeathProofRefs = result.DeathEvidence.Refs
		result.DeathReviewRound = w.RowVersion
	}
	if ReviewedPerStep(w.TemplateKey) && target.Status == ActionStatusInReview {
		result.NeedsStepReviewEnqueue = true
	}
	return result
}

// evaluateIdempotentWrite applies the mandatory request-level idempotency contract to one action
// write. terminal marks an action that must not take a NEW write (already completed / in review).
//
//	first call            -> apply (replay=false)
//	exact replay          -> return original result, no side effects (replay=true)
//	same key, new payload -> ErrIdempotencyConflict
//	new key, terminal row -> ErrActionAlreadyCompleted / ErrActionInReview
func evaluateIdempotentWrite(a WorkflowAction, key, fingerprint string) (replay bool, err error) {
	if a.IdempotencyKey != nil && *a.IdempotencyKey == key {
		if a.RequestFingerprint != nil && *a.RequestFingerprint == fingerprint {
			return true, nil
		}
		return false, ErrIdempotencyConflict
	}
	switch a.Status {
	case ActionStatusCompleted:
		return false, ErrActionAlreadyCompleted
	case ActionStatusInReview:
		return false, ErrActionInReview
	}
	return false, nil
}

// ApplyAnswer runs the answer state machine on one action. It returns the updated action, whether
// the call was an exact idempotent replay (no mutation), or the contract error. Pure: both the
// postgres adapter and the test fakes call this so the rules cannot drift.
func ApplyAnswer(a WorkflowAction, cmd AnswerActionCommand) (WorkflowAction, bool, error) {
	if a.ActionType != ActionTypeQuestion && a.ActionType != ActionTypeQuestionSelect {
		return a, false, ErrActionNotAnswerable
	}
	replay, err := evaluateIdempotentWrite(a, cmd.IdempotencyKey, cmd.RequestFingerprint)
	if err != nil {
		return a, false, err
	}
	if replay {
		return a, true, nil
	}
	answer := strings.TrimSpace(cmd.AnswerValue)
	if answer == "" {
		return a, false, ErrInvalidAnswer
	}
	if a.HasHook(EngineHookWeighKg) && !ValidKidWeightKilograms(answer) {
		return a, false, ErrInvalidAnswer
	}
	if a.AnswerType == AnswerKindNumber && !a.HasHook(EngineHookWeighKg) {
		if _, err := strconv.ParseFloat(answer, 64); err != nil {
			return a, false, ErrInvalidAnswer
		}
	}
	// The Record shed answer names an operational location. Only its FORMAT is checked here, in the
	// pure state machine; whether the pen actually exists is proved against live location rows in
	// the same transaction as the write (see the postgres adapter). A malformed value is rejected
	// before the action is marked completed, so a kid is never recorded as placed by a value that
	// resolves to no pen.
	if a.HasHook(EngineHookRecordPen) {
		if _, _, err := ParseRecordedPenAnswer(answer); err != nil {
			return a, false, err
		}
	}
	mergeProofs(&a, cmd.ProofRef, cmd.Proofs)
	if !a.ProofSatisfied() {
		return a, false, ErrProofRequired
	}
	if a.ActionType == ActionTypeQuestionSelect {
		// A multiselect answer is the chosen options joined by "|", each of which must be authored.
		chosen := []string{answer}
		if a.AnswerType == AnswerKindMultiSelect {
			chosen = strings.Split(answer, "|")
		}
		for _, c := range chosen {
			allowed := false
			for _, opt := range a.Options {
				if opt == strings.TrimSpace(c) {
					allowed = true
					break
				}
			}
			if !allowed {
				return a, false, ErrInvalidAnswer
			}
		}
	}
	by := cmd.AnsweredBy
	at := cmd.AnsweredAt
	key := cmd.IdempotencyKey
	fp := cmd.RequestFingerprint
	a.Status = ActionStatusCompleted
	a.ReworkReason = nil
	a.AnswerValue = &answer
	a.CompletedBy = optionalPtr(by)
	a.CompletedAt = &at
	a.IdempotencyKey = &key
	a.RequestFingerprint = &fp
	a.RowVersion++
	return a, false, nil
}

// ValidKidWeightKilograms accepts a positive finite decimal number without a unit suffix. The
// medical source gives no narrower min/max range, so the backend rejects malformed/non-positive
// input without inventing a clinical threshold.
func ValidKidWeightKilograms(value string) bool {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return false
	}
	for _, part := range parts {
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	weight, err := strconv.ParseFloat(value, 64)
	return err == nil && weight > 0 && !math.IsInf(weight, 0) && !math.IsNaN(weight)
}

// ApplyComplete runs the complete state machine on one action. Approval actions are never operator-
// completable (they flip through the verification verdict consumer); question actions must use
// answer.
func ApplyComplete(a WorkflowAction, cmd CompleteActionCommand) (WorkflowAction, bool, error) {
	switch a.ActionType {
	case ActionTypeAction:
	case ActionTypeApproval:
		return a, false, ErrActionNotCompletable
	default:
		return a, false, ErrActionNotCompletable
	}
	replay, err := evaluateIdempotentWrite(a, cmd.IdempotencyKey, cmd.RequestFingerprint)
	if err != nil {
		return a, false, err
	}
	if replay {
		return a, true, nil
	}
	mergeProofs(&a, cmd.ProofRef, cmd.Proofs)
	if !a.ProofSatisfied() {
		return a, false, ErrProofRequired
	}
	// A capture re-shoot of an `either` proof carries no kind minimum but is still a capture.
	if a.HasHook(EngineHookReshootReport) && len(a.AllProofRefs()) == 0 {
		return a, false, ErrProofRequired
	}
	at := cmd.CompletedAt
	key := cmd.IdempotencyKey
	fp := cmd.RequestFingerprint
	a.Status = ActionStatusCompleted
	a.ReworkReason = nil
	a.AnswerValue = nil
	a.CompletedBy = optionalPtr(cmd.CompletedBy)
	a.CompletedAt = &at
	a.IdempotencyKey = &key
	a.RequestFingerprint = &fp
	a.RowVersion++
	return a, false, nil
}

// TemplateLabel is the operator-facing name of a workflow's kind, rendered verbatim by clients.
func TemplateLabel(templateKey string) string {
	switch templateKey {
	case TemplateKeyBirthKid, TemplateKeyBirthMother:
		return "Birth"
	case TemplateKeyDeath:
		return "Death"
	case TemplateKeyReconcile:
		return "Pen return"
	case TemplateKeyShifting:
		return "Pen move"
	}
	if _, general := GeneralSOPCode(templateKey); general {
		return "Work instruction"
	}
	return ""
}

// DeathVideosComplete reports whether every death_evidence-hooked step is completed. It is the
// PRE-SOP gate, kept for the golden/legacy tests; the approval and release gates use
// DeathStepsComplete (evidence.go), which requires EVERY authored operator step.
func DeathVideosComplete(actions []WorkflowAction) bool {
	total, done := 0, 0
	for _, a := range actions {
		if !a.HasHook(EngineHookDeathVideo) || a.Status == ActionStatusCanceled {
			continue
		}
		total++
		if a.Status == ActionStatusCompleted {
			done++
		}
	}
	return total > 0 && done == total
}

// DeathProofRefs returns every death-evidence proof in step order (the legacy pair was death
// video then post-mortem video; the SOP may author more).
func DeathProofRefs(actions []WorkflowAction) []string {
	sorted := append([]WorkflowAction(nil), actions...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	refs := make([]string, 0, 2)
	for _, a := range sorted {
		if !a.HasHook(EngineHookDeathVideo) {
			continue
		}
		refs = append(refs, a.AllProofRefs()...)
	}
	return refs
}

// AllProofRefs lists every captured proof reference on the step, videos first.
func (a WorkflowAction) AllProofRefs() []string {
	if len(a.ProofRefs) == 0 {
		if a.ProofRef != nil && *a.ProofRef != "" {
			return []string{*a.ProofRef}
		}
		return nil
	}
	out := make([]string, 0, len(a.ProofRefs))
	for _, p := range a.ProofRefs {
		if p.Kind == ProofKindVideo {
			out = append(out, p.Ref)
		}
	}
	for _, p := range a.ProofRefs {
		if p.Kind != ProofKindVideo {
			out = append(out, p.Ref)
		}
	}
	return out
}

// BirthWorkflowComplete requires every operator row in one mother or child workflow. Every
// video-required row must carry evidence before that subject opens its own review gate.
func BirthWorkflowComplete(actions []WorkflowAction) bool {
	foundOperator := false
	for _, action := range actions {
		if action.ActionType == ActionTypeApproval || action.Status == ActionStatusCanceled {
			continue
		}
		foundOperator = true
		if action.Status != ActionStatusCompleted {
			return false
		}
		if !action.ProofSatisfied() {
			return false
		}
	}
	return foundOperator
}

func BirthProofRefs(actions []WorkflowAction) []string {
	refs := make([]string, 0, len(actions))
	for _, action := range actions {
		refs = append(refs, action.AllProofRefs()...)
	}
	return refs
}

func BirthProofOperator(actions []WorkflowAction) string {
	for _, action := range actions {
		if action.RequiresVideo && action.CompletedBy != nil {
			return *action.CompletedBy
		}
	}
	return ""
}

// SignoffBlocked reports whether the approval step reads as "blocked": it is pending while the
// operator steps it signs off are not all in.
func SignoffBlocked(a WorkflowAction, siblings []WorkflowAction) bool {
	return a.ActionType == ActionTypeApproval && a.Status == ActionStatusPending && !DeathStepsComplete(siblings)
}

// OperatorActionBlocked enforces operator-visible dependencies. Normal actions follow their own
// section; scheduled colostrum starts only after the immediate 1st Colostrum and then proceeds one
// round at a time; Tag the kid waits for every other operator task, even on legacy workflow rows
// where its stored seq predates the scheduled colostrum rows. Internal approval rows never
// participate in the operator sequence.
//
// Terminal rows return false so exact idempotency replays continue to reach ApplyAnswer/
// ApplyComplete and return their original result.
//
// templateKey decides what "the previous step is done" means. A per-step-reviewed workflow (birth)
// counts a step as done for sequencing the moment it is RECORDED -- in_review and rework included --
// because the verifier's verdict on clip 3 must never hold clip 4, and a rejected clip 3 is re-shot
// on its own while the operator carries on. A bundle-reviewed workflow (death) keeps the strict
// rule: a bounced pair re-shoots in order.
func OperatorActionBlocked(templateKey string, a WorkflowAction, siblings []WorkflowAction) bool {
	if a.ActionType == ActionTypeApproval ||
		a.Status == ActionStatusCompleted ||
		a.Status == ActionStatusInReview ||
		a.Status == ActionStatusCanceled {
		return false
	}
	// A skipped step is off the path: nothing to do, so nothing is "blocked".
	if a.Status == ActionStatusSkipped {
		return true
	}
	// An answer-gated step waits for its question; the branch is resolved when that answer lands.
	if a.AnswerGate != nil {
		answered := false
		for _, q := range siblings {
			if q.ActionKey == a.AnswerGate.Step {
				answered = q.Status == ActionStatusCompleted || q.Status == ActionStatusInReview || q.Status == ActionStatusRework
				break
			}
		}
		if !answered {
			return true
		}
	}
	// A capture re-shoot answers a verdict on the REPORT, not a step of the track: it is appended
	// after every step, and the lane rule held it behind the whole track (next-day ORS water on a
	// mother) so a rejected report proof could not be re-recorded (E2E 2026-09-17).
	if a.HasHook(EngineHookReshootReport) {
		return false
	}
	done := func(prerequisite WorkflowAction) bool {
		// A skipped step is not in the way: the path simply does not pass through it.
		return prerequisite.Status == ActionStatusSkipped || StepRecorded(templateKey, prerequisite)
	}
	if a.WaitForAll || (a.TaskType == "" && a.ActionKey == ActionKeyTagTheKid) {
		for _, prerequisite := range siblings {
			if prerequisite.ActionID == a.ActionID || prerequisite.ActionType == ActionTypeApproval {
				continue
			}
			if !done(prerequisite) {
				return true
			}
		}
		return false
	}
	requires := a.RequiresKeys
	if a.TaskType == "" && a.Section == SectionColostrumSession {
		requires = []string{ActionKeyFirstColostrum}
	}
	for _, req := range requires {
		satisfied := false
		for _, prerequisite := range siblings {
			if prerequisite.ActionKey == req {
				satisfied = done(prerequisite)
				break
			}
		}
		if !satisfied {
			return true
		}
	}
	for _, previous := range siblings {
		if previous.ActionType == ActionTypeApproval || previous.Section != a.Section || previous.Seq >= a.Seq {
			continue
		}
		if !done(previous) {
			return true
		}
	}
	return false
}

// ReviewedPerStep reports whether a template's evidence is verified one recorded step at a time
// (maintainer decision 2026-09-16: birth). Everything else keeps its bundle: death's two clips
// are one verifier item released by the admin's approval, and the SOP subject workflows report
// once to their owning module.
func ReviewedPerStep(templateKey string) bool {
	return templateKey == TemplateKeyBirthKid || templateKey == TemplateKeyBirthMother
}

// StepRecorded is "the operator has done this step at least once" for sequencing purposes. On a
// per-step-reviewed workflow that includes a step awaiting its verdict and a step sent back for a
// re-shoot; a bundle-reviewed workflow only accepts completed.
func StepRecorded(templateKey string, a WorkflowAction) bool {
	switch a.Status {
	case ActionStatusCompleted:
		return true
	case ActionStatusInReview, ActionStatusRework:
		return ReviewedPerStep(templateKey)
	}
	return false
}

// AnswerGateUnresolved reports a gated step whose question has not been answered yet.
func AnswerGateUnresolved(a WorkflowAction, siblings []WorkflowAction) bool {
	if a.AnswerGate == nil || a.Status != ActionStatusPending {
		return false
	}
	for _, q := range siblings {
		if q.ActionKey == a.AnswerGate.Step {
			return !(q.Status == ActionStatusCompleted || q.Status == ActionStatusInReview || q.Status == ActionStatusRework)
		}
	}
	return true
}

// BranchNote is the farm-worded sentence for a gated step: which question, which answer. The
// question is named by its title so the sentence reads the way the SOP author wrote it.
func BranchNote(a WorkflowAction, siblings []WorkflowAction) string {
	if a.AnswerGate == nil {
		return ""
	}
	question := a.AnswerGate.Step
	for _, q := range siblings {
		if q.ActionKey == a.AnswerGate.Step && strings.TrimSpace(q.Title) != "" {
			question = q.Title
			break
		}
	}
	return "Only if \u201c" + question + "\u201d " + a.AnswerGate.Phrase()
}

// Phrase renders the comparison in words: "is No", "is not Bloat", "is one of A, B", "is more than 30".
func (c AnswerCondition) Phrase() string {
	values := strings.Join(c.Value, ", ")
	switch c.Op {
	case AnswerOpEq:
		if len(c.Value) == 1 {
			return "is " + values
		}
		return "is one of " + values
	case AnswerOpNe:
		if len(c.Value) == 1 {
			return "is not " + values
		}
		return "is none of " + values
	case AnswerOpIn:
		return "is one of " + values
	case AnswerOpNotIn:
		return "is none of " + values
	case AnswerOpGt:
		return "is more than " + values
	case AnswerOpGte:
		return "is " + values + " or more"
	case AnswerOpLt:
		return "is less than " + values
	case AnswerOpLte:
		return "is " + values + " or less"
	}
	return "is " + values
}

// ResolveAnswerBranches settles every branch that hangs on the step just answered: a sibling
// whose gate names it is SKIPPED when the answer does not satisfy the gate, and a step gated on a
// skipped question is skipped with it (the whole branch goes). Steps whose gate is satisfied stay
// pending and become reachable through OperatorActionBlocked. Returns the rows that changed.
// Pure; the repository persists the result inside the answer's transaction.
func ResolveAnswerBranches(answered WorkflowAction, siblings []WorkflowAction) []WorkflowAction {
	answers := map[string]string{}
	skipped := map[string]bool{}
	for _, s := range siblings {
		if s.Status == ActionStatusSkipped {
			skipped[s.ActionKey] = true
		}
		if s.AnswerValue != nil && (s.Status == ActionStatusCompleted || s.Status == ActionStatusInReview || s.Status == ActionStatusRework) {
			answers[s.ActionKey] = *s.AnswerValue
		}
	}
	if answered.AnswerValue != nil {
		answers[answered.ActionKey] = *answered.AnswerValue
	}
	var changed []WorkflowAction
	// Steps are in seq order, so a skip cascades forward within one pass.
	for i := range siblings {
		s := siblings[i]
		if s.AnswerGate == nil || s.Status != ActionStatusPending || s.ActionID == answered.ActionID {
			continue
		}
		gateKey := s.AnswerGate.Step
		answer, has := answers[gateKey]
		switch {
		case skipped[gateKey], has && !s.AnswerGate.Satisfied(answer):
			s.Status = ActionStatusSkipped
			s.RowVersion++
			skipped[s.ActionKey] = true
			changed = append(changed, s)
		}
	}
	return changed
}

// HoldStepForReview is the per-step review gate, applied right after ApplyAnswer/ApplyComplete in
// the SAME transaction: on a per-step-reviewed workflow a step that just completed WITH proof moves
// to in_review, where it stays locked until the verifier's verdict lands (approve -> completed,
// reject -> rework). A step with nothing to watch (Record pen) completes outright, and a
// bundle-reviewed workflow is returned untouched. Pure, so the fakes and the adapter agree.
func HoldStepForReview(templateKey string, a WorkflowAction) WorkflowAction {
	if !ReviewedPerStep(templateKey) || a.Status != ActionStatusCompleted || len(a.AllProofRefs()) == 0 {
		return a
	}
	// A capture re-shoot is reviewed as the REPORT's item (birth_capture), not as a birth step.
	if a.HasHook(EngineHookReshootReport) {
		return a
	}
	a.Status = ActionStatusInReview
	return a
}

// BirthStepReviewKey is the verification idempotency key for ONE recording of ONE birth step:
// the action, its row_version (a monotonic counter bumped by every rework and re-submission, so a
// re-shoot after a rejection always opens a fresh item even with byte-identical proofs), and the
// proofs under review (so the key describes exactly what the verifier is watching). A retried
// request mutates nothing, so the key is stable and de-duplicates. Same shape as the death key.
func BirthStepReviewKey(a WorkflowAction) string {
	key := "counts-birth-step:" + a.ActionID + ":r" + strconv.Itoa(a.RowVersion)
	for _, ref := range a.AllProofRefs() {
		key += ":" + strings.TrimSpace(ref)
	}
	return key
}

// ApplyStepVerdict applies one verifier verdict to one step of a per-step-reviewed workflow.
// approved: in_review -> completed. rejected: in_review -> rework, proofs cleared so the re-shoot is
// mandatory, the verifier's reason kept for the operator, Tag the kid keeps its assigned RFID. A
// verdict for a step that is not awaiting one is a benign redelivery and changes nothing (the
// caller reports changed=false). The immutable recording key must match as well: a delayed
// verdict for a rejected recording must never consume a later re-shoot, even with the same proofs.
// Missing keys fail closed. Pure: the postgres adapter and the fakes share it.
func ApplyStepVerdict(a WorkflowAction, recordingKey string, approved bool, reason string) (WorkflowAction, bool) {
	if a.Status != ActionStatusInReview || recordingKey == "" || recordingKey != BirthStepReviewKey(a) {
		return a, false
	}
	if approved {
		a.Status = ActionStatusCompleted
		a.ReworkReason = nil
		a.RowVersion++
		return a, true
	}
	a.Status = ActionStatusRework
	if !a.HasHook(EngineHookTagKid) {
		a.AnswerValue = nil
	}
	a.ProofRef = nil
	a.ProofRefs = nil
	a.CompletedBy = nil
	a.CompletedAt = nil
	a.VerificationItemID = nil
	a.IdempotencyKey = nil
	a.RequestFingerprint = nil
	a.ReworkReason = optionalPtr(strings.TrimSpace(reason))
	a.RowVersion++
	return a, true
}

// ActionTimeBlocked enforces hard not-before gates from canonical due_at for ORS round 2 and every
// scheduled colostrum round. Once due, an action remains available until completion. Other template
// due times are scheduling guidance unless their own contract explicitly makes them a hard gate. A
// missing due time on either gated shape fails closed.
func ActionTimeBlocked(a WorkflowAction, now time.Time) bool {
	gated := a.HardTimeGate
	if a.TaskType == "" {
		gated = a.ActionKey == ActionKeyORSWater2 || a.Section == SectionColostrumSession
	}
	if !gated ||
		a.Status == ActionStatusCompleted ||
		a.Status == ActionStatusInReview ||
		a.Status == ActionStatusCanceled {
		return false
	}
	return a.DueAt == nil || now.Before(*a.DueAt)
}

// RecomputeCard recalculates the write-maintained card fields from the workflow's own actions. It
// is called in the SAME transaction as every action write (compute-on-write), by the postgres
// adapter and by the test fakes, so the card can never drift from the steps.
//
// projection-review: producer grain = `workflow_actions` unique (workflow_id, action_key);
// consumer card grain = `workflow_instances` unique (tenant_id, template_key, subject_goat_id) =
// 1 row per card. actions_done / actions_total / next_* use the identical key set: every visible
// action_type!=approval operator action across main and scheduled-colostrum sections (join key
// workflow_id, 1:N pre-aggregated on write in the same txn); death workflow completion separately
// uses all section=main rows, including any internal approval. Chip counts group workflow_instances
// rows by derived bucket at (tenant_id, module,
// event_date) grain — numerator and denominator both range over the same workflow_instances key set;
// no join fan-out.
//
// Per-step-reviewed workflows (birth) read the review gate from their own steps: a step awaiting
// its verdict is the operator's work DONE (it counts, and it is never the next step), and the card
// reads awaiting_verification only once nothing is left to record and at least one verdict is
// outstanding. Bundle-reviewed workflows keep the explicit gate the verdict consumers open/close.
func RecomputeCard(w WorkflowInstance, actions []WorkflowAction) WorkflowInstance {
	operatorTotal, operatorDone := 0, 0
	allMainTotal, allMainDone := 0, 0
	perStep := ReviewedPerStep(w.TemplateKey)
	// awaiting_verification is workflow state. Preserve an explicitly opened review gate while also
	// deriving it from any legacy/internal in_review row during rolling migration.
	awaiting := w.AwaitingVerification && !perStep
	anyInReview := false
	var next *WorkflowAction
	for i := range actions {
		a := actions[i]
		if a.Status == ActionStatusSkipped {
			// Off the taken path: not owed, not counted, never next.
			continue
		}
		if a.Status == ActionStatusInReview {
			anyInReview = true
			if !perStep {
				awaiting = true
			}
		}
		if a.Section == SectionMain {
			allMainTotal++
			if a.Status == ActionStatusCompleted {
				allMainDone++
			}
		}
		if a.ActionType == ActionTypeApproval {
			continue
		}
		operatorTotal++
		switch a.Status {
		case ActionStatusCompleted:
			operatorDone++
		case ActionStatusInReview:
			// The operator recorded it; only the verifier's verdict is outstanding.
			operatorDone++
		case ActionStatusCanceled:
			// canceled steps neither count as done nor become the next action
		default:
			if next == nil || a.Seq < next.Seq {
				next = &actions[i]
			}
		}
	}
	if perStep {
		awaiting = anyInReview && next == nil
	}
	w.ActionsTotal = operatorTotal
	w.ActionsDone = operatorDone
	w.AwaitingVerification = awaiting
	if next != nil {
		key := next.ActionKey
		title := next.Title
		w.NextActionKey = &key
		w.NextActionTitle = &title
		w.NextDueAt = next.DueAt
	} else {
		w.NextActionKey = nil
		w.NextActionTitle = nil
		w.NextDueAt = nil
	}
	// State follows the counters in BOTH directions. A one-directional open->completed recompute would
	// leave a workflow sitting in the "completed" chip with outstanding work after a rework bounced a
	// finished death record back for a re-shoot. Canceled is terminal and is never re-derived here.
	workflowComplete := allMainTotal > 0 && allMainDone == allMainTotal
	if w.TemplateKey == TemplateKeyBirthKid {
		workflowComplete = BirthWorkflowComplete(actions)
	}
	switch {
	case w.State == WorkflowStateOpen && workflowComplete:
		w.State = WorkflowStateCompleted
	case w.State == WorkflowStateCompleted && !workflowComplete:
		w.State = WorkflowStateOpen
	}
	return w
}

func optionalPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
