// Package authored holds the parts of an authored SOP document that every module's rulebook
// shares: the CAPTURE SLOTS a card asks for (a live-camera video, a photo, or either; compulsory
// or not), the QUESTIONS an operator answers alongside them, and the validation of a submit's
// proof refs and answers against those rules.
//
// It is pure domain: no tables, no I/O, no module vocabulary. Each module keeps its own
// document type (form_dsl.weighing, form_dsl.feed, ...) and embeds these building blocks, so
// "add a photo beside the video", "replace the video with a photo", "add one more step" and
// "ask one more question" mean the same thing on every card the farm authors -- and the phone
// renders them all with the same widgets.
//
// The weighing removal card (backend/internal/weighing/domain/sop.go, PR #274) carries the
// original of this logic under its own names; it moves onto this package once that PR lands.
package authored

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Capture kinds a slot may ask for.
const (
	KindVideo  = "video"
	KindPhoto  = "photo"
	KindEither = "either"
)

// Question kinds. The phone renders each with its own widget; there is no media kind here
// because the proof slots ARE the card's media.
const (
	QuestionChoice = "choice"
	QuestionMulti  = "multi"
	QuestionText   = "text"
	QuestionNumber = "number"
)

// MaxProofSlots bounds a card: more captures than this is not one card's work.
const MaxProofSlots = 8

// MaxQuestions bounds the questions on one card.
const MaxQuestions = 50

// MaxTextLength bounds free text (instructions, hints, text answers).
const MaxTextLength = 2000

// IDPattern is the shape of a slot key or question id: a-z, 0-9 and _, starting with a letter.
var IDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

// ProofSlot is one capture a card asks for: a live-camera VIDEO, a PHOTO, or EITHER, compulsory
// or not. One capture per slot; a card wanting two captures asks two slots.
type ProofSlot struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Hint  string `json:"hint,omitempty"`
	Kind  string `json:"kind"`
	// Required: the card cannot be submitted without this capture.
	Required bool `json:"required"`
}

// UnmarshalJSON reads a slot with `required` ABSENT as compulsory: a document authored before
// the flag existed carried only compulsory captures, and reading them as optional would change
// its meaning on every farm that published it.
func (p *ProofSlot) UnmarshalJSON(data []byte) error {
	type raw struct {
		Key      string `json:"key"`
		Title    string `json:"title"`
		Hint     string `json:"hint"`
		Kind     string `json:"kind"`
		Required *bool  `json:"required"`
	}
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	p.Key, p.Title, p.Hint, p.Kind = r.Key, r.Title, r.Hint, r.Kind
	p.Required = r.Required == nil || *r.Required
	return nil
}

// Accepts reports whether a slot takes a capture of the given proof type (video / photo).
func (p ProofSlot) Accepts(proofType string) bool {
	switch p.Kind {
	case KindEither:
		return proofType == KindVideo || proofType == KindPhoto
	default:
		return proofType == p.Kind
	}
}

