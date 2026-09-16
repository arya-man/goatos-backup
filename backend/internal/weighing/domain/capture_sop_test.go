package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// WEIGHING SOP -- the weigh captures are authored (maintainer decision 2026-09-16). These pin
// the domain half: the seeded slots ARE today's behaviour, a document without slot lists keeps
// working, the submit is judged slot by slot, and the two capture sections stay separate.

func TestSeededCaptureSlotsAreTodaysBehaviour(t *testing.T) {
	r := SeededRules()
	// The seed file is untouched (migration 000315 embeds it verbatim): no explicit slot lists.
	if r.Capture.Individual.Proofs != nil || r.Capture.LumpSum.Proofs != nil {
		t.Fatalf("seed must carry no explicit slot list; got %v / %v", r.Capture.Individual.Proofs, r.Capture.LumpSum.Proofs)
	}
	ind := r.IndividualProofs()
	if len(ind) != 1 || ind[0].Key != IndividualProofAnimalVideo || ind[0].Kind != RemovalProofKindVideo || !ind[0].Required || ind[0].Title != "Weighing video" {
		t.Fatalf("individual defaults = %+v, want one compulsory 'Weighing video' video slot %q", ind, IndividualProofAnimalVideo)
	}
	lump := r.LumpSumProofs()
	if len(lump) != 1 || lump[0].Key != LumpSumProofPenVideo || lump[0].Kind != RemovalProofKindVideo || lump[0].Min != 1 || lump[0].Max != 5 || lump[0].Title != "Weighing video" {
		t.Fatalf("lump-sum defaults = %+v, want one 'Weighing video' video slot %q 1..5", lump, LumpSumProofPenVideo)
	}
	if len(r.IndividualQuestions()) != 0 || len(r.LumpSumQuestions()) != 0 || r.IndividualQuestions() == nil || r.LumpSumQuestions() == nil {
		t.Fatal("seed asks no questions on either capture, and the accessors are never nil")
	}
	served := r.ServedRules()
	if len(served.Capture.Individual.Proofs) != 1 || len(served.Capture.LumpSum.Proofs) != 1 || served.Capture.LumpSum.VideoMin != 1 || served.Capture.LumpSum.VideoMax != 5 {
		t.Fatalf("served rules must fill both sections explicitly and keep the 1..5 mirror: %+v", served.Capture)
	}
	if problems := ValidateWeighingSOP(served.WeighingSOP); len(problems) != 0 {
		t.Fatalf("served seed must validate: %v", problems)
	}
}

func TestAPublishedLumpSumWindowWithoutSlotsBecomesThePenVideoSlot(t *testing.T) {
	r := SeededRules()
	r.Capture.LumpSum.VideoMin, r.Capture.LumpSum.VideoMax = 2, 3
	lump := r.LumpSumProofs()
	if len(lump) != 1 || lump[0].Key != LumpSumProofPenVideo || lump[0].Min != 2 || lump[0].Max != 3 {
		t.Fatalf("window 2..3 without slots = %+v, want the pen_video slot 2..3", lump)
	}
	// Explicit slots: the mirror is derived from the video/either slots, clamped to 1..5.
	r.Capture.LumpSum.Proofs = []CountedProofSlot{
		{Key: "pen_video", Title: "Weighing video", Kind: RemovalProofKindVideo, Min: 1, Max: 4},
		{Key: "scale_photo", Title: "Scale display photo", Kind: RemovalProofKindPhoto, Min: 1, Max: 1},
		{Key: "gate", Title: "Gate", Kind: RemovalProofKindEither, Min: 0, Max: 3},
	}
	served := r.ServedRules()
	if served.Capture.LumpSum.VideoMin != 1 || served.Capture.LumpSum.VideoMax != 5 {
		t.Fatalf("mirror = %d..%d, want 1..5 (sum 4+3 clamped to the older clients' ceiling)", served.Capture.LumpSum.VideoMin, served.Capture.LumpSum.VideoMax)
	}
}

