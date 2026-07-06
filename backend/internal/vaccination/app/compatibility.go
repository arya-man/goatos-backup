package app

import (
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

type genVaccineMeta struct {
	Code               string `json:"code"`
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
	Type               string
	PathogenClass      string
	CompatibilityGroup string
	Class              vaccineImmunoClass
}

func vaccineProfileFromDSL(dsl genDSL) vaccineProfile {
	return vaccineProfile{
		Code:               strings.TrimSpace(dsl.Vaccine.Code),
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

func (p genCompatibilityPolicy) withDefaults() genCompatibilityPolicy {
	out := p
	if out.LiveToLiveGapDays <= 0 {
		out.LiveToLiveGapDays = 28
	}
	if out.LiveToKilledGapDays <= 0 {
		out.LiveToKilledGapDays = 14
	}
	if out.KilledToKilledGapDays <= 0 {
		out.KilledToKilledGapDays = 14
	}
	if out.KidBoosterMinGapDays <= 0 {
		out.KidBoosterMinGapDays = 21
	}
	if out.MaxVaccinesPerComboSession <= 0 {
		out.MaxVaccinesPerComboSession = 2
	}
	if out.MaxVaccinesPerComboSession > 2 {
		out.MaxVaccinesPerComboSession = 2
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
// another vaccine product. Same-day allowed pairs (bacterial+viral, live viral+killed viral) return 0.
func crossVaccineGapDays(prior, next vaccineImmunoClass, policy genCompatibilityPolicy) int32 {
	policy = policy.withDefaults()
	if prior == immunoUnknown || next == immunoUnknown {
		return 0
	}
	if prior == immunoKilledBacterial && (next == immunoLiveViral || next == immunoKilledViral) {
		return 0
	}
	if next == immunoKilledBacterial && (prior == immunoLiveViral || prior == immunoKilledViral) {
		return 0
	}
	if (prior == immunoLiveViral && next == immunoKilledViral) || (prior == immunoKilledViral && next == immunoLiveViral) {
		return 0
	}
	if isLiveClass(prior) && isLiveClass(next) {
		return policy.LiveToLiveGapDays
	}
	if isLiveClass(prior) && isKilledClass(next) {
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
