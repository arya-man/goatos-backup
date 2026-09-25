// Package vaccinepurpose is the single resolver for the authored
// procurement_policy.purpose_plans vaccine contract. Generation, persistence,
// reconciliation, rescheduling and publish carry-over all call Resolve so the
// animal-purpose applicability of a vaccine can never differ between the code
// that proposes a date and the code that stores it.
package vaccinepurpose

import (
	"encoding/json"
	"strings"
)

// DefaultSecondWaveAfterDays is used only when an authored plan omits the delay.
const DefaultSecondWaveAfterDays int32 = 28

// StringList accepts a JSON string or a JSON string array.
type StringList []string

func (l *StringList) UnmarshalJSON(raw []byte) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		*l = nil
		return nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		*l = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return err
	}
	*l = many
	return nil
}

// Plan is one authored purpose plan.
type Plan struct {
	FirstWave           StringList `json:"first_wave"`
	SecondWaveAfterDays *int32     `json:"second_wave_after_days"`
	GoatSecondWave      StringList `json:"goat_second_wave"`
	SheepSecondWave     StringList `json:"sheep_second_wave"`
}

func (p Plan) empty() bool {
	return len(p.FirstWave) == 0 && len(p.GoatSecondWave) == 0 && len(p.SheepSecondWave) == 0
}

// SecondWave returns the species-specific second wave. Blank means goats.species' default
// ('goat'); any other species -- including one the farm added on Configuration (OPEN UP TO NEW
// SPECIES, 2026-09-25) -- has no second wave until one is authored for it.
func (p Plan) SecondWave(species string) []string {
	switch strings.ToLower(strings.TrimSpace(species)) {
	case "sheep":
		return p.SheepSecondWave
	case "goat", "":
		return p.GoatSecondWave
	default:
		return nil
	}
}

// Delay returns the configured first-wave to second-wave delay in days.
func (p Plan) Delay() int32 {
	if p.SecondWaveAfterDays != nil {
		return *p.SecondWaveAfterDays
	}
	return DefaultSecondWaveAfterDays
}

// Policy is the procurement_policy object of a vaccination rule DSL.
type Policy struct {
	FirstWave           StringList      `json:"first_wave"`
	SecondWaveAfterDays *int32          `json:"second_wave_after_days"`
	GoatSecondWave      StringList      `json:"goat_second_wave"`
	SheepSecondWave     StringList      `json:"sheep_second_wave"`
	PurposePlans        map[string]Plan `json:"purpose_plans"`
}

// DecodePolicy decodes a procurement_policy JSON object; empty/null is an empty policy.
func DecodePolicy(raw []byte) (Policy, error) {
	var p Policy
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return p, nil
	}
	err := json.Unmarshal(raw, &p)
	return p, err
}

func (p Policy) topLevel() Plan {
	return Plan{FirstWave: p.FirstWave, SecondWaveAfterDays: p.SecondWaveAfterDays, GoatSecondWave: p.GoatSecondWave, SheepSecondWave: p.SheepSecondWave}
}

// Decision is the resolved applicability of one vaccine for one animal.
type Decision struct {
	// Applicable is false when the vaccine must not be scheduled or persisted for the animal.
	Applicable bool
	// Reason explains a non-applicable decision.
	Reason string
	// Plan is the governing purpose plan. PlanGoverned is false when no purpose plan restricts
	// the vaccine (breeding/unspecified animals without an authored plan keep the full schedule).
	Plan         Plan
	PlanGoverned bool
	// SecondWave is true when the vaccine is in the governing plan's species second wave; it then
	// requires every FirstWave vaccine and the configured Delay after the latest first-wave dose.
	SecondWave bool
}

// Normalize canonicalizes vaccine names ("ET+TT", "et_tt", "Goat Pox" -> "ettt", "goatpox").
func Normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer(" ", "", "_", "", "+", "", "-", "").Replace(value)
}

// Contains reports whether values holds vaccine after normalization.
func Contains(values []string, vaccine string) bool {
	needle := Normalize(vaccine)
	if needle == "" {
		return false
	}
	for _, v := range values {
		if Normalize(v) == needle {
			return true
		}
	}
	return false
}

// NormalizePurpose canonicalizes a procurement purpose; blank means unspecified.
func NormalizePurpose(purpose string) string {
	p := strings.ToLower(strings.TrimSpace(purpose))
	if p == "" {
		return "unspecified"
	}
	return p
}

// Resolve applies the canonical purpose contract:
//
//   - non_breeding: never vaccinated by the procurement schedule (fail closed).
//   - fattening: the authored purpose_plans.fattening, or, for immutable legacy versions without
//     purpose_plans, the authored top-level waves. No authored plan at all fails closed.
//   - breeding / unspecified: an authored plan for that purpose governs it; without one the animal
//     keeps the full (breeding) schedule, which this contract deliberately does not change.
//   - any other purpose value is unconfigured and fails closed.
//
// A governed vaccine must be in FirstWave or the species SecondWave.
func Resolve(policy Policy, purpose, species, vaccine string) Decision {
	purpose = NormalizePurpose(purpose)
	var plan Plan
	switch purpose {
	case "non_breeding":
		return Decision{Reason: "purpose non_breeding is excluded from vaccination schedules"}
	case "fattening":
		if authored, ok := policy.PurposePlans[purpose]; ok {
			plan = authored
		} else if len(policy.PurposePlans) == 0 && !policy.topLevel().empty() {
			plan = policy.topLevel()
		} else {
			return Decision{Reason: "fattening purpose plan is not configured"}
		}
	case "breeding", "unspecified":
		authored, ok := policy.PurposePlans[purpose]
		if !ok {
			return Decision{Applicable: true}
		}
		plan = authored
	default:
		return Decision{Reason: "purpose " + purpose + " has no configured purpose plan"}
	}
	if plan.empty() {
		return Decision{Reason: "purpose " + purpose + " plan is empty"}
	}
	d := Decision{Plan: plan, PlanGoverned: true}
	if Contains(plan.FirstWave, vaccine) {
		d.Applicable = true
		return d
	}
	if Contains(plan.SecondWave(species), vaccine) {
		d.Applicable = true
		d.SecondWave = true
		return d
	}
	d.Reason = "vaccine " + vaccine + " is not in the " + purpose + " plan for " + strings.ToLower(strings.TrimSpace(species))
	return d
}
