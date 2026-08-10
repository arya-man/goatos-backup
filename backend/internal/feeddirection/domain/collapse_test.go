package domain

import (
	"reflect"
	"testing"
)

// kg is a resolved cell of the given quantity, echoing the rate and factor it came from.
func kg(item, quantity, gramsPerHead, shedFactor string) ItemQuantity {
	q := ItemQuantity{FeedItem: item, Status: QuantityResolved, QuantityKg: &quantity}
	if gramsPerHead != "" {
		q.GramsPerHead = &gramsPerHead
	}
	if shedFactor != "" {
		q.ShedFactor = &shedFactor
	}
	return q
}

// gap is a cell with no authored ration: no number, and a reason naming the missing coordinate.
func gap(item, detail string) ItemQuantity {
	return ItemQuantity{
		FeedItem:      item,
		Status:        QuantityBlocked,
		BlockedReason: &BlockedReason{Code: BlockReasonNoRationRate, Detail: detail},
	}
}

func findItem(t *testing.T, row DirectionRow, item string) ItemQuantity {
	t.Helper()
	for _, got := range row.Items {
		if got.FeedItem == item {
			return got
		}
	}
	t.Fatalf("row for %s has no %q cell (items: %+v)", row.ShedLabel, item, row.Items)
	return ItemQuantity{}
}

// Two breeds standing in one pen are ONE feeding instruction, not two rows an operator re-adds by
// hand. This is the maintainer decision of 2026-08-10 and the reason this fold exists.
func TestCollapseMergesRationGrainsOfOneLocationIntoOneRow(t *testing.T) {
	rows := []DirectionRow{
		{
			ShedID: "shed-castro", ShedLabel: "Castro", PartitionLabel: "1",
			ShedTag: "F2-Male", Breed: "Beetal", RationGroup: "Beetal/Sirohi",
			SessionNo: 1, SessionLabel: "Morning", HeadCount: 40,
			Items:          []ItemQuantity{kg("Concentrate", "8.000", "200.000", "1.0")},
			SessionTotalKg: "8.000",
		},
		{
			ShedID: "shed-castro", ShedLabel: "Castro", PartitionLabel: "1",
			ShedTag: "F2-Male", Breed: "Sojat", RationGroup: "Beetal/Sirohi",
			SessionNo: 1, SessionLabel: "Morning", HeadCount: 25,
			Items:          []ItemQuantity{kg("Concentrate", "5.000", "200.000", "1.0")},
			SessionTotalKg: "5.000",
		},
	}

	got := CollapseDirectionRowsByLocation(rows)

	if len(got) != 1 {
		t.Fatalf("expected ONE row for one pen in one session, got %d: %+v", len(got), got)
	}
	row := got[0]
	if row.HeadCount != 65 {
		t.Errorf("head count = %d, want 65 (40 + 25 summed across the pen's grains)", row.HeadCount)
	}
	// Dominant breed first, exactly as describeGrains orders an experiment row's columns.
	if row.Breed != "Beetal + Sojat" {
		t.Errorf("breed = %q, want %q", row.Breed, "Beetal + Sojat")
	}
	if row.ShedTag != "F2-Male" {
		t.Errorf("shed tag = %q, want %q (one distinct value must not be joined to itself)", row.ShedTag, "F2-Male")
	}
	if row.RationGroup != "Beetal/Sirohi" {
		t.Errorf("ration group = %q, want %q", row.RationGroup, "Beetal/Sirohi")
	}
	concentrate := findItem(t, row, "Concentrate")
	if concentrate.QuantityKg == nil || *concentrate.QuantityKg != "13.000" {
		t.Errorf("concentrate = %v, want 13.000 (8.000 + 5.000)", concentrate.QuantityKg)
	}
	if row.SessionTotalKg != "13.000" {
		t.Errorf("session total = %q, want %q", row.SessionTotalKg, "13.000")
	}
	if row.Blocked {
		t.Error("row is blocked with no gap anywhere in it")
	}
	// Both grains resolved through the same authored rate, so echoing it is still honest.
	if concentrate.GramsPerHead == nil || *concentrate.GramsPerHead != "200.000" {
		t.Errorf("grams per head = %v, want 200.000 (both grains agree)", concentrate.GramsPerHead)
	}
}

