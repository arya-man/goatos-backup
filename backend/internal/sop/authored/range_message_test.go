package authored

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestNumberOutOfRangeNamesOnlyTheBoundsAuthored: a number question with only a minimum told the
// operator "Enter a value between 0 and 0" (the absent maximum read as 0) -- found live by the
// herd-ops E2E on a birth capture card, 2026-09-17.
func TestNumberOutOfRangeNamesOnlyTheBoundsAuthored(t *testing.T) {
	zero, ten := 0.0, 10.0
	cases := []struct {
		name     string
		min, max *float64
		answer   string
		want     string
	}{
		{"min only", &zero, nil, "-1", "Enter a value of at least 0 for: Weak kids"},
		{"max only", nil, &ten, "11", "Enter a value of at most 10 for: Weak kids"},
		{"both", &zero, &ten, "11", "Enter a value between 0 and 10 for: Weak kids"},
	}
	for _, tc := range cases {
		q := []Question{{ID: "kids_weak", Kind: QuestionNumber, Title: "Weak kids", Min: tc.min, Max: tc.max}}
		err := ValidateAnswers(q, Answers{"kids_weak": json.RawMessage(tc.answer)})
		var ae *AnswerError
		if !errors.As(err, &ae) || !strings.EqualFold(ae.Message, tc.want) {
			t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}
