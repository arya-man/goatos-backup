// Package domain holds the toxin test flow: the 7-step guided procedure, its
// server-enforced wait gates, the round state machine, and the backend-owned farm copy.
//
// TOXIN MODULE (maintainer decisions 2026-08-25). Every feed load recorded on
// /procurement/feed-purchases is screened for aflatoxins with the SafetiX SHF 001-A
// rapid strip kit. The farm's procedure does NOT centrifuge — after shaking, the extract
// SITS for about an hour. The kit is QUALITATIVE: the developed strip reads Negative,
// Positive, or Invalid (no control line -> void strip).
//
// Proof at every step: steps 1/2/3/5/6 each require one in-app-camera VIDEO uploaded as
// the step completes; step 4 is the settling wait; step 7 is the reading plus one final
// in-app-camera PHOTO of the strip. All three waits are HARD-BLOCKED on the server
// clock. Steps are person-independent — any toxin.execute holder may complete the next
// open step, and each completion records who did it.
//
// An Invalid strip or a CEO/CXO reject CANCELS the whole round and mints a fresh retest
// task for the same load; evidence of the cancelled round stays as permanent history.
// Review (accept/reject) is CEO/CXO-only. This module is an approval gate in the
// counts_approver shape, deliberately NOT a generic Verification category, so the
// 2026-08-03 verifier verdict-exclusivity lock stays intact. Canonical prose:
// docs/decisions/toxin-testing-module.md.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Task statuses.
const (
	StatusInProgress    = "in_progress"
	StatusPendingReview = "pending_review"
	StatusAccepted      = "accepted"
	StatusCancelled     = "cancelled"
)

// Strip outcomes, exactly the SHF 001-A readings.
const (
	OutcomeNegative = "negative"
	OutcomePositive = "positive"
	OutcomeInvalid  = "invalid"
)

// Task origins: why this round exists.
const (
	OriginPurchase       = "purchase"
	OriginInvalidRetest  = "invalid_retest"
	OriginRejectedRetest = "rejected_retest"
)

// Verdict decisions.
const (
	VerdictAccept = "accept"
	VerdictReject = "reject"
)

// Step kinds.
const (
	StepKindVideo        = "video"
	StepKindWait         = "wait"
	StepKindPhotoReading = "photo_reading"
)

// FinalStepNo is the reading step; completing it is the submit.
const FinalStepNo = 7

var (
	// ErrInvalidOutcome rejects an outcome outside the kit's three readings.
	ErrInvalidOutcome = errors.New("toxin: outcome must be negative, positive or invalid")
	// ErrProofRequired rejects a step completion with no capture attached.
	ErrProofRequired = errors.New("toxin: proof capture is required")
	// ErrTaskNotOpen rejects step work on a task that is not in progress.
	ErrTaskNotOpen = errors.New("toxin: task is not open for testing")
	// ErrUnknownStep rejects a step number outside the procedure.
	ErrUnknownStep = errors.New("toxin: unknown step")
	// ErrStepNotCompletable rejects completing the wait row or the final step through
	// the plain step route (step 7 goes through submit, with the reading).
	ErrStepNotCompletable = errors.New("toxin: step is not completable")
	// ErrStepOutOfOrder rejects a step whose predecessors are not all done.
	ErrStepOutOfOrder = errors.New("toxin: earlier steps are not finished")
	// ErrProofAlreadyUsed rejects a capture already recorded against another step. One capture
	// proves ONE step: each working step is filmed separately, and the proof validator cannot
	// tell which step a clip shows, so reuse is refused at the write.
	ErrProofAlreadyUsed = errors.New("toxin: that capture is already recorded against another step")
	// ErrStepAlreadyDone rejects re-completing a finished step.
	ErrStepAlreadyDone = errors.New("toxin: step already completed")
	// ErrWaitNotElapsed rejects a step attempted before its wait gate opened.
	ErrWaitNotElapsed = errors.New("toxin: the waiting time has not passed yet")
	// ErrNotReviewable rejects a verdict on a task that is not awaiting review.
	ErrNotReviewable = errors.New("toxin: task is not awaiting review")
	// ErrInvalidVerdict rejects a verdict outside accept/reject.
	ErrInvalidVerdict = errors.New("toxin: verdict must be accept or reject")
	// ErrRejectReasonRequired rejects a reject with no reason — the testers are owed the
	// why before re-running the whole test.
	ErrRejectReasonRequired = errors.New("toxin: a reject must carry a reason")
)

