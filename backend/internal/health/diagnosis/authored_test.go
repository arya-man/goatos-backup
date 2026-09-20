package diagnosis

import (
	"strings"
	"testing"
)

func f64(v float64) *float64 { return &v }

// minimal builds a register that validates cleanly, so each test below can break
// exactly one thing and prove that one thing is what gets refused.
func minimal() *AuthoredRegister {
	return &AuthoredRegister{
		RegisterVersion: "test-1",
		AppliesClass:    []string{ClassAdult},
		Questions: []Question{{
			ID:    "nasal",
			Kind:  QuestionChoice,
			Title: "Nasal discharge",
			Options: []Option{
				{Value: "no", Label: "No"},
				{Value: "yes", Label: "Yes", Emits: []string{"nasal_discharge"}},
			},
		}},
		Rules: []Rule{{
			ID:            "FEVER",
			Pathognomonic: []Clause{{Findings: []string{"nasal_discharge"}}},
			SeverityBase:  3,
		}},
	}
}

func fatalPaths(ps Problems) []string {
	out := []string{}
	for _, p := range ps {
		if p.Fatal {
			out = append(out, p.Path)
		}
	}
	return out
}

func mustHaveFatal(t *testing.T, ps Problems, wantSubstr string) {
	t.Helper()
	for _, p := range ps {
		if p.Fatal && strings.Contains(p.Path+": "+p.Message, wantSubstr) {
			return
		}
	}
	t.Fatalf("no fatal problem mentioning %q; got %v", wantSubstr, ps)
}

func TestAMinimalRegisterValidates(t *testing.T) {
	if ps := minimal().Validate(); ps.Fatal() {
		t.Fatalf("clean register refused: %v", fatalPaths(ps))
	}
}

// The first of the two directions: a rule naming a token no answer can produce is a
// disease that can never be diagnosed, however well-formed it looks.
func TestARuleNamingATokenNoAnswerEmitsIsRefused(t *testing.T) {
	r := minimal()
	r.Rules[0].Pathognomonic = []Clause{{Findings: []string{"frothy_mouth"}}}

	ps := r.Validate()
	if !ps.Fatal() {
		t.Fatal("a disease that can never fire was published")
	}
	mustHaveFatal(t, ps, "can never be diagnosed")
}

// The second direction, and its deliberate escape hatch: an orphan token is refused
// unless the author DECLARED it, which is how a symptom is staged in one publish and
// the rule that reads it written in the next.
func TestAnAnswerReachingNoRuleIsRefusedUnlessDeclared(t *testing.T) {
	r := minimal()
	r.Questions[0].Options[1].Emits = []string{"nasal_discharge", "snoring"}

	ps := r.Validate()
	if !ps.Fatal() {
		t.Fatal("a question that does nothing was published with no complaint")
	}
	mustHaveFatal(t, ps, "reaches no rule")

	r.Vocabulary = []string{"snoring"}
	ps = r.Validate()
	if ps.Fatal() {
		t.Fatalf("a declared, not-yet-read token was refused: %v", fatalPaths(ps))
	}
	var warned bool
	for _, p := range ps {
		if !p.Fatal && strings.Contains(p.Message, "snoring") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("a staged token should still be reported to the author")
	}
}

// Bands are first-match-wins, so listing them least-severe first leaves the severe
// band permanently unreachable -- a silent under-read of the sickest animals.
func TestABandAnEarlierBandAlreadyCoversIsRefused(t *testing.T) {
	r := minimal()
	r.Vocabulary = []string{"FEVER", "HIGH_FEVER"}
	r.Questions = append(r.Questions, Question{
		ID: "temp", Kind: QuestionNumber, Title: "Temperature", Unit: "degF",
		Bands: []Band{
			{Gte: f64(103.5), Emits: []string{"FEVER"}},
			{Gt: f64(106), Emits: []string{"HIGH_FEVER", "FEVER"}},
		},
	})
	r.Rules[0].Probable = []Clause{{Findings: []string{"FEVER"}}}

	ps := r.Validate()
	mustHaveFatal(t, ps, "can never fire")

	// The right order validates, and is the order the seeded registers use.
	r.Questions[1].Bands = []Band{
		{Gt: f64(106), Emits: []string{"HIGH_FEVER", "FEVER"}},
		{Gte: f64(103.5), Emits: []string{"FEVER"}},
	}
	if ps := r.Validate(); ps.Fatal() {
		t.Fatalf("most-severe-first bands refused: %v", fatalPaths(ps))
	}
}

