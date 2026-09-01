package app

import (
	"encoding/json"
	"strings"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// approvedDriveCombos lists PDF/source-approved same-day bundles for cross-version date alignment.
var approvedDriveCombos = map[string][]string{
	"FMD+HS":                {"FMD", "HS"},
	"PPR+FMD+HS":            {"PPR", "FMD", "HS"},
	"PPR+Blue Tongue":       {"PPR", "Blue Tongue"},
	"ET+TT+PPR":             {"ET+TT", "PPR"},
	"Sheep Pox+Blue Tongue": {"Sheep Pox", "Blue Tongue"},
}

// VaccineMatrixPriority returns disease priority from the source matrix (lower = schedule first).
func VaccineMatrixPriority(vaccineCode string) int32 {
	switch normalizedVaccineMatrixCode(vaccineCode) {
	case "et+tt", "et tt":
		return 1
	case "ppr":
		return 2
	case "goat pox", "sheep pox":
		return 3
	case "blue tongue":
		return 4
	case "fmd", "hs":
		return 5
	default:
		return 0
	}
}

func normalizedVaccineMatrixCode(vaccineCode string) string {
	code := strings.ToLower(strings.TrimSpace(vaccineCode))
	code = strings.NewReplacer("_", " ", "-", " ").Replace(code)
	return strings.Join(strings.Fields(code), " ")
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
		Capacity struct {
			MaxPerDay int32 `json:"max_per_day"`
		} `json:"capacity"`
		DrivePolicy struct {
			Enabled                   *bool  `json:"enabled"`
			MaxGoatsPerDrive          int32  `json:"max_goats_per_drive"`
			Priority                  int32  `json:"priority"`
			ComboAlignWindowDays      int32  `json:"combo_align_window_days"`
			MaxBatchingHoldDays       int32  `json:"max_batching_hold_days"`
			MaxBatchingHoldCount      int32  `json:"max_batching_hold_count"`
			SpeciesGroupingPolicy     string `json:"species_grouping_policy"`
			MaxShotsPerAnimalPerDrive int32  `json:"max_shots_per_animal_per_drive"`
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
	if dsl.Capacity.MaxPerDay > 0 && (planner.MaxGoatsPerDrive <= 0 || dsl.Capacity.MaxPerDay < planner.MaxGoatsPerDrive) {
		planner.MaxGoatsPerDrive = dsl.Capacity.MaxPerDay
	}
	if dsl.DrivePolicy.Priority > 0 {
		planner.VaccinePriority = dsl.DrivePolicy.Priority
	}
	if dsl.DrivePolicy.ComboAlignWindowDays > 0 {
		planner.ComboAlignWindowDays = dsl.DrivePolicy.ComboAlignWindowDays
	}
	if dsl.DrivePolicy.MaxBatchingHoldDays > 0 {
		planner.MaxBatchingHoldDays = dsl.DrivePolicy.MaxBatchingHoldDays
	}
	if dsl.DrivePolicy.MaxBatchingHoldCount > 0 {
		planner.MaxBatchingHoldCount = dsl.DrivePolicy.MaxBatchingHoldCount
	}
	if strings.TrimSpace(dsl.DrivePolicy.SpeciesGroupingPolicy) != "" {
		planner.SpeciesGroupingPolicy = strings.TrimSpace(dsl.DrivePolicy.SpeciesGroupingPolicy)
	}
	if dsl.DrivePolicy.MaxShotsPerAnimalPerDrive > 0 {
		planner.MaxShotsPerAnimalPerDrive = dsl.DrivePolicy.MaxShotsPerAnimalPerDrive
	}
	return vaccineCode, resolvedDrivePlannerSettings(planner, vaccineCode)
}

// ApplyCapacityShotCapOverride applies the tenant's admin-editable per-animal shot-cap override
// (vaccination_capacity_config.max_shots_per_animal_per_drive, migration 000045) over a
// DSL/default-resolved planner. capacityMaxShots is nil when no override is authored (the common
// case): the DSL/default value is left untouched. A non-nil, >=1 value wins over the DSL value; a
// non-positive value is ignored (defence in depth -- the write path already rejects <1, see
// domain.CapacityConfig.Validate in the vaccinationexecution package).
func ApplyCapacityShotCapOverride(planner domain.DrivePlannerSettings, capacityMaxShots *int32) domain.DrivePlannerSettings {
	if capacityMaxShots == nil || *capacityMaxShots <= 0 {
		return planner
	}
	out := planner
	out.MaxShotsPerAnimalPerDrive = *capacityMaxShots
	return out
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
	if out.MaxBatchingHoldDays <= 0 {
		out.MaxBatchingHoldDays = defaults.MaxBatchingHoldDays
	}
	if out.MaxBatchingHoldCount <= 0 {
		out.MaxBatchingHoldCount = defaults.MaxBatchingHoldCount
	}
	if strings.TrimSpace(out.SpeciesGroupingPolicy) == "" {
		out.SpeciesGroupingPolicy = defaults.SpeciesGroupingPolicy
	}
	if out.MaxShotsPerAnimalPerDrive <= 0 {
		out.MaxShotsPerAnimalPerDrive = defaults.MaxShotsPerAnimalPerDrive
	}
	return out
}

// ComboAlignmentSettingsForPlans returns the strictest combo-drive alignment policy
// across the protocol versions participating in one sweep pass.
func ComboAlignmentSettingsForPlans(plans []SweepVersionPriority) (alignWindowDays int32, maxShotsPerAnimalPerDrive int32, maxDriveCells int32) {
	defaults := domain.DefaultDrivePlannerSettings()
	alignWindowDays = defaults.ComboAlignWindowDays
	maxShotsPerAnimalPerDrive = defaults.MaxShotsPerAnimalPerDrive
	maxDriveCells = defaults.MaxGoatsPerDrive
	for _, plan := range plans {
		planner := resolvedDrivePlannerSettings(plan.Config.DrivePlanner, plan.Config.VaccineCode)
		if planner.ComboAlignWindowDays > 0 && planner.ComboAlignWindowDays < alignWindowDays {
			alignWindowDays = planner.ComboAlignWindowDays
		}
		if planner.MaxShotsPerAnimalPerDrive > 0 && planner.MaxShotsPerAnimalPerDrive < maxShotsPerAnimalPerDrive {
			maxShotsPerAnimalPerDrive = planner.MaxShotsPerAnimalPerDrive
		}
		if planner.MaxGoatsPerDrive > 0 && (maxDriveCells <= 0 || planner.MaxGoatsPerDrive < maxDriveCells) {
			maxDriveCells = planner.MaxGoatsPerDrive
		}
	}
	return alignWindowDays, maxShotsPerAnimalPerDrive, maxDriveCells
}