func TestValidateCaptureSlotsRefusesByPath(t *testing.T) {
	base := func() WeighingSOP { return SeededRules().WeighingSOP }
	cases := []struct {
		name string
		mut  func(*WeighingSOP)
		want string
	}{
		{"video_required still locked", func(d *WeighingSOP) { d.Capture.Individual.VideoRequired = false }, "weighing.capture.individual.video_required"},
		{"individual empty slot list", func(d *WeighingSOP) { d.Capture.Individual.Proofs = []RemovalProofSlot{} }, "weighing.capture.individual.proofs: 1.."},
		{"individual fifth slot", func(d *WeighingSOP) {
			for i := 0; i < 5; i++ {
				d.Capture.Individual.Proofs = append(d.Capture.Individual.Proofs, RemovalProofSlot{Key: "s" + string(rune('a'+i)), Title: "S", Kind: "video", Required: true})
			}
		}, "weighing.capture.individual.proofs: at most 4"},
		{"individual no compulsory slot", func(d *WeighingSOP) {
			d.Capture.Individual.Proofs = []RemovalProofSlot{{Key: "a", Title: "A", Kind: "photo", Required: false}}
		}, "weighing.capture.individual.proofs: at least one compulsory capture per animal"},
		{"individual bad key", func(d *WeighingSOP) {
			d.Capture.Individual.Proofs = []RemovalProofSlot{{Key: "Bad Key", Title: "A", Kind: "video", Required: true}}
		}, "weighing.capture.individual.proofs.0.key"},
		{"individual duplicate key", func(d *WeighingSOP) {
			d.Capture.Individual.Proofs = []RemovalProofSlot{{Key: "a", Title: "A", Kind: "video", Required: true}, {Key: "a", Title: "B", Kind: "photo", Required: false}}
		}, "weighing.capture.individual.proofs.1.key"},
		{"individual unknown kind", func(d *WeighingSOP) {
			d.Capture.Individual.Proofs = []RemovalProofSlot{{Key: "a", Title: "A", Kind: "audio", Required: true}}
		}, "weighing.capture.individual.proofs.0.kind"},
		{"individual empty title", func(d *WeighingSOP) {
			d.Capture.Individual.Proofs = []RemovalProofSlot{{Key: "a", Title: " ", Kind: "video", Required: true}}
		}, "weighing.capture.individual.proofs.0.title"},
		{"individual bad question", func(d *WeighingSOP) {
			d.Capture.Individual.Questions = []SOPQuestion{{ID: "q1", Kind: "choice", Title: "Q"}}
		}, "weighing.capture.individual.questions.0.options"},
		{"lump empty slot list", func(d *WeighingSOP) { d.Capture.LumpSum.Proofs = []CountedProofSlot{} }, "weighing.capture.lump_sum.proofs: 1.."},
		{"lump min above max", func(d *WeighingSOP) {
			d.Capture.LumpSum.Proofs = []CountedProofSlot{{Key: "a", Title: "A", Kind: "video", Min: 3, Max: 2}}
		}, "weighing.capture.lump_sum.proofs.0.min"},
		{"lump max above five", func(d *WeighingSOP) {
			d.Capture.LumpSum.Proofs = []CountedProofSlot{{Key: "a", Title: "A", Kind: "video", Min: 1, Max: 6}}
		}, "weighing.capture.lump_sum.proofs.0.max"},
		{"lump max zero", func(d *WeighingSOP) {
			d.Capture.LumpSum.Proofs = []CountedProofSlot{{Key: "a", Title: "A", Kind: "video", Min: 0, Max: 0}}
		}, "weighing.capture.lump_sum.proofs.0.max"},
		{"lump no compulsory slot", func(d *WeighingSOP) {
			d.Capture.LumpSum.Proofs = []CountedProofSlot{{Key: "a", Title: "A", Kind: "video", Min: 0, Max: 2}}
		}, "weighing.capture.lump_sum.proofs: at least one compulsory capture"},
		{"lump total above ten", func(d *WeighingSOP) {
			d.Capture.LumpSum.Proofs = []CountedProofSlot{{Key: "a", Title: "A", Kind: "video", Min: 1, Max: 5}, {Key: "b", Title: "B", Kind: "photo", Min: 1, Max: 5}, {Key: "c", Title: "C", Kind: "either", Min: 0, Max: 1}}
		}, "weighing.capture.lump_sum.proofs: at most 10 captures"},
		{"lump bad question", func(d *WeighingSOP) {
			d.Capture.LumpSum.Questions = []SOPQuestion{{ID: "q1", Kind: "number", Title: "Q", Min: f(5), Max: f(1)}}
		}, "weighing.capture.lump_sum.questions.0.min"},
		{"legacy window still judged without slots", func(d *WeighingSOP) { d.Capture.LumpSum.VideoMax = 6 }, "weighing.capture.lump_sum.video_max"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := base()
			tc.mut(&d)
			problems := ValidateWeighingSOP(d)
			found := false
			for _, p := range problems {
				if strings.HasPrefix(p, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("problems = %v, want one starting %q", problems, tc.want)
			}
		})
	}
	// Explicit slots: the legacy window is NOT judged (the slots are the truth) -- a document
	// carrying slots and a stale video_max 6 validates.
	d := base()
	d.Capture.LumpSum.Proofs = []CountedProofSlot{{Key: "pen_video", Title: "Weighing video", Kind: "video", Min: 1, Max: 5}}
	d.Capture.LumpSum.VideoMax = 6
	if problems := ValidateWeighingSOP(d); len(problems) != 0 {
		t.Fatalf("explicit slots must make the legacy window a mirror only: %v", problems)
	}
	// Questions-only alongside the compulsory slot is fine on both sections.
	d = base()
	d.Capture.Individual.Questions = []SOPQuestion{{ID: "limp", Kind: SOPQuestionChoice, Title: "Limping?", Required: true, Options: []SOPOption{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}}}
	d.Capture.LumpSum.Questions = []SOPQuestion{{ID: "all_in", Kind: SOPQuestionChoice, Title: "Every animal on the scale?", Required: true, Options: []SOPOption{{Value: "yes", Label: "Yes"}}}}
	if problems := ValidateWeighingSOP(d); len(problems) != 0 {
		t.Fatalf("questions beside the seeded slot must validate: %v", problems)
	}
}

