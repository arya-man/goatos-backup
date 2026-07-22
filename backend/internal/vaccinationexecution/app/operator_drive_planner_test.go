package app

import (
	"testing"
	"time"
)

func TestOperatorDrivePlannerCPTAdultMockRunUsesAllOperatorsByAnimalCap(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate: date(2026, 7, 23),
		Availability: []DriveDateAvailability{{
			Date: date(2026, 7, 23),
			Operators: []DriveOperator{
				{ID: "amit", Name: "Amit Kumar", Cap: 200, Available: true},
				{ID: "darshan", Name: "Darshan Talwar", Cap: 200, Available: true},
				{ID: "sagar", Name: "Sagar Mahoor", Cap: 200, Available: true},
			},
		}},
		WorkBlocks: []DriveWorkBlock{
			{ID: "gandhi-1", Park: "CPT", RawShed: "Gandhi 1", Animals: 42, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "gandhi-2", Park: "CPT", RawShed: "Gandhi 2", Animals: 31, Bundle: "Goat: ET+TT Booster + Goat Pox"},
			{ID: "gandhi-3", Park: "CPT", RawShed: "Gandhi 3", Animals: 41, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "godel-1-part-1", Park: "CPT", RawShed: "Godel 1 - Part 1", Animals: 58, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "godel-1-part-3", Park: "CPT", RawShed: "Godel 1 - Part 3", Animals: 30, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "godel-1-part-4", Park: "CPT", RawShed: "Godel 1 - Part 4", Animals: 30, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "godel-2-part-4", Park: "CPT", RawShed: "Godel 2 - Part 4", Animals: 32, Bundle: "Goat: ET+TT Booster + Goat Pox"},
			{ID: "mandela-2-part-1", Park: "CPT", RawShed: "Mandela 2 - Part 1", Animals: 1, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "mandela-2-part-2", Park: "CPT", RawShed: "Mandela 2 - Part 2", Animals: 2, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "mandela-2-part-3", Park: "CPT", RawShed: "Mandela 2 - Part 3", Animals: 1, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "mandela-2-part-7", Park: "CPT", RawShed: "Mandela 2 - Part 7", Animals: 13, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "mandela-2-part-8", Park: "CPT", RawShed: "Mandela 2 - Part 8", Animals: 30, Bundle: "Goat: ET+TT Booster + Goat Pox"},
			{ID: "old-yashoda-1", Park: "CPT", RawShed: "Old Yashoda 1", Animals: 8, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "old-yashoda-5", Park: "CPT", RawShed: "Old Yashoda 5", Animals: 2, Bundle: "Goat: ET+TT Booster + PPR"},
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Unassigned) != 0 {
		t.Fatalf("unassigned blocks = %d, want 0", len(plan.Unassigned))
	}
	if len(plan.Days) != 1 {
		t.Fatalf("days = %d, want 1", len(plan.Days))
	}
	if got := plan.Days[0].Assigned; got != 321 {
		t.Fatalf("assigned animals = %d, want 321", got)
	}

	totals := totalsByOperator(plan.Days[0])
	if totals["Amit Kumar"] != 114 || totals["Darshan Talwar"] != 118 || totals["Sagar Mahoor"] != 89 {
		t.Fatalf("operator totals = %#v, want Amit 114 Darshan 118 Sagar 89", totals)
	}
	assertShedAssignment(t, plan.Days[0], "Amit Kumar", "Gandhi", 114)
	assertShedAssignment(t, plan.Days[0], "Darshan Talwar", "Godel 1", 118)
	assertShedAssignment(t, plan.Days[0], "Sagar Mahoor", "Godel 2", 32)
	assertShedAssignment(t, plan.Days[0], "Sagar Mahoor", "Mandela 2", 47)
	assertShedAssignment(t, plan.Days[0], "Sagar Mahoor", "Old Yashoda", 10)
}