// Question is one authored question. Wire-shaped the same way the procurement inspection's and
// the weighing removal card's questions are, so the phone renders it with the widgets it has.
type Question struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Hint     string   `json:"hint,omitempty"`
	Required bool     `json:"required"`
	Options  []Option `json:"options,omitempty"`
	// AllowOther lets a pick-one carry free text under the "other" option ("<id>_other").
	AllowOther bool     `json:"allow_other,omitempty"`
	Min        *float64 `json:"min,omitempty"`
	Max        *float64 `json:"max,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	// OnlyIf hides the question unless an EARLIER pick-one holds the given value.
	OnlyIf *Condition `json:"only_if,omitempty"`
}

// Option is one choice of a pick-one / pick-many question.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Condition is a single-question dependency.
type Condition struct {
	QuestionID string `json:"question_id"`
	Value      string `json:"value"`
}

// Adder collects validation problems, each naming its path.
type Adder func(format string, args ...any)

// ValidateProofSlots names every problem in a slot list. requireOne demands at least one
// compulsory slot (a card that must be proven by something the verifier can see).
func ValidateProofSlots(path string, slots []ProofSlot, requireOne bool, add Adder) {
	if len(slots) > MaxProofSlots {
		add("%s: at most %d captures per card", path, MaxProofSlots)
	}
	seen := map[string]bool{}
	required := 0
	for i, p := range slots {
		pp := fmt.Sprintf("%s.%d", path, i)
		if !IDPattern.MatchString(p.Key) {
			add("%s.key: %q must be a-z, 0-9 and _ (start with a letter)", pp, p.Key)
		}
		if seen[p.Key] {
			add("%s.key: %q is listed twice", pp, p.Key)
		}
		seen[p.Key] = true
		if strings.TrimSpace(p.Title) == "" {
			add("%s.title: required", pp)
		}
		if len(p.Hint) > MaxTextLength {
			add("%s.hint: too long", pp)
		}
		switch p.Kind {
		case KindVideo, KindPhoto, KindEither:
		default:
			add("%s.kind: %q is not video / photo / either", pp, p.Kind)
		}
		if p.Required {
			required++
		}
	}
	if requireOne && required == 0 {
		add("%s: at least one compulsory capture -- the work must be proven by something the verifier can see", path)
	}
}

// ValidateQuestions names every problem in a question list.
func ValidateQuestions(path string, questions []Question, add Adder) {
	if len(questions) > MaxQuestions {
		add("%s: at most %d", path, MaxQuestions)
	}
	seen := map[string]Question{}
	for qi, q := range questions {
		qp := fmt.Sprintf("%s.%d", path, qi)
		if !IDPattern.MatchString(q.ID) {
			add("%s.id: %q must be a-z, 0-9 and _ (start with a letter)", qp, q.ID)
		}
		if strings.HasSuffix(q.ID, "_other") {
			add("%s.id: %q -- the _other suffix is reserved for the free text of a pick-one", qp, q.ID)
		}
		if _, dup := seen[q.ID]; dup {
			add("%s.id: %q is used twice", qp, q.ID)
		}
		if strings.TrimSpace(q.Title) == "" {
			add("%s.title: required", qp)
		}
		switch q.Kind {
		case QuestionChoice, QuestionMulti:
			if len(q.Options) == 0 {
				add("%s.options: a pick-one / pick-many question needs at least one choice", qp)
			}
			seenOpt := map[string]bool{}
			for oi, o := range q.Options {
				if strings.TrimSpace(o.Value) == "" || strings.TrimSpace(o.Label) == "" {
					add("%s.options.%d: value and label are required", qp, oi)
				}
				if seenOpt[o.Value] {
					add("%s.options.%d: value %q is used twice", qp, oi, o.Value)
				}
				seenOpt[o.Value] = true
			}
			if q.AllowOther && !seenOpt["other"] {
				add("%s.allow_other: needs an option with value \"other\" to attach the free text to", qp)
			}
		case QuestionNumber:
			if q.Min != nil && q.Max != nil && *q.Min > *q.Max {
				add("%s.min: must not exceed max", qp)
			}
		case QuestionText:
		default:
			add("%s.kind: %q is not a question kind (choice / multi / text / number)", qp, q.Kind)
		}
		if q.OnlyIf != nil {
			dep, ok := seen[q.OnlyIf.QuestionID]
			switch {
			case !ok:
				add("%s.only_if.question_id: %q must be an EARLIER question", qp, q.OnlyIf.QuestionID)
			case dep.Kind != QuestionChoice:
				add("%s.only_if.question_id: %q must be a pick-one question", qp, q.OnlyIf.QuestionID)
			case !hasOption(dep.Options, q.OnlyIf.Value):
				add("%s.only_if.value: %q is not a choice of %q", qp, q.OnlyIf.Value, q.OnlyIf.QuestionID)
			}
		}
		seen[q.ID] = q
	}
}

func hasOption(options []Option, v string) bool {
	for _, o := range options {
		if o.Value == v {
			return true
		}
	}
	return false
}

// --- Proof refs ---------------------------------------------------------------------------

// ProofRefs is {slot key: proof ref} -- what a submit carries.
type ProofRefs map[string]string

// ProofError names the slot a submit failed on, for the phone to point at.
type ProofError struct {
	SlotKey string
	Message string
}

func (e *ProofError) Error() string { return e.SlotKey + ": " + e.Message }

// ErrProofInvalid wraps every ProofError so callers can errors.Is it.
var ErrProofInvalid = errors.New("authored sop: proof invalid")

func proofInvalid(key, msg string) error {
	return fmt.Errorf("%w: %w", ErrProofInvalid, &ProofError{SlotKey: key, Message: msg})
}

// OrderedProof is one accepted capture, in slot order, with the slot it proves.
type OrderedProof struct {
	Slot ProofSlot
	Ref  string
}

// ValidateProofRefs checks a submit's captures against the slots: every compulsory slot carries
// a ref, no ref lands in a slot the card does not ask for, and no ref is used in two slots (one
// capture cannot prove two things). The refs' TYPES are the store's to check against the proof
// register (a photo in a video slot is refused there, through Accepts). Returns the accepted
// captures in slot order, for the verifier item.
func ValidateProofRefs(slots []ProofSlot, refs ProofRefs) ([]OrderedProof, error) {
	known := map[string]bool{}
	for _, p := range slots {
		known[p.Key] = true
	}
	for key := range refs {
		if !known[key] {
			return nil, proofInvalid(key, "This capture is not part of this card.")
		}
	}
	seen := map[string]string{}
	ordered := make([]OrderedProof, 0, len(refs))
	for _, p := range slots {
		ref := strings.TrimSpace(refs[p.Key])
		if ref == "" {
			if p.Required {
				return nil, proofInvalid(p.Key, "Record: "+p.Title)
			}
			continue
		}
		if other, dup := seen[ref]; dup {
			return nil, proofInvalid(p.Key, "The same capture cannot prove both "+other+" and "+p.Title+".")
		}
		seen[ref] = p.Title
		ordered = append(ordered, OrderedProof{Slot: p, Ref: ref})
	}
	return ordered, nil
}

// NormalizeProofRefs drops blank entries so the stored map holds only real captures.
func NormalizeProofRefs(refs ProofRefs) ProofRefs {
	out := ProofRefs{}
	for k, v := range refs {
		if strings.TrimSpace(v) != "" {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

// --- Answers ------------------------------------------------------------------------------

// Answers is {question id: answer}: a pick-one answer is its option value; an "other" free text
// rides under "<id>_other"; pick-many answers are arrays of option values; numbers are JSON
// numbers or numeric strings; text is text.
type Answers map[string]json.RawMessage

// AnswerError names the question a submit failed on, for the phone to point at.
type AnswerError struct {
	QuestionID string
	Message    string
}

func (e *AnswerError) Error() string { return e.QuestionID + ": " + e.Message }

// ErrAnswerInvalid wraps every AnswerError so callers can errors.Is it.
var ErrAnswerInvalid = errors.New("authored sop: answer invalid")

func answerInvalid(id, msg string) error {
	return fmt.Errorf("%w: %w", ErrAnswerInvalid, &AnswerError{QuestionID: id, Message: msg})
}

// ValidateAnswers checks a card's answers against the questions: every applicable required
// question answered, every choice among the offered options, every number in range, no answer
// to a question the card does not ask. A question whose only_if does not hold is skipped -- an
// answer given to it is dropped by NormalizeAnswers, not refused.
func ValidateAnswers(questions []Question, a Answers) error {
	applicable := applicableQuestions(questions, a)
	known := map[string]bool{}
	for _, q := range questions {
		known[q.ID] = true
		if q.Kind == QuestionChoice && q.AllowOther {
			known[q.ID+"_other"] = true
		}
	}
	for id := range a {
		if !known[id] {
			return answerInvalid(id, "This question is not part of this card.")
		}
	}
	for _, q := range questions {
		if !applicable[q.ID] {
			continue
		}
		switch q.Kind {
		case QuestionChoice:
			v := a.choice(q.ID)
			if v == "" {
				if q.Required {
					return answerInvalid(q.ID, "Answer: "+q.Title)
				}
				continue
			}
			if !hasOption(q.Options, v) {
				return answerInvalid(q.ID, "Pick one of the offered answers for: "+q.Title)
			}
			if v == "other" && q.AllowOther && strings.TrimSpace(a.text(q.ID+"_other")) == "" {
				return answerInvalid(q.ID, "Say which, for: "+q.Title)
			}
		case QuestionMulti:
			vals, ok := a.multi(q.ID)
			if !ok || len(vals) == 0 {
				if q.Required {
					return answerInvalid(q.ID, "Tick at least one for: "+q.Title)
				}
				continue
			}
			for _, v := range vals {
				if !hasOption(q.Options, v) {
					return answerInvalid(q.ID, "Pick only the offered answers for: "+q.Title)
				}
			}
		case QuestionNumber:
			n, present, err := a.number(q.ID)
			if !present {
				if q.Required {
					return answerInvalid(q.ID, "Enter: "+q.Title)
				}
				continue
			}
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return answerInvalid(q.ID, "Enter a number for: "+q.Title)
			}
			if (q.Min != nil && n < *q.Min) || (q.Max != nil && n > *q.Max) {
				return answerInvalid(q.ID, fmt.Sprintf("Enter a value between %g and %g for: %s", deref(q.Min), deref(q.Max), q.Title))
			}
		case QuestionText:
			t := a.text(q.ID)
			if q.Required && strings.TrimSpace(t) == "" {
				return answerInvalid(q.ID, "Enter: "+q.Title)
			}
			if len(t) > MaxTextLength {
				return answerInvalid(q.ID, "Too long: "+q.Title)
			}
		}
	}
	return nil
}

// NormalizeAnswers keeps only the answers of questions that apply (a conditional whose
// condition failed is dropped rather than stored), so the stored row reads exactly as the card
// asked. Returns an empty (non-nil) map when the card asks nothing.
func NormalizeAnswers(questions []Question, a Answers) Answers {
	out := Answers{}
	applicable := applicableQuestions(questions, a)
	for _, q := range questions {
		if !applicable[q.ID] {
			continue
		}
		if raw, ok := a[q.ID]; ok {
			out[q.ID] = raw
		}
		if q.Kind == QuestionChoice && q.AllowOther {
			if raw, ok := a[q.ID+"_other"]; ok && a.choice(q.ID) == "other" {
				out[q.ID+"_other"] = raw
			}
		}
	}
	return out
}

// Conditions may only reference earlier questions (validated at publish), so a single ordered
// pass resolves the entire ancestry: a hidden answer cannot activate a descendant.
func applicableQuestions(questions []Question, a Answers) map[string]bool {
	out := make(map[string]bool, len(questions))
	for _, q := range questions {
		cond := q.OnlyIf
		out[q.ID] = cond == nil || (out[cond.QuestionID] && a.choice(cond.QuestionID) == cond.Value)
	}
	return out
}

func (a Answers) choice(id string) string {
	raw, ok := a[id]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func (a Answers) text(id string) string {
	raw, ok := a[id]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func (a Answers) multi(id string) ([]string, bool) {
	raw, ok := a[id]
	if !ok {
		return nil, false
	}
	var vals []string
	if err := json.Unmarshal(raw, &vals); err != nil {
		var one string
		if err := json.Unmarshal(raw, &one); err != nil || strings.TrimSpace(one) == "" {
			return nil, false
		}
		return []string{one}, true
	}
	return vals, true
}

func (a Answers) number(id string) (float64, bool, error) {
	raw, ok := a[id]
	if !ok {
		return 0, false, nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, true, err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseFloat(s, 64)
	return n, true, err
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// AnswerRow is one recorded answer in farm words -- the question's title and the answer as the
// operator would read it -- for a verifier's context rows.
type AnswerRow struct {
	Title string
	Value string
}

// AnswerRows renders stored answers against the questions, in question order. Unanswered
// questions are skipped; a choice renders its label, a pick-many every picked label, a number
// with its unit, text as typed. The "other" free text follows its choice.
func AnswerRows(questions []Question, a Answers) []AnswerRow {
	out := make([]AnswerRow, 0, len(questions))
	for _, q := range questions {
		switch q.Kind {
		case QuestionChoice:
			v := a.choice(q.ID)
			if v == "" {
				continue
			}
			label := v
			for _, o := range q.Options {
				if o.Value == v {
					label = o.Label
				}
			}
			if v == "other" && q.AllowOther {
				if t := strings.TrimSpace(a.text(q.ID + "_other")); t != "" {
					label = label + " — " + t
				}
			}
			out = append(out, AnswerRow{Title: q.Title, Value: label})
		case QuestionMulti:
			vals, ok := a.multi(q.ID)
			if !ok || len(vals) == 0 {
				continue
			}
			labels := make([]string, 0, len(vals))
			for _, v := range vals {
				label := v
				for _, o := range q.Options {
					if o.Value == v {
						label = o.Label
					}
				}
				labels = append(labels, label)
			}
			out = append(out, AnswerRow{Title: q.Title, Value: strings.Join(labels, ", ")})
		case QuestionNumber:
			n, present, err := a.number(q.ID)
			if !present || err != nil {
				continue
			}
			v := strconv.FormatFloat(n, 'f', -1, 64)
			if q.Unit != "" {
				v += " " + q.Unit
			}
			out = append(out, AnswerRow{Title: q.Title, Value: v})
		case QuestionText:
			t := strings.TrimSpace(a.text(q.ID))
			if t == "" {
				continue
			}
			out = append(out, AnswerRow{Title: q.Title, Value: t})
		}
	}
	return out
}
