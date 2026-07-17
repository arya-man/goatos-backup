package app

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

func TestComboSessionKeyFMDHS(t *testing.T) {
	if got := comboSessionKey("FMD"); got != "combo:FMD+HS" {
		t.Fatalf("FMD session = %q, want combo:FMD+HS", got)
	}
	if got := comboSessionKey("HS"); got != "combo:FMD+HS" {
		t.Fatalf("HS session = %q, want combo:FMD+HS", got)
	}
}

func TestBatchSessionUsesComboWhenKnown(t *testing.T) {
	if got := batchSession("rule-fmd", "FMD"); got != "combo:FMD+HS" {
		t.Fatalf("session = %q, want combo:FMD+HS", got)
	}
	if got := batchSession("rule-ppr", "PPR"); got != "combo:PPR+Blue Tongue" {
		t.Fatalf("session = %q, want combo:PPR+Blue Tongue", got)
	}
	if got := batchSession("rule-gpox", "Goat Pox"); got != "rule:rule-gpox" {
		t.Fatalf("session = %q, want rule:rule-gpox", got)
	}
}

func TestVaccineMatrixPriority(t *testing.T) {
	cases := map[string]int32{
		"ET+TT":       1,
		"ET_TT":       1,
		"PPR":         2,
		"Goat Pox":    3,
		"GOAT_POX":    3,
		"Sheep Pox":   3,
		"SHEEP_POX":   3,
		"Blue Tongue": 4,
		"BLUE_TONGUE": 4,
		"FMD":         5,
		"HS":          5,
	}
	for code, want := range cases {
		if got := VaccineMatrixPriority(code); got != want {
			t.Fatalf("%s priority = %d, want %d", code, got, want)
		}
	}
}

func TestDrivePlannerFromRuleDSLUsesMatrixPriority(t *testing.T) {
	_, planner := DrivePlannerFromRuleDSL([]byte(`{"vaccine":{"code":"PPR"}}`))
	if planner.VaccinePriority != 2 {
		t.Fatalf("PPR planner priority = %d, want 2", planner.VaccinePriority)
	}
	if planner.MaxGoatsPerDrive != 0 {
		t.Fatalf("max goats = %d, want unlimited (0)", planner.MaxGoatsPerDrive)
	}
}

func TestDrivePlannerFromRuleDSLReadsHoldGroupingAndShotCap(t *testing.T) {
	_, planner := DrivePlannerFromRuleDSL([]byte(`{"drive_policy":{"max_batching_hold_days":5,"max_batching_hold_count":1,"species_grouping_policy":"species_specific","max_shots_per_animal_per_drive":3}}`))
	if planner.MaxBatchingHoldDays != 5 || planner.MaxBatchingHoldCount != 1 || planner.SpeciesGroupingPolicy != "species_specific" || planner.MaxShotsPerAnimalPerDrive != 3 {
		t.Fatalf("planner = %#v, want configured hold/grouping/shot cap", planner)
	}
}

func TestPickBestDriveDatePrefersMaxCoverage(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	rows := []driveCandidate{
		{ObligationID: "a", DueAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), WindowEnd: &winEnd},
		{ObligationID: "b", DueAt: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC), WindowEnd: &winEnd},
	}
	got := pickBestDriveDate(now, rows, 2)
	if got == nil {
		t.Fatal("expected planned date")
	}
	// Both obligations first fit on Jul 8; later window-end dates are not better unless they add coverage.
	want := businessDate(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC))
	if !got.Equal(want) {
		t.Fatalf("planned=%s want %s", got, want)
	}
}

func TestSplitObligationIDsByMaxGoats(t *testing.T) {
	ids := []string{"1", "2", "3", "4", "5"}
	chunks := splitObligationIDs(ids, 2)
	if len(chunks) != 3 {
		t.Fatalf("chunks=%d want 3", len(chunks))
	}
	if len(chunks[0]) != 2 || len(chunks[1]) != 2 || len(chunks[2]) != 1 {
		t.Fatalf("unexpected chunk sizes: %#v", chunks)
	}
}

func TestSpeciesGroupingKeyMixesKidsOnly(t *testing.T) {
	if got := speciesGroupingKey("sheep", "K1", "kid_mixed"); got != "kid_mixed" {
		t.Fatalf("sheep kid key = %q, want kid_mixed", got)
	}
	if got := speciesGroupingKey("goat", "adult", "kid_mixed"); got != "species:goat" {
		t.Fatalf("goat adult key = %q, want species:goat", got)
	}
	if got := speciesGroupingKey("sheep", "K1", "species_specific"); got != "species:sheep" {
		t.Fatalf("species-specific kid key = %q, want species:sheep", got)
	}
}

