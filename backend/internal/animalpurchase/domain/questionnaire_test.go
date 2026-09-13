package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func j(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// fullAnswers is a complete, valid inspection of a lactating female goat.
func fullAnswers() Answers {
	return Answers{
		"species": j("goat"), "goat_id": j("GW-12"), "well_fed": j("yes"), "teeth": j("4"),
		"sex": j("female"), "pregnant": j("no"), "weight_kg": j(24.5), "height_cm": j("48"), "rectal_temp_c": j(39.1),
		"anaemic": j("no"), "mouth_breathing": j("no"), "watery_eyes": j("no"), "eye_colour": j("no"), "nasal_discharge": j("no"),
		"face_scabs": j("no"), "acidosis": j("no"), "diarrhea": j("no"), "ticks_hair_loss": j("no"), "wounds": j("no"),
		"body_scabs": j("no"), "lumps": j("no"), "arthritis": j("no"),
		"udder_state": j([]string{"normal"}), "lactating": j("yes"), "mastitis": j("negative"), "teats": j("2"), "teat_discharge": j("no"),
		"field_verdict": j("selected"), "breed": j("Sirohi"),
	}
}

func fullMedia() MediaRefs {
	return MediaRefs{SlotTeeth: {"p1"}, SlotAnimal: {"p2", "p3"}, SlotUdder: {"p4"}}
}

func field(err error) string {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Field
	}
	return "<nil>"
}

func TestQuestionnaireAcceptsACompleteInspection(t *testing.T) {
	if err := ValidateAnswers(fullAnswers(), fullMedia()); err != nil {
		t.Fatalf("complete inspection refused: %v", err)
	}
}

// Every REQUIRED question, when blanked, is the field the error names -- so the phone can
// scroll to it -- and no optional question is ever demanded.
func TestQuestionnaireNamesTheMissingRequiredQuestion(t *testing.T) {
	for _, q := range Questionnaire() {
		if q.Kind == KindSection || q.Kind == KindMedia {
			continue
		}
		a := fullAnswers()
		delete(a, q.ID)
		err := ValidateAnswers(a, fullMedia())
		if q.Required && a.Applies(q) {
			if field(err) != q.ID {
				t.Errorf("blank %s: want error on %s, got %v", q.ID, q.ID, err)
			}
		} else if err != nil {
			t.Errorf("blank optional %s must not fail, got %v", q.ID, err)
		}
	}
}

func TestQuestionnaireMediaRules(t *testing.T) {
	m := fullMedia()
	delete(m, SlotUdder)
	if field(ValidateAnswers(fullAnswers(), m)) != "udder_media" {
		t.Fatal("a required media slot must be demanded by its question id")
	}
	m = fullMedia()
	m[SlotUdder] = []string{"a", "b"}
	if field(ValidateAnswers(fullAnswers(), m)) != "udder_media" {
		t.Fatal("more files than the slot allows must be refused")
	}
	m = fullMedia()
	m["selfie"] = []string{"x"}
	if field(ValidateAnswers(fullAnswers(), m)) != "media" {
		t.Fatal("an unknown slot must be refused")
	}
	m = fullMedia()
	m[SlotWeight] = []string{"w1"}
	m[SlotSuspicious] = []string{"s1", "s2", "s3"}
	if err := ValidateAnswers(fullAnswers(), m); err != nil {
		t.Fatalf("optional slots within limits must pass: %v", err)
	}
}

