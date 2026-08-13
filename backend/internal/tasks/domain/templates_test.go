package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// eventAt anchors template scheduling tests: 2026-07-27 14:30 IST.
var eventAt = time.Date(2026, 7, 27, 14, 30, 0, 0, biztime.DefaultLocation())

func TestTemplateBirthKidShape(t *testing.T) {
	// A 19:20 IST birth has only the 22:00 slot left on the birth day, then all
	// five slots on the following day: six scheduled feeds plus 1st Colostrum.
	tmpl := TemplateBirthKidAt(time.Date(2026, 7, 27, 19, 20, 0, 0, biztime.DefaultLocation()))
	if tmpl.Module != ModuleBirth {
		t.Fatalf("module = %q, want %q", tmpl.Module, ModuleBirth)
	}
	if got := tmpl.OperatorActionCount(); got != 14 {
		t.Fatalf("kid operator actions = %d, want 14", got)
	}
	if got := len(tmpl.Actions); got != 14 {
		t.Fatalf("kid total rows = %d, want 14 (8 main + 6 birth-time-derived colostrum sessions)", got)
	}
	sessions := 0
	for _, a := range tmpl.Actions {
		if a.Section == SectionColostrumSession {
			sessions++
		}
	}
	if sessions != 6 {
		t.Fatalf("colostrum sessions = %d, want 6", sessions)
	}
	tag := actionByKey(t, tmpl, ActionKeyTagTheKid)
	if got := tmpl.Actions[len(tmpl.Actions)-1].Key; got != ActionKeyTagTheKid {
		t.Fatalf("last kid task = %q, want %q", got, ActionKeyTagTheKid)
	}
	for _, action := range tmpl.Actions {
		if action.Section == SectionColostrumSession && tag.Seq <= action.Seq {
			t.Fatalf("tag seq = %d, must follow colostrum seq = %d", tag.Seq, action.Seq)
		}
	}
	assertUniqueKeysAndSeq(t, tmpl)

	// Weight is a free numeric-kilograms question; the client must not render bands.
	weight := actionByKey(t, tmpl, ActionKeyTakeWeight)
	if weight.Type != ActionTypeQuestion || len(weight.Options) != 0 {
		t.Fatalf("take_weight must be a numeric question without options, got %+v", weight)
	}
	// 1st Colostrum requires a video.
	if !actionByKey(t, tmpl, ActionKeyFirstColostrum).RequiresVideo {
		t.Fatal("first_colostrum must require a video")
	}
}

func TestTemplateBirthKidScheduleOffsets(t *testing.T) {
	tmpl := TemplateBirthKidAt(eventAt)

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

	// At 14:30 IST, the 15:00 pre-notification cutoff (14:45) has not started. The
	// birth day therefore contributes 15:00, 18:30 and 22:00, followed by all
	// five sessions on the next day.
	wantSessions := []struct{ day, h, m int }{
		{27, 15, 0}, {27, 18, 30}, {27, 22, 0},
		{28, 7, 0}, {28, 11, 0}, {28, 15, 0}, {28, 18, 30}, {28, 22, 0},
	}
	i := 0
	for _, a := range tmpl.Actions {
		if a.Section != SectionColostrumSession {
			continue
		}
		due := a.Schedule.DueAt(eventAt).In(biztime.DefaultLocation())
		if due.Year() != 2026 || due.Month() != 7 || due.Day() != wantSessions[i].day {
			t.Fatalf("session %d lands on %v, want July %d", i, due, wantSessions[i].day)
		}
		if due.Hour() != wantSessions[i].h || due.Minute() != wantSessions[i].m {
			t.Fatalf("session %d at %02d:%02d, want %02d:%02d", i, due.Hour(), due.Minute(), wantSessions[i].h, wantSessions[i].m)
		}
		i++
	}
	if i != len(wantSessions) {
		t.Fatalf("visited %d sessions, want %d", i, len(wantSessions))
	}
}

