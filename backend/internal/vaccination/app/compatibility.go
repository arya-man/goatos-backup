package app

import (
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

type genVaccineMeta struct {
	Code               string `json:"code"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	PathogenClass      string `json:"pathogen_class"`
	CompatibilityGroup string `json:"compatibility_group"`
}

type vaccineImmunoClass int

const (
	immunoUnknown vaccineImmunoClass = iota
	immunoLiveViral
	immunoKilledViral
	immunoKilledBacterial
	immunoLive
	immunoKilled
)

type vaccineProfile struct {
	Code               string
	Name               string
	Type               string
	PathogenClass      string
	CompatibilityGroup string
	Class              vaccineImmunoClass
}

func vaccineProfileFromDSL(dsl genDSL) vaccineProfile {
	return vaccineProfile{
		Code:               strings.TrimSpace(dsl.Vaccine.Code),
		Name:               strings.TrimSpace(dsl.Vaccine.Name),
		Type:               strings.TrimSpace(dsl.Vaccine.Type),
		PathogenClass:      strings.TrimSpace(dsl.Vaccine.PathogenClass),
		CompatibilityGroup: strings.TrimSpace(dsl.Vaccine.CompatibilityGroup),
		Class:              classifyVaccine(dsl.Vaccine.Type, dsl.Vaccine.PathogenClass),
	}
}

func vaccineProfileFromAdministration(admin domain.RecentVaccineAdministration) vaccineProfile {
	return vaccineProfile{
		Code:          strings.TrimSpace(admin.VaccineCode),
		Type:          strings.TrimSpace(admin.VaccineType),
		PathogenClass: strings.TrimSpace(admin.PathogenClass),
		Class:         classifyVaccine(admin.VaccineType, admin.PathogenClass),
	}
}

func classifyVaccine(vaccineType, pathogenClass string) vaccineImmunoClass {
	t := strings.ToLower(strings.TrimSpace(vaccineType))
	p := strings.ToLower(strings.TrimSpace(pathogenClass))
	switch t {
	case "toxoid":
		return immunoKilledBacterial
	case "live":
		if p == "viral" {
			return immunoLiveViral
		}
		return immunoLive
	case "killed":
		if p == "viral" {
			return immunoKilledViral
		}
		if p == "bacterial" {
			return immunoKilledBacterial
		}
		return immunoKilled
	default:
		return immunoUnknown
	}
}

// Default cross-vaccine medical safety gaps, in days. These are the single source of
// truth for both enforcement (withDefaults, below, applied when rule_dsl omits a value)
// and the read-only "Automatic safety rules" copy shown in the vaccination Config UI
// (adminui pageSpecificCopy("config"), keys modal.rule_editor.guided.safety_*). The
// guard test TestSafetyRuleCopyMatchesEnforcedDefaults locks that UI copy to these, so
// the displayed number can never silently drift from the enforced number.
const (
	DefaultLiveToLiveGapDays          int32 = 28
	DefaultLiveToKilledGapDays        int32 = 14
	DefaultKilledToKilledGapDays      int32 = 14
	DefaultCourseBoosterMinGapDays    int32 = 21
	DefaultMaxVaccinesPerComboSession int32 = 3
)

func (p genCompatibilityPolicy) withDefaults() genCompatibilityPolicy {
	out := p
	if out.LiveToLiveGapDays <= 0 {
		out.LiveToLiveGapDays = DefaultLiveToLiveGapDays
	}
	if out.LiveToKilledGapDays <= 0 {
		out.LiveToKilledGapDays = DefaultLiveToKilledGapDays
	}
	if out.KilledToKilledGapDays <= 0 {
		out.KilledToKilledGapDays = DefaultKilledToKilledGapDays
	}
	if out.CourseBoosterMinGapDays <= 0 {
		out.CourseBoosterMinGapDays = DefaultCourseBoosterMinGapDays
	}
	if out.MaxVaccinesPerComboSession <= 0 {
		out.MaxVaccinesPerComboSession = DefaultMaxVaccinesPerComboSession
	}
	if out.MaxVaccinesPerComboSession != DefaultMaxVaccinesPerComboSession {
		out.MaxVaccinesPerComboSession = DefaultMaxVaccinesPerComboSession
	}
	return out
}

func isLiveClass(c vaccineImmunoClass) bool {
	return c == immunoLiveViral || c == immunoLive
}

func isKilledClass(c vaccineImmunoClass) bool {
	return c == immunoKilledViral || c == immunoKilledBacterial || c == immunoKilled
}

// crossVaccineGapDays returns the minimum whole-day wait after a prior vaccine before scheduling
// another vaccine product. Same-day allowed pairs (bacterial+viral, live viral+killed viral) return 0
// only when their corresponding policy switch is true. Unknown vaccine classes fail-closed by returning
// the strictest configured gap (LiveToLiveGapDays); unknown classifications should have been rejected
// upstream during configuration/publishing (no vaccine with unknown type/pathogen_class should be scheduled).
func crossVaccineGapDays(prior, next vaccineImmunoClass, policy genCompatibilityPolicy) int32 {
	policy = policy.withDefaults()
	// BUG #4: unknown immunoclass fails-closed (returns strictest gap, not 0).
	// Unknown is not a valid classification and must never be persisted or scheduled — it represents
	// a data integrity error at the protocol/seed layer. We do not silently allow same-day but instead
	// return the strictest gap to fail-closed. If this path executes, a defect exists upstream.
	if prior == immunoUnknown || next == immunoUnknown {
		return policy.LiveToLiveGapDays // strictest default gap
	}
	// BUG #3: respect the policy switches for same-day allowed pairs.
	if prior == immunoKilledBacterial && (next == immunoLiveViral || next == immunoKilledViral) {
		if policy.BacterialViralSameDayAllowed {
			return 0
		}
		// bacterial + viral not allowed same-day: fall through to class-based gap.
	}
	if next == immunoKilledBacterial && (prior == immunoLiveViral || prior == immunoKilledViral) {
		if policy.BacterialViralSameDayAllowed {
			return 0
		}
		// viral + bacterial not allowed same-day: fall through to class-based gap.
	}
	if (prior == immunoLiveViral && next == immunoKilledViral) || (prior == immunoKilledViral && next == immunoLiveViral) {
		if policy.LiveKilledViralSameDayAllowed {
			return 0
		}
		// live viral + killed viral not allowed same-day: fall through to class-based gap.
	}
	if isLiveClass(prior) && isLiveClass(next) {
		return policy.LiveToLiveGapDays
	}
	if isLiveClass(prior) && isKilledClass(next) {
		return policy.LiveToKilledGapDays
	}
	if isKilledClass(prior) && isLiveClass(next) {
		// Symmetric with live→killed: killed→live also requires the gap
		return policy.LiveToKilledGapDays
	}
	if isKilledClass(prior) && isKilledClass(next) {
		return policy.KilledToKilledGapDays
	}
	return 0
}

func applyCrossVaccineGapFloor(due time.Time, last *domain.RecentVaccineAdministration, next vaccineProfile, policy genCompatibilityPolicy) time.Time {
	if last == nil || last.AdministeredAt.IsZero() {
		return due
	}
	prior := vaccineProfileFromAdministration(*last)
	if prior.Code != "" && next.Code != "" && strings.EqualFold(prior.Code, next.Code) {
		return due
	}
	gap := crossVaccineGapDays(prior.Class, next.Class, policy)
	if gap <= 0 {
		return due
	}
	floor := businessDayStart(last.AdministeredAt).AddDate(0, 0, int(gap))
	if due.Before(floor) {
		return floor
	}
	return due
}

func applyCrossVaccineGapFloorFromHistory(due time.Time, history []domain.RecentVaccineAdministration, next vaccineProfile, policy genCompatibilityPolicy) time.Time {
	out := due
	for _, admin := range history {
		if admin.AdministeredAt.IsZero() {
			continue
		}
		prior := vaccineProfileFromAdministration(admin)
		if prior.Code != "" && next.Code != "" && strings.EqualFold(prior.Code, next.Code) {
			continue
		}
		gap := crossVaccineGapDays(prior.Class, next.Class, policy)
		if gap <= 0 {
			continue
		}
		floor := businessDayStart(admin.AdministeredAt).AddDate(0, 0, int(gap))
		if out.Before(floor) {
			out = floor
		}
	}
	return out
}