func TestUnknownWeighingSOPKeysCoversCaptureSlots(t *testing.T) {
	doc := map[string]any{"weighing": map[string]any{
		"capture": map[string]any{
			"individual": map[string]any{"video_required": true, "proofs": []any{map[string]any{"key": "a", "title": "A", "kind": "video", "required": true, "hnt": "x"}}, "questions": []any{map[string]any{"id": "q", "kind": "text", "title": "Q", "requird": true}}},
			"lump_sum":   map[string]any{"video_min": 1, "video_max": 5, "proofs": []any{map[string]any{"key": "p", "title": "P", "kind": "video", "min": 1, "max": 5, "count": 2}}, "questions": []any{}},
		},
	}}
	got := UnknownWeighingSOPKeys(doc)
	want := []string{"capture.individual.proofs.0.hnt", "capture.individual.questions.0.requird", "capture.lump_sum.proofs.0.count"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unknown keys = %v, want %v", got, want)
	}
	// The known slot/question keys are not findings.
	doc = map[string]any{"weighing": map[string]any{"capture": map[string]any{
		"individual": map[string]any{"proofs": []any{map[string]any{"key": "a", "title": "A", "hint": "h", "kind": "video", "required": true}}, "questions": []any{}},
		"lump_sum":   map[string]any{"proofs": []any{map[string]any{"key": "p", "title": "P", "hint": "h", "kind": "video", "min": 1, "max": 5}}, "questions": []any{}},
	}}}
	if got := UnknownWeighingSOPKeys(doc); len(got) != 0 {
		t.Fatalf("known capture keys reported: %v", got)
	}
}

func TestIndividualProofRefsJudgedSlotBySlot(t *testing.T) {
	r := SeededRules()
	r.Capture.Individual.Proofs = []RemovalProofSlot{
		{Key: "animal_video", Title: "Weighing video", Kind: "video", Required: true},
		{Key: "scale_photo", Title: "Scale display", Kind: "photo", Required: true},
		{Key: "ear_tag", Title: "Ear tag close-up", Kind: "either", Required: false},
	}
	ordered, err := r.ValidateIndividualProofRefs(IndividualProofRefs{"animal_video": "v1", "scale_photo": "p1"})
	if err != nil || strings.Join(ordered, ",") != "v1,p1" {
		t.Fatalf("ordered = %v err = %v, want v1,p1 (slot order, primary first)", ordered, err)
	}
	var pe *ProofError
	_, err = r.ValidateIndividualProofRefs(IndividualProofRefs{"animal_video": "v1"})
	if !errors.Is(err, ErrCaptureProofInvalid) || !errors.As(err, &pe) || pe.SlotKey != "scale_photo" {
		t.Fatalf("missing compulsory slot err = %v, want ErrCaptureProofInvalid naming scale_photo", err)
	}
	if errors.Is(err, ErrSOPProofInvalid) {
		t.Fatal("a weigh capture error must not read as the removal card's error")
	}
	_, err = r.ValidateIndividualProofRefs(IndividualProofRefs{"animal_video": "v1", "scale_photo": "p1", "hoof": "x"})
	if !errors.As(err, &pe) || pe.SlotKey != "hoof" {
		t.Fatalf("unknown slot err = %v, want naming hoof", err)
	}
	_, err = r.ValidateIndividualProofRefs(IndividualProofRefs{"animal_video": "v1", "scale_photo": "v1"})
	if !errors.As(err, &pe) || pe.SlotKey != "scale_photo" {
		t.Fatalf("one capture in two slots err = %v, want naming scale_photo", err)
	}
	// The seeded document judges the legacy shape: the single primary ref on the seeded slot.
	legacy := LegacyIndividualRefs(r, "v9")
	if legacy["animal_video"] != "v9" || len(legacy) != 1 {
		t.Fatalf("legacy refs = %v, want {animal_video: v9}", legacy)
	}
	if kinds := r.IndividualSlotKinds(); kinds["animal_video"] != "video" || kinds["scale_photo"] != "photo" || kinds["ear_tag"] != "either" {
		t.Fatalf("slot kinds = %v", kinds)
	}
}

