package http

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

// THE THREE UPLOAD FORMATS ANSWER DIFFERENT JOBS, and JSON is the document itself rather than a
// third spelling of the sheet: a vet edits a workbook, a script writes a CSV, and a SYSTEM emits
// the structure it already holds. Flattening that into rows only to rebuild it is loss for no one.
func TestARegisterRoundTripsThroughJSON(t *testing.T) {
	doc := &diagnosis.AuthoredRegister{
		RegisterVersion: "kid-milk-9",
		AppliesClass:    []string{"kid_milk"},
		Questions: []diagnosis.Question{
			{ID: "temp", Kind: "number", Title: "Temperature", Section: "Vitals", Unit: "degF"},
			{
				ID: "udder", Kind: "choice", Title: "Udder", Section: "Udder", OnlyIfSex: "F",
				Options: []diagnosis.Option{
					{Value: "normal", Label: "Normal"},
					{Value: "hard", Label: "Hard", Emits: []string{"udder:hard"}},
				},
			},
		},
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(doc); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := readRegisterJSON(&body)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(got.Questions) != 2 {
		t.Fatalf("questions = %d, want both", len(got.Questions))
	}
	// The nesting the sheet grammar exists to rebuild survives untouched.
	if got.Questions[1].OnlyIfSex != "F" || len(got.Questions[1].Options) != 2 ||
		got.Questions[1].Options[1].Emits[0] != "udder:hard" {
		t.Fatalf("the question lost its gate or its answers: %+v", got.Questions[1])
	}
	if got.Questions[0].Section != "Vitals" {
		t.Fatalf("a question lost the PAGE it belongs on: %+v", got.Questions[0])
	}
}

// A MISSPELLED KEY IS NAMED, not dropped.
//
// encoding/json ignores what it does not recognise, so a typo would upload a register missing a
// whole section of rules and report success -- the silent-loader defect this repo has a standing
// rule about. Uploading half a rulebook and being told it worked is worse than being refused.
func TestAMisspelledKeyIsRefusedRatherThanDropped(t *testing.T) {
	_, err := readRegisterJSON(strings.NewReader(
		`{"register_version":"v1","questions":[{"id":"temp","kind":"number","title":"Temperature"}],"quetions":[]}`))
	if err == nil {
		t.Fatal("a misspelled key uploaded silently; it must be named")
	}
	if !strings.Contains(err.Error(), "quetions") {
		t.Fatalf("refusal = %q, want it to name the key the author mistyped", err)
	}
}

// An empty document is refused rather than saved as a register that asks nothing.
func TestAJSONWithNoQuestionsIsRefused(t *testing.T) {
	if _, err := readRegisterJSON(strings.NewReader(`{"register_version":"v1"}`)); err == nil {
		t.Fatal("a register with no questions was accepted")
	}
}
