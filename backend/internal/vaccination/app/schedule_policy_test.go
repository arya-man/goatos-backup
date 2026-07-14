package app

import (
	"testing"
	"time"

	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestSchedulePathForGoat(t *testing.T) {
	dob := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := dob.AddDate(0, 0, 200) // ~28.5 weeks: past the 20w kid-course finishing window
	proc := genProcurementPolicy{KidsNormalScheduleUntilWeeks: 16}

	// B4 fix: origin_type="birth" no longer bypasses age unconditionally. A farm-born
	// animal with no kid-management stage tag, long past the 20w finishing window, is
	// adult — this was confirmed bug #5 ("kid doses generated for animals long past
	// the cutoff"; origin=birth previously forced kid path regardless of age).
	farmBornPastCutoff := domain.EligibleGoat{OriginType: "birth", DOB: &dob, Stage: "adult"}
	if got := schedulePathForGoat(farmBornPastCutoff, proc, asOf, nil); got != schedulePathAdultProcurement {
		t.Fatalf("farm-born past 20w cutoff, no kid tag = %q, want adult_procurement", got)
	}

	// A farm-born animal still within the 16-20w finishing window stays kid so it can
	// receive its spacing-shifted dose (e.g. 20w-derived Goat Pox after 16w PPR).
	finishingWindowAsOf := dob.AddDate(0, 0, 126) // 18 weeks
	farmBornFinishing := domain.EligibleGoat{OriginType: "birth", DOB: &dob, Stage: "adult"}
	if got := schedulePathForGoat(farmBornFinishing, proc, finishingWindowAsOf, nil); got != schedulePathKid {
		t.Fatalf("farm-born within 16-20w finishing window = %q, want kid", got)
	}

	procuredKidDOB := dob.AddDate(0, 0, 70)
	procuredKid := domain.EligibleGoat{OriginType: "procured", DOB: &procuredKidDOB, Stage: "K1"}
	if got := schedulePathForGoat(procuredKid, proc, asOf, nil); got != schedulePathKid {
		t.Fatalf("procured <=16w = %q, want kid", got)
	}

	procuredAdult := domain.EligibleGoat{OriginType: "procured", DOB: &dob, Stage: "adult"}
	if got := schedulePathForGoat(procuredAdult, proc, asOf, nil); got != schedulePathAdultProcurement {
		t.Fatalf("procured adult = %q, want adult_procurement", got)
	}

	// A live kid-management stage tag past the finishing window is still honored
	// (Operating Rules: "if a tag and age disagree, review the animal" — a live K-tag
	// is a stronger, reviewable signal than a blank/adult default).
	procuredOldKidStage := domain.EligibleGoat{OriginType: "procured", DOB: &dob, Stage: "K2"}
	if got := schedulePathForGoat(procuredOldKidStage, proc, asOf, nil); got != schedulePathKid {
		t.Fatalf("procured old kid stage = %q, want kid", got)
	}

	// B3: DOB unknown, no stage tag, no kid-course vaccination history → must route
	// adult, never default to kid (the 257-animal blank-origin/no-DOB/no-arrival
	// cohort from the audit).
	noSignal := domain.EligibleGoat{Stage: ""}
	if got := schedulePathForGoat(noSignal, proc, asOf, nil); got != schedulePathAdultProcurement {
		t.Fatalf("no DOB, no stage, no history = %q, want adult_procurement", got)
	}

	// B2/B4: DOB unknown but the goat has an accepted kid-course vaccination on
	// record — genuinely a kid in progress, so per-vaccine continuation (B1) via the
	// kid path is honored rather than mis-routed to adult.
	kidHistory := []domain.RecentVaccineAdministration{{
		AdministeredAt: dob.AddDate(0, 0, 84),
		VaccineCode:    "FMD",
		DoseCode:       "fmd_kid_12w",
	}}
	noDOBWithKidHistory := domain.EligibleGoat{Stage: ""}
	if got := schedulePathForGoat(noDOBWithKidHistory, proc, asOf, kidHistory); got != schedulePathKid {
		t.Fatalf("no DOB with kid-course history = %q, want kid", got)
	}
}

func TestRecoveryRescheduleDue(t *testing.T) {
	policy := genRecoveryPolicy{}
	asOf := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	nearby := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	due, reason := recoveryRescheduleDue(asOf, policy, &nearby)
	wantNearby := businessDayStart(nearby)
	if !due.Equal(wantNearby) || reason != recoveryAlignNearbyDrive {
		t.Fatalf("nearby align = %v %q, want %v %q", due, reason, wantNearby, recoveryAlignNearbyDrive)
	}
	due, reason = recoveryRescheduleDue(asOf, policy, nil)
	if !due.Equal(asOf) || due.Location().String() != "Asia/Kolkata" || reason != recoveryMicroDrive {
		t.Fatalf("micro-drive = %v %q, want %v in Asia/Kolkata with %q", due, reason, asOf, recoveryMicroDrive)
	}
	far := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	due, reason = recoveryRescheduleDue(asOf, policy, &far)
	if !due.Equal(asOf) || due.Location().String() != "Asia/Kolkata" || reason != recoveryMicroDrive {
		t.Fatalf("beyond window = %v %q, want micro-drive now", due, reason)
	}
}

func TestRecoveryRescheduleDueUsesOneWeekMaxBuffer(t *testing.T) {
	policy := genRecoveryPolicy{MaxNearbyDriveAlignDays: 7}
	cases := []struct {
		name       string
		recovered  time.Time
		drive      time.Time
		wantReason string
		wantDue    time.Time
	}{
		{
			name:       "july 5 recovery can join july 10 drive",
			recovered:  time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC),
			drive:      time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
			wantReason: recoveryAlignNearbyDrive,
			wantDue:    businessDayStart(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:       "july 4 recovery can join july 10 drive",
			recovered:  time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC),
			drive:      time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
			wantReason: recoveryAlignNearbyDrive,
			wantDue:    businessDayStart(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:       "seven day boundary can still batch",
			recovered:  time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC),
			drive:      time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC),
			wantReason: recoveryAlignNearbyDrive,
			wantDue:    businessDayStart(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:       "eight days cannot wait and becomes micro drive",
			recovered:  time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC),
			drive:      time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC),
			wantReason: recoveryMicroDrive,
			wantDue:    time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotDue, gotReason := recoveryRescheduleDue(tc.recovered, policy, &tc.drive)
			if gotReason != tc.wantReason || !gotDue.Equal(tc.wantDue) {
				t.Fatalf("got due=%v reason=%q, want due=%v reason=%q", gotDue, gotReason, tc.wantDue, tc.wantReason)
			}
		})
	}
}