// WaitNotElapsed carries when a hard-blocked step's gate opens, so transports can tell
// the tester how long is left. errors.Is(err, ErrWaitNotElapsed) matches it.
type WaitNotElapsed struct{ OpensAt time.Time }

func (w WaitNotElapsed) Error() string        { return ErrWaitNotElapsed.Error() }
func (w WaitNotElapsed) Is(target error) bool { return target == ErrWaitNotElapsed }

// StepSpec declares one procedure step. The spec is BACKEND-OWNED: clients render the
// titles/instructions verbatim and never invent step logic of their own.
type StepSpec struct {
	No          int
	Kind        string
	Title       string
	Instruction string
	// GateAfterStep/GateMinutes hard-block this step until GateMinutes have passed since
	// GateAfterStep's completion, on the SERVER clock. Zero means no gate.
	GateAfterStep int
	GateMinutes   int
	// WaitMinutes renders the wait row's duration (kind wait only).
	WaitMinutes int
}

// Steps is the canonical 7-step procedure.
func Steps() []StepSpec {
	return []StepSpec{
		{No: 1, Kind: StepKindVideo, Title: "Take the sample",
			Instruction: "Take the feed sample out of this load on camera."},
		{No: 2, Kind: StepKindVideo, Title: "Grind and weigh",
			Instruction: "Grind the sample and weigh out 5 g on camera."},
		{No: 3, Kind: StepKindVideo, Title: "Mix and shake",
			Instruction: "Add the extraction solution and shake for 3 minutes on camera."},
		{No: 4, Kind: StepKindWait, Title: "Let it sit",
			Instruction: "Leave the mixture to settle for 1 hour. The next step unlocks when the hour has passed.",
			WaitMinutes: 60},
		{No: 5, Kind: StepKindVideo, Title: "Dilute and fill the well",
			Instruction:   "Draw the clear liquid, dilute it as the kit table directs, and fill the microwell on camera.",
			GateAfterStep: 3, GateMinutes: 60},
		{No: 6, Kind: StepKindVideo, Title: "Place the strip",
			Instruction:   "After 3 minutes in the well, place the test strip and let it develop for 8 minutes — on camera.",
			GateAfterStep: 5, GateMinutes: 3},
		{No: 7, Kind: StepKindPhotoReading, Title: "Read the strip",
			Instruction:   "Remove the pad, read the strip within 1 minute, photograph it immediately and record the reading.",
			GateAfterStep: 6, GateMinutes: 8},
	}
}

// StepSpecFor returns the spec for a step number.
func StepSpecFor(no int) (StepSpec, bool) {
	for _, s := range Steps() {
		if s.No == no {
			return s, true
		}
	}
	return StepSpec{}, false
}

// WorkingSteps are the steps that record a completion (everything but the wait row).
func WorkingSteps() []int { return []int{1, 2, 3, 5, 6, 7} }

// Task is one testing round for one purchased feed load.
type Task struct {
	TaskID             string
	TenantID           string
	FeedPurchaseID     string
	RoundNo            int
	RetestOfTaskID     string
	Origin             string
	FarmLabel          string
	FeedItemKey        string
	FeedItemLabel      string
	Vendor             string
	BatchNo            int
	PurchaseDate       string // business DATE, YYYY-MM-DD
	QuantityKg         float64
	Status             string
	Outcome            string // "" until step 7
	StripPhotoRef      string
	SubmittedBy        string
	SubmittedAt        string
	ReviewedBy         string
	ReviewedAt         string
	ReviewReason       string
	CancelReason       string
	SupersededByTaskID string
	RowVersion         int64
	CreatedAt          string
}

