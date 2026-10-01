package app

import "testing"

// TestAPhoneTaskDocumentPassesTheRealVersionValidator: the document the module SOP page's phone-task
// editor saves (schema_version, fields: [], phone_task, the "no SOP-level proof" policy) must pass
// ValidateFormDSL -- the generic "at least one field is required" check runs before any module
// contract, and before phone_task was a module-owned section every save was refused.
func TestAPhoneTaskDocumentPassesTheRealVersionValidator(t *testing.T) {
	formDSL := map[string]any{
		"schema_version": "goatos.sop-form.v1",
		"fields":         []any{},
		"phone_task": map[string]any{
			"tab":          map[string]any{"icon": "fumigation", "filters": []any{"status", "pen"}},
			"instruction":  "Spray every pen.",
			"scope_kind":   "all_pens",
			"cadence_kind": "daily",
			"review_kind":  "verifier",
			"evidence":     map[string]any{"questions": []any{}, "photo": map[string]any{"min": float64(0), "max": float64(0)}, "video": map[string]any{"min": float64(2), "max": float64(2)}, "presence": "off"},
			"parks":        []any{map[string]any{"park_id": "p", "assignee_user_id": "u", "pens": []any{}}},
		},
	}
	policy := map[string]any{"subject_scope": "task", "types": []any{"video", "photo"}, "required": false, "minimum_count": float64(0), "verify_before_apply": false}
	if report := ValidateFormDSL(formDSL, policy); !report.Valid {
		t.Fatalf("the phone-task editor's document is refused: %+v", report.Errors)
	}
}
