package domain

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// THE TOXIN PROCEDURE IS AUTHORED (maintainer decision 2026-09-20: procurement end to end, with
// "steps authored, the toxin engine keeps state"). How many steps the aflatoxin test has, what
// each one tells the tester to do, whether it is filmed or photographed, how long the extract
// sits and which step each wait gates were SEVEN GO CONSTANTS in task.go. A kit change, a farm
// that centrifuges, or one word of a wrong instruction meant a backend release.
//
// They are now `form_dsl.toxin` of the published `procurement.toxin_test` SOP, authored on
// Procurement > Procurement SOP and served to the phone from the version the ROUND started on.
//
// WHAT IS NOT AUTHORED, deliberately: the round's state machine, the retest minting, the
// CEO/CXO-only verdict, the reading vocabulary (Negative / Positive / Invalid) and the
// server-clock enforcement of every gate. The document says what the procedure IS; the engine
// still decides what happens when a strip comes back void, and no published version can change
// that. This is the maintainer's choice of the two ways to make toxin SOP-driven, and it is why
// a medically-gated flow could be opened up at all.
const (
	// SOPCodeToxinTest is the authored procedure's SOP code.
	SOPCodeToxinTest = "procurement.toxin_test"
	// ToxinSchemaVersion tags every toxin document.
	ToxinSchemaVersion = "goatos.sop-toxin.v1"

	maxProcedureSteps    = 20
	maxProcedureGateMins = 24 * 60
)

//go:embed toxinseed/toxin_test.json
var seededToxinJSON []byte

// SeededToxinJSON is the day-one document, embedded verbatim in the migration that publishes it
// as v1 (pinned by TestMigrationEmbedsTheSeededProcedure).
func SeededToxinJSON() []byte { return append([]byte(nil), seededToxinJSON...) }

// ToxinDSL is form_dsl.toxin: the ordered steps of the procedure.
type ToxinDSL struct {
	SchemaVersion string         `json:"schema_version"`
	Steps         []ToxinDSLStep `json:"steps"`
}

// ToxinDSLStep is one authored step. It is the StepSpec the engine already ran, in wire form.
type ToxinDSLStep struct {
	No            int    `json:"no"`
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	Instruction   string `json:"instruction"`
	WaitMinutes   int    `json:"wait_minutes,omitempty"`
	GateAfterStep int    `json:"gate_after_step,omitempty"`
	GateMinutes   int    `json:"gate_minutes,omitempty"`
}

// Procedure is a compiled, versioned toxin procedure: what a round runs, start to finish. The
// zero value is NOT usable -- every caller resolves the version the round was started on, so a
// procedure published mid-round never moves the steps under the tester's feet.
type Procedure struct {
	Version int
	Steps   []StepSpec
}

// ErrToxinMissing / ErrToxinInvalid name a document the engine cannot run.
var (
	ErrToxinMissing = errors.New("toxin: SOP version has no toxin section")
	ErrToxinInvalid = errors.New("toxin: toxin procedure is invalid")
)

