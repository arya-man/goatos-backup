package authored

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestAnswerRefusalsReadAsFarmSentences: the phone showed "Say which, for: Suspected cause" on a
// refused death report (Realme E2E, 2026-09-17). Each refusal tells the operator what to do.
func TestAnswerRefusalsReadAsFarmSentences(t *testing.T) {
	cause := Question{
		ID: "cause", Kind: QuestionChoice, Title: "Suspected cause", Required: true, AllowOther: true,
		Options: []Option{{Value: "bloat", Label: "Bloat"}, {Value: "other", Label: "Other"}},
	}
	note := Question{ID: "note", Kind: QuestionText, Title: "What you saw"}
	cases := []struct {
		name      string
		questions []Question
		answers   Answers
		want      string
	}{
		{"other without text", []Question{cause}, Answers{"cause": json.RawMessage(`"other"`)}, "Write the other answer for: Suspected cause"},
		{"text too long", []Question{note}, Answers{"note": json.RawMessage(`"` + strings.Repeat("x", MaxTextLength+1) + `"`)}, "Write a shorter answer for: What you saw"},
	}
	for _, tc := range cases {
		err := ValidateAnswers(tc.questions, tc.answers)
		var ae *AnswerError
		if !errors.As(err, &ae) || ae.Message != tc.want {
			t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}
