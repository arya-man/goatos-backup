package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// eventAt anchors template scheduling tests: 2026-07-27 14:30 IST.
var eventAt = time.Date(2026, 7, 27, 14, 30, 0, 0, biztime.DefaultLocation())

func TestTemplateBirthKidShape(t *testing.T) {
	tmpl := TemplateBirthKid()
	if tmpl.Module != ModuleBirth {
		t.Fatalf("module = %q, want %q", tmpl.Module, ModuleBirth)
	}
	if got := tmpl.MainActionCount(); got != 8 {
		t.Fatalf("kid main actions = %d, want 8", got)
	}
	if got := len(tmpl.Actions); got != 13 {
		t.Fatalf("kid total rows = %d, want 13 (8 main + 5 colostrum sessions)", got)
	}
	sessions := 0
	for _, a := range tmpl.Actions {
		if a.Section == SectionColostrumSession {
			sessions++
		}
	}
	if sessions != 5 {
		t.Fatalf("colostrum sessions = %d, want 5", sessions)
	}
	assertUniqueKeysAndSeq(t, tmpl)

	// The weight step is a question_select with declared bands.
	weight := actionByKey(t, tmpl, ActionKeyTakeWeight)
	if weight.Type != ActionTypeQuestionSelect || len(weight.Options) == 0 {
		t.Fatalf("take_weight must be a question_select with options, got %+v", weight)
	}
	// 1st Colostrum requires a video.
	if !actionByKey(t, tmpl, ActionKeyFirstColostrum).RequiresVideo {
		t.Fatal("first_colostrum must require a video")
	}
}

func TestTemplateBirthKidScheduleOffsets(t *testing.T) {
	tmpl := TemplateBirthKid()

	// kid_standing is EVENT+1H.
	standing := actionByKey(t, tmpl, ActionKeyKidStanding)
	if got, want := standing.Schedule.DueAt(eventAt), eventAt.Add(time.Hour); !got.Equal(want) {
		t.Fatalf("kid_standing due = %v, want %v", got, want)
	}

	// tag_the_kid is EVENT+2D at 07:00 IST.
	tag := actionByKey(t, tmpl, ActionKeyTagTheKid)
	wantTag := time.Date(2026, 7, 29, 7, 0, 0, 0, biztime.DefaultLocation())
	if got := tag.Schedule.DueAt(eventAt); !got.Equal(wantTag) {
		t.Fatalf("tag_the_kid due = %v, want %v", got, wantTag)
	}

	// The 5 colostrum sessions land on the BIRTH date at fixed IST wall-clock times.
	wantSessions := []struct{ h, m int }{{7, 0}, {11, 0}, {15, 0}, {18, 30}, {22, 0}}
	i := 0
	for _, a := range tmpl.Actions {
		if a.Section != SectionColostrumSession {
			continue
		}
		due := a.Schedule.DueAt(eventAt).In(biztime.DefaultLocation())
		if due.Year() != 2026 || due.Month() != 7 || due.Day() != 27 {
			t.Fatalf("session %d lands on %v, want the birth date", i, due)
		}
		if due.Hour() != wantSessions[i].h || due.Minute() != wantSessions[i].m {
			t.Fatalf("session %d at %02d:%02d, want %02d:%02d", i, due.Hour(), due.Minute(), wantSessions[i].h, wantSessions[i].m)
		}
		i++
	}
	if i != 5 {
		t.Fatalf("visited %d sessions, want 5", i)
	}
}

func TestTemplateBirthMotherShape(t *testing.T) {
	tmpl := TemplateBirthMother()
	if tmpl.Module != ModuleBirth {
		t.Fatalf("module = %q, want %q", tmpl.Module, ModuleBirth)
	}
	if got := tmpl.MainActionCount(); got != 6 {
		t.Fatalf("mother main actions = %d, want 6", got)
	}
	if got := len(tmpl.Actions); got != 6 {
		t.Fatalf("mother total rows = %d, want 6", got)
	}
	assertUniqueKeysAndSeq(t, tmpl)

	// The 2nd ORS round is the fixed EVENT+6H simplification of legacy FUNC_ORS_2.
	ors2 := actionByKey(t, tmpl, ActionKeyORSWater2)
	if got, want := ors2.Schedule.DueAt(eventAt), eventAt.Add(6*time.Hour); !got.Equal(want) {
		t.Fatalf("ors_water_2 due = %v, want %v", got, want)
	}
}

