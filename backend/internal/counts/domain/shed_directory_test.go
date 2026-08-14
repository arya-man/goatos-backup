package domain

import "testing"

func capacityOf(v int32) *int32 { return &v }

// TestPivotShedDirectoryPairsTheSameShedNameAcrossParks is the whole point of the directory: the
// farm runs a "Castro" in both parks and reads them side by side, so one name is one row with one
// cell per park -- each cell carrying its OWN park's shed_id, tag and capacity.
func TestPivotShedDirectoryPairsTheSameShedNameAcrossParks(t *testing.T) {
	got := PivotShedDirectory([]ShedDirectoryEntry{
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "shed-cbe-castro", ShedName: "Castro", Tag: "F2-Male", Capacity: capacityOf(150)},
		{ParkID: "park-cpt", ParkCode: "CPT", ParkLabel: "Channapatna", ShedID: "shed-cpt-castro", ShedName: "Castro", Tag: "F2-Male", Capacity: capacityOf(60)},
	})

	if len(got.Items) != 1 {
		t.Fatalf("want 1 pivoted row, got %d", len(got.Items))
	}
	if got.TotalRows != 1 {
		t.Fatalf("want total_rows 1, got %d", got.TotalRows)
	}
	row := got.Items[0]
	if row.ShedName != "Castro" {
		t.Fatalf("want shed name Castro, got %q", row.ShedName)
	}
	// The two cells must stay distinct: this is the park-merge defect the operational-location
	// rule bans, one layer up from SQL.
	if row.Cells["park-cbe"].ShedID != "shed-cbe-castro" || *row.Cells["park-cbe"].Capacity != 150 {
		t.Fatalf("CBE cell wrong: %+v", row.Cells["park-cbe"])
	}
	if row.Cells["park-cpt"].ShedID != "shed-cpt-castro" || *row.Cells["park-cpt"].Capacity != 60 {
		t.Fatalf("CPT cell wrong: %+v", row.Cells["park-cpt"])
	}
}

// TestPivotShedDirectoryOrdersParkColumnsByLabel pins that the park columns come out in a stable
// order, so the two column-pairs do not swap places between calls.
func TestPivotShedDirectoryOrdersParkColumnsByLabel(t *testing.T) {
	got := PivotShedDirectory([]ShedDirectoryEntry{
		{ParkID: "park-cpt", ParkCode: "CPT", ParkLabel: "Channapatna", ShedID: "s1", ShedName: "Gandhi"},
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "s2", ShedName: "Gandhi"},
	})

	if len(got.Parks) != 2 {
		t.Fatalf("want 2 park columns, got %d", len(got.Parks))
	}
	// Ordered by label, so the column pairs do not shuffle between calls.
	if got.Parks[0].ParkLabel != "Channapatna" || got.Parks[1].ParkLabel != "Coimbatore" {
		t.Fatalf("park columns not label-ordered: %+v", got.Parks)
	}
	if got.Parks[0].ParkCode != "CPT" || got.Parks[1].ParkCode != "CBE" {
		t.Fatalf("park codes did not travel with their labels: %+v", got.Parks)
	}
}

// TestPivotShedDirectoryPairsPensAcrossParksAndOrdersThemNumerically pins the pen grain: a pen is
// its own row, keyed by the operational-location LABEL so the two parks' "Godel 1 - Part 3" pair
// up, and pens sort by NUMBER so Part 2 precedes Part 10. String order there reads as missing pens.
func TestPivotShedDirectoryPairsPensAcrossParksAndOrdersThemNumerically(t *testing.T) {
	got := PivotShedDirectory([]ShedDirectoryEntry{
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "s1", ShedName: "Godel 1", PartitionLabel: "Part 10", Capacity: capacityOf(10)},
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "s1", ShedName: "Godel 1", PartitionLabel: "Part 2", Capacity: capacityOf(10)},
		{ParkID: "park-cpt", ParkCode: "CPT", ParkLabel: "Channapatna", ShedID: "s2", ShedName: "Godel 1", PartitionLabel: "Part 2", Capacity: capacityOf(20)},
	})

	if len(got.Items) != 2 {
		t.Fatalf("want one row per pen label, got %d: %+v", len(got.Items), got.Items)
	}
	if got.Items[0].OperationalLocationDisplay != "Godel 1 - Part 2" || got.Items[1].OperationalLocationDisplay != "Godel 1 - Part 10" {
		t.Fatalf("pens not ordered numerically: %q then %q",
			got.Items[0].OperationalLocationDisplay, got.Items[1].OperationalLocationDisplay)
	}
	// Part 2 exists in both parks and pairs onto ONE row, each cell keeping its own shed_id.
	part2 := got.Items[0]
	if part2.Cells["park-cbe"].ShedID != "s1" || part2.Cells["park-cpt"].ShedID != "s2" {
		t.Fatalf("pens did not pair across parks with their own shed ids: %+v", part2.Cells)
	}
	if *part2.Cells["park-cbe"].Capacity != 10 || *part2.Cells["park-cpt"].Capacity != 20 {
		t.Fatalf("each park's pen must keep its OWN capacity: %+v", part2.Cells)
	}
	if part2.PartitionLabel != "Part 2" {
		t.Fatalf("want the human pen label, got %q", part2.PartitionLabel)
	}
}