// StepCompletion is one immutable completed working step.
type StepCompletion struct {
	StepNo   int
	ProofRef string
	// CompletedBy is the WORKFORCE NAME of whoever recorded this step, resolved server-side.
	// It is deliberately never the user id: an id on an operator's screen is not an answer to
	// "who did this", and rendering one is the copy-firewall defect the counts approval queue
	// already shipped once. An unresolvable person leaves this BLANK so the screen simply omits
	// the attribution rather than printing a uuid.
	CompletedBy string
	CompletedAt time.Time
}

// completionFor finds a step's completion.
func completionFor(completions []StepCompletion, stepNo int) (StepCompletion, bool) {
	for _, c := range completions {
		if c.StepNo == stepNo {
			return c, true
		}
	}
	return StepCompletion{}, false
}

// GateOpensAt returns when a step's wait gate opens given the completions so far. The
// zero time means the step has no gate (or its predecessor is not done yet — order is
// checked separately).
func GateOpensAt(spec StepSpec, completions []StepCompletion) time.Time {
	if spec.GateAfterStep == 0 || spec.GateMinutes == 0 {
		return time.Time{}
	}
	prev, ok := completionFor(completions, spec.GateAfterStep)
	if !ok {
		return time.Time{}
	}
	return prev.CompletedAt.Add(time.Duration(spec.GateMinutes) * time.Minute)
}

// CheckStepCompletable decides whether stepNo may be completed NOW. now is the server
// clock. Returns the gate-open instant alongside ErrWaitNotElapsed so transports can
// tell the tester when the step unlocks.
func CheckStepCompletable(taskStatus string, stepNo int, completions []StepCompletion, now time.Time) (time.Time, error) {
	if taskStatus != StatusInProgress {
		return time.Time{}, ErrTaskNotOpen
	}
	spec, ok := StepSpecFor(stepNo)
	if !ok {
		return time.Time{}, ErrUnknownStep
	}
	if spec.Kind == StepKindWait {
		return time.Time{}, ErrStepNotCompletable
	}
	if _, done := completionFor(completions, stepNo); done {
		return time.Time{}, ErrStepAlreadyDone
	}
	// Every earlier WORKING step must be done first.
	for _, no := range WorkingSteps() {
		if no >= stepNo {
			break
		}
		if _, done := completionFor(completions, no); !done {
			return time.Time{}, ErrStepOutOfOrder
		}
	}
	if opensAt := GateOpensAt(spec, completions); !opensAt.IsZero() && now.Before(opensAt) {
		return opensAt, ErrWaitNotElapsed
	}
	return time.Time{}, nil
}

// ValidateOutcome asserts a step-7 reading is one of the kit's three.
func ValidateOutcome(outcome string) error {
	switch outcome {
	case OutcomeNegative, OutcomePositive, OutcomeInvalid:
		return nil
	default:
		return ErrInvalidOutcome
	}
}

// SubmitResult is what completing step 7 does to the round.
type SubmitResult struct {
	// NextStatus is pending_review (Negative/Positive) or cancelled (Invalid strip).
	NextStatus string
	// CreatesRetest is true when the round cancels and a fresh round must be minted in
	// the same transaction.
	CreatesRetest bool
	// RetestOrigin labels the minted round.
	RetestOrigin string
	// CancelReason is the backend-owned farm copy stored on the cancelled round.
	CancelReason string
}

