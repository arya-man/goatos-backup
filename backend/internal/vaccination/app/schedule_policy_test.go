package app

import (
	"testing"
	"time"

	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestSchedulePathForGoat(t *testing.T) {
	dob := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := dob.AddDate(0, 0, 200) // ~28 weeks
	proc := genProcurementPolicy{KidsNormalScheduleUntilWeeks: 16}

	farmBorn := domain.EligibleGoat{OriginType: "birth", DOB: &dob, Stage: "adult"}
	if got := schedulePathForGoat(farmBorn, proc, asOf); got != schedulePathKid {
		t.Fatalf("farm-born = %q, want kid", got)
	}

	procuredKidDOB := dob.AddDate(0, 0, 70)
	procuredKid := domain.EligibleGoat{OriginType: "procured", DOB: &procuredKidDOB, Stage: "K1"}
	if got := schedulePathForGoat(procuredKid, proc, asOf); got != schedulePathKid {
		t.Fatalf("procured <=16w = %q, want kid", got)
	}

	procuredAdult := domain.EligibleGoat{OriginType: "procured", DOB: &dob, Stage: "adult"}
	if got := schedulePathForGoat(procuredAdult, proc, asOf); got != schedulePathAdultProcurement {
		t.Fatalf("procured adult = %q, want adult_procurement", got)
	}

	procuredOldKidStage := domain.EligibleGoat{OriginType: "procured", DOB: &dob, Stage: "K2"}
	if got := schedulePathForGoat(procuredOldKidStage, proc, asOf); got != schedulePathKid {
		t.Fatalf("procured old kid stage = %q, want kid", got)
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
