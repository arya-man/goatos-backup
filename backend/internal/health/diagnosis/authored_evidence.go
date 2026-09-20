package diagnosis

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Answer is one question's answer: the ticked values of a choice/multi, or the
// reading of a measurement.
//
// The wire accepts a bare string, an array of strings, or a number, because that is
// what the form already sends for its three widget kinds and narrowing it would make
// every client compose a shape no human would write by hand.
type Answer struct {
	Values []string
	Number *float64
}

// NotApplicable is the value a SEX-HIDDEN question records.
//
// It is a real answer, not a blank, and that distinction is the whole reason this
// constant exists: every question is compulsory precisely so a blank can mean
// "nobody looked", and a male's udder question must not look like an udder nobody
// examined. It emits nothing.
const NotApplicable = "na"

func (a *Answer) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	switch {
	case trimmed == "" || trimmed == "null":
		*a = Answer{}
		return nil
	case strings.HasPrefix(trimmed, "["):
		var vals []string
		if err := json.Unmarshal(data, &vals); err != nil {
			return err
		}
		*a = Answer{Values: vals}
		return nil
	case strings.HasPrefix(trimmed, `"`):
		var v string
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*a = Answer{Values: []string{v}}
		return nil
	default:
		n, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return fmt.Errorf("answer %q is not a value, a list or a number", trimmed)
		}
		*a = Answer{Number: &n}
		return nil
	}
}

func (a Answer) MarshalJSON() ([]byte, error) {
	if a.Number != nil {
		return json.Marshal(*a.Number)
	}
	if a.Values == nil {
		return []byte("null"), nil
	}
	return json.Marshal(a.Values)
}

func (a Answer) has(v string) bool {
	for _, got := range a.Values {
		if got == v {
			return true
		}
	}
	return false
}

// Answers is the whole ticked form, keyed by question id.
type Answers map[string]Answer

// Asks reports whether this question is put to this animal at all. A question hidden
// by sex or by an earlier answer is not asked, and is therefore not owed an answer.
func (q Question) Asks(animal Animal, ans Answers) bool {
	if q.OnlyIfSex != "" && !strings.EqualFold(q.OnlyIfSex, animal.Sex) {
		return false
	}
	if q.OnlyIf != nil && !ans[q.OnlyIf.QuestionID].has(q.OnlyIf.Value) {
		return false
	}
	return true
}

