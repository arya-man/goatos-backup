package domain

import (
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func strptr(s string) *string { return &s }

// A resolved cell (authored zero included) and a blocked cell, in one row, to prove the
// blocked-vs-zero distinction survives flatten -> reconstruct as two DISTINCT states.
func mixedRow() DirectionRow {
	return DirectionRow{
		ParkID: "park", ParkLabel: "CBE", ShedID: "shed-a", ShedLabel: "Shed A",
		ShedTag: "Non-Pregnant", Breed: "Beetal", RationGroup: "Beetal/Sirohi",
		SessionNo: 1, SessionLabel: "Morning", HeadCount: 10, Workflow: WorkflowNormal,
		SessionTotalKg: "1.000", OverduePending: false,
		Items: []ItemQuantity{
			// Authored zero: fully numeric, resolved.
			{FeedItem: "Milk", Status: QuantityResolved, QuantityKg: strptr("0.000"), GramsPerHead: strptr("0.000"), ShedFactor: strptr("1.0000")},
			// Resolved non-zero.
			{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("1.000"), GramsPerHead: strptr("100.000"), ShedFactor: strptr("1.0000")},
			// Blocked: no number, a reason.
			{FeedItem: "Hay", Status: QuantityBlocked, BlockedReason: &BlockedReason{Code: BlockReasonNoRationRate, Detail: "no rate for Hay"}},
		},
	}
}

// The single most important property of storage: a blocked cell (nil quantity) and an authored zero
// ("0.000") are opposite states, and neither collapses into the other through a persist round trip.
func TestFlattenReconstructPreservesBlockedVsZero(t *testing.T) {
	t.Parallel()
	rows := []DirectionRow{mixedRow()}
	cells := FlattenRows(rows)

	// The blocked cell stores a NULL quantity and a reason; the zero stores 0.000 and no reason.
	var blocked, zero *StoredCell
	for i := range cells {
		switch cells[i].FeedItemLabel {
		case "Hay":
			blocked = &cells[i]
		case "Milk":
			zero = &cells[i]
		}
	}
	if blocked == nil || blocked.QuantityKg != nil {
		t.Fatalf("blocked cell must store a NULL quantity, got %+v", blocked)
	}
	if blocked.BlockedReasonCode == nil || *blocked.BlockedReasonCode != BlockReasonNoRationRate {
		t.Fatalf("blocked cell must carry a reason code, got %+v", blocked)
	}
	if zero == nil || zero.QuantityKg == nil || *zero.QuantityKg != "0.000" {
		t.Fatalf("authored zero must store 0.000, got %+v", zero)
	}
	if zero.BlockedReasonCode != nil {
		t.Fatalf("authored zero must NOT carry a blocked reason")
	}

	got := ReconstructRows(cells)
	if len(got) != 1 || len(got[0].Items) != 3 {
		t.Fatalf("reconstruct shape = %d rows, want 1 row of 3 items", len(got))
	}
	items := map[string]ItemQuantity{}
	for _, it := range got[0].Items {
		items[it.FeedItem] = it
	}
	if items["Hay"].Status != QuantityBlocked || items["Hay"].QuantityKg != nil {
		t.Errorf("Hay must reconstruct as blocked with nil quantity, got %+v", items["Hay"])
	}
	if items["Milk"].Status != QuantityResolved || items["Milk"].QuantityKg == nil || *items["Milk"].QuantityKg != "0.000" {
		t.Errorf("Milk must reconstruct as resolved 0.000, got %+v", items["Milk"])
	}
	if got[0].Blocked != true {
		t.Errorf("a row with a blocked cell must reconstruct with Blocked=true")
	}
}

// A multi-grain shed (two breeds sharing a ration group) must round-trip as two distinct rows in the
// same shed+session, which is why the natural key carries the grain discriminator.
func TestReconstructKeepsMultiGrainShedRowsDistinct(t *testing.T) {
	t.Parallel()
	rows := []DirectionRow{
		{ParkID: "p", ShedID: "shed-a", ShedTag: "Non-Pregnant", Breed: "Beetal", RationGroup: "Beetal/Sirohi", SessionNo: 1, Workflow: WorkflowNormal, SessionTotalKg: "1.000",
			Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("1.000")}}},
		{ParkID: "p", ShedID: "shed-a", ShedTag: "Non-Pregnant", Breed: "Sojat", RationGroup: "Beetal/Sirohi", SessionNo: 1, Workflow: WorkflowNormal, SessionTotalKg: "2.000",
			Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("2.000")}}},
	}
	cells := FlattenRows(rows)
	// The two cells share (shed, session, feed_item) but differ by breed -- so their keys differ.
	if cells[0].Key() == cells[1].Key() {
		t.Fatalf("multi-grain cells collided on the natural key: %+v", cells[0].Key())
	}
	got := ReconstructRows(cells)
	if len(got) != 2 {
		t.Fatalf("multi-grain shed reconstructed %d rows, want 2", len(got))
	}
}

