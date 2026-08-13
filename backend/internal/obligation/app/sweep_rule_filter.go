package app

import "github.com/vgoats/goatos/backend/internal/obligation/domain"

func sweepRuleAllowed(cfg SweepConfig, ruleID string) bool {
	if len(cfg.AllowedRuleIDs) == 0 {
		return true
	}
	_, ok := cfg.AllowedRuleIDs[ruleID]
	return ok
}

func filterAllowedUnbatchedDueRows(cfg SweepConfig, rows []domain.UnbatchedDue) []domain.UnbatchedDue {
	if (len(cfg.AllowedRuleIDs) == 0 && len(cfg.AllowedTargetIDs) == 0) || len(rows) == 0 {
		return rows
	}
	out := rows[:0]
	for _, row := range rows {
		if sweepRuleAllowed(cfg, row.RuleID) && sweepTargetAllowed(cfg, row.TargetID) {
			out = append(out, row)
		}
	}
	return out
}

func filterAllowedParkConsolidationRows(cfg SweepConfig, rows []domain.ParkConsolidationCandidate) []domain.ParkConsolidationCandidate {
	if (len(cfg.AllowedRuleIDs) == 0 && len(cfg.AllowedTargetIDs) == 0) || len(rows) == 0 {
		return rows
	}
	out := rows[:0]
	for _, row := range rows {
		if sweepRuleAllowed(cfg, row.RuleID) && sweepTargetAllowed(cfg, row.TargetID) {
			out = append(out, row)
		}
	}
	return out
}

func sweepTargetAllowed(cfg SweepConfig, targetID string) bool {
	if len(cfg.AllowedTargetIDs) == 0 {
		return true
	}
	_, ok := cfg.AllowedTargetIDs[targetID]
	return ok
}
