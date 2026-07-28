package domain

import (
	"errors"
	"time"
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
	// ErrProofRequired is a requires_video completion without a proof_ref (HTTP 422 proof_required).
	ErrProofRequired = errors.New("tasks: a video proof (proof_ref) is required to complete this action")
	// ErrActionNotAnswerable is answer on a non-question action (HTTP 400).
	ErrActionNotAnswerable = errors.New("tasks: action is not a question and cannot be answered")
	// ErrActionNotCompletable is complete on a question/approval action (HTTP 400).
	ErrActionNotCompletable = errors.New("tasks: action cannot be completed directly")
	// ErrInvalidAnswer is a question_select answer outside the declared options (HTTP 400).
	ErrInvalidAnswer = errors.New("tasks: answer_value is not one of the allowed options")
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
}

// WorkflowAction mirrors one workflow_actions row.
type WorkflowAction struct {
	ActionID           string
	TenantID           string
	WorkflowID         string
	ActionKey          string
	Seq                int
	Section            string
	ActionType         string
	Title              string
	Detail             string
	RequiresVideo      bool
	Options            []string
	DueAt              *time.Time
	Status             string
	AnswerValue        *string
	ProofRef           *string
	CompletedBy        *string
	CompletedAt        *time.Time
	VerificationItemID *string
	IdempotencyKey     *string
	RequestFingerprint *string
	RowVersion         int
}

// AnswerActionCommand answers a question / question_select action.
type AnswerActionCommand struct {
	TenantID           string
	WorkflowID         string
	ActionID           string
	AnswerValue        string
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
	// DeathProofRefs carries both death-video proofs, ordered (death video, post mortem video), when
	// NeedsVerificationEnqueue is set.
	DeathProofRefs []string
	// DeathReviewRound is the workflow row_version — a monotonic counter of review rounds. It goes
	// into the verification idempotency
	// key so that a re-shoot after a rejection ALWAYS opens a new review item, even in the pathological
	// case where the operator re-submits byte-identical proof refs. A replay does not mutate the
	// sign-off, so the round is stable and genuine retries still de-duplicate.
	DeathReviewRound int
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
	if cmd.AnswerValue == "" {
		return a, false, ErrInvalidAnswer
	}
	if a.ActionType == ActionTypeQuestionSelect {
		allowed := false
		for _, opt := range a.Options {
			if opt == cmd.AnswerValue {
				allowed = true
				break
			}
		}
		if !allowed {
			return a, false, ErrInvalidAnswer
		}
	}
	answer := cmd.AnswerValue
	by := cmd.AnsweredBy
	at := cmd.AnsweredAt
	key := cmd.IdempotencyKey
	fp := cmd.RequestFingerprint
	a.Status = ActionStatusCompleted
	a.AnswerValue = &answer
	a.CompletedBy = optionalPtr(by)
	a.CompletedAt = &at
	a.IdempotencyKey = &key
	a.RequestFingerprint = &fp
	a.RowVersion++
	return a, false, nil
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
	if a.RequiresVideo && cmd.ProofRef == "" {
		return a, false, ErrProofRequired
	}
	at := cmd.CompletedAt
	key := cmd.IdempotencyKey
	fp := cmd.RequestFingerprint
	a.Status = ActionStatusCompleted
	a.ProofRef = optionalPtr(cmd.ProofRef)
	a.AnswerValue = nil
	a.CompletedBy = optionalPtr(cmd.CompletedBy)
	a.CompletedAt = &at
	a.IdempotencyKey = &key
	a.RequestFingerprint = &fp
	a.RowVersion++
	return a, false, nil
}

// DeathVideosComplete reports whether both mandatory death videos are completed.
func DeathVideosComplete(actions []WorkflowAction) bool {
	done := 0
	for _, a := range actions {
		if (a.ActionKey == ActionKeyDeathVideo || a.ActionKey == ActionKeyPostMortemVideo) &&
			a.Status == ActionStatusCompleted {
			done++
		}
	}
	return done == 2
}

