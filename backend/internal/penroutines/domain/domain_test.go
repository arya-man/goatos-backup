package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	const start = "2026-01-01"
	daily := Definition{CadenceKind: CadenceDaily, StartDate: start}
	if !daily.RaisesOn("2026-09-16") {
		t.Fatal("daily must raise every day")
	}
	weekly := Definition{CadenceKind: CadenceWeekly, Weekdays: []int{1, 3}, StartDate: start} // Mon, Wed
	if !weekly.RaisesOn("2026-09-16") {                                                       // Wednesday
		t.Fatal("weekly Mon/Wed must raise on a Wednesday")
	}
	if weekly.RaisesOn("2026-09-17") { // Thursday
		t.Fatal("weekly Mon/Wed must not raise on a Thursday")
	}
	sunday := Definition{CadenceKind: CadenceWeekly, Weekdays: []int{7}, StartDate: start}
	if !sunday.RaisesOn("2026-09-20") {
		t.Fatal("ISO weekday 7 is Sunday")
	}
	monthly := Definition{CadenceKind: CadenceMonthly, MonthDays: []int{1, 31}, StartDate: start}
	if !monthly.RaisesOn("2026-09-01") || !monthly.RaisesOn("2026-09-30") {
		t.Fatal("month day 31 must clamp to the 30th in September")
	}
	if monthly.RaisesOn("2026-09-29") {
		t.Fatal("29 Sep is neither the 1st nor the clamped last day")
	}
	feb := Definition{CadenceKind: CadenceMonthly, MonthDays: []int{30}, StartDate: start}
	if !feb.RaisesOn("2026-02-28") {
		t.Fatal("month day 30 must clamp to 28 Feb 2026")
	}
	after := Definition{CadenceKind: CadenceAfterWork, AfterWorkKinds: []string{WorkDeworming}, DueOffsetDays: 1, StartDate: start}
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
		"Every 3 days":                           {CadenceKind: CadenceEveryNDays, IntervalDays: 3},
		"Every 2 days":                           {CadenceKind: CadenceEveryNDays, IntervalDays: 2},
	}
	for want, d := range cases {
		if got := CadenceLine(d); got != want {
			t.Errorf("CadenceLine = %q, want %q", got, want)
		}
	}
}