// ParseToxin extracts and type-checks form_dsl.toxin.
func ParseToxin(formDSL map[string]any) (ToxinDSL, error) {
	raw, ok := formDSL["toxin"]
	if !ok || raw == nil {
		return ToxinDSL{}, ErrToxinMissing
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return ToxinDSL{}, fmt.Errorf("%w: %v", ErrToxinInvalid, err)
	}
	var out ToxinDSL
	dec := json.NewDecoder(strings.NewReader(string(encoded)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return ToxinDSL{}, fmt.Errorf("%w: %v", ErrToxinInvalid, err)
	}
	if out.SchemaVersion != ToxinSchemaVersion {
		return ToxinDSL{}, fmt.Errorf("%w: schema_version %q", ErrToxinInvalid, out.SchemaVersion)
	}
	return out, nil
}

// SeededToxinDSL parses the embedded day-one document.
func SeededToxinDSL() ToxinDSL {
	dsl, err := ParseToxin(map[string]any{"toxin": json.RawMessage(seededToxinJSON)})
	if err != nil {
		panic("toxin: seeded procedure does not parse: " + err.Error())
	}
	return dsl
}

// SeededProcedure is the procedure a tenant runs before anything is authored, and the fallback a
// version the library no longer carries resolves to. It compiles to EXACTLY Steps() -- pinned by
// TestSeededProcedureCompilesToTheLegacySteps, which is what makes the authored path day-one
// identical to the seven constants it replaced.
func SeededProcedure() Procedure { return CompileProcedure(SeededToxinDSL(), 1) }

// CompileProcedure turns an authored document into the procedure the engine runs.
func CompileProcedure(dsl ToxinDSL, version int) Procedure {
	out := Procedure{Version: version, Steps: make([]StepSpec, 0, len(dsl.Steps))}
	for _, s := range dsl.Steps {
		out.Steps = append(out.Steps, StepSpec{
			No: s.No, Kind: s.Kind, Title: s.Title, Instruction: s.Instruction,
			GateAfterStep: s.GateAfterStep, GateMinutes: s.GateMinutes, WaitMinutes: s.WaitMinutes,
		})
	}
	return out
}

// ValidateToxin names every problem by path. Publishing runs it, so a document the engine could
// not run never becomes the published version.
//
// The RULES ARE THE ENGINE'S INVARIANTS, not taste: the steps are numbered 1..N in order because
// the phone, the completions table and every gate key on the number; exactly one reading step
// exists and it is LAST because the reading is what ends the round; a gate may only point
// BACKWARDS at a working step, or a round could never open it; and a wait row must carry a
// duration, or it is a row that tells the tester nothing.
func ValidateToxin(dsl ToxinDSL) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if len(dsl.Steps) == 0 {
		add("toxin.steps: at least one step is required")
		return problems
	}
	if len(dsl.Steps) > maxProcedureSteps {
		add("toxin.steps: at most %d steps", maxProcedureSteps)
	}
	working := map[int]bool{}
	readings := 0
	for i, s := range dsl.Steps {
		sp := fmt.Sprintf("toxin.steps.%d", i)
		if s.No != i+1 {
			add("%s.no: steps must be numbered 1..N in order (found %d at position %d)", sp, s.No, i+1)
		}
		if strings.TrimSpace(s.Title) == "" {
			add("%s.title: required", sp)
		}
		if strings.TrimSpace(s.Instruction) == "" {
			add("%s.instruction: required -- the tester is told what to do, never left to guess", sp)
		}
		switch s.Kind {
		case StepKindVideo:
			working[s.No] = true
		case StepKindPhotoReading:
			working[s.No] = true
			readings++
			if i != len(dsl.Steps)-1 {
				add("%s.kind: the reading step must be the LAST step -- it is what ends the round", sp)
			}
		case StepKindWait:
			if s.WaitMinutes <= 0 {
				add("%s.wait_minutes: a waiting step needs a duration", sp)
			}
			if s.GateAfterStep != 0 || s.GateMinutes != 0 {
				add("%s.gate_after_step: a waiting step is not gated itself -- gate the step that follows it", sp)
			}
		default:
			add("%s.kind: %q is not a toxin step kind (video, wait, photo_reading)", sp, s.Kind)
		}
		if s.Kind != StepKindWait && s.WaitMinutes != 0 {
			add("%s.wait_minutes: only a waiting step carries a duration", sp)
		}
		if (s.GateAfterStep == 0) != (s.GateMinutes == 0) {
			add("%s.gate_after_step: a gate needs both the step it follows and its minutes", sp)
		}
		if s.GateAfterStep != 0 {
			if s.GateAfterStep >= s.No {
				add("%s.gate_after_step: %d must be an EARLIER step than %d", sp, s.GateAfterStep, s.No)
			} else if !working[s.GateAfterStep] {
				add("%s.gate_after_step: %d is not a step that records a completion, so its gate could never open", sp, s.GateAfterStep)
			}
			if s.GateMinutes < 0 || s.GateMinutes > maxProcedureGateMins {
				add("%s.gate_minutes: must be between 0 and %d", sp, maxProcedureGateMins)
			}
		}
	}
	if readings != 1 {
		add("toxin.steps: exactly one reading step is required (found %d)", readings)
	}
	if len(working) < 2 {
		add("toxin.steps: a procedure needs at least one step before the reading")
	}
	return problems
}

