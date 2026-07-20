package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

const (
	SchedulePathKid              = "kid"
	SchedulePathAdultProcurement = "adult_procurement"

	schedulePathKid              = SchedulePathKid
	schedulePathAdultProcurement = SchedulePathAdultProcurement
)

// SchedulePathProcurementPolicy is the public, seed-safe subset of the vaccination
// procurement policy needed to choose between kid and adult-procurement rule families.
type SchedulePathProcurementPolicy struct {
	KidsNormalScheduleUntilWeeks int32
}

type genCompatibilityPolicy struct {
	LiveToKilledGapDays           int32 `json:"live_to_killed_gap_days"`
	KilledToKilledGapDays         int32 `json:"killed_to_killed_gap_days"`
	LiveToLiveGapDays             int32 `json:"live_to_live_gap_days"`
	CourseBoosterMinGapDays       int32 `json:"kid_booster_min_gap_days"`
	BacterialViralSameDayAllowed  bool  `json:"bacterial_viral_same_day_allowed"`
	LiveKilledViralSameDayAllowed bool  `json:"live_killed_viral_same_day_allowed"`
	MaxVaccinesPerComboSession    int32 `json:"max_vaccines_per_combo_session"`
}

type genProcurementPolicy struct {
	WarmupNoVaccinationDays      int32 `json:"warmup_no_vaccination_days"`
	KidsNormalScheduleUntilWeeks int32 `json:"kids_normal_schedule_until_weeks"`
	// AdultPriorVaccinationAllowed is a pointer so an EXPLICIT false is distinguishable from an
	// omitted field (R2-03). A plain bool made active() require a positive field, so publishing only
	// {"adult_prior_vaccination_allowed": false} left the policy "inactive" and the false was ignored.
	AdultPriorVaccinationAllowed *bool `json:"adult_prior_vaccination_allowed"`
}

func (p genProcurementPolicy) active() bool {
	// An explicitly-present adult-prior flag (true OR false) makes the procurement policy active,
	// even when every numeric field is zero -- R2-03.
	return p.WarmupNoVaccinationDays > 0 || p.KidsNormalScheduleUntilWeeks > 0 || p.AdultPriorVaccinationAllowed != nil
}

// adultPriorAllowed reports whether adult prior vaccination history may suppress/anchor adult work.
// Default is true (backward compat) when the flag is omitted; an explicit false is honored (R2-03).
func (p genProcurementPolicy) adultPriorAllowed() bool {
	return p.AdultPriorVaccinationAllowed == nil || *p.AdultPriorVaccinationAllowed
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

type genRecoveryPolicy struct {
	MaxNearbyDriveAlignDays int32 `json:"max_nearby_drive_align_days"`
}

func (p genRecoveryPolicy) alignDays() int32 {
	if p.MaxNearbyDriveAlignDays > 0 {
		return p.MaxNearbyDriveAlignDays
	}
	return 7
}

type genVersionPolicies struct {
	Procurement   genProcurementPolicy
	Pregnancy     genPregnancyPolicy
	Compatibility genCompatibilityPolicy
	Recovery      genRecoveryPolicy
	MissedDose    genMissedDosePolicy
}

type genMissedDosePolicy struct {
	NearbyDriveAlignDays          int32 `json:"nearby_drive_align_days"`
	MaterializeOnlyFutureOpenWork bool  `json:"materialize_only_future_open_work"`
}

func (p *genMissedDosePolicy) UnmarshalJSON(raw []byte) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		*p = genMissedDosePolicy{}
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var legacy string
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return err
		}
		*p = genMissedDosePolicy{}
		return nil
	}
	type alias genMissedDosePolicy
	var out alias
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*p = genMissedDosePolicy(out)
	return nil
}

func (p genMissedDosePolicy) alignDays() int32 {
	if p.NearbyDriveAlignDays > 0 {
		return p.NearbyDriveAlignDays
	}
	return 14
}

const (
	recoveryAlignNearbyDrive = "recovery_align_nearby_drive"
	recoveryMicroDrive       = "recovery_micro_drive"
)

// recoveryRescheduleDue picks the due time after a health defer clears. When a planned drive exists
// within the align window, join that drive date; otherwise due immediately so the sweeper can run a
// micro-drive for a single goat.
func recoveryRescheduleDue(asOf time.Time, policy genRecoveryPolicy, nearbyDriveDate *time.Time) (time.Time, string) {
	recoveredDay := businessDayStart(asOf)
	alignEnd := recoveredDay.AddDate(0, 0, int(policy.alignDays()))
	if nearbyDriveDate != nil {
		aligned := businessDayStart(*nearbyDriveDate)
		if !aligned.Before(recoveredDay) && !aligned.After(alignEnd) {
			return aligned, recoveryAlignNearbyDrive
		}
	}
	return asOf.In(biztime.DefaultLocation()), recoveryMicroDrive
}

func recoveryDueWindows(due time.Time, dueWindowDays int32) (time.Time, *time.Time) {
	start := due.In(biztime.DefaultLocation())
	if dueWindowDays <= 0 {
		return start, nil
	}
	end := start.AddDate(0, 0, int(dueWindowDays))
	return start, &end
}

