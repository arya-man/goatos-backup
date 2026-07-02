package app

import (
	"strings"
	"time"

	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

const (
	schedulePathKid              = "kid"
	schedulePathAdultProcurement = "adult_procurement"
)

type genCompatibilityPolicy struct {
	LiveToKilledGapDays          int32 `json:"live_to_killed_gap_days"`
	KilledToKilledGapDays        int32 `json:"killed_to_killed_gap_days"`
	LiveToLiveGapDays            int32 `json:"live_to_live_gap_days"`
	KidBoosterMinGapDays         int32 `json:"kid_booster_min_gap_days"`
	BacterialViralSameDayAllowed   bool  `json:"bacterial_viral_same_day_allowed"`
	LiveKilledViralSameDayAllowed  bool  `json:"live_killed_viral_same_day_allowed"`
}

type genProcurementPolicy struct {
	WarmupNoVaccinationDays       int32 `json:"warmup_no_vaccination_days"`
	KidsNormalScheduleUntilWeeks  int32 `json:"kids_normal_schedule_until_weeks"`
	AdultSourceVaccinationAllowed bool  `json:"adult_source_vaccination_allowed"`
}

func (p genProcurementPolicy) active() bool {
	return p.WarmupNoVaccinationDays > 0 || p.KidsNormalScheduleUntilWeeks > 0 || p.AdultSourceVaccinationAllowed
}

type genPregnancyPolicy struct {
	AllowUntilPregnancyMonth  int32 `json:"allow_until_pregnancy_month"`
	SkipFromPregnancyMonth    int32 `json:"skip_from_pregnancy_month"`
	SkipThroughPregnancyMonth int32 `json:"skip_through_pregnancy_month"`
	PostDeliveryCatchUpDays   int32 `json:"post_delivery_catch_up_days"`
}

func (p genPregnancyPolicy) active() bool {
	return p.AllowUntilPregnancyMonth > 0 ||
		p.SkipFromPregnancyMonth > 0 ||
		p.SkipThroughPregnancyMonth > 0 ||
		p.PostDeliveryCatchUpDays > 0
}

type genVersionPolicies struct {
	Procurement   genProcurementPolicy
	Pregnancy     genPregnancyPolicy
	Compatibility genCompatibilityPolicy
}

func schedulePathForGoat(g domain.EligibleGoat, proc genProcurementPolicy, asOf time.Time) string {
	origin := strings.ToLower(strings.TrimSpace(g.OriginType))
	if origin == "birth" {
		return schedulePathKid
	}
	kidWeeks := int32(16)
	if proc.KidsNormalScheduleUntilWeeks > 0 {
		kidWeeks = proc.KidsNormalScheduleUntilWeeks
	}
	if g.DOB != nil {
		if wholeDaysBetween(*g.DOB, asOf)/7 <= int(kidWeeks) {
			return schedulePathKid
		}
	}
	if isKidManagementStage(g.Stage) {
		return schedulePathKid
	}
	if warmingEntryAt(g) != nil {
		return schedulePathAdultProcurement
	}
	if origin == "procured" || origin == "imported" {
		return schedulePathAdultProcurement
	}
	return schedulePathKid
}

func isKidManagementStage(stage string) bool {
	stage = strings.ToUpper(strings.TrimSpace(stage))
	if len(stage) >= 2 && strings.HasPrefix(stage, "K") {
		return true
	}
	return false
}

func ruleMatchesSchedulePath(rule protodomain.Rule, path string) bool {
	switch rule.TriggerType {
	case "manual_campaign", "calendar":
		return true
	case "birth_age":
		return path == schedulePathKid
	case "post_arrival":
		return path == schedulePathAdultProcurement
	default:
		return false
	}
}

func warmingEntryAt(g domain.EligibleGoat) *time.Time {
	if g.WarmingEntryAt != nil {
		return g.WarmingEntryAt
	}
	if g.EntryDate != nil {
		start := time.Date(g.EntryDate.Year(), g.EntryDate.Month(), g.EntryDate.Day(), 0, 0, 0, 0, time.UTC)
		return &start
	}
	return nil
}

func policyDeferReason(g domain.EligibleGoat, policies genVersionPolicies, asOf time.Time) string {
	if reason := warmingDeferReason(g, policies.Procurement, asOf); reason != "" {
		return reason
	}
	if reason := pregnancyDeferReason(g, policies.Pregnancy, asOf); reason != "" {
		return reason
	}
	return ""
}

func warmingDeferReason(g domain.EligibleGoat, proc genProcurementPolicy, asOf time.Time) string {
	if proc.WarmupNoVaccinationDays <= 0 {
		return ""
	}
	anchor := warmingEntryAt(g)
	if anchor == nil {
		return ""
	}
	holdEnd := anchor.AddDate(0, 0, int(proc.WarmupNoVaccinationDays))
	if asOf.Before(holdEnd) {
		return "warming_hold"
	}
	return ""
}

func pregnancyDeferReason(g domain.EligibleGoat, preg genPregnancyPolicy, asOf time.Time) string {
	if !preg.active() {
		return ""
	}
	status := strings.ToLower(strings.TrimSpace(g.ReproductiveStatus))
	switch status {
	case "pregnant":
		if g.BreedingDate == nil {
			return "pregnancy_month_review"
		}
		month := pregnancyMonth(*g.BreedingDate, asOf)
		if preg.SkipFromPregnancyMonth > 0 && month >= int(preg.SkipFromPregnancyMonth) {
			if preg.SkipThroughPregnancyMonth <= 0 || month <= int(preg.SkipThroughPregnancyMonth) {
				return "late_pregnancy_hold"
			}
		}
		if preg.AllowUntilPregnancyMonth > 0 && month > int(preg.AllowUntilPregnancyMonth) {
			return "late_pregnancy_hold"
		}
	case "lactating", "mother", "milking":
		if preg.PostDeliveryCatchUpDays <= 0 {
			return ""
		}
		if g.LastDeliveryDate == nil {
			return ""
		}
		if wholeDaysBetween(*g.LastDeliveryDate, asOf) > int(preg.PostDeliveryCatchUpDays) {
			return "post_delivery_catch_up_closed"
		}
	}
	return ""
}

func pregnancyMonth(breedingDate, asOf time.Time) int {
	days := wholeDaysBetween(breedingDate, asOf)
	if days < 0 {
		return 0
	}
	return days/30 + 1
}

func reproductiveStateExcluded(g domain.EligibleGoat, excluded []string, preg genPregnancyPolicy) bool {
	for _, state := range excluded {
		normalized := normDim(state)
		if normalized == "" || !sameDim(state, g.ReproductiveStatus) {
			continue
		}
		if preg.active() && (normalized == "pregnant" || normalized == "lactating") {
			continue
		}
		return true
	}
	return false
}

func adjustPostArrivalDue(g domain.EligibleGoat, rule protodomain.Rule, proc genProcurementPolicy) time.Time {
	anchor := warmingEntryAt(g)
	if anchor == nil {
		return time.Time{}
	}
	due := anchor.Add(time.Duration(rule.OffsetDays) * 24 * time.Hour)
	if proc.WarmupNoVaccinationDays > 0 {
		minDue := anchor.AddDate(0, 0, int(proc.WarmupNoVaccinationDays))
		if due.Before(minDue) {
			due = minDue
		}
	}
	return due
}
