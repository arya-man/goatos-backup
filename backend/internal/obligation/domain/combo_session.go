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

// ApprovedVaccineComboSessions returns every approved same-day combo membership for a vaccine.
// Some vaccines participate in more than one approved pair (PPR can pair with ET+TT or Blue
// Tongue), so callers must not compare only VaccineComboSession's primary session.
func ApprovedVaccineComboSessions(vaccineCode string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 2)
	add := func(session string) {
		session = strings.TrimSpace(session)
		if session == "" {
			return
		}
		if _, ok := seen[session]; ok {
			return
		}
		seen[session] = struct{}{}
		out = append(out, session)
	}
	if session := VaccineComboSession(vaccineCode); session != "" {
		add(session)
	}
	switch normalizeVaccineComboCode(vaccineCode) {
	case "et tt", "et+tt":
		add("combo:ET+TT+PPR")
	case "ppr":
		add("combo:ET+TT+PPR")
		add("combo:PPR+Blue Tongue")
		add("combo:PPR+FMD+HS")
	case "blue tongue":
		add("combo:PPR+Blue Tongue")
		add("combo:Sheep Pox+Blue Tongue")
	case "fmd":
		add("combo:PPR+FMD+HS")
	case "hs":
		add("combo:PPR+FMD+HS")
	case "sheep pox":
		add("combo:Sheep Pox+Blue Tongue")
	}
	return out
}

// VaccinesShareApprovedCombo reports whether two vaccine codes are configured as an approved
// same-day pair.
func VaccinesShareApprovedCombo(a, b string) bool {
	left := ApprovedVaccineComboSessions(a)
	right := ApprovedVaccineComboSessions(b)
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, session := range left {
		seen[session] = struct{}{}
	}
	for _, session := range right {
		if _, ok := seen[session]; ok {
			return true
		}
	}
	return false
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

func normalizeVaccineComboCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.NewReplacer("_", " ", "-", " ").Replace(code)
	return strings.Join(strings.Fields(code), " ")
}
