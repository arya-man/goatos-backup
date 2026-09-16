package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/domain"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/domain/sopseed"
)

func seededFollowUpDSL(t *testing.T, code string) map[string]any {
	t.Helper()
	raw, err := sopseed.FollowUp(code)
	if err != nil {
		t.Fatal(err)
	}
	var followUp map[string]any
	if err := json.Unmarshal(raw, &followUp); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"follow_up": followUp}
}

func followUpReport(t *testing.T, code string, formDSL map[string]any) domain.ValidationReport {
	t.Helper()
	report := domain.ValidationReport{Valid: true}
	(&Service{}).validateFollowUpContract(context.Background(), &report, "tenant", code, formDSL)
	return report
}

func steps(t *testing.T, formDSL map[string]any, track int) []any {
	t.Helper()
	return formDSL["follow_up"].(map[string]any)["tracks"].([]any)[track].(map[string]any)["steps"].([]any)
}

func TestSeededFollowUpDocumentsStillSave(t *testing.T) {
	for _, code := range []string{tasksdomain.SOPCodeBirth, tasksdomain.SOPCodeDeath, tasksdomain.SOPCodeReconcile, tasksdomain.SOPCodeShifting} {
		if r := followUpReport(t, code, seededFollowUpDSL(t, code)); !r.Valid {
			t.Fatalf("%s seeded document refused: %+v", code, r.Errors)
		}
	}
}

// A misspelt key ("vidoe") was dropped silently by encoding/json and published a step with NO
// proof. Every follow_up level -- the section, a track, a step, its proof and its schedule -- now
// refuses a key it does not define, by path.
func TestFollowUpUnknownKeysAreRefusedByPath(t *testing.T) {
	dsl := seededFollowUpDSL(t, tasksdomain.SOPCodeDeath)
	followUp := dsl["follow_up"].(map[string]any)
	followUp["colour"] = "red"
	track := followUp["tracks"].([]any)[0].(map[string]any)
	track["owner"] = "x"
	step := steps(t, dsl, 0)[0].(map[string]any)
	step["proof"] = map[string]any{"vidoe": 1}
	step["schedule"] = map[string]any{"kind": "immediately", "offset_minute": 5}
	step["titel"] = "Death video"
	report := followUpReport(t, tasksdomain.SOPCodeDeath, dsl)
	if report.Valid {
		t.Fatal("a follow_up with unknown keys must not be valid")
	}
	got := map[string]bool{}
	for _, e := range report.Errors {
		got[e.Field] = true
	}
	for _, want := range []string{
		"form_dsl.follow_up.colour",
		"form_dsl.follow_up.tracks.0.owner",
		"form_dsl.follow_up.tracks.0.steps.0.titel",
		"form_dsl.follow_up.tracks.0.steps.0.proof.vidoe",
		"form_dsl.follow_up.tracks.0.steps.0.schedule.offset_minute",
	} {
		if !got[want] {
			t.Errorf("unknown key %s not refused by path; errors=%+v", want, report.Errors)
		}
	}
}