// A rate that differs between the grains has no single value to echo, so neither is printed as "the"
// rate. Dropping it is honest; picking one would attribute the pen's whole quantity to a rate that
// produced only part of it.
func TestCollapseDropsEchoedRateWhenGrainsDisagree(t *testing.T) {
	rows := []DirectionRow{
		{
			ShedID: "shed-a", PartitionLabel: "", ShedTag: "F2-Male", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: 1, HeadCount: 10,
			Items: []ItemQuantity{kg("Concentrate", "2.000", "200.000", "1.0")},
		},
		{
			ShedID: "shed-a", PartitionLabel: "", ShedTag: "ICU-Kid", Breed: "Beetal",
			RationGroup: "Kid", SessionNo: 1, HeadCount: 5,
			Items: []ItemQuantity{kg("Concentrate", "0.500", "100.000", "1.0")},
		},
	}

	got := CollapseDirectionRowsByLocation(rows)

	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	concentrate := findItem(t, got[0], "Concentrate")
	if concentrate.QuantityKg == nil || *concentrate.QuantityKg != "2.500" {
		t.Errorf("concentrate = %v, want 2.500", concentrate.QuantityKg)
	}
	if concentrate.GramsPerHead != nil {
		t.Errorf("grams per head = %q, want none: the two grains used 200 and 100 g/head", *concentrate.GramsPerHead)
	}
	if concentrate.ShedFactor == nil || *concentrate.ShedFactor != "1.0" {
		t.Errorf("shed factor = %v, want 1.0: both grains agree on it", concentrate.ShedFactor)
	}
	if got[0].ShedTag != "F2-Male + ICU-Kid" {
		t.Errorf("shed tag = %q, want %q (head count descending)", got[0].ShedTag, "F2-Male + ICU-Kid")
	}
	if got[0].RationGroup != "Beetal/Sirohi + Kid" {
		t.Errorf("ration group = %q, want %q", got[0].RationGroup, "Beetal/Sirohi + Kid")
	}
}

// MAINTAINER DECISION 2026-08-10: a pen where only part of the ration is authored PRINTS what is
// configured and names the gap. The operator feeds those animals and records their video rather than
// being handed a blank cell.
func TestCollapsePrintsConfiguredQuantityAndNamesTheGap(t *testing.T) {
	rows := []DirectionRow{
		{
			ShedID: "shed-a", ShedTag: "F2-Male", Breed: "Beetal", RationGroup: "Beetal/Sirohi",
			SessionNo: 1, HeadCount: 40,
			Items: []ItemQuantity{kg("Concentrate", "8.000", "200.000", "1.0")},
		},
		{
			ShedID: "shed-a", ShedTag: "F2-Male", Breed: "Sojat", RationGroup: "",
			SessionNo: 1, HeadCount: 25, Blocked: true,
			Items: []ItemQuantity{gap("Concentrate", `adult breed "Sojat" has no ration-group mapping`)},
		},
	}

	got := CollapseDirectionRowsByLocation(rows)

	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	row := got[0]
	concentrate := findItem(t, row, "Concentrate")
	if concentrate.Status != QuantityResolved {
		t.Errorf("status = %q, want %q: the configured half must still be feedable", concentrate.Status, QuantityResolved)
	}
	if concentrate.QuantityKg == nil || *concentrate.QuantityKg != "8.000" {
		t.Errorf("concentrate = %v, want 8.000 (the configured grain only)", concentrate.QuantityKg)
	}
	if !row.Blocked {
		t.Error("row must stay flagged: its total covers only part of the pen")
	}
	if len(row.BlockedReasons) != 1 {
		t.Fatalf("expected exactly 1 named gap, got %d: %+v", len(row.BlockedReasons), row.BlockedReasons)
	}
	if row.BlockedReasons[0].Detail == "" {
		t.Error("the gap must name which group is unconfigured, or the operator cannot close it")
	}
	if row.HeadCount != 65 {
		t.Errorf("head count = %d, want 65: the unconfigured animals are still standing in the pen", row.HeadCount)
	}
}

// The blocked-vs-zero contract survives the fold. An item nothing resolved has NO number -- it is not
// summed to a clean 0 kg that reads as a complete instruction.
func TestCollapseKeepsFullyUnresolvedCellBlockedAndNumberless(t *testing.T) {
	rows := []DirectionRow{
		{
			ShedID: "shed-a", ShedTag: "F2-Male", Breed: "Beetal", SessionNo: 1, HeadCount: 10, Blocked: true,
			Items: []ItemQuantity{gap("Concentrate", "no rate for Beetal/Sirohi"), kg("Maize", "3.000", "300.000", "1.0")},
		},
		{
			ShedID: "shed-a", ShedTag: "F2-Male", Breed: "Sojat", SessionNo: 1, HeadCount: 5, Blocked: true,
			Items: []ItemQuantity{gap("Concentrate", "no rate for Sojat"), kg("Maize", "1.500", "300.000", "1.0")},
		},
	}

	got := CollapseDirectionRowsByLocation(rows)

	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	concentrate := findItem(t, got[0], "Concentrate")
	if concentrate.Status != QuantityBlocked {
		t.Errorf("status = %q, want %q", concentrate.Status, QuantityBlocked)
	}
	if concentrate.QuantityKg != nil {
		t.Errorf("quantity = %q, want none: nothing in this pen has an authored ration for it", *concentrate.QuantityKg)
	}
	if concentrate.BlockedReason == nil {
		t.Error("a blocked cell must carry its reason")
	}
	// The other item resolved on both grains and is unaffected.
	maize := findItem(t, got[0], "Maize")
	if maize.QuantityKg == nil || *maize.QuantityKg != "4.500" {
		t.Errorf("maize = %v, want 4.500", maize.QuantityKg)
	}
	if got[0].SessionTotalKg != "4.500" {
		t.Errorf("session total = %q, want 4.500: a blocked cell contributes nothing", got[0].SessionTotalKg)
	}
	if len(got[0].BlockedReasons) != 2 {
		t.Errorf("expected both distinct gaps named, got %+v", got[0].BlockedReasons)
	}
}