func TestLumpSumProofRefsJudgedByCount(t *testing.T) {
	r := SeededRules()
	r.Capture.LumpSum.Proofs = []CountedProofSlot{
		{Key: "pen_video", Title: "Weighing video", Kind: "video", Min: 1, Max: 3},
		{Key: "scale_photo", Title: "Scale display photo", Kind: "photo", Min: 1, Max: 1},
		{Key: "extra", Title: "Extra", Kind: "either", Min: 0, Max: 2},
	}
	ordered, err := r.ValidateLumpSumProofRefs(LumpSumProofRefs{"pen_video": {"v1", "v2"}, "scale_photo": {"p1"}})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := []string{}
	for _, c := range ordered {
		got = append(got, c.SlotKey+":"+c.Ref+":"+itoa(c.Index)+"/"+itoa(c.Count))
	}
	if strings.Join(got, ",") != "pen_video:v1:1/2,pen_video:v2:2/2,scale_photo:p1:1/1" {
		t.Fatalf("ordered = %v", got)
	}
	var pe *ProofError
	_, err = r.ValidateLumpSumProofRefs(LumpSumProofRefs{"pen_video": {"v1", "v2", "v3", "v4"}, "scale_photo": {"p1"}})
	if !errors.Is(err, ErrCaptureProofInvalid) || !errors.As(err, &pe) || pe.SlotKey != "pen_video" || !strings.Contains(pe.Message, "3") {
		t.Fatalf("four in a max-3 slot err = %v, want naming pen_video and the 3", err)
	}
	_, err = r.ValidateLumpSumProofRefs(LumpSumProofRefs{"pen_video": {"v1"}})
	if !errors.As(err, &pe) || pe.SlotKey != "scale_photo" {
		t.Fatalf("missing compulsory photo err = %v, want naming scale_photo", err)
	}
	_, err = r.ValidateLumpSumProofRefs(LumpSumProofRefs{"pen_video": {"v1", "v1"}, "scale_photo": {"p1"}})
	if !errors.As(err, &pe) || pe.SlotKey != "pen_video" {
		t.Fatalf("same ref twice err = %v, want naming pen_video", err)
	}
	_, err = r.ValidateLumpSumProofRefs(LumpSumProofRefs{"pen_video": {"v1"}, "scale_photo": {"p1"}, "nope": {"x"}})
	if !errors.As(err, &pe) || pe.SlotKey != "nope" {
		t.Fatalf("unknown slot err = %v", err)
	}
	// Labels: a slot carrying one capture is titled plainly; several are numbered.
	if CaptureMediaLabel("Weighing video", 1, 1) != "Weighing video" || CaptureMediaLabel("Weighing video", 2, 3) != "Weighing video 2 of 3" {
		t.Fatal("capture media label")
	}
	// The seeded document: the legacy list lands on the pen_video slot.
	seed := SeededRules()
	legacy := LegacyLumpSumRefs(seed, []string{"a", "b"})
	if strings.Join(legacy["pen_video"], ",") != "a,b" || len(legacy) != 1 {
		t.Fatalf("legacy lump refs = %v", legacy)
	}
}

func itoa(n int) string { return string(rune('0' + n)) }

