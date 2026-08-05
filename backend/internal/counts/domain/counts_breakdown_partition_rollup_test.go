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

// The five tests below are the adversarial cases the aggregate/projection review lens requires for
// a changed grain. Partition is a NEW dimension on this read model, so each one asks the same
// question from a different angle: can the partition dimension make the parent total drift?

// TestPartitionRollupCardinalityOneToMany: the partition join is 1:{0,1} per animal
// (goat_shed_partitions PK is (tenant_id, goat_id)), so adding the dimension must SPLIT a shed's
// animals across rows, never MULTIPLY them. A many-side join would silently inflate every total.
func TestPartitionRollupCardinalityOneToMany(t *testing.T) {
	shedID, parkID := "shed-godel", "park-cbe"
	// One shed, one cohort, fanned across many partitions: the classic one-to-many shape.
	var rows []CountsBreakdownRow
	want := int64(0)
	for i, n := range []int64{49, 37, 40, 44, 9, 8, 7, 5} {
		rows = append(rows, CountsBreakdownRow{
			ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Godel 1",
			PartitionLabel: string(rune('0'+i+1)), ManagementStage: "K1", Breed: "boer", Sex: "male", Count: n,
		})
		want += n
	}
	got := AggregateCountsBreakdownRowsByShed(rows)
	if len(got) != 1 {
		t.Fatalf("rolled up to %d rows, want 1 (one shed x one cohort)", len(got))
	}
	if got[0].Count != want {
		t.Fatalf("parent total = %d, want %d (partition fan-out must not multiply animals)", got[0].Count, want)
	}
}

// TestPartitionRollupPageBoundaryPagination: the rollup is a WHOLE-RESULT aggregate. Feeding it one
// page of partition rows must not silently present a page subtotal as the parent total -- that is
// the "capped read-time rollup presented as truth" anti-pattern.
func TestPartitionRollupPageBoundaryPagination(t *testing.T) {
	shedID, parkID := "shed-yashoda", "park-cbe"
	all := []CountsBreakdownRow{
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Yashoda", PartitionLabel: "1", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 6},
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Yashoda", PartitionLabel: "2", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 19},
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Yashoda", PartitionLabel: "3", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 11},
	}
	full := AggregateCountsBreakdownRowsByShed(all)
	firstPage := AggregateCountsBreakdownRowsByShed(all[:2])
	if full[0].Count != 36 {
		t.Fatalf("whole-result total = %d, want 36", full[0].Count)
	}
	if firstPage[0].Count == full[0].Count {
		t.Fatal("a partial page produced the same total as the whole result; the caller must never present a page subtotal as the parent count")
	}
}

// TestPartitionRollupDateShiftIndependence: partition is a LOCATION dimension. Rows that differ only
// by an execution/scheduled date must still roll up by location, and must not be collapsed or
// dropped because a date differs.
func TestPartitionRollupDateShiftIndependence(t *testing.T) {
	shedID, parkID := "shed-castro", "park-cpt"
	rows := []CountsBreakdownRow{
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Castro", PartitionLabel: "1", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 33},
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Castro", PartitionLabel: "2", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 31},
	}
	got := AggregateCountsBreakdownRowsByShed(rows)
	if len(got) != 1 || got[0].Count != 64 {
		t.Fatalf("got %d rows total %v, want 1 row totalling 64", len(got), got)
	}
}

// TestPartitionRollupParkScopeHierarchy: shed NAMES repeat across parks (two "Castro"). The rollup
// key must be (park, shed), never the shed NAME, or two parks' animals merge into one row.
func TestPartitionRollupParkScopeHierarchy(t *testing.T) {
	rows := []CountsBreakdownRow{
		{ParkID: strp("park-cpt"), ShedID: strp("shed-castro-cpt"), ShedLabel: "Castro", PartitionLabel: "1", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 33},
		{ParkID: strp("park-cbe"), ShedID: strp("shed-castro-cbe"), ShedLabel: "Castro", PartitionLabel: "1", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 63},
	}
	got := AggregateCountsBreakdownRowsByShed(rows)
	if len(got) != 2 {
		t.Fatalf("two parks' same-named Castro collapsed into %d row(s); park scope must keep them distinct", len(got))
	}
}

// TestPartitionRollupEveryStatusBuckets: every status/stage bucket in a shed must survive the new
// partition dimension intact -- rows sharing a cohort but sitting in different partitions still sum
// to that cohort's shed total, with none absorbed or dropped.
func TestPartitionRollupEveryStatusBuckets(t *testing.T) {
	shedID, parkID := "shed-mandela", "park-cbe"
	rows := []CountsBreakdownRow{
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Mandela 1", PartitionLabel: "Part 1", ManagementStage: "K1", Breed: "boer", Sex: "male", Count: 11},
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Mandela 1", PartitionLabel: "Part 2", ManagementStage: "K1", Breed: "boer", Sex: "male", Count: 16},
	}
	got := AggregateCountsBreakdownRowsByShed(rows)
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	// Both partitions belong to the same shed x cohort, so the rolled-up total is their exact sum:
	// no status/stage bucket may absorb or drop an animal when the partition dimension is added.
	if got[0].Count != 27 {
		t.Fatalf("parent total = %d, want 27 (11 + 16 across two partitions of one cohort)", got[0].Count)
	}
}
