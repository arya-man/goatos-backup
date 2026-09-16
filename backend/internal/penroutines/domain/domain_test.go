package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func ptr(f float64) *float64 { return &f }

func sampleEvidence() Evidence {
	return NormalizeEvidence(Evidence{
		Questions: []Question{
			{ID: "cleaned", Kind: QuestionYesNo, Title: "Was the pen cleaned?", Required: true},
			{ID: "water", Kind: QuestionChoice, Title: "Water trough", Options: []Option{{Value: "clean", Label: "Clean"}, {Value: "dirty", Label: "Dirty"}}},
			{ID: "issues", Kind: QuestionMultiChoice, Title: "Issues seen", Options: []Option{{Value: "limping"}, {Value: "coughing"}}},
			{ID: "sick", Kind: QuestionNumber, Title: "Sick animals", Min: ptr(0), Max: ptr(500), Unit: "animals"},
			{ID: "note", Kind: QuestionText, Title: "Anything else"},
		},
		Photo:    ProofRule{Min: 1, Max: 3},
		Video:    ProofRule{Min: 0, Max: 1},
		Presence: PresenceRequired,
	})
}

func TestCadenceRaisesOnBusinessDays(t *testing.T) {
	daily := Definition{CadenceKind: CadenceDaily}
	if !daily.RaisesOn("2026-09-16") {
		t.Fatal("daily must raise every day")
	}
	weekly := Definition{CadenceKind: CadenceWeekly, Weekdays: []int{1, 3}} // Mon, Wed
	if !weekly.RaisesOn("2026-09-16") {                                     // Wednesday
		t.Fatal("weekly Mon/Wed must raise on a Wednesday")
	}
	if weekly.RaisesOn("2026-09-17") { // Thursday
		t.Fatal("weekly Mon/Wed must not raise on a Thursday")
	}
	sunday := Definition{CadenceKind: CadenceWeekly, Weekdays: []int{7}}
	if !sunday.RaisesOn("2026-09-20") {
		t.Fatal("ISO weekday 7 is Sunday")
	}
	monthly := Definition{CadenceKind: CadenceMonthly, MonthDays: []int{1, 31}}
	if !monthly.RaisesOn("2026-09-01") || !monthly.RaisesOn("2026-09-30") {
		t.Fatal("month day 31 must clamp to the 30th in September")
	}
	if monthly.RaisesOn("2026-09-29") {
		t.Fatal("29 Sep is neither the 1st nor the clamped last day")
	}
	feb := Definition{CadenceKind: CadenceMonthly, MonthDays: []int{30}}
	if !feb.RaisesOn("2026-02-28") {
		t.Fatal("month day 30 must clamp to 28 Feb 2026")
	}
	after := Definition{CadenceKind: CadenceAfterWork, AfterWorkKinds: []string{WorkDeworming}, DueOffsetDays: 1}
	if after.RaisesOn("2026-09-16") {
		t.Fatal("an after_work routine never raises from the calendar")
	}
	planned, err := after.PlannedDateFor("2026-09-16")
	if err != nil || planned != "2026-09-17" {
		t.Fatalf("planned = %q, %v", planned, err)
	}
}

func TestCadenceLineSpeaksFarm(t *testing.T) {
	cases := map[string]Definition{
		"Every day":                              {CadenceKind: CadenceDaily},
		"Every Mon, Wed, Fri":                    {CadenceKind: CadenceWeekly, Weekdays: []int{5, 1, 3}},
		"1st and 15th of the month":              {CadenceKind: CadenceMonthly, MonthDays: []int{15, 1}},
		"The day after vaccination or deworming": {CadenceKind: CadenceAfterWork, AfterWorkKinds: []string{WorkDeworming, WorkVaccination}, DueOffsetDays: 1},
		"The same day as weighing":               {CadenceKind: CadenceAfterWork, AfterWorkKinds: []string{WorkWeighing}},
		"3 days after pen move":                  {CadenceKind: CadenceAfterWork, AfterWorkKinds: []string{WorkShifting}, DueOffsetDays: 3},
	}
	for want, d := range cases {
		if got := CadenceLine(d); got != want {
			t.Errorf("CadenceLine = %q, want %q", got, want)
		}
	}
}

