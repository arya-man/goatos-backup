package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The seeded document IS the pre-SOP behaviour: removal required on every task, both clips,
// no questions, cap 100, 1..5 lump-sum videos, both capture modes. Mutation-tested by editing
// the seed's mode to "optional" (goes red).
func TestSeededWeighingSOPIsThePreSOPBehaviour(t *testing.T) {
	r := SeededRules()
	if r.Version != 0 {
		t.Fatalf("seeded version = %d, want 0", r.Version)
	}
	if r.FeedWaterRemoval.Mode != RemovalModeRequired {
		t.Fatalf("seeded removal mode = %q, want required (the 2026-09-03 rule)", r.FeedWaterRemoval.Mode)
	}
	if !r.RemovalApplies(nil) || !r.RemovalApplies(boolPtr(false)) {
		t.Fatal("under required the planner cannot opt out")
	}
	if len(r.FeedWaterRemoval.Proofs) != 2 || r.RemovalProof(RemovalProofFeed).Title == "" || r.RemovalProof(RemovalProofWater).Title == "" {
		t.Fatalf("seeded proofs = %+v, want the feed and water clips with titles", r.FeedWaterRemoval.Proofs)
	}
	if len(r.FeedWaterRemoval.Questions) != 0 {
		t.Fatal("the seed asks no removal questions")
	}
	if r.Planning.DefaultCapPerDay != 100 {
		t.Fatalf("default cap = %d, want 100 (the literal the service carried)", r.Planning.DefaultCapPerDay)
	}
	if !r.ModeAllowed(CategoryIndividualAnimal) || !r.ModeAllowed(CategoryPerShedPartition) {
		t.Fatal("both capture modes are offered day one")
	}
	if r.Capture.LumpSum.VideoMin != 1 || r.Capture.LumpSum.VideoMax != MaxShedProofArtifacts {
		t.Fatalf("lump-sum window = %d..%d, want 1..%d", r.Capture.LumpSum.VideoMin, r.Capture.LumpSum.VideoMax, MaxShedProofArtifacts)
	}
	if !r.Capture.Individual.VideoRequired {
		t.Fatal("per-animal video is locked on")
	}
}

func TestValidateWeighingSOPRefusesTheLockedRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*WeighingSOP)
		want   string
	}{
		{"per-animal video off", func(d *WeighingSOP) { d.Capture.Individual.VideoRequired = false }, "video_required: fixed to true"},
		{"lump-sum max over policy", func(d *WeighingSOP) { d.Capture.LumpSum.VideoMax = MaxShedProofArtifacts + 1 }, "video_max: 1.."},
		{"lump-sum min zero", func(d *WeighingSOP) { d.Capture.LumpSum.VideoMin = 0 }, "video_min: at least 1"},
		{"lump-sum min over max", func(d *WeighingSOP) { d.Capture.LumpSum.VideoMin = 4; d.Capture.LumpSum.VideoMax = 3 }, "must not exceed video_max"},
		{"unknown removal mode", func(d *WeighingSOP) { d.FeedWaterRemoval.Mode = "sometimes" }, "mode: \"sometimes\""},
		{"water slot missing", func(d *WeighingSOP) { d.FeedWaterRemoval.Proofs = d.FeedWaterRemoval.Proofs[:1] }, "\"water_video\" must be present"},
		{"a third slot", func(d *WeighingSOP) {
			d.FeedWaterRemoval.Proofs = append(d.FeedWaterRemoval.Proofs, RemovalProofSlot{Key: "gate_video", Title: "Gate", Kind: "video"})
		}, "carries exactly feed_video and water_video"},
		{"photo slot", func(d *WeighingSOP) { d.FeedWaterRemoval.Proofs[0].Kind = "photo" }, "removal proof is a live-camera video"},
		{"no capture mode", func(d *WeighingSOP) { d.Planning.Modes = nil }, "at least one capture mode"},
		{"unknown capture mode", func(d *WeighingSOP) { d.Planning.Modes = []string{"by_truck"} }, "is not a capture mode"},
		{"cap zero", func(d *WeighingSOP) { d.Planning.DefaultCapPerDay = 0 }, "default_cap_per_day: 1..10000"},
		{"question kind", func(d *WeighingSOP) {
			d.FeedWaterRemoval.Questions = []SOPQuestion{{ID: "q1", Kind: "media", Title: "Photo"}}
		}, "is not a question kind"},
		{"choice without options", func(d *WeighingSOP) {
			d.FeedWaterRemoval.Questions = []SOPQuestion{{ID: "q1", Kind: SOPQuestionChoice, Title: "Which"}}
		}, "needs at least one choice"},
		{"only_if on a later question", func(d *WeighingSOP) {
			d.FeedWaterRemoval.Questions = []SOPQuestion{
				{ID: "q1", Kind: SOPQuestionText, Title: "Note", OnlyIf: &SOPCondition{QuestionID: "q2", Value: "yes"}},
				{ID: "q2", Kind: SOPQuestionChoice, Title: "Done", Options: []SOPOption{{"yes", "Yes"}, {"no", "No"}}},
			}
		}, "must be an EARLIER question"},
		{"reserved _other id", func(d *WeighingSOP) {
			d.FeedWaterRemoval.Questions = []SOPQuestion{{ID: "q1_other", Kind: SOPQuestionText, Title: "x"}}
		}, "_other suffix is reserved"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := SeededRules().WeighingSOP
			d.FeedWaterRemoval.Proofs = append([]RemovalProofSlot(nil), d.FeedWaterRemoval.Proofs...)
			tc.mutate(&d)
			problems := ValidateWeighingSOP(d)
			if len(problems) == 0 {
				t.Fatalf("expected a problem containing %q, document accepted", tc.want)
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("problems = %v, want one containing %q", problems, tc.want)
			}
		})
	}
	if problems := ValidateWeighingSOP(SeededRules().WeighingSOP); len(problems) != 0 {
		t.Fatalf("seed must validate clean, got %v", problems)
	}
}