func TestValidateDefinitionRefusesTheHalfWritten(t *testing.T) {
	base := Definition{ParkID: "p", Name: "Pen cleaning", ScopeKind: ScopeAllPens, CadenceKind: CadenceDaily, StartDate: "2026-09-16", AssigneeRoles: []string{RoleParkHead}, NotifyTime: "07:00", ReviewKind: ReviewNone, Evidence: sampleEvidence()}
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
		func() Definition { d := base; d.AssigneeRoles = nil; return d }(),
		func() Definition { d := base; d.AssigneeRoles = []string{"operator"}; return d }(),
		func() Definition { d := base; d.AssigneeRoles = []string{RoleParkHead, RoleParkHead}; return d }(),
		func() Definition { d := base; d.StartDate = "someday"; return d }(),
		func() Definition { d := base; d.StartDate = ""; return d }(),
		func() Definition { d := base; d.CadenceKind = CadenceEveryNDays; d.IntervalDays = 1; return d }(),
		func() Definition { d := base; d.CadenceKind = CadenceEveryNDays; d.IntervalDays = 91; return d }(),
		func() Definition {
			d := base
			d.ScopeKind = ScopePark
			d.CadenceKind = CadenceAfterWork
			d.AfterWorkKinds = []string{WorkDeworming}
			return d
		}(),
		func() Definition { d := base; d.ScopeKind = ScopePark; d.Pens = []PenRef{{ShedID: "s"}}; return d }(),
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
	if err := CheckSubmit(task, Actor{UserID: "u2"}, photo, nil, 3); !errors.Is(err, ErrNotAssignee) {
		t.Fatalf("stranger: %v", err)
	}
	if err := CheckSubmit(task, me, nil, nil, 3); !errors.Is(err, ErrProofCount) {
		t.Fatalf("no photo: %v", err)
	}
	if err := CheckSubmit(task, me, photo, nil, 3); !errors.Is(err, ErrPresenceMissing) {
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
	if err := CheckSubmit(task, me, photo, nil, 3); err != nil {
		t.Fatalf("submit after enter: %v", err)
	}
	if err := CheckSubmit(task, me, append(photo, ProofItem{Ref: "v", Kind: ProofKindVideo}, ProofItem{Ref: "v2", Kind: ProofKindVideo}), nil, 3); !errors.Is(err, ErrProofCount) {
		t.Fatalf("two videos over max: %v", err)
	}
	if err := CheckSubmit(task, me, []ProofItem{{Ref: "x", Kind: "audio"}}, nil, 3); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("unknown kind: %v", err)
	}
	task.Status = StatusPendingVerification
	if err := CheckSubmit(task, me, photo, nil, 3); !errors.Is(err, ErrInReview) {
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

// TestRolesAndEveryNDaysAndParkScope pins the 2026-09-17 revision (docs/decisions/pen-routines.md):
// routines are for ROLES from a closed vocabulary, every_n_days is anchored on the start date and
// nothing raises before it, and a whole-park routine refuses after_work and speaks "check in".
func TestRolesAndEveryNDaysAndParkScope(t *testing.T) {
	if len(AssignableRoles) != 8 || RoleLabel(RoleCXO) != "CXO" || RoleLabel(RolePCDirector) != "Preventive Care Director" || RoleLabel(RoleParkHead) != "Park Head" {
		t.Fatalf("role vocabulary drifted: %v", AssignableRoles)
	}
	if IsAssignableRole("operator") || !IsAssignableRole(RoleProcurementDirector) {
		t.Fatal("operator is not assignable; procurement director is")
	}
	if err := ValidateDefinition(Definition{ParkID: "p", Name: "x", ScopeKind: ScopeAllPens, CadenceKind: CadenceDaily, StartDate: "2026-09-16", NotifyTime: "07:00", ReviewKind: ReviewNone, Evidence: sampleEvidence()}); !errors.Is(err, ErrNoRoles) || !errors.Is(err, ErrInvalidRoutine) {
		t.Fatalf("no roles must be ErrNoRoles (an ErrInvalidRoutine), got %v", err)
	}
	every3 := Definition{CadenceKind: CadenceEveryNDays, IntervalDays: 3, StartDate: "2026-09-14"}
	for date, want := range map[string]bool{
		"2026-09-11": false, // before start, even though 3 days before it
		"2026-09-13": false, // before start
		"2026-09-14": true,  // the start date itself
		"2026-09-15": false,
		"2026-09-16": false,
		"2026-09-17": true, // start + 3
		"2026-09-18": false,
		"2026-09-20": true, // start + 6
	} {
		if got := every3.RaisesOn(date); got != want {
			t.Errorf("every 3 days from 14 Sep: RaisesOn(%s) = %v, want %v", date, got, want)
		}
	}
	for _, cadence := range []Definition{
		{CadenceKind: CadenceDaily, StartDate: "2026-09-17"},
		{CadenceKind: CadenceWeekly, Weekdays: []int{3}, StartDate: "2026-09-17"}, // 16 Sep is a Wednesday
		{CadenceKind: CadenceMonthly, MonthDays: []int{16}, StartDate: "2026-09-17"},
	} {
		if cadence.RaisesOn("2026-09-16") {
			t.Errorf("%s must raise nothing before its start date", cadence.CadenceKind)
		}
	}
	if (Definition{CadenceKind: CadenceAfterWork, StartDate: "2026-09-17"}).StartedBy("2026-09-16") {
		t.Error("an after_work routine has not started before its start date")
	}

	park := Task{RoutineName: "Medicine store", ParkName: "Coimbatore", ScopeKind: ScopePark, Evidence: sampleEvidence(), ReviewKind: ReviewNone,
		CadenceLine: "Every 3 days", PlannedDate: "2026-09-16", DueDate: "2026-09-16", WorkState: WorkStateScheduled, Status: StatusOpen, AssigneeIDs: []string{"u1"}}
	if Title(park) != "Medicine store · Coimbatore" {
		t.Fatalf("park title %q", Title(park))
	}
	if PresenceLine(park) != "Check in to start" {
		t.Fatalf("park presence %q", PresenceLine(park))
	}
	if Instruction(park, Actor{UserID: "u1"}) != "Check in when you arrive, then answer and submit." {
		t.Fatalf("park instruction %q", Instruction(park, Actor{UserID: "u1"}))
	}
	step := StepFor(park, Actor{UserID: "u1"}, "2026-09-16")
	if step.ScopeKind != ScopePark || step.ShedID != "" || step.PenLabel != "" || step.Partition != "" || step.ShedName != "" {
		t.Fatalf("park step names a pen: %+v", step)
	}
	if step.EvidenceLine != "5 questions · 1 to 3 photos · up to 1 video · check in" {
		t.Fatalf("park evidence line %q", step.EvidenceLine)
	}
}

// TestQuestionProofRulesAreEnforcedPerQuestion pins the 2026-09-18 per-question proof: a
// capture naming a question must land on a question that asked for that medium, within its
// single/multiple count; a required question with a proof rule owes a capture; an optional
// one owes it only once answered; and question captures never count toward the task-wide
// photo/video rule.
func TestQuestionProofRulesAreEnforcedPerQuestion(t *testing.T) {
	e := NormalizeEvidence(Evidence{
		Questions: []Question{
			{ID: "clean", Kind: QuestionYesNo, Title: "Pen cleaned?", Required: true, Proof: &QuestionProof{Kind: QuestionProofPhoto, Count: QuestionProofSingle}},
			{ID: "water", Kind: QuestionText, Title: "Water trough note", Proof: &QuestionProof{Kind: QuestionProofVideo, Count: QuestionProofMultiple}},
			{ID: "plain", Kind: QuestionYesNo, Title: "Gate shut?", Required: true},
		},
		Photo: ProofRule{Min: 1, Max: 1},
	})
	if err := ValidateEvidence(e); err != nil {
		t.Fatalf("valid evidence: %v", err)
	}
	if e.QuestionsWithProof() != 2 || !strings.Contains(EvidenceLine(e), "2 questions with proof") {
		t.Fatalf("evidence line: %q", EvidenceLine(e))
	}
	taskWide := ProofItem{Ref: "t1", Kind: ProofKindPhoto}
	cleanPhoto := ProofItem{Ref: "c1", Kind: ProofKindPhoto, QuestionID: "clean"}
	answered := map[string]bool{"clean": true, "plain": true}

	// The required question's photo is owed even with the task-wide photo present.
	if err := CheckProofs(e, []ProofItem{taskWide}, answered); !errors.Is(err, ErrQuestionProofMissing) {
		t.Fatalf("required question without proof: %v", err)
	}
	// A question capture does not satisfy the task-wide rule.
	if err := CheckProofs(e, []ProofItem{cleanPhoto}, answered); !errors.Is(err, ErrProofCount) {
		t.Fatalf("question photo counted task-wide: %v", err)
	}
	if err := CheckProofs(e, []ProofItem{taskWide, cleanPhoto}, answered); err != nil {
		t.Fatalf("both present: %v", err)
	}
	// Single means one: a second capture on the same question is refused.
	if err := CheckProofs(e, []ProofItem{taskWide, cleanPhoto, {Ref: "c2", Kind: ProofKindPhoto, QuestionID: "clean"}}, answered); !errors.Is(err, ErrProofCount) {
		t.Fatalf("second single capture: %v", err)
	}
	// Wrong medium, unknown question, question with no proof rule: all invalid.
	for _, bad := range []ProofItem{
		{Ref: "v", Kind: ProofKindVideo, QuestionID: "clean"},
		{Ref: "p", Kind: ProofKindPhoto, QuestionID: "nope"},
		{Ref: "p", Kind: ProofKindPhoto, QuestionID: "plain"},
	} {
		if err := CheckProofs(e, []ProofItem{taskWide, cleanPhoto, bad}, answered); !errors.Is(err, ErrInvalidProof) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	// The optional video question owes nothing while blank, and up to MaxProofPerKind once answered.
	answered["water"] = true
	if err := CheckProofs(e, []ProofItem{taskWide, cleanPhoto}, answered); !errors.Is(err, ErrQuestionProofMissing) {
		t.Fatalf("answered optional without proof: %v", err)
	}
	videos := []ProofItem{taskWide, cleanPhoto}
	for i := 0; i < MaxProofPerKind; i++ {
		videos = append(videos, ProofItem{Ref: fmt.Sprintf("w%d", i), Kind: ProofKindVideo, QuestionID: "water"})
	}
	if err := CheckProofs(e, videos, answered); err != nil {
		t.Fatalf("multiple at cap: %v", err)
	}
	if err := CheckProofs(e, append(videos, ProofItem{Ref: "over", Kind: ProofKindVideo, QuestionID: "water"}), answered); !errors.Is(err, ErrProofCount) {
		t.Fatalf("multiple over cap: %v", err)
	}
	if got := QuestionProofRefs(videos)["water"]; len(got) != MaxProofPerKind {
		t.Fatalf("grouped refs: %d", len(got))
	}

	// Authoring: unknown proof kind/count refused; a blank kind normalizes to no proof.
	bad := e
	bad.Questions = append([]Question{}, e.Questions...)
	bad.Questions[0].Proof = &QuestionProof{Kind: "audio", Count: QuestionProofSingle}
	if err := ValidateEvidence(bad); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("unknown proof kind: %v", err)
	}
	bad.Questions[0].Proof = &QuestionProof{Kind: QuestionProofPhoto, Count: "lots"}
	if err := ValidateEvidence(bad); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("unknown proof count: %v", err)
	}
	blank := NormalizeEvidence(Evidence{Questions: []Question{{ID: "q", Kind: QuestionYesNo, Title: "x", Proof: &QuestionProof{}}}})
	if blank.Questions[0].Proof != nil {
		t.Fatal("blank proof block should normalize to nil")
	}
	// An old stored document without the key still parses.
	if _, err := ParseEvidence([]byte(`{"questions":[{"id":"q","kind":"yes_no","title":"x","required":true}],"photo":{"min":0,"max":0},"video":{"min":0,"max":0},"presence":"off"}`)); err != nil {
		t.Fatalf("legacy evidence: %v", err)
	}
}
