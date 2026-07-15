package app

import (
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
