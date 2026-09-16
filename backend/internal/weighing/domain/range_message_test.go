package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestNumberOutOfRangeNamesOnlyTheBoundsAuthored: a number question with only a minimum told the
// operator "Enter a value between 0 and 0" -- the absent maximum read as 0.
func TestNumberOutOfRangeNamesOnlyTheBoundsAuthored(t *testing.T) {
	zero, ten := 0.0, 10.0
	cases := []struct {
		name     string
		min, max *float64
		answer   string
		want     string
	}{
		{"min only", &zero, nil, "-1", "Enter a value of at least 0 for: Buckets removed"},
		{"max only", nil, &ten, "11", "Enter a value of at most 10 for: Buckets removed"},
		{"both", &zero, &ten, "11", "Enter a value between 0 and 10 for: Buckets removed"},
	}
	for _, tc := range cases {
		q := []SOPQuestion{{ID: "buckets", Kind: SOPQuestionNumber, Title: "Buckets removed", Min: tc.min, Max: tc.max}}
		err := validateSOPAnswers(q, SOPAnswers{"buckets": json.RawMessage(tc.answer)}, "unknown")
		var ae *AnswerError
		if !errors.As(err, &ae) || ae.Message != tc.want {
			t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}
