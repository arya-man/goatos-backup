package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// eitherRaiseRules pins the ONE case where the raise snapshot is load-bearing: a raise slot
// authored as `either`. The card cannot say whether the operator sent a video or a photo, so
// concreteKind(slot.Kind) is "" and the stored snapshot is the only thing that knows.
func eitherRaiseRules() domain.ShiftingRules {
	r := pinnedRules()
	r.Raise.Proofs = []authored.ProofSlot{{Key: "pen_capture", Title: "Pen capture", Kind: authored.KindEither, Required: false}}
	return r
}

func eitherRaiseSvc(t *testing.T, evidence json.RawMessage) *capturingEnqueuer {
	t.Helper()
	result := lowResult()
	result.RaiseSOPProofs = authored.ProofRefs{"pen_capture": "r1"}
	result.RaiseSOPAnswers = authored.Answers{"why": ans("Overcrowded")}
	result.RaiseCaptureEvidence = evidence
	repo := &sopShiftingRepo{pin: approvedPin(intp(1), "low"), result: result}
	rules := &ports.StaticShiftingSOPRules{Published: eitherRaiseRules(), ByVersion: map[int]domain.ShiftingRules{1: eitherRaiseRules()}}
	svc, enq := sopService(t, repo, rules, &sopProofMedia{kinds: map[string]string{"v1": "video"}})
	in := baseInput()
	in.SOPProofs = authored.ProofRefs{domain.SlotShiftingVideo: "v1"}
	in.SOPAnswers = authored.Answers{"calm": ans("yes")}
	if _, _, err := svc.Complete(context.Background(), in); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	return enq
}

func raiseMetaKind(t *testing.T, enq *capturingEnqueuer) string {
	t.Helper()
	for i, ref := range enq.request.MediaRefs {
		if ref == "r1" {
			return enq.request.MediaMeta[i].Kind
		}
	}
	t.Fatalf("the raise capture never reached the verifier: refs=%v", enq.request.MediaRefs)
	return ""
}

// THE RAISE SNAPSHOT IS THE ONLY RECORD OF WHAT AN `either` CAPTURE ACTUALLY WAS.
//
// raiseMedia decodes shifting_events.raise_capture_evidence into CountsApprovalCapture and keys
// the kind by proof_id. That whole block can be DELETED today and the entire counts/app suite
// stays green (verified), because the one existing test that carries a raise snapshot authors its
// slot as `photo` and writes "photo" into the snapshot too. The assertion matches whichever
// branch produced it, so it passes with the snapshot read removed.
//
// Only an `either` slot separates the two: the card genuinely cannot say, so if the snapshot is
// not read the verifier is handed a capture with NO kind. That is the quiet failure -- not an
// error, just a proof the reviewer's player does not know how to open.
func TestRaiseEitherCaptureTakesItsKindFromTheStoredSnapshot(t *testing.T) {
	enq := eitherRaiseSvc(t, json.RawMessage(`{"version_label":"SOP v1","rows":[],"media":[{"proof_id":"r1","label":"Pen capture","kind":"video"}]}`))
	if got := raiseMetaKind(t, enq); got != "video" {
		t.Fatalf("raise capture kind = %q, want the video the snapshot recorded; the card only says %q", got, authored.KindEither)
	}
}

// THE TWO HALVES MUST AGREE ON THE FIELD NAMES, and nothing else checks that they do.
//
// JudgeRaise writes the snapshot; raiseMedia reads it back. They are separate structs in separate
// packages and the neighbouring authored.Evidence -- documented as "the same shape" -- spells this
// field `ref`, not `proof_id`. A snapshot written in that spelling still UNMARSHALS without error;
// it simply yields no kinds, and every `either` capture silently loses its kind. So the round trip
// is driven here through the production writer rather than a hand-written literal.
func TestTheRaiseSnapshotProductionWritesIsTheOneRaiseMediaCanRead(t *testing.T) {
	capture := domain.CountsApprovalCapture{
		VersionLabel: "SOP v1",
		Rows:         []domain.CountsApprovalCaptureRow{},
		Media:        []domain.CountsApprovalCaptureMedia{{ProofID: "r1", Label: "Pen capture", Kind: authored.KindPhoto}},
	}
	written, err := json.Marshal(capture)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got := raiseMetaKind(t, eitherRaiseSvc(t, written)); got != authored.KindPhoto {
		t.Fatalf("kind = %q after a round trip through the snapshot the writer produces, want %q -- the reader and the writer disagree about the field names",
			got, authored.KindPhoto)
	}
}

// With NO snapshot the card still cannot say, and the honest answer is a blank kind rather than a
// guess. Pinning it stops a future "helpful" default from asserting video for a photo.
func TestRaiseEitherCaptureWithNoSnapshotReportsNoKindRatherThanGuessing(t *testing.T) {
	if got := raiseMetaKind(t, eitherRaiseSvc(t, nil)); got != "" {
		t.Fatalf("raise capture kind = %q with nothing recorded; an `either` slot must not be guessed", got)
	}
}
