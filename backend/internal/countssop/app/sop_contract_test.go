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
