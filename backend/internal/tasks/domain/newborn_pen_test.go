package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func birthMoment() time.Time {
	return time.Date(2026, 8, 20, 9, 30, 0, 0, biztime.DefaultLocation())
}

func findAction(t *testing.T, tmpl Template, key string) (ActionTemplate, bool) {
	t.Helper()
	for _, a := range tmpl.Actions {
		if a.Key == key {
			return a, true
		}
	}
	return ActionTemplate{}, false
}

// The fallback step exists ONLY when the birth could not resolve a kid pen. A kid already placed in
// one is finished with placement, and asking its operator to record a shed would be busywork that
// invites them to move a correctly-filed animal.
func TestRecordShedStepAppearsOnlyWhenPlacementIsOwed(t *testing.T) {
	if _, ok := findAction(t, TemplateBirthKidAt(birthMoment(), false), ActionKeyRecordShed); ok {
		t.Fatal("a kid already in a kid pen must not carry the Record shed step")
	}
	step, ok := findAction(t, TemplateBirthKidAt(birthMoment(), true), ActionKeyRecordShed)
	if !ok {
		t.Fatal("a kid whose park has no kid pen must carry the Record shed step")
	}
	if step.Section != SectionMain || step.Type != ActionTypeQuestion {
		t.Fatalf("section=%q type=%q, want a main-section question", step.Section, step.Type)
	}
	// No video: the operator is recording WHERE the kid already is, not proving an action they
	// performed. Requiring a clip would gate a configuration repair behind a camera.
	if step.RequiresVideo {
		t.Fatal("Record shed must not require a video: it records a fact, not an action")
	}
}

// Adding the step must not renumber the colostrum series onto a main-section seq, and Tag the kid
// must stay last. Both are ordering contracts other rules read: the colostrum rounds unlock one at a
// time, and Tag the kid waits for every other operator step.
func TestRecordShedStepKeepsTheKidTrackOrdering(t *testing.T) {
	for _, needsPlacement := range []bool{false, true} {
		tmpl := TemplateBirthKidAt(birthMoment(), needsPlacement)
		seqs := map[int]string{}
		maxSeq, lastKey := 0, ""
		for _, a := range tmpl.Actions {
			if previous, clash := seqs[a.Seq]; clash {
				t.Fatalf("needsPlacement=%v: seq %d is used by both %q and %q",
					needsPlacement, a.Seq, previous, a.Key)
			}
			seqs[a.Seq] = a.Key
			if a.Seq > maxSeq {
				maxSeq, lastKey = a.Seq, a.Key
			}
		}
		if lastKey != ActionKeyTagTheKid {
			t.Fatalf("needsPlacement=%v: last step is %q, want %q", needsPlacement, lastKey, ActionKeyTagTheKid)
		}
	}
	// The step lands after the immediate medical work: a kid is cleaned, dipped, fed and weighed in
	// the minutes after delivery, and a pen question must not hold that behind a configuration gap.
	tmpl := TemplateBirthKidAt(birthMoment(), true)
	step, _ := findAction(t, tmpl, ActionKeyRecordShed)
	standing, _ := findAction(t, tmpl, ActionKeyKidStanding)
	if step.Seq <= standing.Seq {
		t.Fatalf("record shed seq=%d must come after the immediate main steps (kid standing seq=%d)",
			step.Seq, standing.Seq)
	}
}

func TestRecordedPenAnswerRoundTrips(t *testing.T) {
	label := "Part 3"
	answer := FormatRecordedPenAnswer("shed-1", &label)
	shedID, partition, err := ParseRecordedPenAnswer(answer)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if shedID != "shed-1" || partition != "Part 3" {
		t.Fatalf("shed=%q partition=%q, want shed-1 / Part 3", shedID, partition)
	}
	// The HUMAN label survives, never the normalized matching key. Storing "3" here would render
	// "Mandela 2 - 3" back to the operator and the verifier.
	if partition == "3" {
		t.Fatal("the answer must carry the human partition label, not the matching key")
	}
}

func TestRecordedPenAnswerRejectsMalformedValues(t *testing.T) {
	for _, answer := range []string{"", "   ", "|Part 3", "|", "shed-1|Part|3"} {
		if _, _, err := ParseRecordedPenAnswer(answer); err == nil {
			t.Fatalf("answer %q was accepted; a value that resolves to no pen must be refused", answer)
		}
	}
	// A bare shed IS legal at the format layer: the adapter proves the shed has no pens before it
	// accepts one, which is a question this pure function cannot answer.
	if _, _, err := ParseRecordedPenAnswer("shed-1|"); err != nil {
		t.Fatalf("a bare-shed answer must parse; the pen check belongs to the adapter: %v", err)
	}
}

// The state machine refuses a malformed pen answer BEFORE marking the step completed, so a kid is
// never recorded as placed by a value that names no pen.
func TestAnswerRefusesAMalformedRecordShedValueWithoutCompleting(t *testing.T) {
	action := WorkflowAction{
		ActionID: "action-1", ActionKey: ActionKeyRecordShed,
		ActionType: ActionTypeQuestion, Status: ActionStatusPending,
	}
	updated, replay, err := ApplyAnswer(action, AnswerActionCommand{
		TenantID: "t", AnswerValue: "not-a-pen-key|a|b",
		IdempotencyKey: "k1", RequestFingerprint: "f1", AnsweredAt: birthMoment(),
	})
	if err == nil {
		t.Fatal("a malformed pen answer must be refused")
	}
	if replay || updated.Status == ActionStatusCompleted {
		t.Fatalf("status=%q replay=%v: a refused answer must leave the step pending", updated.Status, replay)
	}
}
