package domain

import "testing"

// Adversarial cover for the Sheds directory pivot, in the four shapes the aggregate rule names.
// Each one models a defect that actually occurred while this screen was being built, rather than a
// hypothetical: the fan-out and the park-scope cases were both live bugs, and the capacity-state
// case is the distinction the screen's whole honesty rests on.

// TestShedDirectoryOneToManyPensDoNotFanTheShed: a shed contributes ONE row per pen, and those rows
// must stay separate rather than collapsing onto the shed or multiplying it.
//
// The live defect this models: the capacity seed sent one (park, shed) pair per PEN to its
// resolver, so a ten-pen shed looked like ten sheds sharing a name and tripped the ambiguity check
// on entirely correct data. The same one-to-many shape runs through the pivot.
func TestShedDirectoryOneToManyPensDoNotFanTheShed(t *testing.T) {
	entries := []ShedDirectoryEntry{}
	for _, pen := range []string{"Part 1", "Part 2", "Part 3"} {
		entries = append(entries, ShedDirectoryEntry{
			ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore",
			ShedID: "shed-godel-1", ShedName: "Godel 1", PartitionLabel: pen, Capacity: capacityOf(10),
		})
	}

	got := PivotShedDirectory(entries)

	if got.TotalRows != 3 || len(got.Items) != 3 {
		t.Fatalf("three pens must be three rows, got total_rows=%d items=%d", got.TotalRows, len(got.Items))
	}
	// One shed, so exactly one park column -- not one per pen.
	if len(got.Parks) != 1 {
		t.Fatalf("a single shed's pens must not fan the park columns: %+v", got.Parks)
	}
	// Every row carries exactly one cell: its own park's. A pen must never inherit a sibling's.
	for _, row := range got.Items {
		if len(row.Cells) != 1 {
			t.Fatalf("row %q has %d cells, want 1", row.OperationalLocationDisplay, len(row.Cells))
		}
	}
}

// TestShedDirectoryParkScopeKeepsSameNamedShedsApart: the farm runs "Castro" in BOTH parks, and the
// pivot pairs them onto one row by label while each cell keeps its own park's shed_id.
//
// The failure this guards is the operational-location rule's park-merge defect: keying by name
// without carrying park identity merged two different buildings, and merged their numbers with them.
func TestShedDirectoryParkScopeKeepsSameNamedShedsApart(t *testing.T) {
	got := PivotShedDirectory([]ShedDirectoryEntry{
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "cbe-castro", ShedName: "Castro", PartitionLabel: "1", Capacity: capacityOf(50)},
		{ParkID: "park-cpt", ParkCode: "CPT", ParkLabel: "Channapatna", ShedID: "cpt-castro", ShedName: "Castro", PartitionLabel: "1", Capacity: capacityOf(30)},
	})

	if len(got.Items) != 1 {
		t.Fatalf("the same pen label in two parks is ONE row, got %d", len(got.Items))
	}
	row := got.Items[0]
	if row.Cells["park-cbe"].ShedID == row.Cells["park-cpt"].ShedID {
		t.Fatal("two parks' sheds collapsed onto one shed_id")
	}
	if *row.Cells["park-cbe"].Capacity == *row.Cells["park-cpt"].Capacity {
		t.Fatalf("each park's capacity must survive its own cell: %+v", row.Cells)
	}
	// And the two park columns stay distinct, each labelled by its own park.
	if len(got.Parks) != 2 || got.Parks[0].ParkID == got.Parks[1].ParkID {
		t.Fatalf("park columns merged: %+v", got.Parks)
	}
}

// TestShedDirectoryEveryStatusOfCapacityStaysDistinct: the three capacity states the screen must
// keep apart -- recorded, recorded-as-zero, and never-recorded -- plus the fourth state of a park
// that has no such location at all.
//
// Collapsing any two tells an operator something the data does not say: a pen nobody has configured
// would read as a pen configured to hold nothing.
func TestShedDirectoryEveryStatusOfCapacityStaysDistinct(t *testing.T) {
	got := PivotShedDirectory([]ShedDirectoryEntry{
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "s1", ShedName: "Mandela 1", PartitionLabel: "Part 1", Tag: "Non-Pregnant", Capacity: capacityOf(13)},
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "s1", ShedName: "Mandela 1", PartitionLabel: "Part 2", Tag: "Non-Pregnant"},
		{ParkID: "park-cbe", ParkCode: "CBE", ParkLabel: "Coimbatore", ShedID: "s1", ShedName: "Mandela 1", PartitionLabel: "Part 3", Capacity: capacityOf(0)},
		{ParkID: "park-cpt", ParkCode: "CPT", ParkLabel: "Channapatna", ShedID: "s2", ShedName: "Mandela 1", PartitionLabel: "Part 1", Tag: "F2-Male", Capacity: capacityOf(10)},
	})

	byLabel := map[string]ShedDirectoryRow{}
	for _, row := range got.Items {
		byLabel[row.OperationalLocationDisplay] = row
	}

	recorded := byLabel["Mandela 1 - Part 1"].Cells["park-cbe"]
	if recorded.Capacity == nil || *recorded.Capacity != 13 {
		t.Fatalf("a recorded capacity must survive: %+v", recorded)
	}
	unrecorded := byLabel["Mandela 1 - Part 2"].Cells["park-cbe"]
	if unrecorded.Capacity != nil {
		t.Fatalf("an unrecorded capacity must stay nil, got %d", *unrecorded.Capacity)
	}
	zero := byLabel["Mandela 1 - Part 3"].Cells["park-cbe"]
	if zero.Capacity == nil || *zero.Capacity != 0 {
		t.Fatalf("a recorded zero must stay 0 and never become nil: %+v", zero)
	}
	if _, present := byLabel["Mandela 1 - Part 2"].Cells["park-cpt"]; present {
		t.Fatal("a park with no such pen must have NO cell, which is a different fact from an unrecorded one")
	}
	// The tag follows the same three-state rule: a pen whose shed has no configured cohort reports
	// "", never a sibling's cohort.
	if zero.Tag != "" {
		t.Fatalf("an unconfigured cohort must stay empty, got %q", zero.Tag)
	}
}
