package domain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/tasks/domain/sopseed"
)

// KID STAGE SHIFT TASKS (maintainer decision 2026-09-30, docs/decisions/kid-stage-shift-tasks.md).

func seededLitterTrack(t *testing.T) (FollowUpTrack, TaskTypeRegistry) {
	t.Helper()
	raw, err := sopseed.FollowUp(SOPCodeBirth)
	if err != nil {
		t.Fatal(err)
	}
	var dsl map[string]any
	if err := json.Unmarshal(raw, &dsl); err != nil {
		t.Fatal(err)
	}
	doc, err := ParseFollowUp(map[string]any{"follow_up": dsl})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := SeededTaskTypes()
	if err != nil {
		t.Fatal(err)
	}
	if problems := ValidateFollowUp(doc, reg); len(problems) > 0 {
		t.Fatalf("seeded Birth SOP with the litter track does not validate: %v", problems)
	}
	track, ok := doc.Track(TemplateKeyBirthLitter)
	if !ok {
		t.Fatal("seeded Birth SOP carries no birth_litter track")
	}
	// The kid and mother tracks are still there, unchanged in number.
	for _, key := range []string{TemplateKeyBirthKid, TemplateKeyBirthMother} {
		if _, ok := doc.Track(key); !ok {
			t.Fatalf("seeded Birth SOP lost its %s track", key)
		}
	}
	return track, reg
}

// The rule the maintainer dictated: K1 exactly 24 hours after birth, K2 exactly 7 days after the
// litter reached K1, both engine-completed.
func TestSeededLitterTrackIsTheParkHeadsTwoTimedMoves(t *testing.T) {
	track, reg := seededLitterTrack(t)
	born := time.Date(2026, 9, 30, 15, 10, 0, 0, time.UTC)
	tpl, err := CompileTrack(track, reg, CompileOptions{EventAt: born})
	if err != nil {
		t.Fatal(err)
	}
	if len(tpl.Actions) != 2 {
		t.Fatalf("want 2 steps, got %d", len(tpl.Actions))
	}
	k1, k2 := tpl.Actions[0], tpl.Actions[1]
	if k1.TargetStage != "K1" || k2.TargetStage != "K2" {
		t.Fatalf("targets = %q, %q", k1.TargetStage, k2.TargetStage)
	}
	// The PARK HEAD's task (maintainer instruction 2026-09-30): operators have no task access; the
	// park head tells the health managers, who raise the shifting.
	if k1.Owner != "park_head" || k2.Owner != "park_head" {
		t.Fatalf("owners = %q, %q, want park_head", k1.Owner, k2.Owner)
	}
	if k1.EngineHook != EngineHookShiftKidsStage || k2.EngineHook != EngineHookShiftKidsStage {
		t.Fatal("both steps must carry the shift hook")
	}
	if got := k1.Schedule.DueAt(born); !got.Equal(born.Add(24 * time.Hour)) {
		t.Fatalf("K1 due %v, want exactly 24h after birth %v", got, born.Add(24*time.Hour))
	}
	if k2.Schedule.AfterStepKey != "shift_to_k1" || k2.Schedule.Offset != 7*24*time.Hour {
		t.Fatalf("K2 schedule = %+v, want 7 days after shift_to_k1", k2.Schedule)
	}
	if len(k2.Requires) != 1 || k2.Requires[0] != "shift_to_k1" {
		t.Fatalf("K2 must require the K1 step, got %v", k2.Requires)
	}
}

func TestKidShiftStepValidation(t *testing.T) {
	reg, err := SeededTaskTypes()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		track FollowUpTrack
		want  string
	}{
		"only on the litter track": {
			FollowUpTrack{Key: TemplateKeyBirthKid, Module: ModuleBirth, Steps: []FollowUpStep{
				{Key: "s", TaskType: "shift_kids_stage", Title: "Shift", TargetStage: "K1", Schedule: FollowUpSchedule{Kind: "immediately"}}}},
			"only runs on the litter's steps",
		},
		"target required": {
			FollowUpTrack{Key: TemplateKeyBirthLitter, Module: ModuleBirth, Steps: []FollowUpStep{
				{Key: "s", TaskType: "shift_kids_stage", Title: "Shift", Schedule: FollowUpSchedule{Kind: "immediately"}}}},
			"must name the stage",
		},
		"target must be a growth stage": {
			FollowUpTrack{Key: TemplateKeyBirthLitter, Module: ModuleBirth, Steps: []FollowUpStep{
				{Key: "s", TaskType: "shift_kids_stage", Title: "Shift", TargetStage: "K0", Schedule: FollowUpSchedule{Kind: "immediately"}}}},
			"not a stage animals grow into",
		},
		"target only on a shift step": {
			FollowUpTrack{Key: TemplateKeyBirthLitter, Module: ModuleBirth, Steps: []FollowUpStep{
				{Key: "s", TaskType: "do_and_confirm", Title: "Do", TargetStage: "K1", Schedule: FollowUpSchedule{Kind: "immediately"}}}},
			"only a kid shift step names a target stage",
		},
	}
	for name, c := range cases {
		problems := ValidateFollowUp(FollowUpDSL{SchemaVersion: FollowUpSchemaVersion, Tracks: []FollowUpTrack{c.track}}, reg)
		if !strings.Contains(strings.Join(problems, "|"), c.want) {
			t.Errorf("%s: problems %v do not mention %q", name, problems, c.want)
		}
	}
}

