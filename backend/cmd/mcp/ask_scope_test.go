package main

import (
	"strings"
	"testing"
)

// Leaders add this connector to their everyday Claude; it must not route general questions
// through the (minutes-long) analyst. The tool description and server instructions both scope it.
func TestAskGoatOSIsScopedToFarmDataQuestions(t *testing.T) {
	d := askGoatOSDescription(true)
	for _, want := range []string{"ONLY for questions about Mesha", "Do NOT call it for anything else"} {
		if !strings.Contains(d, want) {
			t.Fatalf("agent ask_goatos description must contain %q, got %q", want, d)
		}
	}
	ins, _ := initializeResult()["instructions"].(string)
	if !strings.Contains(ins, "answer normally without calling these tools") {
		t.Fatalf("initialize must carry scoping instructions, got %q", ins)
	}
}