// An identical sheet fingerprints identically (so a re-issue is a no-op) and any change flips it.
func TestFingerprintRowsIsStableAndSensitive(t *testing.T) {
	t.Parallel()
	base := []DirectionRow{mixedRow()}
	if FingerprintRows(base) != FingerprintRows([]DirectionRow{mixedRow()}) {
		t.Fatal("identical sheets must fingerprint identically")
	}
	changed := []DirectionRow{mixedRow()}
	changed[0].Items[1].QuantityKg = strptr("1.500")
	if FingerprintRows(base) == FingerprintRows(changed) {
		t.Fatal("a changed quantity must change the fingerprint")
	}
	// A blocked-vs-zero flip must move the fingerprint too.
	flip := []DirectionRow{mixedRow()}
	flip[0].Items[0].Status = QuantityBlocked
	flip[0].Items[0].QuantityKg = nil
	flip[0].Items[0].BlockedReason = &BlockedReason{Code: BlockReasonNoRationRate}
	if FingerprintRows(base) == FingerprintRows(flip) {
		t.Fatal("a resolved->blocked flip must change the fingerprint")
	}
}

// An amendment must report exactly the changed and removed cells, and the affected sheds.
func TestDiffCellsReportsChangedRemovedAndAffectedSheds(t *testing.T) {
	t.Parallel()
	stored := FlattenRows([]DirectionRow{
		{ShedID: "shed-a", ShedTag: "Non-Pregnant", Breed: "Beetal", SessionNo: 1, Workflow: WorkflowNormal, SessionTotalKg: "1.000",
			Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("1.000")}}},
		{ShedID: "shed-b", ShedTag: "Non-Pregnant", Breed: "Sirohi", SessionNo: 1, Workflow: WorkflowNormal, SessionTotalKg: "2.000",
			Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("2.000")}}},
	})
	// shed-a's quantity changed; shed-b removed entirely.
	generated := FlattenRows([]DirectionRow{
		{ShedID: "shed-a", ShedTag: "Non-Pregnant", Breed: "Beetal", SessionNo: 1, Workflow: WorkflowNormal, SessionTotalKg: "1.500",
			Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("1.500")}}},
	})

	diff := DiffCells(stored, generated)
	if !diff.HasChanges() {
		t.Fatal("expected changes")
	}
	if len(diff.Changed) != 1 || diff.Changed[0].ShedID != "shed-a" {
		t.Fatalf("changed = %+v, want only shed-a", diff.Changed)
	}
	if len(diff.RemovedKeys) != 1 || diff.RemovedKeys[0].ShedID != "shed-b" {
		t.Fatalf("removed = %+v, want only shed-b", diff.RemovedKeys)
	}
	if len(diff.AffectedShedIDs) != 2 {
		t.Fatalf("affected sheds = %v, want both", diff.AffectedShedIDs)
	}

	// An identical recompute is a no-op diff.
	if DiffCells(stored, stored).HasChanges() {
		t.Fatal("an identical recompute must diff to no changes")
	}
}

// The issue instant is the direction_time on the day BEFORE the feed day, in Asia/Kolkata.
func TestExpectedIssueInstantIsDMinusOneAtDirectionTime(t *testing.T) {
	t.Parallel()
	clock := WorkflowClock{Workflow: WorkflowNormal, DirectionTime: "07:00:00", CorrectionTime: "14:00:00"}
	got, err := clock.ExpectedIssueInstant("2026-07-30")
	if err != nil {
		t.Fatalf("ExpectedIssueInstant: %v", err)
	}
	want := time.Date(2026, 7, 29, 7, 0, 0, 0, biztime.DefaultLocation())
	if !got.Equal(want) {
		t.Fatalf("issue instant = %s, want %s (D-1 at 07:00 IST)", got, want)
	}
}

// ---------------------------------------------------------------------------
// HeadCountChangedPens (maintainer decision 2026-08-10)
// ---------------------------------------------------------------------------
//
// The afternoon correction throws away an operator's packing video and makes them film the pen
// again. That is expensive, so it is spent ONLY where the number of mouths actually moved -- which
// is a strictly narrower question than "did this shed's sheet reprint", and a PER-PEN one.

// packingCell builds one stored cell at a given pen and head count, holding everything else fixed so
// a test can move exactly one variable.
func packingCell(shedID, partition string, headCount int64, quantity string) []StoredCell {
	return FlattenRows([]DirectionRow{{
		ShedID: shedID, PartitionLabel: partition, ShedTag: "Non-Pregnant", Breed: "Beetal",
		SessionNo: 1, Workflow: WorkflowNormal, HeadCount: headCount, SessionTotalKg: quantity,
		Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr(quantity)}},
	}})
}

