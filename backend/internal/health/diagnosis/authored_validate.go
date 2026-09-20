package diagnosis

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Validate names everything wrong with an authored register.
//
// It returns EVERY problem rather than the first, because an author fixing a
// register one refusal per publish learns nothing about the shape of the mistake --
// and because the two directions below are usually made together.
//
// THE TWO DIRECTIONS ARE THE POINT:
//
//	a rule clause naming a token NO answer emits   -> a disease that can never fire
//	an answer emitting a token NO rule reads       -> a question that does nothing
//
// The first is always fatal. The second is fatal UNLESS the token is declared in
// `vocabulary`, which is how a vet stages a symptom in one publish and writes the
// rule that reads it in the next. Declaring it is the author saying "I meant this";
// an undeclared orphan is a typo, and the two are indistinguishable without it.
func (a *AuthoredRegister) Validate() Problems {
	var ps Problems
	add := func(path, format string, args ...any) {
		ps = append(ps, Problem{Path: path, Message: fmt.Sprintf(format, args...), Fatal: true})
	}
	warn := func(path, format string, args ...any) {
		ps = append(ps, Problem{Path: path, Message: fmt.Sprintf(format, args...), Fatal: false})
	}

	if strings.TrimSpace(a.RegisterVersion) == "" {
		add("register_version", "required")
	}
	if len(a.AppliesClass) == 0 {
		add("applies_class", "name at least one animal class this register serves")
	}
	for i, c := range a.AppliesClass {
		if !knownClass(c) {
			add(fmt.Sprintf("applies_class.%d", i), "%q is not an animal class", c)
		}
	}

	emitted := a.validateQuestions(add, warn)
	a.validateCorrections(emitted, add)
	a.validateRules(emitted, add, warn)

	return ps
}

func knownClass(c string) bool {
	for _, k := range Classes {
		if k == c {
			return true
		}
	}
	return false
}

// validateQuestions checks the form half and returns every token the answers can
// emit, mapped to the path that emits it.
func (a *AuthoredRegister) validateQuestions(add, warn func(string, string, ...any)) map[string]string {
	emitted := map[string]string{}

	if len(a.Questions) == 0 {
		add("questions", "a register with no questions can diagnose nothing")
	}
	if len(a.Questions) > MaxQuestions {
		add("questions", "at most %d questions on one form", MaxQuestions)
	}

	seen := map[string]int{}
	for i, q := range a.Questions {
		p := fmt.Sprintf("questions.%d", i)

		if !IDPattern.MatchString(q.ID) {
			add(p+".id", "%q must be a-z, 0-9 and _ (start with a letter)", q.ID)
		}
		if prev, dup := seen[q.ID]; dup {
			add(p+".id", "%q is already question %d", q.ID, prev)
		}
		seen[q.ID] = i

		if strings.TrimSpace(q.Title) == "" {
			add(p+".title", "required")
		}
		if len(q.Title) > MaxTitleLen {
			add(p+".title", "too long (max %d)", MaxTitleLen)
		}
		if len(q.Hint) > MaxHintLen {
			add(p+".hint", "too long (max %d)", MaxHintLen)
		}
		if q.OnlyIfSex != "" && q.OnlyIfSex != "M" && q.OnlyIfSex != "F" {
			add(p+".only_if_sex", "%q is not M or F", q.OnlyIfSex)
		}
		for j, st := range q.OnlyIfStage {
			if strings.TrimSpace(st) == "" {
				add(fmt.Sprintf("%s.only_if_stage.%d", p, j), "blank")
			}
		}

		switch q.Kind {
		case QuestionChoice, QuestionMulti:
			a.validateOptions(p, q, emitted, add)
			if len(q.Bands) > 0 {
				add(p+".bands", "bands belong to a number question")
			}
		case QuestionNumber:
			a.validateBands(p, q, emitted, add)
			if len(q.Options) > 0 {
				add(p+".options", "options belong to a choice question")
			}
		default:
			add(p+".kind", "%q is not choice / multi / number", q.Kind)
		}

		a.validateCondition(p, i, q, add)
	}

	_ = warn
	return emitted
}

