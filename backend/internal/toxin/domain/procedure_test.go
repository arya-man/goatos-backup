package domain

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestSeededProcedureCompilesToTheLegacySteps is the whole day-one claim of authoring the
// procedure: the seeded document compiles to EXACTLY the seven Go constants the engine ran
// before, step for step, gate for gate. Steps() stays in task.go as that oracle; if the two ever
// diverge, a farm that has authored nothing would silently start running a different test.
func TestSeededProcedureCompilesToTheLegacySteps(t *testing.T) {
	got := SeededProcedure()
	if got.Version != 1 {
		t.Fatalf("seeded version = %d", got.Version)
	}
	if !reflect.DeepEqual(got.Steps, Steps()) {
		t.Fatalf("seeded procedure != legacy Steps():\n got %+v\nwant %+v", got.Steps, Steps())
	}
	if problems := ValidateToxin(SeededToxinDSL()); len(problems) > 0 {
		t.Fatalf("seeded procedure invalid: %v", problems)
	}
	if got.FinalStepNo() != FinalStepNo {
		t.Fatalf("final step = %d, want %d", got.FinalStepNo(), FinalStepNo)
	}
	if !reflect.DeepEqual(got.WorkingSteps(), WorkingSteps()) {
		t.Fatalf("working steps = %v, want %v", got.WorkingSteps(), WorkingSteps())
	}
	// The waiting row mirrors the step it waits FOR, resolved from the document rather than the
	// hard-coded "step 5" the payload composer used to assume.
	gated, ok := got.GatedByWait(4)
	if !ok || gated.No != 5 || gated.GateAfterStep != 3 || gated.GateMinutes != 60 {
		t.Fatalf("wait row gates %+v (ok=%v)", gated, ok)
	}
}

// TestValidateToxinRefusesWhatTheEngineCannotRun covers each invariant the engine depends on.
// These are not taste: every one of them is a way to publish a procedure that would strand a
// round mid-test.
func TestValidateToxinRefusesWhatTheEngineCannotRun(t *testing.T) {
	cases := map[string]struct {
		mutate func(*ToxinDSL)
		want   string
	}{
		"renumbered steps":        {func(d *ToxinDSL) { d.Steps[2].No = 9 }, "numbered 1..N in order"},
		"no reading step":         {func(d *ToxinDSL) { d.Steps[6].Kind = StepKindVideo }, "exactly one reading step"},
		"two reading steps":       {func(d *ToxinDSL) { d.Steps[5].Kind = StepKindPhotoReading }, "exactly one reading step"},
		"reading not last":        {func(d *ToxinDSL) { d.Steps = d.Steps[:6]; d.Steps[4].Kind = StepKindPhotoReading }, "must be the LAST step"},
		"gate points forward":     {func(d *ToxinDSL) { d.Steps[4].GateAfterStep = 6 }, "must be an EARLIER step"},
		"gate points at the wait": {func(d *ToxinDSL) { d.Steps[4].GateAfterStep = 4 }, "could never open"},
		"half a gate":             {func(d *ToxinDSL) { d.Steps[4].GateMinutes = 0 }, "needs both the step it follows"},
		"wait with no duration":   {func(d *ToxinDSL) { d.Steps[3].WaitMinutes = 0 }, "needs a duration"},
		"blank instruction":       {func(d *ToxinDSL) { d.Steps[0].Instruction = "" }, "never left to guess"},
		"unknown kind":            {func(d *ToxinDSL) { d.Steps[0].Kind = "centrifuge" }, "not a toxin step kind"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dsl := SeededToxinDSL()
			dsl.Steps = append([]ToxinDSLStep(nil), dsl.Steps...)
			tc.mutate(&dsl)
			problems := ValidateToxin(dsl)
			found := false
			for _, p := range problems {
				if contains(p, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("problems = %v, want one containing %q", problems, tc.want)
			}
		})
	}
}

// TestAShorterProcedureRunsEndToEnd proves the point of authoring at all: a farm that drops the
// settling hour and one video runs a 4-step test, and every engine read follows the document
// rather than the old constants.
func TestAShorterProcedureRunsEndToEnd(t *testing.T) {
	dsl := ToxinDSL{SchemaVersion: ToxinSchemaVersion, Steps: []ToxinDSLStep{
		{No: 1, Kind: StepKindVideo, Title: "Take the sample", Instruction: "On camera."},
		{No: 2, Kind: StepKindVideo, Title: "Mix", Instruction: "On camera."},
		{No: 3, Kind: StepKindVideo, Title: "Fill the well", Instruction: "On camera.", GateAfterStep: 2, GateMinutes: 5},
		{No: 4, Kind: StepKindPhotoReading, Title: "Read the strip", Instruction: "Photograph it."},
	}}
	if problems := ValidateToxin(dsl); len(problems) > 0 {
		t.Fatalf("a four-step procedure must be publishable: %v", problems)
	}
	proc := CompileProcedure(dsl, 3)
	if proc.FinalStepNo() != 4 {
		t.Fatalf("final step = %d", proc.FinalStepNo())
	}
	if got := proc.WorkingSteps(); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatalf("working steps = %v", got)
	}
	if got := proc.NextStepNo(nil); got != 1 {
		t.Fatalf("next step = %d", got)
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// TestMigrationEmbedsTheSeededProcedure pins migration 000375 to toxinseed/toxin_test.json byte
// for byte: the golden test above proves the SEED equals the legacy steps, and this proves the
// database is seeded from that same seed. Either one alone proves nothing about what a tenant
// actually runs.
func TestMigrationEmbedsTheSeededProcedure(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", "000375_toxin_procedure_sop.sql"))
	if err != nil {
		t.Fatal(err)
	}
	seed := strings.TrimSpace(string(SeededToxinJSON()))
	if !strings.Contains(string(raw), "$seed$"+seed+"$seed$") {
		t.Fatal("migration 000375 does not embed toxinseed/toxin_test.json verbatim; regenerate the SQL")
	}
	if !strings.Contains(string(raw), "'procurement.toxin_test'") {
		t.Fatal("migration 000375 does not seed the procurement.toxin_test SOP code")
	}
	// The column that makes a round keep its own procedure is part of the same migration: without
	// it, publishing a new version would move the steps under every test in progress.
	if !strings.Contains(string(raw), "sop_version") {
		t.Fatal("migration 000375 does not add the round's sop_version column")
	}
}