// A COSMETIC change reprints the sheet but must NOT reopen the pen: the packer weighed out the right
// quantity for the right number of animals, and their video still proves it. Reopening here would
// discard good work for a relabelled ration group.
func TestDiffCellsDoesNotReportAPenWhoseHeadCountDidNotMove(t *testing.T) {
	t.Parallel()
	stored := packingCell("shed-a", "1", 40, "1.000")
	generated := packingCell("shed-a", "1", 40, "1.000")
	generated[0].RationGroup = "Kid (renamed)"

	diff := DiffCells(stored, generated)

	if !diff.HasChanges() {
		t.Fatal("a relabelled ration group must still reprint the sheet")
	}
	if len(diff.AffectedShedIDs) != 1 {
		t.Fatalf("affected sheds = %v, want the shed to reprint", diff.AffectedShedIDs)
	}
	if len(diff.HeadCountChangedPens) != 0 {
		t.Fatalf("head-count-changed pens = %+v, want none -- a cosmetic change must not discard a packing video", diff.HeadCountChangedPens)
	}
}

// THE CASE THE FEATURE EXISTS FOR: animals shifted in, so the pen now feeds more mouths and the
// quantities moved with them. The already-filmed bag is for the old count.
func TestDiffCellsReportsAPenWhoseHeadCountMoved(t *testing.T) {
	t.Parallel()
	stored := packingCell("shed-a", "1", 40, "1.000")
	generated := packingCell("shed-a", "1", 50, "1.250")

	diff := DiffCells(stored, generated)

	want := []PenKey{{ShedID: "shed-a", PartitionKey: "1"}}
	if !reflect.DeepEqual(diff.HeadCountChangedPens, want) {
		t.Fatalf("head-count-changed pens = %+v, want %+v", diff.HeadCountChangedPens, want)
	}
}

// THE PEN, NOT THE SHED. Castro 1 / 2 / 3 share one shed_id and hold different animals on different
// rations. AffectedShedIDs cannot express this -- it would reopen all three because one changed,
// making two packers refilm work that never moved. Same distinction migration 000137 exists for.
func TestDiffCellsReportsOnlyTheChangedPenOfASharedShed(t *testing.T) {
	t.Parallel()
	stored := append(packingCell("castro", "1", 40, "1.000"), packingCell("castro", "2", 30, "0.750")...)
	generated := append(packingCell("castro", "1", 40, "1.000"), packingCell("castro", "2", 45, "1.125")...)

	diff := DiffCells(stored, generated)

	want := []PenKey{{ShedID: "castro", PartitionKey: "2"}}
	if !reflect.DeepEqual(diff.HeadCountChangedPens, want) {
		t.Fatalf("head-count-changed pens = %+v, want only Castro - 2; Castro - 1 did not move and its video is still good", diff.HeadCountChangedPens)
	}
	// The shed-level signal deliberately CANNOT tell the two pens apart -- which is exactly why the
	// reopen must not be driven from it.
	if len(diff.AffectedShedIDs) != 1 || diff.AffectedShedIDs[0] != "castro" {
		t.Fatalf("affected sheds = %v, want the one shared shed", diff.AffectedShedIDs)
	}
}

// Animals LEFT: the pen's grain vanished from the recompute. It is packing for fewer mouths than the
// video was shot for, so it reopens on the same terms as one that gained animals.
func TestDiffCellsReportsAPenWhoseGrainDisappeared(t *testing.T) {
	t.Parallel()
	stored := packingCell("shed-a", "1", 40, "1.000")

	diff := DiffCells(stored, nil)

	want := []PenKey{{ShedID: "shed-a", PartitionKey: "1"}}
	if !reflect.DeepEqual(diff.HeadCountChangedPens, want) {
		t.Fatalf("head-count-changed pens = %+v, want the emptied pen", diff.HeadCountChangedPens)
	}
}

// An undivided shed normalizes to the stable 'whole' key, so it matches
// feed_packing_completions.partition_key rather than missing on an empty string.
func TestDiffCellsNormalizesAnUndividedShedToWhole(t *testing.T) {
	t.Parallel()
	diff := DiffCells(packingCell("yashoda", "", 40, "1.000"), packingCell("yashoda", "", 55, "1.375"))

	want := []PenKey{{ShedID: "yashoda", PartitionKey: "whole"}}
	if !reflect.DeepEqual(diff.HeadCountChangedPens, want) {
		t.Fatalf("head-count-changed pens = %+v, want the whole-shed key", diff.HeadCountChangedPens)
	}
}
