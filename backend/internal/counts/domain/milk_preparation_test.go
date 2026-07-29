package domain

import "testing"

func TestBuildMilkPreparationRowUsesApprovedCohortVolumes(t *testing.T) {
	tests := []struct {
		stage        string
		headCount    int
		wantSessions []int64
		wantDailyML  int64
	}{
		{stage: "K1", headCount: 3, wantSessions: []int64{600, 600, 600, 600}, wantDailyML: 2400},
		{stage: "K2", headCount: 2, wantSessions: []int64{600, 600, 600, 600}, wantDailyML: 2400},
		{stage: "K3", headCount: 4, wantSessions: []int64{800, 0, 0, 800}, wantDailyML: 1600},
	}

	for _, tc := range tests {
		t.Run(tc.stage, func(t *testing.T) {
			row, ok := BuildMilkPreparationRow(MilkPreparationCohortCount{
				ShedID:          "shed-1",
				ShedLabel:       "Kid Shed",
				ManagementStage: tc.stage,
				HeadCount:       tc.headCount,
			})
			if !ok {
				t.Fatalf("stage %s was not recognized", tc.stage)
			}
			if row.DailyRequiredML != tc.wantDailyML {
				t.Fatalf("daily ml=%d, want %d", row.DailyRequiredML, tc.wantDailyML)
			}
			if len(row.Sessions) != len(tc.wantSessions) {
				t.Fatalf("sessions=%d, want %d", len(row.Sessions), len(tc.wantSessions))
			}
			for i, want := range tc.wantSessions {
				if row.Sessions[i].RequiredML != want {
					t.Errorf("session %d ml=%d, want %d", i+1, row.Sessions[i].RequiredML, want)
				}
			}
		})
	}
}

func TestBuildMilkPreparationSummaryUsesWholeScopeAndCitricRatio(t *testing.T) {
	rows := []MilkPreparationRow{
		{ShedID: "shed-1", HeadCount: 3, DailyRequiredML: 2400, Status: MilkPreparationStatusReady},
		{ShedID: "shed-1", HeadCount: 2, DailyRequiredML: 2400, Status: MilkPreparationStatusReady},
		{ShedID: "", HeadCount: 4, DailyRequiredML: 1600, Status: MilkPreparationStatusBlocked},
	}

	got := BuildMilkPreparationSummary(rows)
	if got.ShedCount != 1 || got.CohortCount != 3 || got.HeadCount != 9 {
		t.Fatalf("summary counts=%+v", got)
	}
	if got.TotalRequiredML != 6400 {
		t.Fatalf("total ml=%d, want 6400", got.TotalRequiredML)
	}
	if got.CitricAcidGrams != 35.2 {
		t.Fatalf("citric acid=%v, want 35.2", got.CitricAcidGrams)
	}
	if got.BlockedRowCount != 1 {
		t.Fatalf("blocked rows=%d, want 1", got.BlockedRowCount)
	}
}

func TestBuildMilkPreparationRowRejectsNonMilkStage(t *testing.T) {
	if _, ok := BuildMilkPreparationRow(MilkPreparationCohortCount{ManagementStage: "Adult", HeadCount: 10}); ok {
		t.Fatal("adult stage must not become a milk preparation row")
	}
}