func TestRemovalAppliesFollowsTheMode(t *testing.T) {
	rules := func(mode string) Rules {
		r := SeededRules()
		r.FeedWaterRemoval.Mode = mode
		return r
	}
	tt := []struct {
		mode      string
		requested *bool
		want      bool
	}{
		{RemovalModeRequired, nil, true}, {RemovalModeRequired, boolPtr(false), true},
		{RemovalModeOff, nil, false}, {RemovalModeOff, boolPtr(true), false},
		{RemovalModeOptional, nil, true}, {RemovalModeOptional, boolPtr(true), true}, {RemovalModeOptional, boolPtr(false), false},
	}
	for _, tc := range tt {
		if got := rules(tc.mode).RemovalApplies(tc.requested); got != tc.want {
			t.Fatalf("mode %s requested %v: applies = %v, want %v", tc.mode, tc.requested, got, tc.want)
		}
	}
}

func TestValidateRemovalAnswersJudgesByTheDocument(t *testing.T) {
	r := SeededRules()
	r.FeedWaterRemoval.Questions = []SOPQuestion{
		{ID: "all_pens", Kind: SOPQuestionChoice, Title: "Every pen emptied?", Required: true, Options: []SOPOption{{"yes", "Yes"}, {"no", "No"}, {"other", "Other"}}, AllowOther: true},
		{ID: "why_not", Kind: SOPQuestionText, Title: "Why not", Required: true, OnlyIf: &SOPCondition{QuestionID: "all_pens", Value: "no"}},
		{ID: "buckets", Kind: SOPQuestionNumber, Title: "Water buckets removed", Min: f(0), Max: f(50)},
		{ID: "issues", Kind: SOPQuestionMulti, Title: "Issues", Options: []SOPOption{{"leak", "Leak"}, {"broken", "Broken trough"}}},
	}
	raw := func(s string) SOPAnswers {
		var a SOPAnswers
		if err := json.Unmarshal([]byte(s), &a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	answerErr := func(err error) *AnswerError {
		var ae *AnswerError
		if !errors.Is(err, ErrSOPAnswerInvalid) || !errors.As(err, &ae) {
			t.Fatalf("err = %v, want an ErrSOPAnswerInvalid naming the question", err)
		}
		return ae
	}
	if ae := answerErr(r.ValidateRemovalAnswers(raw(`{}`))); ae.QuestionID != "all_pens" {
		t.Fatalf("missing required -> %v, want all_pens", ae)
	}
	if ae := answerErr(r.ValidateRemovalAnswers(raw(`{"all_pens":"maybe"}`))); ae.QuestionID != "all_pens" {
		t.Fatalf("off-list choice -> %v", ae)
	}
	if ae := answerErr(r.ValidateRemovalAnswers(raw(`{"all_pens":"other"}`))); ae.QuestionID != "all_pens" {
		t.Fatalf("other without text -> %v", ae)
	}
	if ae := answerErr(r.ValidateRemovalAnswers(raw(`{"all_pens":"no"}`))); ae.QuestionID != "why_not" {
		t.Fatalf("conditional required -> %v, want why_not", ae)
	}
	if ae := answerErr(r.ValidateRemovalAnswers(raw(`{"all_pens":"yes","buckets":99}`))); ae.QuestionID != "buckets" {
		t.Fatalf("number out of range -> %v", ae)
	}
	if ae := answerErr(r.ValidateRemovalAnswers(raw(`{"all_pens":"yes","issues":["fire"]}`))); ae.QuestionID != "issues" {
		t.Fatalf("off-list multi -> %v", ae)
	}
	if ae := answerErr(r.ValidateRemovalAnswers(raw(`{"all_pens":"yes","gate":"shut"}`))); ae.QuestionID != "gate" {
		t.Fatalf("unknown question -> %v", ae)
	}
	// A conditional whose condition fails is skipped on validation and DROPPED on normalize.
	ok := raw(`{"all_pens":"yes","why_not":"stale","buckets":"12","issues":["leak"]}`)
	if err := r.ValidateRemovalAnswers(ok); err != nil {
		t.Fatalf("valid answers refused: %v", err)
	}
	norm := r.NormalizeRemovalAnswers(ok)
	if _, kept := norm["why_not"]; kept {
		t.Fatal("an answer to a question whose condition failed must be dropped")
	}
	if len(norm) != 3 {
		t.Fatalf("normalized = %v, want the three applicable answers", norm)
	}
	// The seed asks nothing: any answer is unknown, and an empty map is fine.
	if err := SeededRules().ValidateRemovalAnswers(SOPAnswers{}); err != nil {
		t.Fatal(err)
	}
	if err := SeededRules().ValidateRemovalAnswers(raw(`{"all_pens":"yes"}`)); err == nil {
		t.Fatal("the seed asks no question, so an answer must be refused")
	}
}

func boolPtr(b bool) *bool { return &b }
func f(v float64) *float64 { return &v }
