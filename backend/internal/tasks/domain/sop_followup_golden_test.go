package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain/sopseed"
)

// legacyView is the projection of a step the phone and the engine observed BEFORE the SOP-driven
// change: everything the old code template stamped into workflow_actions. The golden test asserts
// the seeded documents reproduce it exactly, so day-one behaviour did not move when the source of
// truth moved from Go to the database.
type legacyView struct {
	Key, Section, Type, Title, Detail string
	Seq                               int
	RequiresVideo                     bool
	Options                           []string
	DueAt                             string // RFC3339 or "" for dependency-timed
}

func legacyViews(t *testing.T, tpl Template, eventAt time.Time) []legacyView {
	t.Helper()
	out := make([]legacyView, 0, len(tpl.Actions))
	for _, a := range tpl.Actions {
		due := ""
		if a.Key != ActionKeyORSWater2 && !a.Schedule.IsDependencyTimed() {
			if a.Schedule != (Schedule{}) {
				due = a.Schedule.DueAt(eventAt).UTC().Format(time.RFC3339)
			} else {
				due = eventAt.UTC().Format(time.RFC3339)
			}
		}
		out = append(out, legacyView{a.Key, a.Section, a.Type, a.Title, a.Detail, a.Seq, a.RequiresVideo, a.Options, due})
	}
	return out
}

func loadSeeded(t *testing.T, code string) FollowUpDSL {
	t.Helper()
	raw, err := sopseed.FollowUp(code)
	if err != nil {
		t.Fatal(err)
	}
	var followUp map[string]any
	if err := json.Unmarshal(raw, &followUp); err != nil {
		t.Fatal(err)
	}
	dsl, err := ParseFollowUp(map[string]any{"follow_up": followUp})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := SeededTaskTypes()
	if err != nil {
		t.Fatal(err)
	}
	if problems := ValidateFollowUp(dsl, reg); len(problems) > 0 {
		t.Fatalf("%s: seeded follow_up does not validate: %v", code, problems)
	}
	return dsl
}

// TestSeededBirthCompilesToTheLegacyTemplates covers both kid tracks (with and without the pen
// fallback) at three birth moments that exercise the colostrum series eligibility rule: before
// the first slot, mid-day (some slots gone), and after the last slot's pre-notify cutoff.
func TestSeededBirthCompilesToTheLegacyTemplates(t *testing.T) {
	dsl := loadSeeded(t, SOPCodeBirth)
	reg, _ := SeededTaskTypes()
	ist := biztime.DefaultLocation()
	moments := []time.Time{
		time.Date(2026, 9, 13, 5, 30, 0, 0, ist),
		time.Date(2026, 9, 13, 14, 50, 0, 0, ist), // 15:00 slot: 14:50 is NOT before 14:45 -> excluded
		time.Date(2026, 9, 13, 22, 40, 0, 0, ist),
	}
	for _, at := range moments {
		for _, needsPen := range []bool{false, true} {
			track, _ := dsl.Track(TemplateKeyBirthKid)
			got, err := CompileTrack(track, reg, CompileOptions{EventAt: at, NeedsShedPlacement: needsPen})
			if err != nil {
				t.Fatalf("kid @%s pen=%v: %v", at, needsPen, err)
			}
			want := TemplateBirthKidAt(at, needsPen)
			assertLegacyEqual(t, "birth_kid", want, got, at)
		}
		track, _ := dsl.Track(TemplateKeyBirthMother)
		got, err := CompileTrack(track, reg, CompileOptions{EventAt: at})
		if err != nil {
			t.Fatal(err)
		}
		assertLegacyEqual(t, "birth_mother", TemplateBirthMother(), got, at)
		// The dependency-timed ORS round is now declared in config rather than special-cased by key.
		ors2 := got.Actions[len(got.Actions)-1]
		if ors2.Key != ActionKeyORSWater2 || ors2.Schedule.AfterStepKey != ActionKeyORSWater1 || ors2.Schedule.Offset != 50*time.Minute || !ors2.HardTimeGate {
			t.Fatalf("ors_water_2 lost its dependency schedule: %+v", ors2.Schedule)
		}
	}
}

