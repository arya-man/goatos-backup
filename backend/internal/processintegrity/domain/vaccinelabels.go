package domain

import (
	"regexp"
	"strings"

	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

var doseWaveSuffix = regexp.MustCompile(`_w([0-9]+)$`)

// ControlTowerDoseLabel builds the Control Tower / process-integrity display
// label for a vaccination obligation from its raw dose code. It composes the
// canonical antigen label (vaccination/domain.DoseDisplayLabel — the single
// backend-owned label source) with the Control Tower course/dose qualifiers.
//
// This replaces the former SQL ceo_ai.vaccine_label_for() join in the Control
// Tower query: the process-integrity read path is a core operator surface and
// must not depend on the leadership-assistant reporting schema (ceo_ai). See
// docs/decisions/ceo-ai-reporting-boundary.md.
func ControlTowerDoseLabel(protocolName, doseCode string) string {
	doseCode = strings.TrimSpace(doseCode)
	label := vaccinationdomain.DoseDisplayLabel(protocolName, doseCode)
	if doseCode == "" {
		return label
	}

	lc := strings.ToLower(doseCode)
	if strings.Contains(lc, "revac") || strings.Contains(lc, "booster") {
		label += " · Booster"
	}
	switch {
	case strings.Contains(lc, "_adult_"):
		label += " adult course"
	case strings.Contains(lc, "_kid_"):
		label += " kid course"
	}
	if m := doseWaveSuffix.FindStringSubmatch(lc); m != nil {
		label += " dose " + m[1]
	}
	return strings.TrimSpace(label)
}
