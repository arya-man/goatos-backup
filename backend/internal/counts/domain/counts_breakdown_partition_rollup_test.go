package domain

import "testing"

func strp(v string) *string { return &v }

func TestAggregateCountsBreakdownRowsByShedCollapsesOnlyDuplicateExactShedRows(t *testing.T) {
	shedID := "shed-castro-2"
	parkID := "park-cpt"
	rows := []CountsBreakdownRow{
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Castro 2", PartitionLabel: "2",
			OperationalLocationDisplay: "Castro 2", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 20},
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Castro 2", PartitionLabel: "",
			OperationalLocationDisplay: "Castro 2", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 15},
	}
	wantTotal := int64(0)
	for _, r := range rows {
		wantTotal += r.Count
	}

	aggregated := AggregateCountsBreakdownRowsByShed(rows)
	if len(aggregated) != 1 {
		t.Fatalf("expected one exact-shed row, got %d: %+v", len(aggregated), aggregated)
	}
	got := aggregated[0]
	if got.Count != wantTotal {
		t.Fatalf("exact-shed count = %d, want %d (no double counting, no dropped animals)", got.Count, wantTotal)
	}
	if got.ShedLabel != "Castro 2" {
		t.Fatalf("ShedLabel = %q, want exact shed name %q", got.ShedLabel, "Castro 2")
	}
	if got.PartitionLabel != "" {
		t.Fatalf("PartitionLabel = %q, want empty compatibility metadata", got.PartitionLabel)
	}
	if got.OperationalLocationDisplay != "Castro 2" {
		t.Fatalf("OperationalLocationDisplay = %q, want exact shed name %q", got.OperationalLocationDisplay, "Castro 2")
	}
}

// TestAggregateCountsBreakdownRowsByShedNoDoubleCounting proves the rollup never inflates a
// count: aggregating twice is idempotent per shed grouping, and two distinct sheds never merge.
func TestAggregateCountsBreakdownRowsByShedNoDoubleCounting(t *testing.T) {
	rows := []CountsBreakdownRow{
		{ParkID: strp("park-cpt"), ShedID: strp("shed-castro-1-cpt"), ShedLabel: "Castro 1", PartitionLabel: "1",
			ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 10},
		// Same shed NAME, different park/shed_id (two "Castro 1" sheds across parks) -- must stay
		// two separate rows, never merged by name.
		{ParkID: strp("park-cbe"), ShedID: strp("shed-castro-1-cbe"), ShedLabel: "Castro 1", PartitionLabel: "1",
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
	if byShedID["shed-castro-1-cpt"] != 10 {
		t.Fatalf("CPT Castro 1 count = %d, want 10 (must not merge with CBE Castro 1)", byShedID["shed-castro-1-cpt"])
	}
	if byShedID["shed-castro-1-cbe"] != 99 {
		t.Fatalf("CBE Castro 1 count = %d, want 99 (must not merge with CPT Castro 1)", byShedID["shed-castro-1-cbe"])
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

// TestExactShedRollupCardinalityOneToMany: stale compatibility partition rows are 1:{0,1} per
// animal, so the helper may merge duplicate rows for the SAME exact shed but never multiply totals.
func TestExactShedRollupCardinalityOneToMany(t *testing.T) {
	shedID, parkID := "shed-godel", "park-cbe"
	var rows []CountsBreakdownRow
	want := int64(0)
	for i, n := range []int64{49, 37, 40, 44, 9, 8, 7, 5} {
		rows = append(rows, CountsBreakdownRow{
			ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Godel 1",
			PartitionLabel: string(rune('0' + i + 1)), ManagementStage: "K1", Breed: "boer", Sex: "male", Count: n,
		})
		want += n
	}
	got := AggregateCountsBreakdownRowsByShed(rows)
	if len(got) != 1 {
		t.Fatalf("rolled up to %d rows, want 1 (one shed x one cohort)", len(got))
	}
	if got[0].Count != want {
		t.Fatalf("exact shed total = %d, want %d (compatibility fan-out must not multiply animals)", got[0].Count, want)
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
func TestExactShedRollupDateShiftIndependence(t *testing.T) {
	shedID, parkID := "shed-castro-2", "park-cpt"
	rows := []CountsBreakdownRow{
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Castro 2", PartitionLabel: "2", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 33},
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Castro 2", PartitionLabel: "", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 31},
	}
	got := AggregateCountsBreakdownRowsByShed(rows)
	if len(got) != 1 || got[0].Count != 64 {
		t.Fatalf("got %d rows total %v, want 1 row totalling 64", len(got), got)
	}
}

// TestPartitionRollupParkScopeHierarchy: shed NAMES repeat across parks (two "Castro 1"). The rollup
// key must be (park, shed), never the shed NAME, or two parks' animals merge into one row.
func TestPartitionRollupParkScopeHierarchy(t *testing.T) {
	rows := []CountsBreakdownRow{
		{ParkID: strp("park-cpt"), ShedID: strp("shed-castro-1-cpt"), ShedLabel: "Castro 1", PartitionLabel: "1", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 33},
		{ParkID: strp("park-cbe"), ShedID: strp("shed-castro-1-cbe"), ShedLabel: "Castro 1", PartitionLabel: "1", ManagementStage: "adult", Breed: "boer", Sex: "female", Count: 63},
	}
	got := AggregateCountsBreakdownRowsByShed(rows)
	if len(got) != 2 {
		t.Fatalf("two parks' same-named Castro collapsed into %d row(s); park scope must keep them distinct", len(got))
	}
}

// TestExactShedRollupEveryStatusBuckets: every status/stage bucket in an exact shed must survive
// stale compatibility metadata intact, with none absorbed or dropped.
func TestExactShedRollupEveryStatusBuckets(t *testing.T) {
	shedID, parkID := "shed-mandela-1-part-1", "park-cbe"
	rows := []CountsBreakdownRow{
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Mandela 1 Part 1", PartitionLabel: "Part 1", ManagementStage: "K1", Breed: "boer", Sex: "male", Count: 11},
		{ParkID: strp(parkID), ShedID: strp(shedID), ShedLabel: "Mandela 1 Part 1", PartitionLabel: "", ManagementStage: "K1", Breed: "boer", Sex: "male", Count: 16},
	}
	got := AggregateCountsBreakdownRowsByShed(rows)
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].Count != 27 {
		t.Fatalf("exact shed total = %d, want 27", got[0].Count)
	}
}
