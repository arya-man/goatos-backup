// Package diagnosis implements the deterministic adult health SOP engine.
//
// It is a PURE package: no database, no HTTP, no clock, no I/O. A form plus an
// animal plus a follow-up context go in; a Proposal comes out. Same input, same
// output, every time. That purity is what lets the 180-story acceptance catalog
// (adult-1) act as the contract, and it is why this package holds no adapters.
//
// The algorithm is specified in vgoats/health-sop:
//
//	engine/ALGORITHM.md      the ordered pipeline
//	adult/register.yaml      the rule table (register_version: adult-1)
//	adult/DIRECTOR_ENGINE.md the clinical contract
//
// Three properties of the design are load-bearing and easy to break:
//
//  1. Rules evaluate in PARALLEL over an evidence set. There is no tree, no
//     ordering dependence and no backtracking. Co-morbidity is normal, and the
//     absence of a sign does not exclude a disease.
//  2. The output is a PROPOSAL. Nothing here closes a problem, culls an animal,
//     or moves it between sheds. Housing is a directive that some other module
//     applies; the engine never writes location or lifecycle state.
//  3. Every run pins the register version it ran against, so a case stays
//     interpretable after the rule table is edited.
package diagnosis

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Tier is the confidence a rule reached. It is a separate axis from severity:
// a POSSIBLE bloat must outrank a CONFIRMED pinkeye, because ranking on
// confidence alone treats the eye while the bladder ruptures.
type Tier string

const (
	TierConfirmed Tier = "CONFIRMED"
	TierProbable  Tier = "PROBABLE"
	TierPossible  Tier = "POSSIBLE"
)

func (t Tier) rank() int {
	switch t {
	case TierConfirmed:
		return 3
	case TierProbable:
		return 2
	case TierPossible:
		return 1
	default:
		return 0
	}
}

// Kind separates a diagnosis from an action taken in place. A field action is
// treated once, never enters ICU, and raises no daily follow-up — without this
// distinction minor findings either flooded ICU or vanished entirely.
type Kind string

const (
	KindProblem Kind = "problem"
	KindField   Kind = "field"
)

// Clause is an AND of findings. A rule matches on the FIRST clause that matches,
// scanned pathognomonic -> probable -> possible, so clause order inside a tier
// is irrelevant but tier order is not.
//
// A residual clause fires only when every specific finding in it is still
// unexplained by a specific hit. That is what keeps symptom-labels (Fever,
// Red urine, Wounds) from opening a phantom problem alongside the disease that
// already produced those findings — and it is why the register carries almost
// no suppression pairs.
type Clause struct {
	Findings []string `yaml:"findings"`
	Residual bool     `yaml:"residual"`
}

// UnmarshalYAML accepts both clause spellings the register uses: the mapping
// form (`- findings: [a, b]`, optionally with `residual: true`) and the bare
// sequence form (`- [a, b]`). The reference loader accepts both, so rejecting
// one here would silently drop rules from a register a vet legitimately edited.
func (c *Clause) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		var findings []string
		if err := value.Decode(&findings); err != nil {
			return fmt.Errorf("clause sequence: %w", err)
		}
		c.Findings = findings
		c.Residual = false
		return nil
	case yaml.MappingNode:
		var raw struct {
			Findings []string `yaml:"findings"`
			Residual bool     `yaml:"residual"`
		}
		if err := value.Decode(&raw); err != nil {
			return fmt.Errorf("clause mapping: %w", err)
		}
		c.Findings = raw.Findings
		c.Residual = raw.Residual
		return nil
	default:
		return fmt.Errorf("clause must be a mapping or a sequence, got yaml kind %d", value.Kind)
	}
}

// SeverityModifier may only ESCALATE severity, never reduce it. Applying it as
// max() rather than assignment is deliberate: a modifier is a statement that
// this presentation is at least this bad.
type SeverityModifier struct {
	Finding  string `yaml:"finding"`
	Severity int    `yaml:"severity"`
}

