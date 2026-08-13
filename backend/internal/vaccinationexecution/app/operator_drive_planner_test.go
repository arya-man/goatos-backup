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
			{ID: "gandhi-2", Park: "CPT", RawShed: "Gandhi 2", Animals: 30, Bundle: "Goat: ET+TT Booster + Goat Pox"},
			{ID: "gandhi-3", Park: "CPT", RawShed: "Gandhi 3", Animals: 42, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "godel-1-part-1", Park: "CPT", RawShed: "Godel 1 Part 1", Animals: 60, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "godel-1-part-3", Park: "CPT", RawShed: "Godel 1 Part 3", Animals: 30, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "godel-1-part-4", Park: "CPT", RawShed: "Godel 1 Part 4", Animals: 30, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "godel-2-part-4", Park: "CPT", RawShed: "Godel 2 Part 4", Animals: 32, Bundle: "Goat: ET+TT Booster + Goat Pox"},
			{ID: "mandela-2-part-1", Park: "CPT", RawShed: "Mandela 2 Part 1", Animals: 1, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "mandela-2-part-2", Park: "CPT", RawShed: "Mandela 2 Part 2", Animals: 2, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "mandela-2-part-3", Park: "CPT", RawShed: "Mandela 2 Part 3", Animals: 1, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "mandela-2-part-7", Park: "CPT", RawShed: "Mandela 2 Part 7", Animals: 13, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "mandela-2-part-8", Park: "CPT", RawShed: "Mandela 2 Part 8", Animals: 30, Bundle: "Goat: ET+TT Booster + Goat Pox"},
			{ID: "old-yashoda-1", Park: "CPT", RawShed: "Old Yashoda 1", Animals: 8, Bundle: "Sheep: ET+TT Booster + PPR"},
			{ID: "old-yashoda-5", Park: "CPT", RawShed: "Old Yashoda 5", Animals: 3, Bundle: "Goat: ET+TT Booster + PPR"},
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
	if got := plan.Days[0].Assigned; got != 324 {
		t.Fatalf("assigned animals = %d, want 324", got)
	}

	totals := totalsByOperator(plan.Days[0])
	if totals["Amit Kumar"] != 104 || totals["Darshan Talwar"] != 101 || totals["Sagar Mahoor"] != 119 {
		t.Fatalf("operator totals = %#v, want Amit 104 Darshan 101 Sagar 119", totals)
	}
	assertShedAssignment(t, plan.Days[0], "Amit Kumar", "Gandhi 1", 42)
	assertShedAssignment(t, plan.Days[0], "Sagar Mahoor", "Gandhi 3", 42)
	assertShedAssignment(t, plan.Days[0], "Darshan Talwar", "Gandhi 2", 30)
	assertShedAssignment(t, plan.Days[0], "Darshan Talwar", "Godel 1 Part 1", 60)
	assertShedAssignment(t, plan.Days[0], "Amit Kumar", "Godel 1 Part 3", 30)
	assertShedAssignment(t, plan.Days[0], "Amit Kumar", "Godel 2 Part 4", 32)
	assertShedAssignment(t, plan.Days[0], "Sagar Mahoor", "Mandela 2 Part 8", 30)
}

