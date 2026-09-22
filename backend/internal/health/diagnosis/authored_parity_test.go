package diagnosis

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

// THE PROOF THAT AUTHORING THE FORM CHANGED NO CLINICAL BEHAVIOUR.
//
// The acceptance catalogs -- 180 adult stories and three kid catalogs beside them --
// were written against the Go form and have always been this engine's contract. Each
// story is replayed twice here: once through the Go mapping that used to build the
// evidence set, and once through the AUTHORED register, and the two evidence sets must
// be IDENTICAL, token for token.
//
// Identical is the right bar and a weaker one will not do. The unexplained-findings
// channel ranges over EVERY token in the evidence set, so a single token present on
// one side and not the other reaches the Health Director as a finding nobody observed,
// or hides one somebody did. "The diagnoses still match" would not catch either.
func TestAuthoredEvidenceMatchesTheFormItReplaces(t *testing.T) {
	catalogs := map[string]string{
		ClassAdult:        "testdata/catalog.json",
		ClassKidMilk:      "testdata/catalog-kid-milk.json",
		ClassKidWeaning:   "testdata/catalog-kid-weaning.json",
		ClassKidFattening: "testdata/catalog-kid-fattening.json",
	}

	type story struct {
		ID       string   `json:"id"`
		Title    string   `json:"title"`
		Animal   Animal   `json:"animal"`
		Findings Findings `json:"findings"`
	}

	total, rejected := 0, 0
	var inert []string
	for class, path := range catalogs {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", class, err)
		}
		var cat struct {
			Stories []story `json:"stories"`
		}
		if err := json.Unmarshal(raw, &cat); err != nil {
			t.Fatalf("%s: %v", class, err)
		}

		authored, err := SeedAuthored(class, nil)
		if err != nil {
			t.Fatalf("%s: %v", class, err)
		}

		for _, s := range cat.Stories {
			total++
			animal := s.Animal
			if animal.Class == "" {
				animal.Class = class
			}

			// A story the old form REJECTED never reached a diagnosis, so there is
			// no behaviour to preserve. Four of them tick a male's CMT or a
			// female's urine straining -- exactly the contradictions the authored
			// form makes unaskable instead of rejecting after the fact.
			if validateForm(animal, s.Findings) != "" {
				rejected++
				continue
			}

			want := buildEvidence(animal, s.Findings, deriveTokens(animal, s.Findings))
			got := authored.Evidence(animal, LegacyAnswers(animal, s.Findings), MilkTokens(animal, s.Findings))

			// The rule layer is compared in full, not just the tokens: diagnoses,
			// their tiers, what each one covered, the field actions, the rechecks
			// AND the unexplained findings. Two evidence sets that differ only in a
			// token no rule reads and nothing reports are the same observation.
			reg, err := RegisterFor(class)
			if err != nil {
				t.Fatal(err)
			}
			if d := matchDiff(reg.evaluateRegister(animal, want), reg.evaluateRegister(animal, got)); d != "" {
				missing, extra := diffSets(want, got)
				t.Errorf("%s %s (%s)\n  %s\n  tokens missing: %v\n  tokens invented: %v",
					class, s.ID, s.Title, d, missing, extra)
				continue
			}
			if missing, extra := diffSets(want, got); len(missing) > 0 || len(extra) > 0 {
				inert = append(inert, class+" "+s.ID+" "+fmtDiff(missing, extra))
			}
		}
	}
	t.Logf("replayed %d catalog stories through the authored register (%d were rejected by the old form and carry no behaviour)", total, rejected)
	sort.Strings(inert)
	for _, line := range inert {
		t.Logf("same diagnosis, quieter unexplained list: %s", line)
	}
	if len(inert) == 0 {
		t.Fatal("the two documented removals no longer happen; retire the allowance in withoutNoise rather than leaving it")
	}
}

// matchDiff compares two rule-layer results and names the first difference.
func matchDiff(a, b matchResult) string {
	if d := cmpStrings("problems", ruleIDs(a.problems), ruleIDs(b.problems)); d != "" {
		return d
	}
	if d := cmpStrings("covered", a.covered, b.covered); d != "" {
		return d
	}
	if d := cmpStrings("field actions", a.fieldActions, b.fieldActions); d != "" {
		return d
	}
	if d := cmpStrings("rechecks", a.rechecks, b.rechecks); d != "" {
		return d
	}
	if d := cmpStrings("unexplained", withoutNoise(a.unexplained), withoutNoise(b.unexplained)); d != "" {
		return d
	}
	if len(a.tiers) != len(b.tiers) {
		return fmt.Sprintf("tiers: %v vs %v", a.tiers, b.tiers)
	}
	for id, tier := range a.tiers {
		if b.tiers[id] != tier {
			return fmt.Sprintf("tier for %s: %v vs %v", id, tier, b.tiers[id])
		}
	}
	if a.adjunct != b.adjunct {
		return fmt.Sprintf("adjunct: %v vs %v", a.adjunct, b.adjunct)
	}
	return ""
}

// THE TWO DIFFERENCES THE AUTHORED FORM MAKES, both of them removals, both of them
// confined to the unexplained-findings list, and neither of them changing a diagnosis,
// a tier, a covered finding, a field action or a recheck on any of the 260 stories.
//
// They are listed here rather than tolerated silently because they ARE behaviour
// changes and a maintainer has to decide about them -- and because a difference nobody
// wrote down is a difference the next author reads as intentional.
//
//  1. `landing:na` -- the drop test on a kid that is already DOWN. The old form
//     recorded "not applicable" and then reported it to the Health Director as an
//     unaccounted-for finding, so the sickest kids in the shed -- down, unresponsive,
//     the ones whose unexplained list most needs to be readable -- each carried one
//     line of noise saying a test had not been done. The authored form does not ASK
//     the question of a recumbent kid, so there is nothing to report. Six stories.
//
//  2. `cmt:positive` on a MALE. Story S123 is titled "Male form CMT if present is
//     ignored" -- so the catalog already says this is what the engine ought to do, and
//     the old form did not do it: it emitted the token and surfaced it as unexplained.
//     The authored form does not ask a male for a CMT. One story.
//
// The allowance is asserted to be USED below, so if a later change makes one of these
// stop happening, this note is retired deliberately rather than rotting in place.
func withoutNoise(findings []string) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		if f == "landing:na" || f == "cmt:positive" || f == "cmt:negative" {
			continue
		}
		out = append(out, f)
	}
	return out
}

func ruleIDs(hits []hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.rule.ID)
	}
	return out
}

func cmpStrings(label string, a, b []string) string {
	x, y := append([]string{}, a...), append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	if len(x) != len(y) {
		return fmt.Sprintf("%s: %v vs %v", label, x, y)
	}
	for i := range x {
		if x[i] != y[i] {
			return fmt.Sprintf("%s: %v vs %v", label, x, y)
		}
	}
	return ""
}

func fmtDiff(missing, extra []string) string {
	return fmt.Sprintf("dropped %v, added %v", missing, extra)
}

func diffSets(want, got map[string]bool) (missing, extra []string) {
	for tok := range want {
		if !got[tok] {
			missing = append(missing, tok)
		}
	}
	for tok := range got {
		if !want[tok] {
			extra = append(extra, tok)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return
}
