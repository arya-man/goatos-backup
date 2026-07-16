package app

import (
	"sort"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// dueGroup is one due-window batching group (same scope/rule/window/species-grouping-policy
// bucket), the unit sweepVersion's main loop plans one drive date + shot-cap selection for. It is
// a named, package-level type (rather than the anonymous struct literal previously defined inline
// in sweepVersion) precisely so PreflightVisitShotCapTies (VAX-REV-04) can group rows with the
// IDENTICAL logic sweepVersion itself uses -- one shared function, not two copies that could drift
// apart and let the write-free preflight decide differently than the real sweep that follows it.
type dueGroup struct {
	scopeType   string
	scopeID     string
	ruleID      string
	windowStart *time.Time
	windowEnd   *time.Time
	rows        []domain.UnbatchedDue
	ids         []string
}

// groupUnbatchedDue partitions rows into dueGroups the same way sweepVersion's main loop always
// has, returning the group keys in deterministic first-seen order alongside the groups themselves.
func groupUnbatchedDue(rows []domain.UnbatchedDue, speciesGroupingPolicy string) ([]string, map[string]*dueGroup) {
	order := make([]string, 0)
	groups := make(map[string]*dueGroup)
	for _, r := range rows {
		k := sweepWindowGroupKey(r, speciesGroupingPolicy)
		g := groups[k]
		if g == nil {
			g = &dueGroup{scopeType: r.ScopeType, scopeID: r.ScopeID, ruleID: r.RuleID, windowStart: r.WindowStart, windowEnd: r.WindowEnd}
			groups[k] = g
			order = append(order, k)
		}
		g.rows = append(g.rows, r)
		g.ids = append(g.ids, r.ObligationID)
	}
	return order, groups
}

func orderDueGroupsByVaccinePriority(order []string, groups map[string]*dueGroup, cfg SweepConfig) []string {
	out := append([]string(nil), order...)
	sort.SliceStable(out, func(i, j int) bool {
		left := groups[out[i]]
		right := groups[out[j]]
		if left == nil || right == nil {
			return out[i] < out[j]
		}
		leftID := cfg.getRuleVaccineIdentity(left.ruleID)
		rightID := cfg.getRuleVaccineIdentity(right.ruleID)
		if leftID.VaccinePriority != rightID.VaccinePriority {
			return leftID.VaccinePriority < rightID.VaccinePriority
		}
		if leftID.VaccineCode != rightID.VaccineCode {
			return leftID.VaccineCode < rightID.VaccineCode
		}
		return out[i] < out[j]
	})
	return out
}

func orderParkCandidatesByVaccinePriority(rows []domain.ParkConsolidationCandidate, identityFor ruleVaccineIdentityResolver) []domain.ParkConsolidationCandidate {
	out := append([]domain.ParkConsolidationCandidate(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		leftID := identityFor(out[i].RuleID)
		rightID := identityFor(out[j].RuleID)
		if leftID.VaccinePriority != rightID.VaccinePriority {
			return leftID.VaccinePriority < rightID.VaccinePriority
		}
		if leftID.VaccineCode != rightID.VaccineCode {
			return leftID.VaccineCode < rightID.VaccineCode
		}
		if !out[i].DueAt.Equal(out[j].DueAt) {
			return out[i].DueAt.Before(out[j].DueAt)
		}
		return out[i].ObligationID < out[j].ObligationID
	})
	return out
}