// SubmitDecision applies the step-7 state machine. Order/gate checks are
// CheckStepCompletable's job; this decides what the reading does.
func SubmitDecision(outcome string) (SubmitResult, error) {
	if err := ValidateOutcome(outcome); err != nil {
		return SubmitResult{}, err
	}
	if outcome == OutcomeInvalid {
		return SubmitResult{
			NextStatus:    StatusCancelled,
			CreatesRetest: true,
			RetestOrigin:  OriginInvalidRetest,
			CancelReason:  "The strip was invalid — no control line. A retest was created.",
		}, nil
	}
	return SubmitResult{NextStatus: StatusPendingReview}, nil
}

// VerdictResult is what a CEO/CXO decision does to the round.
type VerdictResult struct {
	NextStatus    string
	CreatesRetest bool
	RetestOrigin  string
	CancelReason  string
}

// VerdictDecision applies the review state machine: accept closes the round; reject
// cancels it (mandatory reason) and mints a fresh retest round.
func VerdictDecision(currentStatus, decision, reason string) (VerdictResult, error) {
	if currentStatus != StatusPendingReview {
		return VerdictResult{}, ErrNotReviewable
	}
	switch decision {
	case VerdictAccept:
		return VerdictResult{NextStatus: StatusAccepted}, nil
	case VerdictReject:
		if strings.TrimSpace(reason) == "" {
			return VerdictResult{}, ErrRejectReasonRequired
		}
		return VerdictResult{
			NextStatus:    StatusCancelled,
			CreatesRetest: true,
			RetestOrigin:  OriginRejectedRetest,
			CancelReason:  "Review sent this test back: " + strings.TrimSpace(reason),
		}, nil
	default:
		return VerdictResult{}, ErrInvalidVerdict
	}
}

// OutcomeLabel is the farm-worded reading shown on every surface. Raw tokens never
// reach a screen.
func OutcomeLabel(outcome string) string {
	switch outcome {
	case OutcomeNegative:
		return "Negative"
	case OutcomePositive:
		return "Positive"
	case OutcomeInvalid:
		return "Invalid strip"
	default:
		return ""
	}
}

// OriginLine explains why a round exists, for the card subtitle. Round 1 needs none.
func OriginLine(origin string) string {
	switch origin {
	case OriginInvalidRetest:
		return "Retest — the last strip was invalid"
	case OriginRejectedRetest:
		return "Retest — review sent the last test back"
	default:
		return ""
	}
}

// StatusChip is the backend-owned chip for a task row.
//
// OVERDUE OUTRANKS PROGRESS. A round past its 12-hour start deadline reads "Overdue" even when a
// step is mid-wait, because the operator needs to see that this load has been sitting longer than
// the farm allows before they see which step is next. The countdown chip it replaces is still
// reachable — the step list on the detail screen carries it — so nothing is lost.
func StatusChip(t Task, completions []StepCompletion, now time.Time) string {
	switch t.Status {
	case StatusInProgress:
		if createdAt, err := time.Parse(time.RFC3339Nano, t.CreatedAt); err == nil && IsOverdue(t, createdAt, now) {
			if hours := int(OverdueBy(createdAt, now) / time.Hour); hours >= 1 {
				return fmt.Sprintf("Overdue by %s", hourPhrase(hours))
			}
			return "Overdue"
		}
		next := NextStepNo(completions)
		if next == 0 {
			return "Test due"
		}
		spec, _ := StepSpecFor(next)
		if opensAt := GateOpensAt(spec, completions); !opensAt.IsZero() && now.Before(opensAt) {
			remaining := opensAt.Sub(now).Round(time.Minute)
			minutes := int(remaining / time.Minute)
			if minutes < 1 {
				minutes = 1
			}
			return fmt.Sprintf("Waiting — next step in %d min", minutes)
		}
		done := len(completions)
		return fmt.Sprintf("Step %d of %d", done+1, len(WorkingSteps()))
	case StatusPendingReview:
		return "Waiting for review"
	case StatusAccepted:
		// "Completed", not "Reviewed" (maintainer decision 2026-08-26). The operator's work and
		// the reviewer's are both finished at this point, and "Reviewed" left the tester unsure
		// whether anything else was owed of them.
		return "Completed"
	case StatusCancelled:
		return "Cancelled — retest created"
	default:
		return ""
	}
}