func TestOperatorDrivePlannerSpillsToNextDateWithThatDatesAvailability(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate: date(2026, 7, 23),
		Availability: []DriveDateAvailability{
			{
				Date: date(2026, 7, 23),
				Operators: []DriveOperator{
					{ID: "amit", Name: "Amit Kumar", Cap: 50, Available: true},
					{ID: "darshan", Name: "Darshan Talwar", Cap: 50, Available: true},
				},
			},
			{
				Date: date(2026, 7, 24),
				Operators: []DriveOperator{
					{ID: "amit", Name: "Amit Kumar", Cap: 50, Available: false},
					{ID: "darshan", Name: "Darshan Talwar", Cap: 50, Available: true},
					{ID: "sagar", Name: "Sagar Mahoor", Cap: 50, Available: true},
				},
			},
		},
		WorkBlocks: []DriveWorkBlock{
			{ID: "a", Park: "CPT", RawShed: "Alpha 1", Animals: 50},
			{ID: "b", Park: "CPT", RawShed: "Beta 1", Animals: 50},
			{ID: "c", Park: "CPT", RawShed: "Charlie 1", Animals: 50},
			{ID: "d", Park: "CPT", RawShed: "Delta 1", Animals: 50},
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Days) != 2 {
		t.Fatalf("days = %d, want 2", len(plan.Days))
	}
	if plan.Days[0].Assigned != 100 || plan.Days[1].Assigned != 100 {
		t.Fatalf("assigned by day = %d/%d, want 100/100", plan.Days[0].Assigned, plan.Days[1].Assigned)
	}
	secondDayTotals := totalsByOperator(plan.Days[1])
	if secondDayTotals["Amit Kumar"] != 0 || secondDayTotals["Darshan Talwar"] != 50 || secondDayTotals["Sagar Mahoor"] != 50 {
		t.Fatalf("second-day totals = %#v, want Amit excluded and Darshan/Sagar 50 each", secondDayTotals)
	}
}

func TestOperatorDrivePlannerCountsMultiVaccineAnimalOnce(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate: date(2026, 7, 23),
		Availability: []DriveDateAvailability{{
			Date:      date(2026, 7, 23),
			Operators: []DriveOperator{{ID: "amit", Name: "Amit Kumar", Cap: 1, Available: true}},
		}},
		WorkBlocks: []DriveWorkBlock{{
			ID:      "goat-1",
			Park:    "CPT",
			RawShed: "Gandhi 2",
			Animals: 1,
			Bundle:  "ET+TT Booster + PPR + Goat Pox",
		}},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got := plan.Days[0].Assigned; got != 1 {
		t.Fatalf("assigned animals = %d, want 1 unique animal despite multi-vaccine bundle", got)
	}
}

func TestOperatorDrivePlannerSplitsOversizedPartitionAcrossOperatorsAndDays(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate: date(2026, 7, 23),
		Availability: []DriveDateAvailability{
			{
				Date: date(2026, 7, 23),
				Operators: []DriveOperator{
					{ID: "amit", Name: "Amit Kumar", Cap: 50, Available: true},
					{ID: "darshan", Name: "Darshan Talwar", Cap: 50, Available: true},
				},
			},
			{
				Date: date(2026, 7, 24),
				Operators: []DriveOperator{
					{ID: "sagar", Name: "Sagar Mahoor", Cap: 50, Available: true},
				},
			},
		},
		WorkBlocks: []DriveWorkBlock{{
			ID:      "large-partition",
			Park:    "CPT",
			RawShed: "Gandhi 1",
			Animals: 120,
			Bundle:  "ET+TT Booster + PPR",
		}},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Unassigned) != 0 {
		t.Fatalf("unassigned blocks = %d, want 0", len(plan.Unassigned))
	}
	if plan.Days[0].Assigned != 100 || plan.Days[1].Assigned != 20 {
		t.Fatalf("assigned by day = %d/%d, want 100/20", plan.Days[0].Assigned, plan.Days[1].Assigned)
	}
	if totals := totalsByOperator(plan.Days[0]); totals["Amit Kumar"] != 50 || totals["Darshan Talwar"] != 50 {
		t.Fatalf("first-day totals = %#v, want Amit/Darshan 50 each", totals)
	}
	if totals := totalsByOperator(plan.Days[1]); totals["Sagar Mahoor"] != 20 {
		t.Fatalf("second-day totals = %#v, want Sagar 20", totals)
	}
	for _, day := range plan.Days {
		for _, assignment := range day.Assignments {
			if assignment.PhysicalShed != "Gandhi" {
				t.Fatalf("physical shed = %q, want Gandhi", assignment.PhysicalShed)
			}
			if len(assignment.Partitions) != 1 || assignment.Partitions[0] != "1" {
				t.Fatalf("partitions = %#v, want partition 1", assignment.Partitions)
			}
		}
	}
}