// Two pens of one shed are two feeding instructions, and two sessions are two more. The fold key is
// the same (shed, partition, session) key the packing bag uses, so a row and its bag always describe
// the same physical pen.
func TestCollapseKeepsPartitionsAndSessionsApart(t *testing.T) {
	rows := []DirectionRow{
		{ShedID: "s", ShedLabel: "Castro", PartitionLabel: "1", SessionNo: 1, Breed: "Beetal", HeadCount: 10},
		{ShedID: "s", ShedLabel: "Castro", PartitionLabel: "2", SessionNo: 1, Breed: "Beetal", HeadCount: 20},
		{ShedID: "s", ShedLabel: "Castro", PartitionLabel: "1", SessionNo: 2, Breed: "Beetal", HeadCount: 10},
		// An authoring variant of the SAME pen must not split it into two rows.
		{ShedID: "s", ShedLabel: "Castro", PartitionLabel: "1", SessionNo: 1, Breed: "Sojat", HeadCount: 5},
	}

	got := CollapseDirectionRowsByLocation(rows)

	if len(got) != 3 {
		t.Fatalf("expected 3 rows (pen 1 x2 sessions, pen 2 x1), got %d: %+v", len(got), got)
	}
	if got[0].PartitionLabel != "1" || got[0].SessionNo != 1 || got[0].HeadCount != 15 {
		t.Errorf("first row = pen %q session %d head %d, want pen 1 session 1 head 15",
			got[0].PartitionLabel, got[0].SessionNo, got[0].HeadCount)
	}
	if got[1].PartitionLabel != "2" || got[1].HeadCount != 20 {
		t.Errorf("second row = pen %q head %d, want pen 2 head 20", got[1].PartitionLabel, got[1].HeadCount)
	}
	if got[2].SessionNo != 2 {
		t.Errorf("third row session = %d, want 2", got[2].SessionNo)
	}
}

// An experiment sheet is already one row per pen. The fold must be a no-op on it, including the
// deliberately blank ration group, which must not gain a separator or a fabricated value.
func TestCollapseLeavesAnAlreadyPerLocationRowAlone(t *testing.T) {
	row := DirectionRow{
		ShedID: "shed-a", ShedLabel: "Godel 1", PartitionLabel: "Part 3",
		ShedTag: "F2-Male + F2-Female", Breed: "Anantapur Sheep", RationGroup: "",
		ExperimentArm: "Sheep M NEW", SessionNo: 1, HeadCount: 63,
		HeadCountInformational: true, Workflow: WorkflowExperiment,
		Items:          []ItemQuantity{kg("Concentrate", "12.000", "", "")},
		SessionTotalKg: "12.000",
	}

	got := CollapseDirectionRowsByLocation([]DirectionRow{row})

	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if !reflect.DeepEqual(got[0], row) {
		t.Errorf("experiment row changed under the fold:\n got %+v\nwant %+v", got[0], row)
	}
}

// A pen's completion status must survive the fold intact. Every grain of one (shed, partition,
// session, workflow) is stamped from the SAME completion row before this runs, so the merged row
// reports that one status -- a finished pen still reads finished, and a pen awaiting the verifier
// still reads awaiting.
func TestCollapseCarriesTheCompletionStatusOfThePen(t *testing.T) {
	rows := []DirectionRow{
		{
			ShedID: "shed-a", SessionNo: 1, Breed: "Beetal", HeadCount: 10, Workflow: WorkflowNormal,
			Completed: true, LifecycleStatus: SessionStatusCompleted,
			Items: []ItemQuantity{kg("Concentrate", "2.000", "200.000", "1.0")},
		},
		{
			ShedID: "shed-a", SessionNo: 1, Breed: "Sojat", HeadCount: 5, Workflow: WorkflowNormal,
			Completed: true, LifecycleStatus: SessionStatusCompleted,
			Items: []ItemQuantity{kg("Concentrate", "1.000", "200.000", "1.0")},
		},
	}

	got := CollapseDirectionRowsByLocation(rows)

	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if !got[0].Completed || got[0].LifecycleStatus != SessionStatusCompleted {
		t.Errorf("completed = %v / status = %q, want true / %q: the operator finished this pen",
			got[0].Completed, got[0].LifecycleStatus, SessionStatusCompleted)
	}
}