func TestTemplateBirthKidColostrumEligibilityUsesBirthTimeAndPreNotifyCutoff(t *testing.T) {
	tests := []struct {
		name          string
		birthHour     int
		birthMinute   int
		birthSecond   int
		wantSessions  int
		wantFirstDue  time.Time
		wantLastDue   time.Time
		wantLastTitle string
	}{
		{
			name:      "before first cutoff gets all birth-day and next-day sessions",
			birthHour: 6, birthMinute: 44, birthSecond: 59, wantSessions: 10,
			wantFirstDue:  time.Date(2026, 7, 27, 7, 0, 0, 0, biztime.DefaultLocation()),
			wantLastDue:   time.Date(2026, 7, 28, 22, 0, 0, 0, biztime.DefaultLocation()),
			wantLastTitle: "11th Colostrum",
		},
		{
			name:      "exactly at first cutoff skips the seven oclock slot",
			birthHour: 6, birthMinute: 45, wantSessions: 9,
			wantFirstDue:  time.Date(2026, 7, 27, 11, 0, 0, 0, biztime.DefaultLocation()),
			wantLastDue:   time.Date(2026, 7, 28, 22, 0, 0, 0, biztime.DefaultLocation()),
			wantLastTitle: "10th Colostrum",
		},
		{
			name:      "nineteen twenty produces the seven-colostrum legacy example",
			birthHour: 19, birthMinute: 20, wantSessions: 6,
			wantFirstDue:  time.Date(2026, 7, 27, 22, 0, 0, 0, biztime.DefaultLocation()),
			wantLastDue:   time.Date(2026, 7, 28, 22, 0, 0, 0, biztime.DefaultLocation()),
			wantLastTitle: "7th Colostrum",
		},
		{
			name:      "one second before final cutoff keeps the twenty-two slot",
			birthHour: 21, birthMinute: 44, birthSecond: 59, wantSessions: 6,
			wantFirstDue:  time.Date(2026, 7, 27, 22, 0, 0, 0, biztime.DefaultLocation()),
			wantLastDue:   time.Date(2026, 7, 28, 22, 0, 0, 0, biztime.DefaultLocation()),
			wantLastTitle: "7th Colostrum",
		},
		{
			name:      "exactly at final cutoff starts with next-day session one",
			birthHour: 21, birthMinute: 45, wantSessions: 5,
			wantFirstDue:  time.Date(2026, 7, 28, 7, 0, 0, 0, biztime.DefaultLocation()),
			wantLastDue:   time.Date(2026, 7, 28, 22, 0, 0, 0, biztime.DefaultLocation()),
			wantLastTitle: "6th Colostrum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			birthAt := time.Date(2026, 7, 27, tt.birthHour, tt.birthMinute, tt.birthSecond, 0, biztime.DefaultLocation())
			tmpl := TemplateBirthKidAt(birthAt)
			var sessions []ActionTemplate
			for _, action := range tmpl.Actions {
				if action.Section == SectionColostrumSession {
					sessions = append(sessions, action)
				}
			}
			if len(sessions) != tt.wantSessions {
				t.Fatalf("scheduled sessions = %d, want %d", len(sessions), tt.wantSessions)
			}
			if got := sessions[0].Schedule.DueAt(birthAt); !got.Equal(tt.wantFirstDue) {
				t.Fatalf("first scheduled session = %v, want %v", got, tt.wantFirstDue)
			}
			if got := sessions[len(sessions)-1].Schedule.DueAt(birthAt); !got.Equal(tt.wantLastDue) {
				t.Fatalf("last scheduled session = %v, want %v", got, tt.wantLastDue)
			}
			if sessions[0].Title != "2nd Colostrum" {
				t.Fatalf("first scheduled title = %q, want 2nd Colostrum", sessions[0].Title)
			}
			if got := sessions[len(sessions)-1].Title; got != tt.wantLastTitle {
				t.Fatalf("last scheduled title = %q, want %q", got, tt.wantLastTitle)
			}
		})
	}
}