// Rule is one row of the register.
type Rule struct {
	ID   string `yaml:"id"`
	Kind Kind   `yaml:"kind"`

	AppliesSpecies []string `yaml:"applies_species"`
	AppliesSex     []string `yaml:"applies_sex"`
	AppliesStatus  []string `yaml:"applies_status"`

	// GateRequired and GateExcluded are hard filters evaluated before any
	// clause. A gate also supplies the specificity that lets an otherwise
	// non-specific clause be anchored.
	GateRequired []string `yaml:"gate_required"`
	GateExcluded []string `yaml:"gate_excluded"`

	// HumanSelectedOnly rules never fire automatically. Heat Stress is the only
	// one: `pant` raises a Director-confirm flag, it does not open a course.
	HumanSelectedOnly bool `yaml:"human_selected_only"`

	Pathognomonic []Clause `yaml:"pathognomonic"`
	Probable      []Clause `yaml:"probable"`
	Possible      []Clause `yaml:"possible"`

	SeverityBase      int                `yaml:"severity_base"`
	SeverityModifiers []SeverityModifier `yaml:"severity_modifiers"`

	// AcuteActionable separates "bad" from "bad in the next ten minutes". PPR is
	// severity 4 with no acute intervention; bloat is severity 4 and entirely
	// about the next ten minutes. Conflating them designs in alarm fatigue.
	AcuteActionable bool   `yaml:"acute_actionable"`
	TreatmentRisk   string `yaml:"treatment_risk"`

	ExitType    string `yaml:"exit_type"`
	SOPRef      string `yaml:"sop_ref"`
	Containment string `yaml:"containment"`

	// ExplainsFindings marks findings this diagnosis accounts for even though no
	// clause matched on them, so a residual rule does not re-open them.
	ExplainsFindings []string `yaml:"explains_findings"`

	// Suppresses is specific-suppresses-specific only. A suppressed rule stays
	// on the record as covered, never deleted: if the animal is not improving,
	// the covered diagnosis is the first thing re-opened.
	Suppresses []string `yaml:"suppresses"`

	RecheckIDPossible string   `yaml:"recheck_id_possible"`
	AdjunctWhen       []string `yaml:"adjunct_when"`
}

// Register is the loaded rule table, pinned to a version.
type Register struct {
	Version string `yaml:"register_version"`

	// NonSpecific findings appear in almost every sick animal. They never enter
	// the explained set even when a clause matched on them, which is what keeps
	// the unexplained-findings channel alive.
	NonSpecific []string `yaml:"non_specific"`
	Vocabulary  []string `yaml:"vocabulary"`
	Rules       []Rule   `yaml:"rules"`

	byID        map[string]*Rule
	nonSpecific map[string]bool
	vocabulary  map[string]bool
	quarantine  map[string]bool
}

// Load parses a register from bytes.
//
// It takes bytes rather than a path so the caller decides where the register
// lives — a committed file today, an authored and version-published row later.
// That choice is deliberately not made here.
//
// Unknown fields are REJECTED. A register key nothing reads is an
// accept-and-discard: it looks authored, changes no behaviour, and reads to the
// next author as already honoured. Failing loudly is the only safe option for a
// clinical rule table.
func Load(src []byte) (*Register, error) {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)

	var reg Register
	if err := dec.Decode(&reg); err != nil {
		return nil, fmt.Errorf("decode register: %w", err)
	}
	if strings.TrimSpace(reg.Version) == "" {
		return nil, fmt.Errorf("register has no register_version")
	}
	if len(reg.Rules) == 0 {
		return nil, fmt.Errorf("register %s has no rules", reg.Version)
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
	return &reg, nil
}

func applyRuleDefaults(r *Rule) {
	if r.Kind == "" {
		r.Kind = KindProblem
	}
	if len(r.AppliesSpecies) == 0 {
		r.AppliesSpecies = []string{"goat", "sheep"}
	}
	if len(r.AppliesSex) == 0 {
		r.AppliesSex = []string{"M", "F"}
	}
	if len(r.AppliesStatus) == 0 {
		r.AppliesStatus = []string{"any"}
	}
	if r.SeverityBase == 0 {
		r.SeverityBase = 2
	}
	if r.TreatmentRisk == "" {
		r.TreatmentRisk = "low"
	}
	if r.ExitType == "" {
		r.ExitType = "F"
	}
	if r.Containment == "" {
		r.Containment = "home"
	}
}

