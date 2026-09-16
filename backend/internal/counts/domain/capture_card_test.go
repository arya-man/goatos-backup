package domain

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

func captureCardDoc(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

const sampleCaptureCard = `{"follow_up":{"schema_version":"goatos.sop-followup.v1"},"capture_card":{"schema_version":"goatos.sop-capture.v1","instruction":"Photograph the newborns with the mother.",
 "proofs":[{"key":"newborns_with_mother","title":"Newborns with the mother","kind":"photo","required":true},{"key":"pen_video","title":"Pen video","kind":"either","required":false}],
 "questions":[{"id":"delivery_type","kind":"choice","title":"How was the delivery?","required":true,"options":[{"value":"normal","label":"Normal"},{"value":"assisted","label":"Assisted"}]},
  {"id":"assisted_by","kind":"text","title":"Who assisted?","required":true,"only_if":{"question_id":"delivery_type","value":"assisted"}},
  {"id":"kid_weight","kind":"number","title":"Kid weight","required":false,"unit":"kg"}]}}`

func TestParseCaptureCardAbsentIsTheEmptyCard(t *testing.T) {
	card, err := ParseCaptureCard(captureCardDoc(t, `{"follow_up":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !card.IsEmpty() || len(card.Proofs) != 0 || len(card.Questions) != 0 {
		t.Fatalf("absent section must be the empty card (today's behaviour), got %+v", card)
	}
	card, err = ParseCaptureCard(captureCardDoc(t, sampleCaptureCard))
	if err != nil {
		t.Fatal(err)
	}
	if card.IsEmpty() || len(card.Proofs) != 2 || len(card.Questions) != 3 || card.Instruction == "" {
		t.Fatalf("card = %+v", card)
	}
	if !card.Proofs[0].Required || card.Proofs[1].Required {
		t.Fatalf("required flags = %+v", card.Proofs)
	}
	if _, err := ParseCaptureCard(captureCardDoc(t, `{"capture_card":{"schema_version":"nope"}}`)); err == nil {
		t.Fatal("an unknown schema version must be refused")
	}
	if _, err := ParseCaptureCard(captureCardDoc(t, `{"capture_card":"not an object"}`)); err == nil {
		t.Fatal("a non-object section must be refused")
	}
}

func TestValidateCaptureCardNamesEveryProblemByPath(t *testing.T) {
	card := CaptureCard{SchemaVersion: CaptureSchemaVersion,
		Proofs: []authored.ProofSlot{
			{Key: "Bad Key", Title: "", Kind: "gif", Required: true},
			{Key: "dup", Title: "One", Kind: "photo", Required: true},
			{Key: "dup", Title: "Two", Kind: "video", Required: true},
		},
		Questions: []authored.Question{
			{ID: "q1", Kind: "choice", Title: "Pick", Required: true},
			{ID: "q2", Kind: "text", Title: "", OnlyIf: &authored.Condition{QuestionID: "missing", Value: "x"}},
		},
	}
	problems := ValidateCaptureCard(card)
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"capture_card.proofs.0.key", "capture_card.proofs.0.title", "capture_card.proofs.0.kind",
		"capture_card.proofs.2.key", "capture_card.questions.0.options", "capture_card.questions.1.title",
		"capture_card.questions.1.only_if.question_id",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("problems must name %q by path, got:\n%s", want, joined)
		}
	}
	// Bounds: more than the authored maximum of slots is one problem naming the list.
	many := CaptureCard{SchemaVersion: CaptureSchemaVersion}
	for i := 0; i < authored.MaxProofSlots+1; i++ {
		many.Proofs = append(many.Proofs, authored.ProofSlot{Key: "s" + string(rune('a'+i)), Title: "S", Kind: "photo", Required: true})
	}
	if p := ValidateCaptureCard(many); len(p) == 0 || !strings.Contains(p[0], "capture_card.proofs") {
		t.Fatalf("slot bound not enforced: %v", p)
	}
	if p := ValidateCaptureCard(CaptureCard{SchemaVersion: CaptureSchemaVersion, Instruction: strings.Repeat("x", authored.MaxTextLength+1)}); len(p) != 1 || !strings.Contains(p[0], "capture_card.instruction") {
		t.Fatalf("instruction bound: %v", p)
	}
}

func TestQuestionsOnlyCardIsValid(t *testing.T) {
	card := CaptureCard{SchemaVersion: CaptureSchemaVersion, Questions: []authored.Question{
		{ID: "delivery_type", Kind: "choice", Title: "How was the delivery?", Required: true, Options: []authored.Option{{Value: "normal", Label: "Normal"}}},
	}}
	if p := ValidateCaptureCard(card); len(p) != 0 {
		t.Fatalf("a questions-only card is allowed (maintainer decision 6): %v", p)
	}
	if p := ValidateCaptureCard(CaptureCard{SchemaVersion: CaptureSchemaVersion}); len(p) != 0 {
		t.Fatalf("an empty card is allowed: %v", p)
	}
}

func TestCaptureEvidenceComposesSlotOrderLabelsAndAnswerRows(t *testing.T) {
	card, err := ParseCaptureCard(captureCardDoc(t, sampleCaptureCard))
	if err != nil {
		t.Fatal(err)
	}
	proofs := authored.ProofRefs{"pen_video": "ref-pen", "newborns_with_mother": "ref-mother"}
	answers := authored.Answers{"delivery_type": json.RawMessage(`"assisted"`), "assisted_by": json.RawMessage(`"Amit"`), "kid_weight": json.RawMessage(`3.2`)}
	kinds := map[string]string{"ref-pen": "video", "ref-mother": "photo"}
	got := ComposeCaptureEvidence("v2", card, proofs, kinds, answers, []string{"Pen video"})
	want := authored.Evidence{
		VersionLabel: "v2",
		Media: []authored.EvidenceMedia{
			{Ref: "ref-mother", Kind: "photo", Label: "Newborns with the mother"},
			{Ref: "ref-pen", Kind: "video", Label: "Pen video"},
		},
		Rows: []authored.EvidenceRow{
			{Label: "How was the delivery?", Value: "Assisted", Group: CaptureEvidenceGroup},
			{Label: "Who assisted?", Value: "Amit", Group: CaptureEvidenceGroup},
			{Label: "Kid weight", Value: "3.2 kg", Group: CaptureEvidenceGroup},
		},
		MissingNote: "Pen video",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evidence =\n%+v\nwant\n%+v", got, want)
	}
	// An `either` slot whose register kind is unknown keeps a BLANK kind (never a guessed video).
	got = ComposeCaptureEvidence("v2", card, authored.ProofRefs{"newborns_with_mother": "ref-mother", "pen_video": "ref-pen"}, map[string]string{"ref-mother": "photo"}, nil, nil)
	if got.Media[1].Kind != "" {
		t.Fatalf("either slot with no register kind must stay blank, got %q", got.Media[1].Kind)
	}
	// A photo slot whose register kind is unknown takes the slot's own kind.
	if got.Media[0].Kind != "photo" {
		t.Fatalf("typed slot falls back to its kind, got %q", got.Media[0].Kind)
	}
	// Missing items join into one note.
	got = ComposeCaptureEvidence("v1", card, nil, nil, nil, []string{"Newborns with the mother", "How was the delivery?"})
	if got.MissingNote != "Newborns with the mother; How was the delivery?" {
		t.Fatalf("missing note = %q", got.MissingNote)
	}
}

func TestJudgeCaptureOlderAppAndNewApp(t *testing.T) {
	card, _ := ParseCaptureCard(captureCardDoc(t, sampleCaptureCard))
	// OLDER APP: nothing sent. Accepted; every compulsory slot/question is noted as missing.
	res, err := JudgeCapture(card, nil)
	if err != nil {
		t.Fatalf("older app must be accepted: %v", err)
	}
	if !reflect.DeepEqual(res.Missing, []string{"Newborns with the mother", "How was the delivery?"}) {
		t.Fatalf("missing = %v", res.Missing)
	}
	if len(res.Proofs) != 0 || len(res.Answers) != 0 {
		t.Fatalf("older app maps nothing: %+v", res)
	}
	// Older app on an EMPTY card notes nothing.
	if res, _ := JudgeCapture(CaptureCard{}, nil); len(res.Missing) != 0 {
		t.Fatalf("empty card notes nothing: %v", res.Missing)
	}
	// NEW APP: judged strictly.
	_, err = JudgeCapture(card, &CaptureSubmission{Proofs: authored.ProofRefs{}, Answers: authored.Answers{"delivery_type": json.RawMessage(`"normal"`)}})
	var slotErr *authored.ProofError
	if !errorsAs(err, &slotErr) || slotErr.SlotKey != "newborns_with_mother" {
		t.Fatalf("missing compulsory slot must name it: %v", err)
	}
	_, err = JudgeCapture(card, &CaptureSubmission{Proofs: authored.ProofRefs{"newborns_with_mother": "r1"}, Answers: authored.Answers{"delivery_type": json.RawMessage(`"assisted"`)}})
	var ansErr *authored.AnswerError
	if !errorsAs(err, &ansErr) || ansErr.QuestionID != "assisted_by" {
		t.Fatalf("missing conditional answer must name it: %v", err)
	}
	res, err = JudgeCapture(card, &CaptureSubmission{Proofs: authored.ProofRefs{"newborns_with_mother": "r1", "unused": ""}, Answers: authored.Answers{"delivery_type": json.RawMessage(`"normal"`), "assisted_by": json.RawMessage(`"x"`)}})
	if err != nil {
		t.Fatalf("valid new-app submit: %v", err)
	}
	if len(res.Missing) != 0 || res.Proofs["newborns_with_mother"] != "r1" || len(res.Answers) != 1 {
		t.Fatalf("new app result = %+v (conditional answer dropped, blank slot dropped)", res)
	}
}

func errorsAs(err error, target any) bool {
	switch t := target.(type) {
	case **authored.ProofError:
		var e *authored.ProofError
		if asErr(err, &e) {
			*t = e
			return true
		}
	case **authored.AnswerError:
		var e *authored.AnswerError
		if asErr(err, &e) {
			*t = e
			return true
		}
	}
	return false
}

func asErr(err error, target any) bool { return errors.As(err, target) }