func TestSeededDeathCompilesToTheLegacyTemplate(t *testing.T) {
	dsl := loadSeeded(t, SOPCodeDeath)
	reg, _ := SeededTaskTypes()
	at := time.Date(2026, 9, 13, 9, 0, 0, 0, biztime.DefaultLocation())
	track, _ := dsl.Track(TemplateKeyDeath)
	got, err := CompileTrack(track, reg, CompileOptions{EventAt: at})
	if err != nil {
		t.Fatal(err)
	}
	assertLegacyEqual(t, "death", TemplateDeath(), got, at)
	for _, a := range got.Actions {
		if a.EngineHook != EngineHookDeathVideo {
			t.Fatalf("death step %q must carry the death_evidence hook, got %q", a.Key, a.EngineHook)
		}
	}
}

// The reconcile and shifting documents have no legacy code template: they replace one hardcoded
// video on a card / event. Pin their day-one shape (exactly one video step) so a later edit is a
// deliberate SOP change, not drift.
func TestSeededReconcileAndShiftingAreOneVideoOnDayOne(t *testing.T) {
	reg, _ := SeededTaskTypes()
	at := time.Date(2026, 9, 13, 9, 0, 0, 0, biztime.DefaultLocation())
	for code, key := range map[string]string{SOPCodeReconcile: TemplateKeyReconcile, SOPCodeShifting: TemplateKeyShifting} {
		dsl := loadSeeded(t, code)
		track, ok := dsl.Track(key)
		if !ok {
			t.Fatalf("%s: track %q missing", code, key)
		}
		got, err := CompileTrack(track, reg, CompileOptions{EventAt: at})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Actions) != 1 || !got.Actions[0].RequiresVideo || got.Actions[0].Proof.Video != 1 || got.Actions[0].Type != ActionTypeAction {
			t.Fatalf("%s: expected one video step, got %+v", code, got.Actions)
		}
	}
}

// The golden oracle is only meaningful while the code templates keep their known shape.
func TestLegacyTemplatesStillHaveTheirKnownShape(t *testing.T) {
	at := time.Date(2026, 9, 13, 5, 30, 0, 0, biztime.DefaultLocation())
	if n := len(TemplateBirthKidAt(at, false).Actions); n != 18 {
		t.Fatalf("birth_kid before 06:45 should stamp 18 steps, got %d", n)
	}
	if n := len(TemplateBirthMother().Actions); n != 6 {
		t.Fatalf("birth_mother should stamp 6 steps, got %d", n)
	}
	if n := len(TemplateDeath().Actions); n != 2 {
		t.Fatalf("death should stamp 2 steps, got %d", n)
	}
}

func assertLegacyEqual(t *testing.T, label string, want, got Template, at time.Time) {
	t.Helper()
	if want.Key != got.Key || want.Module != got.Module {
		t.Fatalf("%s @%s: key/module %q/%q vs %q/%q", label, at, want.Key, want.Module, got.Key, got.Module)
	}
	w := legacyViews(t, want, at)
	g := legacyViews(t, got, at)
	if len(w) != len(g) {
		t.Fatalf("%s @%s: %d legacy steps vs %d compiled\nwant %+v\ngot  %+v", label, at, len(w), len(g), w, g)
	}
	for i := range w {
		if wj, gj := mustJSON(t, w[i]), mustJSON(t, g[i]); wj != gj {
			t.Fatalf("%s @%s step %d differs:\nwant %s\ngot  %s", label, at, i+1, wj, gj)
		}
	}
	if want.OperatorActionCount() != got.OperatorActionCount() {
		t.Fatalf("%s: operator action count %d vs %d", label, want.OperatorActionCount(), got.OperatorActionCount())
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