// TestPivotShedDirectoryRendersAShedWithNoPensBare pins that an unpartitioned shed keeps its bare
// name -- never a synthetic "Q1 whole", and never a dangling separator.
func TestPivotShedDirectoryRendersAShedWithNoPensBare(t *testing.T) {
	got := PivotShedDirectory([]ShedDirectoryEntry{
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "s1", ShedName: "Q1", Tag: "Quarantine", Capacity: capacityOf(5)},
	})

	if got.Items[0].OperationalLocationDisplay != "Q1" {
		t.Fatalf("want bare shed name, got %q", got.Items[0].OperationalLocationDisplay)
	}
	if got.Items[0].PartitionLabel != "" {
		t.Fatalf("a shed with no pens must carry no pen label, got %q", got.Items[0].PartitionLabel)
	}
}

// TestPivotShedDirectoryKeepsAbsentAndUnconfiguredApart pins the three-way distinction the screen
// depends on: a park with no shed of that name has NO cell; a shed with no profile has a cell with
// a nil capacity; and a shed configured to hold nothing has a cell with capacity 0. Collapsing any
// two of those would tell an operator something the data does not say.
func TestPivotShedDirectoryKeepsAbsentAndUnconfiguredApart(t *testing.T) {
	got := PivotShedDirectory([]ShedDirectoryEntry{
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "s1", ShedName: "Q1", Tag: "Quarantine", Capacity: capacityOf(5)},
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "s2", ShedName: "Sumathi 1", Tag: "Non-Pregnant"},
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "s3", ShedName: "Retired barn", Capacity: capacityOf(0)},
		{ParkID: "park-cpt", ParkCode: "CPT", ParkLabel: "Channapatna", ShedID: "s4", ShedName: "Q1", Tag: "Quarantine", Capacity: capacityOf(5)},
	})

	byName := map[string]ShedDirectoryRow{}
	for _, row := range got.Items {
		byName[row.ShedName] = row
	}

	if _, present := byName["Sumathi 1"].Cells["park-cpt"]; present {
		t.Fatal("a park with no shed of that name must have no cell at all")
	}
	if cell := byName["Sumathi 1"].Cells["park-cbe"]; cell.Capacity != nil {
		t.Fatalf("an unconfigured capacity must stay nil, got %v", *cell.Capacity)
	}
	if cell := byName["Retired barn"].Cells["park-cbe"]; cell.Capacity == nil || *cell.Capacity != 0 {
		t.Fatalf("a recorded zero capacity must survive as 0, got %+v", cell)
	}
}

// TestPivotShedDirectoryNeverMergesTwoShedsIntoOneCell proves a duplicate name inside ONE park
// stays visible as a second row rather than overwriting the first. Two buildings must never
// collapse into one -- that is the defect, and hiding it would be worse than showing it.
func TestPivotShedDirectoryNeverMergesTwoShedsIntoOneCell(t *testing.T) {
	got := PivotShedDirectory([]ShedDirectoryEntry{
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "shed-a", ShedName: "Castro", Capacity: capacityOf(150)},
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "shed-b", ShedName: "Castro", Capacity: capacityOf(20)},
	})

	if len(got.Items) != 2 {
		t.Fatalf("want both sheds visible as 2 rows, got %d", len(got.Items))
	}
	seen := map[string]bool{}
	for _, row := range got.Items {
		seen[row.Cells["park-cbe"].ShedID] = true
	}
	if !seen["shed-a"] || !seen["shed-b"] {
		t.Fatalf("a shed was swallowed by the pivot: %+v", got.Items)
	}
}
