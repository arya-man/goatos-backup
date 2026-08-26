package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var anchor = time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)

func done(stepNo int, at time.Time) StepCompletion {
	return StepCompletion{StepNo: stepNo, ProofRef: "proof", CompletedAt: at}
}

func TestStepOrderIsEnforced(t *testing.T) {
	// Step 2 before step 1 is refused.
	if _, err := CheckStepCompletable(StatusInProgress, 2, nil, anchor); !errors.Is(err, ErrStepOutOfOrder) {
		t.Fatalf("step 2 first: err = %v", err)
	}
	// Step 1 on a fresh task is fine.
	if _, err := CheckStepCompletable(StatusInProgress, 1, nil, anchor); err != nil {
		t.Fatalf("step 1 fresh: err = %v", err)
	}
	// The wait row is never completable.
	if _, err := CheckStepCompletable(StatusInProgress, 4, nil, anchor); !errors.Is(err, ErrStepNotCompletable) {
		t.Fatalf("wait row: err = %v", err)
	}
	// A finished step cannot be redone.
	if _, err := CheckStepCompletable(StatusInProgress, 1, []StepCompletion{done(1, anchor)}, anchor); !errors.Is(err, ErrStepAlreadyDone) {
		t.Fatalf("redo step 1: err = %v", err)
	}
	// Only an in-progress task takes step work.
	for _, status := range []string{StatusPendingReview, StatusAccepted, StatusCancelled} {
		if _, err := CheckStepCompletable(status, 1, nil, anchor); !errors.Is(err, ErrTaskNotOpen) {
			t.Fatalf("status %s: err = %v", status, err)
		}
	}
	if _, err := CheckStepCompletable(StatusInProgress, 9, nil, anchor); !errors.Is(err, ErrUnknownStep) {
		t.Fatalf("unknown step: err = %v", err)
	}
}

// The three wait gates are hard blocks on the server clock: 60 min after step 3 before
// step 5, 3 min after step 5 before step 6, 8 min after step 6 before step 7.
func TestWaitGatesAreHardBlocked(t *testing.T) {
	base := []StepCompletion{done(1, anchor), done(2, anchor), done(3, anchor)}

	// 59 minutes after the shake video: still blocked, and the caller learns when it opens.
	opensAt, err := CheckStepCompletable(StatusInProgress, 5, base, anchor.Add(59*time.Minute))
	if !errors.Is(err, ErrWaitNotElapsed) {
		t.Fatalf("step 5 early: err = %v", err)
	}
	if want := anchor.Add(60 * time.Minute); !opensAt.Equal(want) {
		t.Fatalf("step 5 opens at %v, want %v", opensAt, want)
	}
	// At the hour it opens.
	if _, err := CheckStepCompletable(StatusInProgress, 5, base, anchor.Add(60*time.Minute)); err != nil {
		t.Fatalf("step 5 on time: err = %v", err)
	}

	step5At := anchor.Add(61 * time.Minute)
	withFive := append(append([]StepCompletion{}, base...), done(5, step5At))
	if _, err := CheckStepCompletable(StatusInProgress, 6, withFive, step5At.Add(2*time.Minute)); !errors.Is(err, ErrWaitNotElapsed) {
		t.Fatalf("step 6 early: err = %v", err)
	}
	if _, err := CheckStepCompletable(StatusInProgress, 6, withFive, step5At.Add(3*time.Minute)); err != nil {
		t.Fatalf("step 6 on time: err = %v", err)
	}

	step6At := step5At.Add(4 * time.Minute)
	withSix := append(append([]StepCompletion{}, withFive...), done(6, step6At))
	if _, err := CheckStepCompletable(StatusInProgress, 7, withSix, step6At.Add(7*time.Minute)); !errors.Is(err, ErrWaitNotElapsed) {
		t.Fatalf("step 7 early: err = %v", err)
	}
	if _, err := CheckStepCompletable(StatusInProgress, 7, withSix, step6At.Add(8*time.Minute)); err != nil {
		t.Fatalf("step 7 on time: err = %v", err)
	}
}

