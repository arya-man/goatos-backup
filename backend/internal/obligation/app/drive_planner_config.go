package app

import (
	"encoding/json"
	"strings"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// vaccineComboSession maps a vaccine code to its approved same-day combo session (deterministic).
// Each session bundles at most MaxVaccinesPerComboSession distinct vaccine products per visit.
var vaccineComboSession = map[string]string{
	"et+tt":       "combo:ET+TT+PPR",
	"ppr":         "combo:PPR+Blue Tongue",
	"blue tongue": "combo:PPR+Blue Tongue",
	"sheep pox":   "combo:Sheep Pox+Blue Tongue",
	"fmd":         "combo:FMD+HS",
	"hs":          "combo:FMD+HS",
}

// approvedDriveCombos lists PDF/source-approved same-day bundles for cross-version date alignment.
var approvedDriveCombos = map[string][]string{
	"FMD+HS":                {"FMD", "HS"},
	"PPR+Blue Tongue":       {"PPR", "Blue Tongue"},
	"ET+TT+PPR":             {"ET+TT", "PPR"},
	"Sheep Pox+Blue Tongue": {"Sheep Pox", "Blue Tongue"},
}

// VaccineMatrixPriority returns disease priority from the source matrix (lower = schedule first).
func VaccineMatrixPriority(vaccineCode string) int32 {
	switch strings.ToLower(strings.TrimSpace(vaccineCode)) {
	case "et+tt":
		return 1
	case "ppr":
		return 2
	case "goat pox", "sheep pox", "blue tongue":
		return 3
	case "fmd", "hs":
		return 4
	default:
		return 0
	}
}

// DrivePlannerFromRuleDSL extracts sweep planner settings from a published vaccination rule_dsl.
// Stock expiry is intentionally not used for drive date selection (reservation-time FEFO only).
func DrivePlannerFromRuleDSL(raw []byte) (vaccineCode string, planner domain.DrivePlannerSettings) {
	if len(raw) == 0 {
		return "", resolvedDrivePlannerSettings(domain.DrivePlannerSettings{}, "")
	}
	var dsl struct {
		Vaccine struct {
			Code string `json:"code"`
		} `json:"vaccine"`
		DrivePolicy struct {
			Enabled              *bool `json:"enabled"`
			MaxGoatsPerDrive     int32 `json:"max_goats_per_drive"`
			Priority             int32 `json:"priority"`
			ComboAlignWindowDays int32 `json:"combo_align_window_days"`
		} `json:"drive_policy"`
	}
	if err := json.Unmarshal(raw, &dsl); err != nil {
		return "", resolvedDrivePlannerSettings(domain.DrivePlannerSettings{}, "")
	}
	vaccineCode = strings.TrimSpace(dsl.Vaccine.Code)
	if dsl.DrivePolicy.Enabled != nil {
		planner.Enabled = *dsl.DrivePolicy.Enabled
	}
	if dsl.DrivePolicy.MaxGoatsPerDrive > 0 {
		planner.MaxGoatsPerDrive = dsl.DrivePolicy.MaxGoatsPerDrive
	}
	if dsl.DrivePolicy.Priority > 0 {
		planner.VaccinePriority = dsl.DrivePolicy.Priority
	}
	if dsl.DrivePolicy.ComboAlignWindowDays > 0 {
		planner.ComboAlignWindowDays = dsl.DrivePolicy.ComboAlignWindowDays
	}
	return vaccineCode, resolvedDrivePlannerSettings(planner, vaccineCode)
}

func resolvedDrivePlannerSettings(cfg domain.DrivePlannerSettings, vaccineCode string) domain.DrivePlannerSettings {
	out := cfg
	defaults := domain.DefaultDrivePlannerSettings()
	if !out.Enabled {
		out.Enabled = defaults.Enabled
	}
	// MaxGoatsPerDrive 0 = unlimited (maintainer default).
	if out.VaccinePriority <= 0 {
		if p := VaccineMatrixPriority(vaccineCode); p > 0 {
			out.VaccinePriority = p
		} else {
			out.VaccinePriority = defaults.VaccinePriority
		}
	}
	if out.ComboAlignWindowDays <= 0 {
		out.ComboAlignWindowDays = defaults.ComboAlignWindowDays
	}
	return out
}
