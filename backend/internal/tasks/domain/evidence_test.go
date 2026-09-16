package domain

import (
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

func evidenceStep(key, title string, seq int, proofs []ProofItem) WorkflowAction {
	a := WorkflowAction{ActionID: key + "-id", ActionKey: key, Seq: seq, Section: SectionMain, ActionType: ActionTypeAction, Title: title, Status: ActionStatusCompleted, ProofRefs: proofs, TaskType: "death_evidence", EngineHook: EngineHookDeathVideo}
	if len(proofs) > 0 {
		a.ProofMinVideos = 1
		for _, p := range proofs {
			if p.Kind == ProofKindPhoto {
				a.ProofMinPhotos = 1
			}
		}
		r := proofs[0].Ref
		a.ProofRef = &r
	}
	return a
}

func TestStepMediaMetaNumbersOnlyRepeatedKinds(t *testing.T) {
	a := evidenceStep("carcass", "Carcass photos", 1, []ProofItem{
		{Ref: "v1", Kind: ProofKindVideo}, {Ref: "p1", Kind: ProofKindPhoto}, {Ref: "p2", Kind: ProofKindPhoto},
	})
	got := StepMediaMeta(a)
	want := []MediaMetaItem{
		{Label: "Carcass photos", Kind: ProofKindVideo},
		{Label: "Carcass photos · photo 1 of 2", Kind: ProofKindPhoto},
		{Label: "Carcass photos · photo 2 of 2", Kind: ProofKindPhoto},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meta = %+v, want %+v", got, want)
	}
	// One of each kind: the raw title, no numbering (WS-A numbers repeats across the item).
	single := evidenceStep("one", "Record death video", 1, []ProofItem{{Ref: "v", Kind: ProofKindVideo}})
	if got := StepMediaMeta(single); len(got) != 1 || got[0].Label != "Record death video" {
		t.Fatalf("single = %+v", got)
	}
}

func TestStepMediaMetaKindComesFromTheProof(t *testing.T) {
	a := evidenceStep("s", "Step", 1, []ProofItem{{Ref: "p", Kind: ProofKindPhoto}})
	a.RequiresVideo = true // the legacy column says video; the register said photo
	got := StepMediaMeta(a)
	if len(got) != 1 || got[0].Kind != ProofKindPhoto {
		t.Fatalf("kind must come from the proof item, got %+v", got)
	}
	// Legacy single proof_ref with no kind on file is a video.
	legacy := WorkflowAction{Title: "Legacy", Status: ActionStatusCompleted}
	ref := "old"
	legacy.ProofRef = &ref
	if got := StepMediaMeta(legacy); len(got) != 1 || got[0].Kind != ProofKindVideo || got[0].Label != "Legacy" {
		t.Fatalf("legacy = %+v", got)
	}
	// Order matches AllProofRefs: videos first, then photos, whatever the capture order.
	mixed := evidenceStep("m", "Mixed", 1, []ProofItem{{Ref: "p", Kind: ProofKindPhoto}, {Ref: "v", Kind: ProofKindVideo}})
	got = StepMediaMeta(mixed)
	if got[0].Kind != ProofKindVideo || got[1].Kind != ProofKindPhoto {
		t.Fatalf("order must follow AllProofRefs (videos first): %+v", got)
	}
}

func TestStepAnswerRowReadsInFarmWords(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name   string
		action WorkflowAction
		pen    string
		want   EvidenceRow
		ok     bool
	}{
		{"yes/no", WorkflowAction{Title: "Is the kid clean?", AnswerType: AnswerKindYesNo, AnswerValue: str("yes")}, "", EvidenceRow{Label: "Is the kid clean?", Value: "Yes", Group: "Is the kid clean?"}, true},
		{"no", WorkflowAction{Title: "Suck reflex", AnswerType: AnswerKindYesNo, AnswerValue: str("no")}, "", EvidenceRow{Label: "Suck reflex", Value: "No", Group: "Suck reflex"}, true},
		{"multiselect", WorkflowAction{Title: "Symptoms", AnswerType: AnswerKindMultiSelect, AnswerValue: str("bloat|fever")}, "", EvidenceRow{Label: "Symptoms", Value: "bloat, fever", Group: "Symptoms"}, true},
		{"weight", WorkflowAction{Title: "Take Weight of Kid", AnswerType: AnswerKindNumber, EngineHook: EngineHookWeighKg, AnswerValue: str("3.2")}, "", EvidenceRow{Label: "Take Weight of Kid", Value: "3.2 kg", Group: "Take Weight of Kid"}, true},
		{"record pen", WorkflowAction{Title: "Record shed", EngineHook: EngineHookRecordPen, AnswerValue: str("shed-uuid|Part 3")}, "Godel 1 - Part 3", EvidenceRow{Label: "Record shed", Value: "Godel 1 - Part 3", Group: "Record shed"}, true},
		{"tag kid", WorkflowAction{Title: "Tag the kid", EngineHook: EngineHookTagKid, AnswerValue: str("1420 0001")}, "", EvidenceRow{Label: "Tag the kid", Value: "1420 0001", Group: "Tag the kid"}, true},
		{"blank", WorkflowAction{Title: "Nothing", AnswerValue: str("  ")}, "", EvidenceRow{}, false},
		{"nil", WorkflowAction{Title: "Nothing"}, "", EvidenceRow{}, false},
	}
	for _, tc := range cases {
		got, ok := StepAnswerRow(tc.action, tc.pen)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("%s: row=%+v ok=%v, want %+v ok=%v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDeathStepsCompleteRequiresEveryAuthoredStep(t *testing.T) {
	video := evidenceStep("death_video", "Record death video", 1, []ProofItem{{Ref: "v1", Kind: ProofKindVideo}})
	post := evidenceStep("post_mortem_video", "Record post-mortem video", 2, []ProofItem{{Ref: "v2", Kind: ProofKindVideo}})
	question := WorkflowAction{ActionKey: "cause", Seq: 3, Section: SectionMain, ActionType: ActionTypeQuestion, Title: "Likely cause?", Status: ActionStatusPending, TaskType: "question"}
	approval := WorkflowAction{ActionKey: ActionKeyParkHeadSignoff, Seq: 4, ActionType: ActionTypeApproval, Status: ActionStatusPending}
	// The legacy gate is satisfied by the two death_evidence steps alone -- that is bug 1.
	if !DeathVideosComplete([]WorkflowAction{video, post, question, approval}) {
		t.Fatal("precondition: the legacy pair gate is green")
	}
	if DeathStepsComplete([]WorkflowAction{video, post, question, approval}) {
		t.Fatal("an authored question step still pending must hold the death approval")
	}
	ans := "bloat"
	question.Status, question.AnswerValue = ActionStatusCompleted, &ans
	if !DeathStepsComplete([]WorkflowAction{video, post, question, approval}) {
		t.Fatal("every operator step completed must be complete")
	}
	// A completed proof step whose proofs were cleared (rework) is not complete.
	post.ProofRefs, post.ProofRef = nil, nil
	if DeathStepsComplete([]WorkflowAction{video, post, question}) {
		t.Fatal("a proof step without its proof must not count")
	}
	// Canceled steps are skipped; no operator steps at all is not complete.
	post.Status = ActionStatusCanceled
	if !DeathStepsComplete([]WorkflowAction{video, post, question}) {
		t.Fatal("canceled steps are ignored")
	}
	if DeathStepsComplete([]WorkflowAction{approval}) {
		t.Fatal("no operator step means nothing is complete")
	}
}

func TestDeathEvidenceBundleCarriesEveryProofTitleKindAndAnswer(t *testing.T) {
	ans := "bloat"
	actions := []WorkflowAction{
		{ActionKey: "cause", Seq: 3, ActionType: ActionTypeQuestion, Title: "Likely cause?", Status: ActionStatusCompleted, AnswerValue: &ans},
		evidenceStep("post_mortem_video", "Record post-mortem video", 2, []ProofItem{{Ref: "v2", Kind: ProofKindVideo}, {Ref: "p2", Kind: ProofKindPhoto}}),
		evidenceStep("death_video", "Record death video", 1, []ProofItem{{Ref: "v1", Kind: ProofKindVideo}}),
		{ActionKey: ActionKeyParkHeadSignoff, Seq: 4, ActionType: ActionTypeApproval, Status: ActionStatusPending},
		evidenceStep("canceled", "Canceled", 5, []ProofItem{{Ref: "x", Kind: ProofKindVideo}}),
	}
	actions[4].Status = ActionStatusCanceled
	capture := authored.Evidence{
		VersionLabel: "v2",
		Media:        []authored.EvidenceMedia{{Ref: "c1", Kind: "photo", Label: "Animal with tag"}},
		Rows:         []authored.EvidenceRow{{Label: "Found where?", Value: "In the pen"}},
		MissingNote:  "Not captured (older app): Carcass photo",
	}
	b := DeathEvidenceBundle(capture, actions)
	wantRefs := []string{"c1", "v1", "v2", "p2"}
	if !reflect.DeepEqual(b.Refs, wantRefs) {
		t.Fatalf("refs = %v, want capture first then steps in seq order: %v", b.Refs, wantRefs)
	}
	wantMeta := []MediaMetaItem{
		{Label: "At report · Animal with tag", Kind: ProofKindPhoto},
		{Label: "Record death video", Kind: ProofKindVideo},
		{Label: "Record post-mortem video", Kind: ProofKindVideo},
		{Label: "Record post-mortem video", Kind: ProofKindPhoto},
	}
	if !reflect.DeepEqual(b.Meta, wantMeta) {
		t.Fatalf("meta = %+v, want %+v", b.Meta, wantMeta)
	}
	wantRows := []EvidenceRow{
		{Label: "Found where?", Value: "In the pen", Group: "At report"},
		{Label: authored.MissingNoteOlderApp, Value: "Not captured (older app): Carcass photo", Group: "At report"},
		{Label: "Likely cause?", Value: "bloat", Group: "Likely cause?"},
	}
	if !reflect.DeepEqual(b.Rows, wantRows) {
		t.Fatalf("rows = %+v, want %+v", b.Rows, wantRows)
	}
	if len(b.Refs) != len(b.Meta) {
		t.Fatal("meta is positional against refs")
	}
}

func TestDeathEvidenceKeyIsByteIdenticalForTheSeededPair(t *testing.T) {
	actions := []WorkflowAction{
		evidenceStep("death_video", "Record death video", 1, []ProofItem{{Ref: "proof-death_video", Kind: ProofKindVideo}}),
		evidenceStep("post_mortem_video", "Record post-mortem video", 2, []ProofItem{{Ref: "proof-post_mortem_video", Kind: ProofKindVideo}}),
	}
	b := DeathEvidenceBundle(authored.Evidence{}, actions)
	got := DeathEvidenceKey("wf-1", 7, b.Refs)
	if got != "counts-death-evidence:wf-1:r7:proof-death_video:proof-post_mortem_video" {
		t.Fatalf("key = %q", got)
	}
	if !reflect.DeepEqual(b.Refs, DeathProofRefs(actions)) {
		t.Fatalf("seeded pair refs %v must equal the legacy DeathProofRefs %v", b.Refs, DeathProofRefs(actions))
	}
}

func TestDeathReworkReopensEveryProofStepClearsProofsKeepsAnswers(t *testing.T) {
	ans := "bloat"
	by := "op"
	actions := []WorkflowAction{
		evidenceStep("death_video", "Record death video", 1, []ProofItem{{Ref: "v1", Kind: ProofKindVideo}}),
		evidenceStep("carcass", "Carcass photo", 2, []ProofItem{{Ref: "p1", Kind: ProofKindPhoto}}),
		{ActionKey: "cause", Seq: 3, ActionType: ActionTypeQuestion, Title: "Likely cause?", Status: ActionStatusCompleted, AnswerValue: &ans, CompletedBy: &by},
		{ActionKey: ActionKeyParkHeadSignoff, Seq: 4, ActionType: ActionTypeApproval, Status: ActionStatusInReview},
	}
	actions[1].TaskType, actions[1].EngineHook, actions[1].ProofMinVideos, actions[1].ProofMinPhotos = "photo_record", "", 0, 1
	actions[1].CompletedBy = &by
	changed := ReopenDeathProofSteps(actions, "timestamp not visible")
	if len(changed) != 2 {
		t.Fatalf("changed = %d, want the two proof steps (question kept, approval untouched)", len(changed))
	}
	for _, i := range []int{0, 1} {
		a := actions[i]
		if a.Status != ActionStatusRework || a.ProofRef != nil || len(a.ProofRefs) != 0 || a.CompletedAt != nil || a.CompletedBy != nil {
			t.Fatalf("step %d = %+v, want rework with proofs cleared", i, a)
		}
		if a.ReworkReason == nil || *a.ReworkReason != "timestamp not visible" {
			t.Fatalf("step %d must carry the verifier's words", i)
		}
	}
	if actions[2].Status != ActionStatusCompleted || actions[2].AnswerValue == nil {
		t.Fatal("an answer-only step keeps its answer")
	}
	if got := ReopenDeathProofSteps(actions, "again"); len(got) != 0 {
		t.Fatal("a second rework on already-reopened steps is a no-op")
	}
}
