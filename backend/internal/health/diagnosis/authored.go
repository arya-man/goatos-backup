package diagnosis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The AUTHORED register: the questions the observation form asks, the tokens each
// answer emits, and the rules those tokens fire -- published as ONE version.
//
// Maintainer decision 2026-09-21 (docs/decisions/health-diagnosis-register-authoring.md).
// This supersedes the committed-YAML half of the 2026-08-14 decision recorded in
// embed.go, which named itself the starting point.
//
// WHY ALL THREE LAYERS ARE ONE VERSION. A question whose answer emits a token no
// rule reads is a question that does nothing; a rule clause naming a token no
// question emits is a disease that can never be diagnosed. Those are the exact two
// states two independently versioned halves would let a farm publish, and the place
// they would be discovered is in front of a sick animal. Validate() refuses both
// directions, so they can only be published together or not at all.
//
// WHAT IS DELIBERATELY NOT AUTHORABLE, and why each would be less safe as data:
//
//   - There is NO `required` flag. EVERY question is compulsory. A blank cannot
//     distinguish "nobody looked" from "normal", and the unexplained-findings
//     channel -- the thing that catches what the diagnosis failed to account for --
//     depends on that distinction. An optional question would silently disarm it.
//   - Species, sex, status and stage come from the herd register, never from the
//     form, so a manager cannot retype them into a different animal.
//   - The kid milk/refusal ladder reads HISTORY (refusals today, session, K1 vs K2
//     first-miss), not symptoms. It is follow-up logic over a context the form does
//     not carry, and it stays in milkProblem().
//   - The ranking pipeline -- tier over severity, residual resolution, suppression.
//     The register says WHICH rules exist; the engine decides how they compete.

// Question kinds. A yes/no finding is a `choice` with two options rather than its
// own kind: an author who later needs a third answer ("no / mild / severe") then
// adds an option instead of needing a new question and a new field everywhere.
const (
	QuestionChoice = "choice" // pick exactly one
	QuestionMulti  = "multi"  // pick one or more
	QuestionNumber = "number" // a measurement, banded into tokens
)

// Bounds. These are legibility and reviewability limits, not storage limits: a form
// past them stops being one head-to-toe pass over one animal.
const (
	MaxQuestions        = 120
	MaxOptionsPerQ      = 24
	MaxBandsPerQ        = 8
	MaxCorrections      = 32
	MaxEmitsPerAnswer   = 8
	MaxTitleLen         = 120
	MaxHintLen          = 400
	MaxRulesPerRegister = 200
)

// IDPattern is the shape of a question id, an option value and a correction id.
var IDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

// TokenPattern is the shape of an evidence token. Tokens are `family:value` or a
// bare word, and the register's own vocabulary uses both (`breathing:cough`,
// `frothy_mouth`, `FEVER`). Uppercase is conventional for a DERIVED token -- one no
// answer carries directly -- and the convention is not enforced, because enforcing
// it would break every register already written.
var TokenPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_>< .–-]*(:[A-Za-z0-9_>< .–-]+)?$`)

// Option is one answer a choice/multi question offers.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`

	// Emits is what ticking this answer puts into the evidence set. EMPTY IS
	// NORMAL and is the common case: "normal", "no", "none" are the absence of a
	// sign, not a sign named "none", and emitting them would put a normal finding
	// where a clause could anchor on it and where it would surface as an
	// unexplained finding.
	Emits []string `json:"emits,omitempty"`

	// ConflictsWith names answers on the SAME question this one cannot be ticked
	// beside. "None" beside a list of nervous signs conflicts with every one of
	// them; the phone clears them when it is picked and the engine refuses the pair.
	//
	// It is a LIST rather than an exclusive flag because the real rules are not
	// uniform: an animal that is off feed cannot also be eating concentrate, green
	// feed or normally -- but it CAN be eating dry feed, and a blanket exclusive
	// would quietly ban a combination the farm actually records.
	ConflictsWith []string `json:"conflicts_with,omitempty"`
}

// Band maps a numeric answer to tokens. Bounds are half-open and may be combined
// (`gte` with `lt`); the FIRST band that matches wins, so an author orders them
// most-severe first exactly as the clinical contract reads.
//
// Bands replace what deriveTokens hardcoded. The fever cut differs between an adult
// (> 103.5degF) and a kid (>= 103.5degF) by exactly one tenth of a degree, and that
// tenth decides whether a kid is treated at all -- authoring it per class is how the
// difference stays visible to the vet who owns it instead of living in a Go branch.
type Band struct {
	Gt    *float64 `json:"gt,omitempty"`
	Gte   *float64 `json:"gte,omitempty"`
	Lt    *float64 `json:"lt,omitempty"`
	Lte   *float64 `json:"lte,omitempty"`
	Emits []string `json:"emits"`
}