func TestOperatorDrivePlannerPrioritizesLatestSafeDateAndFlagsBreach(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate: date(2026, 7, 23),
		Availability: []DriveDateAvailability{{
			Date: date(2026, 7, 23),
			Operators: []DriveOperator{{
				ID: "amit", Name: "Amit Kumar", Cap: 50, Available: true,
			}},
		}},
		WorkBlocks: []DriveWorkBlock{
			{
				ID:             "later-safe",
				Park:           "CPT",
				RawShed:        "Godel 1 - Part 1",
				Animals:        50,
				Bundle:         "ET+TT Booster + PPR",
				DueDate:        date(2026, 7, 23),
				LatestSafeDate: date(2026, 7, 30),
			},
			{
				ID:             "crossing-buffer",
				Park:           "CPT",
				RawShed:        "Gandhi 1",
				Animals:        50,
				Bundle:         "ET+TT Booster + PPR",
				DueDate:        date(2026, 7, 21),
				LatestSafeDate: date(2026, 7, 23),
			},
			{
				ID:             "breach",
				Park:           "CPT",
				RawShed:        "Gandhi 2",
				Animals:        25,
				Bundle:         "ET+TT Booster + Goat Pox",
				DueDate:        date(2026, 7, 21),
				LatestSafeDate: date(2026, 7, 23),
			},
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	assertShedAssignment(t, plan.Days[0], "Amit Kumar", "Gandhi", 75)
	if plan.Days[0].Assigned != 75 {
		t.Fatalf("assigned animals = %d, want 75 over-cap latest-safe animals", plan.Days[0].Assigned)
	}
	if len(plan.Unassigned) != 1 || plan.Unassigned[0].ID != "later-safe" {
		t.Fatalf("unassigned blocks = %#v, want only later-safe work left for a future date", plan.Unassigned)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want no unresolved breach after over-cap latest-safe assignment", plan.Warnings)
	}
	foundOverCap := false
	for _, assignment := range plan.Days[0].Assignments {
		for _, warning := range assignment.Warnings {
			if warning == "over_cap_required_latest_safe" {
				foundOverCap = true
			}
		}
	}
	if !foundOverCap {
		t.Fatalf("latest-safe over-cap assignment warning missing: %#v", plan.Days[0].Assignments)
	}
}

func totalsByOperator(day DrivePlanDay) map[string]int {
	totals := map[string]int{}
	for _, assignment := range day.Assignments {
		totals[assignment.OperatorName] += assignment.Animals
	}
	return totals
}

func assertShedAssignment(t *testing.T, day DrivePlanDay, operator, shed string, animals int) {
	t.Helper()
	for _, assignment := range day.Assignments {
		if assignment.OperatorName == operator && assignment.PhysicalShed == shed {
			if assignment.Animals != animals {
				t.Fatalf("%s/%s animals = %d, want %d", operator, shed, assignment.Animals, animals)
			}
			return
		}
	}
	t.Fatalf("missing assignment for %s/%s", operator, shed)
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