func (a *AuthoredRegister) validateOptions(p string, q Question, emitted map[string]string, add func(string, string, ...any)) {
	if len(q.Options) < 2 {
		add(p+".options", "a question needs at least two answers")
	}
	if len(q.Options) > MaxOptionsPerQ {
		add(p+".options", "at most %d answers (max %d)", MaxOptionsPerQ, MaxOptionsPerQ)
	}
	seen := map[string]bool{}
	for j, o := range q.Options {
		op := fmt.Sprintf("%s.options.%d", p, j)
		if !IDPattern.MatchString(o.Value) {
			add(op+".value", "%q must be a-z, 0-9 and _ (start with a letter)", o.Value)
		}
		if seen[o.Value] {
			add(op+".value", "%q is listed twice", o.Value)
		}
		seen[o.Value] = true
		if strings.TrimSpace(o.Label) == "" {
			add(op+".label", "required")
		}
		if len(o.ConflictsWith) > 0 && q.Kind != QuestionMulti {
			add(op+".conflicts_with", "only a pick-many answer can conflict with another")
		}
		for k, other := range o.ConflictsWith {
			cp := fmt.Sprintf("%s.conflicts_with.%d", op, k)
			if other == o.Value {
				add(cp, "an answer cannot conflict with itself")
			} else if q.option(other) == nil {
				add(cp, "%q is not an answer to this question", other)
			}
		}
		validateEmits(op, o.Emits, q.ID+"."+o.Value, emitted, add)
	}
}

// validateBands refuses a band that can never be reached, which is the mistake this
// check exists for: bands are FIRST-MATCH-WINS, so authoring them least-severe first
// ("103.5 and up is a fever", then "106 and up is a high fever") leaves the high
// fever permanently unreachable -- a silent under-read of the sickest animals.
//
// Overlap itself is legitimate and is how the seeded registers are written; a GAP is
// legitimate too, and is how a normal temperature emits nothing at all.
func (a *AuthoredRegister) validateBands(p string, q Question, emitted map[string]string, add func(string, string, ...any)) {
	if len(q.Bands) == 0 {
		add(p+".bands", "a measurement with no bands emits nothing and can diagnose nothing")
	}
	if len(q.Bands) > MaxBandsPerQ {
		add(p+".bands", "at most %d bands on one measurement", MaxBandsPerQ)
	}
	if q.Min != nil && q.Max != nil && *q.Min > *q.Max {
		add(p+".min", "the smallest value is larger than the largest")
	}

	type iv struct{ lo, hi float64 }
	spans := make([]iv, len(q.Bands))

	for j, b := range q.Bands {
		bp := fmt.Sprintf("%s.bands.%d", p, j)
		if !b.bounded() && j != len(q.Bands)-1 {
			add(bp, "a band with no bounds matches every value, so nothing after it is reachable")
		}
		if b.Gt != nil && b.Gte != nil {
			add(bp, "set either gt or gte, not both")
		}
		if b.Lt != nil && b.Lte != nil {
			add(bp, "set either lt or lte, not both")
		}
		if len(b.Emits) == 0 {
			add(bp+".emits", "a band that emits nothing has no effect")
		}
		validateEmits(bp, b.Emits, fmt.Sprintf("%s band %d", q.ID, j), emitted, add)

		lo, hi := math.Inf(-1), math.Inf(1)
		if b.Gt != nil {
			lo = *b.Gt
		}
		if b.Gte != nil {
			lo = *b.Gte
		}
		if b.Lt != nil {
			hi = *b.Lt
		}
		if b.Lte != nil {
			hi = *b.Lte
		}
		if lo > hi {
			add(bp, "the lower bound is above the upper bound")
		}
		spans[j] = iv{lo, hi}

		for k := 0; k < j; k++ {
			if spans[k].lo <= lo && hi <= spans[k].hi {
				add(bp, "band %d already covers every value this band matches, so it can never fire", k)
				break
			}
		}
	}
}

func (a *AuthoredRegister) validateCondition(p string, idx int, q Question, add func(string, string, ...any)) {
	if q.OnlyIf == nil {
		return
	}
	cp := p + ".only_if"
	var src *Question
	for i := range a.Questions {
		if a.Questions[i].ID == q.OnlyIf.QuestionID {
			if i >= idx {
				// A forward reference would need the form answered out of order, and
				// a self-reference can never be satisfied.
				add(cp+".question_id", "%q is not asked before this question", q.OnlyIf.QuestionID)
				return
			}
			src = &a.Questions[i]
			break
		}
	}
	if src == nil {
		add(cp+".question_id", "%q is not a question on this form", q.OnlyIf.QuestionID)
		return
	}
	if len(q.OnlyIf.In) == 0 {
		add(cp+".in", "name at least one answer that makes this question appear")
		return
	}
	for j, v := range q.OnlyIf.In {
		if src.option(v) == nil {
			add(fmt.Sprintf("%s.in.%d", cp, j), "%q is not an answer to %q", v, q.OnlyIf.QuestionID)
		}
	}
	// A condition covering EVERY answer never hides anything, which means the author
	// meant a different question or a shorter list -- either way the form does not do
	// what the register says it does.
	if len(q.OnlyIf.In) >= len(src.Options) {
		add(cp+".in", "this covers every answer to %q, so the question is never hidden", q.OnlyIf.QuestionID)
	}
}

