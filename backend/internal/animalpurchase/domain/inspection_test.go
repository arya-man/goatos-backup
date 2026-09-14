package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestSeededInspectionCompilesToTheLegacyQuestionnaire pins day-one behaviour: the embedded
// document, paged, compiles to EXACTLY the catalog the Go code served before (same ids, kinds,
// titles, hints, options, slots, limits, conditions, order). Mutation-tested: editing one hint
// in the seed turns it red.
func TestSeededInspectionCompilesToTheLegacyQuestionnaire(t *testing.T) {
	dsl, err := ParseInspection(map[string]any{"inspection": json.RawMessage(SeededInspectionJSON())})
	if err != nil {
		t.Fatal(err)
	}
	if problems := ValidateInspection(dsl); len(problems) > 0 {
		t.Fatalf("seeded inspection does not validate: %v", problems)
	}
	got := CompileInspection(dsl)
	want := Questionnaire()
	if len(got) != len(want) {
		t.Fatalf("compiled %d questions, legacy %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			g, _ := json.Marshal(got[i])
			w, _ := json.Marshal(want[i])
			t.Fatalf("question %d differs:\n got %s\nwant %s", i, g, w)
		}
	}
	if SeededCatalog().Version != QuestionnaireVersion {
		t.Fatalf("seeded catalog version = %d, want %d", SeededCatalog().Version, QuestionnaireVersion)
	}
}

func TestValidateInspectionNamesTheField(t *testing.T) {
	dsl, _ := ParseInspection(map[string]any{"inspection": json.RawMessage(SeededInspectionJSON())})
	// break: duplicate id, a media question with no accepts, an only_if on a later question,
	// a locked kind change, and a missing locked question.
	p := &dsl.Pages[0]
	p.Questions = append(p.Questions, Question{ID: "goat_id", Kind: KindText, Title: "dup"})
	p.Questions = append(p.Questions, Question{ID: "extra_media", Kind: KindMedia, Title: "x"})
	p.Questions = append(p.Questions, Question{ID: "dep", Kind: KindChoice, Title: "d", Options: yesNo, OnlyIf: &Condition{QuestionID: "field_verdict", Value: "selected"}})
	for i := range p.Questions {
		if p.Questions[i].ID == "sex" {
			p.Questions[i].Kind = KindText
		}
	}
	last := &dsl.Pages[len(dsl.Pages)-1]
	var kept []Question
	for _, q := range last.Questions {
		if q.ID != "breed" {
			kept = append(kept, q)
		}
	}
	last.Questions = kept
	problems := strings.Join(ValidateInspection(dsl), "\n")
	for _, want := range []string{`"goat_id" is used twice`, "accepts: say whether", "must be an EARLIER question", `"sex" is fixed to "choice"`, `question "breed" must be present`} {
		if !strings.Contains(problems, want) {
			t.Errorf("expected a problem containing %q, got:\n%s", want, problems)
		}
	}
}

// An authored change is honoured by the compiled catalog the phone and validator use: make a
// question optional, move it to another page, and drop a photo requirement.
func TestAuthoredChangesReachTheCatalog(t *testing.T) {
	dsl, _ := ParseInspection(map[string]any{"inspection": json.RawMessage(SeededInspectionJSON())})
	for pi := range dsl.Pages {
		for qi := range dsl.Pages[pi].Questions {
			q := &dsl.Pages[pi].Questions[qi]
			if q.ID == "teeth_media" {
				q.Required = false
				q.Accepts = []string{"video"}
			}
		}
	}
	dsl.Pages = append(dsl.Pages, InspectionPage{Key: "extra", Title: "Extra checks", Questions: []Question{{ID: "hoof_photo", Kind: KindMedia, Title: "Hoof photo", Required: true, Accepts: []string{"photo"}, MaxFiles: 2}}})
	if problems := ValidateInspection(dsl); len(problems) > 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	cat := Catalog{Version: 2, Questions: CompileInspection(dsl)}
	a, m := fullAnswers(), fullMedia()
	delete(m, SlotTeeth)
	if err := cat.ValidateAnswers(a, m); field(err) != "hoof_photo" {
		t.Fatalf("want the new mandatory hoof photo to be demanded, got %v", err)
	}
	m["hoof_photo"] = []string{"p9"}
	if err := cat.ValidateAnswers(a, m); err != nil {
		t.Fatalf("teeth media made optional must not be demanded: %v", err)
	}
	if sec, ok := cat.ByID("sec_extra"); !ok || sec.Kind != KindSection || sec.Title != "Extra checks" {
		t.Fatalf("new page must compile to a section row, got %+v", sec)
	}
}
