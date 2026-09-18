package domain

import (
	"strings"
	"testing"
	"time"
)

// Answer-driven branches (maintainer decision 2026-09-18, SOP studio phase 2). The rule in one
// sentence: a step with `when_answer` waits for its question, then runs or is skipped by the
// answer -- and a skipped step is off the path for every count and every gate.

func branchTrack() (FollowUpTrack, TaskTypeRegistry) {
	reg, _ := SeededTaskTypes()
	track := FollowUpTrack{Key: "test.branch", Module: "counts", Label: "Branch test", Steps: []FollowUpStep{
		{Key: "ready", TaskType: "record_yes_no", Title: "Is the animal ready?", Schedule: FollowUpSchedule{Kind: "immediately"}},
		{Key: "approve", TaskType: "do_and_confirm", Title: "Request supervisor approval", Schedule: FollowUpSchedule{Kind: "immediately"},
			WhenAnswer: &AnswerCondition{Step: "ready", Op: AnswerOpEq, Value: []string{"no"}}},
		{Key: "approve_note", TaskType: "record_text", Title: "Approval note", Schedule: FollowUpSchedule{Kind: "immediately"},
			WhenAnswer: &AnswerCondition{Step: "approve", Op: AnswerOpEq, Value: []string{"done"}}},
		{Key: "weight", TaskType: "record_number", Title: "Weight", Schedule: FollowUpSchedule{Kind: "immediately"}},
		{Key: "heavy", TaskType: "video_record", Title: "Film the heavy one", Proof: FollowUpProof{Video: 1}, Schedule: FollowUpSchedule{Kind: "immediately"},
			WhenAnswer: &AnswerCondition{Step: "weight", Op: AnswerOpGt, Value: []string{"30"}}},
		{Key: "finish", TaskType: "do_and_confirm", Title: "Record outcome", Schedule: FollowUpSchedule{Kind: "immediately"}},
	}}
	return track, reg
}

func TestValidateFollowUpChecksAnswerBranches(t *testing.T) {
	track, reg := branchTrack()
	// approve_note branches on a do_and_confirm step, which records no answer.
	problems := ValidateFollowUp(FollowUpDSL{SchemaVersion: "1", Tracks: []FollowUpTrack{track}}, reg)
	if len(problems) != 1 || !strings.Contains(problems[0], `"approve" records no answer`) {
		t.Fatalf("problems = %v", problems)
	}
	track.Steps[2].WhenAnswer = nil
	if problems := ValidateFollowUp(FollowUpDSL{SchemaVersion: "1", Tracks: []FollowUpTrack{track}}, reg); len(problems) != 0 {
		t.Fatalf("a well-formed branch set must validate: %v", problems)
	}
	cases := map[string]AnswerCondition{
		"later step":      {Step: "finish", Op: AnswerOpEq, Value: []string{"yes"}},
		"itself":          {Step: "approve", Op: AnswerOpEq, Value: []string{"yes"}},
		"not yes/no":      {Step: "ready", Op: AnswerOpEq, Value: []string{"maybe"}},
		"unknown op":      {Step: "ready", Op: "like", Value: []string{"yes"}},
		"no value":        {Step: "ready", Op: AnswerOpEq},
		"gt on yes/no":    {Step: "ready", Op: AnswerOpGt, Value: []string{"1"}},
		"gt two values":   {Step: "weight", Op: AnswerOpGt, Value: []string{"1", "2"}},
		"gt not a number": {Step: "weight", Op: AnswerOpGt, Value: []string{"heavy"}},
		"unknown option":  {Step: "kind", Op: AnswerOpIn, Value: []string{"cow"}},
		"eq non-number":   {Step: "weight", Op: AnswerOpEq, Value: []string{"x"}},
	}
	for name, c := range cases {
		tr := track
		tr.Steps = append([]FollowUpStep(nil), track.Steps...)
		tr.Steps = append(tr.Steps[:3], append([]FollowUpStep{{Key: "kind", TaskType: "record_select", Title: "Kind", Options: []string{"goat", "sheep"}, Schedule: FollowUpSchedule{Kind: "immediately"}}}, tr.Steps[3:]...)...)
		cc := c
		tr.Steps[1].WhenAnswer = &cc
		if problems := ValidateFollowUp(FollowUpDSL{SchemaVersion: "1", Tracks: []FollowUpTrack{tr}}, reg); len(problems) == 0 {
			t.Errorf("%s: must be refused", name)
		}
	}
}

