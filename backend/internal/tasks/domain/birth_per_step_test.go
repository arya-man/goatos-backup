package domain

import "testing"

// Birth evidence is reviewed ONE STEP AT A TIME (maintainer decision 2026-09-16): every video the
// operator records goes to the verifier on its own, the moment it is recorded, and a rejection
// sends back exactly that step. These tests pin the three pure rules the postgres adapter and the
// fakes share so the two cannot drift.

func birthStep(id string, seq int, status string, proof string) WorkflowAction {
	a := WorkflowAction{
		ActionID: id, ActionKey: id, Seq: seq, Section: SectionMain, ActionType: ActionTypeAction,
		RequiresVideo: true, ProofMinVideos: 1, Status: status,
	}
	if proof != "" {
		a.ProofRefs = []ProofItem{{Ref: proof, Kind: ProofKindVideo}}
		a.ProofRef = &a.ProofRefs[0].Ref
	}
	return a
}

func TestBirthStepIsHeldForReviewTheMomentItIsRecorded(t *testing.T) {
	recorded := birthStep("iodine", 2, ActionStatusCompleted, "proof-iodine")
	held := HoldStepForReview(TemplateKeyBirthKid, recorded)
	if held.Status != ActionStatusInReview {
		t.Fatalf("birth step status after record = %q, want in_review", held.Status)
	}
	if held.ProofRef == nil || *held.ProofRef != "proof-iodine" {
		t.Fatal("holding a step for review must keep its proof; the verifier watches exactly this clip")
	}

	// A step with no proof to watch has nothing to verify and completes outright (Record pen).
	answered := WorkflowAction{ActionID: "record_shed", Seq: 8, ActionType: ActionTypeQuestion, Status: ActionStatusCompleted}
	if got := HoldStepForReview(TemplateKeyBirthKid, answered); got.Status != ActionStatusCompleted {
		t.Fatalf("proof-less birth step = %q, want completed", got.Status)
	}
	// Death keeps its bundle: both clips wait for the admin's approval, then ONE item.
	death := birthStep(ActionKeyDeathVideo, 1, ActionStatusCompleted, "proof-death")
	if got := HoldStepForReview(TemplateKeyDeath, death); got.Status != ActionStatusCompleted {
		t.Fatalf("death step = %q, want completed (death is not reviewed per step)", got.Status)
	}
}

func TestBirthStepUnderReviewOrSentBackNeverHoldsTheNextStep(t *testing.T) {
	steps := []WorkflowAction{
		birthStep("kid_clean", 1, ActionStatusInReview, "p1"),
		birthStep("iodine", 2, ActionStatusRework, ""),
		birthStep("teeth", 3, ActionStatusPending, ""),
		birthStep("suck", 4, ActionStatusPending, ""),
	}
	if OperatorActionBlocked(TemplateKeyBirthKid, steps[2], steps) {
		t.Fatal("step 3 must stay open while step 1 awaits its verdict and step 2 is being re-shot")
	}
	if OperatorActionBlocked(TemplateKeyBirthKid, steps[3], steps) == false {
		t.Fatal("step 4 still waits for the never-recorded step 3")
	}
	// Tag the kid waits for every other step to be RECORDED, not for the verifier to have watched
	// them all; day-3 tagging must not depend on the verifier's pace.
	tag := WorkflowAction{ActionID: ActionKeyTagTheKid, ActionKey: ActionKeyTagTheKid, Seq: 9, Section: SectionMain,
		ActionType: ActionTypeAction, Status: ActionStatusPending, WaitForAll: true}
	recorded := []WorkflowAction{
		birthStep("kid_clean", 1, ActionStatusInReview, "p1"),
		birthStep("iodine", 2, ActionStatusRework, ""),
		birthStep("teeth", 3, ActionStatusCompleted, "p3"),
		tag,
	}
	if OperatorActionBlocked(TemplateKeyBirthKid, tag, recorded) {
		t.Fatal("Tag the kid must open once every other step has been recorded at least once")
	}

	// Death is unchanged: a bounced pair re-shoots in order, second waits for first.
	deathSteps := []WorkflowAction{
		birthStep(ActionKeyDeathVideo, 1, ActionStatusRework, ""),
		birthStep(ActionKeyPostMortemVideo, 2, ActionStatusRework, ""),
	}
	if !OperatorActionBlocked(TemplateKeyDeath, deathSteps[1], deathSteps) {
		t.Fatal("death post-mortem must still wait for the re-shot death video")
	}
}

func TestRecomputeCardBirthAwaitsVerificationOnlyWhenNothingIsLeftToRecord(t *testing.T) {
	w := WorkflowInstance{TemplateKey: TemplateKeyBirthMother, State: WorkflowStateOpen}
	steps := []WorkflowAction{
		birthStep("babies_inside", 1, ActionStatusInReview, "p1"),
		birthStep("licking", 2, ActionStatusPending, ""),
	}
	got := RecomputeCard(w, steps)
	if got.ActionsDone != 1 || got.ActionsTotal != 2 {
		t.Fatalf("progress = %d/%d, want 1/2: a recorded clip is the operator's work done", got.ActionsDone, got.ActionsTotal)
	}
	if got.NextActionKey == nil || *got.NextActionKey != "licking" {
		t.Fatalf("next = %v, want licking (a step under review is never the next step)", got.NextActionKey)
	}
	if got.AwaitingVerification {
		t.Fatal("card must not read awaiting verification while the operator still has a step to record")
	}
	if got.State != WorkflowStateOpen {
		t.Fatalf("state = %q, want open", got.State)
	}

	steps[1] = birthStep("licking", 2, ActionStatusInReview, "p2")
	got = RecomputeCard(w, steps)
	if !got.AwaitingVerification || got.NextActionKey != nil || got.State != WorkflowStateOpen {
		t.Fatalf("all recorded, verdicts outstanding: awaiting=%v next=%v state=%q; want true/nil/open",
			got.AwaitingVerification, got.NextActionKey, got.State)
	}

	steps[0].Status = ActionStatusCompleted
	steps[1].Status = ActionStatusCompleted
	got = RecomputeCard(w, steps)
	if got.AwaitingVerification || got.State != WorkflowStateCompleted {
		t.Fatalf("all approved: awaiting=%v state=%q; want false/completed", got.AwaitingVerification, got.State)
	}

	// A rejection re-opens the card: one step back, the rest untouched.
	steps[0] = birthStep("babies_inside", 1, ActionStatusRework, "")
	got = RecomputeCard(w, steps)
	if got.AwaitingVerification || got.State != WorkflowStateOpen || got.ActionsDone != 1 ||
		got.NextActionKey == nil || *got.NextActionKey != "babies_inside" {
		t.Fatalf("after one rejection: awaiting=%v state=%q done=%d next=%v", got.AwaitingVerification, got.State, got.ActionsDone, got.NextActionKey)
	}
}

func TestBirthStepReviewIsKeyedPerRecording(t *testing.T) {
	a := birthStep("iodine", 2, ActionStatusInReview, "proof-a")
	a.RowVersion = 3
	first := BirthStepReviewKey(a)
	a.RowVersion = 5 // bounced and re-shot: even byte-identical proofs open a fresh item
	if BirthStepReviewKey(a) == first {
		t.Fatal("a re-shoot must never collide with the rejected recording's verification item")
	}
	if BirthStepReviewKey(a) != BirthStepReviewKey(a) {
		t.Fatal("the key must be stable so a retried request de-duplicates")
	}
}
