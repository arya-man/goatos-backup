package domain

import (
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// Decision 5 (2026-09-16): a verifier REJECT of a capture-form proof sends a "re-shoot" step back
// to the operator with the reason.

func TestCaptureReshootStepsAreOnePerCaptureProofAndIdempotent(t *testing.T) {
	capture := authored.Evidence{Media: []authored.EvidenceMedia{
		{Ref: "r1", Kind: "photo", Label: "Newborns with the mother"},
		{Ref: "r2", Kind: "", Label: "Pen clip"},
	}}
	existing := []WorkflowAction{{ActionKey: "kid_clean", Seq: 3}, {ActionKey: ActionKeyParkHeadSignoff, Seq: 9, ActionType: ActionTypeApproval}}
	steps := CaptureReshootSteps(capture, existing, "recording-key-1", "Mother's face not visible")
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want one per capture proof", len(steps))
	}
	a := steps[0]
	if a.Title != "Re-shoot report proof · Newborns with the mother" || a.EngineHook != EngineHookReshootReport ||
		a.ProofMinPhotos != 1 || a.ProofMinVideos != 0 || a.Status != ActionStatusRework || a.ReworkReason == nil || *a.ReworkReason != "Mother's face not visible" {
		t.Fatalf("step 0 = %+v", a)
	}
	if a.Seq != 10 || steps[1].Seq != 11 || a.Section != SectionMain || a.ActionType != ActionTypeAction {
		t.Fatalf("seq/section = %d %d %q %q", a.Seq, steps[1].Seq, a.Section, a.ActionType)
	}
	if idx, ok := ReshootMediaIndex(a); !ok || idx != 0 {
		t.Fatalf("index = %d %v", idx, ok)
	}
	if idx, ok := ReshootMediaIndex(steps[1]); !ok || idx != 1 {
		t.Fatalf("index = %d %v", idx, ok)
	}
	// An `either` / unknown-kind proof: neither minimum, but the step still demands a capture.
	if steps[1].ProofMinPhotos != 0 || steps[1].ProofMinVideos != 0 {
		t.Fatalf("either step minimums = %+v", steps[1])
	}
	empty := steps[1]
	empty.Status = ActionStatusPending
	if _, _, err := ApplyComplete(empty, CompleteActionCommand{IdempotencyKey: "k", RequestFingerprint: "f"}); err != ErrProofRequired {
		t.Fatalf("a re-shoot step with no capture must be refused, got %v", err)
	}
	// Same verdict redelivered: identical keys (the natural key absorbs it).
	again := CaptureReshootSteps(capture, existing, "recording-key-1", "Mother's face not visible")
	if again[0].ActionKey != a.ActionKey || again[1].ActionKey != steps[1].ActionKey {
		t.Fatal("keys must be deterministic per verdict")
	}
	if other := CaptureReshootSteps(capture, existing, "recording-key-2", "x"); other[0].ActionKey == a.ActionKey {
		t.Fatal("a later rejection mints new steps")
	}
	if only := CaptureReshootSteps(capture, existing, "recording-key-1", "x", 1); len(only) != 1 || only[0].ActionKey != steps[1].ActionKey {
		t.Fatalf("a per-slot rejection re-shoots only that proof: %+v", only)
	}
	if len(CaptureReshootSteps(authored.Evidence{}, existing, "k", "r")) != 0 {
		t.Fatal("no capture media, no re-shoot")
	}
	// Recorded re-shoot completes outright on a per-step-reviewed track (its own item is the
	// report item, not a birth step item).
	done := a
	done.Status = ActionStatusCompleted
	done.ProofRefs = []ProofItem{{Ref: "new", Kind: "photo"}}
	if got := HoldStepForReview(TemplateKeyBirthMother, done); got.Status != ActionStatusCompleted {
		t.Fatalf("re-shoot step must not wait for a per-step verdict, got %q", got.Status)
	}
}

func TestReplaceCaptureMediaSwapsOnlyTheReshotProof(t *testing.T) {
	capture := authored.Evidence{VersionLabel: "v2", Media: []authored.EvidenceMedia{
		{Ref: "r1", Kind: "photo", Label: "A"}, {Ref: "r2", Kind: "video", Label: "B"},
	}}
	got := ReplaceCaptureMedia(capture, 1, ProofItem{Ref: "new", Kind: "video"})
	want := authored.Evidence{VersionLabel: "v2", Media: []authored.EvidenceMedia{
		{Ref: "r1", Kind: "photo", Label: "A"}, {Ref: "new", Kind: "video", Label: "B"},
	}}
	if !reflect.DeepEqual(got, want) || capture.Media[1].Ref != "r2" {
		t.Fatalf("got %+v (input must not be mutated)", got)
	}
	if out := ReplaceCaptureMedia(capture, 5, ProofItem{Ref: "x"}); !reflect.DeepEqual(out, capture) {
		t.Fatal("out-of-range index changes nothing")
	}
}