func TestOperatorDrivePlannerCPTAdultsOneOperatorKeepsWholeShedsAcrossTwoDays(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate:             date(2027, 1, 6),
		ConfiguredOperatorCap: 200,
		Availability: []DriveDateAvailability{
			{Date: date(2027, 1, 6), Operators: []DriveOperator{{ID: "darshan", Name: "Darshan Talwar", Cap: 200, Available: true}}},
			{Date: date(2027, 1, 7), Operators: []DriveOperator{{ID: "darshan", Name: "Darshan Talwar", Cap: 200, Available: true}}},
		},
		WorkBlocks: []DriveWorkBlock{
			{ID: "gandhi-1", Park: "CPT", RawShed: "Gandhi 1", Animals: 42},
			{ID: "gandhi-2", Park: "CPT", RawShed: "Gandhi 2", Animals: 30},
			{ID: "gandhi-3", Park: "CPT", RawShed: "Gandhi 3", Animals: 42},
			{ID: "godel-1-part-1", Park: "CPT", RawShed: "Godel 1 Part 1", Animals: 60},
			{ID: "godel-1-part-3", Park: "CPT", RawShed: "Godel 1 Part 3", Animals: 30},
			{ID: "godel-1-part-4", Park: "CPT", RawShed: "Godel 1 Part 4", Animals: 30},
			{ID: "godel-2-part-4", Park: "CPT", RawShed: "Godel 2 Part 4", Animals: 32},
			{ID: "mandela-2-part-1", Park: "CPT", RawShed: "Mandela 2 Part 1", Animals: 1},
			{ID: "mandela-2-part-2", Park: "CPT", RawShed: "Mandela 2 Part 2", Animals: 2},
			{ID: "mandela-2-part-3", Park: "CPT", RawShed: "Mandela 2 Part 3", Animals: 1},
			{ID: "mandela-2-part-7", Park: "CPT", RawShed: "Mandela 2 Part 7", Animals: 13},
			{ID: "mandela-2-part-8", Park: "CPT", RawShed: "Mandela 2 Part 8", Animals: 30},
			{ID: "old-yashoda-1", Park: "CPT", RawShed: "Old Yashoda 1", Animals: 8},
			{ID: "old-yashoda-5", Park: "CPT", RawShed: "Old Yashoda 5", Animals: 3},
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Unassigned) != 0 || len(plan.Days) != 2 {
		t.Fatalf("plan=%#v, want all 324 animals assigned over two days", plan)
	}
	if plan.Days[0].Assigned != 199 || plan.Days[1].Assigned != 125 {
		t.Fatalf("assigned by day=%d/%d, want 199/125", plan.Days[0].Assigned, plan.Days[1].Assigned)
	}
	assertShedAssignment(t, plan.Days[0], "Darshan Talwar", "Gandhi 1", 42)
	assertShedAssignment(t, plan.Days[0], "Darshan Talwar", "Gandhi 2", 30)
	assertShedAssignment(t, plan.Days[0], "Darshan Talwar", "Gandhi 3", 42)
	assertShedAssignment(t, plan.Days[0], "Darshan Talwar", "Godel 1 Part 1", 60)
	assertShedAssignment(t, plan.Days[0], "Darshan Talwar", "Mandela 2 Part 7", 13)
	assertShedAssignment(t, plan.Days[1], "Darshan Talwar", "Godel 1 Part 3", 30)
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

func TestOperatorDrivePlannerCarriesWholePartitionPastResidualCapacity(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate:             date(2026, 8, 8),
		ConfiguredOperatorCap: 200,
		Availability: []DriveDateAvailability{
			{
				Date:      date(2026, 8, 8),
				Operators: []DriveOperator{{ID: "darshan", Name: "Darshan Talwar", Cap: 1, Available: true}},
			},
			{
				Date:      date(2026, 8, 9),
				Operators: []DriveOperator{{ID: "darshan", Name: "Darshan Talwar", Cap: 200, Available: true}},
			},
		},
		WorkBlocks: []DriveWorkBlock{{
			ID:             "godel-1-part-1",
			Park:           "CPT",
			PhysicalShed:   "Godel 1",
			Partition:      "Part 1",
			Animals:        3,
			DueDate:        date(2026, 8, 8),
			LatestSafeDate: date(2026, 8, 15),
		}},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Days[0].Assigned != 0 || plan.Days[1].Assigned != 3 {
		t.Fatalf("assigned by day = %d/%d, want 0/3 so the partition moves whole", plan.Days[0].Assigned, plan.Days[1].Assigned)
	}
	if len(plan.Days[0].Assignments) != 0 {
		t.Fatalf("first day assignments = %#v, want none instead of residual-cap split", plan.Days[0].Assignments)
	}
	assertShedAssignment(t, plan.Days[1], "Darshan Talwar", "Godel 1", 3)
}

func TestOperatorDrivePlannerCarriesWholePhysicalShedPastResidualCapacity(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate:             date(2027, 1, 6),
		ConfiguredOperatorCap: 200,
		Availability: []DriveDateAvailability{
			{Date: date(2027, 1, 6), Operators: []DriveOperator{{ID: "darshan", Name: "Darshan Talwar", Cap: 200, Available: true}}},
			{Date: date(2027, 1, 7), Operators: []DriveOperator{{ID: "darshan", Name: "Darshan Talwar", Cap: 200, Available: true}}},
		},
		WorkBlocks: []DriveWorkBlock{
			{ID: "alpha-whole", Park: "CPT", PhysicalShed: "Alpha", Partition: "whole", Animals: 150},
			{ID: "beta-part-1", Park: "CPT", PhysicalShed: "Beta", Partition: "Part 1", Animals: 30},
			{ID: "beta-part-2", Park: "CPT", PhysicalShed: "Beta", Partition: "Part 2", Animals: 30},
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Unassigned) != 0 {
		t.Fatalf("unassigned blocks = %#v, want none", plan.Unassigned)
	}
	if len(plan.Days) != 2 || plan.Days[0].Assigned != 150 || plan.Days[1].Assigned != 60 {
		t.Fatalf("assigned by day = %#v, want 150 then whole Beta shed 60", plan.Days)
	}
	for _, assignment := range plan.Days[0].Assignments {
		if assignment.PhysicalShed == "Beta" {
			t.Fatalf("first day assignment=%#v, Beta must carry whole to the next day", assignment)
		}
	}
	assertShedAssignment(t, plan.Days[1], "Darshan Talwar", "Beta", 60)
}

func TestOperatorDrivePlannerUsesHRMSConfiguredCapBeforeRequestFallback(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate:             date(2027, 1, 6),
		ConfiguredOperatorCap: 200,
		Availability: []DriveDateAvailability{{
			Date: date(2027, 1, 6),
			Operators: []DriveOperator{
				{ID: "amit", Name: "Amit Kumar", Cap: 100, ConfiguredCap: 100, Available: true},
				{ID: "darshan", Name: "Darshan Talwar", Cap: 100, ConfiguredCap: 100, Available: true},
			},
		}},
		WorkBlocks: []DriveWorkBlock{
			{ID: "alpha-part-1", Park: "CPT", PhysicalShed: "Alpha", Partition: "Part 1", Animals: 75},
			{ID: "alpha-part-2", Park: "CPT", PhysicalShed: "Alpha", Partition: "Part 2", Animals: 75},
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Unassigned) != 0 || len(plan.Days) != 1 || plan.Days[0].Assigned != 150 {
		t.Fatalf("plan=%#v, want HRMS cap 100 to allow 75/75 partition fallback on one day", plan)
	}
	if got := totalsByOperator(plan.Days[0]); got["Amit Kumar"] != 75 || got["Darshan Talwar"] != 75 {
		t.Fatalf("operator totals=%#v, want 75/75 under HRMS cap 100", got)
	}
}

func TestOperatorDrivePlannerLatestSafeCarriesWholePartitionPastResidualCapacity(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate:             date(2026, 8, 8),
		ConfiguredOperatorCap: 200,
		Availability: []DriveDateAvailability{
			{
				Date:      date(2026, 8, 8),
				Operators: []DriveOperator{{ID: "darshan", Name: "Darshan Talwar", Cap: 1, Available: true}},
			},
			{
				Date:      date(2026, 8, 9),
				Operators: []DriveOperator{{ID: "darshan", Name: "Darshan Talwar", Cap: 200, Available: true}},
			},
		},
		WorkBlocks: []DriveWorkBlock{{
			ID:             "godel-1-part-1",
			Park:           "CPT",
			PhysicalShed:   "Godel 1",
			Partition:      "Part 1",
			Animals:        3,
			DueDate:        date(2026, 8, 8),
			LatestSafeDate: date(2026, 8, 8),
		}},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Days[0].Assigned != 0 || plan.Days[1].Assigned != 3 {
		t.Fatalf("assigned by day = %d/%d, want 0/3 so latest-safe never cuts the partition", plan.Days[0].Assigned, plan.Days[1].Assigned)
	}
	if len(plan.Days[0].Assignments) != 0 {
		t.Fatalf("first day assignments = %#v, want none instead of latest-safe residual split", plan.Days[0].Assignments)
	}
	assertShedAssignment(t, plan.Days[1], "Darshan Talwar", "Godel 1", 3)
}

func TestOperatorDrivePlannerCountsMultiVaccineAnimalOnce(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate:             date(2026, 7, 23),
		ConfiguredOperatorCap: 50,
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
			if assignment.PhysicalShed != "Gandhi 1" {
				t.Fatalf("physical shed = %q, want Gandhi 1", assignment.PhysicalShed)
			}
			if len(assignment.Partitions) != 1 || assignment.Partitions[0] != "whole" {
				t.Fatalf("partitions = %#v, want whole exact shed", assignment.Partitions)
			}
		}
	}
}

