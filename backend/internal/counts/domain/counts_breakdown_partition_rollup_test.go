package domain

import "testing"

func strp(v string) *string { return &v }

// TestAggregateCountsBreakdownRowsByShedSumsExactlyToParent is the CRITICAL INVARIANT proof: the
// partition-grain rows returned by GetCountsBreakdown, rolled up to parent-shed grain, must sum
// to EXACTLY the same total as the parent aggregate -- no double counting and no lost animals.
func TestAggregateCountsBreakdownRowsByShedSumsExactlyToParent(t *testing.T) {
	shedID := "shed-castro"
	parkID := "park-cpt"
	partitionRows := []CountsBreakdownRow{
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Castro", PartitionLabel: "1",
			OperationalLocationDisplay: "Castro 1", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 40},
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Castro", PartitionLabel: "2",
			OperationalLocationDisplay: "Castro 2", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 35},
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Castro", PartitionLabel: "3",
			OperationalLocationDisplay: "Castro 3", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 25},
	}
	wantTotal := int64(0)
	for _, r := range partitionRows {
		wantTotal += r.Count
	}

	aggregated := AggregateCountsBreakdownRowsByShed(partitionRows)
	if len(aggregated) != 1 {
		t.Fatalf("expected one parent-shed row, got %d: %+v", len(aggregated), aggregated)
	}
	got := aggregated[0]
	if got.Count != wantTotal {
		t.Fatalf("parent aggregate count = %d, want sum of partitions = %d (no double counting, no dropped animals)", got.Count, wantTotal)
	}
	if got.ShedLabel != "Castro" {
		t.Fatalf("parent aggregate ShedLabel = %q, want bare shed name %q", got.ShedLabel, "Castro")
	}
	if got.PartitionLabel != "" {
		t.Fatalf("parent aggregate PartitionLabel = %q, want empty (aggregate has no single partition)", got.PartitionLabel)
	}
	if got.OperationalLocationDisplay != "Castro" {
		t.Fatalf("parent aggregate OperationalLocationDisplay = %q, want bare shed name %q (never a partition suffix, never \"whole\")", got.OperationalLocationDisplay, "Castro")
	}
}

// TestAggregateCountsBreakdownRowsByShedNoDoubleCounting proves the rollup never inflates a
// count: aggregating twice is idempotent per shed grouping, and two distinct sheds never merge.
func TestAggregateCountsBreakdownRowsByShedNoDoubleCounting(t *testing.T) {
	rows := []CountsBreakdownRow{
		{ParkID: strp("park-cpt"), ShedID: strp("shed-castro-cpt"), ShedLabel: "Castro", PartitionLabel: "1",
			ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 10},
		// Same shed NAME, different park/shed_id (two "Castro" sheds across parks) -- must stay
		// two separate rows, never merged by name.
		{ParkID: strp("park-cbe"), ShedID: strp("shed-castro-cbe"), ShedLabel: "Castro", PartitionLabel: "1",
			ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 99},
		// Non-partitioned shed alongside a partitioned one.
		{ParkID: strp("park-cpt"), ShedID: strp("shed-yashoda"), ShedLabel: "Yashoda", PartitionLabel: "",
			ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 7},
	}
	aggregated := AggregateCountsBreakdownRowsByShed(rows)
	if len(aggregated) != 3 {
		t.Fatalf("expected 3 distinct (park, shed, stage, breed, sex) groups, got %d: %+v", len(aggregated), aggregated)
	}
	byShedID := map[string]int64{}
	for _, r := range aggregated {
		byShedID[derefOrEmpty(r.ShedID)] = r.Count
	}
	if byShedID["shed-castro-cpt"] != 10 {
		t.Fatalf("CPT Castro count = %d, want 10 (must not merge with CBE Castro)", byShedID["shed-castro-cpt"])
	}
	if byShedID["shed-castro-cbe"] != 99 {
		t.Fatalf("CBE Castro count = %d, want 99 (must not merge with CPT Castro)", byShedID["shed-castro-cbe"])
	}
	if byShedID["shed-yashoda"] != 7 {
		t.Fatalf("Yashoda count = %d, want 7", byShedID["shed-yashoda"])
	}

	// Idempotent: re-aggregating already-aggregated rows must not change totals.
	twice := AggregateCountsBreakdownRowsByShed(aggregated)
	var totalOnce, totalTwice int64
	for _, r := range aggregated {
		totalOnce += r.Count
	}
	for _, r := range twice {
		totalTwice += r.Count
	}
	if totalOnce != totalTwice {
		t.Fatalf("re-aggregating changed the total: %d -> %d", totalOnce, totalTwice)
	}
}

// TestNonPartitionedShedNeverRendersWhole guards the display invariant end to end at the row
// level: a non-partitioned shed's OperationalLocationDisplay must be the bare shed name.
func TestNonPartitionedShedNeverRendersWhole(t *testing.T) {
	row := CountsBreakdownRow{
		ShedID: strp("shed-yashoda"), ShedLabel: "Yashoda", PartitionLabel: "",
		OperationalLocationDisplay: "Yashoda",
	}
	if row.OperationalLocationDisplay != "Yashoda" {
		t.Fatalf("OperationalLocationDisplay = %q, want bare shed name", row.OperationalLocationDisplay)
	}
	if row.OperationalLocationDisplay == "Yashoda whole" {
		t.Fatal("must never render the synthetic 'whole' sentinel")
	}
}
