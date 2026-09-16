package app

import (
	"context"
	"encoding/json"
	"strings"
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

// The birth kid track's tag and pen steps drive server behaviour (the RFID gate and the kid's
// placement when its pen is unresolved). The editor keeps their key and type fixed; deleting the
// step must be refused at save, naming it.
func TestBirthEngineHookStepsCannotBeRemoved(t *testing.T) {
	for _, tc := range []struct{ key, title string }{{"tag_the_kid", "Tag"}, {"record_shed", "shed"}} {
		dsl := seededFollowUpDSL(t, tasksdomain.SOPCodeBirth)
		track := dsl["follow_up"].(map[string]any)["tracks"].([]any)[0].(map[string]any)
		var kept []any
		for _, s := range track["steps"].([]any) {
			if s.(map[string]any)["key"] != tc.key {
				kept = append(kept, s)
			}
		}
		track["steps"] = kept
		report := followUpReport(t, tasksdomain.SOPCodeBirth, dsl)
		if report.Valid {
			t.Fatalf("removing %s was accepted", tc.key)
		}
		found := false
		for _, e := range report.Errors {
			if strings.Contains(e.Message, tc.key) {
				found = true
			}
		}
		if !found {
			t.Fatalf("removing %s: no error names the step: %+v", tc.key, report.Errors)
		}
	}
}

// Steps whose hook only labels or reports (weight, colostrum feeds, death clips, the reconcile
// return) may be removed: the SOP decides what the operator records.
func TestReportingHookStepsMayBeRemoved(t *testing.T) {
	dsl := seededFollowUpDSL(t, tasksdomain.SOPCodeBirth)
	track := dsl["follow_up"].(map[string]any)["tracks"].([]any)[0].(map[string]any)
	var kept []any
	for _, s := range track["steps"].([]any) {
		switch s.(map[string]any)["key"] {
		case "take_weight", "first_colostrum", "colostrum_series":
			continue
		}
		kept = append(kept, s)
	}
	track["steps"] = kept
	if r := followUpReport(t, tasksdomain.SOPCodeBirth, dsl); !r.Valid {
		t.Fatalf("removing reporting steps refused: %+v", r.Errors)
	}
}
