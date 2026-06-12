package csvutil

import "testing"

func TestSafeCellEscapesFormulaPrefixesAfterWhitespace(t *testing.T) {
	cases := map[string]string{
		"\t=1+1":      "'=1+1",
		"\r+SUM(A:A)": "'+SUM(A:A)",
		"\n-42":       "'-42",
		" @cmd":       "'@cmd",
		"plain":       "plain",
	}
	for input, want := range cases {
		if got := SafeCell(input); got != want {
			t.Fatalf("SafeCell(%q)=%q, want %q", input, got, want)
		}
	}
}