func TestTemplateBirthMotherShape(t *testing.T) {
	tmpl := TemplateBirthMother()
	if tmpl.Module != ModuleBirth {
		t.Fatalf("module = %q, want %q", tmpl.Module, ModuleBirth)
	}
	if got := tmpl.OperatorActionCount(); got != 6 {
		t.Fatalf("mother main actions = %d, want 6", got)
	}
	if got := len(tmpl.Actions); got != 6 {
		t.Fatalf("mother total rows = %d, want 6", got)
	}
	assertUniqueKeysAndSeq(t, tmpl)

	for _, action := range tmpl.Actions {
		if !action.RequiresVideo {
			t.Fatalf("mother action %s must require one video, got %+v", action.Key, action)
		}
	}
	medicine := actionByKey(t, tmpl, ActionKeyMothersMedicine)
	wantMedicine := "Chocolate Injection at 1.5 ml SQ\nMeloxicam Paracetamol at 4 ml IM\nExapar at 20 ml\nGlucoboost at 100 ml mix with 150gms Concentrate"
	if medicine.Detail != wantMedicine {
		t.Fatalf("mother medicine detail = %q, want %q", medicine.Detail, wantMedicine)
	}

	// The second ORS round is dependency-timed by the repository when round 1 completes.
	ors2 := actionByKey(t, tmpl, ActionKeyORSWater2)
	if ors2.Schedule != (Schedule{}) {
		t.Fatalf("ors_water_2 must not have an event-anchored schedule: %+v", ors2.Schedule)
	}
}

func TestRequiresVideoQuestionRejectsProoflessAnswer(t *testing.T) {
	action := WorkflowAction{
		ActionID: "mother-question", ActionType: ActionTypeQuestion,
		RequiresVideo: true, Status: ActionStatusPending,
	}
	_, _, err := ApplyAnswer(action, AnswerActionCommand{
		AnswerValue: "yes", IdempotencyKey: "mother-answer-1", RequestFingerprint: "fp-1",
	})
	if !errors.Is(err, ErrProofRequired) {
		t.Fatalf("proofless mother answer err = %v, want ErrProofRequired", err)
	}
}

func TestRequiresVideoQuestionStoresAnswerAndOneProof(t *testing.T) {
	action := WorkflowAction{
		ActionID: "mother-question", ActionType: ActionTypeQuestion,
		RequiresVideo: true, Status: ActionStatusPending,
	}
	updated, replay, err := ApplyAnswer(action, AnswerActionCommand{
		AnswerValue: "yes", ProofRef: "proof-mother-1",
		IdempotencyKey: "mother-answer-1", RequestFingerprint: "fp-1",
	})
	if err != nil || replay {
		t.Fatalf("answer with video: replay=%v err=%v", replay, err)
	}
	if updated.AnswerValue == nil || *updated.AnswerValue != "yes" || updated.ProofRef == nil || *updated.ProofRef != "proof-mother-1" {
		t.Fatalf("updated answer/proof = %+v, want one answer and one proof", updated)
	}
}

func TestTemplateDeathShape(t *testing.T) {
	tmpl := TemplateDeath()
	if tmpl.Module != ModuleDeath {
		t.Fatalf("module = %q, want %q", tmpl.Module, ModuleDeath)
	}
	if got := tmpl.OperatorActionCount(); got != 2 {
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
		{ActionID: "first", ActionKey: "first", Seq: 1, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusPending},
		{ActionID: "second", ActionKey: "second", Seq: 2, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusPending},
		{ActionID: "first-colostrum", ActionKey: ActionKeyFirstColostrum, Seq: 5, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusPending},
		{ActionID: "session", ActionKey: "colostrum_day_1_2200", Seq: 9, Section: SectionColostrumSession, ActionType: ActionTypeAction, Status: ActionStatusPending},
		{ActionID: "tag", ActionKey: ActionKeyTagTheKid, Seq: 15, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusPending},
	}

	if OperatorActionBlocked(actions[0], actions) {
		t.Fatal("first main action must be enabled")
	}
	if !OperatorActionBlocked(actions[1], actions) {
		t.Fatal("second main action must be blocked until the first completes")
	}
	if !OperatorActionBlocked(actions[3], actions) {
		t.Fatal("first scheduled colostrum must wait for immediate 1st Colostrum")
	}

	actions[0].Status = ActionStatusCompleted
	if OperatorActionBlocked(actions[1], actions) {
		t.Fatal("second main action must enable after the first completes")
	}
	actions[2].Status = ActionStatusCompleted
	if OperatorActionBlocked(actions[3], actions) {
		t.Fatal("first scheduled colostrum must enable after immediate 1st Colostrum")
	}
	if !OperatorActionBlocked(actions[4], actions) {
		t.Fatal("tag must wait for unfinished scheduled colostrum")
	}
	actions[1].Status = ActionStatusCompleted
	actions[3].Status = ActionStatusCompleted
	if OperatorActionBlocked(actions[4], actions) {
		t.Fatal("tag must enable after every other kid task completes")
	}
	// Exact replays of a completed action must never be rejected as out of sequence.
	if OperatorActionBlocked(actions[0], actions) {
		t.Fatal("a completed action must remain replayable")
	}
}