// NextStepNo is the first working step without a completion; 0 when all are done.
func NextStepNo(completions []StepCompletion) int {
	for _, no := range WorkingSteps() {
		if _, done := completionFor(completions, no); !done {
			return no
		}
	}
	return 0
}

// ReadingGuide explains how to read the strip, shown beside the outcome selector.
func ReadingGuide() []string {
	return []string{
		"Negative: test line as dark as, or darker than, the control line.",
		"Positive: test line fainter than the control line, or missing.",
		"Invalid strip: no control line — the strip is void; this test cancels and a retest is created.",
	}
}

// Filter keys the task list is sliced by. The client sends a KEY, never a status list, so the
// meaning of "Pending" stays one backend definition rather than a vocabulary each surface
// re-derives.
const (
	// FilterAll is the DEFAULT: every round, in whatever state.
	FilterAll = "all"
	// FilterPending is work still moving — being run, or waiting on a CEO/CXO reading.
	FilterPending = "pending"
	// FilterCompleted is work nobody has to touch again — accepted, or cancelled because an
	// invalid strip or a rejected review already minted the replacement round.
	FilterCompleted = "completed"
)

// TaskFilter is one selectable list slice with its BACKEND-OWNED label.
type TaskFilter struct {
	Key   string
	Label string
	// Statuses this filter resolves to; empty means every status.
	Statuses []string
	// EmptyMessage is what the screen says when THIS slice has no rows. It travels with the
	// filter because "nothing here" means something different in each: an empty Completed list
	// is not the same news as an empty Pending one, and a single message for all three tells
	// the operator the wrong thing in two of them.
	EmptyMessage string
}

// TaskFilters returns the list's filter chips in display order.
//
// Pending and Completed are DISJOINT and together EXHAUSTIVE over the four statuses, so no
// round can hide from both: a status added later without a home here would show only under
// All, which TestTaskFiltersPartitionEveryStatus refuses.
func TaskFilters() []TaskFilter {
	return []TaskFilter{
		{
			Key:          FilterAll,
			Label:        "All",
			EmptyMessage: "No feed loads waiting for a test",
		},
		{
			Key:          FilterPending,
			Label:        "Pending",
			Statuses:     []string{StatusInProgress, StatusPendingReview},
			EmptyMessage: "No tests waiting on anyone",
		},
		{
			Key:          FilterCompleted,
			Label:        "Completed",
			Statuses:     []string{StatusAccepted, StatusCancelled},
			EmptyMessage: "No tests finished yet",
		},
	}
}

// StatusesForFilter resolves a filter key to its statuses. An unknown or empty key resolves to
// All (nil), because a stale client sending a retired key must still see its work rather than an
// empty screen.
func StatusesForFilter(key string) []string {
	for _, f := range TaskFilters() {
		if f.Key == key {
			return f.Statuses
		}
	}
	return nil
}

// FilterKeyOrDefault normalizes a requested key, falling back to the default selection.
func FilterKeyOrDefault(key string) string {
	for _, f := range TaskFilters() {
		if f.Key == key {
			return f.Key
		}
	}
	return FilterAll
}

// CountForFilter sums whole-tenant status counts into one filter's badge. All is the total.
func CountForFilter(key string, statusCounts map[string]int) int {
	statuses := StatusesForFilter(key)
	if len(statuses) == 0 {
		total := 0
		for _, n := range statusCounts {
			total += n
		}
		return total
	}
	total := 0
	for _, s := range statuses {
		total += statusCounts[s]
	}
	return total
}

// hourPhrase keeps the overdue chip in farm words rather than a bare number.
func hourPhrase(hours int) string {
	if hours == 1 {
		return "1 hour"
	}
	if hours >= 48 {
		days := hours / 24
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	return fmt.Sprintf("%d hours", hours)
}
