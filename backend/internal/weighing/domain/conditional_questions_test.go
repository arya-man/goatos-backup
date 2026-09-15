package domain

import (
	"encoding/json"
	"testing"
)

func TestRemovalQuestionsRequireApplicableAncestors(t *testing.T) {
	r := SeededRules()
	options := []SOPOption{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}
	r.FeedWaterRemoval.Questions = []SOPQuestion{
		{ID: "first", Title: "First", Kind: SOPQuestionChoice, Required: true, Options: options},
		{ID: "second", Title: "Second", Kind: SOPQuestionChoice, Required: true, Options: options, OnlyIf: &SOPCondition{QuestionID: "first", Value: "yes"}},
		{ID: "third", Title: "Third", Kind: SOPQuestionText, Required: true, OnlyIf: &SOPCondition{QuestionID: "second", Value: "yes"}},
	}
	if problems := ValidateWeighingSOP(r.WeighingSOP); len(problems) > 0 {
		t.Fatal(problems)
	}
	a := SOPAnswers{"first": json.RawMessage(`"no"`), "second": json.RawMessage(`"yes"`)}
	if err := r.ValidateRemovalAnswers(a); err != nil {
		t.Fatalf("hidden parent must not activate third: %v", err)
	}
	a["third"] = json.RawMessage(`"stale answer"`)
	normalized := r.NormalizeRemovalAnswers(a)
	if len(normalized) != 1 {
		t.Fatalf("hidden descendants survived: %v", normalized)
	}
	a["first"] = json.RawMessage(`"yes"`)
	delete(a, "third")
	if err := r.ValidateRemovalAnswers(a); err == nil {
		t.Fatal("active required descendant must block")
	}
	a["third"] = json.RawMessage(`"fresh answer"`)
	if err := r.ValidateRemovalAnswers(a); err != nil {
		t.Fatal(err)
	}
	if len(r.NormalizeRemovalAnswers(a)) != 3 {
		t.Fatal("active answers lost")
	}
}
