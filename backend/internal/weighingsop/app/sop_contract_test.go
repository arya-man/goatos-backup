package app

import (
	"encoding/json"
	"strings"
	"testing"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// THE WEIGH CAPTURES ARE AUTHORED (2026-09-16): the save-time contract refuses a per-animal
// section with no compulsory capture, a whole-pen section over its ceilings, and an unknown key
// inside a slot -- each by path -- while the seed (no slot lists) still saves.
func TestWeighingSOPContractRefusesAnimalWithoutCompulsoryCapture(t *testing.T) {
	var seed map[string]any
	if err := json.Unmarshal(domain.SeededWeighingSOPJSON(), &seed); err != nil {
		t.Fatal(err)
	}
	report := &sopdomain.ValidationReport{Valid: true}
	WeighingSOPContract(domain.SOPCodeWeighingSession, map[string]any{"weighing": seed}, report)
	if !report.Valid {
		t.Fatalf("seed must save: %+v", report.Errors)
	}

	capture := seed["capture"].(map[string]any)
	capture["individual"] = map[string]any{"video_required": true, "proofs": []any{map[string]any{"key": "scale_photo", "title": "Scale display", "kind": "photo", "required": false}}, "questions": []any{}}
	capture["lump_sum"] = map[string]any{"video_min": 1, "video_max": 5, "proofs": []any{
		map[string]any{"key": "pen_video", "title": "Weighing video", "kind": "video", "min": 1, "max": 5},
		map[string]any{"key": "photo", "title": "Photo", "kind": "photo", "min": 0, "max": 5, "count": 3},
		map[string]any{"key": "gate", "title": "Gate", "kind": "either", "min": 3, "max": 2},
	}, "questions": []any{}}
	report = &sopdomain.ValidationReport{Valid: true}
	WeighingSOPContract(domain.SOPCodeWeighingSession, map[string]any{"weighing": seed}, report)
	if report.Valid {
		t.Fatal("must refuse")
	}
	joined := ""
	for _, e := range report.Errors {
		joined += e.Field + "\n"
	}
	for _, want := range []string{
		"form_dsl.weighing.capture.individual.proofs: at least one compulsory capture per animal",
		"form_dsl.weighing.capture.lump_sum.proofs: at most 10 captures per pen in total",
		"form_dsl.weighing.capture.lump_sum.proofs.2.min: must not exceed max",
		"form_dsl.weighing.capture.lump_sum.proofs.1.count: not a field of this document",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing refusal %q in:\n%s", want, joined)
		}
	}
}