func TestQuestionnaireConditionalQuestionsFollowTheSex(t *testing.T) {
	a := fullAnswers()
	a["sex"] = j("male")
	for _, id := range []string{"pregnant", "lactating", "mastitis", "teats", "teat_discharge", "milk_yield"} {
		delete(a, id)
	}
	// A male owes none of the female questions, and the udder slot doubles as testicles.
	if err := ValidateAnswers(a, fullMedia()); err != nil {
		t.Fatalf("male without female questions refused: %v", err)
	}
	a["scrotum_cm"] = j(31)
	if err := ValidateAnswers(a, fullMedia()); err != nil {
		t.Fatalf("male scrotum answer refused: %v", err)
	}
	// A female that is not lactating owes no mastitis result.
	b := fullAnswers()
	b["lactating"] = j("no")
	delete(b, "mastitis")
	if err := ValidateAnswers(b, fullMedia()); err != nil {
		t.Fatalf("non-lactating female refused: %v", err)
	}
}

func TestQuestionnaireRefusesBadValues(t *testing.T) {
	cases := []struct {
		id   string
		v    any
		want string
	}{
		{"teeth", "5", "teeth"},
		{"watery_eyes", "maybe", "watery_eyes"},
		{"rectal_temp_c", 55, "rectal_temp_c"},
		{"rectal_temp_c", "warm", "rectal_temp_c"},
		{"weight_kg", -3, "weight_kg"},
		{"udder_state", []string{"normal", "purple"}, "udder_state"},
		{"field_verdict", "accepted", "field_verdict"},
		{"goat_id", strings.Repeat("x", 600), "goat_id"},
	}
	for _, c := range cases {
		a := fullAnswers()
		a[c.id] = j(c.v)
		if got := field(ValidateAnswers(a, fullMedia())); got != c.want {
			t.Errorf("%s=%v: want error on %s, got %s", c.id, c.v, c.want, got)
		}
	}
}

// "Yes" on a where-question must say WHERE; the SOP's "mention area in others".
func TestQuestionnaireOtherTextIsRequiredWhenChosen(t *testing.T) {
	a := fullAnswers()
	a["wounds"] = j("other")
	if field(ValidateAnswers(a, fullMedia())) != "wounds" {
		t.Fatal("'other' without its text must be refused")
	}
	a["wounds_other"] = j("Left rear hoof")
	if err := ValidateAnswers(a, fullMedia()); err != nil {
		t.Fatalf("'other' with text refused: %v", err)
	}
	q, _ := QuestionByID("wounds")
	if got := AnswerLabel(q, a); got != "Yes · Left rear hoof" {
		t.Fatalf("display label = %q", got)
	}
}

func TestQuestionnaireDisplayLabels(t *testing.T) {
	a := fullAnswers()
	q, _ := QuestionByID("weight_kg")
	if got := AnswerLabel(q, a); got != "24.5 kg" {
		t.Fatalf("number label = %q", got)
	}
	q, _ = QuestionByID("udder_state")
	if got := AnswerLabel(q, a); got != "Normal" {
		t.Fatalf("multi label = %q", got)
	}
	q, _ = QuestionByID("watery_eyes")
	if got := AnswerLabel(q, a); got != "No" {
		t.Fatalf("choice label = %q", got)
	}
	if FieldVerdictLabel("on_hold") != "On hold on farm" {
		t.Fatal("verdict label")
	}
}

// Every question id is unique and every OnlyIf points at a question that exists and precedes it.
func TestQuestionnaireIsWellFormed(t *testing.T) {
	seen := map[string]int{}
	for i, q := range Questionnaire() {
		if _, dup := seen[q.ID]; dup {
			t.Fatalf("duplicate question id %s", q.ID)
		}
		seen[q.ID] = i
		if q.OnlyIf != nil {
			j, ok := seen[q.OnlyIf.QuestionID]
			if !ok || j >= i {
				t.Fatalf("%s depends on %s which does not precede it", q.ID, q.OnlyIf.QuestionID)
			}
		}
		if q.Kind == KindMedia && (q.Slot == "" || q.MaxFiles == 0) {
			t.Fatalf("media question %s needs a slot and a limit", q.ID)
		}
		if (q.Kind == KindChoice || q.Kind == KindMulti) && len(q.Options) == 0 {
			t.Fatalf("choice question %s has no options", q.ID)
		}
	}
}
