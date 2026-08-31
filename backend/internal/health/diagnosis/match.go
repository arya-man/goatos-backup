package diagnosis

import "sort"

// hit is one rule that fired, with everything arbitration needs to rank it or
// throw it away.
type hit struct {
	rule       *Rule
	tier       Tier
	severity   int
	residual   bool
	matched    []string
	constraint int
}

// matchResult is the output of the parallel-match and arbitration layers,
// before reconcile and housing.
type matchResult struct {
	problems     []hit
	rechecks     []string
	fieldActions []string
	covered      []string
	unexplained  []string
	adjunct      bool
	tiers        map[string]Tier
}

// applicable filters the register down to rules that can fire on this animal at
// all, before any clause is considered.
func applicable(rule *Rule, animal Animal, evidence map[string]bool) bool {
	if rule.HumanSelectedOnly {
		return false
	}
	if !contains(rule.AppliesSpecies, animal.species()) {
		return false
	}
	if !contains(rule.AppliesSex, animal.Sex) {
		return false
	}
	if !contains(rule.AppliesStatus, "any") && !contains(rule.AppliesStatus, animal.status()) {
		return false
	}
	for _, gate := range rule.GateRequired {
		if !evidence[gate] {
			return false
		}
	}
	for _, gate := range rule.GateExcluded {
		if evidence[gate] {
			return false
		}
	}
	return true
}

// firstMatch scans pathognomonic -> probable -> possible and returns the first
// clause that matches, which sets the rule's tier. residualPass selects which
// half of the two-pass evaluation this is.
func firstMatch(rule *Rule, evidence map[string]bool, residualPass bool) (Tier, Clause, bool) {
	for _, group := range rule.clausesByTier() {
		for _, cl := range group.clauses {
			if cl.Residual != residualPass {
				continue
			}
			if clauseMatches(cl, evidence) {
				return group.tier, cl, true
			}
		}
	}
	return "", Clause{}, false
}

// clauseMatches is an AND over the clause's findings. An empty clause never
// matches — it would fire on every animal.
func clauseMatches(cl Clause, evidence map[string]bool) bool {
	if len(cl.Findings) == 0 {
		return false
	}
	for _, tok := range cl.Findings {
		if !evidence[tok] {
			return false
		}
	}
	return true
}

// severityFor applies modifiers as max(), so a modifier can only escalate.
func severityFor(rule *Rule, evidence map[string]bool) int {
	sev := rule.SeverityBase
	for _, mod := range rule.SeverityModifiers {
		if mod.Finding == "" || mod.Severity == 0 {
			continue
		}
		if evidence[mod.Finding] && mod.Severity > sev {
			sev = mod.Severity
		}
	}
	return sev
}

// evaluateRegister runs layers 3 to 6: applicability, the two-pass parallel
// match, residual arbitration, suppression, ranking and record typing.
func (r *Register) evaluateRegister(animal Animal, evidence map[string]bool) matchResult {
	var candidates []*Rule
	for i := range r.Rules {
		rule := &r.Rules[i]
		if applicable(rule, animal, evidence) {
			candidates = append(candidates, rule)
		}
	}

	// PASS 1 — specific clauses only.
	var specific []hit
	for _, rule := range candidates {
		tier, cl, ok := firstMatch(rule, evidence, false)
		if !ok {
			continue
		}
		specific = append(specific, hit{
			rule:       rule,
			tier:       tier,
			severity:   severityFor(rule, evidence),
			matched:    cl.Findings,
			constraint: len(rule.GateRequired) + len(cl.Findings),
		})
	}

	// The explained set is what pass 2 subtracts from.
	//
	// Two exclusions are load-bearing. Non-specific tokens never enter it even
	// when a clause matched on them — that is what keeps the unexplained-findings
	// channel alive. And FIELD ACTIONS EXPLAIN NOTHING: a snot-alone
	// antihistamine must not make nasal discharge count as accounted for, or
	// cough+nasal would read as "already explained" and never open Fever.
	explained := map[string]bool{}
	for _, h := range specific {
		if h.rule.Kind != KindProblem {
			continue
		}
		for _, tok := range h.matched {
			if !r.nonSpecific[tok] {
				explained[tok] = true
			}
		}
		for _, tok := range h.rule.ExplainsFindings {
			explained[tok] = true
		}
	}

	// PASS 2 — residual clauses. A residual rule fires only if at least one of
	// its specific findings is still unexplained. If every one is already
	// explained the rule is recorded as COVERED rather than dropped, because
	// suppression merges treatment and must never erase evidence: if the animal
	// is not improving, the covered diagnosis is the first thing re-opened.
	alreadyHit := map[string]bool{}
	for _, h := range specific {
		alreadyHit[h.rule.ID] = true
	}

	var residual []hit
	var covered []string
	for _, rule := range candidates {
		if alreadyHit[rule.ID] {
			continue
		}
		tier, cl, ok := firstMatch(rule, evidence, true)
		if !ok {
			continue
		}
		allExplained := true
		for _, tok := range cl.Findings {
			if r.nonSpecific[tok] {
				continue
			}
			if !explained[tok] {
				allExplained = false
				break
			}
		}
		if allExplained {
			covered = append(covered, rule.ID)
			continue
		}
		residual = append(residual, hit{
			rule:       rule,
			tier:       tier,
			severity:   severityFor(rule, evidence),
			residual:   true,
			matched:    cl.Findings,
			constraint: len(rule.GateRequired) + len(cl.Findings),
		})
	}

	residual = r.resolveResidualCollisions(residual)

	hits := append(append([]hit{}, specific...), residual...)

	// Specific-suppresses-specific. The suppressed rule moves to covered.
	present := map[string]bool{}
	for _, h := range hits {
		present[h.rule.ID] = true
	}
	suppressed := map[string]bool{}
	for _, h := range hits {
		for _, other := range h.rule.Suppresses {
			if present[other] && other != h.rule.ID {
				suppressed[other] = true
				covered = append(covered, other)
			}
		}
	}
	kept := hits[:0]
	for _, h := range hits {
		if !suppressed[h.rule.ID] {
			kept = append(kept, h)
		}
	}
	hits = kept

	// Rank severity first, confidence second. This is the axis separation: a
	// POSSIBLE bloat outranks a CONFIRMED pinkeye. sort.SliceStable keeps
	// register order as the final tiebreak so the output is deterministic.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].severity != hits[j].severity {
			return hits[i].severity > hits[j].severity
		}
		return hits[i].tier.rank() > hits[j].tier.rank()
	})

	return r.classify(hits, covered, evidence, explained)
}

