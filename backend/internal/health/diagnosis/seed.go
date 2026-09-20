package diagnosis

import (
	"fmt"
	"sort"
)

// SeedAuthored builds the v1 authored register for one animal class: the committed
// rule table this repo has always shipped, plus the transcribed form that feeds it.
//
// It is the ONE-TIME bridge. After a farm publishes its first version, this function
// is history -- it is how the farm's rulebook was born, not how it is maintained.
//
// treatsFor resolves a rule's display-form sop_ref to the disease key its treatment
// card is authored under. It is a PARAMETER rather than a call into the health domain
// because this package is pure and must stay that way; the caller owns the alias map,
// which is also where it already lives.
//
// What this bakes in permanently is the END of that derivation. From v1 the mapping
// is written down as `treats:` on each rule, so the next author reads a disease key
// instead of trusting a string transformation to land on one.
func SeedAuthored(class string, treatsFor func(sopRef string) string) (*AuthoredRegister, error) {
	reg, err := RegisterFor(class)
	if err != nil {
		return nil, err
	}

	questions := SeedQuestions(class)
	corrections := seedCorrections()

	rules := make([]Rule, len(reg.Rules))
	copy(rules, reg.Rules)
	for i := range rules {
		if treatsFor != nil {
			rules[i].Treats = treatsFor(rules[i].SOPRef)
		}
	}

	out := &AuthoredRegister{
		RegisterVersion: reg.Version,
		AppliesClass:    []string{class},
		Questions:       questions,
		Corrections:     corrections,
		NonSpecific:     append([]string{}, reg.NonSpecific...),
		Rules:           rules,
	}
	out.Vocabulary = seedVocabulary(reg, out)

	if ps := out.Validate(); ps.Fatal() {
		return nil, fmt.Errorf("health: seeded %s register is not publishable: %s", class, ps.Error())
	}
	return out, nil
}

// seedVocabulary declares everything the seeded FORM can produce, on top of what the
// register already declared.
//
// This is what makes the seed publishable without weakening the check it passes. The
// Go form ran the same head-to-toe pass for every class -- a kid was asked about its
// udder and its FAMACHA exactly as an adult was -- while each class's RULES read only
// the subset that class can present with. So a faithful transcription necessarily
// emits tokens some class's rules never read, and the orphan check is right to notice
// them.
//
// Declaring them says what is true: the form can produce this, and no rule on THIS
// class reads it yet. The author sees each one as a warning and can either delete the
// question from that class's form or write the rule -- which is the conversation the
// check exists to start, and one a fatal refusal at seed time would end by making the
// farm's own rulebook unpublishable.
func seedVocabulary(reg *Register, a *AuthoredRegister) []string {
	set := map[string]bool{}
	for _, v := range reg.Vocabulary {
		set[v] = true
	}
	for _, q := range a.Questions {
		for _, o := range q.Options {
			for _, tok := range o.Emits {
				set[tok] = true
			}
		}
		for _, b := range q.Bands {
			for _, tok := range b.Emits {
				set[tok] = true
			}
		}
	}
	for _, c := range a.Corrections {
		for _, tok := range c.Add {
			set[tok] = true
		}
	}
	out := make([]string, 0, len(set))
	for tok := range set {
		out = append(out, tok)
	}
	sort.Strings(out)
	return out
}