// A GAP between bands is legitimate and must stay legitimate: it is how a normal
// temperature emits nothing at all.
func TestAGapBetweenBandsIsNotAnError(t *testing.T) {
	r := minimal()
	r.Vocabulary = []string{"FEVER", "HYPOTHERMIA"}
	r.Questions = append(r.Questions, Question{
		ID: "temp", Kind: QuestionNumber, Title: "Temperature",
		Bands: []Band{
			{Gte: f64(103.5), Emits: []string{"FEVER"}},
			{Lt: f64(100), Emits: []string{"HYPOTHERMIA"}},
		},
	})
	r.Rules[0].Probable = []Clause{{Findings: []string{"FEVER"}}}
	if ps := r.Validate(); ps.Fatal() {
		t.Fatalf("a normal-temperature gap was refused: %v", fatalPaths(ps))
	}
}

// The herd register owns species, sex, status and stage. An answer that could emit
// one would let a manager's tick override the animal's own record.
func TestAnAnswerMayNotEmitAnAnimalFact(t *testing.T) {
	r := minimal()
	r.Questions[0].Options[1].Emits = []string{"sex:F"}
	mustHaveFatal(t, r.Validate(), "supplied by the herd register")
}

// A CHECK THAT WAS TRIED AND REMOVED. "The form never names a disease" is a real
// rule, but it cannot be enforced by comparing a question's title to a rule id,
// because this register deliberately carries SYMPTOM-LABEL rules -- RED_URINE,
// WOUNDS, LUMPS, TICKS -- whose job is to surface a finding no diagnosis accounted
// for. Their ids ARE sign names, so the check refused four of the farm's own
// questions for being named after the signs they record. It stays a review rule; see
// the note in authored_validate.go.