func TestOperatorDrivePlannerOnlySplitsPartitionLargerThanConfiguredCap(t *testing.T) {
	planner := OperatorDrivePlanner{}
	plan, err := planner.Plan(DrivePlanRequest{
		StartDate:             date(2026, 7, 23),
		ConfiguredOperatorCap: 200,
		Availability: []DriveDateAvailability{
			{
				Date: date(2026, 7, 23),
				Operators: []DriveOperator{
					{ID: "amit", Name: "Amit Kumar", Cap: 200, Available: true},
				},
			},
			{
				Date: date(2026, 7, 24),
				Operators: []DriveOperator{
					{ID: "darshan", Name: "Darshan Talwar", Cap: 200, Available: true},
				},
			},
		},
		WorkBlocks: []DriveWorkBlock{{
			ID:        "oversized",
			Park:      "CPT",
			RawShed:   "Mandela 2 Part X",
			Animals:   205,
			GoatIDs:   numberedGoatIDs(205),
			DueDate:   date(2026, 7, 23),
			Bundle:    "ET+TT Booster + Goat Pox",
			Partition: "Part X",
		}},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Unassigned) != 0 {
		t.Fatalf("unassigned = %#v, want oversized partition split across days", plan.Unassigned)
	}
	if plan.Days[0].Assigned != 200 || plan.Days[1].Assigned != 5 {
		t.Fatalf("assigned by day = %d/%d, want 200/5 for the only legal split case", plan.Days[0].Assigned, plan.Days[1].Assigned)
	}
	foundForcedSplit := false
	for _, assignment := range plan.Days[0].Assignments {
		for _, warning := range assignment.Warnings {
			if warning == "forced_partition_split" {
				foundForcedSplit = true
			}
		}
	}
	if !foundForcedSplit {
		t.Fatalf("oversized partition split warning missing: %#v", plan.Days[0].Assignments)
	}
}

func numberedGoatIDs(count int) []string {
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, "goat-"+time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC).Format("150405"))
	}
	return out
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