// DeathProofRefs returns the two death-video proofs ordered (death video, post mortem video).
func DeathProofRefs(actions []WorkflowAction) []string {
	var death, postMortem string
	for _, a := range actions {
		if a.ProofRef == nil {
			continue
		}
		switch a.ActionKey {
		case ActionKeyDeathVideo:
			death = *a.ProofRef
		case ActionKeyPostMortemVideo:
			postMortem = *a.ProofRef
		}
	}
	refs := make([]string, 0, 2)
	if death != "" {
		refs = append(refs, death)
	}
	if postMortem != "" {
		refs = append(refs, postMortem)
	}
	return refs
}

// SignoffBlocked reports whether the approval step reads as "blocked": it is pending while the
// videos it signs off are not both in.
func SignoffBlocked(a WorkflowAction, siblings []WorkflowAction) bool {
	return a.ActionType == ActionTypeApproval && a.Status == ActionStatusPending && !DeathVideosComplete(siblings)
}

// OperatorActionBlocked enforces the operator-visible sequence within one workflow section. A
// separate section is a separate operational lane: birth's time-based colostrum-session strip, for
// example, must not wait behind the main track's day+2 RFID action. Internal approval rows never
// participate in the operator sequence.
//
// Terminal rows return false so exact idempotency replays continue to reach ApplyAnswer/
// ApplyComplete and return their original result.
func OperatorActionBlocked(a WorkflowAction, siblings []WorkflowAction) bool {
	if a.ActionType == ActionTypeApproval ||
		a.Status == ActionStatusCompleted ||
		a.Status == ActionStatusInReview ||
		a.Status == ActionStatusCanceled {
		return false
	}
	for _, previous := range siblings {
		if previous.ActionType == ActionTypeApproval || previous.Section != a.Section || previous.Seq >= a.Seq {
			continue
		}
		if previous.Status != ActionStatusCompleted {
			return true
		}
	}
	return false
}

// RecomputeCard recalculates the write-maintained card fields from the workflow's own actions. It
// is called in the SAME transaction as every action write (compute-on-write), by the postgres
// adapter and by the test fakes, so the card can never drift from the steps.
//
// projection-review: producer grain = `workflow_actions` unique (workflow_id, action_key);
// consumer card grain = `workflow_instances` unique (tenant_id, template_key, subject_goat_id) =
// 1 row per card. actions_done / actions_total / next_* use the identical key set: that workflow's
// section=main, action_type!=approval operator actions (join key workflow_id, 1:N pre-aggregated on
// write in the same txn); workflow completion uses all section=main rows, including the internal
// approval. Chip counts group workflow_instances rows by derived bucket at (tenant_id, module,
// event_date) grain — numerator and denominator both range over the same workflow_instances key set;
// no join fan-out.
func RecomputeCard(w WorkflowInstance, actions []WorkflowAction) WorkflowInstance {
	operatorTotal, operatorDone := 0, 0
	allMainTotal, allMainDone := 0, 0
	// awaiting_verification is workflow state. Preserve an explicitly opened review gate while also
	// deriving it from any legacy/internal in_review row during rolling migration.
	awaiting := w.AwaitingVerification
	var next *WorkflowAction
	for i := range actions {
		a := actions[i]
		if a.Status == ActionStatusInReview {
			awaiting = true
		}
		if a.Section != SectionMain {
			continue
		}
		allMainTotal++
		if a.Status == ActionStatusCompleted {
			allMainDone++
		}
		if a.ActionType == ActionTypeApproval {
			continue
		}
		operatorTotal++
		switch a.Status {
		case ActionStatusCompleted:
			operatorDone++
		case ActionStatusCanceled:
			// canceled steps neither count as done nor become the next action
		default:
			if next == nil || a.Seq < next.Seq {
				next = &actions[i]
			}
		}
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
	switch {
	case w.State == WorkflowStateOpen && allMainTotal > 0 && allMainDone == allMainTotal:
		w.State = WorkflowStateCompleted
	case w.State == WorkflowStateCompleted && allMainDone < allMainTotal:
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