// resolveResidualCollisions keeps the more constrained rule when two residual
// hits compete for the same unexplained finding. Constraint is gates plus clause
// length — the rule that had to satisfy more to fire is the better explanation.
func (r *Register) resolveResidualCollisions(residual []hit) []hit {
	if len(residual) < 2 {
		return residual
	}
	byFinding := map[string][]int{}
	for i, h := range residual {
		for _, tok := range h.matched {
			if !r.nonSpecific[tok] {
				byFinding[tok] = append(byFinding[tok], i)
			}
		}
	}
	losers := map[int]bool{}
	for _, group := range byFinding {
		if len(group) < 2 {
			continue
		}
		best := group[0]
		for _, idx := range group[1:] {
			if residual[idx].constraint > residual[best].constraint {
				best = idx
			}
		}
		for _, idx := range group {
			if residual[idx].rule.ID != residual[best].rule.ID {
				losers[idx] = true
			}
		}
	}
	out := make([]hit, 0, len(residual))
	for i, h := range residual {
		if !losers[i] {
			out = append(out, h)
		}
	}
	return out
}

// classify assigns each ranked hit its record type.
//
//	field + adjunct_when matching a live problem -> an adjunct on that problem's
//	                                               card, not a standalone task
//	field                                        -> a field action, done in place
//	problem at POSSIBLE                          -> a RECHECK, not a problem
//	problem at PROBABLE or CONFIRMED             -> a Problem
//
// Demoting POSSIBLE to a recheck is what stops a maybe from opening a course.
func (r *Register) classify(hits []hit, covered []string, evidence, explained map[string]bool) matchResult {
	liveProblems := map[string]bool{}
	for _, h := range hits {
		if h.rule.Kind == KindProblem && (h.tier == TierConfirmed || h.tier == TierProbable) {
			liveProblems[h.rule.ID] = true
		}
	}

	res := matchResult{tiers: map[string]Tier{}}
	for _, h := range hits {
		if h.rule.Kind == KindField {
			isAdjunct := false
			for _, when := range h.rule.AdjunctWhen {
				if liveProblems[when] {
					isAdjunct = true
					break
				}
			}
			if isAdjunct {
				res.adjunct = true
				continue
			}
			res.fieldActions = appendUnique(res.fieldActions, h.rule.ID)
			continue
		}
		if h.tier == TierPossible {
			recheckID := h.rule.RecheckIDPossible
			if recheckID == "" {
				recheckID = h.rule.ID + "_POSSIBLE"
			}
			res.rechecks = appendUnique(res.rechecks, recheckID)
			continue
		}
		res.problems = append(res.problems, h)
		res.tiers[h.rule.ID] = h.tier
	}

	res.covered = dedupe(covered)
	res.unexplained = unexplainedFrom(r, evidence, explained)
	return res
}

// unexplainedFrom is the decompression path for the lossy compression the
// disease layer performs: findings go in, a label comes out, information is
// destroyed. Anything specific and abnormal that no diagnosis accounts for
// surfaces here, and it belongs in the output as a HEADLINE rather than a
// footnote — "Mastitis (Confirmed)" invites a rubber stamp, while the same
// result beside a block of unaccounted-for findings keeps the reviewer thinking.
//
// Excluded: non-specific tokens (they never explain anything, so they can never
// be unexplained), the animal's own descriptors, and normal-state tokens that
// are recorded but are not findings.
func unexplainedFrom(r *Register, evidence, explained map[string]bool) []string {
	skipExact := map[string]bool{
		"leg:normal":   true,
		"has_milk":     true,
		"cmt:negative": true,
		"lactation:no": true,
		"TENT_GT4":     true,
	}
	var out []string
	for tok := range evidence {
		if r.nonSpecific[tok] || skipExact[tok] || explained[tok] {
			continue
		}
		if hasAnyPrefix(tok, "species:", "sex:", "status:") {
			continue
		}
		out = append(out, tok)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && s[:len(p)] == p {
			return true
		}
	}
	return false
}

func appendUnique(list []string, value string) []string {
	if contains(list, value) {
		return list
	}
	return append(list, value)
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(list))
	for _, v := range list {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