func TestCaptureAnswerRowsInFarmWords(t *testing.T) {
	r := SeededRules()
	r.Capture.Individual.Questions = []SOPQuestion{
		{ID: "limp", Kind: SOPQuestionChoice, Title: "Limping?", Required: true, Options: []SOPOption{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}},
		{ID: "note", Kind: SOPQuestionText, Title: "Note", OnlyIf: &SOPCondition{QuestionID: "limp", Value: "yes"}},
	}
	r.Capture.LumpSum.Questions = []SOPQuestion{
		{ID: "count_seen", Kind: SOPQuestionNumber, Title: "Animals seen", Required: true, Unit: "head"},
	}
	if err := r.ValidateIndividualAnswers(SOPAnswers{}); !errors.Is(err, ErrCaptureAnswerInvalid) {
		t.Fatalf("missing required individual answer err = %v, want ErrCaptureAnswerInvalid", err)
	}
	if err := r.ValidateIndividualAnswers(SOPAnswers{"count_seen": json.RawMessage(`3`)}); !errors.Is(err, ErrCaptureAnswerInvalid) {
		t.Fatal("a whole-pen question is not an animal question: the two sections are separate")
	}
	a := SOPAnswers{"limp": json.RawMessage(`"yes"`), "note": json.RawMessage(`"left fore"`)}
	if err := r.ValidateIndividualAnswers(a); err != nil {
		t.Fatalf("err = %v", err)
	}
	rows := r.IndividualAnswerRows(r.NormalizeIndividualAnswers(a))
	if len(rows) != 2 || rows[0].Label != "Limping?" || rows[0].Value != "Yes" || rows[1].Value != "left fore" {
		t.Fatalf("rows = %+v", rows)
	}
	if err := r.ValidateLumpSumAnswers(SOPAnswers{"limp": json.RawMessage(`"yes"`)}); !errors.Is(err, ErrCaptureAnswerInvalid) {
		t.Fatal("an animal question is not a whole-pen question")
	}
	la := SOPAnswers{"count_seen": json.RawMessage(`12`)}
	if err := r.ValidateLumpSumAnswers(la); err != nil {
		t.Fatalf("err = %v", err)
	}
	if rows := r.LumpSumAnswerRows(la); len(rows) != 1 || rows[0].Value != "12 head" {
		t.Fatalf("lump rows = %+v", rows)
	}
	// Removal helpers still delegate to the same engine (no behaviour change).
	if err := r.ValidateRemovalAnswers(SOPAnswers{"limp": json.RawMessage(`"yes"`)}); !errors.Is(err, ErrSOPAnswerInvalid) {
		t.Fatal("removal keeps its own sentinel and its own question list")
	}
}

func TestLegacyRefsMapOntoSeededSlots(t *testing.T) {
	// A version that RENAMED the seeded slot: the legacy single video lands on the first
	// compulsory video slot; the other compulsory items are reported as not captured.
	r := SeededRules()
	r.Capture.Individual.Proofs = []RemovalProofSlot{
		{Key: "scale_photo", Title: "Scale display", Kind: "photo", Required: true},
		{Key: "weigh_clip", Title: "Weigh clip", Kind: "video", Required: true},
	}
	r.Capture.Individual.Questions = []SOPQuestion{{ID: "limp", Kind: SOPQuestionText, Title: "Limping?", Required: true}}
	refs := LegacyIndividualRefs(r, "v1")
	if refs["weigh_clip"] != "v1" || len(refs) != 1 {
		t.Fatalf("legacy refs = %v, want {weigh_clip: v1}", refs)
	}
	missing := r.IndividualNotCapturedRows(refs, nil)
	if len(missing) != 2 || missing[0].Label != "Scale display" || missing[0].Value != NotCapturedOlderApp || missing[1].Label != "Limping?" {
		t.Fatalf("not-captured rows = %+v", missing)
	}
	// Nothing missing on the seed: no rows.
	if rows := SeededRules().IndividualNotCapturedRows(LegacyIndividualRefs(SeededRules(), "v1"), nil); len(rows) != 0 {
		t.Fatalf("seed not-captured rows = %+v, want none", rows)
	}
	// Lump sum: renamed slot + an extra compulsory photo.
	l := SeededRules()
	l.Capture.LumpSum.Proofs = []CountedProofSlot{
		{Key: "pen_clip", Title: "Pen clip", Kind: "video", Min: 1, Max: 5},
		{Key: "scale_photo", Title: "Scale photo", Kind: "photo", Min: 1, Max: 1},
	}
	lrefs := LegacyLumpSumRefs(l, []string{"a"})
	if strings.Join(lrefs["pen_clip"], ",") != "a" || len(lrefs) != 1 {
		t.Fatalf("legacy lump refs = %v", lrefs)
	}
	if rows := l.LumpSumNotCapturedRows(lrefs, nil); len(rows) != 1 || rows[0].Label != "Scale photo" {
		t.Fatalf("lump not-captured rows = %+v", rows)
	}
	// A version with NO video-accepting compulsory slot cannot take a legacy video at all.
	p := SeededRules()
	p.Capture.Individual.Proofs = []RemovalProofSlot{{Key: "scale_photo", Title: "Scale display", Kind: "photo", Required: true}}
	if refs := LegacyIndividualRefs(p, "v1"); len(refs) != 0 {
		t.Fatalf("photo-only version must not take a legacy video: %v", refs)
	}
}