// matches reports whether v falls inside this band.
func (b Band) matches(v float64) bool {
	if b.Gt != nil && !(v > *b.Gt) {
		return false
	}
	if b.Gte != nil && !(v >= *b.Gte) {
		return false
	}
	if b.Lt != nil && !(v < *b.Lt) {
		return false
	}
	if b.Lte != nil && !(v <= *b.Lte) {
		return false
	}
	return true
}

func (b Band) bounded() bool {
	return b.Gt != nil || b.Gte != nil || b.Lt != nil || b.Lte != nil
}

// Condition hides a question unless an EARLIER question holds one of the listed
// answers.
//
// It takes a LIST rather than a single value because the rules it has to express are
// mostly negative ones -- "ask the CMT only when there is milk to test" covers four
// of the five lactation answers, and "grade the drop test only while the kid is on
// its feet" covers every activity except down. Written as equality those would need
// a NOT, and a condition language with negation in it is one an author has to reason
// about rather than read.
type Condition struct {
	QuestionID string   `json:"question_id"`
	In         []string `json:"in"`
}

// Question is one thing the manager is asked about the animal in front of them.
//
// THE FORM NEVER NAMES A DISEASE. The manager records what they see; the engine
// proposes and the Director confirms. A question whose title names a diagnosis is
// refused at publish -- see Validate.
type Question struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Hint    string `json:"hint,omitempty"`
	Section string `json:"section,omitempty"`

	Options []Option `json:"options,omitempty"`

	Bands []Band   `json:"bands,omitempty"`
	Unit  string   `json:"unit,omitempty"`
	Min   *float64 `json:"min,omitempty"`
	Max   *float64 `json:"max,omitempty"`

	// OnlyIfSex hides the question for the other sex -- udder and lactation for a
	// male, urine straining for a female. A hidden question records "not
	// applicable", never nothing, so the compulsory rule still holds.
	OnlyIfSex string `json:"only_if_sex,omitempty"`

	// OnlyIfStage hides the question outside the listed lifecycle stages. The stage
	// comes from the herd register, never from the form, for the same reason sex
	// does: it decides how a missed feed is READ, and a manager who could type it
	// could turn a real refusal into a learner's miss.
	OnlyIfStage []string `json:"only_if_stage,omitempty"`

	// OnlyIf hides the question unless an earlier question holds one of the answers.
	OnlyIf *Condition `json:"only_if,omitempty"`
}

// Correction rewrites the evidence set after the answers are read, for the cases
// where one finding changes how ANOTHER is read.
//
// There is exactly one in the seeded registers and it is the reason this exists as
// data rather than code: a goat with its stomach drawn in reads a skin tent one band
// worse than it is, so `>4s` beside `stomach_inside` is recorded as `2-4s`. Written
// in Go that rule would be keyed on two question ids, which would make those ids
// load-bearing -- a vet renaming `skin_tent` would break dehydration scoring with no
// error anywhere. Authored beside the questions it names, a rename is a publish-time
// refusal instead.
type Correction struct {
	ID string `json:"id"`
	// When fires the correction only when EVERY token listed is present.
	When   []string `json:"when"`
	Remove []string `json:"remove,omitempty"`
	Add    []string `json:"add,omitempty"`
	// Note is the clinical reason, shown to the author. It never reaches an operator.
	Note string `json:"note,omitempty"`
}

// AuthoredRegister is one published version: the form, the mapping and the rules.
type AuthoredRegister struct {
	RegisterVersion string   `json:"register_version"`
	AppliesClass    []string `json:"applies_class"`

	Questions   []Question   `json:"questions"`
	Corrections []Correction `json:"corrections,omitempty"`

	NonSpecific []string `json:"non_specific,omitempty"`
	Vocabulary  []string `json:"vocabulary,omitempty"`
	Rules       []Rule   `json:"rules"`
}

// AnimalTokenPrefixes are the token families the ENGINE supplies from the herd
// register rather than from an answer. A rule may name one without any question
// emitting it; nothing else may be named unemitted.
var AnimalTokenPrefixes = []string{"species:", "sex:", "status:", "stage:"}

