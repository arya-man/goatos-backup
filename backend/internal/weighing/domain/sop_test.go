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
	feed, okFeed := r.RemovalProof(RemovalProofFeed)
	water, okWater := r.RemovalProof(RemovalProofWater)
	if len(r.FeedWaterRemoval.Proofs) != 2 || !okFeed || !okWater || feed.Title == "" || water.Title == "" ||
		feed.Kind != RemovalProofKindVideo || water.Kind != RemovalProofKindVideo || !feed.Required || !water.Required {
		t.Fatalf("seeded proofs = %+v, want the feed and water clips: video, compulsory, titled", r.FeedWaterRemoval.Proofs)
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
		{"no compulsory capture", func(d *WeighingSOP) {
			d.FeedWaterRemoval.Proofs = []RemovalProofSlot{{Key: "gate_photo", Title: "Gate", Kind: "photo", Required: false}}
		}, "at least one compulsory capture"},
		{"unknown capture kind", func(d *WeighingSOP) { d.FeedWaterRemoval.Proofs[0].Kind = "audio" }, "is not video / photo / either"},
		{"duplicate slot key", func(d *WeighingSOP) {
			d.FeedWaterRemoval.Proofs = append(d.FeedWaterRemoval.Proofs, RemovalProofSlot{Key: "feed_video", Title: "Again", Kind: "photo"})
		}, "is listed twice"},
		{"bad slot key", func(d *WeighingSOP) { d.FeedWaterRemoval.Proofs[0].Key = "Feed Video" }, "must be a-z, 0-9 and _"},
		{"untitled slot", func(d *WeighingSOP) { d.FeedWaterRemoval.Proofs[1].Title = "" }, "title: required"},
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

// The slot list is the author's: a photo beside the videos, a video swapped for a photo, a slot
// dropped, an optional slot -- each validates, and a submit is judged by it. Mutation-tested by
// making ValidateRemovalProofRefs skip the Required check (the missing-compulsory case goes red).
func TestAuthoredProofSlotsShapeTheSubmit(t *testing.T) {
	r := SeededRules()
	r.FeedWaterRemoval.Proofs = []RemovalProofSlot{
		{Key: "feed_video", Title: "Feed removed", Kind: RemovalProofKindVideo, Required: true},
		{Key: "water_photo", Title: "Empty water trough", Kind: RemovalProofKindPhoto, Required: true},
		{Key: "gate", Title: "Gate closed", Kind: RemovalProofKindEither, Required: false},
	}
	if problems := ValidateWeighingSOP(r.WeighingSOP); len(problems) != 0 {
		t.Fatalf("a video + photo + optional either document must validate, got %v", problems)
	}
	proofErr := func(err error) *ProofError {
		var pe *ProofError
		if !errors.Is(err, ErrSOPProofInvalid) || !errors.As(err, &pe) {
			t.Fatalf("err = %v, want an ErrSOPProofInvalid naming the slot", err)
		}
		return pe
	}
	const a, b, c = "00000000-0000-4000-8000-00000000000a", "00000000-0000-4000-8000-00000000000b", "00000000-0000-4000-8000-00000000000c"
	if pe := proofErr(mustErr(r.ValidateRemovalProofRefs(RemovalProofRefs{"feed_video": a}))); pe.SlotKey != "water_photo" {
		t.Fatalf("missing compulsory photo -> %v, want water_photo", pe)
	}
	if pe := proofErr(mustErr(r.ValidateRemovalProofRefs(RemovalProofRefs{"feed_video": a, "water_photo": b, "roof": c}))); pe.SlotKey != "roof" {
		t.Fatalf("unknown slot -> %v, want roof", pe)
	}
	if pe := proofErr(mustErr(r.ValidateRemovalProofRefs(RemovalProofRefs{"feed_video": a, "water_photo": a}))); pe.SlotKey != "water_photo" {
		t.Fatalf("one capture in two slots -> %v, want water_photo", pe)
	}
	ordered, err := r.ValidateRemovalProofRefs(RemovalProofRefs{"gate": c, "feed_video": a, "water_photo": b})
	if err != nil || len(ordered) != 3 || ordered[0] != a || ordered[1] != b || ordered[2] != c {
		t.Fatalf("ordered refs = %v err %v, want [a b c] in SLOT order", ordered, err)
	}
	// The optional slot may be left empty; blanks are dropped on normalize.
	ordered, err = r.ValidateRemovalProofRefs(RemovalProofRefs{"feed_video": a, "water_photo": b, "gate": " "})
	if err != nil || len(ordered) != 2 {
		t.Fatalf("optional slot empty -> %v err %v, want two refs", ordered, err)
	}
	if n := NormalizeRemovalProofRefs(RemovalProofRefs{"feed_video": a, "gate": ""}); len(n) != 1 {
		t.Fatalf("normalize = %v, want the one real capture", n)
	}
	// Kind acceptance, per slot.
	if !r.FeedWaterRemoval.Proofs[2].Accepts("photo") || !r.FeedWaterRemoval.Proofs[2].Accepts("video") || r.FeedWaterRemoval.Proofs[1].Accepts("video") || r.FeedWaterRemoval.Proofs[0].Accepts("photo") {
		t.Fatal("either accepts both; a photo slot refuses video; a video slot refuses photo")
	}
}

func mustErr(_ []string, err error) error { return err }

// A slot published before `required` existed reads as COMPULSORY (the shape those documents
// meant); an explicit false stays optional. Found by the play-through: v2 on the proof clone
// 500'd every planner read once the flag was introduced.
func TestASlotWithoutTheRequiredFlagIsCompulsory(t *testing.T) {
	doc := map[string]any{"weighing": json.RawMessage(`{"schema_version":"goatos.sop-weighing.v1","planning":{"modes":["individual_animal"],"default_cap_per_day":100},"feed_water_removal":{"mode":"required","proofs":[{"key":"feed_video","title":"Feed","kind":"video"},{"key":"gate","title":"Gate","kind":"photo","required":false}],"questions":[]},"capture":{"individual":{"video_required":true},"lump_sum":{"video_min":1,"video_max":5}}}`)}
	dsl, err := ParseWeighingSOP(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !dsl.FeedWaterRemoval.Proofs[0].Required || dsl.FeedWaterRemoval.Proofs[1].Required {
		t.Fatalf("required flags = %v / %v, want absent=true, false=false", dsl.FeedWaterRemoval.Proofs[0].Required, dsl.FeedWaterRemoval.Proofs[1].Required)
	}
	if problems := ValidateWeighingSOP(dsl); len(problems) != 0 {
		t.Fatalf("a pre-flag document must still validate, got %v", problems)
	}
}

// A misspelt key is refused at save by name; the lenient parser would otherwise drop it and
// the farm would run on the farm evening believing it authored its own.
func TestUnknownWeighingSOPKeysAreNamedByPath(t *testing.T) {
	doc := map[string]any{"weighing": map[string]any{
		"schema_version": WeighingSOPSchemaVersion,
		"planning":       map[string]any{"modes": []any{"individual_animal"}, "default_cap_per_day": 100},
		"feed_water_removal": map[string]any{"mode": "optional", "cutoff_tme": "21:30",
			"proofs":    []any{map[string]any{"key": "feed_video", "title": "Feed", "kind": "video", "requried": true}},
			"questions": []any{map[string]any{"id": "q1", "kind": "text", "title": "T", "hnt": "x"}}},
		"capture": map[string]any{"individual": map[string]any{"video_required": true}, "lump_sum": map[string]any{"video_min": 1, "video_max": 5, "audio": true}},
		"bonus":   1,
	}}
	got := UnknownWeighingSOPKeys(doc)
	want := []string{"bonus", "capture.lump_sum.audio", "feed_water_removal.cutoff_tme", "feed_water_removal.proofs.0.requried", "feed_water_removal.questions.0.hnt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unknown keys = %v, want %v", got, want)
	}
	var seed map[string]any
	if err := json.Unmarshal(SeededWeighingSOPJSON(), &seed); err != nil {
		t.Fatal(err)
	}
	if keys := UnknownWeighingSOPKeys(map[string]any{"weighing": seed}); len(keys) != 0 {
		t.Fatalf("the seed must carry no unknown key, got %v", keys)
	}
}