func TestValidateDefinitionRefusesTheHalfWritten(t *testing.T) {
	base := Definition{ParkID: "p", Name: "Pen cleaning", ScopeKind: ScopeAllPens, CadenceKind: CadenceDaily, NotifyTime: "07:00", ReviewKind: ReviewNone, Evidence: sampleEvidence()}
	if err := ValidateDefinition(base); err != nil {
		t.Fatalf("base must validate: %v", err)
	}
	bad := []Definition{
		func() Definition { d := base; d.Name = ""; return d }(),
		func() Definition { d := base; d.ScopeKind = ScopeSelectedPens; return d }(),
		func() Definition { d := base; d.CadenceKind = CadenceWeekly; return d }(),
		func() Definition { d := base; d.CadenceKind = CadenceWeekly; d.Weekdays = []int{8}; return d }(),
		func() Definition { d := base; d.CadenceKind = CadenceMonthly; d.MonthDays = []int{0}; return d }(),
		func() Definition {
			d := base
			d.CadenceKind = CadenceAfterWork
			d.AfterWorkKinds = []string{"painting"}
			return d
		}(),
		func() Definition { d := base; d.DueOffsetDays = 31; return d }(),
		func() Definition { d := base; d.NotifyTime = "7am"; return d }(),
		func() Definition { d := base; d.ReviewKind = "maybe"; return d }(),
		func() Definition { d := base; d.Evidence.Photo = ProofRule{Min: 2, Max: 1}; return d }(),
		func() Definition { d := base; d.Evidence.Questions[0].ID = "has space"; return d }(),
		func() Definition { d := base; d.Evidence.Questions[1].Options = nil; return d }(),
		func() Definition {
			d := base
			d.Evidence.Questions = append(d.Evidence.Questions, Question{ID: "cleaned", Kind: QuestionText, Title: "dup"})
			return d
		}(),
	}
	for i, d := range bad {
		if err := ValidateDefinition(d); err == nil {
			t.Errorf("case %d must be refused", i)
		} else if !errors.Is(err, ErrInvalidRoutine) && !errors.Is(err, ErrInvalidEvidence) {
			t.Errorf("case %d: wrong sentinel %v", i, err)
		}
	}
}

func TestParseEvidenceFailsLoudOnUnknownKeys(t *testing.T) {
	if _, err := ParseEvidence([]byte(`{"questions":[],"photo":{"min":0,"max":0},"video":{"min":0,"max":0},"presence":"off","geofence":true}`)); err == nil {
		t.Fatal("an unknown key must be refused, never dropped")
	}
	e, err := ParseEvidence([]byte(`{"questions":[{"id":"ok","kind":"yes_no","title":"Ok?","required":true,"options":[{"value":"yes","label":"Yes"},{"value":"no","label":"No"}]}],"photo":{"min":1,"max":1},"video":{"min":0,"max":0},"presence":"required"}`))
	if err != nil || !e.PresenceRequired() || len(e.Questions) != 1 {
		t.Fatalf("valid document refused: %v", err)
	}
}

func TestCheckAnswersEnforcesKindAndRequired(t *testing.T) {
	e := sampleEvidence()
	raw := func(s string) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	got, err := CheckAnswers(e, raw(`{"cleaned":"yes","water":"dirty","issues":["limping","limping"],"sick":3,"note":"  fine  "}`))
	if err != nil {
		t.Fatalf("valid answers refused: %v", err)
	}
	if got["cleaned"] != "yes" || got["note"] != "fine" || got["sick"] != float64(3) {
		t.Fatalf("normalized answers wrong: %#v", got)
	}
	if list, ok := got["issues"].([]string); !ok || len(list) != 1 {
		t.Fatalf("multi choice must dedupe: %#v", got["issues"])
	}
	bad := []string{
		`{}`,                                   // required cleaned missing
		`{"cleaned":"maybe"}`,                  // not an option
		`{"cleaned":"yes","sick":600}`,         // above max
		`{"cleaned":"yes","sick":"three"}`,     // not a number
		`{"cleaned":"yes","issues":"limping"}`, // multi needs a list
		`{"cleaned":"yes","extra":"x"}`,        // not a question
	}
	for _, s := range bad {
		if _, err := CheckAnswers(e, raw(s)); !errors.Is(err, ErrAnswerInvalid) {
			t.Errorf("%s must be ErrAnswerInvalid, got %v", s, err)
		}
	}
	rows := AnswerRows(Task{Evidence: e, Answers: got})
	if len(rows) != 5 || rows[0].Value != "Yes" || rows[1].Value != "Dirty" || rows[3].Value != "3 animals" {
		t.Fatalf("rendered rows wrong: %#v", rows)
	}
}

