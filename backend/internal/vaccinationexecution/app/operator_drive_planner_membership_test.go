package app

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// BUG-029: the planner splits a work block by COUNT ("200 on Jul 24, 124 on Jul 25") and never
// decides WHICH animals go to which operator/date. Downstream persistence then synthesizes the
// membership at write time in goat_id order, so the split is auditable but arbitrary: the fact of
// which goat an operator must actually handle on a given day is an artifact of insertion order, not
// a planning decision. These tests hold the planner to naming its arms.

func namedBlock(id, park, shed, partition string, goatIDs ...string) DriveWorkBlock {
	return DriveWorkBlock{
		ID:      id,
		Park:    park,
		RawShed: shed,
		Animals: len(goatIDs),
		GoatIDs: append([]string(nil), goatIDs...),
	}
}

func goatSeries(prefix string, n int) []string {
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("%s-%03d", prefix, i))
	}
	return out
}

// armMembership flattens a plan into date|operator -> sorted goat ids.
func armMembership(plan DrivePlan) map[string][]string {
	out := make(map[string][]string)
	for _, day := range plan.Days {
		for _, assignment := range day.Assignments {
			key := assignment.Date + "|" + assignment.OperatorID
			ids := append([]string(nil), out[key]...)
			ids = append(ids, assignment.GoatIDs...)
			sort.Strings(ids)
			out[key] = ids
		}
	}
	return out
}