func TestSubmitDecision(t *testing.T) {
	for _, outcome := range []string{OutcomeNegative, OutcomePositive} {
		res, err := SubmitDecision(outcome)
		if err != nil || res.NextStatus != StatusPendingReview || res.CreatesRetest {
			t.Fatalf("%s: res = %+v err = %v", outcome, res, err)
		}
	}
	res, err := SubmitDecision(OutcomeInvalid)
	if err != nil || res.NextStatus != StatusCancelled || !res.CreatesRetest || res.RetestOrigin != OriginInvalidRetest {
		t.Fatalf("invalid: res = %+v err = %v", res, err)
	}
	if res.CancelReason == "" {
		t.Fatal("invalid cancel reason must carry farm copy")
	}
	if _, err := SubmitDecision("faint"); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("unknown outcome: err = %v", err)
	}
}

func TestVerdictDecision(t *testing.T) {
	res, err := VerdictDecision(StatusPendingReview, VerdictAccept, "")
	if err != nil || res.NextStatus != StatusAccepted || res.CreatesRetest {
		t.Fatalf("accept: res = %+v err = %v", res, err)
	}
	res, err = VerdictDecision(StatusPendingReview, VerdictReject, "shake video does not show the load")
	if err != nil || res.NextStatus != StatusCancelled || !res.CreatesRetest || res.RetestOrigin != OriginRejectedRetest {
		t.Fatalf("reject: res = %+v err = %v", res, err)
	}
	if !strings.Contains(res.CancelReason, "shake video does not show the load") {
		t.Fatalf("reject reason must reach the cancel copy: %q", res.CancelReason)
	}
	if _, err := VerdictDecision(StatusPendingReview, VerdictReject, "  "); !errors.Is(err, ErrRejectReasonRequired) {
		t.Fatalf("blank reject reason: err = %v", err)
	}
	if _, err := VerdictDecision(StatusPendingReview, "approve", ""); !errors.Is(err, ErrInvalidVerdict) {
		t.Fatalf("unknown decision: err = %v", err)
	}
	for _, status := range []string{StatusInProgress, StatusAccepted, StatusCancelled} {
		if _, err := VerdictDecision(status, VerdictAccept, ""); !errors.Is(err, ErrNotReviewable) {
			t.Fatalf("status %s: err = %v", status, err)
		}
	}
}

// The copy firewall: chips and labels are farm wording, never raw tokens, and the
// waiting chip tells the tester how long is left — that is backend-owned because the
// phone's clock is not the gate's clock.
func TestBackendOwnedCopy(t *testing.T) {
	fresh := Task{Status: StatusInProgress}
	if got := StatusChip(fresh, nil, anchor); got != "Step 1 of 6" {
		t.Fatalf("fresh chip = %q", got)
	}
	waiting := []StepCompletion{done(1, anchor), done(2, anchor), done(3, anchor)}
	got := StatusChip(fresh, waiting, anchor.Add(30*time.Minute))
	if got != "Waiting — next step in 30 min" {
		t.Fatalf("waiting chip = %q", got)
	}
	if got := StatusChip(Task{Status: StatusPendingReview}, nil, anchor); got != "Waiting for review" {
		t.Fatalf("review chip = %q", got)
	}
	if got := StatusChip(Task{Status: StatusCancelled}, nil, anchor); got != "Cancelled — retest created" {
		t.Fatalf("cancelled chip = %q", got)
	}
	for _, outcome := range []string{OutcomeNegative, OutcomePositive, OutcomeInvalid} {
		if OutcomeLabel(outcome) == outcome || OutcomeLabel(outcome) == "" {
			t.Fatalf("outcome label for %q = %q", outcome, OutcomeLabel(outcome))
		}
	}
	if OriginLine(OriginInvalidRetest) == "" || OriginLine(OriginRejectedRetest) == "" || OriginLine(OriginPurchase) != "" {
		t.Fatal("origin lines: retests carry copy, round 1 carries none")
	}
	if len(Steps()) != 7 || len(WorkingSteps()) != 6 || len(ReadingGuide()) == 0 {
		t.Fatal("procedure shape drifted")
	}
}