func TestAnUnknownKeyIsRejectedRatherThanIgnored(t *testing.T) {
	_, err := LoadAuthored([]byte(`{"register_version":"x","severity_fudge":3,"rules":[]}`))
	if err == nil {
		t.Fatal("an unread key was accepted; it would read to the next author as honoured")
	}
	if !strings.Contains(err.Error(), "severity_fudge") {
		t.Fatalf("the error should name the key: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Evidence
// ---------------------------------------------------------------------------

func TestEvidenceEmitsOnlyWhatAnswersCarry(t *testing.T) {
	r := minimal()
	animal := Animal{Class: ClassAdult, Species: "goat", Sex: "F", Status: "lactating"}

	ev := r.Evidence(animal, Answers{"nasal": {Values: []string{"yes"}}}, nil)
	if !ev["nasal_discharge"] {
		t.Fatal("a ticked finding did not reach the evidence set")
	}
	if !ev["species:goat"] || !ev["sex:F"] || !ev["status:lactating"] {
		t.Fatalf("the herd register's own facts are missing: %v", ev)
	}

	// "No" is the ABSENCE of a sign, not a sign named "no": emitting it would let a
	// clause anchor on it and would surface it as an unexplained finding.
	ev = r.Evidence(animal, Answers{"nasal": {Values: []string{"no"}}}, nil)
	if ev["nasal_discharge"] {
		t.Fatal("a normal answer emitted a finding")
	}
	if len(ev) != 3 {
		t.Fatalf("a normal answer emitted something beyond the animal's own facts: %v", ev)
	}
}

// The one correction the seeded registers carry, and the reason corrections are data:
// written in Go this would be keyed on two question ids, so renaming `skin_tent`
// would break dehydration scoring silently.
func TestACorrectionRewritesOneFindingInLightOfAnother(t *testing.T) {
	r := minimal()
	r.Vocabulary = []string{"skin_tent:2-4s"}
	r.Questions = append(r.Questions,
		Question{ID: "skin_tent", Kind: QuestionChoice, Title: "Skin tent", Options: []Option{
			{Value: "lt2", Label: "Under 2s"},
			{Value: "gt4", Label: "Over 4s", Emits: []string{"skin_tent:>4s", "TENT_GT4"}},
		}},
		Question{ID: "stomach_inside", Kind: QuestionChoice, Title: "Stomach drawn in", Options: []Option{
			{Value: "no", Label: "No"},
			{Value: "yes", Label: "Yes", Emits: []string{"misc:stomach_inside"}},
		}},
	)
	r.Corrections = []Correction{{
		ID:     "tent_with_stomach_inside",
		When:   []string{"misc:stomach_inside", "skin_tent:>4s"},
		Remove: []string{"skin_tent:>4s", "TENT_GT4"},
		Add:    []string{"skin_tent:2-4s"},
		Note:   "A drawn-in stomach reads one band worse than the animal is.",
	}}
	r.Rules[0].Probable = []Clause{{Findings: []string{"skin_tent:>4s"}}, {Findings: []string{"TENT_GT4"}}, {Findings: []string{"misc:stomach_inside"}}}

	if ps := r.Validate(); ps.Fatal() {
		t.Fatalf("refused: %v", fatalPaths(ps))
	}

	animal := Animal{Class: ClassAdult}
	ans := Answers{
		"nasal":          {Values: []string{"no"}},
		"skin_tent":      {Values: []string{"gt4"}},
		"stomach_inside": {Values: []string{"yes"}},
	}
	ev := r.Evidence(animal, ans, nil)
	if ev["skin_tent:>4s"] || ev["TENT_GT4"] {
		t.Fatalf("the corrected reading was not removed: %v", ev)
	}
	if !ev["skin_tent:2-4s"] {
		t.Fatal("the corrected reading was not recorded")
	}

	// Without the second finding the correction must not fire.
	ans["stomach_inside"] = Answer{Values: []string{"no"}}
	ev = r.Evidence(animal, ans, nil)
	if !ev["skin_tent:>4s"] || !ev["TENT_GT4"] {
		t.Fatalf("the correction fired on its own: %v", ev)
	}
}

// Every asked question is compulsory: a blank cannot distinguish "nobody looked"
// from "normal", and the unexplained-findings channel depends on that distinction.
func TestAnUnansweredQuestionIsRefused(t *testing.T) {
	r := minimal()
	ps := r.ValidateAnswers(Animal{Class: ClassAdult}, Answers{})
	if !ps.Fatal() {
		t.Fatal("a form with a blank was accepted")
	}
	mustHaveFatal(t, ps, "has not been answered")
}

// A hidden question is not owed an answer -- and an answer to one is refused rather
// than dropped, because the stored form is what an override review reads.
func TestASexHiddenQuestionIsNeitherOwedNorAccepted(t *testing.T) {
	r := minimal()
	r.Vocabulary = []string{"udder:swollen_hard"}
	r.Questions = append(r.Questions, Question{
		ID: "udder", Kind: QuestionChoice, Title: "Udder", OnlyIfSex: "F",
		Options: []Option{
			{Value: "normal", Label: "Normal"},
			{Value: "swollen_hard", Label: "Swollen and hard", Emits: []string{"udder:swollen_hard"}},
		},
	})
	r.Rules[0].Probable = []Clause{{Findings: []string{"udder:swollen_hard"}}}
	if ps := r.Validate(); ps.Fatal() {
		t.Fatalf("refused: %v", fatalPaths(ps))
	}

	male := Animal{Class: ClassAdult, Sex: "M"}
	if ps := r.ValidateAnswers(male, Answers{"nasal": {Values: []string{"no"}}}); ps.Fatal() {
		t.Fatalf("a male was asked for an udder: %v", fatalPaths(ps))
	}
	ps := r.ValidateAnswers(male, Answers{
		"nasal": {Values: []string{"no"}},
		"udder": {Values: []string{"swollen_hard"}},
	})
	mustHaveFatal(t, ps, "was not asked for this animal")

	female := Animal{Class: ClassAdult, Sex: "F"}
	if ps := r.ValidateAnswers(female, Answers{"nasal": {Values: []string{"no"}}}); !ps.Fatal() {
		t.Fatal("a female was not asked for an udder")
	}
}

func TestAnExclusiveAnswerCannotBeTickedAlongsideAnother(t *testing.T) {
	r := minimal()
	r.Vocabulary = []string{"neuro:circling", "neuro:ataxia"}
	r.Questions = append(r.Questions, Question{
		ID: "neuro", Kind: QuestionMulti, Title: "Nervous signs",
		Options: []Option{
			{Value: "none", Label: "None", ConflictsWith: []string{"circling", "ataxia"}},
			{Value: "circling", Label: "Circling", Emits: []string{"neuro:circling"}},
			{Value: "ataxia", Label: "Unsteady", Emits: []string{"neuro:ataxia"}},
		},
	})
	r.Rules[0].Probable = []Clause{{Findings: []string{"neuro:circling"}}, {Findings: []string{"neuro:ataxia"}}}
	if ps := r.Validate(); ps.Fatal() {
		t.Fatalf("refused: %v", fatalPaths(ps))
	}

	ps := r.ValidateAnswers(Animal{Class: ClassAdult}, Answers{
		"nasal": {Values: []string{"no"}},
		"neuro": {Values: []string{"none", "circling"}},
	})
	mustHaveFatal(t, ps, "cannot be ticked alongside")
}