// A CHECK THAT WAS TRIED AND REMOVED, recorded so it is not re-proposed.
//
// "The form never names a disease" is a real rule -- the manager records what they
// see and the engine names the illness -- and refusing a question titled after a rule
// looked like the way to enforce it. It cannot be: this register deliberately carries
// SYMPTOM-LABEL rules (RED_URINE, WOUNDS, LUMPS, TICKS, FEVER), whose whole job is to
// surface a finding no diagnosis accounted for. Their ids ARE sign names, so the
// check refused four of the farm's own questions -- "Red urine", "Wounds", "Lumps",
// "Ticks" -- for being named after the signs they record.
//
// A guard that refuses the correct register is worse than no guard, and no mechanical
// test separates "Pneumonia?" from "Red urine" here. The rule stays a review rule.

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, "?")
	repl := strings.NewReplacer("_", " ", "-", " ", ".", " ")
	return strings.Join(strings.Fields(repl.Replace(s)), " ")
}

func validateEmits(path string, emits []string, source string, emitted map[string]string, add func(string, string, ...any)) {
	if len(emits) > MaxEmitsPerAnswer {
		add(path+".emits", "at most %d tokens from one answer", MaxEmitsPerAnswer)
	}
	seen := map[string]bool{}
	for k, tok := range emits {
		ep := fmt.Sprintf("%s.emits.%d", path, k)
		if !TokenPattern.MatchString(tok) {
			add(ep, "%q is not a finding token", tok)
			continue
		}
		if seen[tok] {
			add(ep, "%q is listed twice on the same answer", tok)
		}
		seen[tok] = true
		if isAnimalToken(tok) {
			// The herd register owns these. An answer that could emit `sex:F` would
			// let a manager's tick override the animal's own record.
			add(ep, "%q is supplied by the herd register and may not be emitted by an answer", tok)
			continue
		}
		if _, dup := emitted[tok]; !dup {
			emitted[tok] = source
		}
	}
}

func (a *AuthoredRegister) validateCorrections(emitted map[string]string, add func(string, string, ...any)) {
	if len(a.Corrections) > MaxCorrections {
		add("corrections", "at most %d corrections", MaxCorrections)
	}
	seen := map[string]bool{}
	for i, c := range a.Corrections {
		p := fmt.Sprintf("corrections.%d", i)
		if !IDPattern.MatchString(c.ID) {
			add(p+".id", "%q must be a-z, 0-9 and _ (start with a letter)", c.ID)
		}
		if seen[c.ID] {
			add(p+".id", "%q is listed twice", c.ID)
		}
		seen[c.ID] = true
		if len(c.When) == 0 {
			add(p+".when", "a correction with no condition would rewrite every observation")
		}
		if len(c.Remove) == 0 && len(c.Add) == 0 {
			add(p+".", "a correction that neither removes nor adds a token has no effect")
		}
		for j, tok := range c.When {
			if _, ok := emitted[tok]; !ok && !isAnimalToken(tok) && !isEngineToken(tok) {
				add(fmt.Sprintf("%s.when.%d", p, j), "no answer on this form emits %q", tok)
			}
		}
		for j, tok := range c.Remove {
			if _, ok := emitted[tok]; !ok {
				add(fmt.Sprintf("%s.remove.%d", p, j), "no answer on this form emits %q, so there is nothing to remove", tok)
			}
		}
		// An ADDED token needs no emitter -- adding one is precisely how a
		// correction introduces a reading no single answer carries. It is checked
		// against the rules below like any other token.
		for j, tok := range c.Add {
			if !TokenPattern.MatchString(tok) {
				add(fmt.Sprintf("%s.add.%d", p, j), "%q is not a finding token", tok)
				continue
			}
			if _, dup := emitted[tok]; !dup {
				emitted[tok] = "correction " + c.ID
			}
		}
	}
}