func TestSelectorMatchesTreatsStarAsWildcard(t *testing.T) {
	if !selectorMatches("goat", genStringList{"*"}) {
		t.Fatalf("star selector should match any concrete dimension")
	}
	if !selectorMatches("female", genStringList{"any"}) || !selectorMatches("male", genStringList{"all"}) {
		t.Fatalf("any/all selectors should remain wildcard aliases")
	}
}

func TestWarmingDeferReason(t *testing.T) {
	entry := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	proc := genProcurementPolicy{WarmupNoVaccinationDays: 7}
	g := domain.EligibleGoat{WarmingEntryAt: &entry}

	if got := warmingDeferReason(g, proc, entry.AddDate(0, 0, 3)); got != "warming_hold" {
		t.Fatalf("day 3 = %q, want warming_hold", got)
	}
	if got := warmingDeferReason(g, proc, entry.AddDate(0, 0, 8)); got != "" {
		t.Fatalf("day 8 = %q, want no hold", got)
	}
}

func TestPregnancyDeferReason(t *testing.T) {
	preg := genPregnancyPolicy{
		AllowUntilPregnancyMonth:  3,
		SkipFromPregnancyMonth:    4,
		SkipThroughPregnancyMonth: 5,
		PostDeliveryCatchUpDays:   14,
	}
	breeding := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	unknown := domain.EligibleGoat{ReproductiveStatus: "pregnant"}
	if got := pregnancyDeferReason(unknown, preg, breeding.AddDate(0, 2, 0)); got != "pregnancy_month_review" {
		t.Fatalf("unknown month = %q, want pregnancy_month_review", got)
	}

	early := domain.EligibleGoat{ReproductiveStatus: "pregnant", BreedingDate: &breeding}
	if got := pregnancyDeferReason(early, preg, breeding.AddDate(0, 1, 15)); got != "" {
		t.Fatalf("month 2 = %q, want allowed", got)
	}

	late := domain.EligibleGoat{ReproductiveStatus: "pregnant", BreedingDate: &breeding}
	if got := pregnancyDeferReason(late, preg, breeding.AddDate(0, 4, 0)); got != "late_pregnancy_hold" {
		t.Fatalf("month 5 = %q, want late_pregnancy_hold", got)
	}
}