func TestTemplateDeathShape(t *testing.T) {
	tmpl := TemplateDeath()
	if tmpl.Module != ModuleDeath {
		t.Fatalf("module = %q, want %q", tmpl.Module, ModuleDeath)
	}
	if got := tmpl.MainActionCount(); got != 2 {
		t.Fatalf("death actions = %d, want exactly 2 videos", got)
	}
	if got := len(tmpl.Actions); got != 2 {
		t.Fatalf("death persisted actions = %d, want exactly 2; approval/verification are workflow state", got)
	}
	assertUniqueKeysAndSeq(t, tmpl)

	for _, key := range []string{ActionKeyDeathVideo, ActionKeyPostMortemVideo} {
		a := actionByKey(t, tmpl, key)
		if !a.RequiresVideo || a.Type != ActionTypeAction {
			t.Fatalf("%s must be a requires_video action, got %+v", key, a)
		}
	}
	for _, action := range tmpl.Actions {
		if action.Type == ActionTypeApproval || action.Key == ActionKeyParkHeadSignoff {
			t.Fatalf("death must not persist an operator approval action, got %+v", action)
		}
	}
}

func TestOperatorActionBlockedEnforcesSequenceWithinItsSection(t *testing.T) {
	actions := []WorkflowAction{
		{ActionID: "first", Seq: 1, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusPending},
		{ActionID: "second", Seq: 2, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusPending},
		// A separate scheduled lane must not be held behind the main lane.
		{ActionID: "session", Seq: 9, Section: SectionColostrumSession, ActionType: ActionTypeAction, Status: ActionStatusPending},
	}

	if OperatorActionBlocked(actions[0], actions) {
		t.Fatal("first main action must be enabled")
	}
	if !OperatorActionBlocked(actions[1], actions) {
		t.Fatal("second main action must be blocked until the first completes")
	}
	if OperatorActionBlocked(actions[2], actions) {
		t.Fatal("first action in a separate section must be enabled")
	}

	actions[0].Status = ActionStatusCompleted
	if OperatorActionBlocked(actions[1], actions) {
		t.Fatal("second main action must enable after the first completes")
	}
	// Exact replays of a completed action must never be rejected as out of sequence.
	if OperatorActionBlocked(actions[0], actions) {
		t.Fatal("a completed action must remain replayable")
	}
}

func TestRecomputeCardUsesOperatorActionsButWaitsForInternalApproval(t *testing.T) {
	w := WorkflowInstance{State: WorkflowStateOpen}
	actions := []WorkflowAction{
		{ActionID: "death", Seq: 1, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusCompleted},
		{ActionID: "postmortem", Seq: 2, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusCompleted},
		{ActionID: "approval", Seq: 3, Section: SectionMain, ActionType: ActionTypeApproval, Status: ActionStatusPending},
	}

	got := RecomputeCard(w, actions)
	if got.ActionsDone != 2 || got.ActionsTotal != 2 {
		t.Fatalf("operator progress = %d/%d, want 2/2", got.ActionsDone, got.ActionsTotal)
	}
	if got.State != WorkflowStateOpen {
		t.Fatalf("workflow state = %q, want open until internal approval completes", got.State)
	}
	if got.NextActionKey != nil {
		t.Fatalf("next operator action = %q, want none", *got.NextActionKey)
	}
}

func TestTemplateByKey(t *testing.T) {
	for _, key := range []string{TemplateKeyBirthKid, TemplateKeyBirthMother, TemplateKeyDeath} {
		if _, ok := TemplateByKey(key); !ok {
			t.Fatalf("TemplateByKey(%q) not found", key)
		}
	}
	if _, ok := TemplateByKey("nope"); ok {
		t.Fatal("unknown key must not resolve")
	}
	if got := ModuleForTemplate(TemplateKeyBirthMother); got != ModuleBirth {
		t.Fatalf("ModuleForTemplate(birth_mother) = %q", got)
	}
	if got := ModuleForTemplate(TemplateKeyDeath); got != ModuleDeath {
		t.Fatalf("ModuleForTemplate(death) = %q", got)
	}
}

func assertUniqueKeysAndSeq(t *testing.T, tmpl Template) {
	t.Helper()
	keys := map[string]bool{}
	seqs := map[int]bool{}
	for _, a := range tmpl.Actions {
		if keys[a.Key] {
			t.Fatalf("duplicate action key %q in %s", a.Key, tmpl.Key)
		}
		if seqs[a.Seq] {
			t.Fatalf("duplicate seq %d in %s", a.Seq, tmpl.Key)
		}
		keys[a.Key] = true
		seqs[a.Seq] = true
		if a.Title == "" {
			t.Fatalf("action %q has no title", a.Key)
		}
	}
}

func actionByKey(t *testing.T, tmpl Template, key string) ActionTemplate {
	t.Helper()
	for _, a := range tmpl.Actions {
		if a.Key == key {
			return a
		}
	}
	t.Fatalf("action %q not found in %s", key, tmpl.Key)
	return ActionTemplate{}
}