// ValidateAnswers refuses a form that cannot be read, BEFORE anything is diagnosed
// from it.
//
// EVERY ASKED QUESTION MUST BE ANSWERED. That is the oldest rule on this form and it
// is not a convenience: a blank cannot distinguish "nobody looked" from "normal",
// and the unexplained-findings channel -- the thing that catches what the diagnosis
// failed to account for -- reads every recorded normal as evidence that it WAS
// looked at. Wrong ticks still happen; missing ticks must not.
func (a *AuthoredRegister) ValidateAnswers(animal Animal, ans Answers) Problems {
	var ps Problems
	add := func(path, format string, args ...any) {
		ps = append(ps, Problem{Path: path, Message: fmt.Sprintf(format, args...), Fatal: true})
	}

	asked := map[string]bool{}
	for _, q := range a.Questions {
		if !q.Asks(animal, ans) {
			continue
		}
		asked[q.ID] = true
		got, present := ans[q.ID]
		if !present || (len(got.Values) == 0 && got.Number == nil) {
			add(q.ID, "%s has not been answered", q.Title)
			continue
		}

		switch q.Kind {
		case QuestionNumber:
			if got.Number == nil {
				add(q.ID, "%s takes a measurement", q.Title)
				continue
			}
			if q.Min != nil && *got.Number < *q.Min {
				add(q.ID, "%s cannot be below %s", q.Title, trimFloat(*q.Min))
			}
			if q.Max != nil && *got.Number > *q.Max {
				add(q.ID, "%s cannot be above %s", q.Title, trimFloat(*q.Max))
			}
		case QuestionChoice:
			if len(got.Values) != 1 {
				add(q.ID, "%s takes exactly one answer", q.Title)
				continue
			}
			a.checkValues(q, got.Values, add)
		case QuestionMulti:
			a.checkValues(q, got.Values, add)
			if len(got.Values) > 1 {
				for _, v := range got.Values {
					if o := q.option(v); o != nil && o.Exclusive {
						add(q.ID, "%q cannot be ticked alongside another answer", o.Label)
					}
				}
			}
		}
	}

	// An answer to a question that was not asked is refused rather than ignored.
	// Silently dropping it would let a male's udder answer, or an answer to a
	// question the form hid, sit in the stored observation looking like evidence
	// that was considered -- and the stored form is what an override review reads.
	extra := make([]string, 0)
	for id := range ans {
		if !asked[id] {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	for _, id := range extra {
		if a.question(id) == nil {
			add(id, "is not a question on this form")
			continue
		}
		add(id, "was not asked for this animal")
	}
	return ps
}

func (a *AuthoredRegister) checkValues(q Question, vals []string, add func(string, string, ...any)) {
	seen := map[string]bool{}
	for _, v := range vals {
		if v == NotApplicable {
			continue
		}
		if q.option(v) == nil {
			add(q.ID, "%q is not an answer to %s", v, q.Title)
		}
		if seen[v] {
			add(q.ID, "%q is ticked twice", v)
		}
		seen[v] = true
	}
}

func (q Question) option(value string) *Option {
	for i := range q.Options {
		if q.Options[i].Value == value {
			return &q.Options[i]
		}
	}
	return nil
}

func (a *AuthoredRegister) question(id string) *Question {
	for i := range a.Questions {
		if a.Questions[i].ID == id {
			return &a.Questions[i]
		}
	}
	return nil
}

// Evidence turns an answered form into the token set the rules are matched against.
//
// This is the authored replacement for the ~150-line Go switch that used to stand
// here (`if f.Nasal { ev["nasal_discharge"] = true }`, forty times over). That switch
// was the reason a new SYMPTOM was a four-file change across two languages while a
// new DISEASE was only a YAML edit: the rules were data and the mapping into them
// was not.
//
// `engine` carries the tokens code supplies on its own -- the milk-bar refusal
// reading, which is a judgement over the animal's recent history rather than
// something the manager can see and tick. It is merged BEFORE the corrections run so
// a correction may read it.
func (a *AuthoredRegister) Evidence(animal Animal, ans Answers, engine map[string]bool) map[string]bool {
	ev := map[string]bool{}

	// The herd register's own facts. No answer may emit these -- Validate refuses a
	// register that tries -- so a manager cannot tick an animal into another sex,
	// species or lifecycle stage than the one it is recorded as.
	ev["species:"+animal.species()] = true
	if animal.Sex != "" {
		ev["sex:"+animal.Sex] = true
	}
	ev["status:"+animal.status()] = true
	if animal.Stage != "" {
		ev["stage:"+animal.Stage] = true
	}

	for tok := range engine {
		ev[tok] = true
	}

	for _, q := range a.Questions {
		if !q.Asks(animal, ans) {
			continue
		}
		got := ans[q.ID]

		if q.Kind == QuestionNumber {
			if got.Number == nil {
				continue
			}
			// FIRST MATCH WINS, so a register lists its bands most-severe first.
			// Validate refuses a band an earlier one already covers entirely,
			// which is what makes that ordering safe to rely on.
			for _, b := range q.Bands {
				if b.matches(*got.Number) {
					for _, tok := range b.Emits {
						ev[tok] = true
					}
					break
				}
			}
			continue
		}

		for _, v := range got.Values {
			o := q.option(v)
			if o == nil {
				// `na` and anything else unknown emit nothing. A hidden question's
				// recorded not-applicable is an answer, not a finding.
				continue
			}
			for _, tok := range o.Emits {
				ev[tok] = true
			}
		}
	}

	a.applyCorrections(ev)
	return ev
}

// applyCorrections rewrites the evidence set where one finding changes how another
// is read.
//
// Corrections are evaluated against the evidence as it stood BEFORE any of them ran,
// so two corrections cannot chain into a third state nobody authored and the result
// does not depend on the order they were typed in. A correction that wants to read
// another's output is two rules pretending to be one, and the author should say so.
func (a *AuthoredRegister) applyCorrections(ev map[string]bool) {
	if len(a.Corrections) == 0 {
		return
	}
	before := make(map[string]bool, len(ev))
	for k, v := range ev {
		before[k] = v
	}
	for _, c := range a.Corrections {
		fired := true
		for _, tok := range c.When {
			if !before[tok] {
				fired = false
				break
			}
		}
		if !fired {
			continue
		}
		for _, tok := range c.Remove {
			delete(ev, tok)
		}
		for _, tok := range c.Add {
			ev[tok] = true
		}
	}
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