func businessDayStart(t time.Time) time.Time {
	return biztime.BusinessDayStart(t)
}

// schedulePathForGoat decides whether birth_age (kid) or post_arrival (adult
// procurement) rules apply to this goat. B4 (age transition): a NEW kid course may
// only START through kidWeeks (default 16); a kid already progressing through the
// course may FINISH its spacing-shifted dose through finishWeeks (kidWeeks+4 = 20,
// e.g. Goat Pox derived to 20w after a 16w PPR dose). Past finishWeeks the goat is
// always adult — origin_type="birth" no longer bypasses age unconditionally (the
// confirmed defect: kid doses generated for animals long past the cutoff). An
// explicit kid-management stage tag is still honored past finishWeeks: Operating
// Rules say "if a tag and age disagree, review the animal" — a live K-stage tag is
// a stronger, reviewable signal than a blank/adult default, so it is not silently
// discarded, only no longer inferred from origin_type alone.
//
// B3 (never-received vaccine + no DOB): when DOB is unknown, NEVER default to kid.
// A kid-management stage tag or an existing kid-course vaccination proves the
// animal is genuinely on the kid course (B1/B2 per-vaccine continuation still
// applies regardless of path via after_previous_completion rules); otherwise route
// to the adult catch-up/primary path so a never-received vaccine is scheduled from
// the adult two-visit course rather than a fabricated kid_12w/kid_16w obligation.
func schedulePathForGoat(g domain.EligibleGoat, proc genProcurementPolicy, asOf time.Time, vaccineHistory []domain.RecentVaccineAdministration) string {
	return SchedulePathForGoat(g, SchedulePathProcurementPolicy{
		KidsNormalScheduleUntilWeeks: proc.KidsNormalScheduleUntilWeeks,
	}, asOf, vaccineHistory)
}

// SchedulePathForGoat decides whether birth_age (kid) or post_arrival (adult procurement)
// rules apply to a goat. Runtime generation, seed import, and any replay/cutover code must
// use this shared decision instead of carrying a local shortcut such as "origin=birth is kid".
func SchedulePathForGoat(g domain.EligibleGoat, proc SchedulePathProcurementPolicy, asOf time.Time, vaccineHistory []domain.RecentVaccineAdministration) string {
	kidWeeks := int32(16)
	if proc.KidsNormalScheduleUntilWeeks > 0 {
		kidWeeks = proc.KidsNormalScheduleUntilWeeks
	}
	finishWeeks := kidWeeks + 4

	if g.DOB != nil {
		ageWeeks := wholeDaysBetween(*g.DOB, asOf) / 7
		if ageWeeks <= int(kidWeeks) {
			// A NEW kid course may only START through kidWeeks (default 16).
			return schedulePathKid
		}
		if ageWeeks <= int(finishWeeks) {
			// 16-20w is CONTINUATION-ONLY: a kid already in the course may finish its
			// spacing-shifted dose, but a new course may NOT start here. "Already in the course"
			// = a kid-management stage tag (still a FRESH signal at this age) or a recorded
			// kid-course administration. A goat with neither cannot start a new course and routes
			// adult (fixes the "16-20w always kid even if never started" defect).
			if isKidManagementStage(g.Stage) || hasKidCourseHistory(vaccineHistory) {
				return schedulePathKid
			}
			return schedulePathAdultProcurement
		}
		// Past finishWeeks (20w) the goat is ALWAYS adult. A K1/K2 stage tag is now STALE and no
		// longer overrides age — it must never generate kid vaccinations (confirmed defect #5). The
		// tag/age conflict is surfaced as a review signal in genOneGoat, not by routing kid.
		return schedulePathAdultProcurement
	}

	if isKidManagementStage(g.Stage) {
		return schedulePathKid
	}
	if hasKidCourseHistory(vaccineHistory) {
		return schedulePathKid
	}
	return schedulePathAdultProcurement
}

// DerivedStageFromDOB derives the age-appropriate kid/adult stage from a goat's DOB.
// It is the canonical stage derived from pure age and must be used during seed and ingestion
// to auto-correct contradictory source tags (e.g. K2 tag on a 22-week-old goat).
// Returns "K1" (kid) or "Adult" based on age at asOf time, using the vaccination schedule's
// kid-course finish cutoff (default: 16 weeks start, 20 weeks finish).
func DerivedStageFromDOB(dob *time.Time, asOf time.Time) string {
	if dob == nil {
		// No DOB: cannot derive age-based stage, return empty (use provided stage or default)
		return ""
	}
	ageWeeks := wholeDaysBetween(*dob, asOf) / 7
	kidFinishWeeks := 16 + 4 // default kidWeeks=16, finishWeeks=kidWeeks+4
	if ageWeeks <= kidFinishWeeks {
		return "K1" // Kid-management stage indicator
	}
	return "Adult" // Past kid finish cutoff
}

