package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/domain"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// TestProcurementIntakeSOPContract is the publish-time half of PROCUREMENT IS SOP-DRIVEN END TO
// END (2026-09-20): the two purchase documents save as seeded, must keep the track the opener
// compiles from, and must keep the steps only the ENGINE can complete.
//
// Both refusals matter for the same reason: their failure is SILENT. A version with no track
// leaves every load from then on with no steps at all, and a version missing an engine step
// leaves work nothing can ever finish -- neither shows up as an error anywhere, which is exactly
// why publish is where they are caught.
func TestProcurementIntakeSOPContract(t *testing.T) {
	svc := (&Service{}).WithDesignationSource(stubDesignations{codes: []string{"park_head", "procurement_director", "ceo_internal"}})
	report := func(code string, formDSL map[string]any) domain.ValidationReport {
		r := domain.ValidationReport{Valid: true}
		svc.validateFollowUpContract(context.Background(), &r, "tenant", code, formDSL)
		return r
	}

	for _, code := range []string{tasksdomain.SOPCodeAnimalPurchaseIntake, tasksdomain.SOPCodeFeedPurchaseIntake} {
		if !FollowUpRequired(code) {
			t.Fatalf("%s must require a follow_up section", code)
		}
		if r := report(code, seededFollowUpDSL(t, code)); !r.Valid {
			t.Fatalf("seeded %s document refused: %+v", code, r.Errors)
		}
	}

	// Removing an engine step is refused BY NAME, so the author is told which one and why.
	cases := []struct {
		code      string
		dropTitle string
		wantStep  string
	}{
		{tasksdomain.SOPCodeAnimalPurchaseIntake, "Decision on every animal", "Decision on every animal"},
		{tasksdomain.SOPCodeFeedPurchaseIntake, "Mark the load as reached", "Mark the load as reached"},
		{tasksdomain.SOPCodeFeedPurchaseIntake, "Aflatoxin test signed off", "Aflatoxin test signed off"},
	}
	for _, tc := range cases {
		t.Run(tc.dropTitle, func(t *testing.T) {
			dsl := seededFollowUpDSL(t, tc.code)
			track := dsl["follow_up"].(map[string]any)["tracks"].([]any)[0].(map[string]any)
			kept := []any{}
			for _, raw := range track["steps"].([]any) {
				if raw.(map[string]any)["title"] != tc.dropTitle {
					kept = append(kept, raw)
				}
			}
			track["steps"] = kept
			r := report(tc.code, dsl)
			if r.Valid {
				t.Fatalf("removing %q must be refused", tc.dropTitle)
			}
			found := false
			for _, e := range r.Errors {
				if e.Code == "engine_step_removed" && strings.Contains(e.Message, tc.wantStep) {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected engine_step_removed naming %q, got %+v", tc.wantStep, r.Errors)
			}
		})
	}

	// A document that drops the whole track is refused too: the opener would have nothing to
	// compile and every load after it would open with no steps.
	dsl := seededFollowUpDSL(t, tasksdomain.SOPCodeAnimalPurchaseIntake)
	dsl["follow_up"].(map[string]any)["tracks"] = []any{}
	r := report(tasksdomain.SOPCodeAnimalPurchaseIntake, dsl)
	missing := false
	for _, e := range r.Errors {
		if e.Code == "missing_track" {
			missing = true
		}
	}
	if r.Valid || !missing {
		t.Fatalf("dropping the intake track must be refused with missing_track, got %+v", r.Errors)
	}
}