func TestPickBestDriveDateWithHoldOnlyWhenCoverageImproves(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	rows := []driveCandidate{{
		ObligationID: "obl-1",
		DueAt:        now,
		WindowEnd:    &winEnd,
	}}
	planner := domain.DefaultDrivePlannerSettings()
	got := pickBestDriveDateWithHold(now, rows, planner)
	if got == nil || !got.Equal(businessDate(now)) {
		t.Fatalf("single-animal date = %v, want due date %v", got, businessDate(now))
	}

	laterDue := now.AddDate(0, 0, 5)
	rows = append(rows, driveCandidate{ObligationID: "obl-2", DueAt: laterDue, WindowEnd: &winEnd})
	got = pickBestDriveDateWithHold(now, rows, planner)
	want := businessDate(laterDue)
	if got == nil || !got.Equal(want) {
		t.Fatalf("clubbed date = %v, want first max-output date %v", got, want)
	}

	rows[0].BatchingHoldCount = planner.MaxBatchingHoldCount
	got = pickBestDriveDateWithHold(now, rows, planner)
	if got == nil || !got.Equal(businessDate(now)) {
		t.Fatalf("held-once date = %v, want due date %v", got, businessDate(now))
	}

	rows[0].BatchingHoldCount = 0
	rows[1].DueAt = now.AddDate(0, 0, int(planner.MaxBatchingHoldDays)+1)
	got = pickBestDriveDateWithHold(now, rows, planner)
	if got == nil || !got.Equal(businessDate(now)) {
		t.Fatalf("beyond-hold date = %v, want due date %v", got, businessDate(now))
	}
}

func TestPickBestDriveDateWithHoldDoesNotBackdateOverdueHoldCap(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	planner := domain.DefaultDrivePlannerSettings()
	rows := []driveCandidate{{
		ObligationID: "overdue-valid",
		DueAt:        due,
		WindowEnd:    &winEnd,
	}}

	got := pickBestDriveDateWithHold(now, rows, planner)
	want := businessDate(now)
	if got == nil || !got.Equal(want) {
		t.Fatalf("overdue held date = %v, want sweep day %v", got, want)
	}

	rows[0].BatchingHoldCount = planner.MaxBatchingHoldCount
	got = pickBestDriveDateWithHold(now, rows, planner)
	if got == nil || !got.Equal(want) {
		t.Fatalf("already-held overdue date = %v, want sweep day %v", got, want)
	}
}

func TestSelectIDsWithinVisitShotCap(t *testing.T) {
	planned := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "a", TargetID: "goat-1"},
		{ObligationID: "b", TargetID: "goat-1"},
		{ObligationID: "c", TargetID: "goat-1"},
		{ObligationID: "d", TargetID: "goat-2"},
	}
	counts := make(map[string]int32)
	got := selectIDsWithinVisitShotCap(rows, &planned, 2, counts)
	want := []string{"a", "b", "d"}
	if len(got) != len(want) {
		t.Fatalf("selected=%#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selected=%#v want %#v", got, want)
		}
	}
}

func TestNormalizedDrivePlannerSettingsDefaults(t *testing.T) {
	got := normalizedDrivePlannerSettings(domain.DrivePlannerSettings{Enabled: true}, "PPR")
	if got.VaccinePriority != 2 {
		t.Fatalf("priority = %d, want matrix PPR=2", got.VaccinePriority)
	}
	if got.ComboAlignWindowDays != domain.DefaultDrivePlannerSettings().ComboAlignWindowDays {
		t.Fatalf("combo window = %d", got.ComboAlignWindowDays)
	}
	if got.MaxShotsPerAnimalPerDrive != 2 || got.MaxBatchingHoldCount != 1 || got.MaxBatchingHoldDays != 7 {
		t.Fatalf("planner defaults = %#v, want shot cap/hold defaults", got)
	}
}

func TestComboAlignmentSettingsForPlansUsesStrictestPolicy(t *testing.T) {
	plans := []SweepVersionPriority{
		{
			VersionID: "fmd",
			Config: SweepConfig{
				VaccineCode: "FMD",
				DrivePlanner: domain.DrivePlannerSettings{
					Enabled:                   true,
					ComboAlignWindowDays:      10,
					MaxShotsPerAnimalPerDrive: 2,
				},
			},
		},
		{
			VersionID: "hs",
			Config: SweepConfig{
				VaccineCode: "HS",
				DrivePlanner: domain.DrivePlannerSettings{
					Enabled:                   true,
					ComboAlignWindowDays:      3,
					MaxShotsPerAnimalPerDrive: 1,
				},
			},
		},
	}

	alignWindowDays, maxShotsPerAnimalPerDrive := ComboAlignmentSettingsForPlans(plans)
	if alignWindowDays != 3 {
		t.Fatalf("align window = %d, want strictest 3", alignWindowDays)
	}
	if maxShotsPerAnimalPerDrive != 1 {
		t.Fatalf("max shots = %d, want strictest 1", maxShotsPerAnimalPerDrive)
	}
}
