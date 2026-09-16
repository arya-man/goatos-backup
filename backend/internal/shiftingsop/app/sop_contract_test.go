package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

func seededFormDSL(t *testing.T) map[string]any {
	t.Helper()
	var section map[string]any
	if err := json.Unmarshal(domain.SeededShiftingSOPJSON(), &section); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"shifting": section}
}

// The `shifting` section is REQUIRED on the shifting code and ignored on every other code.
func TestShiftingSOPContractRequiresSection(t *testing.T) {
	report := sopdomain.ValidationReport{Valid: true}
	ShiftingSOPContract(domain.SOPCodeShifting, map[string]any{"follow_up": map[string]any{}}, &report)
	if report.Valid || len(report.Errors) == 0 || report.Errors[0].Field != "form_dsl.shifting" {
		t.Fatalf("missing section accepted: %+v", report)
	}
	other := sopdomain.ValidationReport{Valid: true}
	ShiftingSOPContract("counts.birth", map[string]any{}, &other)
	if !other.Valid {
		t.Fatalf("another code was judged by the shifting contract: %+v", other)
	}
	ok := sopdomain.ValidationReport{Valid: true}
	ShiftingSOPContract(domain.SOPCodeShifting, seededFormDSL(t), &ok)
	if !ok.Valid {
		t.Fatalf("seed refused: %+v", ok.Errors)
	}
	// A problem names its path; an unknown key is refused at save.
	bad := seededFormDSL(t)
	bad["shifting"].(map[string]any)["completion"].(map[string]any)["proofs"] = []any{}
	bad["shifting"].(map[string]any)["completion"].(map[string]any)["proofz"] = []any{}
	badReport := sopdomain.ValidationReport{Valid: true}
	ShiftingSOPContract(domain.SOPCodeShifting, bad, &badReport)
	fields := []string{}
	for _, e := range badReport.Errors {
		fields = append(fields, e.Field)
	}
	joined := strings.Join(fields, "|")
	if badReport.Valid || !strings.Contains(joined, "form_dsl.shifting.completion.proofs") || !strings.Contains(joined, "form_dsl.shifting.completion.proofz") {
		t.Fatalf("problems not named by path: %v", fields)
	}
}
