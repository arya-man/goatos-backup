package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
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

// The seeded general SOP (migration 000354) ships `fields: []` -- a general SOP has no capture
// form -- and the web's Operator-steps editor keeps the capture form untouched when it saves a
// new version. ValidateFormDSL demanded a field and made the seeded document impossible to edit
// or re-publish (PR 308 review, P1). The seeded shape must save through the full CreateVersion
// path; a MODULE document with no field is still refused.
func TestSeededGeneralSOPWithNoFieldsStillSavesAndPublishes(t *testing.T) {
	repo := newFakeRepo()
	repo.sop.Code = sopseed.SOPCodeGateVisitorCheck
	repo.sop.Kind = domain.SOPKindGeneral
	service := NewService(repo)
	dsl := seededFollowUpDSL(t, sopseed.SOPCodeGateVisitorCheck)
	dsl["schema_version"] = "goatos.sop-form.v1"
	dsl["sop_code"] = sopseed.SOPCodeGateVisitorCheck
	dsl["title"] = "Gate visitor check"
	dsl["fields"] = []any{}
	policy := map[string]any{"subject_scope": "task", "types": []any{"video", "photo"}, "required": false, "minimum_count": float64(0), "verify_before_apply": false, "approval_before_apply": false}
	created, err := service.CreateVersion(context.Background(), ports.CreateVersionCommand{TenantID: testTenantID, ActorID: testActorID, SOPID: testSOPID, Body: domain.CreateSOPVersionRequest{VersionLabel: "edited on the web", FormDSL: dsl, ProofPolicy: policy}}, "trace")
	if err != nil {
		t.Fatalf("the seeded general document must save: %#v", err)
	}
	if r := ValidateFormDSL(dsl, policy); !r.Valid {
		t.Fatalf("the seeded general document must validate: %+v", r.Errors)
	}
	if _, err := service.PublishVersion(context.Background(), ports.VersionCommand{TenantID: testTenantID, ActorID: testActorID, SOPID: testSOPID, SOPVersionID: created.Version.SOPVersionID, RowVersion: 1}, "trace"); err != nil {
		t.Fatalf("the seeded general document must publish: %#v", err)
	}
	// A module document without a capture field is still refused: the exemption reads the
	// general track, not the SOP kind alone.
	moduleDSL := seededFollowUpDSL(t, tasksdomain.SOPCodeBirth)
	moduleDSL["schema_version"] = "goatos.sop-form.v1"
	moduleDSL["fields"] = []any{}
	if r := ValidateFormDSL(moduleDSL, policy); r.Valid {
		t.Fatal("a module document with no field must still be refused")
	}
}

type stubDesignations struct{ codes []string }

func (s stubDesignations) ListActiveDesignationCodes(context.Context) ([]string, error) {
	return s.codes, nil
}

// SALES SOP (2026-09-19): the seeded sale document saves; its tag step (the engine hook that
// ties the tagged animals to the sale) cannot be removed; and a step owner must be an active
// designation from the catalog -- free text would lock a step for everyone.
func TestSalesSOPContract(t *testing.T) {
	svc := (&Service{}).WithDesignationSource(stubDesignations{codes: []string{"park_head", "procurement_director", "operator"}})
	report := func(formDSL map[string]any) domain.ValidationReport {
		r := domain.ValidationReport{Valid: true}
		svc.validateFollowUpContract(context.Background(), &r, "tenant", tasksdomain.SOPCodeSalesDeal, formDSL)
		return r
	}
	if r := report(seededFollowUpDSL(t, tasksdomain.SOPCodeSalesDeal)); !r.Valid {
		t.Fatalf("seeded sale document refused: %+v", r.Errors)
	}
	if !FollowUpRequired(tasksdomain.SOPCodeSalesDeal) {
		t.Fatal("sales.deal must require a follow_up section")
	}

	// The tag step is engine-owned: removing it is refused with its title.
	dsl := seededFollowUpDSL(t, tasksdomain.SOPCodeSalesDeal)
	track := dsl["follow_up"].(map[string]any)["tracks"].([]any)[0].(map[string]any)
	track["steps"] = steps(t, dsl, 0)[1:]
	r := report(dsl)
	if r.Valid {
		t.Fatal("removing the tag-animals step must be refused")
	}
	found := false
	for _, e := range r.Errors {
		if e.Code == "engine_step_removed" && strings.Contains(e.Message, "Tag the animals sold") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected engine_step_removed naming the tag step, got %+v", r.Errors)
	}

	// An owner outside the catalog is refused by path; a catalog owner passes; blank is anyone.
	dsl = seededFollowUpDSL(t, tasksdomain.SOPCodeSalesDeal)
	step := steps(t, dsl, 0)[1].(map[string]any)
	step["owner"] = "gate_keeper"
	r = report(dsl)
	if r.Valid {
		t.Fatal("an owner outside the designation catalog must be refused")
	}
	if r.Errors[0].Field != "form_dsl.follow_up.tracks.0.steps.1.owner" || r.Errors[0].Code != "unknown_designation" {
		t.Fatalf("owner error = %+v", r.Errors[0])
	}
	step["owner"] = ""
	if r := report(dsl); !r.Valid {
		t.Fatalf("a blank owner is anyone's step, got %+v", r.Errors)
	}

	// Without a wired catalog (unit fixtures) the owner check is skipped, never a false refusal.
	dsl = seededFollowUpDSL(t, tasksdomain.SOPCodeSalesDeal)
	steps(t, dsl, 0)[1].(map[string]any)["owner"] = "gate_keeper"
	if r := followUpReport(t, tasksdomain.SOPCodeSalesDeal, dsl); !r.Valid {
		t.Fatalf("no catalog wired: owner must not be checked, got %+v", r.Errors)
	}
}