// EngineEmittedTokens are the tokens code puts into the evidence set on its own.
// They are declared here so Validate can tell a legitimate one from a typo, and the
// list is deliberately short -- every entry is a piece of logic the decision doc
// records as staying in code.
// EngineReadTokens are tokens the ENGINE consumes rather than a rule.
//
// They exist so the milk ladder can read the form without reading a QUESTION ID. If
// milkProblem() looked up `milk_intake` by id, that id would become load-bearing and
// a vet renaming the question would silently disarm the refusal reading -- the same
// trap corrections exist to avoid. Reading a token instead means the register says
// which answer feeds the ladder, and the id is free to change.
//
// The orphan check counts these as read, which is why a form emitting one is not
// reported as a question that does nothing.
var EngineReadTokens = []string{
	// The bar reading as the manager saw it, before the refusal history is applied.
	"milk:refused_this_feed",
	"milk:reduced",
}

var EngineEmittedTokens = []string{
	// milkProblem() reads the refusal history and the milk-bar stage, not a symptom.
	"milk_intake:not_drinking",
	"milk_intake:reduced",
	// A milk kid off the bar is off feed: the bar IS the diet at that stage.
	"eating:not_eating",
}

func isAnimalToken(tok string) bool {
	for _, p := range AnimalTokenPrefixes {
		if strings.HasPrefix(tok, p) {
			return true
		}
	}
	return false
}

func isEngineToken(tok string) bool {
	for _, t := range EngineEmittedTokens {
		if t == tok {
			return true
		}
	}
	return false
}

func isEngineReadToken(tok string) bool {
	for _, t := range EngineReadTokens {
		if t == tok {
			return true
		}
	}
	return false
}

// LoadAuthored parses a published register document.
//
// Unknown fields are REJECTED, for the same reason the YAML loader rejects them: a
// key nothing reads is an accept-and-discard. It looks authored, changes no
// behaviour, and reads to the next author as already honoured. On a clinical rule
// table that is the worst possible failure mode, so it fails loudly instead.
func LoadAuthored(src []byte) (*AuthoredRegister, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.DisallowUnknownFields()

	var reg AuthoredRegister
	if err := dec.Decode(&reg); err != nil {
		return nil, fmt.Errorf("decode register: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("decode register: trailing content after the document")
	}
	return &reg, nil
}

// Problem is one thing wrong with an authored register. Fatal problems refuse the
// publish; the rest are shown to the author and allow it.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	Fatal   bool   `json:"fatal"`
}

func (p Problem) String() string { return p.Path + ": " + p.Message }

// Problems is the whole verdict on one document.
type Problems []Problem

func (ps Problems) Fatal() bool {
	for _, p := range ps {
		if p.Fatal {
			return true
		}
	}
	return false
}

func (ps Problems) Error() string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.String())
	}
	return strings.Join(out, "; ")
}

// CompileRegister turns a published authored document into the rule table the
// diagnosis engine evaluates.
//
// The authored document owns the form and the answer mapping too, but the current
// observation endpoint still receives the legacy Findings struct. Compiling the
// rules here is the bridge that makes publishing a register change the next
// diagnosis run without changing the wire shape in the same PR.
func (a AuthoredRegister) CompileRegister(class string) (*Register, error) {
	if strings.TrimSpace(a.RegisterVersion) == "" {
		return nil, fmt.Errorf("register has no register_version")
	}
	if len(a.Rules) == 0 {
		return nil, fmt.Errorf("register %s has no rules", a.RegisterVersion)
	}
	if len(a.AppliesClass) > 0 && !containsString(a.AppliesClass, class) {
		return nil, fmt.Errorf("health: %s authored register applies_class=%v does not claim class %s",
			class, a.AppliesClass, class)
	}

	reg := &Register{
		Version:      a.RegisterVersion,
		AppliesClass: append([]string(nil), a.AppliesClass...),
		NonSpecific:  append([]string(nil), a.NonSpecific...),
		Vocabulary:   append([]string(nil), a.Vocabulary...),
		Rules:        append([]Rule(nil), a.Rules...),
		boundClass:   class,
	}
	reg.byID = make(map[string]*Rule, len(reg.Rules))
	reg.quarantine = make(map[string]bool)
	for i := range reg.Rules {
		r := &reg.Rules[i]
		if strings.TrimSpace(r.ID) == "" {
			return nil, fmt.Errorf("register %s: rule at index %d has no id", reg.Version, i)
		}
		if _, dup := reg.byID[r.ID]; dup {
			return nil, fmt.Errorf("register %s: duplicate rule id %s", reg.Version, r.ID)
		}
		applyRuleDefaults(r)
		reg.byID[r.ID] = r
		if r.Containment == "quarantine" {
			reg.quarantine[r.ID] = true
		}
	}
	reg.nonSpecific = toSet(reg.NonSpecific)
	reg.vocabulary = toSet(reg.Vocabulary)
	if errs := reg.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("health: %s authored register failed validation: %w", class, errs[0])
	}
	return reg, nil
}