func litterActions() []WorkflowAction {
	return []WorkflowAction{
		{ActionID: "a1", ActionKey: "shift_to_k1", Seq: 1, Section: SectionMain, ActionType: ActionTypeAction,
			Status: ActionStatusPending, EngineHook: EngineHookShiftKidsStage, TargetStage: "K1", OwnerRole: "park_head"},
		{ActionID: "a2", ActionKey: "shift_to_k2", Seq: 2, Section: SectionMain, ActionType: ActionTypeAction,
			Status: ActionStatusPending, EngineHook: EngineHookShiftKidsStage, TargetStage: "K2", OwnerRole: "park_head",
			RequiresKeys: []string{"shift_to_k1"}, AfterActionKey: "shift_to_k1", AfterOffsetSeconds: 7 * 24 * 3600},
	}
}

func TestLitterShiftCompletesOnlyWhenTheWholeLitterReachedTheStage(t *testing.T) {
	actions := litterActions()
	t1 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	// Twins, one moved: nothing completes -- the task must not read done over a K0 kid.
	changed, cancel := ApplyLitterShift(actions, []LitterKid{{GoatID: "k1", Stage: "K1", Alive: true}, {GoatID: "k2", Stage: "K0", Alive: true}}, t1)
	if len(changed) != 0 || cancel {
		t.Fatalf("half a litter moved must change nothing, got %d changes cancel=%v", len(changed), cancel)
	}

	// Both on K1: the K1 step completes at the moment the LAST kid moved, and the K2 step is due
	// exactly 7 days later -- not 7 days after birth.
	t2 := t1.Add(3 * time.Hour)
	changed, _ = ApplyLitterShift(actions, []LitterKid{{GoatID: "k1", Stage: "K1", Alive: true}, {GoatID: "k2", Stage: "K1", Alive: true}}, t2)
	if actions[0].Status != ActionStatusCompleted || !actions[0].CompletedAt.Equal(t2) {
		t.Fatalf("K1 step = %s at %v, want completed at %v", actions[0].Status, actions[0].CompletedAt, t2)
	}
	if actions[1].Status != ActionStatusPending || actions[1].DueAt == nil || !actions[1].DueAt.Equal(t2.Add(7*24*time.Hour)) {
		t.Fatalf("K2 step = %s due %v, want pending due %v", actions[1].Status, actions[1].DueAt, t2.Add(7*24*time.Hour))
	}
	if len(changed) != 2 {
		t.Fatalf("want the completed step and its dependent, got %d", len(changed))
	}

	// A redelivery changes nothing.
	if again, _ := ApplyLitterShift(actions, []LitterKid{{GoatID: "k1", Stage: "K1", Alive: true}, {GoatID: "k2", Stage: "K1", Alive: true}}, t2.Add(time.Minute)); len(again) != 0 {
		t.Fatalf("redelivery changed %d rows", len(again))
	}

	// Both on K2: the K2 step completes.
	t3 := t2.Add(7 * 24 * time.Hour)
	ApplyLitterShift(actions, []LitterKid{{GoatID: "k1", Stage: "K2", Alive: true}, {GoatID: "k2", Stage: "K2", Alive: true}}, t3)
	if actions[1].Status != ActionStatusCompleted {
		t.Fatalf("K2 step = %s, want completed", actions[1].Status)
	}
}

func TestLitterShiftIgnoresKidsThatLeftTheFarm(t *testing.T) {
	actions := litterActions()
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	ApplyLitterShift(actions, []LitterKid{{GoatID: "k1", Stage: "K1", Alive: true}, {GoatID: "k2", Stage: "K0", Alive: false}}, at)
	if actions[0].Status != ActionStatusCompleted {
		t.Fatalf("a dead K0 twin must not hold the move open, K1 step = %s", actions[0].Status)
	}

	// Every kid gone: the litter owes nothing and the workflow is canceled.
	actions = litterActions()
	changed, cancel := ApplyLitterShift(actions, []LitterKid{{GoatID: "k1", Stage: "K0", Alive: false}, {GoatID: "k2", Stage: "K0", Alive: false}}, at)
	if !cancel || len(changed) != 2 || actions[0].Status != ActionStatusCanceled || actions[1].Status != ActionStatusCanceled {
		t.Fatalf("no live kid: cancel=%v changed=%d statuses %s/%s", cancel, len(changed), actions[0].Status, actions[1].Status)
	}
}

