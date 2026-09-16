package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

type fakeCaptureSource struct {
	published CaptureCardVersion
	byID      map[string]CaptureCardVersion
}

func (f *fakeCaptureSource) PublishedCaptureCard(context.Context, string, string) (CaptureCardVersion, error) {
	return f.published, nil
}
func (f *fakeCaptureSource) CaptureCardVersion(_ context.Context, _, _, id string) (CaptureCardVersion, error) {
	v, ok := f.byID[id]
	if !ok {
		return CaptureCardVersion{}, ErrCaptureSOPVersionUnknown
	}
	return v, nil
}

type fakeCaptureKinds struct{ kinds map[string]string }

func (f fakeCaptureKinds) ProofKinds(_ context.Context, _ string, refs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, r := range refs {
		if k, ok := f.kinds[r]; ok {
			out[r] = k
		}
	}
	return out, nil
}

func birthCaptureCard() domain.CaptureCard {
	return domain.CaptureCard{SchemaVersion: domain.CaptureSchemaVersion, Instruction: "Photograph the newborns.",
		Proofs: []authored.ProofSlot{{Key: "newborns_with_mother", Title: "Newborns with the mother", Kind: "photo", Required: true}},
		Questions: []authored.Question{{ID: "delivery_type", Kind: "choice", Title: "How was the delivery?", Required: true,
			Options: []authored.Option{{Value: "normal", Label: "Normal"}, {Value: "assisted", Label: "Assisted"}}}},
	}
}

func TestOlderAppSubmitIsAcceptedAndMissingItemsAreNoted(t *testing.T) {
	src := &fakeCaptureSource{published: CaptureCardVersion{VersionID: "v-id", VersionLabel: "v2", Card: birthCaptureCard()}}
	svc := NewCaptureCardService(src, fakeCaptureKinds{})
	got, err := svc.Judge(context.Background(), "t", CaptureKindBirth, nil)
	if err != nil {
		t.Fatalf("older app must be accepted: %v", err)
	}
	want := &domain.ApprovalCapture{SOPVersionID: "v-id", Proofs: authored.ProofRefs{}, Answers: authored.Answers{},
		Evidence: authored.Evidence{VersionLabel: "v2", MissingNote: "Newborns with the mother; How was the delivery?"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("older app = %+v, want %+v", got, want)
	}
	// An empty published card and an older app: nothing to store at all.
	src.published = CaptureCardVersion{Card: domain.CaptureCard{SchemaVersion: domain.CaptureSchemaVersion}}
	if got, err := svc.Judge(context.Background(), "t", CaptureKindDeath, nil); err != nil || got != nil {
		t.Fatalf("empty card + older app = %+v err=%v, want nil", got, err)
	}
}

func TestNewAppSubmitIsJudgedStrictlyAndPinned(t *testing.T) {
	card := CaptureCardVersion{VersionID: "v-id", VersionLabel: "v2", Card: birthCaptureCard()}
	src := &fakeCaptureSource{published: CaptureCardVersion{VersionID: "v-newer", VersionLabel: "v3", Card: birthCaptureCard()}, byID: map[string]CaptureCardVersion{"v-id": card}}
	svc := NewCaptureCardService(src, fakeCaptureKinds{kinds: map[string]string{"ref-photo": "photo", "ref-video": "video"}})

	if _, err := svc.Judge(context.Background(), "t", CaptureKindBirth, &domain.CaptureSubmission{SOPVersionID: "gone"}); !errors.Is(err, ErrCaptureSOPVersionUnknown) {
		t.Fatalf("unknown pin err = %v", err)
	}
	var slotErr *authored.ProofError
	_, err := svc.Judge(context.Background(), "t", CaptureKindBirth, &domain.CaptureSubmission{SOPVersionID: "v-id", Answers: authored.Answers{"delivery_type": json.RawMessage(`"normal"`)}})
	if !errors.As(err, &slotErr) || slotErr.SlotKey != "newborns_with_mother" {
		t.Fatalf("missing compulsory slot must be named: %v", err)
	}
	// The register says video where the slot asks for a photo: refused by slot.
	_, err = svc.Judge(context.Background(), "t", CaptureKindBirth, &domain.CaptureSubmission{SOPVersionID: "v-id",
		Proofs: authored.ProofRefs{"newborns_with_mother": "ref-video"}, Answers: authored.Answers{"delivery_type": json.RawMessage(`"normal"`)}})
	if !errors.As(err, &slotErr) || slotErr.SlotKey != "newborns_with_mother" {
		t.Fatalf("register kind mismatch must name the slot: %v", err)
	}
	// A ref the register has not finished: refused by slot.
	_, err = svc.Judge(context.Background(), "t", CaptureKindBirth, &domain.CaptureSubmission{SOPVersionID: "v-id",
		Proofs: authored.ProofRefs{"newborns_with_mother": "ref-unknown"}, Answers: authored.Answers{"delivery_type": json.RawMessage(`"normal"`)}})
	if !errors.As(err, &slotErr) {
		t.Fatalf("unknown ref must name the slot: %v", err)
	}
	var ansErr *authored.AnswerError
	_, err = svc.Judge(context.Background(), "t", CaptureKindBirth, &domain.CaptureSubmission{SOPVersionID: "v-id",
		Proofs: authored.ProofRefs{"newborns_with_mother": "ref-photo"}, Answers: authored.Answers{"delivery_type": json.RawMessage(`"maybe"`)}})
	if !errors.As(err, &ansErr) || ansErr.QuestionID != "delivery_type" {
		t.Fatalf("bad answer must name the question: %v", err)
	}
	got, err := svc.Judge(context.Background(), "t", CaptureKindBirth, &domain.CaptureSubmission{SOPVersionID: "v-id",
		Proofs: authored.ProofRefs{"newborns_with_mother": "ref-photo"}, Answers: authored.Answers{"delivery_type": json.RawMessage(`"assisted"`)}})
	if err != nil {
		t.Fatal(err)
	}
	if got.SOPVersionID != "v-id" || got.Evidence.VersionLabel != "v2" {
		t.Fatalf("must pin the ECHOED version, not the newer publish: %+v", got)
	}
	wantEvidence := authored.Evidence{VersionLabel: "v2",
		Media: []authored.EvidenceMedia{{Ref: "ref-photo", Kind: "photo", Label: "Newborns with the mother"}},
		Rows:  []authored.EvidenceRow{{Label: "How was the delivery?", Value: "Assisted", Group: domain.CaptureEvidenceGroup}}}
	if !reflect.DeepEqual(got.Evidence, wantEvidence) {
		t.Fatalf("evidence = %+v, want %+v", got.Evidence, wantEvidence)
	}
	if _, err := svc.Card(context.Background(), "t", "shifting"); !errors.Is(err, ErrCaptureKindUnknown) {
		t.Fatalf("unknown kind err = %v", err)
	}
	view, err := svc.Card(context.Background(), "t", CaptureKindBirth)
	if err != nil || view.VersionID != "v-newer" || view.SOPCode != "counts.birth" || view.Card.Instruction == "" {
		t.Fatalf("card view = %+v err=%v", view, err)
	}
}