func TestSubmitRulesRunInOrder(t *testing.T) {
	now := time.Date(2026, 9, 16, 9, 12, 0, 0, time.UTC)
	me := Actor{UserID: "u1"}
	task := Task{Evidence: sampleEvidence(), ReviewKind: ReviewVerifier, WorkState: WorkStateScheduled, Status: StatusOpen, AssigneeIDs: []string{"u1"}, RowVersion: 3}
	photo := []ProofItem{{Ref: "p1", Kind: ProofKindPhoto}}
	if err := CheckSubmit(task, Actor{UserID: "u2"}, photo, 3); !errors.Is(err, ErrNotAssignee) {
		t.Fatalf("stranger: %v", err)
	}
	if err := CheckSubmit(task, me, nil, 3); !errors.Is(err, ErrProofCount) {
		t.Fatalf("no photo: %v", err)
	}
	if err := CheckSubmit(task, me, photo, 3); !errors.Is(err, ErrPresenceMissing) {
		t.Fatalf("presence required: %v", err)
	}
	if err := CheckPresence(task, me, PresenceLeave, 3); !errors.Is(err, ErrPresenceState) {
		t.Fatalf("leave before enter: %v", err)
	}
	if err := CheckPresence(task, me, PresenceEnter, 2); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale version: %v", err)
	}
	if err := CheckPresence(task, me, PresenceEnter, 3); err != nil {
		t.Fatalf("enter: %v", err)
	}
	task.EnteredAt, task.EnteredBy = &now, "u1"
	if !task.InPen(me) || task.CanCheckIn(me) {
		t.Fatal("in pen after enter")
	}
	if err := CheckPresence(task, me, PresenceEnter, 3); !errors.Is(err, ErrPresenceState) {
		t.Fatalf("double enter: %v", err)
	}
	if err := CheckSubmit(task, me, photo, 3); err != nil {
		t.Fatalf("submit after enter: %v", err)
	}
	if err := CheckSubmit(task, me, append(photo, ProofItem{Ref: "v", Kind: ProofKindVideo}, ProofItem{Ref: "v2", Kind: ProofKindVideo}), 3); !errors.Is(err, ErrProofCount) {
		t.Fatalf("two videos over max: %v", err)
	}
	if err := CheckSubmit(task, me, []ProofItem{{Ref: "x", Kind: "audio"}}, 3); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("unknown kind: %v", err)
	}
	task.Status = StatusPendingVerification
	if err := CheckSubmit(task, me, photo, 3); !errors.Is(err, ErrInReview) {
		t.Fatalf("in review: %v", err)
	}
	ws, st := SubmitOutcome(ReviewNone)
	if ws != WorkStateCompleted || st != StatusCompleted {
		t.Fatal("review none completes on submit")
	}
	ws, st = SubmitOutcome(ReviewVerifier)
	if ws != "" || st != StatusPendingVerification {
		t.Fatal("review verifier locks pending")
	}
}

func TestCopyIsBackendOwned(t *testing.T) {
	now := time.Date(2026, 9, 16, 3, 42, 0, 0, time.UTC) // 9:12 AM IST
	task := Task{RoutineName: "Pen cleaning", PenLabel: "Castro 2", ParkName: "Coimbatore", Evidence: sampleEvidence(), ReviewKind: ReviewVerifier,
		CadenceLine: "Every day", SourceDate: "2026-09-16", PlannedDate: "2026-09-16", DueDate: "2026-09-16", WorkState: WorkStateScheduled, Status: StatusOpen, AssigneeIDs: []string{"u1"}}
	if Title(task) != "Pen cleaning · Castro 2 · Coimbatore" {
		t.Fatalf("title %q", Title(task))
	}
	if StateChip(task, "2026-09-16") != "Due today" || StateTone(task) != "info" {
		t.Fatalf("chip %q", StateChip(task, "2026-09-16"))
	}
	if ReasonLine(task, "2026-09-16") != "Every day" {
		t.Fatalf("reason %q", ReasonLine(task, "2026-09-16"))
	}
	if EvidenceLine(task.Evidence) != "5 questions · 1 to 3 photos · up to 1 video · check in to pen" {
		t.Fatalf("evidence %q", EvidenceLine(task.Evidence))
	}
	if PresenceLine(task) != "Check in to the pen to start" {
		t.Fatalf("presence %q", PresenceLine(task))
	}
	task.EnteredAt, task.EnteredBy = &now, "u1"
	if PresenceLine(task) != "In pen since 9:12 am" {
		t.Fatalf("presence %q", PresenceLine(task))
	}
	after := task
	after.TriggerKinds = []string{WorkTicksRemoval, WorkDeworming}
	after.SourceDate = "2026-09-15"
	if ReasonLine(after, "2026-09-16") != "After deworming, ticks removal yesterday" {
		t.Fatalf("reason %q", ReasonLine(after, "2026-09-16"))
	}
	delayed := task
	delayed.WorkState = WorkStateDelayed
	since := "2026-09-14"
	delayed.DelayedSince = &since
	if StateChip(delayed, "2026-09-16") != "Delayed since "+biztime.FarmDateFromBusinessDate("2026-09-14") {
		t.Fatalf("delayed chip %q", StateChip(delayed, "2026-09-16"))
	}
	step := StepFor(task, Actor{UserID: "u1"}, "2026-09-16")
	if !step.CanSubmit || step.CanCheckIn || !step.InPen || !step.PresenceRequired || len(step.Form.Questions) != 5 {
		t.Fatalf("step %+v", step)
	}
	if StepFor(task, Actor{UserID: "u9"}, "2026-09-16").CanSubmit {
		t.Fatal("a stranger cannot submit")
	}
}