// Rule returns the rule with this id, or nil.
func (r *Register) Rule(id string) *Rule {
	if r == nil {
		return nil
	}
	return r.byID[id]
}

// IsNonSpecific reports whether a token is one that appears in almost every sick
// animal and therefore may not vote on identity.
func (r *Register) IsNonSpecific(token string) bool { return r.nonSpecific[token] }

func (r *Rule) clausesByTier() []struct {
	tier    Tier
	name    string
	clauses []Clause
} {
	return []struct {
		tier    Tier
		name    string
		clauses []Clause
	}{
		{TierConfirmed, "pathognomonic", r.Pathognomonic},
		{TierProbable, "probable", r.Probable},
		{TierPossible, "possible", r.Possible},
	}
}

// Validate runs Test 1, the structural validators, over a loaded register.
//
// These run on every edit because reading a rule table for consistency never
// terminates — it asymptotes — while these three checks do terminate:
//
//	vocabulary   a clause may not reference a finding the schema does not define
//	anchoring    a clause may not consist only of non-specific findings
//	reachability a lower-tier clause may not be a superset of a higher-tier one
//
// The reachability check compares ALL THREE tier pairs, including
// probable-vs-possible. Checking only against pathognomonic found four of the
// five dead clauses in the original drafts; the fifth was visible only once the
// probable-vs-possible pair was checked too.
func (r *Register) Validate() []error {
	var errs []error
	for i := range r.Rules {
		rule := &r.Rules[i]
		if rule.HumanSelectedOnly {
			continue
		}
		for _, gate := range append(append([]string{}, rule.GateRequired...), rule.GateExcluded...) {
			if !r.vocabulary[gate] {
				errs = append(errs, fmt.Errorf("%s: gate %q not in vocabulary", rule.ID, gate))
			}
		}

		// higher holds every clause seen so far, in tier order, so each clause is
		// compared against every strictly-higher-tier clause of the same rule.
		var higher []map[string]bool
		for _, group := range rule.clausesByTier() {
			for _, cl := range group.clauses {
				for _, tok := range cl.Findings {
					if !r.vocabulary[tok] {
						errs = append(errs, fmt.Errorf("%s %s: %q not in vocabulary", rule.ID, group.name, tok))
					}
				}
				if err := r.checkAnchored(rule, group.name, cl); err != nil {
					errs = append(errs, err)
				}
				set := toSet(cl.Findings)
				for _, hs := range higher {
					if isStrictSuperset(set, hs) {
						errs = append(errs, fmt.Errorf(
							"%s %s %v is a superset of higher-tier %v (unreachable)",
							rule.ID, group.name, sortedKeys(set), sortedKeys(hs)))
					}
				}
				higher = append(higher, set)
			}
		}
	}
	return errs
}

// checkAnchored enforces that a clause carries something that votes on identity.
// A status token counts as the anchor, whether it sits in the clause or in the
// rule's own gate — a clause of only non-specific findings would fire on nearly
// every sick animal.
func (r *Register) checkAnchored(rule *Rule, tierName string, cl Clause) error {
	hasSpecific := false
	statusAnchor := false
	for _, tok := range cl.Findings {
		if strings.HasPrefix(tok, "status:") {
			statusAnchor = true
			continue
		}
		if !r.nonSpecific[tok] {
			hasSpecific = true
		}
	}
	for _, gate := range rule.GateRequired {
		if strings.HasPrefix(gate, "status:") {
			statusAnchor = true
		}
	}
	if !hasSpecific && !statusAnchor {
		return fmt.Errorf("%s %s %v: unanchored (only non-specific findings)", rule.ID, tierName, cl.Findings)
	}
	return nil
}

func toSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

func isStrictSuperset(a, b map[string]bool) bool {
	if len(a) <= len(b) {
		return false
	}
	for k := range b {
		if !a[k] {
			return false
		}
	}
	return true
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
