package postgres

import (
	"testing"
	"time"

	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
)

func TestPlanMovedDriveAssignmentsKeepsWholePartitionWhenTargetDateHasOnlyResidualCapacity(t *testing.T) {
	source := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	target := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	next := target.AddDate(0, 0, 1)
	operator := "20000000-0000-4000-8000-000000000001"
	rows := []movedDriveAssignment{
		{
			batchID:          "10000000-0000-4000-8000-000000000001",
			parkID:           "30000000-0000-4000-8000-000000000001",
			physicalShed:     "Godel 1",
			partitionLabel:   "Part 1",
			animalCount:      3,
			movedRuleIDs:     []string{"40000000-0000-4000-8000-000000000001"},
			movedAnimalCount: 3,
			movedDoseCount:   3,
		},
	}
	planned := planMovedDriveAssignments(rows, []vaccexecapp.DriveDateAvailability{
		{Date: target, Operators: []vaccexecapp.DriveOperator{{ID: operator, Name: "Darshan", Cap: 1, ConfiguredCap: 200, Available: true}}},
		{Date: next, Operators: []vaccexecapp.DriveOperator{{ID: operator, Name: "Darshan", Cap: 200, ConfiguredCap: 200, Available: true}}},
	}, target)

	byDate := map[string]int32{}
	for _, row := range planned {
		if row.capacityStatus != "within_cap" {
			t.Fatalf("capacity status = %q, want within_cap for split-forward move: %+v", row.capacityStatus, planned)
		}
		byDate[row.plannedDate.Format("2006-01-02")] += row.animalCount
	}
	if got := byDate[target.Format("2006-01-02")]; got != 0 {
		t.Fatalf("target date animals = %d, want 0 because the 3-animal partition must move whole: %+v", got, planned)
	}
	if got := byDate[next.Format("2006-01-02")]; got != 3 {
		t.Fatalf("next date animals = %d, want all 3 animals in the whole partition: %+v", got, planned)
	}
	if len(byDate) != 1 {
		t.Fatalf("planned dates = %#v, want only next date for the intact partition", byDate)
	}
	if source.After(target) {
		t.Fatalf("fixture sanity: source %s should not be after target %s", source, target)
	}
}
