package bootstrap

import (
	"testing"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

// TestPhoneTaskSOPContractChecksTheDocumentOnSave: an SOP version carrying a phone_task section is
// checked when SAVED (an unknown icon, no park), with the reason in farm words on the report; an
// SOP with no such section is untouched by this contract.
func TestPhoneTaskSOPContractChecksTheDocumentOnSave(t *testing.T) {
	bad := map[string]any{"phone_task": map[string]any{
		"tab": map[string]any{"icon": "rocket"}, "scope_kind": "all_pens", "cadence_kind": "daily", "review_kind": "none",
		"evidence": map[string]any{"questions": []any{}, "photo": map[string]any{"min": 0, "max": 0}, "video": map[string]any{"min": 1, "max": 1}, "presence": "off"},
		"parks":    []any{},
	}}
	report := sopdomain.ValidationReport{Valid: true}
	phoneTaskSOPContract("pc_care.wash", bad, &report)
	if report.Valid || len(report.Errors) != 1 || report.Errors[0].Code != "invalid_phone_task" || report.Errors[0].Message == "" {
		t.Fatalf("bad document report = %+v", report)
	}
	plain := sopdomain.ValidationReport{Valid: true}
	phoneTaskSOPContract("pc_care.tasks", map[string]any{"pc_care": map[string]any{}}, &plain)
	if !plain.Valid || len(plain.Errors) != 0 {
		t.Fatalf("an SOP without a phone task was judged: %+v", plain)
	}
}