// TestPlannerNamesTheGoatsOnEachAssignmentArm is the core BUG-029 proof: when a work block is split
// across operators because no single operator has room for the whole block, each resulting arm must
// carry the EXACT goat ids it covers -- named by the planner, not inferred later.
//
// Fixture: one park/shed/partition block of 9 named animals; two operators with cap 5 each on one
// date. The count split (5 + 4) is unchanged; what must now also be true is that the two arms name
// 5 and 4 specific goats, that those sets are disjoint, and that their union is exactly the block.
func TestPlannerNamesTheGoatsOnEachAssignmentArm(t *testing.T) {
	goats := goatSeries("goat", 9)
	plan, err := OperatorDrivePlanner{}.Plan(DrivePlanRequest{
		StartDate: date(2026, 7, 23),
		Availability: []DriveDateAvailability{{
			Date: date(2026, 7, 23),
			Operators: []DriveOperator{
				{ID: "amit", Name: "Amit Kumar", Cap: 5, Available: true},
				{ID: "darshan", Name: "Darshan Talwar", Cap: 5, Available: true},
			},
		}},
		WorkBlocks: []DriveWorkBlock{namedBlock("gandhi-1", "CPT", "Gandhi 1", "", goats...)},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Unassigned) != 0 {
		t.Fatalf("unassigned blocks = %d, want 0", len(plan.Unassigned))
	}
	if len(plan.Days) != 1 || len(plan.Days[0].Assignments) != 2 {
		t.Fatalf("days/assignments = %d/%v, want 1 day with 2 arms", len(plan.Days), plan.Days)
	}

	seen := make(map[string]string)
	total := 0
	for _, assignment := range plan.Days[0].Assignments {
		if len(assignment.GoatIDs) != assignment.Animals {
			t.Fatalf("arm %s/%s names %d goats but plans %d animals -- the count and the names must be the same fact",
				assignment.Date, assignment.OperatorID, len(assignment.GoatIDs), assignment.Animals)
		}
		for _, goatID := range assignment.GoatIDs {
			if other, dup := seen[goatID]; dup {
				t.Fatalf("goat %s is named on BOTH %s and %s -- one animal cannot be vaccinated by two operators on one drive",
					goatID, other, assignment.OperatorID)
			}
			seen[goatID] = assignment.OperatorID
		}
		total += assignment.Animals
	}
	if total != 9 {
		t.Fatalf("planned animals = %d, want 9", total)
	}
	if len(seen) != 9 {
		t.Fatalf("named goats = %d, want 9 (every animal in the block must land on exactly one arm)", len(seen))
	}
	for _, goatID := range goats {
		if _, ok := seen[goatID]; !ok {
			t.Fatalf("goat %s was planned by count but never named on any arm", goatID)
		}
	}
	// Per-operator unique-animal cap must still hold after naming.
	for _, assignment := range plan.Days[0].Assignments {
		if assignment.Animals > 5 {
			t.Fatalf("arm %s carries %d animals, over the operator cap of 5", assignment.OperatorID, assignment.Animals)
		}
	}
}

// TestPlannerNamedMembershipIsStableAcrossRuns is the determinism proof. Membership derived from Go
// map iteration (or from any downstream insertion order) is not reproducible: re-planning the same
// unchanged inputs would silently reshuffle which operator handles which animal. The same inputs
// must yield the identical goat-to-arm mapping every time.
func TestPlannerNamedMembershipIsStableAcrossRuns(t *testing.T) {
	request := func() DrivePlanRequest {
		return DrivePlanRequest{
			StartDate: date(2026, 7, 23),
			Availability: []DriveDateAvailability{
				{Date: date(2026, 7, 23), Operators: []DriveOperator{
					{ID: "amit", Name: "Amit Kumar", Cap: 40, Available: true},
					{ID: "darshan", Name: "Darshan Talwar", Cap: 40, Available: true},
					{ID: "sagar", Name: "Sagar Mahoor", Cap: 40, Available: true},
				}},
				{Date: date(2026, 7, 24), Operators: []DriveOperator{
					{ID: "amit", Name: "Amit Kumar", Cap: 40, Available: true},
					{ID: "darshan", Name: "Darshan Talwar", Cap: 40, Available: true},
				}},
			},
			WorkBlocks: []DriveWorkBlock{
				namedBlock("gandhi-1", "CPT", "Gandhi 1", "", goatSeries("gandhi1", 95)...),
				namedBlock("gandhi-2", "CPT", "Gandhi 2", "", goatSeries("gandhi2", 44)...),
				namedBlock("godel-1-part-1", "CPT", "Godel 1 - Part 1", "", goatSeries("godel1p1", 61)...),
				namedBlock("godel-1-part-2", "CPT", "Godel 1 - Part 2", "", goatSeries("godel1p2", 12)...),
			},
		}
	}
	first, err := OperatorDrivePlanner{}.Plan(request())
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	baseline := armMembership(first)
	if len(baseline) == 0 {
		t.Fatalf("no arms produced")
	}
	// Total operator capacity here (3x40 on day one, 2x40 on day two) is 200 against 212 animals of
	// work, so 12 animals legitimately stay unassigned. Every animal must still be accounted for by
	// name exactly once, either on an arm or in the unassigned residual.
	named := make(map[string]bool)
	for _, ids := range baseline {
		for _, goatID := range ids {
			if named[goatID] {
				t.Fatalf("goat %s named on two arms", goatID)
			}
			named[goatID] = true
		}
	}
	if len(named) != 200 {
		t.Fatalf("named animals across arms = %d, want 200 (the day's total operator capacity)", len(named))
	}
	for _, block := range first.Unassigned {
		if len(block.GoatIDs) != block.Animals {
			t.Fatalf("unassigned residual names %d goats but carries %d animals", len(block.GoatIDs), block.Animals)
		}
		for _, goatID := range block.GoatIDs {
			if named[goatID] {
				t.Fatalf("goat %s is both assigned and unassigned", goatID)
			}
			named[goatID] = true
		}
	}
	if len(named) != 95+44+61+12 {
		t.Fatalf("accounted animals = %d, want %d", len(named), 95+44+61+12)
	}
	for run := 0; run < 50; run++ {
		next, err := OperatorDrivePlanner{}.Plan(request())
		if err != nil {
			t.Fatalf("Plan() run %d error = %v", run, err)
		}
		got := armMembership(next)
		if len(got) != len(baseline) {
			t.Fatalf("run %d produced %d arms, baseline has %d", run, len(got), len(baseline))
		}
		for key, want := range baseline {
			have, ok := got[key]
			if !ok {
				t.Fatalf("run %d lost arm %s", run, key)
			}
			if strings.Join(have, ",") != strings.Join(want, ",") {
				t.Fatalf("run %d arm %s membership drifted:\n got: %s\nwant: %s",
					run, key, strings.Join(have, ","), strings.Join(want, ","))
			}
		}
	}
}

// TestPlannerNamesGoatsOnLatestSafeKeepsNormalPartitionWhole covers the window-end branch: a
// normal shed/partition still moves as one unit when today's residual capacity cannot hold it.
func TestPlannerNamesGoatsOnLatestSafeKeepsNormalPartitionWhole(t *testing.T) {
	goats := goatSeries("safe", 25)
	block := namedBlock("gandhi-1", "CPT", "Gandhi 1", "", goats...)
	block.LatestSafeDate = date(2026, 7, 23)
	block.DueDate = date(2026, 7, 23)

	plan, err := OperatorDrivePlanner{}.Plan(DrivePlanRequest{
		StartDate:             date(2026, 7, 23),
		ConfiguredOperatorCap: 200,
		Availability: []DriveDateAvailability{
			{
				Date: date(2026, 7, 23),
				Operators: []DriveOperator{
					{ID: "amit", Name: "Amit Kumar", Cap: 8, Available: true},
					{ID: "darshan", Name: "Darshan Talwar", Cap: 8, Available: true},
				},
			},
			{
				Date: date(2026, 7, 24),
				Operators: []DriveOperator{
					{ID: "sagar", Name: "Sagar Mahoor", Cap: 200, Available: true},
				},
			},
		},
		WorkBlocks: []DriveWorkBlock{block},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Days[0].Assigned != 0 || plan.Days[1].Assigned != 25 {
		t.Fatalf("assigned by day = %d/%d, want 0/25 so latest-safe does not split the partition", plan.Days[0].Assigned, plan.Days[1].Assigned)
	}
	if len(plan.Days[0].Assignments) != 0 {
		t.Fatalf("first day assignments = %#v, want none", plan.Days[0].Assignments)
	}
	if len(plan.Days[1].Assignments) != 1 {
		t.Fatalf("second day assignments = %#v, want one whole-partition arm", plan.Days[1].Assignments)
	}
	assignment := plan.Days[1].Assignments[0]
	if strings.Join(assignment.GoatIDs, ",") != strings.Join(goats, ",") {
		t.Fatalf("second day goats drifted:\n got: %s\nwant: %s", strings.Join(assignment.GoatIDs, ","), strings.Join(goats, ","))
	}
	for _, warning := range assignment.Warnings {
		if warning == "forced_partition_split" || warning == "over_cap_required_latest_safe" {
			t.Fatalf("unexpected split/latest-safe warning on normal partition: %#v", assignment.Warnings)
		}
	}
}

// TestPlannerNamesGoatsOnOversizedBlockSplit covers the non-safe-window oversize branch: a block
// bigger than every operator's remaining room is chunked across operators and whatever is left over
// becomes an unassigned residual. Every chunk and the residual must be named, disjoint, and total.
func TestPlannerNamesGoatsOnOversizedBlockSplit(t *testing.T) {
	goats := goatSeries("over", 30)
	plan, err := OperatorDrivePlanner{}.Plan(DrivePlanRequest{
		StartDate: date(2026, 7, 23),
		Availability: []DriveDateAvailability{{
			Date: date(2026, 7, 23),
			Operators: []DriveOperator{
				{ID: "amit", Name: "Amit Kumar", Cap: 7, Available: true},
				{ID: "darshan", Name: "Darshan Talwar", Cap: 7, Available: true},
			},
		}},
		WorkBlocks: []DriveWorkBlock{namedBlock("gandhi-1", "CPT", "Gandhi 1", "", goats...)},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	seen := make(map[string]bool)
	assigned := 0
	for _, day := range plan.Days {
		for _, assignment := range day.Assignments {
			if len(assignment.GoatIDs) != assignment.Animals {
				t.Fatalf("oversize chunk %s names %d goats but plans %d animals",
					assignment.OperatorID, len(assignment.GoatIDs), assignment.Animals)
			}
			assigned += assignment.Animals
			for _, goatID := range assignment.GoatIDs {
				if seen[goatID] {
					t.Fatalf("goat %s named on two chunks", goatID)
				}
				seen[goatID] = true
			}
		}
	}
	if assigned != 14 {
		t.Fatalf("assigned animals = %d, want 14 (2 operators x cap 7)", assigned)
	}
	residual := 0
	for _, block := range plan.Unassigned {
		if len(block.GoatIDs) != block.Animals {
			t.Fatalf("unassigned residual names %d goats but carries %d animals", len(block.GoatIDs), block.Animals)
		}
		residual += block.Animals
		for _, goatID := range block.GoatIDs {
			if seen[goatID] {
				t.Fatalf("goat %s is both assigned and unassigned", goatID)
			}
			seen[goatID] = true
		}
	}
	if residual != 16 {
		t.Fatalf("unassigned animals = %d, want 16", residual)
	}
	if len(seen) != 30 {
		t.Fatalf("accounted goats = %d, want 30", len(seen))
	}
}

// TestPlannerCountOnlyBlocksStillPlan proves the naming is additive: a caller that has not yet
// plumbed identities through (a count-only work block) must plan exactly as before, with no names.
func TestPlannerCountOnlyBlocksStillPlan(t *testing.T) {
	plan, err := OperatorDrivePlanner{}.Plan(DrivePlanRequest{
		StartDate: date(2026, 7, 23),
		Availability: []DriveDateAvailability{{
			Date:      date(2026, 7, 23),
			Operators: []DriveOperator{{ID: "amit", Name: "Amit Kumar", Cap: 200, Available: true}},
		}},
		WorkBlocks: []DriveWorkBlock{
			{ID: "gandhi-1", Park: "CPT", RawShed: "Gandhi 1", Animals: 42},
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Days) != 1 || len(plan.Days[0].Assignments) != 1 {
		t.Fatalf("unexpected plan shape: %#v", plan.Days)
	}
	if got := plan.Days[0].Assignments[0].Animals; got != 42 {
		t.Fatalf("animals = %d, want 42", got)
	}
	if got := plan.Days[0].Assignments[0].GoatIDs; len(got) != 0 {
		t.Fatalf("count-only block produced names %v, want none", got)
	}
}

var _ = time.Time{}
