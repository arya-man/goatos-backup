package domain

import (
	"errors"
	"testing"
)

// TestNumberOutOfRangeNamesOnlyTheBoundsAuthored: a number question with only a minimum told the
// buyer "Enter a value between 0 and 0" -- the absent maximum read as 0. Both the inspection
// questions and the load form share the message.
func TestNumberOutOfRangeNamesOnlyTheBoundsAuthored(t *testing.T) {
	zero, ten := 0.0, 10.0
	cases := []struct {
		name     string
		min, max *float64
		answer   any
		want     string
	}{
		{"min only", &zero, nil, -1, "Enter a value of at least 0 for: Lame animals"},
		{"max only", nil, &ten, 11, "Enter a value of at most 10 for: Lame animals"},
		{"both", &zero, &ten, 11, "Enter a value between 0 and 10 for: Lame animals"},
	}
	for _, tc := range cases {
		q := Question{ID: "lame", Kind: KindNumber, Title: "Lame animals", Min: tc.min, Max: tc.max}
		for surface, err := range map[string]error{
			"inspection": Catalog{Questions: []Question{q}}.ValidateAnswers(Answers{"lame": j(tc.answer)}, MediaRefs{}),
			"load form":  Catalog{LoadQuestions: []Question{q}}.ValidateLoadAnswers(Answers{"lame": j(tc.answer)}),
		} {
			var v *ValidationError
			if !errors.As(err, &v) || v.Message != tc.want {
				t.Errorf("%s %s: err = %v, want %q", surface, tc.name, err, tc.want)
			}
		}
	}
}