func kidFinishWeeks(proc genProcurementPolicy) int {
	kidWeeks := int32(16)
	if proc.KidsNormalScheduleUntilWeeks > 0 {
		kidWeeks = proc.KidsNormalScheduleUntilWeeks
	}
	return int(kidWeeks) + 4
}

func isKidManagementStage(stage string) bool {
	stage = strings.ToUpper(strings.TrimSpace(stage))
	if len(stage) >= 2 && strings.HasPrefix(stage, "K") {
		return true
	}
	return false
}

// hasKidCourseHistory reports whether the goat has any accepted administration
// recorded against a kid-course (birth_age) dose. Used only when DOB is unknown, to
// distinguish a genuinely-tracked kid (B4 in-course signal) from a never-received
// vaccine with no anchor at all (B3: must route adult, never default to kid). The
// seeded matrix's dose_code convention marks every kid-course dose with a "_kid_"
// segment (e.g. "fmd_kid_12w") distinct from adult ("_adult_wN") and repeat
// ("_revac") doses; RecentVaccineAdministration does not carry the originating
// rule's trigger_type, so this is the least-coupled available signal without
// widening this task's scope into the domain/adapter layer.
func hasKidCourseHistory(history []domain.RecentVaccineAdministration) bool {
	for _, admin := range history {
		if admin.AdministeredAt.IsZero() {
			continue
		}
		if strings.Contains(strings.ToLower(strings.TrimSpace(admin.DoseCode)), "_kid_") {
			return true
		}
	}
	return false
}

// isPrimaryAnchorRule reports whether a rule schedules a primary dose off a birth/arrival anchor
// (DOB or entry_date). These are exactly the rules that vaccination history outranks: once a
// same-vaccine administration exists, after_previous_completion owns future scheduling and these
// anchor-based primaries must be suppressed.
func isPrimaryAnchorRule(rule protodomain.Rule) bool {
	return rule.TriggerType == "birth_age" || rule.TriggerType == "post_arrival"
}

func ruleMatchesSchedulePath(rule protodomain.Rule, path string) bool {
	switch rule.TriggerType {
	case "manual_campaign", "calendar":
		return true
	case "after_previous_completion":
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
		start := businessDayStart(*g.EntryDate)
		return &start
	}
	return nil
}

func policyDeferReason(g domain.EligibleGoat, policies genVersionPolicies, asOf time.Time) string {
	if reason := warmingDeferReason(g, policies.Procurement, asOf); reason != "" {
		return reason
	}
	if reason := postBreedingDeferReason(g, asOf); reason != "" {
		return reason
	}
	if reason := pregnancyDeferReason(g, policies.Pregnancy, asOf); reason != "" {
		return reason
	}
	if reason := milkingDeferReason(g); reason != "" {
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
	if wholeDaysBetween(*anchor, asOf) < int(proc.WarmupNoVaccinationDays) {
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
			// Locked rule (section 5): "Pregnant, month unknown -> schedule normally".
			// Missing/unknown gestation must NOT block — only a PROVEN pregnancy month 4-5
			// (a known breeding date) defers. A missing breeding date is not proof of a
			// late-pregnancy window, so scheduling proceeds.
			return ""
		}
		month := pregnancyMonth(*g.BreedingDate, asOf)
		if preg.SkipFromPregnancyMonth > 0 && month >= int(preg.SkipFromPregnancyMonth) {
			if preg.SkipThroughPregnancyMonth <= 0 || month <= int(preg.SkipThroughPregnancyMonth) {
				return "late_pregnancy_hold"
			}
		}
	case "lactating", "mother", "milking":
		// Outside the post-delivery catch-up window, normal scheduling resumes.
		if preg.PostDeliveryCatchUpDays <= 0 || g.LastDeliveryDate == nil {
			return ""
		}
		if wholeDaysBetween(*g.LastDeliveryDate, asOf) > int(preg.PostDeliveryCatchUpDays) {
			return ""
		}
	}
	return ""
}

func postBreedingDeferReason(g domain.EligibleGoat, asOf time.Time) string {
	status := strings.ToLower(strings.TrimSpace(g.ReproductiveStatus))
	if status != "bred" && status != "breeding" {
		return ""
	}
	if g.BreedingDate == nil {
		return "post_breeding_date_review"
	}
	if wholeDaysBetween(*g.BreedingDate, asOf) < 30 {
		return "post_breeding_hold"
	}
	return ""
}

func milkingDeferReason(g domain.EligibleGoat) string {
	status := strings.ToLower(strings.TrimSpace(g.ReproductiveStatus))
	stage := strings.ToUpper(strings.TrimSpace(g.Stage))
	if status == "milking" || (strings.Contains(stage, "MILKING") && !strings.Contains(stage, "WAITING") && !strings.Contains(stage, "WARMUP")) {
		return "milking_window_hold"
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
	due := businessDayStart(*anchor).AddDate(0, 0, int(rule.OffsetDays))
	if proc.WarmupNoVaccinationDays > 0 {
		minDue := businessDayStart(*anchor).AddDate(0, 0, int(proc.WarmupNoVaccinationDays))
		if due.Before(minDue) {
			due = minDue
		}
	}
	return due
}
