package domain

import "strings"

// VaccineComboSession maps a vaccine code to its approved same-day combo session.
// The deterministic session key is shared by sweep batching and recovery/missed-dose alignment.
func VaccineComboSession(vaccineCode string) string {
	code := strings.ToLower(strings.TrimSpace(vaccineCode))
	code = strings.NewReplacer("_", " ", "-", " ").Replace(code)
	code = strings.Join(strings.Fields(code), " ")
	if code == "" {
		return ""
	}
	return vaccineComboSessions[code]
}

// BatchSession returns the deterministic planned-batch session for a rule/vaccine pair.
func BatchSession(ruleID, vaccineCode string) string {
	if session := VaccineComboSession(vaccineCode); session != "" {
		return session
	}
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return ""
	}
	return "rule:" + ruleID
}

// CompatiblePlannedBatchSessions returns all sessions a recovered/missed animal may rejoin.
// The rule-specific session preserves legacy/non-combo batches; the combo session catches
// approved same-park drive sessions such as FMD+HS and PPR+Blue Tongue.
func CompatiblePlannedBatchSessions(ruleID, vaccineCode string) []string {
	sessions := make([]string, 0, 2)
	if ruleID = strings.TrimSpace(ruleID); ruleID != "" {
		sessions = append(sessions, "rule:"+ruleID)
	}
	if combo := VaccineComboSession(vaccineCode); combo != "" {
		for _, session := range sessions {
			if session == combo {
				return sessions
			}
		}
		sessions = append(sessions, combo)
	}
	return sessions
}

var vaccineComboSessions = map[string]string{
	"et+tt":       "combo:ET+TT+PPR",
	"et tt":       "combo:ET+TT+PPR",
	"ppr":         "combo:PPR+Blue Tongue",
	"blue tongue": "combo:PPR+Blue Tongue",
	"sheep pox":   "combo:Sheep Pox+Blue Tongue",
	"fmd":         "combo:FMD+HS",
	"hs":          "combo:FMD+HS",
}