func (a *AuthoredRegister) validateRules(emitted map[string]string, add, warn func(string, string, ...any)) {
	if len(a.Rules) == 0 {
		add("rules", "a register with no rules can diagnose nothing")
	}
	if len(a.Rules) > MaxRulesPerRegister {
		add("rules", "at most %d rules in one register", MaxRulesPerRegister)
	}

	vocab := map[string]bool{}
	for _, v := range a.Vocabulary {
		vocab[v] = true
	}

	read := map[string]bool{}
	seen := map[string]bool{}

	// A correction that reads a token is a consumer of it as surely as a clause is.
	for _, c := range a.Corrections {
		for _, tok := range c.When {
			read[tok] = true
		}
		for _, tok := range c.Remove {
			read[tok] = true
		}
	}

	for i := range a.Rules {
		r := a.Rules[i]
		p := fmt.Sprintf("rules.%d", i)
		if strings.TrimSpace(r.ID) == "" {
			add(p+".id", "required")
		}
		if seen[r.ID] {
			add(p+".id", "%q is listed twice", r.ID)
		}
		seen[r.ID] = true

		if r.SeverityBase < 0 || r.SeverityBase > 5 {
			add(p+".severity_base", "severity is 1-5, got %d", r.SeverityBase)
		}

		check := func(sub string, clauses []Clause) {
			for j, c := range clauses {
				if len(c.Findings) == 0 {
					add(fmt.Sprintf("%s.%s.%d", p, sub, j), "a clause with no findings matches every animal")
				}
				for k, tok := range c.Findings {
					read[tok] = true
					if _, ok := emitted[tok]; ok || isAnimalToken(tok) || isEngineToken(tok) {
						continue
					}
					add(fmt.Sprintf("%s.%s.%d.findings.%d", p, sub, j, k),
						"no answer on this form emits %q, so %s can never be diagnosed", tok, r.ID)
				}
			}
		}
		check("pathognomonic", r.Pathognomonic)
		check("probable", r.Probable)
		check("possible", r.Possible)

		for j, tok := range r.GateRequired {
			read[tok] = true
			if _, ok := emitted[tok]; !ok && !isAnimalToken(tok) && !isEngineToken(tok) {
				add(fmt.Sprintf("%s.gate_required.%d", p, j), "no answer on this form emits %q", tok)
			}
		}
		for j, tok := range r.GateExcluded {
			read[tok] = true
			if _, ok := emitted[tok]; !ok && !isAnimalToken(tok) && !isEngineToken(tok) {
				add(fmt.Sprintf("%s.gate_excluded.%d", p, j), "no answer on this form emits %q", tok)
			}
		}
		for _, tok := range r.ExplainsFindings {
			read[tok] = true
		}
		for _, m := range r.SeverityModifiers {
			read[m.Finding] = true
			if _, ok := emitted[m.Finding]; !ok && !isAnimalToken(m.Finding) && !isEngineToken(m.Finding) {
				add(p+".severity_modifiers", "no answer on this form emits %q", m.Finding)
			}
		}
		for j, id := range r.Suppresses {
			if !ruleExists(a.Rules, id) {
				add(fmt.Sprintf("%s.suppresses.%d", p, j), "%q is not a rule in this register", id)
			}
		}
		if r.Treats != "" && !ValidTreatsKey(r.Treats) {
			add(p+".treats", "%q must be a-z, 0-9 and _ (start with a letter)", r.Treats)
		}
	}

	// A non-specific token nothing emits is INERT -- the list only ever stops a
	// token from voting on identity, so an entry that can never appear changes
	// nothing. That is a different failure from a rule clause, which silently
	// disables a whole diagnosis, so it is reported and not refused.
	//
	// The adult register ships one: `head_position:down`, declared non-specific
	// while no question has ever asked about head position.
	for _, tok := range a.NonSpecific {
		read[tok] = true
		if _, ok := emitted[tok]; !ok && !isAnimalToken(tok) && !isEngineToken(tok) {
			warn("non_specific", "no answer on this form emits %q, so listing it has no effect", tok)
		}
	}

	// The second direction: an answer that reaches no rule.
	orphans := make([]string, 0)
	for tok := range emitted {
		if !read[tok] && !isEngineReadToken(tok) {
			orphans = append(orphans, tok)
		}
	}
	sort.Strings(orphans)
	for _, tok := range orphans {
		if vocab[tok] {
			warn("questions", "%q (from %s) is declared but no rule reads it yet", tok, emitted[tok])
			continue
		}
		add("questions", "%q (from %s) reaches no rule; declare it in vocabulary if the rule is still to come",
			tok, emitted[tok])
	}
}

func ruleExists(rules []Rule, id string) bool {
	for i := range rules {
		if rules[i].ID == id {
			return true
		}
	}
	return false
}

// ValidTreatsKey reports whether a rule's treats reference is shaped like a disease
// key. Whether such a protocol is actually PUBLISHED is checked inside the publish
// transaction, where the protocol table can be read.
func ValidTreatsKey(k string) bool { return IDPattern.MatchString(k) }
