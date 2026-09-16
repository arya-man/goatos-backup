package app

import (
	"encoding/json"
	"strings"
	"testing"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

func doc(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

const broken = `{"capture_card":{"schema_version":"goatos.sop-capture.v1","proofs":[{"key":"Bad","title":"","kind":"gif"}]}}`

func TestCaptureCardContractRefusesABrokenCardOnCountsCodes(t *testing.T) {
	for _, code := range []string{"counts.birth", "counts.death"} {
		report := sopdomain.ValidationReport{Valid: true}
		CaptureCardContract(code, doc(t, broken), &report)
		if report.Valid || len(report.Errors) < 3 {
			t.Fatalf("%s: broken card must be refused with every problem named, got %+v", code, report)
		}
		for _, e := range report.Errors {
			if !strings.HasPrefix(e.Field, "form_dsl.capture_card") {
				t.Fatalf("%s: problem must name its path, got %q", code, e.Field)
			}
		}
	}
	// A valid card and an absent card both pass.
	for _, raw := range []string{`{"follow_up":{}}`, `{"capture_card":{"schema_version":"goatos.sop-capture.v1","questions":[{"id":"q","kind":"text","title":"Notes","required":false}]}}`} {
		report := sopdomain.ValidationReport{Valid: true}
		CaptureCardContract("counts.birth", doc(t, raw), &report)
		if !report.Valid {
			t.Fatalf("%s must pass: %+v", raw, report.Errors)
		}
	}
}

func TestCaptureCardContractIgnoresOtherCodes(t *testing.T) {
	for _, code := range []string{"shifting", "counts.reconcile", "weighing.session", "feed.packing"} {
		report := sopdomain.ValidationReport{Valid: true}
		CaptureCardContract(code, doc(t, broken), &report)
		if !report.Valid || len(report.Errors) != 0 {
			t.Fatalf("%s: the capture card contract must not judge other codes: %+v", code, report)
		}
	}
}

// TestCaptureCardContractRefusesUnknownKeysByPath: a misspelt key ("requird", "only_iff") would
// otherwise be dropped silently and publish a card that means something the author did not write
// -- a compulsory slot read as optional, a conditional question shown unconditionally.
func TestCaptureCardContractRefusesUnknownKeysByPath(t *testing.T) {
	raw := `{"capture_card":{"schema_version":"goatos.sop-capture.v1","colour":"red",
	  "proofs":[{"key":"a","title":"A","kind":"video","requird":false}],
	  "questions":[{"id":"q","kind":"choice","title":"Q","required":true,"options":[{"value":"x","label":"X","tone":1}]},
	               {"id":"r","kind":"text","title":"R","required":true,"only_if":{"question_id":"q","value":"x","op":"eq"}}]}}`
	report := sopdomain.ValidationReport{Valid: true}
	CaptureCardContract("counts.birth", doc(t, raw), &report)
	want := []string{"form_dsl.capture_card.colour", "form_dsl.capture_card.proofs.0.requird", "form_dsl.capture_card.questions.0.options.0.tone", "form_dsl.capture_card.questions.1.only_if.op"}
	got := map[string]bool{}
	for _, e := range report.Errors {
		got[e.Field] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Fatalf("unknown key %s not refused by path; errors=%+v", w, report.Errors)
		}
	}
	if report.Valid {
		t.Fatal("a card with unknown keys must not be valid")
	}
}