func TestPolicyDeferReasonPostBreedingHold(t *testing.T) {
	breeding := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	policies := genVersionPolicies{}
	held := domain.EligibleGoat{ReproductiveStatus: "bred", BreedingDate: &breeding}
	if got := policyDeferReason(held, policies, breeding.AddDate(0, 0, 20)); got != "post_breeding_hold" {
		t.Fatalf("day 20 reason = %q, want post_breeding_hold", got)
	}
	if got := policyDeferReason(held, policies, breeding.AddDate(0, 0, 31)); got != "" {
		t.Fatalf("day 31 reason = %q, want released", got)
	}
	missingDate := domain.EligibleGoat{ReproductiveStatus: "breeding"}
	if got := policyDeferReason(missingDate, policies, breeding.AddDate(0, 0, 20)); got != "post_breeding_date_review" {
		t.Fatalf("missing date reason = %q, want post_breeding_date_review", got)
	}
	flushing := domain.EligibleGoat{ReproductiveStatus: "flushing"}
	if got := policyDeferReason(flushing, policies, breeding.AddDate(0, 0, 20)); got != "" {
		t.Fatalf("flushing reason = %q, want no post-breeding hold", got)
	}
}

func TestPolicyDeferReasonMilkingWindowHold(t *testing.T) {
	policies := genVersionPolicies{}
	milkingStatus := domain.EligibleGoat{ReproductiveStatus: "milking"}
	if got := policyDeferReason(milkingStatus, policies, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)); got != "milking_window_hold" {
		t.Fatalf("milking status reason = %q, want milking_window_hold", got)
	}
	milkingStage := domain.EligibleGoat{Stage: "MILKING"}
	if got := policyDeferReason(milkingStage, policies, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)); got != "milking_window_hold" {
		t.Fatalf("milking stage reason = %q, want milking_window_hold", got)
	}
	waitingStage := domain.EligibleGoat{Stage: "MOTHER_MILKING_WAITING"}
	if got := policyDeferReason(waitingStage, policies, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)); got != "" {
		t.Fatalf("waiting stage reason = %q, want schedulable outside active milking", got)
	}
	warmupStage := domain.EligibleGoat{Stage: "MILKING_WARMUP"}
	if got := policyDeferReason(warmupStage, policies, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)); got != "" {
		t.Fatalf("warmup stage reason = %q, want schedulable outside active milking", got)
	}
}

func TestMissedDosePolicyDefaultsToTwoWeekNearbyDrive(t *testing.T) {
	if got := (genMissedDosePolicy{}).alignDays(); got != 14 {
		t.Fatalf("default missed-dose align days = %d, want 14", got)
	}
	if got := (genMissedDosePolicy{NearbyDriveAlignDays: 9}).alignDays(); got != 9 {
		t.Fatalf("configured missed-dose align days = %d, want 9", got)
	}
}

func TestAdjustPostArrivalDueHonorsWarmupFloor(t *testing.T) {
	entry := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	g := domain.EligibleGoat{EntryDate: &entry, WarmingEntryAt: &entry}
	rule := protodomain.Rule{TriggerType: "post_arrival", OffsetDays: 0}
	proc := genProcurementPolicy{WarmupNoVaccinationDays: 7}

	got := adjustPostArrivalDue(g, rule, proc)
	want := businessDayStart(entry).AddDate(0, 0, 7)
	if !got.Equal(want) {
		t.Fatalf("due = %s, want %s", got, want)
	}
}

func TestRuleMatchesSchedulePath(t *testing.T) {
	kidRule := protodomain.Rule{TriggerType: "birth_age"}
	adultRule := protodomain.Rule{TriggerType: "post_arrival"}
	if !ruleMatchesSchedulePath(kidRule, schedulePathKid) || ruleMatchesSchedulePath(adultRule, schedulePathKid) {
		t.Fatalf("kid path matching failed")
	}
	if !ruleMatchesSchedulePath(adultRule, schedulePathAdultProcurement) || ruleMatchesSchedulePath(kidRule, schedulePathAdultProcurement) {
		t.Fatalf("adult path matching failed")
	}
}