// TWO WORKFLOWS AT ONE LOCATION STAY TWO ROWS. Their completions are recorded separately, so merging
// them would report one workflow's verdict for both -- showing finished work as outstanding, or
// outstanding work as finished. Reachable because the normal and experiment sheets freeze at
// different clocks on the same day and the serve path unions them.
func TestCollapseKeepsWorkflowsApartSoCompletionsAreNotCrossed(t *testing.T) {
	rows := []DirectionRow{
		{
			ShedID: "shed-a", SessionNo: 1, Breed: "Beetal", HeadCount: 10, Workflow: WorkflowNormal,
			Completed: true, LifecycleStatus: SessionStatusCompleted,
			Items: []ItemQuantity{kg("Concentrate", "2.000", "200.000", "1.0")},
		},
		{
			ShedID: "shed-a", SessionNo: 1, Breed: "Beetal", HeadCount: 10, Workflow: WorkflowExperiment,
			Completed: false, LifecycleStatus: SessionStatusPending,
			Items: []ItemQuantity{kg("Concentrate", "4.000", "", "")},
		},
	}

	got := CollapseDirectionRowsByLocation(rows)

	if len(got) != 2 {
		t.Fatalf("expected the two workflows to stay apart, got %d row(s): %+v", len(got), got)
	}
	if got[0].Workflow != WorkflowNormal || !got[0].Completed {
		t.Errorf("normal row = workflow %q completed %v, want %q / true", got[0].Workflow, got[0].Completed, WorkflowNormal)
	}
	if got[1].Workflow != WorkflowExperiment || got[1].Completed {
		t.Errorf("experiment row = workflow %q completed %v, want %q / false", got[1].Workflow, got[1].Completed, WorkflowExperiment)
	}
	// The hand-authored absolute must not have been added to the per-head quantity either.
	if q := findItem(t, got[1], "Concentrate"); q.QuantityKg == nil || *q.QuantityKg != "4.000" {
		t.Errorf("experiment concentrate = %v, want 4.000 unmixed", q.QuantityKg)
	}
}

// THE SHEET AND THE BAG MUST STILL AGREE TO THE GRAM. Packing sums the already-rounded per-grain
// quantities; this fold sums the identical values, so a fully resolved pen's row and its packing line
// carry the same numbers. This is the property that would break if the fold were moved into the
// planner, where rounding happens once over summed grams instead.
func TestCollapsedRowMatchesThePackingLineItPacks(t *testing.T) {
	perGrain := []DirectionRow{
		{
			ShedID: "shed-a", ShedLabel: "Castro", PartitionLabel: "1",
			ShedTag: "F2-Male", Breed: "Beetal", RationGroup: "Beetal/Sirohi", SessionNo: 1, HeadCount: 40,
			Items: []ItemQuantity{kg("Concentrate", "8.400", "200.000", "1.0"), kg("Maize", "2.100", "50.000", "1.0")},
		},
		{
			ShedID: "shed-a", ShedLabel: "Castro", PartitionLabel: "1",
			ShedTag: "F2-Male", Breed: "Sojat", RationGroup: "Beetal/Sirohi", SessionNo: 1, HeadCount: 25,
			Items: []ItemQuantity{kg("Concentrate", "5.300", "200.000", "1.0"), kg("Maize", "1.400", "50.000", "1.0")},
		},
	}

	// Packing is built from the PER-GRAIN rows, exactly as the serve path does it.
	packing := BuildPackingRows(perGrain, nil)
	collapsed := CollapseDirectionRowsByLocation(perGrain)

	if len(packing) != 1 || len(collapsed) != 1 {
		t.Fatalf("expected 1 packing line and 1 direction row, got %d and %d", len(packing), len(collapsed))
	}
	if packing[0].HeadCount != collapsed[0].HeadCount {
		t.Errorf("head count: sheet %d, bag %d", collapsed[0].HeadCount, packing[0].HeadCount)
	}
	if packing[0].TotalKg != collapsed[0].SessionTotalKg {
		t.Errorf("total: sheet %q, bag %q", collapsed[0].SessionTotalKg, packing[0].TotalKg)
	}
	for _, want := range packing[0].Items {
		got := findItem(t, collapsed[0], want.FeedItem)
		if got.QuantityKg == nil || want.QuantityKg == nil || *got.QuantityKg != *want.QuantityKg {
			t.Errorf("%s: sheet %v, bag %v", want.FeedItem, got.QuantityKg, want.QuantityKg)
		}
	}
}