func TestActionTimeBlockedEnforcesORSAndColostrumNotBeforeGates(t *testing.T) {
	due := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	orsRoundTwo := WorkflowAction{
		ActionKey: ActionKeyORSWater2,
		Status:    ActionStatusPending,
		DueAt:     &due,
	}

	if !ActionTimeBlocked(orsRoundTwo, due.Add(-time.Nanosecond)) {
		t.Fatal("ORS round 2 must remain blocked immediately before due_at")
	}
	if ActionTimeBlocked(orsRoundTwo, due) {
		t.Fatal("ORS round 2 must unlock exactly at due_at")
	}

	orsRoundTwo.DueAt = nil
	if !ActionTimeBlocked(orsRoundTwo, due) {
		t.Fatal("ORS round 2 with no recorded first-round deadline must fail closed")
	}

	orsRoundTwo.Status = ActionStatusCompleted
	if ActionTimeBlocked(orsRoundTwo, due) {
		t.Fatal("completed ORS round 2 must remain replayable")
	}

	colostrum := WorkflowAction{
		ActionKey: "colostrum_day_2_0700",
		Section:   SectionColostrumSession,
		Status:    ActionStatusPending,
		DueAt:     &due,
	}
	if !ActionTimeBlocked(colostrum, due.Add(-time.Nanosecond)) {
		t.Fatal("colostrum must remain blocked immediately before due_at")
	}
	if ActionTimeBlocked(colostrum, due) {
		t.Fatal("colostrum must unlock exactly at due_at and remain available")
	}
	colostrum.DueAt = nil
	if !ActionTimeBlocked(colostrum, due) {
		t.Fatal("colostrum with no scheduled time must fail closed")
	}

	other := WorkflowAction{ActionKey: ActionKeyKidStanding, Status: ActionStatusPending, DueAt: &due}
	if ActionTimeBlocked(other, due.Add(-time.Hour)) {
		t.Fatal("other workflow due times remain scheduling guidance")
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

func TestRecomputeCardIncludesScheduledColostrumInOperatorProgress(t *testing.T) {
	w := WorkflowInstance{TemplateKey: TemplateKeyBirthKid, State: WorkflowStateOpen}
	actions := []WorkflowAction{
		{ActionID: "first-colostrum", ActionKey: ActionKeyFirstColostrum, Seq: 1, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusCompleted},
		{ActionID: "scheduled-colostrum-2", ActionKey: "colostrum_day_1_1500", Seq: 2, Section: SectionColostrumSession, ActionType: ActionTypeAction, Status: ActionStatusCompleted},
		{ActionID: "scheduled-colostrum-3", ActionKey: "colostrum_day_1_1830", Seq: 3, Section: SectionColostrumSession, ActionType: ActionTypeAction, Status: ActionStatusPending},
		{ActionID: "tag", ActionKey: ActionKeyTagTheKid, Seq: 4, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusPending},
	}

	got := RecomputeCard(w, actions)
	if got.ActionsDone != 2 || got.ActionsTotal != 4 {
		t.Fatalf("operator progress = %d/%d, want 2/4 including scheduled colostrum", got.ActionsDone, got.ActionsTotal)
	}
}

func TestTemplateByKey(t *testing.T) {
	for _, key := range []string{TemplateKeyBirthKid, TemplateKeyBirthMother, TemplateKeyDeath} {
		if _, ok := TemplateByKeyAt(key, eventAt); !ok {
			t.Fatalf("TemplateByKeyAt(%q) not found", key)
		}
	}
	if _, ok := TemplateByKeyAt("nope", eventAt); ok {
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