// ---- the engine's reads, now per procedure ----

// StepSpecFor returns the spec for a step number in this procedure.
func (p Procedure) StepSpecFor(no int) (StepSpec, bool) {
	for _, s := range p.Steps {
		if s.No == no {
			return s, true
		}
	}
	return StepSpec{}, false
}

// WorkingSteps are the steps that record a completion (everything but the waiting rows).
func (p Procedure) WorkingSteps() []int {
	out := make([]int, 0, len(p.Steps))
	for _, s := range p.Steps {
		if s.Kind != StepKindWait {
			out = append(out, s.No)
		}
	}
	return out
}

// FinalStepNo is the reading step: completing it is the submit. Zero for a document with none,
// which the validator refuses to publish.
func (p Procedure) FinalStepNo() int {
	for _, s := range p.Steps {
		if s.Kind == StepKindPhotoReading {
			return s.No
		}
	}
	return 0
}

// GatedByWait returns the step a waiting row is really about: the next working step that carries
// a gate. The wait row has no state of its own -- it mirrors what it is waiting FOR -- and this
// replaces the hard-coded "step 5" the payload composer used to assume.
func (p Procedure) GatedByWait(waitStepNo int) (StepSpec, bool) {
	for _, s := range p.Steps {
		if s.No <= waitStepNo || s.Kind == StepKindWait {
			continue
		}
		if s.GateAfterStep != 0 {
			return s, true
		}
		return StepSpec{}, false
	}
	return StepSpec{}, false
}

// NextStepNo is the first working step with no completion, or 0 when they are all done.
func (p Procedure) NextStepNo(completions []StepCompletion) int {
	for _, no := range p.WorkingSteps() {
		if _, done := completionFor(completions, no); !done {
			return no
		}
	}
	return 0
}

// CheckStepCompletable decides whether stepNo may be completed NOW, against this procedure.
func (p Procedure) CheckStepCompletable(taskStatus string, stepNo int, completions []StepCompletion, now time.Time) (time.Time, error) {
	if taskStatus != StatusInProgress {
		return time.Time{}, ErrTaskNotOpen
	}
	spec, ok := p.StepSpecFor(stepNo)
	if !ok {
		return time.Time{}, ErrUnknownStep
	}
	if spec.Kind == StepKindWait {
		return time.Time{}, ErrStepNotCompletable
	}
	if _, done := completionFor(completions, stepNo); done {
		return time.Time{}, ErrStepAlreadyDone
	}
	for _, no := range p.WorkingSteps() {
		if no >= stepNo {
			break
		}
		if _, done := completionFor(completions, no); !done {
			return time.Time{}, ErrStepOutOfOrder
		}
	}
	if opensAt := GateOpensAt(spec, completions); !opensAt.IsZero() && now.Before(opensAt) {
		return opensAt, ErrWaitNotElapsed
	}
	return time.Time{}, nil
}

// StatusChip is the backend-owned chip for a round on this procedure.
func (p Procedure) StatusChip(t Task, completions []StepCompletion, now time.Time) string {
	if t.Status != StatusInProgress {
		return StatusChip(t, completions, now)
	}
	next := p.NextStepNo(completions)
	if next == 0 {
		return "Test due"
	}
	spec, _ := p.StepSpecFor(next)
	if opensAt := GateOpensAt(spec, completions); !opensAt.IsZero() && now.Before(opensAt) {
		remaining := opensAt.Sub(now).Round(time.Minute)
		minutes := int(remaining / time.Minute)
		if minutes < 1 {
			minutes = 1
		}
		return fmt.Sprintf("Waiting — next step in %d min", minutes)
	}
	return fmt.Sprintf("Step %d of %d", len(completions)+1, len(p.WorkingSteps()))
}