// A kid moved straight past K1 still counts as having reached it; a kid in ICU is neither.
func TestJudgeLitterShiftReadsTheLadder(t *testing.T) {
	o := JudgeLitterShift([]LitterKid{{GoatID: "a", Stage: "K2", Alive: true}, {GoatID: "b", Stage: "ICU-Kid", Alive: true}}, "K1")
	if o.Reached != 1 || o.Waiting != 0 || o.Live != 2 || !o.Done() {
		t.Fatalf("got %+v", o)
	}
	if JudgeLitterShift([]LitterKid{{GoatID: "a", Stage: "ICU-Kid", Alive: true}}, "K1").Done() {
		t.Fatal("a litter whose only kid is in ICU has not reached K1")
	}
}

func TestKidShiftStepCannotBeTappedDone(t *testing.T) {
	a := WorkflowAction{EngineHook: EngineHookShiftKidsStage}
	if !errors.Is(EngineCompletedStepRefusal(a), ErrKidShiftPending) {
		t.Fatal("a by-hand completion of a kid shift step must be refused")
	}
}

// TestMigrationEmbedsTheKidShiftSeed pins migration 000462 to the embedded seed files: the track
// every published Birth SOP gains and the task type row must be the SAME bytes the seeded
// document compiles from, or the tests above prove nothing about what a farm runs.
func TestMigrationEmbedsTheKidShiftSeed(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", "000462_kid_stage_shift_tasks.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"counts_birth_litter_track.json", "task_types_kid_shift.json"} {
		doc, err := sopseed.Raw(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "$seed$"+strings.TrimSpace(string(doc))+"$seed$") {
			t.Fatalf("migration 000462 does not embed %s verbatim", name)
		}
	}
}

// A litter that reached K1 on Monday and is judged on Thursday (a late event, or the backfill of a
// litter already on the farm) completes AT MONDAY, so its K2 task is due the next Monday.
func TestLitterShiftCountsFromWhenTheKidsReallyReachedTheStage(t *testing.T) {
	actions := litterActions()
	monday := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	sunday := monday.Add(-24 * time.Hour)
	thursday := monday.Add(72 * time.Hour)
	ApplyLitterShift(actions, []LitterKid{
		{GoatID: "a", Stage: "K1", Alive: true, StageSince: &sunday},
		{GoatID: "b", Stage: "K1", Alive: true, StageSince: &monday},
	}, thursday)
	if actions[0].CompletedAt == nil || !actions[0].CompletedAt.Equal(monday) {
		t.Fatalf("K1 step completed at %v, want the last kid's entry %v", actions[0].CompletedAt, monday)
	}
	if !actions[1].DueAt.Equal(monday.Add(7 * 24 * time.Hour)) {
		t.Fatalf("K2 due %v, want 7 days after %v", actions[1].DueAt, monday)
	}

	// A kid with no recorded entry (seeded on its stage): the caller's instant stands.
	actions = litterActions()
	ApplyLitterShift(actions, []LitterKid{{GoatID: "a", Stage: "K1", Alive: true, StageSince: &monday}, {GoatID: "b", Stage: "K1", Alive: true}}, thursday)
	if !actions[0].CompletedAt.Equal(thursday) {
		t.Fatalf("with unknown history, completed at %v, want %v", actions[0].CompletedAt, thursday)
	}
}

// Mixed-sex twins cannot share one growth shifting (counts refuses missing_impacts), so the task
// offers one raise per sex; same-sex twins stay one raise; a dead twin is never offered.
func TestShiftGroupsSplitWhatOneShiftingCannotCarry(t *testing.T) {
	kid := func(id, sex, breed string, alive bool) LitterKidView {
		return LitterKidView{GoatID: id, Tag: "T-" + id, Stage: "K0", Alive: alive, Sex: sex, Breed: breed, AgeBand: "kid"}
	}
	mixed := ShiftGroups([]LitterKidView{kid("a", "female", "Sirohi", true), kid("b", "male", "Sirohi", true)}, "K1")
	if len(mixed) != 2 || mixed[0].Label != "1 female kid" || mixed[1].Label != "1 male kid" {
		t.Fatalf("mixed-sex twins: %+v", mixed)
	}
	same := ShiftGroups([]LitterKidView{kid("a", "female", "Sirohi", true), kid("b", "female", "Sirohi", true)}, "K1")
	if len(same) != 1 || same[0].Label != "2 female kids" || len(same[0].Kids) != 2 {
		t.Fatalf("same-sex twins: %+v", same)
	}
	dead := ShiftGroups([]LitterKidView{kid("a", "female", "Sirohi", false), kid("b", "male", "Sirohi", true)}, "K1")
	if len(dead) != 1 || dead[0].Label != "1 male kid" {
		t.Fatalf("dead twin: %+v", dead)
	}
	breeds := ShiftGroups([]LitterKidView{kid("a", "female", "Sirohi", true), kid("b", "female", "Jamunapari", true)}, "K1")
	if len(breeds) != 2 || breeds[0].Label != "1 female Sirohi kid" {
		t.Fatalf("two breeds: %+v", breeds)
	}
	moved := ShiftGroups([]LitterKidView{{GoatID: "a", Stage: "K1", Alive: true, Sex: "female"}}, "K1")
	if len(moved) != 0 {
		t.Fatalf("a kid already on the target is offered: %+v", moved)
	}
}