func TestCompileTrackCarriesTheAnswerGate(t *testing.T) {
	track, reg := branchTrack()
	track.Steps[2].WhenAnswer = nil
	tmpl, err := CompileTrack(track, reg, CompileOptions{EventAt: time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Actions[1].AnswerGate == nil || tmpl.Actions[1].AnswerGate.Step != "ready" || tmpl.Actions[0].AnswerGate != nil {
		t.Fatalf("gates = %+v / %+v", tmpl.Actions[0].AnswerGate, tmpl.Actions[1].AnswerGate)
	}
}

func branchActions() []WorkflowAction {
	q := func(key string, seq int, gate *AnswerCondition, answerType string) WorkflowAction {
		a := WorkflowAction{ActionID: key + "-id", ActionKey: key, Seq: seq, Section: SectionMain, ActionType: ActionTypeAction, Status: ActionStatusPending, AnswerGate: gate, AnswerType: answerType}
		if answerType != "" && answerType != AnswerKindNone {
			a.ActionType = ActionTypeQuestion
		}
		return a
	}
	return []WorkflowAction{
		q("ready", 1, nil, AnswerKindYesNo),
		q("approve", 2, &AnswerCondition{Step: "ready", Op: AnswerOpEq, Value: []string{"no"}}, ""),
		q("approve_note", 3, &AnswerCondition{Step: "approve", Op: AnswerOpEq, Value: []string{"yes"}}, AnswerKindText),
		q("weight", 4, nil, AnswerKindNumber),
		q("heavy", 5, &AnswerCondition{Step: "weight", Op: AnswerOpGt, Value: []string{"30"}}, ""),
		q("finish", 6, nil, ""),
	}
}

func answered(a WorkflowAction, value string) WorkflowAction {
	a.Status = ActionStatusCompleted
	a.AnswerValue = &value
	return a
}

func TestAnswerBranchesGateSkipAndCascade(t *testing.T) {
	actions := branchActions()
	// Before the question is answered the gated step is blocked, and so is everything behind it
	// in the section; the question itself is open.
	if OperatorActionBlocked("test.branch", actions[0], actions) {
		t.Fatalf("the question must be open")
	}
	if !OperatorActionBlocked("test.branch", actions[1], actions) {
		t.Fatalf("a gated step must wait for its question")
	}
	// Answer YES: the "no" branch (approve) is skipped, and approve_note -- gated on approve --
	// goes with it. Nothing else moves.
	actions[0] = answered(actions[0], "yes")
	changed := ResolveAnswerBranches(actions[0], actions)
	keys := []string{}
	for _, c := range changed {
		keys = append(keys, c.ActionKey+":"+c.Status)
		actions[findKey(actions, c.ActionKey)] = c
	}
	if strings.Join(keys, ",") != "approve:skipped,approve_note:skipped" {
		t.Fatalf("changed = %v", keys)
	}
	// The skipped steps no longer block the next one, which is now open.
	if OperatorActionBlocked("test.branch", actions[3], actions) {
		t.Fatalf("weight must be open past the skipped branch")
	}
	// Answer weight 25: heavy (> 30) is skipped; finish is open.
	actions[3] = answered(actions[3], "25")
	changed = ResolveAnswerBranches(actions[3], actions)
	if len(changed) != 1 || changed[0].ActionKey != "heavy" || changed[0].Status != ActionStatusSkipped || changed[0].RowVersion != 1 {
		t.Fatalf("changed = %+v", changed)
	}
	actions[4] = changed[0]
	if OperatorActionBlocked("test.branch", actions[5], actions) {
		t.Fatalf("finish must be open")
	}
	// The card counts only the path taken: 3 of 6 remain, and once finish is done the workflow
	// completes although three rows were never completed.
	w := RecomputeCard(WorkflowInstance{TemplateKey: "test.branch", State: WorkflowStateOpen}, actions)
	if w.ActionsTotal != 3 || w.ActionsDone != 2 || w.NextActionKey == nil || *w.NextActionKey != "finish" {
		t.Fatalf("card = total %d done %d next %v", w.ActionsTotal, w.ActionsDone, w.NextActionKey)
	}
	actions[5].Status = ActionStatusCompleted
	if w := RecomputeCard(w, actions); w.State != WorkflowStateCompleted {
		t.Fatalf("workflow must complete past the skipped branch, state = %s", w.State)
	}
}

func TestAnswerBranchesTakenPathStaysPending(t *testing.T) {
	actions := branchActions()
	actions[0] = answered(actions[0], "no")
	if changed := ResolveAnswerBranches(actions[0], actions); len(changed) != 0 {
		t.Fatalf("a satisfied branch changes nothing: %+v", changed)
	}
	if OperatorActionBlocked("test.branch", actions[1], actions) {
		t.Fatalf("the taken branch must be open")
	}
	actions[3] = answered(actions[3], "42")
	if changed := ResolveAnswerBranches(actions[3], actions); len(changed) != 0 {
		t.Fatalf("42 > 30 keeps heavy on the path: %+v", changed)
	}
}

func TestAnswerConditionOperators(t *testing.T) {
	yes := AnswerCondition{Op: AnswerOpEq, Value: []string{"Yes"}}
	if !yes.Satisfied("yes") || yes.Satisfied("no") {
		t.Fatalf("eq is case-insensitive")
	}
	in := AnswerCondition{Op: AnswerOpIn, Value: []string{"bloat", "fever"}}
	if !in.Satisfied("cough|fever") || in.Satisfied("cough") {
		t.Fatalf("in matches any picked value of a pick-many answer")
	}
	notIn := AnswerCondition{Op: AnswerOpNotIn, Value: []string{"bloat"}}
	if !notIn.Satisfied("fever") || notIn.Satisfied("bloat|fever") {
		t.Fatalf("not_in")
	}
	lte := AnswerCondition{Op: AnswerOpLte, Value: []string{"30"}}
	if !lte.Satisfied("30") || lte.Satisfied("30.5") || lte.Satisfied("heavy") {
		t.Fatalf("lte")
	}
}

func findKey(actions []WorkflowAction, key string) int {
	for i, a := range actions {
		if a.ActionKey == key {
			return i
		}
	}
	return -1
}
