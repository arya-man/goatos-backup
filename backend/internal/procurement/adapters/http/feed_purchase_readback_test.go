package http

import (
	"encoding/json"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"testing"
)

func TestFeedPurchaseHistoricalAnswersOnWire(t *testing.T) {
	var purchase domain.FeedPurchase
	// A hydrated purchase carries its persisted version, extras, and resolved historical labels.
	err := json.Unmarshal([]byte(`{"SOPAnswers":{"lorry_number":"TN42 ABC"},"QuestionnaireVersion":2,"AnswerRows":[{"QuestionID":"lorry_number","Label":"Original lorry label","Value":"TN42 ABC"}]}`), &purchase)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(toFeedPurchasePayload(purchase))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Answers map[string]string `json:"answers"`
		Version int               `json:"questionnaire_version"`
		Rows    []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"answer_rows"`
	}
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.Answers["lorry_number"] != "TN42 ABC" || len(got.Rows) != 1 || got.Rows[0].Label != "Original lorry label" || got.Rows[0].Value != "TN42 ABC" {
		t.Fatalf("historical answers lost: %s", wire)
	}
}
